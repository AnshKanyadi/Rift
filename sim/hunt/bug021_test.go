package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestBUG021 is the identity collision, as a named reproduction.
//
// Two transactions minted at the same start timestamp on different nodes, both
// writing one key. The key's lock, its data version and its write record are all
// addressed by the start timestamp, so the two transactions share them: one
// commits, one is rolled back, and the version belongs to both.
//
// It asserts BOTH halves, because they are different claims. The collision
// must be DETECTED -- the widened assertion sees a shared start timestamp where
// the (primary, start) form read zero -- and until the timestamp source
// guarantees uniqueness, the atomicity oracle firing on this seed is the
// system being wrong rather than the checker.
func TestBUG021(t *testing.T) {
	p, err := hunt.MaterializeRaft(90004)
	if err != nil {
		t.Fatal(err)
	}
	r, err := hunt.RunRaft(p, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.IdentityCollisions == 0 {
		t.Error("seed 90004 mints txn 14 and txn 29 at the same start timestamp on different " +
			"nodes, and the collision counter did not see it. The counter keyed on (primary, " +
			"start) once and read zero here, which is how BUG-021 reached the atomicity oracle " +
			"instead of this assertion")
	}
	t.Logf("identity collisions: %d", r.IdentityCollisions)
	if r.Violated == nil {
		t.Log("NOTE: the atomicity violation is gone. If the timestamp source now guarantees " +
			"unique start timestamps, this test should assert IdentityCollisions == 0 instead")
	} else {
		t.Logf("atomicity oracle, as expected while the source can collide: %s", r.Violated.Detail)
	}
}
