package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestRaftSmoke drives one seed end to end so the wiring is exercised before the
// sweep spends ten thousand of them on it.
func TestRaftSmoke(t *testing.T) {
	p, err := hunt.MaterializeRaft(1)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	r, err := hunt.RunRaft(p, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("outcome %s", r.Outcome)
	t.Logf("census  %s", r.Census)
	for _, rep := range r.Reports {
		t.Logf("report  %s", rep)
	}
	if r.Violated != nil {
		t.Errorf("violation: %s", r.Violated)
	}
	if r.Census.ElectionsWon == 0 {
		t.Error("no election was won in the whole run; the cluster never had a leader, " +
			"so nothing downstream proves anything")
	}
	_ = sim.VerdictPass
}
