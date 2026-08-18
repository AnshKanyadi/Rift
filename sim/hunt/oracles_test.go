package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// The four safety oracles, each with a planted violation behind it.
//
// # Why these tests exist in this shape
//
// Amendment A2 and the standing rule: no oracle counts until its failure has
// been induced and observed. A sweep reporting zero violations is compatible
// with four working oracles and with four oracles that cannot detect anything,
// and nothing in the sweep distinguishes those. So each oracle has a mutant in
// sim/mutants/ that plants a real defect in raft/ -- not a flag, not a fixture,
// a source defect -- and one of these tests is its covering test.
//
// Each test asserts about ITS OWN oracle only: across the seed range, the first
// violation observed was never this one's. A violation from a different oracle
// is logged and passed over, which is what makes the attribution meaningful --
// M20's defect trips state-machine safety on seed 15 and log matching on seed
// 237, and only a per-oracle assertion can say which mutant induced which.
//
// The assertion is precisely "the FIRST violation observed was never mine",
// because a run halts at the first violation and an oracle later in the order
// may simply not have been reached. That is the honest statement, and it is
// exactly the statement the mutant lane needs.
//
// # Where the seed counts come from
//
// Each is roughly four to six times the measured seeds-to-detection of its
// mutant, the same margin rule the harness-power floors use. Measured at commit
// 124fd37 over 300 seeds unless stated:
//
//	oracle                  mutant                            detected   first seed
//	election-safety         M17-vote-twice-in-one-term        146/300    0
//	leader-completeness     M19-vote-for-a-shorter-log        228/300    1
//	state-machine-safety    M20-conflicting-entry-kept         46/300    15
//	log-matching            M18-prev-log-check-term-blind       1/300    237
//
// Log matching is the outlier and the number is not noise. A log that diverges
// almost always shows up first as a divergent APPLY: applies happen continuously,
// while the log-matching oracle needs the narrower coincidence of two durable
// logs agreeing at some later (index, term) while differing earlier. So it is
// floored at detected-at-all with a 1000-seed range, the same treatment
// floors.go gives dirty-read and ack-counting, and no rate is claimed for it.

// assertOracleSilent sweeps and fails only when the named oracle reports.
func assertOracleSilent(t *testing.T, oracle string, seeds uint64) {
	t.Helper()
	byOther := map[string]int{}
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if r.Violated == nil {
			continue
		}
		if r.Violated.Checker == oracle {
			t.Fatalf("%s reported a violation on seed %d after %d seeds: %s",
				oracle, seed, seed+1, r.Violated.Detail)
		}
		byOther[r.Violated.Checker]++
	}
	for other, n := range byOther {
		t.Logf("%s was silent; %s reported on %d seed(s), which is not this test's business", oracle, other, n)
	}
}

// TestElectionSafetyOracleReportsNothing is the covering test for
// M17-vote-twice-in-one-term: a node that has already voted grants again, so two
// candidates win the same term.
func TestElectionSafetyOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "election-safety", 50)
}

// TestLogMatchingOracleReportsNothing is the covering test for
// M18-prev-log-check-term-blind: the consistency check compares the previous
// index and ignores its term, so a follower splices a leader's suffix onto a
// prefix that disagrees.
func TestLogMatchingOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "log-matching", 1000)
}

// TestLeaderCompletenessOracleReportsNothing is the covering test for
// M19-vote-for-a-shorter-log: the up-to-date check is dropped, so a candidate
// missing committed entries can win and those entries are gone.
func TestLeaderCompletenessOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "leader-completeness", 50)
}

// TestStateMachineSafetyOracleReportsNothing is the covering test for
// M20-conflicting-entry-kept: a follower keeps its own entry at a conflicting
// index instead of truncating from it, so two nodes apply different commands at
// the same log position.
func TestStateMachineSafetyOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "state-machine-safety", 100)
}
