# A0 close condition: the eleven-step checklist

**Ruled by:** Ansh, 2026-08-17. **Maintained by:** Claude. **Status of A0:** open.

This file is A0's close condition. Nothing else is.

## Why it exists

Until 2026-08-17 this checklist lived only in rulings passed between Ansh and Claude. The repository
carried a *different* numbering — DESIGN-A0 §6's `A0.1`–`A0.12` landing plan — which does not
correspond to it step for step, and the two were being cited interchangeably. Steps 9 and 11 had no
anchor anywhere in the tree: `simctl hunt` and `node/` are both A0-close-blocking, and neither was
written down. A close condition that survives only in conversation is a close condition that can
quietly disappear, and it contradicts the claim this project rests on — that a stranger can check our
work from a clean clone.

So: eleven steps, their real names, their status, their exit condition, their anchor in the tree.

---

## 1. The two numberings are different things

Both exist on purpose. They answer different questions and they are not interchangeable.

| | DESIGN-A0 §6, `A0.1`–`A0.12` | This file, steps 1–11 |
|---|---|---|
| **What it is** | the **landing plan** | the **close condition** |
| **Question it answers** | what order do we build this in, in PR-sized diffs? | what must be true before Ansh signs A0 off? |
| **Written** | before any code, as part of the approved A0 design | during execution, as the work revealed what closing actually requires |
| **Authority** | an estimate; may be re-ordered | binding; only Ansh changes it |

**They do not correspond index for index, and citing one as the other is the error this file exists
to prevent.** Two concrete mismatches: landing-plan row `A0.8` is the plan schema while checklist
step 8 is `simctl`; and landing-plan row `A0.9` folds the oracle framework and the toy into one PR,
where the checklist splits them into steps 6 and 7 because they close on different evidence.

The mapping, as reconciled:

| checklist step | landing-plan row |
|---|---|
| — | `A0.1` skeleton, `A0.2` `internal/rng`, `A0.3` `tools/determinismcheck`, `A0.4` `clock`, `A0.5` `engine` |
| 1 | `A0.6` |
| 2 | — (no row; carried by step 8) |
| 3, 4 | `A0.7` |
| 5 | `A0.8` |
| 6 | `A0.9` (oracle half) |
| 7 | `A0.9` (`sim/toy` half) |
| 8 | `A0.10` |
| 9 | `A0.11` (`hunt` half; `minimize` half cut to STRETCH.md) |
| 10 | `A0.12` |
| 11 | **— no landing-plan row exists.** `node/` is a close condition the landing plan never scheduled |

Landing-plan rows `A0.1`–`A0.5` predate the checklist and have no step number. They landed in commits
`4f9f360`/`690ab2b` (skeleton), `e042b44` (rng), `a68916c` (determinismcheck, tags `a0.3-signed` and
`a0.3b-signed`), `ee2322b`/`e3b9abd` (clock), `eefa269` (engine). They are not exempt from A0's exit
criteria — they are covered by the lane set, which every step below also depends on.

Step 11 having no landing-plan row is the sharpest illustration of why both documents are needed. The
landing plan was written before anyone knew that the mailbox rule would land provisionally, so it
never scheduled the package that makes it real.

---

## 2. The eleven steps

Status vocabulary: **CLOSED** means Ansh signed it off. **LANDED** means the code is in the tree and
its stated exit condition is met, awaiting a ruling — Claude never closes a step. **PARTIAL** means
named conditions are outstanding. **NOT STARTED** means no implementation exists.

The close-blocking column is the close condition at a glance: A0 cannot close while any row in it
says yes.

| # | step | status | close-blocking |
|---|---|---|---|
| 1 | the event loop | CLOSED | no |
| 2 | the fresh-process trace-hash gate | CLOSED | no |
| 3 | transport over the real wire codec | CLOSED | no |
| 4 | fault injectors with fire counts | CLOSED | no |
| 5 | the fault plan schema | CLOSED | no |
| 6 | the oracle framework | CLOSED | no |
| 7 | the toy over 1k seeds | CLOSED | no |
| 8 | `simctl run \| replay`, and the bundle chain | CLOSED | no |
| 9 | `simctl hunt` | LANDED — awaiting a ruling | **yes** |
| 10 | the mutant suite as patches | NOT STARTED | **yes** |
| 11 | `node/`, the real-mode mailbox driver | LANDED — awaiting a ruling | **yes** |

