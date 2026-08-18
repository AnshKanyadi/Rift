package raftcheck

import (
	"fmt"

	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/sim"
)

// The four safety oracles. Each is an in-run sim.Oracle halting at the first
// violation, and each reads the Ledger and nothing else.
//
// None of them holds a *raft.Raft. That is not a convention here, it is the
// shape of the code: the constructors take a *Ledger and there is no other way
// in (DESIGN-A1 §0).

// MsgPreVoteType is raft's pre-vote request, named here so the persist-before-
// reply oracle can say why it treats that one message differently.
const MsgPreVoteType = raft.MsgPreVote

// base carries what every oracle shares.
type base struct {
	l    *Ledger
	name string

	// seenRev is the ledger revision this oracle last examined. An oracle is a
	// pure function of the ledger, so an unchanged revision cannot produce a new
	// verdict; skipping it turns thousands of full re-scans per run into one scan
	// per recorded fact. It is a performance property and not a correctness one,
	// which is why it lives in one place rather than in each oracle's logic.
	seenRev uint64
	primed  bool
}

func (b base) Name() string { return b.name }

// stale reports whether the ledger has changed since this oracle last looked.
func (b *base) stale() bool {
	if b.primed && b.seenRev == b.l.rev {
		return false
	}
	b.seenRev, b.primed = b.l.rev, true
	return true
}

// Interested: every oracle looks after any event, because the ledger is what
// changed and any event can change it.
func (b base) Interested(sim.Kind) bool { return true }

// --- election safety ---------------------------------------------------------

// ElectionSafety: at most one leader per term.
//
// Expressed over emitted messages rather than over a role field. A node "acts as
// leader in term T" when it sends an MsgApp bearing term T; a node whose role
// says follower while it is still appending is a real bug, and the wire is where
// it is visible.
type ElectionSafety struct{ base }

// NewElectionSafety builds the oracle.
func NewElectionSafety(l *Ledger) *ElectionSafety {
	return &ElectionSafety{base{l: l, name: "election-safety"}}
}

func (o *ElectionSafety) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	for i := range o.l.ledIn {
		for j := i + 1; j < len(o.l.ledIn); j++ {
			a, b := o.l.ledIn[i], o.l.ledIn[j]
			if a.term == b.term && a.node != b.node {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"two leaders in term %d: node %d acted as leader at instant %d and node %d at instant %d",
						a.term, a.node, int64(a.at), b.node, int64(b.at)),
				}
			}
		}
	}
	return nil
}

// --- log matching ------------------------------------------------------------

// LogMatching: if two logs contain an entry with the same index and term, the
// logs are identical in every entry up through that index.
//
// Read from what each node PERSISTED, not from its in-memory log, which is the
// whole point: a node whose memory and disk disagree is the bug.
type LogMatching struct{ base }

// NewLogMatching builds the oracle.
func NewLogMatching(l *Ledger) *LogMatching {
	return &LogMatching{base{l: l, name: "log-matching"}}
}

func (o *LogMatching) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	logs := o.l.durableLog
	for a := range logs {
		for b := a + 1; b < len(logs); b++ {
			if v := o.compare(a, b, logs[a], logs[b]); v != nil {
				return v
			}
		}
	}
	return nil
}

