// Package quality defines the built-in ranked quality set, the stateless
// decision engine, quality profiles, and their REST surface.
package quality

import "github.com/hellboundg/nexus/internal/parsing"

// QualityDefinition is one entry in the fixed, code-defined quality ladder.
// IDs and ranks are stable; profiles reference definitions by ID.
type QualityDefinition struct {
	ID         int                `json:"id"`
	Name       string             `json:"name"`
	Source     parsing.Source     `json:"source"`
	Resolution parsing.Resolution `json:"resolution"`
	Rank       int                `json:"rank"`
}

// definitions is the global ladder, low→high. Rank == slice index.
var definitions = buildDefinitions()

func buildDefinitions() []QualityDefinition {
	rows := []struct {
		id   int
		name string
		src  parsing.Source
		res  parsing.Resolution
	}{
		{0, "Unknown", parsing.SourceUnknown, parsing.ResUnknown},
		{1, "SDTV", parsing.SourceHDTV, parsing.Res480p},
		{2, "WEBDL-480p", parsing.SourceWEBDL, parsing.Res480p},
		{3, "Bluray-480p", parsing.SourceBluray, parsing.Res480p},
		{4, "HDTV-720p", parsing.SourceHDTV, parsing.Res720p},
		{5, "HDTV-1080p", parsing.SourceHDTV, parsing.Res1080p},
		{6, "WEBDL-720p", parsing.SourceWEBDL, parsing.Res720p},
		{7, "WEBDL-1080p", parsing.SourceWEBDL, parsing.Res1080p},
		{8, "Bluray-720p", parsing.SourceBluray, parsing.Res720p},
		{9, "Bluray-1080p", parsing.SourceBluray, parsing.Res1080p},
		{10, "HDTV-2160p", parsing.SourceHDTV, parsing.Res2160p},
		{11, "WEBDL-2160p", parsing.SourceWEBDL, parsing.Res2160p},
		{12, "Bluray-2160p", parsing.SourceBluray, parsing.Res2160p},
	}
	defs := make([]QualityDefinition, len(rows))
	for i, r := range rows {
		defs[i] = QualityDefinition{ID: r.id, Name: r.name, Source: r.src, Resolution: r.res, Rank: i}
	}
	return defs
}

// Definitions returns the ranked ladder (low→high).
func Definitions() []QualityDefinition {
	out := make([]QualityDefinition, len(definitions))
	copy(out, definitions)
	return out
}

// DefinitionByID looks up a definition by its stable ID.
func DefinitionByID(id int) (QualityDefinition, bool) {
	for _, d := range definitions {
		if d.ID == id {
			return d, true
		}
	}
	return QualityDefinition{}, false
}
