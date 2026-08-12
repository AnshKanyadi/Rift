package clock

import (
	"testing"
	"time"
)

const maxOffset = 500 * time.Millisecond

// sampleMaxSkew is the strawman: a checker that samples on a fixed grid, which
// is what anyone writes first and what looks correct in every test built from
// round numbers. It lives in the test file because it is not a thing this
// package should offer.
func sampleMaxSkew(a, b *Timeline, from, to Instant, stride Instant) (time.Duration, Instant) {
	max := time.Duration(-1)
	var at Instant
	for t := from; t <= to; t += stride {
		d := a.Wall(t).Sub(b.Wall(t))
		if d < 0 {
			d = -d
		}
		if d > max {
			max, at = d, t
		}
	}
	return max, at
}

// TestExactCheckerCatchesWhatSamplingMisses is the induced failure the D5
// amendment requires, and the reason the exactness claim is worth anything.
//
// The schedule holds a pair just inside the bound, then ramps to a peak that
// exists only strictly inside the ramp, between the sample points any
// reasonable grid would choose. A sampling checker passes it. The exact checker
// fails it, because the peak sits at a segment breakpoint and breakpoints are
// what it evaluates.
func TestExactCheckerCatchesWhatSamplingMisses(t *testing.T) {
	a := Flat()

	// b already sits at 495ms, just inside the 500ms bound, and takes a brief
	// excursion 6ms higher: 8ms up, 8ms down, peaking at 1337ms. The pair is
	// over the bound only within about 2.7ms of that peak, which no plausible
	// grid lands on -- while the ramp itself stays physical, since a slew
	// wider than its ramp would need the oscillator to run backwards.
	const base = 495 * int64(time.Millisecond)
	const peak = 501 * int64(time.Millisecond)
	const peakDur = 501 * time.Millisecond
	rampStart := Instant(1_329 * int64(time.Millisecond))
	peakAt := Instant(1_337 * int64(time.Millisecond))
	rampEnd := Instant(1_345 * int64(time.Millisecond))

	rate := mulDiv(peak-base, ppb, int64(peakAt-rampStart))
	b := &Timeline{Skew: []Segment{
		{Start: 0, Off: base, SlopePPB: 0},
		{Start: rampStart, Off: base, SlopePPB: rate},
		{Start: peakAt, Off: peak, SlopePPB: -rate},
		{Start: rampEnd, Off: base, SlopePPB: 0},
	}, Epoch: DefaultEpoch}
	if err := b.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	from, to := Instant(0), 2*s

	// The strawman, on a grid a careful person would pick.
	for _, stride := range []Instant{100 * ms, 50 * ms, 25 * ms} {
		got, _ := sampleMaxSkew(a, b, from, to, stride)
		if got > maxOffset {
			t.Fatalf("the fixture is not a counterexample at stride %v: sampling found %d", stride, got)
		}
	}

	// The exact checker.
	rep := Check(a, b, from, to, maxOffset)
	if !rep.Exceeded {
		t.Errorf("exact checker missed the peak: max %d at %d, bound %d", rep.Max, rep.At, rep.Bound)
	}
	if rep.Max != peakDur {
		t.Errorf("exact max = %v at %d, want exactly %v at %d", rep.Max, rep.At, peakDur, peakAt)
	}
	if rep.At != peakAt {
		t.Errorf("exact checker located the peak at %d, want %d", rep.At, peakAt)
	}
}

