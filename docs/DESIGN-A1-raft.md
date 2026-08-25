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

**A mark's coverage is frozen at handover.** *(Instructed otherwise; corrected; **ratified by Ansh**
after the correction was reported.)*

The instruction was that `dirty()` reuse the open mark until it is acknowledged, on the reasoning
that at most one mark is then open at a time and one high-water index suffices. The premise is
correct and the consequence is not, and the reason is durability-critical enough to write down rather
than leave a future reader assuming the design was arbitrary:

**A reused mark's coverage grows after the driver has already started writing it.** The driver
submits batch one under mark *m* and begins its sync; more state is mutated; the same mark *m* now
also covers batch two. When batch one completes, the driver reports *m* durable — truthfully, for
everything it knew *m* covered when it wrote it — and raft releases an append response attesting to
batch two, which is still in flight. That is BUG-006: 257 of 300 seeds, one entry acknowledged ahead
of its own write.

It is also a **convoy**. Under a steady stream of appends a reused mark never stops growing, so it
never becomes fully durable, and every message gated on it waits behind writes that keep arriving.
Safety and liveness fail together, which is unusual and is a sign the abstraction was wrong rather
than the arithmetic.

Each handover now takes its own mark, and anything mutated after it gets a new one. `tail.persisted`
advances only on an acknowledgement that reaches the most recent handover, lagging conservatively
rather than guessing. There is still no per-mark span table: two scalars, `markLastIdx` and
`lastHandedMark`.

The driver keeps an independent second defence — it acknowledges a mark only when *every* write
issued under it is durable — and BUG-006 records the measurement showing that either one alone
prevents the defect (0 of 300 each, 257 of 300 with both removed). That is why `M28` removes both.

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

## 5c. The oracle was reading the engine's own account

This is the most important result in A1 and it outranks the bug it was found under. It is written
here at length because it is the strongest evidence this project has that its central discipline is
load-bearing rather than decorative — and the evidence is that the discipline was **violated in the
mechanism that exists to enforce it**, and nothing noticed for a whole sweep.

### What was true

`raft.tail.persisted` — the highest log index the driver has acknowledged durable — was assigned
**nowhere**. `persistedIndex()` had been returning 0 since the line was written.

The ledger that the persist-before-reply oracle judges every acknowledgement against was built by
calling `store.readDurable()`, which reads the engine. An `engine.Engine` read returns the **visible**
state, and the visible state includes batches that have been applied and not yet synced — that window
is not an accident, it is the entire point of the model and DR-15 says so explicitly.

So the oracle whose job is to catch a node claiming durability it does not have was comparing that
node's claims against **the engine's own account of what it held**, one layer of indirection removed
through the ledger.

### Say the uncomfortable part

§0 of this document records the ruling that shapes the whole phase:

> *an oracle which interrogates the engine believes the lie.*

That is exactly what happened, to the letter, in the mechanism the sentence was written to protect.
Not a near miss and not a different failure that the rule would incidentally have covered: the
recorded sentence names this failure, and the implementation committed it anyway, because the reach
into the engine was one function call away from the oracle instead of inside it. Indirection was
enough to hide it from everyone who read the code, including the person who wrote the ruling into the
package comment directly above it.

**And it was found by fixing an unrelated field.** Nobody audited their way to it. `tail.persisted`
was corrected because a *different* defect needed a durability watermark that moved, and the moment
it moved the oracle started disagreeing with the engine. If that unrelated fix had not been necessary,
A1 would have shipped 10,000 green seeds over an oracle that could not fail.

### What it cost, in numbers

| measurement | value |
|---|---|
| times the read-back was ahead of true durability, 10,000 seeds | **44,911** |
| violations on a 300-seed sweep, oracle reading the engine | 2 |
| violations on the same 300 seeds, oracle reading what the driver recorded | **257** |

