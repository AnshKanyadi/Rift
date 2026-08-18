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
	durableHS []hsRecord

	// durableLog[node] is the entries that node has durable ABOVE its snapshot,
	// and durableSnap[node] is that snapshot. After A2 the two are inseparable:
	// a log is a suffix, and reading one without the other is how a compacted
	// node looks like a node that lost entries.
	durableLog  [][]raft.Entry
	durableSnap []raft.SnapshotMeta

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

	// recv records every message a node accepted at its boundary.
	//
	// Sends alone cannot answer a question about cause. A leadership transfer
	// order that was dropped, delayed or delivered to a crashed node produces no
	// campaign, and from the send stream that is indistinguishable from a target
	// that ignored it. The delivery is the second boundary observation, and it is
	// what makes "it campaigned BECAUSE it was told to" a statement rather than a
	// correlation.
	recv []sentRecord

	// snaps records every snapshot a node created or installed. The digest is
	// of the bytes that crossed the boundary, which is what makes a snapshot
	// checkable against an independently computed state.
	snaps []snapRecord

	nodes int

	// rev increments on every recorded fact. An oracle is a pure function of the
	// ledger, so a rev that has not moved cannot produce a verdict that has --
	// and the sweep calls every oracle on every event, which is thousands of full
	// re-scans per run once logs are long enough to compact.
	rev uint64
}

// Rev is the ledger's revision, for oracles that skip unchanged state.
func (l *Ledger) Rev() uint64 { return l.rev }

type hsRecord struct {
	node int
	hs   raft.HardState
	at   clock.Instant
}

