package chaos_test

import (
	"testing"

	"github.com/anshkanyadi/rift/chaos"
	"github.com/anshkanyadi/rift/sim"
)

func newTestClient() (*chaos.Client, *sim.History) {
	h := &sim.History{}
	return chaos.NewClient(1, &stepClock{step: 10}, h), h
}

func TestTheHappyPathIsMatchedExactlyOnce(t *testing.T) {
	c, h := newTestClient()
	_, seq := c.BeginSeq("put", "k", "v")
	if got := c.Correlate(seq, sim.RespOK, "v"); got != chaos.Matched {
		t.Fatalf("first response = %v, want matched", got)
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("history invalid: %v", err)
	}
	if c.Correlation() != (chaos.Correlation{}) {
		t.Fatalf("a clean exchange produced stray counters: %+v", c.Correlation())
	}
}

// Case (i): a response for an operation the client has no record of.
func TestAResponseForAnOperationNeverIssuedIsLoud(t *testing.T) {
	c, h := newTestClient()
	c.BeginSeq("put", "k", "v")
	if got := c.Correlate(9999, sim.RespOK, "v"); got != chaos.Unissued {
		t.Fatalf("stray response = %v, want unissued", got)
	}
	if got := c.Correlation(); got.Unissued != 1 || !got.Loud() {
		t.Fatalf("stray response was not loud: %+v", got)
	}
	// And it did not become an operation. Inventing an invocation time for a
	// response nobody asked for would fabricate the exact thing the history is
	// the record of.
	if n := h.Len(); n != 1 {
		t.Fatalf("stray response entered the history: %d ops", n)
	}
}

// Case (ii), agreeing: wire weather, counted, quiet.
func TestASecondAgreeingResponseIsCountedAndQuiet(t *testing.T) {
	c, h := newTestClient()
	_, seq := c.BeginSeq("get", "k", "")
	c.Correlate(seq, sim.RespOK, "v")
	if got := c.Correlate(seq, sim.RespOK, "v"); got != chaos.Duplicate {
		t.Fatalf("duplicate = %v, want duplicate", got)
	}
	cc := c.Correlation()
	if cc.Duplicate != 1 || cc.Loud() {
		t.Fatalf("an agreeing duplicate was mishandled: %+v", cc)
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("the duplicate corrupted the history: %v", err)
	}
}

// Case (ii), disagreeing: two different answers to one request.
//
// This is the case that motivates the whole mechanism. Porcupine cannot catch
// it -- the history holds one response per operation by construction, so the
// second answer never reaches the checker at all.
func TestASecondDISAGREEINGResponseIsLoud(t *testing.T) {
	c, _ := newTestClient()
	_, seq := c.BeginSeq("get", "k", "")
	c.Correlate(seq, sim.RespOK, "v1")
	if got := c.Correlate(seq, sim.RespOK, "v2"); got != chaos.Conflicting {
		t.Fatalf("disagreeing response = %v, want conflicting", got)
	}
	if got := c.Correlation(); got.Conflicting != 1 || !got.Loud() {
		t.Fatalf("a disagreement was not loud: %+v", got)
	}
}

func TestADifferingSTATUSAlsoConflicts(t *testing.T) {
	c, _ := newTestClient()
	_, seq := c.BeginSeq("get", "k", "")
	c.Correlate(seq, sim.RespOK, "")
	if got := c.Correlate(seq, sim.RespError, ""); got != chaos.Conflicting {
		t.Fatalf("status disagreement = %v, want conflicting", got)
	}
}

// The first response decides. Not the last, and not the best.
func TestTheFirstResponseDecidesTheOutcome(t *testing.T) {
	c, h := newTestClient()
	handle, seq := c.BeginSeq("get", "k", "")
	c.Correlate(seq, sim.RespOK, "first")
	c.Correlate(seq, sim.RespOK, "second")
	op := h.Events()[handle]
	if op.Value != "first" {
		t.Fatalf("the history kept %q; the first response must decide", op.Value)
	}
}

// A late response after a timeout is not a conflict: RespTimeout already means
// "may or may not have happened", and any answer is inside that.
func TestALateResponseAfterATimeoutIsNotAConflict(t *testing.T) {
	c, _ := newTestClient()
	_, seq := c.BeginSeq("put", "k", "v")
	c.Timeout(seq)
	if got := c.Correlate(seq, sim.RespOK, "v"); got != chaos.LateAfterTimeout {
		t.Fatalf("late response = %v, want late-after-timeout", got)
	}
	cc := c.Correlation()
	if cc.LateAfterTimeout != 1 || cc.Conflicting != 0 || cc.Loud() {
		t.Fatalf("a late response was misclassified: %+v", cc)
	}
}

// Records are never reaped, and this is why: a forgotten operation turns case
// (ii) into case (i), and the loud counter starts accusing the cluster of
// something the client did.
func TestATimedOutOperationIsStillRECOGNISEDAsItsOwn(t *testing.T) {
	c, _ := newTestClient()
	_, seq := c.BeginSeq("put", "k", "v")
	c.Timeout(seq)
	if got := c.Correlate(seq, sim.RespOK, "v"); got == chaos.Unissued {
		t.Fatal("a timed-out operation was forgotten, so its own late response read as a fabrication")
	}
}

// RespUnset is refused at the call site, for the reason End refuses it: a check
// that fires far from the mistake reports a symptom without a suspect.
func TestCorrelateRefusesRespUnset(t *testing.T) {
	c, _ := newTestClient()
	_, seq := c.BeginSeq("put", "k", "v")
	defer func() {
		if recover() == nil {
			t.Fatal("RespUnset was accepted as an outcome")
		}
	}()
	c.Correlate(seq, sim.RespUnset, "")
}
