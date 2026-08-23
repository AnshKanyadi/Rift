package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestBUG024 pins the two seeds where a transaction mixed two snapshots.
//
// A transaction that restarts above an uncertain commit takes a new start
// timestamp and re-reads. The reads it issued BEFORE the restart are still in
// flight; their answers arrive afterwards carrying the old snapshot, and nothing
// checked which incarnation they belonged to. The transfer then computed its
// writes from two different instants, which conserves nothing — in whichever
// direction the two snapshots happened to differ, which is why seed 2521 lost 19
// units and seed 10303 created 10.
func TestBUG024(t *testing.T) {
	for _, seed := range []uint64{2521, 10303} {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatal(err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if r.Violated != nil {
			t.Errorf("seed %d [%s]: %s", seed, r.Violated.Checker, r.Violated.Detail)
		}
		for _, rep := range r.Reports {
			if rep.Verdict.String() == "violation" {
				t.Errorf("seed %d report [%s]: %.200s", seed, rep.Checker, rep.Detail)
			}
		}
	}
}
