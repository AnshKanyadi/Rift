package hunt_test

import (
	"github.com/anshkanyadi/rift/sim/hunt"
	"testing"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/toy"
)

// TestEveryAssertionMechanismIsInvoked is the lane, and it is the last thing A0
// owes.
//
// **A check that is never invoked is indistinguishable from a check that always
// passes.** The repository has shipped five such mechanisms; three were found by
// an audit that went looking for something else. Nothing in the tree could tell
// the difference, because "the function exists and its own test passes" is
// exactly what all five looked like.
//
// So the claim is made by census rather than by inspection: run a perfectly
// ordinary seed, and require every mechanism registered as every-run to appear in
// the record of what actually executed. Reading the source and satisfying oneself
// that the call is there is how this was missed five times.
func TestEveryAssertionMechanismIsInvoked(t *testing.T) {
	// A completely ordinary run. Nothing about it is arranged to exercise the
	// assertions; if a mechanism does not fire here it does not fire in practice.
	sc := toy.Scenario{Flaw: toy.FlawNone, Placement: toy.PlacementReactive}
	p, err := toy.MaterializeToy(1, sc)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	res, err := hunt.RunToy(p, sc, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	census := res.Counters
	for _, a := range sim.EveryRunAssertions() {
		if n := census.AssertionCount(a.Name); n == 0 {
			t.Errorf("ASSERTION NEVER INVOKED: %s ran zero times on an ordinary run.\n"+
				"  refuses:    %s\n"+
				"  invoked by: %s\n"+
				"  A check that is never invoked is indistinguishable from one that always "+
				"passes, and everything it was supposed to refuse is now unrefused.",
				a.Name, a.Refuses, a.InvokedBy)
		} else {
			t.Logf("%-32s %d", a.Name, n)
		}
	}
}

// TestAssertionRegistryIsWellFormed keeps the registry from rotting into a list
// nobody maintains: every entry classified, named and explained.
//
// The unset kind is rejected rather than defaulted, on the same discipline as
// clock.Hold's realization and the plan's wall epoch -- a mechanism nobody
// classified must not default into being exempt, because that is precisely how
// the five got there.
func TestAssertionRegistryIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	var everyRun, diagnostic int
	for _, a := range sim.Assertions() {
		switch {
		case a.Name == "":
			t.Error("an assertion has no name")
		case seen[a.Name]:
			t.Errorf("assertion %q is registered twice", a.Name)
		case a.Kind == sim.AssertionUnset:
			t.Errorf("assertion %q is unclassified; a mechanism nobody classified must not "+
				"default into being exempt", a.Name)
		case a.Refuses == "":
			t.Errorf("assertion %q does not say what it refuses, so a red lane could not explain "+
				"what stopped being checked", a.Name)
		case a.InvokedBy == "":
			t.Errorf("assertion %q does not say who invokes it", a.Name)
		}
		seen[a.Name] = true
		switch a.Kind {
		case sim.AssertionEveryRun:
			everyRun++
		case sim.AssertionDiagnostic:
			diagnostic++
		case sim.AssertionUnset:
		}
	}
	if everyRun == 0 {
		t.Error("no mechanism is registered as every-run, so the lane asserts nothing")
	}
	t.Logf("%d every-run mechanisms, %d diagnostics", everyRun, diagnostic)
}
