package store

import (
	"bytes"
	"testing"

	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/raft"
)

// TestTheNoOpMatchesNoStateMachineArm is D-A7-6's FIRST proposition, induced.
//
// Ansh, on the ruling: *assert what A makes true rather than trusting it. No
// committed entry with empty Data and a zero ID ever reaches a state-machine
// arm, and no such entry ever answers a client. Both are checkable, both are
// cheap, and A's correctness is precisely those two propositions.*
//
// The property, stated as a difference rather than as a predicate: **replaying a
// log WITH term-start no-ops in it produces byte-identical state to replaying the
// same log without them.** That is stronger than asserting the three arms do not
// match, because it exercises the guard on the path that actually runs -- and it
// is the shape `M74` attacks.
func TestTheNoOpMatchesNoStateMachineArm(t *testing.T) {
	base := encodeMachine(FirstRangeDescriptor(), hlc.Timestamp{}, nil)

	real := []raft.Entry{
		{Type: raft.EntryNormal, Term: 1, Index: 1, ID: raft.ProposalID{Node: 1, Seq: 1},
			Data: encodeCmd("put", "k1", "v1", hlc.Timestamp{Wall: 10})},
		{Type: raft.EntryNormal, Term: 2, Index: 3, ID: raft.ProposalID{Node: 1, Seq: 2},
			Data: encodeCmd("put", "k2", "v2", hlc.Timestamp{Wall: 20})},
	}
	// The same log with a term-start no-op ahead of each term's first command,
	// which is where becomeLeader puts them.
	withNoOps := []raft.Entry{
		{Type: raft.EntryNormal, Term: 1, Index: 1},
		real[0],
		{Type: raft.EntryNormal, Term: 2, Index: 3},
		real[1],
	}

	_, markA, recsA, okA := ReplayMachine(base, real)
	_, markB, recsB, okB := ReplayMachine(base, withNoOps)
	if !okA || !okB {
		t.Fatalf("replay failed: without no-ops ok=%v, with no-ops ok=%v", okA, okB)
	}
	if markA != markB {
		t.Errorf("the no-ops moved the GC mark: %v without, %v with", markA, markB)
	}
	if len(recsA) != len(recsB) {
		t.Fatalf("the no-ops changed the record count: %d without, %d with -- a "+
			"dataless entry reached a state-machine arm", len(recsA), len(recsB))
	}
	for i := range recsA {
		if !bytes.Equal(recsA[i].Key, recsB[i].Key) || !bytes.Equal(recsA[i].Value, recsB[i].Value) {
			t.Errorf("record %d differs: key %q value %q without the no-ops, key %q value %q with",
				i, recsA[i].Key, recsA[i].Value, recsB[i].Key, recsB[i].Value)
		}
	}

	// # And the thing the replay path ACTUALLY rests on, which is not the skip
	//
	// Induced first and it did not fail: removing `if len(e.Data) == 0` from
	// applyOne leaves this test green, because applyOne HAS a default arm and
	// what protects it there is `decodeCmd` returning an op that the inner
	// `switch op` matches nothing for. The early return is a fast path; this is
	// the guard.
	//
	// The node's apply loop is the opposite -- no default, last arm guarded by
	// `len(e.Data) > 0` -- so **the two paths protect one property by two
	// different mechanisms, and snapshot equivalence compares their results.**
	// Either mechanism changing alone is a divergence, which is why both are
	// asserted rather than one.
	if op, _, _, _ := decodeCmd(nil); op == "put" || op == opGC {
		t.Errorf("decodeCmd on an empty command returned %q, which the replay's "+
			"default arm DOES act on. A term-start no-op would then mutate the "+
			"replayed state machine and diverge from the node, which ignores it "+
			"structurally (DESIGN-A7 section 3a)", op)
	}
}

