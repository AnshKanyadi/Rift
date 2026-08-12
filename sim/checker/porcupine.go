// Package checker holds the end-of-run checkers.
//
// It sits deliberately outside the determinism boundary (Amendment A5). History
// *collection* runs in-sim, is dependency-free and obeys every determinism
// rule; the checking runs afterwards, imports an external library, and needs a
// wall-clock timeout to bound an NP-hard search. Those are different jobs with
// different constraints, and putting them in one package would have forced one
// of the two to be wrong.
//
// porcupine is pinned at v1.3.0 and is one of the four dependencies CLAUDE.md
// pre-approves. It is used for exactly one thing: deciding whether a
// client-observed history is linearizable.
//
// # What it is allowed to see
//
// Client-observed histories only. Never node state, never the engine's account
// of itself. An oracle that interrogates the system under test believes the lie
// (DESIGN-A0's oracle-independence principle), and linearizability is defined
// over what an outside observer saw in the first place.
//
// # Three outcomes
//
// A timeout is INCONCLUSIVE, never a pass. Linearizability checking is NP-hard
// in general, so a bounded checker sometimes cannot finish; "zero violations
// across N seeds" is quietly false if a fraction of those seeds never finished
// checking. When the inconclusive rate rises, the response is a smaller
// problem -- shorter windows, harder per-key partitioning -- never a weaker
// oracle.
package checker

import (
	"fmt"
	"time"

	"github.com/anishathalye/porcupine"
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
)

// Linearizability checks each key's history independently.
//
// Per-key rather than whole-history because per-key histories are independent,
// so a wide key space is many small problems instead of one large one -- which
// is what keeps the check affordable enough to run on every seed (DESIGN-A0
// D12).
type Linearizability struct {
	// Timeout bounds the search per key. Exceeding it is inconclusive.
	Timeout time.Duration

	// Floor is the minimum number of operations below which this checker
	// cannot conclude anything. A checker handed zero ops returns green by
	// construction, and that green would be banked as evidence.
	Floor int
}

// NewLinearizability returns a checker with defaults that have been chosen
// rather than inherited: one second per key, and a floor of two operations,
// since a single operation is linearizable by inspection and proves nothing.
func NewLinearizability() *Linearizability {
	return &Linearizability{Timeout: time.Second, Floor: 2}
}

func (c *Linearizability) Name() string { return "linearizability" }

func (c *Linearizability) MinOps() int { return c.Floor }

// registerModel is the register semantics porcupine checks against: a get
// returns the last value written, and a put always succeeds.
var registerModel = porcupine.Model{
	Init: func() any { return "" },
	Step: func(state, input, output any) (bool, any) {
		in := input.(request)
		out := output.(response)
		switch in.op {
		case "put":
			return true, in.value
		case "get":
			// An operation that never returned, or returned a timeout, is
			// unconstrained: it may or may not have happened, and forcing it to
			// match would manufacture violations out of unavailability.
			if out.unknown {
				return true, state
			}
			return out.value == state.(string), state
		}
		return true, state
	},
}

type request struct {
	op    string
	value string
}

type response struct {
	value   string
	unknown bool
}

// Check runs the model per key.
func (c *Linearizability) Check(h *sim.History) sim.Report {
	consumed := 0
	for _, kh := range h.ByKey() {
		ops, n := toPorcupine(kh)
		consumed += n
		if len(ops) < 2 {
			continue
		}

		res, info := porcupine.CheckOperationsVerbose(registerModel, ops, c.Timeout)
		switch res {
		case porcupine.Ok:
			continue
		case porcupine.Illegal:
			return sim.Report{
				Verdict:  sim.VerdictViolation,
				Detail:   fmt.Sprintf("key %q has a non-linearizable history over %d operations", kh.Key, len(ops)),
				At:       firstReturn(kh),
				Consumed: consumed,
			}
		case porcupine.Unknown:
			return sim.Report{
				Verdict:  sim.VerdictInconclusive,
				Detail:   fmt.Sprintf("key %q did not finish checking within %s over %d operations; this is not a pass, and the fix is a smaller problem rather than a longer timeout", kh.Key, c.Timeout, len(ops)),
				Consumed: consumed,
			}
		}
		_ = info
	}

	if consumed < c.Floor {
		return sim.Report{
			Verdict:  sim.VerdictInconclusive,
			Detail:   fmt.Sprintf("only %d checkable operations, below the floor of %d", consumed, c.Floor),
			Consumed: consumed,
		}
	}
	return sim.Report{
		Verdict:  sim.VerdictPass,
		Detail:   fmt.Sprintf("every key linearizable over %d operations", consumed),
		Consumed: consumed,
	}
}

// toPorcupine converts one key's history, dropping nothing.
//
// An operation that timed out or never returned becomes an operation with an
// unknown response and an open-ended return time, which is what lets the
// checker consider both the world where it happened and the world where it did
// not. Dropping them instead would silently make every partition look clean.
func toPorcupine(kh sim.KeyHistory) ([]porcupine.Operation, int) {
	ops := make([]porcupine.Operation, 0, len(kh.Events))
	for _, e := range kh.Events {
		in := request{op: e.Op, value: e.Value}
		out := response{value: e.Value}

		call := int64(e.Call)
		ret := int64(e.Return)
		switch {
		case e.InFlight() || e.Outcome == sim.RespTimeout:
			out.unknown = true
			// An unreturned operation is open-ended: it may linearize anywhere
			// after its call.
			ret = int64(^uint64(0) >> 2)
		case e.Outcome == sim.RespError:
			out.unknown = true
		}

		ops = append(ops, porcupine.Operation{
			ClientId: e.Client,
			Input:    in,
			Call:     call,
			Output:   out,
			Return:   ret,
		})
	}
	return ops, len(ops)
}

func firstReturn(kh sim.KeyHistory) clock.Instant {
	if len(kh.Events) == 0 {
		return 0
	}
	return kh.Events[0].Call
}