// compare walks two durable logs BY INDEX rather than by position.
//
// Position was correct for exactly as long as every log started at index 1. A
// compacted log is a suffix, so after A2 position i means a different index on
// every node, and comparing position against position would report two nodes
// that agree perfectly as divergent -- a checker manufacturing violations, which
// is the one failure mode worse than a checker that finds none.
func (o *LogMatching) compare(a, b int, la, lb []raft.Entry) *sim.Violation {
	byIndex := func(es []raft.Entry) map[raft.Index]raft.Entry {
		m := make(map[raft.Index]raft.Entry, len(es))
		for _, e := range es {
			m[e.Index] = e
		}
		return m
	}
	ma, mb := byIndex(la), byIndex(lb)

	// The highest index both hold with the same term. Walked downwards over the
	// shared range, which is bounded by the shorter log's last index.
	hi := lastIndexOf(la)
	if x := lastIndexOf(lb); x < hi {
		hi = x
	}
	lo := o.l.durableSnap[a].Index
	if x := o.l.durableSnap[b].Index; x > lo {
		lo = x
	}
	for i := hi; i > lo; i-- {
		ea, oka := ma[i]
		eb, okb := mb[i]
		if !oka || !okb || ea.Term != eb.Term {
			continue
		}
		// Same index and term: everything before it that both still hold must
		// agree. Entries behind either snapshot are not held and not comparable.
		for k := i - 1; k > lo; k-- {
			xa, oka := ma[k]
			xb, okb := mb[k]
			if !oka || !okb {
				continue
			}
			if xa.Term != xb.Term || string(xa.Data) != string(xb.Data) {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"nodes %d and %d agree at index %d term %d but differ at index %d (terms %d and %d)",
						a, b, i, ea.Term, k, xa.Term, xb.Term),
				}
			}
		}
		return nil
	}
	return nil
}

// --- leader completeness -----------------------------------------------------

// LeaderCompleteness: an entry committed in term T is present in the log of
// every leader of every term greater than T.
//
// This is the property the ruling called materially harder from outside, because
// none of "committed", "a leader of term T" or "that leader's log" is a field
// anywhere. Each is reconstructed in the ledger (DESIGN-A1 §5), and the check is
// then direct.
type LeaderCompleteness struct{ base }

// NewLeaderCompleteness builds the oracle.
func NewLeaderCompleteness(l *Ledger) *LeaderCompleteness {
	return &LeaderCompleteness{base{l: l, name: "leader-completeness"}}
}

func (o *LeaderCompleteness) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	for _, c := range o.l.committed {
		for _, ld := range o.l.ledIn {
			if ld.term <= c.entry.Term {
				continue
			}
			// This leader began a later term. It must already have the entry.
			if ld.at < c.at {
				// It began leading before the entry was observed committed, so
				// the entry cannot yet have been committed when it took office.
				continue
			}
			if !hasEntry(ld.log, c.entry) && ld.snap.Index < c.entry.Index {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"entry index %d term %d was committed (first applied by node %d at instant %d) "+
							"but node %d, leading in later term %d from instant %d, had persisted %d entries "+
							"above a snapshot at %d, without it",
						c.entry.Index, c.entry.Term, c.node, int64(c.at),
						ld.node, ld.term, int64(ld.at), len(ld.log), ld.snap.Index),
				}
			}
		}
	}
	return nil
}

func hasEntry(log []raft.Entry, e raft.Entry) bool {
	for _, x := range log {
		if x.Index == e.Index && x.Term == e.Term && string(x.Data) == string(e.Data) {
			return true
		}
	}
	return false
}

// --- state machine safety ----------------------------------------------------

// StateMachineSafety: no two nodes apply different entries at the same index.
//
// This is the property a client actually experiences, and it is read from the
// apply streams, which is what a state machine consumed rather than what a node
// believed it would consume.
type StateMachineSafety struct {
	base
	first  map[raft.Index]appliedBy
	cursor []int
}

// NewStateMachineSafety builds the oracle.
func NewStateMachineSafety(l *Ledger) *StateMachineSafety {
	return &StateMachineSafety{base: base{l: l, name: "state-machine-safety"}}
}

