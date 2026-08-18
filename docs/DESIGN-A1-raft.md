# DESIGN-A1: single-group Raft

**Status:** code has landed and this document has been amended to match it — see §5a and §5b, which
record what the implementation taught and what it contradicted. Not signed off; Claude does not mark
phases complete. **Author:** Claude. **Decider:** Ansh.
**Phase:** A1. **Depends on:** A0, closed — the harness, oracles, mutant suite and real-mode driver.

---

## 0. The ruling that shapes everything else

Recorded before a line of `raft/` was written, because it is the constraint the rest of the design
bends around.

> **Ansh, 2026-08-17.** *The four safety oracles read from the Ready stream and from what each node
> persisted, never from `raft.Raft` internals. Oracle independence is the thesis of this entire
> project and the recorded sentence is that an oracle which interrogates the engine believes the lie.
> An oracle reading `raft.Raft` state directly cannot detect a Raft whose in-memory state disagrees
> with what it persisted or emitted, which is precisely the bug class Raft implementations actually
> have. You are right that leader completeness is materially harder this way, since it is defined over
> committed entries across configurations; build the harness-side ledger that makes it expressible
> rather than reaching into the node, and if some property turns out genuinely inexpressible from
> outside, stop and report it as a written case rather than quietly reaching in.*

**What that forbids, concretely.** No oracle may call a method on `*raft.Raft`, read its role, its
term, its log, its commit index, or its match table. The oracles are handed a **ledger** built from
two streams and nothing else:

1. **What each node emitted** — every `Ready` the driver observed: hard state, entries to persist,
   entries to apply, messages to send.
2. **What each node persisted** — every write that reached the `Engine`, and when it became durable.

**Why this is not pedantry.** The interesting Raft bugs are exactly disagreements between memory and
durability: a node that votes, replies, and crashes before the vote is durable; a leader whose
in-memory commit index runs ahead of what its followers acknowledged; a term bump applied to memory
and lost on restart. An oracle reading `r.term` sees the intent. An oracle reading the ledger sees
what the cluster actually did, which is the thing the next leader will act on.

**The cost, accepted.** Leader completeness is defined over *committed* entries and *all future
leaders*, neither of which is a field anywhere. §5 builds both from the outside.

---

## 1. `raft/` is pure, and it is in core determinism scope from the first commit

No goroutines, no channels, no clocks, no I/O. Input is `Step(Message)` and `Tick()`; output is a
`Ready`. This is the property that makes deterministic simulation possible and it is not retrofitted:
the package lands inside the determinism pass's core scope, so `math/rand`, `time.Now`, map iteration
and concurrency primitives are build failures there from the beginning.

**Randomised election timeouts** are the one thing a pure Raft cannot produce for itself. `Raft` holds
an integer `randomizedElectionTimeout` and exposes `SetElectionTimeout(int)`; the *driver* supplies it
from a plan-carried PRF keyed by `(node, term)`. No live draw, no `math/rand`, and the value is
reproducible from the plan alone — the same discipline as the transport's per-message dice.

---

## 2. Persistence goes through the `Engine`, and persist-before-reply is a `Ready` contract

Every Raft persistent-state write — term, vote, log entries — goes through the frozen `engine.Engine`
interface. Not a side table, not a test shortcut. A crash therefore takes exactly what a crash should
take, and recovery is the real path: `raft.Restore` reads the engine back and rebuilds the state
machine, so the restart path is exercised by every crash the harness injects rather than by a
bespoke test.

**The contract, as it actually landed.** An earlier draft of this section said the driver must make
`HardState` and `Entries` durable before it sends `Messages`, and proposed an oracle to check that it
had. That design makes persist-before-reply *conventional* and then guards the convention. **An
oracle guarding a rule that should be structurally unbreakable is the weaker design wearing the
stronger design's clothes**, and it passes review precisely because it has an oracle attached.

Under DR-7 the property is structural instead. `raft` never places a message in `Ready.Messages`
whose meaning depends on state that is not yet durable: gated messages are withheld inside the
package and released when `AckPersisted` arrives. **The driver therefore has no ordering obligation
at all** — it cannot send a message early, because it never holds one.

The oracle survives, demoted and stated as such. It no longer stands between the cluster and two
leaders in one term; it confirms from outside that the interface behaved as its contract says, which
is a different and much weaker claim, and it is worth keeping only because a structural guarantee
nobody observes is a guarantee nobody would notice losing. It found BUG-005 in that reduced role,
which is the argument for keeping it.

The generalization is worth stating once: whenever a safety property can be discharged in the type
system or the interface, an oracle for it is a consolation prize, not a defense.

---

## 3. The four safety oracles

Each is an in-run `Oracle`, halting the run at the first violation. Each is induced by a planted
violation landing in `sim/mutants/` **in the same commit**, per Amendment A2, so none counts until it
has been shown to fail.

| oracle | property | expressed from |
|---|---|---|
| election safety | at most one leader per term | messages: a node that sends `MsgApp` in term T acted as leader in T |
| log matching | two logs agreeing on (index, term) agree on every entry before it | persisted entries per node |
| leader completeness | an entry committed in term T is present in every later leader's log | the committed ledger, §5 |
| state machine safety | no two nodes apply different entries at the same index | apply streams |

