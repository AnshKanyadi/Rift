package hunt

import (
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/sim/toy"
)

// This file is the toy scenario driver, and it lives here rather than in a test
// file for one reason: `simctl` and the hunt must drive the toy through the
// *same* code.
//
// Step 8's requirement is that a violation found by sweeping seeds produces a
// bundle a human can replay. If the sweep and the replay each had their own copy
// of "build a toy cluster on a plan and check the result", the two would drift,
// and the first thing to break would be the claim that a bundle reproduces the
// violation it was cut from. A repro chain with two implementations of its
// middle is not a repro chain.
//
// This is not step 9. There is no seed sweeping here, no worker pool and no
// `simctl hunt` subcommand; those are step 9's and are not started.

// ToyGenConfig is the plan shape every toy run is materialized against, and it
// is exported for the same reason RunToy is.
//
// A seed only names a run relative to the configuration it was generated
// against. When the sweep used one config and `simctl` used another, seed 29
// meant two different plans and the violation the sweep found did not exist in
// the bundle -- which is a repro chain that is broken in the quietest possible
// way, since both halves run cleanly and report a hash.
//
// Anything that says "toy, seed N" resolves it through here.
func ToyGenConfig() plan.GenConfig {
	cfg := plan.DefaultGenConfig()
	cfg.Nodes = 3
	cfg.Duration = 5 * time.Second
	cfg.ClientOps = 30
	cfg.Crashes = 1
	cfg.Partitions = 1
	cfg.Holds = 0 // the toy has no clock-sensitive logic; holds would only cost time
	return cfg
}

// MaterializeToy turns a seed into a prepared toy plan: generation and scenario
// preparation in one call, so a caller cannot do one and forget the other.
func MaterializeToy(seed uint64, sc Scenario) (*plan.Plan, error) {
	p, err := plan.Materialize(seed, ToyGenConfig())
	if err != nil {
		return nil, fmt.Errorf("hunt: materialize: %w", err)
	}
	if err := Prepare(p, sc); err != nil {
		return nil, err
	}
	return p, nil
}

// Placement is where a scenario aims its crash. Closed enum: adding a placement
// must break every consumer that has not decided what to do about it.
type Placement uint8

const (
	// PlacementUnset is the zero value and is refused, so a forgotten field
	// cannot silently select a placement and quietly change what an ablation
	// measured.
	PlacementUnset Placement = iota

	// PlacementReactive fires the crash on the unsynced window, so it lands
	// inside the window by construction rather than by luck.
	PlacementReactive

	// PlacementUniform fires the crash at a uniformly drawn instant, which is
	// the null hypothesis reactive targeting has to beat.
	PlacementUniform

	numPlacements
)

// ParsePlacement is the inverse of String. The unset value is not parseable, so
// a bundle or a command line cannot select it by accident.
func ParsePlacement(s string) (Placement, error) {
	for p := PlacementReactive; p < numPlacements; p++ {
		if p.String() == s {
			return p, nil
		}
	}
	return PlacementUnset, fmt.Errorf("hunt: unknown crash placement %q", s)
}

func (p Placement) String() string {
	switch p {
	case PlacementUnset:
		return "unset"
	case PlacementReactive:
		return "reactive"
	case PlacementUniform:
		return "uniform"
	case numPlacements:
		return "invalid"
	}
	return "unknown"
}

// crashDelay and restartDelay are the reactive rule's offsets from the window
// opening. The uniform placement reuses the same 190ms downtime so that the two
// cells differ in *placement only* -- if uniform also changed how long the
// primary stayed down, the ablation would be measuring two things again, which
// is the exact error the ablation was created to correct.
const (
	crashDelay   = clock.Instant(10_000_000)  // 10ms
	promoteDelay = clock.Instant(20_000_000)  // 20ms, so a backup takes over after the primary dies
	restartDelay = clock.Instant(200_000_000) // 200ms
)

// successor is the backup promoted on failover. Fixed rather than drawn: the
// toy has no election, and choosing at random would add a variable to every
// measurement without adding a question any of them are asking.
const successor = 1