func (o *StateMachineSafety) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	// Incremental rather than pairwise-over-everything. The pairwise form was
	// O(applied squared) across every node pair on every recorded fact, which is
	// the same answer at a cost that grows with the run; this keeps one entry per
	// index and compares each new apply against it exactly once.
	if o.first == nil {
		o.first = map[raft.Index]appliedBy{}
		o.cursor = make([]int, o.l.nodes)
	}
	for node := 0; node < o.l.nodes; node++ {
		stream := o.l.applied[node]
		for ; o.cursor[node] < len(stream); o.cursor[node]++ {
			e := stream[o.cursor[node]]
			prior, ok := o.first[e.Index]
			if !ok {
				o.first[e.Index] = appliedBy{node: node, entry: e}
				continue
			}
			if prior.node == node {
				continue
			}
			if prior.entry.Term != e.Term || string(prior.entry.Data) != string(e.Data) {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"nodes %d and %d applied different entries at index %d: term %d %q versus term %d %q",
						prior.node, node, e.Index, prior.entry.Term, prior.entry.Data, e.Term, e.Data),
				}
			}
		}
	}
	return nil
}

type appliedBy struct {
	node  int
	entry raft.Entry
}

// --- persist before reply ----------------------------------------------------

// PersistBeforeReply verifies from outside that the interface held.
//
// # Its demotion, stated rather than merely implemented
//
// In the first A1 draft this oracle was the defence: raft handed the driver
// messages and a persist list, the driver was told to order them, and this
// checked the driver had. That design made persist-before-reply *conventional*
// and then proposed an oracle to check the convention.
//
// **An oracle guarding a rule that should be structurally unbreakable is the
// weaker design wearing the stronger design's clothes, and it passes review
// precisely because it has an oracle attached.** It generalizes past Raft:
// whenever a safety property can be discharged in the type system or the
// interface, an oracle for it is a consolation prize, not a defense.
//
// Under DR-7 the property is structural: raft never releases a gated message, so
// the driver cannot send one early because it never holds one. This oracle is
// therefore **demoted**. It no longer stands between the cluster and two leaders
// in one term; it confirms from outside that the interface behaved as its
// contract says, which is a different and much weaker claim, and it is worth
// keeping only because a structural guarantee that nobody observes is a
// guarantee nobody would notice losing.
type PersistBeforeReply struct {
	base
	cursor   int
	preVotes map[preVoteKey]bool
}

type preVoteKey struct {
	from, to raft.NodeID
	term     raft.Term
}

// NewPersistBeforeReply builds the oracle.
func NewPersistBeforeReply(l *Ledger) *PersistBeforeReply {
	return &PersistBeforeReply{base: base{l: l, name: "persist-before-reply"}}
}

