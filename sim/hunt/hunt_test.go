package hunt_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
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

// result is one seed's outcome.
type result struct {
	seed    uint64
	outcome sim.Outcome
	reports []sim.Report
	counts  *sim.Counters
}

// runSeed materializes a plan from a seed, builds a toy cluster on it, drives
// the workload, and checks the resulting history.
//
// The whole point of the shape: the workload is *in the plan*, the faults are
// *in the plan*, and the history the checker sees is what a client observed.
// Nothing here reads node state.
func runSeed(t *testing.T, seed uint64, flaw toy.Flaw) result {
	return runSeedWith(t, seed, flaw, 0)
}

func runSeedWith(t *testing.T, seed uint64, flaw toy.Flaw, syncLatency clock.Instant) result {
	t.Helper()

	cfg := plan.DefaultGenConfig()
	cfg.Nodes = 3
	cfg.Duration = 5 * time.Second
	cfg.ClientOps = 30
	cfg.Crashes = 1
	cfg.Partitions = 1
	cfg.Holds = 0 // the toy has no clock-sensitive logic; holds would only cost time

	p, err := plan.Materialize(seed, cfg)
	if err != nil {
		t.Fatalf("seed %d: materialize: %v", seed, err)
	}

	// DR-15's reactive crash: fire while the primary holds unsynced data, so
	// the crash lands inside the window by construction rather than by luck,
	// and restart soon after so a later read can observe what was lost.
	p.Faults.Rules = append(p.Faults.Rules,
		plan.Rule{On: "unsynced_window_open", AfterNS: int64(10 * time.Millisecond), Action: "crash", Node: 0, Times: 1},
		plan.Rule{On: "unsynced_window_open", AfterNS: int64(200 * time.Millisecond), Action: "restart", Node: 0, Times: 1},
	)

	hist := sim.NewHistory()
	nodes := make([]sim.Node, p.Config.Nodes)
	toys := make([]*toy.Node, p.Config.Nodes)

	// Built in two passes: the transport needs the loop, and the nodes need the
	// transport, so the nodes are filled in after Build has produced both.
	for i := range nodes {
		nodes[i] = &lateBinder{}
	}
	run, err := plan.Build(p, nodes)
	if err != nil {
		t.Fatalf("seed %d: build: %v", seed, err)
	}

	var peers []sim.NodeID
	for i := 1; i < p.Config.Nodes; i++ {
		peers = append(peers, sim.NodeID(i))
	}
	for i := range nodes {
		toys[i] = toy.New(sim.NodeID(i), 0, peersFor(sim.NodeID(i), peers, p.Config.Nodes),
			run.Transport, hist, flaw)
		toys[i].SyncLatency = syncLatency
		nodes[i].(*lateBinder).inner = toys[i]
	}
	toys[0].OnUnsyncedWindow = func() { run.Trigger("unsynced_window_open") }

	// Client operations come from the plan, fully materialized, so the
	// minimizer could delete one without perturbing anything else.
	for _, op := range p.Workload.Ops {
		idx := hist.Begin(clock.Instant(op.AtNS), op.Client, op.Seq, op.Kind, op.Key, op.Value)
		run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, 0, toy.Request{
			Client: op.Client, Seq: op.Seq, Op: op.Kind,
			Key: op.Key, Value: op.Value, HistIdx: idx,
		})
	}

	out, err := run.Loop.Run()
	if err != nil {
		t.Fatalf("seed %d: run: %v", seed, err)
	}

	reports := sim.CheckAll(hist, checker.NewLinearizability())
	return result{seed: seed, outcome: out, reports: reports, counts: run.Counters}
}

// peersFor returns every node but this one.
func peersFor(self sim.NodeID, _ []sim.NodeID, n int) []sim.NodeID {
	var out []sim.NodeID
	for i := range n {
		if sim.NodeID(i) != self {
			out = append(out, sim.NodeID(i))
		}
	}
	return out
}

// lateBinder lets the node set be handed to Build before the nodes exist, since
// the nodes need the transport that Build creates.
type lateBinder struct{ inner sim.Node }

