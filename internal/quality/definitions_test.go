package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/parsing"
)

func TestResolveKnown(t *testing.T) {
	cases := []struct {
		title    string
		wantName string
	}{
		{"Show.S01E01.1080p.BluRay.x264-GRP", "Bluray-1080p"},
		{"Show.S01E01.720p.WEB-DL.x264-GRP", "WEBDL-720p"},
		{"Show.S01E01.2160p.HDTV.x265-GRP", "HDTV-2160p"},
		{"Show.S01E01.480p.SDTV-GRP", "SDTV"},
	}
	for _, c := range cases {
		p := parsing.Parse(c.title, provider.KindTV)
		got := Resolve(p)
		if got.Name != c.wantName {
			t.Errorf("Resolve(%q) = %q, want %q", c.title, got.Name, c.wantName)
		}
	}
}

func TestResolveUnknown(t *testing.T) {
	got := Resolve(parsing.Parse("random text with no quality", provider.KindMovie))
	if got.ID != 0 || got.Name != "Unknown" {
		t.Fatalf("expected Unknown, got id=%d name=%q", got.ID, got.Name)
	}
}

func TestDefinitionsRankedAndLookup(t *testing.T) {
	defs := Definitions()
	if len(defs) == 0 {
		t.Fatal("no definitions")
	}
	for i := 1; i < len(defs); i++ {
		if defs[i].Rank < defs[i-1].Rank {
			t.Fatalf("definitions not rank-ordered at %d", i)
		}
	}
	d, ok := DefinitionByID(defs[len(defs)-1].ID)
	if !ok || d.Name != defs[len(defs)-1].Name {
		t.Fatal("DefinitionByID mismatch")
	}
}
