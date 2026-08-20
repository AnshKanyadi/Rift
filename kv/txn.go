package kv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
)

// The Percolator records, and the one sentence everything else is derived from.
//
// # A transaction is committed if and only if the write record for its PRIMARY
// # key exists
//
// Not "the coordinator returned", not "every secondary has a commit record",
// not "the locks are gone". A secondary holding a lock with no commit record is
// not maybe-committed: its status is whatever the primary says, and any observer
// may look. The recovery protocol is a consequence of that sentence rather than
// a set of cases, and an oracle can check it directly (DESIGN-A6 D-A6-3).

// TxnStatus is what a transaction record says.
type TxnStatus uint8

const (
	// TxnPending is the absence of a record. It is never written: a pending
	// transaction is one nobody has decided about, and writing "pending" would
	// make the absence and the record mean different things to a resolver.
	TxnPending TxnStatus = iota
	TxnCommitted
	TxnRolledBack
)

func (s TxnStatus) String() string {
	switch s {
	case TxnCommitted:
		return "committed"
	case TxnRolledBack:
		return "rolled-back"
	default:
		return "pending"
	}
}

// TxnRecord is a transaction's decision, stored on its primary key's range.
type TxnRecord struct {
	Status   TxnStatus
	StartTS  hlc.Timestamp
	CommitTS hlc.Timestamp // zero unless committed
}

// Lock is a prewritten intent: this key is claimed by a transaction whose
// decision lives on Primary.
type Lock struct {
	Primary []byte
	StartTS hlc.Timestamp

	// Deadline is when this lock may be broken by somebody else, expressed as a
	// TIMESTAMP rather than a wall duration.
	//
	// It is compared against a resolver's READ timestamp, never against a
	// resolver's clock. Two replicas resolving the same lock at the same log
	// position have to reach the same verdict or they diverge, and "my clock
	// now" is not a value they share (DESIGN-A6 §8).
	Deadline hlc.Timestamp
}

var (
	// ErrWriteConflict is returned by a prewrite that found a newer commit than
	// the transaction's own start timestamp. The transaction cannot commit
	// without violating snapshot isolation, so it aborts.
	ErrWriteConflict = errors.New("kv: write conflict")

	// ErrKeyIsLocked is returned by a prewrite or a read that found a lock it
	// may not break. The caller resolves it and retries; it is not a failure.
	ErrKeyIsLocked = errors.New("kv: key is locked")

	// ErrTxnAlreadyDecided is returned when a transaction tries to move a
	// decision that already exists. It is the safety net under the rule that a
	// resolver may only make a record EXIST.
	ErrTxnAlreadyDecided = errors.New("kv: the transaction record already exists")
)

// PrewriteInto stages a lock and a data version for one key.
//
// # The two checks, and why both are at the START timestamp
//
// A newer COMMIT than our start timestamp means somebody else wrote this key
// inside our snapshot's lifetime: committing on top would make our transaction's
// read of the key stale with respect to its own write, which is exactly what
// snapshot isolation forbids. An existing LOCK means somebody else is mid-flight
// here; we may not stack on it, and whether it can be broken is the resolver's
// question, not the prewriter's.
func (s *Store) PrewriteInto(b *engine.Batch, key []byte, l Lock, value []byte) error {
	if !l.StartTS.IsSet() {
		return ErrUnsetTimestamp
	}
	if l.StartTS.LessEq(s.gcMark) {
		return fmt.Errorf("%w: prewrite at %s is at or below the mark %s", ErrBelowGCMark, l.StartTS, s.gcMark)
	}
	if cur, ok, err := s.Lock(key); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("%w: %q held by the transaction at %s", ErrKeyIsLocked, key, cur.StartTS)
	}
	if commit, _, ok, err := s.newestCommit(key); err != nil {
		return err
	} else if ok && l.StartTS.Less(commit) {
		return fmt.Errorf("%w: %q was committed at %s, after this transaction started at %s",
			ErrWriteConflict, key, commit, l.StartTS)
	}

	b.Set(LockKey(s.ns, key), encodeLock(l))
	b.Set(EncodeKey(s.ns, key, l.StartTS), value)
	s.prewrites++
	return nil
}