// TestExactCheckerTakesBothLimitsAtAStep covers the clause a careful-looking
// implementation gets wrong: a step is a discontinuity, so the supremum can be
// attained on the side that is not sampled. Evaluating "at the jump" reads as
// one point and is two.
func TestExactCheckerTakesBothLimitsAtAStep(t *testing.T) {
	a := Flat()

	// b's oscillator ramps up to 400ms ahead over the first second, and is then
	// stepped straight back to zero. The maximum skew therefore exists ONLY as
	// the left limit at the step: at the step instant itself the pair is back
	// in agreement, and every earlier sample is below the peak.
	const ahead = 400 * int64(time.Millisecond)
	const aheadDur = 400 * time.Millisecond
	stepAt := 1 * s

	b := &Timeline{
		Skew: []Segment{
			{Start: 0, Off: 0, SlopePPB: 400_000_000}, // +40%: reaches 400ms at 1s
			{Start: stepAt, Off: ahead, SlopePPB: 0},
		},
		Steps: []Step{{At: stepAt, Delta: -ahead}},
		Epoch: DefaultEpoch,
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	rep := Check(a, b, 0, 2*s, maxOffset)
	if rep.Max != aheadDur {
		t.Errorf("max skew = %v at %d, want %v (the left limit at the step)", rep.Max, rep.At, aheadDur)
	}
	if rep.At != stepAt {
		t.Errorf("max located at %d, want %d", rep.At, stepAt)
	}

	// And the values on the two sides really do differ, so the test is
	// testing what it claims.
	if l, r := b.WallLimit(stepAt), b.Wall(stepAt); l == r {
		t.Errorf("no discontinuity at the step: both sides read %d", l)
	}
}

// TestExactAgreesWithDenseSampling is exit criterion 1: on schedules where a
// dense grid is trustworthy, the exact answer matches it. Exactness is checked
// against an independent oracle rather than asserted.
func TestExactAgreesWithDenseSampling(t *testing.T) {
	a := Drifting(-50)
	b := &Timeline{Skew: []Segment{
		{Start: 0, Off: 0, SlopePPB: 0},
		{Start: 100 * ms, Off: 0, SlopePPB: 400_000},
		{Start: 700 * ms, Off: 240_000, SlopePPB: -900_000},
		{Start: 1200 * ms, Off: -210_000, SlopePPB: 0},
	}, Epoch: DefaultEpoch}
	if err := b.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	from, to := Instant(0), 2*s
	exact, at := MaxSkew(a, b, from, to)
	dense, denseAt := sampleMaxSkew(a, b, from, to, Instant(1_000)) // every microsecond

	if exact < dense {
		t.Errorf("exact max %v at %d is below dense sampling's %v at %d", exact, at, dense, denseAt)
	}
	if exact-dense > time.Microsecond {
		t.Errorf("exact max %v at %d is implausibly above dense sampling's %v at %d", exact, at, dense, denseAt)
	}
}

// TestDemonstrationScheduleA is exit criterion 2: the hold, in both
// realizations. This is the substrate A8 consumes, proven before A8 needs it.
func TestDemonstrationScheduleA(t *testing.T) {
	for _, tc := range []struct {
		name string
		ramp time.Duration
		want Realization
	}{
		{"slew", 2 * time.Second, SlewHold},
		{"step", 0, StepHold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Hold{
				A: 1, B: 2,
				AtFrac: Percent(98),
				From:   10 * s,
				To:     40 * s,
				Ramp:   tc.ramp,
			}
			a := Flat()
			b, realized, err := h.Compile(*Flat(), maxOffset)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if realized != tc.want {
				t.Errorf("realized as %v, want %v", realized, tc.want)
			}

			// 0.98 of 500ms, stated as an integer: the fraction is authored
			// intent, and everything downstream of the compile is nanoseconds.
			const want = 490 * time.Millisecond

			// Inside the window the pair sits exactly at the target, at every
			// breakpoint and in between.
			for at := h.From; at <= h.To; at += 137 * ms {
				if got := b.Wall(at).Sub(a.Wall(at)); got != want {
					t.Fatalf("at %d the pair is %v apart, want %v held", at, got, want)
				}
			}

			rep := Check(a, &b, 0, h.To+2*s, maxOffset)
			if rep.Exceeded {
				t.Errorf("a hold at 0.98 exceeded the bound: %d > %d at %d", rep.Max, rep.Bound, rep.At)
			}
			if rep.Max != want {
				t.Errorf("max skew %v, want exactly %v", rep.Max, want)
			}
			t.Logf("hold %v: max skew %v at %d, bound %v, realization %v",
				tc.name, rep.Max, rep.At, rep.Bound, realized)
		})
	}
}

