// Command simctl runs, replays and inspects simulated runs.
//
//	simctl run    --seed N [--out DIR]    materialize a plan, run it, print its hash
//	              [--workload toy|none]   what drives the run; toy is the A0 protocol
//	              [--flaw NAME]           plant a known defect in the toy
//	              [--placement NAME]      reactive | uniform crash targeting
//	              [--failover]            crash the primary and promote a backup
//	simctl run    --workload raft --seed N  drive the A1 Raft group on that seed
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
	"github.com/anshkanyadi/rift/store"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/raft"
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
	workloadRaft = "raft"
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

	// Inconclusive is a checker that could not reach a verdict, when none
	// reached a violation. Amendment A4's third outcome, which this tool could
	// not express until A6.
	Inconclusive *InconclusiveMeta `json:"inconclusive,omitempty"`

	// Raft is the build the raft workload ran against: what the cluster IS, as
	// opposed to what happens to it.
	//
	// It is recorded for the same reason the toy's scenario is. A2 turned on
	// snapshots, pre-vote and leadership transfers, and every stored raft bundle
	// immediately replayed a different run -- not because the schedule moved but
	// because the cluster did. A bundle that does not pin its build is a bundle
	// whose meaning changes every time a feature lands.
	Raft *RaftMeta `json:"raft,omitempty"`

	// Census is the election census for a raft-workload run.
	//
	// It is in the bundle because it is the only evidence that separates "the
	// cluster behaved" from "the cluster did nothing and every checker agreed".
	// BUG-001 is the case: a codec off-by-one meant no node ever became leader,
	// every operation went unanswered, and the safety verdict was clean. A
	// bundle carrying only the verdict would preserve the wrong half of that
	// story.
	Census *CensusMeta `json:"census,omitempty"`

	// Mutant is the patch that reintroduces the defect this schedule exposed,
	// for a bundle whose bug is already fixed.
	//
	// # Why a fixed bug's bundle records no violation
	//
	// The toy's flaw is a scenario flag carried in the plan, so a toy bundle
	// replays its finding directly. Rift's defects are not flags -- raft/ has no
	// flaw switch and must not grow one -- so once a bug is fixed, its schedule
	// replays clean. The schedule is still exactly half of the reproduction, and
	// the other half is the mutant, which is where this project preserves
	// defects anyway under Amendment A2.
	//
	// So the bundle records the schedule and names the patch, and the two
	// together reproduce the bug at any commit: apply the mutant, replay the
	// bundle. Recording the violation instead would make the corpus lane fail
	// against the fixed tree, which is the wrong way round -- the lane exists to
	// notice rot, not to demand the bug back.
	Mutant string `json:"mutant,omitempty"`

	// Mutants is the patch SET, for a defect no single patch reintroduces.
	//
	// # Why a set, and why it was a string first
	//
	// The arrangement is *the bundle carries the schedule, the mutant carries
	// the defect*, and nothing in that requires the defect to be atomic.
	// BUG-021 is the case that made it explicit: two nodes minting one start
	// timestamp needs BOTH halves of the fix removed -- the node tag (`M67`) and
	// the minted restart timestamp (`M68`) -- because a tree with either half
	// still refuses the collision the other allows. With only a single-patch
	// field, the honest options were a bundle that could not reproduce or no
	// bundle at all, and BUGS.md rule 2 asks every entry for one.
	//
	// `Mutant` stays for the single-patch case, which is every other bundle, and
	// the two are read together: the set is `Mutant` if it is set, plus
	// `Mutants`. A reader gets one list either way.
	Mutants []string `json:"mutants,omitempty"`
}

// MutantSet is every patch this bundle needs, from either field.
func (m Meta) MutantSet() []string {
	var out []string
	if m.Mutant != "" {
		out = append(out, m.Mutant)
	}
	return append(out, m.Mutants...)
}