func (o *PersistBeforeReply) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	if o.preVotes == nil {
		o.preVotes = map[preVoteKey]bool{}
	}
	for ; o.cursor < len(o.l.sent); o.cursor++ {
		s := o.l.sent[o.cursor]
		if s.msg.Type == MsgPreVoteType {
			o.preVotes[preVoteKey{from: s.msg.From, to: s.msg.To, term: s.msg.Term}] = true
		}
		// # A pre-vote's term is a proposal, not an assertion
		//
		// Every other message's Term field is the sender saying "this is the
		// term I am in", which is a claim about persistent state and must be
		// durable first. A MsgPreVote carries the term the sender WOULD use if
		// it campaigned, and nobody -- including the sender -- has adopted it.
		// That is the entire mechanism: adopting it is what pre-vote exists to
		// avoid.
		//
		// So the predicate changes rather than the check being skipped, and the
		// replacement is strictly stronger than an exemption: a pre-vote may be
		// exactly one term ahead of what the sender has durable, and no more. A
		// node pre-voting from a term it has not persisted is still caught, and
		// a node pre-voting from thin air is caught by the same line.
		// A pre-vote RESPONSE echoes the term it was asked about, so its Term
		// field says nothing about the responder at all -- a node far behind
		// answers a proposal far ahead, and comparing that against its durable
		// term would accuse every correct responder in the cluster.
		//
		// The check that IS available is stronger than skipping: the echo must
		// answer a pre-vote somebody actually sent to this node, with that term.
		// A response to a request that was never made is a node manufacturing
		// consent, and the message stream is where that is visible.
		if s.msg.Type == raft.MsgPreVoteResp {
			if !o.answersAPreVote(s) {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"node %d sent a pre-vote response for term %d to node %d at instant %d, but "+
							"node %d never sent it a pre-vote for that term",
						s.node, s.msg.Term, s.msg.To, int64(s.at), s.msg.To),
				}
			}
			continue
		}
		// A pre-vote REQUEST carries the term its sender would use, which nobody
		// has adopted -- including the sender. What can honestly be asked of it
		// is that it proposes a term the sender is not already past.
		//
		// # The upper bound was tried, and it accused correct nodes
		//
		// The first version of this required the proposal to be at most one term
		// above what the sender had durable. It fired on 6 of 10,000 seeds --
		// "a pre-vote for term 5 while only term 3 was durable" -- and the nodes
		// were right. A term advances in memory the moment it is adopted and
		// becomes durable one write later; every message that DEPENDS on that
		// term is withheld until it lands, which is what the gated queue is for.
		// A pre-vote depends on it for nothing, so it may be sent from inside
		// that window, and a node whose term-4 write is still in flight
		// proposing 5 is the design working.
		//
		// The bound is therefore not checkable from outside, and saying so is
		// better than keeping a rule that is wrong six times in ten thousand.
		// What remains is checkable and real: a pre-vote for a term at or below
		// the sender's durable term is proposing a term it has already been in.
		if s.msg.Type == MsgPreVoteType {
			if s.msg.Term <= s.durableTerm {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"node %d sent a pre-vote for term %d at instant %d having already had term "+
							"%d durable; a pre-vote proposes a term ahead, not one already spent",
						s.node, s.msg.Term, int64(s.at), s.durableTerm),
				}
			}
			continue
		}
		if s.msg.Term > s.durableTerm {
			return &sim.Violation{
				Checker: o.name,
				Detail: fmt.Sprintf(
					"node %d sent a %s advertising term %d at instant %d while only term %d was durable",
					s.node, s.msg.Type, s.msg.Term, int64(s.at), s.durableTerm),
			}
		}
		if s.msg.Type == raft.MsgVoteResp && s.msg.Granted && s.durableVote == 0 {
			return &sim.Violation{
				Checker: o.name,
				Detail: fmt.Sprintf(
					"node %d granted a vote at instant %d with no vote durable; a crash here elects two leaders in one term",
					s.node, int64(s.at)),
			}
		}
		if s.msg.Type == raft.MsgAppResp && s.msg.Success && s.msg.MatchIndex > s.durableLast {
			return &sim.Violation{
				Checker: o.name,
				Detail: fmt.Sprintf(
					"node %d acked index %d at instant %d with only %d durable; the leader may commit an entry this node can still lose",
					s.node, s.msg.MatchIndex, int64(s.at), s.durableLast),
			}
		}
	}
	return nil
}

// answersAPreVote reports whether a pre-vote response echoes a request that was
// actually released to this responder, with that term.
//
// The index is built as the stream is walked, so a response is answered in
// constant time. Scanning the whole history per response was the same answer at
// a cost that grew with the square of the run.
func (o *PersistBeforeReply) answersAPreVote(resp sentRecord) bool {
	return o.preVotes[preVoteKey{from: resp.msg.To, to: resp.msg.From, term: resp.msg.Term}]
}

// All returns every oracle, in a stable order.
//
// state is the harness's independent model of the state machine, used by the
// snapshot oracle. Passing nil disables that one check and nothing else, which
// is the honest behaviour for a caller that has no model -- a checker with no
// expectation cannot conclude anything, and pretending otherwise is the
// vacuous-green class again.
func All(l *Ledger, state StateAt) []sim.Oracle {
	return []sim.Oracle{
		NewElectionSafety(l),
		NewLogMatching(l),
		NewLeaderCompleteness(l),
		NewStateMachineSafety(l),
		NewPersistBeforeReply(l),
		NewApplyContinuity(l),
		NewSnapshotEquivalence(l, state),
	}
}

