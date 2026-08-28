package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/sim/toy"
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
		// Not a skip. The point of this test is that a replay in a FRESH PROCESS
		// notices a plan that no longer matches its recorded hash, and with
		// nothing perturbed it notices nothing while passing. The bundle is
		// generated a few lines up from a seed this test chose, so "no fault
		// entry" means the generator stopped producing them -- which is a
		// finding about the fixture, not a reason to report success.
		t.Fatal("the generated plan carries no fault entry to perturb, so nothing was changed " +
			"and a pass here would say only that replay ran, not that it can refuse")
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

// TestToyViolationBundlesAndReplays is checklist step 8's second half, proved
// end to end rather than described.
//
// Before this existed, `simctl` hashed a run of do-nothing nodes: the gate
// covered the loop, transport, plan and clock, and the toy was reachable only
// through `go test`. So no toy-level violation could become a replayable
// bundle, `seeds/` held only a README, and the repro chain -- the thing every
// reproducibility claim in CLAUDE.md rests on -- had no mechanism behind it.
//
// The chain, whole: hunt seeds through the CLI until one trips the knowingly
// broken toy; bundle it; replay the bundle from a *fresh process* and require
// the same verdict, not merely the same hash. The hash says the run was
// reproduced. The verdict says the finding was, and only the second is the claim
// a corpus entry makes.
func TestToyViolationBundlesAndReplays(t *testing.T) {
	if testing.Short() {
		t.Skip("spawning processes is not a -short test")
	}
	bin := build(t)
	dir := t.TempDir()

	// Hunt. The sweep is through the command line on purpose: it is the path a
	// human takes, and a chain proved only through library calls would leave the
	// CLI untested at exactly the seam that matters.
	const maxSeeds = 200
	seed, out := -1, ""
	for s := range maxSeeds {
		b, err := exec.Command(bin, "run", "--workload=toy", "--flaw=ack-before-sync",
			fmt.Sprintf("--seed=%d", s), "--out", dir).CombinedOutput()
		if err != nil {
			t.Fatalf("run seed %d: %v\n%s", s, err, b)
		}
		if strings.Contains(string(b), "VIOLATION") {
			seed, out = s, string(b)
			break
		}
	}
	if seed < 0 {
		t.Fatalf("a knowingly broken toy survived %d seeds through simctl; either the harness "+
			"cannot find a real defect or the CLI is not driving the toy", maxSeeds)
	}
	t.Logf("hunted: seed %d trips ack-before-sync (seeds-to-detection %d)", seed, seed+1)
	t.Logf("%s", strings.TrimSpace(out))

	// The bundle must carry everything the finding is made of.
	for _, f := range []string{"plan.json", "meta.json", "history.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("bundle is missing %s: %v", f, err)
		}
	}
	var meta struct {
		Seed      uint64 `json:"seed"`
		Workload  string `json:"workload"`
		Violation *struct {
			Checker   string `json:"checker"`
			Detail    string `json:"detail"`
			AtNS      int64  `json:"at_ns"`
			Step      uint64 `json:"step"`
			StepKnown bool   `json:"step_known"`
		} `json:"violation"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("reading meta: %v", err)
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("parsing meta: %v", err)
	}
	switch {
	case meta.Violation == nil:
		t.Fatal("the bundle records no violation, so it cannot be a corpus entry")
	case meta.Seed != uint64(seed):
		t.Errorf("bundle records seed %d, hunted %d", meta.Seed, seed)
	case meta.Workload != "toy":
		t.Errorf("bundle records workload %q", meta.Workload)
	case !meta.Violation.StepKnown:
		t.Error("the violation has no step ordinal, so it locates a finding in virtual time only")
	}
	t.Logf("bundle: seed %d, %s at instant %d, step %d",
		meta.Seed, meta.Violation.Checker, meta.Violation.AtNS, meta.Violation.Step)

	// Replay, fresh process. Same hash and same verdict.
	rb, err := exec.Command(bin, "replay", "--bundle", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("replay: %v\n%s", err, rb)
	}
	text := string(rb)
	for _, want := range []string{"MATCH", "violation reproduced"} {
		if !strings.Contains(text, want) {
			t.Fatalf("replay did not reproduce the finding; missing %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, meta.Violation.Detail) {
		t.Errorf("the replayed violation is not the recorded one:\n%s", text)
	}
	t.Logf("replayed from a fresh process:\n%s", strings.TrimSpace(text))

	// And the triage gate on the same bundle: this violation must NOT survive
	// having its faults removed, or it was the harness all along.
	sb, err := exec.Command(bin, "replay", "--bundle", dir, "--strip-faults").CombinedOutput()
	if err != nil {
		t.Fatalf("stripped replay: %v\n%s", err, sb)
	}
	if !strings.Contains(string(sb), "VIOLATION DID NOT SURVIVE") {
		t.Errorf("a violation found by crashing the primary survived with zero injected faults, "+
			"which makes it the harness or the workload rather than the toy:\n%s", sb)
	}
	t.Logf("triage:\n%s", strings.TrimSpace(string(sb)))
}

// TestOneSeedMeansOnePlan makes the single-entry-point rule structural instead
// of a convention.
//
// A seed only names a run relative to the configuration it was generated
// against. When `simctl` materialized against `plan.DefaultGenConfig` and the
// sweep used its own, **seed 29 meant two different plans**: the violation the
// sweep found did not exist in the bundle, and both halves ran cleanly and
// printed a hash. That is the plan-is-the-repro-unit claim failing silently,
// which is the worst way for it to fail.
//
// Both paths now call `toy.MaterializeToy`. That is a convention, and a
// convention is what just failed — so this compares the plan the CLI actually
// wrote into a bundle against the plan the library entry point produces, byte
// for byte, across a spread of seeds. A second config sneaking into either path
// fails here rather than diverging in the field.
func TestOneSeedMeansOnePlan(t *testing.T) {
	if testing.Short() {
		t.Skip("spawning processes is not a -short test")
	}
	bin := build(t)

	sc := toy.Scenario{Flaw: toy.FlawAckBeforeSync, Placement: toy.PlacementReactive}
	for _, seed := range []uint64{0, 1, 29, 103, 512, 4242} {
		dir := t.TempDir()
		out, err := exec.Command(bin, "run", "--workload=toy", "--flaw=ack-before-sync",
			fmt.Sprintf("--seed=%d", seed), "--out", dir).CombinedOutput()
		if err != nil {
			t.Fatalf("seed %d: run: %v\n%s", seed, err, out)
		}
		viaCLI, err := os.ReadFile(filepath.Join(dir, "plan.json"))
		if err != nil {
			t.Fatalf("seed %d: reading bundle plan: %v", seed, err)
		}

		p, err := toy.MaterializeToy(seed, sc)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		viaLib, err := plan.Marshal(p)
		if err != nil {
			t.Fatalf("seed %d: marshal: %v", seed, err)
		}

		if !bytes.Equal(viaCLI, viaLib) {
			t.Fatalf("seed %d produces two different plans: the CLI path and the library path have "+
				"diverged, so a seed no longer names one run\n  CLI %d bytes, library %d bytes",
				seed, len(viaCLI), len(viaLib))
		}
	}
	t.Logf("six seeds, two paths, byte-identical plans")
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
