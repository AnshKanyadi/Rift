package kv_test

import (
	"time"

	"errors"
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
)

// noSkew is a zero uncertainty window: these tests are about the commit
// protocol, and the interval has its own.
var noSkew = hlc.Timestamp{}

func apply(t *testing.T, db interface {
	Apply(*engine.Batch, bool) (engine.SeqNum, error)
}, b *engine.Batch) {
	t.Helper()
	if _, err := db.Apply(b, false); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

// prewrite stages and applies a prewrite in one step, for the tests that are
// about what happens afterwards.
func prewrite(t *testing.T, s *kv.Store, db dbLike, key, primary string, start hlc.Timestamp, ttl hlc.Timestamp, val string) error {
	t.Helper()
	b := engine.NewBatch()
	err := s.PrewriteInto(b, []byte(key), kv.Lock{
		Primary: []byte(primary), StartTS: start, Deadline: ttl,
	}, []byte(val))
	if err != nil {
		return err
	}
	apply(t, db, b)
	return nil
}

type dbLike interface {
	Apply(*engine.Batch, bool) (engine.SeqNum, error)
}

// TestACommittedTransactionIsVisibleAndAPendingOneIsNot is the commit point,
// stated as a test: a transaction is committed if and only if the write record
// for its primary exists.
func TestACommittedTransactionIsVisibleAndAPendingOneIsNot(t *testing.T) {
	s, db := newStore(t)
	start, commit := ts(100, 0), ts(200, 0)

	if err := prewrite(t, s, db, "k", "k", start, ts(1000, 0), "v"); err != nil {
		t.Fatalf("prewrite: %v", err)
	}

	// Prewritten and not committed: a read at any timestamp at or above the
	// start finds a LOCK, not a value and not an absence. "The older value"
	// would be a guess about a decision nobody has made.
	if _, _, err := s.GetTxn([]byte("k"), ts(150, 0), noSkew); !errors.Is(err, kv.ErrKeyIsLocked) {
		t.Fatalf("a read over a live lock returned %v, want ErrKeyIsLocked", err)
	}
	// Below the lock's start it is simply absent, which is a fact about history
	// and not about the lock.
	if _, ok, err := s.GetTxn([]byte("k"), ts(50, 0), noSkew); err != nil || ok {
		t.Fatalf("a read before the transaction started saw something: ok=%v err=%v", ok, err)
	}

	b := engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{
		Status: kv.TxnCommitted, StartTS: start, CommitTS: commit,
	}); err != nil {
		t.Fatalf("put txn: %v", err)
	}
	if err := s.CommitInto(b, []byte("k"), start, commit); err != nil {
		t.Fatalf("commit: %v", err)
	}
	apply(t, db, b)

	for _, c := range []struct {
		at   hlc.Timestamp
		want string
		ok   bool
	}{
		{ts(199, 0), "", false}, // before the commit timestamp: not yet visible
		{ts(200, 0), "v", true}, // exactly at it
		{ts(999, 0), "v", true},
	} {
		got, ok, err := s.GetTxn([]byte("k"), c.at, noSkew)
		if err != nil || ok != c.ok || string(got) != c.want {
			t.Errorf("read at %s = (%q,%v,%v), want (%q,%v)", c.at, got, ok, err, c.want, c.ok)
		}
	}
}