// stringList is a repeatable string flag.
//
// It exists so `--mutant` can be given more than once, which is what a defect
// that no single patch reintroduces needs (see Meta.Mutants).
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// RaftMeta is the raft workload's build parameters.
type RaftMeta struct {
	PreVote           bool   `json:"pre_vote"`
	SnapshotThreshold uint64 `json:"snapshot_threshold"`
	Transfers         int    `json:"transfers"`
	Learners          int    `json:"learners"`
	ConfChanges       int    `json:"conf_changes"`
	PromotionLag      uint64 `json:"promotion_lag"`

	// A4's two build parameters. A bundle that did not carry them would replay
	// a single-range cluster against a schedule recorded on a split one, which
	// is the "bundle that does not pin its build" failure this struct exists to
	// prevent -- and it would do it silently, since the plan is identical either
	// way.
	SplitThreshold int `json:"split_threshold"`
	Rebalances     int `json:"rebalances"`

	// A5's two. Same reasoning: a bundle that did not carry them would replay a
	// single-version store against a schedule recorded on an MVCC one.
	GCRetentionNS        int64  `json:"gc_retention_ns"`
	SnapshotReadPerMille uint64 `json:"snapshot_read_per_mille"`

	// A6's two. Same reasoning a third time, and the sharpest instance of it: a
	// bundle that did not carry these would replay a cluster with NO
	// TRANSACTIONS against a schedule recorded on one running the bank, and
	// every A6 finding in seeds/ would reproduce as a clean run.
	Accounts     int `json:"accounts"`
	Transfers2PC int `json:"transfers_2pc"`

	// A7's two, and the reasoning a FOURTH time -- which is the point at which
	// the reasoning stops being a comment and becomes a pattern.
	//
	// A bundle that did not carry these replays a cluster with the read-index
	// path OFF against a schedule recorded on one where it is on. It happened:
	// A7 shipped with `ReadIndex` and `FollowerReadPerMille` on RaftOptions and
	// neither on this struct, so every bundle recorded or re-recorded during the
	// phase replayed A6-SHAPED, silently, exactly as the three comments above
	// predict -- "the plan is identical either way".
	//
	// The failure has one signature every time: an option is added to the SHAPE
	// and not to the thing that PINS the shape, and the corpus goes on being
	// green about a cluster that is no longer the one under test.
	ReadIndex            bool `json:"read_index"`
	FollowerReadPerMille int  `json:"follower_read_per_mille"`
}

// InconclusiveMeta is a checker that could not reach a verdict.
//
// Amendment A4: an inconclusive result is never a pass, and a bundle that
// recorded only violations would file the whole class of "the checker could not
// tell" under "nothing happened". BUG-001's bundle is exactly that shape -- its
// finding is a green verdict over a history of unknowns, which is now reported
// as inconclusive rather than as a violation.
type InconclusiveMeta struct {
	Checker string `json:"checker"`
	Detail  string `json:"detail"`
}

