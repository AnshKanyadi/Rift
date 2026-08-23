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
	"sort"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/internal/sorted"
	"github.com/anshkanyadi/rift/raft"
)

// Ledger is the harness's record of what the cluster did, as observed from
// outside the nodes.
//
// It is append-only and everything in it is a fact the harness witnessed rather
// than a value it asked a node for.
//
// # Per range, and the audit that says why
//
// A4 made the cluster many Raft groups, and every one of the ledger's facts
// belongs to exactly one of them. Merging them would not be a smaller version of
// the truth: comparing range A's log against range B's is comparing unrelated
// sequences, and DESIGN-A4 §6 works through what each oracle would then say. Six
// would produce false positives, which announce themselves. **Two -- log matching
// and persist-before-reply -- would go quietly weaker**, which does not, and that
// is the whole reason this type is keyed by range rather than left alone.
type Ledger struct {
	// ranges holds one sub-ledger per range, in a SORTED slice. Sorted because
	// oracles walk it and this package is in core determinism scope.
	ranges []*rangeLedger

	nodes int
	rev   uint64

	// reads is every answer a replica gave a client, in the order the harness
	// observed them leaving the nodes.
	reads      []ReadRecord
	txnReads   []TxnReadRecord
	audits     []AuditRecord
	confOrders []ConfOrder

	// txns is every transaction the harness's coordinator issued.
	txns []TxnRecord

	// txnRestarts is the ledger's own count, kept beside the per-transaction
	// one so a sweep can compare it against the coordinator's.
	txnRestarts int

	// moves is every replica movement the HARNESS commanded, in the order it
	// commanded them.
	//
	// # Why the harness records its own orders instead of asking the cluster
	//
	// A move is an intent, and no sequence of committed entries states one: an
	// add and a remove look exactly like two unrelated membership changes. The
	// oracle therefore needs to know which range a move targeted and which two
	// nodes it named -- and taking that from the system would be the forbidden
	// shape, since a store that started no move at all could name a quiet range
	// and the check would come out green over nothing.
	//
	// The harness issued the order, so the harness witnessed it. What it never
	// takes from the system is whether the order was CARRIED OUT; that it reads
	// from the committed log like every other verdict.
	moves []MoveRecord
}

// ReadRecord is one answer a replica gave a client, taken as it crossed the
// boundary out of the node.
//
// # What is observed here, and what is not
//
// The KEY, the TIMESTAMP the read named, and the ANSWER: these are the bytes
// leaving the node, which is exactly where the ledger takes everything else.
// What is NOT taken is any statement by the store about what it believes was
// visible -- that is the thing under test, and an oracle that consumed it would
// be asking the system to grade itself.
//
// Refused says the store declined the read because the timestamp was at or below
// the collection mark. It is a first-class outcome and not an error: the oracle
// checks BOTH directions, because a store that refused everything would pass a
// checker that only looked at wrong answers.
type ReadRecord struct {
	Range uint64
	Node  int

	// Index is the log position of the entry this answer came from. It is what
	// lets the oracle compare an answer against the state THAT POSITION
	// produces, rather than against the state at some wall-clock moment nobody
	// can reconstruct.
	Index raft.Index

	Key     string
	At      hlc.Timestamp
	Value   string
	Found   bool
	Refused bool
	When    clock.Instant
}

// TxnRecord is one transaction the harness's coordinator issued, and what it
// was later observed to decide.
//
// # What is observed here, and what is not
//
// The harness ISSUED the transaction, so the harness witnessed its start, its
// primary, its key set and its commit timestamp. Those are its own facts, the
// same provenance as a move order.
//
// What it never takes from the system is whether the transaction is committed.
// That is read from the committed logs -- the primary's write record, which is
// the commit point (DESIGN-A6 D-A6-3) -- because it is the thing under test.
type TxnRecord struct {
	ID       int
	StartTS  hlc.Timestamp
	CommitTS hlc.Timestamp // what the coordinator intended, zero if it never got there
	Primary  string
	Keys     []string
	Began    clock.Instant
	Decided  clock.Instant
	Reached  bool // the coordinator observed its primary record land

	// Restarts is how many times this transaction took a new start timestamp.
	// Recorded because a transaction's identity MOVED, and an oracle matching on
	// the original would be matching a value the system abandoned.
	Restarts int
}

