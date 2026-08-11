package determinismcheck

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
)

var updateHatches = flag.Bool("update-hatches", false, "rewrite HATCHES.txt from the current tree")

const registryFile = "HATCHES.txt"

const registryHeader = `# Determinism escape-hatch registry.
#
# Every //rift:allow-nondeterminism in the repo, one per line, as
#
#   <file>:<line>  <reason>
#
# TestHatchRegistry diffs this file against what the determinism pass actually
# finds, so a hatch cannot appear in the tree without appearing here: adding one
# is a conscious edit to a checked-in list, reviewed like any other diff.
#
# Regenerate with:  go test ./tools/determinismcheck -update-hatches
#
# Ruled by Ansh, 2026-08-11. Hatches never sanction go, select, chan or sync in
# core scope -- those are refused outright, so nothing of that kind can ever
# appear below.

`

// TestHatchRegistry runs the pass over the real tree, which makes it two
// assertions rather than one: the repo has no unexcused determinism violation,
// and its exceptions are exactly the ones on the checked-in list.
//
// It duplicates `make determinism` on purpose. The lane is what CI fails on;
// this is what fails under `go test ./...` on a laptop, before the push.
func TestHatchRegistry(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locating repo root: %v", err)
	}

	sink := captureAllowances(t)
	setFlags(t, map[string]string{"root": root})

	diags := runOverTree(t, root)
	if len(diags) != 0 {
		t.Errorf("the tree has %d unexcused determinism violation(s):\n  %s",
			len(diags), strings.Join(diags, "\n  "))
	}

	got := hatchLines(sink)

	if *updateHatches {
		// Local-only, ruled 2026-08-11. A lane that can rewrite the list it is
		// checking against is not checking anything: CI asserts diff-clean, and
		// updating the registry is a thing a person does on a laptop and then
		// commits, in the diff, where it can be argued with.
		if ci := os.Getenv("CI"); ci != "" {
			t.Fatalf("-update-hatches is local-only and CI=%s; the lane asserts the registry is diff-clean", ci)
		}
		writeRegistry(t, filepath.Join(root, registryFile), got)
		t.Logf("wrote %s with %d hatch(es)", registryFile, len(got))
		return
	}

	want := readRegistry(t, filepath.Join(root, registryFile))
	if slices.Equal(got, want) {
		return
	}
	t.Errorf("%s does not match the tree.\n\nregistered:\n  %s\n\nfound:\n  %s\n\n"+
		"If the change is intended, run: go test ./tools/determinismcheck -update-hatches",
		registryFile, strings.Join(want, "\n  "), strings.Join(got, "\n  "))
}

// runOverTree drives the analyzer by hand rather than through analysistest,
// which only knows how to run against testdata. Assembling the Pass is the
// whole of what a driver does for an analyzer with no facts.
func runOverTree(t *testing.T, root string) []string {
	t.Helper()

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:   root,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("loading the tree: %v", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatalf("the tree does not load cleanly; see errors above")
	}
	if len(pkgs) == 0 {
		t.Fatal("loaded no packages, which means this test proves nothing")
	}

	seen := make(map[string]bool)
	var diags []string
	for _, pkg := range pkgs {
		if len(pkg.Syntax) == 0 {
			continue
		}
		pass := &analysis.Pass{
			Analyzer:  Analyzer,
			Fset:      pkg.Fset,
			Files:     pkg.Syntax,
			Pkg:       pkg.Types,
			TypesInfo: pkg.TypesInfo,
			ResultOf:  map[*analysis.Analyzer]any{inspect.Analyzer: inspector.New(pkg.Syntax)},
			Report: func(d analysis.Diagnostic) {
				p := relPosition(pkg, d)
				if line := fmt.Sprintf("%s: %s", p, d.Message); !seen[line] {
					seen[line] = true
					diags = append(diags, line)
				}
			},
		}
		if _, err := Analyzer.Run(pass); err != nil {
			t.Fatalf("running over %s: %v", pkg.PkgPath, err)
		}
	}
	slices.Sort(diags)
	return diags
}

func relPosition(pkg *packages.Package, d analysis.Diagnostic) string {
	p := pkg.Fset.Position(d.Pos)
	if rel, err := filepath.Rel(flagRoot, p.Filename); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Sprintf("%s:%d:%d", rel, p.Line, p.Column)
	}
	return p.String()
}

// hatchLines turns the announcement stream into registry lines, deduplicated:
// a package and its test variant both carry the same files, so every hatch is
// announced more than once.
func hatchLines(sink *bytes.Buffer) []string {
	const prefix = "determinismcheck: HATCH "

	seen := make(map[string]bool)
	var out []string
	for _, line := range strings.Split(sink.String(), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if entry := strings.TrimSpace(strings.TrimPrefix(line, prefix)); !seen[entry] {
			seen[entry] = true
			out = append(out, entry)
		}
	}
	slices.Sort(out)
	return out
}

func readRegistry(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the registry: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	slices.Sort(out)
	return out
}

func writeRegistry(t *testing.T, path string, entries []string) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString(registryHeader)
	for _, e := range entries {
		buf.WriteString(e)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
}