None of these reads node state. Election safety in particular is deliberately expressed over *emitted
messages* rather than over a role field: a node whose role says follower while it is still sending
append entries is a real bug, and the message stream is where it is visible.

---

## 4. Schedule mix: the single-cut geometry is weighted

DESIGN-A0.7 blessed directed partitions with a forward binding — *A1's schedule mix weights the
single-cut send-without-receive geometry.* That binding is honoured here. A symmetric cut is two
directed cuts and produces a cleanly isolated node; a **single** cut produces a node that can send but
not receive, which is where the interesting consensus bugs live: it campaigns, bumps terms, and never
learns it lost. Symmetric partitions never generate it.

---

## 5. The harness-side ledger, which is what makes leader completeness expressible

Leader completeness is *"if an entry is committed in term T, it appears in the log of every leader of
every term greater than T."* From outside a node, none of those three things is directly visible, so
each is reconstructed:

- **Committed.** A node's `Ready.Committed` is what it applied. An entry is *committed* the first time
  any node applies it — a node only applies what its leader told it was committed, so this is a sound
  outside witness. The ledger records `(index, term, data, first-applied-at)`.
- **A leader of term T.** Any node that emitted an `MsgApp` bearing term T. Recorded as
  `(term, node)`; election safety separately asserts this is a function rather than a relation.
- **That leader's log.** What that node had *persisted* at the instant it first acted as leader.

So the check is: for every committed `(index, term)` and every leader of a later term, that leader's
persisted log at the moment it began leading contains that exact entry. Expressible entirely from the
two streams, with no reach into the node.

**If something turns out inexpressible from outside, the ruling is to stop and make the written
case.** Nothing has, so far.

---

## 5a. A safety oracle over an unknown-dominated history is vacuous

This section exists because A1's first finding was not a Raft bug. It was a demonstration that the
verification machinery could vouch, in full, for a system that was doing nothing at all.

### What happened

`store/codec.go`'s `decodeMessage` read eleven `uint64`s where `encodeMessage` wrote ten. Every frame
failed to decode. **No message in the cluster was ever received.** No node ever became leader. All
forty client operations in the run went unanswered.

Porcupine returned **PASS**.

The four safety oracles were green too, and correctly green: no node ever led, so election safety
held; no log ever diverged, so log matching held; nothing was ever committed or applied, so leader
completeness and state machine safety held vacuously. Total system failure, clean safety verdict.

The only mechanism that saw anything was the **election census**, reporting zero elections won.

### Why the checker was right to pass

A history of unknowns is trivially linearizable. Every one of those forty operations was still in
flight when the run ended, and an in-flight operation is a *free choice* for a linearizability
checker — it may place the operation in whichever world satisfies the rest. With no decided
operations there is nothing to satisfy, so the checker is free, and free means green.

That is not a defect in porcupine and not a defect in the oracle framework. It is what
linearizability means. The defect was in believing a green verdict over that history said something.

### The rule

> **A safety oracle over an unknown-dominated history is vacuous. Every safety claim in this project
> is therefore paired with a liveness census proving the system did the thing whose safety is being
> asserted.**

"Zero safety violations across 10,000 seeds" and "the cluster never did anything on 10,000 seeds" are
the same sentence unless something counts the elections. §6's criterion 8 was written for this reason
and is not a nice-to-have: a mix that produces no contention is a mix that needs fixing, and that is
invisible unless it is counted.

### Where the rule lives now, so it is not a habit

Two structural rules, from the two sides, each induced by its own mutant:

| side | rule | verdict | mutant |
|---|---|---|---|
| client | a history below `sim.UnknownDominatedPerMille` **decided** operations | inconclusive | `M15-vacuity-rule-removed` |
| cluster | a run that elected nobody | inconclusive | `M16-no-leader-banks-a-pass` |

Neither is a pass, ever. Amendment A4 already forbade counting an inconclusive as a pass; these two
say what else must be inconclusive.

The threshold is 250 per mille, derived rather than chosen. Measured over 2000 A1 seeds, decided
operations per mille of the history: min 0, p1 550, p5 700, p25 850, p50 900, p90 975, max 1000.
The floor sits at roughly half the observed 1st percentile — the same margin rule the harness-power
floors use — and flags 3 seeds per mille against a 30-per-mille inconclusive ceiling. The literal
reading of "unknown-dominated", more unknowns than knowns at 500 per mille, was measured (7 per
mille) and rejected: it spends a quarter of the ceiling on healthy runs, and a gate that fires on
healthy runs is a gate somebody eventually loosens.

Also note the shape of the original floor, because it is the same error one level down. Checklist
step 6 put a minimum-operations floor in `CheckAll` so a checker that consumed nothing could not bank
a green — and it counted operations **recorded**, which asks whether the harness produced traffic. It
now counts operations **decided**, which asks whether the run produced evidence. The codec bug
satisfied the first floor with forty operations and produced none of the second.

---

## 5b. Three corrections to this document, made while closing BUG-005

