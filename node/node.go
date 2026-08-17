// Package node is the real-mode driver: it runs node logic against wall-clock
// time and real concurrency, behind a mailbox.
//
// # Why this package exists at all
//
// The mailbox rule (DESIGN-A0 DR-2, Amendment A1) says that in real mode every
// cross-goroutine interaction — transport receive, durability completion, timer
// fire, client request — enters a node through its mailbox, and core state
// touched off the node loop is a bug. Until this package existed the rule was
// enforced against fixtures only, and `tools/determinismcheck/scope.go` marked
// it provisional in those words: *A0 does not exit until node/ exists and the
// rule has end-to-end teeth.* A rule proven only against testdata is a rule that
// has never met real code.
//
// # The load-bearing property: one Node interface, two modes
//
// `Driver` drives a `sim.Node` — **the same interface the simulator's loop
// drives, with the same `Handle(Event, Scheduler)` signature.** There is no build
// tag, no `if realMode`, and no second implementation of node logic. The toy that
// runs under a thousand seeded schedules is byte-for-byte the toy that runs here.
//
// That is not tidiness, it is the entire argument. If real mode needed its own
// copy of the protocol, the deterministic simulation would be verifying a
// program that never ships, and every seed in the corpus would be evidence about
// the wrong artifact. The two modes differ only in *who calls Handle and when*:
//
//	sim mode   one goroutine, virtual time, events from a seeded queue
//	real mode  one goroutine per node, wall time, events from a mailbox
//
// Node logic cannot tell the difference, because the only things it may touch are
// the `Event` it is handed and the `Scheduler` it is given.
//
// # Why the concurrency is here and not there
//
// Amendment A5: code that needs a goroutine is orchestration and lives outside
// the determinism boundary, or the design is wrong. This package is named in
// `determinismcheck`'s exclusion list for exactly that reason, and
// `TestScopeTable` pins the polarity — `node/` out, everything it drives in. The
// `-race` lane is the backstop.
//
// The mailbox is the seam that makes that split safe. Exactly one goroutine ever
// calls `Handle`, so node state is touched by one goroutine and the concurrency
// stops at the channel.
package node

import (
	"fmt"
	"sync"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
)

// Driver runs one node in real mode.
//
// It is both the mailbox and the node's Scheduler, which is what lets the same
// Handle run under it: whatever node logic schedules comes back through the same
// door everything else arrives by.
type Driver struct {
	id    sim.NodeID
	node  sim.Node
	clock clock.Clock

	mbox chan sim.Event

	mu      sync.Mutex
	started bool
	stopped bool
	timers  []*time.Timer

	done chan struct{}

	// handled counts events the node loop has processed, for tests that need to
	// wait for quiescence without sleeping on a guess.
	handledMu sync.Mutex
	handled   uint64
	handledCh chan struct{}
}

// New builds a driver. depth bounds the mailbox; a full mailbox blocks the
// sender rather than dropping, because a silently dropped message is a fault
// injector nobody configured.
func New(id sim.NodeID, n sim.Node, c clock.Clock, depth int) (*Driver, error) {
	if n == nil {
		return nil, fmt.Errorf("node: driver for %d has no node", id)
	}
	if c == nil {
		return nil, fmt.Errorf("node: driver for %d has no clock", id)
	}
	if depth <= 0 {
		return nil, fmt.Errorf("node: mailbox depth must be positive, got %d", depth)
	}
	return &Driver{
		id: id, node: n, clock: c,
		mbox:      make(chan sim.Event, depth),
		done:      make(chan struct{}),
		handledCh: make(chan struct{}, 1),
	}, nil
}

// Start launches the node loop. Exactly one goroutine ever calls Handle, which
// is what makes the mailbox rule true rather than aspirational.
func (d *Driver) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return fmt.Errorf("node: driver %d already started", d.id)
	}
	d.started = true
	go d.loop()
	return nil
}

func (d *Driver) loop() {
	for {
		select {
		case ev := <-d.mbox:
			// The one call site. Node state is reached from this goroutine and
			// no other, so a data race in node logic is unrepresentable here for
			// the same reason it is in sim mode.
			d.node.Handle(ev, d)
			d.bumpHandled()
		case <-d.done:
			return
		}
	}
}

// Post is the mailbox: the only way anything enters this node.
//
// Transport receives, durability completions, timer fires and client requests
// all arrive through here. There is deliberately no second entry point, because
// a second entry point is the rule's failure mode.
func (d *Driver) Post(ev sim.Event) {
	select {
	case <-d.done:
	case d.mbox <- ev:
	}
}

// Stop halts the loop and cancels outstanding timers.
func (d *Driver) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.stopped = true
	for _, t := range d.timers {
		t.Stop()
	}
	close(d.done)
}

// Handled is how many events the loop has processed.
func (d *Driver) Handled() uint64 {
	d.handledMu.Lock()
	defer d.handledMu.Unlock()
	return d.handled
}

func (d *Driver) bumpHandled() {
	d.handledMu.Lock()
	d.handled++
	d.handledMu.Unlock()
	select {
	case d.handledCh <- struct{}{}:
	default:
	}
}

// WaitFor blocks until the loop has handled at least n events, or the timeout
// elapses. It exists so tests wait on a condition rather than on a sleep, which
// is the difference between a test and a flake.
func (d *Driver) WaitFor(n uint64, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if d.Handled() >= n {
			return true
		}
		select {
		case <-d.handledCh:
		case <-deadline:
			return d.Handled() >= n
		}
	}
}

// --- Scheduler ---------------------------------------------------------------
//
// The same interface the simulator hands node logic. In sim mode these enqueue
// into the event queue at a virtual instant; here they arm a real timer that
// posts back through the mailbox. Node logic cannot tell which it is talking to,
// which is the property that lets one implementation serve both modes.

var _ sim.Scheduler = (*Driver)(nil)

// Now is this node's current time, taken from its injected clock.
//
// Real mode reads the clock; it does not read the wall directly. That keeps the
// single wall-clock touchpoint in clock.Real, where the hatch registry can see
// it, rather than scattering time.Now through the driver.
func (d *Driver) Now() clock.Instant { return clock.Instant(d.clock.Mono()) }

// At schedules an event for an absolute instant on this node's clock.
func (d *Driver) At(at clock.Instant, kind sim.Kind, n sim.NodeID, payload any) {
	delay := time.Duration(at - d.Now())
	if delay < 0 {
		delay = 0
	}
	d.After(delay, kind, n, payload)
}

// After schedules an event a duration from now.
//
// The timer fires on the runtime's own goroutine and posts into the mailbox
// rather than calling Handle: a timer that invoked node logic directly would be
// a second goroutine touching node state, which is precisely the bug the mailbox
// rule exists to make impossible.
func (d *Driver) After(delay time.Duration, kind sim.Kind, n sim.NodeID, payload any) {
	at := d.Now().Add(delay)
	t := time.AfterFunc(delay, func() {
		d.Post(sim.Event{At: at, Kind: kind, Node: n, Payload: payload})
	})
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		t.Stop()
		return
	}
	d.timers = append(d.timers, t)
	d.mu.Unlock()
}