**Steps 1 through 8 are closed** (Ansh, 2026-08-17). A0 closes on 9, 10 and 11.

### Step 1 — the event loop. CLOSED

**Exit condition.** One queue, one node at a time, virtual time advancing only at event boundaries;
`Node.Handle` the sole entry point, synchronous and non-blocking; ticks driven by each node's own
clock so drift shapes tick rate and not only `Now()`; identical seeds produce identical traces.

**Ratified** by Ansh, 2026-08-11, with the outcome enum hardened to a closed four-variant enum on the
same ruling. **Anchor:** `docs/DESIGN-A0.6-eventloop.md`; `sim/loop.go`, `sim/event.go`,
`sim/counters.go`; `sim/loop_test.go` (`TestSameRunSameTrace`,
`TestTotalOrderIsByInstantThenInsertion`, `TestDriftShapesTickCountEndToEnd`,
`TestRunReportsWhyItStopped`, `TestQuiescentRunDoesNotCountTowardSoakHours`, and five more).

### Step 2 — the fresh-process trace-hash gate. CLOSED

**Exit condition.** Identical seeds produce identical trace hashes across *separate process
invocations* — not two runs in one process, which share an address space, map seeds and everything
initialized once per process — including under perturbed `GOGC` and `GOMAXPROCS`; and the gate's
failure is induced in both directions rather than described.

**Ratified** by Ansh, 2026-08-17, as part of the step 8 ruling. **Anchor:**
`cmd/simctl/freshprocess_test.go:39` `TestFreshProcessTraceHashIsStable` (four invocations, two
perturbed) and `:84` `TestFreshProcessGateDetectsDivergence`; `sim/trace.go`; `sim/trace_test.go`.
Recorded cross-invocation hash, seed 4242, darwin/arm64:
`046a9ce5f129c15948279ba8e2e8ed59a9621a9a7a65ff8184ed5c4954ab055a` (moved once from `a679fba6bc13468491e9cb06745609810d97c9e145925f658f8bd5d15574e6de` by the fire-count fixes; see DESIGN-A0.10 §3), held for comparison against CI's
runner when the remote lands.

This step has no landing-plan row; it rode with step 8 because `replay` is by definition a
fresh-process re-execution, so the gate lands where the process boundary already is.

### Step 3 — transport over the real wire codec. CLOSED

**Exit condition.** Every simulated message crosses the production encoder; the codec is fixed-width
and explicit, with every truncation of a valid frame rejected; per-message dice are a keyed PRF over
`(from, to, ordinal on that directed link)` so one link's traffic cannot perturb another's.

**Ratified** by Ansh, 2026-08-11. **Anchor:** `docs/DESIGN-A0.7-transport.md`; `sim/transport.go`,
`sim/codec.go`; `sim/transport_test.go` (`TestCodecRoundTrip`, `TestCodecRejectsMalformedFrames`,
`TestDiceAreIdentityKeyedNotSequential`, `TestSendIsFireAndForget`).

Link independence is a **blessed property with a forward binding**: `minimize`'s design doc must cite
`TestDiceAreIdentityKeyedNotSequential` as a soundness precondition when it is built.

### Step 4 — fault injectors with fire counts. CLOSED

**Exit condition.** Drop, delay, duplicate, reorder, directed partitions, crash, restart; every
injector counts its fires, `Require` declares a minimum, and a shortfall is a run failure rather than
a note — the difference between a chaos suite and a chaos-shaped decoration. Partitions are directed,
so the asymmetric case that produces send-but-not-receive is generated.

**Ratified** by Ansh, 2026-08-11. **Anchor:** `docs/DESIGN-A0.7-transport.md`; `sim/transport.go`,
`sim/counters.go`; `sim/transport_test.go` (`TestFaultsActuallyFire`, `TestPartitionsAreDirected`,
`TestShortfallIsAFailure`).