type leaderRecord struct {
	term raft.Term
	node int
	at   clock.Instant
	// log is what that node had persisted when it first acted as leader, and
	// snap is the snapshot that log sat on top of. Leader completeness is checked
	// against both: an entry the leader compacted away is one it applied, not one
	// it lost, and reading only the log would call every compacted leader
	// incomplete.
	log  []raft.Entry
	snap raft.SnapshotMeta
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

// SnapshotRecord is a snapshot crossing a node's boundary: created and handed to
// the engine, or received and installed.
type SnapshotRecord struct {
	Index  raft.Index
	Term   raft.Term
	Digest uint64

	// Taken distinguishes a snapshot this node produced from one it received.
	// Both are checkable and they check different things: a created snapshot
	// must equal the state its own log produces, and an installed one must equal
	// the state the cluster's log produces at that index.
	Taken bool
}

type snapRecord struct {
	node int
	rec  SnapshotRecord
	at   clock.Instant
}

// NewLedger returns a ledger for n nodes.
func NewLedger(n int) *Ledger {
	return &Ledger{
		durableLog:  make([][]raft.Entry, n),
		durableSnap: make([]raft.SnapshotMeta, n),
		applied:     make([][]raft.Entry, n),
		nodes:       n,
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
func (l *Ledger) RecordDurable(node int, stO provenance.Observed[DurableState], at clock.Instant) {
	l.rev++
	st := stO.Fact()
	l.durableHS = append(l.durableHS, hsRecord{node: node, hs: st.HardState, at: at})
	cp := make([]raft.Entry, len(st.Log))
	copy(cp, st.Log)
	l.durableLog[node] = cp
	l.durableSnap[node] = st.Snapshot
}

// DurableState is what one node has on disk: its hard state, the snapshot its
// log sits on top of, and the entries above that snapshot.
//
// The three travel together because after A2 they only mean anything together.
// A log suffix without its snapshot index is a set of entries at unknown
// positions, and a checker handed one would compare position against position
// and call two agreeing nodes divergent.
type DurableState struct {
	HardState raft.HardState
	Snapshot  raft.SnapshotMeta
	Log       []raft.Entry
}

// holds reports whether a node's durable state covers index i: either the entry
// is in the log suffix, or the snapshot is at or past it.
//
// The snapshot arm rests on a stated assumption rather than on an observation,
// and it is worth naming: a snapshot is taken from an APPLIED prefix, so an
// index the snapshot covers is one this node applied. That is sound exactly as
// far as state machine safety holds, and state machine safety is checked
// independently -- so if it ever fails, this arm is unreliable and the run has
// already reported the reason.
func (l *Ledger) holds(node int, e raft.Entry) bool {
	if l.durableSnap[node].Index >= e.Index {
		return true
	}
	for _, x := range l.durableLog[node] {
		if x.Index == e.Index && x.Term == e.Term && string(x.Data) == string(e.Data) {
			return true
		}
	}
	return false
}

// RecordSent records a released message alongside the sender's durable state.
//
// The message is Observed for the same reason the durable record is: it is a
// value that crossed the node's boundary on its way to the transport, not an
// answer to a question about what the node believes it sent.
func (l *Ledger) RecordSent(node int, mO provenance.Observed[raft.Message], at clock.Instant) {
	l.rev++
	m := mO.Fact()
	hs := l.durableHardState(node)
	l.sent = append(l.sent, sentRecord{
		node: node, msg: m, at: at,
		durableTerm: hs.Term, durableVote: hs.Vote,
		durableLast: l.durableLast(node),
	})

	// Acting as leader is observed here, from the wire, and nowhere else.
	if m.Type == raft.MsgApp {
		l.noteLeader(m.Term, node, at)
	}
}

// RecordReceived records a message a node accepted.
//
// Observed: it is the frame crossing into the node, taken before the node has
// done anything with it, not an answer to a question about what it thinks it
// received.
func (l *Ledger) RecordReceived(node int, mO provenance.Observed[raft.Message], at clock.Instant) {
	l.rev++
	l.recv = append(l.recv, sentRecord{node: node, msg: mO.Fact(), at: at})
}

// RecordApplied records entries a node applied.
//
// Observed: these are the entries the Ready handed over for application, taken
// as they cross the boundary rather than read back out of the state machine
// afterwards. Reading them back would ask the system what it applied, which is
// the question the oracle exists to answer independently.
func (l *Ledger) RecordApplied(node int, entriesO provenance.Observed[[]raft.Entry], at clock.Instant) {
	l.rev++
	entries := entriesO.Fact()
	l.applied[node] = append(l.applied[node], entries...)
	for _, e := range entries {
		if !l.isCommitted(e.Index) {
			l.committed = append(l.committed, commitRecord{entry: e, node: node, at: at})
		}
	}
}

// RecordSnapshot records a snapshot a node created or installed.
//
// Observed: the digest is of the bytes handed to the engine, or of the bytes
// that arrived on the wire. Neither is a question asked of a state machine about
// what it thinks it holds.
func (l *Ledger) RecordSnapshot(node int, rO provenance.Observed[SnapshotRecord], at clock.Instant) {
	l.rev++
	l.snaps = append(l.snaps, snapRecord{node: node, rec: rO.Fact(), at: at})
}

// Snapshots returns every recorded snapshot event, in order.
func (l *Ledger) Snapshots() []SnapshotRecord {
	out := make([]SnapshotRecord, 0, len(l.snaps))
	for _, s := range l.snaps {
		out = append(out, s.rec)
	}
	return out
}

// SnapshotsTaken and SnapshotsInstalled count the two directions, so a lane can
// ask whether either path ran at all.
func (l *Ledger) SnapshotsTaken() int {
	n := 0
	for _, s := range l.snaps {
		if s.rec.Taken {
			n++
		}
	}
	return n
}

// SnapshotsInstalled is the receiving half of the same count.
func (l *Ledger) SnapshotsInstalled() int { return len(l.snaps) - l.SnapshotsTaken() }

// AppliedBy returns what a node applied, in order. It exists for the apply
// continuity oracle, which is about one node's stream rather than about two
// nodes agreeing.
func (l *Ledger) AppliedBy(node int) []raft.Entry { return l.applied[node] }

// InstallsBy returns the snapshot indices a node installed, in order.
func (l *Ledger) InstallsBy(node int) []raft.Index {
	var out []raft.Index
	for _, s := range l.snaps {
		if s.node == node && !s.rec.Taken {
			out = append(out, s.rec.Index)
		}
	}
	return out
}

// Nodes is how many nodes this ledger covers.
func (l *Ledger) Nodes() int { return l.nodes }

func (l *Ledger) noteLeader(term raft.Term, node int, at clock.Instant) {
	for _, r := range l.ledIn {
		if r.term == term && r.node == node {
			return
		}
	}
	log := make([]raft.Entry, len(l.durableLog[node]))
	copy(log, l.durableLog[node])
	l.ledIn = append(l.ledIn, leaderRecord{
		term: term, node: node, at: at, log: log, snap: l.durableSnap[node],
	})
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

// durableLast is the highest index this node has durable, counting the snapshot.
//
// Reading only the log suffix was correct for exactly as long as every log
// started at index 1. A compacted node whose whole log is inside its snapshot has
// an EMPTY suffix and everything durable, and taking the suffix's last index
// there reports zero -- which is not a small error, it is the persist-before-reply
// oracle accusing a correct node of acknowledging something it never wrote.
func (l *Ledger) durableLast(node int) raft.Index {
	last := l.durableSnap[node].Index
	if x := lastIndexOf(l.durableLog[node]); x > last {
		last = x
	}
	return last
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

// committedPrefix returns the committed entries 1..through, in index order, and
// reports whether the ledger has witnessed every one of them.
//
// Incomplete is not a failure. It means the harness has not seen enough to make
// a claim, and a checker that guessed at the gap would be asserting over a
// history it does not have.
func (l *Ledger) committedPrefix(through raft.Index) ([]raft.Entry, bool) {
	if through == 0 {
		return nil, true
	}
	byIndex := make(map[raft.Index]raft.Entry, len(l.committed))
	for _, c := range l.committed {
		if c.entry.Index <= through {
			byIndex[c.entry.Index] = c.entry
		}
	}
	out := make([]raft.Entry, 0, through)
	for i := raft.Index(1); i <= through; i++ {
		e, ok := byIndex[i]
		if !ok {
			return nil, false
		}
		out = append(out, e)
	}
	return out, true
}

// TransferRecord is one leadership transfer, reconstructed from the wire.
//
// Nothing here is asked of a node. A transfer is visible from outside as three
// messages in sequence, and that is how it is recorded: the order to campaign,
// the campaign, and the first act of leadership.
type TransferRecord struct {
	From raft.NodeID
	To   raft.NodeID
	At   clock.Instant

	// Campaigned is when the target sent its vote request, or zero if it never
	// did. Won is when it first acted as leader, or zero.
	Campaigned clock.Instant
	Won        clock.Instant

	// PreVotedAt is when the target sent a pre-vote after the order, if it did.
	PreVotedAt clock.Instant

	// PreVoted is true if the target ran a pre-vote round before campaigning.
	// A transfer is supposed to skip it: the current leader's say-so is stronger
	// evidence than any quorum of peers could give, and a target that pre-votes
	// anyway has not taken the shortcut the feature exists for.
	PreVoted bool
}

// Transfers reconstructs every leadership transfer from the wire.
//
// Keyed on DELIVERY rather than on the send. A transfer order that never arrives
// produces no campaign, and counting it against the feature would be measuring
// the network. What is attributable is what a node did after it accepted one.
func (l *Ledger) Transfers() []TransferRecord {
	var out []TransferRecord
	for _, d := range l.recv {
		if d.msg.Type != raft.MsgTimeoutNow {
			continue
		}
		t := TransferRecord{From: d.msg.From, To: d.msg.To, At: d.at}
		for _, x := range l.sent {
			if x.at < d.at || x.msg.From != t.To {
				continue
			}
			switch x.msg.Type {
			case raft.MsgPreVote:
				if t.Campaigned == 0 {
					t.PreVoted, t.PreVotedAt = true, x.at
				}
			case raft.MsgVote:
				if t.Campaigned == 0 {
					t.Campaigned = x.at
				}
			case raft.MsgApp:
				if t.Campaigned != 0 && t.Won == 0 {
					t.Won = x.at
				}
			case raft.MsgVoteResp, raft.MsgAppResp, raft.MsgPreVoteResp, raft.MsgSnap,
				raft.MsgTimeoutNow:
			}
		}
		out = append(out, t)
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
