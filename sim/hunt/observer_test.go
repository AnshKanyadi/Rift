package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
)

// The ledger must not change the run it watches.
//
// # This had never been asked, in eight phases
//
// `raftcheck.Ledger` has watched every simulated run this project has ever done.
// There was no way to run without it until BUG-055 made it optional, so "the
// oracle does not perturb the system" was an architectural belief with nothing
// behind it — and the whole verification argument rests on it:
//
//	AN OBSERVER THAT CHANGES THE RUN IS NOT AN OBSERVER. If the ledger perturbed
//	anything, then every seed in the corpus reproduces a system that only exists
//	while it is being watched, and the corpus is evidence about the wrong
//	artifact.
//
// The check is a byte-identical trace hash across the same plan run both ways.
// It is cheap. If it ever fails it is a far larger finding than any throughput
// number, which is why it is asserted rather than assumed.
//
// # STATED SCOPE: rebalance is off, and that is a finding, not a convenience
//
// `sim/hunt`'s rebalance driver calls `ledger.Ranges()` to choose which range to
// move. The HARNESS'S WORKLOAD therefore reads the oracle's data structure, so
// with the ledger off the driver sees nothing and orders no moves -- and the
// traces differ for a reason that is about the harness rather than about the
// system under test.
//
//	THE OBSERVER IS NOT COUPLED TO THE SYSTEM; IT IS COUPLED TO THE WORKLOAD.
//	The distinction matters and is worth stating precisely rather than rounding
//	to "the observer changes the run".
//
// This test therefore runs the A7 shape with `Rebalances = 0`, which is the
// largest configuration in which the question can be asked at all. GF-58 records
// the coupling as an obligation: until the driver reads range state from
// somewhere that is not an oracle, the identity is asserted over everything
// except replica movement.
func TestTheLedgerIsAnObserver(t *testing.T) {
	opt := hunt.CurrentOptions()
	opt.Rebalances = 0
	for seed := uint64(0); seed < boundSeeds(64); seed++ {
		p, err := hunt.MaterializeRaftWith(seed, opt)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}

		observed := &sim.Trace{}
		obs, err := hunt.RunRaftWith(p, opt, observed)
		if err != nil {
			t.Fatalf("seed %d observed: %v", seed, err)
		}

		// The SAME plan, materialized again, so neither run can consume state
		// the other left behind.
		p2, err := hunt.MaterializeRaftWith(seed, opt)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		blind := opt
		blind.Unobserved = true
		unobserved := &sim.Trace{}
		blindRes, err := hunt.RunRaftWith(p2, blind, unobserved)
		if err != nil {
			t.Fatalf("seed %d unobserved: %v", seed, err)
		}
		if !blindRes.Unobserved {
			t.Fatalf("seed %d: an unobserved run did not report itself unobserved", seed)
		}

		if observed.Sum() != unobserved.Sum() {
			t.Fatalf("seed %d: the ledger CHANGED THE RUN.\n"+
				"      observed   %s\n"+
				"      unobserved %s\n"+
				"      An observer that changes the run is not an observer, and every seed in the\n"+
				"      corpus would then reproduce a system that only exists while it is watched.\n"+
				"      This is a larger finding than anything it was measured beside.",
				seed, observed.Short(), unobserved.Short())
		}

		// AND THE HISTORIES, because the trace hash does not cover payload
		// CONTENT. See the measured limit below.
		assertSameHistory(t, seed, obs.History, blindRes.History)
	}
}

// assertSameHistory compares what clients observed, event by event.
//
// # WHAT THIS PAIR DETECTS, established by two plants rather than by reading
//
// `sim.Trace.Step` folds `(step, at, kind, node, payloadLen)` -- not the
// payload's CONTENT. So the hash alone proves the event stream's SHAPE: which
// events, at which instants, to which nodes, of which sizes. The history closes
// most of the rest, because it carries the values clients actually saw.
//
// Where the boundary falls was measured, not argued:
//
//	PLANT                                       RESULT     WHY
//	drop one message per Ready when unobserved  FAILED     the event stream changed
//	                                            on seed 0
//	one extra hlc.Now() per client op when      PASSED     nothing a client can
//	unobserved                                  BOTH       observe changed, and no
//	                                                       event changed shape
//
// **The second plant is a boundary marker, not a missed defect.** It advances an
// internal logical counter; the values written and read are identical, every
// message keeps its size, and the history is identical event for event. A test
// that failed on it would be asserting something stronger than "the observer did
// not change the run" -- it would be asserting that no internal state differs,
// which is not what an observer claim means and not what any oracle reads.
//
//	SAYING THAT PRECISELY MATTERS. "The plant passed, so the test is blind" is the
//	tempting reading and it is wrong; the honest one is that the pair detects
//	CHANGES TO THE RUN, and the plant did not make one.
//
// The residual gap is therefore narrow and real: a difference in internal state
// that never reaches a message, a client, or an event's shape. Nothing an oracle
// reads lives there, which is why this is stated rather than closed.
func assertSameHistory(t *testing.T, seed uint64, a, b *sim.History) {
	t.Helper()
	if a == nil || b == nil {
		t.Fatalf("seed %d: a run produced no history", seed)
	}
	ea, eb := a.Events(), b.Events()
	if len(ea) != len(eb) {
		t.Fatalf("seed %d: observed run recorded %d operations, unobserved %d",
			seed, len(ea), len(eb))
	}
	for i := range ea {
		if ea[i] != eb[i] {
			t.Fatalf("seed %d: operation %d differs between observed and unobserved runs.\n"+
				"      observed   %+v\n"+
				"      unobserved %+v\n"+
				"      The trace hash covers payload LENGTH, not content; this is what catches a\n"+
				"      value that changed without changing size.", seed, i, ea[i], eb[i])
		}
	}
}
