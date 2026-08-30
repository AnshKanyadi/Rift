package chaos_test

import (
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/chaos"
	"github.com/anshkanyadi/rift/sim"
)

func good() chaos.Run {
	return chaos.Run{
		Counters:    chaos.Counters{Started: 5, Kills: 12, Restarts: 2},
		Ops:         chaos.OpCounters{Issued: 1000, Completed: 940, Failed: 60, Keys: 32},
		Faults:      []chaos.Fault{{At: time.Now(), Kind: "kill", Node: 1}},
		LeaderKills: 4,
		LedTicks:    900,
		Persistent:  true,
		Verdicts:    []chaos.Verdict{{Checker: "linearizability", Outcome: sim.VerdictPass, Consumed: 940}},
	}
}

// TestTheGateCatchesEachWayARunCanNotHaveHappened.
//
// Every arm is a way a chaos run produces a clean verdict while having done
// nothing, and each is checked separately because they are independent: a run
// can start nodes and kill none, kill plenty and serve nothing, or serve
// plenty of nothing.
func TestTheGateCatchesEachWayARunCanNotHaveHappened(t *testing.T) {
	if g := good().Gate(10, 500); len(g.Failures) != 0 {
		t.Fatalf("a healthy run failed the gate: %v.\n"+
			"      Both numbers are required: a gate that only ever fires is as useless as one "+
			"that never does", g.Failures)
	}

	for _, tc := range []struct {
		name string
		want string
		mut  func(*chaos.Run)
	}{
		{"no node started", "does not exist", func(r *chaos.Run) { r.Counters.Started = 0 }},
		{"faults did not land", "DID NOT LAND", func(r *chaos.Run) { r.Counters.Kills = 1 }},
		{"a node died uninvited", "WITHOUT being killed", func(r *chaos.Run) { r.Counters.ExitedOther = 1 }},
		{"history too thin", "too thin", func(r *chaos.Run) { r.Ops.Completed = 3 }},
		{"vacuous by content", "vacuous by content", func(r *chaos.Run) { r.Ops.Keys = 0 }},
		{"accounting disagrees", "disagrees with itself", func(r *chaos.Run) { r.Ops.Issued = 10 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := good()
			tc.mut(&r)
			g := r.Gate(10, 500)
			if len(g.Failures) == 0 {
				t.Fatalf("the gate passed a run that %s", tc.name)
			}
			if !strings.Contains(strings.Join(g.Failures, " "), tc.want) {
				t.Errorf("the gate fired but not for the stated reason: %v", g.Failures)
			}
		})
	}
}

// TestAGreenCarriesItsCaveatInTheSameOutput is the one Ansh asked for before
// the first run rather than after.
//
// A caveat a reader has to go and find is a caveat that travels separately from
// the number it bounds. This asserts the sentence is in the same bytes as the
// result.
func TestAGreenCarriesItsCaveatInTheSameOutput(t *testing.T) {
	r := good()
	var b strings.Builder
	r.Report(&b, r.Gate(10, 500))
	out := b.String()

	for _, want := range []string{
		"nothing was observed under the schedules",
		"ACTUALLY OCCURRED",
		"not a statement that nothing is there",
		"WEAKER CLAIM THAN ANY GREEN TRACK A EVER REPORTED",
		"DIFFERENT",
	} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("a green result did not carry %q in its own output.\n%s", want, out)
		}
	}
}

// TestAViolationPrintsTheWorkflowRatherThanLeavingItToJudgement.
//
// The disposition is a documented workflow, not a decision made in the moment
// -- so it is printed with the violation, at the moment somebody is deciding.
func TestAViolationPrintsTheWorkflowRatherThanLeavingItToJudgement(t *testing.T) {
	r := good()
	r.Verdicts = append(r.Verdicts, chaos.Verdict{
		Checker: "linearizability", Outcome: sim.VerdictViolation, Consumed: 940, Detail: "key k07 non-linearizable",
	})
	var b strings.Builder
	r.Report(&b, r.Gate(10, 500))
	// NORMALISED, because the report is wrapped for humans and a content
	// assertion should not depend on where a line happened to break. The first
	// version of this searched raw output and failed on "SIMULATOR'S FAULT\n
	// MODEL" -- a test failing on its own formatting rather than on content.
	out := strings.Join(strings.Fields(b.String()), " ")

	for _, want := range []string{
		"CAPTURE the history",
		"REPRODUCES IN SIM",
		"SIMULATOR'S FAULT MODEL",
		"NEVER CLOSED BY RE-RUNNING",
		"benchmark section does not run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("a violation report omitted %q:\n%s", want, out)
		}
	}
	// And a violation must NOT also print the green caveat, which would read as
	// reassurance attached to a failure.
	if strings.Contains(out, "GREEN, AND HERE IS WHAT THAT MEANS") {
		t.Error("a run with a violation printed the green caveat")
	}
}

