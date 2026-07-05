package store

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newQualityTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestQualityProfileCRUD(t *testing.T) {
	st := newQualityTestStore(t)
	ctx := context.Background()

	p := QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  true,
		Items:           []QualityProfileItem{{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true}},
	}
	created, err := st.CreateQualityProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("bad created: %+v", created)
	}
	got, err := st.GetQualityProfile(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "HD" || len(got.Items) != 2 || got.Items[1].QualityID != 9 || !got.UpgradeAllowed {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	got.Name = "HD-updated"
	got.Items = append(got.Items, QualityProfileItem{QualityID: 12, Allowed: false})
	if err := st.UpdateQualityProfile(ctx, got); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := st.GetQualityProfile(ctx, created.ID)
	if reloaded.Name != "HD-updated" || len(reloaded.Items) != 3 {
		t.Fatalf("update not persisted: %+v", reloaded)
	}

	list, err := st.ListQualityProfiles(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v (err %v)", list, err)
	}

	if err := st.DeleteQualityProfile(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQualityProfile(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteQualityProfileInUse(t *testing.T) {
	st := newQualityTestStore(t)
	ctx := context.Background()
	created, err := st.CreateQualityProfile(ctx, QualityProfile{Name: "P", CutoffQualityID: 9, Items: []QualityProfileItem{{QualityID: 9, Allowed: true}}})
	if err != nil {
		t.Fatal(err)
	}
	// Reference it from a series (root folder nullable, quality profile set).
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, 1, &created.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteQualityProfile(ctx, created.ID); !errors.Is(err, ErrProfileInUse) {
		t.Fatalf("expected ErrProfileInUse, got %v", err)
	}
}
