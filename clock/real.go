package clock

import "time"

// Real is the production Clock: the host's oscillator, read through the one
// wall-clock call this repository makes.
//
// It is in the determinism pass's core scope like everything else in this
// package, and it takes a per-line hatch rather than living in an excluded
// package, so that every wall-clock touchpoint in the repo is enumerable from
// HATCHES.txt. There is exactly one, below, and a second would fail
// `make hatches`.
type Real struct {
	epoch     time.Time // captured at construction; carries a monotonic reading
	maxOffset time.Duration
}

// NewReal captures this boot's epoch. Mono is measured from it, so a Real's
// monotonic epoch is per-process rather than per-machine-boot -- which is the
// same contract from the node's point of view, since a node that restarts gets
// a new Real (DESIGN-A0.4 D1 amendment).
func NewReal(maxOffset time.Duration) *Real {
	return &Real{epoch: hostNow(), maxOffset: maxOffset}
}

// Mono is elapsed time since this clock was constructed. Sub on two Times both
// carrying monotonic readings uses those readings, so this is immune to wall
// steps exactly as CLOCK_MONOTONIC is.
func (c *Real) Mono() Mono { return Mono(hostNow().Sub(c.epoch)) }

// Wall is the host's estimate of physical time as nanoseconds since the Unix
// epoch. UnixNano is absolute: no location is involved, so no reading here
// depends on the host's timezone.
func (c *Real) Wall() Wall { return Wall(hostNow().UnixNano()) }

func (c *Real) MaxOffset() time.Duration { return c.maxOffset }

// hostNow is the only wall-clock read in Rift.
//
//rift:allow-nondeterminism the repo's single wall-clock read; the real Clock implementation is where physical time necessarily enters, and every other package takes an injected Clock so this stays the one place
func hostNow() time.Time { return time.Now() }

// compile-time assertion: the real clock is a Clock.
var _ Clock = (*Real)(nil)
