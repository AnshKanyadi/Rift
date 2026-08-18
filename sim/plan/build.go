package plan

import (
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/internal/sorted"
	"github.com/anshkanyadi/rift/sim"
)

// Run is one plan, built and ready to execute.
type Run struct {
	Loop      *sim.Loop
	Transport *sim.SimTransport
	Counters  *sim.Counters
	Clocks    []*clock.Sim

	// Rand is poisoned. It is handed to anything that expects a source of
	// randomness so that a stray sequential draw during execution dies at the
	// call site rather than quietly producing a run the plan does not describe.
	//
	// This is the mechanical half of "a plan is a complete repro". Not a
	// promise, a panic.
	Rand rng.Rand

	// OnPromote is called when a "promote" action fires, naming the node that
	// becomes primary. Nil ignores the action.
	//
	// A hook rather than something this package implements, because what a
	// promotion *means* is a property of the protocol under test and this
	// package deliberately knows no protocol. It stays a plan entry so a
	// failover is scheduled, ordered against the traffic in flight at its
	// instant, and individually deletable like every other fault -- the same
	// argument Loop.Do makes for link cuts.
	OnPromote func(sim.NodeID)

	plan *Plan
	// fired counts per rule index, parallel to plan.Faults.Rules.
	fired []int
}

// Build turns a plan into a runnable simulation.
//
// Nothing here draws sequentially. Every quantity comes from the plan or from a
// plan-carried PRF key, which is what makes the plan alone sufficient — and
// what makes deleting one fault entry leave every other dice outcome
// bit-identical, the property ddmin's validity rests on.
func Build(p *Plan, nodes []sim.Node) (*Run, error) {
	counts := sim.NewCounters()

	if err := p.Validate(); err != nil {
		return nil, err
	}
	counts.Asserted("plan.Plan.Validate")
	if len(nodes) != p.Config.Nodes {
		return nil, fmt.Errorf("plan: plan wants %d nodes, got %d", p.Config.Nodes, len(nodes))
	}

	clocks, err := buildClocks(p)
	if err != nil {
		return nil, err
	}
	// clock.NewSim cannot return without Timeline.Validate having run.
	counts.Asserted("clock.Timeline.Validate")

	netKey, err := rng.ParseKey(p.Keys.Net)
	if err != nil {
		return nil, fmt.Errorf("plan: net key: %w", err)
	}

	loop, err := sim.NewLoop(sim.Config{
		Nodes:        nodes,
		Clocks:       clocks,
		TickInterval: ns(p.Config.TickIntervalNS),
		Until:        at(p.Config.DurationNS),
		MaxSteps:     p.Config.MaxSteps,
		Counters:     counts,
	})
	if err != nil {
		return nil, err
	}

	// Sorted, because an error message naming "the first unknown injector"
	// must name the same one on every run.
	for _, name := range sorted.Keys(p.Assert.MinFires) {
		inj, ok := sim.InjectorByName(name)
		if !ok {
			return nil, fmt.Errorf("plan: min_fires names unknown injector %q", name)
		}
		counts.Require(inj, p.Assert.MinFires[name])
	}

	tr := sim.NewSimTransport(loop, netKey, counts)
	for _, l := range p.Network.Links {
		tr.SetLink(nodeID(l.From), nodeID(l.To), sim.LinkParams{
			DropPerMille: l.DropPerMille,
			DupPerMille:  l.DupPerMille,
			LatMin:       ns(l.LatMinNS),
			LatMax:       ns(l.LatMaxNS),
			TailPerMille: l.TailPerMille,
			TailMax:      ns(l.TailMaxNS),
		})
	}

	r := &Run{
		Loop:      loop,
		Transport: tr,
		Counters:  counts,
		Clocks:    clocks,
		Rand:      rng.Poisoned("plan execution"),
		plan:      p,
		fired:     make([]int, len(p.Faults.Rules)),
	}

	// Timed fault entries become scheduled events, so a crash is ordered
	// against the messages in flight at that instant rather than applied out of
	// band.
	for _, e := range p.Faults.Entries {
		r.schedule(at(e.AtNS), e.Action, e.Node, e.From, e.To)
	}
	return r, nil
}

