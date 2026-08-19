package hunt_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim"
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
	t.Logf("vacuity:      a seed with no leader, or a history below %d per mille decided, is "+
		"inconclusive and never a pass", sim.UnknownDominatedPerMille)
	t.Logf("a2 features:  %d snapshots taken, %d installed, %d leadership transfers requested",
		c.SnapshotsTaken, c.SnapshotsApplied, c.TransfersAsked)
	t.Logf("a3 features:  %d membership changes proposed, %d refused (%d of those a lagging learner)",
		c.ConfProposed, c.ConfRefused, c.LagRefused)
	t.Logf("a4 features:  %d splits proposed, %d applied, most ranges on one machine %d, "+
		"%d requests refused for a stale descriptor epoch",
		c.SplitsProposed, c.SplitsApplied, c.Ranges, c.StaleEpochRefusals)
	t.Logf("a3 recovery:  %d restarts recovered a log carrying a configuration change, %d of them "+
		"cross-checked against a snapshot configuration", c.ConfRecoveries, c.ConfCrossChecks)
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

	// 2b. A2's features must have RUN. A green sweep in which no snapshot was
	//     ever taken says nothing about snapshots, and this is the same
	//     vacuous-green rule the census enforces for elections: the system has to
	//     do the thing whose safety is being asserted.
	if c.SnapshotsTaken == 0 {
		t.Error("no snapshot was taken across the whole sweep; every snapshot oracle in this run " +
			"checked nothing, and the compaction path was never executed")
	}
	if c.SnapshotsApplied == 0 {
		t.Error("no snapshot was ever INSTALLED across the whole sweep. Taking one exercises " +
			"compaction; installing one is what exercises InstallSnapshot racing appends and " +
			"restarts, which CLAUDE.md names as A2's danger zone")
	}
	if c.ConfProposed == 0 {
		t.Error("no membership change was ever proposed across the whole sweep; every configuration " +
			"oracle in this run checked nothing, and the change path was never executed")
	}
	if c.SplitsApplied == 0 {
		t.Error("no split was applied across the whole sweep; every per-range oracle judged a " +
			"single range, which is what they already did before A4")
	}
	if c.Ranges < 2 {
		t.Error("no machine ever hosted more than one range, so multi-raft was never exercised")
	}
	if c.ConfRecoveries == 0 {
		t.Error("no restart ever recovered a log carrying a configuration change, so nothing in " +
			"this sweep exercised a configuration surviving a crash")
	}
	if c.ConfCrossChecks == 0 {
		t.Error("no recovery was ever checked against a snapshot's configuration, so the one " +
			"place recomputeConf can be compared against an independent derivation never ran")
	}
	if c.TransfersAsked == 0 {
		t.Error("no leadership transfer was requested across the whole sweep, so the transfer path " +
			"is unexercised and its exit criterion unevidenced")
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
