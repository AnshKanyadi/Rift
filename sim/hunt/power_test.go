package hunt_test

import (
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestHarnessPower is the lane. Every planted flaw class must still be detected
// at or above its standing floor.
//
// This is the harness's own regression suite, and it should have existed three
// cycles ago. Three harness defects have silently reduced detection power while
// every lane stayed green; a fourth would have gone the same way. A drop from
// 504 of 1000 to 82 now breaks the build instead of appearing in a report
// nobody diffs.
//
// The failure message states the floor, what it was measured against, and why
// it is where it is — because the first question on a red lane is "is the floor
// wrong or is the harness?", and the answer has to be in the output.
func TestHarnessPower(t *testing.T) {
	if testing.Short() {
		t.Skip("the power lane sweeps thousands of seeds; not a -short test")
	}

	for _, f := range hunt.PowerFloors() {
		t.Run(f.Name, func(t *testing.T) {
			elapsedSince := wallTimer()
			sweep := sweepSeeds(t, f.Seeds, f.Scenario)
			elapsed := elapsedSince()

			if sweep.eligible == 0 {
				t.Fatalf("no seed was eligible: every one was refused as a regime the flaw cannot "+
					"manifest in, so this sweep measured nothing\n  floor set against: %s", f.Measured)
			}

			perMille := sweep.caught * 1000 / sweep.eligible
			t.Logf("%d of %d eligible caught (%d per mille, %d refused) in %s",
				sweep.caught, sweep.eligible, perMille, sweep.refused, elapsed.Round(time.Millisecond))
			if sweep.detected {
				t.Logf("seeds-to-detection: %d", sweep.seedsToDetection())
			}

			// The weaker floor: the class must remain reachable, with no claim
			// about how reachable. Used where the measured rate is too small to
			// derive a rate floor from without asserting noise.
			if !sweep.detected {
				t.Fatalf("FLAW CLASS UNREACHABLE: %q was not detected in %d seeds.\n"+
					"  floor:    detected at all\n"+
					"  measured: %s\n"+
					"  why:      %s\n"+
					"  A planted flaw that can no longer be found means the harness lost power, and "+
					"every clean sweep since is a sweep over an empty search space.",
					f.Name, f.Seeds, f.Measured, f.Why)
			}

			if f.MinPerMille > 0 && perMille < f.MinPerMille {
				t.Errorf("DETECTION RATE BELOW FLOOR: %q caught %d per mille, floor is %d.\n"+
					"  measured: %s\n"+
					"  why:      %s\n"+
					"  Either the harness lost power or the floor is wrong; the harness is the "+
					"default assumption and the floor is never lowered to get green.",
					f.Name, perMille, f.MinPerMille, f.Measured, f.Why)
			}

			if got := sweep.seedsToDetection(); got > f.MaxSeedsToDetection {
				t.Errorf("SEEDS-TO-DETECTION ABOVE FLOOR: %q first caught at %d, ceiling is %d.\n"+
					"  measured: %s\n"+
					"  why:      %s\n"+
					"  This degrades independently of the rate: a class can hold its rate while its "+
					"first detection moves far later in the seed space, which is what decides whether "+
					"a smoke lane would ever see it.",
					f.Name, got, f.MaxSeedsToDetection, f.Measured, f.Why)
			}
		})
	}
}

// TestEveryObservableFlawHasAFloor stops the table from silently falling behind
// the flaw set.
//
// A new flaw class added without a floor is a class with no standing
// measurement, which is precisely how a bug class drifts back into being
// uncatchable with nobody noticing — the failure this whole file exists to
// prevent, reintroduced one flaw at a time.
func TestEveryObservableFlawHasAFloor(t *testing.T) {
	floored := make(map[string]bool)
	for _, f := range hunt.PowerFloors() {
		floored[f.Scenario.Flaw.String()] = true
	}

	// Every flaw except the correct build and the two that are covered
	// elsewhere: dup-apply has no workload that retries yet, and
	// ack-before-replicate without failover is a recorded gap asserted in both
	// directions by TestBrokenToyIsCaughtByAHunt.
	exempt := map[string]string{
		"none":      "the correct toy; there is nothing to detect",
		"dup-apply": "no workload retries a request yet, so the class is not reachable to floor",
	}

	for _, name := range flawNames() {
		if floored[name] {
			continue
		}
		if why, ok := exempt[name]; ok {
			t.Logf("exempt: %s -- %s", name, why)
			continue
		}
		t.Errorf("flaw %q has no entry in PowerFloors and is not exempt; a class with no standing "+
			"measurement is one that can quietly stop being detected", name)
	}
}
