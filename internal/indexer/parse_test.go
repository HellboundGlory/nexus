package indexer

import (
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseNewznab(t *testing.T) {
	data, _ := os.ReadFile("testdata/newznab_search.xml")
	rels, err := parseReleases(data, "1", provider.ProtocolUsenet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("want 1 release, got %d", len(rels))
	}
	r := rels[0]
	if r.Title != "The.Show.S02E05.1080p.WEB-DL" {
		t.Errorf("title = %q", r.Title)
	}
	if r.Size != 1610612736 {
		t.Errorf("size = %d", r.Size)
	}
	if r.Protocol != provider.ProtocolUsenet {
		t.Errorf("protocol = %q", r.Protocol)
	}
	if len(r.Categories) != 2 || r.Categories[0] != 5000 {
		t.Errorf("categories = %v", r.Categories)
	}
	if r.DownloadURL == "" || r.IndexerID != "1" {
		t.Errorf("download/indexer = %q %q", r.DownloadURL, r.IndexerID)
	}
	if r.Seeders != nil {
		t.Errorf("usenet should have nil seeders")
	}
	if r.PublishDate.IsZero() {
		t.Errorf("pubdate not parsed")
	}
}

func TestParseTorznab(t *testing.T) {
	data, _ := os.ReadFile("testdata/torznab_search.xml")
	rels, err := parseReleases(data, "2", provider.ProtocolTorrent)
	if err != nil {
		t.Fatal(err)
	}
	r := rels[0]
	if r.Protocol != provider.ProtocolTorrent {
		t.Errorf("protocol = %q", r.Protocol)
	}
	if r.Seeders == nil || *r.Seeders != 42 {
		t.Errorf("seeders = %v", r.Seeders)
	}
	if r.Leechers == nil || *r.Leechers != 8 { // peers(50) - seeders(42)
		t.Errorf("leechers = %v (want 8)", r.Leechers)
	}
	if r.Size != 2147483648 {
		t.Errorf("size = %d", r.Size)
	}
}
