package hunt_test

import (
	"fmt"
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
	reportExitCensus(t, c)
	assertExitCriteria(t, c)
}

// reportExitCensus prints what the run did.
//
// Split out of TestRaftExitCriteria so the SHARDED exit run reports and asserts
// through exactly the same code. Two copies of the exit criteria, one for the
// whole-run form and one for the aggregate, is two chances to let them drift --
// and the aggregate is the one the phase is signed off on.
func reportExitCensus(t *testing.T, c hunt.RaftCensus) {
	t.Helper()
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
	// Proposed is per LEADER and applied is per REPLICA, so applied is several
	// times proposed and the labels say which. A ratio read as a failure rate
	// would be alarming and wrong.
	t.Logf("a4 splits:    %d proposed by leaders, %d applied by replicas, most ranges on one machine %d",
		c.SplitsProposed, c.SplitsApplied, c.Ranges)
	t.Logf("a4 routing:   %d requests refused for a stale descriptor epoch, %d committed commands "+
		"refused for naming a key outside the range's extent",
		c.StaleEpochRefusals, c.OutOfExtentRefusals)
	t.Logf("a4 rebalance: %d moves ordered, %d completed, %d raced an unrelated membership change",
		c.MovesOrdered, c.MovesCompleted, c.MovesRacingChurn)
	t.Logf("a5 mvcc:      %d collections proposed, %d applied, %d versions collected",
		c.GCProposed, c.GCApplied, c.VersionsCollected)
	t.Logf("a5 reads:     %d snapshot reads at remembered timestamps, %d refused below the mark, "+
		"%d writes refused below the mark", c.SnapshotReads, c.MVCCReadsRefused, c.MVCCWritesRefused)
	t.Logf("a5 clock:     %d peer timestamps refused for exceeding maxOffset (expected zero: the "+
		"schedule mix keeps skew inside the envelope)", c.EnvelopeRefusals)
	t.Logf("a3 recovery:  %d restarts recovered a log carrying a configuration change, %d of them "+
		"cross-checked against a snapshot configuration", c.ConfRecoveries, c.ConfCrossChecks)
	t.Logf("a6 bank:      %d transfers started, %d reached their commit point, %d aborted after "+
		"losing a race, %d abandoned, %d lost the record to a resolver",
		c.TxnStarted, c.TxnCommitted, c.TxnAborted, c.TxnAbandoned, c.TxnLostToResolver)
	t.Logf("a6 reads:     %d snapshot reads issued, %d blocked by a lock, %d restarted above an "+
		"uncertain commit, %d refused below the mark, %d unparseable",
		c.TxnReads, c.ReadsBlocked, c.UncertaintyRestarts, c.TxnReadsRefused, c.UnparseableReads)
	t.Logf("a6 recovery:  %d resolutions issued by a READER that ran into a lock; the primary's "+
		"range answered %d already-decided, DECLARED %d owners dead, and left %d alone as alive",
		c.ReaderResolves, c.ResolveAlreadyDecided, c.ResolveDeclaredDead, c.ResolveWaits)
	t.Logf("a6 authority: %d rolled-back records in the final states were attributed to a "+
		"RESOLVER rather than to a coordinator's own abort, and every one of them had a resolve "+
		"behind it carrying Deadline < ExpireAt (M62's detector, DESIGN-A6 section 40)",
		c.ResolverDeclarations)
	t.Logf("a6 verdicts:  readers carried %d verdicts back to a locked key (%d forward, %d back); "+
		"%d rolled forward and %d rolled back at the key, %d found the lock already gone",
		c.ResolvedForward+c.ResolvedBack, c.ResolvedForward, c.ResolvedBack,
		c.RollForwards, c.RollBacks, c.ResolveNoLock)
	t.Logf("a6 conflicts: %d prewrites refused for a newer commit or an newer read, %d for a "+
		"live lock, %d transaction records lost to somebody who decided first",
		c.WriteConflicts, c.PrewriteBlocked, c.TxnRaceLost)
	t.Logf("a6 bug-022:   %d read marks staged, %d prewrites refused because somebody had "+
		"already been answered above their snapshot (the two halves are counted apart because "+
		"they fail apart)", c.ReadMarks, c.ReadConflicts)
	t.Logf("a6 audits:    %d started, %d read every account at one timestamp, %d hit a lock, "+
		"%d restarted on an uncertain commit, %d reads re-asked after no answer",
		c.AuditsStarted, c.AuditsComplete, c.AuditsLocked, c.AuditsUncertain, c.AuditsRetried)
	t.Logf("a6 si:        %d second-pass reads re-asked an audit's accounts at its own timestamp; "+
		"snapshot-isolation compared %d settled answers against an earlier one",
		c.SecondPassReads, c.SnapshotsCompared)
	t.Logf("a6 bug-019:   %d commits or rollbacks found another transaction's lock and left it "+
		"alone; %d transactions shared a (primary, start) identity with an earlier one (expected "+
		"zero: see DESIGN-A6 section 15)", c.ForeignLocksKept, c.IdentityCollisions)
	for _, why := range c.InconclusiveCauses {
		t.Logf("inconclusive: %s", why)
	}

}

