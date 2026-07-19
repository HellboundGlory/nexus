package importing

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// seedQueueRow inserts a grabbed queue row whose client item id is itemID.
func seedQueueRow(t *testing.T, st *store.Store, itemID, title string) store.QueueItem {
	t.Helper()
	row, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		DownloadClientID: "sab", ClientItemID: itemID, Protocol: "usenet",
		SourceTitle: title, MediaKind: "movie", Status: store.QueueGrabbed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func liveItem(itemID string) provider.DownloadItem {
	return provider.DownloadItem{ID: itemID, DownloadClientID: "sab", Status: provider.StatusDownloading}
}

func TestRemoveQueueItemRemovesFromClientByDefault(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true}); err != nil {
		t.Fatal(err)
	}
	if !q.removed["h1"] {
		t.Fatal("expected the download to be removed from the client")
	}
	if !q.removedDeleteData["h1"] {
		t.Fatal("Remove must be called with deleteData: true, so partial files go too")
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("queue row still present, err = %v", err)
	}
}

func TestRemoveQueueItemSkipsClientWhenNotRequested(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: false}); err != nil {
		t.Fatal(err)
	}
	if q.removed["h1"] {
		t.Fatal("client removal should not happen when RemoveFromClient is false")
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("row should still be deleted")
	}
}

// The escape hatch from spec §4.5: a failing client must not trap the user with
// an undeletable row, but opting out of client removal is how you force it.
func TestRemoveQueueItemKeepsRowWhenClientFails(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}, removeErr: errors.New("connection refused")}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); err != nil {
		t.Fatal("row must be KEPT when the client removal failed")
	}

	// Unchecking "remove from client" deletes it regardless.
	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("escape hatch failed: row should be gone")
	}
}

// No live match means the client already finished with it — not a failure.
func TestRemoveQueueItemWithNoLiveMatchDeletesRow(t *testing.T) {
	q := &fakeQueue{} // no live items
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true}); err != nil {
		t.Fatal(err)
	}
	if len(q.removed) != 0 {
		t.Fatal("no live match: Remove should not have been called")
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("row should be deleted")
	}
}

// No live match AND an unreachable client is the orphaning hole this closes:
// an unreachable client contributes zero items to the snapshot, so it is
// indistinguishable from "finished" unless ClientErrors is also checked.
// Mirrors ClearQueue's preflight refusal (§4.4) at the single-item level.
func TestRemoveQueueItemRefusesWhenClientIsUnreachable(t *testing.T) {
	q := &fakeQueue{clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); err != nil {
		t.Fatal("row must be KEPT when the client is unreachable — deleting it would orphan the download")
	}
}

// The escape hatch still works even when the reason for "no live match" is an
// unreachable client rather than a failed Remove call.
func TestRemoveQueueItemUnreachableClientEscapeHatchDeletesRow(t *testing.T) {
	q := &fakeQueue{clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("escape hatch failed: row should be gone")
	}
}

func TestRemoveQueueItemBlocklistsWhenRequested(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true, Blocklist: true}); err != nil {
		t.Fatal(err)
	}
	bl, err := st.ListBlocklist(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 || bl[0].SourceTitle != "Dune.2021-GRP" {
		t.Fatalf("blocklist = %+v, want one entry for the removed release", bl)
	}
}

func TestRemoveQueueItemUnknownIDIsNotFound(t *testing.T) {
	svc, _ := newSvc(t)
	err := svc.RemoveQueueItem(context.Background(), 4242, RemoveOptions{})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

// Spec §4.4: an unreachable client means we cannot see the whole picture, so a
// non-forced clear refuses and deletes NOTHING.
func TestClearQueueRefusesWhenAClientIsUnreachable(t *testing.T) {
	q := &fakeQueue{
		items:        []provider.DownloadItem{liveItem("h1")},
		clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}},
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	_, err := svc.ClearQueue(context.Background(), false)
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
	rows, _ := st.ListQueue(context.Background())
	if len(rows) != 2 {
		t.Fatalf("%d rows left, want both kept — a refused clear deletes nothing", len(rows))
	}
}

func TestClearQueueRemovesEveryRowAndItsDownload(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1"), liveItem("h2")}}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	res, err := svc.ClearQueue(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("Removed = %d, want 2", res.Removed)
	}
	if !q.removed["h1"] || !q.removed["h2"] {
		t.Fatalf("both downloads should be removed from the client, got %v", q.removed)
	}
	if !q.removedDeleteData["h1"] || !q.removedDeleteData["h2"] {
		t.Fatalf("Remove must be called with deleteData: true, so partial files go too, got %v", q.removedDeleteData)
	}
	if rows, _ := st.ListQueue(context.Background()); len(rows) != 0 {
		t.Fatalf("%d rows left, want 0", len(rows))
	}
}

// Force tolerates failure — it does NOT skip the work. A healthy client still
// gets every Remove call.
func TestClearQueueForceStillRemovesFromHealthyClient(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")

	res, err := svc.ClearQueue(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !q.removed["h1"] {
		t.Fatal("force must still attempt the client removal, not skip it")
	}
	if len(res.ClientErrors) != 0 {
		t.Fatalf("ClientErrors = %v, want none for a healthy client", res.ClientErrors)
	}
}

func TestClearQueueForceProceedsAndReportsClientErrors(t *testing.T) {
	q := &fakeQueue{
		items:        []provider.DownloadItem{liveItem("h1")},
		clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}},
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	res, err := svc.ClearQueue(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("Removed = %d, want 2 — force empties the queue", res.Removed)
	}
	if len(res.ClientErrors) == 0 {
		t.Fatal("a forced clear must REPORT the errors it tolerated, not hide them")
	}
	if rows, _ := st.ListQueue(context.Background()); len(rows) != 0 {
		t.Fatalf("%d rows left, want 0", len(rows))
	}
}

// The mid-sweep-drop case: preflight is clean but Remove itself fails. Force
// must continue rather than abort. This is distinct from the preflight case and
// regresses if force is implemented as preflight-skip only.
func TestClearQueueForceContinuesWhenRemoveErrors(t *testing.T) {
	q := &fakeQueue{
		items:     []provider.DownloadItem{liveItem("h1"), liveItem("h2")},
		removeErr: errors.New("connection refused"),
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	res, err := svc.ClearQueue(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("Removed = %d, want 2 — force continues past Remove failures", res.Removed)
	}
	if len(res.ClientErrors) != 2 {
		t.Fatalf("ClientErrors = %d, want one per failed removal", len(res.ClientErrors))
	}
}

// Without force, the same mid-sweep failure aborts — rows already removed stay
// removed, and the caller learns the client is unavailable.
func TestClearQueueWithoutForceAbortsOnRemoveError(t *testing.T) {
	q := &fakeQueue{
		items:     []provider.DownloadItem{liveItem("h1")},
		removeErr: errors.New("connection refused"),
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")

	_, err := svc.ClearQueue(context.Background(), false)
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
}
