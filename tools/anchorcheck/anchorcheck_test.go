package anchorcheck_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Package anchorcheck_test requires every mutant patch to anchor on CODE.
//
// # The defect this exists for, and it is not hypothetical
//
// `M77-a-snapshot-read-is-served-by-read-index` was disarmed by an edit that
// changed no behavior at all. Its hunk anchored on three comment lines:
//
//	 // D-A7-4, ruled: BOTH paths stay for the phase. The replicated path is the
//	 // differential oracle's other half, and a differential between them is the
//	 // only instrument that can catch a stale read no client observed.
//	-if n.cfg.ReadIndex && req.Op == "get" && !req.ReadTS.IsSet() && req.Txn == nil {
//
// A7 rewrote that comment. The line the patch MUTATES is byte-identical to this
// day, and its three trailing context lines are byte-identical too. The prose
// above it moved, and the mutant stopped applying.
//
// `power-refute` reported it as *"the code moved and the mutation did not"*,
// which is false, and which sends a reader looking for a behavioral change that
// never happened. That message is now split (scripts/power-refute.sh) precisely
// because the two causes need different remedies.
//
// # The general form, which is a standing rule
//
// **A MUTANT ANCHORED ON COMMENT LINES IS DISARMED BY PROSE EDITS THAT CHANGE NO
// BEHAVIOR.**
//
// In a repository whose comments carry the arguments, prose edits are constant.
// That makes this a standing disarming mechanism rather than an incident: every
// time an argument is sharpened, some patch somewhere silently stops applying,
// and nothing says so until a lane nobody can afford to run gets run.
//
// # And A7 learned the sibling of this one axis over
//
// `M80`'s lesson was that a patch must not MUTATE comment lines, because
// coverage never marks them, so `mutant-covered` could never answer for it. This
// is the same mistake on the matching side rather than the changing side: not
// what the patch replaces, but what it anchors on. **A rule about what a patch
// may replace did not generalize to what it may anchor on** -- which is the
// wrap-up's own siblings rule paying out again.
//
// # The threshold is MEASURED, and the measurement is the whole argument
//
// The obvious rule is "no comment may appear in any context line". Measured over
// the catalogue, that flags **47 of 71** -- two thirds of it -- and most of what
// it names is not a risk, because `patch(1)` has fuzz.
//
// So fuzz was measured rather than read off the man page. A file with N all-prose
// leading context lines, the prose then rewritten end to end, the hunk otherwise
// untouched, on this toolchain (`patch 2.0-12u11-Apple`):
//
//	leading all-prose context lines = 1   ->  applies
//	leading all-prose context lines = 2   ->  applies
//	leading all-prose context lines = 3   ->  FAILS      <- M77 exactly
//	one INTERIOR prose context line       ->  FAILS
//
// Fuzz trims a hunk's EDGES, up to two lines. It never trims the interior. So the
// unrecoverable cases are exactly two, and they are this rule:
//
//  1. **An all-prose side of three or more lines.** No code in the block to match
//     on, and too long for fuzz to drop. M77's was three.
//  2. **Any interior prose context line.** Between the first and last changed
//     line, so fuzz cannot reach it at any length.
//
// Under that rule the catalogue flags **19 of 71**, all of shape (1).
//
// **The number that matters is the one that was measured.** A threshold picked to
// make a count small would be a weakened checker; this one is picked by what the
// tool in the lanes actually does, and if that tool changes the measurement has
// to be retaken. That is why fuzzReach is a named constant with a table behind it
// rather than a 2 sitting in an expression.
//
// # One more thing the measurement settled, and it constrains the remedy
//
// `patch 2.0-12u11-Apple` REFUSES a hunk with zero leading context, even a
// trivially correct one -- measured on a five-line file. `git apply` accepts the
// same hunk. So "trim the prose block to nothing and let the other side carry the
// anchor" is not available on this toolchain, and the advice below does not offer
// it. M77 is re-anchored by NARROWING its context to two prose lines, which the
// table above shows fuzz absorbs, rather than by trimming to zero.

// mutantDir is the catalogue. sim/mutants/ holds every class; blind patches live
// elsewhere and are checked by their own lane.
const mutantDir = "../../sim/mutants"

// fuzzReach is how many all-prose context lines patch(1) will absorb at a hunk's
// edge before the hunk stops applying. MEASURED on this toolchain, not assumed:
// see the table in the header. It is the reason this rule has a threshold at all
// rather than refusing every comment context line.
const fuzzReach = 2

