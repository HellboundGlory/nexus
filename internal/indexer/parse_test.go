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

func TestParseCapturesIDAttrs(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
  <channel>
    <item>
      <title>The.Film.2020.1080p.BluRay.x264-GRP</title>
      <guid>https://idx.test/details/xyz</guid>
      <link>https://idx.test/getnzb/xyz.nzb</link>
      <enclosure url="https://idx.test/getnzb/xyz.nzb" length="100" type="application/x-nzb"/>
      <newznab:attr name="category" value="2040"/>
      <newznab:attr name="tmdbid" value="603"/>
      <newznab:attr name="imdbid" value="tt0133093"/>
      <newznab:attr name="tvdbid" value="78901"/>
    </item>
  </channel>
</rss>`
	rels, err := parseReleases([]byte(feed), "1", provider.ProtocolUsenet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("want 1 release, got %d", len(rels))
	}
	r := rels[0]
	if r.TMDBID != 603 {
		t.Errorf("tmdbid = %d, want 603", r.TMDBID)
	}
	if r.IMDbID != "0133093" { // "tt" stripped
		t.Errorf("imdbid = %q, want %q", r.IMDbID, "0133093")
	}
	if r.TVDBID != 78901 {
		t.Errorf("tvdbid = %d, want 78901", r.TVDBID)
	}
}

func TestParseMissingIDAttrsAreZero(t *testing.T) {
	data, _ := os.ReadFile("testdata/newznab_search.xml") // no id attrs
	rels, _ := parseReleases(data, "1", provider.ProtocolUsenet)
	if len(rels) != 1 {
		t.Fatalf("want 1 release, got %d", len(rels))
	}
	r := rels[0]
	if r.TMDBID != 0 || r.IMDbID != "" || r.TVDBID != 0 {
		t.Errorf("absent id attrs should be zero, got tmdb=%d imdb=%q tvdb=%d", r.TMDBID, r.IMDbID, r.TVDBID)
	}
}
