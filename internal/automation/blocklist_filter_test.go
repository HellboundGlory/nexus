package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

func TestFilterBlocklisted(t *testing.T) {
	cands := []Candidate{
		{Release: provider.Release{Title: "Dune.2021.1080p-GRP"}},
		{Release: provider.Release{Title: "Dune.2021.2160p-GRP"}},
	}
	blocked := map[string]bool{store.NormReleaseTitle("Dune.2021.1080p-GRP"): true}
	got := filterBlocklisted(cands, blocked)
	if len(got) != 1 || got[0].Release.Title != "Dune.2021.2160p-GRP" {
		t.Fatalf("expected only the 2160p release, got %+v", got)
	}
}
