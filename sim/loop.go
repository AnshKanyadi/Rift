package sim

import (
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
)

// Node is what the loop drives. One entry point, synchronous, non-blocking.
//
// This is the shape the whole determinism argument rests on (DESIGN-A0 D1): a
// node is a state machine, the loop calls Handle, Handle runs to completion and
// may schedule future events through the Scheduler it is given. There are no
// goroutines in a node, so a data race in node logic is not unlikely, it is
// unrepresentable — and the same Handle runs in real mode behind a mailbox, so
// the two modes cannot diverge in behaviour.
type Node interface {
	Handle(ev Event, s Scheduler)
}

// Scheduler is what a node may do to the future: schedule an event for itself
// or another node, at or after now. It cannot rewind, cancel arbitrarily, or
// read the queue -- all three would let node logic depend on the loop's
// internals rather than on events.
type Scheduler interface {
	// At schedules an event at an absolute instant, which must not be in the
	// past.
	At(at clock.Instant, kind Kind, node NodeID, payload any)

	// After schedules an event a duration from now.
	After(d time.Duration, kind Kind, node NodeID, payload any)

	// Now is the current global virtual time. Node logic that wants a *node's*
	// time reads its Clock; this is for scheduling arithmetic.
	Now() clock.Instant
}

// Config is one run's shape.
type Config struct {
	// Nodes is the node set. Index is the NodeID.
	Nodes []Node

	// Clocks are the per-node timelines, parallel to Nodes.
	Clocks []*clock.Sim

	// TickInterval is the node-local period between ticks. It is one global
	// constant: per-node tick *rates* emerge from clock drift, which is the
	// point (DESIGN-A0.4 D3).
	TickInterval time.Duration

	// Until bounds the run in global virtual time.
	Until clock.Instant

	// MaxSteps bounds the run in events, as a backstop against a workload that
	// schedules faster than time advances. Zero means no bound, which is only
	// safe for a workload known to terminate.
	MaxSteps uint64
}

// Loop is the deterministic event loop.
//
// Virtual time advances only at event boundaries: computation is instantaneous,
// which is the idealization DESIGN-A0 §7.1 records, and slowness is injected
// explicitly rather than emerging from how long anything really took.
type Loop struct {
	cfg    Config
	q      queue
	now    clock.Instant
	seq    uint64
	steps  uint64
	closed bool

	// down tracks crashed nodes. A crashed node receives no events until it is
	// restarted, and events scheduled for it in the meantime are dropped
	// rather than queued -- a message to a dead process does not wait for it.
	down []bool
}

// NewLoop validates a configuration and returns a loop with each node's first
// tick already scheduled.
func NewLoop(cfg Config) (*Loop, error) {
	if len(cfg.Nodes) == 0 {
		return nil, fmt.Errorf("sim: no nodes")
	}
	if len(cfg.Clocks) != len(cfg.Nodes) {
		return nil, fmt.Errorf("sim: %d nodes but %d clocks", len(cfg.Nodes), len(cfg.Clocks))
	}
	if cfg.TickInterval <= 0 {
		return nil, fmt.Errorf("sim: tick interval must be positive, got %s", cfg.TickInterval)
	}

	// Every node must advertise the same maxOffset. Two nodes with different
	// bounds are each individually self-consistent, so nothing downstream can
	// detect the divergence: it presents as a lease violation that is a harness
	// artifact, or as a real violation a too-generous bound hides (Q2).
	cs := make([]clock.Clock, len(cfg.Clocks))
	for i, c := range cfg.Clocks {
		if c == nil {
			return nil, fmt.Errorf("sim: node %d has no clock", i)
		}
		cs[i] = c
	}
	if err := clock.AssertUniformMaxOffset(cs...); err != nil {
		return nil, err
	}

	l := &Loop{cfg: cfg, down: make([]bool, len(cfg.Nodes))}
	for i := range cfg.Nodes {
		l.scheduleTick(NodeID(i))
	}
	return l, nil
}

// Now is the current global virtual time.
func (l *Loop) Now() clock.Instant { return l.now }

// Steps is how many events have fired.
func (l *Loop) Steps() uint64 { return l.steps }

// Pending is how many events are queued, for checkers and for the quiescence
// question ("did this run stop because it finished, or because nothing was
// left to do?").
func (l *Loop) Pending() int { return l.q.len() }