An inflated durability watermark does not make an oracle noisy. It makes it **silent**: every
acknowledgement looks covered. A checker that reports false violations is annoying and gets fixed in
a day; a checker that reports nothing is indistinguishable from a system that is working, which is
why this class costs so much more than it looks like it should.

### Where it sits in the class

The eighth, and the first inside an oracle. The register, so the count is checkable rather than
rhetorical:

| # | mechanism | what it reported | source |
|---|---|---|---|
| 1 | the loop marked a crashed node down without telling it | crashes injected: none, silently | `sim/hunt/floors.go` |
| 2 | `Trigger` counted `Times` per condition, not per rule | a restart rule sharing a trigger never fired; detection ran at a sixth of its power | `floors.go`, checklist step 7 |
| 3 | the fire-count machinery, broken four ways, including `Counters.Check()` never called | `min_fires` decorative from step 4 | `floors.go` |
| 4 | `History.Validate`, written to reject an uncheckable history | called by nothing | `sim/oracle.go` |
| 5 | `simctl` ran on `noopNode{}` | the gate hashed the loop, transport, plan and clock, and never the toy | checklist step 8(b) |
| 6 | the minimum-operations floor counted operations *recorded* | 40 unanswered operations satisfied it; porcupine returned PASS | BUGS.md BUG-001 |
| 7 | `StaleEpochDrops` and `EpochFailure`, collected every run | consulted by nothing; `M14` survived the suite | BUGS.md BUG-002 |
| 8 | **the ledger's durability record, read back from the engine** | **the oracle compared the system against its own account, 44,911 times ahead of the truth** | this section |
| 9 | the mutant suite could not tell a deleted covering test from an uncaught defect | `go test -run` exits 0 on no match, so a mutant whose test had been removed read as ALIVE and blamed the checker | DESIGN-A2 §9.6 |
| 10 | `make power` floored four TOY classes and **zero** mutant classes | green through the largest power regression in the project: M18 10-in-500 to 0, M19 228-in-300 to 1 | DESIGN-A2 §9.7 |
| 11 | the range-epoch check, guarding *no request served under a stale descriptor epoch* | **zero** refusals across 10,000 seeds; the sweep's clients carried no routing, so the check was skipped on every request they ever made | DESIGN-A4 §9.4b |
| 12 | the power lane's rate floor could not see a **kill-time** regression | M19's rate held (10 to 7 per 1500, floor 4) while its seeds-to-detection tripled, 145 to 553; the lane was green and the class had left the reach of every short run | DESIGN-A5, and Amendment A2 had named it |
| 13 | **`make corpus`, guarding the reproducibility claim itself** | **green while a bundle had stopped carrying its finding**: a fixed bug's bundle records no violation by design, so the lane compared a clean replay against a clean recording and matched | DESIGN-A5 §16 |
| 14 | the power lane's `power-config` default, spelled `a3` | it meant "what the sweep runs" and was written when A3 was what the sweep ran; the probe had no case for `a3` at all, so it fell through to current — correct behaviour under a label that had stopped describing it | DESIGN-A5 §11b |
| 15 | **snapshot-isolation's stability property** | the same `(key, timestamp)` is almost never asked twice by accident, so the property ran over an **empty set** and reported green; it took a deliberate second pass by the workload before it compared anything | DESIGN-A6 §9, §15 |
| 16 | **`cfg.Holds = 0`**, carrying its own justification: *A1 Raft has no clock-sensitive logic* | true when written; A6's phase headline is *hybrid logical clocks with uncertainty intervals* and its sweep injected **no clock skew at all**, so every uncertainty restart it ever produced came from HLC ordering rather than from clocks disagreeing | DESIGN-A6 §18 |
| 17 | **`tools/provcheck`**, the lane that enforces oracle-input provenance | **red across a whole commit** and nobody saw it, because there is no remote and the lane only runs when somebody remembers to type it | DESIGN-A6 §20 |
| 18 | **`make test` itself**, the every-change lane | `go test ./...` with nothing set runs the exit sweep at its 10,000-seed default — about 26 hours at A6's cost, and dead on Go's ten-minute timeout long before that. Unrunnable since A1, unnoticed for the same reason as 17 | DESIGN-A6 §20 |
| 19 | **the guard for BUG-021's own class**, keyed on `(primary, startTS)` | the class was predicted and guarded, and the key was one field too wide: a version is addressed by `EncodeKey(ns, key, startTS)`, which has no primary in it, so the counter read **zero** on precisely the seed that had the collision | DESIGN-A6 §22.7 |
| 20 | **a CORRECTION written to stop a repeat mistake**, pointing the next reader at `TxnRecord.Restarts` | the field was written by nothing and read zero however many restarts occurred, so the correction converted *I do not know* into *no* | DESIGN-A6 §29.2 |
| 21 | **`make power-mutants`'s declarations**, asserted against measurements nobody had taken | red since `M67` and `M70` landed and unseen for half a phase, because the lane costs fifteen CPU-hours and nothing on this machine runs it. The cheap check over its inputs found **six** inconsistent declarations in milliseconds | DESIGN-A6 §31, §37.2 |
| 22 | **`TestPowerProbe`'s `noticed()`**, inside the power lane itself | it consulted a hand-listed subset of the harness's detectors, so no class whose detector is an aggregate assertion could be measured at all — and it reported zero for those classes as though zero meant unreachable | DESIGN-A6 §35.1 |
| 23 | **an OPT-OUT**, which is a reachability claim | `power-mutants.sh` skips any patch carrying a `power:` line, so the claim exempts itself from the only instrument that could refute it. `M56`'s was reasoned by analogy with `M53` and never measured; it is **280 of 300, first at seed 0**, and **28 of 30 under A5's own shape** — false on the day it was written, not gone stale | DESIGN-A6 §42.3 |
| 24 | **the DECISION about which claims to re-measure**, at DESIGN-A6 §42.1 | the pass that re-measured every class reading zero **excluded `M30` by citing `M30`'s own declaration** — *measured trace-identical over 10k seeds* — rather than a measurement. `M30` is **1 of 300 with a leader-completeness violation at seed 178**: `committed is forever`, broken. The one class reasoned out of the set was the one that was wrong | DESIGN-A6 §43.5a |
| 25 | **`power-mutants`' MEASURING path, against its own GATING path** | `--measure` at `POWER_JOBS=1` sets its status inline and never reads the result file, so it produced correct numbers for a whole cycle — while the gating path read `cut -f1` over a multi-line file and could not report a pass for any class whose probe emitted a sweep line. **One tool, two entry points, and only one of them worked** | DESIGN-A6 §43.9e |

