package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestBUG023 pins the seed where a split-born range stamped below its own data.
//
// The history was six operations long and unambiguous: `put "v9"` on k06
// completed at 2.500s, and a `get` beginning at 2.765s returned empty. The read
// was answered by range 14 — born from a split at k06 moments earlier — at a
// timestamp 92ms of wall clock BELOW the write's. MVCC hid the write, correctly,
// because at that timestamp it did not exist.
//
// Both halves of the fix are exercised here: the child seeds from the value the
// split entry carries, and from the maximum timestamp in the records it ingests.
// `store.TestASplitChildInheritsItsParentsClock` covers them separately.
func TestBUG023(t *testing.T) {
	p, err := hunt.MaterializeRaft(12504)
	if err != nil {
		t.Fatal(err)
	}
	r, err := hunt.RunRaft(p, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.Violated != nil {
		t.Errorf("seed 12504 [%s]: %s", r.Violated.Checker, r.Violated.Detail)
	}
	for _, rep := range r.Reports {
		if rep.Verdict.String() == "violation" {
			t.Errorf("seed 12504 report [%s]: %.240s", rep.Checker, rep.Detail)
		}
	}
}
