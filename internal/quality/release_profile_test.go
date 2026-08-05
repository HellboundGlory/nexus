package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

func TestRequiredAny(t *testing.T) {
	p := store.ReleaseProfile{RequiredMode: "any", RequiredAny: []string{"1080p", "bluray"}}
	// Candidate matching exactly one of two terms - the only shape that
	// discriminates any from all.
	if m := MatchReleaseProfile("Show.S01E01.1080p.WEB-DL", p); !m.Accepted {
		t.Fatalf("expected accepted, got %+v", m)
	}
	if m := MatchReleaseProfile("Show.S01E01.720p.WEB-DL", p); m.Accepted {
		t.Fatalf("expected rejected (no required term), got %+v", m)
	}
}

func TestRequiredAll(t *testing.T) {
	p := store.ReleaseProfile{RequiredMode: "all", RequiredAll: []string{"Indigo", "1080p"}}
	// Candidate matching exactly one of two terms must be rejected under "all".
	if m := MatchReleaseProfile("Pokemon.Indigo.League.S01E01.720p", p); m.Accepted {
		t.Fatalf("expected rejected (only one of two required), got %+v", m)
	}
	if m := MatchReleaseProfile("Pokemon.Indigo.League.S01E01.1080p", p); !m.Accepted {
		t.Fatalf("expected accepted (both required), got %+v", m)
	}
}

// A bad required_mode value fails to the permissive "any" default.
func TestRequiredModeDefaultsToAny(t *testing.T) {
	p := store.ReleaseProfile{RequiredMode: "bogus", RequiredAny: []string{"1080p", "bluray"}}
	if m := MatchReleaseProfile("Show.S01E01.1080p", p); !m.Accepted {
		t.Fatalf("bogus mode must behave as any, got %+v", m)
	}
}

func TestIgnored(t *testing.T) {
	p := store.ReleaseProfile{Ignored: []string{"dub"}}
	if m := MatchReleaseProfile("Show.S01E01.HebDub.1080p", p); m.Accepted {
		t.Fatalf("expected rejected (ignored term), got %+v", m)
	}
	if m := MatchReleaseProfile("Show.S01E01.1080p", p); !m.Accepted {
		t.Fatalf("expected accepted (no ignored term), got %+v", m)
	}
}

func TestPreferredScores(t *testing.T) {
	p := store.ReleaseProfile{Preferred: []string{"bluray", "remux"}}
	low := MatchReleaseProfile("Show.S01E01.1080p.WEB-DL", p)
	high := MatchReleaseProfile("Show.S01E01.1080p.BluRay.Remux", p)
	if !low.Accepted || !high.Accepted {
		t.Fatalf("preferred must not gate acceptance: low=%+v high=%+v", low, high)
	}
	if high.Score != 2 || low.Score != 0 {
		t.Fatalf("expected high.Score=2 low.Score=0, got %d vs %d", high.Score, low.Score)
	}
}

// Matching is case-insensitive substring on the RAW title, so tokens parsing
// strips (HebDub) remain targetable.
func TestCaseInsensitiveSubstringOnRawTitle(t *testing.T) {
	p := store.ReleaseProfile{Ignored: []string{"hebdub"}}
	if m := MatchReleaseProfile("Show.S01E01.HebDub.1080p", p); m.Accepted {
		t.Fatalf("expected rejected (case-insensitive ignored), got %+v", m)
	}
}