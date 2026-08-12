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
	setFlags(t, map[string]string{"core": "core,coreclean", "exclude": "monocore", "mono-pkg": "monocore", "closed-enums": "monocore.Outcome"})

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

// TestHatchesCannotSanctionConcurrency covers the one line the escape hatch
// does not cross. Every hatch in the fixture is well-formed and carries a
// reason; every one is refused, and refusing consumes the hatch so the author
// is not also told their annotation excused nothing.
func TestHatchesCannotSanctionConcurrency(t *testing.T) {
	sink := captureAllowances(t)
	setFlags(t, map[string]string{"core": "hardrules"})

	analysistest.Run(t, analysistest.TestData(), Analyzer, "hardrules")

	if n := strings.Count(sink.String(), "REFUSED"); n != 5 {
		t.Errorf("got %d REFUSED announcements, want 5 (sync, atomic, chan, go, select):\n%s", n, sink)
	}
	if strings.Contains(sink.String(), "ALLOWED") {
		t.Errorf("a hard rule was excused:\n%s", sink)
	}
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
	} {
		if !strings.Contains(out, want) {
			t.Errorf("announcement missing %q; got:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "ALLOWED"); n != 4 {
		t.Errorf("got %d ALLOWED announcements, want 4 (two in allowfile, two in allowline):\n%s", n, out)
	}
	// One HATCH line per declared hatch, used or not: that stream is what
	// HATCHES.txt is diffed against, so it has to be complete rather than
	// merely correct about the ones that fired.
	if n := strings.Count(out, "HATCH "); n != 4 {
		t.Errorf("got %d HATCH declarations, want 4 (one in allowfile, three in allowline):\n%s", n, out)
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
		"9: escape: this escape hatch excused nothing",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d diagnostics %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("diagnostic %d = %q, want prefix %q", i, got[i], want[i])
		}
	}
	// The two malformed hatches are not hatches at all, so exactly one is
	// declared and lands in the registry stream.
	if n := strings.Count(sink.String(), "HATCH "); n != 1 {
		t.Errorf("got %d HATCH declarations, want 1:\n%s", n, sink)
	}
}

// TestScopeTable pins the package classification itself, which is the part of
// this pass that decides whether any of the rest of it runs.
// TestSynthesizedTestMainIsExcluded pins both directions of the exclusion. The
// generated test binary main imports os and flag and is nobody's code to fix; a
// source directory that happens to be named x.test is somebody's code and stays
// in scope.
func TestSynthesizedTestMainIsExcluded(t *testing.T) {
	const mod = "github.com/anshkanyadi/rift/"
	cases := []struct {
		path, pkgName string
		want          bool
	}{
		{mod + "raft.test", "main", true},               // the generated one
		{mod + "raft/fixtures.test", "fixtures", false}, // a source directory so named
		{mod + "cmd/simctl", "main", false},             // an ordinary command
		{mod + "raft", "raft", false},
	}
	for _, tc := range cases {
		if got := isSynthesizedTestMain(tc.path, tc.pkgName); got != tc.want {
			t.Errorf("isSynthesizedTestMain(%q, %q) = %v, want %v", tc.path, tc.pkgName, got, tc.want)
		}
	}
}

func TestScopeTable(t *testing.T) {
	const mod = "github.com/anshkanyadi/rift/"
	cases := []struct {
		path string
		want scope
	}{
		// In scope: everything that executes during a simulated run.
		{mod + "raft", scopeCore},
		{mod + "raft/quorum", scopeCore},
		{mod + "raft_test", scopeCore},          // an external test package is still the package
		{mod + "raft/fixtures.test", scopeCore}, // a source directory named x.test is somebody's code
		{mod + "raft.test", scopeOff},           // a sibling path, matched by no pattern
		{mod + "kv", scopeCore},
		{mod + "engine", scopeCore},
		{mod + "engine/model", scopeCore},
		{mod + "clock", scopeCore},
		{mod + "sim", scopeCore},
		{mod + "sim/toy", scopeCore},
		{mod + "sim/toy/mutants", scopeCore},
		{mod + "internal/rng", scopeCore},
		{mod + "internal/sorted", scopeCore},

		// A subpackage nobody has classified is in scope, not out of it. That
		// default is the whole point: a new package under engine/ arrives
		// checked, and gets excluded only by someone writing it down.
		{mod + "engine/wherever", scopeCore},

		// Excluded by name: the real-mode adapters that need what the rules
		// forbid, and which do not run inside a simulated run.
		{mod + "engine/real", scopeOff},
		{mod + "engine/pump/poller", scopeOff},

		// Orchestration around runs.
		{mod + "cmd/simctl", scopeOff},
		{mod + "soak", scopeOff},
		{mod + "tools/determinismcheck", scopeOff},

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
