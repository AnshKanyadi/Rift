package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestBUG022 pins the seed where a commit landed underneath an answer already
// given, and it is M71's covering test.
//
// # What it looked like
//
// The audit at 1600000008790243029.0 read all eight accounts and they summed to
// -19. The transfer that lost the money is txn 26, and the key is a00:
//
//	r1 idx=106  txn-get  a00  at 1600000007480000000.1792 -> "-15@4"   (txn 16)
//	r1 idx=107  txn-get  a00  at 1600000007750000000.514  -> "-15@4"   (txn 26)
//	r1 idx=109  prewrite a00  start 1600000007480000000.1792  "4@16"
//	r1 idx=111  commit   a00  start ...1792 -> commit 1600000007630000000.3072
//	r1 idx=112  prewrite a00  start 1600000007750000000.514   "-20@26"
//
// Txn 26 was answered "-15" at 7.75 at index 107. Txn 16 then committed "4" at
// **7.63** — below the timestamp txn 26 had already read at — so txn 26's
// snapshot acquired a commit after the fact, and the balance it wrote was
// computed from a value that was no longer the one at its own timestamp.
//
// # Neither existing guard could fire, and that is the finding
//
// `ErrKeyIsLocked` covers LOG order: it refuses a prewrite arriving while
// somebody's lock stands, which here is the window [109, 111). Txn 26's prewrite
// arrived at 112, one entry after the lock was released.
//
// `ErrWriteConflict` covers TIMESTAMP order: it refuses a prewrite whose
// snapshot predates a commit already recorded. Txn 16's commit timestamp is
// below txn 26's start, so there was nothing to refuse.
//
// The two are total only where log order and timestamp order agree. Percolator
// gets the agreement from a single TSO; per-node HLCs do not give it, and A6's
// clock holds make the gap up to 450ms wide. The third guard is the read mark.
func TestBUG022(t *testing.T) {
	p, err := hunt.MaterializeRaft(2521)
	if err != nil {
		t.Fatal(err)
	}
	r, err := hunt.RunRaft(p, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.Violated != nil {
		t.Errorf("seed 2521 [%s]: %s", r.Violated.Checker, r.Violated.Detail)
	}
	for _, rep := range r.Reports {
		if rep.Verdict.String() == "violation" {
			t.Errorf("seed 2521 report [%s]: %.240s", rep.Checker, rep.Detail)
		}
	}
	// Non-vacuity, both halves. A run in which nothing was ever marked proves
	// nothing about a guard that reads marks, and a run in which the guard never
	// refused reached no interleaving it exists for. The seed is pinned, so
	// these are facts about this schedule rather than hopes about the sweep.
	if r.ReadMarks == 0 {
		t.Error("no read mark was staged on seed 2521, so the guard this seed exists for had " +
			"nothing to consult and the green above is about an empty mechanism")
	}
	if r.ReadConflicts == 0 {
		t.Error("no prewrite was refused by the read mark on seed 2521. This is the seed the " +
			"refusal was derived from; if it no longer refuses, the schedule has moved and the " +
			"pin is stale")
	}
}

// TestBUG024 pins the seed where a transaction mixed two snapshots, and it is
// M73's covering test.
//
// A transaction that restarts above an uncertain commit takes a new start
// timestamp and re-reads. The reads it issued BEFORE the restart are still in
// flight; their answers arrive afterwards carrying the old snapshot, and nothing
// checked which incarnation they belonged to. The transfer then computed its
// writes from two different instants, which conserves nothing — in whichever
// direction the two snapshots happened to differ, which is why seed 10303
// created ten units out of nothing.
//
// It is BUG-020's family: an answer accepted for the wrong incarnation. The
// epoch guard in `store` is the same shape one layer down.
func TestBUG024(t *testing.T) {
	p, err := hunt.MaterializeRaft(10303)
	if err != nil {
		t.Fatal(err)
	}
	r, err := hunt.RunRaft(p, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.Violated != nil {
		t.Errorf("seed 10303 [%s]: %s", r.Violated.Checker, r.Violated.Detail)
	}
	for _, rep := range r.Reports {
		if rep.Verdict.String() == "violation" {
			t.Errorf("seed 10303 report [%s]: %.240s", rep.Checker, rep.Detail)
		}
	}
	if r.StaleIncarnation == 0 {
		t.Error("no read answer from a dead incarnation was rejected on seed 10303, so the guard " +
			"this seed exists for never fired and the green above is vacuous")
	}
}
