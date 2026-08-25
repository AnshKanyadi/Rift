package raftcheck

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/anshkanyadi/rift/hlc"
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

// eachRange walks every range in order and returns the first violation, with the
// range named in the detail.
//
// Every oracle goes through here. DESIGN-A4 §6 is the audit that says why: over
// many ranges, six of these would produce false positives and two -- log matching
// and persist-before-reply -- would go quietly weaker, which is the failure this
// project has now recorded ten of.
// # Keyed by range ID, not by position, and that distinction was a bug
//
// The first version passed the range's index in the sorted slice and the
// stateful oracles keyed their cursors by it. Ranges are inserted in SORTED
// position, not appended, so a range born with a lower id shifts every index
// after it -- and each oracle's cursor silently became attached to a different
// range's stream. State machine safety then reported two nodes applying
// different entries at an index where the dump showed them applying the same
// one, which is a checker manufacturing a violation.
//
// An identifier is not a position. That is BUG-004's sentence, and this is the
// fourth subsystem to need it.
func (b *base) eachRange(f func(id uint64, l *rangeLedger) *sim.Violation) *sim.Violation {
	for _, rl := range b.l.ranges {
		if v := f(rl.id, rl); v != nil {
			v.Detail = fmt.Sprintf("range %d: %s", rl.id, v.Detail)
			return v
		}
	}
	return nil
}

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
	return o.eachRange(o.check)
}

