// Package hlc is the hybrid logical clock: a physical reading plus a logical
// counter, giving every event a timestamp consistent with causality across
// nodes whose physical clocks disagree.
//
// # What this package is allowed to read, and why it is only one thing
//
// It wraps clock.Wall and nothing else. Never clock.Mono.
//
// A Mono is elapsed time on one node's oscillator since one boot. An HLC
// timestamp is persisted, sent on the wire, and compared across nodes and
// across restarts -- every one of which is the monotonic-leakage class DESIGN-A0.4
// made uncompilable rather than reviewable. This package inherits that
// enforcement rather than restating it: Wall and Mono are distinct defined types
// so one does not assign to the other, Mono has no encoder, and determinismcheck
// rejects a clock.Mono in an exported or tagged field outside clock/.
//
// A5 adds nothing to those three layers. What A5 must not do is add a hatch.
package hlc

import (
	"errors"
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
)

// Timestamp is a point in the hybrid clock's order: a physical reading and a
// logical counter that breaks ties.
//
// The zero Timestamp is "unset", and that is load-bearing in the same way
// clock.Wall's zero is: simulated wall time is offset by a nonzero epoch, so a
// forgotten field reads as unset rather than as the beginning of time.
type Timestamp struct {
	// Wall is the physical half. It crosses the wire; that is the whole reason
	// D-A5-1 chose it.
	Wall clock.Wall

	// Logical breaks ties within one Wall nanosecond. It is not a clock: it
	// counts events at the same physical reading and resets whenever the
	// physical reading advances.
	Logical uint32
}

// IsSet reports whether this timestamp was ever assigned.
func (t Timestamp) IsSet() bool { return t.Wall.IsSet() || t.Logical != 0 }

// Less reports whether t orders strictly before u.
func (t Timestamp) Less(u Timestamp) bool {
	if t.Wall != u.Wall {
		return t.Wall < u.Wall
	}
	return t.Logical < u.Logical
}

// LessEq reports whether t orders at or before u.
func (t Timestamp) LessEq(u Timestamp) bool { return !u.Less(t) }

// Equal reports whether two timestamps are the same point.
func (t Timestamp) Equal(u Timestamp) bool { return t == u }

// Next is the smallest timestamp strictly after t.
//
// It exists because "just after this version" is a real question -- a write that
// must land above a read, a scan bound that must exclude a version -- and
// spelling it as t.Wall+1 is wrong: the answer is one logical tick, not one
// nanosecond, and the difference is every event that shares that nanosecond.
func (t Timestamp) Next() Timestamp {
	if t.Logical == ^uint32(0) {
		return Timestamp{Wall: t.Wall + 1}
	}
	return Timestamp{Wall: t.Wall, Logical: t.Logical + 1}
}

func (t Timestamp) String() string {
	return fmt.Sprintf("%d.%d", int64(t.Wall), t.Logical)
}

// Max returns whichever of the two orders later.
func Max(a, b Timestamp) Timestamp {
	if a.Less(b) {
		return b
	}
	return a
}

// ErrBeyondEnvelope is returned by Update when a peer's timestamp is further
// ahead than maxOffset allows.
//
// # Why this is an error and not a max, and not a panic either
//
// Classic HLC takes the max unconditionally. That is the failure mode which
// makes clock skew unbounded in practice: one node whose clock has jumped
// forward drags the entire cluster's notion of physical time with it, and every
// bound that rests on maxOffset -- uncertainty intervals at A6, leases in
// STRETCH -- quietly stops meaning anything. The envelope exists to bound
// exactly this, so accepting the timestamp is discarding the thing being
// checked.
//
// The other candidate was a panic, which is what CockroachDB does. That is a
// caller-bug classification for a runtime condition: a peer's clock is not
// something the receiving node can check before it receives the message, and
// BUG-010 is this project's standing lesson about getting that distinction
// wrong. A refusal is reported, counted, and survivable.
var ErrBeyondEnvelope = errors.New("hlc: a peer's timestamp is beyond maxOffset ahead of this node's clock")

