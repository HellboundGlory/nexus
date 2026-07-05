package quality

import "github.com/hellboundg/nexus/internal/parsing"

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
