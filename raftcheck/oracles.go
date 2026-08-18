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

// base carries what every oracle shares.
type base struct {
	l    *Ledger
	name string
}

func (b base) Name() string { return b.name }

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
	logs := o.l.durableLog
	for a := range logs {
		for b := a + 1; b < len(logs); b++ {
			la, lb := logs[a], logs[b]
			n := min(len(la), len(lb))
			for i := n - 1; i >= 0; i-- {
				if la[i].Term != lb[i].Term {
					continue
				}
				// Same index and term: everything before must agree.
				for k := 0; k < i; k++ {
					if la[k].Term != lb[k].Term || string(la[k].Data) != string(lb[k].Data) {
						return &sim.Violation{
							Checker: o.name,
							Detail: fmt.Sprintf(
								"nodes %d and %d agree at index %d term %d but differ at index %d (terms %d and %d)",
								a, b, la[i].Index, la[i].Term, la[k].Index, la[k].Term, lb[k].Term),
						}
					}
				}
				break
			}
		}
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
			if !hasEntry(ld.log, c.entry) {
				return &sim.Violation{
					Checker: o.name,
					Detail: fmt.Sprintf(
						"entry index %d term %d was committed (first applied by node %d at instant %d) "+
							"but node %d, leading in later term %d from instant %d, had persisted %d entries without it",
						c.entry.Index, c.entry.Term, c.node, int64(c.at),
						ld.node, ld.term, int64(ld.at), len(ld.log)),
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
type StateMachineSafety struct{ base }

// NewStateMachineSafety builds the oracle.
func NewStateMachineSafety(l *Ledger) *StateMachineSafety {
	return &StateMachineSafety{base{l: l, name: "state-machine-safety"}}
}

func (o *StateMachineSafety) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	for a := range o.l.applied {
		for b := a + 1; b < len(o.l.applied); b++ {
			for _, ea := range o.l.applied[a] {
				for _, eb := range o.l.applied[b] {
					if ea.Index != eb.Index {
						continue
					}
					if ea.Term != eb.Term || string(ea.Data) != string(eb.Data) {
						return &sim.Violation{
							Checker: o.name,
							Detail: fmt.Sprintf(
								"nodes %d and %d applied different entries at index %d: term %d %q versus term %d %q",
								a, b, ea.Index, ea.Term, ea.Data, eb.Term, eb.Data),
						}
					}
				}
			}
		}
	}
	return nil
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
type PersistBeforeReply struct{ base }

// NewPersistBeforeReply builds the oracle.
func NewPersistBeforeReply(l *Ledger) *PersistBeforeReply {
	return &PersistBeforeReply{base{l: l, name: "persist-before-reply"}}
}

func (o *PersistBeforeReply) OnStep(_ sim.View, _ sim.Event) *sim.Violation {
	for _, s := range o.l.sent {
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
func All(l *Ledger) []sim.Oracle {
	return []sim.Oracle{
		NewElectionSafety(l),
		NewLogMatching(l),
		NewLeaderCompleteness(l),
		NewStateMachineSafety(l),
		NewPersistBeforeReply(l),
	}
}
