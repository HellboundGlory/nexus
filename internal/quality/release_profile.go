package quality

import (
	"strings"

	"github.com/hellboundg/nexus/internal/core/store"
)

// ReleaseProfileMatch is the result of evaluating one release against one
// release profile.
type ReleaseProfileMatch struct {
	Accepted bool
	Score    int
	Reason   string // rejection reason, when not accepted
}

// MatchReleaseProfile evaluates a raw release title against a release profile.
// Matching is case-insensitive substring on the RAW title, not the parsed
// title, so tokens parsing strips (HebDub, -BurCyg) remain targetable.
//
// Required terms use required_mode: "any" (default) accepts when any term
// matches; "all" requires every term. Any value other than "all" is treated as
// "any", so a bad value fails to the permissive default rather than silently
// rejecting everything. Ignored terms reject when any matches. Preferred terms
// do not gate acceptance; each match adds one to the score.
func MatchReleaseProfile(rawTitle string, p store.ReleaseProfile) ReleaseProfileMatch {
	lower := strings.ToLower(rawTitle)
	contains := func(term string) bool { return strings.Contains(lower, strings.ToLower(term)) }

	for _, term := range p.Ignored {
		if term != "" && contains(term) {
			return ReleaseProfileMatch{Accepted: false, Reason: "ignored term: " + term}
		}
	}

	any := p.RequiredAny
	all := p.RequiredAll
	if p.RequiredMode == "all" {
		for _, term := range all {
			if term != "" && !contains(term) {
				return ReleaseProfileMatch{Accepted: false, Reason: "missing required term: " + term}
			}
		}
	} else {
		// "any" (default): at least one required-any term must match.
		matched := false
		for _, term := range any {
			if term != "" && contains(term) {
				matched = true
				break
			}
		}
		if len(any) > 0 && !matched {
			return ReleaseProfileMatch{Accepted: false, Reason: "no required term matched"}
		}
	}

	score := 0
	for _, term := range p.Preferred {
		if term != "" && contains(term) {
			score++
		}
	}
	return ReleaseProfileMatch{Accepted: true, Score: score}
}