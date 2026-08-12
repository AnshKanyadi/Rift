package clock

import (
	"testing"
	"time"
)

const (
	ms = Instant(time.Millisecond)
	s  = Instant(time.Second)
)

// TestOscNeverGoesBackwards is the property every other guarantee rests on:
// solveOsc inverts the oscillator, and the inverse is well defined only because
// the function never decreases. Validate's slope > -1e9 rule is what buys it.
//
// Non-decreasing, deliberately not strictly increasing. A clock running slower
// than real time advances fewer than one nanosecond per nanosecond, so at
// integer granularity it plateaus -- which is the physical truth about a slow
// crystal, not a rounding artifact, and is why solveOsc returns the earliest
// time the target has been reached.
func TestOscNeverGoesBackwards(t *testing.T) {
	tl := &Timeline{Skew: []Segment{
		{Start: 0, Off: 0, SlopePPB: 0},
		{Start: 1 * s, Off: 0, SlopePPB: 500_000_000},                // +50%, absurd but legal
		{Start: 2 * s, Off: 500 * int64(ms), SlopePPB: -999_000_000}, // -99.9%, still forward
	}, Epoch: DefaultEpoch}
	if err := tl.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	prev := tl.osc(0)
	for at := Instant(1); at <= 3*s; at += Instant(997) { // a prime stride, to land off segment edges
		got := tl.osc(at)
		if got < prev {
			t.Fatalf("Osc went backwards at %d: %d after %d", at, got, prev)
		}
		prev = got
	}

	// It plateaus but never stalls: over a long enough span even the 0.001x
	// segment makes progress.
	if a, b := tl.osc(2*s), tl.osc(3*s); b <= a {
		t.Errorf("oscillator stalled permanently: %d at 2s, %d at 3s", a, b)
	}

	// A clock at or above real time advances on every nanosecond, so the
	// plateaus really are a property of slowness rather than of the code.
	fast := Drifting(1000)
	for at := Instant(0); at < 1*ms; at++ {
		if fast.osc(at+1) <= fast.osc(at) {
			t.Fatalf("a fast oscillator plateaued at %d", at)
		}
	}
}

func TestValidateRejectsTheThreeImpossibleSchedules(t *testing.T) {
	cases := []struct {
		name string
		tl   Timeline
	}{
		{"backwards oscillator", Timeline{Skew: []Segment{{Start: 0, SlopePPB: -ppb}}, Epoch: DefaultEpoch}},
		{"discontinuous oscillator", Timeline{Skew: []Segment{
			{Start: 0, Off: 0, SlopePPB: 0},
			{Start: 1 * s, Off: 500 * int64(ms), SlopePPB: 0}, // teleports; belongs in Steps
		}, Epoch: DefaultEpoch}},
		{"unsorted segments", Timeline{Skew: []Segment{
			{Start: 0, SlopePPB: 0},
			{Start: 2 * s, SlopePPB: 0},
			{Start: 1 * s, SlopePPB: 0},
		}, Epoch: DefaultEpoch}},
		{"unsorted steps", Timeline{
			Skew:  []Segment{{Start: 0}},
			Steps: []Step{{At: 2 * s, Delta: 1}, {At: 1 * s, Delta: 1}},
			Epoch: DefaultEpoch,
		}},
		{"zero epoch", Timeline{Skew: []Segment{{Start: 0}}}},
	}
	for _, tc := range cases {
		if err := tc.tl.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", tc.name)
		}
	}
}

// TestDriftShapesTicks is exit criterion 4, and the test that would have caught
// either rejected tick design shipping by mistake: a fixed global cadence
// quantizes drift away entirely, and per-segment rescheduling drifts off across
// boundaries.
func TestDriftShapesTicks(t *testing.T) {
	const interval = 10 * time.Millisecond
	const window = 100 * s

	nominal := Flat()
	fast := Drifting(200)  // +200 ppm
	slow := Drifting(-200) // -200 ppm

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

	base, up, down := count(nominal), count(fast), count(slow)

	// 100s at 10ms is 10_000 ticks; 200 ppm of that is 2 ticks either way.
	wantUp, wantDown := base+2, base-2
	if up != wantUp {
		t.Errorf("+200ppm node ticked %d times, want %d (nominal %d)", up, wantUp, base)
	}
	if down != wantDown {
		t.Errorf("-200ppm node ticked %d times, want %d (nominal %d)", down, wantDown, base)
	}
	if up <= base || down >= base {
		t.Errorf("drift did not shape the tick rate at all: %d/%d/%d", down, base, up)
	}
}

