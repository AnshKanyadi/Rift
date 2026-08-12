# CLAUDE.md: Rift, a distributed transactional key-value database

Full specification. Go distributed layer, C++ storage engine, verified by deterministic simulation and a continuous soak farm.

> Amendments to this constitution are recorded at the bottom under **Amendments**, and are marked
> inline as `[A<n>]`. Amendments are made only by Ansh, and only with a written rationale.

## Mission

Build a distributed, transactional, MVCC key-value database: from-scratch multi-group Raft in Go, a from-scratch LSM-tree storage engine in C++ underneath, Percolator-style and parallel-commit transactions over hybrid logical clocks, linearizable reads via read index and leader leases, dynamic range splitting and load-based rebalancing. Every safety claim must be reproducible on demand: any historical bug replays from a single seed, every benchmark number reproduces from a clean clone by script.

Headline claims we are building toward (never stated anywhere until true and reproducible):

1. From-scratch Raft: leader election, log replication, persistence, snapshots, pre-vote, leadership transfer, single-node membership changes with learner catch-up. (Joint consensus is STRETCH.md `[A6]`.)
2. Multi-raft: data split into ranges, each range its own Raft group, dynamic size-threshold splitting, and replica movement by manual rebalance command. (Automatic load-based balancing is STRETCH.md `[A6]`.)
3. Distributed transactions over MVCC: Percolator-style 2PC, hybrid logical clocks with uncertainty intervals, snapshot isolation. (Parallel commits are STRETCH.md `[A6]`.)
4. Linearizable reads via read index. (Expiration-based leader leases and the clock-skew envelope are STRETCH.md `[A6]`; the clock machinery they need landed in A0.4.)
5. A from-scratch C++ LSM storage engine (skiplist memtable, WAL, SSTables with bloom filters and block index, leveled compaction, MANIFEST) behind a batch-oriented cgo interface.
6. Zero safety violations across [N] seeded fault schedules, [M] operations, and [H] cumulative CPU-hours of fault-injected soak (tracked in SOAK.md), under crashes, restarts, partitions, message drop/delay/duplication/reordering, bounded clock skew, and loss of unsynced writes.
7. [X] ops/s sustained with p99 [Y] ms on a [Z]-node cluster while a chaos script kills the leader every 10 seconds, including kills mid-compaction.

## Roles

Ansh is the architect. Claude is the implementation pair. Non-negotiable rules:

- Every phase starts with `docs/DESIGN-<id>-<topic>.md`: problem, 2 to 3 candidate designs, tradeoffs, recommendation. STOP and wait for Ansh's decision. Record the decision AND the rejected alternatives with reasons. Write these as if you will be cross-examined on them, because Ansh will be.
- Never mark a phase complete. Ansh does, after exit criteria pass.
- A failing test or checker means the code is wrong until proven otherwise. Never weaken, skip, tune, or delete a checker to get green. If you believe a checker is itself buggy, stop and make the written case first.
- Small diffs. Run the relevant test lane after every change.
- No new dependencies without approval. Pre-approved: `github.com/anishathalye/porcupine`, gRPC for real-mode transport, CMake and GoogleTest on the C++ side, and `golang.org/x/tools` for the `go/analysis` determinism vet pass — tooling only, never linked into a shipping binary `[A1]`. Everything else: ask. Randomness is supplied by `internal/rng`, a project-owned PCG64 with pinned test vectors and named sub-streams; it is not an external dependency and it does not use `math/rand` `[A1]`.

## Two-track structure

The build runs as two parallel tracks plus an integration stage. Track B depends only on the `engine/` interface defined in A0, so both tracks start immediately. Run them in separate git worktrees with separate Claude Code sessions; this file is the shared constitution for both.

- Track A (Go): simulator, Raft, multi-raft, MVCC, transactions, reads, rebalancing. Uses `engine/model` (a simple deterministic Go engine) throughout, so consensus and transaction bugs never alias with storage bugs.
- Track B (C++): the LSM engine, developed and verified standalone against its own fault-injecting environment, plus differential tests against `engine/model`.
- Integration: swap the C++ engine under the full stack, rerun the seed corpus in verification mode, run real-cluster chaos, take final numbers.

