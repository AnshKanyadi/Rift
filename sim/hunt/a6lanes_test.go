package hunt_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// lanesSeeds is how many seeds the two reduced-size A6 lanes run.
//
// Reduced deliberately, and both for reasons Ansh set. The unthrottled
// collector writes an entry per apply, which is the shape A5's throttle
// replaced; the overlapped drivers are an interleaving that has never run. A
// full-size sweep of either would cost more than the question is worth, and the
// question in both cases is "what happens at all", not "what happens 10,000
// times".
func lanesSeeds(t *testing.T, def uint64) uint64 {
	t.Helper()
	if v := os.Getenv("LANE_SEEDS"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("LANE_SEEDS: %v", err)
		}
		return n
	}
	return def
}

// TestUnthrottledCollector is the run the A5 sign-off owes A6.
//
// # What it is for
//
// A5 throttled the collector: one collection in flight per range, and the mark
// must move by a quarter of the window before another is worth an entry.
// Without it a leader proposes a collection on essentially every apply. The
// throttle was kept on two conditions, and this is the second: DESIGN-A0 §7
// names it as an idealization, and A6 runs one sweep without it at a reduced
// seed count so that the idealization is MEASURED rather than assumed harmless.
//
// # What it asserts
//
// Everything the ordinary exit run asserts about safety, at a smaller size, plus
// the two numbers the experiment exists to produce: how much more collection
// traffic the unthrottled shape generates, and whether any checker sees a
// difference. A safety violation here would mean the throttle was hiding a
// defect, which is the outcome that would matter most.
func TestUnthrottledCollector(t *testing.T) {
	if testing.Short() {
		t.Skip("the unthrottled-collector lane is not a -short test")
	}
	seeds := lanesSeeds(t, 60)

	opt := hunt.CurrentOptions()
	opt.GCUnthrottled = true
	start := time.Now()
	c, err := hunt.SweepRaftWith(0, seeds, opt)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	t.Logf("unthrottled: %d seeds in %s; %d collections proposed, %d applied, %d versions collected",
		c.Seeds, time.Since(start).Round(time.Millisecond), c.GCProposed, c.GCApplied,
		c.VersionsCollected)

	base, err := hunt.SweepRaftWith(0, seeds, hunt.CurrentOptions())
	if err != nil {
		t.Fatalf("baseline sweep: %v", err)
	}
	t.Logf("throttled:   %d seeds; %d collections proposed, %d applied, %d versions collected",
		base.Seeds, base.GCProposed, base.GCApplied, base.VersionsCollected)
	if base.GCProposed > 0 {
		t.Logf("ratio:       the unthrottled collector proposes %.1fx as many collections",
			float64(c.GCProposed)/float64(base.GCProposed))
	}

	if c.Violations != 0 {
		t.Errorf("SAFETY VIOLATION: %d across %d unthrottled seeds; first at seed %d. The throttle "+
			"was hiding a defect, which is the one outcome that makes this experiment urgent",
			c.Violations, c.Seeds, c.FirstViolation)
	}
	// The experiment has to have BEEN an experiment. If the unthrottled shape
	// proposed no more collections than the throttled one, the switch did
	// nothing and this lane is measuring the baseline twice.
	if c.GCProposed <= base.GCProposed {
		t.Errorf("the unthrottled collector proposed %d collections against the throttled %d. The "+
			"switch changed nothing, so this lane ran the baseline twice and reported it as an "+
			"experiment", c.GCProposed, base.GCProposed)
	}
}

