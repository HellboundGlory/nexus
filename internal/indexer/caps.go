package indexer

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type CapCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Capabilities struct {
	Limits struct {
		Max     int `json:"max"`
		Default int `json:"default"`
	} `json:"limits"`
	Search      bool          `json:"search"`
	TVSearch    bool          `json:"tvSearch"`
	MovieSearch bool          `json:"movieSearch"`
	Categories  []CapCategory `json:"categories"`
}

func (c Capabilities) supports(t provider.SearchType) bool {
	switch t {
	case provider.SearchTV:
		return c.TVSearch
	case provider.SearchMovie:
		return c.MovieSearch
	default:
		return c.Search
	}
}

// xmlCaps mirrors the Newznab/Torznab caps document.
type xmlCaps struct {
	XMLName xml.Name `xml:"caps"`
	Limits  struct {
		Max     int `xml:"max,attr"`
		Default int `xml:"default,attr"`
	} `xml:"limits"`
	Searching struct {
		Search      xmlAvail `xml:"search"`
		TVSearch    xmlAvail `xml:"tv-search"`
		MovieSearch xmlAvail `xml:"movie-search"`
	} `xml:"searching"`
	Categories struct {
		Categories []struct {
			ID   int    `xml:"id,attr"`
			Name string `xml:"name,attr"`
		} `xml:"category"`
	} `xml:"categories"`
}

type xmlAvail struct {
	Available string `xml:"available,attr"`
}

func parseCaps(data []byte) (Capabilities, error) {
	var x xmlCaps
	if err := xml.Unmarshal(data, &x); err != nil {
		return Capabilities{}, fmt.Errorf("parse caps: %w", err)
	}
	var c Capabilities
	c.Limits.Max = x.Limits.Max
	c.Limits.Default = x.Limits.Default
	c.Search = x.Searching.Search.Available == "yes"
	c.TVSearch = x.Searching.TVSearch.Available == "yes"
	c.MovieSearch = x.Searching.MovieSearch.Available == "yes"
	for _, cat := range x.Categories.Categories {
		c.Categories = append(c.Categories, CapCategory{ID: cat.ID, Name: cat.Name})
	}
	return c, nil
}

func discoverCaps(ctx context.Context, hc *http.Client, base, apiKey string) (Capabilities, error) {
	raw, err := buildSearchURL(base, apiKey, provider.Query{Type: provider.SearchType("caps")})
	if err != nil {
		return Capabilities{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Capabilities{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Capabilities{}, fmt.Errorf("%w: %v", ErrIndexerUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Capabilities{}, ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return Capabilities{}, fmt.Errorf("%w: caps status %d", ErrIndexerUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Capabilities{}, err
	}
	return parseCaps(body)
}
