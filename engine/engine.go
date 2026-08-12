package engine

import "errors"

// SeqNum is a monotone counter over applied batches. Every Apply returns the
// sequence at which its writes became visible, and DurableSeq is the highest
// sequence guaranteed to survive a crash.
type SeqNum uint64

// ErrNotFound is returned by Get for a key that is absent or deleted. It is a
// sentinel rather than a (value, bool) pair because the C++ engine returns
// error codes across the cgo boundary and one shape is cheaper than two.
var ErrNotFound = errors.New("engine: key not found")

// Engine is the storage interface both engines implement: the deterministic Go
// reference in engine/model, and the C++ LSM behind cgo in Track B.
//
// The contract that matters is durability, and it is exactly a WAL's:
//
//   - Apply makes writes visible immediately and never blocks on I/O.
//   - DurableSeq is a watermark that advances behind visibility.
//   - A crash reverts state to exactly DurableSeq -- not more, not less.
//
// Buffered writes are readable and losable. That gap is not an implementation
// detail to be minimized; it is the window in which acknowledged-but-unsynced
// data exists, and it is where the injector that finds ack-before-durable bugs
// lives (DESIGN-A0 D9, DR-15).
type Engine interface {
	// Apply makes b visible to subsequent reads immediately and returns the
	// sequence number at which it became visible. It never blocks on I/O.
	//
	// sync=true additionally requests durability for everything at or below
	// the returned SeqNum; sync=false leaves it buffered, and a crash takes
	// it. Either way the write is visible on return: sync asks about
	// persistence, not about visibility.
	Apply(b *Batch, sync bool) (SeqNum, error)

	// DurableSeq is the highest SeqNum guaranteed to survive a crash. Monotone
	// non-decreasing. A crash reverts state to exactly this point.
	DurableSeq() SeqNum

	// OnDurable registers a callback invoked when DurableSeq advances.
	//
	// In sim the simulator schedules a durability event on the node's loop; in
	// real mode a per-engine poller owns the blocking Sync() and posts a
	// DurabilityAdvanced event to the node's mailbox. The callback therefore
	// runs on the node loop in both modes and must never touch core state
	// directly, and no C-to-Go callback crosses the cgo boundary (DR-11).
	OnDurable(func(SeqNum))

	// Get returns the value for key, or ErrNotFound. The returned bytes are
	// owned by the caller.
	Get(key []byte) ([]byte, error)

	// NewIter returns an iterator over [Lower, Upper).
	NewIter(o IterOptions) Iterator

	// NewSnapshot pins a consistent view of the current visible state. It must
	// be Closed, and it holds its version against compaction until it is.
	NewSnapshot() Snapshot

	// ApproximateDiskBytes estimates the bytes occupied by [start, end), for
	// split decisions. Approximate is in the name because the C++ engine
	// answers from table metadata rather than by scanning.
	ApproximateDiskBytes(start, end []byte) (uint64, error)

	Close() error
}

// IterOptions bounds an iterator to [Lower, Upper). A nil Lower means the
// beginning; a nil Upper means the end.
type IterOptions struct {
	Lower []byte
	Upper []byte
}

// Iterator walks keys in byte order. Key and Value are valid only until the
// next positioning call, which is what lets the C++ engine hand back pointers
// into a block rather than copying every pair across cgo.
type Iterator interface {
	SeekGE(key []byte) bool
	SeekLT(key []byte) bool
	First() bool
	Last() bool
	Next() bool
	Prev() bool
	Valid() bool
	Key() []byte
	Value() []byte
	Error() error
	Close() error
}

// Snapshot is a pinned view. Reads through it see the state as of the moment it
// was taken, regardless of what has been applied since.
type Snapshot interface {
	Get(key []byte) ([]byte, error)
	NewIter(o IterOptions) Iterator
	Close() error
}