Directed cuts are a **blessed property with a forward binding**: A1's schedule mix weights and
DESIGN-A2's pre-vote argument cite this geometry.

### Step 5 — the fault plan schema. CLOSED

**Exit condition.** A plan is a total repro with **no live RNG**: generation may draw sequentially,
execution draws nothing, and a built run carries a poisoned `Rand` whose every method panics — so
each seed that completes is that seed's own proof. Plans round-trip, carry no floating point, are
validated independently of who wrote them, and deleting one fault entry perturbs only itself.

**Ratified** by Ansh, 2026-08-11, with three corrections applied. **Anchor:**
`docs/DESIGN-A0.8-plan.md`; `sim/plan/plan.go`, `build.go`, `materialize.go`; `sim/plan/plan_test.go`
(`TestReplayFromPlanTakesNoLiveDraw`, `TestDeletingAFaultEntryPerturbsOnlyItself`,
`TestValidationRejectsIllegalHolds`, `TestHoldRealizationIsFlippable`) and `nofloat_test.go`
(`TestPlanCarriesNoFloatingPoint`).

### Step 6 — the oracle framework. CLOSED

**Exit condition.** Three verdicts, with inconclusive never a shade of pass and the zero verdict
rejected rather than defaulted; a minimum-operations floor enforced by `CheckAll` before any checker
runs, so a check that consumed nothing cannot bank a green; halting at the *first* violation; oracles
reach node state through nothing; and the gate induced in both halves — a real violation caught, and
the wiring proven by a planted hit that halts the run.

**Ratified** by Ansh, 2026-08-11. **Anchor:** `docs/DESIGN-A0.9-oracles.md`; `sim/oracle.go`,
`sim/history.go`, `sim/checker/porcupine.go`; `sim/oracle_test.go` (`TestPlantedViolationHaltsTheRun`,
`TestOracleHaltsAtTheFirstViolation`, `TestVerdictUnsetIsNotAPass`, `TestHaltedRunBanksNoSoakHours`)
and `sim/checker/porcupine_test.go` (`TestEmptyHistoryIsInconclusiveNeverPass`,
`TestTimeoutIsInconclusiveNotPass`, `TestUnavailabilityIsNotAViolation`).

### Step 7 — the toy over 1k seeds. PARTIAL

**Exit condition.** The toy survives 1k seeds with zero violations and a bounded inconclusive rate,
**and** a knowingly broken toy is caught by a hunt rather than by a hand-built fixture, with
seeds-to-first-detection recorded per planted flaw class and asserted in both directions — an
observable flaw that is missed fails, and an unobservable flaw that is suddenly caught fails too.

**Landed.** `sim/toy/toy.go`; `sim/hunt/hunt_test.go` (`TestToySurvivesOneThousandSeeds`:
1000 seeds, 1000 deadline, 1000 pass, 0 violation, 0 inconclusive, 30000 ops.
`TestBrokenToyIsCaughtByAHunt`: `ack-before-sync` 82/1000, seeds-to-detection 30;
`ack-before-replicate` 0/1000, recorded gap). Findings recorded in
`docs/DESIGN-A0.10-toy-and-simctl.md`.

**Both outstanding conditions were met and the step was closed by Ansh, 2026-08-17:**

1. **The uniform-crash ablation cell.** Placement is a `hunt.Placement` parameter, and
   `TestAblationCrashPlacementAndWindow` measures both at 50ms: reactive 504/1000 with
   seeds-to-detection 1, uniform 44/1000 with seeds-to-detection 12. Reactive targeting wins on both
   axes by an order of magnitude.
2. **Promotion in the toy.** `plan` carries a `promote` action, the toy takes `SetPrimary`, and
   `ack-before-replicate` under failover is caught by 35 of 1000 seeds with seeds-to-detection 7. The
   recorded gap is closed with a number rather than an argument, and the no-failover row is retained
   so the claim stays falsifiable.

**What the work found on the way**, and what Ansh has not yet ruled on:

- A harness defect: `Trigger` counted `Times` per condition rather than per rule, so a restart rule
  sharing a trigger with a crash rule never fired. `ack-before-sync` detection was 82/1000 and is now
  504/1000 — the harness had been at a sixth of its power. Recorded in the commit message, not
  BUGS.md, per DR-29.
