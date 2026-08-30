package main

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/anshkanyadi/rift/clock"
	riftnet "github.com/anshkanyadi/rift/net"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/store"
)

// serving is the client protocol: the last seam, and the only one where an
// operation -- rather than a Raft message -- crosses a socket.
//
// # It is a sim.Node wrapping a sim.Node, and that is the point
//
// The driver calls Handle ON THE NODE LOOP. Everything below therefore runs
// where store state is legally reachable, which is the only reason a response
// can be built from the node's history at all. Amendment A1's rule -- every
// cross-goroutine interaction enters through the mailbox -- is satisfied by
// construction here rather than by discipline: there is no other goroutine.
//
//	THE ALTERNATIVE WAS A GOROUTINE WATCHING THE HISTORY FOR COMPLETIONS, which
//	is a data race on an object the node loop writes, and which -race would have
//	found only if a completion and a scan happened to interleave during a lane.
//
// # This is NOT the adapter GF-55 said would falsify its claim
//
// GF-55 recorded that store/, riftcgo and node.Driver fit together by type with
// nothing between them. This type sits in that chain, so the distinction has to
// be stated rather than assumed: `serving` adds a PROTOCOL the three pieces do
// not have. It reconciles nothing. riftnode ran end to end without it, which is
// the test of whether an adapter was needed, and it passed. If some day a type
// has to exist to make store/ and node.Driver agree, GF-55 falls -- this is not
// that type.
// Fault is a deliberate misbehaviour, for calibrating the chaos harness.
//
// # sim/toy's job, for chaos/ (GF-62 item 2)
//
// `sim/hunt`'s mutants work because `sim/toy` exists: a protocol that can be
// broken on purpose, so "did the harness notice?" is answerable. `chaos/` had no
// equivalent, so every gate arm and every checker in the real-mode path was a
// mechanism with no positive control.
//
//	A CHECK THAT HAS NEVER BEEN SEEN FIRING ON A REAL RUN IS A CHECK NOBODY HAS
//	CALIBRATED. Inducing it by hand-editing the runner proves the code path; it
//	does not prove the mechanism catches a cluster actually doing the thing.
//
// These are off unless --fault names one, and riftnode SAYS SO on its startup
// line beside the engine and the ledger, because a cluster deliberately breaking
// its own guarantees must not be mistakable for one that is not.
type Fault string

const (
	FaultNone Fault = ""
	// FaultStaleRead answers a get from a remembered earlier value without going
	// through raft. A linearizability violation the checker must catch.
	FaultStaleRead Fault = "stale-read"
	// FaultDoubleAnswer sends a second, DISAGREEING response for every answered
	// operation. The client's Conflicting counter must catch it.
	FaultDoubleAnswer Fault = "double-answer"
)

