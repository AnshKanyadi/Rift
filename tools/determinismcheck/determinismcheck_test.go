package determinismcheck

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCoreRules runs every core rule against a fixture written in the shape of
// real node logic, and -- the half that matters more -- against a clean fixture
// that must produce nothing. A pass with false positives gets an escape hatch
// pasted over it within a fortnight, at which point it is decoration.
func TestCoreRules(t *testing.T) {
	sink := captureAllowances(t)
	setFlags(t, map[string]string{"core": "core,coreclean"})

	analysistest.Run(t, analysistest.TestData(), Analyzer, "core", "coreclean")

	if sink.Len() != 0 {
		t.Errorf("neither fixture uses the escape hatch, but the pass announced:\n%s", sink)
	}
}

// TestM5WallClock is A0.3's exit gate. DESIGN-A0 §5's mutant table gives
// M5-wall-clock a budget of "immediate" and names this pass as its first
// catcher: the mutant has to die at compile time, before a seed is spent on it.
func TestM5WallClock(t *testing.T) {
	setFlags(t, map[string]string{"core": "m5wallclock"})
	analysistest.Run(t, analysistest.TestData(), Analyzer, "m5wallclock")
}

// TestMailboxRule covers DESIGN-A0 DR-2's second layer. The first is that core
// state is unexported in another package; this is what catches the calls the
// compiler still permits.
func TestMailboxRule(t *testing.T) {
	setFlags(t, map[string]string{"core": "mailboxcore", "mailbox": "mailboxnode"})
	analysistest.Run(t, analysistest.TestData(), Analyzer, "mailboxnode")
}

// TestOutOfScopeIsUntouched runs with the real scope table over a fixture full
// of violations. The simulator, the real clock and the load generators all need
// exactly these constructs; a pass that shouted at them would be switched off.
func TestOutOfScopeIsUntouched(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "offscope")
}

// TestEscapeHatch checks both that a hatch suppresses and that it cannot do so
// quietly. The announcement is the entire mechanism keeping exemptions from
// accumulating, so it is asserted rather than assumed.
func TestEscapeHatch(t *testing.T) {
	sink := captureAllowances(t)
	setFlags(t, map[string]string{"core": "allowfile,allowline"})

	analysistest.Run(t, analysistest.TestData(), Analyzer, "allowfile", "allowline")

	out := sink.String()
	for _, want := range []string{
		"ALLOWED (file)", "exercises the file-level escape hatch",
		"ALLOWED (line)", "hatch on the line above", "trailing hatch",
		"UNUSED (line)", "too far from the call below",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("announcement missing %q; got:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "ALLOWED"); n != 4 {
		t.Errorf("got %d ALLOWED announcements, want 4 (two in allowfile, two in allowline):\n%s", n, out)
	}
}

// TestEscapeHatchMustCarryAReason covers the two ways a hatch fails to be one.
// Both are reported rather than ignored: a hatch that silently does not apply
// leaves its author believing the code is exempt, and nobody finds out until
// the rule fires somewhere unrelated.
func TestEscapeHatchMustCarryAReason(t *testing.T) {
	const src = `package p

//rift:allow-nondeterminism
func a() {}

// rift:allow-nondeterminism spaced out, so it is just a comment
func b() {}

//rift:allow-nondeterminism excuses nothing in this file
func c() {}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var got []string
	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{f},
		Report: func(d analysis.Diagnostic) {
			got = append(got, fmt.Sprintf("%d: %s", fset.Position(d.Pos).Line, d.Message))
		},
	}

	sink := captureAllowances(t)
	newAllowIndex(pass).finish()

	want := []string{
		"3: escape: //rift:allow-nondeterminism requires a written reason",
		"6: escape: malformed escape hatch",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d diagnostics %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("diagnostic %d = %q, want prefix %q", i, got[i], want[i])
		}
	}
	if out := sink.String(); !strings.Contains(out, "UNUSED (line)") || !strings.Contains(out, "excuses nothing") {
		t.Errorf("stale hatch was not announced; got:\n%s", out)
	}
}

// TestScopeTable pins the package classification itself, which is the part of
// this pass that decides whether any of the rest of it runs.
func TestScopeTable(t *testing.T) {
	const mod = "github.com/anshkanyadi/rift/"
	cases := []struct {
		path string
		want scope
	}{
		{mod + "raft", scopeCore},
		{mod + "raft/quorum", scopeCore},
		{mod + "raft_test", scopeCore}, // an external test package is still the package
		{mod + "kv", scopeCore},
		{mod + "engine", scopeCore},
		{mod + "engine/model", scopeCore},
		{mod + "engine/cgo", scopeOff}, // DR-11 puts the poller goroutine here
		{mod + "sim/toy", scopeCore},
		{mod + "sim/toy/mutants", scopeCore},
		{mod + "sim", scopeOff},   // owns the nondeterminism it injects
		{mod + "clock", scopeOff}, // CLAUDE.md exempts the real clock by name
		{mod + "internal/rng", scopeOff},
		{mod + "cmd/simctl", scopeOff},
		{mod + "node", scopeMailbox},
		{mod + "node/transport", scopeMailbox},
		{mod + "raftlike", scopeOff}, // a prefix is not a parent directory
		{"time", scopeOff},
	}
	for _, tc := range cases {
		if got := scopeFor(tc.path); got != tc.want {
			t.Errorf("scopeFor(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// setFlags points the scope table at fixture packages for the duration of one
// test, and puts it back afterwards.
func setFlags(t *testing.T, kv map[string]string) {
	t.Helper()

	old := make(map[string]string)
	Analyzer.Flags.VisitAll(func(f *flag.Flag) { old[f.Name] = f.Value.String() })
	t.Cleanup(func() {
		for name, v := range old {
			if err := Analyzer.Flags.Set(name, v); err != nil {
				t.Errorf("restoring -%s: %v", name, err)
			}
		}
	})

	for name, v := range kv {
		if err := Analyzer.Flags.Set(name, v); err != nil {
			t.Fatalf("setting -%s=%s: %v", name, v, err)
		}
	}
}

func captureAllowances(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := allowanceSink
	allowanceSink = &buf
	t.Cleanup(func() { allowanceSink = old })
	return &buf
}