func (o *ElectionSafety) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	for i := range l.ledIn {
		for j := i + 1; j < len(l.ledIn); j++ {
			a, b := l.ledIn[i], l.ledIn[j]
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
	return o.eachRange(o.check)
}

func (o *LogMatching) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	logs := l.durableLog
	for a := range logs {
		for b := a + 1; b < len(logs); b++ {
			if v := o.compare(l, a, b, logs[a], logs[b]); v != nil {
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
func (o *LogMatching) compare(l *rangeLedger, a, b int, la, lb []raft.Entry) *sim.Violation {
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
	lo := l.durableSnap[a].Index
	if x := l.durableSnap[b].Index; x > lo {
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
	return o.eachRange(o.check)
}

func (o *LeaderCompleteness) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	for _, c := range l.committed {
		for _, ld := range l.ledIn {
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
	// Per range, indexed by the range's position in the ledger's sorted slice.
	// Ranges are only ever appended, so the index is stable -- and sharing one
	// cursor across ranges would advance it past streams it never read.
	per map[uint64]*smsState
}

type smsState struct {
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
	return o.eachRange(o.check)
}

func (o *StateMachineSafety) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	// Incremental rather than pairwise-over-everything. The pairwise form was
	// O(applied squared) across every node pair on every recorded fact, which is
	// the same answer at a cost that grows with the run; this keeps one entry per
	// index and compares each new apply against it exactly once.
	if o.per == nil {
		o.per = map[uint64]*smsState{}
	}
	st := o.per[rangeID]
	if st == nil {
		st = &smsState{first: map[raft.Index]appliedBy{}, cursor: make([]int, l.nodes)}
		o.per[rangeID] = st
	}
	for node := 0; node < l.nodes; node++ {
		stream := l.applied[node]
		for ; st.cursor[node] < len(stream); st.cursor[node]++ {
			e := stream[st.cursor[node]]
			prior, ok := st.first[e.Index]
			if !ok {
				st.first[e.Index] = appliedBy{node: node, entry: e}
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
	per map[uint64]*pbrState
}

type pbrState struct {
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
	return o.eachRange(o.check)
}

func (o *PersistBeforeReply) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	if o.per == nil {
		o.per = map[uint64]*pbrState{}
	}
	st := o.per[rangeID]
	if st == nil {
		st = &pbrState{preVotes: map[preVoteKey]bool{}}
		o.per[rangeID] = st
	}
	for ; st.cursor < len(l.sent); st.cursor++ {
		s := l.sent[st.cursor]
		if s.msg.Type == MsgPreVoteType {
			st.preVotes[preVoteKey{from: s.msg.From, to: s.msg.To, term: s.msg.Term}] = true
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
			if !st.preVotes[preVoteKey{from: s.msg.To, to: s.msg.From, term: s.msg.Term}] {
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

// All returns every oracle, in a stable order.
//
// state is the harness's independent model of the state machine, used by the
// snapshot oracle. Passing nil disables that one check and nothing else, which
// is the honest behaviour for a caller that has no model -- a checker with no
// expectation cannot conclude anything, and pretending otherwise is the
// vacuous-green class again.
// PercolatorInvariants is deliberately NOT in this list. It is a FINAL-STATE
// check -- the harness runs it once after the loop, because evaluating it per
// step would assert an eventual property against a run caught mid-cleanup and
// would replay every range's whole log to do it.
func All(l *Ledger, state StateAt, splits SplitsAt, extent ExtentOf, reads ReadsAt, facts TxnFactsAt) []sim.Oracle {
	// A nil ValueAtIndex leaves read-index agreement with no model, and the
	// oracle then compares nothing rather than asserting over an expectation it
	// does not have -- the same rule the paragraph above states for StateAt.
	os, _, _ := AllWithRebalance(l, state, splits, extent, reads, facts, nil)
	return os
}

// AllWithRebalance is All plus a handle on the rebalance oracle, whose
// unattributable count the sweep reports. Returned rather than reachable from
// the slice, because finding an oracle by type-asserting over a []sim.Oracle is
// how a checker's internals become everybody's business.
func AllWithRebalance(l *Ledger, state StateAt, splits SplitsAt, extent ExtentOf, reads ReadsAt,
	facts TxnFactsAt, valueAt ValueAtIndex) ([]sim.Oracle, *RebalanceSafety, *ReadIndexAgreement) {
	reb := NewRebalanceSafety(l)
	agree := NewReadIndexAgreement(l, valueAt)
	return []sim.Oracle{
		NewElectionSafety(l),
		NewLogMatching(l),
		NewLeaderCompleteness(l),
		NewStateMachineSafety(l),
		NewPersistBeforeReply(l),
		NewApplyContinuity(l),
		NewSnapshotEquivalence(l, state),
		NewSingleServerChange(l),
		reb,
		NewSplitPartition(l, splits, extent),
		NewMVCCReadCorrectness(l, reads),
		NewTransactionAtomicity(l, facts),
		// A7's differential. A FIXTURE rather than a lane (ruling 5): it is the
		// only instrument here that can catch a stale read no client observed,
		// and an instrument of that description is worth nothing if it runs when
		// somebody remembers.
		agree,
	}, reb, agree
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
	per map[uint64]*acState
}

type acState struct {
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
	return o.eachRange(o.check)
}

func (o *ApplyContinuity) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	if o.per == nil {
		o.per = map[uint64]*acState{}
	}
	if o.per[rangeID] == nil {
		st := &acState{
			seen:    make([]map[raft.Index]raft.Entry, l.nodes),
			prev:    make([]raft.Index, l.nodes),
			started: make([]bool, l.nodes),
			cursor:  make([]int, l.nodes),
		}
		for i := range st.seen {
			st.seen[i] = map[raft.Index]raft.Entry{}
		}
		o.per[rangeID] = st
	}
	st := o.per[rangeID]
	for node := 0; node < l.nodes; node++ {
		seen := st.seen[node]
		prev, started := st.prev[node], st.started[node]
		stream := l.applied[node]
		for ; st.cursor[node] < len(stream); st.cursor[node]++ {
			e := stream[st.cursor[node]]
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
				if !o.restartsAtRecoverable(l, node, e.Index) {
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
				if !o.jumpedByInstall(l, node, prev, e.Index) {
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
		st.prev[node], st.started[node] = prev, started
	}
	return nil
}

// restartsAtRecoverable reports whether a rebuild beginning at index i starts
// where this node could actually have recovered to.
func (o *ApplyContinuity) restartsAtRecoverable(l *rangeLedger, node int, i raft.Index) bool {
	if i == 1 {
		return true // no snapshot: the whole log is replayed
	}
	for _, s := range l.snaps {
		if s.node == node && s.rec.Index == i-1 {
			return true
		}
	}
	return false
}

// jumpedByInstall reports whether a snapshot install accounts for the stream
// moving from prev to next: the snapshot must land exactly where the stream
// resumes and cover everything skipped.
func (o *ApplyContinuity) jumpedByInstall(l *rangeLedger, node int, prev, next raft.Index) bool {
	for _, s := range l.snaps {
		if s.node == node && !s.rec.Taken && s.rec.Index >= prev && s.rec.Index == next-1 {
			return true
		}
	}
	return false
}

// --- split partition -----------------------------------------------------------

// SplitStep is one split entry in a range's committed log, as the harness's
// model reads it.
//
// Applied is the model's verdict on whether the entry took effect: a split
// applies only against exactly the extent it names, and one that names an extent
// the range has moved past is refused by every replica.
type SplitStep struct {
	Index   raft.Index
	Applied bool

	Child      uint64
	ChildStart []byte
	ChildEnd   []byte
	ChildEpoch uint64
}

// SplitsAt and ExtentOf are supplied by the harness, which owns the wire format
// and restates the rule for applying a split in its own terms.
type SplitsAt func(base []byte, entries []raft.Entry) []SplitStep

// ExtentOf reads the extent out of a range's recorded birth state.
type ExtentOf func(base []byte) (start, end []byte, epoch uint64, ok bool)

// SplitPartition: a split creates exactly the range it named, and a refused
// split creates nothing.
//
// # The one thing no per-range oracle can see
//
// Every other oracle in this package judges one range against its own history.
// That is right, and it is why they survived A4 -- but it leaves the failure
// that exists only BETWEEN two ranges completely unwatched: a parent that splits
// one way and a child that is born another way. Each is internally consistent.
// Together they are two ranges disagreeing about who owns a key.
//
// So this oracle compares two facts the system produced independently: the split
// entry sitting in the parent's committed log, and the birth state the child's
// replicas actually wrote. The harness decodes both -- sharing the wire format,
// which is not the thing under test -- and nothing here asks a node what it
// thinks it did.
//
// The second clause is the one worth naming. A split entry that names a stale
// extent is refused by every replica, and a refused split must create NOTHING.
// If a range comes into existence from an entry the cluster declined, it exists
// on some replicas and not others, and no per-range oracle would ever look at
// it, because from inside its own history it is perfectly consistent.
type SplitPartition struct {
	base
	splits SplitsAt
	extent ExtentOf
}

func NewSplitPartition(l *Ledger, splits SplitsAt, extent ExtentOf) *SplitPartition {
	return &SplitPartition{base: base{l: l, name: "split-partition"}, splits: splits, extent: extent}
}

func (o *SplitPartition) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	if v := o.everyRangeWasBorn(); v != nil {
		return v
	}
	return o.eachRange(func(id uint64, rl *rangeLedger) *sim.Violation {
		for _, st := range o.splits(rl.base, rl.Committed()) {
			child := o.l.rangeByID(st.Child)
			born := child != nil && child.base != nil

			if !st.Applied {
				if born {
					return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
						"the split entry at index %d names an extent this range had already moved past, "+
							"so every replica refused it -- and range %d exists anyway. A range born from "+
							"an entry the cluster declined is claimed by whoever applied it and by nobody "+
							"else, and no oracle that judges one range at a time would ever see it",
						st.Index, st.Child)}
				}
				continue
			}
			if !born {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"the split entry at index %d took effect, so range %d owns the keys above %q -- and "+
						"no replica anywhere ever created it. Those keys are now owned by nothing",
					st.Index, st.Child, st.ChildStart)}
			}
			start, end, epoch, ok := o.extent(child.base)
			if !ok {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d was born from the split at index %d with a state carrying no extent at all",
					st.Child, st.Index)}
			}
			if !bytes.Equal(start, st.ChildStart) || !bytes.Equal(end, st.ChildEnd) || epoch != st.ChildEpoch {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"the split at index %d says range %d covers [%q,%q) at epoch %d, and range %d was "+
						"actually born covering [%q,%q) at epoch %d. The two ranges disagree about which "+
						"keys belong to which, which is the one failure a per-range oracle cannot see",
					st.Index, st.Child, st.ChildStart, st.ChildEnd, st.ChildEpoch,
					st.Child, start, end, epoch)}
			}
		}
		return nil
	})
}

// everyRangeWasBorn: no range exists that no committed log ever created.
//
// # The two streams a split stands on, and the check that they stayed ordered
//
// A split writes its effects on TWO durability streams: the left range's log
// holds the entry, and the right range's birth snapshot is a separate batch on
// a separate range's keys. A2's rule for a message attesting to state in two
// independent streams is that it waits for BOTH -- and the honest answer for a
// split is that these two are NOT independent. The log entry is written first
// and durability advances in engine sequence order, so the effect cannot be
// durable while the entry that justifies it is not.
//
// That is an argument, and it stands on a property of the engine. This is the
// check that fires if the argument ever stops holding: a crash that kept the
// right range's birth snapshot and lost the left's split entry leaves a range
// that no committed log created, claiming keys the left range still claims.
//
// Nothing else would see it. The child is internally consistent -- it has a
// birth state and a log of its own -- and the parent is internally consistent
// too, because after recovery its log simply has no split in it. Both look
// perfect from inside; only the pair is wrong.
func (o *SplitPartition) everyRangeWasBorn() *sim.Violation {
	created := map[uint64]bool{}
	for _, rl := range o.l.ranges {
		for _, st := range o.splits(rl.base, rl.Committed()) {
			if st.Applied {
				created[st.Child] = true
			}
		}
	}
	for _, rl := range o.l.ranges {
		if rl.id == firstRangeID || rl.base == nil || created[rl.id] {
			continue
		}
		return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
			"range %d exists -- some replica wrote its birth state -- and no committed log anywhere "+
				"contains the split that created it. Its keys are claimed by it and by whatever range "+
				"still believes it owns them, and no oracle that judges one range at a time can see "+
				"that, because both of them are perfectly consistent with their own histories",
			rl.id)}
	}
	return nil
}

// firstRangeID is the range every machine is born hosting. It is the one range
// no split creates, so it is the one exception to everyRangeWasBorn.
//
// Duplicated from store rather than imported: this package takes value types
// from raft and nothing from the system it judges.
const firstRangeID = 1

// --- rebalance safety ---------------------------------------------------------

// RebalanceSafety: a replica move adds before it removes.
//
// CLAUDE.md's invariant, in its own words: *replica moves are add-then-remove;
// quorum availability is never voluntarily reduced.* The two halves are one
// statement once membership changes one server at a time (D-A3-1). An AddVoter
// raises the committed voter count by one and a RemoveNode lowers it by one, so
// if the destination is a voter before the source is removed, the count goes
// N -> N+1 -> N and never dips. Order is therefore the whole property, and it is
// visible in the committed log without asking anybody what they meant.
//
// What the oracle takes from the harness is which move was ORDERED -- the range
// and the two nodes. What it takes from the system is nothing: whether the move
// happened, and in what order, it reads from committed entries.
//
// # Why an unfinished move is not a violation
//
// A move can stall. The leader that ordered it can lose leadership between the
// add and the remove, and the next leader has no idea a move was in progress.
// That leaves the range with an extra replica and no removal, which is
// wasteful and completely safe -- it is the direction the invariant WANTS to
// fail in. So a missing removal passes here, and the sweep's non-vacuity check
// is what stops "every move stalled" from reading as evidence.
type RebalanceSafety struct {
	base

	// unattributable is the set of moves whose removal fell inside a window in
	// which the churn driver also ordered a change to the same node. Neither
	// pass nor violation: the log cannot say whose removal it is, and Amendment
	// A4's answer to "the checker could not tell" is a third outcome rather than
	// a guess.
	//
	// A SET, keyed by the move's index, because OnStep re-evaluates every move
	// on every step it runs. A counter reported 5,651 unattributable moves in a
	// run that ordered nine, which is not a number anybody can act on -- and is
	// the same shape as the transfer's read counter in BUG-020: a count of
	// observations is not a count of things.
	unattributable map[int]bool
}

func NewRebalanceSafety(l *Ledger) *RebalanceSafety {
	return &RebalanceSafety{
		base: base{l: l, name: "rebalance-safety"}, unattributable: map[int]bool{}}
}

// Unattributable is how many DISTINCT moves this run could not judge.
func (o *RebalanceSafety) Unattributable() int { return len(o.unattributable) }

func (o *RebalanceSafety) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	for i, m := range o.l.moves {
		rl := o.l.rangeByID(m.Range)
		if rl == nil {
			continue
		}
		until := o.l.moveEnds(i)
		removed, ok := rl.firstConfChange(m.At, until, raft.ConfChangeRemoveNode, m.From)
		if !ok {
			continue
		}
		// # Whose removal is this?
		//
		// If the churn driver also ordered a change to this node inside the
		// move's window, the committed log holds a removal that either driver
		// could have caused, and nothing in it says which. Judging it anyway is
		// how 252 of 300 seeds became false violations; judging it the other way
		// -- calling it fine -- would be a pass over a question that was never
		// answered. So it is neither, and it is counted.
		if o.l.churnTouched(int(m.From), m.At, until) {
			o.unattributable[i] = true
			continue
		}
		added, ok := rl.firstConfChange(m.At, until, raft.ConfChangeAddVoter, m.To)
		if !ok {
			return &sim.Violation{
				Checker: o.name,
				Detail: fmt.Sprintf(
					"range %d: the move of node %d to node %d committed the REMOVAL of %d at index %d "+
						"with no committed entry ever making %d a voter. The range lost a replica and "+
						"gained nothing, so its quorum was voluntarily reduced",
					m.Range, m.From, m.To, m.From, removed, m.To),
			}
		}
		if added > removed {
			return &sim.Violation{
				Checker: o.name,
				Detail: fmt.Sprintf(
					"range %d: the move of node %d to node %d removed %d at index %d and only made %d a "+
						"voter at index %d. Between the two the range was one replica short of where it "+
						"started, which is exactly the window a move exists to avoid",
					m.Range, m.From, m.To, m.From, removed, m.To, added),
			}
		}
	}
	return nil
}

// --- snapshot equivalence -----------------------------------------------------

// StateAt is supplied by the harness: given the committed entries through some
// index, in index order, it returns a digest of the state machine they produce.
//
// base is the state the range was born holding: its extent and its keys. Both
// halves are needed, because a range born from a split inherits keys, and
// because a split entry applies only against the exact extent it names -- so the
// model has to track the extent forward to judge which splits took effect.
//
// It is injected rather than implemented here for one reason and it matters: the
// harness re-implements what a command DOES, so a defect in applying commands
// cannot cancel out on both sides of the comparison. What it shares with the
// system is the wire format, which is not the thing under test.
type StateAt func(base []byte) StateCursor

// StateCursor replays one range's committed log incrementally.
//
// # Why a cursor and not a function
//
// The first shape was `func(base, entries) uint64`, rebuilt from the birth
// payload for every snapshot. A range takes a snapshot every few applied
// entries, so that is quadratic in the log -- fine when a command was a put, and
// measured at A6 as the difference between 0.35 seconds a seed and 5.2, with a
// 2,000-seed sweep failing to finish inside two hours.
//
// A cursor advances through the prefix once. Nothing about the CHECK changes:
// each snapshot is still compared against the state its own committed prefix
// produces. Snapshots are fed in index order, because a cursor only moves
// forward.
type StateCursor interface {
	// DigestThrough advances to cover prefix and returns the state's digest.
	DigestThrough(prefix []raft.Entry) uint64
}

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

	// cursors is one incremental replay per range, and digests memoises the
	// answer per (range, index) so several replicas snapshotting one index cost
	// one replay between them.
	cursors map[uint64]StateCursor
	digests map[snapKey]uint64

	// done remembers what has already been verified. A snapshot's contents do
	// not change once recorded, so re-deriving the log's state at its index on
	// every subsequent event is pure cost.
	done map[snapKey]bool
}

