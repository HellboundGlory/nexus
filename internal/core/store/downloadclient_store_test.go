package store

import (
	"context"
	"testing"
)

func TestDownloadClientCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateDownloadClient(ctx, DownloadClient{
		Name: "sab", Implementation: "sabnzbd", Protocol: "usenet",
		Host: "localhost", Port: 8080, UseSSL: false, URLBase: "",
		APIKey: "k", Category: "tv", Enabled: true, Priority: 25,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetDownloadClient(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "sab" || got.Implementation != "sabnzbd" || got.Protocol != "usenet" ||
		got.Port != 8080 || got.Category != "tv" || got.Priority != 25 {
		t.Fatalf("unexpected client: %+v", got)
	}

	got.Enabled = false
	got.Name = "renamed"
	if err := s.UpdateDownloadClient(ctx, *got); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListDownloadClients(ctx, false)
	if err != nil || len(all) != 1 || all[0].Name != "renamed" {
		t.Fatalf("list all: %+v err=%v", all, err)
	}
	enabled, err := s.ListDownloadClients(ctx, true)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("list enabled: %+v err=%v", enabled, err)
	}

	if err := s.SetDownloadClientStatus(ctx, id, "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDownloadClient(ctx, id)
	if got.Status != "failed" || got.FailMessage != "boom" {
		t.Fatalf("status not persisted: %+v", got)
	}

	if err := s.DeleteDownloadClient(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDownloadClient(ctx, id); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