// assertExitCriteria is every exit assertion, over a census.
// exitCriteriaFailures is every exit assertion, as a list of failures over a
// census.
//
// # Why it is a list and not a set of t.Error calls
//
// Because two callers need the same answer and only one of them is a test.
// `TestPowerProbe` measures whether the harness NOTICES a planted defect, and
// its notion of noticing was a hand-listed subset -- a violation, a report
// verdict, an election census -- which is blind to every class whose detector is
// one of the assertions below. `M62` drives `ResolveWaits` to zero and this list
// says so; the probe measured `0 of 300` and called the class undetectable.
//
// So the criteria live in one place and both callers consult all of them. A new
// criterion is covered by construction rather than by somebody remembering to
// add it to a second list, which is the same rule AddCensus is written out for.
//
// A `t.Fatal` in the original becomes an entry like any other: stopping early
// was a convenience for a test and would silently shorten the probe's answer.
func exitCriteriaFailures(c hunt.RaftCensus) []string {
	var out []string
	add := func(s string) { out = append(out, s) }
	if c.Seeds == 0 {
		return []string{"the census covers zero seeds, so every criterion below is a division by " +
			"nothing and none of them is evidence"}
	}
	// 1. Zero safety violations.
	if c.Violations != 0 {
		add(fmt.Sprintf("SAFETY VIOLATION: %d across %d seeds; first at seed %d", c.Violations, c.Seeds, c.FirstViolation))
	}

	// 2. Inconclusive is tracked separately and never counted as a pass. A
	//    rising rate means shrink the window or partition harder per key, never
	//    loosen the checker.
	if perMille := c.Inconclusive * 1000 / c.Seeds; perMille > 30 {
		add(fmt.Sprintf("inconclusive rate %d per mille is above the 30 threshold; the remedy is a smaller "+
			"problem -- shorter histories, harder per-key partitioning -- never a looser checker", perMille))
	}

	// 2b. A2's features must have RUN. A green sweep in which no snapshot was
	//     ever taken says nothing about snapshots, and this is the same
	//     vacuous-green rule the census enforces for elections: the system has to
	//     do the thing whose safety is being asserted.
	if c.SnapshotsTaken == 0 {
		add("no snapshot was taken across the whole sweep; every snapshot oracle in this run " +
			"checked nothing, and the compaction path was never executed")
	}
	if c.SnapshotsApplied == 0 {
		add("no snapshot was ever INSTALLED across the whole sweep. Taking one exercises " +
			"compaction; installing one is what exercises InstallSnapshot racing appends and " +
			"restarts, which CLAUDE.md names as A2's danger zone")
	}
	if c.ConfProposed == 0 {
		add("no membership change was ever proposed across the whole sweep; every configuration " +
			"oracle in this run checked nothing, and the change path was never executed")
	}
	if c.SplitsApplied == 0 {
		add("no split was applied across the whole sweep; every per-range oracle judged a " +
			"single range, which is what they already did before A4")
	}
	if c.Ranges < 2 {
		add("no machine ever hosted more than one range, so multi-raft was never exercised")
	}
	// A4's three mechanisms must have RUN, on the same rule as A2's and A3's.
	//
	// The epoch refusal is the one this rule was written for. It read ZERO
	// across 10,000 seeds and nothing said so, because the sweep's clients
	// carried no routing at all and the check was skipped on every request they
	// ever made. That is the eleventh instance of the vacuous-green class, and
	// it was guarding an invariant CLAUDE.md names by name.
	if c.StaleEpochRefusals == 0 {
		add("no request was ever refused for a stale descriptor epoch across the whole sweep; " +
			"the epoch check is the mechanism behind \"no request served under a stale descriptor " +
			"epoch\", and a sweep that never reaches it says nothing about that invariant")
	}
	if c.OutOfExtentRefusals == 0 {
		add("no committed command was ever refused for naming a key outside its range's extent; " +
			"that check is BUG-014's fix and this sweep never executed it")
	}
	// Ordered is not enough. A move that stalls is SAFE -- it leaves an extra
	// replica and removes nothing -- so the rebalance oracle passes over a sweep
	// in which every single move stalled, and would be saying nothing.
	if c.MovesCompleted == 0 {
		add(fmt.Sprintf("%d replica moves were ordered and NONE completed; a stalled move is safe, so the "+
			"rebalance oracle is green over a mechanism that never finished once", c.MovesOrdered))
	}
	// # The bidirectional half of a RECORDED GAP
	//
	// DESIGN-A4 section 10 records "a move racing an unrelated membership
	// change" as unexercised, and says why: the two drivers are separated in
	// time because a move's add and somebody else's removal are
	// indistinguishable in a committed log. A recorded gap has to be able to
	// become wrong, or it is a claim that decays into a lie the first time the
	// schedule mix moves.
	//
	// So this asserts ZERO, and failing here is GOOD NEWS badly reported: it
	// means the interleaving is now reachable, the rebalance oracle's
	// attribution needs revisiting before it can judge those seeds, and the
	// design doc is stale. It is on A6's checklist for exactly that reason.
	if c.MovesRacingChurn != 0 {
		add(fmt.Sprintf("%d move windows contained a membership change the move did not make. "+
			"DESIGN-A4 section 10 records this interleaving as UNEXERCISED and the record is now "+
			"wrong. Before deleting this assertion, revisit rebalance-safety's attribution: it "+
			"cannot tell whose removal it is looking at when both drivers are live, which is what "+
			"produced 252 false violations in 300 seeds (BUG-016)", c.MovesRacingChurn))
	}
	// A5's mechanisms, on the same rule: every count printed above is asserted
	// on or deleted (DESIGN-A4 section 9.4b).
	if c.GCApplied == 0 {
		add("no collection was ever applied across the whole sweep; the garbage-collection " +
			"mark never moved, so every claim about what is answerable below it is a claim about " +
			"a mechanism that did not run")
	}
	if c.VersionsCollected == 0 {
		add("collections applied and collected NOTHING: the mark moved over a history with no " +
			"versions under it, so the collector was never asked to remove anything")
	}
	if c.SnapshotReads == 0 {
		add("no read named a remembered timestamp; every read was at now, which is the one " +
			"shape that cannot tell an MVCC store from a single-version one")
	}
	if c.MVCCReadsRefused == 0 {
		add("no read was ever refused below the collection mark. That refusal is the whole " +
			"reason the mark is a mark and not a cleanup: without it a read below the mark is " +
			"answered from a history that is no longer there")
	}
	// # A write below the mark is asserted at ZERO, and it is a recorded gap
	//
	// Every write in this workload is stamped at propose, so its timestamp is
	// always above the collection mark and the refusal is unreachable HERE. It
	// is not unreachable in general: A6's transactions write at a timestamp
	// chosen when the transaction began, which can fall behind the mark while
	// the transaction is in flight, and that is precisely the case the refusal
	// exists for.
	//
	// So the mechanism is exercised by kv.TestWritingBelowTheMarkIsRefused, the
	// sweep records ZERO, and this assertion makes the record able to become
	// wrong: the day a workload can reach it, the lane says so instead of
	// quietly proving something new. DESIGN-A5 section 11 carries the entry.
	if c.MVCCWritesRefused != 0 {
		add(fmt.Sprintf("%d writes were refused below the collection mark. DESIGN-A5 section 11 records "+
			"this as unreachable in A5's workload -- every write is stamped at propose, above the "+
			"mark by construction -- and the record is now wrong", c.MVCCWritesRefused))
	}

	// # The envelope refusal is asserted at ZERO, and that is a bidirectional gap
	//
	// The schedule mix keeps skew inside maxOffset, so no peer should ever be
	// refused. A nonzero count means either the mix has drifted outside the
	// envelope -- in which case every bounded-skew claim in this sweep is about
	// a different experiment -- or the check is firing on nodes that are inside
	// it. Both are worth stopping for, and the envelope EXPERIMENT that
	// deliberately exceeds maxOffset is STRETCH (Amendment A6), not this lane.
	if c.EnvelopeRefusals != 0 {
		add(fmt.Sprintf("%d peer timestamps were refused for exceeding maxOffset in a bounded-skew sweep. "+
			"Either the schedule mix now leaves the envelope, or the check is refusing nodes "+
			"inside it; hlc.TestCausalityUnderSkew is the model of what bounded means",
			c.EnvelopeRefusals))
	}
	if c.ConfRecoveries == 0 {
		add("no restart ever recovered a log carrying a configuration change, so nothing in " +
			"this sweep exercised a configuration surviving a crash")
	}
	if c.ConfCrossChecks == 0 {
		add("no recovery was ever checked against a snapshot's configuration, so the one " +
			"place recomputeConf can be compared against an independent derivation never ran")
	}
	if c.TransfersAsked == 0 {
		add("no leadership transfer was requested across the whole sweep, so the transfer path " +
			"is unexercised and its exit criterion unevidenced")
	}

	// # A6's mechanisms, on the same rule: asserted or deleted
	//
	// The three groups are the phase. A sweep in which transfers committed but
	// no read ever met a lock has exercised the happy path of a protocol whose
	// entire difficulty is in the unhappy one.
	if c.TxnCommitted == 0 {
		add("no transaction ever reached its commit point across the whole sweep; every " +
			"transaction oracle in this run judged a workload that never committed")
	}
	if c.TxnAbandoned == 0 {
		add("no coordinator was ever abandoned mid-transaction. Every lock this sweep " +
			"resolved belonged to a transaction that finished, so the recovery path -- the " +
			"reason resolution exists -- ran against nothing it was built for")
	}
	if c.ReadsBlocked == 0 {
		add("no read ever found a lock across the whole sweep, so reader-side lock discovery " +
			"never happened and every resolution path below it is unreachable")
	}
	if c.ReaderResolves == 0 {
		add("no reader ever issued a resolution. Locks were discovered and nothing was done " +
			"about them, which is the half of Percolator that is not bookkeeping")
	}
	// Both directions of the verdict, separately. A sweep that only ever rolled
	// forward has never executed rollback and vice versa, and A6's exit criteria
	// name both by name.
	if c.RollForwards == 0 {
		add("no lock was ever ROLLED FORWARD: no resolver ever found a committed primary " +
			"whose secondary was unwritten, which is the case that makes a commit point mean " +
			"anything")
	}
	if c.RollBacks == 0 {
		add("no lock was ever ROLLED BACK: no transaction was ever cleaned up after its " +
			"coordinator stopped, so the other half of resolution is unevidenced")
	}
	if c.ResolveDeclaredDead == 0 {
		add("no resolver ever DECLARED an owner dead. The TTL is expiry rather than " +
			"permission precisely because somebody has to write the rollback record, and nobody " +
			"in this sweep ever did")
	}
	if c.ResolveWaits == 0 {
		add("no resolver ever left a live owner alone. A sweep that expired everything it " +
			"met has never exercised the verdict that keeps cleanup from breaking atomicity")
	}
	// The resolution-authority oracle's own non-vacuity, and it is a different
	// question from ResolveDeclaredDead.
	//
	// That counter is the NODE's: how often the apply path took the
	// declare-dead branch. This one is the ORACLE's: how many rolled-back
	// records in the final state it attributed to a resolver rather than to a
	// coordinator's own abort. An oracle that judged nothing reports a silence
	// that means only that resolution never fired, and the whole reason this
	// oracle exists is that M62's class is invisible to everything else -- so a
	// vacuous green here is a vacuous green about a class that had no sweep
	// instrument at all until this one. Measured 64 to 72 per 50 seeds across
	// 200 seeds of the clean tree (DESIGN-A6 section 40).
	if c.ResolverDeclarations == 0 {
		add("resolution-only-breaks-expired-locks judged NOTHING: not one rolled-back record " +
			"in the whole sweep was attributed to a resolver's declaration, so its silence is " +
			"about an empty set and the symmetric-apply class it covers is unwatched")
	}
	if c.ResolveAlreadyDecided == 0 {
		add("no resolver ever found a decision already made, so the make-it-exist rule -- " +
			"the whole safety argument for concurrent resolvers -- was never exercised")
	}
	if c.WriteConflicts == 0 {
		add("no prewrite was ever refused for a commit newer than its snapshot. " +
			"First-committer-wins never fired, so snapshot isolation's write rule is unevidenced")
	}
	if c.PrewriteBlocked == 0 {
		add("no prewrite ever met a live lock, so two transactions never contended for one " +
			"key -- which is the only condition under which any of this machinery matters")
	}
	// # BUG-022's evidence, both halves, because they fail independently
	//
	// The mark is staged by the apply path and consulted by the prewrite. A tree
	// with only the first stages marks nobody reads; a tree with only the second
	// reads marks nobody stages. Neither shows up in a violation count, and one
	// number cannot tell them apart -- so there are two.
	if c.ReadMarks == 0 {
		add("no read mark was staged across the whole sweep, so BUG-022's guard had nothing " +
			"to consult and every prewrite in this run passed a check that was never true")
	}
	if c.ReadConflicts == 0 {
		add("no prewrite was ever refused because somebody had already been answered a read " +
			"of the key above its snapshot. That is the interleaving BUG-022 lives in, and a " +
			"sweep that never reaches it says nothing about the fix")
	}
	if c.TxnAborted == 0 {
		add("no transaction ever aborted after losing a race; the explicit-abort path is " +
			"unexercised and every rollback in this sweep came from a dead coordinator")
	}
	// # BUG-019's evidence
	//
	// A commit or a rollback that found somebody else's lock and left it alone.
	// Every one of these is a lock the pre-fix code would have stolen, orphaning
	// a committed version nobody could ever see. Zero means the schedule never
	// reached the bug, and the fix is unevidenced.
	if c.ForeignLocksKept == 0 {
		add("no commit or rollback ever found a lock belonging to another transaction. " +
			"BUG-019 is the defect where it took that lock anyway, and a sweep that never " +
			"reaches the condition says nothing about the fix")
	}
	// # A6's uncertainty machinery must be REACHED, not merely implemented
	//
	// Unit-green and sweep-exercised are different claims, and the second is the
	// one a phase can rest on.
	// # The ledger's account of restarts must match the coordinator's
	//
	// Not a redundancy. `TxnRecord.StartTS` is what an investigation places a
	// transaction in time by, and a restart MOVES it. When nothing recorded the
	// move, the ledger held an abandoned timestamp and `Restarts` read zero
	// however many restarts there had been -- which is how a confidently wrong
	// lost-update finding got written down. The two counts are kept apart so the
	// day the recording path stops being called, this says so.
	if c.LedgerRestarts != c.UncertaintyRestarts {
		add(fmt.Sprintf("the coordinator restarted %d transactions and the ledger was told about %d. "+
			"Every transaction the ledger did not hear about is one whose recorded start "+
			"timestamp is a value the system abandoned, and reasoning from it places the "+
			"transaction somewhere it never was", c.UncertaintyRestarts, c.LedgerRestarts))
	}
	if c.UncertaintyRestarts == 0 {
		add("no read ever restarted above an uncertain commit across the whole sweep. The " +
			"uncertainty interval is A6's answer to bounded clock skew and this sweep never " +
			"executed it, which is the vacuous-green class with a headline claim attached")
	}
	// # snapshot-isolation's stability check must have COMPARED something
	//
	// The same (key, timestamp) pair is almost never asked twice by accident,
	// so without the deliberate second pass the property runs over an empty set
	// and reports green. That is the vacuous-green class, and this is the
	// assertion that keeps it from recurring here.
	if c.SnapshotsCompared == 0 {
		add("snapshot-isolation compared no answer against an earlier answer to the same " +
			"question, so its stability property -- the one that catches a transaction committing " +
			"into the past -- asserted nothing at all across the whole sweep")
	}

	// # The conservation evidence itself
	if c.AuditsComplete == 0 {
		add("no audit ever read every account at one timestamp, so bank-conservation " +
			"checked nothing at all: the oracle iterates a list that is empty")
	}
	if c.AuditsLocked == 0 {
		add("no audit ever met a lock, so every conservation check in this sweep was taken " +
			"over a quiet instant -- which is not the instant the property is interesting at")
	}
	// # Bidirectional: the identity Percolator assumes
	//
	// A transaction record is addressed by (primary, start timestamp). Two
	// transactions sharing that pair would share a record and the second would
	// silently adopt the first's fate. Percolator is safe because a TSO issues
	// start timestamps; a per-node HLC does not guarantee it, so the assumption
	// is asserted rather than assumed. DESIGN-A6 section 15 carries the entry.
	if c.IdentityCollisions != 0 {
		add(fmt.Sprintf("%d transactions shared a (primary, start timestamp) with an earlier one. "+
			"That pair is the transaction record's address, so the second transaction's "+
			"decision would be refused as already made and it would adopt the first's fate. "+
			"The fix is the IDENTITY -- a transaction id in the record key, or the TSO fallback "+
			"Amendment A6 pre-authorises -- and never this assertion", c.IdentityCollisions))
	}
	// # BUG-021's SECOND half, and BUG-024's, which the exit run collected and
	// # never consulted
	//
	// `IdentityCollisions` above is one half of D-A6-12: two nodes must not mint
	// the same start timestamp. The other half is that a RESTART mints its own
	// rather than adopting `RestartAt`, which carries the tag of whoever minted
	// the commit it restarted on -- and `ForeignTagStarts` is the counter for it.
	// It was plumbed through the census and through every shard and asserted
	// nowhere, which is §22.6b's two-halves rule in the assertion dimension: the
	// decision had two halves and the exit run watched one.
	//
	// All three are safe to add against the signed run rather than by argument:
	// its recorded shard censuses carry ForeignTagStarts=0, StaleRestarts=0 and
	// StaleIncarnation=5195 across 25,000 seeds.
	// # D-A7-6's two propositions, and the non-vacuity that makes them mean
	// # something
	//
	// Ansh, ruling A: *assert what A makes true rather than trusting it. No
	// committed entry with empty Data and a zero ID ever reaches a state-machine
	// arm, and no such entry ever answers a client. Both are checkable, both are
	// cheap, and A's correctness is precisely those two propositions.*
	//
	// The non-vacuity comes first deliberately. Two zeros over a sweep that never
	// produced a no-op are a green over nothing, which is this register's
	// commonest entry -- and the term-start no-op is one per election, so the
	// count is robustly non-zero the moment A7's change is in the tree.
	// A7's non-vacuity. Only asserted when the sweep actually ran the read-index
	// path: under D-A7-4 both paths exist for the phase, so a sweep on the
	// replicated path legitimately serves none, and demanding otherwise would
	// fail a run for being the other half of the comparison.
	// Ruling 2: followers serve reads, and the exercise must be NON-VACUOUS.
	// The absence of exactly this count is why D-A7-2 was implemented and
	// exercised by nothing for the length of the phase.
	if c.ReadIndexRuns > 0 && c.FollowerReads == 0 {
		add("read index ran and not one read was answered by a NON-LEADER. D-A7-2 is in " +
			"CLAUDE.md's scope for this phase and ruling 2 required the exercise to be " +
			"non-vacuous: a sweep in which every read was served by a leader has not tested " +
			"follower reads at all, and the absence of this assertion is why that went " +
			"unnoticed once already")
	}
	if c.ReadIndexRuns > 0 && c.ReadsServed == 0 {
		add("the sweep ran with read index ON and not one read was served off the log. " +
			"Every staleness assertion about the read-index path is then green over a path " +
			"nothing took, which is this register's commonest entry (DESIGN-A7 section 8.2)")
	}
	// BUG-026's fix, with the number that says it ran.
	//
	// The window is arrival-to-answer: a read routed to the range that owned its
	// key, then answered after that range split the key away. A sweep with
	// splits in which this never fires has not opened the window, so every
	// assertion about the fix is green over a path nothing took -- and the fix's
	// own defect, BUG-026, was invisible for exactly as long as nothing counted
	// this. Measured on the clean tree: 9 across 240 seeds, about one seed in
	// twenty-seven, so an aggregate of 25,000 that reads zero has not got quiet,
	// it has got broken.
	if c.ReadIndexRuns > 0 && c.SplitsApplied > 0 && c.ReadsOutOfExtent == 0 {
		add("splits were applied and read index ran, and not one read-index read was ever " +
			"declined for naming a key its range had split away. That window is BUG-026's, " +
			"it opens on about one seed in twenty-seven, and a sweep this size that never " +
			"opened it is not exercising the fix -- it is reporting green over the path the " +
			"defect lived on")
	}
	if c.NoOpsApplied == 0 {
		add("no term-start no-op was ever applied across the whole sweep. A7 appends " +
			"one per election, so a sweep with elections and no no-ops means the entry " +
			"is not being produced -- and the two assertions below are then green over " +
			"a property nothing exercised (DESIGN-A7 section 3a.2)")
	}
	if c.NoOpReachedArm != 0 {
		add(fmt.Sprintf("%d dataless entries matched a state-machine arm. The apply "+
			"switch's arms are isTxnCommand, isSplitCommand and len(e.Data) > 0 with NO "+
			"default, so a term-start no-op matches nothing by construction. This firing "+
			"means the construction stopped being true and the no-op is mutating the "+
			"state machine (M74)", c.NoOpReachedArm))
	}
	if c.NoOpAnswered != 0 {
		add(fmt.Sprintf("%d dataless, zero-identity entries completed a client operation. "+
			"answerAt returns on e.ID.Zero(), and that guard is the only thing between a "+
			"raft-internal entry and somebody's in-flight request. This firing means a "+
			"client was told its request applied when what applied was raft's own no-op "+
			"(M75)", c.NoOpAnswered))
	}

	if c.ForeignTagStarts != 0 {
		add(fmt.Sprintf("%d start timestamps carried the tag of a node other than the one asked "+
			"for them. A restart that adopts RestartAt instead of minting above it hands the "+
			"transaction another node's identity, which is BUG-021 one level out", c.ForeignTagStarts))
	}
	if c.StaleRestarts != 0 {
		add(fmt.Sprintf("%d restarts did not clear the commit they restarted on, so the next read "+
			"meets the same uncertainty and the transaction makes no progress by restarting",
			c.StaleRestarts))
	}
	// Non-vacuity, not a zero-assertion: BUG-024's guard rejects a read answer
	// from a dead incarnation, and a sweep in which it never fired never reached
	// the schedule the guard exists for.
	if c.StaleIncarnation == 0 {
		add("no read answer from a pre-restart incarnation was ever rejected across the whole " +
			"sweep. That guard is BUG-024's fix and a sweep that never reaches it says nothing " +
			"about it")
	}

	// A read this workload cannot parse is a read answered with another key's
	// value. Asserted at zero, and it has never been nonzero.
	if c.UnparseableReads != 0 {
		add(fmt.Sprintf("%d reads returned a value this workload never wrote, which means a read was "+
			"answered from a key it did not name", c.UnparseableReads))
	}

	// 3. Elections must be observed CONTENDING, not merely completing. A run
	//    where the leader is never challenged proves nothing, and a mix that
	//    produces one is a mix that needs fixing.
	if c.ElectionsWon == 0 {
		add("no election was won across the whole sweep; every client operation went unanswered " +
			"and the linearizability checker reported green over a history of unknowns")
	}
	if c.SplitVotes == 0 {
		add("no split vote across the whole sweep: the schedule mix never made two nodes " +
			"campaign in the same term, so the contended path is untested")
	}
	if got := c.SeedsWithContention * 100 / c.Seeds; got < 10 {
		add(fmt.Sprintf("only %d%% of seeds saw contention (more than one election won, or a split vote); "+
			"the mix is too quiet to be evidence about a consensus protocol", got))
	}
	// A cluster that never elects anybody on a large fraction of seeds is a
	// liveness smell that would hide safety coverage behind unanswered clients.
	if got := c.SeedsWithNoLeader * 100 / c.Seeds; got > 20 {
		add(fmt.Sprintf("%d%% of seeds never elected a leader; those seeds check nothing", got))
	}
	return out
}

// assertExitCriteria reports every failure in exitCriteriaFailures.
func assertExitCriteria(t *testing.T, c hunt.RaftCensus) {
	t.Helper()
	for _, f := range exitCriteriaFailures(c) {
		t.Error(f)
	}
}