// buildClocks assembles each node's timeline and layers the holds onto it.
func buildClocks(p *Plan) ([]*clock.Sim, error) {
	tls := make([]*clock.Timeline, p.Config.Nodes)
	for i := range tls {
		tls[i] = &clock.Timeline{
			Skew:  []clock.Segment{{Start: 0, Off: 0, SlopePPB: 0}},
			Epoch: clock.Wall(p.Clock.EpochNS),
		}
	}
	for _, nc := range p.Clock.Nodes {
		if nc.Node < 0 || nc.Node >= len(tls) {
			return nil, fmt.Errorf("plan: clock schedule names unknown node %d", nc.Node)
		}
		tl := tls[nc.Node]
		if len(nc.Skew) > 0 {
			tl.Skew = nil
			for _, seg := range nc.Skew {
				tl.Skew = append(tl.Skew, clock.Segment{Start: at(seg.StartNS), Off: seg.OffNS, SlopePPB: seg.SlopePPB})
			}
		}
		for _, st := range nc.Steps {
			tl.Steps = append(tl.Steps, clock.Step{At: at(st.AtNS), Delta: st.DeltaNS})
		}
		for _, b := range nc.Boots {
			tl.Boots = append(tl.Boots, at(b))
		}
	}

	for i, h := range p.Clock.Holds {
		if h.B < 0 || h.B >= len(tls) {
			return nil, fmt.Errorf("plan: hold %d names unknown node %d", i, h.B)
		}
		out, _, err := clock.Hold{
			A: h.A, B: h.B,
			AtPPB:    h.AtPPB,
			From:     at(h.FromNS),
			To:       at(h.ToNS),
			Ramp:     ns(h.RampNS),
			Realize:  realization(h.Realize),
			Envelope: h.Envelope,
		}.Compile(*tls[h.B], ns(p.Config.MaxOffsetNS))
		if err != nil {
			return nil, fmt.Errorf("plan: hold %d: %w", i, err)
		}
		tls[h.B] = &out
	}

	out := make([]*clock.Sim, len(tls))
	for i, tl := range tls {
		c, err := clock.NewSim(tl, ns(p.Config.MaxOffsetNS))
		if err != nil {
			return nil, fmt.Errorf("plan: node %d clock: %w", i, err)
		}
		out[i] = c
	}
	return out, nil
}

// Trigger fires the reactive rules registered for a named condition.
//
// Reactive faults exist because "partition the leader 200ms after it is
// elected" cannot be materialized by time: the instant is not known until the
// run produces it. They stay declarative and individually deletable, so the
// nemesis is a rule generator rather than an escape from the plan.
//
// Nothing random happens here. The delay and the action come from the plan; the
// only run-dependent input is *when* the condition occurred.
// Times is counted **per rule**, not per condition.
//
// It was per condition, and that was a fault injector that silently injected
// less than the plan said. Two rules on one condition -- "crash the primary 10ms
// after the window opens, restart it 200ms after" -- share a counter under that
// reading, so the first rule's fire exhausts the second rule's budget and the
// restart never happens at all. Every seed still ran, every lane stayed green,
// and the plan described a schedule the run did not execute.
//
// The same shape as the crash injector that marked a node down without telling
// it: not a wrong answer, an unasked question. Fire counts do not catch it,
// because the *crash* fires and the counter it increments is the crash's.
func (r *Run) Trigger(condition string) {
	for i, rule := range r.plan.Faults.Rules {
		if rule.On != condition {
			continue
		}
		if rule.Times > 0 && r.fired[i] >= rule.Times {
			continue
		}
		r.fired[i]++
		r.schedule(r.Loop.Now().Add(ns(rule.AfterNS)), rule.Action, rule.Node, rule.From, rule.To)
	}
}

// schedule turns one action into loop or transport work.
//
// Link actions apply at the instant they are scheduled for, so they go through
// the loop as a client event carrying a closure rather than being applied now.
// A partition that took effect at plan-build time rather than at its instant
// would silently cut messages the plan says should have crossed.
func (r *Run) schedule(when clock.Instant, action string, node, from, to int) {
	switch action {
	// No counting here. Scheduling is an intent; the loop counts the event when
	// it fires, which is the only thing a fire count may claim.
	case "crash":
		r.Loop.Crash(when, nodeID(node))
	case "restart":
		r.Loop.Restart(when, nodeID(node))
	case "cut":
		r.Loop.Do(when, func() { r.Transport.Cut(nodeID(from), nodeID(to)) })
	case "heal":
		r.Loop.Do(when, func() { r.Transport.Heal(nodeID(from), nodeID(to)) })
	case "cut_both":
		r.Loop.Do(when, func() { r.Transport.CutBoth(nodeID(from), nodeID(to)) })
	case "heal_both":
		r.Loop.Do(when, func() { r.Transport.HealBoth(nodeID(from), nodeID(to)) })
	case "promote":
		// Read through r at fire time rather than capturing the hook now, so a
		// driver may set OnPromote after Build -- which it must, since the nodes
		// the hook talks to do not exist until Build has produced the transport.
		r.Loop.Do(when, func() {
			if r.OnPromote != nil {
				r.OnPromote(nodeID(node))
			}
		})
	}
}