// TestOverlappedDrivers attempts the interleaving DESIGN-A4 §10 records as
// unexercised: a replica move racing an unrelated membership change.
//
// # Why it could not be attempted before
//
// A move's add and somebody else's removal are indistinguishable in a committed
// log. With both drivers live, rebalance-safety blamed churn's removals on
// moves: 252 false violations in 300 seeds (BUG-016).
//
// # What changed
//
// The harness now records its own churn ORDERS, and a move whose removal falls
// inside a window the churn driver also touched is counted as UNATTRIBUTABLE
// rather than judged. That is Amendment A4's third outcome applied to an oracle
// instead of to a linearizability check: the log cannot say whose removal it is,
// so the honest verdict is neither pass nor violation.
//
// # What this lane asserts
//
// That the interleaving is now REACHED -- moves and churn overlap, and some
// moves are unattributable, which is the proof that they overlap -- and that
// nothing else broke. It deliberately does not assert an unattributable RATE: a
// low one means the drivers rarely collide on the same node, which is a fact
// about the schedule and not a property worth pinning.
func TestOverlappedDrivers(t *testing.T) {
	if testing.Short() {
		t.Skip("the overlapped-driver lane is not a -short test")
	}
	seeds := lanesSeeds(t, 60)

	opt := hunt.CurrentOptions()
	opt.OverlapDrivers = true
	c, err := hunt.SweepRaftWith(0, seeds, opt)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	t.Logf("overlapped:  %d seeds; %d moves ordered, %d completed, %d raced an unrelated "+
		"membership change, %d unattributable",
		c.Seeds, c.MovesOrdered, c.MovesCompleted, c.MovesRacingChurn, c.MovesUnattributable)
	t.Logf("verdicts:    pass=%d violation=%d inconclusive=%d", c.Pass, c.Violations, c.Inconclusive)

	if c.Violations != 0 {
		t.Errorf("SAFETY VIOLATION: %d across %d overlapped seeds; first at seed %d",
			c.Violations, c.Seeds, c.FirstViolation)
	}
	if c.MovesRacingChurn == 0 {
		t.Error("no move window contained a membership change the move did not make, so the two " +
			"drivers still do not overlap and this lane is the ordinary sweep with a flag on")
	}
	if c.MovesCompleted == 0 {
		t.Errorf("%d moves ordered under overlap and NONE completed; a stalled move is safe, so "+
			"the oracle is green over a mechanism that never finished", c.MovesOrdered)
	}
}

// TestEnvelopeExperiment is the run that reaches the HLC's refusal.
//
// # Why this is a lane and not part of the sweep
//
// The refusal fires when a peer's timestamp is more than maxOffset ahead of the
// receiving node's physical clock — that is, when the cluster's skew leaves the
// envelope. CLAUDE.md: skew is "bounded by maxOffset in safety runs,
// deliberately exceeding it in envelope experiments." A safety sweep that
// reached this refusal would be a safety sweep whose bounded-skew claim is
// false, so the sweep asserts it at ZERO and this lane asserts it fires.
//
// # Why the sweep's zero is correct and not a dormant mechanism
//
// It is unreachable there by construction, not by luck: every generated hold
// targets 90% of maxOffset and every one has the same sign, so the widest
// pairwise skew a safety plan can produce is 90% of the bound. The refusal is
// short by a tenth of maxOffset. Measured over 200 seeds: 0.
//
// # What the experiment is allowed to conclude
//
// That the refusal fires, and what the cluster does when the assumption every
// A6 bound rests on is false. It is NOT a safety run: outside the envelope,
// uncertainty intervals do not bound anything and snapshot isolation is not
// guaranteed. So safety findings here are REPORTED rather than asserted, which
// is the honest scoping and is also why this is not a place to go looking for
// green.
func TestEnvelopeExperiment(t *testing.T) {
	if testing.Short() {
		t.Skip("the envelope experiment is not a -short test")
	}
	seeds := lanesSeeds(t, 40)

	opt := hunt.CurrentOptions()
	opt.EnvelopeExceeded = true
	c, err := hunt.SweepRaftWith(0, seeds, opt)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	t.Logf("outside the envelope: %d seeds, %d peer timestamps refused for exceeding maxOffset",
		c.Seeds, c.EnvelopeRefusals)
	t.Logf("what it did to the run: %d uncertainty restarts, %d complete audits, "+
		"verdicts pass=%d violation=%d inconclusive=%d",
		c.UncertaintyRestarts, c.AuditsComplete, c.Pass, c.Violations, c.Inconclusive)

	// The one assertion. Everything else is reported.
	if c.EnvelopeRefusals == 0 {
		t.Errorf("no peer timestamp was refused across %d seeds held at 150%% of maxOffset. The "+
			"refusal is the only thing standing between one node's jumped clock and every bound "+
			"in the cluster, and this lane exists because nothing else reaches it", c.Seeds)
	}
	if c.Violations != 0 {
		t.Logf("REPORTED, not asserted: %d safety violations outside the envelope, first at seed "+
			"%d. Outside the assumption these are expected rather than alarming, and the finding "+
			"is what they are, not that they exist", c.Violations, c.FirstViolation)
	}
}
