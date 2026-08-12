package clock

import (
	"errors"
	"fmt"
	"time"
)

// Instant is global virtual time in nanoseconds: the simulator's coordinate,
// not anything a node can read. Node-visible time is Wall or Mono, and the
// distinction is load-bearing -- a node knows only what its own clock says.
type Instant int64

// Wall is a node's estimate of physical time, in nanoseconds. It is the
// oscillator plus accumulated steps: it moves backwards when a step does, it
// survives a restart, and MaxOffset bounds how far two nodes' Walls may differ.
// Leases, uncertainty intervals and the A5 HLC all read it, and it is the only
// one of the two readings that may cross the wire.
//
// The zero Wall means unset. Simulated wall time is offset by a nonzero epoch
// (see Timeline.Epoch) precisely so that a forgotten field reads as unset
// rather than as the beginning of the run.
type Wall int64

// Mono is elapsed nanoseconds on a node's oscillator since this boot. Drift
// bends it; steps never touch it. Everything that measures a timeout reads it,
// and the tick schedule derives from it.
//
// # The monotonic-leakage bug class
//
// A Mono is meaningful only as a difference between two readings on the same
// node within one boot. Persisting one, sending one on the wire, or comparing
// one across nodes or across a restart is a bug: a lease expiry stored as a
// Mono survives a restart as a number from a timeline that no longer exists,
// and the node then serves reads under a lease it does not hold.
//
// That class is made uncompilable rather than reviewable, in three layers:
//
//  1. Wall and Mono are distinct defined types, so cross-type arithmetic and
//     assignment do not compile and a Mono cannot be passed where a Wall is
//     expected.
//  2. Mono has no encoder. Wall has Nanos and NewWall for the wire codec; Mono
//     has neither, and its MarshalJSON exists only to fail loudly if a
//     reflection-based encoder reaches it anyway.
//  3. determinismcheck rejects a clock.Mono in an exported or tagged struct
//     field outside this package.
//
// The residual gap, stated rather than hidden: because these are defined
// integer types, `a - b` on two Monos still compiles and yields a Mono rather
// than a Duration. Sub is the sanctioned spelling. Closing that too would mean
// making them structs, which costs the comparison operators and every
// constant expression; the ruling said int64, so int64 is what this is.
type Mono int64

// Sub returns the duration from u to t. Instants subtract to durations; that
// they are the same type is what makes the subtraction meaningful.
func (t Wall) Sub(u Wall) time.Duration { return time.Duration(t - u) }

// Add returns t advanced by d, still a Wall.
func (t Wall) Add(d time.Duration) Wall { return t + Wall(d) }

func (t Wall) Before(u Wall) bool { return t < u }
func (t Wall) After(u Wall) bool  { return t > u }
func (t Wall) IsSet() bool        { return t != 0 }

// Nanos is the wire representation of a Wall. It exists on Wall and
// deliberately not on Mono: this is the encoder half of the leakage rule.
func (t Wall) Nanos() int64 { return int64(t) }

// NewWall builds a Wall from nanoseconds, for the decoder side.
func NewWall(ns int64) Wall { return Wall(ns) }

func (t Wall) String() string { return fmt.Sprintf("wall:%dns", int64(t)) }

// Sub returns the elapsed duration from u to t. Both must come from the same
// node and the same boot; nothing here can check that, which is why Mono never
// leaves the node that produced it.
func (t Mono) Sub(u Mono) time.Duration { return time.Duration(t - u) }

// Add returns t advanced by d, still a Mono.
func (t Mono) Add(d time.Duration) Mono { return t + Mono(d) }

func (t Mono) Before(u Mono) bool { return t < u }
func (t Mono) After(u Mono) bool  { return t > u }

func (t Mono) String() string { return fmt.Sprintf("mono:%dns", int64(t)) }

// ErrMonoNotSerializable is returned by Mono.MarshalJSON, which exists only to
// turn a silent success into a loud failure.
var ErrMonoNotSerializable = errors.New(
	"clock: a monotonic reading must never be serialized; its epoch is this boot of this node, so the value means nothing anywhere else -- send a Wall, or send the duration")

// MarshalJSON always fails. Mono has no encoder; this is the tripwire for a
// reflection-based path that the compiler and the vet pass cannot see.
func (t Mono) MarshalJSON() ([]byte, error) { return nil, ErrMonoNotSerializable }

// Clock is a node's view of time: two readings off one oscillator, answering
// different questions (DESIGN-A0.4 D1).
type Clock interface {
	// Mono is elapsed time on this node's oscillator since this boot.
	Mono() Mono

	// Wall is this node's estimate of physical time.
	Wall() Wall

	// MaxOffset is the assumed bound on |Wall_i - Wall_j| across the cluster.
	//
	// It is fixed at construction and immutable for the life of the process.
	// Every consumer of Wall needs the bound in the same breath, and a lease
	// computed against a stale or divergent bound is the failure this
	// placement prevents.
	MaxOffset() time.Duration
}

// AssertUniformMaxOffset requires every node to advertise the same bound, and
// is called by sim setup before a run begins.
//
// # The bug class it closes
//
// Two nodes running with different maxOffset values silently invalidate every
// lease argument and every envelope result in the run. Node A grants itself a
// lease whose stasis margin assumes 500ms of possible disagreement; node B
// believes the bound is 200ms and computes a different validity window; the two
// windows overlap, and the run reports a lease-disjointness violation that is
// an artifact of the harness rather than a bug in the protocol. Worse, it can
// go the other way: a bound that is too generous on one node hides a real
// violation. Nothing downstream can detect either case, because each node is
// individually self-consistent.
//
// So it is checked once, at setup, where it is cheap and unambiguous.
func AssertUniformMaxOffset(clocks ...Clock) error {
	if len(clocks) == 0 {
		return nil
	}
	want := clocks[0].MaxOffset()
	for i, c := range clocks[1:] {
		if got := c.MaxOffset(); got != want {
			return fmt.Errorf(
				"clock: node %d advertises maxOffset %s but node 0 advertises %s; a divergent bound invalidates every lease and envelope argument in the run",
				i+1, got, want)
		}
	}
	return nil
}
