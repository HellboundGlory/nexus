package indexer

import (
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseCaps(t *testing.T) {
	data, err := os.ReadFile("testdata/caps.xml")
	if err != nil {
		t.Fatal(err)
	}
	caps, err := parseCaps(data)
	if err != nil {
		t.Fatal(err)
	}
	if caps.Limits.Max != 100 || caps.Limits.Default != 50 {
		t.Fatalf("limits: %+v", caps.Limits)
	}
	if !caps.supports(provider.SearchGeneric) {
		t.Error("expected generic search supported")
	}
	if !caps.supports(provider.SearchTV) {
		t.Error("expected tv search supported")
	}
	if caps.supports(provider.SearchMovie) {
		t.Error("expected movie search NOT supported")
	}
	if len(caps.Categories) != 2 {
		t.Fatalf("categories: %+v", caps.Categories)
	}
}
