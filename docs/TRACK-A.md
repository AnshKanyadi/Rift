# Track A: what was built, how it was verified, and what the verification could not see

**This file is written for someone who has never seen this repository.** It is not a summary of the
design documents; it is the one file that should let a stranger understand what exists, what is
actually known about it, and where the knowing stops. If you read one thing here, read
[§8, the limits](#8-what-this-verification-cannot-see) — a document that states what it cannot claim
is the one worth trusting on what it does.

Track A is the Go half of Rift: a distributed, transactional, MVCC key-value database. Phases A0
through A7. Track B (the C++ storage engine) and the integration phases are separate and not covered
here.

---

## 1. What was built

| phase | what it added |
|---|---|
| **A0** | the deterministic simulator: one event queue, a seeded RNG, fault injectors (drop, delay, duplicate, reorder, partition, crash, restart, unsynced-write loss, clock drift and jump), and the `Clock` / `Rand` / `Transport` / `Engine` interfaces every later phase is written against |
| **A1** | single-group Raft from scratch: elections, replication, persistence with correct sync ordering, the figure-8 commit rule. A `Ready`-struct interface with **disaggregated acknowledgement** — raft withholds any message whose correctness depends on state the driver has not yet made durable |
| **A2** | snapshots, log compaction, pre-vote, leadership transfer |
| **A3** | single-node membership changes with learner catch-up, configuration carried across snapshots, leader stepdown on self-removal |
| **A4** | multi-raft: range descriptors over `[start, end)` with epochs, many groups on one transport, size-threshold splits, a router with retry semantics, and a manual rebalance command |
| **A5** | MVCC and hybrid logical clocks: versioned key encoding, data/lock/write records, reads at a timestamp, version GC, HLC with `maxOffset` |
| **A6** | Percolator-style distributed transactions: prewrite, commit primary then secondaries, reader-side lock resolution, transaction TTL, uncertainty-interval reads with restarts |
| **A7** | linearizable reads via read index: heartbeat-confirmed leadership, follower reads, and the term-start no-op the protocol requires |

The design decisions, including the rejected alternatives and why, are in `docs/DESIGN-A*.md`. Each
phase began with a design document and stopped for a written ruling before any code was written.

---

## 2. How it is verified

**Everything runs in a deterministic simulator.** One event queue, one seeded RNG, one event at a
time. The same seed produces the same trace byte for byte, so any failure replays exactly.

Four layers, and the distinction between them is the point:

1. **Structural invariants** — election safety, log matching, leader completeness, state machine
   safety, persist-before-reply, apply continuity, snapshot equivalence, range-epoch monotonicity,
   rebalance safety. Checked continuously; a violation halts the run and dumps the seed.
2. **Linearizability** — porcupine, per key, over the history a client actually observed. This is what
   catches a wrong *answer* as opposed to a wrong *structure*.
3. **Conservation laws** — a bank workload where transfers move money between accounts. Structural
   invariants bound the SHAPE of the state; conservation bounds its CONTENT. Two of A6's three defects
   were caught here and by nothing else.
4. **Mutation testing** — 65 planted defects, each with a named covering test, a detection floor and a
   seeds-to-detection ceiling. This is the layer that checks the other three.

**A checker is never weakened to get green.** If a checker looks wrong, the case is written down and
ruled on before anything changes. That rule produced §5's most useful findings.

### 2.1 An inconclusive is not a pass

A checker reports **pass, violation, or inconclusive**. A linearizability check that hit its timeout is
inconclusive, and the soak ledger carries an explicit inconclusive column. *"Zero violations across N
seeds"* is quietly false if some fraction never finished checking. When the inconclusive rate rises the
answer is a smaller problem — shorter histories, harder partitioning per key — never a longer timeout.

---

## 3. What was found

**25 defects, every one with a seed that reproduces it.** `BUGS.md` carries symptom, root cause, the
invariant that caught it, which mutant class would have caught it, and what it would have caused in
production.

The distribution is the interesting part:

| caught by | count | examples |
|---|---|---|
| a structural invariant | 9 | BUG-005 (a follower acked index 15 with 5 on disk), BUG-009 (a replica overwrote entries it had reported committed) |
| linearizability | 4 | BUG-004 (a client told its write succeeded; no committed entry contained it) |
| **conservation** | 4 | BUG-018, BUG-019, **BUG-022** (a transaction committed underneath an answer already given) |
| the harness checking itself | 5 | BUG-016 (the oracle blamed one move on another), BUG-020, **BUG-025** |
| a mutant surviving | 3 | BUG-023, and two dead-code findings |

**Two of these deserve a stranger's attention.**

**BUG-022** — a transaction committed at a timestamp *below* an answer the database had already given.
No error, no divergence, no structural invariant violated: a well-formed database with money missing
from it, on a schedule with **no faults in it at all**. Percolator does not need the guard that
prevents it because Percolator has a single timestamp oracle; per-node hybrid clocks do not. It was
caught by the bank conservation check and by nothing else.

**BUG-025** — `MsgReadIndex` was added to `raft/` carrying a context field, and the transport codec
serialises a fixed field list that did not include it. The message arrived with its type byte intact
and its payload zeroed. Follower reads were forwarded and answered by nobody, silently, while every
test passed — because the raft tests call the state machine directly and never cross the wire.

> **A unit test that exercises a mechanism without its serialisation will pass over a wire that does
> not work.** This was the second such defect; the first was six phases earlier in a different codec,
> and after it nothing had been built that would catch the next.

---

## 4. The register: 26 instances of verification that verified nothing

The single most productive artifact in this repository is a numbered list of times a checking
mechanism reported success while checking nothing. It lives in `docs/DESIGN-A1-raft.md §5c` and it is
what most of the standing rules came out of.

**Three entries carry the argument:**

**#8 — the oracle read the engine's own account.** The checker whose job is catching a node that claims
durability it does not have was comparing that node's claims against *the engine's view of what it
held* — one layer of indirection away from asking the accused. The design document had already written
*"an oracle which interrogates the engine believes the lie"*, and the implementation did it anyway, in
the mechanism that sentence was written to protect.

**#23 — an opt-out is a claim that switches off its own instrument.** A mutant class carried a written
exemption saying the test sweep could not reach it. The lane skips any class with an exemption, so the
claim was never measured. When it finally was: **280 detections in 300 seeds.** The claim had been
false on the day it was written, not gone stale, and it stood for a phase and a half because writing
the exemption is what turns the measurement off.

**#26 — two tests written for this exact failure mode, both vacuous.** Written by someone who had spent
that day documenting twenty-five prior instances, to assert two properties that had just been ruled on.
Both passed under the exact mutation each existed to catch. **One command found both.**

> The register's thesis is that this class is not a competence problem. #26 is the experiment: maximum
> attention, maximum recent exposure, a rule requiring exactly this check — and the tests were still
> vacuous, and no amount of reading them would have shown it.

---

## 5. The general forms

Each came from a specific defect and each is now enforced somewhere.

**A fact maintained by the apply path is a function of the log.** The moment an operation is answered
off the log, every fact that operation used to maintain becomes a fact somebody has to maintain
somewhere else — *and the place it used to live will still compile.* (BUG-022 meeting A7's read index.)

**A detection floor is a property of the class and the SHAPE, jointly.** The same mutant measured `0 of
200` under one phase's workload — reported as unreachable — and `22 of 600` under the next, while being
killable by hand throughout. A floor recorded without its shape is not a measurement.

**A planted defect tests the checker as much as the code.** A kill is evidence about both, and only
about the code if the verdict *describes the defect that was planted*. A7's differential oracle was
caught being wrong this way: it killed its mutant, and the verdict described something the mutant does
not do.

**A green with no baseline is not a result.** Regenerating a corpus after a change made the lane green
in 102 seconds with three bundles silently no longer carrying their findings. The before-verdict, taken
first, made them a two-line diff.

**Absence as evidence needs the experiment verified independently of the result.** A lane that reads
*nothing happened* as confirmation cannot tell an absent effect from an absent experiment — so a
mutation search must verify the mutation is actually present before believing a null.

**Started is read from the process, never from the launch.** Five separate times a number or a state
was quoted from a source whose provenance had not been checked, including two hours of believing an
exit run was in flight when it had refused at launch.

**A label that collapses two opposite meanings is worse than no label.** One exemption keyword meant
both *nothing can reach this* and *something better than a sweep covers this*. The classes wearing it
in the second sense were the best-covered in the tree — killed deterministically in about a second —
and read identically to the ones nobody had measured.

---

## 6. The mechanisms these produced

Nothing above is enforced by intention. Each is a lane that fails:

- **the induced-failure rule** — no gate counts until its failure has been made to happen. Every oracle
  and assertion in this repository has been watched to go red.
- **the two-number standard** — an oracle must fire on its planted defect *and be silent on a clean
  tree*, both at a stated seed count. The induced-failure rule asks only the first, and A7's
  differential passed it while being wrong.
- **mutation power floors and ceilings** — every class carries a detection rate AND a
  seeds-to-detection bound, because a class can keep its rate while moving out of reach of every short
  run. The ceiling caught two live classes reading as dead.
- **declaration checks in milliseconds** — the expensive lanes' *inputs* are checked by cheap lanes
  that actually run, because the measurement costs fifteen CPU-hours and nothing schedules it.
- **provenance typing** — an oracle's inputs are harness-observed; a fact the system reported about
  itself cannot compile into a verdict.
- **a corpus of failing seeds** — every historical bug replays from its bundle, and a separate lane
  requires each bundle to still *produce its finding* rather than merely still run.

---

## 7. The numbers

Filled from the exit runs and the soak ledger, and from nothing else.

| | |
|---|---|
| phases signed | A0 – A6 (**A7 pending**) |
| defects found, all reproducing from a seed | **25** |
| mutant classes | **65**, each with a covering test and a measured floor |
| corpus bundles | **24** |
| vacuous-green register | **26 instances** |
| A6 exit run | 25,000 seeds, zero safety violations |
| A7 exit run | *in flight at the time of writing* |

---

## 8. What this verification cannot see

**A signed phase is signed against a fault mix, not against the world.** This is the most important
sentence in this document and it has two measured demonstrations:

- **A mutant that plants a real defect detected once in 3,000 seeds under one phase's fault mix
  detected ZERO times under the previous phase's.** The defect did not change. The schedule did.
  Widening the mix did not make the numbers look better — it made a real class findable.
- **A whole phase whose headline feature is hybrid logical clocks ran its entire verification with
  clock skew injection switched off.** The configuration said `Holds = 0`, correct when written two
  phases earlier and false from the moment clocks mattered. Nothing connected *this phase is
  clock-sensitive* to *this phase's plan generates clock faults*, and turning it on immediately
  produced a defect.

> Every fault count and duration in the workload configuration is a claim about which defects this
> project can find. **"Zero violations" always means zero violations under the schedules that were
> generated**, and the schedules are a choice.

### 8.1 Idealizations, from DESIGN-A0 §7

- **Computation is instantaneous** unless a slow-node entry says otherwise. Bugs that need a slow
  computation to race a fast one are out of reach.
- **The network is per-link i.i.d. latency**, not a topology with shared bottlenecks. Correlated
  congestion does not occur.
- **Deterministic replay is scoped to simulator runs on the Go model engine.** The C++ engine's
  correctness rests on different instruments, and this scoping is stated everywhere the claim appears.
- **Byzantine faults, disk bit-rot outside injected torn writes, and malicious clients are out of
  scope.** The transport reorders, duplicates and drops; it never corrupts bytes.
- **Linearizability checking is bounded** — per key, per run, with a history cap and a timeout, and a
  check that times out is inconclusive rather than passing.
- **Clock uncertainty is a static promised bound, not a measured one.** Real deployments carry a
  measured uncertainty that widens under load; this one does not.
- **Every client request routes from a cold descriptor cache**, which over-exercises the stale-routing
  path rather than under-exercising it.
- **Garbage-collection pressure is far below production.**

### 8.2 Recorded gaps

- **Unexercised interleavings**, recorded rather than discovered: a rebalance move racing unrelated
  membership churn is the interleaving a production cluster produces constantly, and it is disabled
  because the oracle cannot attribute a removal when both drivers are live. It is carried forward with
  the assertion that fails the day it becomes reachable.
- **Three clock-dependent mechanisms are not established as exercised** — a snapshot-built range whose
  records outrank its clock, two replicas deriving different GC marks under skew, and a snapshot read
  routed to a split-born range. None is a claim that a defect exists; each is a claim that the absence
  of one has not been shown.
- **The symmetric-apply gap.** An apply path wrong identically on every replica is invisible to replay
  equivalence, because that instrument compares two executions and both would be wrong the same way.
  It is covered by a list of mutant classes, which is a claim rather than a proof. One class in that
  list is covered by a mutant and nothing else.
- **Two paths protecting one property by different mechanisms** are invisible to an instrument that
  compares their outputs: they agree on the answer while disagreeing on why, and either mechanism can
  rot to zero load undetected.
- **`make power-mutants` has no executor.** It costs roughly fifteen CPU-hours; there is no CI remote,
  and a pre-push hook cannot hold it. Its first complete run in the project's history happened during
  A7 and came back with eight failures, three of which were real findings.

### 8.3 The largest standing risk

**Nothing inside this repository can make a lane run.** Eight lanes run because a git hook exists;
everything expensive runs when somebody remembers. Four separate silent breakages have been found in
the one lane whose entire purpose is noticing when detection power drops — and none was found by
anything that looks. Two of them left it unable to return a verdict in either configuration.

> The detector for "a lane stopped" is itself a lane in the column that does not run.

The only mitigation available from inside is a **millisecond check on the inputs of an hours-long
lane**, and those are in the hook. They check the lane's claims. They cannot make the lane run.

---

## 9. How to check any of this yourself

```
make test        # unit lanes, every path runs
make smoke       # 500-seed sweep
make corpus      # every historical bug still replays from its bundle
make power       # mutation floors: every planted defect still detected at its floor
make exit-run    # the phase gate: 25,000 seeds, sharded
go run ./cmd/simctl replay --bundle seeds/BUG-022    # any recorded defect, exactly
```

Every number in this document comes from one of those, or it is not in this document.