Seven of the first eight were in the harness. The eighth was in a **verdict**, which is the difference
between a machine that finds less than it should and a machine that certifies something false.

**Instance 19 is a third shape.** Sixteen were instruments that ran and measured nothing; two were
instruments that did not run. This one **ran, was aimed at the right class, and was keyed one field
too wide** — the prediction was right and the guard was wrong, which is the most confident kind of
green there is. The general form: *an assertion keyed on a compound identity is only as strong as the
narrowest thing the identity actually addresses, so the key must come from what the data structure is
addressed by, not from what the concept feels like it is owned by.*

**Instances 17 and 18 are a different shape from the sixteen before them.** Those were instruments
that ran and measured nothing. These two are instruments that **did not run at all** — and the
distinction matters, because everything this register has taught about writing checkers is useless
against a lane nobody executes. A lane that is not run is not a lane. DESIGN-A6 §20 carries the audit
of which lanes run on every change and which run on memory.

**Everything after the eighth has been in an instrument**, and that is the finding the register now
carries: 9 and 10 in the mutant and power lanes, 12 in the power lane's floor shape, 14 in its config
label, and 13 in the corpus lane. The things that watch are the things nobody watches. 11 is the one
exception, and it is the one that shows where to look next: it was in a *mechanism the oracles depend
on*, one layer below where the audit was looking, because the previous ten had been in oracles.

