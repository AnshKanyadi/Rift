package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestBUG021 pins the seed that found it.
//
// # What it looked like
//
// Txn 14 (primary a05, keys a05+a01) and txn 29 (primary a07, keys a07+a05)
// were minted at the same start timestamp `1600000005840000000.26` by two
// different nodes, and both wrote a05. A key's lock owner, its data version
// (`EncodeKey(ns, key, startTS)`) and its write record are all addressed by the
// start timestamp, so for a05 the two transactions shared every one of them.
// Txn 14 committed; txn 29 was rolled back; the version at `.26` belonged to
// both, and `transaction-atomicity` said so.
//
// # Both halves are asserted, because a partial fix passes half of this
//
// The node tag stops two nodes minting the same value. Minting the restart
// timestamp instead of deriving it stops `RestartAt = commit.Next()` handing a
// transaction another node's tag. A tree with only the first still collides on
// restarts, and would pass any test that only looked at collisions on this
// particular seed -- so `store.TestARestartTimestampIsMintedNotDerived` covers
// the second directly, and this one covers the schedule that found the bug.
func TestBUG021(t *testing.T) {
	p, err := hunt.MaterializeRaft(90004)
	if err != nil {
		t.Fatal(err)
	}
	r, err := hunt.RunRaft(p, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.IdentityCollisions != 0 {
		t.Errorf("%d transactions shared a start timestamp. The timestamp source is supposed to "+
			"make that impossible: the low %d bits of Logical carry the minting node's ordinal, "+
			"and a restart mints rather than deriving", r.IdentityCollisions, 8)
	}
	if r.Violated != nil {
		t.Errorf("seed 90004 [%s]: %s", r.Violated.Checker, r.Violated.Detail)
	}
	if r.ForeignTagStarts != 0 {
		t.Errorf("%d start timestamps carried a node tag other than the node that was asked for "+
			"them. A derived restart timestamp is the way that happens, and it is BUG-021 one "+
			"level out", r.ForeignTagStarts)
	}
}

// TestRestartsMintTheirOwnStartTimestamp is M68's real covering test.
//
// # Why TestBUG021 could not be it
//
// M68 makes a restarting transaction adopt `RestartAt` rather than minting above
// it, and `RestartAt` carries the tag of whoever minted the commit that caused
// the restart. Seed 90004 found BUG-021 and does not happen to restart, so it
// SURVIVED there. The class needs a schedule that actually restarts, and the
// assertion is the property rather than the seed: every start timestamp carries
// the tag of the node the client asked for it.
func TestRestartsMintTheirOwnStartTimestamp(t *testing.T) {
	restarts, foreign := 0, 0
	for seed := uint64(0); seed < boundSeeds(60); seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		restarts += r.UncertaintyRestarts
		foreign += r.ForeignTagStarts
	}
	if foreign != 0 {
		t.Errorf("%d start timestamps carried another node's tag across %d uncertainty restarts. "+
			"A restart that adopts RestartAt instead of minting above it hands the transaction "+
			"the identity of whichever node minted the commit it restarted on", foreign, restarts)
	}
	// Non-vacuity: the property is about restarts, so the sweep has to contain
	// some. Without this the test passes on a workload that never restarts,
	// which is exactly how M68 survived its first covering test.
	if restarts == 0 {
		t.Error("no uncertainty restart occurred across the sweep, so this test asserted nothing " +
			"about the path it exists for")
	}
	t.Logf("%d uncertainty restarts, %d foreign tags", restarts, foreign)
}
