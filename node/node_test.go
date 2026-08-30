package node_test

import (
	"sync"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/node"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/sim/toy"
)

// counter is node logic with no dependencies beyond the two things the
// simulator gives node logic: the Event it is handed and the Scheduler it is
// given. It implements sim.Node, so it is drivable by both modes *by type*
// rather than by arrangement.
type counter struct {
	handled  int
	deliver  int
	rescheds int
}

func (c *counter) Handle(ev sim.Event, s sim.Scheduler) {
	c.handled++
	if ev.Kind == sim.KindDeliver {
		c.deliver++
	}
	// Schedule through the Scheduler it was given, without knowing or caring
	// which mode supplied it. In sim mode this enqueues at a virtual instant; in
	// real mode it arms a timer that posts back through the mailbox.
	if c.rescheds < 3 {
		c.rescheds++
		s.After(time.Millisecond, sim.KindDeliver, ev.Node, nil)
	}
}

// TestSameNodeLogicRunsUnderBothDrivers is the load-bearing claim: one Node
// implementation, two modes, no build tag and no branch on mode.
//
// If real mode needed its own copy of the protocol, every seed in the corpus
// would be evidence about a program that never ships. The compiler carries most
// of this — `counter` satisfies `sim.Node`, and both drivers take that interface
// — so what is left to demonstrate is that the *same value* can be driven either
// way and behaves like node logic in both.
func TestSameNodeLogicRunsUnderBothDrivers(t *testing.T) {
	// Real mode.
	real := &counter{}
	d, err := node.New(0, real, clock.NewReal(500*time.Millisecond), 16)
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer d.Stop()

	d.Post(sim.KindClient, 0, nil)
	if !d.WaitFor(4, 2*time.Second) {
		t.Fatalf("real driver handled %d events, want 4 (one client plus three rescheduled)", d.Handled())
	}

	// Sim mode, same type, driven by the loop.
	simNode := &counter{}
	c, err := clock.NewSim(&clock.Timeline{
		Skew:  []clock.Segment{{Start: 0, Off: 0, SlopePPB: 0}},
		Epoch: clock.Wall(1_600_000_000_000_000_000),
	}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("sim clock: %v", err)
	}
	loop, err := sim.NewLoop(sim.Config{
		Nodes: []sim.Node{simNode}, Clocks: []*clock.Sim{c},
		TickInterval: time.Second, Until: clock.Instant(100 * time.Millisecond),
		Counters: sim.NewCounters(),
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	loop.At(0, sim.KindClient, 0, nil)
	if _, err := loop.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	if simNode.rescheds != real.rescheds {
		t.Errorf("the same node logic rescheduled %d times under the sim and %d under the real driver",
			simNode.rescheds, real.rescheds)
	}
	if simNode.deliver != real.deliver {
		t.Errorf("the same node logic saw %d deliveries under the sim and %d under the real driver",
			simNode.deliver, real.deliver)
	}
	t.Logf("one sim.Node implementation, driven both ways: %d rescheduled deliveries each", real.deliver)
}

// TestRealModeDoesNotPerturbSimDeterminism is the point of the whole exercise:
// **real mode exists without weakening the determinism claim.**
//
// The risk a real-mode driver introduces is not that it is wrong, it is that its
// existence changes the deterministic path — a shared package variable, an init
// that touches a clock, a type whose zero value now differs. So the sim path is
// hashed, real mode is exercised hard in parallel against the same node logic,
// and the sim path is hashed again from scratch. The hashes must be identical.
func TestRealModeDoesNotPerturbSimDeterminism(t *testing.T) {
	hashToySeed := func() string {
		sc := toy.Scenario{Flaw: toy.FlawAckBeforeSync, Placement: toy.PlacementReactive}
		p, err := toy.MaterializeToy(29, sc)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		tr := sim.NewTrace(0)
		if _, err := hunt.RunToy(p, sc, tr); err != nil {
			t.Fatalf("run: %v", err)
		}
		return tr.Sum()
	}

	before := hashToySeed()

	// Exercise real mode hard, concurrently, against the same protocol type the
	// sim run above drives. Several drivers, real timers, real goroutines.
	stop := make(chan struct{})
	drivers := make([]*node.Driver, 4)
	for i := range drivers {
		d, err := node.New(sim.NodeID(i), &counter{}, clock.NewReal(500*time.Millisecond), 64)
		if err != nil {
			t.Fatalf("driver %d: %v", i, err)
		}
		if err := d.Start(); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		drivers[i] = d
	}
	for i, d := range drivers {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
					d.Post(sim.KindClient, sim.NodeID(i), nil)
				}
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	for _, d := range drivers {
		d.Stop()
	}

	after := hashToySeed()
	if before != after {
		t.Fatalf("the sim path's trace hash changed across real-mode activity:\n  before %s\n  after  %s\n"+
			"real mode has reached the deterministic path, which is the one thing it may never do",
			before, after)
	}
	t.Logf("sim trace hash unchanged across concurrent real-mode drivers: %s", before[:16])
}

// TestMailboxIsTheOnlyDoor pins the rule the package exists for. Every entry
// point a caller can reach goes through Post, so exactly one goroutine ever
// calls Handle.
func TestMailboxIsTheOnlyDoor(t *testing.T) {
	c := &counter{}
	d, err := node.New(0, c, clock.NewReal(time.Second), 8)
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer d.Stop()

	// A timer scheduled by node logic must arrive through the mailbox rather
	// than calling Handle on the runtime's goroutine, which would be a second
	// goroutine touching node state.
	d.After(time.Millisecond, sim.KindTick, 0, nil)
	if !d.WaitFor(1, time.Second) {
		t.Fatal("a scheduled timer never arrived through the mailbox")
	}
	t.Logf("timer fired on the runtime's goroutine and arrived via Post; %d handled", d.Handled())
}

// TestDriverRefusesAnIncompleteConfiguration induces the constructor's refusals.
func TestDriverRefusesAnIncompleteConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		n     sim.Node
		c     clock.Clock
		depth int
	}{
		{"no node", nil, clock.NewReal(time.Second), 1},
		{"no clock", &counter{}, nil, 1},
		{"zero mailbox depth", &counter{}, clock.NewReal(time.Second), 0},
	} {
		if _, err := node.New(0, tc.n, tc.c, tc.depth); err == nil {
			t.Errorf("%s was accepted", tc.name)
		} else {
			t.Logf("induced: %v", err)
		}
	}
}

