package clock

import "time"

// SkewReport is the outcome of checking one pair over one window.
//
// Verdict is deliberately absent: this package computes what the skew *was*,
// and whether it exceeded the bound. Whether exceeding is a failure or the
// point of the experiment is the caller's business, because `envelope: true`
// inverts the checker rather than the arithmetic (DESIGN-A0.4 D5).
type SkewReport struct {
	Max      int64   // largest |Wall_a - Wall_b| over the window, in nanoseconds
	At       Instant // where it occurred
	Bound    int64   // maxOffset, for the record
	Exceeded bool    // Max > Bound
}

// MaxSkew is the exact maximum of |Wall_a - Wall_b| over [from, to].
//
// Exact, not sampled. Between consecutive breakpoints both walls are linear, so
// their difference is linear and its extremum over any interval sits at an
// endpoint. Evaluating at the union of both timelines' breakpoints is therefore
// not an approximation that happens to be good -- it is the answer.
//
// The evaluation set is the union of:
//
//   - the window edges themselves;
//   - every skew segment start on either node, which includes hold ramp
//     endpoints, since a compiled ramp is a segment;
//   - every step on either node, at which BOTH one-sided limits are taken.
//
// That last clause is the one a careful-looking implementation gets wrong. A
// step is a discontinuity in Wall, so the supremum of the difference may be
// attained on the side that is not sampled; evaluating "at the jump" reads as
// one point and is two.
func MaxSkew(a, b *Timeline, from, to Instant) (int64, Instant) {
	points := mergePoints(a.Breakpoints(from, to), b.Breakpoints(from, to))

	var max int64 = -1
	var at Instant
	consider := func(d int64, t Instant) {
		if d < 0 {
			d = -d
		}
		if d > max {
			max, at = d, t
		}
	}

	for _, t := range points {
		consider(int64(a.Wall(t)-b.Wall(t)), t)
		// The left limit differs from the value only at a step, where it is a
		// genuinely different reading that some observer saw.
		if t > from {
			consider(int64(a.WallLimit(t)-b.WallLimit(t)), t)
		}
	}
	return max, at
}

// Check runs MaxSkew and compares against the bound.
//
// In a safety run a true Exceeded is a *harness* failure, not a protocol one:
// the generator is supposed to constrain schedules to satisfy the bound by
// construction, so the checker firing means the generator is wrong. A generator
// bug that quietly exceeded our own assumption would present as a protocol
// violation and cost days (DESIGN-A0 DR-15), which is why the bound is enforced
// twice -- once by construction and once here.
func Check(a, b *Timeline, from, to Instant, maxOffset time.Duration) SkewReport {
	max, at := MaxSkew(a, b, from, to)
	return SkewReport{
		Max:      max,
		At:       at,
		Bound:    int64(maxOffset),
		Exceeded: max > int64(maxOffset),
	}
}

// mergePoints merges two sorted breakpoint lists, dropping duplicates. Written
// out rather than reached for via a map, because a map would put iteration
// order into a checker whose entire job is to be reproducible.
func mergePoints(x, y []Instant) []Instant {
	out := make([]Instant, 0, len(x)+len(y))
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		var next Instant
		switch {
		case j == len(y) || (i < len(x) && x[i] <= y[j]):
			next = x[i]
			i++
		default:
			next = y[j]
			j++
		}
		if len(out) == 0 || out[len(out)-1] != next {
			out = append(out, next)
		}
	}
	return out
}
