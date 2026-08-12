package sim

import (
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/sorted"
)

// History is the client-observed record of a run: what was asked, when, and
// what came back.
//
// It is the *only* input a safety oracle is allowed. An oracle that reads node
// state is asking the system under test whether it behaved, and a system that
// misbehaves will answer yes -- the oracle-independence principle in DESIGN-A0.
// A history is what an outside observer saw, which is the thing linearizability
// is actually defined over.
//
// Call and return instants are simulated-clock only. A wall-clock timestamp
// anywhere in here would be the determinism rule failing in the place we are
// least likely to notice: histories are consumed post-run by a checker nobody
// reads the output of when it passes.
type History struct {
	events []HistoryEvent
}

// HistoryEvent is one call or one return.
type HistoryEvent struct {
	// Call is the instant the client issued the operation; Return is when it
	// observed the response. Return is zero for an operation still in flight
	// when the run ended -- which is normal, not an error, and is exactly what
	// a linearizability checker needs in order to say "this may or may not have
	// happened".
	Call   clock.Instant
	Return clock.Instant

	Client int
	Seq    uint64
	Key    string
	Op     string // "put" | "get"
	Value  string // written value, or observed value on a get

	// Outcome distinguishes a response from a timeout. A timeout is not a
	// violation and never counts as one: a partitioned cluster that stops
	// answering is behaving correctly. It stays in the history because a
	// checker must treat it as "may or may not have happened", which is a
	// weaker claim than either success or failure.
	Outcome ResponseKind
}

// ResponseKind is what the client observed.
type ResponseKind uint8

const (
	// RespUnset is the zero value: an operation with no recorded outcome. It is
	// rejected by Validate rather than assumed, since a forgotten field must
	// not read as a decision.
	RespUnset ResponseKind = iota
	RespOK
	RespError
	RespTimeout
	numResponseKinds
)

func (r ResponseKind) String() string {
	switch r {
	case RespUnset:
		return "unset"
	case RespOK:
		return "ok"
	case RespError:
		return "error"
	case RespTimeout:
		return "timeout"
	case numResponseKinds:
		return "invalid"
	}
	return "unknown"
}

// InFlight reports whether the operation never returned. It is not a failure:
// an operation outstanding when the run ended is a real thing a client can
// experience, and the checker's job is to consider both worlds.
func (e HistoryEvent) InFlight() bool { return e.Return == 0 }

// NewHistory returns an empty history.
func NewHistory() *History { return &History{} }

// Begin records a call and returns its index, which the caller passes to End.
func (h *History) Begin(at clock.Instant, client int, seq uint64, op, key, value string) int {
	h.events = append(h.events, HistoryEvent{
		Call: at, Client: client, Seq: seq, Op: op, Key: key, Value: value,
	})
	return len(h.events) - 1
}

// End records the response an operation observed.
func (h *History) End(i int, at clock.Instant, kind ResponseKind, value string) {
	if i < 0 || i >= len(h.events) {
		panic(fmt.Sprintf("sim: history index %d out of range", i))
	}
	e := &h.events[i]
	if at < e.Call {
		panic(fmt.Sprintf("sim: operation returned at %d before it was called at %d", at, e.Call))
	}
	e.Return = at
	e.Outcome = kind
	if kind == RespOK && e.Op == "get" {
		e.Value = value
	}
}

// Events returns the recorded events in call order.
func (h *History) Events() []HistoryEvent { return h.events }

// Len is the number of operations.
func (h *History) Len() int { return len(h.events) }

// ByKey partitions the history per key, which is what makes the checking
// affordable: per-key histories are independent, so a wide key space is many
// small problems instead of one large one (DESIGN-A0 D12).
//
// Returned in sorted key order so a report reads the same on every run.
func (h *History) ByKey() []KeyHistory {
	index := make(map[string][]HistoryEvent)
	for _, e := range h.events {
		index[e.Key] = append(index[e.Key], e)
	}
	out := make([]KeyHistory, 0, len(index))
	for _, k := range sorted.Keys(index) {
		out = append(out, KeyHistory{Key: k, Events: index[k]})
	}
	return out
}

// KeyHistory is one key's operations.
type KeyHistory struct {
	Key    string
	Events []HistoryEvent
}

// Validate rejects a history that cannot be checked, so a malformed one fails
// loudly rather than being handed to a checker that returns green.
func (h *History) Validate() error {
	for i, e := range h.events {
		if !e.InFlight() && e.Outcome == RespUnset {
			return fmt.Errorf("sim: history event %d returned at %d with no recorded outcome", i, e.Return)
		}
		if e.Call < 0 {
			return fmt.Errorf("sim: history event %d was called at %d", i, e.Call)
		}
	}
	return nil
}
