package hunt_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/sim/toy"
)

// wallTimer measures how long a hunt took.
//
// This is the one wall-clock read in the simulator's own packages, and it is a
// *report* metric: it measures the harness from outside every run, never enters
// a plan, a history or the trace hash, and no simulated behaviour depends on
// it. The reader asked for seeds-per-second, and seeds-per-second cannot be
// computed from virtual time.
func wallTimer() func() time.Duration {
	s := time.Now()
	return func() time.Duration { return time.Since(s) }
}

// runSeed materializes a plan from a seed, prepares the scenario's faults into
// it, and runs the toy against it.
//
// Every step goes through the exported driver rather than through a copy local
// to this file, which is the point: `simctl` runs the identical code against the
// identical generator config, so a bundle cut from a violation found here
// replays to the same verdict there.
func runSeed(t *testing.T, seed uint64, sc hunt.Scenario) hunt.Result {
	t.Helper()
	r, err := trySeed(seed, sc)
	if err != nil {
		t.Fatalf("seed %d: %v", seed, err)
	}
	return r
}

// trySeed is runSeed without the fatal, for the ablation, which sweeps windows
// the gate legitimately refuses.
func trySeed(seed uint64, sc hunt.Scenario) (hunt.Result, error) {
	p, err := hunt.MaterializeToy(seed, sc)
	if err != nil {
		return hunt.Result{}, err
	}
	return hunt.RunToy(p, sc, nil)
}

// reactive is the scenario every test here uses unless it is measuring
// placement itself.
func reactive(flaw toy.Flaw) hunt.Scenario {
	return hunt.Scenario{Flaw: flaw, Placement: hunt.PlacementReactive}
}

// TestToySurvivesOneThousandSeeds is checklist step 7's exit run.
//
// A clean sweep here is evidence about the *harness*, not yet about the
// checker: it says the machinery runs a thousand distinct fault schedules
// without falling over. What it does not say is that the checker can catch
// anything, which is what TestBrokenToyIsCaughtByAHunt is for.
func TestToySurvivesOneThousandSeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("1k seeds is not a -short test")
	}

	const seeds int64 = 1000
	elapsedSince := wallTimer()

	var census [8]int
	var pass, violation, inconclusive int
	var inconclusiveWhy []string
	var totalOps int

	for seed := uint64(0); seed < uint64(seeds); seed++ {
		r := runSeed(t, seed, reactive(toy.FlawNone))
		census[r.Outcome.Kind]++

		for _, rep := range r.Reports {
			totalOps += rep.Consumed
			switch rep.Verdict {
			case sim.VerdictPass:
				pass++
			case sim.VerdictViolation:
				violation++
				t.Errorf("seed %d: %s", seed, rep)
			case sim.VerdictInconclusive:
				inconclusive++
				if len(inconclusiveWhy) < 10 {
					inconclusiveWhy = append(inconclusiveWhy,
						fmt.Sprintf("seed %d: %s", seed, rep.Detail))
				}
			case sim.VerdictUnset:
				t.Errorf("seed %d: checker returned no verdict", seed)
			}
		}
	}

	elapsed := elapsedSince()

	t.Logf("seeds:        %d", seeds)
	t.Logf("wall time:    %s (%d seeds/sec)", elapsed.Round(time.Millisecond),
		seeds*int64(time.Second)/int64(elapsed))
	t.Logf("outcomes:     deadline=%d quiescent=%d halted=%d step-limit=%d",
		census[sim.OutcomeDeadline], census[sim.OutcomeQuiescent],
		census[sim.OutcomeHalted], census[sim.OutcomeStepLimit])
	t.Logf("verdicts:     pass=%d violation=%d inconclusive=%d", pass, violation, inconclusive)
	t.Logf("checked ops:  %d total, %d per seed", totalOps, totalOps/int(seeds))

	for _, why := range inconclusiveWhy {
		t.Logf("inconclusive: %s", why)
	}

	// An inconclusive rate above a few percent is diagnosed now rather than
	// after A1, and the remedy is always a smaller problem -- shorter windows,
	// harder per-key partitioning -- never a looser checker.
	// Per-mille integers, like every other rate in this repo.
	if perMille := inconclusive * 1000 / int(seeds); perMille > 30 {
		t.Errorf("inconclusive rate %d per mille is above the 30 threshold and needs diagnosis before A1", perMille)
	}
}

