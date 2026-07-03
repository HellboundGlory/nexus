package downloadclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxContentBytes = 32 << 20 // 32 MiB cap on .nzb/.torrent downloads

// fetchContent retrieves the release payload for a grab. Magnet links carry no
// downloadable body, so they pass through as (nil, nil) and the client submits the
// URL directly. Everything else is fetched server-side so the indexer apikey
// embedded in the URL never reaches the download client.
func fetchContent(ctx context.Context, hc *http.Client, rawURL string) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(rawURL), "magnet:") {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("%w: status %d", ErrReleaseUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: fetch status %d", ErrClientUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentBytes))
	if err != nil {
		return nil, err
	}
	return body, nil
}