type snapKey struct {
	rangeID uint64
	node    int
	index   raft.Index
	digest  uint64
}

// NewSnapshotEquivalence builds the oracle.
func NewSnapshotEquivalence(l *Ledger, state StateAt) *SnapshotEquivalence {
	return &SnapshotEquivalence{base: base{l: l, name: "snapshot-equivalence"}, state: state}
}

func (o *SnapshotEquivalence) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if o.state == nil || !o.stale() {
		return nil
	}
	return o.eachRange(o.check)
}

func (o *SnapshotEquivalence) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	if o.done == nil {
		o.done = map[snapKey]bool{}
	}
	if o.cursors == nil {
		o.cursors = map[uint64]StateCursor{}
	}
	if o.digests == nil {
		o.digests = map[snapKey]uint64{}
	}
	// Index order, because the cursor only moves forward. The ledger records a
	// snapshot when it first sees one, and "first seen" is a property of the
	// schedule rather than of the log.
	snaps := append([]snapRecord(nil), l.snaps...)
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].rec.Index < snaps[j].rec.Index })
	for _, s := range snaps {
		k := snapKey{rangeID: rangeID, node: s.node, index: s.rec.Index, digest: s.rec.Digest}
		if o.done[k] {
			continue
		}
		prefix, ok := l.committedPrefix(s.rec.Index)
		if !ok {
			// The ledger has not witnessed every committed entry under this
			// snapshot yet, so there is nothing to compare against. Skipped
			// rather than passed: the run is not asserting anything here.
			continue
		}
		// One replay per (range, index): several replicas snapshot the same
		// index, and the state a prefix produces does not depend on who asked.
		ik := snapKey{rangeID: rangeID, index: s.rec.Index}
		got, seen := o.digests[ik]
		if !seen {
			cur := o.cursors[rangeID]
			if cur == nil {
				cur = o.state(l.base)
				o.cursors[rangeID] = cur
			}
			got = cur.DigestThrough(prefix)
			o.digests[ik] = got
		}
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

// --- one server at a time -----------------------------------------------------

// SingleServerChange: every configuration change in the cluster moves exactly
// one server, with no joint transition.
//
// # Why this is the oracle A3 needs
//
// The entire safety of single-node membership changes is the overlapping-quorum
// argument (DESIGN-A3 §4), and that argument holds only while configurations
// differ by at most one server. A change carrying two is not a smaller version
// of joint consensus, it is the case joint consensus exists for -- and it would
// leave two majorities that need not intersect, which is every Raft safety
// property at once.
//
// ProposeConfChange refuses such a change, so this is the outside confirmation
// that the refusal held: it reads the entries that actually reached logs and
// state machines, not the code path that was supposed to prevent them.
//
// It decodes with raft's own codec. That is the wire format rather than the
// semantics -- the oracle re-derives nothing about what a change MEANS -- and it
// can only make a run fail, which is the side of the provenance rule where a
// system-supplied fact is allowed.
type SingleServerChange struct{ base }

// NewSingleServerChange builds the oracle.
func NewSingleServerChange(l *Ledger) *SingleServerChange {
	return &SingleServerChange{base{l: l, name: "single-server-change"}}
}

func (o *SingleServerChange) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	return o.eachRange(o.check)
}

func (o *SingleServerChange) check(rangeID uint64, l *rangeLedger) *sim.Violation {
	for node := 0; node < l.nodes; node++ {
		for _, stream := range [][]raft.Entry{l.applied[node], l.durableLog[node]} {
			for _, e := range stream {
				if e.Type != raft.EntryConfChange {
					continue
				}
				cc, ok := raft.DecodeConfChange(e.Data)
				if !ok {
					return &sim.Violation{
						Checker: o.name,
						Detail: fmt.Sprintf(
							"node %d holds a configuration entry at index %d that does not decode; a "+
								"membership nobody can read is a membership nobody agrees on",
							node, e.Index),
					}
				}
				if cc.Transition != raft.ConfChangeSimple {
					return &sim.Violation{
						Checker: o.name,
						Detail: fmt.Sprintf(
							"node %d holds a %s configuration change at index %d. Joint transitions "+
								"were cut by Amendment A6, and the overlapping-quorum argument that "+
								"makes v1 safe does not cover them",
							node, cc.Transition, e.Index),
					}
				}
				if len(cc.Changes) != 1 {
					return &sim.Violation{
						Checker: o.name,
						Detail: fmt.Sprintf(
							"node %d holds a configuration change at index %d moving %d servers at "+
								"once. Two configurations differing by more than one server need not "+
								"have intersecting majorities, which is every safety property at once",
							node, e.Index, len(cc.Changes)),
					}
				}
			}
		}
	}
	return nil
}

// --- MVCC read correctness -----------------------------------------------------

// ReadExpectation is what the harness's own model says a read at a log position
// should have returned.
type ReadExpectation struct {
	Index   raft.Index
	Key     string
	At      hlc.Timestamp
	Value   string
	Found   bool
	Refused bool
}

// ReadsAt is supplied by the harness: given a range's birth state and its
// committed entries, what every read entry in that log should have answered.
//
// It is injected for the same reason StateAt is, and the reason is sharper here.
// The whole claim of A5 is "a read at a timestamp returns the version visible at
// that timestamp". If the oracle asked the store what was visible, it would be
// checking that the store agrees with itself. So the harness replays the
// committed log -- writes, collections and splits -- and answers the read from
// its own model, and the two answers are compared.
type ReadsAt func(base []byte, entries []raft.Entry) []ReadExpectation

// MVCCReadCorrectness: every answer a client got is the version visible at the
// timestamp it asked for.
//
// # Both directions, because one of them is how a store passes by refusing
//
// An answer must match the model's value. A REFUSAL must match the model's
// refusal: a read at or below the collection mark is unanswerable, and a store
// that refused everything would sail past a checker that only inspected wrong
// answers. The second direction is the one that makes this oracle worth having.
type MVCCReadCorrectness struct {
	base
	reads ReadsAt
}

func NewMVCCReadCorrectness(l *Ledger, reads ReadsAt) *MVCCReadCorrectness {
	return &MVCCReadCorrectness{base: base{l: l, name: "mvcc-read-correctness"}, reads: reads}
}

