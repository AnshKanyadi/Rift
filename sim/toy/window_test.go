package toy_test

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim/toy"
)

// TestWindowValidationIsAGate induces the assertion's failure, because a gate
// that has only ever passed has demonstrated the cheap half.
//
// The regime it refuses is not a slow one, it is a *blind* one: below the
// margin the flawed toy and the correct toy are behaviourally identical, so a
// thousand green seeds would be a thousand sweeps over an empty search space.
func TestWindowValidationIsAGate(t *testing.T) {
	const rtt = clock.Instant(3_000_000) // 3ms, the default link's upper latency

	// The shipped default must pass its own gate.
	if err := toy.ValidateWindow(toy.DefaultSyncLatency, rtt); err != nil {
		t.Errorf("the default window fails its own validation: %v", err)
	}

	// Induced: the 2ms window the ablation measured at zero detections.
	err := toy.ValidateWindow(clock.Instant(2_000_000), rtt)
	if err == nil {
		t.Fatal("a 2ms fsync window against a 3ms round trip was accepted; that is the regime where the flaw cannot exist")
	}
	for _, want := range []string{"cannot manifest", "empty search space"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not explain itself; missing %q in: %v", want, err)
		}
	}
	t.Logf("induced: %v", err)

	// Exactly at the margin passes; one nanosecond below does not.
	if err := toy.ValidateWindow(rtt*toy.MinWindowMargin, rtt); err != nil {
		t.Errorf("exactly at the margin was refused: %v", err)
	}
	if err := toy.ValidateWindow(rtt*toy.MinWindowMargin-1, rtt); err == nil {
		t.Error("one nanosecond below the margin was accepted")
	}
}
