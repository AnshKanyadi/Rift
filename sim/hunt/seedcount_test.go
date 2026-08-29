package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestASweepCountsEachSeedOnce induces the census defect that made A7's exit run
// unaggregatable.
//
// # Why this is worth two real seeds of wall clock
//
// `TestRaftExitAggregate` already refuses a shard whose Seeds does not equal the
// range it covered -- and it did refuse, correctly, after six and a half hours.
// Nothing asked the same question at a scale anybody would run before launching.
// The guard existed and the loop that would have fired it did not.
//
// So this is that guard at N=2. It is the cheapest possible version of the check
// that cost a 25,000-seed run, and it fails on the exact defect: with the
// increment counted in both the loop and CensusOf, this reads 4.
func TestASweepCountsEachSeedOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("runs two real seeds; the -short lane runs every path, not every sweep")
	}
	const from, to = 0, 2
	c, err := hunt.SweepRaft(from, to)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if c.Seeds != to-from {
		t.Errorf("a sweep of [%d,%d) reported %d seeds.\n"+
			"  A census that does not count seeds once is not merely a wrong log line: "+
			"the shard s/seed figure is elapsed/Seeds, and the exit criteria divide "+
			"Inconclusive, SeedsWithContention and SeedsWithNoLeader by it. A doubled "+
			"denominator halves three rate checks, and the no-leader CEILING is the one "+
			"that gets EASIER to pass -- the direction that hides a failure rather than "+
			"inventing one.", from, to, c.Seeds)
	}
}

// TestTheProgressHookSeesEverySeedInOrder pins the observability half.
//
// It also pins the two things the hook must not become: it is called from the
// sweep's own loop, so `done` is monotone and dense with no gaps, and there is
// no goroutine anywhere for the counts to arrive out of.
func TestTheProgressHookSeesEverySeedInOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("runs two real seeds")
	}
	const from, to = 0, 2
	var seeds []uint64
	var dones []int
	var totals []int
	c, err := hunt.SweepRaftWithProgress(from, to, hunt.CurrentOptions(),
		func(seed uint64, done, total int, _ hunt.RaftCensus) {
			seeds = append(seeds, seed)
			dones = append(dones, done)
			totals = append(totals, total)
		})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(seeds) != to-from {
		t.Fatalf("the hook fired %d times for %d seeds; a progress line that skips is worse "+
			"than none, because it reads as progress", len(seeds), to-from)
	}
	for i := range seeds {
		if seeds[i] != uint64(from+i) || dones[i] != i+1 || totals[i] != to-from {
			t.Errorf("call %d reported seed=%d done=%d total=%d, want seed=%d done=%d total=%d",
				i, seeds[i], dones[i], totals[i], from+i, i+1, to-from)
		}
	}
	if c.Seeds != to-from {
		t.Errorf("the hooked sweep counted %d seeds, want %d", c.Seeds, to-from)
	}
}

// TestTheProgressHookCarriesTheRunningCensus is the gate for I1's amendment to
// SweepRaftWithProgress.
//
// # Why the existing gates do not cover it
//
// TestASweepCountsEachSeedOnce and TestTheProgressHookSeesEverySeedInOrder both
// PASS when the census argument is replaced with a zero value -- measured, by
// doing exactly that. They assert the seed sequence and the final count, and the
// new argument is invisible to both.
//
//	AN ARGUMENT ADDED TO A FUNCTION IS NOT COVERED BY THE TESTS THAT COVERED THE
//	FUNCTION. Widening a signature widens the surface, and the old gates keep
//	passing over the part that did not exist when they were written.
//
// What it asserts, and both halves are required:
//
//   - the census GROWS: seed n's running total is not less than seed n-1's, and
//     the last one equals what the sweep returns. Without this the hook could
//     pass a zero value forever and every assertion about "what it found so far"
//     would be true of nothing.
//   - the census is a COPY: mutating it in the hook must not reach the sweep's
//     own accounting, which is what makes the hook safe to hand to a caller who
//     will inevitably store it somewhere.
//
// # THE SECOND HALF IS NOT INDUCED, AND CANNOT BE, AND SAYS SO
//
// The accumulation half is induced: replacing the argument with RaftCensus{}
// fails it. The copy half is not. Go passes a struct parameter by value, so the
// property holds by the language rather than by this code, and no small
// mutation of the sweep can falsify it -- an attempt to write the hook's copy
// back does not work, because the hook mutated its own parameter and not the
// variable passed.
//
//	AN ASSERTION THAT THE LANGUAGE GUARANTEES IS DOCUMENTATION OF INTENT, NOT A
//	CHECK, AND CALLING IT A CHECK IS HOW A SUITE COMES TO CLAIM COVERAGE IT DOES
//	NOT HAVE.
//
// It is kept because it guards a FUTURE change -- someone widening this hook to
// take *RaftCensus for efficiency would make it fail -- and it is labelled so
// nobody counts it as a live check today.
func TestTheProgressHookCarriesTheRunningCensus(t *testing.T) {
	if testing.Short() {
		t.Skip("runs seeds")
	}
	const from, to = 0, 3

	var seen []hunt.RaftCensus
	final, err := hunt.SweepRaftWithProgress(from, to, hunt.CurrentOptions(),
		func(seed uint64, done, total int, running hunt.RaftCensus) {
			seen = append(seen, running)
			// Mutate the copy. The sweep must not notice.
			running.Seeds = 99999
			running.Violations = 99999
		})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(seen) != to-from {
		t.Fatalf("hook fired %d times, want %d", len(seen), to-from)
	}

	for i, c := range seen {
		if c.Seeds != i+1 {
			t.Errorf("call %d saw Seeds=%d, want %d: the running census is not accumulating, so "+
				"anything reported from it describes a sweep that has not happened", i+1, c.Seeds, i+1)
		}
		if i > 0 && c.Pass < seen[i-1].Pass {
			t.Errorf("call %d saw Pass=%d after %d: the running census went backwards",
				i+1, c.Pass, seen[i-1].Pass)
		}
	}
	last := seen[len(seen)-1]
	if last.Seeds != final.Seeds || last.Pass != final.Pass || last.Violations != final.Violations {
		t.Errorf("the last running census disagrees with the returned one:\n  running %+v\n  final   %+v\n"+
			"      A partial result is only useful if it is the same accounting the final result "+
			"comes from", last, final)
	}
	if final.Seeds != to-from {
		t.Errorf("the hook's mutation reached the sweep: final Seeds=%d, want %d. The census must "+
			"be passed by value, or a caller who stores it can corrupt the run reporting on it",
			final.Seeds, to-from)
	}
	t.Logf("running census across %d seeds: %v", len(seen), func() []int {
		var n []int
		for _, c := range seen {
			n = append(n, c.Seeds)
		}
		return n
	}())
}
