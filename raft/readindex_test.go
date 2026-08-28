package raft

import (
	"bytes"
	"testing"
)

// leaderForRead brings node 1 to leadership and commits its term-start no-op,
// which is the state a leader must be in before it may answer any read (§2).
func leaderForRead(t *testing.T) *Raft {
	t.Helper()
	r, err := New(Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for range 10 {
		r.Tick()
	}
	r.AckPersisted(r.Ready().Mark)
	if err := r.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1, Granted: true}); err != nil {
		t.Fatalf("vote resp: %v", err)
	}
	if r.Role() != RoleLeader {
		t.Fatalf("not leader: %s", r.Role())
	}
	r.AckPersisted(r.Ready().Mark)
	if err := r.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Success: true,
		MatchIndex: r.lastIndex()}); err != nil {
		t.Fatalf("app resp: %v", err)
	}
	return r
}

// TestAReadIsNeverAnsweredBelowItsStampedIndex is RULING 3's condition.
//
// Ansh, ruling D-A7-3: *approved, with the condition that a read arriving at
// index i and confirmed later must be provably not answerable at any index
// below i, induced.*
//
// Arrival capture is the WEAKER of the two options by construction --
// confirmation-time capture is a later point and can never be stale -- so the
// entire safety case for the cheaper choice is that the stamped index is a
// sound floor. That claim is not allowed to live in prose.
func TestAReadIsNeverAnsweredBelowItsStampedIndex(t *testing.T) {
	r := leaderForRead(t)
	_ = r.Ready()

	// A write completes: proposed, replicated, committed. Its index is what any
	// read issued afterwards must reflect.
	if err := r.Propose(ProposalID{Node: 1, Seq: 1}, []byte("v1")); err != nil {
		t.Fatalf("propose: %v", err)
	}
	written := r.lastIndex()
	r.AckPersisted(r.Ready().Mark)
	if err := r.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Success: true,
		MatchIndex: written}); err != nil {
		t.Fatalf("app resp: %v", err)
	}
	if r.commitIndex < written {
		t.Fatalf("the write did not commit: commit=%d written=%d", r.commitIndex, written)
	}

	// The read arrives AFTER that write completed.
	if err := r.ReadIndex([]byte("read-1")); err != nil {
		t.Fatalf("read index: %v", err)
	}
	if err := r.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Success: true,
		MatchIndex: written}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	rd := r.Ready()
	if len(rd.ReadStates) != 1 {
		t.Fatalf("the read was not confirmed by a quorum: %+v", rd.ReadStates)
	}
	got := rd.ReadStates[0]
	if !bytes.Equal(got.Ctx, []byte("read-1")) {
		t.Errorf("the answer came back under the wrong context: %q", got.Ctx)
	}

	// THE CONDITION. Serving this read at any index below the stamped one could
	// miss `written`, which completed before the read was issued.
	if got.Index < written {
		t.Errorf("the read was stamped at index %d, below the index %d of a write that "+
			"completed BEFORE the read was issued. Any answer at that index may miss "+
			"that write, which is a stale read produced by the mechanism whose whole "+
			"job is preventing them (DESIGN-A7 section 5a)", got.Index, written)
	}
}

// TestALeaderDoesNotServeAReadAgainstAnInheritedCommitIndex is §2, which
// CLAUDE.md's sharp-edge list names: *read index needs the term-start no-op*.
func TestALeaderDoesNotServeAReadAgainstAnInheritedCommitIndex(t *testing.T) {
	r, err := New(Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for range 10 {
		r.Tick()
	}
	r.AckPersisted(r.Ready().Mark)
	if err := r.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1, Granted: true}); err != nil {
		t.Fatalf("vote resp: %v", err)
	}
	if r.termStart == 0 {
		t.Fatal("becomeLeader did not record a term-start index")
	}
	// The window is ARRANGED, not hoped for: this test never feeds an append
	// response, so the term-start no-op cannot reach a quorum and commitIndex
	// stays inherited. If it committed anyway the arrangement broke, and the
	// assertion below would pass over a window that does not exist.
	if r.commitIndex >= r.termStart {
		t.Fatalf("the no-op at %d is already committed (commitIndex %d), so the inherited-window "+
			"this test exists to check does not exist in this run. Nothing acknowledged an "+
			"append here, so if the no-op committed, the arrangement is wrong rather than the "+
			"window being unreachable", r.termStart, r.commitIndex)
	}
	if err := r.ReadIndex([]byte("early")); err != nil {
		t.Fatalf("read index: %v", err)
	}
	for _, q := range r.pendingReads {
		if q.index < r.termStart {
			t.Errorf("a read was stamped at %d while this term's no-op sits at %d and is not "+
				"yet committed. commitIndex is inherited until the no-op commits, so an answer "+
				"at that stamp can miss writes committed before this leader took office "+
				"(DESIGN-A7 section 2)", q.index, r.termStart)
		}
	}
}

