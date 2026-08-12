//rift:allow-nondeterminism the plan-compile float boundary: at_frac is authored intent, converted to nanoseconds here and never read again, so no float reaches the evaluation path or the trace hash

// This file is the only place in a core package where floating point appears,
// and it exists so that the exception is a file rather than a habit.
//
// The standing rule (DESIGN-A0.4 §4): no floating point on any path feeding the
// trace hash or replay identity. Floats live only at plan-compile boundaries
// with results materialized as integers before evaluation. `at_frac` is such a
// boundary -- a human writes 0.98 in a plan because 0.98 is what they mean --
// and the conversion below is where it stops being a float.
//
// The conversion itself is safe under the fusing rule that motivated the ban: a
// bare multiply has no addend to be fused with, and the result is rounded to an
// integer immediately, so two machines cannot disagree about it.
package clock

import (
	"fmt"
	"math"
	"time"
)

// fracNanos converts an authored fraction of maxOffset into nanoseconds.
//
// It rejects the values that have no interpretation rather than propagating
// them: a NaN target would compare false against every bound and quietly turn
// the skew checker into a no-op, which is the worst available outcome.
func fracNanos(frac float64, maxOffset time.Duration) (int64, error) {
	if math.IsNaN(frac) {
		return 0, fmt.Errorf("clock: at_frac is NaN; a NaN target compares false against every bound and would silently disable the check")
	}
	if math.IsInf(frac, 0) {
		return 0, fmt.Errorf("clock: at_frac is infinite")
	}

	scaled := frac * float64(maxOffset)
	if math.Abs(scaled) > math.MaxInt64/4 {
		return 0, fmt.Errorf("clock: at_frac %v of %s does not fit in the modelled range", frac, maxOffset)
	}
	return int64(math.Round(scaled)), nil
}

// Percent expresses an authored fraction as an integer percentage, so that Go
// code constructing a Hold carries no float literal at all. Plans authored as
// JSON keep the float field the ruling specified; this is for the call sites
// that would otherwise each need their own exemption.
func Percent(n int64) float64 { return float64(n) / 100 }