func (b *lateBinder) Handle(ev sim.Event, s sim.Scheduler) {
	if b.inner != nil {
		b.inner.Handle(ev, s)
	}
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
		r := runSeed(t, seed, toy.FlawNone)
		census[r.outcome.Kind]++

		for _, rep := range r.reports {
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
		flaw       toy.Flaw
		observable bool
		why        string
	}{
		{toy.FlawAckBeforeSync, true,
			"an acknowledged write is lost when the primary crashes before its own fsync, and a later read of the same key sees the old value"},
		{toy.FlawAckBeforeReplicate, false,
			"reads are served only by the primary and there is no failover, so a write missing from the backups is invisible to every client; closing this needs backup reads or promotion in the toy, not a change to the checker"},

		// The class the harness found in the correct toy, kept catchable. Its
		// detection rate is 1 in 1000, which is weak and is stated as such in
		// docs/TOY-FINDINGS.md rather than left to be inferred from the row.
		{toy.FlawDirtyRead, true,
			"a read observes a write that is neither durable nor acknowledged, and the crash that follows takes it back"},
	} {
		flaw := tc.flaw
		t.Run(flaw.String(), func(t *testing.T) {
			const seeds = 1000
			elapsedSince := wallTimer()

			var caught []uint64
			var firstReport sim.Report
			var inconclusive int

			for seed := uint64(0); seed < seeds; seed++ {
				r := runSeed(t, seed, flaw)
				for _, rep := range r.reports {
					switch rep.Verdict {
					case sim.VerdictViolation:
						if len(caught) == 0 {
							firstReport = rep
						}
						caught = append(caught, seed)
					case sim.VerdictInconclusive:
						inconclusive++
					case sim.VerdictPass, sim.VerdictUnset:
					}
				}
			}

			elapsed := elapsedSince()
			t.Logf("flaw %s: %d of %d seeds caught it in %s (%d inconclusive)",
				flaw, len(caught), seeds, elapsed.Round(time.Millisecond), inconclusive)
			if len(caught) > 0 {
				t.Logf("first catch: seed %d -- %s", caught[0], firstReport.Detail)
				t.Logf("seeds-to-detection: %d", caught[0]+1)
			}

			switch {
			case tc.observable && len(caught) == 0:
				t.Errorf("a knowingly broken toy (%s) survived %d seeds; the harness cannot find a real defect", flaw, seeds)
			case !tc.observable && len(caught) > 0:
				t.Errorf("%s was recorded as unobservable but %d seeds caught it; update the record", flaw, len(caught))
			case !tc.observable:
				t.Logf("RECORDED GAP: %s is not observable through this toy's client interface -- %s", flaw, tc.why)
			}
		})
	}
}

// TestAblationReactiveCrashAtOriginalWindow answers the question the remedy
// bundled two changes into.
//
// The fix that caught ack-before-sync did two things at once: it made the crash
// reactive on the unsynced window, and it widened the modelled fsync from 2ms
// to 50ms. Only the first is legitimate -- A1's real Raft will not offer a
// window knob -- so the second has to be shown unnecessary or admitted as a
// dependency. This measures the reactive crash alone, at the original 2ms.
func TestAblationReactiveCrashAtOriginalWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("an ablation is not a -short test")
	}

	for _, window := range []clock.Instant{2_000_000, 10_000_000, 50_000_000} {
		caught := 0
		// Detected is a distinguishable state, not a negative seed. An in-band
		// magic number in a field that otherwise holds a seed is exactly what
		// Mono's zero value and Hold's rejected realization exist to avoid, and
		// a -1 could be averaged by a future aggregation without complaint.
		detected := false
		var first uint64
		for seed := uint64(0); seed < 1000; seed++ {
			r := runSeedWith(t, seed, toy.FlawAckBeforeSync, window)
			for _, rep := range r.reports {
				if rep.Verdict == sim.VerdictViolation {
					caught++
					if !detected {
						detected, first = true, seed
					}
				}
			}
		}
		firstStr := "not detected"
		if detected {
			firstStr = fmt.Sprintf("first at seed %d", first)
		}
		t.Logf("fsync window %6dus: %3d of 1000 caught, %s", int64(window)/1000, caught, firstStr)
	}
}