// TestAReadIsNotConfirmedByAStaleTerm is §4's fact-table row: a majority of
// responses confirms leadership only AT THE TERM the round was broadcast in.
func TestAReadIsNotConfirmedByAStaleTerm(t *testing.T) {
	r := leaderForRead(t)
	_ = r.Ready()
	if err := r.ReadIndex([]byte("ctx")); err != nil {
		t.Fatalf("read index: %v", err)
	}
	if len(r.pendingReads) != 1 {
		t.Fatalf("the read was not recorded: %d pending", len(r.pendingReads))
	}
	r.pendingReads[0].term = 99
	r.confirmRead(2)
	if len(r.readyReads) != 0 {
		t.Errorf("a read recorded under a term this node has left was confirmed anyway. A "+
			"quorum of responses confirms leadership only at the term the round was broadcast "+
			"in; this leader is no longer the one confirming (DESIGN-A7 section 4): %+v",
			r.readyReads)
	}
}

// TestAFollowerAsksTheLeaderRatherThanConfirmingItself is D-A7-2.
func TestAFollowerAsksTheLeaderRatherThanConfirmingItself(t *testing.T) {
	r, err := New(Config{ID: 2, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := r.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, PrevLogIndex: 0,
		PrevLogTerm: 0, LeaderCommit: 0}); err != nil {
		t.Fatalf("app: %v", err)
	}
	if r.leader != 1 {
		t.Fatalf("the follower did not learn a leader: %d", r.leader)
	}
	if err := r.ReadIndex([]byte("f")); err != nil {
		t.Fatalf("read index: %v", err)
	}

	// # The forward is WITHHELD until this follower's term is durable (BUG-027)
	//
	// The MsgApp above bumped this node from term 0 to term 1, so the hard state
	// is dirty. The forward carries `Term: r.term`, which is a claim about that
	// term, and a claim about a term that is not on disk is the term-amnesia
	// case the gate enumeration names.
	//
	// **This assertion is the shape of the defect.** The version of this test
	// that shipped with A7 drained Ready() here and required the forward to be
	// present -- so it did not merely fail to catch BUG-027, it PINNED the
	// ungated behaviour, and adding the gate turned it red. A test that asserts
	// a message goes out immediately is a test that will resist any gate ever
	// being added to it.
	rd0 := r.Ready()
	for _, m := range rd0.Messages {
		if m.Type == MsgReadIndex {
			t.Fatalf("a follower forwarded a read advertising term %d before its own term was "+
				"durable; a crash here forgets a term this node has already spoken in", m.Term)
		}
	}
	r.AckPersisted(rd0.Mark)

	var asked bool
	for _, m := range r.Ready().Messages {
		if m.Type == MsgReadIndex && m.To == 1 {
			asked = true
		}
	}
	if !asked {
		t.Error("a follower asked to serve a read did not forward to the leader once its term " +
			"was durable; the gate must withhold, not drop")
	}
	if err := r.Step(Message{Type: MsgReadIndexResp, From: 1, To: 2, Term: 1,
		ReadCtx: []byte("f"), ReadIndex: 7}); err != nil {
		t.Fatalf("resp: %v", err)
	}
	rd := r.Ready()
	if len(rd.ReadStates) != 1 || rd.ReadStates[0].Index != 7 {
		t.Errorf("the follower did not adopt the leader's index: %+v", rd.ReadStates)
	}
}

// TestReadIndexRefusesAnEmptyContext keeps the identity rule: an answer is
// matched to a request by the driver's own identifier, never by arrival order.
func TestReadIndexRefusesAnEmptyContext(t *testing.T) {
	r := leaderForRead(t)
	if err := r.ReadIndex(nil); err == nil {
		t.Error("ReadIndex accepted an empty context; answers would then be matched by " +
			"arrival order, which is BUG-004's mistake in a new place")
	}
}