- Two defects in the toy itself, both found by sweeping the *correct* toy: **BUG-001** (dirty read of
  uncommitted state) and **BUG-002** (acknowledgements counted rather than attributed, so a
  duplicated ack satisfied a quorum). Both triaged through the stripped-fault gate, both fixed, both
  carrying a new mutant class per Amendment A2, and both now in `seeds/` as the corpus's first two
  entries.

### Step 8 — `simctl run | replay`, and the bundle chain. CLOSED

**Exit condition, in two halves.**

- **(a) The fresh-process gate.** As step 2. **RATIFIED** by Ansh, 2026-08-17, explicitly including:
  both directions induced, four invocations with perturbed `GOGC`/`GOMAXPROCS`, first-divergent-step
  demonstrated live rather than described, stripped-fault replay as an affordance, and the seed 4242
  hash recorded for the CI comparison.
- **(b) The bundle chain. CLOSED 2026-08-17.** `simctl run --workload toy` drives the toy on
  explicit selection; a violation produces a bundle carrying the plan, the seed, the violating step
  and the violating history; and replay reproduces the *verdict*, not merely the hash.

**Why (b) mattered.** Before it, every `simctl` run was built out of `noopNode{}`, so the gate hashed
the loop, transport, plan and clock and never the toy, which was reachable only through
`go test ./sim/hunt`. No toy-level violation could become a replayable bundle and `seeds/` held only a
README — the entire repro chain, with step 9's hunt having nothing to hand a human and A1's first
corpus entry no mechanism behind it. `seeds/` now holds two entries that replay.

**Anchor:** `cmd/simctl/main.go`; `cmd/simctl/freshprocess_test.go` (five tests, including
`TestToyViolationBundlesAndReplays`); `sim/hunt/scenario.go` (`RunToy`, `MaterializeToy`, shared with
the sweep so the two cannot drift); `sim/trace.go` (`DivergenceReport`, `StepAt`);
`docs/DESIGN-A0.10-toy-and-simctl.md` §3; `seeds/BUG-001`, `seeds/BUG-002`.

### Step 9 — `simctl hunt`. LANDED, awaiting a ruling — A0-close-blocking

**Exit condition.** `hunt` sweeps seeds until a violation and hands a human a replayable bundle: the
seed, the plan, the first-violating step, the violating history. It is the mechanism that turns a
soak-farm violation into a corpus entry, which is what `seeds/` and every reproducibility claim in
CLAUDE.md depend on. It consumes step 8(b) directly.

`minimize` is **cut to STRETCH.md** (`STRETCH.md:114-120`, ratified under Amendment A6): a minimizer
with no failing corpus entry to minimize is a tool built against an imagined input. The cut is to
`simctl run | replay | hunt`. When `minimize` is eventually built, its design doc cites step 3's
`TestDiceAreIdentityKeyedNotSequential` and step 5's `TestDeletingAFaultEntryPerturbsOnlyItself` as
soundness preconditions.

**Anchor:** `sim/hunt/sweep.go` (`Sweep`, `Summarize`); `cmd/simctl/main.go` (`cmdHunt`);
`sim/hunt/sweep_test.go` (`TestWorkerCountDoesNotAffectResults` at 1, 2, 3, 8 and 32 workers,
`TestSweepRejectsABackwardsRange`).

A violation is bundled and triaged **before** it is reported: the hunt re-runs the winning seed with a
trace attached, writes the bundle, runs the stripped-fault replay, and prints the triage verdict
beside the finding. Reporting a seed number without those two steps would hand over a homework
assignment, and they are the two easiest to skip under pressure.

Worker independence is structural rather than tested into existence — each seed is a complete run,
results are written to a preallocated slice at the seed's own index, and no worker reads another's
result — and asserted at five worker counts. The CLI's own output is byte-identical at 1, 2 and 8
workers.

`make smoke` and `make soak` are no longer stubs: both are `simctl hunt` over a seed range.

### Step 10 — the mutant suite as patches. NOT STARTED

