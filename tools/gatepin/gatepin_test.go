package gatepin_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Package gatepin_test pins DR-8's durability-gate enumeration.
//
// It lives under tools/ rather than in raft/ because it reads the source text,
// and raft/ is in the determinism pass's core scope where importing os is a
// build failure -- core packages reach the outside world only through injected
// interfaces. Reading a file to check a contract is tooling, and tooling lives
// here, which is the same reason determinismcheck does.

// TestDurabilityGateSetIsPinned freezes DR-8's enumeration.
//
// The doc comment on Ready.Messages is the freeze surface: it is reproduced
// verbatim from DESIGN-A0 D5's normative amendment, and it is the contract every
// driver in this project is entitled to rely on. Adding or removing a gate
// changes what "persist before reply" means, so it has to be a deliberate act
// with a ruling behind it rather than an edit.
//
// So the set is pinned here. A new gate fails this test until it is added to the
// list below, which is the moment somebody has to justify it.
func TestDurabilityGateSetIsPinned(t *testing.T) {
	want := []string{
		"MsgAppResp (accept)",
		"MsgVoteResp (grant)",
		"MsgVoteResp (reject) and any response emitted after a term bump",
		"MsgAppResp following InstallSnapshot",
		"MsgHeartbeatResp",
		"MsgReadIndexResp (A7)",
	}

	src, err := os.ReadFile("../../raft/raft.go")
	if err != nil {
		t.Fatalf("reading raft.go: %v", err)
	}
	block := docBlock(t, string(src))

	// A gate stanza is a tab-indented line naming a message type, followed by a
	// "gated on:" line. Both halves are required: a gate with no stated
	// dependency is not a gate, it is a note.
	re := regexp.MustCompile(`(?m)^[ \t]*//\t(Msg[^\n]*)\n[ \t]*//\t  gated on: `)
	var got []string
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		got = append(got, strings.TrimSpace(m[1]))
	}

	if len(got) != len(want) {
		t.Fatalf("the gate enumeration has %d entries, pinned at %d.\n  got:  %q\n  want: %q\n"+
			"Changing the gate set changes what persist-before-reply means; it needs a ruling, not an edit.",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gate %d is %q, pinned as %q", i, got[i], want[i])
		}
	}

	// The ungated case is a correctness argument and must stay stated.
	if !strings.Contains(block, "MsgPreVoteResp is deliberately NOT gated") {
		t.Error("the pre-vote non-gate lost its correctness argument; without it a future reader " +
			"cannot tell a deliberate omission from a forgotten one")
	}
	t.Logf("%d gates pinned, plus the pre-vote non-gate", len(got))
}

// TestEveryGateHasACallSite keeps the enumeration and the code from drifting
// apart in the other direction: a gate documented but never applied is exactly
// the family the A0 audit found five of.
func TestEveryGateHasACallSite(t *testing.T) {
	src, err := os.ReadFile("../../raft/raft.go")
	if err != nil {
		t.Fatalf("reading raft.go: %v", err)
	}
	n := strings.Count(string(src), "// GATE:")
	if n == 0 {
		t.Fatal("no call site is marked with a GATE comment, so the enumeration describes nothing")
	}
	// Every gated send must go through sendGated; a plain send of a message that
	// attests to persistent state would be the whole bug.
	if got := strings.Count(string(src), "r.sendGated("); got < n {
		t.Errorf("%d GATE comments but only %d sendGated call sites", n, got)
	}
	t.Logf("%d gated call sites", n)
}

func docBlock(t *testing.T, src string) string {
	t.Helper()
	i := strings.Index(src, "// # Durability gating -- normative")
	if i < 0 {
		t.Fatal("the normative gating block is gone from Ready.Messages; that block IS the contract")
	}
	j := strings.Index(src[i:], "\tMessages []Message")
	if j < 0 {
		t.Fatal("could not find the end of the Messages doc block")
	}
	return src[i : i+j]
}