// TxnReadRecord is one snapshot read a transaction issued, and the answer the
// cluster gave it.
//
// # This is the whole client-observed history the A6 oracles judge
//
// Snapshot isolation and bank conservation are asserted over THESE and nothing
// else. Not over engine state, not over the recovered records, not over what
// the coordinator believed: over what a client asked and what it was told, which
// is the only evidence a real user of this database would ever have.
//
// # Locked, Uncertain and Refused are recorded because they are answers
//
// A history containing only the successful reads would let a store pass by
// refusing everything, which is the shape BUG-016 made a standing rule about.
// Each is the outcome of one of the three questions kv.GetTxn asks, and each is
// checkable: a lock names a primary, an uncertainty names a commit inside the
// interval and a restart strictly above it, a refusal names a timestamp at or
// below the mark.
type TxnReadRecord struct {
	Range uint64
	Node  int

	// Index is the log position whose applied state produced this answer. The
	// same reason ReadRecord carries one: an answer is compared against the
	// state a POSITION produces, never against a wall-clock moment.
	Index raft.Index

	Key string

	// At is the timestamp the read named; StartTS is the transaction it belongs
	// to. They are equal for a transaction's own reads and differ for an audit,
	// which names a timestamp without being a transaction.
	At      hlc.Timestamp
	StartTS hlc.Timestamp

	Value string
	Found bool

	Locked      bool
	LockPrimary string
	LockStartTS hlc.Timestamp

	Uncertain bool
	CommitTS  hlc.Timestamp
	RestartAt hlc.Timestamp

	// Ceiling is the top of the uncertainty interval this read used. Recorded
	// because the envelope checker's job is to compare it against a bound
	// derived from the PLAN, and it cannot do that if the only place the number
	// exists is inside the node that chose it.
	Ceiling hlc.Timestamp

	Refused bool

	When clock.Instant
}

// AuditRecord is one complete audit: every account read at ONE timestamp, and
// what they summed to.
//
// # Only complete audits are recorded
//
// A sum over a subset of the accounts conserves nothing, so a partial audit
// never reaches the ledger at all. Accounts is carried anyway, and the oracle
// checks it, because "complete" is a claim the harness makes and a claim the
// harness makes is a claim the checker verifies.
type AuditRecord struct {
	ReadTS   hlc.Timestamp
	Total    int
	Accounts int
	When     clock.Instant
}

// ConfOrder is one membership change the CHURN driver ordered, as opposed to one
// a move issued.
//
// # Why the harness records its own orders
//
// A move's add and an unrelated removal are indistinguishable in a committed
// log: both are configuration entries and neither says who asked. That is what
// made the two drivers un-overlappable -- the rebalance oracle blamed churn's
// removals on moves, 252 seeds in 300 (BUG-016).
//
// The wire format is not the place to fix it. A move identifier in a
// configuration entry would change a frozen format for a checker's convenience,
// and it would be a fact the system reports about itself. What the HARNESS knows
// for free is what it ordered and when, and that is recorded here as its own
// fact -- the same provenance as a move order.
//
// It does not make every removal attributable, and it is not supposed to. It
// makes the AMBIGUOUS ones identifiable, which is what turns a false violation
// into an honest inconclusive (Amendment A4).
type ConfOrder struct {
	Node int
	At   clock.Instant
}

// MoveRecord is one replica movement the harness ordered.
type MoveRecord struct {
	Range    uint64
	From, To raft.NodeID
	At       clock.Instant
}

// rangeLedger is everything the ledger used to be, for one range.
type rangeLedger struct {
	id uint64

	// base is the state this range STARTED from, as bytes, or nil for a range
	// that started empty.
	//
	// A range born from a split does not begin at nothing: it inherits the keys
	// above the split point from its parent, derived by every replica from its
	// own applied state. Its own log therefore describes only what happened
	// after that, and a model replaying only the log would compute a state the
	// range never had -- which is a checker reporting a violation on every seed
	// that splits, for a system behaving correctly.
	base []byte

	// birthConf is the configuration the range was born with, encoded.
	//
	// Recorded rather than assumed for the same reason the extent is: a range
	// born from a split inherits a membership, and a harness that guessed the
	// cluster's initial one would be describing a different range every time a
	// membership change landed before the split.
	birthConf []byte

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
}

