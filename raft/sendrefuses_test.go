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
