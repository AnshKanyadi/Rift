package checker_test

import (
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
)

func ms(n int64) clock.Instant { return clock.Instant(n * int64(time.Millisecond)) }

// put and get append a completed operation to a history.
func put(h *sim.History, client int, seq uint64, key, val string, call, ret int64) {
	i := h.Begin(ms(call), client, seq, "put", key, val)
	h.End(i, ms(ret), sim.RespOK, "")
}

func get(h *sim.History, client int, seq uint64, key, saw string, call, ret int64) {
	i := h.Begin(ms(call), client, seq, "get", key, "")
	h.End(i, ms(ret), sim.RespOK, saw)
}

// TestLinearizableHistoryPasses is the baseline: a history that is obviously
// correct must be reported so, or every other result here means nothing.
func TestLinearizableHistoryPasses(t *testing.T) {
	h := sim.NewHistory()
	put(h, 1, 1, "a", "one", 0, 10)
	get(h, 2, 1, "a", "one", 20, 30)
	put(h, 1, 2, "a", "two", 40, 50)
	get(h, 2, 2, "a", "two", 60, 70)

	reports := sim.CheckAll(h, checker.NewLinearizability())
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].Verdict != sim.VerdictPass {
		t.Errorf("verdict %v: %s", reports[0].Verdict, reports[0].Detail)
	}
	if reports[0].Consumed != 4 {
		t.Errorf("consumed %d operations, want 4", reports[0].Consumed)
	}
}

// TestNonLinearizableHistoryIsAViolation is the induced failure the gate
// requires: the fixture proves the checker can fail.
//
// The history below is impossible under register semantics. Two writes complete
// in order, and a read strictly after both observes the first -- a value that
// has been overwritten and cannot come back.
func TestNonLinearizableHistoryIsAViolation(t *testing.T) {
	h := sim.NewHistory()
	put(h, 1, 1, "a", "one", 0, 10)
	put(h, 1, 2, "a", "two", 20, 30)
	get(h, 2, 1, "a", "one", 40, 50) // stale read of an overwritten value

	reports := sim.CheckAll(h, checker.NewLinearizability())
	if reports[0].Verdict != sim.VerdictViolation {
		t.Fatalf("a stale read after two completed writes was reported %v: %s",
			reports[0].Verdict, reports[0].Detail)
	}
	if !strings.Contains(reports[0].Detail, "non-linearizable") {
		t.Errorf("violation detail does not say what happened: %q", reports[0].Detail)
	}
	t.Logf("%s", reports[0])
}

// TestEmptyHistoryIsInconclusiveNeverPass is the silent-success failure mode
// the count-not-presence rule exists to catch. A checker handed nothing returns
// green by construction, and that green would be banked as a soak hour.
func TestEmptyHistoryIsInconclusiveNeverPass(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    *sim.History
	}{
		{"nil history", nil},
		{"empty history", sim.NewHistory()},
		{"one operation", func() *sim.History {
			h := sim.NewHistory()
			put(h, 1, 1, "a", "one", 0, 10)
			return h
		}()},
	} {
		reports := sim.CheckAll(tc.h, checker.NewLinearizability())
		if reports[0].Verdict != sim.VerdictInconclusive {
			t.Errorf("%s: verdict %v, want inconclusive", tc.name, reports[0].Verdict)
		}
		if reports[0].Verdict.CountsAsPass() {
			t.Errorf("%s: counted as a pass", tc.name)
		}
		if !strings.Contains(reports[0].Detail, "floor") {
			t.Errorf("%s: detail does not name the floor: %q", tc.name, reports[0].Detail)
		}
	}
}

// TestUnavailabilityIsNotAViolation: a partitioned cluster that stops answering
// is behaving correctly. An oracle that scores that as a safety failure trains
// everyone to ignore it.
func TestUnavailabilityIsNotAViolation(t *testing.T) {
	h := sim.NewHistory()
	put(h, 1, 1, "a", "one", 0, 10)

	// A write that timed out: it may or may not have taken effect.
	i := h.Begin(ms(20), 1, 2, "put", "a", "two")
	h.End(i, ms(30), sim.RespTimeout, "")

	// A read that never returned at all.
	h.Begin(ms(40), 2, 1, "get", "a", "")

	// A later read sees either value, and both worlds are legal.
	get(h, 2, 2, "a", "one", 60, 70)

	reports := sim.CheckAll(h, checker.NewLinearizability())
	if reports[0].Verdict == sim.VerdictViolation {
		t.Errorf("unavailability was scored as a safety violation: %s", reports[0].Detail)
	}
	t.Logf("%s", reports[0])
}

// TestTimeoutIsInconclusiveNotPass pins the third verdict at the place it
// actually arises. A zero timeout forces the search to give up, and giving up
// must never read as green.
func TestTimeoutIsInconclusiveNotPass(t *testing.T) {
	h := sim.NewHistory()
	// Enough concurrent operations that a zero-budget search cannot finish.
	for i := range 30 {
		n := int64(i)
		j := h.Begin(ms(n), i%4, uint64(i), "put", "a", "v")
		h.End(j, ms(n+100), sim.RespOK, "")
	}

	c := checker.NewLinearizability()
	c.Timeout = 1 // one nanosecond: the search cannot complete
	reports := sim.CheckAll(h, c)

	if reports[0].Verdict.CountsAsPass() {
		t.Errorf("a timed-out check counted as a pass: %s", reports[0].Detail)
	}
	if reports[0].Verdict != sim.VerdictInconclusive {
		t.Errorf("verdict %v, want inconclusive", reports[0].Verdict)
	}
	if !strings.Contains(reports[0].Detail, "not a pass") {
		t.Errorf("the detail should say plainly that this is not a pass: %q", reports[0].Detail)
	}
}

// TestSummaryTreatsInconclusiveAsNotClean: the third verdict has to survive
// aggregation, or it dies at the ledger where it matters most.
func TestSummaryTreatsInconclusiveAsNotClean(t *testing.T) {
	cases := []struct {
		reports []sim.Report
		clean   bool
	}{
		{[]sim.Report{{Verdict: sim.VerdictPass}}, true},
		{[]sim.Report{{Verdict: sim.VerdictPass}, {Verdict: sim.VerdictInconclusive}}, false},
		{[]sim.Report{{Verdict: sim.VerdictViolation}}, false},
		{nil, false},
	}
	for i, tc := range cases {
		if got := sim.Summarize(tc.reports).Clean(); got != tc.clean {
			t.Errorf("case %d: clean = %v, want %v", i, got, tc.clean)
		}
	}
}

// TestHistoryCarriesNoWallClock: histories carry simulated-clock instants only.
// A wall-clock timestamp here would be the determinism rule failing in the one
// place we are least likely to notice, because a history is consumed by a
// checker whose output nobody reads when it passes.
func TestHistoryCarriesNoWallClock(t *testing.T) {
	var e sim.HistoryEvent
	// Structural: both instants are clock.Instant, the simulator's coordinate,
	// which no wall clock can produce. If either becomes a time.Time this stops
	// compiling, which is the intent.
	var _ clock.Instant = e.Call
	var _ clock.Instant = e.Return
}
