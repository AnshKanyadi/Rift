package store

import (
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
)

// TestASplitDoesNotInheritAnUnappliedConfiguration is BUG-015's DIRECTED test,
// and it replaces a seed search that could not reach the class.
//
// # Why this is not a sweep
//
// `M46`'s class needs TWO independent things to coincide: a replica applying a
// split while holding a configuration entry appended ABOVE the split's index but
// not yet applied, and the range that split produces then being handed a
// membership change. A seed search buys the first and hopes for the second —
// measured, the precondition occurred 8 times in 200 seeds under A7's shape and
// 25 times in 200 under A4's, and detection was **zero at the seeds where it
// fired**, which is a different object from zero over a range.
//
//	A bundle pins ONE schedule, so a class that needs two independent
//	coincidences has no single schedule to pin.
//
// So this arranges both, deterministically, in milliseconds. No seed, no
// scheduler, no sweep.
//
// # What it asserts
//
// `Configuration()` is the ACTIVE configuration and is effective ON APPEND, so it
// is not a function of the applied prefix. `ConfigurationAt(index)` is the
// configuration the log says was in force AT that position. Two replicas applying
// the same split entry with different appended tails agree on the second and
// disagree on the first — and the new range's BIRTH configuration must be the
// second, or the replica that started behind reads the next membership entry as
// an illegal transition (BUG-015).
func TestASplitDoesNotInheritAnUnappliedConfiguration(t *testing.T) {
	m, err := New(Config{
		ID: 1, Peers: []raft.NodeID{1, 2, 3}, Ordinal: 0,
		Election: 10, Heartbeat: 3, SyncLatency: clock.Instant(1),
		Transport: nullTransport{}, Ledger: raftcheck.NewLedger(3),
		Clock: mustSimClock(t),
		Nodes: 3, SplitThreshold: 0,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	left := m.replicaOf(FirstRange)
	if left == nil {
		t.Fatal("the machine was born without its first range")
	}

	// A leader in term 1 appends the entry the split will be applied at.
	if err := left.raft.Step(raft.Message{
		Type: raft.MsgApp, From: 2, To: 1, Term: 1, PrevLogIndex: 0, PrevLogTerm: 0,
		Entries: []raft.Entry{{Index: 1, Term: 1, Type: raft.EntryNormal, Data: []byte("x")}},
	}); err != nil {
		t.Fatalf("append at 1: %v", err)
	}

	// # The divergent tail
	//
	// A configuration entry ABOVE the split's index, appended and NOT applied.
	// This is the half a seed search kept failing to pair with the other one.
	cc := raft.EncodeConfChange(raft.ConfChangeV2{
		Transition: raft.ConfChangeSimple,
		Changes:    []raft.ConfChangeSingle{{Type: raft.ConfChangeRemoveNode, Node: 3}},
	})
	if err := left.raft.Step(raft.Message{
		Type: raft.MsgApp, From: 2, To: 1, Term: 1, PrevLogIndex: 1, PrevLogTerm: 1,
		Entries: []raft.Entry{{Index: 2, Term: 1, Type: raft.EntryConfChange, Data: cc}},
	}); err != nil {
		t.Fatalf("append the conf entry at 2: %v", err)
	}

	atIndex, err := left.raft.ConfigurationAt(1)
	if err != nil {
		t.Fatalf("configuration at the split's index: %v", err)
	}
	active := left.raft.Configuration()

	// The premise of the whole test. If these agree, the tail is not divergent
	// and the test is asserting nothing -- the vacuity check this repository
	// puts in front of every claim.
	if active.Equal(atIndex) {
		t.Fatalf("the active configuration and the one at the split's index are the same (%s vs "+
			"%s), so this test arranged no divergence and everything below "+
			"it would pass vacuously", active, atIndex)
	}

	// Apply the split AT INDEX 1, below the configuration entry.
	spec := SplitSpec{
		Key:   []byte("c"),
		Left:  RangeDescriptor{ID: FirstRange, Start: nil, End: []byte("c"), Epoch: 2},
		Right: RangeDescriptor{ID: 2, Start: []byte("c"), End: nil, Epoch: 1},
	}
	m.applySplit(left, spec, 1, 0, nullScheduler{})

	right := m.replicaOf(2)
	if right == nil {
		t.Fatal("the split produced no right range")
	}
	born := right.raft.Configuration()

	if !born.Equal(atIndex) {
		t.Errorf("the right range was born with configuration %s; the log at the split's index "+
			"says %s.\n"+
			"  The ACTIVE configuration is effective on append and is not a function of the "+
			"applied prefix, so two replicas applying this same entry with different appended "+
			"tails would give this range two different birth configurations -- and the one that "+
			"started behind refuses the next membership entry as an illegal transition "+
			"(BUG-015). Everything a split derives has to be derived at the split's position.",
			born, atIndex)
	}
	if born.Equal(active) {
		t.Errorf("the right range inherited the ACTIVE configuration %s, which includes a change "+
			"appended above the split and applied by nobody", active)
	}
}
