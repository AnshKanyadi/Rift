package plan_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/plan"
)

// echo is a node that sends on every tick and records what it receives, so a
// run built from a plan actually exercises the transport rather than only the
// tick schedule.
type echo struct {
	id    sim.NodeID
	peers int
	got   []string
	trace []string
}

func (e *echo) Handle(ev sim.Event, s sim.Scheduler) {
	switch ev.Kind {
	case sim.KindTick:
		e.trace = append(e.trace, fmt.Sprintf("tick@%d", int64(ev.At)))
	case sim.KindDeliver:
		frame, ok := ev.Payload.([]byte)
		if !ok {
			return
		}
		env, err := sim.Decode(frame)
		if err != nil {
			e.got = append(e.got, "decode-error")
			return
		}
		e.got = append(e.got, fmt.Sprintf("%d<-%d:%s", ev.Node, env.From, env.Body))
	}
}

func nodesFor(p *plan.Plan) ([]sim.Node, []*echo) {
	es := make([]*echo, p.Config.Nodes)
	ns := make([]sim.Node, p.Config.Nodes)
	for i := range es {
		es[i] = &echo{id: sim.NodeID(i), peers: p.Config.Nodes}
		ns[i] = es[i]
	}
	return ns, es
}

// TestPlanRoundTrip: a plan survives being written and read. A corpus entry
// that half-loads is worse than one that does not load, so the schema version
// is checked rather than best-effort parsed.
func TestPlanRoundTrip(t *testing.T) {
	p, err := plan.Materialize(12345, plan.DefaultGenConfig())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	b, err := plan.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := plan.Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	b2, err := plan.Marshal(back)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	if !bytes.Equal(b, b2) {
		t.Error("a plan did not survive a round trip byte for byte")
	}

	// It has to be readable, not merely parseable: a bundle is a bug report a
	// human argues with, and "seed 8834127" is not one.
	for _, want := range []string{"schema_version", "provenance", "keys", "min_fires", "realize"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("serialized plan has no %q section", want)
		}
	}
}

// TestPlanRejectsWhatItCannotMean covers the validator. Each of these would
// otherwise produce a run that silently means something other than the plan.
func TestPlanRejectsWhatItCannotMean(t *testing.T) {
	base, err := plan.Materialize(1, plan.DefaultGenConfig())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	cases := []struct {
		name   string
		break_ func(*plan.Plan)
	}{
		{"wrong schema version", func(p *plan.Plan) { p.SchemaVersion = 99 }},
		{"zero wall epoch", func(p *plan.Plan) { p.Clock.EpochNS = 0 }},
		{"missing key", func(p *plan.Plan) { p.Keys.Net = "" }},
		{"malformed key", func(p *plan.Plan) { p.Keys.Raft = "not-hex" }},
		{"unauthored hold realization", func(p *plan.Plan) {
			if len(p.Clock.Holds) > 0 {
				p.Clock.Holds[0].Realize = ""
			} else {
				p.Clock.Holds = []plan.Hold{{Realize: ""}}
			}
		}},
		{"unknown fault action", func(p *plan.Plan) {
			p.Faults.Entries = append(p.Faults.Entries, plan.Entry{Action: "explode"})
		}},
		{"rule with no trigger", func(p *plan.Plan) {
			p.Faults.Rules = append(p.Faults.Rules, plan.Rule{Action: "crash"})
		}},
	}
	for _, tc := range cases {
		p := *base
		tc.break_(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

// TestReplayFromPlanTakesNoLiveDraw is step 5's exit criterion and the property
// the whole format exists for.
//
// The run is handed a poisoned Rand whose every method panics. If any execution
// path took a sequential draw, this test dies at the call site rather than
// quietly producing a schedule the plan does not describe. That the run
// completes at all is the proof.
func TestReplayFromPlanTakesNoLiveDraw(t *testing.T) {
	p, err := plan.Materialize(777, plan.DefaultGenConfig())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	nodes, _ := nodesFor(p)
	run, err := plan.Build(p, nodes)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// The poison is real: touching it panics, which is what makes the guarantee
	// mechanical rather than a promise.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the run's Rand is not poisoned; a stray draw would go unnoticed")
			}
		}()
		run.Rand.Uint64()
	}()

	// Drive some traffic so the transport's dice are actually exercised.
	for i := range 500 {
		run.Transport.Send(sim.Envelope{From: 0, To: 1, Body: fmt.Appendf(nil, "m%d", i)})
	}

	out, err := run.Loop.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Steps == 0 {
		t.Fatal("the run did nothing, so it proves nothing")
	}
	t.Logf("outcome: %v", out)
}

// TestSamePlanSameRun: two runs built from one plan produce identical
// histories. This is what "the plan alone reproduces the run" means, and it is
// asserted over the message history rather than over a summary, because a
// summary can match while the order differs.
func TestSamePlanSameRun(t *testing.T) {
	p, err := plan.Materialize(2024, plan.DefaultGenConfig())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	execute := func() ([]string, sim.Outcome) {
		nodes, es := nodesFor(p)
		run, err := plan.Build(p, nodes)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		for i := range 300 {
			run.Transport.Send(sim.Envelope{From: 0, To: 1, Body: fmt.Appendf(nil, "m%d", i)})
			run.Transport.Send(sim.Envelope{From: 1, To: 2, Body: fmt.Appendf(nil, "n%d", i)})
		}
		out, err := run.Loop.Run()
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		var all []string
		for _, e := range es {
			all = append(all, e.got...)
		}
		return all, out
	}

	a, outA := execute()
	b, outB := execute()

	if len(a) == 0 {
		t.Fatal("no messages were delivered, so this proves nothing")
	}
	if len(a) != len(b) {
		t.Fatalf("histories differ in length: %d then %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("histories diverge at %d: %q then %q", i, a[i], b[i])
		}
	}
	if outA.Kind != outB.Kind || outA.Steps != outB.Steps {
		t.Errorf("outcomes differ: %v then %v", outA, outB)
	}
}

