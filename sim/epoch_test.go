package sim

import "testing"

// TestEpochGuardRefusesADeadIncarnation induces the guard rather than
// describing it. Three components rediscovered this class before it was made
// unrepresentable; the guard is worthless if it has only ever accepted.
func TestEpochGuardRefusesADeadIncarnation(t *testing.T) {
	g := NewEpochGuard()
	live := g.Current()
	if !g.Accept(live) {
		t.Fatal("the live incarnation was refused")
	}
	if err := g.Check("node"); err != nil {
		t.Fatalf("a clean guard reported a failure: %v", err)
	}

	dead := live
	g.Advance() // crash
	g.Advance() // restart: a distinct incarnation, not a continuation
	if g.Accept(dead) {
		t.Fatal("a completion from a dead incarnation was accepted; this is the class that " +
			"produced TOY-003, the store restart bug and the durability panic")
	}
	if g.Dropped() != 1 {
		t.Errorf("dropped %d, want 1", g.Dropped())
	}
	if err := g.Check("node"); err == nil {
		t.Fatal("a guard that dropped a cross-epoch delivery reported no failure")
	} else {
		t.Logf("induced: %v", err)
	}

	// The zero epoch is "no incarnation" and must never be accepted, so a
	// forgotten stamp is refused rather than defaulting into being accepted.
	if g.Accept(0) {
		t.Error("an unstamped delivery was accepted")
	}
}