// TestARolledBackTransactionIsInvisibleForever: the tombstone makes nothing
// visible, and a read walks past it to whatever is older.
func TestARolledBackTransactionIsInvisibleForever(t *testing.T) {
	s, db := newStore(t)

	// An older committed value, so the test can tell "skipped the tombstone"
	// from "found nothing".
	b := engine.NewBatch()
	if err := s.PrewriteInto(b, []byte("k"), kv.Lock{Primary: []byte("k"), StartTS: ts(50, 0), Deadline: ts(1000, 0)}, []byte("old")); err != nil {
		t.Fatalf("prewrite old: %v", err)
	}
	apply(t, db, b)
	b = engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{Status: kv.TxnCommitted, StartTS: ts(50, 0), CommitTS: ts(60, 0)}); err != nil {
		t.Fatalf("put txn old: %v", err)
	}
	if err := s.CommitInto(b, []byte("k"), ts(50, 0), ts(60, 0)); err != nil {
		t.Fatalf("commit old: %v", err)
	}
	apply(t, db, b)

	// A second transaction prewrites and is rolled back.
	if err := prewrite(t, s, db, "k", "k", ts(100, 0), ts(1000, 0), "doomed"); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
	b = engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{Status: kv.TxnRolledBack, StartTS: ts(100, 0)}); err != nil {
		t.Fatalf("put rollback: %v", err)
	}
	if err := s.RollbackInto(b, []byte("k"), ts(100, 0)); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	apply(t, db, b)

	got, ok, err := s.GetTxn([]byte("k"), ts(500, 0), noSkew)
	if err != nil || !ok || string(got) != "old" {
		t.Fatalf("after a rollback the read returned (%q,%v,%v); it must skip the tombstone and "+
			"find the older committed value", got, ok, err)
	}
}

// TestAResolverMayOnlyMakeTheRecordExist is the hard rule of D-A6-4, and it is
// the whole safety argument for concurrent resolution.
func TestAResolverMayOnlyMakeTheRecordExist(t *testing.T) {
	s, db := newStore(t)
	b := engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("p"), kv.TxnRecord{Status: kv.TxnRolledBack, StartTS: ts(100, 0)}); err != nil {
		t.Fatalf("first record: %v", err)
	}
	apply(t, db, b)

	// A coordinator waking up late tries to commit itself. It must be refused,
	// not silently ignored: it has lost a race and has to abort.
	b = engine.NewBatch()
	err := s.PutTxnInto(b, []byte("p"), kv.TxnRecord{
		Status: kv.TxnCommitted, StartTS: ts(100, 0), CommitTS: ts(200, 0),
	})
	if !errors.Is(err, kv.ErrTxnAlreadyDecided) {
		t.Fatalf("a coordinator overwrote a rollback tombstone with a commit (err=%v). Two "+
			"observers would then disagree about whether the transaction happened", err)
	}

	// And a second resolver reaching the same conclusion is refused too, which
	// is what makes resolution idempotent rather than merely repeatable.
	b = engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("p"), kv.TxnRecord{Status: kv.TxnRolledBack, StartTS: ts(100, 0)}); !errors.Is(err, kv.ErrTxnAlreadyDecided) {
		t.Errorf("a duplicate rollback was accepted: %v", err)
	}
}