// CensusMeta is what the run's elections looked like.
type CensusMeta struct {
	Terms          uint64 `json:"terms"`
	ElectionsStart int    `json:"elections_started"`
	ElectionsWon   int    `json:"elections_won"`
	SplitVotes     int    `json:"split_votes"`
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
	wl := fs.String("workload", workloadNone, "what drives the run: none | toy | raft")
	flawName := fs.String("flaw", "none", "toy flaw to plant: none | ack-before-sync | ack-before-replicate | dup-apply")
	placeName := fs.String("placement", "reactive", "toy crash targeting: reactive | uniform")
	failover := fs.Bool("failover", false, "crash the primary and promote a backup")
	var mutants stringList
	fs.Var(&mutants, "mutant", "the sim/mutants patch that reintroduces the defect this schedule exposed; repeat it for a defect no single patch reintroduces")
	preVote := fs.Bool("pre-vote", true, "raft workload: run the pre-vote round")
	snapEvery := fs.Uint64("snapshot-threshold", 6, "raft workload: applied entries between snapshots; 0 disables")
	transfers := fs.Int("transfers", 2, "raft workload: leadership transfers the plan schedules")
	learners := fs.Int("learners", 1, "raft workload: how many nodes start as learners")
	confChanges := fs.Int("conf-changes", 4, "raft workload: membership steps the plan schedules")
	promotionLag := fs.Uint64("promotion-lag", 8, "raft workload: how far behind a learner may be and still be promoted")
	splitEvery := fs.Int("split-threshold", 4, "raft workload: keys a range may hold before its leader proposes a split; 0 disables")
	rebalances := fs.Int("rebalances", 12, "raft workload: manual replica-movement orders the plan schedules")
	gcRetention := fs.Int64("gc-retention-ns", int64(2*time.Second), "raft workload: how far behind its clock a leader collects; 0 disables")
	snapReads := fs.Uint64("snapshot-reads-per-mille", 400, "raft workload: share of reads naming a remembered timestamp")
	accounts := fs.Int("accounts", 8, "raft workload: bank accounts, one key each; 0 disables the bank")
	transfers2pc := fs.Int("transfers-2pc", 40, "raft workload: transactions the bank runs; 0 disables the bank")
	readIndex := fs.Bool("read-index", true, "raft workload: serve plain reads by read index instead of by a log entry (A7)")
	engineName := fs.String("engine", "model", "storage under the stack: model | cgo (cgo needs -tags rift_cgo and the C++ archive)")
	engineRoot := fs.String("engine-root", "", "directory the cgo engine places one store per node under; empty uses a temp dir")
	followerReads := fs.Int("follower-read-per-mille", 333, "raft workload: share of plain reads addressed to one replica rather than broadcast (A7)")
	_ = fs.Parse(args)

	newEngine, engErr := resolveEngine(*engineName, *engineRoot)
	if engErr != nil {
		return fail("%v", engErr)
	}

	meta := Meta{Seed: *seed, Workload: *wl}
	// One patch keeps the single-string field every existing bundle uses; more
	// than one goes in the set. A reader takes MutantSet either way.
	switch len(mutants) {
	case 0:
	case 1:
		meta.Mutant = mutants[0]
	default:
		meta.Mutants = mutants
	}
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

	case workloadRaft:
		// Like the toy's, the plan is materialized through the same call the
		// sweep makes, so a bundle carries the plan that ran rather than one
		// regenerated slightly differently later.
		opt := hunt.RaftOptions{
			PreVote:              *preVote,
			SnapshotThreshold:    raft.Index(*snapEvery),
			Transfers:            *transfers,
			Learners:             *learners,
			ConfChanges:          *confChanges,
			PromotionLag:         raft.Index(*promotionLag),
			SplitThreshold:       *splitEvery,
			Rebalances:           *rebalances,
			GCRetention:          time.Duration(*gcRetention),
			SnapshotReadPerMille: *snapReads,
			Accounts:             *accounts,
			Transfers2PC:         *transfers2pc,
			ReadIndex:            *readIndex,
			FollowerReadPerMille: *followerReads,
			NewEngine:            newEngine,
		}
		meta.Raft = &RaftMeta{
			PreVote: opt.PreVote, SnapshotThreshold: uint64(opt.SnapshotThreshold),
			Transfers: opt.Transfers, Learners: opt.Learners,
			ConfChanges: opt.ConfChanges, PromotionLag: uint64(opt.PromotionLag),
			SplitThreshold: opt.SplitThreshold, Rebalances: opt.Rebalances,
			GCRetentionNS:        int64(opt.GCRetention),
			SnapshotReadPerMille: opt.SnapshotReadPerMille,
			Accounts:             opt.Accounts,
			Transfers2PC:         opt.Transfers2PC,
			ReadIndex:            opt.ReadIndex,
			FollowerReadPerMille: opt.FollowerReadPerMille,
		}
		var err error
		if p, err = hunt.MaterializeRaftWith(*seed, opt); err != nil {
			return fail("%v", err)
		}

	default:
		return fail("unknown workload %q; it is selected explicitly because a default would make toy runs and empty runs indistinguishable in a bundle", *wl)
	}

	if err := execute(p, &meta, &hist, newEngine); err != nil {
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
	engineName := fs.String("engine", "model", "storage under the stack: model | cgo (cgo needs -tags rift_cgo and the C++ archive)")
	engineRoot := fs.String("engine-root", "", "directory the cgo engine places one store per node under; empty uses a temp dir")
	strip := fs.Bool("strip-faults", false, "replay with every fault entry removed")
	rerecord := fs.Bool("rerecord", false, "overwrite the bundle's recorded trace with this run's, keeping its plan")
	_ = fs.Parse(args)

	if *rerecord && *strip {
		return fail("--rerecord with --strip-faults would record a trace from a schedule the bundle does not carry")
	}

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
	got := Meta{
		Seed: recorded.Seed, Workload: recorded.Workload,
		Scenario: recorded.Scenario, Raft: recorded.Raft,
	}
	var hist *sim.History
	newEngine, engErr := resolveEngine(*engineName, *engineRoot)
	if engErr != nil {
		return fail("%v", engErr)
	}
	if err := execute(p, &got, &hist, newEngine); err != nil {
		return fail("replay: %v", err)
	}

	at := recorded.Commit
	if at == "" {
		at = "a commit the bundle does not record"
	}
	fmt.Printf("recorded %s (at %s)\n", recorded.TraceHash, at)
	fmt.Printf("replayed %s\n", got.TraceHash)

	// # Re-recording, which is the documented resolution and not an escape hatch
	//
	// A moved trace hash is never a non-event: it means the corpus and the code
	// have diverged. The discipline seeds/README.md states is to regenerate the
	// bundle IN THE SAME COMMIT that moved it, and this is the tool that does it.
	//
	// What it rewrites is the OBSERVATION -- the trace hash, the step hashes, the
	// step count, the outcome, the census, and the commit the run was recorded
	// at. What it never touches is the bundle's PLAN, because the plan is the
	// finding: the schedule is what reproduces the bug, and regenerating it from
	// the seed would silently substitute whatever schedule today's generator
	// produces for the one that found something.
	if *rerecord {
		got.Seed = recorded.Seed
		got.Mutant, got.Mutants = recorded.Mutant, recorded.Mutants
		got.Commit = headCommit()
		if err := writeBundle(*dir, p, got, hist); err != nil {
			return fail("re-recording bundle: %v", err)
		}
		fmt.Printf("re-recorded at %s: the plan is unchanged, the observation is this commit's\n", got.Commit)
		return 0
	}

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

	if recorded.Census != nil || got.Census != nil {
		fmt.Printf("census   recorded %s\n", describeCensus(recorded.Census))
		fmt.Printf("         replayed %s\n", describeCensus(got.Census))
	}

	// The trace hash says the run was reproduced. Whether the *finding* was
	// reproduced is a separate claim, and it is the one a corpus entry makes.
	switch {
	case violationsAgree(recorded.Violation, got.Violation):
		if recorded.Violation != nil {
			fmt.Printf("violation reproduced: %s\n", describeViolation(got.Violation))
		}
	case recorded.Violation == nil:
		// The replay found something the recording did not. Said in that
		// direction rather than as "not reproduced", because on a bundle whose
		// defect has been reintroduced this is the SUCCESS case and the wording
		// decides whether a reader reads it as one.
		fmt.Println("THE REPLAY PRODUCED A VIOLATION THE RECORDING DID NOT")
		fmt.Printf("  replayed: %s\n", describeViolation(got.Violation))
		rc = 1
	default:
		fmt.Println("VIOLATION NOT REPRODUCED")
		fmt.Printf("  recorded: %s\n", describeViolation(recorded.Violation))
		fmt.Printf("  replayed: %s\n", describeViolation(got.Violation))
		rc = 1
	}

	// # The third outcome, printed here too
	//
	// A replay whose checkers could not reach a verdict has not replayed clean:
	// it has replayed INCONCLUSIVE, which Amendment A4 says is never a pass. It
	// was invisible here for the same reason it was invisible in `run` -- the
	// comparison above knows about violations and nothing else -- and BUG-001's
	// whole finding is of this shape: no leader, every operation unanswered, and
	// a linearizability verdict over a history of unknowns.
	switch {
	case got.Inconclusive != nil && recorded.Inconclusive == nil:
		fmt.Println("THE REPLAY WAS INCONCLUSIVE WHERE THE RECORDING WAS NOT")
		fmt.Printf("  replayed: %s: %s\n", got.Inconclusive.Checker, got.Inconclusive.Detail)
		rc = 1
	case got.Inconclusive != nil:
		fmt.Printf("inconclusive reproduced: %s: %s\n",
			got.Inconclusive.Checker, got.Inconclusive.Detail)
	case recorded.Inconclusive != nil:
		fmt.Println("INCONCLUSIVE NOT REPRODUCED")
		fmt.Printf("  recorded: %s: %s\n",
			recorded.Inconclusive.Checker, recorded.Inconclusive.Detail)
		rc = 1
	}

	// A bundle whose defect is already fixed replays clean by design, and the
	// half of the reproduction that is missing is named rather than left for a
	// reader to go looking for.
	if recorded.Violation == nil && len(recorded.MutantSet()) > 0 {
		fmt.Printf("this schedule's defect is fixed; to see the finding, reintroduce it:\n")
		for _, m := range recorded.MutantSet() {
			fmt.Printf("  patch -p1 < %s\n", m)
		}
		fmt.Printf("  simctl replay --bundle %s\n", *dir)
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
// newEngine is the storage the RUN uses, passed beside the plan because a plan
// is data and cannot carry a function. Nil means engine/model.
func execute(p *plan.Plan, meta *Meta, hist **sim.History, newEngine func(node int) store.Engine) error {
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

	case workloadRaft:
		opt := hunt.CurrentOptions()
		if meta.Raft != nil {
			opt = hunt.RaftOptions{
				PreVote:              meta.Raft.PreVote,
				SnapshotThreshold:    raft.Index(meta.Raft.SnapshotThreshold),
				Transfers:            meta.Raft.Transfers,
				Learners:             meta.Raft.Learners,
				ConfChanges:          meta.Raft.ConfChanges,
				PromotionLag:         raft.Index(meta.Raft.PromotionLag),
				SplitThreshold:       meta.Raft.SplitThreshold,
				Rebalances:           meta.Raft.Rebalances,
				GCRetention:          time.Duration(meta.Raft.GCRetentionNS),
				SnapshotReadPerMille: meta.Raft.SnapshotReadPerMille,
				Accounts:             meta.Raft.Accounts,
				Transfers2PC:         meta.Raft.Transfers2PC,
				ReadIndex:            meta.Raft.ReadIndex,
				FollowerReadPerMille: meta.Raft.FollowerReadPerMille,
			}
		}
		// THE ENGINE TRAVELS BESIDE THE PLAN, NOT INSIDE IT, and this line is
		// where it is attached rather than in the block above.
		//
		// A plan is DATA -- that is what makes it a reproduction unit, and a
		// func field could not be serialized into one. So the options rebuilt
		// from a bundle carry every scalar the plan recorded and no engine, and
		// the engine is the runner's choice. Setting it on the plan-building
		// options instead (which is what happened first) produced a run that
		// reported a byte-identical trace on --engine cgo while never opening
		// the C++ engine at all: the engine root was EMPTY.
		//
		//	A RUN THAT REPORTS SUCCESS ON AN ENGINE IT NEVER OPENED IS THE
		//	VACUOUS GREEN THIS PROJECT KEEPS A REGISTER OF, AND A MATCHING TRACE
		//	HASH IS THE MOST CONVINCING FORM IT COULD POSSIBLY TAKE.
		opt.NewEngine = newEngine
		res, err := hunt.RunRaftWith(p, opt, tr)
		if verr := assertEngineWasUsed(); verr != nil {
			return verr
		}
		if err != nil {
			return err
		}
		*hist = res.History
		fillOutcome(meta, tr, res.Outcome)
		meta.Census = &CensusMeta{
			Terms: uint64(res.Census.Terms), ElectionsStart: res.Census.ElectionsStart,
			ElectionsWon: res.Census.ElectionsWon, SplitVotes: res.Census.SplitVotes,
		}

		// The in-run oracle's finding takes precedence: it halted the run, so
		// everything after it is unobserved and any end-of-run verdict is a
		// verdict over a truncated history. An end-of-run violation is recorded
		// only when no oracle fired.
		if res.Violated != nil {
			meta.Violation = &ViolationMeta{
				Checker: res.Violated.Checker, Detail: res.Violated.Detail,
				AtNS: int64(res.Violated.At), Step: res.Violated.Step, StepKnown: res.Violated.Step != 0,
			}
			return nil
		}
		for _, rep := range res.Reports {
			if rep.Verdict == sim.VerdictViolation {
				meta.Violation = violationMeta(rep, tr)
				break
			}
		}
		// # Inconclusive is recorded too, and it was not
		//
		// Amendment A4 makes it a first-class outcome: a checker reports pass,
		// violation, or inconclusive, and an inconclusive is never counted as a
		// pass. This tool could express two of the three. A run whose whole
		// finding is "the checker could not tell" -- which is exactly BUG-001's
		// finding, and what its bundle exists to carry -- came out of `simctl
		// run` looking like a clean run.
		if meta.Violation == nil {
			for _, rep := range res.Reports {
				if rep.Verdict == sim.VerdictInconclusive {
					meta.Inconclusive = &InconclusiveMeta{
						Checker: rep.Checker, Detail: rep.Detail,
					}
					break
				}
			}
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

func describeCensus(c *CensusMeta) string {
	if c == nil {
		return "none recorded"
	}
	return fmt.Sprintf("terms=%d elections-started=%d elections-won=%d split-votes=%d",
		c.Terms, c.ElectionsStart, c.ElectionsWon, c.SplitVotes)
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
	if m.Census != nil {
		fmt.Printf("census   %s\n", describeCensus(m.Census))
		// ENGINE BYTES, AND THE ASSERTION ABOVE ZERO. BUG-046: a run reported a
		// byte-identical trace hash on --engine cgo having never opened the
		// engine, and every checker passed. A trace hash is an answer; this is
		// the mechanism. GF-25 -- they are two instruments, not one.
		if engineRootUsed != "" {
			n, b := engineFootprint(engineRootUsed)
			fmt.Printf("engine   root=%s files=%d bytes=%d\n", engineRootUsed, n, b)
		}
	}
	if m.Violation != nil {
		fmt.Printf("VIOLATION %s\n", describeViolation(m.Violation))
	}
	if m.Inconclusive != nil {
		fmt.Printf("INCONCLUSIVE %s: %s\n", m.Inconclusive.Checker, m.Inconclusive.Detail)
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
	huntEngineName := fs.String("engine", "model", "storage under the stack: model | cgo (cgo needs -tags rift_cgo and the C++ archive)")
	huntEngineRoot := fs.String("engine-root", "", "directory the cgo engine places one store per node under; empty uses a temp dir")
	_ = fs.Parse(args)

	huntEngine, huntEngErr := resolveEngine(*huntEngineName, *huntEngineRoot)
	if huntEngErr != nil {
		return fail("%v", huntEngErr)
	}

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
		// # "no violation found", NOT "all seeds passed", and the words matter
		//
		// This exits 0 while inconclusives may be nonzero, and Amendment A4 is
		// the reason that is correct rather than a hole. A4 forbids BANKING an
		// inconclusive as a pass: counting it toward a claim, quoting it in a
		// total, letting it stand in for a seed that was checked. A hunt banks
		// nothing. It is a search, its success condition is "no violation was
		// found", and the inconclusive count is printed on its own line beside
		// the violation count -- which is the form A4 requires of the public
		// claim itself.
		//
		// The sentence below is the one thing that must not drift. "All seeds
		// passed" would be exactly the banking A4 prohibits, asserted by a
		// program rather than by a person.
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
	if err := execute(p, &meta, &hist, huntEngine); err != nil {
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
	if err := execute(&stripped, &tmeta, &thist, huntEngine); err != nil {
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

// resolveEngine turns the --engine name into a per-node factory.
//
// The temp root is created here rather than inside the factory so that ONE run
// gets ONE root and every node lands beneath it. A factory that made its own
// root per call would scatter n stores across n directories and leave the
// operator nothing to look at after a failure.
func resolveEngine(name, root string) (func(node int) store.Engine, error) {
	if (name == "" || name == "model") && root == "" {
		return hunt.EngineByName(name, "")
	}
	if root == "" {
		d, err := os.MkdirTemp("", "rift-engine-")
		if err != nil {
			return nil, fmt.Errorf("creating an engine root: %w", err)
		}
		root = d
	}
	engineRootUsed = root
	return hunt.EngineByName(name, root)
}

// engineRootUsed is the directory the run's engine wrote into, or "" for the
// in-memory reference engine. It is package state because it is REPORTING
// state: the run must be able to say what it actually touched, and threading it
// through every signature would put harness bookkeeping in the paths the
// determinism rules govern.
var engineRootUsed string

// engineFootprint counts what the engine actually wrote. Files and bytes, not
// existence: a created-and-empty directory is exactly the state BUG-046 was in.
func engineFootprint(root string) (files int, bytes int64) {
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			files++
			bytes += fi.Size()
		}
		return nil
	})
	return files, bytes
}

// assertEngineWasUsed is the non-vacuity check for the engine itself.
//
// BUG-046: a run named --engine cgo, completed 24,622 steps, passed every
// checker, and reported a trace hash BYTE-IDENTICAL to the model's, having
// never opened the engine. The engine root held zero files. Nothing in the
// run's own output could have distinguished that from success, because the
// output was about the answer and the defect was in the mechanism.
//
//	A NON-VACUITY COUNTER EARNS ITS PLACE WHEN THERE IS A MEASURED INSTANCE OF
//	WHAT ITS ABSENCE LOOKS LIKE. There is one, and this is it.
func assertEngineWasUsed() error {
	if engineRootUsed == "" {
		return nil // the reference engine is in memory and writes nothing
	}
	n, b := engineFootprint(engineRootUsed)
	if b > 0 {
		return nil
	}
	return fmt.Errorf("simctl: a non-default engine was named and it wrote NOTHING: %s holds "+
		"%d files and %d bytes. The run completed, and its trace hash may even match the "+
		"model's -- that is exactly BUG-046, and it is what this check exists for",
		engineRootUsed, n, b)
}
