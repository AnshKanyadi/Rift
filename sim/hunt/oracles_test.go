package hunt_test

import (
	"os"
	"strconv"
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
// boundSeeds caps a covering test's seed range when RAFT_SEEDS is set.
//
// # What this costs, stated rather than implied
//
// RAFT_SEEDS is the race lane's knob. Under -race the simulator runs roughly ten
// times slower, which is why Amendment-era A1 bounded the exit run to 200 seeds
// there and recorded the scope. The covering tests were not bounded, and A4's
// growth pushed the package past Go's ten-minute default: the race lane failed
// on a TIMEOUT, with no data race anywhere in it.
//
// So in the race lane every seed search is capped too. **Those runs prove
// nothing about detection while capped** -- a 200-seed slice of a search whose
// first detection is at seed 553 finds nothing, by construction. What they prove
// there is the only thing that lane asks: that no cross-goroutine interaction
// reaches core state while this code runs. The detection claims come from the
// unraced lanes, which run the full ranges, and from the mutant lane, which runs
// them with the defect planted.
func boundSeeds(seeds uint64) uint64 {
	raw := os.Getenv("RAFT_SEEDS")
	if raw == "" {
		return seeds
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == 0 || n >= seeds {
		return seeds
	}
	return n
}

func assertOracleSilent(t *testing.T, oracle string, seeds uint64) {
	t.Helper()
	assertOracleSilentWith(t, oracle, seeds, hunt.CurrentOptions())
}

// assertOracleSilentWith is the same with explicit build options, for an oracle
// whose detection window depends on the configuration.
func assertOracleSilentWith(t *testing.T, oracle string, seeds uint64, opt hunt.RaftOptions) {
	t.Helper()
	byOther := map[string]int{}
	for seed := uint64(0); seed < boundSeeds(seeds); seed++ {
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
// # A4 moved it again, and the mutant lane is what noticed
//
// 500 seeds stopped being enough. M19 came back ALIVE against this test, and the
// cause is a harness change made on purpose one commit earlier: every client
// request now routes from a stale descriptor cache, so a request for a moved key
// takes an extra round through the epoch refusal before it lands. That shifts
// every schedule after the first split.
//
// Measured with M19 planted at commit 20ebf9d: leader-completeness fires on **26
// of 4000 seeds, first at seed 553** -- was first at seed 145. The RATE barely
// moved (10 to 7 per 1500, floor 4). The seeds-to-detection more than tripled,
// which is precisely the regression Amendment A2 says to treat as a harness
// regression even while every mutant is still killed -- and the count-based power
// floor could not see it, because the count was fine.
//
// The range is now 2000, comfortably past the measured 553 with several
// detections inside it. The number moved because a deliberate change moved it,
// and it is recorded rather than absorbed.
func TestLeaderCompletenessOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "leader-completeness", 2000)
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
	for seed := uint64(0); seed < boundSeeds(seeds); seed++ {
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

// TestRebalanceSafetyOracleReportsNothing is the covering test for
// M41-rebalance-removes-before-it-adds.
//
// The mutant makes a move propose the removal of the source as its first step
// instead of adding the destination, which is the whole failure a rebalance
// exists to avoid: the range spends the move one replica short, and a crash in
// that window costs it quorum on a change nobody had to make.
//
// The oracle reads the committed log, so it sees the removal whether or not the
// move ever finishes, and it needs no cooperation from the thing it judges.
//
// 60 seeds. The harness notices the mutant on 192 of the first 300, first at
// seed 0 (scripts/power-mutants.sh, M41).
func TestRebalanceSafetyOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "rebalance-safety", 60)
}

// TestSplitPartitionOracleReportsNothing is the covering test for
// M42-a-split-child-is-born-one-key-wide.
//
// The mutant gives the new range a birth extent that does not match the split
// entry that created it: the parent gives away everything above the cut point
// and the child is born claiming one key less, so the keys between belong to
// nobody.
//
// This is the failure mode no per-range oracle can see. Both ranges are
// internally consistent -- the parent's state matches the parent's log and the
// child's matches the child's -- and every oracle before A4 judged exactly one
// range against exactly its own history. Only a comparison BETWEEN two ranges
// catches it.
//
// 60 seeds. The harness notices the mutant on 300 of the first 300, first at
// seed 0 (scripts/power-mutants.sh, M42) -- every seed that splits at all, which
// is what a broken partition looks like.
func TestSplitPartitionOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "split-partition", 60)
}

// TestSplitInheritsTheConfigurationAtItsIndex is the covering test for
// M46-split-inherits-the-appended-configuration, and it is A4's own bug
// (BUGS.md BUG-015).
//
// A range born from a split inherits its parent's configuration. Asking for the
// ACTIVE one — effective on append — asks a question whose answer is not a
// function of the applied prefix, so two replicas applying the same split entry
// with different appended tails hand the new range two different memberships.
//
// # What catches it is a refusal, not an oracle, and that is the point
//
// No oracle fires. `ApplyConfEntry` declines the next membership entry as an
// illegal transition, because from the behind replica's view it demotes a voter
// to a learner in one step. That is A3's funnel — configuration changes are
// refused at the entry point rather than ignored downstream — catching an A4
// caller that asked the wrong question, one phase after it was built.
//
// # 1000 seeds, and the number is why
//
// Measured with M46 planted, 3000 seeds: **6 detections, first at seed 215**.
// The defect needs a split, a membership change, and two replicas whose appended
// tails differ at the split's index, all in one run. It surfaced originally on
// seed 9595 of the 10,000-seed exit sweep and nowhere smaller.
//
// So the range here is roughly five times the measured seeds-to-detection, the
// same margin rule the power floors use, and the class is floored at
// detected-at-all rather than at a rate — six instances is not a rate.
func TestSplitInheritsTheConfigurationAtItsIndex(t *testing.T) {
	const seeds = 1000
	opt := hunt.CurrentOptions()
	for seed := uint64(0); seed < boundSeeds(seeds); seed++ {
		p, err := hunt.MaterializeRaftWith(seed, opt)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		if _, err := hunt.RunRaftWith(p, opt, nil); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
}

// TestMVCCReadCorrectnessOracleReportsNothing is the covering test for
// M54-read-answers-at-the-newest-version and
// M55-collection-takes-the-version-a-read-still-needs.
//
// The first makes a read answer with the newest version instead of the one
// visible where it asked. The second makes collection remove the version a read
// at the mark's successor still needs. Both are silently wrong reads: the answer
// is a plausible value, and nothing downstream can question it.
//
// # What this oracle needs from the workload, and why the workload changed for it
//
// Neither defect is visible to a read at "now". A workload whose every read
// names the newest timestamp cannot tell an MVCC store from a single-version
// one, so A5 added snapshot reads at remembered timestamps -- and this test is
// the reason they exist rather than a nicety.
func TestMVCCReadCorrectnessOracleReportsNothing(t *testing.T) {
	assertOracleSilent(t, "mvcc-read-correctness", 60)
}
