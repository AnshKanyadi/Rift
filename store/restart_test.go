package store_test

import (
	"testing"

	"github.com/anshkanyadi/rift/hlc"
)

// TestARestartTimestampIsMintedNotDerived is the half of option A that is easy
// to leave out.
//
// # Why it needs its own induction
//
// A partial implementation of the node tag looks exactly like a complete one.
// Every timestamp a node MINTS carries its own ordinal, every test of minting
// passes, and the property holds — right up until a transaction restarts.
//
// `RestartAt` is `observedCommit.Next()`: Logical plus one. Logical's low bits
// are the tag, so the successor of a commit minted by node 4 is tagged 5. A
// transaction that adopts it as its start timestamp is now identified by a
// number belonging to another node's sequence, and BUG-021 returns one level
// out — two transactions at one start timestamp, sharing a key's lock and its
// version.
//
// So the test asserts the failure mode directly (the derived value carries a
// foreign tag) and then that the minted value does not.
func TestARestartTimestampIsMintedNotDerived(t *testing.T) {
	const me, them = uint32(3), uint32(4)

	// What a restart is told to restart above: the successor of a commit some
	// other node minted.
	theirCommit := hlc.Timestamp{Wall: 1000, Logical: 8<<hlc.IDBits | them}
	restartAt := theirCommit.Next()

	// The failure mode, asserted rather than described. If this ever stops being
	// true the derivation has become safe and this test should say so loudly
	// rather than keep passing for a reason that no longer holds.
	if got := hlc.NodeOf(restartAt); got == me {
		t.Fatalf("the successor of node %d's commit is tagged %d, which happens to be this node's "+
			"— pick a different pair, the test is not exercising the collision", them, got)
	}
	if got := hlc.NodeOf(restartAt); got != them+1 {
		t.Errorf("Next() moved the tag from %d to %d, not %d; the arithmetic this test is built on "+
			"has changed", them, got, them+1)
	}

	// What minting gives instead: above the bound, and tagged as ours.
	phys := &wallAt{w: 1000}
	c, err := hlc.New(phys, me)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Update(restartAt); err != nil {
		t.Fatalf("absorbing the bound: %v", err)
	}
	minted := c.Now()

	if !restartAt.Less(minted) {
		t.Errorf("minted %s, which is not strictly above the bound %s. An uncertainty restart that "+
			"does not clear the value it restarted on restarts into the same uncertainty",
			minted, restartAt)
	}
	if got := hlc.NodeOf(minted); got != me {
		t.Errorf("minted %s tagged %d, want this node's %d. A start timestamp carrying another "+
			"node's tag is BUG-021 one level out: the tag stops identifying the minter, and two "+
			"transactions can share it", minted, got, me)
	}
}
