package hunt

import (
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/store"
)

// The A1 Raft scenario driver, alongside the A0 toy's.
//
// Sweeping and driving are orchestration and live here (Amendment A5); the
// materialization that decides *what run to perform* lives in the deterministic
// half, the same split TestScopeTable pins.

// RaftGenConfig is the plan shape A1 seeds are materialized against.
//
// # The schedule mix weights the single-cut geometry
//
// DESIGN-A0.7 blessed directed partitions with a forward binding: *A1's schedule
// mix weights the single-cut send-without-receive geometry.* Honoured here. A
// symmetric cut is two directed cuts and isolates a node cleanly; a SINGLE cut
// leaves a node that can send but not receive, so it campaigns, bumps terms, and
// never learns it lost. That is where the interesting consensus bugs live, and
// symmetric partitions never generate it.
func RaftGenConfig() plan.GenConfig {
	cfg := plan.DefaultGenConfig()
	cfg.Nodes = 3
	cfg.Duration = 8 * time.Second
	cfg.ClientOps = 40
	cfg.Crashes = 2
	cfg.Partitions = 3 // weighted up, and genFaults alternates so most are single cuts
	cfg.Holds = 0      // A1 Raft has no clock-sensitive logic; holds land with leases
	return cfg
}

// MaterializeRaft turns a seed into a prepared A1 plan.
func MaterializeRaft(seed uint64) (*plan.Plan, error) {
	p, err := plan.Materialize(seed, RaftGenConfig())
	if err != nil {
		return nil, fmt.Errorf("hunt: materialize: %w", err)
	}
	return p, nil
}

// RaftResult is one Raft run.
type RaftResult struct {
	Ledger  *raftcheck.Ledger
	History *sim.History

	// StaleEpochDrops is how many completions from a dead incarnation this run's
	// nodes refused. It is EVIDENCE, not a verdict, and
	// TestStaleDurabilityCompletionIsRefused is what asks for it: a nonzero
	// count means the schedules are producing the crash-with-a-sync-in-flight
	// race the guard exists for, and a zero count across a range means the test
	// is proving nothing.
	//
	// sim.EpochGuard.Check is deliberately NOT called here. It reads any drop as
	// a driver defect, which is right for a component that can decline to emit
	// the completion and wrong for this one: the simulator owns the event queue,
	// a durability event scheduled before a crash is delivered after the restart
	// whatever the driver wants, and the stamp is the only thing that can tell it
	// apart. Calling it would have failed 3 seeds in 200 for doing exactly what
	// the guard is for. Collecting a verdict nobody consults is worse than not
	// collecting it, so the field it was stored in is gone.
	StaleEpochDrops int

	Seed     uint64
	Outcome  sim.Outcome
	Reports  []sim.Report
	Census   raftcheck.Census
	Violated *sim.Violation
	Err      error
}

// syncLatency is the modelled fsync for a Raft node's engine.
//
// It must exceed a replication round trip for the persist-before-reply window to
// exist at all -- the same regime argument the toy's window gate makes, and for
// the same reason. Twelve milliseconds: max(crash targeting delay, worst-case
// RTT from the generator's 6ms slowest link).
const syncLatency = clock.Instant(12_000_000)

