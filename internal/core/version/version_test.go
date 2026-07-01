package version

import "testing"

func TestVersionDefault(t *testing.T) {
	if got := Version(); got != "dev" {
		t.Fatalf("Version() = %q, want %q", got, "dev")
	}
}