// TestResolutionRollsForwardAndBack exercises both directions, which exit
// criterion 1 requires to be exercised rather than merely reachable.
func TestResolutionRollsForwardAndBack(t *testing.T) {
	t.Run("forward", func(t *testing.T) {
		s, db := newStore(t)
		start, commit := ts(100, 0), ts(150, 0)
		// The primary committed; the secondary's commit record was never
		// written -- the coordinator died between the two.
		if err := prewrite(t, s, db, "sec", "pri", start, ts(1000, 0), "v"); err != nil {
			t.Fatalf("prewrite: %v", err)
		}
		b := engine.NewBatch()
		if err := s.PutTxnInto(b, []byte("pri"), kv.TxnRecord{Status: kv.TxnCommitted, StartTS: start, CommitTS: commit}); err != nil {
			t.Fatalf("put txn: %v", err)
		}
		apply(t, db, b)

		l, ok, err := s.Lock([]byte("sec"))
		if err != nil || !ok {
			t.Fatalf("lock: %v %v", ok, err)
		}
		r, c, err := s.ResolveLock([]byte("sec"), l, ts(200, 0), true)
		if err != nil || r != kv.ResolveForward || c != commit {
			t.Fatalf("resolve = (%s,%s,%v), want roll-forward at %s", r, c, err, commit)
		}
		b = engine.NewBatch()
		if err := s.ApplyResolutionInto(b, []byte("sec"), l, r, c); err != nil {
			t.Fatalf("apply resolution: %v", err)
		}
		apply(t, db, b)

		got, ok, err := s.GetTxn([]byte("sec"), ts(200, 0), noSkew)
		if err != nil || !ok || string(got) != "v" {
			t.Fatalf("after roll-forward the secondary reads (%q,%v,%v), want v", got, ok, err)
		}
		if s.RollForwards() != 1 {
			t.Errorf("roll-forwards not counted: %d", s.RollForwards())
		}
	})

	t.Run("back on an expired ttl", func(t *testing.T) {
		s, db := newStore(t)
		// No primary record at all, and the deadline is behind the resolver's
		// read timestamp: the resolver declares the owner dead.
		if err := prewrite(t, s, db, "sec", "pri", ts(100, 0), ts(150, 0), "v"); err != nil {
			t.Fatalf("prewrite: %v", err)
		}
		l, _, _ := s.Lock([]byte("sec"))

		// Before the deadline the only safe answer is to wait.
		if r, _, err := s.ResolveLock([]byte("sec"), l, ts(120, 0), true); err != nil || r != kv.ResolveWait {
			t.Fatalf("resolve before the deadline = %s (%v), want wait", r, err)
		}
		// After it, roll back.
		r, _, err := s.ResolveLock([]byte("sec"), l, ts(200, 0), true)
		if err != nil || r != kv.ResolveBack {
			t.Fatalf("resolve after the deadline = %s (%v), want roll-back", r, err)
		}

		b := engine.NewBatch()
		if err := s.PutTxnInto(b, []byte("pri"), kv.TxnRecord{Status: kv.TxnRolledBack, StartTS: l.StartTS}); err != nil {
			t.Fatalf("declare dead: %v", err)
		}
		if err := s.ApplyResolutionInto(b, []byte("sec"), l, r, hlc.Timestamp{}); err != nil {
			t.Fatalf("apply resolution: %v", err)
		}
		apply(t, db, b)

		if _, ok, err := s.GetTxn([]byte("sec"), ts(300, 0), noSkew); err != nil || ok {
			t.Fatalf("after roll-back the secondary still reads something: ok=%v err=%v", ok, err)
		}
		if s.RollBacks() != 1 {
			t.Errorf("roll-backs not counted: %d", s.RollBacks())
		}
	})
}

// TestTheDeadlineIsComparedAgainstTheReadTimestamp is the covering test for
// M57-ttl-compared-against-the-clock.
//
// Two replicas resolving the same lock at the same log position must reach the
// same verdict. A deadline compared against "now" makes the verdict a function
// of when each replica got round to applying, so one rolls a transaction back
// and the other does not -- and the disagreement surfaces as a client error on
// one node and an answer on another.
func TestTheDeadlineIsComparedAgainstTheReadTimestamp(t *testing.T) {
	s, db := newStore(t)
	if err := prewrite(t, s, db, "sec", "pri", ts(100, 0), ts(150, 0), "v"); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
	l, _, _ := s.Lock([]byte("sec"))

	// The same lock, two read timestamps either side of the deadline, resolved
	// in the "wrong" order in wall-clock terms. Each answer depends only on its
	// own timestamp.
	if r, _, _ := s.ResolveLock([]byte("sec"), l, ts(200, 0), true); r != kv.ResolveBack {
		t.Errorf("at 200 (past the 150 deadline) = %s, want roll-back", r)
	}
	if r, _, _ := s.ResolveLock([]byte("sec"), l, ts(120, 0), true); r != kv.ResolveWait {
		t.Errorf("at 120 (before the deadline) = %s, want wait. The verdict moved with something "+
			"other than the timestamp it was asked about", r)
	}
}

