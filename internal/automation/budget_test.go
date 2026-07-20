package automation

import "testing"

func TestNewBudget(t *testing.T) {
	cases := []struct {
		name          string
		limit         int
		inFlight      int
		wantTakes     int // how many takes before allows() goes false
		wantUnlimited bool
	}{
		{name: "limit 1 nothing in flight", limit: 1, inFlight: 0, wantTakes: 1},
		{name: "limit 3 one in flight", limit: 3, inFlight: 1, wantTakes: 2},
		{name: "already at limit", limit: 1, inFlight: 1, wantTakes: 0},
		{name: "over limit clamps to zero", limit: 1, inFlight: 5, wantTakes: 0},
		{name: "zero disables the gate", limit: 0, inFlight: 99, wantUnlimited: true},
		{name: "negative disables the gate", limit: -1, inFlight: 99, wantUnlimited: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBudget(tc.limit, tc.inFlight)
			if tc.wantUnlimited {
				for i := 0; i < 1000; i++ {
					if !b.allows() {
						t.Fatalf("unlimited budget refused after %d takes", i)
					}
					b.take()
				}
				return
			}
			for i := 0; i < tc.wantTakes; i++ {
				if !b.allows() {
					t.Fatalf("want %d takes, refused at %d", tc.wantTakes, i)
				}
				b.take()
			}
			if b.allows() {
				t.Fatalf("want exactly %d takes, but it still allows more", tc.wantTakes)
			}
		})
	}
}