func (o *MVCCReadCorrectness) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	// One expectation table per range, keyed by log index.
	want := map[uint64]map[raft.Index]ReadExpectation{}
	for _, rl := range o.l.ranges {
		m := map[raft.Index]ReadExpectation{}
		for _, e := range o.reads(rl.base, rl.Committed()) {
			m[e.Index] = e
		}
		want[rl.id] = m
	}

	for _, got := range o.l.reads {
		// # An OFF-LOG answer is not this oracle's business, and comparing one
		// # here would manufacture violations
		//
		// This table is keyed by the LOG INDEX OF A READ ENTRY. A read served by
		// read index (A7) has no entry: its Index is the CONFIRMED read index,
		// which is an unrelated position that may well hold some other read's
		// entry. Comparing an off-log answer for one key against the expectation
		// recorded for a different read at that index is a false violation
		// waiting for a coincidence, which is BUG-016's standard.
		//
		// They are checked by `read-index-answers-match-the-log`, which replays
		// the committed prefix TO that index rather than looking up an entry at
		// it.
		if got.OffLog {
			continue
		}
		exp, ok := want[got.Range][got.Index]
		if !ok {
			// The harness has no expectation for this position, which means the
			// entry is not in the committed set the ledger observed. That is a
			// timing artefact of when the oracle runs, not a verdict.
			continue
		}
		switch {
		case got.Refused && !exp.Refused:
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d index %d: node %d refused a read of %q at %s, and the committed log says "+
					"that timestamp is above the collection mark. A refusal the history does not "+
					"justify is a store answering by declining",
				got.Range, got.Index, got.Node, got.Key, got.At)}
		case !got.Refused && exp.Refused:
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d index %d: node %d answered a read of %q at %s with %q, and the committed "+
					"log says the versions visible there were collected. That answer is a state the "+
					"database no longer holds, and it is indistinguishable from a correct one",
				got.Range, got.Index, got.Node, got.Key, got.At, got.Value)}
		case got.Refused:
			// Both refused, and for the same reason.
		case got.Found != exp.Found || got.Value != exp.Value:
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d index %d: node %d read %q at %s and got (%q, found=%v); the committed log "+
					"says the version visible at that timestamp is (%q, found=%v)",
				got.Range, got.Index, got.Node, got.Key, got.At, got.Value, got.Found,
				exp.Value, exp.Found)}
		}
	}
	return nil
}

// --- transaction atomicity -----------------------------------------------------

// CommitFact is what the harness's model found in one range's committed log
// about one transaction's key.
type CommitFact struct {
	Key      string
	StartTS  hlc.Timestamp
	CommitTS hlc.Timestamp
	Rollback bool
}

// TxnFactsAt is supplied by the harness: given a range's birth state and its
// committed entries, every commit or rollback record the log produces, and every
// transaction record it decides.
//
// Injected for the reason every model function here is: the harness restates
// what a prewrite and a commit DO, so a defect in applying them cannot cancel
// out on both sides of the comparison. What crosses the boundary is the wire
// format, which is not the thing under test.
type TxnFactsAt func(base []byte, entries []raft.Entry) (commits []CommitFact, decided map[string]CommitFact)

// TransactionAtomicity: a transaction's keys all move to one commit timestamp,
// or none does.
//
// # The commit point is the whole check
//
// DESIGN-A6 D-A6-3: *a transaction is committed if and only if the write record
// for its PRIMARY key exists.* So the oracle asks two questions of the committed
// logs, and takes nothing from any coordinator:
//
//  1. If the primary is committed at C, then every key the transaction wrote is
//     either committed at C or still locked -- and a key committed at anything
//     other than C is atomicity broken, because a reader between the two
//     timestamps sees half the transaction.
//  2. If the primary is rolled back, no key may be committed at all.
//
// A key still locked is NOT a violation. It is a transaction whose bookkeeping
// is unfinished, which is exactly the state resolution exists to clean up, and
// calling it a violation would make every crashed coordinator a safety failure
// instead of a recovery case.
type TransactionAtomicity struct {
	base
	facts TxnFactsAt
}

func NewTransactionAtomicity(l *Ledger, facts TxnFactsAt) *TransactionAtomicity {
	return &TransactionAtomicity{base: base{l: l, name: "transaction-atomicity"}, facts: facts}
}

func (o *TransactionAtomicity) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	if !o.stale() {
		return nil
	}
	// Gather every commit fact and every decision across all ranges. A
	// transaction's keys can live on any of them, and after a split they can
	// have moved -- which is why this is assembled per run rather than per
	// range.
	byKey := map[string][]CommitFact{}
	decided := map[string]CommitFact{}
	for _, rl := range o.l.ranges {
		commits, dec := o.facts(rl.base, rl.Committed())
		for _, c := range commits {
			byKey[c.Key] = append(byKey[c.Key], c)
		}
		for k, v := range dec {
			decided[k] = v
		}
	}

	for _, t := range o.l.txns {
		d, ok := decided[txnDecisionKey(t.Primary, t.StartTS)]
		if !ok {
			// Undecided: the coordinator died before the primary's record
			// landed, and nobody has resolved it yet. Locks may be outstanding
			// and no key may be committed at this start timestamp.
			for _, k := range t.Keys {
				for _, c := range byKey[k] {
					if c.StartTS == t.StartTS && !c.Rollback {
						return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
							"transaction %d (start %s) has no decision on its primary %q, and key %q "+
								"is committed at %s. A key committed for a transaction nobody has "+
								"decided is a write that appeared without a commit point",
							t.ID, t.StartTS, t.Primary, k, c.CommitTS)}
					}
				}
			}
			continue
		}

		if d.Rollback {
			for _, k := range t.Keys {
				for _, c := range byKey[k] {
					if c.StartTS == t.StartTS && !c.Rollback {
						return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
							"transaction %d (start %s) is ROLLED BACK on its primary %q, and key %q "+
								"is committed at %s. Half of an aborted transaction is visible",
							t.ID, t.StartTS, t.Primary, k, c.CommitTS)}
					}
				}
			}
			continue
		}

		// Committed at d.CommitTS. Every key is at that timestamp or nowhere.
		for _, k := range t.Keys {
			for _, c := range byKey[k] {
				if c.StartTS != t.StartTS || c.Rollback {
					continue
				}
				if c.CommitTS != d.CommitTS {
					return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
						"transaction %d is committed at %s on its primary %q and at %s on key %q. A "+
							"reader between those two timestamps sees half of it",
						t.ID, d.CommitTS, t.Primary, c.CommitTS, k)}
				}
			}
		}
	}
	return nil
}

// txnDecisionKey is how a decision is looked up: primary key AND start
// timestamp, because a key is the primary of many transactions over its life and
// the first draft of the record layout forgot that (kv/encoding.go).
func txnDecisionKey(primary string, startTS hlc.Timestamp) string {
	return primary + "@" + startTS.String()
}

// TxnDecisionKey is the harness's spelling of the same thing.
func TxnDecisionKey(primary string, startTS hlc.Timestamp) string {
	return txnDecisionKey(primary, startTS)
}

// --- Percolator invariants over the recovered state -----------------------------

// RecoveredState is one range's final state machine, decoded into the four
// record kinds. The harness produces it by REPLAYING the committed log through
// the real apply path; this oracle only inspects the result.
type RecoveredState struct {
	Range  uint64
	Start  []byte
	End    []byte
	GCMark hlc.Timestamp

	Locks    []RecoveredLock
	Writes   []CommitFact
	Decided  []CommitFact
	Versions []RecoveredVersion

	// Clock is the range's HLC at the end of the run. Reported by the node, and
	// that is deliberate and safe: the invariant below compares it against the
	// range's OWN versions, which are recovered independently, so a node lying
	// about its clock makes the check FAIL rather than pass. A reported value
	// that can only produce a red verdict is not the provenance rule's concern
	// (DESIGN-A1 §0).
	Clock hlc.Timestamp
}

// RecoveredLock is a lock found in the final state.
type RecoveredLock struct {
	Key      string
	Primary  string
	StartTS  hlc.Timestamp
	Deadline hlc.Timestamp
}

// RecoveredVersion is a data version found in the final state.
type RecoveredVersion struct {
	Key string
	At  hlc.Timestamp
}

// RecoveredAt is supplied by the harness: every range's final state.
type RecoveredAt func() []RecoveredState

// PercolatorInvariants: seven properties of the FINAL state, each checkable by
// inspection.
//
// # Why this exists, and why it is not the model that was removed
//
// Snapshot equivalence used to judge the state machine against an independent
// MODEL. At A6 that model is a second implementation of Percolator, and one
// produced five defects of its own in a single sitting, so it was replaced by an
// independent EXECUTION (store.ReplayMachine). That trade gives up exactly one
// property: **an apply path wrong the same way on every replica**. A cluster
// that mishandles lock expiry identically everywhere agrees with itself, replays
// consistently, and satisfies snapshot isolation and bank conservation for as
// long as the error stays symmetric.
//
// This oracle is one of the two replacements (the other is the symmetric-apply
// mutant classes). The distinction that makes it legitimate where the model was
// not: **it asserts properties of the result, not the derivation.** It never
// says what the state should be; it says what no correct state can look like,
// whatever produced it. Four assertions, no reimplementation.
type PercolatorInvariants struct {
	base
	recovered RecoveredAt
}