**Instances 20 to 23 close A6, and 23 is the one that changes what the register is FOR.** The first
twenty-two are all mechanisms that were *supposed* to be looking: an oracle, a floor, a lane, a
probe, a correction. Every remedy this register has produced is therefore some version of *make the
instrument look harder*. **23 is the first entry where the instrument was switched off BY the claim
it was supposed to check.** `M56` did not drift out of reach and no measurement went stale. A patch
wrote `power: n/a` with a reason it had inferred by analogy, and the lane's own rule — do not measure
an opt-out — made that sentence unfalsifiable for a phase and a half. The general form, which is the
one to carry into every future exemption mechanism:

> **A claim that turns off its own instrument is not a weak claim, it is an unfalsifiable one. A
> floored class is re-measured every time the lane runs; an opted-out class is re-measured never — so
> the exemption has to be earned by a fact about the artefact, never granted by a sentence inside
> it.**

`scripts/power-refute.sh` is that rule mechanised: it re-measures every reachability claim it can
judge soundly, **runs the instrument every covered-by claim names**, and where measurement is unsound
requires the exemption to be *earned by the patch's file list* and to carry a written argument saying
what a sound refutation would have to look like. DESIGN-A6 §43.

**And 24 is 23 one level up, which is the pair worth reading together.** 23 is a claim that switched
off the instrument that could refute it. 24 is a claim that switched off the *decision* to point an
instrument at it — and it is harder to see, because excluding a class from a measurement pass looks
like triage rather than like an exemption. The rule that follows is narrow and mechanical:

> **An exclusion from a measurement pass may cite a measurement, or an argument about reachability. It
> may never cite the excluded class's own declaration.**

**And 24 is jointly authored**, which is recorded because the register is a record of how this project
fails rather than of who failed: the exclusion was written by Claude and ratified by Ansh, on the same
sentence, neither of whom asked for the number.

**25 is the first entry where the two halves of ONE TOOL disagreed about whether it worked**, and that
is what earns it a name rather than a line. Every earlier entry is an instrument that was silent when
it should have spoken. This one *spoke correctly* — §42's five re-measurements, `M56`'s refutation, the
whole post-A6 cycle of numbers came through `--measure` and are good — while the same script, entered
through the path that decides pass or fail, was structurally unable to return a verdict.

> **When a mechanism has a MEASURING path and a GATING path, they are two mechanisms. Each needs its
> own induced failure, and every claim about the mechanism has to say which path produced it.**

The failure mode this closes is specific and it is not "the tool is broken": it is that a correct
number and a working gate look identical from outside, so a mechanism can accumulate a phase of
trustworthy measurements while being incapable of failing. **A number is evidence about the path that
produced it and about nothing else** — which is the provenance rule (entry 8) turned inward, on the
instrument rather than on the system.

**13 is the sharpest of the fourteen** and its general form is the one to carry forward:

> A lane that verifies an artifact reproduces must verify it reproduces **something** — not that it
> reproduces identically to a baseline that is also empty.

### What was done about it

The driver now **records** what it made durable — folded forward from the batches it submitted,
promoted when the engine reports that sequence durable, dropped wholesale on a crash. That is what §0
named as the ledger's second stream in the first place: *every write that reached the Engine, and
when it became durable.* Reading the engine back was the shortcut.

The rule the audit produced, which was not obvious before doing it: **a system-reported fact is not
forbidden — it is forbidden as an input to a verdict that can come out green.** `AssertQuiescent`
reads `r.gated`, which is node state, deliberately, and it can only make a run FAIL; a node that lied
about its withheld queue would buy itself nothing. The ledger's durable record can make a run pass,
and an engine that overstated what it held bought a green every time. The direction of the error is
the whole distinction.