// Source hands out timestamps.
//
// # Why this is an interface at A5 when there is exactly one implementation
//
// CLAUDE.md Amendment A6: "The timestamp source lands behind an interface in A5;
// TSO fallback is pre-authorized if A6's uncertainty machinery is not green by
// Dec 1." The interface is what makes that fallback a construction change rather
// than a rewrite of kv/.
//
// What it must not become is a place where a caller asks WHICH source it has. A
// type switch on this interface anywhere in kv/ defeats the entire purpose, and
// A5's exit criteria treat one as a defect.
//
// MaxOffset is here rather than fetched from a clock because a TSO's uncertainty
// is a property of the TSO, not of any node's local oscillator. A caller that
// reached past this interface to a clock for the bound would be correct today
// and wrong the moment the fallback is taken.
type Source interface {
	// Now returns a timestamp strictly after every timestamp this source has
	// previously returned or been updated with.
	Now() Timestamp

	// Update folds a timestamp received from elsewhere into this source, so
	// that anything it stamps afterwards orders after it.
	Update(Timestamp) error

	// MaxOffset is the assumed bound on physical disagreement.
	MaxOffset() time.Duration
}

// # The logical counter carries the minting node's ordinal in its low bits
//
// BUG-021: two nodes minted the identical (wall, logical) pair, and two
// transactions therefore shared one key's lock and its data version -- which are
// addressed by the start timestamp, not by anything that knows which transaction
// owns them. One committed, one was rolled back, and the version belonged to
// both.
//
// A transaction's start timestamp has to be unique cluster-wide. Percolator gets
// that free from a single TSO; a per-node HLC guarantees it per node and nothing
// across nodes. So the low IDBits of Logical are the minting node's ordinal, and
// two nodes cannot produce the same timestamp at the same wall.
//
// Nothing else changes: the order is the same order, monotonicity per node is
// unchanged, and the counter keeps 32-IDBits of range -- which only advances
// when two events share a nanosecond, so it is not close to a constraint.
//
// **The correctness of this rests on node ordinals being unique and stable.**
// Anything that recycles an ordinal -- a node removed and a new one taking its
// number -- reopens BUG-021 exactly. That is recorded in STRETCH.md beside
// option C, which is the fix for the CLASS rather than for the defect.
const (
	// IDBits is how much of Logical the node ordinal occupies.
	IDBits = 8

	// MaxNodeID is the largest ordinal a tagged clock can carry.
	MaxNodeID = (1 << IDBits) - 1

	logicalStep = uint32(1) << IDBits
	logicalMask = logicalStep - 1
)

// nextTagged is the smallest timestamp at after's wall that carries id in its
// low bits and is strictly greater than after.
//
// It is the whole of the tagging scheme, and it is separated out because both
// Now and Update need exactly this and getting it subtly different in two places
// is how a monotonicity bug is written.
func nextTagged(after Timestamp, id uint32) Timestamp {
	base := after.Logical &^ logicalMask
	if cand := base | id; cand > after.Logical {
		return Timestamp{Wall: after.Wall, Logical: cand}
	}
	return Timestamp{Wall: after.Wall, Logical: base + logicalStep + id}
}

// NodeOf reports which node minted a timestamp, from its tag.
//
// For assertions and for reading a trace. No verdict rests on it: it is a fact
// the SYSTEM writes into the value, so treating it as evidence about the system
// would be the provenance rule's failure with extra steps.
func NodeOf(t Timestamp) uint32 { return t.Logical & logicalMask }

// Clock is the HLC implementation of Source.
//
// It holds no lock. In sim mode a node's logic runs single-threaded off the
// event loop, and in real mode every cross-goroutine interaction enters through
// the mailbox (Amendment A1) -- so a mutex here would be either unnecessary or
// papering over a mailbox violation, and the -race lane is what says which.
type Clock struct {
	phys clock.Clock
	last Timestamp

	// id is this node's ordinal, carried in the low bits of every timestamp
	// this clock mints. See the block above IDBits.
	id uint32

	// updatesRefused counts peers refused for being beyond the envelope. It is
	// evidence, not a verdict: nonzero in a skew run means the envelope check is
	// reachable, and zero in a bounded run means the bound held.
	updatesRefused int

	// physRegressions counts how often the physical clock read BEHIND the last
	// timestamp's wall reading. It is the number that says whether the logical
	// counter is doing any work at all: a run where it is zero never exercised
	// the tie-breaking half of the algorithm.
	physRegressions int
}

// New builds an HLC over a physical clock, tagged with the minting node's
// ordinal.
//
// The ordinal is required rather than defaulted. A defaulted one would make
// every clock in a test cluster share a tag, which is the configuration BUG-021
// is about, and it would do it silently.
func New(phys clock.Clock, node uint32) (*Clock, error) {
	if phys == nil {
		return nil, errors.New("hlc: no physical clock; the HLC wraps Wall and cannot invent one")
	}
	if node > MaxNodeID {
		return nil, fmt.Errorf(
			"hlc: node ordinal %d does not fit in %d bits; two nodes sharing a tag can mint the "+
				"same timestamp, which is BUG-021", node, IDBits)
	}
	return &Clock{phys: phys, id: node}, nil
}