// At schedules an event. Scheduling into the past is a harness bug, not a
// recoverable condition: it means something computed a deadline from a clock
// that had already passed it, and continuing would silently reorder the run.
func (l *Loop) At(at clock.Instant, kind Kind, node NodeID, payload any) {
	if at < l.now {
		panic(fmt.Sprintf("sim: event scheduled into the past: %s at %d, now %d", kind, at, l.now))
	}
	if int(node) >= len(l.cfg.Nodes) {
		panic(fmt.Sprintf("sim: event scheduled for unknown node %d", node))
	}
	l.seq++
	l.q.push(Event{At: at, Kind: kind, Node: node, Seq: l.seq, Payload: payload})
}

// After schedules an event a duration from now.
func (l *Loop) After(d time.Duration, kind Kind, node NodeID, payload any) {
	l.At(l.now.Add(d), kind, node, payload)
}

// Run drives events until the queue empties, time passes Until, or MaxSteps is
// reached. It returns the reason it stopped, which a caller needs: "the queue
// emptied" and "the clock ran out" are different results, and a run that
// stopped because nothing was scheduled has not necessarily done anything.
func (l *Loop) Run() (Reason, error) {
	if l.closed {
		return 0, fmt.Errorf("sim: Run on a finished loop")
	}
	for {
		next, ok := l.q.peek()
		if !ok {
			l.closed = true
			return ReasonQuiescent, nil
		}
		if l.cfg.Until != 0 && next.At > l.cfg.Until {
			l.closed = true
			l.now = l.cfg.Until
			return ReasonDeadline, nil
		}
		if l.cfg.MaxSteps != 0 && l.steps >= l.cfg.MaxSteps {
			l.closed = true
			return ReasonStepLimit, nil
		}
		l.step()
	}
}

// step fires exactly one event. Exported behaviour lives in Run; this is
// separate so a test can single-step a run.
func (l *Loop) step() {
	ev, ok := l.q.pop()
	if !ok {
		return
	}

	// Time never moves backwards. The queue's total order guarantees it, and
	// asserting it here is what turns a broken comparison into a loud failure
	// rather than a subtly different history.
	if ev.At < l.now {
		panic(fmt.Sprintf("sim: virtual time moved backwards, from %d to %d", l.now, ev.At))
	}
	l.now = ev.At
	l.steps++

	for _, c := range l.cfg.Clocks {
		c.Advance(l.now)
	}

	switch ev.Kind {
	case KindCrash:
		l.down[ev.Node] = true
	case KindRestart:
		l.down[ev.Node] = false
		// A restart begins a new boot, so the tick schedule restarts with it.
		l.scheduleTick(ev.Node)
	}

	// A crashed node receives nothing. The event is dropped rather than
	// deferred: a message to a dead process does not wait for it to come back,
	// and a timer that fired while it was down did not fire.
	if l.down[ev.Node] && ev.Kind != KindRestart {
		return
	}

	l.cfg.Nodes[ev.Node].Handle(ev, l)

	if ev.Kind == KindTick {
		l.scheduleTick(ev.Node)
	}
}

// scheduleTick enqueues a node's next tick, taken from its own clock.
//
// This is where A0.4's closed-form inversion meets the loop, and it is why
// drift shapes the tick rate rather than only the reported time: a node whose
// oscillator runs fast reaches its next tick earlier in global time, so it
// campaigns and heartbeats faster, which is what the injector is for.
// The next tick is scheduled even when it falls past Until. Suppressing it
// there would make a tick-only run end with an empty queue and report itself
// quiescent, erasing the difference between "nothing was left to do" and "the
// clock ran out" -- which is the distinction Run exists to report, and the one
// that tells you whether a run went quiet early.
func (l *Loop) scheduleTick(node NodeID) {
	at, _ := l.cfg.Clocks[node].NextTick(l.cfg.TickInterval)
	l.At(at, KindTick, node, nil)
}

// Crash and Restart are scheduling helpers for injectors and tests. They are
// events like everything else, so a crash is ordered against the messages in
// flight at that instant rather than applied out of band.
func (l *Loop) Crash(at clock.Instant, node NodeID)   { l.At(at, KindCrash, node, nil) }
func (l *Loop) Restart(at clock.Instant, node NodeID) { l.At(at, KindRestart, node, nil) }

// Down reports whether a node is currently crashed.
func (l *Loop) Down(node NodeID) bool { return l.down[node] }

// Reason is why a run stopped.
type Reason uint8

const (
	// ReasonQuiescent means the queue emptied. Worth distinguishing: a run
	// that went quiet early did less than its duration suggests.
	ReasonQuiescent Reason = iota + 1
	ReasonDeadline
	ReasonStepLimit
)

func (r Reason) String() string {
	switch r {
	case ReasonQuiescent:
		return "quiescent"
	case ReasonDeadline:
		return "deadline"
	case ReasonStepLimit:
		return "step-limit"
	}
	return "unknown"
}

var _ Scheduler = (*Loop)(nil)
