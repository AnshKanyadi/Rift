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

// assertOracleSilent sweeps the configuration the sweep runs and fails only when
// the named oracle reports.
//
// # The default follows the phase, and now it cannot lag
//
// It lagged once. It was still A2's options after A3 landed, so the membership
// oracles were induced against a configuration that schedules ZERO membership
// changes, and five mutants reported ALIVE against tests that could not have
// caught them -- with the blame pointing at the oracles. An induction run in a
// configuration that cannot produce the defect proves the same nothing as no
// induction, and it proves it while looking like evidence.
//
// What prevents it now is not vigilance: hunt.CurrentOptions is the single
// source of truth, read by the sweep, by these inductions and by the power
// probe. They cannot disagree, because there is only one of them.
func assertOracleSilent(t *testing.T, oracle string, seeds uint64) {
	t.Helper()
	assertOracleSilentWith(t, oracle, seeds, hunt.CurrentOptions())
}

// assertOracleSilentWith is the same with explicit build options, for an oracle
// whose detection window depends on the configuration.
func assertOracleSilentWith(t *testing.T, oracle string, seeds uint64, opt hunt.RaftOptions) {
	t.Helper()
	byOther := map[string]int{}
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaftWith(seed, opt)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaftWith(p, opt, nil)
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
	// # This one arm runs the A1 shape, and the reason is a measured finding
	//
	// Log matching's window IS the retained log: it fires when two nodes agree at
	// some (index, term) and differ below it, and both halves have to be on disk
	// at the same moment for anyone to see it. Two of A2's features close that
	// window, and the numbers were taken with M18 planted, 500 seeds each:
	//
	//	no pre-vote, no snapshots   log-matching 10, first seed 15
	//	pre-vote on                 log-matching  0
	//	full A2                     log-matching  0
	//
	// Pre-vote is the one that costs it, and the mechanism is not mysterious: a
	// node that cannot inflate the term cannot make the cluster re-elect, so
	// conflicting appends across terms become rare and divergent tails stay
	// short. **A feature that makes the cluster calmer makes the fault injection
	// weaker**, and that is worth writing down because it is not a trade anybody
	// chose.
	//
	// M18's defect is still caught under full A2 -- state machine safety takes it
	// on 7 of 500 seeds -- so nothing is going unnoticed. What is lost is the
	// ATTRIBUTION, and an induction that cannot attribute is an induction that
	// proves the suite works rather than that this oracle does. So log matching
	// is induced where it has a window, and that it runs in the main sweep under
	// compaction as well is a separate, weaker claim.
	opt := hunt.RaftOptions{PreVote: false, SnapshotThreshold: 0, Transfers: 0}
	assertOracleSilentWith(t, "log-matching", 500, opt)
}

// TestLeaderCompletenessOracleReportsNothing is the covering test for
// M19-vote-for-a-shorter-log: the candidate up-to-date check is dropped, so a
// node missing committed entries can win and those entries are overwritten.
//
// 500 seeds, and the number moved for a measured reason. Under A1 the mutant was
// caught on 228 of 300 seeds, first at seed 1. Under A2 it is caught on 1 of 300,
// first at seed 81.
//
// The cause is the one that also narrowed log matching's window: pre-vote stops a
// disrupted node inflating the term, so most of the elections a dropped
// up-to-date check could have stolen never happen. What is left is the genuine
// case -- a node with a real chance of winning that should not -- and it is
// rarer by construction rather than by accident.
//
// So the range is sized to the new rate, at roughly six times the measured
// seeds-to-detection, and the drop is recorded here rather than absorbed into a
// number nobody re-derives.
func TestLeaderCompletenessOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "leader-completeness", 500)
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

// TestApplyContinuityOracleReportsNothing is the covering test for
// M32-apply-stream-skips-an-index: the driver acknowledges one more applied
// index than it applied, so a command reaches no state machine.
//
// 50 seeds; the mutant is caught on every one of the first 300, first at seed 0.
func TestApplyContinuityOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "apply-continuity", 50)
}

