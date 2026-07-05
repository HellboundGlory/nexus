package automation

import (
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// hdProfile allows WEBDL-1080p(7) and Bluray-1080p(9); Bluray outranks WEBDL.
func hdProfile() store.QualityProfile {
	return store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{
			{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true},
		},
	}
}

func seedersPtr(n int) *int { return &n }

func TestDecideDropsDisallowedQuality(t *testing.T) {
	rel := provider.Release{Title: "The.Show.S01E01.720p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet}
	got := Decide([]provider.Release{rel}, provider.KindTV, hdProfile())
	if len(got) != 0 {
		t.Fatalf("Bluray-720p is not in the HD profile; want 0 candidates, got %d", len(got))
	}
}

func TestDecideRanksHigherQualityFirst(t *testing.T) {
	web := provider.Release{Title: "The.Show.S01E01.1080p.WEB-DL.x264-GRP", Protocol: provider.ProtocolUsenet}
	blu := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet}
	got := Decide([]provider.Release{web, blu}, provider.KindTV, hdProfile())
	if len(got) != 2 {
		t.Fatalf("want 2 accepted, got %d", len(got))
	}
	if got[0].Release.Title != blu.Title {
		t.Fatalf("Bluray-1080p should rank first, got %q", got[0].Release.Title)
	}
}

func TestDecideTorrentSeedersTiebreak(t *testing.T) {
	low := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolTorrent, Seeders: seedersPtr(3)}
	high := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-OTHER", Protocol: provider.ProtocolTorrent, Seeders: seedersPtr(50)}
	got := Decide([]provider.Release{low, high}, provider.KindTV, hdProfile())
	if len(got) != 2 || got[0].Release.Seeders == nil || *got[0].Release.Seeders != 50 {
		t.Fatalf("more seeders should rank first, got %+v", got)
	}
}

func TestDecideUsenetAgeThenSizeTiebreak(t *testing.T) {
	older := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet, PublishDate: time.Unix(1000, 0), Size: 100}
	newer := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-NEW", Protocol: provider.ProtocolUsenet, PublishDate: time.Unix(2000, 0), Size: 100}
	got := Decide([]provider.Release{older, newer}, provider.KindTV, hdProfile())
	if len(got) != 2 || !got[0].Release.PublishDate.Equal(time.Unix(2000, 0)) {
		t.Fatalf("newer usenet should rank first, got %+v", got)
	}
}