type serving struct {
	fault Fault
	stale map[string]string

	inner sim.Node
	st    *store.Node
	self  sim.NodeID
	hist  *sim.History
	tr    interface{ Send(sim.Envelope) }

	// pending maps a LOCAL history index to the client that is waiting on it.
	//
	// The index is local and it has to be: in sim one History object is shared
	// by every node, so the sweep's index means the same thing everywhere. Here
	// each process has its own, so an index from another process names a
	// different operation -- or nothing.
	pending map[int]waiter

	// order keeps pending's keys in allocation order, so the scan below visits
	// them without ranging a map. That rule is determinismcheck's and cmd/ is
	// outside its scope, but the reason it exists does not stop applying at a
	// package boundary: the ORDER RESPONSES GO OUT IN IS BEHAVIOUR, and behaviour
	// that varies run to run for no reason is behaviour nobody can debug.
	order []int

	// served and refused are written on the node loop and read by the counters
	// writer, which is a different goroutine. Atomics rather than a comment
	// explaining why the race is benign: the mailbox rule exists because
	// "benign" is a judgement nobody can check, and -race does not accept it.
	// admitted counts requests that REACHED this seam, and it exists because its
	// absence cost a debugging session: with only served and refused, a node
	// that never saw a request and a node that saw every request and answered
	// none report the identical pair of zeroes. The first chaos run reported
	// served=0 refused=0 on all three nodes and the number could not say which.
	//
	//	TWO INDISTINGUISHABLE FAILURES UNDER ONE COUNTER IS ONE COUNTER TOO FEW.
	admitted        atomic.Uint64
	served, refused atomic.Uint64

	// leaderTicks counts ticks on which this node led at least one range.
	//
	// It is sampled ON THE NODE LOOP, immediately after the tick that could
	// have changed it, because store.Node.IsLeader reads core state and reading
	// it from the counters goroutine is exactly the off-loop touch Amendment A1
	// makes a bug.
	//
	// It is a REALITY COUNTER, not decoration:
	//
	//	A CLUSTER THAT NEVER ELECTED A LEADER OBSERVED NOTHING, and every checker
	//	over its history is green because nothing happened. The run looks
	//	identical to a healthy one from the outside -- processes up, bytes on the
	//	wire, no violations -- which is the shape of every vacuous green this
	//	project has recorded.
	leaderTicks atomic.Uint64
	ticks       atomic.Uint64

	// isLeader is the LATEST sample, published so a chaos runner can aim.
	//
	// CLAUDE.md's headline is "killing the leader every 10 seconds". A
	// round-robin kill hits the leader a third of the time on three nodes and is
	// a different experiment; a runner that cannot ask who leads cannot run the
	// one the claim names. So the node answers, from its own loop, and the
	// runner aims.
	isLeader atomic.Bool

	statusMu                 sync.Mutex
	role                     string
	term, vote, last, commit uint64
}

type waiter struct {
	client sim.NodeID
	seq    uint64
}

func newServing(st *store.Node, self sim.NodeID, hist *sim.History, tr interface{ Send(sim.Envelope) }, f Fault) *serving {
	return &serving{
		inner: st, st: st, self: self, hist: hist, tr: tr,
		pending: map[int]waiter{}, fault: f, stale: map[string]string{},
	}
}

// Handle admits a client request, delegates, and then flushes whatever the
// delegation decided.
func (s *serving) Handle(ev sim.Event, sch sim.Scheduler) {
	if ev.Kind == sim.KindDeliver {
		// The payload is a FRAME, because that is what store.Node's deliver arm
		// reads (BUG-051). Decoding it here to sniff the kind and passing the
		// frame through untouched keeps ONE representation on the mailbox: a
		// wrapper that forwarded a different type than it received would be the
		// same bug one layer up.
		if frame, ok := ev.Payload.([]byte); ok {
			if e, err := sim.Decode(frame); err == nil && e.Kind == riftnet.KindRequest {
				s.admit(e, sch)
				return
			}
		}
	}
	s.inner.Handle(ev, sch)
	if ev.Kind == sim.KindTick {
		s.ticks.Add(1)
		led := s.st != nil && s.st.IsLeader()
		s.isLeader.Store(led)
		if s.st != nil {
			role, term, vote, last, commit := s.st.RoleTerm()
			s.statusMu.Lock()
			s.role, s.term, s.vote, s.last, s.commit = role, term, vote, last, commit
			s.statusMu.Unlock()
		}
		if led {
			s.leaderTicks.Add(1)
		}
	}
	s.flush(sch.Now())
}