## Architecture

1. `raft/`: pure state machine. No goroutines, no channels, no clocks, no I/O. Input: `Step(Message)` and `Tick()`. Output: a `Ready` struct (messages to send, entries to persist, entries to apply, snapshot ops). Interface style inspired by etcd/raft, implementation from scratch. This purity is what makes deterministic simulation possible; it is non-negotiable.
2. `store/`: multi-raft node. Hosts many Raft groups (one per range) over one shared transport, drives persist/apply loops, executes splits, reports per-range load stats.
3. `kv/`: MVCC and transactions. Versioned key encoding; data, lock, and write records; prewrite/commit/resolve; parallel-commit staging records and recovery; reads at a timestamp with uncertainty handling.
4. `router/`: client library. Range descriptor cache, retries on NotLeader and StaleRangeEpoch, transaction coordinator (both Percolator and parallel-commit paths).
5. `clock/`: hybrid logical clock with configurable maxOffset; sim and real implementations.
6. `balancer/`: load-based rebalancing. Store heartbeats carry per-range QPS and write-bytes; the balancer computes moves. DESIGN-A10 decides the metadata topology: meta ranges stored in the system itself (Cockroach style) versus a standalone placement service (PD style).
7. `engine/`: the storage interface both engines implement: batched writes with a sync flag, point reads, iterators, engine snapshots, approximate size for split decisions, and `DeleteRange` over `[start, end)` `[A3]`. `engine/model` is the deterministic Go reference. `engine-cpp/` is the C++ LSM plus cgo bindings.
8. `sim/`: deterministic simulator and fault injectors. `cmd/simctl`: run seeds, replay a seed, hunt until failure, minimize a failing schedule.
9. `bench/`, `chaos/`, `soak/`: load generators, real-mode chaos scripts, and the soak-farm runner that updates SOAK.md.

In sim mode, node logic is event-driven: the simulator owns one event queue (message deliveries, timer ticks, crashes, restarts, fsync completions, clock adjustments) and delivers one event at a time from a seeded RNG. Same seed, same trace, byte for byte, when running on `engine/model`.

## Determinism rules, Go side (violations are bugs)

Which packages these rules bind is settled by the scope principle: any code that executes during a simulated run is in scope, orchestration around runs is not, and an unclassified package defaults in `[A5]`.

- No `time.Now()`, `time.Sleep`, `time.After` outside the real clock implementation. Inject `Clock`.
- No global `math/rand`. Inject `Rand` owned by the simulator `[A1]`.
- No direct network or filesystem access in `raft/`, `store/`, `kv/`, `router/`, `balancer/`. Inject `Transport` and `Engine`.
- Never iterate a map where order can affect behavior; sort keys first. This is the classic Go determinism leak.
- In sim mode, each node's logic runs single-threaded off the event loop; goroutine scheduling nondeterminism must not reach core logic.
- In real mode, every cross-goroutine interaction — transport receive, durability completion, timer fire, client request — enters a node through its mailbox. Core state touched off the node loop is a bug `[A1]`.
- Fsync is modeled: writes buffer until a sync event completes; a crash discards unsynced writes.
- Clock skew is modeled: per-node drift and jump schedules, bounded by maxOffset in safety runs, deliberately exceeding it in envelope experiments.

## Determinism and fault injection, C++ side

Env is the C++ side of the same boundary the Go determinism pass enforces: every syscall goes through it for the reason every clock read goes through `Clock` `[A5]`.

- The engine performs all file operations through an `Env` abstraction (open, read, write, sync, rename, list), LevelDB style. Production Env hits the real filesystem. TestEnv injects: sync loss windows, torn writes at arbitrary byte offsets, IO errors, disk-full via quota, and kill points at any syscall boundary.
- Crash-consistency rig: run a randomized workload, kill at a swept set of Env call points, reopen, verify recovered state against the operation log. Every recovery invariant violation gets a BUGS.md entry like any sim bug.
- Differential testing: apply identical operation sequences to `engine/model` and the C++ engine; full iterator output must be byte-identical. Run this under randomized workloads continuously.
- Deterministic-replay guarantees are scoped to sim runs on `engine/model`; C++ engine correctness comes from the Env fault rig, differential tests, corpus reruns in verification mode, and real chaos. State this scoping honestly everywhere claims appear.