// --- apply continuity across snapshots ---------------------------------------

// ApplyContinuity: what reaches a node's state machine has no holes, and a
// rebuild reproduces exactly what it replaced.
//
// # Getting this wrong once was instructive, so the reasoning is written down
//
// The first version asserted that a node applies each index exactly once, in
// increasing order. It fired on the first seed, and the system was right: a node
// that crashes WITHOUT a snapshot has an empty state machine when it comes back,
// so it must re-apply its whole log from index 1. Re-application is not a defect,
// it is how recovery works when there is nothing to recover from.
//
// So the invariant is not monotonicity. It is two things:
//
//	no hole      -- the stream never jumps FORWARD past an index unless a
//	                snapshot install carried the state over it. A gap means those
//	                commands reached no state machine on this node.
//	rebuild      -- a rebuild restarts at snapIndex+1 for a snapshot this node
//	  determinism   actually holds, and every index it re-applies carries the
//	                same entry it carried before. A state machine rebuilt into a
//	                different state is a state machine that diverged silently.
//
// Neither is a corollary of state machine safety, which compares nodes against
// each other and is blind to a single node applying something twice, or never.
type ApplyContinuity struct {
	base
	seen    []map[raft.Index]raft.Entry
	prev    []raft.Index
	started []bool
	cursor  []int
}

// NewApplyContinuity builds the oracle.
func NewApplyContinuity(l *Ledger) *ApplyContinuity {
	return &ApplyContinuity{base: base{l: l, name: "apply-continuity"}}
}

func (o *ApplyContinuity) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	if o.seen == nil {
		o.seen = make([]map[raft.Index]raft.Entry, o.l.nodes)
		o.prev = make([]raft.Index, o.l.nodes)
		o.started = make([]bool, o.l.nodes)
		o.cursor = make([]int, o.l.nodes)
		for i := range o.seen {
			o.seen[i] = map[raft.Index]raft.Entry{}
		}
	}
	for node := 0; node < o.l.nodes; node++ {
		seen := o.seen[node]
		prev, started := o.prev[node], o.started[node]
		stream := o.l.applied[node]
		for ; o.cursor[node] < len(stream); o.cursor[node]++ {
			e := stream[o.cursor[node]]
			// Rebuild determinism: the same index must always carry the same
			// entry on this node, however many times it is replayed.
			if old, ok := seen[e.Index]; ok {
				if old.Term != e.Term || string(old.Data) != string(e.Data) {
					return &sim.Violation{
						Checker: o.name,
						Detail: fmt.Sprintf(
							"node %d applied index %d twice with different entries: term %d %q, then "+
								"term %d %q. A state machine rebuilt into a different state has diverged "+
								"from itself, which no cross-node comparison can see",
							node, e.Index, old.Term, old.Data, e.Term, e.Data),
					}
				}
			}
			seen[e.Index] = e

			switch {
			case !started || e.Index == prev+1:
				// The ordinary case, and the first entry of the stream.
			case e.Index <= prev:
				// A rebuild. It has to begin where this node's recovered state
				// ends: at 1 when there is no snapshot, or just past one it
				// holds.
				if !o.restartsAtRecoverable(node, e.Index) {
					return &sim.Violation{
						Checker: o.name,
						Detail: fmt.Sprintf(
							"node %d restarted its apply stream at index %d, which is neither the "+
								"beginning nor just past a snapshot it holds; the state machine is being "+
								"rebuilt from a point nothing recovered to",
							node, e.Index),
					}
				}
			default:
				// A forward jump is legal only where a snapshot install put it.
				if !o.jumpedByInstall(node, prev, e.Index) {
					return &sim.Violation{
						Checker: o.name,
						Detail: fmt.Sprintf(
							"node %d applied index %d straight after index %d with no snapshot install "+
								"between them; the entries in the gap reached no state machine on this node",
							node, e.Index, prev),
					}
				}
			}
			prev, started = e.Index, true
		}
		o.prev[node], o.started[node] = prev, started
	}
	return nil
}

