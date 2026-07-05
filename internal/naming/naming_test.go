package naming

import "testing"

func TestRenderEpisodeAndMovie(t *testing.T) {
	tok := Tokens{SeriesTitle: "The Show", EpisodeTitle: "Pilot", Quality: "Bluray-1080p", Season: 2, Episode: 5}
	got := Render(DefaultConfig().EpisodeFile, tok)
	want := "The Show - S02E05 - Pilot [Bluray-1080p]"
	if got != want {
		t.Fatalf("episode = %q, want %q", got, want)
	}

	mt := Tokens{MovieTitle: "Movie Title", Year: 2019, Quality: "WEBDL-720p"}
	if got := Render(DefaultConfig().MovieFile, mt); got != "Movie Title (2019) [WEBDL-720p]" {
		t.Fatalf("movie = %q", got)
	}
	if got := Render(DefaultConfig().SeasonFolder, tok); got != "Season 02" {
		t.Fatalf("season folder = %q", got)
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize(`A:B/C\D?E*F "G" <H>|I`); got != "ABCDEF G HI" {
		t.Fatalf("sanitize = %q", got)
	}
	if got := Sanitize("trailing dots..."); got != "trailing dots" {
		t.Fatalf("trailing = %q", got)
	}
}