// TestBrokenToyIsCaughtByAHunt is the result that matters: a knowingly
// incorrect toy, found by sweeping seeds rather than by a hand-built fixture.
//
// The fixture in sim/checker proves the checker can return a violation for a
// history somebody constructed to be non-linearizable. This proves something
// different and stronger: that the harness -- plan generation, fault injection,
// transport, engine, history collection and the checker together -- surfaces a
// real defect in a real implementation from nothing but a seed.
func TestBrokenToyIsCaughtByAHunt(t *testing.T) {
	if testing.Short() {
		t.Skip("a hunt is not a -short test")
	}

	// Whether a flaw is *observable* is a property of the toy's client
	// interface, not of the checker. Recorded per flaw rather than assumed,
	// because a mutant that cannot be seen is a gap in the harness and has to
	// be visible as one.
	for _, tc := range []struct {
		name       string
		flaw       toy.Flaw
		failover   bool
		observable bool
		why        string
	}{
		{"ack-before-sync", toy.FlawAckBeforeSync, false, true,
			"an acknowledged write is lost when the primary crashes before its own fsync, and a later read of the same key sees the old value"},

		// The gap, kept as a row rather than deleted. It is still true that
		// without failover this flaw cannot be seen, and the row below is what
		// makes that statement falsifiable instead of folklore.
		{"ack-before-replicate/no-failover", toy.FlawAckBeforeReplicate, false, false,
			"reads are served only by the primary and there is no failover, so a write missing from the backups is invisible to every client; closing this needs promotion in the toy, not a change to the checker"},

		// The gap closed. Same flaw, same checker, same seeds -- the only thing
		// that changed is that a backup can be promoted, which is precisely what
		// the recorded gap said was missing.
		{"ack-before-replicate/failover", toy.FlawAckBeforeReplicate, true, true,
			"the primary dies after acknowledging a write a backup never received, and the promoted backup serves a read of that key"},

		// The two classes the harness found in the correct toy this cycle. They
		// are rows here, with their own seeds-to-detection, because Amendment A2
		// says the moment a fix lands is the only moment we have a precise
		// description of the blind spot -- and a bug class with no standing
		// measurement drifts back into being uncatchable without anyone noticing.
		{"dirty-read", toy.FlawDirtyRead, false, true,
			"a read observes a write that is neither durable nor acknowledged, and the crash that follows takes it back"},
		{"ack-counting", toy.FlawAckCounting, true, true,
			"one backup's duplicated acknowledgement satisfies the whole quorum, so the promoted replica is missing a write the client was told had succeeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const seeds = 1000
			elapsedSince := wallTimer()

			sc := hunt.Scenario{Flaw: tc.flaw, Placement: hunt.PlacementReactive, Failover: tc.failover}
			sweep := sweepSeeds(t, seeds, sc)
			elapsed := elapsedSince()

			t.Logf("flaw %s (failover=%t): %d of %d seeds caught it in %s (%d inconclusive)",
				tc.flaw, tc.failover, sweep.caught, seeds, elapsed.Round(time.Millisecond), sweep.inconclusive)
			if sweep.detected {
				t.Logf("first catch: seed %d -- %s", sweep.first, sweep.detail)
				t.Logf("seeds-to-detection: %d", sweep.seedsToDetection())
			}

			switch {
			case tc.observable && sweep.caught == 0:
				t.Errorf("a knowingly broken toy (%s, failover=%t) survived %d seeds; the harness cannot find a real defect",
					tc.flaw, tc.failover, seeds)
			case !tc.observable && sweep.caught > 0:
				t.Errorf("%s (failover=%t) was recorded as unobservable but %d seeds caught it; update the record",
					tc.flaw, tc.failover, sweep.caught)
			case !tc.observable:
				t.Logf("RECORDED GAP: %s is not observable here -- %s", tc.flaw, tc.why)
			}
		})
	}
}

// TestFailoverDoesNotManufactureViolations is the other half of promotion, and
// the half that is easy to skip.
//
// Adding a failover path adds a way for the *harness* to produce a
// non-linearizable history: promote too eagerly, lose a write the old primary
// had legitimately committed, and every seed reports a violation that belongs to
// the promotion mechanism rather than to the toy. That is the
// harness-manufactured-violation class, and at three seeds in a thousand it
// would be indistinguishable from a real find.
//
// So the correct toy must stay clean across exactly the schedule that catches
// the broken one.
func TestFailoverDoesNotManufactureViolations(t *testing.T) {
	if testing.Short() {
		t.Skip("1k seeds is not a -short test")
	}

	const seeds = 1000
	sc := hunt.Scenario{Flaw: toy.FlawNone, Placement: hunt.PlacementReactive, Failover: true}
	sweep := sweepSeeds(t, seeds, sc)

	t.Logf("correct toy under failover: %d violations, %d inconclusive over %d seeds",
		sweep.caught, sweep.inconclusive, seeds)
	if sweep.caught > 0 {
		t.Errorf("promotion manufactured %d violations in a toy with no flaw; the first is seed %d -- %s",
			sweep.caught, sweep.first, sweep.detail)
	}
}