## cgo boundary rules

- The C API is `extern "C"`, error codes not exceptions, no C++ types across the boundary.
- Batch everything: WriteBatch commit is one call; iterators return blocks of N key-value pairs per call. Per-call cgo overhead is real; the interface must amortize it, and BENCHMARKS.md must measure the boundary cost explicitly (same workload, C++ engine called from Go versus from a native C++ harness).
- Respect cgo pointer rules: no Go pointers stored by C beyond the call. Buffers are copied at the boundary or owned by the C++ side with explicit free calls.
- No C-to-Go callbacks. The C++ engine exposes a blocking `Sync()`; the Go wrapper's per-engine poller goroutine adapts it to the async durability contract and posts completions to the node mailbox `[A1]`.
- Build: CMake produces a static archive; cgo links it. C++ CI lanes run the engine's own tests under ASan and UBSan directly, not through cgo.

## Invariants (the sacred list)

Checked continuously; any violation fails the run, dumps the seed or kill point, and halts.

- Election safety: at most one leader per term. Log matching, leader append-only, leader completeness, state machine safety.
- Joint consensus safety: during C_old,new, elections and commits require majorities of both configs; configuration entries take effect when appended; snapshots carry the active config; overlapping membership changes are rejected; a leader excluded from C_new steps down after committing it.
- Committed is forever, across any crash/restart schedule.
- Linearizability of single-key reads and writes (porcupine, per key), including reads served via read index and via leases.
- Lease disjointness: under skew within maxOffset, no two nodes' lease validity intervals for a range ever overlap.
- Transaction atomicity across ranges and splits, for both Percolator and parallel-commit paths, including all recovery paths.
- Snapshot isolation: no read observes a partial commit; the bank workload conserves total balance exactly; uncertainty-interval restarts preserve it under skew.
- Parallel-commit recovery determinism: any observer resolving a STAGING record reaches the same commit-or-abort outcome.
- Range epoch monotonicity: no request served under a stale descriptor epoch.
- Rebalance safety: replica moves are add-then-remove; quorum availability is never voluntarily reduced.
- Client request dedupe: retried requests apply at most once.
- Storage recovery: after any crash, the engine recovers exactly the acknowledged-synced prefix; iterators over both engines agree byte for byte in differential runs.

## Track A phases

Gate: no phase starts until its predecessor's exit criteria are signed off by Ansh.

**A0: harness and interfaces.** Repo skeleton; `Clock`, `Rand`, `Transport`, `Engine` interfaces; `engine/model`; event-loop simulator with seeded scheduler; fault injectors (drop, delay, duplicate, reorder, symmetric and asymmetric partitions, crash, restart, unsynced-write loss, clock drift/jump); structured logging keyed by seed, node, term, range; `simctl run | replay | hunt | minimize`. Exit: a toy state machine survives 1k seeds; identical seeds give identical traces; injector fire counts asserted; the mutant suite kills every mutant within its budget `[A2]`.

**A1: single-group Raft.** Elections, heartbeats, replication, persistence with correct sync ordering (term, vote, log durable before replying to any RPC), figure-8 commit rule (only current-term entries commit by counting). Simple KV state machine. Exit: 10k mixed-fault seeds, porcupine green, and a nonempty BUGS.md. If the harness finds zero bugs, the harness is too weak; strengthen it before proceeding.

**A2: snapshots, compaction, pre-vote, leadership transfer.** InstallSnapshot racing appends and restarts is the danger zone. Exit: crash storms plus partitions with snapshot transfers in flight, 10k seeds green.

**A3: single-node membership changes** `[A6]`. Add and remove one voter at a time, learner catch-up before promotion, config-across-snapshot correctness, leader self-removal stepdown. Exit: continuous membership churn under faults, including leader removed mid-transition and snapshot during a pending change, 15k seeds green. DESIGN-A3 documents the choice and what joint consensus would have bought. *Joint consensus is STRETCH.md; production etcd omits it too, and its correctness surface is not justified for v1.*

