# DESIGN-B4: verification hardening — the differential harness, and what it compares

**Status: PROPOSED. Nothing in B4 is written until this is ruled on.**
**Phase:** B4 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Depends on:** B3, signed 2026-08-27. **Blocks:** B5, I1.
**Carries:** `CARRY-FORWARD.md` **CF-3** (standing) and **CF-4**, which comes due here.

---

## 0. What B3 hands over, and what B4 is actually left to build

**Two of B4's four exit criteria are already met, and saying so is the point of this section** — a
phase that opens by re-listing work already done spends its attention in the wrong place.

| exit criterion | state |
|---|---|
| *"kill-point sweep across every Env call in flush, compaction and manifest swap"* | **DONE at B3.7a.** Three regimes — `default` 305, `flush` 990, `compact` 3545 kill points — 0 violations, and each regime's **power measured**, not merely its coverage |
| *"ASan and UBSan lanes clean"* | **DONE since B1.** `cpp-asan` and `cpp-ubsan` are in `CPP_LANES` and gate every mutant run |
| *"BUGS.md entries exist — zero bugs found means the rig is too weak"* | **standing**, and §8 is about it |
| ***"differential harness against `engine/model` running continuously"*** | **NOT STARTED. This is B4.** |

**So B4 is one thing: the differential rig.** The sweep's remaining growth is incidental to it.

**AND THE SWEEP IS NOT THE DIFFERENTIAL.** The crash rig asks *"did this engine recover what it
promised?"* against **its own submission log**. The differential asks *"do two independent
implementations of one contract agree?"* — a question no single-engine rig can pose, and **the first
question in this project whose answer requires two implementations to exist.**

---

## 1. The problem, stated precisely

`engine/model` is **Go**. The C++ engine is **C++**, and B5 — which builds the cgo binding that lets
Go call it — **has not happened yet.** B4 precedes B5 in the phase order, deliberately: B5's exit
criterion is *"differential tests pass through the cgo path"*, which presupposes a differential rig
that already works.

> **B4 MUST COMPARE TWO ENGINES THAT CANNOT CALL EACH OTHER.**

**And a second constraint, from the frozen interface rather than from the languages.** `A0.5`'s
recovery contract is a **universal quantifier**:

> *For any sequence `w` that has been applied, if `DurableSeq() == w` then a crash recovers exactly
> the state produced by applying batches `1..w` in order, and nothing else. This holds for EVERY
> applied `w`, not only for the most recent one.*

`engine/model` retains **every version between the durable watermark and the visible one** precisely
so that quantifier is checkable — its own header says the two-version assumption *"described exactly
the assumption that made a lagging watermark silently recover more state than the engine had
promised."* **The dangerous direction is a lagging watermark recovering MORE than it promised**, and
a model that rounded up would be compared against an engine that rounded up and agree with it.

---

## 2. B4-D1 — the rig's topology

**Candidates.**

**(a) PORT `engine/model` TO C++, inside `rig/`.** The differential becomes one process.
**Rejected outright**, and not on effort: it compares the C++ engine against **a second C++
implementation written by the same author on the same day**, which is the shared-blind-spot failure
`B3-D2b` spent a phase avoiding. The value of `engine/model` as a reference is that **Track A wrote it
for a different purpose and has been running on it since A0**. A port discards exactly that.

**(b) FILE-MEDIATED, TWO PROCESSES, GO JUDGES.** A C++ driver runs a workload against the C++ engine,
kills it at a chosen point, reopens, and writes **three artifacts**: the submission log, the durable
watermark it recovered at, and the recovered state. A **Go** test reads those artifacts, replays the
submission log into `engine/model`, crashes the model to the same watermark, and compares.

**(c) BRING THE cgo BINDING FORWARD FROM B5.** Go drives the C++ engine directly; one process, no
artifacts.
**Rejected on ordering and on scope.** It inverts a dependency the phase plan chose deliberately, and
it makes every B4 differential failure ambiguous between *the engine*, *the binding* and *the rig* —
three suspects where the point of a differential is to have one. B5's own criterion, *"differential
tests pass through the cgo path"*, is only meaningful **if the path is the new variable.**

