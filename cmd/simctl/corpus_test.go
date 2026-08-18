package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The corpus lane: every bundle in seeds/ is replayed, and a bundle that no
// longer reproduces its recorded verdict fails the build.
//
// # Why this lane and not a promise
//
// "Every bug ever found replays from a single seed" is a published claim --
// CLAUDE.md rests a resume line on it and seeds/README.md calls the corpus a
// regression suite rather than a museum. Nothing enforced it. A bundle stops
// reproducing the moment the harness moves under it, and it does so in complete
// silence: the directory is still there, the JSON still parses, and nobody runs
// `simctl replay` on a two-month-old entry unless a lane makes them.
//
// It was already true when this lane was written. Both A0 bundles had rotted --
// same finding, different trace -- and the only reason anybody looked is that
// somebody typed the command by hand.
//
// # What counts as reproducing
//
// Two claims, and they are not the same claim:
//
//	the VERDICT   the finding this bundle exists to carry is found again. This
//	              is what the corpus promises and what a stranger checks.
//	the TRACE     the run is bit-identical to the recorded one. This is a
//	              property of the harness at the recorded commit, so a
//	              deliberate harness change legitimately moves it.
//
// Both fail the lane, deliberately. A moved trace hash is not automatically a
// defect, but it is never a non-event: it means the corpus and the code have
// diverged, and the resolution is to regenerate the bundle IN THE SAME COMMIT
// that moved it, exactly as the fresh-process hash for seed 4242 was moved once
// and recorded. A lane that tolerated it would be back to silence, one step
// removed.

// bundleDir is one corpus entry.
type bundleDir struct {
	name string
	path string
	meta struct {
		Seed      uint64 `json:"seed"`
		Commit    string `json:"commit"`
		Workload  string `json:"workload"`
		TraceHash string `json:"trace_hash"`
		Mutant    string `json:"mutant"`
		Violation *struct {
			Checker string `json:"checker"`
			Detail  string `json:"detail"`
		} `json:"violation"`
	}
}

func corpus(t *testing.T) []bundleDir {
	t.Helper()
	root := filepath.Join("..", "..", "seeds")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	var out []bundleDir
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b := bundleDir{name: e.Name(), path: filepath.Join(root, e.Name())}
		mb, err := os.ReadFile(filepath.Join(b.path, "meta.json"))
		if err != nil {
			t.Errorf("%s is a directory in seeds/ with no meta.json; a corpus entry that is not a "+
				"bundle is either an unfinished one or litter, and both need resolving", b.name)
			continue
		}
		if err := json.Unmarshal(mb, &b.meta); err != nil {
			t.Errorf("%s/meta.json does not parse: %v", b.name, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(b.path, "plan.json")); err != nil {
			t.Errorf("%s has no plan.json, so it reproduces at no commit but its own", b.name)
			continue
		}
		out = append(out, b)
	}
	return out
}

// TestEveryStoredBundleReplays is the lane.
func TestEveryStoredBundleReplays(t *testing.T) {
	bundles := corpus(t)
	if len(bundles) == 0 {
		t.Fatal("seeds/ holds no bundles, so this lane asserts nothing. An empty corpus is a " +
			"legitimate state only before the first bug is found; after that it means entries were " +
			"removed without the lane noticing, which is the failure this lane exists for")
	}

	bin := build(t)
	for _, b := range bundles {
		t.Run(b.name, func(t *testing.T) {
			out, err := exec.Command(bin, "replay", "--bundle", b.path).CombinedOutput()
			text := string(out)
			if err != nil {
				t.Errorf("bundle %s no longer reproduces (recorded at %s):\n%s",
					b.name, commitOrUnknown(b.meta.Commit), indent(text))
				return
			}
			if !strings.Contains(text, "MATCH") {
				t.Errorf("bundle %s replayed without reporting a match:\n%s", b.name, indent(text))
			}
			if b.meta.Violation != nil && !strings.Contains(text, "violation reproduced") {
				t.Errorf("bundle %s records a %s violation that the replay did not reproduce:\n%s",
					b.name, b.meta.Violation.Checker, indent(text))
			}
			t.Logf("seed %d, %s workload, recorded at %s: %s", b.meta.Seed, b.meta.Workload,
				commitOrUnknown(b.meta.Commit), verdictOf(b))
		})
	}
	t.Logf("%d bundle(s) replayed", len(bundles))
}

// TestCorpusLaneDetectsRot induces the lane in both of the directions it checks.
//
// A lane over a corpus that currently reproduces cannot distinguish "every
// bundle replays" from "replay always says yes", and this repository has shipped
// five mechanisms that were never invoked. So a real bundle is copied, damaged
// one way at a time, and the replay is required to refuse it.
func TestCorpusLaneDetectsRot(t *testing.T) {
	bundles := corpus(t)
	if len(bundles) == 0 {
		t.Skip("no bundle to damage")
	}
	src := bundles[0]
	bin := build(t)

	for _, tc := range []struct {
		name   string
		damage func(m map[string]any)
		expect string
	}{
		{
			name:   "trace hash moved",
			damage: func(m map[string]any) { m["trace_hash"] = strings.Repeat("0", 64) },
			expect: "DIVERGED",
		},
		{
			name: "recorded finding replaced",
			damage: func(m map[string]any) {
				m["violation"] = map[string]any{
					"checker": "a-checker-that-does-not-exist",
					"detail":  "a finding this run never produced",
					"at_ns":   1,
				}
			},
			expect: "VIOLATION NOT REPRODUCED",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			copyFile(t, filepath.Join(src.path, "plan.json"), filepath.Join(dir, "plan.json"))

			raw, err := os.ReadFile(filepath.Join(src.path, "meta.json"))
			if err != nil {
				t.Fatalf("reading meta: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("parsing meta: %v", err)
			}
			tc.damage(m)
			mb, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				t.Fatalf("re-encoding meta: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "meta.json"), mb, 0o644); err != nil {
				t.Fatalf("writing meta: %v", err)
			}

			out, err := exec.Command(bin, "replay", "--bundle", dir).CombinedOutput()
			if err == nil {
				t.Fatalf("a bundle with %s replayed clean; the lane would report green over a rotted "+
					"corpus:\n%s", tc.name, indent(string(out)))
			}
			if !strings.Contains(string(out), tc.expect) {
				t.Errorf("expected the replay to report %q, got:\n%s", tc.expect, indent(string(out)))
			}
		})
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("reading %s: %v", from, err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatalf("writing %s: %v", to, err)
	}
}

func commitOrUnknown(c string) string {
	if c == "" {
		return "a commit the bundle does not record"
	}
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func verdictOf(b bundleDir) string {
	if b.meta.Violation != nil {
		return b.meta.Violation.Checker + " -- " + b.meta.Violation.Detail
	}
	if b.meta.Mutant != "" {
		// The schedule is preserved here and the defect is preserved in the
		// mutant; neither half reproduces the bug alone.
		return "schedule only; the defect it exposed is fixed and preserved as " + b.meta.Mutant
	}
	return "no violation recorded; a determinism artifact rather than a finding"
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n    ")
}