**A4: multi-raft.** Range descriptors over [start, end) with epochs; many groups per node over one transport; router cache and retry semantics; size-threshold splits bootstrapping the right-hand range without stopping traffic. Range merges remain out of scope; document why. Exit: workloads spanning many splits mid-traffic, per-key linearizability green, 10k seeds.

**A5: MVCC and HLC.** Versioned key encoding on the engine interface; data, lock, write records; reads at a timestamp; basic version GC; HLC with maxOffset, updated on every message receipt. Exit: MVCC semantics suite deterministic and green; HLC causality property tests green under skew schedules.

**A6: Percolator transactions with uncertainty.** Prewrite with lock and write-conflict checks; commit primary then secondaries; reader-side lock resolution via primary status; transaction TTL and heartbeat; transaction records on the primary key's range with epoch checks so splits cannot orphan them; reads collect uncertainty intervals and restart with a bumped timestamp on ReadWithinUncertaintyInterval; per-node observed timestamps to shrink intervals. Exit: bank workload across ranges with splits, crashes, partitions, and bounded skew: conservation, atomicity, and SI checkers green across 25k seeds; single-key porcupine still green.

**A7: read index.** Full protocol including the term-start no-op requirement; follower reads via read index. Exit: staleness checker green under partitions and leader churn.

**A8: leader leases. — STRUCK FROM THE ACTIVE PLAN** `[A6]`, moved to STRETCH.md. Read index is correct without trusting clocks; leases are a read optimization inside a clock-skew envelope. The clock machinery the envelope experiment needs already landed in A0.4, so this is cut on scope rather than capability. Original scope, retained for the record: Expiration-based leases acquired and transferred as replicated Raft commands; leaseholder serves reads locally only when now plus maxOffset is inside the lease; lease and leadership colocation preference. Exit: lease disjointness checker green across skew-within-maxOffset schedules, 15k seeds; a documented envelope experiment showing what breaks when skew exceeds maxOffset, with the detection story.

**A9: parallel commits. — STRUCK FROM THE ACTIVE PLAN** `[A6]`, moved to STRETCH.md. A latency optimization whose price is a recovery protocol every observer must execute identically; Percolator's extra round trip is a measured cost, and BENCHMARKS.md will state it. Original scope, retained for the record: STAGING transaction records with in-flight write lists; implicit commit once all writes are durable at their sequence; recovery protocol for any observer encountering STAGING (verify in-flight writes, then commit or abort); explicit async commit-record cleanup. Exit: coordinator-death-between-staging-and-cleanup schedules resolve deterministically; atomicity and SI checkers green across 25k seeds; measured commit-latency win versus the Percolator path recorded in BENCHMARKS.md, since that latency win is the entire point.

**A10: rebalancing — COLLAPSED INTO THE A4 RIDER** `[A6]`. v1 ships the *mechanism* (replica movement by conf change plus leadership transfer) as a manual rebalance command riding with A4; the automatic policy moves to STRETCH.md. Rebalance safety is unchanged and still checked: add-then-remove, quorum availability never voluntarily reduced, no request served under a stale epoch. Original scope, retained for the record: Store heartbeats with per-range QPS and write-bytes; balancer computes moves against a mean-plus-threshold heuristic; a move is add replica, wait for catch-up, transfer lease and leadership, remove replica, throttled to one in-flight move per range; DESIGN-A10 resolves meta topology (in-system meta ranges versus placement service). Exit: a hot-range workload converges to bounded load spread with zero availability loss; node kills during moves leave no epoch or quorum violations; 15k seeds green.

## Track B phases (C++ engine, starts immediately after A0 freezes the interface)

**B1: Env, WAL, memtable.** Env abstraction with production and test implementations; skiplist memtable; WAL with checksummed records; clean recovery of the synced prefix. Exit: recovery property tests green; TestEnv sync-loss schedules produce exactly-the-acknowledged-prefix recovery.

