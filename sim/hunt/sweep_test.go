package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/sim/toy"
)

// TestWorkerCountDoesNotAffectResults is the property the hunt rests on.
//
// A hunt whose findings depended on `--workers` could not hand anyone a
// reproduction, which is the only thing it exists to do: "seed 29 fails" would
// mean "seed 29 fails on a machine with this many cores, today". Worse, it would
// fail in the quiet direction — a run at 8 workers that missed what 1 worker
// finds reports a clean sweep.
//
// So the same range is swept at one worker and at several, and every field a
// report or a corpus entry can be built from must match: which seeds were
// examined, which were refused as ineligible, and what verdict each produced
// with what detail.
func TestWorkerCountDoesNotAffectResults(t *testing.T) {
	if testing.Short() {
		t.Skip("sweeping a range four times is not a -short test")
	}

	sc := toy.Scenario{Flaw: toy.FlawAckBeforeSync, Placement: toy.PlacementReactive}
	const from, to = 0, 250

	base, err := hunt.Sweep(from, to, sc, 1)
	if err != nil {
		t.Fatalf("single-worker sweep: %v", err)
	}
	baseCensus := hunt.Summarize(base)
	t.Logf("1 worker: %d seeds, %d eligible, %d violations, first at seed %d",
		baseCensus.Seeds, baseCensus.Eligible, baseCensus.Violations, baseCensus.FirstViolation)

	for _, workers := range []int{2, 3, 8, 32} {
		got, err := hunt.Sweep(from, to, sc, workers)
		if err != nil {
			t.Fatalf("%d-worker sweep: %v", workers, err)
		}

		if len(got) != len(base) {
			t.Fatalf("%d workers examined %d seeds, 1 worker examined %d", workers, len(got), len(base))
		}
		for i := range base {
			a, b := base[i], got[i]
			switch {
			case a.Seed != b.Seed:
				t.Fatalf("%d workers: result %d is seed %d, single-worker had seed %d; the sweep is "+
					"not ordered by seed and a hunt cannot name a stable first violation",
					workers, i, b.Seed, a.Seed)
			case a.Refused != b.Refused:
				t.Errorf("%d workers: seed %d eligibility differs (refused %t vs %t)",
					workers, a.Seed, b.Refused, a.Refused)
			case (a.Err == nil) != (b.Err == nil):
				t.Errorf("%d workers: seed %d errored in one sweep and not the other", workers, a.Seed)
			}
			if !verdictsMatch(a.Reports, b.Reports) {
				t.Errorf("%d workers: seed %d produced different verdicts\n  1 worker: %v\n  %d:       %v",
					workers, a.Seed, a.Reports, workers, b.Reports)
			}
		}

		c := hunt.Summarize(got)
		if c != baseCensus {
			t.Errorf("%d workers produced a different census:\n  1 worker: %+v\n  %d workers: %+v",
				workers, baseCensus, workers, c)
		}
	}
	t.Logf("identical results at 1, 2, 3, 8 and 32 workers")
}

// verdictsMatch compares what a corpus entry would be built from: the verdict
// and the detail, not the timing or anything else that varies between machines.
func verdictsMatch(a, b []sim.Report) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Verdict != b[i].Verdict || a[i].Detail != b[i].Detail || a[i].At != b[i].At {
			return false
		}
	}
	return true
}

// TestSweepRejectsABackwardsRange induces the refusal rather than describing it.
func TestSweepRejectsABackwardsRange(t *testing.T) {
	if _, err := hunt.Sweep(500, 100, toy.Scenario{Flaw: toy.FlawNone, Placement: toy.PlacementReactive}, 1); err == nil {
		t.Fatal("a backwards seed range was accepted")
	} else {
		t.Logf("induced: %v", err)
	}
}
