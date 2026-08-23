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

	// ns is this store's engine-key namespace: the range's prefix. Every key
	// this store writes, reads or collects is inside it, so a range's versions
	// are a contiguous keyspace like the rest of its state (A4).
	ns []byte

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

	// foreignLocksKept counts how often a commit or a rollback found a lock
	// belonging to another transaction and left it alone. Every one of these
	// was a lock BUG-019 would have stolen.
	foreignLocksKept int

	reads        int
	readsRefused int
	prewrites    int
	commits      int
	rollbacks    int
	readsBlocked int

	uncertaintyRestarts int
	rollForwards        int
	rollBacks           int
	versionsWrote       int
	versionsGCd         int

	// readMarks counts read marks staged, and readConflicts counts prewrites
	// refused because somebody had already been answered a read of the key
	// above the prewriter's snapshot. BUG-022's two halves, each with a number:
	// a run in which readMarks is zero recorded nothing for the guard to
	// consult, and one in which readConflicts is zero never reached the
	// interleaving the guard exists for.
	readMarks     int
	readConflicts int
}

// NewStore builds an MVCC store over an engine.
func NewStore(db engine.Engine, ns []byte) (*Store, error) {
	if db == nil {
		return nil, errors.New("kv: no engine")
	}
	return &Store{db: db, ns: append([]byte(nil), ns...)}, nil
}

