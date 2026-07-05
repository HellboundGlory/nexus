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
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}