**B2: SSTables and manifest.** Block-based SSTable format with index and bloom filters; MANIFEST/version-set for atomic state transitions; flush path. `DeleteRange` is implemented internally as iterate-and-point-delete in this phase: correct but slow, so B4's differential tests exercise the semantics early rather than discovering them at I1 `[A3]`. Exit: format round-trip and corruption-detection tests green under torn-write injection.

**B3: compaction and iterators.** Leveled compaction with scoring; merged iterators; engine snapshots pinning versions. Real range tombstones land here, replacing B2's iterate-and-point-delete `DeleteRange`, and must be in place before any I2 benchmark number is taken `[A3]`. Exit: compaction correctness under concurrent reads; space and read amplification measured and recorded.

**B4: verification hardening.** Crash-consistency rig sweeping kill points across every Env call in flush, compaction, and manifest swap; differential harness against `engine/model` running continuously; ASan and UBSan lanes clean. Exit: kill-point sweep green across the full write path; differential runs green across randomized workloads; BUGS.md entries exist (same rule: zero bugs found means the rig is too weak).

**B5: cgo bindings and standalone numbers.** extern C batch API; Go wrapper implementing `engine/`; benchmark fillrandom, readrandom, and mixed workloads for the C++ engine natively and through cgo, and for `engine/model`, in one honest table. Exit: differential tests pass through the cgo path; boundary overhead quantified.

## Integration phases

**I1: engine swap and corpus rerun.** Run the full Track A stack on the C++ engine. Rerun the entire historical seed corpus in verification mode (checkers on, trace identity not required). Exit: all checkers green on the C++ engine; any divergence becomes a BUGS.md entry with a differential reproduction.

**I2: real mode, chaos, final numbers.** Real transport (DESIGN-I2: gRPC versus length-prefixed TCP); YCSB-style mixes; HDR histograms; chaos runner killing leaders every 10s including kills mid-compaction; disk-full and torn-write injection through the production-adjacent Env in a chaos lane. Deliverables: BENCHMARKS.md with hardware, methodology, warmup, workload definitions, p50/p99/p999, throughput-under-chaos graphs, recovery times, both engines; a scripted 90-second demo. Exit: every number reproduces from a clean clone by one script.

## Soak farm (the machine behind the hours claim)

- `soak/` runs seed hunting continuously: nightly CI soaks plus an always-on local runner using all cores, mixed schedules across every phase's workloads, both transaction paths, both engines where applicable.
- SOAK.md is an append-only ledger updated by the runner: date, commit, seeds executed, operations executed, CPU-hours consumed, violations found (with seed and BUGS.md link) or zero, and inconclusive results `[A4]`.
- Checkers report one of three outcomes: pass, violation, or inconclusive. An inconclusive result — a linearizability check that hit its timeout — is never counted as a pass. If the inconclusive rate grows, shrink per-run history windows or partition harder per key. Never loosen the checker `[A4]`.
- The public claim quotes cumulative CPU-hours and operations from SOAK.md and nothing else, and quotes the inconclusive rate alongside the violation count `[A4]`.
- At 32 workers running nightly this accrues thousands of CPU-hours within weeks; the claim becomes true by accumulation, not assertion.
- A violation found by soak halts feature work on that track until root-caused and fixed.

## CI lanes

Go unit plus race on every push; 500-seed smoke on every push; 10k-seed soak nightly; C++ unit under ASan and UBSan; crash-consistency kill-point sweep nightly; differential engine lane nightly; benchmark smoke weekly with regression tracking; mutant-suite lane on every push, recording kill-time per mutant `[A2]`.

## Artifacts (always current)

- `docs/DESIGN-*.md`: decisions plus rejected alternatives with reasons.
- `BUGS.md`: every bug caught by sim, crash rig, or differential testing: symptom, seed or kill point, root cause, fix commit, the invariant that caught it, which mutant class would have caught it, and what it would have caused in production. If no existing mutant class would have caught it, a new mutant is added in the same PR as the fix `[A2]`. This file is the proof behind the verification claim and the best interview material in the repo.
- `SOAK.md`: the cumulative verification ledger, including the inconclusive column `[A4]`.
- `STRETCH.md`: everything deliberately outside v1, with the reasoning preserved. Never claimed `[A6]`.
- `BENCHMARKS.md`: methodology first, numbers second, both engines, boundary costs included.
- `README.md`: 90-second pitch, architecture diagram, and How It Is Verified above the feature list, linking the verification-scope idealizations from DESIGN-A0 §7.
- `seeds/`: failing-seed corpus; `simctl replay <seed>` reproduces any historical bug at the commit that contained it, and the bundled plan reproduces it at any commit.

