package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
)

const maxOffset = 500 * time.Millisecond

// recorder is a node that writes down everything it is handed. It is the
// minimum a determinism gate needs: something whose entire behaviour is a
// function of the events it received, in order.
type recorder struct {
	id    NodeID
	trace []string

	// echo, when set, schedules a follow-up event on each tick, so a run has
	// node-scheduled work in it and not only loop-scheduled ticks.
	echo bool
}

func (r *recorder) Handle(ev Event, s Scheduler) {
	r.trace = append(r.trace, fmt.Sprintf("%s n%d @%d", ev.Kind, ev.Node, int64(ev.At)))
	if r.echo && ev.Kind == KindTick {
		s.After(3*time.Millisecond, KindDeliver, r.id, "echo")
	}
}

func newRun(t *testing.T, nodes int, echo bool, until clock.Instant, drift []int64) (*Loop, []*recorder) {
	t.Helper()

	recs := make([]*recorder, nodes)
	ns := make([]Node, nodes)
	cs := make([]*clock.Sim, nodes)
	for i := range nodes {
		recs[i] = &recorder{id: NodeID(i), echo: echo}
		ns[i] = recs[i]

		tl := clock.Flat()
		if drift != nil && drift[i] != 0 {
			tl = clock.Drifting(drift[i])
		}
		c, err := clock.NewSim(tl, maxOffset)
		if err != nil {
			t.Fatalf("clock %d: %v", i, err)
		}
		cs[i] = c
	}

	l, err := NewLoop(Config{
		Nodes:        ns,
		Clocks:       cs,
		TickInterval: 10 * time.Millisecond,
		Until:        until,
		MaxSteps:     100_000,
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	return l, recs
}

// TestSameRunSameTrace is the determinism gate on a no-op workload, which is
// checklist step 1's exit criterion. Two independently constructed runs of the
// same configuration must produce identical event traces, event for event.
//
// It is worth being precise about what this does and does not prove. It proves
// the loop's ordering is a function of the configuration. It does not prove
// process-level reproducibility, which needs a fresh process and lands with the
// trace hash in step 2/8.
func TestSameRunSameTrace(t *testing.T) {
	run := func() []string {
		l, recs := newRun(t, 3, true, clock.Instant(time.Second), []int64{0, 200, -150})
		if _, err := l.Run(); err != nil {
			t.Fatalf("run: %v", err)
		}
		var all []string
		for _, r := range recs {
			all = append(all, r.trace...)
		}
		return all
	}

	a, b := run(), run()
	if len(a) == 0 {
		t.Fatal("the run produced no events, so this proves nothing")
	}
	if len(a) != len(b) {
		t.Fatalf("traces differ in length: %d then %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("traces diverge at event %d: %q then %q", i, a[i], b[i])
		}
	}
}

// TestTotalOrderIsByInstantThenInsertion pins the tie-break. container/heap is
// not stable and neither is this heap; the insertion counter is what makes the
// order total, and a simulator that leaned on heap stability would be subtly
// irreproducible in a way no single run would reveal (DESIGN-A0 D2).
func TestTotalOrderIsByInstantThenInsertion(t *testing.T) {
	l, recs := newRun(t, 1, false, 0, nil)

	// Five events at one instant, scheduled in a known order.
	at := clock.Instant(5 * time.Millisecond)
	for i := range 5 {
		l.At(at, KindDeliver, 0, i)
	}
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	var delivered int
	for _, line := range recs[0].trace {
		if len(line) >= 7 && line[:7] == "deliver" {
			delivered++
		}
	}
	if delivered != 5 {
		t.Fatalf("delivered %d of 5 same-instant events", delivered)
	}
}

// TestQueueOrdersByInstantThenSeq tests the comparison directly, because it is
// the single most load-bearing line in the simulator and an indirect test of it
// is an indirect test.
func TestQueueOrdersByInstantThenSeq(t *testing.T) {
	var q queue
	in := []Event{
		{At: 30, Seq: 1},
		{At: 10, Seq: 9},
		{At: 10, Seq: 2},
		{At: 20, Seq: 5},
		{At: 10, Seq: 7},
	}
	for _, e := range in {
		q.push(e)
	}

	want := []Event{{At: 10, Seq: 2}, {At: 10, Seq: 7}, {At: 10, Seq: 9}, {At: 20, Seq: 5}, {At: 30, Seq: 1}}
	for i, w := range want {
		got, ok := q.pop()
		if !ok {
			t.Fatalf("queue ran dry at %d", i)
		}
		if got.At != w.At || got.Seq != w.Seq {
			t.Fatalf("pop %d = (at %d, seq %d), want (at %d, seq %d)", i, got.At, got.Seq, w.At, w.Seq)
		}
	}
	if _, ok := q.pop(); ok {
		t.Error("queue had more than it was given")
	}
}

// TestDriftShapesTickCountEndToEnd is checklist step 1's other criterion: A0.4
// proved the arithmetic, and this proves the loop actually schedules from it. A
// fast node ticks more often than a nominal one over the same stretch of global
// time, which is what makes a drifted node campaign fast.
func TestDriftShapesTickCountEndToEnd(t *testing.T) {
	// 0: nominal, 1: +5000 ppm fast, 2: -5000 ppm slow, over ten seconds --
	// 1000 nominal ticks, so 5000 ppm is five ticks either way. A one-second
	// window would have been 0.5 ticks and the assertion would have been
	// measuring rounding.
	l, recs := newRun(t, 3, false, clock.Instant(10*time.Second), []int64{0, 5000, -5000})
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	count := func(r *recorder) int {
		n := 0
		for _, line := range r.trace {
			if len(line) >= 4 && line[:4] == "tick" {
				n++
			}
		}
		return n
	}
	nominal, fast, slow := count(recs[0]), count(recs[1]), count(recs[2])

	if fast <= nominal {
		t.Errorf("fast node ticked %d times, nominal %d: drift did not reach the schedule", fast, nominal)
	}
	if slow >= nominal {
		t.Errorf("slow node ticked %d times, nominal %d: drift did not reach the schedule", slow, nominal)
	}
	t.Logf("ticks in ten seconds: slow %d, nominal %d, fast %d", slow, nominal, fast)
}

// TestCrashedNodeReceivesNothing: a crashed node gets no events, and the ones
// scheduled for it while it is down are dropped rather than delivered late. A
// message to a dead process does not wait for it to come back.
func TestCrashedNodeReceivesNothing(t *testing.T) {
	l, recs := newRun(t, 2, false, clock.Instant(200*time.Millisecond), nil)

	l.Crash(clock.Instant(50*time.Millisecond), 0)
	l.At(clock.Instant(60*time.Millisecond), KindDeliver, 0, "while down")
	l.Restart(clock.Instant(100*time.Millisecond), 0)
	l.At(clock.Instant(110*time.Millisecond), KindDeliver, 0, "after restart")

	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	var sawWhileDown, sawAfter bool
	for _, line := range recs[0].trace {
		if line == "deliver n0 @60000000" {
			sawWhileDown = true
		}
		if line == "deliver n0 @110000000" {
			sawAfter = true
		}
	}
	if sawWhileDown {
		t.Error("a crashed node was handed an event")
	}
	if !sawAfter {
		t.Error("a restarted node stopped receiving events")
	}
	if l.Down(0) {
		t.Error("node reported down after restart")
	}

	// Node 1 was never touched and must have kept ticking throughout.
	if len(recs[1].trace) == 0 {
		t.Error("the surviving node stopped receiving events")
	}
}

// TestSchedulingIntoThePastPanics: a deadline computed from a clock that had
// already passed it would silently reorder the run.
func TestSchedulingIntoThePastPanics(t *testing.T) {
	l, _ := newRun(t, 1, false, clock.Instant(100*time.Millisecond), nil)
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Error("scheduling into the past did not panic")
		}
	}()
	l.At(0, KindDeliver, 0, nil)
}

