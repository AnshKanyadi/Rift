package rng

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/internal/sorted"
)

// TestPoisonedPanicsOnEverything: every method must be a tripwire. A single
// method left un-poisoned would be exactly the hole the poison exists to
// close -- a stray draw during plan execution, producing a run that still
// looks deterministic and still passes, but no longer matches the plan it
// claims to execute.
func TestPoisonedPanicsOnEverything(t *testing.T) {
	calls := map[string]func(r Rand){
		"Uint64":    func(r Rand) { r.Uint64() },
		"Uint64N":   func(r Rand) { r.Uint64N(10) },
		"IntN":      func(r Rand) { r.IntN(10) },
		"Shuffle":   func(r Rand) { r.Shuffle(4, func(i, j int) {}) },
		"Perm":      func(r Rand) { r.Perm(4) },
		"Derive":    func(r Rand) { r.Derive("net") },
		"DeriveKey": func(r Rand) { r.DeriveKey("net") },
	}

	// Sorted so failures report in a stable order; ranging a map directly
	// would make this output nondeterministic, which is the habit this repo
	// is trying to build -- and which the determinism pass now enforces here
	// too, since internal/rng executes inside every simulated run.
	for _, name := range sorted.Keys(calls) {
		func() {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Errorf("%s: poisoned Rand did not panic", name)
					return
				}
				msg, ok := rec.(string)
				if !ok {
					t.Errorf("%s: panicked with %T, want string", name, rec)
					return
				}
				// The message has to tell whoever hits it what to do, because
				// they will hit it while debugging something else entirely.
				for _, want := range []string{name, "plan execution", "do not un-poison"} {
					if !strings.Contains(msg, want) {
						t.Errorf("%s: panic message missing %q:\n%s", name, want, msg)
					}
				}
			}()
			calls[name](Poisoned("plan execution"))
		}()
	}
}

// TestPoisonedIsSubstitutable: the poison must satisfy Rand so it can be
// injected anywhere a real generator is accepted, without a build tag or a
// nil check at the call site.
func TestPoisonedIsSubstitutable(t *testing.T) {
	var r Rand = Poisoned("test")
	if r == nil {
		t.Fatal("Poisoned returned nil")
	}
}
