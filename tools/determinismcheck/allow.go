package determinismcheck

import (
	"fmt"
	"go/token"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// directive is the escape hatch. It is deliberately ugly and deliberately
// requires a reason: an exemption that is cheap to add and invisible afterwards
// stops being an exemption and becomes the rule.
const directive = "//rift:allow-nondeterminism"

// allowanceSink is where used and unused hatches are announced. A package
// variable so tests can read what CI would have seen.
//
// The mutex is not optional: the analysis driver runs one pass per package
// concurrently, and they all announce through this one writer. Without it the
// announcements interleave, which loses exactly the lines a reviewer is meant
// to be counting.
var (
	allowanceMu   sync.Mutex
	allowanceSink io.Writer = os.Stderr
)

type allowIndex struct {
	pass  *analysis.Pass
	files map[string]*fileAllow
}

type fileAllow struct {
	whole *allowance         // covers the file; nil if absent
	lines map[int]*allowance // covers its own line and the line below
}

type allowance struct {
	reason string
	pos    token.Pos
	uses   int
}

// newAllowIndex collects every directive in the package. Two malformed cases
// are reported rather than ignored, because a hatch that silently fails to
// apply is worse than no hatch: the author believes the code is exempted and
// nobody finds out until the rule fires somewhere unrelated.
func newAllowIndex(pass *analysis.Pass) *allowIndex {
	ai := &allowIndex{pass: pass, files: make(map[string]*fileAllow)}

	for _, f := range pass.Files {
		fa := &fileAllow{lines: make(map[int]*allowance)}
		ai.files[pass.Fset.Position(f.Package).Filename] = fa

		for _, cg := range f.Comments {
			for _, c := range cg.List {
				text := strings.TrimSpace(c.Text)
				if !strings.HasPrefix(text, directive) {
					if isNearMiss(text) {
						pass.Reportf(c.Pos(), "%s: malformed escape hatch; write it exactly as %q with no space after the slashes, or it is just a comment",
							ruleEscape, directive)
					}
					continue
				}

				reason := strings.TrimSpace(strings.TrimPrefix(text, directive))
				reason = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(reason, ":")), "*/")
				if reason = strings.TrimSpace(reason); reason == "" {
					pass.Reportf(c.Pos(), "%s: %s requires a written reason; an unexplained exemption is indistinguishable from an accident",
						ruleEscape, directive)
					continue
				}

				a := &allowance{reason: reason, pos: c.Pos()}
				if c.End() < f.Package {
					// Above the package clause: the whole file is exempt.
					fa.whole = a
					continue
				}
				fa.lines[pass.Fset.Position(c.Pos()).Line] = a
			}
		}
	}
	return ai
}

// isNearMiss catches "// rift:allow-nondeterminism", which reads exactly like
// the directive to a human and not at all like it to this pass.
func isNearMiss(text string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "/*"))
	t = strings.TrimLeft(t, "/")
	return strings.HasPrefix(strings.TrimSpace(t), strings.TrimLeft(directive, "/"))
}

// suppress reports whether pos is exempted, recording and announcing the use.
// A line-level hatch covers its own line (trailing comment) and the line below
// it (comment on its own line above the code); nothing further, so a hatch
// cannot drift away from what it excuses.
func (ai *allowIndex) suppress(pos token.Pos, rule, msg string) bool {
	p := ai.pass.Fset.Position(pos)
	fa := ai.files[p.Filename]
	if fa == nil {
		return false
	}

	a := fa.whole
	kind := "file"
	if a == nil {
		kind = "line"
		if a = fa.lines[p.Line]; a == nil {
			a = fa.lines[p.Line-1]
		}
	}
	if a == nil {
		return false
	}

	a.uses++
	ai.printf("determinismcheck: ALLOWED (%s) %s:%d:%d: %s: %s -- reason: %s\n",
		kind, p.Filename, p.Line, p.Column, rule, msg, a.reason)
	return true
}

// finish announces hatches that excused nothing. This is a warning rather than
// a diagnostic: the line-level form covers a two-line window, so a stale hatch
// is a housekeeping problem, and failing a build over housekeeping is how a
// rule set loses its welcome. It is still printed on every run, so a reviewer
// deleting dead exemptions has a list.
func (ai *allowIndex) finish() {
	for _, f := range ai.pass.Files {
		name := ai.pass.Fset.Position(f.Package).Filename
		fa := ai.files[name]
		if fa == nil {
			continue
		}
		if fa.whole != nil && fa.whole.uses == 0 {
			ai.warnUnused(fa.whole, "file")
		}
		// Iterate the file's comments rather than the line map: same set,
		// source order, no map range.
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if a := fa.lines[ai.pass.Fset.Position(c.Pos()).Line]; a != nil && a.pos == c.Pos() && a.uses == 0 {
					ai.warnUnused(a, "line")
				}
			}
		}
	}
}

func (ai *allowIndex) warnUnused(a *allowance, kind string) {
	p := ai.pass.Fset.Position(a.pos)
	ai.printf("determinismcheck: UNUSED (%s) %s:%d:%d: escape hatch excused nothing -- reason was: %s\n",
		kind, p.Filename, p.Line, p.Column, a.reason)
}

func (ai *allowIndex) printf(format string, args ...any) {
	if !flagListAllowances {
		return
	}
	allowanceMu.Lock()
	defer allowanceMu.Unlock()
	fmt.Fprintf(allowanceSink, format, args...)
}