// Scenario is what a toy run needs beyond the plan.
//
// The flaw is a property of the *build*, not of the schedule, which is why it
// lives here and not in the plan: replaying a bundle means running the same
// schedule against the same build, and a plan that carried the flaw would be
// claiming the schedule caused it.
type Scenario struct {
	Flaw      toy.Flaw
	Placement Placement

	// SyncLatency overrides the modelled fsync, for the ablation. Zero takes
	// toy.DefaultSyncLatency. Whatever it is, toy.New validates it against the
	// plan's own round trip and refuses a regime the flaw cannot manifest in.
	SyncLatency clock.Instant

	// Failover crashes the primary and promotes a backup, and it is what makes
	// ack-before-replicate observable at all.
	//
	// Without it, reads are served only by node 0, so a write missing from the
	// backups is invisible to every client and the flaw is a recorded gap rather
	// than a detection. With it, the surviving replica answers, and a write the
	// dead primary acknowledged but never replicated is a stale read.
	//
	// It also moves the crash to the other side of the acknowledgement — see
	// triggerFor — because the two flaws live in windows that do not overlap.
	Failover bool
}

// triggerFor picks the condition the crash rides on, and the choice is the whole
// reason a single scenario cannot chase both flaws at once.
//
//   - Without failover the target is ack-before-sync: the primary must die
//     between acknowledging and its own fsync, so the crash rides on the
//     unsynced window opening.
//   - With failover the target is ack-before-replicate: the primary must die
//     *after* it has answered a client, while a backup is still missing the
//     write, so the crash rides on the acknowledgement itself.
//
// Aiming at the wrong edge does not produce a weaker signal, it produces none:
// a crash before the acknowledgement leaves an in-flight operation the checker
// correctly refuses to call a violation.
func triggerFor(failover bool) string {
	if failover {
		return "write_acked"
	}
	return "unsynced_window_open"
}

// Prepare writes the scenario's faults into the plan, and must be called before
// Build.
//
// It mutates the plan on purpose. The plan that goes into a bundle has to be the
// plan that ran, or a replay reproduces something else -- so the scenario's
// crash lands in the plan as ordinary entries and rules, individually deletable
// like every other fault, rather than being applied out of band at run time.
//
// A replay does not call this: the bundle's plan is already prepared, and
// preparing it twice would double the faults.
func Prepare(p *plan.Plan, sc Scenario) error {
	switch sc.Placement {
	case PlacementUnset:
		return fmt.Errorf("hunt: scenario has no crash placement; the zero value is refused so a forgotten field cannot silently pick one")

	case PlacementReactive:
		// DR-15's reactive crash: fire while the window is open, so the crash
		// lands inside it by construction rather than by luck, and restart soon
		// after so a later read can observe what was lost.
		on := triggerFor(sc.Failover)
		p.Faults.Rules = append(p.Faults.Rules,
			plan.Rule{On: on, AfterNS: int64(crashDelay), Action: "crash", Node: 0, Times: 1},
			plan.Rule{On: on, AfterNS: int64(restartDelay), Action: "restart", Node: 0, Times: 1},
		)
		if sc.Failover {
			p.Faults.Rules = append(p.Faults.Rules,
				plan.Rule{On: on, AfterNS: int64(promoteDelay), Action: "promote", Node: successor, Times: 1})
		}

	case PlacementUniform:
		// The null hypothesis. It crashes *the primary* -- not a uniformly chosen
		// node -- at a uniformly drawn instant, because the question is whether
		// reactive *placement* earns its complexity. Letting the uniform cell
		// also pick a random node would confound placement with target and the
		// comparison would answer neither question.
		//
		// The draw is generation, not execution: it happens before Build, its
		// result is written into the plan, and the plan alone reproduces the run
		// afterwards. It is keyed off the plan's own seed, so the cell is
		// reproducible per seed like everything else.
		r := rng.New(p.Provenance.Seed).Derive("ablation.uniform")
		span := p.Config.DurationNS - int64(restartDelay)
		if span <= 0 {
			return fmt.Errorf("hunt: run of %dns is too short to place a uniform crash with a %dns downtime",
				p.Config.DurationNS, int64(restartDelay))
		}
		at := int64(r.IntN(int(span)))
		p.Faults.Entries = append(p.Faults.Entries,
			plan.Entry{AtNS: at, Action: "crash", Node: 0},
			plan.Entry{AtNS: at + int64(restartDelay-crashDelay), Action: "restart", Node: 0},
		)
		if sc.Failover {
			p.Faults.Entries = append(p.Faults.Entries,
				plan.Entry{AtNS: at + int64(promoteDelay-crashDelay), Action: "promote", Node: successor})
		}

	case numPlacements:
		return fmt.Errorf("hunt: invalid placement %d", sc.Placement)
	}
	return nil
}

