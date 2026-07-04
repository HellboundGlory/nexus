package provider

import (
	"context"
	"testing"
)

// fakeDownloadClient proves the extended DownloadClient interface is satisfiable.
type fakeDownloadClient struct {
	id    string
	proto Protocol
	items []DownloadItem
}

func (f fakeDownloadClient) ID() string         { return f.id }
func (f fakeDownloadClient) Protocol() Protocol { return f.proto }
func (f fakeDownloadClient) Add(_ context.Context, d DownloadRequest) (string, error) {
	return d.Title, nil
}
func (f fakeDownloadClient) Items(context.Context) ([]DownloadItem, error) { return f.items, nil }
func (fakeDownloadClient) Remove(context.Context, string, bool) error      { return nil }
func (fakeDownloadClient) Test(context.Context) error                      { return nil }

func TestDownloadClientContract(t *testing.T) {
	var dc DownloadClient = fakeDownloadClient{
		id:    "1",
		proto: ProtocolTorrent,
		items: []DownloadItem{{
			ID: "h1", Title: "x", Status: StatusDownloading, Progress: 42.5,
			Size: 100, Downloaded: 42, DownloadClientID: "1", Protocol: ProtocolTorrent,
		}},
	}
	if dc.Protocol() != ProtocolTorrent {
		t.Fatalf("protocol = %q", dc.Protocol())
	}
	req := DownloadRequest{URL: "magnet:?xt=x", Title: "grab", Protocol: ProtocolTorrent, Content: nil}
	id, err := dc.Add(context.Background(), req)
	if err != nil || id != "grab" {
		t.Fatalf("add: id=%q err=%v", id, err)
	}
	items, _ := dc.Items(context.Background())
	if len(items) != 1 || items[0].Status != StatusDownloading || items[0].Progress != 42.5 {
		t.Fatalf("items = %+v", items)
	}
}

// Registry works with the extended interface too.
func TestDownloadClientRegistry(t *testing.T) {
	reg := NewRegistry[DownloadClient]()
	if err := reg.Register("a", fakeDownloadClient{id: "a", proto: ProtocolUsenet}); err != nil {
		t.Fatal(err)
	}
	got, ok := reg.Get("a")
	if !ok || got.ID() != "a" {
		t.Fatalf("get: ok=%v", ok)
	}
}
