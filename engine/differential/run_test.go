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
	cutShort := 0
	for _, regime := range []string{"flush", "compact"} {
		for seed := uint64(1); seed <= 4; seed++ {
			// Sweep kill points across the run. The ordinals are a fixed,
			// deterministic set rather than a random sample -- the corpus
			// promise rests on a schedule that reproduces.
			//
			// THEY ARE SPREAD ACROSS THE ORDINAL SPACE ON PURPOSE, not
			// clustered early: a kill at ordinal 7 lands in the Open, and a
			// suite of only-early kills would report a swept run while never
			// reaching a flush, a compaction or a manifest swap.
			for _, kill := range []uint64{7, 23, 61, 137, 291, 613, 907, 1381, 2003} {
				a, outcome, why := runOne(t, bin, regime, seed, kill)
				runs++
				for _, op := range a.Submission {
					if op.Seq == 0 && (op.Kind == OpSet || op.Kind == OpDelete ||
						op.Kind == OpDeleteRange) {
						cutShort++
						break
					}
				}
				if outcome != Agree {
					failures = append(failures, failure{regime, seed, kill, outcome, why})
					if at := Bisect(a); at >= 0 {
						t.Logf("  bisected to operation %d of %d", at, len(a.Submission))
					}
				}
			}
		}
	}
	if runs == 0 {
		t.Fatal("no schedules ran")
	}
	// GF-26 / §8: A GREEN RUN COUNT IS NOT A RESULT UNLESS THE KILLS LANDED.
	//
	// A kill ordinal past the end of a run is a no-op, and a suite of those
	// would report seventy-two swept schedules over seventy-two clean runs. So
	// the sweep asserts that its kills ACTUALLY CUT RUNS SHORT -- which is
	// visible in the log, because ops after a kill carry sequence 0.
	if cutShort == 0 {
		t.Fatal("no schedule was cut short: every kill ordinal was past the " +
			"end of its run, so this suite swept nothing and reported green")
	}
	t.Logf("%d schedules, %d cut short by their kill, %d divergences",
		runs, cutShort, len(failures))
	for _, f := range failures {
		t.Errorf("DIVERGENCE %s seed %d kill %d: %v -- %s",
			f.regime, f.seed, f.kill, f.outcome, f.why)
	}
}

// THE BISECT — B4.3, and the cost topology (b) pays.
//
// A file-mediated differential cannot compare operation-by-operation, so a
// mismatch names a RUN. The bisect narrows it to an OPERATION, and it bisects
// THE SUBMISSION LOG rather than the kill schedule:
//
//	Because the log is an artifact and both engines are deterministic, the
//	bisect is A FUNCTION OF THE FILE. It needs no re-run of the schedule that
//	produced the divergence, and it works at any commit.
//
// That is `seeds/`'s property applied to a two-engine comparison, and it is the
// reason the artifact carries the whole log rather than a summary.
func Bisect(a *Artifact) int {
	if outcome, _ := Judge(a); outcome == Agree {
		return -1
	}
	// CF-3: the progress quantity is `hi - lo`, which strictly shrinks on every
	// iteration whichever branch is taken. It is an integer interval over the
	// log's length and does not depend on the judge's verdict — the thing this
	// loop could be wrong about.
	//
	// Its correctness instrument is separate: TestBisectNamesTheOperation.
	lo, hi := 0, len(a.Submission)
	for lo < hi {
		mid := lo + (hi-lo)/2
		prefix := *a
		prefix.Submission = a.Submission[:mid]
		// The watermark cannot exceed what the prefix applied, or the judge
		// refuses it as a sequence the log never reached — which would report
		// the truncation rather than the defect.
		prefix.Watermark = 0
		for _, op := range prefix.Submission {
			if op.Seq > prefix.Watermark {
				prefix.Watermark = op.Seq
			}
		}
		if outcome, _ := Judge(&prefix); outcome == Agree {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
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

// THE BISECT NAMES AN OPERATION, induced against a synthetic divergence so the
// mechanism is exercised whether or not the engine ever diverges again.
//
// A bisect that has never narrowed anything is a bisect nobody can rely on at
// the moment it is needed, which is always the moment a real divergence appears.
func TestBisectNamesTheOperation(t *testing.T) {
	a := artifact(
		[]Op{set(1, "a", "1"), set(2, "b", "2"), set(3, "c", "3"), sync()},
		3,
		// The engine "lost" c: the divergence is introduced by the third write.
		map[string][]byte{"a": []byte("1"), "b": []byte("2")},
	)
	at := Bisect(a)
	if at != 3 {
		t.Fatalf("bisect = %d, want 3 (the prefix through the third op is the "+
			"shortest that diverges)", at)
	}
}

func TestBisectReturnsMinusOneWhenThereIsNoDivergence(t *testing.T) {
	a := artifact([]Op{set(1, "a", "1"), sync()}, 1, map[string][]byte{"a": []byte("1")})
	if at := Bisect(a); at != -1 {
		t.Fatalf("bisect = %d on an agreeing artifact, want -1", at)
	}
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
