package provider

import (
	"testing"
	"time"
)

func TestQueryAndReleaseExtensions(t *testing.T) {
	season := 2
	q := Query{
		Type:       SearchTV,
		Term:       "the show",
		Categories: []int{5000, 5040},
		Season:     &season,
		Limit:      100,
	}
	if q.Type != SearchTV || *q.Season != 2 || len(q.Categories) != 2 {
		t.Fatal("query fields not set as expected")
	}

	seeders := 12
	r := Release{
		Title:       "The.Show.S02E05",
		DownloadURL: "http://x/t.torrent",
		Size:        1024,
		Protocol:    ProtocolTorrent,
		Seeders:     &seeders,
		PublishDate: time.Unix(0, 0),
	}
	if r.Protocol != ProtocolTorrent || *r.Seeders != 12 {
		t.Fatal("release fields not set as expected")
	}
}