// admit turns a wire request into a store request.
func (s *serving) admit(e sim.Envelope, sch sim.Scheduler) {
	req, err := riftnet.DecodeRequest(e.Body)
	if err != nil {
		// A MALFORMED REQUEST IS COUNTED, NOT ANSWERED. Answering would put a
		// response on the wire for an operation whose seq we could not read,
		// which is precisely the unissued response the client's correlation
		// treats as a broken record.
		s.refused.Add(1)
		return
	}
	op := "get"
	if req.Op == riftnet.OpPut {
		op = "put"
	}
	// The node begins the operation in ITS OWN history purely to get an index
	// the store can complete. This history is discarded; the authoritative one
	// is the client's, on the client's clock, and that separation is the whole
	// of chaos/client.go's monotonic-source rule.
	s.admitted.Add(1)

	// PLANTED: answer a read from a remembered value, bypassing raft entirely.
	if s.fault == FaultStaleRead && op == "get" && s.st != nil && s.st.IsLeader() {
		if v, ok := s.stale[req.Key]; ok {
			body, err := riftnet.EncodeResponse(riftnet.Response{
				Seq: req.Seq, Status: riftnet.StatusOK, Value: v,
			})
			if err == nil {
				s.tr.Send(sim.Envelope{From: s.self, To: e.From, Kind: riftnet.KindResponse, Body: body})
				s.served.Add(1)
				return
			}
		}
	}
	if s.fault == FaultStaleRead && op == "put" {
		if _, seen := s.stale[req.Key]; !seen {
			s.stale[req.Key] = req.Value // the FIRST value ever written, kept forever
		}
	}

	idx := s.hist.Begin(sch.Now(), int(e.From), req.Seq, op, req.Key, req.Value)
	s.pending[idx] = waiter{client: e.From, seq: req.Seq}
	s.order = append(s.order, idx)

	s.inner.Handle(sim.Event{
		At: sch.Now(), Kind: sim.KindClient, Node: s.self,
		Payload: store.Request{
			Client: int(e.From), Seq: req.Seq,
			Op: op, Key: req.Key, Value: req.Value,
			HistIdx: idx,
		},
	}, sch)
	s.flush(sch.Now())
}

// flush sends a response for every pending operation the store has decided.
//
// It scans the whole pending set rather than a watermark, because operations
// complete OUT OF ORDER -- a read index answers while a write waits on a quorum
// -- and a watermark would strand every completion behind the oldest.
func (s *serving) flush(now clock.Instant) {
	if len(s.order) == 0 {
		return
	}
	evs := s.hist.Events()
	keep := s.order[:0]
	for _, idx := range s.order {
		if idx >= len(evs) || evs[idx].Outcome == sim.RespUnset {
			keep = append(keep, idx)
			continue
		}
		w := s.pending[idx]
		delete(s.pending, idx)
		status := riftnet.StatusError
		switch evs[idx].Outcome {
		case sim.RespOK:
			status = riftnet.StatusOK
		case sim.RespTimeout:
			// A NODE NEVER REPORTS A TIMEOUT. A timeout is the client's
			// observation that nothing arrived; a node claiming one would be
			// reporting on a wire it cannot see. If this fires, the store used
			// an outcome this seam does not model, and the loud path is right.
			status = riftnet.StatusError
		}
		body, err := riftnet.EncodeResponse(riftnet.Response{
			Seq: w.seq, Status: status, Value: evs[idx].Value,
		})
		if err != nil {
			s.refused.Add(1)
			continue
		}
		s.tr.Send(sim.Envelope{From: s.self, To: w.client, Kind: riftnet.KindResponse, Body: body})
		s.served.Add(1)

		// PLANTED: a second answer that DISAGREES with the first.
		if s.fault == FaultDoubleAnswer {
			if b2, err := riftnet.EncodeResponse(riftnet.Response{
				Seq: w.seq, Status: riftnet.StatusOK, Value: evs[idx].Value + "-DISAGREE",
			}); err == nil {
				s.tr.Send(sim.Envelope{
					From: s.self, To: w.client, Kind: riftnet.KindResponse, Body: b2,
				})
			}
		}
	}
	sort.Ints(keep)
	s.order = keep
	_ = now
}

// Counters is the serving half of the reality check: a node that answered
// nothing has not served a client, whatever the wire bytes say.
func (s *serving) Counters() (admitted, served, refused uint64) {
	return s.admitted.Load(), s.served.Load(), s.refused.Load()
}

// Leadership reports ticks led out of ticks seen, and whether this node led as
// of its most recent tick.
func (s *serving) Leadership() (led, total uint64, now bool) {
	return s.leaderTicks.Load(), s.ticks.Load(), s.isLeader.Load()
}

// Status is the raft state as of the last tick, sampled on the node loop.
//
// Real mode has no ledger to read and no trace to replay, so a node that cannot
// say what it thinks it is leaves an investigator with nothing but throughput.
func (s *serving) Status() (role string, term, vote, last, commit uint64) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.role, s.term, s.vote, s.last, s.commit
}
