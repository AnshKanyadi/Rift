package chaos_test

import (
	"testing"
	"time"

	"github.com/anshkanyadi/rift/chaos"
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
)

// stepClock is a monotonic source that advances by a fixed step per read, so a
// test can assert ORDER without depending on how fast the machine is.
type stepClock struct {
	n    int64
	step int64
}

func (c *stepClock) Mono() clock.Mono         { c.n += c.step; return clock.Mono(c.n) }
func (c *stepClock) Wall() clock.Wall         { return clock.Wall(c.n) }
func (c *stepClock) MaxOffset() time.Duration { return 0 }

func TestEveryInstantComesFromOneSource(t *testing.T) {
	h := &sim.History{}
	clk := &stepClock{step: 10}
	c := chaos.NewClient(1, clk, h)

	a := c.Begin("put", "k1", "v1")
	b := c.Begin("get", "k2", "")
	c.End(b, sim.RespOK, "v2")
	c.End(a, sim.RespOK, "")

	evs := h.Events()
	if len(evs) != 2 {
		t.Fatalf("got %d events", len(evs))
	}
	for _, e := range evs {
		if e.Call == 0 {
			t.Errorf("an operation has no call instant: %+v", e)
		}
		if e.Return <= e.Call {
			t.Errorf("an operation returned at or before it was issued: call=%d return=%d.\n"+
				"      That is the artifact a per-node-clock history produces, and a checker "+
				"would report it as a violation of the SYSTEM rather than of the measurement",
				e.Call, e.Return)
		}
	}
}

// TestATimedOutOperationStaysInTheHistory.
//
// Dropping it would make the history smaller, cleaner and wrong: the dropped
// operation is exactly the one that might have taken effect invisibly, which is
// the case a chaos run exists to reach.
func TestATimedOutOperationStaysInTheHistory(t *testing.T) {
	h := &sim.History{}
	c := chaos.NewClient(1, &stepClock{step: 5}, h)

	ok := c.Begin("put", "k", "v")
	c.End(ok, sim.RespOK, "")
	lost := c.Begin("put", "k", "w")
	c.End(lost, sim.RespTimeout, "")

	evs := h.Events()
	if len(evs) != 2 {
		t.Fatalf("a timed-out operation was dropped: %d events, want 2", len(evs))
	}
	var sawTimeout bool
	for _, e := range evs {
		if e.Outcome == sim.RespTimeout {
			sawTimeout = true
		}
	}
	if !sawTimeout {
		t.Error("the timeout was recorded as something else; a checker must be able to treat it " +
			"as may-or-may-not-have-happened")
	}
	got := c.Counters()
	if got.Completed != 1 || got.Failed != 1 || got.Issued != 2 {
		t.Errorf("counters %+v: a timeout must count as issued and failed, never as completed", got)
	}
}

// TestAForgottenOutcomeFailsAtTheCallSite.
//
// RespUnset is the zero value, and History.Validate rejects it later. Refusing
// it here means a forgotten field is attributed to the code that forgot it,
// rather than surfacing at the end of a chaos run as an invalid history with no
// author.
func TestAForgottenOutcomeFailsAtTheCallSite(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ending an operation with RespUnset was accepted; a forgotten outcome would " +
				"then read as a decision")
		}
	}()
	c := chaos.NewClient(1, &stepClock{step: 1}, &sim.History{})
	c.End(c.Begin("put", "k", "v"), sim.RespUnset, "")
}

func TestKeysAreCountedDistinctly(t *testing.T) {
	c := chaos.NewClient(1, &stepClock{step: 1}, &sim.History{})
	for _, k := range []string{"a", "b", "a", "c", "b"} {
		c.End(c.Begin("put", k, "v"), sim.RespOK, "")
	}
	if got := c.Counters().Keys; got != 3 {
		t.Errorf("Keys=%d, want 3: the gate uses this to catch a history that is non-vacuous by "+
			"count and vacuous by content", got)
	}
}
