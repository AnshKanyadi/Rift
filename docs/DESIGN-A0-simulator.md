# DESIGN-A0: Harness, Interfaces, and the Deterministic Simulator

**Status:** **APPROVED** by Ansh, 2026-08-10, with amendments (see §9 Decision Record).
**Phase:** A0 (Track A). Blocks: everything in Track A; freezes `engine/` for Track B.
**Author:** Claude (implementation pair). **Decider:** Ansh.

Amendments are marked inline as **[Amended — Ansh, 2026-08-10]**. The full record with rationale and
rejected alternatives is §9; that is the section to read under cross-examination.

---

## 0. What A0 must deliver, restated from CLAUDE.md

Repo skeleton; `Clock`, `Rand`, `Transport`, `Engine` interfaces; `engine/model`; event-loop
simulator with seeded scheduler; fault injectors (drop, delay, duplicate, reorder, symmetric and
asymmetric partitions, crash, restart, unsynced-write loss, clock drift/jump); structured logging
keyed by seed, node, term, range; `simctl run | replay | hunt | minimize`.

**Exit:** a toy state machine survives 1k seeds; identical seeds give identical traces; injector fire
counts asserted.

A0 is the only phase whose output is *leverage* rather than features. Every bug the project ever
claims to have caught is caught by this code. It is worth over-designing.

---

## 1. Problem

We need an execution substrate with four properties that are individually easy and jointly hard:

1. **Bit-for-bit determinism.** Same seed ⇒ same trace, forever, on any machine, across Go
   releases. Without this, `simctl replay` is a lie and `seeds/` is decoration.
2. **Fault expressiveness.** The substrate must be able to express every failure the invariant list
   mentions — including the awkward ones: a write that is acknowledged, buffered, and then lost;
   two nodes whose clocks differ by 490ms when maxOffset is 500ms; a message delivered twice, out
   of order, an hour late.
3. **Speed.** The headline claim is measured in CPU-hours and operations. A simulator that runs
   1k ops/s of simulated work makes the soak farm worthless. Target: a full 3-node, 60-simulated-second
   fault run in well under one wall-second on one core.
4. **Honesty.** Anything the simulator idealizes away is a hole in the safety claim. Each
   idealization must be written down here, not discovered in an interview.

The tension is (1) vs (3) vs (4). Go's runtime is hostile to (1): map iteration is randomized,
`select` over ready channels is randomized, goroutine scheduling is preemptive, `time.Now` is
wall-clock. The design below removes Go's nondeterminism by construction rather than by discipline,
and then *enforces* the removal with a compiler-level check, because discipline does not survive
30k lines.

---

## 2. Decisions

Each decision: candidates, tradeoffs, recommendation, and what is rejected and why. Numbered so
you can rule on them tersely ("D1a, D3b, rest as proposed").

---

### D1 — Concurrency model: how does node logic execute?

**Candidates**

- **(a) Single-threaded event loop; node logic is a synchronous, non-blocking state machine.**
  No goroutines, no channels, no locks anywhere in `raft/`, `store/`, `kv/`, `router/`, `balancer/`.
  Every node exposes one entry point, `Handle(Event)`, which runs to completion and may schedule
  future events. In sim, the simulator calls `Handle`. In real mode, one goroutine per node drains
  a channel and calls the same `Handle`.
- **(b) Goroutines plus a cooperative deterministic scheduler.** Node logic written in natural
  blocking style; every blocking operation routed through a sim-owned scheduler that hands out a
  run token to exactly one goroutine at a time.
- **(c) Real goroutines, virtual clock, quiescence detection.** Let the runtime schedule; advance
  virtual time only when the simulator believes all goroutines are blocked.

**Tradeoffs**

(a) is the only option where determinism is a *structural* property. Its cost is real and should be
named: the store must be written in continuation-passing style — "issue write, remember what to do
on the sync-completion event" — which is more verbose than "write; fsync; reply". It also means the
real-mode driver must be written once, carefully, and never bypassed.

(b) reads better, and it is what `madsim`/`turmoil` do in Rust. In Go it is a trap: there is no
supported hook to control the scheduler, so *every* blocking primitive must be replaced (channels,
mutexes, `select`, `sync.WaitGroup`, timers, GC-triggered finalizers). One raw `<-ch` in a
subordinate package silently destroys determinism, and the failure mode is a flaky soak six weeks
later, which is exactly the failure mode this project exists to avoid. `select` over two ready cases
is randomized by the runtime and is not overridable.

(c) is flaky by construction. Quiescence detection cannot distinguish "blocked" from "about to run",
GC pauses perturb interleavings, and the determinism guarantee degrades to "usually".

**Recommendation: (a).** Additionally: the *same* `Handle` implementation runs in real mode, driven
by a thin per-node goroutine + inbound channel. Core logic is therefore single-threaded in both
modes, so a data race in core logic is not merely unlikely — it is unrepresentable. Real-mode
concurrency lives only in `sim/../real` drivers, transport, and the engine's syncer pool, all of
which are covered by the `-race` lane.

**Rejected:** (b) — Go provides no scheduler control; one missed primitive is an undetectable
determinism leak. (c) — quiescence detection is racy; determinism would be statistical.

**[Amended — Ansh, 2026-08-10] The mailbox rule.** In real mode, *every* cross-goroutine interaction
— transport receive, durability completion, timer fire, client request, admin RPC — enters the node
exclusively through its mailbox. Core state touched off-loop is a bug, full stop.

Enforcement is structural first, mechanical second, because a review checklist alone is worth
approximately nothing at 30k lines:

1. **Compiler-enforced (primary).** Core node state lives in unexported fields of types in the core
   packages. The real-mode driver lives in a *different* package (`node/`), so it is physically
   incapable of reaching that state. Its entire vocabulary is `Handle(Event)` plus the read-only
   `Status()`. There is no exported mutator to misuse.
2. **Vet-enforced (secondary).** `determinismcheck` gains a `mailbox` rule: within `node/`, no
   goroutine may call any method on a core type other than `Handle`, `Status`, and constructors, and
   the only permitted write to the mailbox channel is via `post()`. Any other cross-goroutine touch
   is a build failure.
3. **Race lane (backstop).** `node/` is exercised under `-race` in CI. Given (1) and (2) a race here
   should be impossible; the lane exists to prove that claim rather than assert it.

The review checklist gets the item too, but it is the third line of defense, not the first.

**[Amended — Ansh, 2026-08-11] The rule is provisional until `node/` exists.** A0.3 implemented the
vet layer and proved it against fixtures, which is all that can be proved while the package it
governs is unwritten. It is marked provisional deliberately: the rule gets end-to-end teeth the
moment `node/` lands, and **A0's exit criteria do not close until it does**. A rule that has only
ever been tested against a fixture of itself is a rule with an untested assumption about the shape of
the code it will meet.

---

### D2 — Time model and event ordering

**Candidates**

- **(a) Discrete-event simulation.** Virtual clock in nanoseconds. A priority queue of events; pop
  the earliest, jump the clock to it. Nothing is ever "waited" for.
- **(b) Fixed-tick lockstep.** Advance in quanta (1ms); deliver everything due in the quantum in a
  seeded shuffle.
- **(c) Untimed logical ordering.** A bag of pending events; pick one per step by RNG. No latency,
  no clocks.

**Tradeoffs**

(c) is the fastest bug-finder per line of harness code and is what many toy Raft fuzzers do, but it
cannot express "the lease expires at T", "this node's clock drifts 200ppm fast", or
"ReadWithinUncertaintyInterval". A8 and A5 would be unverifiable. Fatal.

(b) creates artificial synchronization: every node's timers align to quantum boundaries, which
systematically hides races that need sub-quantum interleaving, and wastes steps on empty quanta.

(a) is standard (FoundationDB, Shadow, `madsim`) and costs nothing extra. The one hazard is
**tie-breaking**: two events at the same virtual nanosecond must have a deterministic order.
`container/heap` is not stable.

**Recommendation: (a)**, with events totally ordered by the lexicographic key
`(at_nanos, insertion_seq)` where `insertion_seq` is a monotone counter owned by the simulator.
No `Kind`-based priority, no stability assumptions — insertion order is the tiebreak, and insertion
order is deterministic because everything upstream of it is.

Consequence to state plainly: **virtual time only advances at event boundaries.** Computation is
instantaneous. CPU cost is not modeled, so the simulator cannot find bugs that require a node to be
*slow* rather than *stopped*. Mitigation: injectors can add explicit processing delay to any
`Handle` call (`--slow-node` schedule), which covers the cases we care about (a node that is
GC-paused for 3 seconds while holding a lease). This is an idealization; it goes in README's
verification-scope section.

**Rejected:** (b) — quantum alignment hides sub-tick races. (c) — cannot express leases,
uncertainty intervals, or skew; would make A5/A8 unverifiable.

---

### D3 — The random number generator

The constitution says: *"No global `math/rand`. Inject `*rand.Rand` owned by the simulator."* Two
things need deciding beyond that: **whose generator**, and **how many streams**.

**Candidates for the generator**

- **(a) `math/rand` (v1), `rand.New(rand.NewSource(seed))`.** Go's compatibility promise explicitly
  freezes the v1 stream for a given seed.
- **(b) `math/rand/v2` with an explicit `rand.NewPCG(lo, hi)` source.** Modern, faster, explicitly
  named algorithm.
- **(c) Our own ~60-line PCG64 + rejection-sampled bounded ints, in `internal/rng`, with
  known-answer test vectors.**

**Tradeoffs**

(a) is stable but slow (Lagged Fibonacci, 4.8KB of state per stream, which matters if we derive
hundreds of streams) and its derived helpers (`Perm`, `Shuffle`) are stable only because Go promises
so for v1.

(b) is fast and the *sources* are specified, but the compatibility promise for v2's higher-level
mappings (`IntN`, `Shuffle`, `Perm`, `Float64`) is weaker than v1's frozen-stream guarantee. If Go
changes `IntN`'s rejection strategy in 1.26, every seed in `seeds/` stops reproducing — silently,
because the run will still be self-consistent, just different.

(c) puts the stream under our control permanently. Cost: 60 lines plus a KAT test. Benefit: the
sentence *"every bug ever found replays from a single seed"* survives Go upgrades, which is the
entire point of `seeds/`.