## Sharp edges checklist

Persist before reply. Figure-8 commits. InstallSnapshot versus append races. Joint config effective-on-append, carried in snapshots, no overlapping changes, leader stepdown on self-removal. Split versus in-flight transaction: epoch checks and primary-range record placement. Read index needs the term-start no-op. Lease stasis: never serve past expiration minus maxOffset. Uncertainty restarts must bump past the observed value's timestamp. STAGING resolution must be idempotent and race-safe against the coordinator finishing late. Map iteration order. Unsynced-write loss windows. Duplicate client delivery. cgo pointer rules and per-call overhead. Compaction versus snapshot pinning. Manifest swap atomicity.

## Resume lines this repo must back verbatim, on demand

Fill numbers only from SOAK.md and BENCHMARKS.md.

- Built a distributed transactional KV database: from-scratch multi-group Raft in Go (elections, replication, snapshots, pre-vote, leadership transfer, joint-consensus membership changes) with dynamic range splitting and load-based rebalancing, and a from-scratch C++ LSM storage engine (WAL, SSTables, bloom filters, leveled compaction) underneath via a batch cgo interface.
- Implemented snapshot-isolated distributed transactions over MVCC with hybrid logical clocks: Percolator-style 2PC and parallel commits with a verified recovery protocol; linearizable reads via read index and leader leases with a proven clock-skew safety envelope.
- Zero safety violations across [N] seeded fault schedules, [M] operations, and [H] CPU-hours of continuous fault-injected soak, spanning crashes, partitions, reordering, bounded clock skew, torn writes, and lost unsynced writes; every bug ever found replays from a single seed ([K] documented postmortems).
- Sustained [X] ops/s with p99 [Y] ms while killing the leader every 10 seconds, including mid-compaction; steady-state recovery in [W] ms.

## Kickoff

Session 1 (Track A worktree): read this file end to end, then propose `docs/DESIGN-A0-simulator.md` covering the event-loop design, the Ready-struct Raft interface, the fault injector set including clock schedules, and the seed CLI. Wait for approval before writing code.

Session 2 (Track B worktree): read this file end to end, then propose `docs/DESIGN-B1-engine.md` covering the Env abstraction, WAL record format, memtable design, and the recovery contract. Wait for approval before writing code. Note that A0 has frozen the `engine/` interface, including `DeleteRange`; DESIGN-B1's scope must reflect the B2/B3 staging in `[A3]` and the no-C-to-Go-callbacks rule.

---

## Amendments

Amendments are made only by Ansh. Each records what changed, when, why, and where the full reasoning
lives. Inline occurrences are marked `[A<n>]`.

**A1 — Randomness, dependencies, and the mailbox rule.** Ansh, 2026-08-10, ruling on DESIGN-A0
(DR-2, DR-4, DR-11, DR-16). Randomness comes from `internal/rng`, a project-owned PCG64 with pinned
known-answer test vectors and named derived sub-streams — not from `math/rand`, and not an external
dependency. Rationale: Go's compatibility promise for `math/rand/v2`'s convenience mappings is too
weak to hang a permanent seed corpus on, and a silent change would leave every corpus entry
self-consistent but different. `golang.org/x/tools` is pre-approved for the `go/analysis` determinism
vet pass, tooling only, never linked into a shipping binary. In real mode every cross-goroutine
interaction enters a node through its mailbox; core state touched off-loop is a bug, enforced by
package boundaries, by the vet pass, and by a `-race` lane. C-to-Go callbacks across the cgo boundary
are prohibited; the Go wrapper's per-engine poller adapts the C++ engine's blocking `Sync()`.

