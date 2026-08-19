package kv

import (
	"errors"
	"fmt"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
)

// ErrBelowGCMark is returned by a read at a timestamp garbage collection has
// already passed.
//
// # The refusal is the whole point of the mark
//
// Collecting versions below a mark is easy. What is hard is what happens to a
// read at a timestamp below it afterwards: the versions that WERE visible there
// are gone, and an implementation that answers anyway returns whatever is left.
// That is an older state that never existed, or a newer one -- and either way
// the answer is a perfectly plausible value, so nothing downstream can tell.
//
// A silently wrong read is worse than a refused one by exactly the margin that
// makes this project worth building. So a read at or below the mark is refused,
// typed, and countable.
var ErrBelowGCMark = errors.New("kv: read at a timestamp below the garbage-collection mark")

// ErrUnsetTimestamp is returned for a read or write at the zero timestamp.
//
// The zero Timestamp means "unset" (see hlc.Timestamp), and a store that
// accepted it would write a version nothing can name and answer reads nobody
// asked for. It is a caller bug, but it is returned rather than panicked: the
// caller here is the apply loop, which decodes timestamps off the wire.
var ErrUnsetTimestamp = errors.New("kv: timestamp is unset")

// Store is MVCC over the engine interface: versions keyed by timestamp, reads at
// a timestamp, and a garbage-collection mark below which reads are refused.
//
// It holds no lock, for the reason hlc.Clock holds none: in sim mode a node's
// logic runs single-threaded off the event loop, and in real mode everything
// enters through the mailbox (Amendment A1).
type Store struct {
	db engine.Engine

	// gcMark is the low-water mark. Versions at or below it may have been
	// collected, so reads at or below it are refused.
	//
	// # It is APPLIED STATE, and that is not a detail
	//
	// It advances by an applied command, never by a timer, so every replica has
	// the same mark at the same log position. A mark that advanced locally would
	// let two replicas disagree about whether a read is answerable -- a
	// divergence that surfaces as a client error on one node and an answer on
	// another, which is the hardest kind to attribute.
	//
	// That is A4's class in A5's dimension: a fact derived from a position has
	// to be derived at that position, and "when this node's timer fired" is not
	// a position anybody else can compute (DESIGN-A5 sections 6 and 7).
	gcMark hlc.Timestamp

	reads         int
	readsRefused  int
	versionsWrote int
	versionsGCd   int
}

// NewStore builds an MVCC store over an engine.
func NewStore(db engine.Engine) (*Store, error) {
	if db == nil {
		return nil, errors.New("kv: no engine")
	}
	return &Store{db: db}, nil
}

// PutInto stages a versioned write into a batch.
//
// The batch, not the engine: a write belongs to whatever atomic unit its caller
// is building -- an apply, a split, a snapshot install -- and a store that
// applied on its own would break every one of those atomicity arguments.
func (s *Store) PutInto(b *engine.Batch, key []byte, ts hlc.Timestamp, value []byte) error {
	if !ts.IsSet() {
		return ErrUnsetTimestamp
	}
	if ts.LessEq(s.gcMark) {
		// Writing below the mark would create a version no read may ever see,
		// because every read that could name it is refused. It is not merely
		// useless: the next GC pass would collect it, so the write's effect
		// depends on when GC next runs, which is not a property a state machine
		// may have.
		return fmt.Errorf("%w: write at %s is at or below the mark %s", ErrBelowGCMark, ts, s.gcMark)
	}
	b.Set(EncodeKey(key, ts), value)
	s.versionsWrote++
	return nil
}

