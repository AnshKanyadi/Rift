// Command simctl runs, replays and inspects simulated runs.
//
//	simctl run    --seed N [--out DIR]    materialize a plan, run it, print its hash
//	simctl replay --bundle DIR            re-execute a bundle and compare hashes
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
	"os"
	"path/filepath"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/plan"
)

// Meta is what a bundle records beside its plan.
type Meta struct {
	Seed        uint64   `json:"seed"`
	TraceHash   string   `json:"trace_hash"`
	StepHashes  []string `json:"step_hashes"`
	Steps       uint64   `json:"steps"`
	Outcome     string   `json:"outcome"`
	OutcomeAtNS int64    `json:"outcome_at_ns"`
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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: simctl run --seed N [--out DIR] | simctl replay --bundle DIR [--strip-faults]")
	os.Exit(2)
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	seed := fs.Uint64("seed", 0, "seed to materialize a plan from")
	out := fs.String("out", "", "directory to write the bundle into; empty writes nothing")
	quiet := fs.Bool("quiet", false, "print only the trace hash")
	_ = fs.Parse(args)

	p, err := plan.Materialize(*seed, plan.DefaultGenConfig())
	if err != nil {
		return fail("materialize: %v", err)
	}

	meta, err := execute(p, fmt.Sprintf("seed %d", *seed))
	if err != nil {
		return fail("run: %v", err)
	}
	meta.Seed = *seed

	if *quiet {
		fmt.Println(meta.TraceHash)
	} else {
		fmt.Printf("seed    %d\n", meta.Seed)
		fmt.Printf("outcome %s at %d after %d steps\n", meta.Outcome, meta.OutcomeAtNS, meta.Steps)
		fmt.Printf("trace   %s\n", meta.TraceHash)
	}

	if *out != "" {
		if err := writeBundle(*out, p, meta); err != nil {
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
		stripped = len(p.Faults.Entries)
		p.Faults.Entries = nil
		p.Faults.Rules = nil
		fmt.Printf("stripped %d fault entries: a violation surviving this is the harness or the workload,\n", stripped)
		fmt.Printf("         not the system under test\n")
	}

	got, err := execute(p, *dir)
	if err != nil {
		return fail("replay: %v", err)
	}

	fmt.Printf("recorded %s\n", recorded.TraceHash)
	fmt.Printf("replayed %s\n", got.TraceHash)

	if *strip {
		// A stripped replay is a different run by construction, so its hash is
		// expected to differ. Saying so is the point: a report that showed a
		// mismatch here without explanation would be read as a failure.
		fmt.Println("hashes are expected to differ: stripping faults changes the run")
		return 0
	}

	if got.TraceHash == recorded.TraceHash {
		fmt.Println("MATCH")
		return 0
	}
	fmt.Println("DIVERGED")
	fmt.Println(sim.DivergenceReport(recorded.StepHashes, got.StepHashes))
	return 1
}

// execute builds a plan into a run, drives it, and returns its meta.
func execute(p *plan.Plan, ref string) (Meta, error) {
	nodes := make([]sim.Node, p.Config.Nodes)
	for i := range nodes {
		nodes[i] = noopNode{}
	}

	tr := sim.NewTrace(0)
	run, err := plan.Build(p, nodes)
	if err != nil {
		return Meta{}, err
	}
	run.Loop.SetTrace(tr)

	// The workload comes from the plan, so a replay drives exactly the traffic
	// the recorded run did.
	for _, op := range p.Workload.Ops {
		run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, 0, nil)
	}

	out, err := run.Loop.Run()
	if err != nil {
		return Meta{}, err
	}
	return Meta{
		TraceHash:   tr.Sum(),
		StepHashes:  tr.Steps(),
		Steps:       out.Steps,
		Outcome:     out.Kind.String(),
		OutcomeAtNS: int64(out.At),
	}, nil
}

// noopNode records nothing and schedules nothing. simctl's job here is the
// harness's identity, not a protocol's behaviour; the toy's own hunts exercise
// the protocol.
type noopNode struct{}

func (noopNode) Handle(sim.Event, sim.Scheduler) {}

func writeBundle(dir string, p *plan.Plan, meta Meta) error {
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
	return os.WriteFile(filepath.Join(dir, "meta.json"), mb, 0o644)
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "simctl: "+format+"\n", args...)
	return 1
}