// TestWriteConflictAbortsThePrewrite: a newer commit inside our snapshot's
// lifetime means committing would make our own read of the key stale.
func TestWriteConflictAbortsThePrewrite(t *testing.T) {
	s, db := newStore(t)
	b := engine.NewBatch()
	if err := s.PrewriteInto(b, []byte("k"), kv.Lock{Primary: []byte("k"), StartTS: ts(100, 0), Deadline: ts(1000, 0)}, []byte("a")); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
	apply(t, db, b)
	b = engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{Status: kv.TxnCommitted, StartTS: ts(100, 0), CommitTS: ts(300, 0)}); err != nil {
		t.Fatalf("put txn: %v", err)
	}
	if err := s.CommitInto(b, []byte("k"), ts(100, 0), ts(300, 0)); err != nil {
		t.Fatalf("commit: %v", err)
	}
	apply(t, db, b)

	// A transaction that started at 200 -- before that commit landed at 300 --
	// may not write this key.
	b = engine.NewBatch()
	err := s.PrewriteInto(b, []byte("k"), kv.Lock{Primary: []byte("k"), StartTS: ts(200, 0), Deadline: ts(1000, 0)}, []byte("b"))
	if !errors.Is(err, kv.ErrWriteConflict) {
		t.Fatalf("a prewrite over a newer commit was accepted: %v", err)
	}

	// One that started after it may.
	b = engine.NewBatch()
	if err := s.PrewriteInto(b, []byte("k"), kv.Lock{Primary: []byte("k"), StartTS: ts(400, 0), Deadline: ts(1000, 0)}, []byte("c")); err != nil {
		t.Fatalf("a prewrite after the commit was refused: %v", err)
	}
}

// TestAPrewriteOverALockIsRefused: two live transactions may not stack on one
// key. Whether the lock can be broken is the resolver's question.
func TestAPrewriteOverALockIsRefused(t *testing.T) {
	s, db := newStore(t)
	if err := prewrite(t, s, db, "k", "k", ts(100, 0), ts(1000, 0), "a"); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
	b := engine.NewBatch()
	if err := s.PrewriteInto(b, []byte("k"), kv.Lock{Primary: []byte("k"), StartTS: ts(200, 0), Deadline: ts(1000, 0)}, []byte("b")); !errors.Is(err, kv.ErrKeyIsLocked) {
		t.Fatalf("a second prewrite stacked on a live lock: %v", err)
	}
}

// TestAnUncertainCommitRestartsAboveIt is the covering test for
// M58-uncertainty-restarts-at-the-wrong-timestamp, and it pins the sharp edge
// CLAUDE.md names: the restart must bump past the OBSERVED VALUE's timestamp.
func TestAnUncertainCommitRestartsAboveIt(t *testing.T) {
	s, db := newStore(t)
	maxOffset := 500 * time.Nanosecond

	b := engine.NewBatch()
	if err := s.PrewriteInto(b, []byte("k"), kv.Lock{Primary: []byte("k"), StartTS: ts(1000, 0), Deadline: ts(9000, 0)}, []byte("v")); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
	apply(t, db, b)
	b = engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{Status: kv.TxnCommitted, StartTS: ts(1000, 0), CommitTS: ts(1200, 0)}); err != nil {
		t.Fatalf("put txn: %v", err)
	}
	if err := s.CommitInto(b, []byte("k"), ts(1000, 0), ts(1200, 0)); err != nil {
		t.Fatalf("commit: %v", err)
	}
	apply(t, db, b)

	// A read at 1000 with a 500 window: the commit at 1200 is inside (1000,1500].
	_, _, err := s.GetTxn([]byte("k"), ts(1000, 0), kv.UncertaintyCeiling(ts(1000, 0), maxOffset))
	var ue *kv.UncertaintyError
	if !errors.As(err, &ue) {
		t.Fatalf("a commit inside the uncertainty interval did not restart the read: %v", err)
	}
	if want := ts(1200, 0).Next(); ue.RestartAt() != want {
		t.Errorf("restart at %s, want %s -- strictly above the OBSERVED value, not at read+maxOffset "+
			"and not at now", ue.RestartAt(), want)
	}

	// Outside the window it is simply invisible, which is the ordinary case and
	// must not restart anything.
	if _, ok, err := s.GetTxn([]byte("k"), ts(600, 0), kv.UncertaintyCeiling(ts(600, 0), maxOffset)); err != nil || ok {
		t.Errorf("a read well below the commit restarted or saw it: ok=%v err=%v", ok, err)
	}
	// At or above the commit it is just visible.
	if got, ok, err := s.GetTxn([]byte("k"), ts(1200, 0), kv.UncertaintyCeiling(ts(1200, 0), maxOffset)); err != nil || !ok || string(got) != "v" {
		t.Errorf("read at the commit timestamp = (%q,%v,%v)", got, ok, err)
	}
}

