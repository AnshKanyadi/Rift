package determinismcheck

import (
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// directive is the escape hatch. It is deliberately ugly and deliberately
// requires a reason: an exemption that is cheap to add and invisible afterwards
// stops being an exemption and becomes the rule.
const directive = "//rift:allow-nondeterminism"

// allowanceSink is where hatch activity is announced. A package variable so
// tests can read what CI would have seen.
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
	decls []*allowance       // every hatch in the file, in source order
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
				fa.decls = append(fa.decls, a)
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

// covering returns the hatch that applies at pos, if any. A line-level hatch
// covers its own line (trailing comment) and the line below it (comment on its
// own line above the code); nothing further, so a hatch cannot drift away from
// what it excuses.
func (ai *allowIndex) covering(pos token.Pos) (*allowance, string) {
	p := ai.pass.Fset.Position(pos)
	fa := ai.files[p.Filename]
	if fa == nil {
		return nil, ""
	}
	if fa.whole != nil {
		return fa.whole, "file"
	}
	if a := fa.lines[p.Line]; a != nil {
		return a, "line"
	}
	if a := fa.lines[p.Line-1]; a != nil {
		return a, "line"
	}
	return nil, ""
}

// suppress reports whether pos is exempted, recording and announcing the use.
func (ai *allowIndex) suppress(pos token.Pos, rule, msg string) bool {
	a, kind := ai.covering(pos)
	if a == nil {
		return false
	}
	a.uses++
	ai.announce("ALLOWED", kind, pos, rule+": "+msg+" -- reason: "+a.reason)
	return true
}

// refuse consumes a hatch that covers an unhatchable rule and reports whether
// one was there. The hatch is marked used so the author gets the one diagnostic
// that matters -- the rule itself -- rather than that plus a confusing
// complaint that their hatch excused nothing.
func (ai *allowIndex) refuse(pos token.Pos, rule, msg string) bool {
	a, kind := ai.covering(pos)
	if a == nil {
		return false
	}
	a.uses++
	ai.announce("REFUSED", kind, pos, rule+": "+msg+" -- refused reason: "+a.reason)
	return true
}

// finish announces every declared hatch, which is what HATCHES.txt is diffed
// against, and fails any that excused nothing.
//
// Unused hatches are a diagnostic rather than a warning, ruled 2026-08-11:
// warnings rot and nobody reads CI warnings. A hatch that excuses nothing is
// either a rule that has been fixed -- delete the hatch -- or a hatch that has
// drifted off the line it was written for, which means something is now
// unexcused and unnoticed.
func (ai *allowIndex) finish() {
	for _, f := range ai.pass.Files {
		fa := ai.files[ai.pass.Fset.Position(f.Package).Filename]
		if fa == nil {
			continue
		}
		for _, a := range fa.decls {
			p := ai.pass.Fset.Position(a.pos)
			ai.printf("determinismcheck: HATCH %s:%d  %s\n", displayPath(p.Filename), p.Line, a.reason)
			if a.uses == 0 {
				ai.pass.Reportf(a.pos,
					"%s: this escape hatch excused nothing; delete it, or move it onto the line it was written for", ruleEscape)
			}
		}
	}
}

func (ai *allowIndex) announce(verdict, kind string, pos token.Pos, detail string) {
	p := ai.pass.Fset.Position(pos)
	ai.printf("determinismcheck: %s (%s) %s:%d:%d: %s\n", verdict, kind, displayPath(p.Filename), p.Line, p.Column, detail)
}

func (ai *allowIndex) printf(format string, args ...any) {
	if !flagListAllowances {
		return
	}
	allowanceMu.Lock()
	defer allowanceMu.Unlock()
	fmt.Fprintf(allowanceSink, format, args...)
}

// displayPath renders a file relative to the repo root, so announcements are
// stable enough to diff against a checked-in registry and short enough to read
// in a build log.
func displayPath(name string) string {
	root := flagRoot
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return name
		}
		root = wd
	}
	rel, err := filepath.Rel(root, name)
	if err != nil || strings.HasPrefix(rel, "..") {
		return name
	}
	return rel
}
