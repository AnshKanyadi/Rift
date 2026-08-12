package determinismcheck

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"github.com/anshkanyadi/rift/internal/sorted"
	"golang.org/x/tools/go/ast/inspector"
)

// checkExhaustive requires every switch over a closed enum to cover all of its
// variants and to carry no default arm.
//
// Go has no exhaustive switch, so a new variant silently falls through every
// existing switch -- and the consumer that matters here is the one deciding
// which runs count toward SOAK.md's cumulative hours. A default arm is the
// specific danger: it makes the omission invisible forever, and the ledger's
// honesty was the whole reason the enum was closed (DESIGN-A0.6).
//
// The rule is deliberately not scoped to core packages. The soak runner is
// orchestration, exempt from every determinism rule, and it is exactly where an
// unhandled outcome would be banked as an hour.
func checkExhaustive(r *reporter, insp *inspector.Inspector) {
	enums := splitPatterns(flagClosedEnums)
	if len(enums) == 0 {
		return
	}

	insp.Preorder([]ast.Node{(*ast.SwitchStmt)(nil)}, func(n ast.Node) {
		sw := n.(*ast.SwitchStmt)
		if sw.Tag == nil {
			return // a tagless switch is a chain of conditions, not a dispatch
		}
		named, ok := r.pass.TypesInfo.TypeOf(sw.Tag).(*types.Named)
		if !ok {
			return
		}
		obj := named.Obj()
		if obj == nil || obj.Pkg() == nil {
			return
		}
		full := obj.Pkg().Path() + "." + obj.Name()
		if !slices.Contains(enums, full) {
			return
		}

		covered := make(map[string]bool)
		hasDefault := false
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			if clause.List == nil {
				hasDefault = true
				r.report(clause.Pos(), ruleExhaustive,
					"a switch over the closed enum %s may not have a default arm; a default makes an unhandled variant invisible forever, which is exactly what adding one has to break", full)
				continue
			}
			for _, expr := range clause.List {
				if id, ok := r.pass.TypesInfo.Uses[identOf(expr)]; ok && id != nil {
					covered[id.Name()] = true
				}
			}
		}

		var missing []string
		for _, name := range variantsOf(obj) {
			if !covered[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 && !hasDefault {
			r.report(sw.Pos(), ruleExhaustive,
				"switch over the closed enum %s does not handle %s; a new variant has to break every consumer that has not decided what to do about it",
				full, strings.Join(missing, ", "))
		}
	})
}

// identOf reduces a case expression to the identifier naming a constant,
// looking through the pkg.Name qualifier.
func identOf(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

// variantsOf lists the exported constants declared with the enum's type, in
// sorted order so a diagnostic reads the same on every run.
func variantsOf(obj types.Object) []string {
	scope := obj.Pkg().Scope()
	found := make(map[string]bool)
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok {
			continue
		}
		if named, ok := c.Type().(*types.Named); ok && named.Obj() == obj {
			found[name] = true
		}
	}
	return sorted.Keys(found)
}
