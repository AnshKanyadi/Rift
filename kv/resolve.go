package kv

import (
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
)

// Resolution is what a resolver decided to do about somebody else's lock.
type Resolution uint8

const (
	// ResolveWait: the owner is alive by its own TTL. Leaving it alone is the
	// only safe answer, and it is an answer rather than a failure.
	ResolveWait Resolution = iota

	// ResolveForward: the owner committed and this key's commit record was
	// never written. The resolver writes it, at the owner's commit timestamp.
	ResolveForward

	// ResolveBack: the owner is rolled back, or was just declared so.
	ResolveBack
)

func (r Resolution) String() string {
	switch r {
	case ResolveForward:
		return "roll-forward"
	case ResolveBack:
		return "roll-back"
	default:
		return "wait"
	}
}

// ResolveLock decides what to do about a lock, from the transaction record its
// primary key names.
//
// # The protocol, and the one rule that makes it race-safe
//
//	the primary record says COMMITTED at C  -> roll forward: write this key's
//	                                           commit record at C
//	the primary record says ROLLED BACK     -> roll back: drop the lock and the
//	                                           uncommitted version
//	no record, TTL not expired at readTS    -> wait
//	no record, TTL expired at readTS        -> declare the owner dead by writing
//	                                           a rollback record on its PRIMARY,
//	                                           then roll this key back
//
// **A resolver may only ever make the primary's record EXIST.** Never delete
// one, never turn a rollback into a commit. That single restriction is the whole
// safety argument: two resolvers reach the same verdict because the first write
// wins and the second reads it, and a coordinator that wakes up to find itself
// tombstoned aborts rather than arguing. It is enforced by PutTxnInto, which
// refuses to overwrite.
//
// **The TTL is expiry, not permission.** A resolver that finds an expired TTL
// does not conclude the owner is dead -- it MAKES the owner dead, by committing
// a rollback record through the log. There is no window in which both outcomes
// are believed, because both go through one range's log and one of them is
// first.
//
// # Why readTS and not a clock
//
// The deadline is compared against the RESOLVER'S READ TIMESTAMP. Two replicas
// applying the same resolution at the same log position must reach the same
// verdict, and "my clock now" is not a value they share. That is DESIGN-A6 §8's
// first row, and it is A4's log-position class in the timestamp dimension.
//
// primaryOnThisRange says whether this store holds the primary's record. When it
// does not, the caller routes the primary half elsewhere and comes back; the
// decision itself is unchanged, which is what keeps a cross-range resolution from
// being a different protocol.
func (s *Store) ResolveLock(key []byte, l Lock, readTS hlc.Timestamp, primaryOnThisRange bool) (Resolution, hlc.Timestamp, error) {
	if !primaryOnThisRange {
		// The caller must fetch the record from wherever the primary lives now.
		// Re-routing by KEY is what makes a split harmless here: the lock names
		// the primary key, not the range it was on when the lock was written
		// (BUG-011's class, D-A6-1).
		return ResolveWait, hlc.Timestamp{}, nil
	}
	rec, ok, err := s.Txn(l.Primary, l.StartTS)
	if err != nil {
		return ResolveWait, hlc.Timestamp{}, err
	}
	switch {
	case ok && rec.Status == TxnCommitted:
		return ResolveForward, rec.CommitTS, nil
	case ok:
		return ResolveBack, hlc.Timestamp{}, nil
	case readTS.LessEq(l.Deadline):
		return ResolveWait, hlc.Timestamp{}, nil
	default:
		return ResolveBack, hlc.Timestamp{}, nil
	}
}

// ApplyResolutionInto stages the effect of a resolution on ONE key.
//
// The primary's rollback record -- the act of declaring the owner dead -- is the
// caller's, because it belongs on the primary's range and this store may not be
// it. Splitting it out keeps the two writes honest about which range they land
// on, and keeps this function a pure function of its arguments.
func (s *Store) ApplyResolutionInto(b *engine.Batch, key []byte, l Lock, r Resolution, commitTS hlc.Timestamp) error {
	switch r {
	case ResolveForward:
		s.rollForwards++
		return s.CommitInto(b, key, l.StartTS, commitTS)
	case ResolveBack:
		s.rollBacks++
		return s.RollbackInto(b, key, l.StartTS)
	default:
		return nil
	}
}

// RollForwards and RollBacks are the recovery path's non-vacuity evidence.
//
// Both are asserted nonzero in the exit run, and that is exit criterion 1: the
// interesting failures are all in the recovery path of a coordinator that died
// mid-commit, so a sweep that only ever ran the happy path is green about the
// half of the protocol nobody wrote down carefully.
func (s *Store) RollForwards() int { return s.rollForwards }
func (s *Store) RollBacks() int    { return s.rollBacks }
