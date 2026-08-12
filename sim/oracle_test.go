package sim

import (
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
)

// tripwire fires a violation once the run reaches a chosen step. It stands in
// for a real safety oracle: what is under test here is the wiring, not the
// judgement.
type tripwire struct {
	at    uint64
	kinds []Kind
	seen  int
}

func (o *tripwire) Name() string { return "tripwire" }

func (o *tripwire) Interested(k Kind) bool {
	if o.kinds == nil {
		return true
	}
	for _, want := range o.kinds {
		if k == want {
			return true
		}
	}
	return false
}

func (o *tripwire) OnStep(v View, ev Event) *Violation {
	o.seen++
	if v.Steps() < o.at {
		return nil
	}
	return &Violation{Checker: o.Name(), Detail: "planted violation for the wiring test"}
}

// TestPlantedViolationHaltsTheRun is the second half of the oracle gate: the
// fixture proves the checker can fail, and this proves the wiring carries a
// failure all the way to an outcome an investigator can act on.
func TestPlantedViolationHaltsTheRun(t *testing.T) {
	rec := &recorder{}
	c, err := clock.NewSim(clock.Flat(), maxOffset)
	if err != nil {
		t.Fatalf("clock: %v", err)
	}

	l, err := NewLoop(Config{
		Nodes:        []Node{rec},
		Clocks:       []*clock.Sim{c},
		TickInterval: 10 * time.Millisecond,
		Until:        clock.Instant(time.Second),
		MaxSteps:     10_000,
		Oracles:      []Oracle{&tripwire{at: 5}},
		PlanRef:      "seed 4242 / plan.json",
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}

	out, err := l.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Kind != OutcomeHalted {
		t.Fatalf("outcome %v, want %v", out.Kind, OutcomeHalted)
	}
	if out.CountsTowardSoakHours() {
		t.Error("a halted run counted toward soak hours; a violation is a result, not a duration")
	}
	if l.Violation() == nil {
		t.Fatal("the loop halted without recording a violation")
	}

	// The dump has to be usable, which means naming the plan. A dump that says
	// a checker fired without saying which plan produced it is a bug report
	// nobody can reproduce.
	dump := l.Dump()
	for _, want := range []string{"VIOLATION", "seed 4242 / plan.json", "step", "census"} {
		if !strings.Contains(dump, want) {
			t.Errorf("dump is missing %q:\n%s", want, dump)
		}
	}
	t.Logf("\n%s", dump)
}

// TestOracleHaltsAtTheFirstViolation: first, not last. A checker firing after
// the system has already gone wrong is reporting a consequence, and an
// investigator wants the cause.
func TestOracleHaltsAtTheFirstViolation(t *testing.T) {
	rec := &recorder{}
	c, _ := clock.NewSim(clock.Flat(), maxOffset)
	trip := &tripwire{at: 3}

	l, err := NewLoop(Config{
		Nodes: []Node{rec}, Clocks: []*clock.Sim{c},
		TickInterval: 10 * time.Millisecond,
		Until:        clock.Instant(time.Second),
		MaxSteps:     10_000,
		Oracles:      []Oracle{trip},
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	out, _ := l.Run()

	if out.Steps != 3 {
		t.Errorf("halted after %d steps, want 3: the run kept going past the first violation", out.Steps)
	}
	if l.Violation().Step != 3 {
		t.Errorf("violation recorded at step %d, want 3", l.Violation().Step)
	}
}

// TestUninterestedOraclesAreNotCalled: expensive checkers subscribe to the
// kinds they care about, because running every checker on every step is the
// dominant cost in most simulators of this shape (DR-18).
func TestUninterestedOraclesAreNotCalled(t *testing.T) {
	rec := &recorder{}
	c, _ := clock.NewSim(clock.Flat(), maxOffset)
	// Interested only in deliveries; this run produces only ticks.
	trip := &tripwire{at: ^uint64(0), kinds: []Kind{KindDeliver}}

	l, err := NewLoop(Config{
		Nodes: []Node{rec}, Clocks: []*clock.Sim{c},
		TickInterval: 10 * time.Millisecond,
		Until:        clock.Instant(100 * time.Millisecond),
		MaxSteps:     10_000,
		Oracles:      []Oracle{trip},
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if trip.seen != 0 {
		t.Errorf("an uninterested oracle was called %d times", trip.seen)
	}
}

// TestVerdictUnsetIsNotAPass: a checker that forgot to set a verdict must not
// read as green. Same discipline as the unset hold realization and the zero
// wall epoch -- a forgotten field is never a decision.
func TestVerdictUnsetIsNotAPass(t *testing.T) {
	if VerdictUnset.CountsAsPass() {
		t.Error("the zero verdict counts as a pass")
	}
	for _, v := range []Verdict{VerdictViolation, VerdictInconclusive} {
		if v.CountsAsPass() {
			t.Errorf("%v counts as a pass", v)
		}
	}
	if !VerdictPass.CountsAsPass() {
		t.Error("pass does not count as a pass")
	}
}

// forgetful returns no verdict at all, which CheckAll must convert to
// inconclusive rather than let default to the zero value.
type forgetful struct{}

func (forgetful) Name() string          { return "forgetful" }
func (forgetful) MinOps() int           { return 1 }
func (forgetful) Check(*History) Report { return Report{} }

func TestCheckerWithNoVerdictIsInconclusive(t *testing.T) {
	h := NewHistory()
	i := h.Begin(0, 1, 1, "put", "a", "1")
	h.End(i, 10, RespOK, "")

	reports := CheckAll(h, forgetful{})
	if reports[0].Verdict != VerdictInconclusive {
		t.Errorf("a checker returning no verdict was reported %v", reports[0].Verdict)
	}
}

// TestHistoryRejectsAnUnrecordedOutcome: a returned operation with no outcome
// cannot be checked, and a malformed history must fail loudly rather than be
// handed to a checker that returns green.
func TestHistoryRejectsAnUnrecordedOutcome(t *testing.T) {
	h := NewHistory()
	h.Begin(0, 1, 1, "put", "a", "1")
	h.events[0].Return = 10 // returned, but no outcome recorded

	if err := h.Validate(); err == nil {
		t.Error("a returned operation with no outcome was accepted")
	}
}

// TestHaltedRunBanksNoSoakHours answers the question directly, at the single
// site the policy lives. A violation is a result, not a duration; banking it
// would credit the ledger with time spent discovering that the system is
// broken.
func TestHaltedRunBanksNoSoakHours(t *testing.T) {
	for _, tc := range []struct {
		kind OutcomeKind
		bank bool
	}{
		{OutcomeDeadline, true},
		{OutcomeQuiescent, false},
		{OutcomeHalted, false},
		{OutcomeStepLimit, false},
	} {
		if got := (Outcome{Kind: tc.kind}).CountsTowardSoakHours(); got != tc.bank {
			t.Errorf("%v banks soak hours = %v, want %v", tc.kind, got, tc.bank)
		}
	}
}
