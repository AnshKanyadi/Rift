// Command simctl runs, replays and inspects simulated runs.
//
//	simctl run    --seed N [--out DIR]    materialize a plan, run it, print its hash
//	              [--workload toy|none]   what drives the run; toy is the A0 protocol
//	              [--flaw NAME]           plant a known defect in the toy
//	              [--placement NAME]      reactive | uniform crash targeting
//	              [--failover]            crash the primary and promote a backup
//	simctl hunt   --from N --to M         sweep a seed range; bundle and triage the
//	              [--workers W]           first violation before reporting it
//	simctl replay --bundle DIR            re-execute a bundle and compare
//	              [--strip-faults]        replay with every fault entry removed
//
// # Why this is the process boundary
//
// `replay` is by definition a fresh-process re-execution, so this command is
// where process spawning belongs, and the fresh-process determinism gate rides
// with it rather than needing a throwaway spawner of its own. That gate exists
// because two runs inside one process share too much: an in-process rerun
// cannot catch map iteration order seeded from process state, address-dependent
// behaviour, or anything initialized once per process. Only separate
// invocations can.
//
// # Why the toy has to be reachable from here
//
// Until it was, the gate hashed a run of do-nothing nodes: it covered the loop,
// the transport, the plan and the clock, and the toy was reachable only through
// `go test`. So no toy-level violation could produce a replayable bundle, and
// `seeds/` held nothing. That is the whole repro chain — a hunt with no artifact
// to hand a human, and no mechanism behind A1's first corpus entry.
//
// The workload is selected explicitly rather than inferred. A default that
// silently ran the toy would make `--workload none` runs and toy runs
// indistinguishable in a bundle, and the two have different trace hashes.
//
// # The bundle
//
//	plan.json      the plan that ran, faults and workload fully materialized
//	meta.json      seed, workload, scenario, trace hash, per-step digests,
//	               outcome, and the violation if there was one
//	history.json   what the client observed, which is what the checker judged
//
// # The stripped-fault replay
//
// `--strip-faults` re-executes a plan with every injected fault removed. It is
// the first question asked of any violation before it becomes a corpus
// candidate: **a violation that survives with zero injected faults is the
// harness or the workload, not the system under test.** A harness-manufactured
// violation that reached the corpus would replay faithfully forever and spend
// the credibility of every genuine entry beside it.
//
// It is sound because deleting fault entries perturbs only what they touch,
// which `TestDeletingAFaultEntryPerturbsOnlyItself` establishes.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/sim/toy"
)

// workload names what drives a run. It is recorded in the bundle because a
// replay must drive the same traffic; a bundle that did not say would replay a
// different run and report the difference as divergence.
const (
	workloadNone = "none"
	workloadToy  = "toy"
)

// Meta is what a bundle records beside its plan.
type Meta struct {
	Seed uint64 `json:"seed"`

	// Commit is the revision the bundle was recorded at.
	//
	// seeds/README.md has claimed since A0 that meta.json carries it, and it did
	// not. It matters for exactly one reason: the trace hash is a property of
	// the HARNESS, not of the plan, so a hash that no longer matches means
	// either corpus rot or a deliberate harness change -- and without the commit
	// there is no way to tell which, or to go and look.
	Commit string `json:"commit"`

	Workload    string        `json:"workload"`
	Scenario    *ScenarioMeta `json:"scenario,omitempty"`
	TraceHash   string        `json:"trace_hash"`
	StepHashes  []string      `json:"step_hashes"`
	Steps       uint64        `json:"steps"`
	Outcome     string        `json:"outcome"`
	OutcomeAtNS int64         `json:"outcome_at_ns"`

	// Violation is the finding this bundle exists to carry, or nil. A bundle
	// with no violation is still useful -- it is a determinism artifact -- but a
	// corpus entry is one of these with this field set.
	Violation *ViolationMeta `json:"violation,omitempty"`
}

// ScenarioMeta is the build the plan ran against, as opposed to the schedule.
//
// The flaw lives here rather than in the plan on purpose: replaying a bundle
// means running the same schedule against the same build, and a plan carrying
// the flaw would be claiming the schedule caused it.
type ScenarioMeta struct {
	Flaw          string `json:"flaw"`
	Placement     string `json:"placement"`
	Failover      bool   `json:"failover"`
	SyncLatencyNS int64  `json:"sync_latency_ns,omitempty"`
}

