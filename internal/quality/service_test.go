package quality

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newQualityService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	return NewService(st), st
}

func validProfile() store.QualityProfile {
	return store.QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  true,
		Items:           []store.QualityProfileItem{{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true}},
	}
}

func TestServiceCreateValidatesName(t *testing.T) {
	svc, _ := newQualityService(t)
	p := validProfile()
	p.Name = ""
	if _, err := svc.CreateProfile(context.Background(), p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("empty name should be invalid, got %v", err)
	}
}

func TestServiceCreateValidatesCutoffInAllowedSet(t *testing.T) {
	svc, _ := newQualityService(t)
	p := validProfile()
	p.CutoffQualityID = 12 // not in items
	if _, err := svc.CreateProfile(context.Background(), p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("cutoff outside allowed set should be invalid, got %v", err)
	}
}

func TestServiceCreateValidatesRealDefinitions(t *testing.T) {
	svc, _ := newQualityService(t)
	p := validProfile()
	p.Items = append(p.Items, store.QualityProfileItem{QualityID: 999, Allowed: true})
	if _, err := svc.CreateProfile(context.Background(), p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("unknown quality id should be invalid, got %v", err)
	}
}

func TestServiceCreateAndGet(t *testing.T) {
	svc, _ := newQualityService(t)
	created, err := svc.CreateProfile(context.Background(), validProfile())
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetProfile(context.Background(), created.ID)
	if err != nil || got.Name != "HD" {
		t.Fatalf("get mismatch: %+v err=%v", got, err)
	}
}
