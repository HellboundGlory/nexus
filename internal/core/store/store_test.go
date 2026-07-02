package store

import (
	"context"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newTestStore(t *testing.T) *Store {
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

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, _ := s.GetSetting(ctx, "x"); ok {
		t.Fatal("expected missing key")
	}
	if err := s.SetSetting(ctx, "x", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "x", "2"); err != nil { // upsert
		t.Fatal(err)
	}
	v, ok, err := s.GetSetting(ctx, "x")
	if err != nil || !ok || v != "2" {
		t.Fatalf("got %q ok=%v err=%v", v, ok, err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if n, _ := s.CountUsers(ctx); n != 0 {
		t.Fatalf("expected 0 users, got %d", n)
	}
	id, err := s.CreateUser(ctx, "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUserByUsername(ctx, "admin")
	if err != nil || u.ID != id || u.PasswordHash != "hash" {
		t.Fatalf("bad user: %+v err=%v", u, err)
	}
	tok := "tok123"
	if err := s.CreateSession(ctx, tok, id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSession(ctx, tok)
	if err != nil || sess.UserID != id {
		t.Fatalf("bad session: %+v err=%v", sess, err)
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, tok); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTasksUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := Task{ID: "a", Name: "Test", Status: "queued"}
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	task.Status = "completed"
	task.Progress = 100
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, "a")
	if err != nil || got.Status != "completed" || got.Progress != 100 {
		t.Fatalf("bad task: %+v err=%v", got, err)
	}
}
