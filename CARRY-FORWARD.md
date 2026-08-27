# CARRY-FORWARD.md

Obligations this phase created for a **later** one, each with the phase that
discharges it and the check that discharges it. Not a wish list and not a notes
file: every entry names something that must be *done*, in a *named phase*, with
a *named measurement*.

**Why this file exists.** GF-5 in BUGS.md: an accidental defence is worse than a
missing one, because it makes a real gap measure as covered and then removes
itself on a schedule nobody is tracking. A gap with a date is only safe if
somebody is holding the date. This file holds the dates.

**Rules.**

1. Every entry names the **phase that discharges it**. "Later" is not a phase.
2. Every entry names the **check** — a lane, a test, or a measurement that is
   re-run in that phase, and what its result must be compared against.
3. An entry is removed only when its check has actually been run in the phase
   that owns it, and the result recorded here in the closing note.
4. **A number that moves when the obligation comes due is the obligation
   expiring on time, not a regression.** Anyone reading a moved number without
   this file will misdiagnose it, which is the whole reason for the file.

---

## CF-1 — BM2's detection rate must be re-measured the cycle the flush lands

**Status: DISCHARGED WITH REFUTATION, B2.7, 2026-08-25.** Ansh's ruling: the deviation from this
entry's fail-the-campaign instruction is approved, and the entry is AMENDED rather than closed,
because the prediction it rested on was wrong in a way worth keeping.

| field | value |
|---|---|
| **Raised** | B1.9b, 2026-08-25 |
| **Raised by** | HARNESS-010 |
| **Discharged by** | **B2**, in the same cycle as the memtable flush |
| **Check** | `make cpp-campaign`, the `BM2-accept-torn-tail` row |
| **Compare against** | 194 per mille, first detection at kill point 14 (measured at `f257e29`, 175 kill points) |

**The obligation.** B1's recovery can apply BATCH records past the last
`GROUP_END`. In B1 those records land in the memtable at sequences **above** the
recovered watermark, where every read's snapshot hides them — present and
unreadable. That is an **accidental** defence: the read path was not defending
anything, it simply had no way to show the damage. The sweep's post-reopen
continuation is what makes the damage visible today, and BM2's floor rests on
it.

**Why B2 is the date.** The flush writes the memtable out. Uncommitted records
that were merely hidden become **durable, visible and permanent** in an SSTable.
The accident ends there.

**What must be true after B2.**

- BM2's measured rate is re-taken and recorded below. It is expected to **rise**,
  because the flush gives the defect a second and more permanent way to show.
- If it **falls**, that is not the accident expiring — that is a regression, and
  the campaign must fail rather than the floor be lowered.
- The floor is re-derived from the new measurement in the same commit, and the
  reasoning column records that it was re-derived and why.

**Closing note (B2), 2026-08-25 — DISCHARGED WITH REFUTATION.**

Re-measured under B2's shape, in both regimes, at B2.7:

| regime | measured | vs `f257e29` |
|---|---|---|
| **default** (no flush) | **34 / 300 = 113 per mille**, first at kill point **39** | count **unchanged at 34**; rate fell; first detection +25 |
| **flush** | **36 / 985 = 36 per mille**, first at kill point **39** | count **34 → 36**; rate fell |

### What was predicted

That the rate would **rise**, because the flush writes the memtable out and uncommitted records
recovery had applied — merely hidden under the snapshot in B1 — would become durable, visible and
permanent in an SSTable. *"The flush gives the defect a second and more permanent way to show."*

### What was measured

The count did not move. Not under a `Sync` after the continuation write; not with filler enough to
guarantee the flush regime actually flushes; and **not under a second kill and a second recovery**,
which is the mechanism this entry describes and which the sweep did not previously perform. All three
extensions were made specifically to look for the predicted path. **The count is 36 against 34, and
the two extra detections are the longer workload's extra torn-sync points — not the flush.**

### Why the predicted mechanism cannot exist

**A torn tail leaves its first uncommitted batch at exactly watermark + 1**, and watermark + 1 is
exactly the sequence the post-reopen continuation write takes. So every kill point at which the
flush could expose the defect is a kill point the continuation *already* exposes. The second path is
not weak, or hard to reach, or unlucky — it is **coextensive with the first**. A flush cannot add a
detection where one already exists.

