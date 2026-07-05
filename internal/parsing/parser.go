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
	reSeasonEp   = regexp.MustCompile(`(?i)\bS(\d{1,2})((?:E\d{1,2})+)(?:-?E?(\d{1,2}))?\b`)
	reSeasonPack = regexp.MustCompile(`(?i)\bS(?:eason)?[ ._-]?(\d{1,2})\b`)
	reEpNums     = regexp.MustCompile(`(?i)E(\d{1,2})`)
	reYear       = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reGroup      = regexp.MustCompile(`-(\w+)$`)
	reEdition    = regexp.MustCompile(`(?i)\b(director'?s cut|extended|unrated|remastered|imax|theatrical)\b`)
	reLanguage   = regexp.MustCompile(`(?i)\b(multi|english|french|german|spanish|italian|japanese|korean|dutch)\b`)
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

	if kind == provider.KindTV {
		if m := reSeasonEp.FindStringSubmatch(title); m != nil {
			p.Season = atoi(m[1])
			for _, em := range reEpNums.FindAllStringSubmatch(m[2], -1) {
				p.Episodes = append(p.Episodes, atoi(em[1]))
			}
			if m[3] != "" { // range end, e.g. E10-E12
				end := atoi(m[3])
				for e := p.Episodes[len(p.Episodes)-1] + 1; e <= end; e++ {
					p.Episodes = append(p.Episodes, e)
				}
			}
		} else if m := reSeasonPack.FindStringSubmatch(title); m != nil {
			// Season pack: a season is named but no episode → whole-season release.
			p.Season = atoi(m[1])
		}
	} else {
		if m := reYear.FindString(title); m != "" {
			p.Year = atoi(m)
		}
		if m := reEdition.FindString(title); m != "" {
			p.Edition = canonicalEdition(m)
		}
	}
	if m := reGroup.FindStringSubmatch(title); m != nil {
		p.ReleaseGroup = m[1]
	}
	for _, lm := range reLanguage.FindAllStringSubmatch(title, -1) {
		p.Languages = append(p.Languages, strings.ToLower(lm[1]))
	}
	p.Title = cleanTitle(title, kind)
	return p
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func canonicalEdition(m string) string {
	s := strings.ToLower(m)
	switch {
	case strings.HasPrefix(s, "director"):
		return "Director's Cut"
	case s == "extended":
		return "Extended"
	case s == "unrated":
		return "Unrated"
	case s == "remastered":
		return "Remastered"
	case s == "imax":
		return "IMAX"
	case s == "theatrical":
		return "Theatrical"
	}
	return m
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
func cleanTitle(title string, kind provider.MediaKind) string {
	cut := len(title)
	consider := func(loc []int) {
		if loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	consider(reResolution.FindStringIndex(title))
	for _, sp := range sourcePatterns {
		consider(sp.re.FindStringIndex(title))
	}
	if kind == provider.KindTV {
		consider(reSeasonEp.FindStringIndex(title))
	} else {
		consider(reYear.FindStringIndex(title))
	}
	name := title[:cut]
	name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.TrimSpace(name)
}
