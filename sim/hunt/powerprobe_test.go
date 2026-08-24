package hunt_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// # POWER_FROM makes the probe shardable, on the exit run's argument
	//
	// `MaterializeRaftWith(seed, opt)` derives a whole plan from the seed alone,
	// so a seed's verdict does not depend on which invocation ran it. A rare
	// class needs thousands of seeds and one process is hours; contiguous
	// non-overlapping ranges in separate processes are the same seeds in a
	// fraction of the wall clock, which is exactly why `scripts/exit-run.sh`
	// exists. `first` stays an ABSOLUTE seed so shards can be compared.
	var from uint64
	if v := os.Getenv("POWER_FROM"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("POWER_FROM: %v", err)
		}
		from = n
	}

	detected, first := 0, int64(-1)
	var agg hunt.RaftCensus
	for seed := from; seed < seeds; seed++ {
		bad, c := noticed(seed, opt)
		agg = hunt.AddCensus(agg, c)
		if bad {
			detected++
			if first < 0 {
				first = int64(seed)
			}
		}
	}

	// # The SWEEP verdict, and the class of detector the per-seed rate cannot see
	//
	// A per-seed rate can only measure a detector that fires on a seed. Several
	// of the exit criteria are aggregate NON-VACUITY assertions -- *no resolver
	// ever left a live owner alone*, *no snapshot was ever taken* -- and a
	// mutant that makes one of them true is caught by the exit run and invisible
	// to a rate.
	//
	// `M73` is exactly that, and it is the class this fix FOUND. It removes
	// BUG-024's incarnation guard, so `StaleIncarnation` goes 10-15 per fifty
	// seeds to a flat zero and the criterion *no read answer from a pre-restart
	// incarnation was ever rejected* fires in every shard -- against `0 of 200`
	// on the per-seed rate, before and after. It had been carrying an opt-out
	// that said a floor would need a 24-hour sweep; the sweep verdict costs 200
	// seeds (DESIGN-A6 §42).
	//
	// `M62` was the case this fix was WRITTEN for, and the guess about it was
	// half wrong, which is worth leaving in. The guess was that it drives
	// `ResolveWaits` to zero. It does not: the node's apply path keeps its own
	// copy of the expiry comparison and `M62` mutates only `kv.ResolveLock`, so
	// waits fall (83 -> 60 in one shard of fifty) without vanishing, and no
	// criterion fired. What `M62` needed was not a better probe but an oracle
	// nobody had written, and it has one now (§40): 18 of 200, first at seed 20.
	//
	// So the probe consults `exitCriteriaFailures` over the accumulated census,
	// which is the same list the exit run asserts, and reports what failed. The
	// lane decides whether a failure is NEW by running this on the unmutated
	// tree at the same seed count -- a difference, not a presence, for the reason
	// DESIGN-A6 §16.4 records about the corpus matcher.
	fails := exitCriteriaFailures(agg)
	fmt.Printf("POWER detected=%d of=%d from=%d first=%d sweepfail=%d\n",
		detected, seeds-from, from, first, len(fails))
	// # The census itself, so "reachable" stops being an argument
	//
	// A class that measures zero has two very different explanations and the
	// numbers above cannot tell them apart: the defect happened and nothing
	// noticed, or the defect never happened at all. `M63` was the second --
	// it mutated a distinction the code does not make -- and the only way that
	// was established was by reading the call sites.
	//
	// A census that is byte-identical to the unmutated tree's says the mutation
	// changed nothing the harness counts, which is evidence for the second. A
	// census that differs says the behaviour moved and no oracle spoke, which is
	// the first and is a finding about the oracles.
	fmt.Printf("POWER-CENSUS %+v\n", agg)
	for _, f := range fails {
		one := strings.Join(strings.Fields(f), " ")
		if len(one) > 110 {
			one = one[:110]
		}
		fmt.Printf("POWER-SWEEP %s\n", one)
	}
}

// noticed reports whether the harness objected to this seed in any way.
// noticed reports whether the harness objected to this seed in any way, and
// returns the seed's census so the caller can ask the exit criteria about the
// sweep as a whole.
//
// # Detection here is deliberately broad
//
// Any oracle violation, any end-of-run violation, any harness error, any panic,
// or a run that elected nobody. Not "the oracle I expected fired": power is
// about whether the machinery notices, and attribution belongs to the mutant
// lane, which already checks it per class.
//
// What it is NOT is the whole story, and that is what the census is for. A
// detector that only speaks about a whole sweep cannot speak here.
func noticed(seed uint64, opt hunt.RaftOptions) (bad bool, c hunt.RaftCensus) {
	defer func() {
		if recover() != nil {
			bad = true
		}
	}()
	p, err := hunt.MaterializeRaftWith(seed, opt)
	if err != nil {
		return true, c
	}
	r, err := hunt.RunRaftWith(p, opt, nil)
	if err != nil {
		return true, c
	}
	c = hunt.CensusOf(seed, r)
	if r.Violated != nil || r.Census.ElectionsWon == 0 {
		return true, c
	}
	for _, rep := range r.Reports {
		if rep.Verdict == sim.VerdictViolation {
			return true, c
		}
	}
	return false, c
}

var _ = plan.Plan{}
