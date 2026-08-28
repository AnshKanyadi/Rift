package determinismcheck_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Build-tagged packages must be OFFERED to this lane, not silently absent.
//
// # The gap this closes, measured rather than argued
//
// `go list ./...` does not list a package whose every file carries a build tag.
// Neither does the analyzer's loader. So a tagged package is not "analysed and
// clean", it is **never loaded**, and the lane reports green over source it has
// not seen. Measured on a probe package with one tag and one `os` import:
//
//	default tags:              "build constraints exclude all Go files" -> no finding
//	GOFLAGS=-tags=<the tag>:   the os import is reported
//
// And passing `-tags` to the analyzer command does NOT help: `singlechecker`
// accepts the flag and does not forward it to the loader, so the output is
// byte-identical with and without it. GOFLAGS is the route that works.
//
// This is the same distinction TestHatchRegistry closes one level down: there,
// an exemption must appear in a checked-in list rather than merely existing;
// here, a package must be analysed or NAMED as not analysed, with a reason.
// **Silence is not allowed to be the mechanism either way.**

// notAnalysed lists packages this lane knowingly does not analyse, by exact
// import path, each with the reason.
//
// A package earns a line here only when it cannot be LOADED at all, not when it
// is merely inconvenient, and the entry is checked against the tree rather than
// believed: TestEveryBuildTaggedPackageIsAnalysedOrNamed fails on any entry
// whose package does load.
//
// # It is empty, and it is empty because the one entry it held was false
//
// `engine/riftcgo` sat here from the A7/B5 merge with the reason *"cannot
// type-check without the C++ static archive and rift.h, which this lane does not
// build."* Measured at the merge: `go build -tags rift_cgo ./engine/riftcgo/`
// succeeds with no archive anywhere in the tree, and registry_test.go's own
// loader has been loading the package successfully the whole time, fifty lines
// away.
//
// **And the entry was UNREACHABLE, which is why its falsity was undetectable.**
// The loop below `continue`s on `len(p.Syntax) > 0` -- loaded and analysable --
// before it ever consults this map. riftcgo loads, so the lookup never happened.
// Induced: the test passed with no "not analysed, by name" line in `-v` output
// and no mention of riftcgo at all.
//
// `CARRY-FORWARD.md` claimed this test *"will stop accepting the notAnalysed
// entry the moment the package becomes loadable -- because a named exemption is
// only honest while the reason holds."* There was no such rejection. That is
// `blind-unused-hatch`'s rule one level up, in a file that had no such rule.
// **It has one now**, and it is what makes this map safe to be empty: an empty
// list with a live rejection is a claim; an empty list with nothing checking it
// is only an absence.
var notAnalysed = map[string]string{}

// TestEveryBuildTaggedPackageIsAnalysedOrNamed walks the tree for build tags,
// loads each tagged package WITH its tag, and requires it to either analyse or
// be named above.
func TestEveryBuildTaggedPackageIsAnalysedOrNamed(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving the repo root: %v", err)
	}
	tags := buildTagsInTree(t, root)
	if len(tags) == 0 {
		t.Log("no build-tagged Go files in the tree; this test starts doing work when one lands")
		return
	}
	t.Logf("build tags found in the tree: %v", tags)

	for _, tag := range tags {
		cfg := &packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
				packages.NeedSyntax,
			Dir:        root,
			Tests:      true,
			BuildFlags: []string{"-tags=" + tag},
		}
		pkgs, err := packages.Load(cfg, "./...")
		if err != nil {
			t.Fatalf("loading with -tags=%s: %v", tag, err)
		}
		for _, p := range pkgs {
			if len(p.GoFiles) == 0 && len(p.CompiledGoFiles) == 0 {
				continue // not a tagged package, or not this tag's
			}
			if len(p.Syntax) > 0 {
				// LOADED. Before moving on: if something claims this package
				// cannot be loaded, that claim is now false and says so.
				checkStale(t, notAnalysed, p.PkgPath)
				continue
			}
			if why, named := notAnalysed[p.PkgPath]; named {
				t.Logf("  %s: not analysed, by name -- %s", p.PkgPath, why)
				continue
			}
			t.Errorf("%s carries build tag %q, does not load for analysis, and is not named in "+
				"notAnalysed.\n"+
				"  A package the lane cannot load is not a package the lane found clean. It is "+
				"one the lane never saw, and the green it reports is about the packages it did "+
				"see. Either make it loadable, or name it here with the reason.", p.PkgPath, tag)
		}
	}
}

