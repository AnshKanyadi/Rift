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
		func(seed uint64, done, total int) {
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
