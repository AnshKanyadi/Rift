package engine

import "bytes"

// OpKind is what a batch entry does.
type OpKind uint8

const (
	OpSet OpKind = iota
	OpDelete
	OpDeleteRange
)

func (k OpKind) String() string {
	switch k {
	case OpSet:
		return "set"
	case OpDelete:
		return "delete"
	case OpDeleteRange:
		return "deleterange"
	}
	return "unknown"
}

// Op is one entry in a batch. For OpDeleteRange, Key is the inclusive start and
// End the exclusive end; for the others End is nil.
type Op struct {
	Kind  OpKind
	Key   []byte
	End   []byte
	Value []byte
}

// Batch is an ordered set of writes applied atomically. Ordering matters:
// within a batch the last write to a key wins, and a DeleteRange removes
// everything written before it in the same batch that falls inside the range.
//
// Every method copies its arguments. The alternative -- retaining caller slices
// -- makes an engine's behaviour depend on whether a caller happens to reuse a
// buffer, which is a class of bug that reproduces on one machine and not
// another, and is exactly what this project cannot afford.
type Batch struct {
	ops []Op
}

// NewBatch returns an empty batch.
func NewBatch() *Batch { return &Batch{} }

// Set writes value at key.
func (b *Batch) Set(key, value []byte) *Batch {
	b.ops = append(b.ops, Op{Kind: OpSet, Key: clone(key), Value: clone(value)})
	return b
}

// Delete removes key.
func (b *Batch) Delete(key []byte) *Batch {
	b.ops = append(b.ops, Op{Kind: OpDelete, Key: clone(key)})
	return b
}

// DeleteRange removes every key in [start, end).
//
// It is in the frozen interface by Amendment A3, over the original
// recommendation: snapshot application needs an atomic clear-then-ingest that a
// best-effort helper cannot provide at the right isolation, and replica removal
// at scale cannot be one unbounded point-delete batch. engine/model implements
// it natively; the C++ engine implements it internally as
// iterate-and-point-delete through B2, so B4's differential tests exercise the
// semantics long before real range tombstones land in B3.
func (b *Batch) DeleteRange(start, end []byte) *Batch {
	b.ops = append(b.ops, Op{Kind: OpDeleteRange, Key: clone(start), End: clone(end)})
	return b
}

// Ops returns the batch's entries in order. The slice is the batch's own; an
// engine reads it and must not retain it past Apply.
func (b *Batch) Ops() []Op { return b.ops }

// Len is the number of entries.
func (b *Batch) Len() int { return len(b.ops) }

// Empty reports whether the batch would change nothing.
func (b *Batch) Empty() bool { return len(b.ops) == 0 }

// Reset empties the batch for reuse.
func (b *Batch) Reset() { b.ops = b.ops[:0] }

// InRange reports whether key falls in [start, end), with a nil end meaning
// unbounded. It is here rather than in each engine so that both agree on the
// half-open convention by construction.
func InRange(key, start, end []byte) bool {
	if start != nil && bytes.Compare(key, start) < 0 {
		return false
	}
	if end != nil && bytes.Compare(key, end) >= 0 {
		return false
	}
	return true
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