### What survives and what does not

**GF-5's claim survives in full.** The accidental defence was real — BM2 measured ZERO before the
continuation existed — and it did expire on schedule. What did not survive is **the specific
consequence attached to it**: that its expiry would be *visible as a rise*. The remedy (the
continuation) was already complete, so the expiry has no observable consequence in this sweep.

**A measurement refuting a predicted mechanism is worth more than the confirmation would have been**,
and it is why this entry is amended rather than deleted: an entry that recorded only "re-measured, no
change" would have left the theory intact and unexamined.

### The instrument that replaced the failing one

`FLOORS.txt` gained a **detection-COUNT floor** as a third bound. BM2's is **30**, against 34 and 36.
See the general form below, which this is the second instance of.

### The second recovery stays

The sweep keeps the second kill and second recovery it grew while hunting the predicted path. It is a
real second detection path; that it adds nothing is a fact about **this defect**, not about the lane,
and a later defect that the continuation cannot reach will find it already there.

---

## The general rule this taught, recorded because it is the second instance

**A RATE IS A RATIO AND BOTH TERMS MOVE.** A floor on a rate alone cannot tell a loss of detection
power from a denominator that grew into territory where the class was never detectable. B2 grew the
default regime from 175 kill points to 300 by adding a manifest, and every floor in this file fell
while not one detection was lost.

So a rate floor needs **one of two things beside it**:

- **a floor on the COUNT**, which is immune to the denominator and blind to per-point dilution — the
  rate is the reverse, which is why it is a third bound and not a replacement; or
- **a REGIME LABEL** that keeps incomparable denominators from being compared at all.

**Track A learned the regime half at A6. This is the count half.** Both halves are now in
`FLOORS.txt`: a `regime` column and a detection-count column.

---

## CF-2 — every mutant class without a standing measurement

| field | value |
|---|---|
| **Raised** | B2.7, 2026-08-25 |
| **Raised by** | Ansh, on B2's close |
| **Discharged by** | **B3** |
| **Check** | `comm -23` of the patch basenames in `engine-cpp/mutants/` against the class column of `engine-cpp/FLOORS.txt` |
| **Compare against** | **47 of 98 classes unlisted at B2's close.** The check must print zero. |

**The obligation.** `FLOORS.txt` says in its own header: *"An exempt class is still listed: a class
missing from this file is a class with no standing measurement, which is how a bug class drifts back
into being uncatchable one flaw at a time."* At B2's close **47 of 98 classes are missing from it.**
All 47 predate B2; B2's own 40 are all listed.

**What is and is not true of them.** Every one of the 47 carries a `covering-lane` in its patch
header and is killed by `make cpp-mutants`, so none is uncaught. What none of them has is a **standing
measurement** or a **split label naming the instrument** — the `covered-by: <test>` that says *which
assertion* catches it rather than *which lane*. By lane: 28 `cpp-test`, 12 `cpp-scan`, 2 `cpp-tsan`,
2 `cpp-ci`, 1 `cpp-asan`, 1 `cpp-ubsan`, 1 `cpp-campaign`.

**The cost of leaving it.** This is the M56 situation with the labels **absent rather than wrong**,
which is the cheaper half to fix and the easier half to forget. A class killed by a lane but named by
nothing is a class whose covering assertion can be deleted without any lane going red — the mutant
still dies, on some *other* assertion in the same lane, and the class quietly stops testing what it
was written to test.

**Why B3 and not B2.** Ansh's ruling: *do not fold it into B2's close.* It is 47 entries of research
into which specific test catches each class, and folding it in would mix a bookkeeping sweep with a
phase whose evidence is otherwise self-contained.

**Closing note (B3.1), 2026-08-25 — DISCHARGED, AND CONVERTED INTO A LANE.**

All **47** classes now carry a `covered-by:` naming the specific assertion. The check prints **0**,
and it is no longer a check somebody has to remember to run: **`cpp-scan` part 6 fails when any class
in `engine-cpp/mutants/` has no entry in `FLOORS.txt`.** Induced by removing one entry; restored.