type hunk struct {
	n     int
	ext   string
	lines []line
}

type line struct {
	kind byte // ' ', '+', '-'
	text string
}

// isComment is deliberately syntactic and deliberately cheap. It does not parse:
// a lane that needed a Go parser to answer "is this line prose" would not be
// affordable enough to run on every push, and the answer it needs is the one a
// human reading the patch would give.
func isComment(text, ext string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false // blank context is not prose; it is structure
	}
	switch ext {
	case ".go", ".cc", ".cpp", ".h", ".hh":
		return strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/*") || strings.HasPrefix(s, "*")
	case ".sh", ".py", ".txt", ".yml", ".yaml", "":
		return strings.HasPrefix(s, "#")
	}
	return false
}

func parse(t *testing.T, path string) []hunk {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var out []hunk
	var cur *hunk
	ext := ""
	for _, raw := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(raw, "--- "), strings.HasPrefix(raw, "+++ "):
			// A NEW FILE ENDS THE CURRENT HUNK. Without this, a multi-file
			// patch's first file keeps accumulating the second file's lines,
			// and a trailing comment at the end of one file's last hunk is
			// reported as an INTERIOR comment of a hunk that spans two files
			// with two extensions. Found by this lane disagreeing with the
			// migration about M45, which is the only reason it was found at
			// all: two implementations of one rule, and they were compared.
			if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
			f := strings.Fields(raw)
			if len(f) > 1 && strings.HasPrefix(raw, "+++ ") {
				ext = filepath.Ext(f[1])
			}
		case strings.HasPrefix(raw, "@@"):
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &hunk{n: len(out) + 1, ext: ext}
		case cur != nil && len(raw) > 0 && (raw[0] == ' ' || raw[0] == '+' || raw[0] == '-'):
			cur.lines = append(cur.lines, line{kind: raw[0], text: raw[1:]})
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// TestEveryMutantAnchorsOnCode is the lane.
func TestEveryMutantAnchorsOnCode(t *testing.T) {
	ents, err := os.ReadDir(mutantDir)
	if err != nil {
		t.Fatalf("reading %s: %v", mutantDir, err)
	}
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".patch") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no patches found, so this lane asserts nothing")
	}

	checked := 0
	for _, name := range names {
		for _, h := range parse(t, filepath.Join(mutantDir, name)) {
			first, last := -1, -1
			for i, l := range h.lines {
				if l.kind != ' ' {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
			if first < 0 {
				continue // a hunk that changes nothing is another lane's problem
			}
			checked++

			for _, side := range []struct {
				what string
				blk  []line
			}{
				{"leading", h.lines[:first]},
				{"trailing", h.lines[last+1:]},
			} {
				if len(side.blk) == 0 {
					continue
				}
				allProse := true
				for _, l := range side.blk {
					if !isComment(l.text, h.ext) {
						allProse = false
						break
					}
				}
				// THE THRESHOLD IS MEASURED, NOT READ OFF A MAN PAGE. See the
				// fuzz table in this file's header: one or two all-prose lines
				// are absorbed, three are not.
				if allProse && len(side.blk) >= fuzzReach+1 {
					t.Errorf("%s hunk %d: %s context is %d comment line(s) and no code, "+
						"which is past what fuzz can absorb (%d).\n%s",
						name, h.n, side.what, len(side.blk), fuzzReach, advice(side.blk))
				}
			}

			for _, l := range h.lines[first : last+1] {
				if l.kind == ' ' && isComment(l.text, h.ext) {
					t.Errorf("%s hunk %d: an INTERIOR context line is a comment, and fuzz only "+
						"trims edges, so this one can never be dropped:\n      %s\n%s",
						name, h.n, strings.TrimSpace(l.text), advice(nil))
				}
			}
		}
	}
	t.Logf("%d hunk(s) across %d patch(es) anchor on code", checked, len(names))
}

func advice(blk []line) string {
	var b strings.Builder
	if len(blk) > 0 {
		b.WriteString("      ")
		b.WriteString(strings.TrimSpace(blk[0].text))
		b.WriteString("\n")
	}
	b.WriteString("      Re-anchor it. Two remedies, and a third that this toolchain refuses:\n")
	b.WriteString("        WIDEN  regenerate with a wider window until the block reaches code\n")
	b.WriteString("               (git diff -U6, -U12...). Best when code is a few lines away.\n")
	b.WriteString("        NARROW regenerate with -U2, so the prose block is short enough for\n")
	b.WriteString("               fuzz to absorb. Use when the prose above runs for dozens of\n")
	b.WriteString("               lines and widening would only anchor on MORE prose.\n")
	b.WriteString("        NOT trimming the block to zero: patch(1) on this toolchain refuses a\n")
	b.WriteString("               hunk with no leading context, measured. git apply accepts it.\n")
	b.WriteString("      Verify the rewrite by applying BOTH the old and the new patch to clean\n")
	b.WriteString("      copies and requiring the trees to be byte-identical: re-anchoring must\n")
	b.WriteString("      change where the patch matches, never what it does.")
	return b.String()
}

// TestTheRuleWouldHaveCaughtM77 is the induction, and it is a reconstruction
// rather than a live patch, because M77 is fixed and a lane cannot be induced by
// the defect it already eliminated.
//
// The bytes below are M77's hunk exactly as it stood when the prose edit disarmed
// it. If someone narrows this rule -- to leading-only, to a length threshold, to
// "at least one comment is fine" -- this test says so.
func TestTheRuleWouldHaveCaughtM77(t *testing.T) {
	const m77 = `--- a/store/node.go
+++ b/store/node.go
@@ -508,7 +508,7 @@
 	// D-A7-4, ruled: BOTH paths stay for the phase. The replicated path is the
 	// differential oracle's other half, and a differential between them is the
 	// only instrument that can catch a stale read no client observed.
-	if n.cfg.ReadIndex && req.Op == "get" && !req.ReadTS.IsSet() && req.Txn == nil {
+	if n.cfg.ReadIndex && req.Op == "get" && req.Txn == nil {
 		n.readSeq++
 		ctx := encodeReadCtx(n.cfg.ID, n.readSeq)
 		if err := n.raft.ReadIndex(ctx); err != nil {
`
	dir := t.TempDir()
	p := filepath.Join(dir, "M77.patch")
	if err := os.WriteFile(p, []byte(m77), 0o644); err != nil {
		t.Fatal(err)
	}
	hs := parse(t, p)
	if len(hs) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hs))
	}
	h := hs[0]
	first := -1
	for i, l := range h.lines {
		if l.kind != ' ' {
			first = i
			break
		}
	}
	if first != 3 {
		t.Fatalf("expected 3 leading context lines, got %d", first)
	}
	for i, l := range h.lines[:first] {
		if !isComment(l.text, h.ext) {
			t.Fatalf("leading context line %d is not recognised as a comment: %q", i, l.text)
		}
	}
	t.Log("the historical M77 hunk is flagged: three leading context lines, all prose, no code")
}

// TestBothRotSitesTellTheTwoCausesApart pins the split in the two scripts that
// report a patch failure.
//
// # Why a source pin and not a live induction
//
// `mutants.sh` and `power-refute.sh` have no per-mutant filter (CARRY-FORWARD
// records that as owed), so inducing their ROT branch means a multi-hour run.
// The classifier itself IS induced -- `scripts/patch-rot-kind.sh --self-test`
// plants one rot of each kind and requires them told apart -- and what remains
// is that the two lanes actually ASK it. That is a source-text question, and
// tools/gatepin answers the same kind of question the same way.
//
// **What this refuses is a regression to one sentence for two causes.** At the
// A7/B5 merge both scripts printed *"the code moved and the mutation did not"*
// for M77, whose code had not moved.
func TestBothRotSitesTellTheTwoCausesApart(t *testing.T) {
	for _, f := range []string{"../../scripts/mutants.sh", "../../scripts/power-refute.sh"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		src := string(b)
		if !strings.Contains(src, "patch-rot-kind.sh") {
			t.Errorf("%s reports a patch failure without asking WHY it failed.\n"+
				"      Anchor drift and structural drift need different remedies, and one\n"+
				"      sentence for both was false for M77 at the merge. Call\n"+
				"      scripts/patch-rot-kind.sh in the branch that handles a failed patch.", f)
		}
		if strings.Contains(src, "the code moved and the mutation did not") {
			t.Errorf("%s still carries the single diagnosis that was false for M77:\n"+
				"      \"the code moved and the mutation did not\".", f)
		}
	}
	if _, err := os.Stat("../../scripts/patch-rot-kind.sh"); err != nil {
		t.Fatalf("the classifier both lanes depend on is missing: %v", err)
	}
}
