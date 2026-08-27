package raft

import "testing"

// TestASnapshottedPrefixRefusesAnAppendFromZero is BUG-009's class, directed.
//
// # Why this exists, and what it replaces
//
// `M34`'s covering test was `TestSnapshotPrefixIsNotOverwritten`: a 3,000-seed
// sweep under A2's shape, four minutes on a good day. It no longer kills the
// mutant, and the reason is measured rather than guessed — under A2 the
// precondition **does not occur at all**:
//
//	shape        appends from index 1 carrying entries   receiver already had a snapshot
//	A7 current   5,051 / 200 seeds                       1
//	a3             649 / 200 seeds                       0
//	a2             636 / 200 seeds                       0
//
// A sweep is an instrument whose reach moves when the workload moves, and a
// covering test built on one stops covering without saying so. This arranges the
// precondition directly, in microseconds, and cannot drift: the node HAS a
// snapshot because this test gave it one.
//
// # The property
//
// "Append from the very beginning" is only agreeable to a node that HAS a
// beginning. Once a prefix is in a snapshot, accepting an append at index 1 means
// overwriting entries this node has already applied and told the cluster were
// committed. Rejecting is safe and self-correcting: the reject carries lastIndex
// as its hint, so the leader jumps forward and sends a snapshot instead.
func TestASnapshottedPrefixRefusesAnAppendFromZero(t *testing.T) {
	cfg := Config{ID: 1, Peers: []NodeID{1, 2, 3}, ElectionTimeout: 10, HeartbeatTimeout: 1}
	r, err := Restore(cfg, HardState{Term: 4}, SnapshotMeta{Index: 6, Term: 3}, nil)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	// The premise, asserted rather than assumed: without a snapshot this test
	// arranges nothing and the assertion below passes vacuously.
	if r.snapIndex == 0 {
		t.Fatal("the node under test has no snapshot, so an append from index 1 is legitimately " +
			"agreeable and this test asserts nothing")
	}
	r.AckPersisted(r.Ready().Mark)

	// A leader whose view of this follower has been reset: PrevLogIndex 0, and
	// entries starting at 1 — squarely inside the prefix the snapshot holds.
	if err := r.Step(Message{
		Type: MsgApp, From: 2, To: 1, Term: 4, PrevLogIndex: 0, PrevLogTerm: 0,
		LeaderCommit: 0,
		Entries: []Entry{
			{Index: 1, Term: 4, Type: EntryNormal, Data: []byte("overwrite")},
			{Index: 2, Term: 4, Type: EntryNormal, Data: []byte("overwrite")},
		},
	}); err != nil {
		t.Fatalf("step: %v", err)
	}

	if r.snapIndex != 6 {
		t.Errorf("the snapshot index moved to %d; an append from the beginning discarded a prefix "+
			"this node had already applied", r.snapIndex)
	}
	// The reject is the whole mechanism: it carries lastIndex as its hint so the
	// leader jumps forward and sends a snapshot, rather than walking backwards
	// into a log that no longer exists.
	// Both drains: the reject is gated on the term, so it may be released in the
	// first Ready or in the one after the acknowledgement. Collecting only the
	// second would report a silent drop that did not happen.
	rd1 := r.Ready()
	r.AckPersisted(rd1.Mark)
	rd2 := r.Ready()
	var sawReject, sawAccept bool
	for _, m := range append(append([]Message{}, rd1.Messages...), rd2.Messages...) {
		if m.Type != MsgAppResp {
			continue
		}
		if m.Success {
			sawAccept = true
		} else {
			sawReject = true
			if m.Hint == 0 {
				t.Errorf("the rejection carries no hint; without lastIndex the leader walks " +
					"backwards into a log that has been compacted away")
			}
		}
	}
	if sawAccept {
		t.Errorf("the node ACCEPTED an append starting at index 1 while holding a snapshot at %d.\n"+
			"  Accepting means overwriting entries this node has already applied and told the "+
			"cluster were committed, which is state machine safety failing (BUG-009).", r.snapIndex)
	}
	if !sawReject {
		t.Error("the node neither accepted nor rejected the append; a silent drop is " +
			"indistinguishable from a partition and the leader never learns to send a snapshot")
	}
}
