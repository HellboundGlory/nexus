package automation

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func TestConfigDefaultsWhenAbsent(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	got, err := svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultConfig() {
		t.Fatalf("want defaults %+v, got %+v", DefaultConfig(), got)
	}
	if got.MissingSearchIntervalHours != 6 || got.MissingSearchBatchSize != 100 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	want := Config{MissingSearchIntervalHours: 12, MissingSearchBatchSize: 25}
	if err := svc.SetConfig(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want.RSSSyncEnabled = false
	want.RSSSyncIntervalMinutes = 15
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestConfigRSSDefaults(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	got, err := svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.RSSSyncEnabled {
		t.Fatalf("RSS should default enabled")
	}
	if got.RSSSyncIntervalMinutes != 15 {
		t.Fatalf("RSS interval default = %d, want 15", got.RSSSyncIntervalMinutes)
	}
}

func TestConfigRSSIntervalClamps(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	// Persist a zero interval and disabled=false; interval must clamp, disabled respected.
	if err := svc.SetConfig(ctx, Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: false, RSSSyncIntervalMinutes: 0,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.RSSSyncIntervalMinutes != 15 {
		t.Fatalf("non-positive interval should clamp to 15, got %d", got.RSSSyncIntervalMinutes)
	}
	if got.RSSSyncEnabled {
		t.Fatalf("explicit disabled=false must be respected")
	}
}

func TestConfigRSSRoundTrip(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	want := Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: true, RSSSyncIntervalMinutes: 30,
	}
	if err := svc.SetConfig(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}