// ViolationMeta locates a finding in both coordinate systems an investigator
// needs: virtual time, and the step ordinal that indexes the trace.
type ViolationMeta struct {
	Checker string `json:"checker"`
	Detail  string `json:"detail"`
	AtNS    int64  `json:"at_ns"`

	// Step is the trace ordinal at or after AtNS. StepKnown is false when the
	// trace was capped before reaching it, in which case Step is not written:
	// a wrong step ordinal sends an investigator to the wrong event with full
	// confidence, which is worse than sending them to the instant alone.
	Step      uint64 `json:"step,omitempty"`
	StepKnown bool   `json:"step_known"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "replay":
		os.Exit(cmdReplay(os.Args[2:]))
	case "hunt":
		os.Exit(cmdHunt(os.Args[2:]))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: simctl run --seed N [--workload toy|none] [--flaw NAME] [--placement NAME] [--out DIR]")
	fmt.Fprintln(os.Stderr, "       simctl replay --bundle DIR [--strip-faults]")
	fmt.Fprintln(os.Stderr, "       simctl hunt --from N --to M [--workers W] [--flaw NAME] [--placement NAME] [--failover] [--out DIR]")
	os.Exit(2)
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	seed := fs.Uint64("seed", 0, "seed to materialize a plan from")
	out := fs.String("out", "", "directory to write the bundle into; empty writes nothing")
	quiet := fs.Bool("quiet", false, "print only the trace hash")
	wl := fs.String("workload", workloadNone, "what drives the run: none | toy")
	flawName := fs.String("flaw", "none", "toy flaw to plant: none | ack-before-sync | ack-before-replicate | dup-apply")
	placeName := fs.String("placement", "reactive", "toy crash targeting: reactive | uniform")
	failover := fs.Bool("failover", false, "crash the primary and promote a backup")
	_ = fs.Parse(args)

	meta := Meta{Seed: *seed, Workload: *wl}
	var p *plan.Plan
	var hist *sim.History

	// A seed only names a run relative to the configuration it was generated
	// against, so the toy resolves its seed through toy.MaterializeToy -- the
	// same call the sweep makes. `--workload none` keeps the default generator,
	// which is what the recorded fresh-process hashes were taken against.
	switch *wl {
	case workloadNone:
		var err error
		if p, err = plan.Materialize(*seed, plan.DefaultGenConfig()); err != nil {
			return fail("materialize: %v", err)
		}

	case workloadToy:
		sc, err := scenarioFrom(*flawName, *placeName, *failover, 0)
		if err != nil {
			return fail("%v", err)
		}
		// MaterializeToy leaves the plan prepared, and the bundle stores that
		// result: the plan in a bundle has to be the plan that ran, or replay
		// reproduces something else.
		if p, err = toy.MaterializeToy(*seed, sc); err != nil {
			return fail("%v", err)
		}
		meta.Scenario = &ScenarioMeta{
			Flaw: sc.Flaw.String(), Placement: sc.Placement.String(),
			Failover: sc.Failover, SyncLatencyNS: int64(sc.SyncLatency),
		}

	default:
		return fail("unknown workload %q; it is selected explicitly because a default would make toy runs and empty runs indistinguishable in a bundle", *wl)
	}

	if err := execute(p, &meta, &hist); err != nil {
		return fail("run: %v", err)
	}

	if *quiet {
		fmt.Println(meta.TraceHash)
	} else {
		report(meta)
	}

	if *out != "" {
		meta.Commit = headCommit()
		if err := writeBundle(*out, p, meta, hist); err != nil {
			return fail("writing bundle: %v", err)
		}
		fmt.Fprintf(os.Stderr, "bundle written to %s\n", *out)
	}
	return 0
}

func cmdReplay(args []string) int {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	dir := fs.String("bundle", "", "bundle directory to replay")
	strip := fs.Bool("strip-faults", false, "replay with every fault entry removed")
	_ = fs.Parse(args)

	if *dir == "" {
		return fail("replay needs --bundle")
	}

	planBytes, err := os.ReadFile(filepath.Join(*dir, "plan.json"))
	if err != nil {
		return fail("reading plan: %v", err)
	}
	p, err := plan.Unmarshal(planBytes)
	if err != nil {
		return fail("parsing plan: %v", err)
	}

	metaBytes, err := os.ReadFile(filepath.Join(*dir, "meta.json"))
	if err != nil {
		return fail("reading meta: %v", err)
	}
	var recorded Meta
	if err := json.Unmarshal(metaBytes, &recorded); err != nil {
		return fail("parsing meta: %v", err)
	}

	stripped := 0
	if *strip {
		stripped = stripFaults(p)
		fmt.Printf("stripped %d fault entries: a violation surviving this is the harness or the workload,\n", stripped)
		fmt.Printf("         not the system under test\n")
	}

	// The bundle's plan is already prepared, so Prepare is deliberately not
	// called again here: doing so would double the scenario's faults and replay
	// a schedule the recorded run never had.
	got := Meta{Seed: recorded.Seed, Workload: recorded.Workload, Scenario: recorded.Scenario}
	var hist *sim.History
	if err := execute(p, &got, &hist); err != nil {
		return fail("replay: %v", err)
	}

	at := recorded.Commit
	if at == "" {
		at = "a commit the bundle does not record"
	}
	fmt.Printf("recorded %s (at %s)\n", recorded.TraceHash, at)
	fmt.Printf("replayed %s\n", got.TraceHash)

	if *strip {
		// A stripped replay is a different run by construction, so its hash is
		// expected to differ. Saying so is the point: a report that showed a
		// mismatch here without explanation would be read as a failure.
		fmt.Println("hashes are expected to differ: stripping faults changes the run")
		return reportStrippedVerdict(recorded, got)
	}

	rc := 0
	if got.TraceHash == recorded.TraceHash {
		fmt.Println("MATCH")
	} else {
		fmt.Println("DIVERGED")
		fmt.Println(sim.DivergenceReport(recorded.StepHashes, got.StepHashes))
		rc = 1
	}

	// The trace hash says the run was reproduced. Whether the *finding* was
	// reproduced is a separate claim, and it is the one a corpus entry makes.
	if !violationsAgree(recorded.Violation, got.Violation) {
		fmt.Println("VIOLATION NOT REPRODUCED")
		fmt.Printf("  recorded: %s\n", describeViolation(recorded.Violation))
		fmt.Printf("  replayed: %s\n", describeViolation(got.Violation))
		rc = 1
	} else if recorded.Violation != nil {
		fmt.Printf("violation reproduced: %s\n", describeViolation(got.Violation))
	}
	return rc
}

// reportStrippedVerdict answers the triage question the stripped replay exists
// to ask. It is the whole reason for the mode: a differing hash is expected, a
// surviving violation is not.
func reportStrippedVerdict(recorded, got Meta) int {
	switch {
	case recorded.Violation == nil:
		fmt.Println("the recorded run had no violation, so this replay has nothing to triage")
		return 0
	case got.Violation == nil:
		fmt.Println("VIOLATION DID NOT SURVIVE: consistent with a defect in the system under test,")
		fmt.Println("         since removing the faults removed the finding")
		return 0
	default:
		fmt.Println("VIOLATION SURVIVED WITH ZERO INJECTED FAULTS")
		fmt.Printf("  %s\n", describeViolation(got.Violation))
		fmt.Println("  this is the harness or the workload, not the system under test; it must not")
		fmt.Println("  enter the corpus, where it would replay faithfully forever and spend the")
		fmt.Println("  credibility of every genuine entry beside it")
		return 1
	}
}

// execute builds a plan into a run, drives it, and fills in the meta.
//
// The workload comes from the meta rather than from a flag, so a replay drives
// exactly the traffic the recorded run did.
func execute(p *plan.Plan, meta *Meta, hist **sim.History) error {
	tr := sim.NewTrace(0)

	switch meta.Workload {
	case workloadToy:
		sc, err := scenarioFromMeta(meta.Scenario)
		if err != nil {
			return err
		}
		res, err := hunt.RunToy(p, sc, tr)
		if err != nil {
			return err
		}
		*hist = res.History
		fillOutcome(meta, tr, res.Outcome)
		if v := res.Violation(); v != nil {
			meta.Violation = violationMeta(*v, tr)
		}
		return nil

	case workloadNone:
		nodes := make([]sim.Node, p.Config.Nodes)
		for i := range nodes {
			nodes[i] = noopNode{}
		}
		run, err := plan.Build(p, nodes)
		if err != nil {
			return err
		}
		run.Loop.SetTrace(tr)
		for _, op := range p.Workload.Ops {
			run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, 0, nil)
		}
		out, err := run.Loop.Run()
		if err != nil {
			return err
		}
		fillOutcome(meta, tr, out)
		return nil
	}
	return fmt.Errorf("bundle names unknown workload %q", meta.Workload)
}

func fillOutcome(meta *Meta, tr *sim.Trace, out sim.Outcome) {
	meta.TraceHash = tr.Sum()
	meta.StepHashes = tr.Steps()
	meta.Steps = out.Steps
	meta.Outcome = out.Kind.String()
	meta.OutcomeAtNS = int64(out.At)
}

func violationMeta(r sim.Report, tr *sim.Trace) *ViolationMeta {
	v := &ViolationMeta{Checker: r.Checker, Detail: r.Detail, AtNS: int64(r.At)}
	v.Step, v.StepKnown = tr.StepAt(r.At)
	return v
}

// violationsAgree compares two findings by what identifies them, which is the
// checker, the detail and the instant -- never the step, which is a derived
// locator.
func violationsAgree(a, b *ViolationMeta) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Checker == b.Checker && a.Detail == b.Detail && a.AtNS == b.AtNS
}

func describeViolation(v *ViolationMeta) string {
	if v == nil {
		return "no violation"
	}
	if v.StepKnown {
		return fmt.Sprintf("%s: %s [instant %d, step %d]", v.Checker, v.Detail, v.AtNS, v.Step)
	}
	return fmt.Sprintf("%s: %s [instant %d, step beyond the retained trace]", v.Checker, v.Detail, v.AtNS)
}

func scenarioFrom(flawName, placeName string, failover bool, syncLatency clock.Instant) (toy.Scenario, error) {
	flaw, err := toy.ParseFlaw(flawName)
	if err != nil {
		return toy.Scenario{}, err
	}
	place, err := toy.ParsePlacement(placeName)
	if err != nil {
		return toy.Scenario{}, err
	}
	return toy.Scenario{Flaw: flaw, Placement: place, Failover: failover, SyncLatency: syncLatency}, nil
}

func scenarioFromMeta(m *ScenarioMeta) (toy.Scenario, error) {
	if m == nil {
		return toy.Scenario{}, fmt.Errorf("bundle drives the toy but records no scenario, so there is no build to replay against")
	}
	return scenarioFrom(m.Flaw, m.Placement, m.Failover, clock.Instant(m.SyncLatencyNS))
}

func report(m Meta) {
	fmt.Printf("seed     %d\n", m.Seed)
	fmt.Printf("workload %s\n", m.Workload)
	if m.Scenario != nil {
		fmt.Printf("scenario flaw=%s placement=%s failover=%t\n", m.Scenario.Flaw, m.Scenario.Placement, m.Scenario.Failover)
	}
	fmt.Printf("outcome  %s at %d after %d steps\n", m.Outcome, m.OutcomeAtNS, m.Steps)
	fmt.Printf("trace    %s\n", m.TraceHash)
	if m.Violation != nil {
		fmt.Printf("VIOLATION %s\n", describeViolation(m.Violation))
	}
}

// noopNode records nothing and schedules nothing. It is what `--workload none`
// runs: the harness's own identity, with no protocol in the way.
type noopNode struct{}

func (noopNode) Handle(sim.Event, sim.Scheduler) {}

// headCommit is the revision a bundle is being recorded at, or a stated unknown.
//
// A missing sha is written as "unknown" rather than left empty: an empty string
// reads as "the field was not part of the format", and the whole point of the
// field is to say where the recorded hash came from.
func headCommit() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func writeBundle(dir string, p *plan.Plan, meta Meta, hist *sim.History) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	pb, err := plan.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), pb, 0o644); err != nil {
		return err
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), mb, 0o644); err != nil {
		return err
	}
	if hist == nil {
		return nil
	}
	// The history is the evidence: it is what the checker judged, and a bundle
	// that carried a verdict without it would be asking an investigator to take
	// the finding on faith.
	hb, err := json.MarshalIndent(hist.Events(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "history.json"), hb, 0o644)
}

// stripFaults removes every injected fault from a plan, and with them the
// assertions that were about those faults.
//
// Clearing min_fires matters now that Counters.Check is actually consulted: a
// plan that still demanded a crash while having no crash entries would fail as a
// shortfall, and the triage would report a harness error instead of the answer it
// was asked for. A run with no faults injected asserts nothing about injection.
// "sent" survives because it is about traffic, not about faults.
func stripFaults(p *plan.Plan) int {
	n := len(p.Faults.Entries)
	p.Faults.Entries = nil
	p.Faults.Rules = nil
	for _, k := range []string{"crash", "restart", "partition"} {
		delete(p.Assert.MinFires, k)
	}
	return n
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "simctl: "+format+"\n", args...)
	return 1
}

// cmdHunt sweeps a seed range and, on the first violation, produces the artifact
// a human can act on without being asked to do anything else.
//
// # Why the bundle and the triage are not optional steps
//
// A hunt that printed "seed 29 failed" and stopped would be handing over a
// number and a homework assignment, and the two things that then have to happen
// are exactly the two that are easiest to skip under pressure:
//
//  1. **Write the bundle.** The finding has to survive the terminal it was
//     printed in. seeds/ exists for entries that replay at any commit.
//  2. **Run the stripped-fault triage.** A violation that survives with zero
//     injected faults is the harness or the workload, not the system under test.
//     At 913 of 1000 that was unmissable; at 3 of 1000 it would be
//     indistinguishable from a real find, and a poisoned corpus entry is worse
//     than a missing one.
//
// So the hunt does both before it reports, and it reports the triage verdict
// alongside the finding rather than leaving the reader to infer it.
func cmdHunt(args []string) int {
	fs := flag.NewFlagSet("hunt", flag.ExitOnError)
	from := fs.Uint64("from", 0, "first seed, inclusive")
	to := fs.Uint64("to", 1000, "last seed, exclusive")
	workers := fs.Int("workers", 0, "parallel workers; 0 uses every core")
	out := fs.String("out", "", "directory to write the bundle into when a violation is found")
	flawName := fs.String("flaw", "none", "toy flaw to plant")
	placeName := fs.String("placement", "reactive", "crash targeting: reactive | uniform")
	failover := fs.Bool("failover", false, "crash the primary and promote a backup")
	_ = fs.Parse(args)

	sc, err := scenarioFrom(*flawName, *placeName, *failover, 0)
	if err != nil {
		return fail("%v", err)
	}

	results, err := hunt.Sweep(*from, *to, sc, *workers)
	if err != nil {
		return fail("%v", err)
	}
	c := hunt.Summarize(results)

	fmt.Printf("swept    seeds [%d,%d) flaw=%s placement=%s failover=%t\n",
		*from, *to, sc.Flaw, sc.Placement, sc.Failover)
	fmt.Printf("eligible %d of %d (%d refused as regimes the flaw cannot exist in)\n",
		c.Eligible, c.Seeds, c.Refused)
	fmt.Printf("verdicts %d violation, %d inconclusive, %d harness errors\n",
		c.Violations, c.Inconclusive, c.Errors)

	if c.Errors > 0 {
		// A harness error is not a finding and must not be reported as one.
		for _, r := range results {
			if r.Err != nil {
				return fail("seed %d: %v", r.Seed, r.Err)
			}
		}
	}
	if !c.FoundAViolation {
		fmt.Println("no violation found")
		return 0
	}

	fmt.Printf("VIOLATION at seed %d (seeds-to-detection %d)\n", c.FirstViolation, c.FirstViolation-*from+1)

	// Re-run the winning seed with a trace attached. The sweep runs without one
	// because retaining per-step digests for every seed is what makes a hunt
	// expensive; the single seed that matters gets the full artifact.
	p, err := toy.MaterializeToy(c.FirstViolation, sc)
	if err != nil {
		return fail("re-materializing seed %d: %v", c.FirstViolation, err)
	}
	meta := Meta{
		Seed: c.FirstViolation, Workload: workloadToy,
		Scenario: &ScenarioMeta{
			Flaw: sc.Flaw.String(), Placement: sc.Placement.String(),
			Failover: sc.Failover, SyncLatencyNS: int64(sc.SyncLatency),
		},
	}
	var hist *sim.History
	if err := execute(p, &meta, &hist); err != nil {
		return fail("re-running seed %d: %v", c.FirstViolation, err)
	}
	if meta.Violation == nil {
		return fail("seed %d violated during the sweep but not on re-run; the hunt is not reproducible "+
			"and that is a harness defect, not a finding", c.FirstViolation)
	}
	fmt.Printf("         %s\n", describeViolation(meta.Violation))

	if *out != "" {
		meta.Commit = headCommit()
		if err := writeBundle(*out, p, meta, hist); err != nil {
			return fail("writing bundle: %v", err)
		}
		fmt.Printf("bundle   %s\n", *out)
	}

	// The triage gate, run before the finding is reported as a candidate.
	stripped := *p
	stripped.Assert.MinFires = maps.Clone(p.Assert.MinFires)
	stripFaults(&stripped)
	tmeta := Meta{Seed: meta.Seed, Workload: meta.Workload, Scenario: meta.Scenario}
	var thist *sim.History
	if err := execute(&stripped, &tmeta, &thist); err != nil {
		return fail("stripped-fault triage: %v", err)
	}
	if tmeta.Violation != nil {
		fmt.Println("TRIAGE   VIOLATION SURVIVED WITH ZERO INJECTED FAULTS")
		fmt.Println("         this is the harness or the workload, not the system under test, and it")
		fmt.Println("         must not enter the corpus")
		return 1
	}
	fmt.Println("TRIAGE   violation did not survive fault stripping: consistent with a defect in the")
	fmt.Println("         system under test, and a corpus candidate")
	return 0
}
