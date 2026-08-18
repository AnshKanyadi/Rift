package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim"
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

// TestPersistBeforeReplyOracleReportsNothing is the covering test for
// M25-restart-recovers-unsynced-writes: a restart delivered to a node that is
// not down rebuilds it from the engine's VISIBLE state, so it recovers writes no
// crash would have kept and then answers for them.
//
// 500 seeds. The defect it induces first appeared at seed 92 when it was live,
// so this is roughly five times the measured seeds-to-detection, the same margin
// the four above use.
func TestPersistBeforeReplyOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "persist-before-reply", 500)
}

// TestStateMachineSafetyOracleReportsNothing is the covering test for
// M20-conflicting-entry-kept: a follower keeps its own entry at a conflicting
// index instead of truncating from it, so two nodes apply different commands at
// the same log position.
func TestStateMachineSafetyOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "state-machine-safety", 100)
}

// TestClientHistoryIsLinearizable is the end-of-run checker's covering test,
// separate from the four in-run oracles above.
//
// The division of labour is the point and it is not redundancy: the safety
// oracles watch the log, and porcupine watches what a client saw. BUG-004 is the
// case that proves they are different questions -- the four oracles were green
// and correctly green, because an entry overwritten by a later leader is legal
// Raft, while the driver was telling a client its write had succeeded for a slot
// somebody else's command occupied. Only the client history could see that.
//
// 100 seeds: BUG-004 produced 26 violations in 300 seeds when it was live, so
// this is roughly an order of magnitude above the rate needed to catch it.
func TestClientHistoryIsLinearizable(t *testing.T) {
	const seeds = 100
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		for _, rep := range r.Reports {
			if rep.Verdict == sim.VerdictViolation {
				t.Fatalf("seed %d: %s reported a violation the safety oracles did not see: %s",
					seed, rep.Checker, rep.Detail)
			}
		}
	}
}

// TestDurableRecordAgreesWithTheEngine covers the one input to a green verdict
// that the harness derives rather than reads.
//
// # What is actually being checked
//
// The ledger's durability record is what the persist-before-reply oracle judges
// every acknowledgement against, and it cannot be an engine read-back: an engine
// read returns the VISIBLE state, which includes writes a crash would take, and
// feeding that to the oracle is what made it silent for the whole of A1's first
// sweep. So the driver RECORDS what it made durable instead — folded forward
// from the batches it submitted, promoted when the engine reports the sequence
// durable.
//
// That record is a derivation, and a derivation can be wrong. The engine's own
// account is a legitimate input to a check that can only FAIL, so the two are
// compared on every durability completion at which the engine has nothing in
// flight — the one moment a read-back honestly IS the durable state.
//
// Both directions matter and both are induced: M26 leaves a truncated suffix in
// the engine (engine ahead of the record), M27 makes the fold ignore a clear
// (record ahead of the engine). Measured at commit 4c1fd0b, each is caught on 7
// of 300 seeds with seeds-to-detection 84 — against 905 when the comparison ran
// only at recovery, which is the difference between a check that runs twice a
// run and one that runs on every completion.
//
// 300 seeds, roughly three and a half times the measured seeds-to-detection.
func TestDurableRecordAgreesWithTheEngine(t *testing.T) {
	const seeds = 300
	checks := 0
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		checks += r.DurabilityCrossChecks
	}
	t.Logf("%d comparisons of the durability record against the engine across %d seeds", checks, seeds)
	if checks == 0 {
		t.Fatal("the durability record was never once compared against the engine, so this test " +
			"asserts nothing and the oracle's one derived input is unverified")
	}
}
