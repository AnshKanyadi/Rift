package hunt_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/sim/plan"
)

// TestPowerProbe measures how often the harness NOTICES a planted defect.
//
// # Why this exists, and what was wrong before it
//
// `make power` has stood since A0 as the lane that fails when detection power
// drops. It covered four toy flaw classes and zero mutant classes. So when
// pre-vote landed and M18's log-matching detections went from 10 in 500 to 0,
// and M19's from 228 in 300 to 1, the lane was green -- not because it judged the
// drop acceptable but because it had never been looking. A lane whose whole
// purpose is to catch a power regression, silent through the largest power
// regression in the project, is not a lane.
//
// This probe is the missing half. It is a MEASUREMENT, not an assertion: it runs
// a seed range against a mutated tree and reports how many seeds noticed.
// scripts/power-mutants.sh is what turns the number into a build failure.
//
// # A detection is "the run did not complete cleanly"
//
// Any oracle violation, any end-of-run violation, any harness error, any panic,
// or a run that elected nobody. Deliberately not "the oracle I expected fired":
// power is about whether the machinery notices, and attribution belongs to the
// mutant lane, which already checks it per class. Defining detection narrowly
// here would let a class stay covered on paper while the number that matters
// moves.
func TestPowerProbe(t *testing.T) {
	raw := os.Getenv("POWER_SEEDS")
	if raw == "" {
		t.Skip("POWER_SEEDS unset: this is a probe driven by scripts/power-mutants.sh, not a test")
	}
	seeds, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("POWER_SEEDS: %v", err)
	}

	// The default is what the sweep runs, so a floor measures the machine as it
	// actually is. The alternatives exist because some classes are only
	// observable in an older shape, and naming which is the point.
	//
	// Getting this wrong is instructive: the first version measured under A2's
	// options, which schedule no membership changes at all, and reported that
	// three A3 mutants were undetectable. They were undetectable in a
	// configuration that never exercised them.
	opt := hunt.CurrentOptions()
	switch os.Getenv("POWER_CONFIG") {
	case "a1":
		opt = hunt.RaftOptions{PreVote: false, SnapshotThreshold: 0, Transfers: 0}
	case "a2":
		opt = hunt.A2Options()
	case "a3":
		// Every historical shape is named, because a patch that pins itself to
		// one is making a claim that has to keep meaning the same thing. The
		// DEFAULT is `current`: unnamed means "whatever the sweep runs today",
		// which is what the old `a3` default silently meant after A3 stopped
		// being current.
		opt = hunt.A3Options()
	case "a4":
		opt = hunt.A4Options()
	case "a5":
		opt = hunt.A5Options()
	}

	// # POWER_UNTHROTTLED, and the figure it exists to produce
	//
	// DESIGN-A0 §7 item 9 records what the A5 collection throttle costs in
	// DETECTION: M53's class went from 1 detection in 60 seeds to 0 in 3,000
	// once collection was throttled. That is a number about the harness, and
	// CARRY-FORWARD requires it re-measured under A6's shape rather than
	// inherited from A5's.
	//
	// `TestUnthrottledCollector` measures the other half — collection VOLUME and
	// whether the unthrottled shape finds a violation the throttled one hides —
	// and it cannot measure this one, because detection is per mutant class and
	// that lane runs no mutant. So the switch goes here, where the class is.
	//
	// **After the config switch, not before.** Every case above REPLACES `opt`
	// wholesale, so a flag set beforehand is silently dropped for any class that
	// names a shape — which is the same defect as the `a3` default that meant
	// `current` for a phase, one line down.
	if os.Getenv("POWER_UNTHROTTLED") != "" {
		opt.GCUnthrottled = true
	}

	detected, first := 0, int64(-1)
	for seed := uint64(0); seed < seeds; seed++ {
		if noticed(seed, opt) {
			detected++
			if first < 0 {
				first = int64(seed)
			}
		}
	}
	fmt.Printf("POWER detected=%d of=%d first=%d\n", detected, seeds, first)
}

// noticed reports whether the harness objected to this seed in any way.
func noticed(seed uint64, opt hunt.RaftOptions) (bad bool) {
	defer func() {
		if recover() != nil {
			bad = true
		}
	}()
	p, err := hunt.MaterializeRaftWith(seed, opt)
	if err != nil {
		return true
	}
	r, err := hunt.RunRaftWith(p, opt, nil)
	if err != nil {
		return true
	}
	if r.Violated != nil || r.Census.ElectionsWon == 0 {
		return true
	}
	for _, rep := range r.Reports {
		if rep.Verdict == sim.VerdictViolation {
			return true
		}
	}
	return false
}

var _ = plan.Plan{}
