package kv

import (
	"errors"
	"fmt"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
)

// ErrReadWithinUncertaintyInterval is returned when a read at T finds a value
// committed in (T, T+maxOffset].
//
// # Why this is not "just read the older value"
//
// The clocks disagree by up to maxOffset and nothing on this node can tell
// whether that value was written before the read began in real time. Returning
// the older value would be a stale read: a transaction that started after the
// write, by a wall clock nobody can consult, would fail to see it. Snapshot
// isolation forbids exactly that, and the bound is the only thing that makes the
// question answerable at all.
//
// The restart timestamp is carried on the error because it is not derivable by
// the caller: it must be strictly ABOVE the observed value's commit timestamp,
// not T+maxOffset and not now (CLAUDE.md's sharp-edge list says so by name).
type UncertaintyError struct {
	Key      []byte
	ReadTS   hlc.Timestamp
	CommitTS hlc.Timestamp
}

func (e *UncertaintyError) Error() string {
	return fmt.Sprintf("kv: read at %s found %q committed at %s, inside the uncertainty interval; "+
		"restart above %s", e.ReadTS, e.Key, e.CommitTS, e.CommitTS)
}

// RestartAt is the timestamp the transaction must retry at: strictly above the
// value that caused the restart.
func (e *UncertaintyError) RestartAt() hlc.Timestamp { return e.CommitTS.Next() }

// LockedError is returned by a read that found a lock at or below its timestamp.
// The caller resolves and retries; it is not a failure.
type LockedError struct {
	Key  []byte
	Lock Lock
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("%s: %q by the transaction at %s, primary %q",
		ErrKeyIsLocked, e.Key, e.Lock.StartTS, e.Lock.Primary)
}

func (e *LockedError) Unwrap() error { return ErrKeyIsLocked }

// GetTxn reads the value of key visible at readTS under snapshot isolation.
//
// # Three questions, each answered AT readTS
//
//  1. Is there a lock at or below readTS? Then the value at readTS depends on a
//     transaction that has not been decided, and the answer is not yet
//     available -- not "the older value", which would be a guess about a
//     decision nobody has made.
//  2. Is there a commit in (readTS, readTS+maxOffset]? Then the answer is
//     uncertain and the transaction restarts above it.
//  3. Otherwise: the newest commit at or before readTS names a start timestamp,
//     and the data version at that start timestamp is the answer.
//
// The third is the indirection Percolator buys with its lock: the value landed
// under a timestamp that existed at prewrite time, and the commit record is what
// makes it visible. A rollback tombstone is a commit record that makes nothing
// visible, which is why it can be skipped rather than special-cased.
func (s *Store) GetTxn(key []byte, readTS hlc.Timestamp, maxOffset hlc.Timestamp) ([]byte, bool, error) {
	if !readTS.IsSet() {
		return nil, false, ErrUnsetTimestamp
	}
	s.reads++
	if readTS.LessEq(s.gcMark) {
		s.readsRefused++
		return nil, false, fmt.Errorf("%w: read at %s, mark at %s", ErrBelowGCMark, readTS, s.gcMark)
	}

	if l, ok, err := s.Lock(key); err != nil {
		return nil, false, err
	} else if ok && l.StartTS.LessEq(readTS) {
		s.readsBlocked++
		return nil, false, &LockedError{Key: append([]byte(nil), key...), Lock: l}
	}

	prefix := WritePrefix(s.ns, key)
	it := s.db.NewIter(engine.IterOptions{Lower: prefix, Upper: prefixEnd(prefix)})
	defer func() { _ = it.Close() }()

	// Commit records sort newest-first, so the scan starts above readTS and
	// walks down. Everything it passes on the way is a candidate for the
	// uncertainty window, which is the only reason the scan starts there rather
	// than seeking straight to readTS.
	for ok := it.First(); ok; ok = it.Next() {
		gotKey, commitTS, ok := DecodeWriteKey(s.ns, it.Key())
		if !ok || string(gotKey) != string(key) {
			break
		}
		startTS, rollback, ok := decodeWrite(it.Value())
		if !ok {
			continue
		}
		switch {
		case readTS.Less(commitTS):
			// Above the read. Uncertain only if it is inside the envelope AND
			// it is a real commit: a rollback tombstone made nothing visible,
			// so there is nothing for the reader to have missed.
			if !rollback && commitTS.LessEq(uncertaintyBound(readTS, maxOffset)) {
				s.uncertaintyRestarts++
				return nil, false, &UncertaintyError{
					Key: append([]byte(nil), key...), ReadTS: readTS, CommitTS: commitTS,
				}
			}
			continue
		case rollback:
			// A tombstone at or below the read: this start timestamp is dead.
			// Keep walking down for an older real commit.
			continue
		default:
			v, err := s.db.Get(EncodeKey(s.ns, key, startTS))
			if errors.Is(err, engine.ErrNotFound) {
				return nil, false, fmt.Errorf(
					"kv: %q is committed at %s naming start %s, and no version exists there; a commit "+
						"record outlived the value it makes visible", key, commitTS, startTS)
			}
			if err != nil {
				return nil, false, err
			}
			return append([]byte(nil), v...), true, nil
		}
	}
	return nil, false, it.Error()
}

// uncertaintyBound is the top of the interval: readTS advanced by maxOffset.
//
// maxOffset arrives as a Timestamp rather than a Duration because the caller
// takes it from hlc.Source.MaxOffset() and converts once. A store that converted
// per read would be doing clock arithmetic in the one place that must be a pure
// function of its arguments.
func uncertaintyBound(readTS, maxOffset hlc.Timestamp) hlc.Timestamp {
	return hlc.Timestamp{Wall: readTS.Wall + maxOffset.Wall, Logical: 0}
}

// ReadsBlocked and UncertaintyRestarts report what a run exercised. Both are
// asserted somewhere: a count nobody asserts on is decoration that looks like
// evidence.
func (s *Store) ReadsBlocked() int        { return s.readsBlocked }
func (s *Store) UncertaintyRestarts() int { return s.uncertaintyRestarts }