So the two kinds of fact have two types. `internal/provenance` gives `Observed[T]` for a boundary
observation and `Reported[T]` for the system's own account, `Ledger.Record*` accepts only the first,
`store.readDurable` returns only the second, and the wiring that caused this is now a **compile
error** — induced by `tools/provcheck/testdata/reported`, which is built and required to fail. Fifth
instance of the house move, after Wall/Mono, the epoch stamp, the D5 conformance check and `markFor`'s
refusal.

It does not make laundering impossible; `Witness(x.Unverified())` compiles, exactly as `Wall(mono)`
does. What the type buys is that laundering must be **written**, and `tools/provcheck` fails the build
when anyone writes it.

And because the durable record is now a *derivation* rather than a reading, it is checked against the
engine's account on every durability completion at which the engine has nothing in flight — the one
moment a read-back honestly is the durable state, and a comparison that can only fail. 36,912
comparisons per 300 seeds, both directions induced (`M26`, `M27`).

### The provenance of every oracle input, since the question has to be answerable

Two primitive sources, and after the fix both are boundary observations:

- **emitted output** — `Ready.Messages` and `Ready.Committed`, taken in `store.drain` as they cross
  the node's boundary, before the transport and before the state machine.
- **durable record** — the batch the driver submitted, promoted on the engine's completion.

| oracle | reads | provenance |
|---|---|---|
| election safety | `ledIn`: term and node of an emitted `MsgApp` | emitted output |
| log matching | `durableLog` per node | durable record |
| leader completeness | `committed` (from `Ready.Committed`), `ledIn`, and the `durableLog` snapshot taken when a node first led | emitted output + durable record |
| state machine safety | `applied` (from `Ready.Committed`) | emitted output |
| persist-before-reply | `sent`, with `durableTerm`, `durableVote`, `durableLast` resolved from the durable record at send time | emitted output + durable record |

`sim.View` exposes only `Now`, `Steps` and `Down`, and every oracle ignores both of `OnStep`'s
parameters. `raftcheck` imports neither `engine`, nor `engine/model`, nor `store`, and never receives
a `*raft.Raft`. None of the other four had the defect; the one that did is the one already fixed, and
the value of having asked is that this table is now checkable rather than asserted.

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
| 6 | persist-before-reply structural inside `raft/`, checked from the ledger | DR-7 gated queue; the oracle is the outside confirmation, induced by `M25` and `M28`, and its ledger input is now typed by provenance (§5c) |
| 7 | every persistent write through the `Engine`; recovery the real path | `store/node.go`; `Restore` runs on every injected restart |
| 8 | elections observed contending | highest term 79, 111,790 started, 48,253 won, 43,442 split votes, 9,641 of 10,000 seeds contended, **0** seeds without a leader |
| 9 | every bug in BUGS.md with its mutant-class answer | 8 entries, 9 mutant classes, 8 of them added because none existed |

Mutant suite alongside: 25 killed, 1 canary alive, 0 mismatched, 0 rotted.

### The scope of each number, stated so no summary can widen it

- **10,000 seeds** is the uninstrumented exit run (`make test`, `make soak`). Every violation,
  inconclusive and census figure above comes from it.
- **200 seeds** is what runs under `-race`. `make race` sets `RACE_SEEDS=200` deliberately: the
  instrumentation costs about twenty times, so the default exit run would put that lane at roughly
  ten hours against three minutes without it. The race lane asks one question — does any
  cross-goroutine interaction reach node state off the mailbox — and `node/`'s own tests plus a few
  hundred simulated seeds answer it. **The race claim covers 200 seeds and not 10,000, and no summary
  of this phase may say otherwise.** Bounded honestly is fine; silently narrower is not.
- **2,000 seeds** is what the `UnknownDominatedPerMille` threshold was measured over, and **300** is
  what most planted-violation detection rates are measured over. Each number carries its own range at
  its own site, because a measurement quoted without its range is the same failure one level up.