Recorded here rather than in a commit message alone, because each contradicts something §2 or the
`Ready` contract asserted, and a design document that quietly stops describing the code is worse than
one that never existed.

**The ledger's second stream cannot be an engine read-back.** §0 names it as *"every write that
reached the Engine, and when it became durable"*, and the driver implemented it by reading the engine
back. An `engine.Engine` read returns the **visible** state, which by construction includes batches
applied and not yet synced — that window is the whole point of the model (DR-15). The ledger's durable
watermark was therefore inflated: across 10,000 seeds the read-back was ahead of durability 44,911
times. An inflated watermark does not make the persist-before-reply oracle noisy, it makes it
**silent**, because every ack looks covered. The driver now *records* what it made durable, folded
forward from the writes the engine completed and dropped wholesale on a crash, which is what §0 said
in the first place. With the honest record the 300-seed sweep went from 2 violations to 257.

**A mark's coverage is frozen at handover.** The original design had `dirty()` reuse the open mark
until it was acknowledged, on the reasoning that at most one mark is then open at a time and one
high-water index suffices. The premise is fine; the consequence is not. A reused mark's coverage
*grows after the driver has started writing it*, so the acknowledgement means strictly less than the
messages gated on that mark require — the driver reports batch one durable and raft releases an
append response attesting to batch two. It is also a convoy: under a steady stream of appends a
reused mark never stops growing and never completes, so everything gated on it waits forever. Each
handover now takes its own mark, `tail.persisted` advances only on an acknowledgement that reaches
the most recent handover, and there is still no per-mark span table — two scalars, `markLastIdx` and
`lastHandedMark`.

**Durable is not committed.** `truncateFrom` asserted that no truncation may reach at or below the
durable watermark, on the reasoning that the driver would then have acknowledged an entry that later
vanished. That is a stronger claim than Raft makes and a false one: §5.3 of the paper has a follower
delete a conflicting entry and everything after it, and those entries are routinely already on disk.
A follower's persisted suffix being overwritten by a new leader is the protocol working. What may
never be truncated is a **committed** entry, which is what it asserts now. The false assertion was
unreachable for exactly as long as the durable watermark never moved, and fired on the first seed
after it did.

One thing was *not* corrected, and the honest note matters more than the code: gating the accept
response on the later of `markFor(last)` and the term mark is the correct general expression of the
two gates DR-8 enumerates separately, and at A1 **the two provably coincide**, so it changed no
verdict. What makes the coincidence checkable rather than assumed is the assertion in `markFor`: an
index that is neither durable nor covered by an open mark has no gate to wait on, and that state is
now refused where it is constructed. It becomes load-bearing when A2's snapshot stream gives
`markFor` a second answer.

---

## 6. Exit criteria

All true before this is reported ready for sign-off:

1. 10k seeds green, zero safety violations.
2. Porcupine over client histories, inconclusive tracked separately and never counted as a pass, with
   the count *and cause* reported.
3. Election safety, log matching, leader completeness and state machine safety each an oracle, not a
   test.
4. Each of those four induced by a planted violation before it counts.
5. Schedule mix weighting the single-cut send-without-receive geometry.
6. Persist-before-reply inside `raft/`, checked from the ledger.
7. Every Raft persistent-state write through the `Engine`; recovery the real path.
8. **Elections observed actually contending**, not merely completing: a census of terms, elections
   started, elections won and split votes across the 10k. A run where the leader is never challenged
   proves nothing, and a mix that produces one is a mix that needs fixing.
9. Every bug found entering BUGS.md, each answering which mutant class would have caught it, with a
   new mutant landing in the same commit as the fix if none would have.

### Status against those criteria

Claude does not mark phases complete; this is evidence for a ruling, not a ruling.

| # | criterion | evidence |
|---|---|---|
| 1 | 10k seeds, zero safety violations | 10,000 seeds: pass 9966, violation **0**, inconclusive 34, errors 0 |
| 2 | porcupine, inconclusive tracked separately and never a pass | 34 inconclusive, 3.4 per mille against a 30 per mille ceiling, each with its cause printed |
| 3 | four oracles, in-run, halting at the first violation | `raftcheck.All`; each reads the ledger and nothing else |
| 4 | each induced by a planted violation | `M17` election safety 146/300 · `M18` log matching 1/300 · `M19` leader completeness 228/300 · `M20` state machine safety 46/300 |
| 5 | schedule mix weights the single-cut geometry | `RaftGenConfig`, §4 |
| 6 | persist-before-reply structural inside `raft/`, checked from the ledger | DR-7 gated queue; the oracle is the outside confirmation, and `M25` induces it |
| 7 | every persistent write through the `Engine`; recovery the real path | `store/node.go`; `Restore` runs on every injected restart |
| 8 | elections observed contending | highest term 79, 111,790 started, 48,253 won, 43,442 split votes, 9,641 of 10,000 seeds contended, **0** seeds without a leader |
| 9 | every bug in BUGS.md with its mutant-class answer | 5 entries, 5 mutant classes, 4 of them added because none existed |

Mutant suite alongside: 21 killed, 1 canary alive, 0 mismatched, 0 rotted.
