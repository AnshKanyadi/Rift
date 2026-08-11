package rng

import "fmt"

// Poisoned returns a Rand whose every method panics.
//
// It is installed during plan execution, where taking a sequential draw is a
// correctness bug rather than a style violation: a plan is required to be a
// complete reproduction of a run, so any randomness not either materialized in
// the plan or derived from a plan-carried PRF key would make the plan
// unreproducible and, worse, would make delta-debugging unsound -- the
// minimizer would attribute a change in behavior to the fault entry it removed
// when the real cause was a shifted draw.
//
// The failure mode this prevents is quiet. Without the tripwire, a stray draw
// produces a run that is still internally deterministic and still passes, but
// no longer matches the plan it claims to execute; the corpus rots invisibly.
// With it, the run dies at the exact call site.
//
// reason names the context, e.g. "plan execution".
func Poisoned(reason string) Rand { return poisoned{reason: reason} }

type poisoned struct{ reason string }

func (p poisoned) die(method string) {
	panic(fmt.Sprintf(
		"rng: %s called on a poisoned Rand during %s.\n"+
			"No sequential randomness may be drawn here: a plan must reproduce its "+
			"run exactly, with every random quantity either materialized in the plan "+
			"or derived from a plan-carried PRF key (see Key.PRF).\n"+
			"Fix the caller to take its value from the plan; do not un-poison the Rand.",
		method, p.reason))
}

func (p poisoned) Uint64() uint64              { p.die("Uint64"); return 0 }
func (p poisoned) Uint64N(uint64) uint64       { p.die("Uint64N"); return 0 }
func (p poisoned) IntN(int) int                { p.die("IntN"); return 0 }
func (p poisoned) Float64() float64            { p.die("Float64"); return 0 }
func (p poisoned) Bool(float64) bool           { p.die("Bool"); return false }
func (p poisoned) Shuffle(int, func(i, j int)) { p.die("Shuffle") }
func (p poisoned) Perm(int) []int              { p.die("Perm"); return nil }
func (p poisoned) Derive(name string) Rand     { p.die("Derive(" + name + ")"); return nil }
func (p poisoned) DeriveKey(name string) Key   { p.die("DeriveKey(" + name + ")"); return Key{} }

// compile-time assertion: the poison satisfies the same interface, so it can be
// substituted anywhere a real Rand is accepted.
var _ Rand = poisoned{}