// TestDeletingAFaultEntryPerturbsOnlyItself is the property ddmin's validity
// rests on, checked at the plan level rather than only at the transport's.
//
// Removing one crash from a plan must leave the network dice untouched. Under
// sequential randomness it would not: every draw after the deleted entry would
// shift, and the minimizer would attribute a change in behaviour to the entry
// it removed when the real cause was a moved draw.
func TestDeletingAFaultEntryPerturbsOnlyItself(t *testing.T) {
	cfg := plan.DefaultGenConfig()
	cfg.Crashes = 2
	cfg.Partitions = 0
	cfg.Holds = 0

	p, err := plan.Materialize(31337, cfg)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	deliveries := func(pp *plan.Plan) []string {
		nodes, es := nodesFor(pp)
		run, err := plan.Build(pp, nodes)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		// Traffic on a link between two nodes that are never crashed, so the
		// deleted entry cannot affect it causally either.
		for i := range 400 {
			run.Transport.Send(sim.Envelope{From: 1, To: 2, Body: fmt.Appendf(nil, "m%d", i)})
		}
		if _, err := run.Loop.Run(); err != nil {
			t.Fatalf("run: %v", err)
		}
		return es[2].got
	}

	full := deliveries(p)

	// Delete every fault entry touching node 0, which is not an endpoint of the
	// link under observation.
	trimmed := *p
	trimmed.Faults.Entries = nil
	for _, e := range p.Faults.Entries {
		if e.Node != 0 {
			trimmed.Faults.Entries = append(trimmed.Faults.Entries, e)
		}
	}
	if len(trimmed.Faults.Entries) == len(p.Faults.Entries) {
		t.Skip("this seed produced no node-0 faults to delete")
	}

	after := deliveries(&trimmed)
	if len(full) != len(after) {
		t.Fatalf("deleting an unrelated fault changed link 1->2: %d deliveries then %d", len(full), len(after))
	}
	for i := range full {
		if full[i] != after[i] {
			t.Fatalf("deleting an unrelated fault changed link 1->2 at %d: %q then %q", i, full[i], after[i])
		}
	}
}

// TestHoldRealizationIsFlippable: ddmin has to be able to convert a slew into a
// step and ask whether the bug survives, which it cannot do to a field derived
// from another field.
func TestHoldRealizationIsFlippable(t *testing.T) {
	cfg := plan.DefaultGenConfig()
	cfg.Holds = 1
	p, err := plan.Materialize(99, cfg)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(p.Clock.Holds) == 0 {
		t.Fatal("the generator produced no hold")
	}

	build := func(pp *plan.Plan) error {
		nodes, _ := nodesFor(pp)
		_, err := plan.Build(pp, nodes)
		return err
	}
	if err := build(p); err != nil {
		t.Fatalf("as generated: %v", err)
	}

	// Flip it, exactly as a minimizer would: change the authored field and
	// nothing else. A slew needs a ramp, so flipping to slew supplies one --
	// which is itself the point, since a step carries no ramp to reuse.
	flipped := *p
	flipped.Clock.Holds = append([]plan.Hold(nil), p.Clock.Holds...)
	if flipped.Clock.Holds[0].Realize == "slew" {
		flipped.Clock.Holds[0].Realize = "step"
	} else {
		flipped.Clock.Holds[0].Realize = "slew"
		flipped.Clock.Holds[0].RampNS = int64(3 * time.Second)
		if flipped.Clock.Holds[0].FromNS < flipped.Clock.Holds[0].RampNS {
			flipped.Clock.Holds[0].FromNS = flipped.Clock.Holds[0].RampNS
			flipped.Clock.Holds[0].ToNS = flipped.Clock.Holds[0].FromNS + int64(time.Second)
		}
	}
	if err := build(&flipped); err != nil {
		t.Fatalf("flipped realization: %v", err)
	}
}

// TestReactiveRulesFireOnCondition: a fault whose instant is not known until
// the run produces it stays declarative and individually deletable, so the
// nemesis is a rule generator rather than an escape from the plan.
func TestReactiveRulesFireOnCondition(t *testing.T) {
	p, err := plan.Materialize(5, plan.DefaultGenConfig())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	p.Faults.Entries = nil
	p.Faults.Rules = []plan.Rule{{
		On: "leader_elected", AfterNS: int64(200 * time.Millisecond),
		Action: "cut_both", From: 0, To: 1, Times: 1,
	}}

	nodes, _ := nodesFor(p)
	run, err := plan.Build(p, nodes)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	run.Trigger("leader_elected")
	run.Trigger("leader_elected") // Times: 1, so the second is ignored
	run.Trigger("never_happens")  // no rule, no effect

	before := run.Counters.Count(sim.InjPartition)
	if _, err := run.Loop.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	after := run.Counters.Count(sim.InjPartition)

	if after == before {
		t.Error("a reactive rule did not cut the link it names")
	}
	if after-before != 2 {
		t.Errorf("cut_both fired %d cuts, want 2 (one per direction); Times was not honoured", after-before)
	}
}