// CommitInto stages the write record for one key and drops its lock.
//
// It does NOT check the transaction record. The caller has already established
// the decision -- either it is the coordinator that just wrote the primary's
// record, or it is a resolver that read one -- and re-deriving it here would
// invite a second, differently-timed answer to a question already settled
// (DESIGN-A6 §8: a fact derived at a position, derived once).
func (s *Store) CommitInto(b *engine.Batch, key []byte, startTS, commitTS hlc.Timestamp) error {
	if !startTS.IsSet() || !commitTS.IsSet() {
		return ErrUnsetTimestamp
	}
	if commitTS.Less(startTS) {
		return fmt.Errorf("kv: commit at %s is before the start at %s; a transaction cannot commit "+
			"before it began, and a reader would see the write appear in its own past", commitTS, startTS)
	}
	b.Set(WriteKey(s.ns, key, commitTS), encodeWrite(startTS, false))
	b.Delete(LockKey(s.ns, key))
	s.commits++
	return nil
}

// RollbackInto stages a rollback tombstone for one key and drops its lock and
// its uncommitted data version.
//
// The tombstone is written at the START timestamp rather than at a commit
// timestamp, because a rolled-back transaction has none. It exists so that a
// later prewrite by the same transaction cannot resurrect the key: the record
// says this start timestamp is dead, and it is dead for everybody.
func (s *Store) RollbackInto(b *engine.Batch, key []byte, startTS hlc.Timestamp) error {
	if !startTS.IsSet() {
		return ErrUnsetTimestamp
	}
	b.Set(WriteKey(s.ns, key, startTS), encodeWrite(startTS, true))
	b.Delete(LockKey(s.ns, key))
	b.Delete(EncodeKey(s.ns, key, startTS))
	s.rollbacks++
	return nil
}

// PutTxnInto stages a transaction record, and REFUSES to overwrite one.
//
// # The hard rule of resolution
//
// A resolver may only ever make the primary's record EXIST. It may write a
// rollback tombstone where there is none; it may never delete one or turn it
// into a commit. That single restriction is what makes resolution idempotent and
// race-safe against a coordinator finishing late: two resolvers reach the same
// verdict because the first write wins and the second reads it, and a coordinator
// that wakes up to find itself tombstoned aborts rather than arguing.
//
// The refusal is an error rather than a silent no-op because the two callers
// mean different things by it. A coordinator hitting it has lost a race and must
// abort; a resolver hitting it has been beaten to the same conclusion and may
// carry on. Silence would let the first mistake the second's outcome for its own.
func (s *Store) PutTxnInto(b *engine.Batch, primary []byte, r TxnRecord) error {
	if !r.StartTS.IsSet() {
		return ErrUnsetTimestamp
	}
	if r.Status == TxnCommitted && !r.CommitTS.IsSet() {
		return fmt.Errorf("kv: a committed transaction record with no commit timestamp")
	}
	if cur, ok, err := s.Txn(primary, r.StartTS); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("%w: %q is %s at %s", ErrTxnAlreadyDecided, primary, cur.Status, cur.CommitTS)
	}
	b.Set(TxnKey(s.ns, primary, r.StartTS), encodeTxn(r))
	return nil
}

// Txn reads a transaction record by its primary key and start timestamp.
func (s *Store) Txn(primary []byte, startTS hlc.Timestamp) (TxnRecord, bool, error) {
	v, err := s.db.Get(TxnKey(s.ns, primary, startTS))
	if errors.Is(err, engine.ErrNotFound) {
		return TxnRecord{}, false, nil
	}
	if err != nil {
		return TxnRecord{}, false, err
	}
	r, ok := decodeTxn(v)
	return r, ok, nil
}

// Lock reads a key's lock.
func (s *Store) Lock(key []byte) (Lock, bool, error) {
	v, err := s.db.Get(LockKey(s.ns, key))
	if errors.Is(err, engine.ErrNotFound) {
		return Lock{}, false, nil
	}
	if err != nil {
		return Lock{}, false, err
	}
	l, ok := decodeLock(v)
	return l, ok, nil
}

