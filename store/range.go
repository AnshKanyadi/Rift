package store

import (
	"bytes"
	"fmt"

	"github.com/anshkanyadi/rift/raft"
)

// RangeDescriptor is a range's identity and extent: who it is, which keys it
// owns, and how many times that has changed.
//
// # The epoch is the anti-staleness device, and it is why a refusal is typed
//
// A client routes a request from a cached descriptor and sends the epoch it
// routed under. A replica refuses a request whose epoch is behind its own -- and
// answers with StaleRangeEpoch rather than dropping it, because a silent drop is
// indistinguishable from a partition and the client would retry forever against
// the same stale cache. CLAUDE.md's invariant list names it: no request served
// under a stale descriptor epoch.
type RangeDescriptor struct {
	ID    RangeID
	Start []byte // inclusive
	End   []byte // exclusive; nil means unbounded
	Epoch uint64
}

// Contains reports whether key falls in [Start, End).
func (d RangeDescriptor) Contains(key []byte) bool {
	if bytes.Compare(key, d.Start) < 0 {
		return false
	}
	return d.End == nil || bytes.Compare(key, d.End) < 0
}

func (d RangeDescriptor) String() string {
	end := "∞"
	if d.End != nil {
		end = string(d.End)
	}
	return fmt.Sprintf("r%d[%s,%s)@%d", d.ID, string(d.Start), end, d.Epoch)
}

// Clone copies the descriptor, because it travels in log entries and snapshots
// and must never alias either.
func (d RangeDescriptor) Clone() RangeDescriptor {
	return RangeDescriptor{
		ID:    d.ID,
		Start: append([]byte(nil), d.Start...),
		End:   append([]byte(nil), d.End...),
		Epoch: d.Epoch,
	}
}

// SplitSpec is what an EntrySplit carries: where to cut, and what the two halves
// become.
//
// The right-hand range is DERIVED from each replica's own applied state rather
// than transferred, so nothing can be lost in transit and every replica builds
// the same thing -- which is the property state machine safety already
// guarantees, spent here (DESIGN-A4 §4). What travels in the entry is only the
// agreement about where the cut is and what the halves are called.
type SplitSpec struct {
	Key   []byte
	Left  RangeDescriptor
	Right RangeDescriptor
}

// encodeSplit and decodeSplit are the split entry's wire and storage form.
func encodeSplit(s SplitSpec) []byte {
	b := putBytes(nil, s.Key)
	b = encodeDescInto(b, s.Left)
	return encodeDescInto(b, s.Right)
}

func decodeSplit(b []byte) (SplitSpec, bool) {
	var s SplitSpec
	var ok bool
	if s.Key, b, ok = takeBytes(b); !ok {
		return s, false
	}
	if s.Left, b, ok = decodeDescFrom(b); !ok {
		return s, false
	}
	s.Right, _, ok = decodeDescFrom(b)
	return s, ok
}

func encodeDescInto(b []byte, d RangeDescriptor) []byte {
	b = putU64(b, uint64(d.ID))
	b = putU64(b, d.Epoch)
	b = putBytes(b, d.Start)
	// A nil End is unbounded and an empty End is not a thing, so one flag byte
	// keeps the distinction the codec would otherwise lose.
	if d.End == nil {
		return append(b, 0)
	}
	return putBytes(append(b, 1), d.End)
}

func decodeDescFrom(b []byte) (RangeDescriptor, []byte, bool) {
	var d RangeDescriptor
	id, b, ok := takeU64(b)
	if !ok {
		return d, nil, false
	}
	d.ID = RangeID(id)
	if d.Epoch, b, ok = takeU64(b); !ok {
		return d, nil, false
	}
	if d.Start, b, ok = takeBytes(b); !ok {
		return d, nil, false
	}
	if len(b) < 1 {
		return d, nil, false
	}
	bounded := b[0] == 1
	b = b[1:]
	if !bounded {
		return d, b, true
	}
	d.End, b, ok = takeBytes(b)
	return d, b, ok
}

// encodeDesc and decodeDesc store a descriptor on its own.
func encodeDesc(d RangeDescriptor) []byte { return encodeDescInto(nil, d) }

func decodeDesc(b []byte) (RangeDescriptor, bool) {
	d, _, ok := decodeDescFrom(b)
	return d, ok
}

// splitKeyFor picks where to cut a range, given the keys it holds.
//
// The median of the sorted keys, so both halves are non-empty and the choice is
// a function of the state every replica already agrees on. A split key drawn
// from anything else -- a clock, a counter, the leader's mood -- would be a
// value the followers cannot re-derive, and the split entry carries it precisely
// so they do not have to.
func splitKeyFor(keys []string) ([]byte, bool) {
	if len(keys) < 2 {
		return nil, false
	}
	return []byte(keys[len(keys)/2]), true
}

// entrySplitType is the log entry kind a split rides in. It reuses raft's
// EntryNormal envelope with a marker prefix, because raft has no business
// knowing what a range is: to the state machine below it, a split is a command
// like any other, and to raft it is bytes.
const splitMarker byte = 0xFF

func isSplitCommand(data []byte) bool { return len(data) > 0 && data[0] == splitMarker }

func encodeSplitCommand(s SplitSpec) []byte {
	return append([]byte{splitMarker}, encodeSplit(s)...)
}

func decodeSplitCommand(data []byte) (SplitSpec, bool) {
	if !isSplitCommand(data) {
		return SplitSpec{}, false
	}
	return decodeSplit(data[1:])
}

// DecodeSplitCommand exposes a split command to the harness, which models the
// state machine independently and has to apply splits -- and has to decide which
// ones to apply -- to stay faithful to it.
//
// What crosses this boundary is the WIRE FORMAT and nothing else. The rule for
// deciding whether a split applies is restated in the harness in its own terms;
// sharing that rule is what would let a defect cancel out on both sides of the
// comparison.
func DecodeSplitCommand(data []byte) (SplitSpec, bool) { return decodeSplitCommand(data) }

// DecodeDescriptor decodes a range extent recorded in the ledger.
func DecodeDescriptor(b []byte) (RangeDescriptor, bool) { return decodeDesc(b) }

// EncodeDescriptor encodes a range extent for the ledger.
func EncodeDescriptor(d RangeDescriptor) []byte { return encodeDesc(d) }

var _ = raft.EntryNormal

// FirstRange is the range every machine is born hosting, covering the whole key
// space. Its descriptor is a constant rather than a record: there is no earlier
// state it could have been derived from.
const FirstRange RangeID = 1

// FirstRangeDescriptor is the extent FirstRange is born with.
func FirstRangeDescriptor() RangeDescriptor {
	return RangeDescriptor{ID: FirstRange, Epoch: 1}
}

func hasRange(ds []RangeDescriptor, id RangeID) bool {
	for _, d := range ds {
		if d.ID == id {
			return true
		}
	}
	return false
}