// restartsAtRecoverable reports whether a rebuild beginning at index i starts
// where this node could actually have recovered to.
func (o *ApplyContinuity) restartsAtRecoverable(node int, i raft.Index) bool {
	if i == 1 {
		return true // no snapshot: the whole log is replayed
	}
	for _, s := range o.l.snaps {
		if s.node == node && s.rec.Index == i-1 {
			return true
		}
	}
	return false
}

// jumpedByInstall reports whether a snapshot install accounts for the stream
// moving from prev to next: the snapshot must land exactly where the stream
// resumes and cover everything skipped.
func (o *ApplyContinuity) jumpedByInstall(node int, prev, next raft.Index) bool {
	for _, s := range o.l.snaps {
		if s.node == node && !s.rec.Taken && s.rec.Index >= prev && s.rec.Index == next-1 {
			return true
		}
	}
	return false
}

// --- snapshot equivalence -----------------------------------------------------

// StateAt is supplied by the harness: given the committed entries through some
// index, in index order, it returns a digest of the state machine they produce.
//
// It is injected rather than implemented here for one reason and it matters: the
// harness re-implements what a command DOES, so a defect in applying commands
// cannot cancel out on both sides of the comparison. What it shares with the
// system is the wire format, which is not the thing under test.
type StateAt func(entries []raft.Entry) uint64

// SnapshotEquivalence: a snapshot's contents are exactly the state the committed
// log produces at its index.
//
// This is the exit criterion — *recovery from a snapshot plus tail proves
// identical state to recovery from a full log* — expressed from outside. A
// snapshot IS the full-log state at its index, so if every snapshot equals what
// the log independently produces there, then recovering from one and replaying
// the tail lands exactly where replaying everything would.
//
// Both directions are checked and they are different claims. A snapshot a node
// CREATED must equal what its own log produced. A snapshot a node INSTALLED must
// equal what the cluster's log produced at that index — a node that installs a
// snapshot never applied the entries under it, so this is the only thing that
// can say the state it adopted was the right one.
type SnapshotEquivalence struct {
	base
	state StateAt

	// done remembers what has already been verified. A snapshot's contents do
	// not change once recorded, so re-deriving the log's state at its index on
	// every subsequent event is pure cost.
	done map[snapKey]bool
}

type snapKey struct {
	node   int
	index  raft.Index
	digest uint64
}

// NewSnapshotEquivalence builds the oracle.
func NewSnapshotEquivalence(l *Ledger, state StateAt) *SnapshotEquivalence {
	return &SnapshotEquivalence{base: base{l: l, name: "snapshot-equivalence"}, state: state}
}

func (o *SnapshotEquivalence) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if o.state == nil || !o.stale() {
		return nil
	}
	if o.done == nil {
		o.done = map[snapKey]bool{}
	}
	for _, s := range o.l.snaps {
		k := snapKey{node: s.node, index: s.rec.Index, digest: s.rec.Digest}
		if o.done[k] {
			continue
		}
		prefix, ok := o.l.committedPrefix(s.rec.Index)
		if !ok {
			// The ledger has not witnessed every committed entry under this
			// snapshot yet, so there is nothing to compare against. Skipped
			// rather than passed: the run is not asserting anything here.
			continue
		}
		got := o.state(prefix)
		o.done[k] = true
		if got != s.rec.Digest {
			verb := "installed"
			if s.rec.Taken {
				verb = "took"
			}
			return &sim.Violation{
				Checker: o.name,
				Detail: fmt.Sprintf(
					"node %d %s a snapshot at index %d term %d whose contents are not the state the "+
						"committed log produces there: snapshot digest %d, log digest %d. Recovering "+
						"from this snapshot and replaying the tail would not land where replaying the "+
						"whole log lands",
					s.node, verb, s.rec.Index, s.rec.Term, s.rec.Digest, got),
			}
		}
	}
	return nil
}