func NewPercolatorInvariants(l *Ledger, recovered RecoveredAt) *PercolatorInvariants {
	return &PercolatorInvariants{base: base{l: l, name: "percolator-invariants"}, recovered: recovered}
}

// Interested returns false for every kind: these are properties of the FINAL
// state, and evaluating them mid-run is both meaningless and ruinously
// expensive.
//
// # Both halves of that sentence are load-bearing
//
// Meaningless, because "a committed transaction leaves no lock" is eventual and
// a run caught mid-cleanup is not a failure -- see invariant 1's own comment.
// The instantaneous forms are true at every step, but checking them at every
// step buys nothing a final check does not.
//
// Ruinously expensive, because each evaluation REPLAYS every range's entire
// committed log through the real apply path. Called per step that changed the
// ledger, that is quadratic in the log, and it took the A6 sweep from half a
// second a seed to five: measured, 2,000 seeds did not finish inside two hours.
//
// Returning false here means the loop never calls OnStep, and Check is what the
// harness calls once at the end.
func (o *PercolatorInvariants) Interested(sim.Kind) bool { return false }

// OnStep is never called: see Interested.
func (o *PercolatorInvariants) OnStep(sim.View, sim.Event) *sim.Violation { return nil }

// Check evaluates the invariants once, over the final recovered state.
func (o *PercolatorInvariants) Check() *sim.Violation {
	states := o.recovered()

	// # Invariant 6: no range's clock sits below a version it holds
	//
	// BUG-023's invariant, stated directly. A range that can stamp a read below
	// data it already holds will hide a completed write, and MVCC will be right
	// to hide it — the answer is correct for the timestamp it was asked at, and
	// the timestamp is what is wrong.
	//
	// It is here rather than in the linearizability checker because it is a
	// property of the STATE, checkable by inspection, and it fires whether or not
	// any client happened to observe the stale read. BUG-023 was found by
	// porcupine, which needs a client to have watched; this needs nobody to have
	// been looking.
	for _, st := range states {
		if !st.Clock.IsSet() {
			continue
		}
		for _, v := range st.Versions {
			if st.Clock.Less(v.At) {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d holds a version of %q at %s and its clock is at %s, BELOW it. The "+
						"next read this range stamps can land under data it already has, and the "+
						"write will be invisible at a timestamp that is correct for the question "+
						"asked (BUG-023)", st.Range, v.Key, v.At, st.Clock)}
			}
		}
		for _, w := range st.Writes {
			if st.Clock.Less(w.CommitTS) {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d holds a commit record for %q at %s and its clock is at %s, BELOW it "+
						"(BUG-023)", st.Range, w.Key, w.CommitTS, st.Clock)}
			}
		}
	}

	// Decisions are cluster-wide: a lock's primary can be on any range, and
	// after a split it is on a different one from where it started.
	decided := map[string]CommitFact{}
	for _, st := range states {
		for _, d := range st.Decided {
			decided[txnDecisionKey(d.Key, d.StartTS)] = d
		}
	}
	committedAt := map[string]bool{}
	for _, d := range decided {
		if !d.Rollback {
			committedAt[d.StartTS.String()+"->"+d.CommitTS.String()] = true
		}
	}

	for _, st := range states {
		// 1. A key is never both COMMITTED and LOCKED for one transaction.
		//
		// CommitInto writes the record and drops the lock in ONE batch, so the
		// two can never coexist for the same (key, start timestamp). A lock left
		// beside its own commit record is a commit that landed without clearing
		// it -- and the key is then unreadable forever, because every reader
		// blocks on a lock whose transaction is already decided.
		//
		// # Why not "a committed transaction leaves no lock anywhere"
		//
		// That is the property, and it is EVENTUAL: a committed transaction's
		// secondary commit steps may still be in flight, and a resolver will
		// finish them. A finite run that stops in the middle of that is not a
		// failure -- it is the state resolution exists for, and the atomicity
		// oracle says the same thing about the same shape. Asserting the
		// eventual form at the end of a run would make every crashed coordinator
		// a safety violation.
		//
		// What IS instantaneously true is the pair above, and that is what a
		// checker may say.
		locked := map[string]hlc.Timestamp{}
		for _, l := range st.Locks {
			locked[l.Key] = l.StartTS
		}
		for _, w := range st.Writes {
			if w.Rollback {
				continue
			}
			if st, ok := locked[w.Key]; ok && st == w.StartTS {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"%q is committed at %s for the transaction that started at %s, and still holds "+
						"that transaction's lock. A commit clears its own lock in the same batch, so "+
						"the two cannot coexist -- and a reader blocks forever on a lock whose "+
						"transaction is already decided",
					w.Key, w.CommitTS, w.StartTS)}
			}
		}

		for _, l := range st.Locks {

			// 4. Every lock is resolvable: some range covers its primary key.
			//
			// A lock naming a primary no range holds can never be decided by
			// anybody, which is the orphan a split would create if a lock named
			// a range instead of a key (D-A6-1).
			if !coversKey(states, []byte(l.Primary)) {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d: %q is locked by a transaction whose primary %q is covered by no "+
						"range. Nothing can ever read that decision, so the lock is permanent",
					st.Range, l.Key, l.Primary)}
			}
		}

		// 2. A commit record implies a committed transaction record.
		//
		// A write record says "the version at this start timestamp is committed
		// here". If no transaction record agrees, a value is visible that no
		// commit point ever authorised -- which is the read half of atomicity
		// broken, and it is invisible to a checker that only compares replicas
		// with each other because every replica shows the same thing.
		for _, w := range st.Writes {
			if w.Rollback {
				continue
			}
			if !committedAt[w.StartTS.String()+"->"+w.CommitTS.String()] {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d: %q is committed at %s for the transaction that started at %s, and no "+
						"transaction record anywhere says that transaction committed at that "+
						"timestamp. A value is visible that no commit point authorised",
					st.Range, w.Key, w.CommitTS, w.StartTS)}
			}
		}

		// 5. A rolled-back version does not exist.
		//
		// # This invariant was added because a mutant survived
		//
		// M61 leaves the uncommitted data version behind when a transaction
		// rolls back. It is SYMMETRIC -- every replica does it -- so replay
		// equivalence cannot see it: the replay runs the same apply path and
		// leaves the same version. It is invisible to clients too, because no
		// commit record points at the version and the read path walks past the
		// tombstone. The mutant survived its covering test, which is the
		// surrendered property made visible rather than argued about.
		//
		// The property that catches it is not a comparison at all: a rollback
		// tombstone says this start timestamp is dead, and a dead version is
		// unreachable storage that grows without bound. No correct final state
		// contains one.
		rolledBack := map[string]bool{}
		for _, w := range st.Writes {
			if w.Rollback {
				rolledBack[w.Key+"@"+w.StartTS.String()] = true
			}
		}
		for _, v := range st.Versions {
			if rolledBack[v.Key+"@"+v.At.String()] {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d: %q has a version at %s whose transaction was ROLLED BACK. No commit "+
						"record will ever point at it, so it is unreachable storage -- invisible to "+
						"every client and to every checker that compares replicas, because they all "+
						"hold the same dead byte",
					st.Range, v.Key, v.At)}
			}
		}

		// 7. Over the TRANSACTIONAL keyspace, every data version is accounted
		//    for: a lock names it, or a write record does.
		//
		// # The domain is the invariant, not a filter on it
		//
		// The premise is *a version exists because a prewrite wrote it, and a
		// prewrite writes the version and the lock in one batch*. That premise
		// is what the invariant is derived from, and it is true of exactly the
		// keys the transaction protocol touches. The plain workload writes
		// versions through `PutInto` with no lock and no write record — A5's
		// non-transactional MVCC, correct, and a different thing — so those keys
		// are **outside the domain rather than exceptions inside it**.
		//
		// The domain is stated the only way the recovered state can state it: a
		// key carrying at least one write record or lock has been through the
		// protocol; a key with neither has never been prewritten.
		//
		// The unscoped form was written first and fired on seed 0, on `k06` —
		// the invariant finding its own domain before anybody believed it.
		//
		// # What it says, and why nothing else can say it
		//
		// So at every instant a version has a lock naming its start timestamp,
		// or the transaction was decided and a write record names it — a commit
		// record or a rollback tombstone. **A version with neither is an
		// orphan**: nobody will resolve it, because there is no lock; nobody
		// will read it, because there is no commit record.
		//
		// Every replica holds the same orphan, so replay equivalence agrees with
		// itself. No client observed the write, so porcupine has nothing to
		// compare. The bank notices only if the missing money lands inside an
		// audit's window. That is the shape §13.4 surrendered when the
		// independent model was retired, and it is the second time a structural
		// property has been the answer to it rather than a tuned test —
		// invariant 5 was the first.
		//
		// # What the domain costs
		//
		// An orphan on a key that has never carried a write record or a lock is
		// outside it. That is a real limit and it is stated rather than argued
		// away: the invariant covers versions the protocol produced, and a key
		// the protocol never touched has no version it produced.
		accounted := map[string]bool{}
		transactional := map[string]bool{}
		for _, w := range st.Writes {
			accounted[w.Key+"@"+w.StartTS.String()] = true
			transactional[w.Key] = true
		}
		for _, lk := range st.Locks {
			accounted[lk.Key+"@"+lk.StartTS.String()] = true
			transactional[lk.Key] = true
		}
		for _, v := range st.Versions {
			if !transactional[v.Key] || accounted[v.Key+"@"+v.At.String()] {
				continue
			}
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d: %q has a version at %s with NO lock naming it and NO write record "+
					"naming it. A prewrite writes the version and the lock in one batch, so a "+
					"version with neither is an orphan: nobody will resolve it because there is "+
					"no lock, and nobody will read it because there is no commit record. Every "+
					"replica holds the same orphan, so nothing that compares replicas can see it "+
					"(BUG-019's class)",
				st.Range, v.Key, v.At)}
		}

		// 3. At most one version at or below the collection mark, per key.
		//
		// Not "no version below the mark": collection deliberately KEEPS the
		// newest version at or below it, because that is the one a read at the
		// mark's successor needs. Two of them means collection did not run where
		// it said it had, and the mark is a claim about history nobody enforced.
		below := map[string]int{}
		for _, v := range st.Versions {
			if v.At.LessEq(st.GCMark) {
				below[v.Key]++
				if below[v.Key] > 1 {
					return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
						"range %d: %q has %d versions at or below the collection mark %s. Collection "+
							"keeps exactly one -- the version a read just above the mark needs -- so "+
							"more than one means the mark moved over history it never collected",
						st.Range, v.Key, below[v.Key], st.GCMark)}
				}
			}
		}
	}
	return nil
}

