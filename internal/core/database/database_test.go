package database

import (
	"path/filepath"
	"testing"
)

func TestOpenAndMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (idempotent): %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM settings`).Scan(&n); err != nil {
		t.Fatalf("settings table missing: %v", err)
	}
	var applied int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 8 {
		t.Fatalf("expected 8 applied migrations, got %d", applied)
	}
}
