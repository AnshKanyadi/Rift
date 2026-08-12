package clock

import (
	"fmt"
	"time"
)

// Sim is a node's Clock in simulation: a Timeline plus the current virtual
// time. It holds no state of its own beyond that instant, so a reading is a
// pure function of (timeline, now) and two runs of the same plan cannot
// diverge here.
//
// The event loop owns virtual time and calls Advance; A0.4 ships the arithmetic
// and A0.6 wires it to the queue (DESIGN-A0.4 Q5).
type Sim struct {
	tl        *Timeline
	now       Instant
	maxOffset time.Duration
}

// NewSim returns a clock over tl. The timeline is validated here rather than
// on first read, so an unusable schedule fails at setup with a message about
// the schedule, not later with a reading nobody can explain.
func NewSim(tl *Timeline, maxOffset time.Duration) (*Sim, error) {
	if err := tl.Validate(); err != nil {
		return nil, err
	}
	if maxOffset < 0 {
		return nil, fmt.Errorf("clock: maxOffset is negative: %s", maxOffset)
	}
	return &Sim{tl: tl, maxOffset: maxOffset}, nil
}

// Advance moves the clock to global virtual time t.
//
// It panics on a backward move. Virtual time is the event loop's monotone
// counter, and a backward step means the loop popped events out of order --
// a harness bug that would otherwise surface much later as a node reading a
// time it had already passed. Node-visible clocks may move backwards; the
// simulator's cursor may not.
func (s *Sim) Advance(t Instant) {
	if t < s.now {
		panic(fmt.Sprintf("clock: virtual time moved backwards, from %d to %d", s.now, t))
	}
	s.now = t
}

// Now is the current global virtual time, for the loop's own bookkeeping. It is
// not a node-visible reading: nodes see Mono and Wall.
func (s *Sim) Now() Instant { return s.now }

// Timeline returns the schedule this clock reads, for checkers.
func (s *Sim) Timeline() *Timeline { return s.tl }

func (s *Sim) Mono() Instant            { return s.tl.Mono(s.now) }
func (s *Sim) Wall() Instant            { return s.tl.Wall(s.now) }
func (s *Sim) MaxOffset() time.Duration { return s.maxOffset }

// NextTick is the global time of this node's next tick after the current
// instant, with its per-boot ordinal.
func (s *Sim) NextTick(interval time.Duration) (Instant, int64) {
	return s.tl.NextTick(s.now, interval)
}

// Flat returns a timeline with no drift, no steps and no restarts: the clock a
// node has when the plan says nothing about it.
func Flat() *Timeline {
	return &Timeline{Skew: []Segment{{Start: 0, Off: 0, SlopePPB: 0}}}
}

// Drifting returns a timeline whose oscillator runs fast or slow by ppm parts
// per million for the whole run. A convenience for tests and for the simplest
// plans; anything with structure is built from Segments or compiled from Holds.
func Drifting(ppm int64) *Timeline {
	return &Timeline{Skew: []Segment{{Start: 0, Off: 0, SlopePPB: ppm * 1000}}}
}

// compile-time assertion: the sim clock is a Clock.
var _ Clock = (*Sim)(nil)
