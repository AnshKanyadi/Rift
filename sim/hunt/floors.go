package hunt

import "github.com/anshkanyadi/rift/sim/toy"

// This file is the harness's own regression suite: a standing floor under every
// planted flaw class's detection power, checked by a lane.
//
// # Why it exists
//
// Six harness defects have now been found, every one of them in the observer
// rather than the observed, and until this file nothing in the repository would
// have noticed the next:
//
//  1. the loop marked a crashed node down without telling it, so the crash
//     injector injected nothing and the whole ack-before-durable class was
//     unreachable;
//  2. the generator's out-of-order client sequences manufactured violations
//     (this one was loud — 913 of 1000 — and is the exception);
//  3. `Trigger` counted `Times` per condition rather than per rule, so the
//     restart rule sharing a trigger with the crash rule never fired, and
//     `ack-before-sync` detection ran at 82 of 1000 instead of 504 for five
//     checklist steps;
//  4. the fire-count machinery -- the designated defence against exactly this
//     class -- was itself broken four ways, including `Counters.Check()` never
//     being called on any run path, so `min_fires` was decorative from step 4.
//
// Five of the six were silent, and two were found only because they were
// suppressing something else. **A drop from 504 to 82 must break the build,
// not appear in a report nobody diffs.** That is what this lane is.
//
// # Why floors rather than exact values
//
// The sweep is deterministic: the same seeds produce the same counts on every
// run, so an exact assertion is possible. It is deliberately not used. An exact
// value fails on any benign change — one more client op, a different key space —
// and a lane that cries wolf is a lane people delete. A floor with margin passes
// ordinary drift and fails a collapse, which is the only failure worth a build
// break.
//
// # Where a class is weak, the floor is "detected at all"
//
// `dirty-read` and `ack-counting` currently detect at 1 in 1000. A rate floor
// derived from a single detection would be noise, so those two are floored at
// detected-at-all: the class must remain reachable, and no claim is made about
// how reachable. They are the first classes to re-measure at higher seed counts
// when the soak farm switches on at A1 (docs/TOY-FINDINGS.md).

// Floor is one flaw class's standing power requirement.
//
// Measured is the number the floor was derived from, kept beside it: a floor
// with no record of what it was set against cannot be re-derived when the
// machinery changes, and every ablation and measurement in this project expires
// when the machinery under it moves.
type Floor struct {
	Name     string
	Scenario toy.Scenario
	Seeds    uint64

	// MinPerMille is the detection rate this class must sustain, per mille of
	// eligible seeds. Zero means the weaker requirement: detected at all.
	MinPerMille int

	// MaxSeedsToDetection bounds how long a hunt may take to find the first
	// instance. It is the number that answers "would a smoke lane catch this",
	// and it degrades independently of the rate — a class can keep its rate
	// while its first detection moves far later in the seed space.
	MaxSeedsToDetection uint64

	Measured string
	Why      string
}

// PowerFloors is the table the lane enforces.
//
// Every planted flaw class that is observable at all appears here. A class
// recorded as *unobservable* is covered by the bidirectional gap assertion in
// TestBrokenToyIsCaughtByAHunt instead, which fails if it starts being caught.
func PowerFloors() []Floor {
	return []Floor{
		{
			Name:                "ack-before-sync",
			Scenario:            toy.Scenario{Flaw: toy.FlawAckBeforeSync, Placement: toy.PlacementReactive},
			Seeds:               1000,
			MinPerMille:         250,
			MaxSeedsToDetection: 10,
			Measured:            "499 of 1000 eligible, seeds-to-detection 1, at commit 9b186eb (12ms default window)",
			Why: "the headline class, and the one the Trigger defect suppressed. The floor is set at " +
				"roughly half the measured rate: the suppressed level was 82 per mille, so 250 fails " +
				"loudly on a regression of that kind while absorbing ordinary drift in the schedule mix. " +
				"Seeds-to-detection is floored at 10 against a measured 1, because a class this strong " +
				"moving to the tenth seed already means something changed.",
		},
		{
			Name:                "ack-before-replicate under failover",
			Scenario:            toy.Scenario{Flaw: toy.FlawAckBeforeReplicate, Placement: toy.PlacementReactive, Failover: true},
			Seeds:               1000,
			MinPerMille:         15,
			MaxSeedsToDetection: 100,
			Measured:            "32 of 1000 eligible, seeds-to-detection 7, at commit 9b186eb (12ms default window)",
			Why: "detection needs a dropped replicate to coincide with the targeted crash, so the rate " +
				"is inherently lower than ack-before-sync's and noisier against changes in the drop " +
				"dice. Floored at 15 per mille, under half the measured 35, and at 100 seeds to first " +
				"detection against a measured 7 — wide enough that a change to the link parameters " +
				"does not break the build, narrow enough that losing the class does.",
		},
		{
			Name:                "dirty-read",
			Scenario:            toy.Scenario{Flaw: toy.FlawDirtyRead, Placement: toy.PlacementReactive},
			Seeds:               1000,
			MinPerMille:         0,
			MaxSeedsToDetection: 1000,
			Measured:            "1 of 1000 eligible, seeds-to-detection 104, at commit 9b186eb (12ms default window)",
			Why: "detected at all, no rate claimed. A floor derived from a single detection would be " +
				"noise, and asserting 1 per mille would break on any change that reshuffles which seed " +
				"happens to hit it. What must not silently change is that the class remains reachable.",
		},
		{
			Name:                "ack-counting",
			Scenario:            toy.Scenario{Flaw: toy.FlawAckCounting, Placement: toy.PlacementReactive, Failover: true},
			Seeds:               1000,
			MinPerMille:         0,
			MaxSeedsToDetection: 1000,
			Measured:            "1 of 1000 eligible, seeds-to-detection 154, at commit 9b186eb (12ms default window)",
			Why: "same reasoning as dirty-read. Needs a duplicated ack, a crash after the " +
				"acknowledgement, and a promotion, so it is the rarest coincidence in the table.",
		},
	}
}
