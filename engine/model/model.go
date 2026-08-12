// Package model is the deterministic Go reference engine.
//
// It exists so that consensus and transaction bugs never alias with storage
// bugs: Track A runs on this engine throughout, and when the C++ engine is
// swapped in at I1, any divergence is a storage bug by elimination. It is also
// the semantic reference the differential harness checks the C++ engine
// against from B4 onward, which is why DeleteRange is implemented natively here
// rather than as a helper (Amendment A3).
//
// # Structure
//
// A copy-on-write sorted slice, per DESIGN-A0 D12. O(n) per write and O(1) per
// snapshot, which is the right trade at simulation key counts: thousands of
// keys, not millions, where an n-element memcpy beats pointer chasing and is
// impossible to get wrong. The benchmark gate for upgrading to a hash-priority
// treap is >10% of hunt CPU; until that fires, correctness wins.
//
// A map is deliberately not used. It would invite `range` over a map -- the
// determinism leak this project's vet pass exists to catch -- and hide
// iteration cost inside every scan.
//
// # Durability
//
// Two versions are kept: visible (every applied batch) and durable (batches at
// or below DurableSeq). Crash discards visible and reloads from durable, which
// is exactly the WAL contract: buffered writes are readable and losable.
//
// Durability does not advance on its own. The owner calls AdvanceDurable, which
// in simulation is driven by an event the simulator schedules at a chosen
// latency, so there is always a real, non-empty stretch of virtual time in
// which acknowledged, readable, non-durable data exists. That window is not an
// artifact to be minimized; it is where the injector that finds
// ack-before-durable bugs does its work (DR-15).
package model

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/anshkanyadi/rift/engine"
)

// entry is one key-value pair. Versions share entries, so the bytes are
// immutable once stored: a write replaces the entry rather than editing it.
type entry struct {
	key   []byte
	value []byte
}

// version is an immutable sorted snapshot of the keyspace. Sharing one with a
// Snapshot is what makes snapshots O(1).
type version struct {
	entries []entry
	seq     engine.SeqNum
}

// DB is the reference engine.
type DB struct {
	// durable is the state a crash would recover. pending holds every version
	// applied since, in increasing sequence order; the last of them is what
	// reads see. Keeping them all is what lets the durability watermark sit at
	// any applied sequence and a crash revert to exactly that -- the whole
	// point of the model.
	durable *version
	pending []*version

	seq    engine.SeqNum
	closed bool

	onDurable []func(engine.SeqNum)

	openSnapshots int
	openIters     int
}

// New returns an empty engine.
func New() *DB {
	return &DB{durable: &version{}}
}

var _ engine.Engine = (*DB)(nil)

// visible is the version reads see: the newest pending one, or the durable one
// when nothing is in flight.
func (d *DB) visible() *version {
	if n := len(d.pending); n > 0 {
		return d.pending[n-1]
	}
	return d.durable
}

// Apply makes b visible immediately and returns the sequence at which it became
// visible. It never blocks: sync=true is a request for durability up to this
// sequence, and the owner advances the watermark when the modelled write
// completes. Both flavours are visible on return -- sync asks about
// persistence, not about visibility.
func (d *DB) Apply(b *engine.Batch, sync bool) (engine.SeqNum, error) {
	if d.closed {
		return 0, fmt.Errorf("model: Apply on a closed engine")
	}
	d.seq++

	prev := d.visible()
	if b == nil || b.Empty() {
		// An empty batch still takes a sequence and still produces a version.
		// It has to: syncing an empty batch is a barrier asking for everything
		// before it to be durable, and with no version to advance to, that
		// request would be unobservable. The entries are shared, not copied --
		// versions are immutable, and applyTo copies before it writes.
		d.pending = append(d.pending, &version{entries: prev.entries, seq: d.seq})
		return d.seq, nil
	}

	d.pending = append(d.pending, applyTo(prev, b, d.seq))
	return d.seq, nil
}

// DurableSeq is the highest sequence guaranteed to survive a crash.
func (d *DB) DurableSeq() engine.SeqNum { return d.durable.seq }

// OnDurable registers a callback for when the watermark advances.
func (d *DB) OnDurable(f func(engine.SeqNum)) {
	if f != nil {
		d.onDurable = append(d.onDurable, f)
	}
}

// AdvanceDurable moves the durability watermark to the highest applied sequence
// at or below seq.
//
// This is the model's stand-in for an fsync completing, and it is the only way
// the watermark moves. A sync-loss window is modelled by simply not calling it,
// which is why that injector needs no cooperation from the engine at all.
func (d *DB) AdvanceDurable(seq engine.SeqNum) {
	if seq > d.seq {
		panic(fmt.Sprintf("model: durability advanced to %d, past the last applied sequence %d", seq, d.seq))
	}
	advanced := false
	for len(d.pending) > 0 && d.pending[0].seq <= seq {
		d.durable = d.pending[0]
		d.pending = d.pending[1:]
		advanced = true
	}
	if !advanced {
		return
	}
	for _, f := range d.onDurable {
		f(d.durable.seq)
	}
}