// TestAblationCrashPlacementAndWindow answers two questions the original remedy
// bundled together, and keeps them separate.
//
// The fix that first caught ack-before-sync did two things at once: it made the
// crash reactive on the unsynced window, and it widened the modelled fsync from
// 2ms to 50ms. Two independent claims came out of that, and neither is free:
//
//  1. **The window.** Swept below. It is a *limitation*, not a detector setting:
//     at 2ms fsync completes before a replication round trip, so a primary
//     awaiting backup acks is already durable when it answers and the flawed toy
//     is behaviourally identical to the correct one. There is nothing in
//     existence to detect.
//
//  2. **The placement.** Reactive targeting is complexity, and complexity has to
//     earn its place. The uniform cell is the null hypothesis: the same crash,
//     aimed at the same node, for the same duration, at a uniformly drawn
//     instant instead of at the window. If uniform detects comparably, reactive
//     targeting is unproven complexity and should go.
func TestAblationCrashPlacementAndWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("an ablation is not a -short test")
	}

	const seeds = 1000
	for _, cell := range []struct {
		placement hunt.Placement
		window    clock.Instant
	}{
		{hunt.PlacementReactive, 2_000_000},
		{hunt.PlacementReactive, 10_000_000},
		{hunt.PlacementReactive, 50_000_000},
		{hunt.PlacementUniform, 50_000_000},
	} {
		sc := hunt.Scenario{
			Flaw:        toy.FlawAckBeforeSync,
			Placement:   cell.placement,
			SyncLatency: cell.window,
		}
		sweep := sweepSeeds(t, seeds, sc)

		// Detected is a distinguishable state, not a negative seed. An in-band
		// magic number in a field that otherwise holds a seed is exactly what
		// Mono's zero value and Hold's rejected realization exist to avoid, and
		// a -1 could be averaged by a future aggregation without complaint.
		firstStr := "not detected"
		if sweep.detected {
			firstStr = fmt.Sprintf("seeds-to-detection %d (first at seed %d)", sweep.seedsToDetection(), sweep.first)
		}
		t.Logf("%-8s placement, fsync %6dus: %3d of %4d eligible caught (%d refused as blind), %s",
			cell.placement, int64(cell.window)/1000, sweep.caught, sweep.eligible, sweep.refused, firstStr)
	}

	t.Log("read the two 50000us rows against each other: that pair is the placement ablation.")
	t.Log("the three reactive rows are the window curve, and the refused column is the gate")
	t.Log("working: a seed whose network is fast relative to the modelled fsync has no")
	t.Log("ack-before-durable flaw in it to find, so it is excluded rather than counted as a miss")
}

// sweep is one 1000-seed measurement.
type sweep struct {
	caught       int
	inconclusive int
	detected     bool
	first        uint64
	detail       string

	// eligible is the seeds whose own network was slow enough relative to the
	// modelled fsync for the flaw to exist at all; refused is the rest.
	//
	// The denominator of a detection rate is the eligible count, never the total.
	// Per-seed link latencies vary, so a window that is productive on a fast
	// seed's network is blind on a slow one's -- and averaging the two reports a
	// weaker harness than the one we have, over a population that includes runs
	// which could not have found anything.
	eligible int
	refused  int
}

// seedsToDetection is how many seeds had to run before the first catch. Seeds
// are zero-based and this is a count, so it is the first seed plus one -- the
// number that answers "how long would a hunt have taken".
func (s sweep) seedsToDetection() uint64 { return s.first + 1 }

func sweepSeeds(t *testing.T, seeds uint64, sc hunt.Scenario) sweep {
	t.Helper()
	var out sweep
	for seed := uint64(0); seed < seeds; seed++ {
		r, err := trySeed(seed, sc)
		switch {
		case errors.Is(err, toy.ErrWindowTooNarrow):
			// Not a failure and not a pass: this seed's network was fast enough
			// relative to the modelled fsync that the flaw does not exist in it,
			// so there was nothing here to find.
			out.refused++
			continue
		case err != nil:
			t.Fatalf("seed %d: %v", seed, err)
		}
		out.eligible++

		for _, rep := range r.Reports {
			switch rep.Verdict {
			case sim.VerdictViolation:
				out.caught++
				if !out.detected {
					out.detected, out.first, out.detail = true, seed, rep.Detail
				}
			case sim.VerdictInconclusive:
				out.inconclusive++
			case sim.VerdictPass, sim.VerdictUnset:
			}
		}
	}
	return out
}

// TestPrepareRefusesAnUnsetPlacement induces the refusal rather than describing
// it. A scenario that picked a placement by zero value would silently change
// what the ablation above measured.
func TestPrepareRefusesAnUnsetPlacement(t *testing.T) {
	p, err := plan.Materialize(1, hunt.ToyGenConfig())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := hunt.Prepare(p, hunt.Scenario{Flaw: toy.FlawNone}); err == nil {
		t.Fatal("a scenario with no placement was prepared")
	} else {
		t.Logf("induced: %v", err)
	}
}
