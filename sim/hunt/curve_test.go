package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim/toy"
)

// TestWindowCurveIsRecorded keeps the fsync sensitivity curve as a live artifact
// rather than a number in a comment that ages badly.
//
// It exists because the previous curve was measured under the Trigger budget
// defect, on a harness at a sixth of its power, and was then used to justify a
// gate margin for three cycles. A curve that is re-derived by running it cannot
// go stale that way: it is re-measured every time the lane runs, and if the shape
// changes the numbers in toy.DefaultSyncLatency's rationale are wrong and must be
// updated with it.
//
// It asserts the shape rather than exact values, because the shape is the claim:
// detection is negligible at or below the reactive crash delay and at full power
// immediately above it, which is what says the crash delay is the binding
// constraint and the replication round trip is not.
func TestWindowCurveIsRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("the curve sweeps 1000 seeds per row; not a -short test")
	}
	var atDelayRefused, atDelayEligible, aboveDelay int
	delay := toy.CrashDelay()
	for _, w := range []clock.Instant{delay, delay + 1_000_000, 12_000_000, 20_000_000, 50_000_000} {
		sc := toy.Scenario{Flaw: toy.FlawAckBeforeSync, Placement: toy.PlacementReactive, SyncLatency: w}
		s := sweepSeeds(t, 1000, sc)
		first := "not detected"
		if s.detected {
			first = itoa(s.seedsToDetection())
		}
		rate := 0
		if s.eligible > 0 {
			rate = s.caught * 1000 / s.eligible
		}
		t.Logf("fsync %6dus: eligible %4d refused %4d caught %3d (%4d per mille) seeds-to-detection %s",
			int64(w)/1000, s.eligible, s.refused, s.caught, rate, first)
		switch w {
		case delay:
			atDelayRefused, atDelayEligible = s.refused, s.eligible
		case delay + 1_000_000:
			aboveDelay = rate
		}
	}

	// The shape, asserted, and deliberately not as a detection rate at the crash
	// delay: the gate now refuses that regime, so no measurement can be taken
	// there and an assertion about its rate would be vacuous. What is asserted is
	// the pair of facts the constraint rests on.
	//
	// Below is the historical measurement that justified the constraint, taken at
	// MinWindowMargin 1 before the reachability check existed and recorded rather
	// than re-derivable: at exactly the crash delay, 344 seeds were eligible and
	// 11 per mille detected; one millisecond above, 534. That is the evidence the
	// gate was set from.
	//
	// Note the eligibility column at 11ms: 344 of 1000. The rate saturates one
	// millisecond past the crash delay, but two thirds of seeds are refused there
	// by the *equivalence* constraint, which is why DefaultSyncLatency is 12ms
	// and not 11 -- see its derivation.
	if atDelayEligible != 0 {
		t.Errorf("a window equal to the crash delay left %d seeds eligible; the reachability "+
			"constraint is not refusing the regime it was added for", atDelayEligible)
	}
	if atDelayRefused == 0 {
		t.Error("no seed was refused at the crash delay, so the gate is not binding there")
	}
	if aboveDelay <= 300 {
		t.Errorf("one millisecond past the crash delay detection is only %d per mille; the curve "+
			"no longer saturates immediately above the bound and the recorded rationale is wrong", aboveDelay)
	}
	t.Logf("step confirmed: %d of %d seeds refused at the crash delay; %d per mille one millisecond above it",
		atDelayRefused, atDelayRefused+atDelayEligible, aboveDelay)
}

func itoa(u uint64) string {
	if u == 0 {
		return "0"
	}
	var b []byte
	for u > 0 {
		b = append([]byte{byte('0' + u%10)}, b...)
		u /= 10
	}
	return string(b)
}
