package downloadclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// SABnzbdClient talks to a SABnzbd usenet download client over its JSON API.
type SABnzbdClient struct {
	id       string
	base     string // origin + url base, e.g. http://host:8080
	apiKey   string
	category string
	http     *http.Client
}

func newSABnzbd(id, base, apiKey, category string, hc *http.Client) *SABnzbdClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &SABnzbdClient{id: id, base: strings.TrimRight(base, "/"), apiKey: apiKey, category: category, http: hc}
}

func (c *SABnzbdClient) ID() string                { return c.id }
func (c *SABnzbdClient) Protocol() provider.Protocol { return provider.ProtocolUsenet }

func (c *SABnzbdClient) apiURL(v url.Values) string {
	v.Set("apikey", c.apiKey)
	v.Set("output", "json")
	return c.base + "/api?" + v.Encode()
}

// get issues a GET against the SAB API and returns the decoded body.
func (c *SABnzbdClient) get(ctx context.Context, v url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(v), nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *SABnzbdClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrClientUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentBytes))
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}

func (c *SABnzbdClient) Test(ctx context.Context) error {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, url.Values{"mode": {"version"}}, &v); err != nil {
		return err
	}
	if v.Version == "" {
		return ErrInvalidResponse
	}
	return nil
}

func (c *SABnzbdClient) Add(ctx context.Context, d provider.DownloadRequest) (string, error) {
	category := d.Category
	if category == "" {
		category = c.category
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("nzbfile", d.Title+".nzb")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(d.Content); err != nil {
		return "", err
	}
	mw.Close()

	v := url.Values{"mode": {"addfile"}}
	if category != "" {
		v.Set("cat", category)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(v), &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var res struct {
		Status bool     `json:"status"`
		NzoIDs []string `json:"nzo_ids"`
	}
	if err := c.do(req, &res); err != nil {
		return "", err
	}
	if !res.Status || len(res.NzoIDs) == 0 {
		return "", fmt.Errorf("%w: addfile rejected", ErrInvalidResponse)
	}
	return res.NzoIDs[0], nil
}

func (c *SABnzbdClient) Remove(ctx context.Context, id string, deleteData bool) error {
	del := "0"
	if deleteData {
		del = "1"
	}
	// Try the queue first, then history; SAB returns ok for a missing id.
	for _, mode := range []string{"queue", "history"} {
		v := url.Values{"mode": {mode}, "name": {"delete"}, "value": {id}, "del_files": {del}}
		if err := c.get(ctx, v, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *SABnzbdClient) Items(ctx context.Context) ([]provider.DownloadItem, error) {
	var q struct {
		Queue struct {
			Slots []struct {
				NzoID      string `json:"nzo_id"`
				Filename   string `json:"filename"`
				Status     string `json:"status"`
				Percentage string `json:"percentage"`
				MB         string `json:"mb"`
				MBLeft     string `json:"mbleft"`
			} `json:"slots"`
		} `json:"queue"`
	}
	if err := c.get(ctx, url.Values{"mode": {"queue"}}, &q); err != nil {
		return nil, err
	}
	var h struct {
		History struct {
			Slots []struct {
				NzoID       string `json:"nzo_id"`
				Name        string `json:"name"`
				Status      string `json:"status"`
				Bytes       int64  `json:"bytes"`
				FailMessage string `json:"fail_message"`
			} `json:"slots"`
		} `json:"history"`
	}
	if err := c.get(ctx, url.Values{"mode": {"history"}}, &h); err != nil {
		return nil, err
	}

	items := make([]provider.DownloadItem, 0, len(q.Queue.Slots)+len(h.History.Slots))
	for _, s := range q.Queue.Slots {
		mb := parseFloat(s.MB)
		mbleft := parseFloat(s.MBLeft)
		size := int64(mb * 1024 * 1024)
		items = append(items, provider.DownloadItem{
			ID:               s.NzoID,
			Title:            s.Filename,
			Status:           sabQueueStatus(s.Status),
			Progress:         parseFloat(s.Percentage),
			Size:             size,
			Downloaded:       int64((mb - mbleft) * 1024 * 1024),
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolUsenet,
		})
	}
	for _, s := range h.History.Slots {
		status := sabHistoryStatus(s.Status)
		it := provider.DownloadItem{
			ID:               s.NzoID,
			Title:            s.Name,
			Status:           status,
			Size:             s.Bytes,
			Downloaded:       s.Bytes,
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolUsenet,
			ErrorMessage:     s.FailMessage,
		}
		if status == provider.StatusCompleted {
			it.Progress = 100
		}
		items = append(items, it)
	}
	return items, nil
}

func sabQueueStatus(s string) provider.DownloadStatus {
	switch strings.ToLower(s) {
	case "queued":
		return provider.StatusQueued
	case "paused":
		return provider.StatusPaused
	case "downloading", "fetching", "checking", "grabbing":
		return provider.StatusDownloading
	default:
		return provider.StatusDownloading
	}
}

func sabHistoryStatus(s string) provider.DownloadStatus {
	switch strings.ToLower(s) {
	case "completed":
		return provider.StatusCompleted
	case "failed":
		return provider.StatusFailed
	default:
		return provider.StatusDownloading // verifying/extracting/repairing
	}
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