// TestTheNoOpAnswersNobody is D-A7-6's SECOND proposition, induced.
//
// answerAt sits OUTSIDE the data switch and `owned` defaults true, so a no-op
// does arrive there -- verified by reading the loop, not assumed. The zero-ID
// guard is the only thing between a raft-internal entry and an in-flight client
// request, and `M75` removes it.
func TestTheNoOpAnswersNobody(t *testing.T) {
	if !(raft.ProposalID{}).Zero() {
		t.Fatalf("the zero ProposalID does not report Zero(); the no-op's whole " +
			"identity rests on this")
	}

	// And the identity is not a convention: Propose REFUSES to issue it, so no
	// client proposal can collide with the no-op. This is the named premise in
	// DESIGN-A7 §4.1 -- if the refusal is ever relaxed, the no-op stops being
	// distinguishable and breaks silently, so the premise is asserted here
	// rather than trusted to stay true.
	// # Asserted as a DIFFERENCE, because the first version of this was vacuous
	//
	// It read `if err := r.Propose(ProposalID{}, ...); err == nil { fail }`, and
	// that is satisfied by ErrNotLeader -- which a zero-value Raft returns
	// whatever the id is. Relaxing the zero-id rule left the test green, and the
	// induction caught it in one command.
	//
	// So the assertion is the DIFFERENCE between the two ids on one receiver: a
	// zero id must be refused for a reason a non-zero id is not. If the rule
	// goes, both return the same error and this fails.
	var r raft.Raft
	zeroErr := r.Propose(raft.ProposalID{}, []byte("x"))
	namedErr := r.Propose(raft.ProposalID{Node: 1, Seq: 1}, []byte("x"))
	if zeroErr == nil {
		t.Fatalf("Propose accepted the zero ProposalID")
	}
	if namedErr != nil && zeroErr.Error() == namedErr.Error() {
		t.Errorf("Propose refused the zero ProposalID and a named one for the SAME "+
			"reason (%v), so nothing here distinguishes them. The term-start no-op "+
			"is identified by holding the one identity the propose path is "+
			"forbidden to issue; if that rule is relaxed, a client proposal can "+
			"wear the no-op's identity and this breaks SILENTLY "+
			"(DESIGN-A7 section 4.1)", zeroErr)
	}
}

// TestBothPathsAssertTheNoOpProperty is DESIGN-A6 §13.4b's remedy, induced.
//
// Snapshot equivalence compares the node's state machine against the replay's.
// Both ignore a dataless entry, and **they ignore it for different reasons** --
// the node's switch has no default and a len(e.Data) > 0 guard; the replay's has
// a default and relies on the early skip plus decodeCmd returning an inert op.
// Equivalence sees only that the two outputs match, so it is green whichever
// mechanism is carrying the property and cannot report that one has rotted.
//
// So the property is asserted AT THIS PATH: no dataless, identity-less entry
// reaches the replay's command switch. Removing the skip makes this fail, which
// is the induction equivalence could never provide.
func TestBothPathsAssertTheNoOpProperty(t *testing.T) {
	base := encodeMachine(FirstRangeDescriptor(), hlc.Timestamp{}, nil)
	r, ok := NewReplay(base)
	if !ok {
		t.Fatal("NewReplay failed")
	}
	r.Apply([]raft.Entry{
		{Type: raft.EntryNormal, Term: 1, Index: 1},
		{Type: raft.EntryNormal, Term: 1, Index: 2, ID: raft.ProposalID{Node: 1, Seq: 1},
			Data: encodeCmd("put", "k", "v", hlc.Timestamp{Wall: 10})},
		{Type: raft.EntryNormal, Term: 2, Index: 3},
	})
	if got := r.NoOpReachedSwitch(); got != 0 {
		t.Errorf("%d term-start no-ops reached the replay's command switch, want 0. "+
			"The skip in applyOne is what keeps them out, and snapshot equivalence "+
			"cannot see this because it compares the two paths' OUTPUTS and both "+
			"produce an unchanged state machine (DESIGN-A6 section 13.4b)", got)
	}
}

// TestANoOpDoesNotAnswerAZeroIdentityClientOp routes a test THROUGH answerAt's
// zero-identity guard, which the sweep cannot reach.
//
// # Why this is a unit test rather than a floor
//
// `M75` removes the guard and the 30-seed sweep reported BLIND. That is not the
// sweep being too short: **the class M75 plants requires an in-flight client op
// carrying the zero ProposalID, and P-NOOP makes that unreachable** -- `Propose`
// refuses the zero id, and every clientOp is built from
// `ProposalID{Node: cfg.ID, Seq: propSeq}`, which is never zero. So nothing in a
// run can match, `inflight` never shrinks, and the counter never fires.
//
// That makes the guard **defence-in-depth behind P-NOOP rather than an
// independent protection** (DESIGN-A7 §4.1a), and it is why the guard must NOT
// be deleted under §25.1's third meaning: the day P-NOOP is relaxed, this line
// is the only thing between raft's own entry and somebody's request, and the
// failure is silent. So the class is covered deterministically here, by
// constructing the state a run cannot produce.
func TestANoOpDoesNotAnswerAZeroIdentityClientOp(t *testing.T) {
	n := &Replica{}
	// The state a run cannot produce: an in-flight op wearing the no-op's identity.
	n.inflight = []clientOp{{id: raft.ProposalID{}, op: "put", key: "k", histIdx: 1}}

	noop := raft.Entry{Type: raft.EntryNormal, Term: 3, Index: 9}
	before := len(n.inflight)
	n.answerAt(noop, "", "", hlc.Timestamp{}, 0)

	if len(n.inflight) != before {
		t.Errorf("a term-start no-op completed a client operation: inflight %d -> %d. "+
			"answerAt's e.ID.Zero() guard is the only thing between raft's own "+
			"entry and an in-flight request, and a client would be told its write "+
			"applied when what applied was the no-op (M75, DESIGN-A7 section 3a)",
			before, len(n.inflight))
	}
}
