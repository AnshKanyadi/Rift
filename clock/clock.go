package clock

import "time"

// Instant is nanoseconds on a single node's timeline.
//
// It is deliberately not time.Time: no location, no embedded monotonic reading,
// no formatting, and no way to accidentally compare two nodes' instants as if
// they shared a timeline. Arithmetic on instants is ordinary integer
// arithmetic, which is what makes a run reproduce bit for bit on any machine
// (DESIGN-A0.4 Q1, and the §4 refinement on why no float64 appears below).
type Instant int64

// Add returns t advanced by d.
func (t Instant) Add(d time.Duration) Instant { return t + Instant(d) }

// Sub returns the duration from u to t.
func (t Instant) Sub(u Instant) time.Duration { return time.Duration(t - u) }

// Clock is a node's view of time. Both readings come from one oscillator and
// they answer different questions, which is the whole point of there being two
// (DESIGN-A0.4 D1).
type Clock interface {
	// Mono is elapsed nanoseconds on this node's oscillator since this boot.
	// Strictly increasing within a boot; unaffected by wall-clock steps.
	// Everything that measures a timeout reads this.
	//
	// The epoch is per-boot, so a Mono value is meaningful only as a
	// difference between two readings on the same node within one boot. It
	// must never be persisted, sent on the wire, or compared across nodes or
	// across a restart -- see the monotonic-leakage bug class in DESIGN-A0.4.
	Mono() Instant

	// Wall is this node's estimate of physical time: the oscillator plus
	// accumulated steps. It moves backwards when a step does, and it survives
	// a restart, because a rebooting machine does not forget what time it is.
	// Everything bounded by MaxOffset -- leases, uncertainty intervals, the
	// HLC in A5 -- reads this.
	Wall() Instant

	// MaxOffset is the assumed bound on |Wall_i - Wall_j| across the cluster.
	// It lives here rather than in a config someone else holds, because every
	// consumer of Wall needs the bound in the same breath, and a lease
	// computed against a stale bound is the bug this prevents (Q2).
	MaxOffset() time.Duration
}
