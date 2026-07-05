package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

// profile allows WEBDL-720p(7) and Bluray-1080p(9), ranked in that order.
func hdProfile() store.QualityProfile {
	return store.QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  true,
		Items: []store.QualityProfileItem{
			{QualityID: 7, Allowed: true},
			{QualityID: 9, Allowed: true},
			{QualityID: 12, Allowed: false},
		},
	}
}

func TestDecideAcceptAndReject(t *testing.T) {
	prof := hdProfile()

	accept := Decide(parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV), prof)
	if !accept.Accepted || accept.Quality.Name != "Bluray-1080p" {
		t.Fatalf("expected accept Bluray-1080p, got %+v", accept)
	}

	reject := Decide(parsing.Parse("Show.S01E01.2160p.BluRay.x265-GRP", provider.KindTV), prof)
	if reject.Accepted || reject.RejectionReason == "" {
		t.Fatalf("expected reject for Bluray-2160p (not allowed), got %+v", reject)
	}

	unknown := Decide(parsing.Parse("random", provider.KindMovie), prof)
	if unknown.Accepted {
		t.Fatalf("expected reject for Unknown, got %+v", unknown)
	}
}

func TestDecideScoreReflectsProfileOrder(t *testing.T) {
	prof := hdProfile()
	web := Decide(parsing.Parse("Show.S01E01.720p.WEB-DL.x264-GRP", provider.KindTV), prof)
	blu := Decide(parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV), prof)
	if !(blu.Score > web.Score) {
		t.Fatalf("Bluray-1080p (%d) should outscore WEBDL-720p (%d) per profile order", blu.Score, web.Score)
	}
}

func TestCompare(t *testing.T) {
	prof := hdProfile()
	web := parsing.Parse("Show.S01E01.720p.WEB-DL.x264-GRP", provider.KindTV)
	blu := parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV)
	if Compare(blu, web, prof) != 1 {
		t.Fatal("Bluray should beat WEBDL")
	}
	if Compare(web, blu, prof) != -1 {
		t.Fatal("WEBDL should lose to Bluray")
	}

	// same quality, one is a REPACK → repack wins on revision tiebreak
	plain := parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV)
	repack := parsing.Parse("Show.S01E01.1080p.BluRay.REPACK.x264-GRP", provider.KindTV)
	if Compare(repack, plain, prof) != 1 {
		t.Fatal("REPACK should beat plain on revision tiebreak")
	}
}
