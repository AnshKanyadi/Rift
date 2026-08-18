package sim

// The assertion registry: every mechanism in the harness that asserts something,
// classified as either running on every normal run or as a diagnostic with a
// named caller.
//
// # Why this file exists
//
// **A check that is never invoked is indistinguishable from a check that always
// passes.** This repository has shipped five of them:
//
//	toy.ValidateWindow      declared, tested, called by nothing        (fixed)
//	sim.Counters.Check      required by every plan, called by nothing  (fixed)
//	sim.InjClockHold        required by every plan, fired by nothing   (fixed)
//	sim.History.Validate    written to reject unusable histories, called by nothing
//	clock.Check             the exact skew checker, called by no run path
//
// Three of those were found by an audit that went looking for one thing and
// found the mechanism designated to catch it was itself broken. Five instances
// is not a coincidence, and the response is the same one the escape hatches got:
// a checked-in list, diffed against what the tree actually does, so an exemption
// is a conscious edit rather than an omission nobody sees.
//
// The lane that reads this answers **"was it called"**. Whether a mechanism that
// was called still catches anything is the mutant suite's question. Two failure
// modes, two instruments, and conflating them is how a repository ends up with
// checks that run and prove nothing.

// AssertionKind says what obligation a mechanism carries.
type AssertionKind uint8

const (
	// AssertionUnset is the zero value and is rejected: a mechanism nobody
	// classified must not default into being exempt.
	AssertionUnset AssertionKind = iota

	// AssertionEveryRun must be invoked on any ordinary run. The lane fails if
	// the census does not show it.
	AssertionEveryRun

	// AssertionDiagnostic is invoked deliberately by a named caller rather than
	// by every run. It is listed so that being uninvoked-by-default is a written
	// decision with a reason, not an oversight.
	AssertionDiagnostic

	numAssertionKinds
)

func (k AssertionKind) String() string {
	switch k {
	case AssertionUnset:
		return "unset"
	case AssertionEveryRun:
		return "every-run"
	case AssertionDiagnostic:
		return "diagnostic"
	case numAssertionKinds:
		return "invalid"
	}
	return "unknown"
}

// Assertion is one mechanism and its obligation.
type Assertion struct {
	// Name is the census key, and is the mechanism's qualified name so a
	// failure names something greppable.
	Name string
	Kind AssertionKind

	// What the mechanism refuses, in a sentence, so a red lane explains what
	// stopped being checked rather than only that something did.
	Refuses string

	// InvokedBy names the caller. For every-run mechanisms it is where on the
	// run path they sit; for diagnostics it is who is expected to call them.
	InvokedBy string
}

// Assertions is the registry.
func Assertions() []Assertion {
	return []Assertion{
		{
			Name: "plan.Plan.Validate", Kind: AssertionEveryRun,
			Refuses:   "a plan that cannot mean anything: wrong schema version, zero wall epoch, unparseable keys, unknown fault actions, illegal holds",
			InvokedBy: "plan.Build, before anything is constructed from the plan",
		},
		{
			Name: "clock.Timeline.Validate", Kind: AssertionEveryRun,
			Refuses:   "an impossible oscillator schedule",
			InvokedBy: "clock.NewSim, via plan.buildClocks",
		},
		{
			Name: "clock.AssertUniformMaxOffset", Kind: AssertionEveryRun,
			Refuses:   "nodes advertising different maxOffset bounds, which nothing downstream could detect",
			InvokedBy: "sim.NewLoop",
		},
		{
			Name: "toy.ValidateWindow", Kind: AssertionEveryRun,
			Refuses:   "a regime in which the planted flaws cannot manifest, so a clean sweep would be a sweep over an empty search space",
			InvokedBy: "toy.New, which refuses construction",
		},
		{
			Name: "sim.Counters.Check", Kind: AssertionEveryRun,
			Refuses:   "a run that injected less than its plan asserts",
			InvokedBy: "hunt.RunToy, after the run",
		},
		{
			Name: "sim.CheckAll", Kind: AssertionEveryRun,
			Refuses:   "a checker running below its operation floor, which would return green by construction",
			InvokedBy: "hunt.RunToy, after the run",
		},
		{
			Name: "sim.History.Validate", Kind: AssertionEveryRun,
			Refuses:   "a history that cannot be checked being handed to a checker that then returns green",
			InvokedBy: "sim.CheckAll, before any checker sees the history",
		},
		{
			Name: "clock.Check", Kind: AssertionDiagnostic,
			Refuses:   "nothing by itself: it computes the exact skew envelope between two timelines and reports it",
			InvokedBy: "the clock package's skew tests, and the envelope experiment when A8's successor is built. It is an analysis over a pair of timelines rather than a per-run gate, and there is no per-run question it answers -- maxOffset uniformity is asserted separately and is the property a run depends on.",
		},
	}
}

// EveryRunAssertions is the subset the lane enforces.
func EveryRunAssertions() []Assertion {
	var out []Assertion
	for _, a := range Assertions() {
		if a.Kind == AssertionEveryRun {
			out = append(out, a)
		}
	}
	return out
}
