package hunt_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestRaftExitCriteria is A1's exit run.
//
// Ten thousand seeds by default; RAFT_SEEDS shortens it for iteration. It
// asserts every exit criterion that can be asserted from a sweep, and reports
// the ones that are evidence rather than assertions.
func TestRaftExitCriteria(t *testing.T) {
	if testing.Short() {
		t.Skip("the A1 exit run is not a -short test")
	}
	seeds := uint64(10000)
	if v := os.Getenv("RAFT_SEEDS"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("RAFT_SEEDS: %v", err)
		}
		seeds = n
	}

	start := time.Now()
	c, err := hunt.SweepRaft(0, seeds)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	t.Logf("seeds:        %d in %s (%d seeds/sec)", c.Seeds, elapsed.Round(time.Millisecond),
		int64(c.Seeds)*int64(time.Second)/int64(elapsed))
	t.Logf("verdicts:     pass=%d violation=%d inconclusive=%d errors=%d",
		c.Pass, c.Violations, c.Inconclusive, c.Errors)
	t.Logf("elections:    highest-term=%d started=%d won=%d split-votes=%d",
		c.Terms, c.ElectionsStart, c.ElectionsWon, c.SplitVotes)
	t.Logf("contention:   %d seeds contended, %d seeds never elected anybody",
		c.SeedsWithContention, c.SeedsWithNoLeader)
	for _, why := range c.InconclusiveCauses {
		t.Logf("inconclusive: %s", why)
	}

	// 1. Zero safety violations.
	if c.Violations != 0 {
		t.Errorf("SAFETY VIOLATION: %d across %d seeds; first at seed %d", c.Violations, c.Seeds, c.FirstViolation)
	}

	// 2. Inconclusive is tracked separately and never counted as a pass. A
	//    rising rate means shrink the window or partition harder per key, never
	//    loosen the checker.
	if perMille := c.Inconclusive * 1000 / c.Seeds; perMille > 30 {
		t.Errorf("inconclusive rate %d per mille is above the 30 threshold; the remedy is a smaller "+
			"problem -- shorter histories, harder per-key partitioning -- never a looser checker", perMille)
	}

	// 3. Elections must be observed CONTENDING, not merely completing. A run
	//    where the leader is never challenged proves nothing, and a mix that
	//    produces one is a mix that needs fixing.
	if c.ElectionsWon == 0 {
		t.Fatal("no election was won across the whole sweep; every client operation went unanswered " +
			"and the linearizability checker reported green over a history of unknowns")
	}
	if c.SplitVotes == 0 {
		t.Error("no split vote across the whole sweep: the schedule mix never made two nodes " +
			"campaign in the same term, so the contended path is untested")
	}
	if got := c.SeedsWithContention * 100 / c.Seeds; got < 10 {
		t.Errorf("only %d%% of seeds saw contention (more than one election won, or a split vote); "+
			"the mix is too quiet to be evidence about a consensus protocol", got)
	}
	// A cluster that never elects anybody on a large fraction of seeds is a
	// liveness smell that would hide safety coverage behind unanswered clients.
	if got := c.SeedsWithNoLeader * 100 / c.Seeds; got > 20 {
		t.Errorf("%d%% of seeds never elected a leader; those seeds check nothing", got)
	}
}