// rev increments on every recorded fact. An oracle is a pure function of the
// ledger, so a rev that has not moved cannot produce a verdict that has -- and
// the sweep calls every oracle on every event, which is thousands of full
// re-scans per run once logs are long enough to compact.
//
// One counter for the whole ledger rather than one per range: an oracle that
// walks every range has to re-walk when any of them changes.
func (l *Ledger) Rev() uint64 { return l.rev }

// forRange returns the sub-ledger for id, creating it in sorted position.
//
// Created on first mention rather than declared up front, because ranges are
// born by splitting and the harness learns about one when a node first does
// something with it.
func (l *Ledger) forRange(id uint64) *rangeLedger {
	for _, r := range l.ranges {
		if r.id == id {
			return r
		}
	}
	r := &rangeLedger{
		id:          id,
		durableLog:  make([][]raft.Entry, l.nodes),
		durableSnap: make([]raft.SnapshotMeta, l.nodes),
		applied:     make([][]raft.Entry, l.nodes),
		nodes:       l.nodes,
	}
	l.ranges = append(l.ranges, r)
	for i := len(l.ranges) - 1; i > 0 && l.ranges[i-1].id > l.ranges[i].id; i-- {
		l.ranges[i-1], l.ranges[i] = l.ranges[i], l.ranges[i-1]
	}
	return r
}

// Ranges returns the sub-ledgers in range order, which is the order every oracle
// walks them in.
func (l *Ledger) Ranges() []*rangeLedger { return l.ranges }

// RecordMove records a replica movement the harness ordered.
//
// Observed: the harness is the thing that issued it.
func (l *Ledger) RecordMove(mO provenance.Observed[MoveRecord]) {
	l.rev++
	l.moves = append(l.moves, mO.Fact())
}

// RecordTxnBegin records a transaction the harness issued.
//
// Observed: the harness is the thing that issued it.
func (l *Ledger) RecordTxnBegin(r provenance.Observed[TxnRecord]) {
	l.rev++
	l.txns = append(l.txns, r.Fact())
}

// RecordTxnCommit records that the coordinator observed its primary record land.
//
// This is the coordinator's own account of reaching the commit point, and it is
// EVIDENCE rather than a verdict: the oracle checks the committed log for the
// primary's write record and never takes this as proof of anything. It exists so
// a sweep can say how many transactions got that far.
// TxnCommitRecord is the coordinator's own observation that its primary record
// landed.
//
// # Why it is Observed and not three loose arguments
//
// It was three loose arguments, and `tools/provcheck` had been failing on it
// since the A6 transaction commands landed -- a lane in `make ci`, red across a
// commit, because nobody ran it. The rule it enforces is DESIGN-A1 §0's: every
// input to a verdict that can come out GREEN must be something the harness
// witnessed, and an untyped input is one nobody had to think about.
//
// This one IS harness-witnessed -- the coordinator issued the step and saw it
// apply -- so the fix is the type rather than the fact. What must never be
// witnessed this way is whether the transaction is COMMITTED: that is read from
// the committed logs, because it is the thing under test.
type TxnCommitRecord struct {
	ID       int
	CommitTS hlc.Timestamp
	At       clock.Instant
}

func (l *Ledger) RecordTxnCommit(r provenance.Observed[TxnCommitRecord]) {
	c := r.Fact()
	l.rev++
	for i := range l.txns {
		if l.txns[i].ID == c.ID {
			l.txns[i].CommitTS = c.CommitTS
			l.txns[i].Decided = c.At
			l.txns[i].Reached = true
			return
		}
	}
}

// TxnRestartRecord is a transaction taking a NEW start timestamp.
//
// # Why the field alone was worse than nothing
//
// `TxnRecord.Restarts` existed and nothing ever wrote it, and `StartTS` kept the
// timestamp the transaction was FIRST minted at -- the one the system had
// abandoned. An investigation that placed two transactions by their recorded
// starts therefore placed one of them where it never was, and concluded a lost
// update that had not happened. That cost a full investigation once already, and
// the note warning the next reader to "check Restarts" pointed at a number that
// is zero however many restarts occurred.
//
// So the restart is RECORDED, and recording it moves the start timestamp. A
// ledger whose StartTS is the live one is the only version of this field that
// can be reasoned from.
type TxnRestartRecord struct {
	ID      int
	StartTS hlc.Timestamp
	At      clock.Instant
}