**A2 — Mutant suite promoted to permanent policy.** Ansh, 2026-08-10, ruling on DESIGN-A0 §5
(DR-20). The mutant suite is a standing obligation, not an A0 acceptance device. Every BUGS.md root
cause must answer "which mutant class would have caught this." If no such class exists, a new mutant
is added in the same PR as the fix — not a follow-up issue. CI runs the suite on every push and
records kill-time per mutant (seeds-to-detection and wall-time-to-detection); a regression in
kill-time is treated as a harness regression even while every mutant is still killed. Rationale: the
moment a fix lands is the only moment we will ever have a precise description of the blind spot that
let the bug through, and kill-time turns harness sensitivity from a belief into a monitored number.

**A3 — `DeleteRange` stays in the frozen `Engine` interface.** Ansh, 2026-08-10, ruling on
DESIGN-A0 Q2/D7.1 (DR-13), overruling Claude's recommendation to exclude it. `engine/model`
implements it natively from A0.5. The C++ engine implements it internally as iterate-and-point-delete
through B2, so B4's differential tests exercise the semantics early; real range tombstones are a B3
deliverable and must land before any I2 benchmark number is taken. Rationale: freezing the interface
without it guarantees churn on both tracks later; snapshot application needs an atomic
clear-then-ingest that a best-effort Go helper cannot provide at the right isolation; and replica
removal at scale cannot be one giant point-delete batch without unbounded batch sizes and write
stalls.

**A4 — Inconclusive is a first-class outcome.** Ansh, 2026-08-10, ruling on DESIGN-A0 §D12 (DR-19).
Checkers report pass, violation, or inconclusive. SOAK.md carries an explicit inconclusive column,
and the public claim quotes the inconclusive rate alongside the violation count. When the rate grows,
shrink per-run history windows, then partition harder per key. Never loosen the checker — not the
timeout, not the model, not the operation set. Rationale: "zero violations across N seeds" is quietly
false if a fraction of those seeds never finished checking, and the usual cause of a rising
inconclusive rate is a workload drifting toward pathological concurrency, which is exactly when the
checker matters most.

**A5 — Determinism scope principle.** Ansh, 2026-08-11, ratified on the A0.3 follow-up rulings
(DESIGN-A0 D10, DR-23). Any code that executes during a simulated run is deterministic by
construction; orchestration around runs (hunters, real-mode drivers, `cmd/`) is not required to be.
Each language enforces the boundary with its own mechanism: Go through the determinismcheck scope,
C++ through the Env seam. Every clock read goes through Clock, every random draw through
`internal/rng`, every syscall on the C++ side through Env, all for the same reason. Exceptions are
per-line hatches in a golden registry, never package exclusions. Concurrency primitives are
unhatchable in core scope: code that needs a goroutine is orchestration and lives outside the
boundary, or the design is wrong. Unclassified packages default in.


**A6 — v1 scope.** Ansh, 2026-08-11, ratified after A0.5. The v1 deliverable: from-scratch Raft
(elections, replication, persistence, snapshots, pre-vote, leadership transfer, single-node
membership changes with learner catch-up); multi-raft with size-threshold range splits and a manual
rebalance command (replica movement via conf change plus leadership transfer, riding with A4); MVCC
with HLC timestamps and uncertainty-interval reads; Percolator-style snapshot-isolated transactions;
linearizable reads via read index; the C++ LSM engine (Env, WAL, memtable, SSTables with bloom
filters and block index, manifest, a correct compaction policy chosen and measured in DESIGN-B3)
behind the batch cgo interface; the full verification machinery and soak ledger. Moved to STRETCH.md
with rationale preserved: joint consensus (production etcd's own omission; correctness surface
unjustified for v1), parallel commits (latency optimization with a heavy recovery protocol;
Percolator's extra round trip is the measured cost), leader leases (read optimization; read index is
correct without clock trust), automatic load-based balancing (policy atop mechanisms v1 ships),
observed-timestamps optimization, multi-level leveled compaction beyond the v1 policy, simctl
minimize (build when the first corpus bug earns it). The timestamp source lands behind an interface
in A5; TSO fallback is pre-authorized if A6's uncertainty machinery is not green by Dec 1. Nothing in
the verification spine moves. Resume claims track v1; STRETCH items are never claimed.
