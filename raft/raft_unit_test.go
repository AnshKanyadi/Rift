package raft

import (
	"errors"
	"testing"
)

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
	r, err := Restore(cfg, HardState{Term: 7, Vote: 2}, SnapshotMeta{}, []Entry{{Term: 7, Index: 1}})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if r.term != 7 || r.vote != 2 || r.lastIndex() != 1 {
		t.Fatalf("recovered state is wrong: term=%d vote=%d last=%d", r.term, r.vote, r.lastIndex())
	}
	// A gapped log is refused rather than silently accepted.
	if _, err := Restore(cfg, HardState{}, SnapshotMeta{}, []Entry{{Term: 1, Index: 2}}); err == nil {
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

// TestLeaderCountsItsOwnCopyOnlyWhenDurable is the leader half of the gate DR-8
// enumerates for followers.
//
// # Why the leader is not exempt
//
// DR-8's first gate exists because a quorum has to be a quorum of DURABLE
// copies: a crashed node comes back with what it wrote, so a copy that was never
// written was never a member of the quorum that committed. The rule is stated
// there for a follower acking an append, but nothing in the argument is about
// followers. A leader counting its own unsynced append toward that same quorum
// commits on the strength of a copy it may lose, which is the identical defect
// with the message elided because the leader is talking to itself.
//
// So: one durable follower copy plus the leader's own IN-FLIGHT append is one
// durable copy out of three, and nothing may commit on it.
//
// # What this test covers, exactly
//
// The Propose path, which is the one a live leader takes on every client write.
// becomeLeader initialising its own match index optimistically is the same
// mistake, and it is NOT separately observable: the entries present when a node
// takes office are all from earlier terms, and the figure-8 rule forbids
// committing those by counting at all. Every entry whose commit the leader's own
// match index can affect is one it proposed itself, and that is this path.
func TestLeaderCountsItsOwnCopyOnlyWhenDurable(t *testing.T) {
	r := threeNode(t, 1)

	// Campaign and win. The vote request is gated on the term and vote being
	// durable, so the mark is acknowledged exactly as a driver would.
	for range 10 {
		r.Tick()
	}
	r.AckPersisted(r.Ready().Mark)
	if err := r.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1, Granted: true}); err != nil {
		t.Fatalf("vote resp: %v", err)
	}
	if r.Role() != RoleLeader {
		t.Fatalf("the node did not win the election; role is %s", r.Role())
	}

	if err := r.Propose(ProposalID{Node: 1, Seq: 1}, []byte("x")); err != nil {
		t.Fatalf("propose: %v", err)
	}
	rd := r.Ready()
	if len(rd.Entries) != 1 || rd.Entries[0].Index != 1 {
		t.Fatalf("the proposal was not handed over for persistence: %+v", rd.Entries)
	}
	mark := rd.Mark
	if mark == 0 {
		t.Fatal("an appended entry issued no persist mark, so nothing can gate on its durability")
	}

	// One follower has it on disk. The leader does not, yet.
	if err := r.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Success: true, MatchIndex: 1}); err != nil {
		t.Fatalf("app resp: %v", err)
	}
	if got := r.Ready(); len(got.Committed) != 0 {
		t.Fatalf("index %d was committed on ONE durable copy plus the leader's own unsynced append. "+
			"If the leader crashes here it comes back without the entry, and a cluster of three with "+
			"one copy left cannot keep it: this is DR-8's first gate with the leader on the other end of it",
			got.Committed[0].Index)
	}

	// Now the leader's own write lands: two durable copies, a real quorum.
	r.AckPersisted(mark)
	got := r.Ready()
	if len(got.Committed) != 1 || got.Committed[0].Index != 1 {
		t.Fatalf("index 1 did not commit once the leader's own copy was durable: %+v", got.Committed)
	}
}

// leaderOfThree brings node 1 to leadership in a three-voter cluster.
func leaderOfThree(t *testing.T, cfg Config) *Raft {
	t.Helper()
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for range cfg.ElectionTimeout {
		r.Tick()
	}
	r.AckPersisted(r.Ready().Mark)
	if err := r.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1, Granted: true}); err != nil {
		t.Fatalf("vote resp: %v", err)
	}
	if r.Role() != RoleLeader {
		t.Fatalf("node 1 did not win; role is %s", r.Role())
	}
	return r
}