// TestSnapshotEquivalenceOracleReportsNothing is the covering test for
// M33-state-machine-drops-a-command: a state machine that silently drops one
// command in seven, so its snapshots are not the state its own log produces.
//
// This is the exit criterion — recovery from a snapshot plus tail lands where
// recovery from the full log lands — checked from outside, against a model the
// harness builds itself.
//
// 50 seeds; the mutant is caught on 262 of the first 300, first at seed 0.
func TestSnapshotEquivalenceOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "snapshot-equivalence", 50)
}

// TestSnapshotPrefixIsNotOverwritten is the covering test for
// M34-append-from-zero-over-a-snapshot, and it is A2's own bug (BUGS.md
// BUG-009).
//
// A leader whose view of a follower has been reset sends an append with
// PrevLogIndex 0 — "start from the beginning". The consistency check treated
// index 0 as agreeable to everybody, because before compaction everybody DID
// have a beginning. A node whose prefix is already inside a snapshot then
// accepted it and overwrote entries it had applied and told the cluster were
// committed.
//
// 3000 seeds. The defect is caught on 1 of 3000, first at seed 1364, which is a
// rate low enough to be worth stating plainly: it took the 10,000-seed exit run
// to find it at all, and the range here is sized to roughly twice the measured
// seeds-to-detection rather than to something comfortable.
//
// The instrument is the assertion BUG-007 corrected. Its old form fired on the
// durable watermark and would have caught a legal truncation while missing this;
// the corrected form fires on the commit index, which is what Raft actually
// guarantees, and this is the first thing it caught.
func TestSnapshotPrefixIsNotOverwritten(t *testing.T) {
	// # The A2 shape, and the reason is measured
	//
	// The defect needs a leader that sends PrevLogIndex 0 -- "start from the
	// beginning" -- to a follower that has compacted past it. A leader only
	// sends that if it has not compacted itself, and A3's cluster compacts
	// sooner: four nodes sharing the same client traffic, one of them a learner,
	// so each voter's log crosses the snapshot threshold earlier.
	//
	// Measured with M34 planted, 3000 seeds: A2 shape 2 detections, first at seed
	// 2065; A3 shape ZERO. The class did not become safe, it became unreachable
	// in this configuration -- which is exactly the difference the power lane
	// exists to make visible, and it is visible here because the lane now covers
	// this class.
	const seeds = 3000
	opt := hunt.A2Options()
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaftWith(seed, opt)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		if _, err := hunt.RunRaftWith(p, opt, nil); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
}

// TestSingleServerChangeOracleReportsNothing is the covering test for
// M35-conf-change-carries-two-servers.
//
// The overlapping-quorum argument that makes single-node changes safe without
// joint consensus holds only while configurations differ by at most one server
// (DESIGN-A3 §4). A change carrying two is not a smaller joint consensus, it is
// the case joint consensus exists for.
func TestSingleServerChangeOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "single-server-change", 100)
}

// TestConfigurationSurvivesRecovery is the covering test for the two mutants
// that break a configuration's journey through a crash: M38 skips the recompute
// a truncation forces, and M39 leaves the configuration out of the snapshot.
//
// It asserts two things and the second is the one that keeps it honest: no run
// fails, AND the cross-check between a node's recovered configuration and one
// derived independently from the same recovered bytes actually ran. A run in
// which no node ever recovered a configuration would pass the first trivially.
func TestConfigurationSurvivesRecovery(t *testing.T) {
	const seeds = 200
	checks, recoveries := 0, 0
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		checks += r.ConfCrossChecks
		recoveries += r.ConfRecoveries
	}
	t.Logf("%d recoveries carried a configuration change in the log; %d were checked against a "+
		"snapshot configuration", recoveries, checks)
	if checks == 0 {
		t.Fatal("no recovered configuration was ever compared against an independent derivation, " +
			"so this test asserts nothing about the one function DESIGN-A3 §3 names as where the " +
			"bugs live")
	}
	if recoveries == 0 {
		t.Fatal("no restart ever recovered a log carrying a configuration change")
	}
}