// TestDemonstrationScheduleB is exit criterion 3: the envelope. The checker
// records the excess rather than failing, because `envelope: true` inverts the
// verdict and not the arithmetic -- the numbers are the experiment.
func TestDemonstrationScheduleB(t *testing.T) {
	h := Hold{
		A: 1, B: 3,
		AtFrac:   Percent(120),
		From:     20 * s,
		To:       30 * s,
		Ramp:     3 * time.Second,
		Envelope: true,
	}
	a := Flat()
	b, realized, err := h.Compile(*Flat(), maxOffset)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rep := Check(a, &b, 0, h.To+2*s, maxOffset)
	if !rep.Exceeded {
		t.Fatalf("an envelope hold at 1.20 did not exceed the bound: %+v", rep)
	}

	const want = 600 * time.Millisecond
	if rep.Max != want {
		t.Errorf("max skew %v, want exactly %v", rep.Max, want)
	}
	if excess := rep.Max - rep.Bound; excess != want-maxOffset {
		t.Errorf("excess %v, want %v", excess, want-maxOffset)
	}
	// Permille rather than a ratio: no float on any path a report is built
	// from, so that a printed number and a hashed number cannot disagree.
	t.Logf("envelope %v: max skew %v at %d, bound %v, excess %v (%d permille of bound)",
		realized, rep.Max, rep.At, rep.Bound, rep.Max-rep.Bound, rep.Max*1000/rep.Bound)
}

// TestHoldsRejectCollisions covers the conflict detection: two holds fighting
// over one node's timeline is an authoring error whose symptom would otherwise
// be a schedule that silently means neither of them.
func TestHoldsRejectCollisions(t *testing.T) {
	base := *Flat()
	first, _, err := Hold{A: 1, B: 2, AtFrac: Percent(50), From: 10 * s, To: 20 * s, Ramp: 2 * time.Second}.
		Compile(base, maxOffset)
	if err != nil {
		t.Fatalf("first hold: %v", err)
	}

	if _, _, err := (Hold{A: 1, B: 2, AtFrac: Percent(70), From: 15 * s, To: 25 * s}).Compile(first, maxOffset); err == nil {
		t.Error("a second overlapping hold on the same node was accepted")
	}

	for _, bad := range []Hold{
		{From: 10 * s, To: 10 * s},                                              // empty window
		{From: 20 * s, To: 10 * s},                                              // reversed
		{From: 1 * s, To: 2 * s, Ramp: 2 * time.Second},                         // ramp starts before zero
		{AtFrac: Percent(98), From: 10 * s, To: 20 * s, Ramp: time.Millisecond}, // slew too fast to be physical
	} {
		if _, _, err := bad.Compile(*Flat(), maxOffset); err == nil {
			t.Errorf("Compile accepted %+v", bad)
		}
	}
}

// TestSlewMovesTicksAndStepDoesNot is the operational difference between the
// two realizations, and the reason a bundle has to record which one it used: a
// slew disciplines the oscillator, so the node also ticks fast while the
// correction is applied; a step is a wall correction that timers never see.
func TestSlewMovesTicksAndStepDoesNot(t *testing.T) {
	const interval = 10 * time.Millisecond
	window := 60 * s

	count := func(tl *Timeline) int64 {
		var n int64
		for at := Instant(0); ; {
			next, _ := tl.NextTick(at, interval)
			if next > window {
				return n
			}
			n++
			at = next
		}
	}

	flat := count(Flat())

	slew, _, err := Hold{A: 1, B: 2, AtFrac: Percent(98), From: 10 * s, To: 40 * s, Ramp: 2 * time.Second}.
		Compile(*Flat(), maxOffset)
	if err != nil {
		t.Fatalf("slew: %v", err)
	}
	step, _, err := Hold{A: 1, B: 2, AtFrac: Percent(98), From: 10 * s, To: 40 * s}.Compile(*Flat(), maxOffset)
	if err != nil {
		t.Fatalf("step: %v", err)
	}

	if got := count(&step); got != flat {
		t.Errorf("a step hold changed the tick count: %d, want %d unchanged", got, flat)
	}
	if got := count(&slew); got != flat {
		// The slew ramps up and back down within the window, so the net tick
		// count returns to nominal -- but the ticks in between are displaced.
		t.Logf("slew tick count %d vs nominal %d", got, flat)
	}

	// The observable difference: mid-hold, the slewed node's monotonic reading
	// has moved ahead of the flat node's, because its oscillator was
	// disciplined. The stepped node's has not.
	at := 20 * s
	flatMono := Flat().Mono(at)
	if step.Mono(at) != flatMono {
		t.Errorf("a step perturbed Mono: %v, want %v", step.Mono(at), flatMono)
	}
	if !slew.Mono(at).After(flatMono) {
		t.Errorf("a slew did not move Mono: %v, want more than %v", slew.Mono(at), flatMono)
	}
}