**Exit condition.** Mutants are patches applied to a scratch worktree (`sim/mutants/*.patch`, DR-27),
each header naming the mutant ID and the failure class it validates; every mutant is killed within
its budget; **kill-time per mutant is recorded** — seeds-to-detection and wall-time-to-detection —
and a kill-time regression is treated as a harness regression even while every mutant still dies. The
lane runs on every push. This is permanent policy under Amendment A2, not an A0 acceptance device.

**Anchor:** none yet. `sim/mutants/` does not exist; `make mutants` is a stub (`Makefile:86-88`). The
six oracle-targeting mutant classes are specified in advance at `docs/DESIGN-A0.9-oracles.md:144-158`.

The analogous machinery **one level down is built and green**: `tools/determinismcheck/blind/` holds
19 patches, and `make blind` reports 18 killed, 1 canary alive, 0 mismatched. That is the shape step
10 reproduces for the system under test.

### Step 11 — `node/`, the real-mode mailbox driver. LANDED, awaiting a ruling — A0-close-blocking

**Exit condition.** `node/` exists, and the mailbox rule has end-to-end teeth: in real mode every
cross-goroutine interaction — transport receive, durability completion, timer fire, client request —
enters a node through its mailbox, and core state touched off the node loop is a build failure. This
is what makes the mailbox rule **non-provisional**.

**Why it blocks A0.** The rule is enforced in three layers today, and the primary one is missing its
subject. `tools/determinismcheck/scope.go:93-97` says so in the code: *"`node/` does not exist yet —
it lands with the real-mode driver. Until then it is proven by fixtures only, and DESIGN-A0 marks it
provisional: A0 does not exit until `node/` exists and the rule has end-to-end teeth."*
`docs/DESIGN-A0-simulator.md:119` carries the same amendment. A rule proven only against fixtures is
a rule that has never met real code.

**Anchor:** `node/node.go` (`Driver`, `Post`, and the `sim.Scheduler` implementation);
`node/node_test.go`. Excluded from the determinism pass by name, with the polarity pinned in
`TestScopeTable`: `node/` out, the `sim.Node` it drives in.

**One `Node` interface, two modes.** `Driver` drives a `sim.Node` — the same interface the simulator's
loop drives, same `Handle(Event, Scheduler)` signature, no build tag and no branch on mode. The two
modes differ only in who calls `Handle` and when: one goroutine and virtual time from a seeded queue,
or one goroutine per node and wall time from a mailbox. `TestSameNodeLogicRunsUnderBothDrivers` runs
one implementation both ways.

**The proof that matters** is `TestRealModeDoesNotPerturbSimDeterminism`: the sim path's trace hash is
taken, four real drivers are exercised concurrently against the same node logic with real timers and
real goroutines, and the hash is taken again from scratch. Identical. Real mode exists without
weakening the determinism claim, which is the whole point of building it.

`defaultMailbox` is now empty, and correctly so rather than by omission. The mailbox rule constrains
packages holding *both* node state and concurrency; `node/` holds only the concurrency and reaches
node logic through an interface, so the compiler enforces the separation more strongly than the
analyzer rule would have. The rule, its fixtures and `blind-mailbox.patch` all stay, in force for the
first package that does hold both.

---

## 3. What A0 cannot close without

Ordered by what unblocks what, not by step number:

1. **Step 9** — `simctl hunt`. Its input, the bundle chain, now exists; it produces the corpus
   entries `seeds/` is for at scale rather than one seed at a time.
2. **Step 10** — the mutant suite as patches, with kill-time recorded per mutant. Five flaw classes
   now carry a standing seeds-to-detection number, two of them at 1 in 1000.
3. **Step 11** — `node/`, which converts the mailbox rule from provisional to enforced.

Steps 1 through 8 are closed. Nothing outside 9, 10 and 11 blocks A0.

Beyond the eleven steps, A0's exit criteria in CLAUDE.md also require that `make smoke` stop being a
stub (`Makefile:79-80`), since "a toy state machine survives 1k seeds" is currently proven by
`go test ./sim/hunt` rather than by the lane named for it.

**Only Ansh marks a step closed.** This file records status; it does not confer it.