// RunRaft builds a three-node Raft group on a plan, drives client traffic
// against it, and checks the result.
func RunRaft(p *plan.Plan, tr *sim.Trace) (RaftResult, error) {
	res := RaftResult{Seed: p.Provenance.Seed}
	n := p.Config.Nodes

	hist := sim.NewHistory()
	ledger := raftcheck.NewLedger(n)

	nodes := make([]sim.Node, n)
	for i := range nodes {
		nodes[i] = &lateBinder{}
	}
	run, err := plan.Build(p, nodes)
	if err != nil {
		return res, fmt.Errorf("hunt: build: %w", err)
	}
	if tr != nil {
		run.Loop.SetTrace(tr)
	}

	// The oracles watch the run and halt it at the first violation. They read
	// the ledger and nothing else (DESIGN-A1 §0).
	run.Loop.SetOracles(raftcheck.All(ledger))

	peers := make([]raft.NodeID, n)
	for i := range peers {
		peers[i] = raft.NodeID(i + 1)
	}

	// Election jitter is plan-derived: a pure state machine cannot randomize for
	// itself, and a live draw would break replay.
	jitKey, err := rng.ParseKey(p.Keys.Raft)
	if err != nil {
		return res, fmt.Errorf("hunt: raft key: %w", err)
	}

	drivers := make([]*store.Node, n)
	for i := range nodes {
		ord := i
		d, err := store.New(store.Config{
			ID: raft.NodeID(i + 1), Peers: peers, Ordinal: ord,
			Election: 10, Heartbeat: 3,
			SyncLatency: syncLatency,
			Transport:   run.Transport, Ledger: ledger, History: hist,
			ElectionJitter: func(term raft.Term) int {
				return 10 + int(jitKey.Uint64N(0, uint64(ord), uint64(term), 0, 10))
			},
		})
		if err != nil {
			return res, err
		}
		drivers[i] = d
		nodes[i].(*lateBinder).inner = d
	}

	// Client operations go to every node; only the leader answers. That is how a
	// request reaches a leader whose identity is not known until the run
	// produces it -- the same routing the toy uses under failover.
	for _, op := range p.Workload.Ops {
		idx := hist.Begin(clock.Instant(op.AtNS), op.Client, op.Seq, op.Kind, op.Key, op.Value)
		req := store.Request{
			Client: op.Client, Seq: op.Seq, Op: op.Kind,
			Key: op.Key, Value: op.Value, HistIdx: idx,
		}
		for i := range nodes {
			run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, sim.NodeID(i), req)
		}
	}

	out, err := run.Loop.Run()
	if err != nil {
		return res, fmt.Errorf("hunt: run: %w", err)
	}
	res.Outcome = out
	res.Violated = run.Loop.Violation()
	res.Census = ledger.Census()

	// A node that stopped while still withholding a message is a stall, not a
	// clean run. A permanently withheld message is indistinguishable from one
	// never generated, so it is surfaced rather than left as silence.
	for i, d := range drivers {
		if err := d.AssertQuiescent(); err != nil {
			return res, fmt.Errorf("hunt: node %d: %w", i, err)
		}
		// How often the schedule produced a completion that outlived the
		// incarnation that asked for it. Collected as evidence, judged by
		// TestStaleDurabilityCompletionIsRefused, and never by a verdict here --
		// see the field's own comment for why a drop is the guard working.
		res.StaleEpochDrops += d.StaleEpochDrops()
	}

	// The fire-count assertion only means anything on a run that reached its
	// deadline. A run an oracle halted stopped early by construction, so its
	// schedule is legitimately incomplete -- and reporting a shortfall there
	// would replace the violation with a harness error, hiding the finding
	// behind a complaint about the finding's own consequence.
	if res.Outcome.Kind == sim.OutcomeDeadline {
		if short := run.Counters.Check(); len(short) > 0 {
			return res, fmt.Errorf("hunt: the run injected less than its plan asserts: %v", short)
		}
	}
	res.History = hist
	res.Ledger = ledger
	res.Reports = sim.CheckAll(run.Counters, hist, checker.NewLinearizability())
	return res, nil
}

// RaftCensus aggregates a sweep.
type RaftCensus struct {
	Seeds        int
	Violations   int
	Inconclusive int
	Pass         int
	Errors       int

	Terms          uint64
	ElectionsStart int
	ElectionsWon   int
	SplitVotes     int

	// SeedsWithNoLeader is the number that never elected anybody. A run whose
	// leader is never challenged proves nothing; a run with NO leader proves
	// less than that, because every client operation goes unanswered and the
	// linearizability checker reports green over a history of unknowns.
	SeedsWithNoLeader   int
	SeedsWithContention int

	FirstViolation     uint64
	FoundAViolation    bool
	InconclusiveCauses []string
}

// SweepRaft runs a seed range and aggregates it.
func SweepRaft(from, to uint64) (RaftCensus, error) {
	var c RaftCensus
	for seed := from; seed < to; seed++ {
		p, err := MaterializeRaft(seed)
		if err != nil {
			return c, err
		}
		r, err := RunRaft(p, nil)
		c.Seeds++
		if err != nil {
			c.Errors++
			return c, fmt.Errorf("seed %d: %w", seed, err)
		}

		if uint64(r.Census.Terms) > c.Terms {
			c.Terms = uint64(r.Census.Terms)
		}
		c.ElectionsStart += r.Census.ElectionsStart
		c.ElectionsWon += r.Census.ElectionsWon
		c.SplitVotes += r.Census.SplitVotes
		if r.Census.ElectionsWon == 0 {
			c.SeedsWithNoLeader++
		}
		if r.Census.ElectionsWon > 1 || r.Census.SplitVotes > 0 {
			c.SeedsWithContention++
		}

		if r.Violated != nil {
			c.Violations++
			if !c.FoundAViolation {
				c.FoundAViolation, c.FirstViolation = true, seed
			}
		}
		for _, rep := range r.Reports {
			switch rep.Verdict {
			case sim.VerdictPass:
				c.Pass++
			case sim.VerdictViolation:
				c.Violations++
				if !c.FoundAViolation {
					c.FoundAViolation, c.FirstViolation = true, seed
				}
			case sim.VerdictInconclusive:
				c.Inconclusive++
				if len(c.InconclusiveCauses) < 10 {
					c.InconclusiveCauses = append(c.InconclusiveCauses,
						fmt.Sprintf("seed %d: %s", seed, rep.Detail))
				}
			case sim.VerdictUnset:
			}
		}
	}
	return c, nil
}