// Crash discards everything not durable, exactly as a process death would.
//
// The engine is usable afterwards, which models a restart rather than forcing
// every test to reconstruct one -- and reconstruction would hide the case where
// recovery is subtly not the same as a fresh open.
func (d *DB) Crash() {
	d.pending = nil
	d.seq = d.durable.seq
	d.openSnapshots = 0
	d.openIters = 0
}

// Get returns the value for key, or engine.ErrNotFound.
func (d *DB) Get(key []byte) ([]byte, error) { return d.visible().get(key) }

// NewIter returns an iterator over the visible state.
func (d *DB) NewIter(o engine.IterOptions) engine.Iterator {
	d.openIters++
	return newIter(d.visible(), o, func() { d.openIters-- })
}

// NewSnapshot pins the current visible version.
func (d *DB) NewSnapshot() engine.Snapshot {
	d.openSnapshots++
	return &snapshot{db: d, v: d.visible()}
}

// ApproximateDiskBytes sums key and value lengths in [start, end).
//
// The model has no blocks, so "approximate" is exact here. That is a
// difference from the C++ engine worth naming: a split decision taken on this
// number is taken on a slightly different number in production, so any test
// that depends on the exact byte count is testing the model rather than the
// system.
func (d *DB) ApproximateDiskBytes(start, end []byte) (uint64, error) {
	var total uint64
	for _, e := range d.visible().entries {
		if engine.InRange(e.key, start, end) {
			total += uint64(len(e.key) + len(e.value))
		}
	}
	return total, nil
}

// Close releases the engine. Open iterators and snapshots are a caller bug and
// are reported rather than tolerated: in production they pin versions against
// compaction, and a leak that is invisible here would be a slow disk leak
// there.
func (d *DB) Close() error {
	if d.closed {
		return fmt.Errorf("model: Close on an already closed engine")
	}
	d.closed = true
	if d.openIters != 0 || d.openSnapshots != 0 {
		return fmt.Errorf("model: closed with %d open iterator(s) and %d open snapshot(s); in production these pin versions against compaction",
			d.openIters, d.openSnapshots)
	}
	return nil
}

// VisibleSeq is the sequence of the last applied batch, for checkers.
func (d *DB) VisibleSeq() engine.SeqNum { return d.visible().seq }

// get looks up a key in a version.
func (v *version) get(key []byte) ([]byte, error) {
	i := sort.Search(len(v.entries), func(i int) bool {
		return bytes.Compare(v.entries[i].key, key) >= 0
	})
	if i < len(v.entries) && bytes.Equal(v.entries[i].key, key) {
		out := make([]byte, len(v.entries[i].value))
		copy(out, v.entries[i].value)
		return out, nil
	}
	return nil, engine.ErrNotFound
}

// applyTo returns a new version with b applied to old.
//
// Copy-on-write: old is untouched, so every snapshot holding it keeps seeing
// exactly what it saw. Operations are applied in order, and the last write to a
// key within a batch wins -- including a DeleteRange removing keys an earlier
// op in the same batch wrote.
func applyTo(old *version, b *engine.Batch, seq engine.SeqNum) *version {
	entries := make([]entry, len(old.entries))
	copy(entries, old.entries)

	for _, op := range b.Ops() {
		switch op.Kind {
		case engine.OpSet:
			entries = set(entries, op.Key, op.Value)
		case engine.OpDelete:
			entries = del(entries, op.Key)
		case engine.OpDeleteRange:
			entries = delRange(entries, op.Key, op.End)
		default:
			panic(fmt.Sprintf("model: unknown op kind %d", op.Kind))
		}
	}
	return &version{entries: entries, seq: seq}
}

func search(entries []entry, key []byte) (int, bool) {
	i := sort.Search(len(entries), func(i int) bool {
		return bytes.Compare(entries[i].key, key) >= 0
	})
	return i, i < len(entries) && bytes.Equal(entries[i].key, key)
}

func set(entries []entry, key, value []byte) []entry {
	i, found := search(entries, key)
	if found {
		// Replace rather than edit: the old entry may be shared with a
		// snapshot, and editing it would rewrite history.
		entries[i] = entry{key: entries[i].key, value: value}
		return entries
	}
	entries = append(entries, entry{})
	copy(entries[i+1:], entries[i:])
	entries[i] = entry{key: key, value: value}
	return entries
}

func del(entries []entry, key []byte) []entry {
	i, found := search(entries, key)
	if !found {
		return entries
	}
	return append(entries[:i], entries[i+1:]...)
}

// delRange removes [start, end). A nil start means the beginning and a nil end
// the end, so DeleteRange(nil, nil) is the clear half of snapshot application's
// clear-then-ingest.
func delRange(entries []entry, start, end []byte) []entry {
	lo := 0
	if start != nil {
		lo, _ = search(entries, start)
	}
	hi := len(entries)
	if end != nil {
		hi, _ = search(entries, end)
	}
	if lo >= hi {
		return entries
	}
	return append(entries[:lo], entries[hi:]...)
}