// coversKey reports whether any range's extent contains key.
func coversKey(states []RecoveredState, key []byte) bool {
	for _, st := range states {
		if bytes.Compare(key, st.Start) >= 0 && (len(st.End) == 0 || bytes.Compare(key, st.End) < 0) {
			return true
		}
	}
	return false
}

// --- The A6 oracles over client-observed history --------------------------------

// BankConservation asserts that every audit summed to the same total.
//
// # Where the evidence comes from, and where it deliberately does not
//
// Audits, and nothing else. Not the engine, not the recovered records, not the
// coordinator's arithmetic: a client asked every account what it held at one
// timestamp, the cluster answered, and those answers must sum to what they
// summed to at the beginning.
//
// # Why the expected total is zero and there is no genesis transaction
//
// Every account starts absent, an absent account holds nothing, and a transfer
// moves an amount from one to another. So the sum is zero before the first
// transfer and after every one of them, and a bank whose accounts can go
// negative is still a bank -- it is a ledger, and conservation is the property
// under test rather than solvency.
//
// A genesis transaction would add a startup ordering problem (no leader exists
// at time zero) and would make the invariant depend on the genesis having
// committed, which is a second thing to prove before the first can be checked.
type BankConservation struct {
	base
	accounts int
}

func NewBankConservation(l *Ledger, accounts int) *BankConservation {
	return &BankConservation{base: base{l: l, name: "bank-conservation"}, accounts: accounts}
}

func (o *BankConservation) Interested(sim.Kind) bool                  { return false }
func (o *BankConservation) OnStep(sim.View, sim.Event) *sim.Violation { return nil }

// Check is evaluated once, after the run.
func (o *BankConservation) Check() *sim.Violation {
	for _, a := range o.l.Audits() {
		// "Complete" is a claim the harness makes, so the checker verifies it.
		// An audit over five of eight accounts that summed to zero would
		// otherwise be indistinguishable from evidence.
		if a.Accounts != o.accounts {
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"the audit at %s reached the checker claiming to be complete with %d of %d "+
					"accounts. A sum over a subset conserves nothing, and a partial audit is "+
					"supposed to be discarded before it is ever recorded",
				a.ReadTS, a.Accounts, o.accounts)}
		}
		if a.Total != 0 {
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"the audit at %s read all %d accounts and they sum to %d, not 0. Every transfer "+
					"takes an amount from one account and gives it to another, so a snapshot in "+
					"which the total has moved has either lost half a transaction or applied half "+
					"of one twice",
				a.ReadTS, a.Accounts, a.Total)}
		}
	}
	return nil
}

// SnapshotIsolation asserts the two properties a client can actually observe.
//
// # 1. A snapshot is stable
//
// The same key, read at the same timestamp, answers the same thing forever. Not
// "usually", not "once the transaction finishes": a read at T is a question
// about a fixed instant, and its answer cannot depend on when it was asked.
//
// This is the strongest thing a client-side oracle can say about snapshot
// isolation, and it catches the failure that matters most: a transaction
// COMMITTING INTO THE PAST. If a transaction whose snapshot began before T is
// allowed to commit at a timestamp at or below T, then a read of one of its keys
// before the commit sees the old value and a read of the same key at the same T
// afterwards sees the new one -- and a reader that took its snapshot at T sees
// half of it. No amount of care in the write path prevents that; only the
// commit timestamp being above every timestamp already read does.
//
// # 2. A read only ever blocks on a lock it could have seen
//
// A read at T reports a lock only if that lock's transaction began at or below
// T. A read blocked by a lock ABOVE its timestamp is a read being made to wait
// for a transaction that is not in its snapshot at all, which is a liveness bug
// today and an isolation bug the moment the resolution decides anything.
//
// # What is deliberately NOT asserted here
//
// That a read sees every transaction committed below its timestamp. The harness
// knows which transactions it issued, but not which of them committed -- that is
// read from the logs, by transaction-atomicity, and importing it here would put
// the two oracles one derivation apart (DESIGN-A1 section 0).
type SnapshotIsolation struct {
	base

	// compared is how many times a settled answer was checked against an
	// earlier settled answer to the same question. A run where it is zero
	// asserted stability over nothing, which is why the sweep reports it and
	// the exit run requires it to be nonzero.
	compared int
}

func NewSnapshotIsolation(l *Ledger) *SnapshotIsolation {
	return &SnapshotIsolation{base: base{l: l, name: "snapshot-isolation"}}
}

// Compared is the oracle's non-vacuity evidence.
func (o *SnapshotIsolation) Compared() int { return o.compared }

func (o *SnapshotIsolation) Interested(sim.Kind) bool                  { return false }
func (o *SnapshotIsolation) OnStep(sim.View, sim.Event) *sim.Violation { return nil }

func (o *SnapshotIsolation) Check() *sim.Violation {
	type answer struct {
		r TxnReadRecord
	}
	seen := map[string]answer{}
	for _, r := range o.l.TxnReads() {
		if r.Locked {
			if r.At.Less(r.LockStartTS) {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"the read of %q at %s was blocked by a lock whose transaction began at %s, "+
						"ABOVE the read's own timestamp. That transaction is not in this read's "+
						"snapshot, so waiting for it is waiting for something the read is not "+
						"allowed to see (node %d, index %d)",
					r.Key, r.At, r.LockStartTS, r.Node, r.Index)}
			}
			if r.LockPrimary == "" {
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"the read of %q at %s was blocked by a lock naming no primary. A lock whose "+
						"primary is unknown can never be resolved by anyone, so the key is blocked "+
						"forever (node %d, index %d)", r.Key, r.At, r.Node, r.Index)}
			}
			continue
		}
		// Only settled answers are comparable. Locked, uncertain and refused are
		// each legitimately time-varying: a lock gets decided, a commit inside
		// the interval stops being ahead as the clock moves, and the collection
		// mark only travels forward.
		if r.Uncertain || r.Refused {
			continue
		}
		k := r.Key + "@" + r.At.String()
		prev, ok := seen[k]
		if !ok {
			seen[k] = answer{r: r}
			continue
		}
		o.compared++
		if prev.r.Found == r.Found && prev.r.Value == r.Value {
			continue
		}
		return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
			"the key %q read at %s answered %s at index %d on node %d and %s at index %d on "+
				"node %d. A snapshot is a question about a fixed instant and its answer cannot "+
				"change: something committed at or below %s AFTER that timestamp had already been "+
				"read, so a reader at %s sees half of it",
			r.Key, r.At, describeRead(prev.r), prev.r.Index, prev.r.Node,
			describeRead(r), r.Index, r.Node, r.At, r.At)}
	}
	return nil
}

func describeRead(r TxnReadRecord) string {
	if !r.Found {
		return "absent"
	}
	return strconv.Quote(r.Value)
}