// RecordTxnRestart moves a transaction's start timestamp and counts the restart.
//
// Observed: the coordinator minted the new timestamp and re-issued its reads.
func (l *Ledger) RecordTxnRestart(r provenance.Observed[TxnRestartRecord]) {
	c := r.Fact()
	l.rev++
	l.txnRestarts++
	for i := range l.txns {
		if l.txns[i].ID == c.ID {
			l.txns[i].StartTS = c.StartTS
			l.txns[i].Restarts++
			return
		}
	}
}

// TxnRestarts is how many restarts the ledger was told about.
//
// It exists to be compared against the coordinator's own count. Two numbers for
// one fact is the only way to notice that the recording path stopped being
// called, which is what happened to the field this replaces.
func (l *Ledger) TxnRestarts() int { return l.txnRestarts }

// Txns is every transaction the harness issued.
func (l *Ledger) Txns() []TxnRecord { return l.txns }

// RecordRead records an answer a replica gave a client.
//
// Observed: the bytes crossing the boundary out of the node.
func (l *Ledger) RecordRead(r provenance.Observed[ReadRecord]) {
	l.rev++
	l.reads = append(l.reads, r.Fact())
}

// Reads is every answer the harness observed.
func (l *Ledger) Reads() []ReadRecord { return l.reads }

// RecordTxnRead appends one snapshot read and its answer.
func (l *Ledger) RecordTxnRead(r provenance.Observed[TxnReadRecord]) {
	l.rev++
	l.txnReads = append(l.txnReads, r.Fact())
}

// TxnReads is the client-observed history of snapshot reads.
func (l *Ledger) TxnReads() []TxnReadRecord { return l.txnReads }

// RecordAudit appends one complete audit.
func (l *Ledger) RecordAudit(r provenance.Observed[AuditRecord]) {
	l.rev++
	l.audits = append(l.audits, r.Fact())
}

// Audits is every complete audit this run produced.
func (l *Ledger) Audits() []AuditRecord { return l.audits }

// ReadsRefused counts the answers that were refusals.
func (l *Ledger) ReadsRefused() int {
	n := 0
	for _, r := range l.reads {
		if r.Refused {
			n++
		}
	}
	return n
}

// Moves is every replica movement the harness ordered.
func (l *Ledger) Moves() []MoveRecord { return l.moves }

// RecordConfOrder notes that the churn driver asked for a change to a node.
func (l *Ledger) RecordConfOrder(o provenance.Observed[ConfOrder]) {
	l.rev++
	l.confOrders = append(l.confOrders, o.Fact())
}

// churnTouched says whether the churn driver ordered a change to node n inside
// [from, to]. A move whose removal falls in such a window cannot be attributed.
func (l *Ledger) churnTouched(n int, from, to clock.Instant) bool {
	for _, o := range l.confOrders {
		if o.Node == n && o.At >= from && o.At <= to {
			return true
		}
	}
	return false
}

// MoveEnds is when move i stops owning its range's membership changes: the
// instant the NEXT move on the same range was ordered, or forever.
//
// # Why a window and not "everything after"
//
// A move is identified by its two nodes, and two moves on one range can name
// the same source. Seed 103: a move of node 2 to node 3 stalled after adding
// the learner, and a later move of node 2 to node 1 completed properly. Reading
// "everything after" blamed the second move's removal on the first, and reported
// a range that had done exactly the right thing.
//
// The harness ordered both, so the harness knows where one stops and the next
// begins. The window is its own record, not the cluster's.
func (l *Ledger) MoveEnds(i int) clock.Instant { return l.moveEnds(i) }

func (l *Ledger) moveEnds(i int) clock.Instant {
	for j := i + 1; j < len(l.moves); j++ {
		if l.moves[j].Range == l.moves[i].Range {
			return l.moves[j].At
		}
	}
	return clock.Instant(^uint64(0) >> 1)
}

// MovesCompleted counts the ordered moves whose removal is committed, which is
// the only sense in which a move can be said to have happened.
func (l *Ledger) MovesCompleted() int {
	n := 0
	for i, m := range l.moves {
		rl := l.rangeByID(m.Range)
		if rl == nil {
			continue
		}
		if _, ok := rl.firstConfChange(m.At, l.moveEnds(i), raft.ConfChangeRemoveNode, m.From); ok {
			n++
		}
	}
	return n
}

