package clock

import (
	"testing"
	"time"
)

// These vectors pin the tick schedule, for the same reason internal/rng pins
// the generator: tick times feed the trace hash, so a change to the inversion
// changes every recorded run. This test failing is not a nuisance, it is the
// alarm working.
//
// Provenance, stated plainly: they are self-generated from this
// implementation. They prove the schedule has not moved since the day they were
// recorded. They are not evidence that any other implementation would produce
// them, and nothing depends on that.
//
// Changing a value here is only ever correct alongside a deliberate, documented
// change to the clock that invalidates the corpus. That is a conversation, not
// a commit.
//
// Three schedules, one per behaviour the design turns on:
//
//	drift  -- a sloped segment, where the inversion has to solve a real
//	          equation rather than divide;
//	hold   -- a compiled hold's ramp/flat/ramp, where ticks cross segment
//	          boundaries and a per-segment rescheduler would drift off;
//	reboot -- a restart, where the monotonic curve resets to zero and the
//	          ordinal restarts with it.
var tickVectors = []struct {
	name     string
	timeline func() *Timeline
	interval time.Duration
	want     []tickVector
}{
	{
		name:     "drift",
		timeline: func() *Timeline { return Drifting(200) },
		interval: 10 * time.Millisecond,
		// A node 200 ppm fast reaches each tick early in global time:
		// 10ms/1.0002 = 9,998,000.4, rounded up.
		want: []tickVector{
			{at: 9_998_001, ordinal: 1, mono: 10_000_000},
			{at: 19_996_001, ordinal: 2, mono: 20_000_000},
			{at: 29_994_002, ordinal: 3, mono: 30_000_000},
			{at: 39_992_002, ordinal: 4, mono: 40_000_000},
			{at: 49_990_002, ordinal: 5, mono: 50_000_000},
		},
	},
	{
		name: "hold",
		timeline: func() *Timeline {
			tl, _, err := Hold{A: 1, B: 2, AtFrac: Percent(98), From: 3 * s, To: 9 * s, Ramp: 2 * time.Second}.
				Compile(*Flat(), 500*time.Millisecond)
			if err != nil {
				panic(err)
			}
			return &tl
		},
		interval: 500 * time.Millisecond,
		// Ticks 1-2 precede the ramp, which starts at 1s. Ticks 3-6 fall
		// inside it, where the oscillator runs at 1.245x and the inversion has
		// to solve rather than divide. Tick 7 is the interesting one: it lands
		// after the ramp ends at 3s, where the oscillator already reads 3.49s,
		// so it fires only 10ms later in global time. A per-segment
		// rescheduler gets that one wrong.
		//
		// Tick 5's mono reads one nanosecond over its target. That is the
		// ceiling rounding, and pinning it is the point of a golden vector.
		want: []tickVector{
			{at: 500_000_000, ordinal: 1, mono: 500_000_000},
			{at: 1_000_000_000, ordinal: 2, mono: 1_000_000_000},
			{at: 1_401_606_426, ordinal: 3, mono: 1_500_000_000},
			{at: 1_803_212_852, ordinal: 4, mono: 2_000_000_000},
			{at: 2_204_819_278, ordinal: 5, mono: 2_500_000_001},
			{at: 2_606_425_703, ordinal: 6, mono: 3_000_000_000},
			{at: 3_010_000_000, ordinal: 7, mono: 3_500_000_000},
			{at: 3_510_000_000, ordinal: 8, mono: 4_000_000_000},
		},
	},
	{
		name: "reboot",
		timeline: func() *Timeline {
			tl := Flat()
			tl.Boots = []Instant{2_500_000_000}
			return tl
		},
		interval: time.Second,
		want: []tickVector{
			{at: 1_000_000_000, ordinal: 1, mono: 1_000_000_000},
			{at: 2_000_000_000, ordinal: 2, mono: 2_000_000_000},
			// The restart at 2.5s cancels the pending tick and the ordinal
			// starts again from one, because the monotonic epoch is per boot.
			{at: 3_500_000_000, ordinal: 1, mono: 1_000_000_000},
			{at: 4_500_000_000, ordinal: 2, mono: 2_000_000_000},
		},
	},
}

type tickVector struct {
	at      Instant
	ordinal int64
	mono    Mono
}

func TestKnownAnswersNextTick(t *testing.T) {
	for _, tc := range tickVectors {
		t.Run(tc.name, func(t *testing.T) {
			tl := tc.timeline()
			if err := tl.Validate(); err != nil {
				t.Fatalf("validate: %v", err)
			}

			at := Instant(0)
			for i, want := range tc.want {
				next, ord := tl.NextTick(at, tc.interval)
				got := tickVector{at: next, ordinal: ord, mono: tl.Mono(next)}
				if got != want {
					t.Fatalf("tick %d: got {at:%d ordinal:%d mono:%d}, want {at:%d ordinal:%d mono:%d}",
						i, got.at, got.ordinal, int64(got.mono), want.at, want.ordinal, int64(want.mono))
				}
				at = next
			}
		})
	}
}

// TestWallEpochIsNonzero pins the other half of the plan constant: a zero Wall
// has to be distinguishable from the start of the run, or a struct field
// somebody forgot to fill reads as a valid instant at the beginning of time.
func TestWallEpochIsNonzero(t *testing.T) {
	if DefaultEpoch == 0 {
		t.Fatal("DefaultEpoch is zero, so an unset Wall is indistinguishable from the start of the run")
	}
	if w := Flat().Wall(0); !w.IsSet() {
		t.Errorf("wall at global time zero reads as unset: %v", w)
	}
	var unset Wall
	if unset.IsSet() {
		t.Error("the zero Wall reports itself as set")
	}
}
