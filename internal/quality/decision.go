package quality

import (
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

// Resolve maps a parsed release's (Source, Resolution) to a built-in quality
// definition. WEBRip is treated as WEBDL; unknown source with a known
// resolution falls back to the HDTV-tier of that resolution; anything
// unresolvable is Unknown (ID 0).
func Resolve(p parsing.ParsedRelease) QualityDefinition {
	src := p.Source
	if src == parsing.SourceWEBRip {
		src = parsing.SourceWEBDL
	}
	// exact source+resolution match
	for _, d := range definitions {
		if d.ID == 0 {
			continue
		}
		if d.Source == src && d.Resolution == p.Resolution {
			return d
		}
	}
	// unknown/other source but known resolution → HDTV-tier fallback
	if p.Resolution != parsing.ResUnknown {
		for _, d := range definitions {
			if d.Source == parsing.SourceHDTV && d.Resolution == p.Resolution {
				return d
			}
		}
	}
	return definitions[0] // Unknown
}

// Decision is the result of evaluating one release against a profile.
type Decision struct {
	Accepted        bool              `json:"accepted"`
	Quality         QualityDefinition `json:"quality"`
	Score           int               `json:"score"`
	RejectionReason string            `json:"rejectionReason,omitempty"`
}

// profileRank returns the 0-based position of a quality within the profile's
// allowed items (higher = more preferred) and whether it is allowed at all.
func profileRank(profile store.QualityProfile, qualityID int) (rank int, allowed bool) {
	for i, it := range profile.Items {
		if it.QualityID == qualityID {
			return i, it.Allowed
		}
	}
	return -1, false
}

// revisionBonus is a small additive score so a PROPER/REPACK outscores the
// same quality without one, but never enough to jump a quality tier.
func revisionBonus(rev parsing.Revision) int {
	if rev.Version > 1 {
		return 1
	}
	return 0
}

// Decide evaluates a parsed release against a profile. A release is accepted
// only if its resolved quality is present AND allowed in the profile. Score is
// driven by the quality's rank within the profile (×10 to leave room for the
// revision bonus), so a user's ordering — not the global ladder — governs.
func Decide(p parsing.ParsedRelease, profile store.QualityProfile) Decision {
	q := Resolve(p)
	rank, allowed := profileRank(profile, q.ID)
	if !allowed {
		return Decision{Accepted: false, Quality: q, RejectionReason: "quality not in profile"}
	}
	return Decision{Accepted: true, Quality: q, Score: rank*10 + revisionBonus(p.Revision)}
}

// Compare orders two releases for a profile: +1 if a is better, -1 if b is
// better, 0 if indistinguishable. Higher profile-ranked quality wins; ties
// break on revision version. Releases whose quality is absent from the profile
// rank below any present quality.
func Compare(a, b parsing.ParsedRelease, profile store.QualityProfile) int {
	ra, _ := profileRank(profile, Resolve(a).ID)
	rb, _ := profileRank(profile, Resolve(b).ID)
	switch {
	case ra != rb && ra > rb:
		return 1
	case ra != rb && ra < rb:
		return -1
	}
	switch {
	case a.Revision.Version > b.Revision.Version:
		return 1
	case a.Revision.Version < b.Revision.Version:
		return -1
	}
	return 0
}

// IsUpgrade reports whether importing quality newID over an existing file of
// quality existingID is a profile-sanctioned upgrade: upgrades must be enabled,
// newID must rank strictly above existingID in the profile's item order, and the
// existing quality must rank strictly below the profile's cutoff (once the cutoff
// is met, no further upgrades). Qualities absent from the profile rank below all
// present ones (profileRank returns -1).
func IsUpgrade(newID, existingID int, profile store.QualityProfile) bool {
	if !profile.UpgradeAllowed {
		return false
	}
	newRank, _ := profileRank(profile, newID)
	existingRank, _ := profileRank(profile, existingID)
	cutoffRank, _ := profileRank(profile, profile.CutoffQualityID)
	if existingRank >= cutoffRank {
		return false
	}
	return newRank > existingRank
}

// CutoffUnmet reports whether an existing file of quality existingID is eligible
// for an upgrade under the profile: upgrades enabled AND the existing quality
// ranks strictly below the profile cutoff. It is IsUpgrade's cutoff arm made
// available without a candidate, for use as a pre-search filter. Qualities absent
// from the profile rank below all present ones (profileRank returns -1).
func CutoffUnmet(existingID int, profile store.QualityProfile) bool {
	if !profile.UpgradeAllowed {
		return false
	}
	existingRank, _ := profileRank(profile, existingID)
	cutoffRank, _ := profileRank(profile, profile.CutoffQualityID)
	return existingRank < cutoffRank
}
