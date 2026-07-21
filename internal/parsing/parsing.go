// Package parsing turns a release title into structured fields (quality,
// identity, release group, proper/repack, language). Pure — no I/O.
package parsing

// Source is the release medium/source, ranked loosely low→high by convention.
type Source int

const (
	SourceUnknown Source = iota
	SourceCAM
	SourceTS
	SourceDVD
	SourceHDTV
	SourceWEBRip
	SourceWEBDL
	SourceBluray
)

// Resolution is the vertical pixel resolution bucket.
type Resolution int

const (
	ResUnknown Resolution = iota
	Res480p
	Res720p
	Res1080p
	Res2160p
)

// Revision captures proper/repack. Version starts at 1; a PROPER or REPACK
// marker bumps it to 2. IsRepack is true only for REPACK.
type Revision struct {
	Version  int
	IsRepack bool
}

// ParsedRelease is the structured result of Parse. Identity fields use zero
// values when absent: Season and Year are 0, Episodes/Languages are nil,
// Edition/ReleaseGroup are "".
type ParsedRelease struct {
	Title        string     `json:"title"`
	Year         int        `json:"year"`
	Season       int        `json:"season"`
	Episodes     []int      `json:"episodes"`
	EpisodeTitle string     `json:"episodeTitle"`
	Edition      string     `json:"edition"`
	Source       Source     `json:"source"`
	Resolution   Resolution `json:"resolution"`
	Codec        string     `json:"codec"`
	ReleaseGroup string     `json:"releaseGroup"`
	Revision     Revision   `json:"revision"`
	Languages    []string   `json:"languages"`
}