**Recommendation: (b).**

**What (b) costs, stated rather than discovered:** an artifact format, which is a fourth thing that
can be wrong; and the rig cannot interleave the two engines operation-by-operation, so a divergence is
localised to a *run* rather than to an *operation*. §5 addresses the second with a bisect step.

**What (b) buys that is not obvious:** the artifacts are **replayable**. A divergence is a file, so it
reproduces at any commit without re-running the schedule that produced it — which is `seeds/`'s
property arriving in Track B, and the thing that makes a B4 finding still debuggable at I1.

---

## 3. B4-D2 — what is compared, and the direction that matters

**The comparison is `recovered_state == model.StateAt(w)`**, where `w` is the watermark the C++ engine
**reported before the kill**, and `StateAt` is the model's state after applying batches `1..w`.

**THREE DIRECTIONS, AND ONLY ONE IS OBVIOUS:**

| | what it means | what it catches |
|---|---|---|
| **recovers LESS than `w`** | acknowledged durable data lost | the obvious one; every crash rig catches it |
| **recovers MORE than `w`** | the engine kept data it never promised | **the dangerous one.** Harmless in isolation, and it means the watermark is not the durability boundary — so a *later* crash at a *different* point loses data the caller was told was safe |
| **recovers a state at no `w'` at all** | a torn or interleaved state | the one a two-element set (`{G_{k-1}, G_k}`) cannot express, and which only a model retaining every intermediate sequence can name |

**THE SECOND ROW IS WHY `engine/model` RETAINS EVERY VERSION**, and it is the reason this comparison
is well-defined at all. An engine that rounds its watermark up is only detectable against a model that
does not.

**And the two-element set from B1's exactness oracle still applies**, unchanged: a `Sync` can complete
on the device with the kill preempting its return, so `R ∈ {G_{k-1}, G_k}` when the kill lands
in-flight. The differential inherits that and does not re-derive it — **the same set, judged against a
richer reference.**

---

## 4. B4-D3 — ruling 4 in a world with two engines

**Ruling 4:** *the rig's verdicts come from its own op log, never from asking the engine what it
believes it holds.* In a differential **both sides are engines**, so the rule needs restating rather
than reciting.

> **THE OP LOG IS THE SHARED INPUT. NEITHER ENGINE IS A WITNESS ABOUT THE OTHER.**

Concretely, and mechanically:

1. **The submission log is authored by the C++ driver before either engine is consulted** — it is the
   sequence of operations the rig *decided* to issue, not a transcript of what an engine did with them.
2. **The watermark `w` comes from `DurableSeq()`, which is the engine reporting about ITSELF** — and
   that is admissible for the same reason B1's oracle admits it: it is **a promise**, and the whole
   comparison is *"did you keep the promise you made?"* An engine that lies about `w` fails against
   the model, because the model is held to the lie.
3. **The Go judge reads artifacts and never links the C++ engine.** It cannot ask it anything.
4. **`engine/model` is not asked what the C++ engine holds** — it is asked what the *submission log*
   produces. Two answers to one question, computed independently.

**The failure this forbids, named so the rule is testable:** a rig that recovered the C++ engine and
then asked *it* which watermark to compare at would agree with itself. **`w` is captured BEFORE the
kill, from the pre-crash process, and is an input to both sides.**

---

## 5. B4-D4 — the workload, determinism, and localising a divergence

**The workload is generated from a SEED and the seed is in the artifact.** Same seed, same operations,
byte for byte — the property `seeds/` rests on, applied to a rig that is not the simulator.

**Randomness comes from the rig's own PCG64**, not from `std::mt19937` and not from `rand()`. `[A1]`
binds `internal/rng` on the Go side for a reason that applies here verbatim: *a silent change to a
generator leaves every corpus entry self-consistent and different.* The C++ side needs the same
guarantee and gets it from the same algorithm with pinned test vectors.