// TestTicksAreExactlyInverted checks the inversion rather than its consequences:
// at every tick, the node's own monotonic reading is the tick ordinal times the
// interval, to the nanosecond. A rescheduling implementation passes this within
// one segment and fails once a segment boundary lands mid-interval.
func TestTicksAreExactlyInverted(t *testing.T) {
	const interval = 7 * time.Millisecond // deliberately not a divisor of the segment edges

	tl := &Timeline{Skew: []Segment{
		{Start: 0, Off: 0, SlopePPB: 0},
		{Start: 1 * s, Off: 0, SlopePPB: 300_000},        // +300 ppm
		{Start: 3 * s, Off: 600_000, SlopePPB: -150_000}, // and back down
		{Start: 5 * s, Off: 300_000, SlopePPB: 0},
	}, Epoch: DefaultEpoch}
	if err := tl.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	at := Instant(0)
	for i := 0; i < 800; i++ {
		next, ord := tl.NextTick(at, interval)
		mono := int64(tl.Mono(next))
		want := ord * int64(interval)

		// Rounding up means the tick fires at the first nanosecond at which the
		// target has been reached, so mono is in [want, want+1ns) of oscillator
		// time -- at most one nanosecond of overshoot, never any undershoot.
		if mono < want || mono > want+1 {
			t.Fatalf("tick %d at %d: mono %d, want %d", ord, next, mono, want)
		}
		at = next
	}
}

// TestRestartResetsMonoNotWall is exit criterion 5 and the direct expression of
// the monotonic-leakage bug class: a Mono value that survived a restart is a
// number from a timeline that no longer exists.
func TestRestartResetsMonoNotWall(t *testing.T) {
	tl := &Timeline{
		Skew:  []Segment{{Start: 0, Off: 0, SlopePPB: 0}},
		Boots: []Instant{10 * s},
		Epoch: DefaultEpoch,
	}
	if err := tl.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	beforeMono, beforeWall := tl.Mono(9*s), tl.Wall(9*s)
	afterMono, afterWall := tl.Mono(11*s), tl.Wall(11*s)

	if beforeMono != Mono(9*s) {
		t.Errorf("mono before restart = %v, want %d", beforeMono, 9*s)
	}
	if afterMono != Mono(1*s) {
		t.Errorf("mono after restart = %v, want %d (epoch is per boot)", afterMono, 1*s)
	}
	if !afterMono.Before(beforeMono) {
		t.Errorf("mono did not reset across the restart: %v then %v", beforeMono, afterMono)
	}
	if !afterWall.After(beforeWall) {
		t.Errorf("wall did not continue across the restart: %v then %v", beforeWall, afterWall)
	}
	if want := DefaultEpoch + Wall(11*s); afterWall != want {
		t.Errorf("wall after restart = %v, want %v; a rebooting machine does not forget the date", afterWall, want)
	}
}

// TestStepsMoveWallNotTimers is the other half of criterion 5, and the reason
// D1 recommends two clocks: under one clock, a backward step would stall the
// election timer for the width of the step.
func TestStepsMoveWallNotTimers(t *testing.T) {
	const interval = 10 * time.Millisecond

	plain := Flat()
	stepped := &Timeline{
		Skew:  []Segment{{Start: 0, Off: 0, SlopePPB: 0}},
		Steps: []Step{{At: 1 * s, Delta: -500 * int64(ms)}, {At: 2 * s, Delta: 300 * int64(ms)}},
		Epoch: DefaultEpoch,
	}
	if err := stepped.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Wall follows the steps.
	if got, want := stepped.Wall(1500*ms), DefaultEpoch+Wall(1000*ms); got != want {
		t.Errorf("wall after backward step = %v, want %v", got, want)
	}
	if got, want := stepped.Wall(2500*ms), DefaultEpoch+Wall(2300*ms); got != want {
		t.Errorf("wall after forward step = %v, want %v", got, want)
	}
	// Mono does not.
	if got, want := stepped.Mono(1500*ms), Mono(1500*ms); got != want {
		t.Errorf("mono was perturbed by a step: %v, want %v", got, want)
	}

	// And neither does the tick schedule.
	at, atPlain := Instant(0), Instant(0)
	for i := 0; i < 400; i++ {
		var ord, ordPlain int64
		at, ord = stepped.NextTick(at, interval)
		atPlain, ordPlain = plain.NextTick(atPlain, interval)
		if at != atPlain || ord != ordPlain {
			t.Fatalf("tick %d diverged under stepping: %d/%d vs %d/%d", i, at, ord, atPlain, ordPlain)
		}
	}
}