// NewAt builds an HLC that starts at or above `seed`.
//
// # Why a range born from a split needs one
//
// BUG-023. There is one HLC per range, and a split created the child through the
// ordinary constructor — so the child's clock started at ZERO and its first Now()
// returned the local physical wall. The parent's clock is not at the local
// physical wall: it is at the maximum of every timestamp it has issued and every
// peer timestamp it has absorbed, which under skew sits well ahead of local
// physical time.
//
// The child inherited the parent's VERSIONS and none of its CLOCK, so it stamped
// reads and writes below data it already held. A read at such a timestamp
// correctly finds nothing, and the linearizability checker correctly calls it a
// stale read. The store was right; the timestamp was wrong.
//
// Nothing closed that gap on its own: a range's clock advances through Update on
// messages FOR THAT RANGE, and the child's first messages come from its own
// leader, stamped by the same fresh clock. The window did not close, it expired —
// once local physical time passed the parent's last stamp.
//
// `seed` is required to be set. A child with no inherited value is the defect,
// and a constructor that accepted one would be the defect with a nicer name.
func NewAt(phys clock.Clock, node uint32, seed Timestamp) (*Clock, error) {
	c, err := New(phys, node)
	if err != nil {
		return nil, err
	}
	if !seed.IsSet() {
		return nil, errors.New(
			"hlc: a clock seeded from a parent needs the parent's value; an unset seed is BUG-023")
	}
	// Tagged, so the seeded clock's first issue is this node's and above the
	// parent's. The seed itself carries the PARENT's tag and must not be issued.
	c.last = nextTagged(seed, c.id)
	return c, nil
}

// Now returns the next timestamp from this source.
//
// The physical reading is taken ONCE, here, and everything downstream derives
// from that one reading. Taking it twice inside one operation is the same defect
// A4 found six times in a different dimension: a fact derived from a position
// has to be derived at that position, and two reads of a moving clock are two
// positions (DESIGN-A5 section 7).
func (c *Clock) Now() Timestamp {
	phys := Timestamp{Wall: c.phys.Wall()}
	if c.last.Wall < phys.Wall {
		// The physical clock has moved past everything this clock has issued, so
		// the counter restarts -- at this node's slot, not at zero.
		c.last = Timestamp{Wall: phys.Wall, Logical: c.id}
		return c.last
	}
	c.physRegressions++
	c.last = nextTagged(c.last, c.id)
	return c.last
}

// Update folds a received timestamp in, and refuses one beyond the envelope.
func (c *Clock) Update(m Timestamp) error {
	phys := Timestamp{Wall: c.phys.Wall()}

	// The envelope is checked against the PHYSICAL reading, not against c.last.
	//
	// Checking against c.last would compound: once one over-eager timestamp is
	// accepted, the next one is measured against the inflated value and the
	// bound ratchets outward forever. The physical clock is the only thing in
	// this function that is not downstream of a peer's claim.
	if m.Wall.Sub(phys.Wall) > c.phys.MaxOffset() {
		c.updatesRefused++
		return fmt.Errorf("%w: peer at %s, this node at %s, bound %s",
			ErrBeyondEnvelope, m, phys, c.phys.MaxOffset())
	}

	// # One rule, and it keeps the tag
	//
	// The result must exceed everything this clock knows -- what it last issued,
	// and what the peer sent -- and it must carry this node's tag, or the next
	// timestamp it mints could collide with another node's (BUG-021).
	//
	// The four-case version this replaced could produce an untagged value on
	// three of its four paths, so absorbing one peer message was enough to lose
	// the property for every timestamp afterwards.
	lb := Max(c.last, m)
	if lb.Wall < phys.Wall {
		c.last = Timestamp{Wall: phys.Wall, Logical: c.id}
		return nil
	}
	c.last = nextTagged(lb, c.id)
	return nil
}

// MaxOffset is the bound this source assumes.
func (c *Clock) MaxOffset() time.Duration { return c.phys.MaxOffset() }

// Last is the most recent timestamp this clock issued or absorbed.
//
// Reported, not observed: it is the clock's own account of itself, and it exists
// for assertions inside the node and for tests. No oracle verdict may rest on
// it -- the ledger records timestamps as they cross a boundary, exactly as it
// records entries.
func (c *Clock) Last() Timestamp { return c.last }

// UpdatesRefused and PhysicalRegressions report what the run exercised.
func (c *Clock) UpdatesRefused() int      { return c.updatesRefused }
func (c *Clock) PhysicalRegressions() int { return c.physRegressions }

func maxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

var _ Source = (*Clock)(nil)