// TestAFailedGateSuppressesTheVerdictsAsReportable.
//
// A checker's opinion about a run that did not occur as described is an opinion
// about nothing, and printing it beside a gate failure invites someone to read
// the verdict and skip the gate.
func TestAFailedGateSuppressesTheVerdictsAsReportable(t *testing.T) {
	r := good()
	r.Counters.Kills = 0
	var b strings.Builder
	r.Report(&b, r.Gate(10, 500))
	out := b.String()
	if !strings.Contains(out, "GATE FAILED") {
		t.Fatal("a gate failure was not announced")
	}
	if !strings.Contains(out, "NOT reportable") {
		t.Error("the report did not say the verdicts are unreportable under a failed gate")
	}
	if strings.Contains(out, "GREEN, AND HERE IS WHAT THAT MEANS") {
		t.Error("a run that failed its gate printed the green caveat")
	}
}

func TestTheFaultLogIsOrderedByTime(t *testing.T) {
	t0 := time.Now()
	r := chaos.Run{Faults: []chaos.Fault{
		{At: t0.Add(3 * time.Second), Kind: "restart", Node: 2},
		{At: t0, Kind: "kill", Node: 1},
		{At: t0.Add(time.Second), Kind: "kill", Node: 2},
	}}
	lines := strings.Split(strings.TrimSpace(r.FaultLog()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "kill") || !strings.Contains(lines[0], "node 1") {
		t.Errorf("the log is not in time order: first line is %q", lines[0])
	}
	if !strings.Contains(lines[2], "restart") {
		t.Errorf("last line is %q", lines[2])
	}
}

// A checker that read nothing is not a checker that agreed.
func TestAGreenCheckerThatConsumedNothingFailsTheGate(t *testing.T) {
	r := good()
	r.Verdicts = []chaos.Verdict{{Checker: "linearizability", Outcome: sim.VerdictPass, Consumed: 0}}
	g := r.Gate(1, 1)
	if len(g.Failures) == 0 {
		t.Fatal("a checker reporting green over zero operations passed the gate")
	}
	var out strings.Builder
	r.Report(&out, g)
	if !strings.Contains(out.String(), "consumed=0") {
		t.Fatalf("the consumed count did not travel to the verdict line:\n%s", out.String())
	}
}

// Every verdict prints, greens included, so the report can distinguish "four
// checkers found nothing" from "three checkers never ran".
func TestGreenVerdictsArePrintedWithTheirConsumedCounts(t *testing.T) {
	r := good()
	r.Verdicts = []chaos.Verdict{
		{Checker: "linearizability", Outcome: sim.VerdictPass, Consumed: 940},
		{Checker: "response-agreement", Outcome: sim.VerdictPass, Consumed: 1000},
	}
	var out strings.Builder
	r.Report(&out, r.Gate(1, 1))
	s := out.String()
	for _, want := range []string{"linearizability", "consumed=940", "response-agreement", "consumed=1000"} {
		if !strings.Contains(s, want) {
			t.Fatalf("report omitted %q:\n%s", want, s)
		}
	}
}

// A response for an operation nobody issued breaks the correlation the history
// rests on, so it is a statement about the RECORD and it gates.
func TestAnUnissuedResponseFailsTheGate(t *testing.T) {
	r := good()
	r.Corr = chaos.Correlation{Unissued: 1}
	g := r.Gate(1, 1)
	if len(g.Failures) == 0 {
		t.Fatal("a response for an operation nobody issued passed the gate")
	}
	var out strings.Builder
	r.Report(&out, g)
	if !strings.Contains(out.String(), "unissued=1") {
		t.Fatalf("the correlation counters did not reach the report:\n%s", out.String())
	}
}

// Wire weather is reported and does not gate: a lossy, reordering wire with
// retries produces agreeing duplicates legitimately.
func TestAgreeingDuplicatesAreReportedAndDoNotGate(t *testing.T) {
	r := good()
	r.Corr = chaos.Correlation{Duplicate: 7, LateAfterTimeout: 3}
	if g := r.Gate(1, 1); len(g.Failures) != 0 {
		t.Fatalf("wire weather failed the gate: %v", g.Failures)
	}
	var out strings.Builder
	r.Report(&out, r.Gate(1, 1))
	if !strings.Contains(out.String(), "duplicate=7") || !strings.Contains(out.String(), "late-after-timeout=3") {
		t.Fatalf("wire weather was not reported:\n%s", out.String())
	}
}

// Amendment A4, enforced in the report rather than remembered.
//
// An inconclusive is not a pass. It must not print the green block, it must not
// be folded into the violation count, and its remedy must be the one A4 names.
func TestAnInconclusiveIsNeitherGreenNorAViolation(t *testing.T) {
	r := good()
	r.Verdicts = []chaos.Verdict{{
		Checker: "linearizability", Outcome: sim.VerdictInconclusive, Consumed: 900,
		Detail: "key k03 did not finish checking within 1s",
	}}
	var b strings.Builder
	r.Report(&b, r.Gate(1, 1))
	out := strings.Join(strings.Fields(b.String()), " ")

	if strings.Contains(out, "GREEN, AND HERE IS WHAT THAT MEANS") {
		t.Fatalf("an inconclusive printed the green block:\n%s", out)
	}
	if !strings.Contains(out, "0 violation(s), 1 inconclusive") {
		t.Fatalf("the inconclusive was not quoted beside the violation count:\n%s", out)
	}
	if !strings.Contains(out, "never a longer timeout") {
		t.Fatalf("the report did not name A4's remedy:\n%s", out)
	}
}

// The inconclusive count prints even at zero, so a reader never learns to stop
// looking for it.
func TestTheInconclusiveCountIsQuotedEvenAtZero(t *testing.T) {
	var b strings.Builder
	r := good()
	r.Report(&b, r.Gate(1, 1))
	if !strings.Contains(b.String(), "0 inconclusive") {
		t.Fatalf("a clean run omitted the inconclusive count:\n%s", b.String())
	}
}

// A cluster that never elected a leader never did anything, so its green is a
// statement about an experiment that did not run.
func TestAnUnledClusterFailsTheGate(t *testing.T) {
	r := good()
	r.LedTicks = 0
	if len(r.Gate(1, 1).Failures) == 0 {
		t.Fatal("a run in which no node ever led passed the gate")
	}
}

// A kill schedule that never removed a leader is a gentler experiment than the
// one being reported.
func TestKillsThatNeverHitALeaderFailTheGate(t *testing.T) {
	r := good()
	r.LeaderKills = 0
	g := r.Gate(1, 1)
	if len(g.Failures) == 0 {
		t.Fatal("a schedule that never killed a leader passed the gate")
	}
	var b strings.Builder
	r.Report(&b, g)
	if !strings.Contains(b.String(), "leader-kills=0") {
		t.Fatalf("the leader-kill count did not reach the report:\n%s", b.String())
	}
}

// More aims than signals means a kill was aimed at a node already down.
func TestMoreLeaderKillsThanKillsFailsTheGate(t *testing.T) {
	r := good()
	r.LeaderKills = r.Counters.Kills + 1
	if len(r.Gate(1, 1).Failures) == 0 {
		t.Fatal("a run reporting more leader kills than kills passed the gate")
	}
}

// A verdict from an unobserved cluster is a checker's opinion about a run nobody
// watched.
func TestVerdictsFromAnUnobservedRunFailTheGate(t *testing.T) {
	r := good()
	r.Unobserved = true
	g := r.Gate(1, 1)
	if len(g.Failures) == 0 {
		t.Fatal("a run with the ledger off reported verdicts and passed the gate")
	}
	// And an unobserved run with NO verdicts is legitimate: that is how a
	// benchmark measures anything.
	r2 := good()
	r2.Unobserved, r2.Verdicts = true, nil
	if f := r2.Gate(1, 1).Failures; len(f) != 0 {
		t.Fatalf("an unobserved run claiming nothing failed the gate: %v", f)
	}
}

// A restart on a non-persistent engine is not a crash; it is a fresh node
// wearing an existing identity.
func TestRestartsOnANonPersistentEngineFailTheGate(t *testing.T) {
	r := good()
	r.Persistent = false
	g := r.Gate(1, 1)
	if len(g.Failures) == 0 {
		t.Fatal("a restart schedule on an in-memory engine passed the gate")
	}
	if !strings.Contains(strings.Join(g.Failures, " "), "amnesia") {
		t.Fatalf("the failure does not name what went wrong: %v", g.Failures)
	}
	// A run with NO restarts is fine on any engine: nothing was asked to recover.
	r2 := good()
	r2.Persistent = false
	r2.Counters.Restarts = 0
	if f := r2.Gate(1, 1).Failures; len(f) != 0 {
		t.Fatalf("a run with no restarts failed on the persistence arm: %v", f)
	}
}
