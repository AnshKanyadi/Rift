package differential

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// THE DIFFERENTIAL ITSELF: real C++ artifacts, judged against engine/model.
//
// This is the test the phase exists for. Everything above it checks that the
// pieces work; this one asks the question:
//
//	DO TWO INDEPENDENT IMPLEMENTATIONS OF THE RECOVERY CONTRACT AGREE?
//
// It is skipped — loudly — when the C++ binary is absent, because a Go-only
// checkout cannot answer it and a silent skip would report success.

func riftDiff(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "engine-cpp", "build", "test", "rift_diff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("rift_diff not built (%v) -- run `make cpp-build` first. "+
			"THIS IS A SKIP, NOT A PASS: the differential did not run.", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// runOne produces one artifact and judges it.
func runOne(t *testing.T, bin, regime string, seed uint64, kill uint64) (*Artifact, Outcome, string) {
	t.Helper()
	args := []string{regime, strconv.FormatUint(seed, 10)}
	if kill != 0 {
		args = append(args, strconv.FormatUint(kill, 10))
	}
	cmd := exec.Command(bin, args...)
	// The commits are the harness's, not the binary's -- rift_diff refuses to
	// invent them and the format refuses an artifact naming none.
	cmd.Env = append(os.Environ(),
		"RIFT_ENGINE_COMMIT=differential-test",
		"RIFT_MODEL_COMMIT=differential-test")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v", bin, args, err)
	}
	a, err := Parse(out)
	if err != nil {
		t.Fatalf("%s %v produced an artifact the judge refuses: %v", bin, args, err)
	}
	outcome, why := Judge(a)
	return a, outcome, why
}

// CLEAN RUNS FIRST, AND THEY ARE THE CONTROL. An engine that disagrees without
// any crash is broken in a way no kill schedule is needed to find -- and if
// these do not agree, nothing below them means anything.
func TestCleanRunsAgree(t *testing.T) {
	bin := riftDiff(t)
	for _, regime := range []string{"default", "flush", "compact"} {
		for seed := uint64(1); seed <= 8; seed++ {
			a, outcome, why := runOne(t, bin, regime, seed, 0)
			if outcome != Agree {
				t.Errorf("%s seed %d: %v -- %s (watermark %d, %d ops, %d recovered)",
					regime, seed, outcome, why, a.Watermark, len(a.Submission), len(a.Recovered))
			}
		}
	}
}

// AND THEN KILLS, WHICH IS THE QUESTION THE PHASE EXISTS FOR. Each schedule
// kills at a different Env call and asks whether recovery landed exactly at the
// watermark the engine promised before it died.
func TestKilledRunsAgree(t *testing.T) {
	bin := riftDiff(t)
	type failure struct {
		regime  string
		seed    uint64
		kill    uint64
		outcome Outcome
		why     string
	}
	var failures []failure
	runs := 0
	for _, regime := range []string{"flush", "compact"} {
		for seed := uint64(1); seed <= 4; seed++ {
			// Sweep kill points across the run. The ordinal count is discovered
			// by the driver; here it is sampled at a fixed set of fractions so
			// the test is deterministic and bounded.
			for _, kill := range []uint64{7, 23, 61, 137, 291, 613} {
				a, outcome, why := runOne(t, bin, regime, seed, kill)
				runs++
				if outcome != Agree {
					failures = append(failures, failure{regime, seed, kill, outcome, why})
					_ = a
				}
			}
		}
	}
	if runs == 0 {
		t.Fatal("no schedules ran")
	}
	for _, f := range failures {
		t.Errorf("DIVERGENCE %s seed %d kill %d: %v -- %s",
			f.regime, f.seed, f.kill, f.outcome, f.why)
	}
	t.Logf("%d killed schedules, %d divergences", runs, len(failures))
}

// A CORPUS ENTRY MUST REPRODUCE ITS FINDING, NOT MERELY REPLAY -- B4-Q3's
// strict form. This is the mechanism: re-run the artifact's own parameters,
// judge again, and require the SAME verdict.
func ReproduceFinding(bin string, a *Artifact) error {
	cmd := exec.Command(bin, a.Provenance.Regime, strconv.FormatUint(a.Provenance.Seed, 10))
	cmd.Env = append(os.Environ(),
		"RIFT_ENGINE_COMMIT="+a.Provenance.EngineCommit,
		"RIFT_MODEL_COMMIT="+a.Provenance.ModelCommit)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("re-running: %w", err)
	}
	again, err := Parse(out)
	if err != nil {
		return fmt.Errorf("the re-run produced an artifact the judge refuses: %w", err)
	}
	outcome, why := Judge(again)
	if outcome != a.Outcome {
		return fmt.Errorf("the stored artifact does not reproduce its finding: "+
			"recorded %v, re-run reached %v (%s)", a.Outcome, outcome, why)
	}
	return nil
}

func TestAJudgedArtifactReproducesItsFinding(t *testing.T) {
	bin := riftDiff(t)
	a, outcome, why := runOne(t, bin, "flush", 3, 0)
	a.Outcome, a.Why = outcome, why
	if err := RequireJudged(a); err != nil {
		t.Fatal(err)
	}
	if err := ReproduceFinding(bin, a); err != nil {
		t.Fatal(err)
	}
}