// UncertaintyEnvelope judges the uncertainty machinery against the bound the
// PLAN advertises.
//
// # Why the bound comes from the plan and never from a node
//
// The node's advertised maxOffset is the thing under test. A checker that took
// the bound from the node it is checking would agree with any bound the node
// chose, including a bound it had drifted to, and would be unable to notice the
// one failure that matters: an interval computed against something other than
// what the cluster agreed to assume. The plan is the only place the assumption
// exists independently of the code (DESIGN-A1 section 0).
//
// # The two properties
//
//  1. Every restart names a commit strictly inside (readTS, readTS+maxOffset].
//     Above that window there is no uncertainty -- the commit is plainly in the
//     future and the older value is correct. At or below it the commit is
//     plainly in the past and should have been READ, not restarted on.
//  2. The restart timestamp is strictly above the commit that caused it. Not
//     readTS+maxOffset, not now: CLAUDE.md's sharp-edge list names this one, and
//     restarting anywhere at or below the observed commit restarts into the same
//     uncertainty.
type UncertaintyEnvelope struct {
	base
	maxOffset time.Duration
}

func NewUncertaintyEnvelope(l *Ledger, planMaxOffset time.Duration) *UncertaintyEnvelope {
	return &UncertaintyEnvelope{
		base: base{l: l, name: "uncertainty-envelope"}, maxOffset: planMaxOffset}
}

func (o *UncertaintyEnvelope) Interested(sim.Kind) bool                  { return false }
func (o *UncertaintyEnvelope) OnStep(sim.View, sim.Event) *sim.Violation { return nil }

func (o *UncertaintyEnvelope) Check() *sim.Violation {
	for _, r := range o.l.TxnReads() {
		// # Property zero: the interval the node used is inside the bound the
		// # PLAN advertises
		//
		// Checked on every read, not only the ones that restarted, because a
		// ceiling computed against the wrong bound is wrong on the reads it
		// silently let through as well as on the ones it stopped. A ceiling
		// BELOW the read's own timestamp is legal -- a transaction that has
		// restarted past its original ceiling has an empty interval, which is
		// exactly the shrinkage that makes restarts terminate.
		// # The arithmetic is written out here, not borrowed from kv
		//
		// `kv.UncertaintyCeiling` is the function under test. An oracle that
		// called it would agree with the store by construction, including when
		// both are wrong -- which is the "a defect cannot cancel out on both
		// sides" rule that DESIGN-A1 section 0 is built on, and the reason
		// snapshot equivalence stopped using a model. Two lines of addition is
		// the whole cost of independence here.
		top := hlc.Timestamp{Wall: r.At.Wall.Add(o.maxOffset)}
		if r.Ceiling.IsSet() && top.Less(r.Ceiling) {
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"the read of %q at %s used an uncertainty ceiling of %s, above the %s the plan's "+
					"maxOffset of %s allows. The interval was computed against a bound the cluster "+
					"did not agree to assume (node %d, index %d)",
				r.Key, r.At, r.Ceiling, top, o.maxOffset, r.Node, r.Index)}
		}
		if !r.Uncertain {
			continue
		}
		if r.Ceiling.IsSet() {
			top = r.Ceiling
		}
		switch {
		case !r.At.Less(r.CommitTS):
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"the read of %q at %s restarted on a commit at %s, which is at or BELOW its own "+
					"timestamp. A commit below the read is in its snapshot and is the answer, not "+
					"an uncertainty (node %d, index %d)", r.Key, r.At, r.CommitTS, r.Node, r.Index)}
		case top.Less(r.CommitTS):
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"the read of %q at %s restarted on a commit at %s, which is above its ceiling %s. "+
					"A commit above the interval is plainly in the future and the older value is "+
					"the correct answer, so restarting on it is a transaction giving up progress "+
					"for nothing (node %d, index %d)",
				r.Key, r.At, r.CommitTS, top, r.Node, r.Index)}
		case !r.CommitTS.Less(r.RestartAt):
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"the read of %q at %s restarted on a commit at %s and was told to retry at %s, "+
					"which is not above it. The retry lands in the same uncertainty and the "+
					"transaction makes no progress (node %d, index %d)",
				r.Key, r.At, r.CommitTS, r.RestartAt, r.Node, r.Index)}
		}
	}
	return nil
}

// --- resolution authority -------------------------------------------------------

// ResolveFact is one resolve command as it stands in a committed log: the
// transaction it names, the deadline the lock carried, and the timestamp the
// resolver chose to judge that deadline against.
//
// Both values ride in the command (store.TxnCommand.ExpireAt, .Deadline) and
// they ride there for D-A6-10's reason: every replica must compare the same two
// numbers rather than each consulting its own clock. That is what makes this
// oracle possible at all -- the permission a resolver claimed is written down in
// the log, so it can be read back and checked against what the resolve did.
type ResolveFact struct {
	Primary  string
	StartTS  hlc.Timestamp
	Deadline hlc.Timestamp
	ExpireAt hlc.Timestamp
}

// ProposedRollback is one EXPLICITLY proposed rolled-back transaction record --
// a coordinator abandoning its own transaction, or anybody else writing the
// record directly rather than through a resolve.
//
// It is recorded because it is the other way a rolled-back record can come to
// exist, and an oracle that did not know about it would accuse a coordinator of
// aborting itself.
type ProposedRollback struct {
	Primary string
	StartTS hlc.Timestamp
}

// ResolutionsAt is supplied by the harness: the two command shapes above, as
// they appear in one range's committed log.
//
// It DECODES; it does not model. It reports what the log says and takes no view
// on what any of it did -- which is the distinction that keeps this oracle out
// of the trap the removed model fell into. A supplier that decided which
// resolves declared an owner dead would be re-running the rule this oracle
// exists to check, and would cancel out against a defect in it.
type ResolutionsAt func(entries []raft.Entry) ([]ResolveFact, []ProposedRollback)

// ResolutionAuthority: a resolve that declares an owner dead had the right to.
//
// # The class this exists for, and the measurement that made it a decision
//
// DESIGN-A6 §13.4 surrendered one property when the independent model was
// retired: an apply path wrong the SAME way on every replica. `M62` is that
// property realised and measured. It makes lock expiry always fall through, so
// every resolver rolls back every lock it meets, live or not. Applied to the
// tree it moves 33 census fields and `TxnLostToResolver` goes 0 -> 2 -- live
// coordinators losing their transactions to a resolver with no right to kill
// them -- and nothing said anything: 0 of 300, 0 of 100, sweepfail 0.
//
// The reason nothing said anything is not omission. **Aborting a transaction is
// a legal outcome.** Atomicity holds, snapshot isolation holds, the bank
// conserves, every replica agrees, and the replay agrees with all of them. Every
// client-facing oracle is blind by construction.
//
// # What it asserts, and why it is not the production rule restated
//
// A rolled-back transaction record exists in the final state for exactly two
// reasons: somebody proposed it (`OpPutTxnRecord`), or a resolver declared the
// owner dead (`OpResolveStatus`). The first needs no permission -- a coordinator
// may abandon its own transaction. The second does, and the permission is
// D-A6-5's: **the TTL is expiry, not opinion**, so a resolver may only make an
// owner dead once the deadline the owner published has passed the timestamp the
// resolver judged it at.
//
// So: for every rolled-back record with no explicit proposal behind it, some
// resolve naming that transaction must carry `Deadline < ExpireAt`.
//
// This is not the production predicate re-run. Production uses the two numbers
// to DECIDE; this reads the decision out of the recovered state and the two
// numbers out of the log, and asks whether anything in the log authorised what
// the state shows. Under `M62` the decision happens and no command authorised
// it, which is the violation -- and no code is shared with `kv.ResolveLock` for
// the verdict to cancel out against.
//
// # What it does not say
//
// It does not say a resolve was NEEDED, or that waiting would have been better,
// or that the owner was in fact alive. A transaction whose coordinator died the
// instant before its deadline is legitimately killed and this is silent about
// it. The only thing refused is a killing nothing in the log gave permission for.
type ResolutionAuthority struct {
	base
	cmds      ResolutionsAt
	recovered RecoveredAt

	// declarations is how many rolled-back records this oracle attributed to a
	// resolver's declaration rather than to an explicit proposal.
	//
	// Recorded because a green verdict over nothing is the register's most
	// common entry: a run in which every rollback was self-proposed exercises
	// none of this, and a sweep of such runs would report silence that means
	// only that resolution never fired.
	declarations int
}

// NewResolutionAuthority builds the oracle.
func NewResolutionAuthority(l *Ledger, cmds ResolutionsAt, recovered RecoveredAt) *ResolutionAuthority {
	return &ResolutionAuthority{
		base: base{l: l, name: "resolution-only-breaks-expired-locks"},
		cmds: cmds, recovered: recovered,
	}
}

// Declarations is how many rolled-back records were attributed to a resolver.
// It is the oracle's non-vacuity witness: zero means the run never resolved
// anything and the silence says nothing.
func (o *ResolutionAuthority) Declarations() int { return o.declarations }

// Interested returns false: this is a property of the final state read against
// the whole committed log, and it is evaluated once, like the other A6 oracles.
func (o *ResolutionAuthority) Interested(sim.Kind) bool { return false }

// OnStep is never called: see Interested.
func (o *ResolutionAuthority) OnStep(sim.View, sim.Event) *sim.Violation { return nil }