// TestARollbackTombstoneIsNotUncertain: a tombstone made nothing visible, so
// there is nothing a reader could have missed and nothing to restart for.
func TestARollbackTombstoneIsNotUncertain(t *testing.T) {
	s, db := newStore(t)
	maxOffset := 500 * time.Nanosecond

	if err := prewrite(t, s, db, "k", "k", ts(1200, 0), ts(9000, 0), "doomed"); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
	b := engine.NewBatch()
	if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{Status: kv.TxnRolledBack, StartTS: ts(1200, 0)}); err != nil {
		t.Fatalf("put rollback: %v", err)
	}
	if err := s.RollbackInto(b, []byte("k"), ts(1200, 0)); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	apply(t, db, b)

	if _, ok, err := s.GetTxn([]byte("k"), ts(1000, 0), kv.UncertaintyCeiling(ts(1000, 0), maxOffset)); err != nil || ok {
		t.Fatalf("a rollback tombstone inside the uncertainty window disturbed a read: ok=%v err=%v", ok, err)
	}
}

// TestTheUncertaintyCeilingDoesNotMOVEWhenAReadRestarts is D-A6-7's termination
// argument, as a test.
//
// # What it would look like if the ceiling were recomputed
//
// A transaction restarts above an uncertain commit. If the new read recomputed
// its ceiling from its NEW timestamp, the top of the window would rise by
// exactly as much as the bottom did, and a key being written steadily would
// restart the transaction forever. With the ceiling fixed, each restart strictly
// shrinks the interval, so a transaction can restart at most as many times as
// there are commits inside its first window.
//
// The test asserts the shrinkage directly: after restarting above the first
// uncertain commit, the second commit is still inside the ORIGINAL ceiling and
// restarts once more, and the third read -- now above both -- proceeds. A
// recomputed ceiling would keep pulling in commits that a fixed one has left
// behind, and the loop below would never reach its answer.
func TestTheUncertaintyCeilingDoesNotMoveWhenAReadRestarts(t *testing.T) {
	s, db := newStore(t)
	maxOffset := 500 * time.Nanosecond
	read := ts(1000, 0)
	ceiling := kv.UncertaintyCeiling(read, maxOffset)

	// Two commits inside (1000, 1500], and one above it.
	for _, c := range []struct{ start, commit hlc.Timestamp }{
		{ts(1100, 0), ts(1200, 0)},
		{ts(1300, 0), ts(1400, 0)},
		{ts(1600, 0), ts(1700, 0)},
	} {
		if err := prewrite(t, s, db, "k", "k", c.start, ts(9000, 0), "v"); err != nil {
			t.Fatalf("prewrite at %s: %v", c.start, err)
		}
		b := engine.NewBatch()
		if err := s.PutTxnInto(b, []byte("k"), kv.TxnRecord{
			Status: kv.TxnCommitted, StartTS: c.start, CommitTS: c.commit}); err != nil {
			t.Fatalf("put txn at %s: %v", c.start, err)
		}
		if err := s.CommitInto(b, []byte("k"), c.start, c.commit); err != nil {
			t.Fatalf("commit at %s: %v", c.commit, err)
		}
		apply(t, db, b)
	}

	restarts := 0
	at := read
	for range 8 {
		_, _, err := s.GetTxn([]byte("k"), at, ceiling)
		var ue *kv.UncertaintyError
		if !errors.As(err, &ue) {
			break
		}
		restarts++
		at = ue.RestartAt()
		if !ceiling.Less(at) && !at.LessEq(ceiling) {
			t.Fatalf("restart timestamp %s is unordered against the ceiling", at)
		}
	}
	// ONE restart, not two: the scan walks commit records newest-first, so it
	// meets 1400 before 1200 and restarts above the newest uncertain commit
	// rather than once per commit. That is the shrinkage doing its work in a
	// single step.
	//
	// The counterfactual is the whole point. With a ceiling recomputed from each
	// new read timestamp, the read at 1401 would carry a window of (1401,1901],
	// which pulls in the commit at 1700 that the fixed ceiling had left safely
	// in the future; it would restart to 1701 with a window of (1701,2201], and
	// under a key being written steadily it would never stop.
	if restarts != 1 {
		t.Errorf("restarted %d times, want exactly 1: the newest commit inside the ORIGINAL "+
			"window (1000,1500] is 1400, and restarting above it clears 1200 as well. The commit "+
			"at 1700 is above the ceiling and must never be uncertain", restarts)
	}
	if want := ts(1400, 0).Next(); at != want {
		t.Errorf("settled at %s, want %s: strictly above the NEWEST uncertain commit", at, want)
	}
	// And having restarted past both, the read answers from the newest commit
	// at or below it rather than restarting again.
	if _, ok, err := s.GetTxn([]byte("k"), at, ceiling); err != nil || !ok {
		t.Errorf("after %d restarts the read at %s still did not answer: ok=%v err=%v",
			restarts, at, ok, err)
	}
}

