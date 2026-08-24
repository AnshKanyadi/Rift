package raftcheck_test

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
)

// The induction for resolution-only-breaks-expired-locks.
//
// # Why it is induced here and not on a seed
//
// The oracle exists for M62, whose whole finding is that no sweep detects it:
// 0 of 300, 0 of 100, sweepfail 0 (DESIGN-A6 §35.4). Establishing that this
// oracle SPEAKS therefore cannot wait on a seed search — the sweep measurement
// against M62 is the second half of the evidence and it is worth nothing until
// the first half is nailed down here, by construction, in milliseconds.
//
// The states and logs are built directly. Nothing here runs the system.

// resolveOracle runs the oracle over one built log and one built final state.
func resolveOracle(t *testing.T, rs []raftcheck.ResolveFact, ps []raftcheck.ProposedRollback,
	decided []raftcheck.CommitFact) (string, int) {
	t.Helper()
	l := raftcheck.NewLedger(1)
	// The range has to EXIST in the ledger, because the oracle walks the
	// ledger's ranges to reach their logs -- the same path it takes in a run.
	// The entry's bytes are ignored: the decoder this test injects answers with
	// the facts the case is about, which is what keeps the case about the rule
	// and not about the wire format.
	l.RecordRangeBase(1, provenance.Witness([]byte{}), provenance.Witness([]byte{}))
	l.RecordApplied(1, 0, provenance.Witness([]raft.Entry{{Index: 1, Term: 1}}), clock.Instant(1))
	o := raftcheck.NewResolutionAuthority(l,
		func([]raft.Entry) ([]raftcheck.ResolveFact, []raftcheck.ProposedRollback) {
			return rs, ps
		},
		func() []raftcheck.RecoveredState {
			return []raftcheck.RecoveredState{{
				Range: 1, Start: []byte(""), End: nil, Clock: at(1 << 40), Decided: decided,
			}}
		})
	v := o.Check()
	if v == nil {
		return "", o.Declarations()
	}
	return v.Detail, o.Declarations()
}

// TestAResolverMayNotKillALockThatHasNotExpired is M62's detector, induced.
//
// The lock's deadline is 300 and the resolver judged it at 200 — the lock is
// alive by its own TTL, and the transaction is rolled back anyway with nobody
// having proposed that record. That is a live coordinator killed by a resolver
// with no right to kill it, and it is the one thing no other checker can see:
// aborting a transaction is a legal outcome, so atomicity, snapshot isolation,
// the bank and every replica comparison are all satisfied by it.
func TestAResolverMayNotKillALockThatHasNotExpired(t *testing.T) {
	got, _ := resolveOracle(t,
		[]raftcheck.ResolveFact{{
			Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(200),
		}},
		nil,
		[]raftcheck.CommitFact{{Key: "a00", StartTS: at(100), Rollback: true}},
	)
	if !strings.Contains(got, "unexpired") {
		t.Fatalf("a resolver killed a lock 100 ticks short of its deadline and the oracle "+
			"accepted it; it said %q", got)
	}
}

// TestAResolverAtTheDeadlineExactlyIsRefused pins the boundary the rule names.
//
// D-A6-5 is expiry, not opinion, and the comparison production makes is
// `ExpireAt <= Deadline` means WAIT. So a resolve judged at exactly the deadline
// has not expired anything, and the oracle has to agree at the boundary or the
// two disagree by one tick in the direction that kills live transactions.
func TestAResolverAtTheDeadlineExactlyIsRefused(t *testing.T) {
	got, _ := resolveOracle(t,
		[]raftcheck.ResolveFact{{
			Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(300),
		}},
		nil,
		[]raftcheck.CommitFact{{Key: "a00", StartTS: at(100), Rollback: true}},
	)
	if !strings.Contains(got, "unexpired") {
		t.Fatalf("a resolve judged at exactly the deadline was accepted as an expiry; "+
			"the oracle said %q", got)
	}
}

// TestAnExpiredLockMayBeKilled is the negative half, and it is what stops the
// oracle from being a checker that refuses resolution itself.
func TestAnExpiredLockMayBeKilled(t *testing.T) {
	got, declared := resolveOracle(t,
		[]raftcheck.ResolveFact{{
			Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(301),
		}},
		nil,
		[]raftcheck.CommitFact{{Key: "a00", StartTS: at(100), Rollback: true}},
	)
	if got != "" {
		t.Fatalf("a resolver killed a lock one tick PAST its deadline, which is the "+
			"mechanism working; the oracle said %q", got)
	}
	if declared != 1 {
		t.Fatalf("the declaration was not counted: %d. A count that reads zero on the "+
			"case it exists for is how a green sweep comes to mean nothing", declared)
	}
}

// TestOneAuthorisedResolveIsEnough: several resolvers meet the same lock, and
// only the last one is past the deadline. The record is that one's to write.
func TestOneAuthorisedResolveIsEnough(t *testing.T) {
	got, _ := resolveOracle(t,
		[]raftcheck.ResolveFact{
			{Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(150)},
			{Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(250)},
			{Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(400)},
		},
		nil,
		[]raftcheck.CommitFact{{Key: "a00", StartTS: at(100), Rollback: true}},
	)
	if got != "" {
		t.Fatalf("a rollback with an expired resolve behind it was refused because two "+
			"earlier resolvers had waited; the oracle said %q", got)
	}
}

// TestACoordinatorMayAbandonItself: the record was PROPOSED, so no resolver
// needed permission and none is asked for.
//
// Without this the oracle would fire on every transaction the cluster refused,
// which is the ordinary abort path and not a defect at all.
func TestACoordinatorMayAbandonItself(t *testing.T) {
	got, declared := resolveOracle(t,
		[]raftcheck.ResolveFact{{
			Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(200),
		}},
		[]raftcheck.ProposedRollback{{Primary: "a00", StartTS: at(100)}},
		[]raftcheck.CommitFact{{Key: "a00", StartTS: at(100), Rollback: true}},
	)
	if got != "" {
		t.Fatalf("a coordinator's own abort was reported as a resolver's kill: %q", got)
	}
	if declared != 0 {
		t.Fatalf("a self-proposed rollback was counted as a resolver's declaration (%d), "+
			"which would inflate the non-vacuity witness with cases the oracle never judged",
			declared)
	}
}

// TestACommittedRecordIsNotTheOraclesBusiness: the same transaction, committed.
// Nothing about a commit needs a resolver's permission.
func TestACommittedRecordIsNotTheOraclesBusiness(t *testing.T) {
	got, _ := resolveOracle(t,
		[]raftcheck.ResolveFact{{
			Primary: "a00", StartTS: at(100), Deadline: at(300), ExpireAt: at(200),
		}},
		nil,
		[]raftcheck.CommitFact{{
			Key: "a00", StartTS: at(100), CommitTS: hlc.Timestamp{Wall: at(400).Wall},
		}},
	)
	if got != "" {
		t.Fatalf("a COMMITTED transaction was judged against a resolver's permission: %q", got)
	}
}

// TestARollbackNoCommandAccountsFor is the other arm of the same walk: a
// rolled-back record with neither a proposal nor a resolve behind it.
//
// It is a different defect from M62's — a record no command in the log
// produced — and it is reported differently so a red lane says which one it is.
func TestARollbackNoCommandAccountsFor(t *testing.T) {
	got, _ := resolveOracle(t, nil, nil,
		[]raftcheck.CommitFact{{Key: "a00", StartTS: at(100), Rollback: true}})
	if !strings.Contains(got, "nothing in the log accounts for it") {
		t.Fatalf("a rolled-back record produced by no command was accepted: %q", got)
	}
}
