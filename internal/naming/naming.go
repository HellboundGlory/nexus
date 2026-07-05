// Package naming renders library file/folder names from a small, editable token
// template set. Pure — no I/O, no module imports.
package naming

import (
	"fmt"
	"regexp"
	"strings"
)

// Tokens are the substitution values for a single render.
type Tokens struct {
	SeriesTitle  string
	EpisodeTitle string
	MovieTitle   string
	Quality      string
	ReleaseGroup string
	Season       int
	Episode      int
	Year         int
}

// Config holds the five editable templates.
type Config struct {
	SeriesFolder string `json:"seriesFolder"`
	SeasonFolder string `json:"seasonFolder"`
	EpisodeFile  string `json:"episodeFile"`
	MovieFolder  string `json:"movieFolder"`
	MovieFile    string `json:"movieFile"`
}

// DefaultConfig is the built-in template set.
func DefaultConfig() Config {
	return Config{
		SeriesFolder: "{Series Title}",
		SeasonFolder: "Season {season:00}",
		EpisodeFile:  "{Series Title} - S{season:00}E{episode:00} - {Episode Title} [{Quality}]",
		MovieFolder:  "{Movie Title} ({year})",
		MovieFile:    "{Movie Title} ({year}) [{Quality}]",
	}
}

// Render substitutes tokens in template. Unknown tokens are left as-is. The
// result is NOT sanitized — callers sanitize each path segment via Sanitize.
func Render(template string, t Tokens) string {
	r := strings.NewReplacer(
		"{Series Title}", t.SeriesTitle,
		"{Episode Title}", t.EpisodeTitle,
		"{Movie Title}", t.MovieTitle,
		"{Quality}", t.Quality,
		"{Release Group}", t.ReleaseGroup,
		"{season:00}", fmt.Sprintf("%02d", t.Season),
		"{episode:00}", fmt.Sprintf("%02d", t.Episode),
		"{season}", fmt.Sprintf("%d", t.Season),
		"{episode}", fmt.Sprintf("%d", t.Episode),
		"{year}", fmt.Sprintf("%d", t.Year),
	)
	return r.Replace(template)
}

var illegal = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// Sanitize strips characters illegal in file names on common filesystems,
// collapses the resulting whitespace, and trims trailing dots/spaces.
func Sanitize(name string) string {
	s := illegal.ReplaceAllString(name, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, " .")
}
