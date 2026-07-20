package automation

import "math"

// budget tracks how many more downloads automation may start for one series.
// It is shared across a whole series search (all seasons, packs and episodes)
// so the cap is per series, not per season.
type budget struct{ remaining int }

// newBudget derives the remaining allowance from the configured limit and how
// many rows the series already has in flight. A limit <= 0 disables the gate —
// that is the documented off switch, not a bad value to be clamped.
func newBudget(limit, inFlight int) *budget {
	if limit <= 0 {
		return &budget{remaining: math.MaxInt}
	}
	rem := limit - inFlight
	if rem < 0 {
		rem = 0
	}
	return &budget{remaining: rem}
}

func (b *budget) allows() bool { return b.remaining > 0 }

func (b *budget) take() {
	if b.remaining > 0 && b.remaining < math.MaxInt {
		b.remaining--
	}
}
