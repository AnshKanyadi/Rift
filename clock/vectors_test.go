package clock

import (
	"testing"
	"time"

	"github.com/anshkanyadi/rift/internal/sorted"
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
//
// tickSchedules names the input schedules. Construction lives here because it
// speaks the clock package's authoring DSL, and that DSL changes; the expected
// values live in vectors_data_test.go, which no DSL change can reach.
//
// That split is structural rather than disciplinary. The rule is that a vector
// file may change in the construction of its input but never in a want block --
// and a rule enforced by which file a line is in cannot be got wrong by
// accident (DESIGN-A0.4).
var tickSchedules = map[string]struct {
	timeline func() *Timeline
	interval time.Duration
}{
	"drift": {
		timeline: func() *Timeline { return Drifting(200) },
		interval: 10 * time.Millisecond,
	},
	"hold": {
		timeline: func() *Timeline {
			tl, _, err := Hold{A: 1, B: 2, AtPPB: Percent(98), From: 3 * s, To: 9 * s, Ramp: 2 * time.Second, Realize: SlewHold}.
				Compile(*Flat(), 500*time.Millisecond)
			if err != nil {
				panic(err)
			}
			return &tl
		},
		interval: 500 * time.Millisecond,
	},
	"reboot": {
		timeline: func() *Timeline {
			tl := Flat()
			tl.Boots = []Instant{2_500_000_000}
			return tl
		},
		interval: time.Second,
	},
}

func TestKnownAnswersNextTick(t *testing.T) {
	for _, name := range sorted.Keys(tickSchedules) {
		sched := tickSchedules[name]
		want, ok := tickWant[name]
		if !ok {
			t.Fatalf("schedule %q has no recorded vectors", name)
		}
		t.Run(name, func(t *testing.T) {
			tl := sched.timeline()
			if err := tl.Validate(); err != nil {
				t.Fatalf("validate: %v", err)
			}
			at := Instant(0)
			for i, w := range want {
				next, ord := tl.NextTick(at, sched.interval)
				got := tickVector{at: next, ordinal: ord, mono: tl.Mono(next)}
				if got != w {
					t.Fatalf("tick %d: got {at:%d ordinal:%d mono:%d}, want {at:%d ordinal:%d mono:%d}",
						i, got.at, got.ordinal, int64(got.mono), w.at, w.ordinal, int64(w.mono))
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
