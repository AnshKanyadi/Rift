package toy_test

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/toy"
)

// discard is a transport that goes nowhere. These tests are about whether a
// node can be *constructed*, so nothing is ever sent.
type discard struct{}

func (discard) Send(sim.Envelope) {}

// baseConfig is a construction that succeeds, so each test below changes
// exactly one field and the failure is attributable to that field.
func baseConfig() toy.Config {
	return toy.Config{
		ID: 0, Primary: 0, Peers: []sim.NodeID{1, 2},
		Transport:      discard{},
		History:        sim.NewHistory(),
		Flaw:           toy.FlawNone,
		ReplicationRTT: clock.Instant(3_000_000), // 3ms, the default link's upper latency
	}
}

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

	// Constraint 1, the equivalence bound, at its exact edge. One nanosecond
	// below parity is refused; parity itself clears THIS constraint, and is then
	// judged by the reachability one below -- which is the whole point of them
	// being separate checks with separate reasons.
	if err := toy.ValidateWindow(rtt*toy.MinWindowMargin-1, rtt); err == nil {
		t.Error("one nanosecond below parity with the round trip was accepted")
	}

	// Constraint 2, reachability, and the one the re-measured curve showed
	// actually binds. A window at or below the reactive crash delay has closed
	// before the crash lands, so every attempt yields an in-flight operation the
	// checker refuses to score -- 11 per mille at parity against 534 one
	// millisecond above it.
	//
	// A fast network is used here so constraint 1 is satisfied and the refusal
	// can only be constraint 2; otherwise this would prove nothing about which
	// check fired.
	const fast = clock.Instant(1_000_000) // 1ms round trip
	atDelay := toy.CrashDelay()
	if err := toy.ValidateWindow(atDelay, fast); err == nil {
		t.Error("a window exactly equal to the crash delay was accepted; the crash lands after the window has closed")
	} else if !strings.Contains(err.Error(), "crash delay") {
		t.Errorf("the refusal blames the wrong constraint: %v", err)
	} else {
		t.Logf("induced: %v", err)
	}
	if err := toy.ValidateWindow(atDelay+1, fast); err != nil {
		t.Errorf("one nanosecond past the crash delay was refused, but the curve is at full power there: %v", err)
	}
}

// TestNewRefusesAWindowTheFlawCannotManifestIn proves the *wiring*, which is a
// different claim from the one above.
//
// TestWindowValidationIsAGate proves the rule: call ValidateWindow with a narrow
// window and it returns an error. That is a fixture, and a fixture is satisfied
// by a function nothing calls -- which is exactly what this gate was until now.
// The claim that matters is that no toy can come into existence in a regime
// where its own planted flaws are invisible, and only the live constructor can
// establish it. This is the same distinction as a planted violation in live code
// proving an analyzer is wired, versus a testdata fixture proving the rule.
func TestNewRefusesAWindowTheFlawCannotManifestIn(t *testing.T) {
	// The shipped default constructs.
	if _, err := toy.New(baseConfig()); err != nil {
		t.Fatalf("the default configuration was refused: %v", err)
	}

	// Induced through the constructor: the 2ms window the ablation measured at
	// zero detections. Nothing in this test names ValidateWindow.
	cfg := baseConfig()
	cfg.SyncLatency = clock.Instant(2_000_000)
	n, err := toy.New(cfg)
	if err == nil {
		t.Fatal("a toy was constructed with a 2ms fsync against a 3ms round trip; " +
			"every seed run against it would sweep an empty search space")
	}
	if n != nil {
		t.Error("New returned a node alongside its error; a refused construction must yield nothing to run")
	}
	for _, want := range []string{"cannot manifest", "empty search space"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not explain itself; missing %q in: %v", want, err)
		}
	}
	t.Logf("induced through the constructor: %v", err)
}

// TestNewRefusesAnUnsetRoundTrip closes the way past the gate that a default
// would have opened.
//
// If ReplicationRTT defaulted to some chosen constant, every caller that forgot
// it would still construct, and the window would be validated against a number
// nobody measured -- so the gate would pass on every plan and prove nothing. The
// zero value is therefore refused rather than filled in, the same discipline as
// clock.Hold's unset realization and the plan's zero wall epoch.
func TestNewRefusesAnUnsetRoundTrip(t *testing.T) {
	cfg := baseConfig()
	cfg.ReplicationRTT = 0
	if _, err := toy.New(cfg); err == nil {
		t.Fatal("a toy was constructed with no replication round trip; the window gate had nothing to validate against")
	} else {
		t.Logf("induced: %v", err)
	}
}
