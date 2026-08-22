package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryBundleStillFindsItsBug is the half of the corpus claim the replay
// lane cannot check.
//
// # The two claims, and why only one of them was enforced
//
// TestEveryStoredBundleReplays asserts that a bundle still produces the run it
// recorded: same trace, same census. That is a real property and it caught real
// rot. It says nothing at all about the FINDING.
//
// Every raft bundle in seeds/ carries a fixed bug. The schedule alone therefore
// runs clean, and the entry is only worth keeping because the named mutant
// reintroduces the defect and the schedule then catches it again. Nothing
// checked that. A bundle whose patch had stopped reproducing would replay
// perfectly, report MATCH, and be a lie -- and the claim it is a lie about is on
// the résumé: *every bug ever found replays from a single seed.*
//
// It is not hypothetical. A6 changed the workload, which moved every raft
// trace; the replay lane caught that and the bundles were regenerated in the
// same commit. But regenerating a bundle re-records the SCHEDULE, and a schedule
// that no longer reaches the defect regenerates just as happily as one that
// does. This lane is what makes that difference visible.
//
// # Why it is nightly rather than per-push
//
// It applies a patch to a copy of the tree and rebuilds, once per bundle. That
// is minutes, not seconds. The replay lane runs on every push; this one runs
// where the ten-thousand-seed soak runs.
func TestEveryBundleStillFindsItsBug(t *testing.T) {
	if os.Getenv("CORPUS_REINTRODUCE") == "" {
		t.Skip("set CORPUS_REINTRODUCE=1: this lane patches and rebuilds once per bundle")
	}
	bundles := corpus(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, b := range bundles {
		if b.meta.Mutant == "" {
			// BUG-012 and BUG-016 were harness defects with no planted mutant,
			// and the toy bundles carry their flaw in the plan. Both are covered
			// by the replay lane; neither has a patch to apply.
			continue
		}
		patch := b.meta.Mutant
		if !strings.HasPrefix(patch, "sim/mutants/") {
			patch = filepath.Join("sim", "mutants", patch+".patch")
		}
		if _, err := os.Stat(filepath.Join(root, patch)); err != nil {
			t.Errorf("%s names mutant %q and there is no such patch. A bundle whose patch is gone "+
				"cannot reproduce anything, and it will keep replaying green", b.name, b.meta.Mutant)
			continue
		}
		checked++
		b := b
		t.Run(b.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			// A copy of the tracked tree, so the patch never touches the working
			// one. Applying a mutant in place and forgetting to revert it is a
			// recorded process error in this project, and the answer to a
			// recorded process error is a mechanism.
			cp := exec.Command("bash", "-c",
				"cd "+root+" && git ls-files -z | xargs -0 tar cf - | (cd "+dir+" && tar xf -)")
			if out, err := cp.CombinedOutput(); err != nil {
				t.Fatalf("copying the tree: %v\n%s", err, out)
			}
			ap := exec.Command("patch", "-p1", "-s", "-i", filepath.Join(root, patch))
			ap.Dir = dir
			if out, err := ap.CombinedOutput(); err != nil {
				t.Fatalf("%s does not apply to the current tree: %v\n%s", patch, err, out)
			}
			bin := filepath.Join(dir, "simctl")
			bd := exec.Command("go", "build", "-o", bin, "./cmd/simctl")
			bd.Dir = dir
			if out, err := bd.CombinedOutput(); err != nil {
				t.Fatalf("building with %s applied: %v\n%s", patch, err, out)
			}
			run := exec.Command(bin, "replay", "--bundle", filepath.Join(root, b.path))
			run.Dir = dir
			out, _ := run.CombinedOutput()
			text := string(out)
			// The trace legitimately MOVES: the mutant changes what the cluster
			// does. What must come back is the finding.
			if !strings.Contains(text, "violation") {
				t.Errorf("%s replayed with %s applied and reported NO violation. The bundle's "+
					"schedule no longer reaches the defect its patch reintroduces, so the entry "+
					"proves nothing:\n%s", b.name, b.meta.Mutant, indent(text))
			}
		})
	}
	if checked == 0 {
		t.Fatal("no bundle named a mutant, so this lane asserted nothing")
	}
	t.Logf("%d bundle(s) checked by reintroduction", checked)
}

var _ = json.Unmarshal
