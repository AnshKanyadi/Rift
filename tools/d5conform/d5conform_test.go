// Package d5conform_test pins the frozen interfaces of DESIGN-A0 §D5 against the
// implementation's exported surface.
//
// # Why this exists: twice is a missing mechanism
//
// A1 implemented an interface the frozen design explicitly rejected, twice, and
// both times the omission became the defect:
//
//	Advance() instead of the gated queue     caught in review, before code ran
//	Propose() without ProposalID             caught only after it produced
//	                                         BUG-004, which took a full ledger
//	                                         diagnosis to attribute
//
// Once is a slip. Twice is a missing mechanism. A divergence from a frozen
// interface must be a deliberate act with a ruling behind it, not something
// discovered downstream by a linearizability violation.
//
// It lives under tools/ for gatepin's reason: it reads source text, and raft/ is
// in the determinism pass's core scope where importing os is a build failure.
// Reading a file to check a contract is orchestration.
package d5conform_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// frozen is DESIGN-A0 §D5's Node interface, verbatim from line 388's block.
//
// Each entry is a method name and the exact signature the design froze,
// normalized to the implementation's receiver-free form. A method the design
// froze that the implementation has not built yet is listed with its phase, so
// "not yet" is distinguishable from "diverged".
var frozen = []struct {
	name  string
	sig   string
	phase string // empty means it must exist now
}{
	{name: "Tick", sig: "()"},
	{name: "Step", sig: "(m Message) error"},
	{name: "Propose", sig: "(id ProposalID, data []byte) error"},
	{name: "HasReady", sig: "() bool"},
	{name: "Ready", sig: "() Ready"},
	{name: "AckPersisted", sig: "(m PersistMark)"},
	{name: "AckApplied", sig: "(index Index)"},

	// Frozen but not yet built. Listed so the pin is complete: a reader can see
	// the whole contract, and building one of these to a different shape fails
	// here rather than downstream.
	{name: "ProposeConfChange", sig: "(id ProposalID, cc ConfChangeV2) error", phase: "A3"},
	{name: "ReadIndex", sig: "(ctx []byte) error", phase: "A7"},
	{name: "TransferLeadership", sig: "(target NodeID)", phase: "A2"},
	{name: "Campaign", sig: "() error", phase: "A2"},
	{name: "Status", sig: "() Status", phase: "A2"},
}

// TestD5FrozenSignatures fails when the implementation's exported surface
// diverges from what D5 froze.
func TestD5FrozenSignatures(t *testing.T) {
	got := methodsOf(t, "../../raft/raft.go", "Raft")

	for _, f := range frozen {
		have, ok := got[f.name]
		if !ok {
			if f.phase != "" {
				t.Logf("not yet built: %s%s (frozen, lands in %s)", f.name, f.sig, f.phase)
				continue
			}
			t.Errorf("FROZEN METHOD MISSING: raft.Raft has no %s.\n"+
				"  DESIGN-A0 D5 freezes: %s%s\n"+
				"  A frozen interface is not a suggestion; dropping one is how Propose lost its "+
				"ProposalID and produced BUG-004.", f.name, f.name, f.sig)
			continue
		}
		if have != f.sig {
			t.Errorf("FROZEN SIGNATURE DIVERGED: raft.Raft.%s\n"+
				"  frozen (DESIGN-A0 D5): %s%s\n"+
				"  implemented:           %s%s\n"+
				"  Diverging from a frozen interface must be a deliberate act with a ruling behind "+
				"it. Twice now the omission has been the defect.", f.name, f.name, f.sig, f.name, have)
		}
	}

	// The rejected shapes, pinned as absences. DR-7 struck Advance by name; its
	// return would silently restore the ordering obligation on every driver.
	for _, banned := range []struct{ name, why string }{
		{"Advance", "DR-7 struck the global Advance: it serializes fsync against replication and " +
			"pushes persist-before-reply onto the driver, which is the rule raft/ exists to discharge"},
	} {
		if _, ok := got[banned.name]; ok {
			t.Errorf("REJECTED SHAPE PRESENT: raft.Raft.%s exists. %s", banned.name, banned.why)
		}
	}
	t.Logf("%d frozen methods checked", len(frozen))
}

// methodsOf returns exported methods on the named receiver, as name -> signature.
func methodsOf(t *testing.T, path, recv string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	out := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		if recvName(fn.Recv.List[0].Type) != recv {
			continue
		}
		if !fn.Name.IsExported() {
			continue
		}
		out[fn.Name.Name] = render(fset, src, fn.Type)
	}
	return out
}

func recvName(e ast.Expr) string {
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// render prints a signature as "(params) results", by slicing the source between
// the parameter list and the end of the declaration's type.
func render(fset *token.FileSet, src []byte, ft *ast.FuncType) string {
	lo := fset.Position(ft.Params.Pos()).Offset
	hi := fset.Position(ft.End()).Offset
	return strings.TrimSpace(string(src[lo:hi]))
}