// MovesRacingUnrelatedChanges counts move windows that contain a committed
// membership change the move did not make.
//
// # This is a BIDIRECTIONAL gap assertion, and it is expected to be zero
//
// The plan separates the two membership drivers in time -- churn in the first
// half of a run, rebalance in the second -- because a move's add and an
// unrelated removal are indistinguishable in a committed log, and without the
// separation the rebalance oracle blamed the churn's removals on moves (252 of
// 300 seeds, BUG-016).
//
// That separation is a real gap: a production cluster interleaves these
// constantly. DESIGN-A4 section 10 records it as an unexercised interleaving,
// and a recorded gap has to be able to become WRONG. So this counts the
// interleaving, the exit run asserts it is zero, and the day a schedule change
// makes it reachable the lane says the record is stale instead of quietly
// proving something new.
//
// A change is "the move's own" if it adds the destination or removes the source.
// Anything else committed inside the window belongs to somebody else.
func (l *Ledger) MovesRacingUnrelatedChanges() int {
	n := 0
	for i, m := range l.moves {
		rl := l.rangeByID(m.Range)
		if rl == nil {
			continue
		}
		if rl.foreignConfChange(m.At, l.moveEnds(i), m.From, m.To) {
			n++
		}
	}
	return n
}

// foreignConfChange reports whether a committed membership change in [since,
// until) names neither endpoint of the move that owns that window.
func (l *rangeLedger) foreignConfChange(since, until clock.Instant, from, to raft.NodeID) bool {
	for _, rec := range l.committed {
		if rec.entry.Type != raft.EntryConfChange || rec.at < since || rec.at >= until {
			continue
		}
		cc, ok := raft.DecodeConfChange(rec.entry.Data)
		if !ok {
			continue
		}
		for _, ch := range cc.Changes {
			if ch.Node != from && ch.Node != to {
				return true
			}
		}
	}
	return false
}

func (l *Ledger) rangeByID(id uint64) *rangeLedger {
	for _, r := range l.ranges {
		if r.id == id {
			return r
		}
	}
	return nil
}

// RangeCount is how many ranges the harness has seen.
func (l *Ledger) RangeCount() int { return len(l.ranges) }

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

// SnapshotsByNode returns each recorded snapshot with the node that produced it,
// for diagnosis.
func (l *rangeLedger) SnapshotsByNode() []struct {
	Node int
	Rec  SnapshotRecord
} {
	out := make([]struct {
		Node int
		Rec  SnapshotRecord
	}, 0, len(l.snaps))
	for _, s := range l.snaps {
		out = append(out, struct {
			Node int
			Rec  SnapshotRecord
		}{s.node, s.rec})
	}
	return out
}

// CommittedPrefix exposes the committed prefix for diagnosis.
func (l *rangeLedger) CommittedPrefix(through raft.Index) ([]raft.Entry, bool) {
	return l.committedPrefix(through)
}

