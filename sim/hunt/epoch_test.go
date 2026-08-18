package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestStaleDurabilityCompletionIsRefused is the epoch guard's induction.
//
// # Why this test exists at all
//
// `store.Node` stamps every sync request with the incarnation that issued it and
// drops completions from any other. That guard was being *counted* and never
// *asked*: RunRaft collected StaleEpochDrops and an EpochFailure and the sweep
// consulted neither, so the mechanism was the sixth in this repository to be
// declared, wired, and never invoked. M14-epoch-check-removed survived the whole
// suite because of it.
//
// So the guard is asked here, in both directions:
//
//	it fires      -- a nonzero drop count across the range, or this test is
//	                 running schedules where a completion never outlives its
//	                 incarnation and proves nothing about the guard at all;
//	it protects   -- every run completes. Remove the check and a completion from
//	                 a dead incarnation reaches the engine, which advances its
//	                 durability watermark past its own last applied sequence and
//	                 panics. That is the third rediscovery of this class, one
//	                 component along (sim.Epoch).
//
// # Why a drop is expected rather than a defect
//
// sim.EpochGuard.Check reads any drop as a driver defect. That reading is right
// for a component that can refuse to emit the completion in the first place, and
// wrong for this one: the simulator owns the event queue, a durability event
// scheduled before a crash is delivered after the restart no matter what the
// driver wants, and the stamp is the only thing that can tell it apart. Here the
// count is a fact about the schedule. What would be a defect is the count being
// zero -- that would mean these schedules never produce the race.
func TestStaleDurabilityCompletionIsRefused(t *testing.T) {
	const seeds = 200

	drops, seedsWithDrops := 0, 0
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		drops += r.StaleEpochDrops
		if r.StaleEpochDrops > 0 {
			seedsWithDrops++
		}
	}

	t.Logf("%d completions from a dead incarnation refused across %d seeds, on %d of them",
		drops, seeds, seedsWithDrops)

	if drops == 0 {
		t.Fatal("the epoch guard never fired across the whole range, so this test asserts nothing " +
			"about it: either the schedule mix stopped producing a crash with a sync in flight, or " +
			"the stamping stopped happening. Both are regressions in the evidence, not in the code")
	}
}
