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

	live                                               map[uint64]*inflight
	unissued, duplicate, conflicting, lateAfterTimeout int
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

// # Correlation, and the two cases that are NOT the happy path
//
// The history rests on a three-way correlation: a Begin, a response off the
// wire, and the End that closes the operation. Begin and End are local calls;
// the response is the only one of the three that arrives from somewhere else,
// which makes it the only one that can be wrong. Two ways it can be:
//
//	(i)  A RESPONSE FOR AN OPERATION THE CLIENT HAS NO RECORD OF.
//	(ii) AN OPERATION THAT GETS TWO RESPONSES.
//
// Both are unknown-outcome cases in exactly the family the timeout belongs to,
// and the rule the timeout already established governs them:
//
//	DROPPING EITHER MAKES THE HISTORY SMALLER, CLEANER, AND WRONG.
//
// A dropped stray response is a fact about the cluster erased from the only
// record of the run. So neither is dropped. Each is classified, counted, and
// carried to the verdict alongside the checker results.
//
// The classification, and why each lands where it does:
//
//   - UNISSUED -- a seq this client never handed out. There is no invocation
//     time for it, so it CANNOT become a history operation; inventing one would
//     be fabricating the very timestamps the history exists to record. It is
//     counted and it is LOUD: the only ways to get one are a node answering a
//     request nobody made, a transport delivering another client's traffic here,
//     or seq allocation broken. None of those are acceptable behaviour.
//
//   - CONFLICTING -- a second response that DISAGREES with the recorded one.
//     Two different answers to one request is a safety claim failing, so this is
//     loud too. It is caught here rather than left to porcupine because
//     porcupine never sees the second answer: the history holds one response per
//     operation by construction, and the disagreement would vanish into the
//     dropped duplicate. A CHECK THAT CANNOT FIRE IS NOT A CHECK.
//
//   - DUPLICATE -- a second response that AGREES. A lossy, reordering wire with
//     retries produces these legitimately. Counted, not loud.
//
//   - LATE-AFTER-TIMEOUT -- a response arriving after the operation was already
//     ended as RespTimeout. Never conflicting: RespTimeout already means "may or
//     may not have happened", which subsumes any answer that turns up later.
//     Counted separately because it is the operation the run most wants to know
//     about, and because folding it into Duplicate would hide it.
//
// The FIRST response decides the outcome. Not the last, and not the best: the
// history's response instant must be the moment the client LEARNED the outcome,
// and a later arrival did not teach it anything it did not already know.

// Outcome is what Correlate did with a response.
type Outcome int

const (
	// Matched: the response closed an in-flight operation.
	Matched Outcome = iota
	// Unissued: no such seq was ever issued by this client.
	Unissued
	// Duplicate: a second, agreeing response.
	Duplicate
	// Conflicting: a second, DISAGREEING response.
	Conflicting
	// LateAfterTimeout: a response after the operation timed out.
	LateAfterTimeout
)

func (o Outcome) String() string {
	switch o {
	case Matched:
		return "matched"
	case Unissued:
		return "unissued"
	case Duplicate:
		return "duplicate"
	case Conflicting:
		return "conflicting"
	case LateAfterTimeout:
		return "late-after-timeout"
	}
	return "unknown"
}

// inflight is what the client remembers about one issued operation.
//
// Records are NEVER reaped. A client that forgets an operation turns case (ii)
// into case (i) -- a duplicate the client can no longer recognise reports as a
// fabricated response -- and the loud counter starts firing for a reason that is
// the client's own fault. Memory is cheaper than a false accusation.
type inflight struct {
	handle int
	ended  bool
	kind   sim.ResponseKind
	value  string
}

// BeginSeq records an invocation and returns both its history handle and the seq
// that will identify its response on the wire.
func (c *Client) BeginSeq(op, key, value string) (handle int, seq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.issued++
	c.keys[key] = struct{}{}
	h := c.h.Begin(clock.Instant(c.clk.Mono()), c.id, c.seq, op, key, value)
	if c.live == nil {
		c.live = map[uint64]*inflight{}
	}
	c.live[c.seq] = &inflight{handle: h}
	return h, c.seq
}

// Correlate takes a response off the wire and files it.
//
// It returns what it decided, so a caller that wants to react (a retry loop, a
// test) can, and so the decision is never inferred from a counter delta.
func (c *Client) Correlate(seq uint64, kind sim.ResponseKind, value string) Outcome {
	if kind == sim.RespUnset {
		panic("chaos: a response was correlated with RespUnset; a forgotten outcome must not read as a decision")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	f := c.live[seq]
	if f == nil {
		c.unissued++
		return Unissued
	}
	if !f.ended {
		f.ended, f.kind, f.value = true, kind, value
		if kind == sim.RespOK {
			c.completed++
		} else {
			c.failed++
		}
		c.h.End(f.handle, clock.Instant(c.clk.Mono()), kind, value)
		return Matched
	}
	if f.kind == sim.RespTimeout {
		c.lateAfterTimeout++
		return LateAfterTimeout
	}
	if f.kind != kind || f.value != value {
		c.conflicting++
		return Conflicting
	}
	c.duplicate++
	return Duplicate
}

// Timeout ends an operation as an unknown outcome, keeping its record so a late
// response is still recognised as ITS response rather than as a stray.
func (c *Client) Timeout(seq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.live[seq]
	if f == nil || f.ended {
		return
	}
	f.ended, f.kind = true, sim.RespTimeout
	c.failed++
	c.h.End(f.handle, clock.Instant(c.clk.Mono()), sim.RespTimeout, "")
}

// Correlation is the stray-response half of the report. It travels to the
// verdict beside the checker results, because a green checker over a history
// that quietly discarded four disagreeing responses is not a green run.
type Correlation struct {
	Unissued         int
	Duplicate        int
	Conflicting      int
	LateAfterTimeout int
}

// Loud reports whether anything here is a defect rather than wire weather.
func (c Correlation) Loud() bool { return c.Unissued > 0 || c.Conflicting > 0 }

// Correlation returns the stray-response counters.
func (c *Client) Correlation() Correlation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Correlation{
		Unissued:         c.unissued,
		Duplicate:        c.duplicate,
		Conflicting:      c.conflicting,
		LateAfterTimeout: c.lateAfterTimeout,
	}
}
