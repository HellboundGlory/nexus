package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/downloadclient"
)

func TestRunStartsAndShutsDown(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9599")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()

	// Poll until the health endpoint responds.
	deadline := time.Now().Add(5 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:9599/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		t.Fatal("server never became healthy")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not shut down after cancel")
	}
}

func TestRunMountsIndexerRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9598")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9598/api/v1/indexer", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/indexer status = %d want 200", status)
	}
}

func TestRunMountsDownloadClientRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9599")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9599/api/v1/downloadclient", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/downloadclient status = %d want 200", status)
	}
}

func TestRunMountsMediaRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9597")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9597/api/v1/series", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/series status = %d want 200", status)
	}
}

func TestRunMountsQualityRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9598")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9598/api/v1/quality/definitions", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/quality/definitions status = %d want 200", status)
	}
}

func TestRunRegistersHousekeepingTask(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9596")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9596/api/v1/system/tasks", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var body struct {
			Scheduled []struct {
				Name string `json:"name"`
			} `json:"scheduled"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		for _, s := range body.Scheduled {
			if s.Name == "Housekeeping" {
				found = true
			}
		}
		if !found {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("Housekeeping scheduled task not registered")
	}
}

func TestToQueueSnapshotMapsClientErrorFieldsAndItems(t *testing.T) {
	items := []provider.DownloadItem{
		{ID: "item-1", Title: "Some.Release.1080p", DownloadClientID: "sab"},
	}
	res := downloadclient.QueueResult{
		Items: items,
		ClientErrors: []downloadclient.ClientError{
			{ClientID: "sab", Message: "connection refused"},
			{ClientID: "qbit", Message: "timeout"},
		},
	}

	snap := toQueueSnapshot(res)

	if len(snap.ClientErrors) != len(res.ClientErrors) {
		t.Fatalf("ClientErrors count = %d, want %d", len(snap.ClientErrors), len(res.ClientErrors))
	}
	if snap.ClientErrors[0].ClientID != "sab" {
		t.Errorf("ClientErrors[0].ClientID = %q, want %q", snap.ClientErrors[0].ClientID, "sab")
	}
	if snap.ClientErrors[0].Message != "connection refused" {
		t.Errorf("ClientErrors[0].Message = %q, want %q", snap.ClientErrors[0].Message, "connection refused")
	}
	if snap.ClientErrors[1].ClientID != "qbit" {
		t.Errorf("ClientErrors[1].ClientID = %q, want %q", snap.ClientErrors[1].ClientID, "qbit")
	}
	if snap.ClientErrors[1].Message != "timeout" {
		t.Errorf("ClientErrors[1].Message = %q, want %q", snap.ClientErrors[1].Message, "timeout")
	}

	if len(snap.Items) != len(items) {
		t.Fatalf("Items count = %d, want %d", len(snap.Items), len(items))
	}
	if snap.Items[0].ID != items[0].ID || snap.Items[0].DownloadClientID != items[0].DownloadClientID {
		t.Errorf("Items[0] = %+v, want %+v", snap.Items[0], items[0])
	}
}

func TestToQueueSnapshotHealthyClientProducesNoClientErrors(t *testing.T) {
	res := downloadclient.QueueResult{
		Items:        []provider.DownloadItem{{ID: "item-1"}},
		ClientErrors: nil,
	}

	snap := toQueueSnapshot(res)

	if len(snap.ClientErrors) != 0 {
		t.Fatalf("ClientErrors = %v, want empty", snap.ClientErrors)
	}
	if len(snap.Items) != 1 {
		t.Fatalf("Items count = %d, want 1", len(snap.Items))
	}
}

func TestRunMountsQueueRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9597")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9597/api/v1/queue", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/queue status = %d want 200", status)
	}
}