// Result is one toy run.
type Result struct {
	Outcome  sim.Outcome
	Reports  []sim.Report
	History  *sim.History
	Counters *sim.Counters
}

// Violation returns the first violation report, or nil.
func (r Result) Violation() *sim.Report {
	for i := range r.Reports {
		if r.Reports[i].Verdict == sim.VerdictViolation {
			return &r.Reports[i]
		}
	}
	return nil
}

// RunToy builds a toy cluster on an already-prepared plan, drives the plan's
// workload against it, and checks the resulting history.
//
// The shape is the whole point: the workload is in the plan, the faults are in
// the plan, and the history the checker sees is what a client observed. Nothing
// here reads node state, which is the oracle-independence rule holding at the
// place it is easiest to break.
//
// tr may be nil, which is what a sweep wants once a seed has been checked.
func RunToy(p *plan.Plan, sc Scenario, tr *sim.Trace) (Result, error) {
	hist := sim.NewHistory()

	// Built in two passes: the transport needs the loop, and the nodes need the
	// transport, so the nodes are filled in after Build has produced both.
	nodes := make([]sim.Node, p.Config.Nodes)
	for i := range nodes {
		nodes[i] = &lateBinder{}
	}
	run, err := plan.Build(p, nodes)
	if err != nil {
		return Result{}, fmt.Errorf("hunt: build: %w", err)
	}
	if tr != nil {
		run.Loop.SetTrace(tr)
	}

	// The round trip comes from the plan's own network rather than from a
	// constant, so toy.New validates the modelled fsync against the network this
	// run actually has.
	rtt := p.ReplicationRTT()

	toys := make([]*toy.Node, p.Config.Nodes)
	for i := range nodes {
		n, err := toy.New(toy.Config{
			ID:             sim.NodeID(i),
			Primary:        0,
			Peers:          peersFor(sim.NodeID(i), p.Config.Nodes),
			Transport:      run.Transport,
			History:        hist,
			Flaw:           sc.Flaw,
			SyncLatency:    sc.SyncLatency,
			ReplicationRTT: rtt,
		})
		if err != nil {
			return Result{}, err
		}
		toys[i] = n
		nodes[i].(*lateBinder).inner = n
	}

	// Both triggers go on every node, not only on node 0. Under failover the
	// primary changes, and a hook installed only on the original primary would
	// go quiet exactly when the successor took over -- silently, with every lane
	// still green.
	for _, n := range toys {
		n.OnUnsyncedWindow = func() { run.Trigger("unsynced_window_open") }
		n.OnWriteAcked = func() { run.Trigger("write_acked") }
	}
	run.OnPromote = func(id sim.NodeID) {
		// Every node is told at the same instant, so two simultaneous primaries
		// are unrepresentable rather than merely unlikely.
		for _, n := range toys {
			n.SetPrimary(id)
		}
	}

	// Client operations come from the plan, fully materialized, so the minimizer
	// could delete one without perturbing anything else.
	//
	// Each one is delivered to *every* node, and exactly the one that is primary
	// at that instant answers -- the rest return immediately. That is how a
	// request reaches a primary whose identity is not known until the run
	// produces it: routing by node index would have to be decided when the op is
	// scheduled, which is before any promotion has happened. A crashed node
	// receives nothing, so an operation arriving while the cluster has no live
	// primary stays in flight, which is unavailability and correct.
	for _, op := range p.Workload.Ops {
		idx := hist.Begin(clock.Instant(op.AtNS), op.Client, op.Seq, op.Kind, op.Key, op.Value)
		req := toy.Request{
			Client: op.Client, Seq: op.Seq, Op: op.Kind,
			Key: op.Key, Value: op.Value, HistIdx: idx,
		}
		for i := range nodes {
			run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, sim.NodeID(i), req)
		}
	}

	out, err := run.Loop.Run()
	if err != nil {
		return Result{}, fmt.Errorf("hunt: run: %w", err)
	}

	return Result{
		Outcome:  out,
		Reports:  sim.CheckAll(hist, checker.NewLinearizability()),
		History:  hist,
		Counters: run.Counters,
	}, nil
}

// peersFor returns every node but this one.
func peersFor(self sim.NodeID, n int) []sim.NodeID {
	out := make([]sim.NodeID, 0, n-1)
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
