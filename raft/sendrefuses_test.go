package raft

import (
	"strings"
	"testing"
)

// TestSendRefusesEveryTermBearingType induces BUG-027's structural fix.
//
// The narrow fix was two call sites. This is the fix that matters: `send`
// refuses by default, so a message type added later is GATED unless somebody
// puts it on the list -- rather than ungated unless somebody remembers.
//
// The defect it prevents was described in advance, in the exact terms it
// occurred in, by `sendGatedOn`'s own comment. A comment that predicts a defect
// and does not prevent it is a comment doing the wrong job, so the prediction is
// executable now.
func TestSendRefusesEveryTermBearingType(t *testing.T) {
	// The three with a written non-gate argument on Ready.Messages. Anything
	// else must be refused, INCLUDING types that do not exist yet -- which is
	// the whole point of enumerating the exceptions rather than the rule.
	allowed := map[MessageType]bool{MsgPreVote: true, MsgPreVoteResp: true, MsgTimeoutNow: true}

	for ty := MessageType(0); ty < numMessageTypes; ty++ {
		r, err := New(Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		refused := func() (refused bool) {
			defer func() {
				if rec := recover(); rec != nil {
					refused = true
					if !strings.Contains(rec.(string), "sendGated") {
						t.Errorf("%s: the refusal does not say what to do instead: %v", ty, rec)
					}
				}
			}()
			r.send(Message{Type: ty, From: 1, To: 2, Term: r.term})
			return false
		}()
		if allowed[ty] && refused {
			t.Errorf("%s is a stated non-gate and send() refused it; the enumeration on "+
				"Ready.Messages and this switch have drifted apart", ty)
		}
		if !allowed[ty] && !refused {
			t.Errorf("send() released a %s. Every message that leaves this node carries r.term "+
				"and makes a claim about it; only the three types with a written non-gate "+
				"argument may make that claim before the term is durable (BUG-027)", ty)
		}
	}
}

// TestTheAllowListIsExactlyTheStatedNonGates keeps the switch and the prose from
// drifting. tools/gatepin pins the prose; this pins the code against the same
// three names, so neither can move alone.
func TestTheAllowListIsExactlyTheStatedNonGates(t *testing.T) {
	var released []MessageType
	for ty := MessageType(0); ty < numMessageTypes; ty++ {
		r, _ := New(Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
		func() {
			defer func() { _ = recover() }()
			r.send(Message{Type: ty, From: 1, To: 2, Term: r.term})
			released = append(released, ty)
		}()
	}
	if len(released) != 3 {
		t.Fatalf("send() releases %d of %d message types: %v. The allow list is the ungated "+
			"SET, and growing it is a change to what persist-before-reply means",
			len(released), int(numMessageTypes), released)
	}
}

// TestTheReadIndexWireIsGatedOnTheTerm covers BOTH read-index send sites, and it
// exists because `mutant-covered` refused the previous covering test.
//
// `TestTheAllowListIsExactlyTheStatedNonGates` kills `M80` — the mutation grows
// the allow list and the count changes — but it never executes the two CALL
// SITES the patch also replaces. Killing a mutant and executing the line it
// changes are different questions, and the lane asks the second: a test that goes
// around the path cannot fail for the right reason, and one day it will stop
// failing at all.
//
// So this drives both directions through raft: a follower FORWARDING a read
// (`MsgReadIndex`), and a leader ANSWERING one (`MsgReadIndexResp`). Each carries
// this node's term, and BUG-027 is that neither was withheld until that term was
// durable.
func TestTheReadIndexWireIsGatedOnTheTerm(t *testing.T) {
	t.Run("a follower's forward waits for its own term", func(t *testing.T) {
		r, err := New(Config{ID: 2, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		// A term bump this node has not persisted yet.
		if err := r.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 5,
			PrevLogIndex: 0, PrevLogTerm: 0}); err != nil {
			t.Fatalf("app: %v", err)
		}
		if err := r.ReadIndex([]byte("ctx")); err != nil {
			t.Fatalf("read index: %v", err)
		}
		rd := r.Ready()
		for _, m := range rd.Messages {
			if m.Type == MsgReadIndex {
				t.Fatalf("the forward went out advertising term %d before that term was durable; "+
					"a crash here forgets a term this node has already spoken in (BUG-027)", m.Term)
			}
		}
		r.AckPersisted(rd.Mark)
		var forwarded bool
		for _, m := range r.Ready().Messages {
			if m.Type == MsgReadIndex && m.To == 1 {
				forwarded = true
			}
		}
		if !forwarded {
			t.Error("the forward was never released; the gate must WITHHOLD, not drop")
		}
	})

	t.Run("a leader's answer waits for its own term", func(t *testing.T) {
		r, err := New(Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		if err := r.Campaign(); err != nil {
			t.Fatalf("campaign: %v", err)
		}
		r.AckPersisted(r.Ready().Mark)
		if err := r.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: r.term, Granted: true}); err != nil {
			t.Fatalf("vote resp: %v", err)
		}
		if r.role != RoleLeader {
			t.Fatalf("node 1 did not become leader: %v", r.role)
		}
		r.AckPersisted(r.Ready().Mark)

		// A follower asks. The leader records it and confirms on the next
		// quorum of append responses.
		if err := r.Step(Message{Type: MsgReadIndex, From: 3, To: 1, Term: r.term,
			ReadCtx: []byte("ctx")}); err != nil {
			t.Fatalf("read index: %v", err)
		}
		last := r.lastIndex()
		for _, n := range []NodeID{2, 3} {
			if err := r.Step(Message{Type: MsgAppResp, From: n, To: 1, Term: r.term,
				Success: true, MatchIndex: last}); err != nil {
				t.Fatalf("app resp from %d: %v", n, err)
			}
		}
		// Both drains, for the reason the snapshot-prefix test gives: a gated
		// message may be released in the Ready that carries the mark or in the
		// one after the acknowledgement, and looking at only the second reports
		// a silent drop that did not happen.
		rd1 := r.Ready()
		r.AckPersisted(rd1.Mark)
		rd2 := r.Ready()

		var answered bool
		for _, m := range append(append([]Message{}, rd1.Messages...), rd2.Messages...) {
			if m.Type == MsgReadIndexResp && m.To == 3 {
				answered = true
				if m.Term != r.term {
					t.Errorf("the answer carries term %d, not this leader's %d", m.Term, r.term)
				}
			}
		}
		if !answered {
			t.Errorf("the leader never answered the forwarded read (pending=%d ready=%d gated=%d "+
				"term=%d role=%v); without this the follower waits forever and the read is "+
				"silently unserved (BUG-025's shape)",
				len(r.pendingReads), len(r.readyReads), len(r.gated), r.term, r.role)
		}
	})
}
