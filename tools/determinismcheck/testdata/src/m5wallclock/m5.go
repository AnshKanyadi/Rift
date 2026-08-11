// Package m5wallclock is the M5-wall-clock mutant from DESIGN-A0 §5, reduced to
// the part that matters: the toy's fixed primary arms its retry deadline from
// the wall clock instead of the injected Clock.
//
// The mutant table gives M5 a budget of "immediate" and names two catchers, the
// determinism pass and the trace-identity gate. This fixture is the first of
// them, and it is the reason sim/toy is in the core scope: the mutant has to
// die at compile time, before a single seed is spent on it.
package m5wallclock

import "time"

type primary struct {
	deadline time.Time
	timeout  time.Duration
	acked    uint64
}

func (p *primary) armRetry() {
	p.deadline = time.Now().Add(p.timeout) // want `time: time.Now`
}

func (p *primary) retryDue() bool {
	return time.Since(p.deadline) > 0 // want `time: time.Since`
}

func (p *primary) waitForBackups() {
	time.Sleep(p.timeout) // want `time: time.Sleep`
	p.acked++
}
