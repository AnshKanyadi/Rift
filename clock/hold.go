package clock

import (
	"fmt"
	"math"
	"time"
)

// Realization is how a hold reaches its target, and the two are not
// interchangeable in a corpus.
//
// A Step is an NTP correction: discontinuous, wall-only, invisible to timers.
// It is the A5 case -- the HLC must not go backwards across it, and an
// uncertainty interval must widen. A Slew disciplines the oscillator: gradual,
// and therefore visible in the tick rate as well as the wall reading. It is the
// A8 case -- a lease's stasis margin is consumed continuously while the node
// believes its clock is fine.
//
// A bundle that says only "the clocks disagreed by 490ms" cannot tell an
// investigator which of those they are looking at, so the realization is a
// recorded field rather than something inferred from Ramp at read time
// (DESIGN-A0.4 D4 amendment).
type Realization int

const (
	SlewHold Realization = iota
	StepHold
)

func (r Realization) String() string {
	if r == StepHold {
		return "step"
	}
	return "slew"
}

// Hold is one authored hold, in the form it takes in the plan: a pair, a
// fraction of maxOffset, a window, and how fast to get there. Authoring in
// pairwise intent rather than in absolute per-node offsets is what keeps a
// bundle readable and lets the minimizer delete one hold without disturbing
// every other pair the node participates in (DESIGN-A0.4 D4).
type Hold struct {
	A, B int // node labels; the offset is applied to B, relative to A

	// AtFrac is the target skew as a fraction of maxOffset. A fraction rather
	// than nanoseconds so that a plan holding at the boundary keeps holding at
	// the boundary when maxOffset is swept, which is what an envelope
	// experiment varies. Above 1.0 is deliberate: see Envelope.
	AtFrac float64

	// From and To bound the window in which the pair is held at the target.
	// Ramps sit outside the window: ramp in over [From-Ramp, From], hold flat
	// over [From, To], ramp out over [To, To+Ramp]. "Pinned at X for the
	// window" is therefore literally true, and ramp endpoints are distinct
	// from window edges -- which is exactly why the skew checker evaluates at
	// both (D5 amendment).
	From, To Instant
	Ramp     time.Duration

	// Envelope marks a hold as deliberately outside the assumption. It changes
	// the checker's verdict, never the arithmetic.
	Envelope bool
}

// Compile applies h to node B's base timeline and reports how it realized.
//
// The base must be flat and unstepped across the region the hold touches. That
// is deliberate conflict detection rather than a limitation: two holds fighting
// over one node's timeline is an authoring error whose symptom would otherwise
// be a schedule that silently means neither of them.
func (h Hold) Compile(base Timeline, maxOffset time.Duration) (Timeline, Realization, error) {
	if h.From >= h.To {
		return base, SlewHold, fmt.Errorf("clock: hold window is empty: from %d to %d", h.From, h.To)
	}
	if h.Ramp < 0 {
		return base, SlewHold, fmt.Errorf("clock: hold ramp is negative: %s", h.Ramp)
	}
	if math.IsNaN(h.AtFrac) || math.IsInf(h.AtFrac, 0) {
		return base, SlewHold, fmt.Errorf("clock: hold at_frac is not a number: %v", h.AtFrac)
	}
	start := h.From - Instant(h.Ramp)
	if start < 0 {
		return base, SlewHold, fmt.Errorf("clock: hold ramp starts before time zero (from %d, ramp %s)", h.From, h.Ramp)
	}
	if err := h.checkClear(base, start, h.To+Instant(h.Ramp)); err != nil {
		return base, SlewHold, err
	}

	// The one float multiply in the package, at compile time, with nothing to
	// fuse it with and an immediate rounding to integer. Everything downstream
	// of this line is integer arithmetic (DESIGN-A0.4 §4).
	target := int64(math.Round(h.AtFrac * float64(maxOffset)))

	out := Timeline{
		Skew:  append([]Segment(nil), base.Skew...),
		Steps: append([]Step(nil), base.Steps...),
		Boots: append([]Instant(nil), base.Boots...),
	}

	if h.Ramp == 0 {
		// A step: wall-only, so the oscillator and therefore the tick schedule
		// are untouched. That asymmetry is physical, not an implementation
		// convenience -- an NTP step does not change how fast a crystal runs.
		out.Steps = insertStep(out.Steps, Step{At: h.From, Delta: target})
		out.Steps = insertStep(out.Steps, Step{At: h.To, Delta: -target})
		if err := out.Validate(); err != nil {
			return base, StepHold, err
		}
		return out, StepHold, nil
	}

	// A slew: the oscillator itself is disciplined, so the node also ticks
	// fast or slow while the correction is being applied.
	rampNs := int64(h.Ramp)

	// A correction wider than its ramp cannot be slewed. Applying it would
	// need the oscillator to run backwards on the way out, which is not a
	// fault but a different kind of object -- and it is the same reason real
	// implementations rate-limit slewing rather than letting adjtime take
	// arbitrary corrections. The caller's options are a longer ramp or a step,
	// and those are genuinely different experiments (D4 amendment).
	if abs64(target) >= rampNs {
		return base, SlewHold, fmt.Errorf(
			"clock: hold %d->%d cannot slew %dns of correction over a %s ramp; that needs the oscillator to run backwards -- use a ramp longer than %dns, or Ramp: 0 for a step",
			h.A, h.B, target, h.Ramp, abs64(target))
	}

	rate := mulDiv(target, ppb, rampNs)
	out.Skew = append(out.Skew,
		Segment{Start: start, Off: 0, SlopePPB: rate},
		Segment{Start: h.From, Off: target, SlopePPB: 0},
		Segment{Start: h.To, Off: target, SlopePPB: -rate},
		Segment{Start: h.To + Instant(rampNs), Off: 0, SlopePPB: 0},
	)
	if err := out.Validate(); err != nil {
		return base, SlewHold, fmt.Errorf("compiling hold %d->%d: %w", h.A, h.B, err)
	}
	return out, SlewHold, nil
}

// checkClear rejects a base timeline that already says something about the
// window this hold is about to describe.
func (h Hold) checkClear(base Timeline, from, to Instant) error {
	for _, seg := range base.Skew {
		if seg.Start >= from {
			return fmt.Errorf("clock: hold %d->%d over [%d,%d] collides with an existing skew segment at %d",
				h.A, h.B, from, to, seg.Start)
		}
		if seg.SlopePPB != 0 && seg.Start < from {
			// A sloped segment running into the window would make the hold's
			// target depend on when the hold happens to start.
			return fmt.Errorf("clock: hold %d->%d over [%d,%d] starts inside a drifting segment at %d",
				h.A, h.B, from, to, seg.Start)
		}
	}
	for _, s := range base.Steps {
		if s.At >= from && s.At <= to {
			return fmt.Errorf("clock: hold %d->%d over [%d,%d] collides with a step at %d",
				h.A, h.B, from, to, s.At)
		}
	}
	return nil
}

func insertStep(steps []Step, s Step) []Step {
	i := 0
	for i < len(steps) && steps[i].At <= s.At {
		i++
	}
	steps = append(steps, Step{})
	copy(steps[i+1:], steps[i:])
	steps[i] = s
	return steps
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