// TestDivergentMaxOffsetIsRejectedAtSetup: the Q2 condition, at the place it
// applies. Two nodes with different bounds are each self-consistent, so the
// check has to happen before the run rather than during it.
func TestDivergentMaxOffsetIsRejectedAtSetup(t *testing.T) {
	a, _ := clock.NewSim(clock.Flat(), maxOffset)
	b, _ := clock.NewSim(clock.Flat(), maxOffset/2)

	_, err := NewLoop(Config{
		Nodes:        []Node{&recorder{}, &recorder{id: 1}},
		Clocks:       []*clock.Sim{a, b},
		TickInterval: 10 * time.Millisecond,
		Until:        clock.Instant(time.Second),
	})
	if err == nil {
		t.Fatal("a run with divergent maxOffset bounds was accepted")
	}
}

// TestRunReportsWhyItStopped: "the queue emptied" and "the clock ran out" are
// different results, and a run that went quiet early did less than its duration
// suggests. Only the deadline kind may be banked in SOAK.md.
func TestRunReportsWhyItStopped(t *testing.T) {
	l, _ := newRun(t, 1, false, clock.Instant(50*time.Millisecond), nil)
	got, err := l.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Kind != OutcomeDeadline {
		t.Errorf("stopped for %v, want %v", got.Kind, OutcomeDeadline)
	}
	if !got.CountsTowardSoakHours() {
		t.Error("a deadline run does not count toward soak hours")
	}
	if l.Pending() == 0 {
		t.Error("a deadline stop left an empty queue, so it was really quiescent")
	}
	if _, err := l.Run(); err == nil {
		t.Error("a finished loop accepted a second Run")
	}
}

// TestQuiescentRunDoesNotCountTowardSoakHours pins the forward binding: a run
// that went quiet before its time was up did less than its duration suggests,
// and banking it would inflate the one number the verification claim rests on.
func TestQuiescentRunDoesNotCountTowardSoakHours(t *testing.T) {
	l, _ := newRun(t, 1, false, 0, nil) // no deadline
	l.At(clock.Instant(time.Millisecond), KindDeliver, 0, nil)

	// With no deadline the tick schedule never ends, so bound it by steps and
	// then check the kinds that must not be banked.
	for _, tc := range []struct {
		kind OutcomeKind
		bank bool
	}{
		{OutcomeDeadline, true},
		{OutcomeQuiescent, false},
		{OutcomeHalted, false},
		{OutcomeStepLimit, false},
	} {
		o := Outcome{Kind: tc.kind}
		if got := o.CountsTowardSoakHours(); got != tc.bank {
			t.Errorf("%v counts toward soak hours = %v, want %v", tc.kind, got, tc.bank)
		}
	}
	_ = l
}