**Operation mix, and why each is present rather than "for coverage":**

| operation | why the differential needs it |
|---|---|
| `Set` / `Delete` | the base case, and the only one B1 had |
| `DeleteRange`, bounded and unbounded | **`[A3]`'s reason for freezing it.** The two engines implement it by *entirely different mechanisms* — the model natively, the C++ engine by range tombstones — so agreement here is the strongest evidence the rig can produce |
| `Sync` at varying cadence | the watermark's position is the whole subject |
| iteration, forward and backward, bounded and unbounded | the read path B3 changed most, and the one place a range tombstone's effect is visible across many keys at once |
| snapshot, held across writes | `S`'s effect on what survives, which B3-D1 made a correctness matter |

**LOCALISING A DIVERGENCE — the cost (b) pays, and how it is paid.** The rig cannot compare
operation-by-operation, so a mismatch names a run. **The bisect is on the SUBMISSION LOG, not on the
schedule:** replay the first half of the log into both engines, compare, recurse. Because the log is
an artifact and both engines are deterministic, **the bisect is a function of the file** and needs no
re-run of the original kill schedule.

---

## 6. CF-4, which comes due here

**Two conditions, three instances behind it, one of which paid before it was scheduled.**

**Condition 1 (`GF-15`) — of every fact one rule DERIVES, which other rule DEPENDS on it?** Asked
once across the frozen interface as a whole, because per-decision it has already been asked and that
is the reading that misses it. **Where to look, from B3's map:** the manifest's per-table numbers
re-derived from table bytes; the durable floor from `largest_seq`; recovery's skip point from that
same maximum; `bottom_most` from range disjointness; `S` from the snapshot registry; `unbounded_end`
and `largest_is_exclusive` from the range block.

**Condition 2 (`GF-18`) — of each retired shim, what did it let us avoid deciding?** The question is
the work; listing the shims is not the sweep. **Known members:** `DeleteRange`'s expansion (retired
B3.5, produced `B3-Q4`); `Apply`'s expansion; `table.h`'s residency-as-correctness (retired B3.6);
B2's iterate-and-point-delete.

**Why B4 and not earlier:** the differential rig is the first thing that exercises the whole surface
**against an independent implementation**, so a derived fact two rules disagree about has somewhere to
show up rather than being a reading exercise.

---

## 7. GF-26 applies to every regime B4 adds

**No regime lands until at least one class is floored against it.** B4 will want regimes the sweep
does not have — a differential regime, and plausibly a large-key or high-`DeleteRange` mix.

> **A REGIME WITH NO FLOOR IS A GREEN WHOSE SENSITIVITY IS UNKNOWN**, and a large run count with zero
> divergences reads as thorough while proving only that the lane ran.

The cost is one mutant per regime. **`GF-27` applies beside it:** prefer a new regime to a widened
one, because widening dilutes every floor already measured against the old denominator.

---

## 8. B4 EXPECTS DEFECTS, AND SAYS SO IN ADVANCE

**Every rig in this project has found real defects on its first outing.** `ReplayMachine` found
`BUG-018`; the crash rig found `BUG-001` and `BUG-002` on its first run; B3's adjudicator corrected
`keep(k)` before any compaction existed.

**This is the first rig that compares two independent implementations of one contract**, which is a
strictly stronger question than any asked so far.

> **IF IT FINDS NOTHING, THAT IS A FINDING ABOUT THE RIG RATHER THAN A RESULT ABOUT THE ENGINE, AND IT
> IS TREATED AS ONE.** The response is to strengthen the rig — a wider operation mix, a longer run, a
> harsher kill schedule — **not to record a clean result.**

**Stated now, before the first run**, for §8.1's reason: an expectation fixed in advance cannot be
adjusted to fit whatever arrives. The phase text already says it — *"BUGS.md entries exist (same rule:
zero bugs found means the rig is too weak)"* — and this section is that rule with its consequence
attached.

---

## 9. Landing sequence

