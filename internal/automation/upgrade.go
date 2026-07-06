package automation

import (
	"fmt"

	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/quality"
)

// UpgradeCompleted is emitted when an upgrade / cutoff-unmet sweep finishes.
type UpgradeCompleted struct {
	Grabbed int `json:"grabbed"`
}

func (UpgradeCompleted) Name() string { return "automation.upgrade.completed" }

func movieKey(id int64) string  { return fmt.Sprintf("movie:%d", id) }
func seriesKey(id int64) string { return fmt.Sprintf("series:%d", id) }

type cooldownKey struct {
	item  string
	title string
}

// cooldownSet is the set of (itemKey, normalized release title) pairs grabbed
// within the cooldown window. A candidate matching one must not be re-grabbed —
// this closes the mislabel re-grab loop (a release whose title claims a quality
// its file does not deliver would otherwise be grabbed every sweep).
type cooldownSet map[cooldownKey]struct{}

// buildCooldownSet keys grabbed-history events by their item and normalized
// source title. TV grabbed rows carry series_id (not episode_id), so TV keys are
// series-level; events with neither a movie nor a series id are ignored.
func buildCooldownSet(events []store.HistoryEvent) cooldownSet {
	cs := make(cooldownSet, len(events))
	for _, e := range events {
		var item string
		switch {
		case e.MovieID != nil:
			item = movieKey(*e.MovieID)
		case e.SeriesID != nil:
			item = seriesKey(*e.SeriesID)
		default:
			continue
		}
		cs[cooldownKey{item: item, title: normTitle(e.SourceTitle)}] = struct{}{}
	}
	return cs
}

func (cs cooldownSet) has(itemKey, title string) bool {
	_, ok := cs[cooldownKey{item: itemKey, title: normTitle(title)}]
	return ok
}

// upgradeCandidates keeps only candidates that are a genuine upgrade over the
// existing file's quality AND were not grabbed for this item within the cooldown
// window. Input is assumed already ranked best-first by Decide; order is
// preserved.
func upgradeCandidates(cands []Candidate, existingQualityID int, profile store.QualityProfile, itemKey string, cs cooldownSet) []Candidate {
	var out []Candidate
	for _, c := range cands {
		newID := quality.Resolve(c.Parsed).ID
		if !quality.IsUpgrade(newID, existingQualityID, profile) {
			continue
		}
		if cs.has(itemKey, c.Release.Title) {
			continue
		}
		out = append(out, c)
	}
	return out
}