// Namespace is the engine-key prefix this store owns.
func (s *Store) Namespace() []byte { return append([]byte(nil), s.ns...) }

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
	b.Set(EncodeKey(s.ns, key, ts), value)
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

	prefix := KeyPrefix(s.ns, key)
	it := s.db.NewIter(engine.IterOptions{Lower: prefix, Upper: prefixEnd(prefix)})
	defer func() { _ = it.Close() }()

	// Versions sort newest-first within a key, so the first record at or after
	// the encoding of (key, ts) is the newest version at or before ts.
	if !it.SeekGE(EncodeKey(s.ns, key, ts)) {
		return nil, false, it.Error()
	}
	gotKey, gotTS, ok := DecodeKey(s.ns, it.Key())
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
	lo := append(append([]byte(nil), s.ns...), dataPrefix)
	it := s.db.NewIter(engine.IterOptions{Lower: lo, Upper: prefixEnd(lo)})
	defer func() { _ = it.Close() }()

	// Walk every version, keeping the newest one at or before `to` for each key
	// and dropping everything older. The keeper is what a read at the mark's
	// successor must still find.
	var cur []byte
	kept := false
	for ok := it.First(); ok; ok = it.Next() {
		key, ts, ok := DecodeKey(s.ns, it.Key())
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

// ReadMarks and ReadConflicts are BUG-022's two halves, reported separately
// because they fail separately: a mark nobody consults and a guard with nothing
// to consult look identical from a violation count of zero.
func (s *Store) ReadMarks() int     { return s.readMarks }
func (s *Store) ReadConflicts() int { return s.readConflicts }

// ForeignLocksKept is BUG-019's non-vacuity evidence: a sweep in which it is
// zero never reached the schedule the fix exists for.
func (s *Store) ForeignLocksKept() int { return s.foreignLocksKept }

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

// Version is one record: a user key, the timestamp it was written at, and its
// value.
type Version struct {
	Key   []byte
	At    hlc.Timestamp
	Value []byte
}

// Versions enumerates everything this store holds, in engine order: by key, and
// newest-first within a key.
//
// It is the whole state of the machine, and it is what snapshots serialise,
// splits partition, and the digest covers. Ordering is the engine's, which is
// deterministic and identical on every replica -- a map range here would be the
// classic determinism leak, and it would leak into a snapshot digest, which is
// the one place it would look like a real divergence.
func (s *Store) Versions() ([]Version, error) {
	lo := append(append([]byte(nil), s.ns...), dataPrefix)
	it := s.db.NewIter(engine.IterOptions{Lower: lo, Upper: prefixEnd(lo)})
	defer func() { _ = it.Close() }()

	var out []Version
	for ok := it.First(); ok; ok = it.Next() {
		key, ts, ok := DecodeKey(s.ns, it.Key())
		if !ok {
			continue
		}
		out = append(out, Version{
			Key:   append([]byte(nil), key...),
			At:    ts,
			Value: append([]byte(nil), it.Value()...),
		})
	}
	return out, it.Error()
}

// IngestInto stages a whole state into a batch: this store's DATA VERSIONS are
// cleared and replaced.
//
// # It is A5's shape, and its name overstates what it does
//
// It clears the data prefix only. At A5 that was the whole state machine, so the
// name was accurate; A6 added three record kinds and A6's own fix added a fifth,
// and none of them is touched here. `IngestRecordsInto` is the one that replaces
// a state machine, and it is what every non-test caller uses. This survives
// because tests about versions are clearer with it — and the comment is corrected
// rather than the function deleted, because a name that overstates is a trap
// whether or not anything is currently caught by it.
//
// Clear-then-ingest in ONE batch is why DeleteRange is in the frozen engine
// interface (Amendment A3). A best-effort clear followed by a separate write
// would leave a window where the range holds a mixture of two states, and a
// crash in that window recovers into it.
func (s *Store) IngestInto(b *engine.Batch, vs []Version, mark hlc.Timestamp) {
	lo := append(append([]byte(nil), s.ns...), dataPrefix)
	b.DeleteRange(lo, prefixEnd(lo))
	for _, v := range vs {
		b.Set(EncodeKey(s.ns, v.Key, v.At), v.Value)
	}
	s.gcMark = mark
}

// Keys is the distinct user keys this store holds, in order.
//
// A split cuts at the median of these, not of the versions: cutting at the
// median VERSION would put the cut wherever the write traffic was heaviest
// rather than in the middle of the key space, and a hot key would split a range
// into one holding it and one holding everything else.
func (s *Store) Keys() ([][]byte, error) {
	vs, err := s.Versions()
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, v := range vs {
		if len(out) == 0 || string(out[len(out)-1]) != string(v.Key) {
			out = append(out, v.Key)
		}
	}
	return out, nil
}

// Record is one engine record belonging to this store: a data version, a lock, a
// commit record or a transaction record.
//
// # Why the snapshot carries all four and not just the versions
//
// A5's snapshot payload carried data versions, because at A5 that was the whole
// state machine. A6 adds three more record kinds, and a snapshot that carried
// only versions would hand a follower a state machine with the values and none
// of the locks, commit records or decisions -- so an installed replica would
// answer reads from data nobody had committed and resolve nothing, while every
// other replica saw a consistent picture.
//
// The state machine is everything under this store's namespace. Enumerating it
// rather than listing the kinds is deliberate: a fifth record kind added later
// is carried automatically, and the failure mode of forgetting one is exactly
// the one above.
type Record struct {
	Key   []byte // the ENGINE key, namespace included
	Value []byte
}

// Records is the whole state machine, in engine order.
func (s *Store) Records() ([]Record, error) {
	it := s.db.NewIter(engine.IterOptions{Lower: s.ns, Upper: prefixEnd(s.ns)})
	defer func() { _ = it.Close() }()

	var out []Record
	for ok := it.First(); ok; ok = it.Next() {
		if !s.owns(it.Key()) {
			continue
		}
		out = append(out, Record{
			Key:   append([]byte(nil), it.Key()...),
			Value: append([]byte(nil), it.Value()...),
		})
	}
	return out, it.Error()
}

// owns reports whether an engine key under this namespace belongs to the state
// machine rather than to raft's own bookkeeping.
//
// The two share a namespace because A4 gave each range one contiguous keyspace,
// so the discriminator is the record kind byte. Listing what the state machine
// owns rather than what raft owns is the safer direction: a new raft key is
// excluded by default, and a new state-machine kind has to be added here, where
// the compiler cannot help but the test that snapshots a lock will.
func (s *Store) owns(engineKey []byte) bool {
	if len(engineKey) <= len(s.ns) || string(engineKey[:len(s.ns)]) != string(s.ns) {
		return false
	}
	switch engineKey[len(s.ns)] {
	case dataPrefix, lockPrefix, writePrefix, txnPrefix, readPrefix:
		return true
	}
	return false
}

// IngestRecordsInto replaces this store's whole state in one batch.
func (s *Store) IngestRecordsInto(b *engine.Batch, rs []Record, mark hlc.Timestamp) {
	for _, kind := range []byte{dataPrefix, lockPrefix, writePrefix, txnPrefix, readPrefix} {
		lo := append(append([]byte(nil), s.ns...), kind)
		b.DeleteRange(lo, prefixEnd(lo))
	}
	for _, r := range rs {
		b.Set(r.Key, r.Value)
	}
	s.gcMark = mark
}

// UserKeyOf extracts the user key a record belongs to, whatever its kind.
//
// A split partitions by USER key, and every record kind embeds one: data and
// write records add a timestamp after it, locks and transaction records do not.
// A transaction record goes wherever its PRIMARY key goes, which is what keeps a
// resolver able to find it after a split (D-A6-1).
func (s *Store) UserKeyOf(engineKey []byte) ([]byte, bool) {
	if !s.owns(engineKey) {
		return nil, false
	}
	kind := engineKey[len(s.ns)]
	key, _, ok := takeMetaKey(s.ns, kind, engineKey)
	return key, ok
}
