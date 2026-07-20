package automation

import (
	"context"
	"encoding/json"
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
	want.UpgradeSearchIntervalHours = 12
	want.UpgradeSearchBatchSize = 100
	want.UpgradeGrabCooldownHours = 168
	// UpgradeSearchEnabled stays false (persisted zero value, bool not clamped)
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
		UpgradeSearchIntervalHours: 12, UpgradeSearchBatchSize: 100, UpgradeGrabCooldownHours: 168,
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

func TestConfigUpgradeDefaults(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	got, err := svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpgradeSearchEnabled {
		t.Fatal("upgrade search should default enabled")
	}
	if got.UpgradeSearchIntervalHours != 12 || got.UpgradeSearchBatchSize != 100 || got.UpgradeGrabCooldownHours != 168 {
		t.Fatalf("unexpected upgrade defaults: %+v", got)
	}
}

func TestDefaultConfigLimitsOneDownloadPerSeries(t *testing.T) {
	if got := DefaultConfig().MaxConcurrentPerSeries; got != 1 {
		t.Fatalf("want default 1, got %d", got)
	}
}

// 0 is the documented off switch for the per-series gate. Every OTHER numeric
// field in Config() is clamped to its default when non-positive; this one must
// not be, or the off switch silently becomes a limit of 1.
func TestConfigPreservesZeroMaxConcurrentPerSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)

	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	// Also zero out a sibling that IS supposed to be clamped. If we left it at
	// its already-default value (100), reading it back as 100 would prove
	// nothing - it was never touched. Persisting it as 0 forces Config() to
	// either clamp it back to 100 or leave it at 0; only the former passes.
	c.MissingSearchBatchSize = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxConcurrentPerSeries != 0 {
		t.Fatalf("0 must survive as 'unlimited', got %d", got.MaxConcurrentPerSeries)
	}
	// Sanity: a sibling field IS still clamped, proving the exemption is specific.
	if got.MissingSearchBatchSize != DefaultConfig().MissingSearchBatchSize {
		t.Fatalf("sibling clamp should be unchanged, got %d", got.MissingSearchBatchSize)
	}
}

// TestConfigMaxConcurrentPerSeriesJSONKey pins the wire shape of
// MaxConcurrentPerSeries: it asserts on the raw bytes stored in the settings
// table, not just the in-process round trip through SetConfig/Config. A
// round-trip-only assertion would pass even if the `json:"..."` struct tag
// were misspelled, because SetConfig and Config marshal/unmarshal through the
// same struct and a self-consistent typo survives that path undetected. The
// persisted key is a contract with the frontend settings form, so the actual
// JSON key name must be checked directly.
func TestConfigMaxConcurrentPerSeriesJSONKey(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)

	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 3
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	raw, ok, err := st.GetSetting(ctx, configSettingKey)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected persisted setting, found none")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("persisted setting is not valid JSON: %v (raw=%s)", err, raw)
	}

	msg, ok := fields["maxConcurrentPerSeries"]
	if !ok {
		t.Fatalf(`persisted JSON missing key "maxConcurrentPerSeries", got keys %v (raw=%s)`, keysOf(fields), raw)
	}
	var got int
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("maxConcurrentPerSeries value is not an int: %v", err)
	}
	if got != 3 {
		t.Fatalf(`persisted "maxConcurrentPerSeries" = %d, want 3`, got)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestConfigUpgradeClamps(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	if err := svc.SetConfig(ctx, Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: true, RSSSyncIntervalMinutes: 15,
		UpgradeSearchEnabled: false, UpgradeSearchIntervalHours: 0,
		UpgradeSearchBatchSize: 0, UpgradeGrabCooldownHours: 0,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.UpgradeSearchIntervalHours != 12 || got.UpgradeSearchBatchSize != 100 || got.UpgradeGrabCooldownHours != 168 {
		t.Fatalf("non-positive upgrade ints should clamp to defaults, got %+v", got)
	}
	if got.UpgradeSearchEnabled {
		t.Fatal("explicit UpgradeSearchEnabled=false must be respected")
	}
}
