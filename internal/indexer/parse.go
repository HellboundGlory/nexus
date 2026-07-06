package indexer

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type xmlRSS struct {
	Channel struct {
		Items []xmlItem `xml:"item"`
	} `xml:"channel"`
}

type xmlItem struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Comments  string `xml:"comments"`
	Link      string `xml:"link"`
	PubDate   string `xml:"pubDate"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	// Matches both newznab:attr and torznab:attr (encoding/xml matches local name).
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

var pubDateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822}

func parseReleases(data []byte, indexerID string, proto provider.Protocol) ([]provider.Release, error) {
	var rss xmlRSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	out := make([]provider.Release, 0, len(rss.Channel.Items))
	for _, it := range rss.Channel.Items {
		r := provider.Release{
			Title:       it.Title,
			GUID:        it.GUID,
			InfoURL:     it.Comments,
			DownloadURL: it.Enclosure.URL,
			Size:        it.Enclosure.Length,
			IndexerID:   indexerID,
			Protocol:    proto,
		}
		if r.DownloadURL == "" {
			r.DownloadURL = it.Link
		}
		if r.InfoURL == "" {
			r.InfoURL = it.GUID
		}
		if t, ok := parsePubDate(it.PubDate); ok {
			r.PublishDate = t
		}

		var seeders, peers *int
		for _, a := range it.Attrs {
			switch a.Name {
			case "category":
				if n, err := strconv.Atoi(a.Value); err == nil {
					r.Categories = append(r.Categories, n)
				}
			case "size":
				if r.Size == 0 {
					if n, err := strconv.ParseInt(a.Value, 10, 64); err == nil {
						r.Size = n
					}
				}
			case "seeders":
				if n, err := strconv.Atoi(a.Value); err == nil {
					seeders = &n
				}
			case "peers":
				if n, err := strconv.Atoi(a.Value); err == nil {
					peers = &n
				}
			case "tmdbid":
				if n, err := strconv.Atoi(a.Value); err == nil {
					r.TMDBID = n
				}
			case "imdbid":
				r.IMDbID = strings.TrimPrefix(strings.ToLower(a.Value), "tt")
			case "tvdbid":
				if n, err := strconv.Atoi(a.Value); err == nil {
					r.TVDBID = n
				}
			}
		}
		if proto == provider.ProtocolTorrent {
			r.Seeders = seeders
			if seeders != nil && peers != nil {
				l := *peers - *seeders
				if l < 0 {
					l = 0
				}
				r.Leechers = &l
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func parsePubDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range pubDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
