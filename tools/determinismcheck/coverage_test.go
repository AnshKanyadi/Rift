package determinismcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPackageIsClassifiedExplicitly is the mechanism (c), and it is what
// makes (a) safe to land.
//
// # The defect it exists for
//
// `scopeFor` fell through to `scopeOff` for anything matching no pattern, while
// Amendment A5 says **"Unclassified packages default in."** Every top-level
// package in the tree predated the check that pins the default, and that check
// pins it for a SUBPACKAGE UNDER AN INCLUDED PREFIX -- `engine/wherever` --
// which is not the case A5's sentence is about.
//
//	SO A5'S LETTER WAS UNENFORCED FROM THE DAY IT WAS WRITTEN. Nothing in the
//	tree contradicted it because no new top-level package was added after it,
//	and the first one added in months exposed it within an hour.
//
// Measured at the time: a deliberate `time.Now` planted in the new `net/`
// produced **0 findings**, while the same class planted in `clock/` produced 1.
// `determinismcheck ./net/` printing nothing meant NEVER LOOKED, not clean.
//
// # Why this check and not a better default alone
//
// Changing the default to `scopeCore` (which I2 also does) fixes the direction
// of the failure but not its silence: a package would then be silently INCLUDED
// rather than silently excluded, and a package included by accident is a lane
// that fails for a reason nobody chose.
//
//	AN IMPLICIT ANSWER IS THE PROBLEM, NOT THE DIRECTION IT LEANS. This check
//	requires every package to match an explicit pattern, so the default is
//	unreachable and the enumeration is the whole of the policy.
func TestEveryPackageIsClassifiedExplicitly(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const mod = "github.com/anshkanyadi/rift/"

	var pkgs []string
	err = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || !fi.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		base := filepath.Base(p)
		// Not our code, or not a package.
		if base == "third_party" || base == "vendor" || base == "testdata" ||
			strings.HasPrefix(base, ".") || base == "engine-cpp" || base == "docs" ||
			base == "seeds" || base == "scripts" {
			return filepath.SkipDir
		}
		ents, rerr := os.ReadDir(p)
		if rerr != nil {
			return nil
		}
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".go") {
				pkgs = append(pkgs, mod+filepath.ToSlash(rel))
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no packages found, so this check asserts nothing")
	}

	explicit := func(path string) bool {
		return matchAny(splitPatterns(flagExclude), path) ||
			matchAny(splitPatterns(flagMailbox), path) ||
			matchAny(splitPatterns(flagCore), path)
	}

	var unclassified []string
	for _, p := range pkgs {
		if !explicit(p) {
			unclassified = append(unclassified, p)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("%d package(s) match no scope pattern and would take the DEFAULT:\n      %s\n\n"+
			"      Amendment A5 requires exceptions to be named and visible. A package that "+
			"reaches the default is classified by omission, and the direction the default leans "+
			"does not change that: silently excluded means never checked, silently included means "+
			"a lane fails for a reason nobody chose.\n"+
			"      Add it to defaultCore with nothing, or to defaultExclude WITH ITS REASON at the "+
			"entry, and pin its polarity in TestScopeTable.",
			len(unclassified), strings.Join(unclassified, "\n      "))
	}
	t.Logf("%d packages, all classified by an explicit pattern", len(pkgs))
}
