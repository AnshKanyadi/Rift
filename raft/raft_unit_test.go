package raft

import "testing"

func threeNode(t *testing.T, id NodeID) *Raft {
	t.Helper()
	r, err := New(Config{ID: id, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 3})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return r
}

// TestVoteGrantIsWithheldUntilTheVoteIsDurable is the canonical gate, tested in
// isolation. This is what DR-7 bought: the property is unit-testable here rather
// than distributed across every driver.
func TestVoteGrantIsWithheldUntilTheVoteIsDurable(t *testing.T) {
	r := threeNode(t, 2)
	if err := r.Step(Message{Type: MsgVote, From: 1, To: 2, Term: 1, LastLogIndex: 0}); err != nil {
		t.Fatalf("step: %v", err)
	}

	rd := r.Ready()
	if rd.HardState == nil || rd.HardState.Vote != 1 {
		t.Fatalf("the vote was not offered for persistence: %+v", rd.HardState)
	}
	if len(rd.Messages) != 0 {
		t.Fatalf("a vote grant was released before the vote was durable: %+v", rd.Messages)
	}
	if rd.Mark == 0 {
		t.Fatal("state was mutated but no persist mark was issued, so nothing can gate on it")
	}
	if r.PendingGated() != 1 {
		t.Fatalf("expected the grant to be withheld, %d gated", r.PendingGated())
	}
	if err := r.AssertQuiescent(); err == nil {
		t.Fatal("a node withholding a message reported itself quiescent")
	}

	r.AckPersisted(rd.Mark)
	rd = r.Ready()
	if len(rd.Messages) != 1 || rd.Messages[0].Type != MsgVoteResp || !rd.Messages[0].Granted {
		t.Fatalf("the grant was not released after the ack: %+v", rd.Messages)
	}
	if err := r.AssertQuiescent(); err != nil {
		t.Errorf("quiescent check failed after the queue drained: %v", err)
	}
}

// TestOneVotePerTerm is election safety's mechanism at the unit level.
func TestOneVotePerTerm(t *testing.T) {
	r := threeNode(t, 2)
	_ = r.Step(Message{Type: MsgVote, From: 1, To: 2, Term: 5})
	r.AckPersisted(r.Ready().Mark)
	_ = r.Step(Message{Type: MsgVote, From: 3, To: 2, Term: 5})
	r.AckPersisted(r.Ready().Mark)

	for _, m := range r.Ready().Messages {
		if m.To == 3 && m.Granted {
			t.Fatal("a second candidate was granted a vote in a term already voted in")
		}
	}
}

// TestStaleLogCandidateIsRefused is what leader completeness rests on.
func TestStaleLogCandidateIsRefused(t *testing.T) {
	r := threeNode(t, 2)
	r.log = []Entry{{Term: 3, Index: 1}, {Term: 3, Index: 2}}
	r.term = 3

	_ = r.Step(Message{Type: MsgVote, From: 1, To: 2, Term: 4, LastLogIndex: 1, LastLogTerm: 3})
	r.AckPersisted(r.Ready().Mark)
	for _, m := range r.Ready().Messages {
		if m.Type == MsgVoteResp && m.Granted {
			t.Fatal("granted to a candidate with a shorter log; a committed entry could be overwritten")
		}
	}
}

// TestOnlyCurrentTermCommitsByCounting is the figure-8 rule.
func TestOnlyCurrentTermCommitsByCounting(t *testing.T) {
	r := threeNode(t, 1)
	r.term = 2
	r.role = RoleLeader
	r.log = []Entry{{Term: 1, Index: 1}}
	r.nextIndex = []Index{2, 2, 2}
	r.matchIndex = []Index{1, 1, 0}
	r.maybeCommit()
	if r.commitIndex != 0 {
		t.Fatalf("an entry from an earlier term was committed by counting; commit=%d. "+
			"A later leader with a shorter log could still overwrite it (figure 8)", r.commitIndex)
	}

	// An entry from the current term above it commits both.
	r.log = append(r.log, Entry{Term: 2, Index: 2})
	r.matchIndex = []Index{2, 2, 0}
	r.maybeCommit()
	if r.commitIndex != 2 {
		t.Fatalf("a current-term entry replicated on a majority did not commit; commit=%d", r.commitIndex)
	}
}

// TestRestoreIsTheRealRecoveryPath: recovery reads back what the engine kept.
func TestRestoreIsTheRealRecoveryPath(t *testing.T) {
	cfg := Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 3}
	r, err := Restore(cfg, HardState{Term: 7, Vote: 2}, []Entry{{Term: 7, Index: 1}})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if r.term != 7 || r.vote != 2 || r.lastIndex() != 1 {
		t.Fatalf("recovered state is wrong: term=%d vote=%d last=%d", r.term, r.vote, r.lastIndex())
	}
	// A gapped log is refused rather than silently accepted.
	if _, err := Restore(cfg, HardState{}, []Entry{{Term: 1, Index: 2}}); err == nil {
		t.Error("a log that is not a gapless prefix was accepted")
	}
}

// TestConfigRefusals induces the constructor's checks.
func TestConfigRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"id zero", Config{ID: 0, Peers: []NodeID{1}, ElectionTimeout: 10, HeartbeatTimeout: 3}},
		{"not in own peer set", Config{ID: 4, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 3}},
		{"unsorted peers", Config{ID: 1, Peers: []NodeID{3, 1}, ElectionTimeout: 10, HeartbeatTimeout: 3}},
		{"heartbeat not under election", Config{ID: 1, Peers: []NodeID{1}, ElectionTimeout: 3, HeartbeatTimeout: 3}},
	} {
		if _, err := New(tc.cfg); err == nil {
			t.Errorf("%s was accepted", tc.name)
		}
	}
}