**The labels were DETERMINED, not inferred**, which is what made this research rather than
transcription: each patch was applied, the tree built, and the failing assertion read. Inferring the
label from what a patch says it *blinds* would have produced plausible entries that name the wrong
test — and a wrong `covered-by` is worse than none, because it is the one place a future reader
checks before deleting an assertion.

**THREE CLASSES HAVE NO FAILING TEST AT ALL, AND THEY CARRY A DIFFERENT LABEL FOR IT.**
`BM16-mutex-across-env`, `BM9-apply-does-io` and `REGISTRY-lying-sync-not-suspending` are killed by a
**guard firing** rather than by an assertion failing — the mutex-depth guard and the
exactness-suspension assertion — so the suite reports **no failing test** while the process dies
mid-run.

> **"No failing test" and "killed by a guard" are OPPOSITE CONCLUSIONS FROM IDENTICAL OUTPUT.**

Read as the first, all three are **survivals** — three false findings, in the one file whose entries
a reader trusts before deleting an assertion. So the distinction is a **label**, not a sentence in a
reasoning column: `killed-by-guard: <guard>` rather than `covered-by: <test>`, and `cpp-campaign`
parses both and prints them differently.

They were read correctly only because `RIFT_CHECK` prints a partial-run marker — **landed hours
earlier, for exactly this shape, and it paid for itself the same day.**

**One entry moved rather than being retired**: `ORACLE-includes-engine` was written against the old
part 2b, which B3-Q1 **replaced** — so it now names the ARTIFACTS allow-list. A mutant retired with
its check is a class that stops being watched.

---

## CF-3 — the loop-termination properties must hold under compaction's merge

| field | value |
|---|---|
| **Raised** | B2.7, 2026-08-25 |
| **Raised by** | Ansh, on B2's close, from `HARNESS-013` |
| **Discharged by** | **B3** |
| **Check** | every loop in the compaction merge that terminates by "the cursor strictly moves" carries a `RIFT_CHECK` on that movement, not a comment |
| **Compare against** | `IterImpl::AdvanceToVisible` and `RetreatToVisible`, which now assert it |

**The obligation.** B2 has two loops whose termination rests on the comparator being the order it
claims to be, and both now assert their own progress rather than commenting it — because *a
termination argument that assumes the thing being mutated is not a termination argument*. **B3 is
where iterators get their real workout**: compaction merges N sorted inputs and its inner loops have
exactly this shape.

**What must be true at B3's close.** Every such loop asserts the movement it depends on, using a
property that does **not** depend on the comparator under test — as B2's do, by comparing user keys
bytewise, which is a fact about the merged order rather than about the tag half of it.

**The cost of leaving it.** `HARNESS-013` measured it: eleven and a half hours of a lane waiting on a
spin, and a stalled log that read exactly like a slow one. The watchdog now bounds that cost, but a
watchdog converts an infinite hang into a twenty-minute one — **it does not convert it into a
diagnosis.** The assertion does.

**Closing note (B3.1), 2026-08-25 — DISCHARGED, AND CONVERTED INTO A LANE.**

All **47** classes now carry a `covered-by:` naming the specific assertion. The check prints **0**,
and it is no longer a check somebody has to remember to run: **`cpp-scan` part 6 fails when any class
in `engine-cpp/mutants/` has no entry in `FLOORS.txt`.** Induced by removing one entry; restored.

**The labels were DETERMINED, not inferred**, which is what made this research rather than
transcription: each patch was applied, the tree built, and the failing assertion read. Inferring the
label from what a patch says it *blinds* would have produced plausible entries that name the wrong
test — and a wrong `covered-by` is worse than none, because it is the one place a future reader
checks before deleting an assertion.

**THREE CLASSES HAVE NO FAILING TEST AT ALL, AND THEY CARRY A DIFFERENT LABEL FOR IT.**
`BM16-mutex-across-env`, `BM9-apply-does-io` and `REGISTRY-lying-sync-not-suspending` are killed by a
**guard firing** rather than by an assertion failing — the mutex-depth guard and the
exactness-suspension assertion — so the suite reports **no failing test** while the process dies
mid-run.