// NewLedger returns a ledger for n nodes.
func NewLedger(n int) *Ledger { return &Ledger{nodes: n} }

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
func (l *rangeLedger) RecordDurable(node int, stO provenance.Observed[DurableState], at clock.Instant) {
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
func (l *rangeLedger) holds(node int, e raft.Entry) bool {
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
func (l *rangeLedger) RecordSent(node int, mO provenance.Observed[raft.Message], at clock.Instant) {
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
func (l *rangeLedger) RecordReceived(node int, mO provenance.Observed[raft.Message], at clock.Instant) {
	l.recv = append(l.recv, sentRecord{node: node, msg: mO.Fact(), at: at})
}

// RecordApplied records entries a node applied.
//
// Observed: these are the entries the Ready handed over for application, taken
// as they cross the boundary rather than read back out of the state machine
// afterwards. Reading them back would ask the system what it applied, which is
// the question the oracle exists to answer independently.
func (l *rangeLedger) RecordApplied(node int, entriesO provenance.Observed[[]raft.Entry], at clock.Instant) {
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
func (l *rangeLedger) RecordSnapshot(node int, rO provenance.Observed[SnapshotRecord], at clock.Instant) {
	l.snaps = append(l.snaps, snapRecord{node: node, rec: rO.Fact(), at: at})
}

// RecordRangeBase records the state a range started from.
//
// Observed: these are the bytes handed to the engine as the new range's initial
// snapshot, taken as they cross the boundary.
func (l *rangeLedger) RecordRangeBase(data, conf provenance.Observed[[]byte]) {
	if l.base == nil {
		l.base = append([]byte(nil), data.Fact()...)
		l.birthConf = append([]byte(nil), conf.Fact()...)
	}
}

// CommittedConfig is the range's membership as its COMMITTED log says it is:
// the configuration it was born with, plus every committed configuration entry
// in index order.
//
// Committed, not active. The active configuration is effective on append and is
// not a function of anything the harness can see; this one is, and it is the
// only membership an outside observer is entitled to talk about.
func (l *rangeLedger) CommittedConfig() (raft.Configuration, bool) {
	conf, ok := raft.DecodeConfiguration(l.birthConf)
	if !ok {
		return raft.Configuration{}, false
	}
	entries := append([]raft.Entry(nil), l.Committed()...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Index < entries[j].Index })
	for _, e := range entries {
		if e.Type != raft.EntryConfChange {
			continue
		}
		if next, err := raft.ApplyConfEntry(conf, e.Data); err == nil {
			conf = next
		}
	}
	return conf, true
}

// firstConfChange finds the first committed single-server change of the given
// type naming node, at or after `since`, and reports its log index.
//
// Committed, not merely appended: a membership change that never commits is not
// something the cluster did, and judging a move by an entry that may still be
// truncated away would report on a decision nobody made.
func (l *rangeLedger) firstConfChange(since, until clock.Instant, kind raft.ConfChangeType, node raft.NodeID) (raft.Index, bool) {
	var best raft.Index
	found := false
	for _, rec := range l.committed {
		if rec.entry.Type != raft.EntryConfChange || rec.at < since || rec.at >= until {
			continue
		}
		cc, ok := raft.DecodeConfChange(rec.entry.Data)
		if !ok || len(cc.Changes) != 1 {
			continue
		}
		if cc.Changes[0].Type != kind || cc.Changes[0].Node != node {
			continue
		}
		// The EARLIEST by index, not the first encountered. The ledger records a
		// commit when it first sees one, and "first seen" is a property of the
		// schedule rather than of the log.
		if !found || rec.entry.Index < best {
			best, found = rec.entry.Index, true
		}
	}
	return best, found
}

// ConfRecord is one committed configuration entry, for diagnosis.
type ConfRecord struct {
	Index raft.Index
	At    clock.Instant
	Desc  string
}

// CommittedConfRecords lists this range's committed configuration entries.
func (l *rangeLedger) CommittedConfRecords() []ConfRecord {
	var out []ConfRecord
	for _, rec := range l.committed {
		if rec.entry.Type != raft.EntryConfChange {
			continue
		}
		cc, ok := raft.DecodeConfChange(rec.entry.Data)
		if !ok {
			continue
		}
		d := ""
		for _, ch := range cc.Changes {
			d += fmt.Sprintf("%s(%d) ", ch.Type, ch.Node)
		}
		out = append(out, ConfRecord{Index: rec.entry.Index, At: rec.at, Desc: d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// ID is this range's identifier.
func (l *rangeLedger) ID() uint64 { return l.id }

// Base is the state this range started from, or nil.
func (l *rangeLedger) Base() []byte { return l.base }

// Snapshots returns every recorded snapshot event, in order.
func (l *rangeLedger) Snapshots() []SnapshotRecord {
	out := make([]SnapshotRecord, 0, len(l.snaps))
	for _, s := range l.snaps {
		out = append(out, s.rec)
	}
	return out
}

// SnapshotsTaken and SnapshotsInstalled count the two directions, so a lane can
// ask whether either path ran at all.
func (l *rangeLedger) SnapshotsTaken() int {
	n := 0
	for _, s := range l.snaps {
		if s.rec.Taken {
			n++
		}
	}
	return n
}

// SnapshotsInstalled is the receiving half of the same count.
func (l *rangeLedger) SnapshotsInstalled() int { return len(l.snaps) - l.SnapshotsTaken() }

// AppliedBy returns what a node applied, in order. It exists for the apply
// continuity oracle, which is about one node's stream rather than about two
// nodes agreeing.
func (l *rangeLedger) AppliedBy(node int) []raft.Entry { return l.applied[node] }

// InstallsBy returns the snapshot indices a node installed, in order.
func (l *rangeLedger) InstallsBy(node int) []raft.Index {
	var out []raft.Index
	for _, s := range l.snaps {
		if s.node == node && !s.rec.Taken {
			out = append(out, s.rec.Index)
		}
	}
	return out
}

func (l *rangeLedger) noteLeader(term raft.Term, node int, at clock.Instant) {
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

func (l *rangeLedger) durableHardState(node int) raft.HardState {
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
func (l *rangeLedger) durableLast(node int) raft.Index {
	last := l.durableSnap[node].Index
	if x := lastIndexOf(l.durableLog[node]); x > last {
		last = x
	}
	return last
}

func (l *rangeLedger) isCommitted(i raft.Index) bool {
	for _, c := range l.committed {
		if c.entry.Index == i {
			return true
		}
	}
	return false
}

// Committed returns the committed entries observed so far.
func (l *rangeLedger) Committed() []raft.Entry {
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
func (l *rangeLedger) committedPrefix(through raft.Index) ([]raft.Entry, bool) {
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
func (l *rangeLedger) Transfers() []TransferRecord {
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
func (l *rangeLedger) Census() Census {
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
func (l *rangeLedger) Dump() string {
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

// --- the Ledger's own surface, keyed by range ---------------------------------
//
// Every entry point takes the range the fact belongs to. There is deliberately
// no unkeyed variant: a caller that did not know which range it was recording
// would be a caller recording something the oracles cannot use, and the compiler
// should say so rather than the sweep.

// Nodes is how many nodes this ledger covers.
func (l *Ledger) Nodes() int { return l.nodes }

// RecordDurable records what a node made durable for one range.
func (l *Ledger) RecordDurable(rangeID uint64, node int, st provenance.Observed[DurableState], at clock.Instant) {
	l.rev++
	l.forRange(rangeID).RecordDurable(node, st, at)
}

// RecordSent records a released message.
func (l *Ledger) RecordSent(rangeID uint64, node int, m provenance.Observed[raft.Message], at clock.Instant) {
	l.rev++
	l.forRange(rangeID).RecordSent(node, m, at)
}

// RecordReceived records a message a node accepted.
func (l *Ledger) RecordReceived(rangeID uint64, node int, m provenance.Observed[raft.Message], at clock.Instant) {
	l.rev++
	l.forRange(rangeID).RecordReceived(node, m, at)
}

// RecordApplied records entries a node applied.
func (l *Ledger) RecordApplied(rangeID uint64, node int, entries provenance.Observed[[]raft.Entry], at clock.Instant) {
	l.rev++
	l.forRange(rangeID).RecordApplied(node, entries, at)
}

// RecordSnapshot records a snapshot a node created or installed.
func (l *Ledger) RecordSnapshot(rangeID uint64, node int, r provenance.Observed[SnapshotRecord], at clock.Instant) {
	l.rev++
	l.forRange(rangeID).RecordSnapshot(node, r, at)
}

// Census aggregates every range's election activity.
//
// Summed rather than per range, because the question it answers is about the
// SWEEP -- did these schedules contend -- and a cluster of ten quiet ranges and
// one busy one contended. Per-range censuses are available from Ranges() for
// anyone asking a per-range question.
func (l *Ledger) Census() Census {
	var out Census
	for _, r := range l.ranges {
		c := r.Census()
		if c.Terms > out.Terms {
			out.Terms = c.Terms
		}
		out.ElectionsStart += c.ElectionsStart
		out.ElectionsWon += c.ElectionsWon
		out.SplitVotes += c.SplitVotes
		out.Leaders += c.Leaders
	}
	return out
}

// Transfers concatenates every range's leadership transfers.
func (l *Ledger) Transfers() []TransferRecord {
	var out []TransferRecord
	for _, r := range l.ranges {
		out = append(out, r.Transfers()...)
	}
	return out
}

// SnapshotsTaken and SnapshotsInstalled sum across ranges.
func (l *Ledger) SnapshotsTaken() int {
	n := 0
	for _, r := range l.ranges {
		n += r.SnapshotsTaken()
	}
	return n
}

// SnapshotsInstalled is the receiving half of the same count.
func (l *Ledger) SnapshotsInstalled() int {
	n := 0
	for _, r := range l.ranges {
		n += r.SnapshotsInstalled()
	}
	return n
}

// RecordRangeBase records the state a range started from, keyed by range.
func (l *Ledger) RecordRangeBase(rangeID uint64, data, conf provenance.Observed[[]byte]) {
	l.rev++
	l.forRange(rangeID).RecordRangeBase(data, conf)
}
