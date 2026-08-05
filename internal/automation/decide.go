package automation

import (
	"sort"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// Candidate is a release that passed the profile's accept gate, paired with its
// parsed attributes so callers can inspect season/episode/quality without
// re-parsing. ReleaseProfileScore is the sum of preferred-term matches across
// all applicable release profiles; it is a ranking tiebreaker, not a gate.
type Candidate struct {
	Release             provider.Release
	Parsed              parsing.ParsedRelease
	ReleaseProfileScore int
}

// Decide parses each release, drops any whose resolved quality is not allowed by
// the profile, and returns the accepted candidates ranked best-first. It performs
// no I/O and is the single ranking authority shared by every search strategy.
// rps are the release profiles applicable to the item; a release must pass every
// one of them (required/ignored checks) to be accepted.
func Decide(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, rps []store.ReleaseProfile) []Candidate {
	var out []Candidate
	for _, r := range releases {
		p := parsing.Parse(r.Title, kind)
		if !quality.Decide(p, profile).Accepted {
			continue
		}
		// Release profiles are orthogonal to quality: a release must pass every
		// applicable release profile's required/ignored checks. The score is the
		// sum of preferred-term matches across all applicable profiles.
		score := 0
		rejected := false
		for _, rp := range rps {
			m := quality.MatchReleaseProfile(r.Title, rp)
			if !m.Accepted {
				rejected = true
				break
			}
			score += m.Score
		}
		if rejected {
			continue
		}
		out = append(out, Candidate{Release: r, Parsed: p, ReleaseProfileScore: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compare(out[i], out[j], profile) > 0
	})
	return out
}

// compare orders two accepted candidates: +1 if a is better, -1 if b is better,
// 0 if indistinguishable. Chain (first non-zero wins): profile quality (rank +
// revision) → release-profile score (more) → torrent seeders (more) → usenet
// age (newer) → size (larger). Season-pack vs single-episode selection is
// handled by the search strategy (filtering), not here, so this comparer stays
// context-free.
func compare(a, b Candidate, profile store.QualityProfile) int {
	if c := quality.Compare(a.Parsed, b.Parsed, profile); c != 0 {
		return c
	}
	if a.ReleaseProfileScore != b.ReleaseProfileScore {
		if a.ReleaseProfileScore > b.ReleaseProfileScore {
			return 1
		}
		return -1
	}
	if a.Release.Protocol == provider.ProtocolTorrent && b.Release.Protocol == provider.ProtocolTorrent {
		as, bs := seeders(a.Release), seeders(b.Release)
		if as != bs {
			if as > bs {
				return 1
			}
			return -1
		}
	}
	if a.Release.Protocol == provider.ProtocolUsenet && b.Release.Protocol == provider.ProtocolUsenet {
		if !a.Release.PublishDate.Equal(b.Release.PublishDate) {
			if a.Release.PublishDate.After(b.Release.PublishDate) {
				return 1
			}
			return -1
		}
	}
	if a.Release.Size != b.Release.Size {
		if a.Release.Size > b.Release.Size {
			return 1
		}
		return -1
	}
	return 0
}

func seeders(r provider.Release) int {
	if r.Seeders != nil {
		return *r.Seeders
	}
	return 0
}