// ReadAt returns the value visible at ts: the newest version at or before it.
//
// # Every fact here is derived AT ts, and that is the section-7 discipline
//
// The version is the newest at or before ts, not the newest. The mark is
// compared against ts, not against now. The absence of a version is an absence
// AT ts, not an absence today. Each of those is one line, and each is one of the
// six shapes A4 found in the other dimension.
func (s *Store) ReadAt(key []byte, ts hlc.Timestamp) ([]byte, bool, error) {
	if !ts.IsSet() {
		return nil, false, ErrUnsetTimestamp
	}
	s.reads++
	if ts.LessEq(s.gcMark) {
		s.readsRefused++
		return nil, false, fmt.Errorf("%w: read at %s, mark at %s", ErrBelowGCMark, ts, s.gcMark)
	}

	prefix := KeyPrefix(key)
	it := s.db.NewIter(engine.IterOptions{Lower: prefix, Upper: prefixEnd(prefix)})
	defer func() { _ = it.Close() }()

	// Versions sort newest-first within a key, so the first record at or after
	// the encoding of (key, ts) is the newest version at or before ts.
	if !it.SeekGE(EncodeKey(key, ts)) {
		return nil, false, it.Error()
	}
	gotKey, gotTS, ok := DecodeKey(it.Key())
	if !ok || string(gotKey) != string(key) {
		return nil, false, it.Error()
	}
	if ts.Less(gotTS) {
		// Cannot happen with a correct encoding: a record at or after the seek
		// target is at or before ts by construction. Asserted because if the
		// inversion ever breaks, the failure is a read returning a value from
		// the future, and that is the kind of thing that must not be discovered
		// by a bank workload three phases later.
		return nil, false, fmt.Errorf(
			"kv: read at %s landed on version %s, which is newer; the version encoding does not order",
			ts, gotTS)
	}
	return append([]byte(nil), it.Value()...), true, it.Error()
}

// AdvanceGCInto stages a garbage-collection pass into a batch: every version
// strictly below `to` for keys in [start, end) is removed, and the mark moves.
//
// # Why versions BELOW `to` and not at or below it
//
// The mark refuses reads at or below itself, so a version exactly at the mark is
// unreadable anyway -- but it is also the version a read at the mark's successor
// needs, since that read wants the newest version at or before it. Collecting it
// would make the first answerable timestamp return nothing where it should
// return a value. Off by one here is a silently wrong read, which is the exact
// failure the mark exists to prevent.
func (s *Store) AdvanceGCInto(b *engine.Batch, to hlc.Timestamp) (int, error) {
	if !to.IsSet() {
		return 0, ErrUnsetTimestamp
	}
	if to.LessEq(s.gcMark) {
		// Not an error: a replica re-applying a GC command after recovery sees
		// this, and idempotence is required for the same reason A4's split
		// needed it (appliedIdx is not persisted).
		return 0, nil
	}

	removed := 0
	it := s.db.NewIter(engine.IterOptions{Lower: []byte{dataPrefix}, Upper: []byte{dataPrefix + 1}})
	defer func() { _ = it.Close() }()

	// Walk every version, keeping the newest one at or before `to` for each key
	// and dropping everything older. The keeper is what a read at the mark's
	// successor must still find.
	var cur []byte
	kept := false
	for ok := it.First(); ok; ok = it.Next() {
		key, ts, ok := DecodeKey(it.Key())
		if !ok {
			continue
		}
		if string(key) != string(cur) {
			cur = append([]byte(nil), key...)
			kept = false
		}
		if to.LessEq(ts) {
			continue // above the mark: live
		}
		if !kept {
			kept = true // the newest version at or below `to`: the one a read at to.Next() needs
			continue
		}
		b.Delete(append([]byte(nil), it.Key()...))
		removed++
	}
	if err := it.Error(); err != nil {
		return 0, err
	}
	s.gcMark = to
	s.versionsGCd += removed
	return removed, nil
}

// GCMark is the current low-water mark.
func (s *Store) GCMark() hlc.Timestamp { return s.gcMark }

// SetGCMark restores the mark on recovery, from a snapshot that carried it.
//
// It is a restore rather than an advance: the mark is applied state and the
// value comes from a position, so recovery adopts it outright rather than taking
// a maximum against whatever this node happened to hold (BUG-011's shape).
func (s *Store) SetGCMark(ts hlc.Timestamp) { s.gcMark = ts }

// Reads, ReadsRefused, VersionsWritten and VersionsCollected report what a run
// exercised. Every one of them is asserted somewhere: a count nobody asserts on
// is decoration that looks like evidence (DESIGN-A4 section 9.4b).
func (s *Store) Reads() int             { return s.reads }
func (s *Store) ReadsRefused() int      { return s.readsRefused }
func (s *Store) VersionsWritten() int   { return s.versionsWrote }
func (s *Store) VersionsCollected() int { return s.versionsGCd }

// prefixEnd is the exclusive upper bound for a prefix scan.
func prefixEnd(p []byte) []byte {
	end := append([]byte(nil), p...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil // all 0xFF: unbounded above
}
