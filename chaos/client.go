package chaos

import (
	"sync"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
)

// Client records what a real cluster was asked and what it answered, as a
// sim.History the existing checkers read unchanged.
//
// # ONE MONOTONIC SOURCE, and it is the reason this type exists at all
//
// DESIGN-I2 §2.2: a chaos history must be collected with the same fidelity as a
// simulated one, or porcupine is being handed a different KIND of object under
// the same name.
//
//	A HISTORY ASSEMBLED FROM PER-NODE CLOCKS IS A DIFFERENT PROBLEM WEARING THE
//	SAME NAME. Linearizability is a statement about a single order of events; if
//	invocation came from one machine's clock and response from another's, an
//	operation can appear to return before it was issued, and the checker will
//	report a violation that is an artifact of the measurement.
//
// So every instant here comes from ONE clock -- the client's -- and node clocks
// never touch the history.
//
//	THE CLUSTER'S SKEW IS THE SYSTEM UNDER TEST, NOT THE MEASUREMENT.
//
// # AND THIS IS ORACLE INDEPENDENCE, APPLIED TO THE TIMEBASE
//
// That rule has always been about the DATA an oracle reads: the vacuous-green
// register's eighth entry is "the oracle asked the accused", where a durability
// checker compared a node's claims against the engine's own view of what it
// held. This is the same rule one axis over.
//
//	AN ORACLE THAT TAKES ITS TIMEBASE FROM THE SYSTEM IT IS CHECKING IS ASKING
//	THE ACCUSED WHAT TIME IT IS.
//
// In a run whose whole subject is clock skew, a history stitched from per-node
// clocks produces violations that are artifacts of the instrument -- and they
// look exactly like the finding the phase is hunting for, which is the worst
// possible way to be wrong.
//
// # An unknown outcome is RECORDED, never dropped
//
// A timeout is not a violation: a partitioned cluster that stops answering is
// behaving correctly. But the operation stays in the history, because a checker
// must treat it as "may or may not have happened".
//
//	DROPPING A TIMED-OUT OPERATION MAKES THE HISTORY SMALLER, CLEANER, AND
//	WRONG. The dropped operation is exactly the one that might have taken effect
//	invisibly, which is the case a chaos run exists to reach.
type Client struct {
	id  int
	clk clock.Clock

	mu   sync.Mutex
	h    *sim.History
	seq  uint64
	keys map[string]struct{}

	issued, completed, failed int
}

// NewClient builds a recorder over one history.
//
// The clock is injected rather than read from the wall directly, so this type
// stays testable with a fake and so there is exactly one place the history's
// time comes from.
func NewClient(id int, clk clock.Clock, h *sim.History) *Client {
	return &Client{id: id, clk: clk, h: h, keys: map[string]struct{}{}}
}

// Begin records an invocation and returns its handle.
func (c *Client) Begin(op, key, value string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.issued++
	c.keys[key] = struct{}{}
	return c.h.Begin(clock.Instant(c.clk.Mono()), c.id, c.seq, op, key, value)
}

// End records a response.
//
// kind must be a real outcome. sim.RespUnset is refused HERE rather than left to
// History.Validate, and the placement is the point.
//
//	A CHECK THAT FIRES FAR FROM THE MISTAKE REPORTS A SYMPTOM WITHOUT A SUSPECT.
//
// Validate would reject it too -- at the END of a chaos run, as an invalid
// history with no author, after the cluster is gone and the operation that
// forgot its outcome is one of tens of thousands. Failing at the call site costs
// a panic and names the caller.
func (c *Client) End(handle int, kind sim.ResponseKind, value string) {
	if kind == sim.RespUnset {
		panic("chaos: an operation was ended with RespUnset; a forgotten outcome must not read as a decision")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if kind == sim.RespOK {
		c.completed++
	} else {
		c.failed++
	}
	c.h.End(handle, clock.Instant(c.clk.Mono()), kind, value)
}

// Counters is the client half of the gate.
func (c *Client) Counters() OpCounters {
	c.mu.Lock()
	defer c.mu.Unlock()
	return OpCounters{
		Issued:    c.issued,
		Completed: c.completed,
		Failed:    c.failed,
		Keys:      len(c.keys),
	}
}
