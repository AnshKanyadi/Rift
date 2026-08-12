package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// build compiles simctl once for the suite.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "simctl")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building simctl: %v\n%s", err, out)
	}
	return bin
}

func hashOf(t *testing.T, bin string, args ...string) string {
	t.Helper()
	out, err := exec.Command(bin, append([]string{"run", "--quiet"}, args...)...).Output()
	if err != nil {
		t.Fatalf("simctl run %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestFreshProcessTraceHashIsStable is checklist step 2, carried by step 8.
//
// The comparison is across separate process invocations, not two runs inside
// one process, and the distinction is the entire reason the gate exists. An
// in-process rerun shares its address space, its map seeds and everything
// initialized once per process, so it cannot catch map iteration order seeded
// from process state, address-dependent behaviour, or a value captured at
// package init. Only separate invocations can.
func TestFreshProcessTraceHashIsStable(t *testing.T) {
	if testing.Short() {
		t.Skip("spawning processes is not a -short test")
	}
	bin := build(t)

	const seed = "--seed=4242"
	first := hashOf(t, bin, seed)
	if first == "" {
		t.Fatal("simctl printed no hash")
	}

	// Three further invocations, one of them with the environment perturbed in
	// ways that change process state without changing the run: a different
	// GOGC forces different allocation and collection timing, and GOMAXPROCS
	// changes the scheduler's shape.
	for _, env := range [][]string{
		nil,
		{"GOGC=1"},
		{"GOMAXPROCS=1"},
		{"GOMAXPROCS=8", "GOGC=400"},
	} {
		cmd := exec.Command(bin, "run", "--quiet", seed)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("simctl run with %v: %v", env, err)
		}
		got := strings.TrimSpace(string(out))
		if got != first {
			t.Errorf("fresh process with %v produced a different hash:\n  first %s\n  got   %s", env, first, got)
		}
	}

	t.Logf("cross-invocation hash for seed 4242 (darwin/arm64): %s", first)
	t.Logf("compare this against CI's runner the day the remote lands; if that runner is")
	t.Logf("amd64 it is the FMA defence's first cross-architecture datapoint")
}

// TestFreshProcessGateDetectsDivergence induces the gate's failure.
//
// A gate that has only ever agreed has demonstrated the cheap half. Two
// different seeds are two different runs, so their hashes must differ -- if
// they did not, the hash would not be covering the run and every agreement it
// has ever reported would be worthless.
func TestFreshProcessGateDetectsDivergence(t *testing.T) {
	if testing.Short() {
		t.Skip("spawning processes is not a -short test")
	}
	bin := build(t)

	a := hashOf(t, bin, "--seed=4242")
	b := hashOf(t, bin, "--seed=4243")
	if a == b {
		t.Fatalf("two different seeds produced the same hash %s; the gate cannot detect divergence", a)
	}
	t.Logf("induced: seed 4242 -> %s...", a[:16])
	t.Logf("         seed 4243 -> %s...", b[:16])
}

// TestReplayReportsFirstDivergentStep demonstrates the divergence report
// against a deliberately perturbed plan rather than describing it.
func TestReplayReportsFirstDivergentStep(t *testing.T) {
	if testing.Short() {
		t.Skip("spawning processes is not a -short test")
	}
	bin := build(t)
	dir := t.TempDir()

	if out, err := exec.Command(bin, "run", "--seed=7", "--out", dir).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	// A clean replay matches.
	out, err := exec.Command(bin, "replay", "--bundle", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("replay: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MATCH") {
		t.Fatalf("a clean replay did not match:\n%s", out)
	}

	// Perturb the plan: move one fault entry by a millisecond. The bundle's
	// recorded hash no longer describes the plan beside it.
	planPath := filepath.Join(dir, "plan.json")
	b, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("reading plan: %v", err)
	}
	perturbed, ok := perturbOneFault(string(b))
	if !ok {
		t.Skip("this seed produced no fault entry to perturb")
	}
	if err := os.WriteFile(planPath, []byte(perturbed), 0o644); err != nil {
		t.Fatalf("writing plan: %v", err)
	}

	out, err = exec.Command(bin, "replay", "--bundle", dir).CombinedOutput()
	if err == nil {
		t.Fatalf("replaying a perturbed plan succeeded:\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "DIVERGED") || !strings.Contains(text, "first divergence at step") {
		t.Fatalf("the divergence report does not name a first divergent step:\n%s", text)
	}
	t.Logf("induced divergence:\n%s", strings.TrimSpace(text))
}

// perturbOneFault shifts the first fault entry's instant by one millisecond.
func perturbOneFault(s string) (string, bool) {
	const marker = `"at_ns":`
	i := strings.Index(s, `"entries"`)
	if i < 0 {
		return "", false
	}
	j := strings.Index(s[i:], marker)
	if j < 0 {
		return "", false
	}
	j += i + len(marker)
	k := j
	for k < len(s) && (s[k] == ' ') {
		k++
	}
	start := k
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	if start == k {
		return "", false
	}
	return s[:start] + "1000000" + s[k:], true
}

// TestStrippedFaultReplay demonstrates the triage affordance: the first
// question asked of any violation before it becomes a corpus candidate.
func TestStrippedFaultReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("spawning processes is not a -short test")
	}
	bin := build(t)
	dir := t.TempDir()

	if out, err := exec.Command(bin, "run", "--seed=11", "--out", dir).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "replay", "--bundle", dir, "--strip-faults").CombinedOutput()
	if err != nil {
		t.Fatalf("stripped replay: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"stripped", "not the system under test", "expected to differ"} {
		if !strings.Contains(text, want) {
			t.Errorf("stripped replay output is missing %q:\n%s", want, text)
		}
	}
	t.Logf("%s", strings.TrimSpace(text))
}