// TestReadingsAreOrderIndependent is exit criterion 6's in-process half, and it
// is stronger than repeating a sequence: every reading is a pure function of
// (timeline, t), so evaluating in a shuffled order must produce identical
// results. A cursor or cache that made a reading depend on what was read before
// it would fail here.
//
// The fresh-process half of the criterion needs os/exec, which is orchestration
// and does not belong in a core package; it lands with A0.6's trace-hash gate,
// in the runner that owns process spawning.
func TestReadingsAreOrderIndependent(t *testing.T) {
	tl := &Timeline{
		Skew: []Segment{
			{Start: 0, Off: 0, SlopePPB: 0},
			{Start: 2 * s, Off: 0, SlopePPB: 120_000},
			{Start: 6 * s, Off: 480_000, SlopePPB: 0},
		},
		Steps: []Step{{At: 4 * s, Delta: 250 * int64(ms)}},
		Boots: []Instant{8 * s},
		Epoch: DefaultEpoch,
	}
	if err := tl.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	times := make([]Instant, 0, 512)
	for at := Instant(0); at < 10*s; at += 19 * ms {
		times = append(times, at)
	}

	type reading struct {
		osc  Instant
		mono Mono
		wall Wall
	}
	forward := make([]reading, len(times))
	for i, at := range times {
		forward[i] = reading{tl.osc(at), tl.Mono(at), tl.Wall(at)}
	}
	for i := len(times) - 1; i >= 0; i-- {
		at := times[i]
		got := reading{tl.osc(at), tl.Mono(at), tl.Wall(at)}
		if got != forward[i] {
			t.Fatalf("reading at %d depended on evaluation order: %v then %v", at, forward[i], got)
		}
	}
}

// TestMulDivIsExact pins the integer arithmetic that replaced float64 on the
// evaluation path. The 128-bit intermediate is the whole point: a naive
// a*b/d in int64 overflows here.
func TestMulDivIsExact(t *testing.T) {
	cases := []struct{ a, b, d, want int64 }{
		{100_000, ppb, ppb, 100_000},
		{1, ppb, ppb, 1},
		{-500_000, ppb, ppb, -500_000},
		{3_600_000_000_000, 1_000_000, ppb, 3_600_000_000}, // an hour at 1000 ppm
		{7, 3, 2, 10},                                      // truncation toward zero
		{-7, 3, 2, -10},
	}
	for _, tc := range cases {
		if got := mulDiv(tc.a, tc.b, tc.d); got != tc.want {
			t.Errorf("mulDiv(%d,%d,%d) = %d, want %d", tc.a, tc.b, tc.d, got, tc.want)
		}
	}
	if got := mulDivCeil(7, 3, 2); got != 11 {
		t.Errorf("mulDivCeil(7,3,2) = %d, want 11", got)
	}
}

func TestSimClockAdvanceRejectsGoingBackwards(t *testing.T) {
	c, err := NewSim(Flat(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	c.Advance(5 * s)

	defer func() {
		if recover() == nil {
			t.Error("advancing virtual time backwards did not panic")
		}
	}()
	c.Advance(4 * s)
}

// TestUniformMaxOffsetIsAsserted covers Q2's condition. Two nodes running with
// different bounds are each individually self-consistent, so nothing
// downstream can detect the divergence: it presents as a lease-disjointness
// violation that is an artifact of the harness, or -- worse -- as a real
// violation that a too-generous bound hides.
func TestUniformMaxOffsetIsAsserted(t *testing.T) {
	a, err := NewSim(Flat(), maxOffset)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	b, err := NewSim(Flat(), maxOffset)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := AssertUniformMaxOffset(a, b); err != nil {
		t.Errorf("identical bounds were rejected: %v", err)
	}

	c, err := NewSim(Flat(), maxOffset/2)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := AssertUniformMaxOffset(a, b, c); err == nil {
		t.Error("a divergent bound was accepted; every lease argument in the run would be invalid")
	}

	// The bound is fixed at construction: there is no setter, and the field is
	// unexported, so this is checked by the compiler rather than by this test.
	// What this asserts is that the reading does not drift.
	if got := a.MaxOffset(); got != maxOffset {
		t.Errorf("MaxOffset drifted: %v, want %v", got, maxOffset)
	}
	a.Advance(10 * s)
	if got := a.MaxOffset(); got != maxOffset {
		t.Errorf("MaxOffset changed as time advanced: %v, want %v", got, maxOffset)
	}
}

// TestEnvelopeShapesOffsetsNotTheBound is the other half of Q2: an envelope
// experiment exceeds the bound by moving the true offsets, never by moving the
// bound the nodes advertise. If the bound moved with the experiment, the
// experiment would be vacuous -- nothing would ever be outside it.
func TestEnvelopeShapesOffsetsNotTheBound(t *testing.T) {
	tl, _, err := Hold{A: 1, B: 2, AtFrac: Percent(120), From: 2 * s, To: 4 * s,
		Ramp: 2 * time.Second, Realize: SlewHold, Envelope: true}.Compile(*Flat(), maxOffset)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	c, err := NewSim(&tl, maxOffset)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	c.Advance(3 * s)
	if got := c.MaxOffset(); got != maxOffset {
		t.Errorf("an envelope hold moved the advertised bound to %v; it must stay %v", got, maxOffset)
	}
	rep := Check(Flat(), &tl, 0, 6*s, maxOffset)
	if !rep.Exceeded {
		t.Errorf("the envelope hold did not exceed the bound: %+v", rep)
	}
}
