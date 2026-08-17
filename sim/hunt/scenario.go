package hunt

import (
	"fmt"

	"github.com/anshkanyadi/rift/clock"
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

// Result is one toy run.
type Result struct {
	Seed     uint64
	Outcome  sim.Outcome
	Reports  []sim.Report
	History  *sim.History
	Counters *sim.Counters

	// Refused means the window gate declined this seed: on its network the
	// planted flaw cannot exist, so the seed was never eligible. Not a failure
	// and not a pass.
	Refused bool

	// Err is a harness failure, which is neither of the above and must not be
	// quietly folded into either.
	Err error
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
func RunToy(p *plan.Plan, sc toy.Scenario, tr *sim.Trace) (Result, error) {
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
