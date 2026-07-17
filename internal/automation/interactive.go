package automation

import (
	"fmt"
	"sort"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// ScoredCandidate is a release annotated with why automation would or would not
// have grabbed it. Unlike Candidate (which only ever holds releases that passed
// the accept gate), a ScoredCandidate is produced for EVERY release the indexers
// returned — the whole point of interactive search is to show the rejects.
//
// Rejections is the uniform reason model: an EMPTY Rejections means automation
// would have grabbed this release. That gives the UI one rule (any reasons →
// grey the row + confirm before grabbing) and mirrors Sonarr's rejections array.
type ScoredCandidate struct {
	Candidate
	Decision   quality.Decision
	Rejections []string
}

// Coverage reports why a release does not cover the search target, or "" if it
// does. It is the interactive-search mirror of the per-strategy filters in
// search.go (searchSeason's pack filter, searchEpisode's covering filter) — but
// it annotates instead of dropping. A nil Coverage means no coverage constraint,
// which is the movie case.
type Coverage func(p parsing.ParsedRelease) string

// SeasonPackCoverage accepts only a full pack for the given season: the same
// predicate searchSeason applies at search.go:245 (right season, no episode
// numbers == a pack rather than a single episode).
func SeasonPackCoverage(season int) Coverage {
	return func(p parsing.ParsedRelease) string {
		if p.Season == season && len(p.Episodes) == 0 {
			return ""
		}
		return fmt.Sprintf("not a season %d pack", season)
	}
}

// EpisodeCoverage accepts only releases containing the given episode: the same
// predicate searchEpisode applies at search.go:326.
func EpisodeCoverage(season, episode int) Coverage {
	return func(p parsing.ParsedRelease) string {
		if p.Season == season && containsInt(p.Episodes, episode) {
			return ""
		}
		return fmt.Sprintf("does not cover S%02dE%02d", season, episode)
	}
}

// DecideAll is Decide's interactive sibling: it parses and ranks every release
// exactly as Decide does, but ANNOTATES the ones automation would discard rather
// than dropping them. Decide itself is deliberately untouched so the automatic
// paths cannot regress.
//
// It reproduces all three of automation's filters as annotations — quality
// (Decide's gate), blocklist (enqueueBest's filterBlocklisted), and coverage
// (the search strategy's own filter) — because the guarantee this function sells
// is that row 1 is exactly what auto-search would have grabbed. Annotating only
// some of the three would float a release automation would never take to the top.
//
// blocked maps normalised release title -> blocklist reason (store.BlocklistedReasons);
// nil means nothing is blocked. cover may be nil (no coverage constraint).
func DecideAll(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, blocked map[string]string, cover Coverage) []ScoredCandidate {
	out := make([]ScoredCandidate, 0, len(releases))
	for _, r := range releases {
		p := parsing.Parse(r.Title, kind)
		decision := quality.Decide(p, profile)

		// Non-nil so the DTO serialises [] rather than null (design §5.5).
		rejections := []string{}
		if !decision.Accepted {
			reason := decision.RejectionReason
			if reason == "" {
				reason = "quality not in profile"
			}
			rejections = append(rejections, reason)
		}
		if reason, ok := blocked[store.NormReleaseTitle(r.Title)]; ok {
			rejections = append(rejections, "blocklisted: "+reason)
		}
		if cover != nil {
			if reason := cover(p); reason != "" {
				rejections = append(rejections, reason)
			}
		}

		out = append(out, ScoredCandidate{
			Candidate:  Candidate{Release: r, Parsed: p},
			Decision:   decision,
			Rejections: rejections,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := len(out[i].Rejections) == 0, len(out[j].Rejections) == 0
		// Accepted-first MUST be an explicit key. It is tempting to assume the
		// rejects sink for free because profileRank returns -1 for qualities
		// absent from the profile — but a quality PRESENT and not allowed returns
		// its REAL index (decision.go:47-54), so under [480p allowed, 1080p
		// not-allowed] quality.Compare ranks the rejected 1080p ABOVE the
		// accepted 480p. Without this key, row 1 would be a release automation
		// would never grab.
		if ci != cj {
			return ci
		}
		return compare(out[i].Candidate, out[j].Candidate, profile) > 0
	})
	return out
}