**Recommendation: (c),** exposing a `rift.Rand` interface (still "a Rand owned by the simulator",
just ours). PCG64-DXSM, bounded ints by Lemire's method with rejection, `Shuffle` as
Fisher–Yates descending. A `TestVectors` test pins the first 64 outputs for seeds
`{0, 1, 42, 2^63-1}` so any accidental change to the generator fails CI loudly.

**Streams.** One global stream means adding a single `rng.IntN` call anywhere shifts every
downstream draw, so an unrelated refactor invalidates the whole corpus. Instead: **derived,
named sub-streams.** `r.Derive("net.latency")`, `r.Derive("fault.crash")`,
`r.Derive("workload")`, `r.Derive("node.3.election")`, seeded by
`splitmix64(seed ^ fnv1a64(name))`. Changing how the workload consumes randomness then cannot
perturb the network schedule. Streams are created lazily but named canonically, so creation order
does not matter.

**Rejected:** (a) — slow, fat state, and we gain nothing over owning it. (b) — v2's convenience-method
stream stability is not promised strongly enough to hang the corpus on.

---

### D4 — Reproducibility unit: seed, or materialized plan?

**The problem nobody mentions until it bites:** a seed reproduces a bug *only at the commit that
produced it*. The moment you fix the bug — or add a log line that consumes a random number — the
seed replays a different history. CLAUDE.md already scopes this correctly ("replays at the commit
that contained it"), but we can do materially better, and the minimizer *requires* better.

**Candidates**

- **(a) Seed only.** Everything (topology, workload, fault schedule) is regenerated from the seed at
  run time.
- **(b) Plan only.** A JSON artifact fully describing the run; seeds are just a way to generate plans.
- **(c) Seed generates a plan; the plan is serializable and independently runnable.**

**Recommendation: (c).** `simctl run --seed N` materializes a **fault plan** — a declarative,
ordered list of `(at_nanos, action)` entries plus per-message dice parameters — *before* the run
starts, then executes it. `--plan p.json` skips generation and executes a given plan. On failure the
run writes a **bundle**: `plan.json`, `meta.json` (seed, git SHA, config, failing checker, step
index, virtual time), and optionally the compressed trace.

This buys three things a bare seed cannot:
- **Minimization is possible at all.** Delta-debugging needs a list of removable elements. Dice
  rolled inline are not removable; plan entries are.
- **Corpus entries survive code changes.** A minimized plan keeps failing after a refactor, which is
  exactly what a regression suite must do.
- **Human inspection.** "Partition {1} from {2,3} at 4.2s for 800ms; crash node 2 at 4.9s" is a
  readable bug report; "seed 8834127" is not.

`seeds/` therefore stores bundles, not integers, and the README claim becomes *stronger*: not just
"replays from a seed" but "replays from a seed at that commit, and from a minimized plan at any
commit."

**Reactive faults** (e.g. "partition the leader 200ms after it is elected") cannot be pre-materialized
by time. They are expressed as **rules** in the plan (`{on: leader_elected, after: 200ms,
action: partition_from_majority, times: 1}`) — still declarative, still removable by the minimizer,
still deterministic. The Jepsen-style nemesis is a rule generator, not an escape from the plan.

#### [Amended — Ansh, 2026-08-10] Plan completeness: a plan is a total repro, with no live RNG

**The gap in the proposal as written, stated plainly.** Fault *entries* were materialized up front,
but per-message dice (drop / delay / duplicate) were still drawn **live** from sequential sub-streams
during execution. So `--plan p.json` would not have reproduced a run on its own: it would have needed
the seed, and worse, a sequential stream desynchronizes the moment a code change alters how many
draws are taken. Minimization on plans would have been unsound. Ansh is right; this is fixed before
A0.1 lands.

**The fix: two phases, and sequential randomness exists only in the first.**

- **Phase 1 — generation (seed → plan).** Sequential sub-streams (D3) are used freely. Output is a
  plan file. This phase runs once and its every output is recorded.
- **Phase 2 — execution (plan → run).** **No sequential RNG exists.** Every random quantity is
  either (i) materialized verbatim in the plan, or (ii) a pure keyed function of a plan-embedded key
  and a canonical event identity:

  ```
  prf(key128, domain, a, b, c) -> uint64          // stateless, order-independent
  ```

  | quantity | identity tuple | key |
  |---|---|---|
  | drop / delay / duplicate | `(from, to, ordinal_on_that_directed_link)` | `keys.net` |
  | engine sync-completion latency | `(node, seq_num)` | `keys.engine_sync` |
  | clock jitter per tick | `(node, tick_ordinal)` | `keys.clock` |
  | randomized election timeout (A1+) | `(node, term, election_ordinal)` | `keys.raft` |

This is **stronger** than embedding sub-stream seeds, which was the minimum Ansh asked for. Embedded
stream seeds would reproduce a plan exactly *only until the code changes how many draws it takes*;
counter-based dice have no draw order to desynchronize. Adding a log line, or sending one extra
message on link A→B, cannot perturb link C→D at all, and cannot perturb A→B's *earlier* messages.
That is exactly the property the minimizer needs: deleting fault entry #7 leaves every other dice
outcome bit-identical, so ddmin is testing what it thinks it is testing.

**Plan schema v1** (`plan.json`), all of which is required for a total repro:

```jsonc
{
  "schema_version": 1,
  "provenance": { "seed": 8834127, "git_sha": "...", "generator_version": 3,
                  "config_digest": "..." },          // provenance ONLY — never read during execution
  "config":  { "nodes": 3, "workload": "bank", "duration_ns": 6e10,
               "max_offset_ns": 5e8, "wire": "codec" },
  "keys":    { "net": "0x…", "engine_sync": "0x…", "clock": "0x…", "raft": "0x…" },
  "network": { "links": [ { "from":1, "to":2, "drop_p":0.01, "lat_lo_ns":2e5,
                            "lat_hi_ns":3e6, "tail_p":0.001, "tail_shape":1.5,
                            "dup_p":0.002 } ] },
  "clock":   { "offsets": [ { "node":2, "schedule":[[0,0],[4e9,4.9e8],[9e9,4.9e8]] } ] },
  "faults":  { "entries": [ {"at_ns":4.2e9, "partition":{"a":[1],"b":[2,3]}, "for_ns":8e8} ],
               "rules":   [ {"on":"leader_elected","after_ns":2e8,
                             "action":"partition_from_majority","times":1} ] },
  "workload":{ "ops": [ {"at_ns":1e8,"client":1,"seq":1,"op":"put","k":"a","v":"1"} ] },
  "assert":  { "min_fires": { "partition":1, "crash":1, "sync_loss":1, "duplicate":1 } }
}
```

The workload is **fully materialized** rather than generated live, for the same reason: the minimizer
must be able to delete individual client operations without perturbing anything else.

**The tripwire.** Plan mode installs a *poisoned* `Rand` whose every method panics. If any code path
tries to take a sequential draw during execution, the run dies immediately and loudly rather than
silently producing an unreproducible schedule. This is on by default in plan mode and is the
mechanical guarantee behind the sentence "a plan is a complete repro" — not a promise, a panic.
`minimize` operates on plans only, and inherits the guarantee.

**Rejected:** (a) — makes minimization impossible and the corpus fragile. (b) — losing seed-based
generation loses cheap infinite search, which is where bugs actually come from. *Also rejected within
the amendment:* embedding sequential sub-stream seeds in the plan — it satisfies the letter of "plan
alone reproduces" but stays fragile to draw-count changes, and would give the minimizer a false
guarantee.

---

### D5 — The Raft interface (`Ready` struct)

This is the most consequential interface in the repo; A1–A10 all live behind it.

**Candidates**

- **(a) Classic etcd/raft.** `Ready()` → `{HardState, Entries, Snapshot, CommittedEntries, Messages}`;
  caller persists, sends, applies, then calls `Advance()`. No new `Ready` until `Advance`.
- **(b) `Ready` with disaggregated acknowledgements.** Same struct shape, but no global `Advance`.
  Caller calls `AckPersisted(mark)` and `AckApplied(index)` independently. Raft **withholds** any
  message whose correctness depends on an unacknowledged durability mark, and surfaces it in a later
  `Ready` once the ack arrives.
- **(c) Fully message-based async storage** (etcd v3.6 `AsyncStorageWrites`): storage work is emitted
  as self-addressed messages (`MsgStorageAppend`/`MsgStorageApply`) and completion is fed back via
  `Step`.

**Tradeoffs**

(a) is simplest and proven, but it serializes fsync against replication: with only one outstanding
`Ready`, a follower cannot be appending batch *n+1* while batch *n* is in flight to disk. That caps
throughput at `1/fsync_latency` proposals per node, which directly undercuts the A9/I2 latency and
throughput claims. It also puts the persist-before-reply invariant in the *driver's* hands: the
driver is told "persist these, send those" and must remember the ordering rule. Drivers forget.

(c) is the most flexible and is where etcd landed, but it turns storage into an in-band protocol
with its own message types, ordering rules, and self-addressed routing. It is a larger surface to
get right in A1, when Raft itself is the thing under test.

(b) is (c)'s guarantee with (a)'s ergonomics. The key property, and the reason I want it:
**the driver cannot violate "persist before reply", because raft never hands it a message it
would be unsafe to send.** The sharp-edges checklist item stops being a discipline and becomes a
type-level fact. Pipelining works because raft tracks `persistedIndex` separately from `lastIndex`
and only advances the leader's own match index to `persistedIndex`.

**Recommendation: (b).** Proposed shape:

```go
package raft

// Ready is a drain: calling it returns pending outputs and clears them.
// There is no Advance(). Progress is acknowledged per-resource.
type Ready struct {
    // --- must be made durable before AckPersisted(Mark) ---
    HardState *HardState // non-nil iff (term, vote, commit) changed
    Entries   []Entry    // append at Entries[0].Index, truncating any conflict
    Snapshot  *Snapshot  // install atomically; supersedes Entries
    Mark      PersistMark // opaque monotone token; zero if nothing to persist

    // --- safe to act on immediately ---
    Messages   []Message   // every precondition already satisfied
    Committed  []Entry     // apply in order, then AckApplied(lastIndex)
    ReadStates []ReadState // A7: read-index responses

    // --- observational, for checkers and logs; never load-bearing ---
    Soft SoftState // leader id, role, term (also available via Status())
}

type Node interface { // a struct, not a goroutine. No channels. No I/O. No clock.
    Tick()
    Step(m Message) error

    Propose(id ProposalID, data []byte) error
    ProposeConfChange(id ProposalID, cc ConfChangeV2) error
    ReadIndex(ctx []byte) error
    TransferLeadership(target NodeID)
    Campaign() error

    HasReady() bool
    Ready() Ready
    AckPersisted(m PersistMark)
    AckApplied(index uint64)

    Status() Status // read-only snapshot for checkers/debug
}
```

Reads of the durable log go through a narrow, etcd-style `raft.Storage` (`InitialState`, `Entries`,
`Term`, `FirstIndex`, `LastIndex`, `Snapshot`) implemented by `store/` over `engine/`. Raft owns only
the unstable tail. Randomized election timeouts draw from an injected `Rand` in `Config` — never a
package-level source.

#### [Amended — Ansh, 2026-08-10] What durability gating covers — normative

This is the interface's central safety claim, so it is stated normatively here and reproduced
verbatim as the doc comment on `Ready.Messages`. **The general rule, from which the cases follow:**

> An outbound message is released in `Ready.Messages` only after every element of persistent state
> that the message *attests to* is durable. If a `Step` mutates `HardState` or the log, no message
> generated by that `Step` whose meaning depends on the mutation is released until the corresponding
> `PersistMark` is acknowledged.

Enumerated, with the failure each gate prevents:

| message | gated on | what breaks without the gate |
|---|---|---|
| `MsgAppResp` (accept) | the appended entries **and** `HardState.Term` durable | Follower acks index *i*, leader counts it toward a quorum and commits; follower crashes, loses *i*, comes back and is elected with a shorter log ⇒ **committed entry lost**. Violates Leader Completeness and "committed is forever". |
| `MsgVoteResp` (**grant**) | `(Term, Vote)` durable | Node grants to A, crashes, forgets, restarts, grants to B in the same term ⇒ **two leaders in one term**. Violates Election Safety. This is the canonical case. |
| `MsgVoteResp` (**reject**) *and any response emitted after a term bump* | `HardState.Term` durable | A response reveals the responder's new term. Forgetting a term bump after advertising it lets the node re-participate in a term it has already acted in. Cheap to gate; gated. |
| `MsgAppResp` following `InstallSnapshot` | snapshot **durably installed**, including its config and applied index | Node acks the snapshot, crashes before it is durable, restarts with an empty/older log while the leader has already advanced `Next` past the snapshot index ⇒ silent hole in the log. |
| `MsgHeartbeatResp` | `HardState.Term` durable (only when the heartbeat bumped the term) | Same term-amnesia case as above. |
| `MsgReadIndexResp` (A7) | leadership-confirming quorum, *not* durability | Read index attests to a commit index, which is already durable by the time it is committed. Documented so nobody adds a spurious gate and pays latency for nothing. |

**`MsgPreVoteResp` is deliberately NOT gated, and that is a correctness argument, not an oversight.**
Pre-vote by construction mutates no persistent state: the responder does not advance its term and
does not record a vote. A pre-vote grant attests only to "my log is not more up to date than yours
and I have not heard from a leader recently" — both facts about volatile state that are permitted to
be forgotten across a crash. Gating it would cost an fsync on the hot path of every election attempt
and buy nothing. If a future change makes pre-vote touch `HardState`, this gate must be reinstated;
a test asserts pre-vote handling leaves `HardState` unchanged so that change cannot pass silently.

The consequence worth stating: **the driver has no ordering obligation at all.** It may send
`Ready.Messages` in any order, at any time, before or after it starts the persist, and safety holds.
The persist-before-reply sharp edge is discharged inside `raft/`, where it is unit-testable in
isolation, rather than distributed across every caller.

Migration note: (b) → (c) is mechanical if we ever need multiple concurrently-outstanding persist
marks with independent completion; (b) already supports multiple outstanding marks, so I do not
expect to need it.

**Rejected:** (a) — one outstanding `Ready` serializes fsync with replication and pushes the
persist-before-reply rule onto the driver. (c) — correct but a larger surface than A1 should carry;
(b) is a strict subset with the same safety property.

---

### D6 — Transport, and whether sim messages cross the wire codec

**Interface**

```go
type Envelope struct {
    From, To NodeID
    RangeID  RangeID     // multi-raft from day one; A1 uses RangeID(1)
    Msg      raft.Message // or a store-level union in A4+
}

// Send is best-effort, non-blocking, and never returns an error.
// Loss is a normal outcome; the protocol must tolerate it.
type Transport interface{ Send(Envelope) }
```

Fire-and-forget with no error return is deliberate: any error signal is a covert failure detector,
and covert failure detectors are how consensus implementations accidentally become unsafe. Real
mode gets the same semantics via a bounded per-peer queue that drops on overflow — mirroring the
sim's drop injector rather than blocking.

**The decision: does the sim serialize?** Nodes in sim share an address space. If node A sends a
message holding a slice it later mutates, node B observes the mutation — a bug class that cannot
exist in production and a determinism leak that can.

- **(a) Pass Go values directly.** Fastest; aliasing bugs are possible and would be *false*
  positives, wasting debugging time, or worse, mask real ones.
- **(b) Deep-copy at the boundary.** Removes aliasing. Codec untested by sim.
- **(c) Marshal to bytes and unmarshal on delivery — the real wire codec.**

**Recommendation: (c), always on.** It removes aliasing *and* runs the production codec through
every one of the millions of soak messages for free, so encoding bugs (truncation, missing fields
after a schema change, unbounded sizes) are caught by the corpus rather than by I2. It also makes
message *size* observable, which the delay model and the future bandwidth model want. If profiling
shows it dominates hunt throughput, add `--wire=copy` as an explicitly-labelled reduced-fidelity
mode for hunts only; CI soak stays on `--wire=codec`. I will measure this in A0 and report it.

**Rejected:** (a) — cross-node aliasing produces bugs that cannot happen in production and hides
ones that can.

---

### D7 — `Engine` interface and the fsync model

This interface freezes Track B's contract, so it needs the most care of anything in A0.

The hard part is `sync`. In sim, nothing may block. In the C++ engine, `fsync` blocks. In both, the
model must express: *writes become visible immediately; they become durable later; a crash reverts
to the last durable point.*

**Candidates**

- **(a) `Apply(batch, sync bool) error` blocks until durable when `sync` is true.** Matches
  LevelDB/Pebble/RocksDB. Cannot be called from a non-blocking event loop.
- **(b) `Apply` never blocks; durability is a monotone sequence number advanced by an
  out-of-band notification.**
- **(c) `Apply` never syncs; a separate blocking `Sync()` is called by a driver-owned syncer.**

**Recommendation: (b).**

```go
type SeqNum uint64

type Engine interface {
    // Apply makes b visible to subsequent reads immediately and returns the
    // sequence number at which it became visible. It never blocks on I/O.
    // sync=true additionally requests durability for everything <= the returned
    // SeqNum; sync=false leaves it buffered (and losable on crash).
    Apply(b *Batch, sync bool) (SeqNum, error)

    // DurableSeq is the highest SeqNum guaranteed to survive a crash.
    // Monotone non-decreasing. A crash reverts state to exactly DurableSeq.
    DurableSeq() SeqNum

    // OnDurable registers a callback invoked when DurableSeq advances.
    // Sim:  the simulator schedules a durability event on the node's loop.
    // Real: a per-engine poller goroutine owns the blocking Sync() and posts a
    //       DurabilityAdvanced event to the node's MAILBOX (D1). The callback
    //       therefore runs on the node loop in both modes and must never touch
    //       core state directly. No C-to-Go callbacks cross the cgo boundary.
    OnDurable(func(SeqNum))

    Get(key []byte) ([]byte, error) // ErrNotFound when absent
    NewIter(o IterOptions) Iterator // [Lower, Upper)
    NewSnapshot() Snapshot          // pins a consistent view; must be Closed
    ApproximateDiskBytes(start, end []byte) (uint64, error) // split decisions
    Close() error
}

// Ordered; last-write-wins within a batch. DeleteRange covers [start, end).
type Batch struct{ /* Set(k, v), Delete(k), DeleteRange(start, end) */ }

type Iterator interface {
    SeekGE(key []byte) bool
    SeekLT(key []byte) bool
    First() bool; Last() bool; Next() bool; Prev() bool
    Valid() bool
    Key() []byte   // valid until the next positioning call
    Value() []byte // valid until the next positioning call
    Error() error
    Close() error
}
```

This is exactly the WAL contract: buffered writes are readable and losable; `fsync` advances a
durability watermark; recovery restores the acknowledged-synced prefix and nothing more. The C++
engine keeps its natural blocking `Sync()`; the **Go wrapper** owns a small syncer pool that calls
it and fires `OnDurable`. No C→Go callbacks across cgo, no interface divergence between engines.

Two sub-decisions I want ruled on explicitly, because Track B builds to them:

- **D7.1 — `DeleteRange`. [OVERRULED — Ansh, 2026-08-10. Middle path adopted.]**
  My recommendation was to exclude it from the frozen interface and provide a Go-side
  iterate-and-point-delete helper. **Overruled.** The ruling, which is the better call:

  - `DeleteRange(start, end)` **stays in the frozen `Engine`/`Batch` interface.**
  - `engine/model` implements it **natively** from A0.5.
  - The C++ engine implements it **internally** as iterate-and-point-delete **through B2** — correct
    but slow — so B4's differential tests exercise the *semantics* early rather than discovering them
    at I1.
  - **Real range tombstones are a B3 deliverable**, landing before I2 benchmarks so no published
    number is taken against the slow path.

  Rationale for the record (Ansh's, and it defeats mine on all three counts): freezing the interface
  without it guarantees churn on *both* tracks later, and interface churn after B has built to it is
  the most expensive kind; snapshot application needs an atomic **clear-then-ingest**, which a
  best-effort Go helper cannot provide at the right isolation; and replica removal at scale cannot be
  expressed as one giant point-delete batch without unbounded batch sizes and write stalls. My
  proposal optimized a week of Track B schedule against a permanent interface defect — wrong trade.

  Consequence: `DESIGN-B1` must reflect this scope. Recorded in CLAUDE.md's Track B phase list so
  Session 2 sees it before writing B1.
- **D7.2 — Iterator granularity.** The Go-level `Iterator` is one-KV-at-a-time. The cgo binding
  prefetches N pairs per boundary crossing internally. CLAUDE.md's "iterators return blocks of N"
  is therefore a constraint on the **C API**, not on this Go interface, and BENCHMARKS.md measures
  the difference. Confirming this reading.

**Rejected:** (a) — incompatible with a non-blocking event loop; would force either a thread per
node in sim (kills determinism) or a second, divergent interface. (c) — cleaner in isolation, but
CLAUDE.md specifies a sync flag on the batch write, and (b) honors it while adding group-commit
freedom the C++ engine will want anyway.

---

### D8 — `engine/model`: the deterministic Go reference

Requirements: byte-identical iteration order to the C++ engine, O(1) snapshots, exact fsync-loss
modeling, and fast enough to not dominate sim time.

**Candidates for the data structure**

- **(a) Copy-on-write sorted slice.** O(n) per write, O(1) snapshot, trivially correct.
- **(b) Hand-rolled immutable treap keyed by `bytes.Compare`, priorities from
  `fnv1a64(key)`** (not from RNG — RNG-derived priorities would couple engine internals to the
  global stream). O(log n) write, O(1) snapshot, ~150 lines.
- **(c) `map[string][]byte` + sort on iteration.** O(1) write, O(n log n) iteration.

**Recommendation: start (a), upgrade to (b) behind a benchmark gate.** Sim workloads hold thousands
of keys, not millions; (a)'s O(n) memcpy is likely cheaper than (b)'s pointer chasing at that size,
and it is impossible to get wrong. A0 lands a benchmark; if `engine/model` exceeds ~10% of hunt CPU
time, swap in (b). (c) is rejected outright — it invites `range` over a map, which is the exact
determinism leak CLAUDE.md calls out, and it hides iteration cost inside every scan.

**`DeleteRange`** is implemented natively (a slice splice under (a), a split/join under (b)) per the
D7.1 ruling, so the model is the *semantic* reference the C++ engine's iterate-and-point-delete
implementation is differentially tested against from B4 onward.

**Crash semantics.** The model keeps `visible` (all applies) and `durable` (applies ≤ `DurableSeq`).
`Crash()` discards `visible` and reloads from `durable`. The simulator schedules durability
advancement as an event with configurable latency, so there is always a real window in which
acknowledged-but-unsynced data exists — which is where the "unsynced-write loss" injector lives.
Restart replays from `durable` only.

---

### D9 — Fault injector set and how faults are scheduled

Two mechanisms, deliberately separated:

**Per-message dice** — evaluated inside the sim transport, drawn from `Derive("net.<pair>")`:
- **drop** (p per link, asymmetric per direction)
- **delay** — latency sampled from a per-link distribution; base uniform `[lo, hi]` plus a
  heavy-tail component (`p_tail` chance of `lo + Pareto`) so tail-latency bugs appear
- **duplicate** (p; N copies, each independently delayed — a duplicate that arrives *first* is the
  interesting case and falls out naturally)
- **reorder** — an emergent property of independent per-message latency, not a separate knob. Stated
  explicitly so nobody looks for a `reorder=true` flag: with independent delays, reordering happens
  at a rate we can *assert* on rather than dial.

**Plan entries** — timed or reactive, materialized from `Derive("fault.*")`:
- `partition {A} | {B}` — symmetric, with `until` or `for`
- `partition_asym from={A} to={B}` — one-way link failure; the classic source of "leader that can
  send but not receive" bugs, which symmetric partitions never produce
- `crash node=n` — process death: in-memory state gone, unsynced engine writes gone, in-flight
  messages to and from it dropped
- `restart node=n at=t` — recovery from `durable` only; a fresh `Handle` with no memory
- `pause node=n for=d` — a stopped-the-world node that resumes with stale state and a live lease
  (models a GC pause; distinct from crash because nothing is lost)
- `sync_loss node=n window=[t0,t1]` — during the window, `OnDurable` never fires; a crash inside it
  loses everything written in it. This is the injector that finds ack-before-durable bugs
- `io_error node=n op=<apply|read> count=k` — engine returns an error; forward-declared for B4/I1
- `clock_drift node=n rate=ppm` — applies to both `PhysicalNow()` *and the node's tick rate*, so a
  slow-clocked node also elects slowly. Drift that does not affect ticks is cosmetic
- `clock_jump node=n delta=±d at=t` — including backward jumps
- `slow_node node=n handler_delay=d for=w` — models CPU starvation (see D2's idealization note)

**Skew discipline.** In safety runs, the plan generator constrains
`max_i,j |PhysicalNow_i(t) − PhysicalNow_j(t)| ≤ maxOffset` for all `t`, and a checker asserts it
continuously — so a "we exceeded our own assumption" harness bug can never be mistaken for a
protocol violation. `--skew-envelope` deliberately violates it for the A8 experiment, and in that
mode the checker inverts: it *records* the violation instead of failing, and the safety checkers
are expected to fire.

**Fire-count assertions** (an explicit A0 exit criterion). Every injector increments a counter. At
end of run, for each *enabled* injector with a configured `min_fires` (default 1), zero fires is a
**run failure**, not a pass. This is the difference between a chaos suite and a chaos-shaped
decoration. Counts go in `meta.json` and in the hunt summary, so we can say "10k seeds, of which
partitions fired 41,207 times and sync-loss windows swallowed 3,918 acknowledged writes" — numbers
that make the safety claim mean something.

#### [Amended — Ansh, 2026-08-10] Two capabilities confirmed explicitly

**(1) Crashes land inside the window between `Apply` returning and durability.** Confirmed, and it
falls out of the model rather than being bolted on. `Apply(b, sync=true)` returns visible at virtual
time *t*; the simulator schedules `DurabilityAdvanced(seq)` at *t + λ*, where
`λ = prf(keys.engine_sync, node, seq)` mapped into the configured latency range. The interval
`[t, t+λ)` is therefore a real, non-empty stretch of virtual time in which **acknowledged, readable,
non-durable** data exists, and any `crash` event scheduled inside it discards exactly that data.

Leaving that to coincidence would be weak, so it is targetable three ways:
- **Reactive:** `{on: unsynced_window_open, node: n, after_ns: δ, action: crash}` fires while
  `visible > durable`, so the crash is *guaranteed* to land in a window rather than probably.
- **Widened:** `sync_loss node=n window=[t0,t1]` freezes `DurableSeq` for the window, letting
  arbitrarily many acknowledged writes pile up unsynced before the crash takes all of them at once.
- **Swept:** a generator mode enumerates crash points across every open window in a short run — the
  Go-side analogue of Track B's kill-point sweep.

Measurement, not vibes: the harness counts `unsynced_ops_lost` / `unsynced_bytes_lost` per run and
aggregates them per hunt. `sync_loss` carries a `min_fires`, and a soak whose aggregate lost-write
count is zero is reported as **not having tested durability**, regardless of how green it is.
The engine-durability oracle recomputes expected post-crash state from the *harness's own* operation
log at `DurableSeq` — never by asking the engine what it thinks it had — so an engine that lies about
durability is caught rather than believed.

**(2) Drift schedules can hold nodes near `maxOffset`, sustained.** Confirmed, and the model is
deliberately more expressive than a drift *rate*. Each node's clock offset is a **piecewise-linear
schedule** in the plan: `clock.offsets[].schedule = [[t₀, off₀], [t₁, off₁], …]`, linearly
interpolated. A drift rate is the derivative of a sloped segment; a **sustained hold at a chosen skew
is a flat segment**. So "hold node 2 at `maxOffset − 1ms` relative to node 1 for 10 s" is expressible
exactly.

This matters specifically for A8: lease disjointness is not stressed by a clock that sweeps *through*
near-maximal skew in a millisecond, it is stressed by a cluster that *sits* just short of the cliff
across many lease acquisitions, transfers, and expirations. A rate-only model can only produce the
former. Generator mode `--skew=near-max` biases plans toward sustained pairwise skew in
`[0.9, 1.0) × maxOffset`.

Belt and braces on the bound: the generator constrains pairwise skew to `≤ maxOffset` *by
construction*, **and** a checker asserts it at every step — because a generator bug that quietly
exceeded our own assumption would present as a protocol violation and cost days.
`--skew-envelope` (A8) deliberately crosses the bound; there the checker inverts to *record* rather
than fail, and the safety checkers are *expected* to fire. Characterizing what breaks, and how we
detect it, is the entire experiment.

Node ticks are driven by the node-local clock, so a drifted node also campaigns and heartbeats on a
drifted schedule. Drift that moved only `PhysicalNow()` would be cosmetic.

---

### D10 — Determinism enforcement: a custom vet analyzer, not a code review rule

CLAUDE.md lists determinism rules as things we must not do. Rules that are not mechanically enforced
are violated, on average, once per thousand lines. Proposal: `tools/determinismcheck`, a
`golang.org/x/tools/go/analysis` pass (stdlib-adjacent; flagging as a dependency ask, see Q4) run in
CI over `raft/`, `store/`, `kv/`, `router/`, `balancer/`, `engine/model`, that rejects:

- `time.Now`, `time.Since`, `time.Sleep`, `time.After`, `time.Tick`, `time.NewTimer`, `time.NewTicker`
- any reference to package-level `math/rand` / `math/rand/v2` functions
- imports of `net`, `net/http`, `os` (except `os.Getenv` in `main`), `path/filepath`, `io/ioutil`
- `go` statements, `select` statements, `chan` types, `sync.Mutex`/`RWMutex`/`WaitGroup`
- **`range` over a map type** — outright banned in these packages; use `sortedKeys(m)`. This is the
  classic Go determinism leak and the only one that is both silent and common
- `fmt.Sprintf("%p")`, `unsafe`, and pointer-keyed maps

A per-file `//rift:allow-nondeterminism <reason>` escape hatch exists, must carry a reason, and CI
prints every use in the build log so exceptions stay visible rather than accumulating.

Cost: about a day. Value: this is the mechanism that makes the *whole* verification claim credible
under cross-examination, because the answer to "how do you know a `time.Now()` didn't sneak in?" is
a build failure, not a promise.

#### [Amended — Ansh, 2026-08-11] Scope is a question about *when* code runs; the hatch is a ledger

Ruling on A0.3 as landed. Six parts, of which the first is the one to remember.

**1. The governing principle.**

> **Any code that executes during a simulated run is in scope, no exceptions. Orchestration *around*
> runs — parallel hunting, real-mode drivers, `cmd/` — is not.**

Scope is therefore not a judgement about how dangerous a package looks. It is a question with a
factual answer: does this code run inside a run whose trace must be reproducible? A0.3's original
table was assembled package by package from D10's list and got three of them wrong, all in the same
direction — leaving out code that runs in-band because it did not *look* like node logic.

| package | scope | why |
|---|---|---|
| `raft`, `store`, `kv`, `router`, `balancer` | core | the state machines |
| `engine`, `engine/model` | core | executes in every sim run, and replay identity is *defined* on the model — precisely what the pass exists to protect |
| `engine/real`, `engine/pump` | excluded | DR-11's poller: needs goroutines, runs only in real mode |
| `clock` | core | the sim clock runs in-band; the real implementation takes a per-line hatch on its single `time.Now` |
| `sim`, `sim/toy` | core | seeded, so it needs nothing the rules ban. Plan and bundle file I/O and the parallel hunter must land in their own subpackages and join the exclusion list as they do; being in scope from today is the forcing function for that split, while the moves are cheap |
| `internal/rng` | core | needs nothing banned, and the pass is what stops someone importing `math/rand` inside it later — the exact bug D3 exists to kill |
| `internal/sorted` | core | the only map iteration in the repo, under the only hatch |
| `node` | mailbox | provisional; see the D1 amendment |
| `cmd`, `bench`, `chaos`, `soak`, `tools` | off | orchestration |

A subpackage nobody has classified is **in** scope, not out of it. A new package under `engine/`
arrives checked and is excluded only by someone writing it down, which is the direction the default
has to fall.

**2. Exclusion versus hatch.** Both exist and they are not interchangeable. A hatch excuses a
*symbol* on a *line* and is recorded; an exclusion removes a *package* from the rules entirely. The
test that decides: concurrency is never hatchable (part 4), so a package that needs a goroutine must
be an exclusion, while a package that needs one `time.Now` takes a hatch. Prefer the hatch. An
excluded package hides its exceptions; a hatched line lists them.

**3. The real-time surface is an allowlist, and the iterator hole is closed.**

`time` is checked by allowlist rather than blocklist, matching the scope polarity: value types,
constants, their methods, and the deterministic constructors (`Date`, `Parse`, `Unix`, `FixedZone`,
`UTC`) are legal; **everything else in the package is banned, including whatever Go adds next**. A
blocklist has to be extended each time the package grows a new way to read a clock, and nobody
notices that it needs extending. `Timer` and `Ticker` are banned as *type* references despite the
general rule that types are legal — a `*time.Timer` in core state is real-time machinery, not a
value. `Local` and `LoadLocation` are banned alongside the clock readers: they depend on the host's
TZ, which is not a property of the run. `sync/atomic` sits with `sync` on the unhatchable list; an
atomic in a single-threaded core means someone believes two things are running at once, and if they
are right the event loop has been bypassed.

The go1.23 iterator hole, closed: `slices.Sorted(maps.Keys(m))`, `slices.Collect(maps.Keys(m))`
and `for k := range maps.All(m)` contain no map-range syntax and are exactly the same
nondeterminism, so the rule that catches `for k := range m` sees none of them. Two additions:
importing `maps` is banned in core scope, and `reflect`'s `MapRange`/`MapKeys`/`MapIter`/`Seq`/`Seq2` are flagged,
since reflection reaches map iteration through method calls where neither syntax nor an import ban
can see it. **Sorted iteration lives in `internal/sorted` and nowhere else** — one generic helper,
one hatch, one range statement in the entire repository.

**4. The hatch is a ledger, and one rule has no hatch at all.** Three changes:

- **Unused hatches fail.** Not a warning: warnings rot and nobody reads CI warnings. A hatch that
  excused nothing is either a rule that has since been fixed, or a hatch that has drifted off the
  line it was written for — and the second case means something is now unguarded while its author
  believes it is not.
- **`HATCHES.txt` is a checked-in golden registry** of every hatch in the repo (`file:line` plus
  reason), diffed against the tree by `TestHatchRegistry`. Adding a hatch is a conscious edit to a
  reviewed list, not a comment somebody slips into a diff. `-update-hatches` is **local-only** and
  refuses to run with `CI` set; the lane asserts diff-clean, because a check that can rewrite the
  list it checks against is not a check.
- **No hatch sanctions `go`, `select`, `chan` or `sync` in core scope.** These are refused outright,
  with the covering hatch consumed so the author gets the diagnostic that matters rather than that
  plus a complaint about their annotation. Either the concurrency moves out of core or the design is
  wrong, and neither is something a comment can fix.

**5. The pass is mutation-tested, permanently.** `tools/determinismcheck/blind/*.patch` each blind
one rule; `make blind` applies each to a scratch copy and requires the test named in its header to
fail. A pass that has quietly stopped checking something looks exactly like a pass with nothing to
report, and this is the difference. It runs on every push, alongside the mutant suite: the two halves
of Amendment A2, one covering the protocol and one covering the instrument.

**A kill counts only against the declared test.** Attribution is the whole value: "some test failed"
says nothing about which rule is under test, and a lane that accepts any failure will happily accept
a failure that has nothing to do with the mutation. Two gates enforce that, both added after a lane
reported seven kills while one of the tests doing the killing was failing for its own reasons:

- **Baseline gate.** The unpatched tree must pass its *whole* suite before any patch is applied. A
  red baseline makes every later failure ambiguous, so the lane reports **INVALID** and refuses to
  report kills at all — the failure mode that occurred, encoded so it cannot recur silently.
- **ALIVE canary.** One patch blinds a real rule but is declared against a test that does not cover
  it, and **must survive**. If the canary dies, the lane cannot distinguish "this rule is checked"
  from "this test fails regardless", and every kill it reports is worthless. Every run therefore
  proves both directions: real blinds die, the canary lives.

The same structure applies to `sim/mutants/*.patch` when A0.12 lands it.

#### [Amended — Ansh, 2026-08-11] Two standing policies, both about how a rule earns trust

**A gate is not landed until its failure has been induced and observed.** A gate whose failure has
never fired is a decoration: it has demonstrated that it does not fire when nothing is wrong, which
is the cheap half. Landing one means writing down what was broken to make it fire, and what it
printed. The baseline gate and the ALIVE canary above were both landed this way — the first by
leaving a registry mismatch in the tree and watching the lane report INVALID with zero kills, the
second by repointing a copy of the canary at a test that does cover its rule and watching it report
DIED. This applies to every checker, oracle and lane from here on, including the invariants of A1
through A10.

**Enforcement surfaces are default-deny.** A blocklist requires a human to *notice* that it needs
extending; an allowlist requires a human to *approve* an addition. The first is a hope about
attention, the second is a review. Where an enforcement surface can be expressed either way, it is
expressed as an allowlist: the `time` package (value types, constants, methods and deterministic
constructors are listed; everything else, including whatever Go adds next, is banned), and the scope
table, where an unclassified package defaults **in**. Where a blocklist is unavoidable — banned
imports, whose universe is unbounded — the blind lane carries a patch per entry so that deleting one
is a test failure rather than a quiet loosening.

**6. Test files are checked under the same rules as their package.** A determinism leak in a test
helper is still a leak; it just costs a flaky test instead of a flaky database. This is what forced
`internal/sorted` into existence rather than a private helper per package, and A0.3 landing found
one such leak already in `internal/rng`'s tests.

---

### D11 — Tracing, determinism checking, and structured logs

Storing full traces for 10k seeds is infeasible; comparing them pairwise is the whole point. Resolve
with a **rolling trace hash**.

Every simulator step feeds a canonical, fixed-width encoding of the step into an incremental
SHA-256: `(step, at_nanos, kind, node, range, term, msg_type, msg_index, payload_len, engine_op_digest)`.
Explicitly excluded: pointers, real wall-clock, goroutine ids, map orderings, error *strings*
(error *codes* are included).

- `simctl run` prints `trace=<hex16>` in its summary and in `meta.json`.
- Determinism gate: run each seed twice in-process and once in a fresh process; all three hashes must
  match. (Fresh process catches ASLR/env-dependent behavior that in-process reruns miss.)
- On mismatch, `simctl replay --dump-trace` writes both full traces and reports the **first divergent
  step**, which is nearly always immediately diagnostic.
- Under `--paranoid`, additionally hash a full digest of every node's state after every step. ~10×
  slower; used in the 500-seed smoke lane, not in hunts.

**Structured logs** are separate from the trace hash: a `Logger` with fields
`{seed, step, vt, node, term, range, group}`, sinking to a ring buffer during hunts (only the last N
KB is dumped on failure — cheap) and to `jsonl` during replay. Log statements never affect control
flow and never consume RNG.

---

### D12 — Checkers (`Oracle`) and how they observe without breaking purity

```go
type Oracle interface {
    Name() string
    OnStep(s *SimView, ev Event) error // non-nil == invariant violated; run halts
}
```

`SimView` is a read-only façade: `Nodes()`, `Status(n)` (raft's read-only `Status`), `Engine(n)`
(read-only), `VirtualTime()`. Checkers never mutate and never consume RNG — enforced by the same
analyzer.

A0 ships the harness plus the checkers A0 can already justify:
- **injector fire counts** (D9)
- **clock-skew bound** (D9)
- **event-loop hygiene** — no event scheduled into the past; no `Handle` reentrancy
- **engine durability** — after a crash, recovered state equals the state at `DurableSeq`, computed
  independently by the harness from the operation log rather than by asking the engine
- **linearizability** via porcupine over the client history, per key

Election safety, log matching, etc. arrive with A1; the interface is fixed now so they slot in.

Cost note: checkers running on every step are the dominant sim cost in most FDS-style harnesses.
Mitigation: expensive checkers declare which event kinds they care about and are only invoked for
those; porcupine runs once at end of run, not incrementally (with a cap on history length and an
explicit timeout, since linearizability checking is NP-hard in general and a timeout must be reported
as *inconclusive*, never as *pass*).

#### [Amended — Ansh, 2026-08-10] Inconclusive is a first-class outcome, tracked in SOAK.md

Every run reports one of three outcomes per checker — **pass / violation / inconclusive** — and
`SOAK.md` carries an explicit **inconclusive column** alongside seeds, operations, CPU-hours, and
violations. Hiding timeouts inside "pass" would make the headline claim quietly false: "zero
violations across N seeds" means nothing if 12% of those seeds never finished checking.

Policy, in priority order, when the inconclusive rate grows:

1. **Shrink the per-run history window** — check linearizability over bounded segments rather than
   the whole run.
2. **Partition harder per key** — per-key histories are independent, so a wide key space is many
   small problems instead of one large one; where the workload allows, narrow the concurrency per
   key rather than the number of keys.
3. **Never loosen the checker.** Not the timeout, not the model, not the operation set. An
   inconclusive result is a statement about our budget, and the fix is always to make the *problem*
   smaller, never the *oracle* weaker.

A rising inconclusive rate is treated as a harness regression and investigated like a bug, since the
usual cause is a workload that has drifted toward pathological concurrency — which is also exactly
when we most need the checker to work.

---

## 3. `simctl` — the seed CLI

```
simctl run       --seed N | --plan p.json  [--workload W] [--nodes 3] [--duration 60s]
                 [--checkers all] [--paranoid] [--wire codec|copy] [--trace-out f]
                 [--require-trace-identity]        # default on; off for I1 verification mode
                 → exit 0 pass; 1 violation (writes bundle); 2 harness error

simctl replay    <bundle|seed> [--dump-trace] [--log-level debug] [--stop-at-step N]
                 → re-executes, asserts trace hash matches meta.json, dumps full trace + logs

simctl hunt      [--workers 10] [--from 1] [--time 30m | --seeds 10000]
                 [--keep-going] [--out seeds/]
                 → parallel search; prints seeds/s, injector fire histogram, first failure

simctl minimize  <bundle> [--budget 10m]
                 → ddmin over plan entries + duration + node count + workload ops;
                   re-verifies the *same checker* fails (not merely "some checker"),
                   emits a minimized bundle
```

`hunt` uses `min(cores-1, workers)` processes (11 cores here ⇒ 10), each running disjoint seed
ranges, so it composes directly with the soak farm in `soak/`. Failures are written atomically into
`seeds/` and the process keeps going under `--keep-going`.

`minimize` runs ddmin over the plan's entry list first (biggest wins), then shrinks scalar
parameters (duration, node count, key space, op count) by binary search. The re-verification must
match the **original failing checker and, where available, the original first-violating-step
signature** — otherwise minimization happily "reduces" one bug into a different one, which is a
classic and embarrassing failure mode.

---

## 4. Repo skeleton (A0 lands the tree; most dirs are stubs + doc.go)

```
go.mod                    (module path: see Q1)
Makefile                  test, race, smoke, soak, lint, determinism, bench
CLAUDE.md  README.md  BUGS.md  SOAK.md  BENCHMARKS.md
docs/DESIGN-*.md
internal/rng/             PCG64 + KAT vectors + Derive
clock/                    Clock iface; sim + real impls
engine/                   Engine, Batch, Iterator, Snapshot ifaces; DeleteRange helper
engine/model/             deterministic Go reference engine (D8)
raft/                     A1+ (A0: interfaces + doc.go only)
store/ kv/ router/ balancer/   A2+ stubs
sim/                      event loop, transport, injectors, plan, trace, oracle
sim/toy/                  A0 acceptance protocol (§5) + its mutants
cmd/simctl/               run | replay | hunt | minimize
tools/determinismcheck/   custom analysis pass (D10)
bench/ chaos/ soak/ seeds/
.github/workflows/        unit+race, 500-seed smoke, nightly 10k soak, determinism lint
```

Single Go module. `engine-cpp/` is Track B's worktree and is not built by Track A's lanes.

---

## 5. How A0 proves *itself* (acceptance plan)

"A toy state machine survives 1k seeds" proves the harness runs. It does not prove the harness
*catches* anything, and an uncalibrated harness is worse than none — it manufactures false
confidence. Two-part acceptance:

**Part 1 — the toy must survive.** `sim/toy`: a fixed-primary replicated register (no elections, no
Raft). Primary accepts `put(k,v)`/`get(k)` from clients, replicates synchronously to all backups,
waits for durability (`OnDurable`) **before** acknowledging, dedupes client requests by
`(client_id, seq)`. Under partitions it becomes unavailable — that is correct behavior, and the
checkers must not confuse unavailability with a violation. It exercises transport, all injectors,
engine durability, crash/restart, clock, client dedupe, and porcupine. Gate: **1k seeds, zero
violations, all injectors above their `min_fires`.**

**Part 2 — the harness must catch known bugs.** `sim/toy/mutants`: deliberately broken variants,
each a small patch, each with a documented "which checker should catch this, and within how many
seeds":

| mutant | injected bug | must be caught by | budget |
|---|---|---|---|
| `M1-ack-before-sync` | ack the client before `OnDurable` | engine-durability + porcupine | ≤ 50 seeds |
| `M2-ack-before-replicate` | ack before backups accept | porcupine | ≤ 50 seeds |
| `M3-dup-apply` | apply a retried request twice (counter workload) | porcupine | ≤ 100 seeds |
| `M4-map-range` | pick replication order by `range` over a map | trace-identity gate | ≤ 5 seeds |
| `M5-wall-clock` | use `time.Now()` for a timeout | determinismcheck (compile) + trace gate | immediate |
| `M6-stale-read` | serve reads from a partitioned backup | porcupine | ≤ 100 seeds |
| `M7-lost-restart` | restart from `visible` instead of `durable` | engine-durability | ≤ 20 seeds |

Any mutant that survives its budget means the harness is too weak, and A0 is not done — regardless
of what the 1k clean seeds say. This is the operational form of CLAUDE.md's *"if the harness finds
zero bugs, the harness is too weak."* The mutant suite then runs in CI forever as a **harness
regression test**, so we find out immediately if a future change blinds a checker.

#### [Amended — Ansh, 2026-08-10] Promoted to permanent project policy

The mutant suite is no longer an A0 acceptance device; it is a standing obligation for every phase,
recorded as **Amendment 2 in CLAUDE.md**:

1. **Every BUGS.md root cause must answer: "which mutant class would have caught this?"** A required
   field in the postmortem template, sitting next to the invariant that caught it.
2. **If no such mutant class exists, a new mutant is added in the same PR as the fix.** Not a
   follow-up issue, not a TODO — the same diff. A bug that escaped the harness is evidence of a
   specific blind spot, and the moment the fix lands is the only moment we will ever have a precise
   description of that blind spot.
3. **CI tracks kill-time per mutant** — seeds-to-detection and wall-time-to-detection, recorded every
   run. Kill-time is the harness's sensitivity expressed as a number instead of a belief. A mutant
   whose kill-time *regresses* is a harness regression even while every mutant is still being killed:
   it means we are drifting toward the budget cliff, and we learn that before we go over it.

The second-order effect is the valuable one. The mutant table becomes a catalogue of every way this
system is known to be able to break, each with a proof that we detect it and a measurement of how
fast. That is a materially stronger artifact than a green test suite, and it is what makes "zero
safety violations" mean *"we looked, with instruments of known and monitored sensitivity"* rather
than *"nothing happened to fall on us."*

#### [Amended — Ansh, 2026-08-11] Mutants are patches, not committed source

Discovered while landing A0.3: `sim/toy` is in the determinism pass's core scope, so M4 (`range` over
a map) and M5 (wall clock) **cannot exist as committed Go files** — the repo would not build, which
is the point of them. The mutant suite therefore stores `sim/mutants/*.patch`, applied by the runner
to a scratch worktree. Each patch header names the mutant ID and the failure class it validates. A
patch that no longer applies fails the lane, so the nightly mutant run doubles as patch-rot
detection, and rot is detected on the schedule that matters rather than the next time somebody looks.

The determinism pass gets the same treatment one level down, in
`tools/determinismcheck/blind/*.patch`: each blinds a single rule, and the named test must fail.
Together they are the two halves of Amendment A2 — one instrument checking the protocol, one checking
the instrument.

**Part 3 — determinism gate.** For 200 sampled seeds: in-process rerun, fresh-process rerun, and a
rerun with `GOGC=1` + `GOMAXPROCS=1` and again with `GOMAXPROCS=8` all produce identical trace
hashes.

---

## 6. Landing plan (small diffs, each with its own test lane)

| PR | Contents | Gate |
|---|---|---|
| A0.1 | skeleton, go.mod, Makefile, CI lanes, `doc.go` stubs | builds; lanes run |
| A0.2 | `internal/rng` (PCG64, Derive) + KAT vectors | KAT test |
| A0.3 | `tools/determinismcheck` + wiring | catches M5 at compile time |
| A0.4 | `Clock` ifaces, sim clock with drift/jump, skew checker | skew property tests |
| A0.5 | `engine/` ifaces, `engine/model`, durability model | durability property tests |
| A0.6 | event loop, `Event`, trace hash, structured logging | determinism gate on a no-op workload |
| A0.7 | `Transport`, wire codec, per-message dice | fire counts; codec round-trip fuzz |
| A0.8 | fault plan schema, materialization, plan entries + reactive rules | plan round-trip; replay-from-plan |
| A0.9 | `Oracle`, porcupine wiring, `sim/toy` | 1k seeds green |
| A0.10 | `simctl run \| replay` | replay reproduces bundles |
| A0.11 | `simctl hunt \| minimize` | minimizes a planted mutant bug |
| A0.12 | mutant suite + budgets in CI | all 7 mutants caught in budget |

Estimate: A0.1–A0.6 ≈ 2 days, A0.7–A0.9 ≈ 2 days, A0.10–A0.12 ≈ 2 days, assuming decisions land now.

---

## 7. Known idealizations (these go in README's verification-scope section)

Stated up front so no claim is ever broader than the evidence:

1. **Computation is instantaneous** unless a `slow_node` entry says otherwise (D2). Bugs requiring a
   node to be merely slow are only found where we explicitly inject slowness.
2. **The network model is per-link i.i.d. latency**, not a topology with shared bottlenecks,
   congestion, or bandwidth limits. Correlated network failure beyond partitions is not modeled.
3. **Deterministic replay is scoped to sim runs on `engine/model`** (CLAUDE.md already says this).
   The C++ engine's correctness comes from the Env rig, differential tests, corpus reruns in
   verification mode, and real chaos.
4. **Byzantine faults, disk bit-rot outside injected torn writes, and malicious clients are out of
   scope.** Crash-stop plus omission plus timing only.
5. **Linearizability checking is bounded**: porcupine runs with a history cap and a timeout, and a
   timeout is reported as *inconclusive*, never as *pass*.

---

## 8. Questions — all resolved (Ansh, 2026-08-10)

| # | Question | Ruling |
|---|---|---|
| 1 | Module path | **`github.com/anshkanyadi/rift`** — approved as proposed. |
| 2 | `DeleteRange` in the frozen interface | **Overruled; middle path.** Stays in the interface. See D7.1 and DR-13. |
| 3 | Custom PCG in `internal/rng` | **Approved.** Recorded as Amendment 1 to CLAUDE.md's dependency wording. |
| 4 | `golang.org/x/tools` dependency | **Approved — tooling only, never linked into a shipping binary.** Enforced by a CI check that the dependency does not appear in any `cmd/` binary's build graph. |
| 5 | Go version | **Pin current stable with a `toolchain` directive; CI runs exactly that version.** ~~1.22.5 is what is installed locally.~~ **[Overruled — Ansh, 2026-08-11]** 1.22 was never the current stable this ruling named, and it is now out of support. Pinned to the 1.26 line (`go 1.26.0`, `toolchain go1.26.5`), `golang.org/x/tools` unpinned to latest. See DR-26. |
| 6 | `git init` / worktrees | **Yes.** First commit is CLAUDE.md + DESIGN-A0. `main` plus a `track-b` worktree, `.gitignore`, MIT license, CI lane skeleton with lanes stubbed. |
| 7 | Wire codec fidelity | **Confirmed: fidelity is the default.** Codec stays on in sim. Any future fast path must still deep-copy — nothing ever shares message memory across nodes. Revisit only with measured hunt-throughput data. |

---

## 9. Decision record

**All decisions below: decided by Ansh, 2026-08-10, on this document as proposed by Claude.**
This section is the cross-examination surface. Each entry states what was decided, why, and what was
rejected and why — including the two places where my recommendation was overruled.

| # | Decision | Ruling & rationale | Rejected, and why |
|---|---|---|---|
| **DR-1** | **Concurrency: single-threaded event loop.** Node logic is a synchronous non-blocking state machine; `Handle(Event)` is the sole entry point in both sim and real mode. | Approved as recommended. Determinism becomes structural rather than disciplinary; a data race in core logic is unrepresentable, not merely unlikely. | Goroutines + cooperative scheduler: Go exposes no scheduler hook, `select` over ready cases is runtime-randomized and unoverridable, and one missed blocking primitive is a silent determinism leak surfacing as a flaky soak weeks later. Quiescence detection: cannot distinguish "blocked" from "about to run"; determinism degrades to statistical. |
| **DR-2** | **Mailbox rule (amendment).** In real mode every cross-goroutine interaction — transport receive, durability completion, timer, client request — enters through the node mailbox. Core state touched off-loop is a bug. | Ansh's amendment. Enforced in three layers: unexported core state in a separate package from the driver (compiler-enforced), a `mailbox` rule in `determinismcheck` (vet-enforced), and a `-race` lane on `node/` (backstop). | Review-checklist-only enforcement: rejected as the *primary* mechanism. A rule that depends on a human noticing does not survive 30k lines. It remains as the third line of defense. |
| **DR-3** | **Time: discrete-event, virtual nanoseconds,** total order `(at_nanos, insertion_seq)`. | Approved. Standard, fast, and the only model that can express leases, uncertainty intervals, and skew. | Fixed-tick lockstep: quantum alignment systematically hides sub-tick races and wastes steps. Untimed logical ordering: cannot express clocks at all, making A5 and A8 unverifiable — fatal. |
| **DR-4** | **RNG: own a PCG64 in `internal/rng`** with pinned known-answer vectors and named derived sub-streams. | Approved, and recorded as **Amendment 1** to CLAUDE.md's dependency wording. We own the stream, so `seeds/` survives Go upgrades — which is the entire premise of a seed corpus. | `math/rand` v1: stable but slow with fat per-stream state, and we gain nothing over owning it. `math/rand/v2`: sources are specified but the convenience-method mappings (`IntN`, `Shuffle`, `Perm`) carry a weaker stability promise than v1's frozen stream; a silent change would leave every corpus entry self-consistent but *different*, which is the worst possible failure mode. |
| **DR-5** | **Repro unit: seed materializes a plan;** the plan is serializable and independently runnable. Bundles (`plan.json` + `meta.json` [+ trace]) are what `seeds/` stores. | Approved. Makes minimization possible at all, lets corpus entries survive refactors, and turns a bug report from "seed 8834127" into something a human can read. | Seed-only: makes ddmin impossible (dice rolled inline are not removable) and the corpus fragile. Plan-only: loses cheap infinite seed search, which is where bugs actually come from. |
| **DR-6** | **Plan completeness (amendment).** A plan is a total repro with **no live RNG**. Generation (seed → plan) may use sequential streams; execution (plan → run) uses only plan-materialized values and a stateless keyed PRF over canonical event identities. A poisoned `Rand` panics on any sequential draw during execution. `minimize` operates on plans only. | Ansh's amendment, and it caught a real hole: as originally proposed, per-message dice were still drawn live, so `--plan` would not have reproduced on its own and ddmin would have been unsound. Fixed before A0.1. | Embedding sequential sub-stream seeds in the plan — the minimum that satisfies the letter of the requirement. Rejected in favor of counter-based dice because stream state still desynchronizes the moment a code change alters draw counts, which would hand the minimizer a *false* guarantee. Counter-based dice have no draw order to desynchronize. |
| **DR-7** | **Raft interface: `Ready` + disaggregated acks** (`AckPersisted`/`AckApplied`), no global `Advance`. Raft withholds messages that depend on unacknowledged durability marks. | Approved. Gets pipelining (a single outstanding `Ready` caps throughput at `1/fsync_latency`, undercutting A9/I2) *and* makes persist-before-reply a structural property of the interface rather than a rule every driver must remember. | Classic etcd `Ready`+`Advance`: serializes fsync against replication and delegates the safety-critical ordering rule to the caller. Fully message-based async storage (etcd v3.6 style): correct, but a larger in-band protocol surface than A1 should carry while Raft itself is under test; DR-7 is a strict subset with the same safety property, and the migration is mechanical if ever needed. |
| **DR-8** | **Durability gating is normative and enumerated (amendment):** append responses, vote **grants** (`(Term, Vote)` durable before release), any response following a term bump, snapshot acks, and heartbeat responses that bumped the term. Reproduced verbatim as the doc comment on `Ready.Messages`. | Ansh's amendment. Each gate is documented with the specific safety violation it prevents — lost committed entry, two leaders in one term, silent log hole. Consequence: the driver has **no** ordering obligation whatsoever. | `MsgPreVoteResp` is explicitly **not** gated, with a correctness argument: pre-vote mutates no persistent state, so a pre-vote grant attests only to volatile facts that may be forgotten across a crash. Gating it would cost an fsync per election attempt for nothing. A test asserts pre-vote leaves `HardState` unchanged, so a future change cannot silently invalidate this. |
| **DR-9** | **Transport: fire-and-forget, no error return.** Sim messages cross the **real wire codec** by default. | Approved; fidelity confirmed as the default. No error return is deliberate — an error signal is a covert failure detector, and covert failure detectors are how consensus implementations accidentally become unsafe. Serializing kills cross-node aliasing and fuzzes the production codec across every soak message for free. | A fast path is permitted only on measured hunt-throughput evidence, and **must still deep-copy**: nothing ever shares message memory across nodes. Pass-by-reference is rejected outright — it manufactures bugs that cannot occur in production and masks ones that can. |
| **DR-10** | **Engine: `Apply(batch, sync) (SeqNum, error)` never blocks;** durability is a monotone `DurableSeq` advanced via `OnDurable`. | Approved. This is precisely the WAL contract: buffered writes readable and losable, fsync advancing a watermark, recovery restoring the acknowledged-synced prefix and nothing more. | Blocking `Apply` (LevelDB/Pebble style): incompatible with a non-blocking event loop; would force either a thread per node in sim (kills determinism) or two divergent engine interfaces. Separate blocking `Sync()`: cleaner in isolation, but CLAUDE.md specifies a sync flag on the batch write, and this honors it while leaving the C++ engine room for group commit. |
| **DR-11** | **Real-mode sync completion (amendment):** a per-engine poller goroutine owns the blocking `Sync()` and posts a durability event to the node **mailbox**. | Ansh's amendment, consistent with DR-2. The `OnDurable` callback therefore executes on the node loop in both modes. | C→Go callbacks across the cgo boundary: rejected as designed. The Go wrapper adapts a blocking C `Sync()` into the async contract, so the C API stays `extern "C"`, error-code-based, and callback-free. |
| **DR-12** | **`engine/model`: COW sorted slice,** upgrading to a hash-priority immutable treap behind a benchmark gate (>10% of hunt CPU). | Approved. Impossible to get wrong at sim key counts, and O(1) snapshots fall out for free. Treap priorities derive from `fnv1a64(key)`, never from RNG, so engine internals stay decoupled from the random stream. | `map[string][]byte` + sort-on-iteration: rejected outright. It invites `range` over a map — the exact determinism leak CLAUDE.md calls out — and hides iteration cost inside every scan. |
| **DR-13** | **`DeleteRange` stays in the frozen `Engine` interface.** `engine/model` implements it natively from A0.5. The C++ engine implements it internally as iterate-and-point-delete **through B2**; **real range tombstones are a B3 deliverable**, landing before I2 benchmarks. | **My recommendation was OVERRULED.** Ansh's rationale, which defeats mine on all three counts: (i) freezing the interface without it guarantees churn on *both* tracks later, and interface churn after B has built to it is the most expensive kind; (ii) snapshot application needs an atomic **clear-then-ingest**, which a best-effort Go helper cannot provide at the right isolation; (iii) replica removal at scale cannot be one giant point-delete batch without unbounded batch sizes and write stalls. The staging keeps B4's differential tests exercising the semantics early while deferring the hard LSM work. | My proposal — exclude it, provide a Go-side iterate-and-point-delete helper — traded a permanent interface defect for roughly one week of Track B schedule. Wrong trade, and recorded as such. Consequence: DESIGN-B1's scope updates, propagated via CLAUDE.md's Track B phase list so Session 2 sees it before writing B1. |
| **DR-14** | **Injectors: per-message dice + declarative plan entries;** reorder emergent from independent latency; drift affects tick rate, not just `Now()`; fire counts asserted, zero fires fails the run. | Approved. Fire-count assertion is the difference between a chaos suite and a chaos-shaped decoration. | Nothing rejected; the amendment below extends it. |
| **DR-15** | **Two injector capabilities confirmed explicitly (amendment).** (i) Crashes land inside the `Apply`-ack-to-durability window — via a real virtual-time interval, plus reactive `unsynced_window_open` rules, `sync_loss` widening, and a crash-point sweep; `unsynced_ops_lost` is measured, and a soak that lost zero unsynced writes is reported as **not having tested durability**. (ii) Clock offsets are piecewise-linear schedules, so a **sustained hold** near `maxOffset` is a flat segment. | Ansh's amendment, and (ii) drove a genuine model improvement: A8's lease-disjointness work needs a cluster that *sits* just short of the skew cliff across many lease acquisitions and transfers, which a drift-*rate*-only model cannot express — it can only sweep through. The `maxOffset` bound is enforced both by construction in the generator and by a continuous checker, because a generator bug that exceeded our own assumption would masquerade as a protocol violation. | Drift-rate-only clock model: rejected as insufficiently expressive for A8. Relying on chance for crash-in-unsynced-window: rejected in favor of targeted reactive rules plus measurement. |
| **DR-16** | **Determinism enforcement: `tools/determinismcheck`,** a custom `go/analysis` pass banning `time.Now`/`Sleep`/`After`, package-level `math/rand`, `net`/`os`/filesystem imports, `go`/`select`/`chan`/`sync` primitives, and **`range` over maps** in core packages. Escape hatch requires a written reason and is printed in every CI build log. | Approved, with `golang.org/x/tools` approved as a **tooling-only** dependency never linked into a shipping binary — enforced by a CI check on each `cmd/` binary's build graph. | Convention-and-review: rejected. The answer to "how do you know a `time.Now()` didn't sneak in?" must be a build failure, not a promise. |
| **DR-17** | **Tracing: rolling SHA-256 trace hash** as the determinism gate; full traces only on replay; first-divergent-step reporting; `--paranoid` adds per-step full-state digests. | Approved. Comparing O(1) hashes rather than storing traces is what makes the determinism gate affordable at 10k seeds. | Storing full traces for every seed: infeasible. Fast non-cryptographic hashing: available if profiling demands, but SHA-256's cost is small relative to sim work and removes all doubt. |
| **DR-18** | **Checkers: `Oracle.OnStep` halting on violation;** porcupine per key at end of run with a history cap and timeout. | Approved. Expensive checkers subscribe to relevant event kinds rather than running on every step. | Incremental linearizability checking on every step: rejected on cost; it is the dominant expense in most FDS-style harnesses. |
| **DR-19** | **Inconclusive is a first-class outcome (amendment).** Three outcomes per checker — pass / violation / **inconclusive** — with an explicit **inconclusive column in SOAK.md**. When the rate grows: shrink per-run history windows, then partition harder per key. **Never loosen the checker.** | Ansh's amendment. "Zero violations across N seeds" is quietly false if a fraction of those seeds never finished checking; the column makes that impossible to hide. A rising inconclusive rate is investigated as a harness regression, since the usual cause — a workload drifting toward pathological concurrency — is exactly when the checker matters most. | Folding timeouts into "pass": rejected as claim inflation. Raising the timeout or weakening the model to reduce the rate: rejected permanently. The fix is always a smaller problem, never a weaker oracle. |
| **DR-20** | **Mutant suite promoted to permanent policy (amendment).** Recorded as **Amendment 2** in CLAUDE.md: every BUGS.md root cause answers "which mutant class would have caught this"; if none exists, a new mutant lands **in the same PR as the fix**; CI tracks **kill-time per mutant** and treats regression in kill-time as a harness regression. | Ansh's amendment. The moment a fix lands is the only moment we will ever have a precise description of the blind spot that let the bug through. Kill-time turns harness sensitivity from a belief into a monitored number. | Filing a follow-up issue for the missing mutant: rejected. Follow-ups for test coverage do not get done, and the description degrades within days. |
| **DR-21** | **Section 7 idealizations approved as written,** and will be linked from README's verification section: instantaneous computation, i.i.d. per-link latency, replay scoped to `engine/model`, no Byzantine faults, bounded linearizability checking. | Approved. Every claim the project makes is bounded by this list, stated before anyone asks. | — |
| **DR-22** | **Repo: `git init` now.** First commit is CLAUDE.md + DESIGN-A0. `main` plus a `track-b` worktree, `.gitignore`, MIT license, CI lane skeleton with lanes stubbed. Module path `github.com/anshkanyadi/rift`. Go version pinned via a `toolchain` directive; CI runs exactly that version. | Approved. | Note for the record: the locally installed toolchain is **go1.22.5**, which is what A0.1 pins. It is not the newest upstream stable, and bumping later is a two-line change to `go.mod` plus the CI matrix — flagged rather than silently assumed. Separately, `GOPATH` is set to `GOROOT` (`/Users/anshk/go`), which Go warns about on every invocation; unrelated to this design but worth fixing. |

**Rulings of 2026-08-11, on A0.3 as landed.** Approved as landed: the analyzer, the three-valued
scope, the `make determinism` wiring, `tooling-only.sh`, mutation-testing the analyzer itself, and
the count-not-presence test idiom (now house style for anything emitting a stream). The corrections
and additions follow.

| # | Decision | Ruling & rationale | Rejected, and why |
|---|---|---|---|
| **DR-23** | **Scope is defined by when code runs.** Any code that executes during a simulated run is in scope, no exceptions; orchestration around runs is not. `engine/model`, `internal/rng`, `clock` and `sim` move in; real-mode adapters are excluded **by name** through a new exclusion list that wins over every other pattern. | Ansh's ruling, correcting three packages A0.3 left out — all in the same direction, all because they did not *look* like node logic. `engine/model` is the case that matters: replay identity is defined on it. An unclassified subpackage defaults **in**, so a new package arrives checked and leaves only by a written decision. | Scope assembled package by package from a list of what looks dangerous: rejected. It has no answer for the next package, so it drifts, and the drift is always toward less coverage. |
| **DR-24** | **The go1.23 iterator hole is closed.** `maps` is a banned import in core scope and `reflect.MapRange`/`MapKeys`/`MapIter` are flagged. Sorted iteration lives in `internal/sorted` and nowhere else — one helper, one hatch, one `range` statement in the repo. | Ansh's ruling. `slices.Sorted(maps.Keys(m))` contains no map-range syntax and is exactly the same nondeterminism; the syntax rule sees none of it. The hole opened with the toolchain bump, so it was closed in the same change. | Leaving it to review: rejected on the same grounds as D10 itself. Also rejected: putting the sorted-keys helper in each core package, since collecting keys *is* a map range and every copy would need its own hatch. |
| **DR-25** | **The hatch is a ledger.** Unused hatches **fail**. `HATCHES.txt` is a checked-in golden registry (`file:line` plus reason) diffed against the tree by a test. **No hatch sanctions `go`, `select`, `chan` or `sync` in core scope**; those are refused outright, consuming the hatch so the author gets one diagnostic instead of two. | Ansh's ruling. Warnings rot and nobody reads CI warnings, so a drifted hatch — the dangerous case, where something is unguarded and its author believes otherwise — has to fail. The registry makes adding an exemption a conscious edit to a reviewed list. The hard rule needs no annotation because concurrency in core is a design error, not an exception. | A stderr warning for unused hatches (as originally landed): rejected. Per-file hatches as the only form: kept, but they are the blunt instrument; the per-line form is the one to reach for. |
| **DR-26** | **Toolchain: the 1.26 line** (`go 1.26.0`, `toolchain go1.26.5`), `golang.org/x/tools` unpinned to latest. | **Ansh's earlier ruling (Q5/DR-22) overruled by Ansh.** 1.22 was never the current stable that ruling named, and it is out of support. Landed together with DR-24 because the iterator hole goes live with the bump; every lane, plus the blinded-analyzer suite, reran green on it. | Staying on the locally installed toolchain: rejected. An out-of-support toolchain is a security and compatibility debt that only grows, and the pin exists to make the version a decision rather than an accident. |
| **DR-27** | **Mutants are patches applied to a scratch worktree**, `sim/mutants/*.patch`, each header naming the mutant ID and the failure class it validates. The nightly mutant lane doubles as patch-rot detection. The same applies one level down for the determinism pass, in `tools/determinismcheck/blind/*.patch`. | Ansh's ruling, on a forward dependency found while landing A0.3: `sim/toy` is in core scope, so M4 and M5 **cannot** exist as committed Go files — the repo would not build, which is exactly what those mutants mean. | Committed mutant source behind build tags: rejected. Tagged code that must not compile is a contradiction, and tagged code that does compile is not the mutant. |
| **DR-28** | **The mailbox rule is provisional** until `node/` exists. A0 does not exit until it does and the rule has end-to-end teeth. | Ansh's ruling. A rule proven only against a fixture of itself carries an untested assumption about the shape of the code it will meet. | Deleting it until `node/` lands: rejected. Written now, it is in force the day the package appears rather than retrofitted onto code that has grown around its absence. |
| **DR-29** | **BUGS.md is for bugs the verification machinery caught in the system under test.** Tooling bugs — like the announcement-writer race A0.3's own test caught — stay out; the commit message is their record. | Ansh's ruling. BUGS.md is the evidence behind the verification claim, and diluting it with tooling defects weakens exactly what it is meant to prove. | Logging every bug found anywhere: rejected as claim dilution. |

**Amendments propagated to CLAUDE.md:** Amendment 1 (`internal/rng`, dependency wording),
Amendment 2 (mutant policy in BUGS.md), Amendment 3 (Track B `DeleteRange` staging in B2/B3),
Amendment 4 (SOAK.md inconclusive column).
