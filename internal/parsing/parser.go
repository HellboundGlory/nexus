package parsing

import (
	"regexp"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
)

var (
	reResolution = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p)\b`)
	reCodec      = regexp.MustCompile(`(?i)\b(x264|h\.?264|avc|x265|h\.?265|hevc|xvid|divx)\b`)
	reProper     = regexp.MustCompile(`(?i)\bproper\b`)
	reRepack     = regexp.MustCompile(`(?i)\brepack\b`)
	// Source patterns, checked in priority order (first match wins).
	sourcePatterns = []struct {
		re  *regexp.Regexp
		src Source
	}{
		{regexp.MustCompile(`(?i)\b(bluray|blu-ray|bdrip|brrip|bd25|bd50)\b`), SourceBluray},
		{regexp.MustCompile(`(?i)\b(web-?dl|webdl)\b`), SourceWEBDL},
		{regexp.MustCompile(`(?i)\bweb-?rip\b`), SourceWEBRip},
		{regexp.MustCompile(`(?i)\bhdtv\b`), SourceHDTV},
		{regexp.MustCompile(`(?i)\b(dvdrip|dvd-?r|dvd)\b`), SourceDVD},
		{regexp.MustCompile(`(?i)\b(hdts|telesync|ts)\b`), SourceTS},
		{regexp.MustCompile(`(?i)\b(hdcam|cam)\b`), SourceCAM},
	}
)

// Parse extracts structured fields from a release title. It never errors: an
// unrecognizable title yields a best-effort ParsedRelease with Unknown
// source/resolution. kind selects TV vs movie identity parsing (Task 2).
func Parse(title string, kind provider.MediaKind) ParsedRelease {
	p := ParsedRelease{Season: 0, Revision: Revision{Version: 1}}

	if m := reResolution.FindString(title); m != "" {
		switch strings.ToLower(m) {
		case "2160p":
			p.Resolution = Res2160p
		case "1080p":
			p.Resolution = Res1080p
		case "720p":
			p.Resolution = Res720p
		case "480p":
			p.Resolution = Res480p
		}
	}
	for _, sp := range sourcePatterns {
		if sp.re.MatchString(title) {
			p.Source = sp.src
			break
		}
	}
	if m := reCodec.FindString(title); m != "" {
		p.Codec = normalizeCodec(m)
	}
	if reRepack.MatchString(title) {
		p.Revision = Revision{Version: 2, IsRepack: true}
	} else if reProper.MatchString(title) {
		p.Revision = Revision{Version: 2, IsRepack: false}
	}

	p.Title = cleanTitle(title)
	_ = kind // identity parsing added in Task 2
	return p
}

func normalizeCodec(m string) string {
	s := strings.ToLower(strings.ReplaceAll(m, ".", ""))
	switch s {
	case "h264", "avc":
		return "h264"
	case "h265", "hevc":
		return "x265"
	case "divx":
		return "xvid"
	}
	return s
}

// cleanTitle returns the title text up to the first quality/identity marker,
// with separators normalized to spaces. Task 2 extends the marker set.
func cleanTitle(title string) string {
	cut := len(title)
	if loc := reResolution.FindStringIndex(title); loc != nil && loc[0] < cut {
		cut = loc[0]
	}
	for _, sp := range sourcePatterns {
		if loc := sp.re.FindStringIndex(title); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	name := title[:cut]
	name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.TrimSpace(name)
}