// TestTagForwardingActuallyReachesTheLoader is the induction, and it asserts its
// own premise before asserting the property.
//
// # This is DESIGN-A7 §8.1b's two numbers, applied to a test's own setup
//
// The rule there is that an oracle must fire on its planted defect AND be silent
// on a clean tree, because either number alone is satisfiable by an instrument
// that is not discriminating. The same holds one level down, for a test that
// checks a mechanism reaches something:
//
//	the "before" number -- a tagged package loads ZERO files by default -- is
//	what makes the "after" number mean anything. Without it, a load that
//	returns files proves nothing, because it might have returned them either
//	way, and the assertion would be green over a premise that had quietly
//	stopped holding.
//
// So the zero is asserted first, and it is asserted as a FAILURE rather than a
// skip: if a default load ever starts seeing tagged files, this test says so
// instead of continuing to pass while checking nothing (BUG-036).
func TestTagForwardingActuallyReachesTheLoader(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module probe\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(dir, "p.go"),
		"//go:build probetag\n\npackage probe\n\nimport \"os\"\n\nfunc F() { _ = os.Getenv(\"X\") }\n")

	load := func(flags []string) int {
		cfg := &packages.Config{
			Mode:       packages.NeedName | packages.NeedFiles | packages.NeedSyntax,
			Dir:        dir,
			BuildFlags: flags,
		}
		pkgs, err := packages.Load(cfg, "./...")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		n := 0
		for _, p := range pkgs {
			n += len(p.Syntax)
		}
		return n
	}

	if got := load(nil); got != 0 {
		t.Errorf("without the tag the probe loaded %d files; the premise of this test is that a "+
			"tagged package is EMPTY to a default load, and if that stopped being true the "+
			"forwarding below would be asserting nothing", got)
	}
	if got := load([]string{"-tags=probetag"}); got == 0 {
		t.Error("with -tags=probetag forwarded through BuildFlags the probe still loaded no " +
			"files, so tag forwarding does not reach the loader and every tagged package in " +
			"this repository is invisible to the determinism lane")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

var buildTagRE = regexp.MustCompile(`(?m)^//go:build\s+([A-Za-z0-9_]+)\s*$`)

// buildTagsInTree finds the simple build tags used by Go files in the tree.
func buildTagsInTree(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range buildTagRE.FindAllStringSubmatch(string(b), -1) {
			seen[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// checkStale is the rejection CARRY-FORWARD said already existed.
//
// A named exemption is only honest while its reason holds, and the reason here
// is always the same one: *this package cannot be loaded*. The moment it loads,
// the entry is a false statement sitting in a checked-in list, and nothing else
// in this file would ever look at it -- the caller reaches the map only on the
// not-loaded path, so a stale entry is not merely wrong, it is unreachable.
//
// **That is what makes it the unused-hatch rule rather than a typo check.** The
// determinism pass already refuses a hatch that no longer suppresses anything
// (blind-unused-hatch), for exactly this reason: an exemption nobody re-asks
// rots into a permission nobody granted.
func checkStale(t *testing.T, list map[string]string, loaded string) {
	t.Helper()
	if why, named := list[loaded]; named {
		t.Errorf("%s is named in notAnalysed, and it LOADS.\n"+
			"      Its recorded reason is now false:\n"+
			"        %q\n"+
			"      Delete the entry. A named exemption is only honest while its reason\n"+
			"      holds, and an entry for a package that loads is never even consulted\n"+
			"      by the loop above -- so nothing would have contradicted it.", loaded, why)
	}
}

// TestAStaleNotAnalysedEntryIsRejected is the induction, and it exists because
// notAnalysed is EMPTY.
//
// An empty list with a live rejection is a claim. An empty list with nothing
// exercising the rejection is an absence wearing a rule's clothes -- which is
// the vacuity this repository keeps a register of. So the rejection is fired
// here against a package that certainly loads.
func TestAStaleNotAnalysedEntryIsRejected(t *testing.T) {
	const loads = "github.com/anshkanyadi/rift/internal/sorted"
	stale := map[string]string{loads: "a reason that stopped being true"}

	fake := &testing.T{}
	checkStale(fake, stale, loads)
	if !fake.Failed() {
		t.Fatal("an entry naming a package that LOADS was accepted. That is exactly the state " +
			"engine/riftcgo sat in from the merge until it was measured, and the state " +
			"CARRY-FORWARD.md described as impossible.")
	}

	clean := &testing.T{}
	checkStale(clean, map[string]string{}, loads)
	if clean.Failed() {
		t.Fatal("the rejection fired with an empty list, so it is not discriminating: " +
			"both numbers are required, per DESIGN-A7 section 8.1b")
	}
}