> **"No failing test" and "killed by a guard" are OPPOSITE CONCLUSIONS FROM IDENTICAL OUTPUT.**

Read as the first, all three are **survivals** — three false findings, in the one file whose entries
a reader trusts before deleting an assertion. So the distinction is a **label**, not a sentence in a
reasoning column: `killed-by-guard: <guard>` rather than `covered-by: <test>`, and `cpp-campaign`
parses both and prints them differently.

They were read correctly only because `RIFT_CHECK` prints a partial-run marker — **landed hours
earlier, for exactly this shape, and it paid for itself the same day.**

**One entry moved rather than being retired**: `ORACLE-includes-engine` was written against the old
part 2b, which B3-Q1 **replaced** — so it now names the ARTIFACTS allow-list. A mutant retired with
its check is a class that stops being watched.

**CLOSING NOTE (B3.4), 2026-08-25 — THE MERGE ITSELF, WHICH IS WHAT CF-3 WAS ACTUALLY RAISED FOR.**

The compaction merge is the loop this obligation was aimed at, and it is the first one where **no
cursor would do**: an iteration may emit an entry, drop one, or skip one, so *"the output key strictly
advances"* is false for a **correct** merge and *"an input cursor strictly advances"* is false
whenever a version is dropped.

> **THE HONEST QUANTITY IS `inputs_consumed`**, which rises by exactly one per iteration whatever the
> iteration decides, **bounded by the sum of the input tables' entry counts as `ValidateTable`
> counted them** — a bound that cannot be raised without contradicting the classifier (GF-13).

**Both halves were induced.** `BM78` makes the merge fail to advance past a dropped entry; without
the bound it hangs, with it the process stops at the mistake. `BM81` takes the wrong half of the L1
binary search: **the progress assertion stays true**, the loop terminates cleanly, and it lands on
the wrong file — the third demonstration in this engine, after `BM68` and `BM69`, that

> **A TERMINATION ASSERTION IS NOT A CORRECTNESS ASSERTION.**

Every loop added by B3.3b and B3.4 carries both instruments, and `FLOORS.txt` distinguishes them by
label: `covers-correctness:` for what catches a wrong traversal, `covered-by:` for everything else.




---

**CLOSING NOTE (B4.5), 2026-08-27 — EVERY LOOP B4 ADDED, IN BOTH LANGUAGES.**

The rule is about **what a loop terminates on**, not about C++, so B4's Go loops are audited on the
same terms. **Thirty-four loops across four files**, and all but four are bounded `for` loops over a
fixed container — a shape whose progress quantity is the container's own length and which needs no
note. The four that decide something:

| loop | progress quantity | independent of what it might be wrong about? | correctness instrument |
|---|---|---|---|
| `ParseDiffArtifact`'s **section walk** | `at`, advancing by `5 + length` where `length` was bounds-checked against the footer **before** it was used | **yes** — the section KIND cannot affect it, and an unknown kind is refused rather than skipped | the 21-fixture corpus, both decoders |
| `Issue`'s **batch grouping** | the outer index `i`, set to `j - 1` and then incremented, where `j > i` always | **yes** — `j` advances at least once per batch because the first op is unconditionally included | `DiffDriver.EveryArtifactItWritesIsOneTheClassifierAccepts`, and `cpp-diff` itself |
| `Judge`'s **batch grouping** | the same shape in Go, `i = j - 1` with `j > i` | **yes** | `TestKilledRunsAgree`; a mis-grouping makes the model disagree with the engine everywhere |
| **`Bisect`** | `hi - lo`, strictly shrinking whichever branch is taken | **yes** — *the judge's verdict decides the direction; it does not decide that the interval shrinks* | `TestBisectNamesTheOperation` |

**THE LAST ROW IS THE FAMILIAR ONE AND IT IS THE THIRD TIME.** `ConcatIter::Seek`, `L1FileFor` and now
`Bisect` are all binary searches whose termination rests on the interval and whose **correctness** rests
on the comparator — and all three carry the same sentence, because it is the same fact.

