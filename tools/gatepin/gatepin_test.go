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
// # RULINGS ON THIS SET
//
// A gate set changed without a ruling written beside it is the drift this pin
// was built to prevent, so the rulings live in the file that enforces them.
//
//	**2026-08-26, Ansh, ratifying MsgReadIndex as a durability gate (6 -> 7).**
//	"MsgReadIndex joining the durability-gated set: ratified. BUG-027 is the
//	justification and flagging it as a gate-set change rather than burying it is
//	why gatepin exists."
//
//	BUG-027: both read-index message types went out through `send()`, which
//	"releases a message that attests to no persistent state", each carrying
//	`Term: r.term`. 118 of 25,000 seeds, caught by persist-before-reply -- an A1
//	oracle catching an A7 wire. The stanza that stood for MsgReadIndexResp
//	reasoned about the PAYLOAD ("a read index attests to a commit index, which is
//	already durable") and was true about it, and the message carried a second
//	attestation the argument never reached.
//
//	The set grew by one because the REQUEST is a separate gate with a separate
//	failure: a follower forwarding a read advertises its own term, inside the
//	window between its term bump and that term reaching disk. The response is
//	sent by a leader, whose term is necessarily durable, and it is gated anyway
//	-- belt-and-braces, and stated as such rather than left to look load-bearing.
func TestDurabilityGateSetIsPinned(t *testing.T) {
	want := []string{
		"MsgAppResp (accept)",
		"MsgVoteResp (grant)",
		"MsgVoteResp (reject) and any response emitted after a term bump",
		"MsgAppResp following InstallSnapshot",
		"MsgHeartbeatResp",
		"MsgReadIndex (A7)",
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

	// The ungated cases are correctness arguments and must stay stated. They are
	// pinned individually rather than counted, because the failure mode is one
	// of them quietly losing its argument while the others still have theirs.
	nonGates := []string{
		"MsgPreVoteResp is deliberately NOT gated",
		"MsgTimeoutNow is deliberately NOT gated",
	}
	for _, ng := range nonGates {
		if !strings.Contains(block, ng) {
			t.Errorf("the non-gate %q lost its correctness argument; without it a future reader "+
				"cannot tell a deliberate omission from a forgotten one", ng)
		}
	}
	t.Logf("%d gates pinned, plus %d stated non-gates", len(got), len(nonGates))
}

// TestEveryGateHasACallSite keeps the enumeration and the code from drifting
// apart in the other direction: a gate documented but never applied is exactly
// the family the A0 audit found five of.
func TestEveryGateHasACallSite(t *testing.T) {
	src, err := os.ReadFile("../../raft/raft.go")
	if err != nil {
		t.Fatalf("reading raft.go: %v", err)
	}
	text := string(src)
	n := strings.Count(text, "// GATE:")
	if n == 0 {
		t.Fatal("no call site is marked with a GATE comment, so the enumeration describes nothing")
	}

	// Every gated send must go through a gating helper. A plain r.send() of a
	// message that attests to persistent state would be the whole bug, so the
	// check is per call site rather than a count: for each GATE comment, the
	// first send that follows it must be a withholding one.
	//
	// Both helpers count. sendGated withholds against whatever the current Step
	// made dirty; sendGatedOn withholds against a named mark, which is what an
	// append response needs -- it attests to the entries it acks as well as to
	// the term it carries, and those are separate gates in the enumeration
	// above. Counting only sendGated would have made the stronger call site look
	// like a missing one.
	send := regexp.MustCompile(`r\.send(Gated|GatedOn|)\(`)
	rest := text
	for i := 0; i < n; i++ {
		k := strings.Index(rest, "// GATE:")
		rest = rest[k+len("// GATE:"):]
		m := send.FindStringSubmatchIndex(rest)
		if m == nil {
			t.Fatalf("GATE comment %d is followed by no send at all", i+1)
		}
		if kind := rest[m[2]:m[3]]; kind == "" {
			t.Errorf("GATE comment %d is discharged by a plain r.send(), which releases the message "+
				"immediately; a gate that does not withhold is a comment", i+1)
		}
	}
	t.Logf("%d gated call sites, each discharged by a withholding send", n)
}

// TestEveryEnumeratedGateIsMarkedAtItsCallSite is the OTHER direction, and it is
// the one that was missing when BUG-027 happened.
//
// TestEveryGateHasACallSite walks the `// GATE:` comments and requires each to
// be discharged by a withholding send. That check is keyed on the COMMENT, so a
// gate listed in the enumeration with no comment anywhere is invisible to it --
// which is exactly what MsgReadIndexResp was. It was in the pinned set, its
// stanza said "gated on: a leadership-confirming quorum, *not* durability", its
// call site used a plain `r.send`, and no test in the tree connected those three
// facts.
//
// So this walks the ENUMERATION and requires each entry to have a marked call
// site. Two tests, opposite directions, and the pair is the invariant: the set
// of documented gates and the set of marked gated sends are the same set.
func TestEveryEnumeratedGateIsMarkedAtItsCallSite(t *testing.T) {
	src, err := os.ReadFile("../../raft/raft.go")
	if err != nil {
		t.Fatalf("reading raft.go: %v", err)
	}
	text := string(src)

	// The types that actually have a marked, withholding call site: for each
	// GATE comment, the Type: field of the message literal that follows it.
	gatedType := regexp.MustCompile(`(?s)// GATE:.*?r\.sendGated(?:On)?\(Message\{\s*(?:[^}]*?)Type: (Msg\w+)`)
	marked := map[string]bool{}
	for _, m := range gatedType.FindAllStringSubmatch(text, -1) {
		marked[m[1]] = true
	}
	if len(marked) == 0 {
		t.Fatal("no GATE comment is followed by a typed withholding send; this test is not " +
			"reading the file it thinks it is")
	}

	// A stanza title is "MsgFoo (something)" or just "MsgFoo"; the gate is about
	// the type, and several stanzas can share one.
	title := regexp.MustCompile(`(?m)^[ 	]*//	(Msg\w+)[^
]*
[ 	]*//	  gated on: `)
	block := docBlock(t, text)

	// Exemptions are BY NAME and carry a reason, the way every other exemption
	// in this repo does.
	noSuchType := map[string]string{
		"MsgHeartbeatResp": "heartbeats are MsgApp with no entries in this implementation, so " +
			"there is no MsgHeartbeatResp literal to mark; the gate is discharged at the MsgApp " +
			"and MsgAppResp sites and the stanza says so",
	}

	seen := map[string]bool{}
	for _, m := range title.FindAllStringSubmatch(block, -1) {
		typ := m[1]
		if seen[typ] {
			continue
		}
		seen[typ] = true
		if _, exempt := noSuchType[typ]; exempt {
			continue
		}
		if !marked[typ] {
			t.Errorf("%s is enumerated as a durability gate and has no marked call site: no "+
				"`// GATE:` comment in raft.go is followed by a withholding send of that type.\n"+
				"  A gate that is documented and not applied is the shape of BUG-027: the "+
				"enumeration said the message was gated, the call site used a plain r.send(), "+
				"and the test that walks GATE comments could not see a gate that had none.", typ)
		}
	}
	t.Logf("%d enumerated gate types, %d with marked call sites, %d exempt by name",
		len(seen), len(marked), len(noSuchType))
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
