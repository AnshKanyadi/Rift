// Package raftcheck holds the Raft safety oracles and the harness-side ledger
// they read.
//
// # Oracle independence, which is the constraint this package exists to honour
//
// Ansh's ruling, DESIGN-A1 §0: *the four safety oracles read from the Ready
// stream and from what each node persisted, never from raft.Raft internals.*
//
// Nothing in this package ever receives a *raft.Raft. It imports the raft
// package for its value types -- Term, Index, NodeID, Entry -- and for nothing
// else. An oracle that interrogates the engine believes the lie: a Raft whose
// in-memory state disagrees with what it persisted or emitted is exactly the bug
// class Raft implementations actually have, and reading r.term would see the
// intent rather than what the next leader will act on.
//
// So the oracles are handed a Ledger built from two streams and nothing else:
// what each node emitted, and what each node made durable.
package raftcheck

import (
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/internal/sorted"
	"github.com/anshkanyadi/rift/raft"
)

// Ledger is the harness's record of what the cluster did, as observed from
// outside the nodes.
//
// It is append-only and everything in it is a fact the harness witnessed rather
// than a value it asked a node for.
type Ledger struct {
	// durable[node] is what that node has made durable, most recent last.
	durableHS  []hsRecord
	durableLog [][]raft.Entry // indexed by node ordinal, the persisted prefix

	// ledIn records, per term, which node acted as leader in it. A node "acts as
	// leader in term T" the moment it sends an MsgApp bearing term T -- observed
	// from the message stream rather than from a role field, because a node
	// whose role says follower while it is still appending is a real bug and the
	// message is where it is visible.
	ledIn []leaderRecord

	// committed is every entry any node has applied, keyed by index. An entry is
	// committed the first time any node applies it: a node only applies what its
	// leader told it was committed, so this is a sound outside witness for a
	// property that is not a field anywhere.
	committed []commitRecord

	// applied[node] is what that node applied, in order.
	applied [][]raft.Entry

	// sent records every message released, with the sender's durable hard state
	// at that instant, which is what the persist-before-reply oracle compares.
	sent []sentRecord

	nodes int
}

type hsRecord struct {
	node int
	hs   raft.HardState
	at   clock.Instant
}

type leaderRecord struct {
	term raft.Term
	node int
	at   clock.Instant
	// log is what that node had persisted when it first acted as leader, which
	// is what leader completeness is checked against.
	log []raft.Entry
}

type commitRecord struct {
	entry raft.Entry
	node  int
	at    clock.Instant
}

type sentRecord struct {
	node int
	msg  raft.Message
	// durableTerm and durableVote are what the sender had DURABLE at send time.
	durableTerm raft.Term
	durableVote raft.NodeID
	durableLast raft.Index
	at          clock.Instant
}

// NewLedger returns a ledger for n nodes.
func NewLedger(n int) *Ledger {
	return &Ledger{
		durableLog: make([][]raft.Entry, n),
		applied:    make([][]raft.Entry, n),
		nodes:      n,
	}
}

// RecordDurable records what a node made durable. entries is the node's full
// persisted log prefix after the write.
//
// # Why the parameters are typed
//
// This method took plain values, and what was passed to it was an engine
// read-back: the system's own account of what it held, which includes writes a
// crash would take. The oracle downstream then compared the system's
// acknowledgements against the system's own claims and reported green 44,911
// times over an inflated watermark.
//
// provenance.Observed is the fix, and it is a build failure rather than a
// checker: what may be recorded here is what the harness WITNESSED crossing the
// boundary — the batch the driver submitted, promoted when the engine reported
// that sequence durable — and a provenance.Reported no longer fits.
func (l *Ledger) RecordDurable(node int, hsO provenance.Observed[raft.HardState], entriesO provenance.Observed[[]raft.Entry], at clock.Instant) {
	hs, entries := hsO.Fact(), entriesO.Fact()
	l.durableHS = append(l.durableHS, hsRecord{node: node, hs: hs, at: at})
	cp := make([]raft.Entry, len(entries))
	copy(cp, entries)
	l.durableLog[node] = cp
}

// RecordSent records a released message alongside the sender's durable state.
//
// The message is Observed for the same reason the durable record is: it is a
// value that crossed the node's boundary on its way to the transport, not an
// answer to a question about what the node believes it sent.
func (l *Ledger) RecordSent(node int, mO provenance.Observed[raft.Message], at clock.Instant) {
	m := mO.Fact()
	hs := l.durableHardState(node)
	l.sent = append(l.sent, sentRecord{
		node: node, msg: m, at: at,
		durableTerm: hs.Term, durableVote: hs.Vote,
		durableLast: lastIndexOf(l.durableLog[node]),
	})

	// Acting as leader is observed here, from the wire, and nowhere else.
	if m.Type == raft.MsgApp {
		l.noteLeader(m.Term, node, at)
	}
}

