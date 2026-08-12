package clock

import (
	"fmt"
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
	// RealizeUnset is the zero value and is rejected. A hold's realization is
	// authored, never inferred from Ramp: ddmin has to be able to convert one
	// into the other and ask whether the bug survives, and a field derived from
	// another field cannot be flipped independently. Rejecting the zero value
	// is the same discipline as the nonzero wall epoch -- a forgotten field
	// must not read as a decision.
	RealizeUnset Realization = iota
	SlewHold
	StepHold
)

func (r Realization) String() string {
	switch r {
	case StepHold:
		return "step"
	case SlewHold:
		return "slew"
	case RealizeUnset:
		return "unset"
	}
	return "unknown"
}

// Hold is one authored hold, in the form it takes in the plan: a pair, a
// fraction of maxOffset, a window, and how fast to get there. Authoring in
// pairwise intent rather than in absolute per-node offsets is what keeps a
// bundle readable and lets the minimizer delete one hold without disturbing
// every other pair the node participates in (DESIGN-A0.4 D4).
type Hold struct {
	A, B int // node labels; the offset is applied to B, relative to A

	// AtPPB is the target skew as parts per billion of maxOffset: 980_000_000
	// is 0.98 of the bound. A fraction of the bound rather than a count of
	// nanoseconds, so that a plan holding at the boundary keeps holding at the
	// boundary when maxOffset is swept, which is what an envelope experiment
	// varies. Above 1e9 is deliberate: see Envelope.
	//
	// An integer, and that is a ruling rather than a preference. The plan IS
	// replay identity; a fraction that survived into the serialized plan would
	// be multiplied on the replaying machine, and off + slope*(t-start) is
	// exactly the multiply-add an arm64 fuses and an amd64 without FMA does
	// not. Authoring may take a fraction; compile materializes it here.
	AtPPB int64

	// From and To bound the window in which the pair is held at the target.
	// Ramps sit outside the window: ramp in over [From-Ramp, From], hold flat
	// over [From, To], ramp out over [To, To+Ramp]. "Pinned at X for the
	// window" is therefore literally true, and ramp endpoints are distinct
	// from window edges -- which is exactly why the skew checker evaluates at
	// both (D5 amendment).
	From, To Instant
	Ramp     time.Duration

	// Realize says how the hold reaches its target, and is authored rather than
	// inferred. A step is a discontinuous NTP correction, invisible to timers;
	// a slew disciplines the oscillator and is therefore visible in the tick
	// rate too. ddmin flips this field and asks whether the bug survives, which
	// it cannot do to a value derived from Ramp.
	Realize Realization

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
	if h.Realize == RealizeUnset {
		return base, RealizeUnset, fmt.Errorf(
			"clock: hold %d->%d does not say how it realizes; set Realize to StepHold or SlewHold rather than leaving it to be inferred from Ramp", h.A, h.B)
	}
	if h.From >= h.To {
		return base, SlewHold, fmt.Errorf("clock: hold window is empty: from %d to %d", h.From, h.To)
	}
	if h.Ramp < 0 {
		return base, SlewHold, fmt.Errorf("clock: hold ramp is negative: %s", h.Ramp)
	}
	start := h.From - Instant(h.Ramp)
	if start < 0 {
		return base, SlewHold, fmt.Errorf("clock: hold ramp starts before time zero (from %d, ramp %s)", h.From, h.Ramp)
	}
	baseOff, err := h.baseOffsetFor(base, start, h.To+Instant(h.Ramp))
	if err != nil {
		return base, SlewHold, err
	}

	// Integer throughout: target = maxOffset * AtPPB / 1e9, with a 128-bit
	// intermediate. No float appears on this path at all.
	target := mulDiv(int64(maxOffset), h.AtPPB, ppb)

	out := Timeline{
		Skew:  append([]Segment(nil), base.Skew...),
		Steps: append([]Step(nil), base.Steps...),
		Boots: append([]Instant(nil), base.Boots...),
		Epoch: base.Epoch,
	}

	if h.Realize == StepHold {
		// A step: wall-only, so the oscillator and therefore the tick schedule
		// are untouched. That asymmetry is physical, not an implementation
		// convenience -- an NTP step does not change how fast a crystal runs.
		// Ramp is ignored here rather than rejected, so that ddmin can flip a
		// slew to a step without also having to rewrite the ramp.
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
	if rampNs <= 0 {
		return base, SlewHold, fmt.Errorf(
			"clock: hold %d->%d asks to slew with a zero ramp; a slew is a rate and needs a window, use StepHold for an instant correction", h.A, h.B)
	}

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
	ramp := []Segment{
		{Start: start, Off: baseOff, SlopePPB: rate},
		{Start: h.From, Off: baseOff + target, SlopePPB: 0},
		{Start: h.To, Off: baseOff + target, SlopePPB: -rate},
		{Start: h.To + Instant(rampNs), Off: baseOff, SlopePPB: 0},
	}
	// A ramp beginning exactly where the base's last segment begins replaces
	// it rather than sitting beside it: two segments at one instant is not a
	// schedule, it is a tie the evaluator would have to break.
	if last := len(out.Skew) - 1; last >= 0 && out.Skew[last].Start == ramp[0].Start {
		out.Skew = out.Skew[:last]
	}
	out.Skew = append(out.Skew, ramp...)
	if err := out.Validate(); err != nil {
		return base, SlewHold, fmt.Errorf("compiling hold %d->%d: %w", h.A, h.B, err)
	}
	return out, SlewHold, nil
}

// baseOffsetFor returns the constant offset the base holds across the window a
// hold is about to describe, and rejects a base that already says something
// there.
//
// The condition is "no structure in the window", not "no segments at all": the
// segment in force at the start is the base's own, and a hold layered on a
// constant offset is perfectly meaningful. What is rejected is a segment
// beginning inside the window, or a sloped one running through it -- either
// would make the hold's target depend on when the hold happens to start, and
// two holds fighting over one node's timeline is an authoring error whose
// symptom would otherwise be a schedule that silently means neither of them.
func (h Hold) baseOffsetFor(base Timeline, from, to Instant) (int64, error) {
	inForce := base.Skew[0]
	for _, seg := range base.Skew {
		if seg.Start <= from {
			inForce = seg
			continue
		}
		if seg.Start <= to {
			return 0, fmt.Errorf("clock: hold %d->%d over [%d,%d] collides with a skew segment beginning at %d",
				h.A, h.B, from, to, seg.Start)
		}
	}
	if inForce.SlopePPB != 0 {
		return 0, fmt.Errorf("clock: hold %d->%d over [%d,%d] starts inside a drifting segment at %d; its target would depend on when the hold begins",
			h.A, h.B, from, to, inForce.Start)
	}
	for _, s := range base.Steps {
		if s.At >= from && s.At <= to {
			return 0, fmt.Errorf("clock: hold %d->%d over [%d,%d] collides with a step at %d",
				h.A, h.B, from, to, s.At)
		}
	}
	return offsetIn(inForce, from), nil
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