**`Author`'s batch counter is worth its own line**, because it is the one place a loop's bound depends
on a value the loop sets: `left_in_batch` is decremented each iteration and reset from the RNG when it
reaches zero. **The outer loop is bounded by `count` regardless**, so `left_in_batch` cannot extend it
— it only decides where batches begin. Stated because a reader meeting a counter reset inside a loop
should be told it is not the loop's bound.

**Four for four, and it is only evidence because the question was asked mechanically** — a grep over
every `for` and `while` in the files B4 added, in both languages.

---

**CLOSING NOTE (B3.7), 2026-08-27 — EVERY LOOP B3 ADDED, AUDITED MECHANICALLY.**

**The audit is a grep, not a recollection**, because `GF-23` says a remedy that rests on remembering
has the defect's own shape. Every `for`/`while` in the files B3 added or changed was enumerated, and
each of the **eight new loops** carries its progress quantity at the code:

| loop | progress quantity | independent of what it might be wrong about? | correctness instrument |
|---|---|---|---|
| `RunCompaction`'s merge | `inputs_consumed`, against a **derived** bound (`Σ ValidateTable` entries) | **yes** — a count of entries taken, not a cursor, not a key | `AdjudicateMerge` (order, values) + `AdjudicateDrops` (what survived) |
| clause 1's observable walk | position in `observable`, **fixed before the merge and never modified inside it** | **yes** — independent of tombstones, drop rules and the comparator | `Compaction.ASnapshotBelowARangeTombstoneKeepsTheVersionItHides` |
| clause 2's tombstone verdict | position in `tombstones`, a fixed vector | **yes** | `RangeDelete.ATombstoneWithNothingLeftToHideIsDroppedByCompaction` |
| `L1FileFor`'s binary search | `hi - lo`, strictly shrinking | **yes** — *"the comparator decides the direction; it does not decide that the interval shrinks"* | `Compact.EverythingIsStillThereAfterAReopen` (`BM81` proves the separation) |
| `L1FileFor`'s exclusive-bound skip | `lo`, an integer index bounded by `l1.size()` | **yes** — not derived from the comparator nor from the exclusive flag | `RangeDelete.ARangeSpanningOutputFilesIsSplitAndEveryPieceApplies` |
| `Table::NewestCovering` | position in `tombstones_`, fixed at Open | **yes** | `RangeDelete.ARangeSurvivesTheCompactionThatMovesItToLevelOne` |
| `MemTable::NewestCoveringLocked` | position in `ranges_`, under the lock | **yes** | `RangeDelete.TheSameRuleHoldsAtEveryPlaceItIsWritten` |
| `MeasureAmplification`'s fill | `live_bytes`, rising by a **positive constant** per iteration | **yes** — counted by the harness from what it SUBMITTED, so an engine bug cannot stall it | `AmpInstrument.*` |

**`ConcatIter`'s four loops were audited at B3.3a** and are unchanged: `file_` for `Next`/`Prev`,
`hi - lo` for `Seek`, with the note that *correctness depends on the comparator here; termination does
not.*

**THE ANSWER IS EIGHT FOR EIGHT, AND THAT IS ONLY EVIDENCE BECAUSE THE QUESTION WAS ASKED
MECHANICALLY.** An empty result from a search nobody ran is indistinguishable from an empty result
from a search that found nothing — which is `GF-1`'s shape applied to an audit rather than to a lane.

**Two of the eight needed the answer written down rather than merely being true**, and those are the
ones the audit earned: the exclusive-bound skip (added at B3.5e, no CF-3 note) and the amplification
fill (added at B3.7b, in harness code where the rule applies just as much). **Both terminated
correctly and neither said why**, which is exactly the state CF-3 exists to prevent — the next person
to change them would have had nothing to preserve.

**CF-3 REMAINS OPEN**, deliberately. It is not a B3 obligation that closes at B3's end; it binds every
loop this engine adds, and B4's differential rig and B5's cgo poller will both add some.

---

## CF-4 — ask GF-15's question once across the frozen interface, at B4

