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
	"regexp"
	"strings"
	"sync"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// QBittorrentClient talks to a qBittorrent client over its WebUI API v2.
type QBittorrentClient struct {
	id       string
	base     string
	username string
	password string
	category string
	http     *http.Client

	mu  sync.Mutex
	sid string
}

func newQBittorrent(id, base, username, password, category string, hc *http.Client) *QBittorrentClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &QBittorrentClient{
		id: id, base: strings.TrimRight(base, "/"), username: username,
		password: password, category: category, http: hc,
	}
}

func (c *QBittorrentClient) ID() string                  { return c.id }
func (c *QBittorrentClient) Protocol() provider.Protocol { return provider.ProtocolTorrent }

// login authenticates and stores the SID cookie value.
func (c *QBittorrentClient) login(ctx context.Context) error {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Ok.") {
		return ErrAuthFailed
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "SID" {
			c.mu.Lock()
			c.sid = ck.Value
			c.mu.Unlock()
			return nil
		}
	}
	return ErrAuthFailed
}

func (c *QBittorrentClient) currentSID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sid
}

// send builds a request with the SID cookie, logging in first if needed, and
// retrying once on a 403 (expired session). Returns the response body bytes.
func (c *QBittorrentClient) send(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	if c.currentSID() == "" {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.AddCookie(&http.Cookie{Name: "SID", Value: c.currentSID()})
		return c.http.Do(req)
	}
	resp, err := do()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	if resp.StatusCode == http.StatusForbidden && body == nil {
		// Session likely expired; re-auth and retry once (only safe when body is
		// nil/replayable — callers with bodies pass non-nil and skip retry).
		resp.Body.Close()
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		if resp, err = do(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrClientUnavailable, err)
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrClientUnavailable, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxContentBytes))
}

func (c *QBittorrentClient) Test(ctx context.Context) error {
	// login is exercised inside send(); a successful version call confirms auth.
	body, err := c.send(ctx, http.MethodGet, "/api/v2/app/version", nil, "")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return ErrInvalidResponse
	}
	return nil
}

var btihRe = regexp.MustCompile(`(?i)btih:([0-9a-f]{40})`)

func (c *QBittorrentClient) Add(ctx context.Context, d provider.DownloadRequest) (string, error) {
	category := d.Category
	if category == "" {
		category = c.category
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if len(d.Content) > 0 {
		fw, err := mw.CreateFormFile("torrents", d.Title+".torrent")
		if err != nil {
			return "", err
		}
		if _, err := fw.Write(d.Content); err != nil {
			return "", err
		}
	} else {
		_ = mw.WriteField("urls", d.URL)
	}
	if category != "" {
		_ = mw.WriteField("category", category)
	}
	mw.Close()

	// Ensure auth before sending a body (send() does not retry body requests).
	if c.currentSID() == "" {
		if err := c.login(ctx); err != nil {
			return "", err
		}
	}
	if _, err := c.send(ctx, http.MethodPost, "/api/v2/torrents/add", &buf, mw.FormDataContentType()); err != nil {
		return "", err
	}
	// qBittorrent's add endpoint returns no id; the torrent hash is the identity.
	// For magnets, derive it from the btih; for .torrent files it is not known here
	// (the queue monitor will surface the item by name/hash).
	if m := btihRe.FindStringSubmatch(d.URL); m != nil {
		return strings.ToLower(m[1]), nil
	}
	return "", nil
}

func (c *QBittorrentClient) Remove(ctx context.Context, id string, deleteData bool) error {
	del := "false"
	if deleteData {
		del = "true"
	}
	form := url.Values{"hashes": {id}, "deleteFiles": {del}}
	// Pre-auth so the POST body is sent with a valid cookie.
	if c.currentSID() == "" {
		if err := c.login(ctx); err != nil {
			return err
		}
	}
	_, err := c.send(ctx, http.MethodPost, "/api/v2/torrents/delete",
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	return err
}

func (c *QBittorrentClient) Items(ctx context.Context) ([]provider.DownloadItem, error) {
	body, err := c.send(ctx, http.MethodGet, "/api/v2/torrents/info", nil, "")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Hash       string  `json:"hash"`
		Name       string  `json:"name"`
		Size       int64   `json:"size"`
		Progress   float64 `json:"progress"`
		State      string  `json:"state"`
		Completed  int64   `json:"completed"`
		AmountLeft int64   `json:"amount_left"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	items := make([]provider.DownloadItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, provider.DownloadItem{
			ID:               r.Hash,
			Title:            r.Name,
			Status:           qbitStatus(r.State),
			Progress:         r.Progress * 100,
			Size:             r.Size,
			Downloaded:       r.Completed,
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolTorrent,
		})
	}
	return items, nil
}

func qbitStatus(state string) provider.DownloadStatus {
	switch state {
	case "error", "missingFiles":
		return provider.StatusFailed
	case "pausedDL":
		return provider.StatusPaused
	case "queuedDL":
		return provider.StatusQueued
	case "uploading", "stalledUP", "forcedUP", "queuedUP", "checkingUP", "pausedUP":
		return provider.StatusCompleted
	default: // downloading, stalledDL, metaDL, forcedDL, checkingDL, allocating
		return provider.StatusDownloading
	}
}