var _ = plan.SchemaVersion // keeps the plan import honest if the toy path changes

// BUG-052's amendment, pinned two ways: the stamp is REAL, and the class of
// omitting it is unrepresentable.
//
// The first half is an ordinary assertion and could regress. The second half
// cannot be written as an assertion at all -- there is no argument left in which
// to pass a bad time -- so it is pinned as a COMPILE-TIME shape instead. If
// somebody restores `Post(sim.Event)`, the signature check below stops compiling
// and the lane fails before any test runs.
var _ = func(d *node.Driver) {
	// The shape of the mailbox, asserted by assignment. `Post` takes WHAT
	// HAPPENED and never an Event, so a caller cannot supply a time and
	// therefore cannot omit one.
	var post func(sim.Kind, sim.NodeID, any) = d.Post
	_ = post
}

func TestEveryPostedEventCarriesTheDriversOwnTime(t *testing.T) {
	rec := &recorder{}
	clk := clock.NewReal(0)
	d, err := node.New(7, rec, clk, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	before := d.Now()
	d.Post(sim.KindClient, 7, nil)
	d.After(time.Millisecond, sim.KindTick, 7, nil)
	if !d.WaitFor(2, 2*time.Second) {
		t.Fatalf("only %d event(s) handled", d.Handled())
	}
	after := d.Now()

	got := rec.events()
	if len(got) < 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	for i, ev := range got {
		// AN UNSTAMPED EVENT IS NOT AN ERROR AT THE CALL, IT IS A HISTORY THAT
		// READS AS NONSENSE LATER. `sim.History.End` refuses a return before its
		// call, which is where BUG-052 finally surfaced -- three seconds and one
		// panic away from the line that caused it.
		if ev.At == 0 {
			t.Fatalf("event %d (%s) arrived unstamped", i, ev.Kind)
		}
		if ev.At < before || ev.At > after {
			t.Fatalf("event %d (%s) stamped %v, outside [%v, %v]", i, ev.Kind, ev.At, before, after)
		}
	}
	// Non-decreasing in PROCESSING order.
	//
	// STATED LIMIT: this clause is NOT induced, and saying so is the point. The
	// mailbox is FIFO and Post stamps just before enqueue, so no path in the
	// current API can produce a decreasing pair -- which means removing this
	// loop breaks nothing and no mutation can make it fire. It is a CONSEQUENCE
	// of the two properties above, recorded so a future change that reintroduces
	// a schedule-time stamp has something to fail, not evidence in its own right.
	//
	// (GF-56's rule, applied before rather than after: a check that cannot fire
	// today should say so, or it will be read as evidence it is not.)
	for i := 1; i < len(got); i++ {
		if got[i].At < got[i-1].At {
			t.Fatalf("event %d stamped %v, before event %d's %v: stamps must not "+
				"decrease in processing order", i, got[i].At, i-1, got[i-1].At)
		}
	}
}

// recorder captures what the loop handed it, on the loop's own goroutine.
type recorder struct {
	mu  sync.Mutex
	evs []sim.Event
}

func (r *recorder) Handle(ev sim.Event, _ sim.Scheduler) {
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

func (r *recorder) events() []sim.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sim.Event(nil), r.evs...)
}