// TestARollbackDoesNotStealSomebodyElsesLock is BUG-019.
//
// A lock is one record per key. "Delete the lock" is therefore ambiguous, and
// the only correct reading is "delete the lock IF IT IS MINE". Both resolution
// paths read it the other way and deleted whatever was there.
//
// The schedule that reaches it needs no fault at all:
//
//  1. T1 prewrites k and holds the lock;
//  2. T2 prewrites k, is refused because the key is locked, and aborts -- which
//     is the correct behaviour and the reason first-committer-wins works;
//  3. T2's abort rolls back its own key, and takes T1's lock with it.
//
// After that T1 is a committed transaction with an orphaned version: nothing
// holds the key, so no reader will ever resolve it, and the value it promised is
// invisible forever. In the bank that is money vanishing, which is how it was
// found.
func TestARollbackDoesNotStealSomebodyElsesLock(t *testing.T) {
	s, db := newStore(t)
	if err := prewrite(t, s, db, "k", "k", ts(100, 0), ts(600, 0), "mine"); err != nil {
		t.Fatalf("first prewrite: %v", err)
	}

	// A second transaction's rollback, at its own start timestamp. Legitimate:
	// it is aborting after being refused, and it must be able to clean up.
	b := engine.NewBatch()
	if err := s.RollbackInto(b, []byte("k"), ts(200, 0)); err != nil {
		t.Fatalf("second rollback: %v", err)
	}
	apply(t, db, b)

	l, ok, err := s.Lock([]byte("k"))
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if !ok {
		t.Fatal("the first transaction's lock is GONE, taken by another transaction's rollback. " +
			"Its version is now orphaned: no lock means no reader will ever resolve it, so a " +
			"transaction that committed has a key nobody can see")
	}
	if l.StartTS != ts(100, 0) {
		t.Errorf("the lock is now held by %s, want the original holder %s", l.StartTS, ts(100, 0))
	}
}

// TestACommitDoesNotStealSomebodyElsesLock is the same defect on the other path.
//
// Worse here, because a commit is a resolver's roll-FORWARD: it can be issued by
// a stranger acting on somebody else's behalf, so the lock it deletes belongs to
// a transaction that is not even a party to the operation.
func TestACommitDoesNotStealSomebodyElsesLock(t *testing.T) {
	s, db := newStore(t)
	if err := prewrite(t, s, db, "k", "k", ts(100, 0), ts(600, 0), "mine"); err != nil {
		t.Fatalf("first prewrite: %v", err)
	}
	b := engine.NewBatch()
	if err := s.CommitInto(b, []byte("k"), ts(200, 0), ts(250, 0)); err != nil {
		t.Fatalf("commit: %v", err)
	}
	apply(t, db, b)

	if _, ok, err := s.Lock([]byte("k")); err != nil || !ok {
		t.Fatalf("committing a DIFFERENT transaction took the lock at %s: ok=%v err=%v",
			ts(100, 0), ok, err)
	}
}