| field | value |
|---|---|
| **Raised** | B3.4, 2026-08-26 |
| **Raised by** | Ansh, ratifying B3.4, on `GF-15` |
| **Discharged by** | **B4** |
| **Check** | one pass over the frozen `Engine` surface asking, of every fact one rule DERIVES, which other rule DEPENDS on that fact |
| **Compare against** | `B3-D7b`, the instance that raised it |

**The obligation.** `GF-15` says *a rule derived from one contract is not permission under the
others*. B3.4 found one instance the hard way: `B3-D1` permits dropping the highest-sequenced entry,
and `D7`'s forward binding makes that entry **the only proof of the durable floor** — the manifest
may not record a durable sequence, so `Open` must re-derive the floor from table bytes. Dropping it
preserves every answer and breaks a promise.

> **A CROSS-CONTRACT INTERACTION IS INVISIBLE IN EITHER CONTRACT'S OWN STATEMENT.** That is what
> makes it general, and what makes it undiscoverable by reading one document carefully.

**Why once, and why at B4.** Asked per-decision it has already been asked — each decision reasons
about its own contract, which is exactly the reading that misses this. It needs a pass over the
**whole surface at once**, and B4 is the first point where the whole surface exists and is being
exercised against `engine/model`, so a derived fact two rules disagree about has somewhere to show up.

**Where to look, from B3's map.** Every place a fact is re-derived rather than recorded: the
manifest's per-table numbers re-derived from table bytes at `Open`; the durable floor from
`largest_seq`; recovery's skip point from that same maximum; `bottom_most` from range disjointness;
`S` from the snapshot registry. Each is a rule producing a fact, and each has at least one other rule
reading it.

**The cost of leaving it.** `BM77` is the shape: **the mutant is the faithful implementation of the
rule it is derived from.** Nothing in a careful reading of that rule flags it, and no reviewer
checking the rule's own statement would object.

---

**DISCHARGED, B4.4, 2026-08-27 — `docs/CF4-SWEEP.md`.**

Both conditions swept over the whole surface. **Condition 1** tabulates ten derived facts with who
depends on each: **nine agree, one did not** — `BUG-006`, and the rig found it before the table was
written. **Condition 2** asks *what did this let us avoid deciding* of each retired shim, and its
answers are the phase's: `DeleteRange`'s expansion produced `B3-Q4`, `Apply`'s expansion produced
`GF-20`, `table.h`'s residency produced `CF-5`, and B2's iterate-and-point-delete is **what the
differential is for** — two engines implementing one operation by entirely different mechanisms,
agreeing across 96 runs.

