package sim

import (
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
)

func tracedRun(t *testing.T, seed int64, perturb bool) *Trace {
	t.Helper()
	rec := &recorder{}
	c, err := clock.NewSim(clock.Flat(), maxOffset)
	if err != nil {
		t.Fatalf("clock: %v", err)
	}
	tr := NewTrace(0)
	l, err := NewLoop(Config{
		Nodes: []Node{rec}, Clocks: []*clock.Sim{c},
		TickInterval: 10 * time.Millisecond,
		Until:        clock.Instant(200 * time.Millisecond),
		MaxSteps:     10_000,
		Trace:        tr,
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	// A perturbed run inserts one extra event partway through, which is what a
	// changed plan looks like from the trace's point of view.
	if perturb {
		l.At(clock.Instant(105*time.Millisecond), KindDeliver, 0, []byte("perturbation"))
	}
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	return tr
}

// TestTraceIsIdenticalForIdenticalRuns is the in-process half of the gate.
func TestTraceIsIdenticalForIdenticalRuns(t *testing.T) {
	a, b := tracedRun(t, 1, false), tracedRun(t, 1, false)
	if a.Sum() != b.Sum() {
		t.Fatalf("two identical runs produced different hashes:\n  %s\n  %s", a.Sum(), b.Sum())
	}
	if len(a.Steps()) == 0 {
		t.Fatal("the run recorded no steps, so this proves nothing")
	}
	t.Logf("hash: %s over %d steps", a.Short(), len(a.Steps()))
}

// TestTraceDetectsAPerturbedRun is the induced failure: a gate that has only
// ever agreed has demonstrated the cheap half.
func TestTraceDetectsAPerturbedRun(t *testing.T) {
	clean, dirty := tracedRun(t, 1, false), tracedRun(t, 1, true)
	if clean.Sum() == dirty.Sum() {
		t.Fatal("inserting an event did not change the trace hash; the hash is not covering the run")
	}

	i := FirstDivergence(clean.Steps(), dirty.Steps())
	if i < 0 {
		t.Fatal("hashes differ but no divergent step was found")
	}
	t.Logf("%s", DivergenceReport(clean.Steps(), dirty.Steps()))
}

// TestTraceExcludesWhatVariesBetweenIdenticalRuns pins the exclusions. A gate
// that failed for reasons other than divergence would be ignored within a week.
func TestTraceExcludesWhatVariesBetweenIdenticalRuns(t *testing.T) {
	a := NewTrace(0)
	b := NewTrace(0)

	// Same logical event, different payload identity: two distinct byte slices
	// with the same contents must hash the same.
	ev1 := Event{At: 5, Kind: KindDeliver, Node: 1, Payload: []byte("abc")}
	ev2 := Event{At: 5, Kind: KindDeliver, Node: 1, Payload: []byte("abc")}
	a.Step(1, ev1)
	b.Step(1, ev2)

	if a.Sum() != b.Sum() {
		t.Error("two events with identical contents hashed differently; the hash is covering identity rather than content")
	}
}