// RecordApplied records entries a node applied.
//
// Observed: these are the entries the Ready handed over for application, taken
// as they cross the boundary rather than read back out of the state machine
// afterwards. Reading them back would ask the system what it applied, which is
// the question the oracle exists to answer independently.
func (l *Ledger) RecordApplied(node int, entriesO provenance.Observed[[]raft.Entry], at clock.Instant) {
	entries := entriesO.Fact()
	l.applied[node] = append(l.applied[node], entries...)
	for _, e := range entries {
		if !l.isCommitted(e.Index) {
			l.committed = append(l.committed, commitRecord{entry: e, node: node, at: at})
		}
	}
}

func (l *Ledger) noteLeader(term raft.Term, node int, at clock.Instant) {
	for _, r := range l.ledIn {
		if r.term == term && r.node == node {
			return
		}
	}
	log := make([]raft.Entry, len(l.durableLog[node]))
	copy(log, l.durableLog[node])
	l.ledIn = append(l.ledIn, leaderRecord{term: term, node: node, at: at, log: log})
}

func (l *Ledger) durableHardState(node int) raft.HardState {
	var hs raft.HardState
	for _, r := range l.durableHS {
		if r.node == node {
			hs = r.hs
		}
	}
	return hs
}

func (l *Ledger) isCommitted(i raft.Index) bool {
	for _, c := range l.committed {
		if c.entry.Index == i {
			return true
		}
	}
	return false
}

// Committed returns the committed entries observed so far.
func (l *Ledger) Committed() []raft.Entry {
	out := make([]raft.Entry, 0, len(l.committed))
	for _, c := range l.committed {
		out = append(out, c.entry)
	}
	return out
}

// Census summarises election activity, which is the evidence that a run
// contended rather than merely completed.
type Census struct {
	Terms          raft.Term
	ElectionsStart int
	ElectionsWon   int
	SplitVotes     int
	Leaders        int
}

// Census computes the election census from the message stream.
//
// A run whose leader is never challenged proves nothing, so these numbers are
// reported alongside every sweep: a schedule mix that produces no contention is
// a mix that needs fixing, and that is invisible unless it is counted.
func (l *Ledger) Census() Census {
	var c Census
	startedIn := map[raft.Term]bool{}
	wonIn := map[raft.Term]bool{}
	candidates := map[raft.Term]map[int]bool{}

	for _, s := range l.sent {
		if s.msg.Term > c.Terms {
			c.Terms = s.msg.Term
		}
		switch s.msg.Type {
		case raft.MsgVote:
			if !startedIn[s.msg.Term] {
				startedIn[s.msg.Term] = true
				c.ElectionsStart++
			}
			if candidates[s.msg.Term] == nil {
				candidates[s.msg.Term] = map[int]bool{}
			}
			candidates[s.msg.Term][s.node] = true
		case raft.MsgApp:
			if !wonIn[s.msg.Term] {
				wonIn[s.msg.Term] = true
				c.ElectionsWon++
			}
		case raft.MsgVoteResp, raft.MsgAppResp:
		}
	}
	c.Leaders = len(l.ledIn)

	// A split vote is a term in which two or more nodes campaigned. Counted from
	// candidacy rather than from outcome, since a term with two candidates that
	// still elects somebody has split the vote and simply recovered.
	//
	// Sorted keys rather than a map range: this package is in core scope and map
	// iteration order is the classic determinism leak.
	for _, t := range sorted.Keys(candidates) {
		if len(candidates[t]) > 1 {
			c.SplitVotes++
		}
	}
	return c
}

func (c Census) String() string {
	return fmt.Sprintf("terms=%d elections-started=%d elections-won=%d split-votes=%d leader-terms=%d",
		c.Terms, c.ElectionsStart, c.ElectionsWon, c.SplitVotes, c.Leaders)
}

func lastIndexOf(es []raft.Entry) raft.Index {
	if len(es) == 0 {
		return 0
	}
	return es[len(es)-1].Index
}

// Dump renders the committed ledger and per-node apply streams, for diagnosis.
// It is a report surface, not an oracle input.
func (l *Ledger) Dump() string {
	s := "committed (first applied by any node):\n"
	for _, c := range l.committed {
		s += fmt.Sprintf("  idx=%d term=%d by=node%d at=%d data=%q\n",
			c.entry.Index, c.entry.Term, c.node, int64(c.at), c.entry.Data)
	}
	s += "leaders:\n"
	for _, r := range l.ledIn {
		s += fmt.Sprintf("  term=%d node=%d at=%d persisted=%d entries\n",
			r.term, r.node, int64(r.at), len(r.log))
	}
	for n := range l.applied {
		s += fmt.Sprintf("node %d applied %d entries\n", n, len(l.applied[n]))
	}
	return s
}