**What it did not cover is written down too:** facts derived across the cgo boundary (B5's), facts the
Go side derives (Track A's surface), and the differential's own derived facts.

---

**SECOND SWEEP CONDITION, ADDED 2026-08-26 AFTER `B3-Q4`: EVERY PLACE B2 OR B3 REMOVED AN EXPANSION.**

`GF-18`: *a shim that makes a case unnecessary makes the gap it hides invisible.* B2's
iterate-and-point-delete resolved `Bound::Unbounded()` against the live set, so no format ever had to
represent `[start, ∞)` — and the range-tombstone format frozen at B3.2 could not. Two frozen
artifacts, each internally consistent, never checked against each other. **Neither was wrong; they
were unjoined.**

So the B4 pass has two questions, not one:

1. *(GF-15)* of every fact one rule **derives**, which other rule **depends** on it?
2. *(GF-18)* **of each retired shim, asked as the sweep step and not as a note beside it:**

   > **WHAT DID THIS LET US AVOID DECIDING?**

   and then: do the contracts it stood between agree now that it is gone? The question is the work.
   A shim exists because some case was awkward; the awkwardness is where two contracts meet; the shim
   is what kept them from having to agree. Listing the shims is not the sweep — **asking that
   question of each one is.**

**Known members of (2) so far:** `DeleteRange`'s expansion (retired at B3.5, and it produced
`B3-Q4`); `Apply`'s expansion and `table.h`'s whole-file residency (both retired at B3.5c–d, and
both stood between the Env-call contract and the read path); B2's `DeleteRange` implemented
internally as iterate-and-point-delete per `[A3]`, replaced at B3.

**CF-4 PAID BEFORE IT CAME DUE, AND THE COUNTERFACTUAL IS WHAT MAKES THAT MEASURABLE.**

| when it was found | what it cost |
|---|---|
| **B3.5, by asking the question at retirement** | one design decision (`B3-Q4`), four mutants, and an afternoon |
| **B4, by the differential rig** *(the counterfactual)* | `engine/model` disagrees on a clear-everything workload. The format is already frozen and **already has tables written to it** — every corpus table, every sweep image, every fixture. The fix is a format change plus a rewrite of everything written to the old one, and it lands in the phase whose job is proving the two engines agree |

**Early payment is measurable only against the alternative**, which is why the second row is written
down rather than left as "it would have been worse". One instance is not a trend; it is one data
point, and it is recorded as one.


---

## CF-5 — `table.h`'s residency, and what still depends on it

| field | value |
|---|---|
| **Raised** | B3.5c, 2026-08-26 |
| **Raised by** | Claude, retiring `Apply`'s expansion |
| **Discharged by** | **B3.6** |
| **Check** | a snapshot or iterator taken before a compaction still reads correctly when the input files are **deleted from the Env**, with the table's bytes NOT resident |
| **Compare against** | `db.cc` install step 5, and `table.h`'s header note |

**The obligation.** B2-D7 made whole-file residency a **requirement**: `DeleteRange` expanded at
`Apply`, `Apply` makes no Env call, and the expansion had to read every live table. B3.5 retired the
expansion, so **`Apply` no longer needs it** — and the note was updated to say so.

**What was NOT retired, and is written down rather than assumed:**

> A snapshot or iterator outliving a compaction reads through tables whose **files have already been
> deleted** (install step 5). That is correct **only because the bytes are in memory.**

**"No longer required by X" is not "no longer required."** The residency is now a *lifetime* property
rather than a correctness requirement of `Apply`, and B3.6 is where it becomes a reference count on
the file instead.

**The cost of leaving it.** It is not currently a bug — it is a correctness argument resting on an
implementation detail, which is exactly `GF-20`'s shape: *a premise that moves*. The premise here
moves the day anything reads a block on demand, which is the first thing B5's cache work would do.

---

**DISCHARGED, B3.6, 2026-08-27.** The argument is gone rather than strengthened, per `GF-20`.

`db.cc` keeps the **file** alive until the last reader drops its `shared_ptr`: a retired table goes on
an `obsolete_` list and is deleted when nothing but that list holds it. So a snapshot reading through
a compacted-away table is correct **because the file is there**, not because the bytes happen to be
resident. `table.h`'s note is updated: **residency is now a performance property and no correctness
claim rests on it.**

**The count is `shared_ptr::use_count()` and not a second counter**, because every reader already
holds one for exactly as long as it may read. A separate refcount would be a second source of truth
about one fact.

**IT TOOK TWO ATTEMPTS AND THE FIRST IS THE LESSON.** The first version compared `use_count()` at the
retirement site against a threshold worked out by reasoning — *"`t` is one reference and the caller's
vector is another, so above two is a reader"* — and **double-counted the same reference**, because
`t` is a reference *to* the vector's element. A snapshot's file was deleted underneath it, and the
test caught it because it asserts about **the file** rather than about the answer.

> **A REFERENCE-COUNT THRESHOLD DERIVED BY REASONING ABOUT WHICH LOCALS HAPPEN TO EXIST IS A NUMBER
> THAT CHANGES WHEN SOMEONE ADDS A VARIABLE.**

Restructured so there is nothing to derive: **one place takes a count, and one holder to subtract.**
`use_count() == 1` on the `obsolete_` list means the list is the only holder. Adding a local anywhere
else cannot move it.

**What made the defect visible was the shape of the assertion.** A test that only read through the
snapshot would have passed either way — the bytes are resident, so the answer is right. The gate is
`FileLifetime.AnInputFileOutlivesTheCompactionWhileASnapshotHoldsIt`, and it asserts the **file is on
disk**. Reading through the snapshot is the *other* half.

**`NewIter`'s named gap from B3.4 is closed here too**: an iterator holds its `Version`, so it holds
its files, and the count sees it without a special case.