// newestCommit is the most recent commit record for key, and the start timestamp
// it points at.
func (s *Store) newestCommit(key []byte) (commitTS, startTS hlc.Timestamp, ok bool, err error) {
	prefix := WritePrefix(s.ns, key)
	it := s.db.NewIter(engine.IterOptions{Lower: prefix, Upper: prefixEnd(prefix)})
	defer func() { _ = it.Close() }()
	if !it.First() {
		return hlc.Timestamp{}, hlc.Timestamp{}, false, it.Error()
	}
	gotKey, c, ok := DecodeWriteKey(s.ns, it.Key())
	if !ok || string(gotKey) != string(key) {
		return hlc.Timestamp{}, hlc.Timestamp{}, false, it.Error()
	}
	st, _, ok := decodeWrite(it.Value())
	if !ok {
		return hlc.Timestamp{}, hlc.Timestamp{}, false, it.Error()
	}
	return c, st, true, it.Error()
}

// Prewrites, Commits and Rollbacks report what a run exercised.
func (s *Store) Prewrites() int { return s.prewrites }
func (s *Store) Commits() int   { return s.commits }
func (s *Store) Rollbacks() int { return s.rollbacks }

// --- codecs ---------------------------------------------------------------

func encodeLock(l Lock) []byte {
	b := putTS(nil, l.StartTS)
	b = putTS(b, l.Deadline)
	b = binary.BigEndian.AppendUint32(b, uint32(len(l.Primary)))
	return append(b, l.Primary...)
}

func decodeLock(b []byte) (Lock, bool) {
	var l Lock
	var ok bool
	if l.StartTS, b, ok = takeTS(b); !ok {
		return Lock{}, false
	}
	if l.Deadline, b, ok = takeTS(b); !ok {
		return Lock{}, false
	}
	if len(b) < 4 {
		return Lock{}, false
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	b = b[4:]
	if len(b) != n {
		return Lock{}, false
	}
	l.Primary = append([]byte(nil), b...)
	return l, true
}

func encodeWrite(startTS hlc.Timestamp, rollback bool) []byte {
	b := putTS(nil, startTS)
	if rollback {
		return append(b, 1)
	}
	return append(b, 0)
}

func decodeWrite(b []byte) (startTS hlc.Timestamp, rollback bool, ok bool) {
	st, rest, ok := takeTS(b)
	if !ok || len(rest) != 1 {
		return hlc.Timestamp{}, false, false
	}
	return st, rest[0] == 1, true
}

func encodeTxn(r TxnRecord) []byte {
	b := []byte{byte(r.Status)}
	b = putTS(b, r.StartTS)
	return putTS(b, r.CommitTS)
}

func decodeTxn(b []byte) (TxnRecord, bool) {
	if len(b) < 1 {
		return TxnRecord{}, false
	}
	r := TxnRecord{Status: TxnStatus(b[0])}
	var ok bool
	if r.StartTS, b, ok = takeTS(b[1:]); !ok {
		return TxnRecord{}, false
	}
	if r.CommitTS, _, ok = takeTS(b); !ok {
		return TxnRecord{}, false
	}
	return r, true
}

func putTS(b []byte, t hlc.Timestamp) []byte {
	b = binary.BigEndian.AppendUint64(b, uint64(t.Wall))
	return binary.BigEndian.AppendUint32(b, t.Logical)
}

func takeTS(b []byte) (hlc.Timestamp, []byte, bool) {
	if len(b) < tsBytes {
		return hlc.Timestamp{}, nil, false
	}
	return hlc.Timestamp{
		Wall:    clock.NewWall(int64(binary.BigEndian.Uint64(b[:wallBytes]))),
		Logical: binary.BigEndian.Uint32(b[wallBytes:tsBytes]),
	}, b[tsBytes:], true
}

// DefaultTTL is how long a lock is respected before somebody may break it.
//
// It is a duration here and a TIMESTAMP in the lock, converted once by whoever
// takes the lock. A resolver never converts: it compares timestamps, because two
// replicas must reach the same verdict from the same log position.
const DefaultTTL = 500 * time.Millisecond
