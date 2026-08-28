package blockercheck_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Package blockercheck_test re-asks the blockers that CARRY-FORWARD entries
// declare, so a reason cannot outlive its truth in silence.
//
// # The class, found three times in one day
//
// An obligation records WHY something is blocked. Nothing re-checks the blocker.
// The blocker lifts, and the reason rots while the obligation stands -- so the
// entry keeps telling readers a thing cannot be done that has been doable for
// weeks. All three instances were found at the A7/B5 merge, by a person reading
// records beside the lanes rather than by any lane:
//
//	tools/determinismcheck  a notAnalysed entry said riftcgo "cannot type-check
//	                        without the C++ archive". It builds without one. And
//	                        the entry was unreachable, so nothing consulted it.
//	corpus-reproduces.sh    "20 checked, 4 skipped" summed to the population when
//	                        written and stopped doing so when the corpus grew.
//	CARRY-FORWARD BUG-015   "the covering test cannot be used to check any of
//	                        this as it stands" -- both named blockers were gone.
//
// > **AN OBLIGATION'S BLOCKER IS A CLAIM ABOUT THE WORLD, AND IT IS THE ONLY
// > PART OF AN OBLIGATION NOBODY REREADS: THE TASK IS WHY YOU OPEN THE ENTRY,
// > AND THE BLOCKER IS WHY YOU CLOSE IT AGAIN.**
//
// # The mechanism, and it is deliberately the cheapest one that works
//
// An entry may declare a blocker as an HTML comment:
//
//	<!-- BLOCKER
//	     what: one line naming what is blocked
//	     stale-when: <shell command>
//	-->
//
// The command runs from the repo root. **Exit 0 means the blocker is STALE** --
// the condition that would lift it now holds -- and this lane fails, naming the
// entry. Exit non-zero means it still holds and there is nothing to say.
//
// It does not try to check every blocker. A blocker whose lifting is not a
// machine-checkable condition simply carries no declaration, and this lane is
// silent about it. **That is the honest limit and it is stated rather than
// papered over**: what this buys is that the blockers somebody COULD express get
// re-asked every push, at the cost of one grep each.
var blockerRE = regexp.MustCompile(`(?s)<!--\s*BLOCKER\s*\n(.*?)-->`)

type blocker struct {
	file, what, staleWhen string
}

func declared(t *testing.T, root string) []blocker {
	t.Helper()
	var out []blocker
	docs, err := filepath.Glob(filepath.Join(root, "docs", "CARRY-FORWARD*.md"))
	if err != nil {
		t.Fatalf("globbing: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no CARRY-FORWARD files found, so this lane has nothing to read")
	}
	for _, d := range docs {
		b, err := os.ReadFile(d)
		if err != nil {
			t.Fatalf("reading %s: %v", d, err)
		}
		for _, m := range blockerRE.FindAllStringSubmatch(string(b), -1) {
			out = append(out, parseOne(filepath.Base(d), m[1]))
		}
	}
	return out
}

func parseOne(file, body string) blocker {
	bl := blocker{file: file}
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "what:"):
			bl.what = strings.TrimSpace(strings.TrimPrefix(ln, "what:"))
		case strings.HasPrefix(ln, "stale-when:"):
			bl.staleWhen = strings.TrimSpace(strings.TrimPrefix(ln, "stale-when:"))
		}
	}
	return bl
}

// isStale runs the declared condition. Exit 0 == the blocker has lifted.
func isStale(root string, bl blocker) (bool, error) {
	cmd := exec.Command("sh", "-c", bl.staleWhen)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	return false, err
}

// TestEveryDeclaredBlockerStillHolds is the lane.
func TestEveryDeclaredBlockerStillHolds(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bls := declared(t, root)
	if len(bls) == 0 {
		t.Fatal("no BLOCKER declarations found. This lane exists because three obligations were " +
			"found carrying reasons that had gone false; zero declarations means the mechanism " +
			"is present and unused, which is the state it was built to end")
	}
	for _, bl := range bls {
		if bl.what == "" || bl.staleWhen == "" {
			t.Errorf("%s: a BLOCKER declaration is missing what: or stale-when:", bl.file)
			continue
		}
		stale, err := isStale(root, bl)
		if err != nil {
			t.Errorf("%s: could not evaluate %q: %v", bl.file, bl.staleWhen, err)
			continue
		}
		if stale {
			t.Errorf("%s: THE BLOCKER HAS LIFTED and the entry still says otherwise.\n"+
				"      what:       %s\n"+
				"      stale-when: %s   (exited 0, so the condition holds)\n"+
				"      Rewrite the entry. The obligation may well stand; the REASON it gives for\n"+
				"      standing does not, and that reason is the part nobody rereads.",
				bl.file, bl.what, bl.staleWhen)
			continue
		}
		t.Logf("  still blocked: %s (%s)", bl.what, bl.file)
	}
}

// TestTheStaleDetectorIsInduced fires both arms, because a detector only ever
// seen agreeing is a detector nobody has checked. Section 8.1b's two numbers.
func TestTheStaleDetectorIsInduced(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	lifted, err := isStale(root, blocker{staleWhen: "true"})
	if err != nil || !lifted {
		t.Fatalf("a condition that holds was not reported stale (err=%v)", err)
	}
	holds, err := isStale(root, blocker{staleWhen: "false"})
	if err != nil || holds {
		t.Fatalf("a condition that does not hold was reported stale (err=%v)", err)
	}

	// And the parser, on the exact shape the docs carry.
	got := parseOne("x.md", "     what: the thing\n     stale-when: grep -q foo bar\n")
	if got.what != "the thing" || got.staleWhen != "grep -q foo bar" {
		t.Fatalf("parse produced %+v", got)
	}
}
