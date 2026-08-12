package determinismcheck

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/ast/inspector"
)

// checkMailbox enforces DESIGN-A0 DR-2 in a driver package. The driver is where
// real mode keeps its goroutines -- transport receive, durability completion,
// timers, client requests -- and the rule is that none of them touch core state
// directly: they post an event to the mailbox and the node loop does the rest.
//
// Enforcement here is deliberately blunter than the rule as written. Rather
// than trying to decide which functions can run on a goroutine, which is not a
// question static analysis answers honestly, no function in the package may
// call anything on a core type except the allowlist. That is stricter than
// necessary and the strictness costs nothing, because the driver has no reason
// to reach further.
//
// Layer one of the three is the compiler: core state is unexported and lives in
// another package, so the driver physically cannot reach it. This is layer two,
// and the -race lane on node/ is layer three.
func checkMailbox(r *reporter, insp *inspector.Inspector) {
	allowed := splitPatterns(flagMailboxAllow)

	// The monotonic-leakage rule applies here too. A driver package is exactly
	// where a struct that goes on the wire tends to live, and a Mono is
	// meaningful only on the node and boot that produced it.
	insp.Preorder([]ast.Node{(*ast.StructType)(nil), (*ast.BinaryExpr)(nil)}, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.StructType:
			checkMonoLeak(r, n)
		case *ast.BinaryExpr:
			checkInstantMath(r, n)
		}
	})

	insp.Preorder([]ast.Node{(*ast.SelectorExpr)(nil)}, func(n ast.Node) {
		sel := n.(*ast.SelectorExpr)
		selection := r.pass.TypesInfo.Selections[sel]
		if selection == nil {
			// A qualified identifier: pkg.Func or pkg.Type. Constructors are
			// exactly this shape and are permitted -- building a node is not
			// touching one.
			return
		}
		switch selection.Kind() {
		case types.MethodVal, types.MethodExpr:
		default:
			// A field selection on a core type would be a much louder problem,
			// but it cannot happen: core state is unexported and this package
			// is not that package.
			return
		}

		obj := selection.Obj()
		if obj.Pkg() == nil || !isCorePkg(obj.Pkg().Path()) {
			return
		}
		if slices.Contains(allowed, obj.Name()) {
			return
		}
		r.report(sel.Sel.Pos(), ruleMailbox,
			"%s.%s is core state reached off the node loop; a driver may only call %s, and everything else goes through the mailbox (DESIGN-A0 DR-2)",
			types.TypeString(selection.Recv(), relativeTo(r)), obj.Name(), strings.Join(allowed, " or "))
	})

	// The mailbox itself has exactly one writer. Any other send is a second
	// path into the node, which is the whole thing this rule exists to prevent.
	insp.WithStack([]ast.Node{(*ast.SendStmt)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		if !push {
			return false
		}
		if fn := enclosingFunc(stack); fn == nil || fn.Name.Name != flagPostFunc {
			r.report(n.Pos(), ruleMailbox,
				"channel sends in a driver package are allowed only in %s(), which is the single writer of the node mailbox (DESIGN-A0 DR-2)",
				flagPostFunc)
		}
		return true
	})
}

// enclosingFunc walks out to the nearest declared function, so a send inside a
// closure is attributed to the function that declares the closure rather than
// escaping the rule by being anonymous.
func enclosingFunc(stack []ast.Node) *ast.FuncDecl {
	for i := len(stack) - 1; i >= 0; i-- {
		if fn, ok := stack[i].(*ast.FuncDecl); ok {
			return fn
		}
	}
	return nil
}

func relativeTo(r *reporter) types.Qualifier {
	return types.RelativeTo(r.pass.Pkg)
}
