//go:build rift_cgo

package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestDeterminismThroughTheCgoBoundary is checklist step 2's fresh-process gate,
// now spanning a language boundary.
//
// # What it claims, and the limit is part of the claim
//
//	THE SAME SEED PRODUCES THE SAME TRACE HASH ACROSS SEPARATE PROCESS
//	INVOCATIONS WITH THE C++ ENGINE UNDERNEATH -- ON ONE BUILD.
//
// Two processes on this machine share a compiler, a target architecture and an
// optimization level, so identical hashes are evidence about the SOURCE and not
// about the toolchain. Cross-toolchain determinism is unmeasured and is carried
// as an obligation in docs/CARRY-FORWARD.md with the measurement named: the same
// seed under a second compiler, or the same compiler at a different -O, and
// GitHub's runners are amd64 while this machine is arm64.
//
// The FMA catch (DESIGN-A0.4 Q4) is this exact class on the Go side and is the
// reason the qualifier is written at the claim rather than in a footnote.
//
// # It checks the ENGINE WAS USED in both invocations, not just that two numbers matched
//
// BUG-046: a run reported a byte-identical trace hash on --engine cgo having
// never opened the engine. Two such runs would agree perfectly with each other
// and would be evidence of nothing at all.
//
//	A DETERMINISM GATE THAT COMPARES TWO ANSWERS WITHOUT CHECKING EITHER
//	MECHANISM RAN IS THE MOST SYMMETRIC POSSIBLE FORM OF THAT DEFECT: both sides
//	wrong in the same way, agreeing.
//
// So each invocation must report a non-zero engine footprint, and the two
// footprints must match each other -- the engine wrote the same bytes, not
// merely the same summary.
func TestDeterminismThroughTheCgoBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the stack twice per seed")
	}
	bin := buildTagged(t)

	trace := regexp.MustCompile(`(?m)^trace\s+([a-f0-9]+)`)
	footprint := regexp.MustCompile(`(?m)^engine\s+files=(\d+) bytes=(\d+)`)

	run := func(t *testing.T, seed, root string) (hash string, files, bytes int) {
		t.Helper()
		cmd := exec.Command(bin, "run", "--seed", seed, "--workload", "raft",
			"--engine", "cgo", "--engine-root", root)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("seed %s: %v\n%s", seed, err, out)
		}
		text := string(out)
		m := trace.FindStringSubmatch(text)
		if m == nil {
			t.Fatalf("seed %s produced no trace hash:\n%s", seed, text)
		}
		f := footprint.FindStringSubmatch(text)
		if f == nil {
			t.Fatalf("seed %s produced no engine footprint, so this run proves nothing about "+
				"the C++ engine -- that is BUG-046's shape:\n%s", seed, text)
		}
		nf, _ := strconv.Atoi(f[1])
		nb, _ := strconv.Atoi(f[2])
		if nb == 0 {
			t.Fatalf("seed %s: the engine wrote 0 bytes", seed)
		}
		return m[1], nf, nb
	}

	for _, seed := range []string{"7", "42", "1234"} {
		t.Run("seed"+seed, func(t *testing.T) {
			rootA := filepath.Join(t.TempDir(), "a")
			rootB := filepath.Join(t.TempDir(), "b")
			hA, fA, bA := run(t, seed, rootA)
			hB, fB, bB := run(t, seed, rootB)

			if hA != hB {
				t.Errorf("SAME SEED, SAME BUILD, DIFFERENT TRACE across processes:\n  %s\n  %s\n"+
					"      This is the claim most likely to break at I1 and it has broken. Before "+
					"looking at the engine, check whether a PATH reached the trace: the engine root "+
					"differs between the two runs by construction.", hA, hB)
			}
			if bA != bB || fA != fB {
				t.Errorf("the trace matched but the engine wrote different bytes: "+
					"A files=%d bytes=%d, B files=%d bytes=%d.\n"+
					"      The trace hash is deliberately blind to storage layout, so this is not "+
					"automatically a defect -- but it means the two runs did different work, and "+
					"which one is wrong is not answerable from here.", fA, bA, fB, bB)
			}
			t.Logf("seed %s: trace %s, engine files=%d bytes=%d, identical across processes",
				seed, hA[:16], fA, bA)
		})
	}
	// GUARDED. The first version logged this unconditionally, and printed
	// "DETERMINISTIC ACROSS PROCESSES ON ONE BUILD" while every subtest had
	// failed -- a summary asserting the property the test had just refuted.
	//
	//	A SUCCESS MESSAGE THAT DOES NOT ASK WHETHER THE TEST SUCCEEDED IS A
	//	CLAIM, NOT A REPORT, AND IT IS READ AS THE RESULT.
	if !t.Failed() {
		t.Log("DETERMINISTIC ACROSS PROCESSES ON ONE BUILD. Cross-toolchain determinism is " +
			"unmeasured; see docs/CARRY-FORWARD.md for the measurement that would settle it.")
	}
}

// buildTagged builds simctl WITH the cgo tag and the archive flags.
//
// build() in freshprocess_test.go builds untagged, which is right for every
// other test in this package and wrong for this one: the untagged binary
// correctly REFUSES --engine cgo, so this gate failed with the refusal message
// rather than a determinism result. The refusal did its job -- it named the
// build flags in one line -- and the harness was the thing at fault.
func buildTagged(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "simctl-cgo")
	cmd := exec.Command("go", "build", "-tags", "rift_cgo", "-o", bin, ".")
	cmd.Env = append(os.Environ(),
		"CGO_LDFLAGS=-L"+filepath.Join(root, "engine-cpp", "build", "test")+
			" -lrift_capi -lrift_engine")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("building simctl with -tags rift_cgo needs the C++ archive; "+
			"run `make cpp-cgo` first. Skipping rather than failing, because an absent archive is "+
			"a missing PRECONDITION and not a determinism result: %v\n%s", err, out)
	}
	return bin
}
