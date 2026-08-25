package main

import (
	"os"
	"strings"
	"testing"
)

// TestOneApplyPath asserts that each mutation lane applies its patch in exactly
// ONE place.
//
// # Why this is a test and not a convention
//
// Twice a guard has landed in one of two copies of the same apply-and-diff
// logic, and both times the lane went on reporting confidently with the guard
// inert:
//
//	power-mutants.sh   the sweep detector was taught to the shared helper and
//	                   the inline sequential copy silently did not have it, so
//	                   the lane could not fire the detector in its DEFAULT mode
//	                   for a full cycle (DESIGN-A6 §43.9d).
//	mutant-covered.sh  the UNMUTATED guard was added to cover_one an hour after
//	                   that was written up; the induction reported `1 skipped`
//	                   instead of `UNMUTATED` because sequential mode ran the
//	                   other copy (§43.14d).
//
// A shape that has produced two silent failures will produce a third, so the
// duplication is gone and this is what keeps it gone. Counting call sites is
// crude and it is the honest crude thing: it fails the moment somebody
// reintroduces a second one, which is exactly when it needs to.
func TestOneApplyPath(t *testing.T) {
	b, err := os.ReadFile("../../scripts/mutant-covered.sh")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	n := codeLines(string(b), "patch -p1 --silent --forward -i")
	if n != 1 {
		t.Errorf("mutant-covered.sh has %d apply sites, want exactly 1. Two copies of one "+
			"measurement path is how a guard lands in one of them and the lane goes on "+
			"reporting with it inert -- twice now (DESIGN-A6 section 43.9d, 43.14d)", n)
	}
}

// TestBaselineAndMeasurementAgree is the property the first version of the test
// above got wrong, and it is worth more than the thing it was looking for.
//
// `power-mutants.sh` invokes the probe TWICE and that is correct: once on the
// mutated tree in `measure_one`, once on the clean tree in `baseline_sweep`. They
// are different measurements, not a duplicated path.
//
// But the lane's sweep verdict is a DIFFERENCE between them -- "a criterion the
// baseline passes" -- and a difference is only meaningful if both sides were
// measured the same way. A baseline taken at a different timeout, or against a
// different test, is not a baseline. So the two invocations must carry identical
// flags, and that is what is asserted.
func TestBaselineAndMeasurementAgree(t *testing.T) {
	b, err := os.ReadFile("../../scripts/power-mutants.sh")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var flags []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		i := strings.Index(line, "test -count=1")
		if i < 0 {
			continue
		}
		// Compare the FLAGS, not the shell plumbing around them: the two call
		// sites legitimately differ in where they send output.
		seg := line[i:]
		if j := strings.Index(seg, "./sim/hunt/"); j >= 0 {
			seg = seg[:j+len("./sim/hunt/")]
		}
		flags = append(flags, strings.TrimSpace(seg))
	}
	if len(flags) != 2 {
		t.Fatalf("expected exactly 2 probe invocations (mutated + baseline), got %d: %q", len(flags), flags)
	}
	if flags[0] != flags[1] {
		t.Errorf("the mutated-tree probe and the baseline probe do not use identical flags:\n"+
			"  mutated:  %s\n  baseline: %s\n"+
			"The sweep verdict is a DIFFERENCE between these two runs, so a baseline measured "+
			"differently from the measurement is not a baseline (DESIGN-A6 section 16.4)",
			flags[0], flags[1])
	}
}

// codeLines counts occurrences of needle on lines that are not comments.
func codeLines(s, needle string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, needle) {
			n++
		}
	}
	return n
}