| step | lands | why here |
|---|---|---|
| **B4.0** | the artifact format, and its classifier, **from hand-built bytes** | B2-D6's ordering, sixth use. The format is what both sides agree on; a format defined by its writer is checked by nothing |
| **B4.1** | the C++ driver: seeded workload, kill, reopen, write artifacts | needs the format |
| **B4.2** | the Go judge: replay into `engine/model`, compare three directions | needs artifacts to read |
| **B4.3** | the bisect, on the submission log | needs a divergence to localise, or a synthetic one |
| **B4.4** | CF-4's two-condition sweep | needs the rig, per §6 |
| **B4.5** | continuous running, its regime floored, BUGS entries for what it found | `GF-26`; power measured last |

---

## 10. Open questions for Ansh

**B4-Q1 — WHICH `engine/model` DOES THE JUDGE COMPARE AGAINST, AND WHO OWNS IT?**

This worktree (`rift-b`) has its own `engine/model` because it is a worktree of the same repository —
but it is **a different branch**, and Track A owns that file. Three readings:

- **(a) Compare against `rift-b`'s copy.** Simple, and it means B4 validates the C++ engine against
  **a possibly stale model**. A divergence might be Track A having fixed something I do not have.
- **(b) Pin the model by commit, recorded in the artifact.** The judge states which `engine/model` it
  ran against; a divergence names the pair. Costs a field and makes staleness *visible* rather than
  *absent*.
- **(c) Require a merge from Track A before each differential run.** Correct and coupling: it makes
  Track B's lane depend on Track A's branch state, which the two-worktree structure exists to avoid.

**My reading is (b)**, and I flag it rather than proceed because it touches the worktree boundary,
which is yours. **(b) is also what makes a B4 finding survivable**: a divergence recorded against a
named model commit is still reproducible after Track A moves on.

**B4-Q2 — WHERE DOES THE GO JUDGE LIVE?**

It is Go code that imports `engine/model`, and Track A owns the Go tree. A new package
(`engine-cpp/differential/` or `tools/differential/`) is Track B's own file and collides with nothing
today — but it is **Go code on the `rift-b` branch**, and the merge is yours to sequence. I will not
write into any existing Go package.

**B4-Q3 — IS THE ARTIFACT FORMAT A FROZEN SURFACE?**

`seeds/`'s promise is that *"`simctl replay <seed>` reproduces any historical bug at the commit that
contained it."* If B4's artifacts are to have that property, the format is frozen the day the first
one is committed to `seeds/`. **I recommend freezing it at B4.0 and treating it like the SSTable
format** — classifier first, from hand-built bytes, every refusal induced. If instead these artifacts
are scratch, the format can stay loose and the corpus promise does not extend to them. **This changes
what B4.0 costs**, so it is a decision rather than a preference.

---

## 11. Decision summary

| id | decision | recommendation |
|---|---|---|
| **B4-D1** | rig topology | **(b) file-mediated, two processes, Go judges.** (a) compares against a same-author reimplementation; (c) inverts B5's dependency and gives every failure three suspects |
| **B4-D2** | what is compared | `recovered == model.StateAt(w)`, **three directions**, with recovering MORE than promised named as the dangerous one |
| **B4-D3** | ruling 4 with two engines | the op log is the shared input; `w` is captured **before** the kill; the judge never links the engine |
| **B4-D4** | workload and localisation | seeded from a project-owned PCG64, `DeleteRange` central because the two engines implement it by different mechanisms; bisect on the **log**, not the schedule |
| **CF-4** | comes due | two conditions, swept once over the whole surface |
| **§8** | expectation | **defects are expected; finding none is a finding about the rig** |

| id | question | my reading |
|---|---|---|
| **B4-Q1** | which `engine/model` | **(b) pin by commit in the artifact** — flagged, touches the worktree boundary |
| **B4-Q2** | where the Go judge lives | a new Track B package; the merge sequencing is yours |
| **B4-Q3** | is the artifact format frozen | **freeze at B4.0**, classifier-first — but it changes B4.0's cost, so it is yours to rule |

**Nothing in B4 is written until this is ruled on.**
