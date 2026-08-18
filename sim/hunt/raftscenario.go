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
// RaftOptions are the A2 build parameters: what the cluster is, as opposed to
// what happens to it.
//
// They are deliberately NOT plan entries. The pre-vote ablation runs the same
// schedules with the round on and off, so pre-vote must not perturb the schedule
// it is being measured against -- the same reason the toy carries its flaw in the
// scenario rather than in the plan.
type RaftOptions struct {
	PreVote           bool
	SnapshotThreshold raft.Index

	// Transfers is how many leadership transfers the plan schedules.
	Transfers int
}

// A2Options is what the sweep runs: snapshots on with a threshold small enough
// that a 40-operation workload compacts several times, pre-vote on, and a
// couple of leadership transfers.
func A2Options() RaftOptions {
	return RaftOptions{PreVote: true, SnapshotThreshold: 6, Transfers: 2}
}

func RaftGenConfig() plan.GenConfig {
	cfg := plan.DefaultGenConfig()
	cfg.Nodes = 3
	cfg.Duration = 12 * time.Second
	cfg.ClientOps = 60
	cfg.Crashes = 4
	cfg.Partitions = 5 // weighted up, and genFaults alternates so most are single cuts
	cfg.Holds = 0      // A1 Raft has no clock-sensitive logic; holds land with leases
	return cfg
}

// MaterializeRaft turns a seed into a prepared plan with A2's options.
func MaterializeRaft(seed uint64) (*plan.Plan, error) {
	return MaterializeRaftWith(seed, A2Options())
}

// MaterializeRaftWith turns a seed into a prepared plan, adding the leadership
// transfers the options ask for.
//
// The transfer entries are derived from the seed's own key stream, so a plan is
// still a total repro and replay takes no live draw.
func MaterializeRaftWith(seed uint64, opt RaftOptions) (*plan.Plan, error) {
	p, err := plan.Materialize(seed, RaftGenConfig())
	if err != nil {
		return nil, fmt.Errorf("hunt: materialize: %w", err)
	}
	if opt.Transfers > 0 {
		key, err := rng.ParseKey(p.Keys.Raft)
		if err != nil {
			return nil, fmt.Errorf("hunt: raft key: %w", err)
		}
		span := p.Config.DurationNS
		for i := range opt.Transfers {
			// Spread through the middle of the run: a transfer before anybody
			// has been elected is a transfer of nothing.
			at := span/4 + int64(key.Uint64N(1, uint64(i), 0, 0, uint64(span/2)))
			target := int(key.Uint64N(2, uint64(i), 0, 0, uint64(p.Config.Nodes)))
			p.Faults.Entries = append(p.Faults.Entries, plan.Entry{
				AtNS: at, Action: "promote", Node: target,
			})
		}
	}
	return p, nil
}

// stateDigest is the harness's independent model of the state machine, handed to
// the snapshot oracle.
//
// It re-implements what a command does. What it borrows from store is the
// serialisation only, so a defect in APPLYING commands cannot cancel out on both
// sides of the comparison -- which is the whole reason the oracle takes a
// function instead of importing one.
func stateDigest(entries []raft.Entry) uint64 {
	kv := map[string]string{}
	for _, e := range entries {
		if len(e.Data) == 0 {
			continue
		}
		if op, k, v := store.DecodeCommand(e.Data); op == "put" {
			kv[k] = v
		}
	}
	return store.StateDigest(kv)
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

	// DurabilityCrossChecks is how often a node's durability record was compared
	// against the engine's own account. Evidence, like StaleEpochDrops: a count
	// of zero means the comparison never ran and any test resting on it proved
	// nothing.
	DurabilityCrossChecks int

	// SnapshotsTaken, SnapshotsApplied and TransfersAsked are A2's evidence that
	// its three features ran at all. A sweep in which no snapshot was ever taken
	// proves nothing about snapshots, however green it is.
	SnapshotsTaken   int
	SnapshotsApplied int
	TransfersAsked   int

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
	return RunRaftWith(p, A2Options(), tr)
}

// RunRaftWith drives the group with explicit build options.
func RunRaftWith(p *plan.Plan, opt RaftOptions, tr *sim.Trace) (RaftResult, error) {
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
	run.Loop.SetOracles(raftcheck.All(ledger, stateDigest))

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
			PreVote:           opt.PreVote,
			SnapshotThreshold: opt.SnapshotThreshold,
			SyncLatency:       syncLatency,
			Transport:         run.Transport, Ledger: ledger, History: hist,
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

	// A scheduled promote is A2's leadership transfer: whoever is leading hands
	// off to the named node. The plan carries it as an action, so it replays.
	run.OnPromote = func(target sim.NodeID) {
		for _, d := range drivers {
			if d.RequestTransfer(raft.NodeID(int(target) + 1)) {
				break
			}
		}
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
		res.DurabilityCrossChecks += d.DurabilityCrossChecks()
		res.SnapshotsTaken += d.SnapshotsTaken()
		res.SnapshotsApplied += d.SnapshotsApplied()
		res.TransfersAsked += d.TransfersAsked()
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

	// # A run with no leader concluded nothing, whatever the checkers say
	//
	// sim.CheckAll enforces the vacuous-green rule from the client's side: a
	// history that is mostly unknowns cannot bank a pass. This is the same rule
	// from the cluster's side, and it is a separate fact rather than a
	// restatement -- the checkers are told about operations, and "no node ever
	// became leader" is not an operation.
	//
	// It is worth asserting on its own because it is the shape the codec
	// off-by-one had: no leader, no answers, and a clean linearizability verdict
	// over forty unknowns (BUGS.md BUG-001). A safety claim over a cluster that
	// never did the thing whose safety is asserted is vacuous, so it is reported
	// as inconclusive.
	markVacuousIfNoLeader(res.Reports, res.Census)
	return res, nil
}

// markVacuousIfNoLeader downgrades a pass to inconclusive on a run that never
// elected anybody. Separated out so it can be induced directly: the condition is
// rare enough in the schedule mix -- 0 of 10,000 seeds -- that a sweep is no
// evidence at all that the rule works.
//
// A VIOLATION is never downgraded. A run that misbehaved without ever electing
// anybody found something real, and turning that into "we cannot tell" would
// lose it.
func markVacuousIfNoLeader(reports []sim.Report, census raftcheck.Census) {
	if census.ElectionsWon != 0 {
		return
	}
	for i := range reports {
		if reports[i].Verdict != sim.VerdictPass {
			continue
		}
		reports[i].Verdict = sim.VerdictInconclusive
		reports[i].Detail = "no node ever became leader in this run, so the cluster never did " +
			"the thing whose safety this verdict asserts; a pass here is a statement about nothing"
	}
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

	// A2's evidence that its three features ran. A sweep that never took a
	// snapshot, never installed one and never transferred leadership is green
	// about A1, whatever else it says.
	SnapshotsTaken   int
	SnapshotsApplied int
	TransfersAsked   int

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
		c.SnapshotsTaken += r.SnapshotsTaken
		c.SnapshotsApplied += r.SnapshotsApplied
		c.TransfersAsked += r.TransfersAsked
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