// Check evaluates the invariant once, over the final recovered state and every
// range's committed log.
func (o *ResolutionAuthority) Check() *sim.Violation {
	// Every resolve and every explicit proposal, cluster-wide. Cluster-wide
	// because a transaction's primary can be on any range and a split moves it:
	// the command that authorised a rollback may sit in the PARENT's log while
	// the record it produced is in the child's inherited state.
	//
	// # Why "no command accounts for this record" is a sound thing to say
	//
	// It rests on the two halves coming from ONE list. The recovered state is
	// store.ReplayMachine over rl.Base() and rl.Committed(); this walk is over
	// rl.Committed() for every range. So a record in the final state was
	// produced either by a command in the very entries read here, or by one in
	// an ancestor's -- and every ancestor is in the ledger, because ranges are
	// born by splitting inside the run and the ledger records each one's base.
	//
	// That matters because the ledger does NOT promise a complete committed
	// prefix in general -- committedPrefix exists precisely to report when it
	// has not witnessed one -- and an oracle that asked "is this command
	// anywhere" against a partial log would accuse a correct run. Here the
	// question is closed: the state cannot contain what the log this walk reads
	// did not produce.
	authorised := map[string]ResolveFact{}
	attempted := map[string][]ResolveFact{}
	proposed := map[string]bool{}
	for _, rl := range o.l.Ranges() {
		rs, ps := o.cmds(rl.Committed())
		for _, r := range rs {
			k := txnDecisionKey(r.Primary, r.StartTS)
			attempted[k] = append(attempted[k], r)
			if r.Deadline.Less(r.ExpireAt) {
				authorised[k] = r
			}
		}
		for _, p := range ps {
			proposed[txnDecisionKey(p.Primary, p.StartTS)] = true
		}
	}

	// The states are walked in the harness's order and each range's records in
	// the order the replay produced them, so the first violation reported is a
	// function of the run and not of a map walk.
	for _, st := range o.recovered() {
		for _, d := range st.Decided {
			if !d.Rollback {
				continue
			}
			k := txnDecisionKey(d.Key, d.StartTS)
			if proposed[k] {
				continue // somebody wrote the record on purpose; no permission needed
			}
			if _, ok := authorised[k]; ok {
				o.declarations++
				continue
			}
			tries := attempted[k]
			if len(tries) == 0 {
				// A rolled-back record with neither a proposal nor a resolve
				// behind it. That is a record no command in the log produced,
				// which is a different defect from the one this oracle is for --
				// and it is still not a state any correct run can reach.
				return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
					"range %d: the transaction that started at %s on primary %q is ROLLED BACK, "+
						"and no committed command anywhere proposed that record or resolved that "+
						"transaction. The record exists and nothing in the log accounts for it",
					st.Range, d.StartTS, d.Key)}
			}
			// Every resolve that named this transaction judged it against a
			// timestamp at or below its own deadline, so every one of them was
			// looking at a lock that had not expired -- and the owner is dead
			// anyway.
			worst := tries[0]
			for _, r := range tries[1:] {
				if worst.ExpireAt.Less(r.ExpireAt) {
					worst = r
				}
			}
			o.declarations++
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d: the transaction that started at %s on primary %q is ROLLED BACK by a "+
					"RESOLVER -- nobody proposed that record -- and every one of the %d resolves "+
					"that named it judged an unexpired lock. The nearest carried expire-at %s "+
					"against a deadline of %s, which is not above it. A resolver may only make an "+
					"owner dead once the deadline the owner published has passed: the TTL is "+
					"expiry, not opinion (D-A6-5, D-A6-10). This kills live coordinators, and it "+
					"is invisible to every other checker because aborting a transaction is a legal "+
					"outcome (M62, DESIGN-A6 §13.4)",
				st.Range, d.StartTS, d.Key, len(tries), worst.ExpireAt, worst.Deadline)}
		}
	}
	return nil
}

// ValueAtIndex is supplied by the harness: replay a range's committed log up to
// and including `upto`, and report what `key` held at timestamp `at`.
//
// It is injected for `ReadsAt`'s reason and the reason is sharper again here.
// A7's whole claim is that a read answered OFF the log still reflects everything
// committed as of its confirmed index. If the oracle asked the node what it
// held, it would be checking that the node agrees with itself -- and the node's
// local state is exactly the thing under suspicion, because a deposed leader's
// local state is where a stale read comes from.
//
// So the expectation comes from replaying the committed log, which is the same
// independent execution snapshot equivalence uses, and the two answers are
// compared.
type ValueAtIndex func(rangeID uint64, upto raft.Index, key string, at hlc.Timestamp) (value string, found, ok bool)

// ReadIndexAgreement: every read answered off the log matches what that range's
// committed log produces at the index the read was confirmed at.
//
// # Why this oracle exists, and what nothing else can see
//
// DESIGN-A7 §5 names three properties for the read path and this is the third.
// The first is per-key linearizability, which porcupine already checks -- and it
// is the WEAKEST of the three, because **a stale read is only caught if some
// client observed the write it missed.** In a quiet history nobody observed it
// and porcupine is green over a lie.
//
// The second is a ledger-side bound on the index itself. This is the third: the
// same question answered two ways, one of them not involving the node that
// answered it.
//
// > **It is the only instrument in this phase that can catch a stale read no
// > client observed**, which is why ruling 5 made it a FIXTURE rather than a
// > lane: it runs in the sweep, not when somebody remembers.
//
// # What a violation means, concretely
//
// The node answered `v` for a key, having confirmed it could serve at index i,
// and the committed log at index i says the key held something else. Either the
// answer came from state that is not the log's state at i -- a deposed leader
// reading its own past -- or the confirmed index was too low for the answer
// given. Both are stale reads; the oracle does not need to tell them apart to
// refuse them.
type ReadIndexAgreement struct {
	base
	valueAt  ValueAtIndex
	compared int
}

// NewReadIndexAgreement builds the oracle.
func NewReadIndexAgreement(l *Ledger, valueAt ValueAtIndex) *ReadIndexAgreement {
	return &ReadIndexAgreement{base: base{l: l, name: "read-index-answers-match-the-log"}, valueAt: valueAt}
}

// Compared is how many off-log answers this oracle actually checked. It is the
// non-vacuity witness: a sweep that served no reads off the log exercises none
// of this, and a green over zero comparisons is this register's commonest entry.
func (o *ReadIndexAgreement) Compared() int { return o.compared }

// Interested returns false: this is a property of recorded answers against the
// committed log, evaluated once.
func (o *ReadIndexAgreement) Interested(sim.Kind) bool { return false }

// OnStep is never called: see Interested.
func (o *ReadIndexAgreement) OnStep(sim.View, sim.Event) *sim.Violation { return nil }

// Check compares every off-log answer against the log it claimed to reflect.
func (o *ReadIndexAgreement) Check() *sim.Violation {
	for _, r := range o.l.Reads() {
		if !r.OffLog || r.Refused {
			continue
		}
		if o.valueAt == nil {
			return nil
		}
		// # HALF ONE: the read waited.
		//
		// The quorum established a POSITION. This is the only thing that says
		// the node reached it, and it is the half `M76` removes.
		if r.AppliedAt < r.Index {
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d: node %d answered a read of %q OFF THE LOG having confirmed it could "+
					"serve at index %d, while its own state machine had only applied to %d. The "+
					"confirming quorum establishes a POSITION -- that this leader was still leader "+
					"at or after the read arrived -- and says nothing whatever about whether THIS "+
					"node has got there. Answering before it has is reading your own past",
				r.Range, r.Node, r.Key, r.Index, r.AppliedAt)}
		}
		// # HALF TWO: the node's state is the log's state, AT THE POSITION THE
		// # NODE WAS ACTUALLY AT.
		//
		// Not at Index. A node that has applied past the confirmed index may
		// legitimately return a newer version, and demanding equality at Index
		// would report that as a violation -- a false accusation of correct
		// behaviour, which is BUG-016's standard.
		want, found, ok := o.valueAt(r.Range, r.AppliedAt, r.Key, r.At)
		if !ok {
			// The ledger did not witness a committed prefix for this range, so
			// there is nothing to compare against. Reporting a violation here
			// would accuse a correct run of the harness's own gap, which is
			// BUG-016's lesson.
			continue
		}
		o.compared++
		if found != r.Found || (found && want != r.Value) {
			return &sim.Violation{Checker: o.name, Detail: fmt.Sprintf(
				"range %d: node %d answered a read of %q at %s OFF THE LOG with %q (found=%v), "+
					"having applied to index %d -- but that range's committed log at index %d holds "+
					"%q (found=%v). The node's state machine is not the log's state at the position "+
					"the node itself says it reached, and no client had to observe anything for "+
					"this to be a wrong answer",
				r.Range, r.Node, r.Key, r.At, r.Value, r.Found, r.AppliedAt, r.AppliedAt, want, found)}
		}
	}
	return nil
}