// TestPromotionIsRefusedWhileTheLearnerLags is the catch-up bound.
//
// Promoting a learner that is behind raises the quorum while that learner can
// contribute nothing, so a cluster that tolerated one failure tolerates none
// until it catches up. The refusal is a refusal and not a queue: a queued
// promotion is one whose preconditions were true at some point in the past.
func TestPromotionIsRefusedWhileTheLearnerLags(t *testing.T) {
	r := leaderOfThree(t, Config{
		ID: 1, Peers: []NodeID{1, 2, 3, 4}, Learners: []NodeID{4},
		ElectionTimeout: 10, HeartbeatTimeout: 3, PromotionLag: 2,
	})

	// Put some distance between the leader's log and the learner's.
	for i := range 6 {
		if err := r.Propose(ProposalID{Node: 1, Seq: uint64(i + 1)}, []byte("x")); err != nil {
			t.Fatalf("propose: %v", err)
		}
	}
	r.AckPersisted(r.Ready().Mark)

	promote := ConfChangeV2{
		Transition: ConfChangeSimple,
		Changes:    []ConfChangeSingle{{Type: ConfChangeAddVoter, Node: 4}},
	}
	err := r.ProposeConfChange(ProposalID{Node: 1, Seq: 100}, promote)
	if !errors.Is(err, ErrLearnerLagging) {
		t.Fatalf("a learner six entries behind a bound of two was promoted: %v", err)
	}
	if r.Configuration().IsVoter(4) {
		t.Fatal("the refusal did not stop the configuration from changing")
	}

	// Once it catches up, the same promotion is accepted.
	if err := r.Step(Message{
		Type: MsgAppResp, From: 4, To: 1, Term: 1, Success: true, MatchIndex: r.Status().Last,
	}); err != nil {
		t.Fatalf("app resp: %v", err)
	}
	if err := r.ProposeConfChange(ProposalID{Node: 1, Seq: 101}, promote); err != nil {
		t.Fatalf("a caught-up learner was refused promotion: %v", err)
	}
	if !r.Configuration().IsVoter(4) {
		t.Fatal("the accepted promotion did not take effect on append")
	}
}

// TestPromotedLearnerCannotWinWithoutTheCommittedEntries is the safety half of
// the same criterion: a promotion during catch-up cannot lose a committed entry.
//
// Promotion is a LIVENESS risk, not a safety one, and the reason is worth
// stating because it is what makes the catch-up bound a policy rather than a
// correctness requirement. A promoted-but-lagging voter raises the quorum, so
// commits may stall — but it cannot cause a committed entry to be lost, because
// the up-to-date check refuses it the votes it would need to become leader and
// overwrite one.
//
// This is the check that has to hold even if the bound is set wrong.
func TestPromotedLearnerCannotWinWithoutTheCommittedEntries(t *testing.T) {
	// A voter that holds six entries in term 1.
	voter, err := New(Config{
		ID: 2, Peers: []NodeID{1, 2, 3, 4}, Learners: []NodeID{4},
		ElectionTimeout: 10, HeartbeatTimeout: 3,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	var ents []Entry
	for i := range 6 {
		ents = append(ents, Entry{Type: EntryNormal, Term: 1, Index: Index(i + 1), Data: []byte("x")})
	}
	if err := voter.Step(Message{
		Type: MsgApp, From: 1, To: 2, Term: 1, PrevLogIndex: 0, Entries: ents, LeaderCommit: 6,
	}); err != nil {
		t.Fatalf("app: %v", err)
	}
	voter.AckPersisted(voter.Ready().Mark)

	// Node 4, promoted while holding only two of them, campaigns.
	if err := voter.Step(Message{
		Type: MsgVote, From: 4, To: 2, Term: 2, LastLogIndex: 2, LastLogTerm: 1,
	}); err != nil {
		t.Fatalf("vote: %v", err)
	}
	voter.AckPersisted(voter.Ready().Mark)

	for _, m := range voter.Ready().Messages {
		if m.Type == MsgVoteResp && m.To == 4 && m.Granted {
			t.Fatal("a voter granted its vote to a node missing four committed entries. " +
				"That node would win, and everything it does not have is gone -- which is " +
				"leader completeness failing by way of a promotion")
		}
	}
}
