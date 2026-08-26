# DESIGN-B3 — compaction, iterators, and the first thing this engine deletes

**Status: REVISION 6. B3-Q1 RULED 2026-08-25; B3-Q3 ratified by the implementation instruction;
B3-Q2 proceeding on my stated reading and flagged as such in §11.**

Phase B3 per CLAUDE.md: *leveled compaction with scoring; merged iterators; engine snapshots pinning
versions. Real range tombstones land here, replacing B2's iterate-and-point-delete `DeleteRange`, and
must be in place before any I2 benchmark number is taken `[A3]`. Exit: compaction correctness under
concurrent reads; space and read amplification measured and recorded.*

Amendment A6 governs the policy: **the simplest correct thing wins v1**, multi-level leveled
compaction beyond the v1 policy is STRETCH, and the threshold that reopens it is *a number, not a
mood*.

---

## 0. What B2 hands over, and what it hands over temporary

**Sound and reusable.** The SSTable format and its classifier, induced from fixture bytes before any
writer existed. The bloom filter, with no-false-negatives asserted exactly and the rate as a measured
ceiling. The WAL-framed manifest, with `CURRENT` installed by rename plus a directory sync. The flush
on B2-D5's ordering. `MergedIter`, bidirectional, with direction switches re-seeking every source.
The kill-point sweep in two regimes. `Table`, `TableBuilder`, `internal_key.h`.

**Temporary, and B3 is the scheduled end of each:**

| what | why it expires here |
|---|---|
| `DeleteRange` as iterate-and-point-delete | `[A3]`: real range tombstones are a B3 deliverable and must land before any I2 number |
| The whole SSTable resident in memory (`table.h`) | B2-D7 required it, because `DeleteRange` expands at `Apply` and `Apply` makes no Env call. Range tombstones make the expansion O(1) in the range, which **removes the constraint that forced it** |
| `ApproximateDiskBytes` scanning | B1 divergence 4: it has been exact and O(n) since B1 for want of table metadata. B3 has metadata |
| `kMaxRecordBytes` reachable by a legal `DeleteRange` | §8.6: real range tombstones make the record O(1) in the range, and the cap stops being reachable |

**The obligations that come due here.** `CF-2` — 47 of 98 mutant classes have no standing
measurement. `CF-3` — every loop the phase adds must assert the movement it terminates on.

---

## 1. B3-D1 — THE DROP CLAIM, stated before the policy is chosen

> **Ansh, on B2's close:** *compaction is the first thing in this engine that deletes data the system
> previously held, so the oracle question is different from B1 and B2's. Recovery equivalence asks
> whether what survives matches; compaction needs a claim about what may be dropped and when, stated
> before the policy is chosen, because a policy that drops something it should not will look
> identical to a correct one under any check that only compares surviving state.*

This section is that claim. **It is a decision in its own right and it precedes B3-D3 deliberately**:
a policy chosen first would define the claim, and the claim is what the policy has to satisfy.

### 1.1 The observable contract, restated so the claim can be stated against it

For any sequence `s` and user key `k`, `Get(k, s)` returns the value of the newest version of `k`
whose sequence is `≤ s`, unless that version is a deletion, in which case `kNotFound`.

**Only some `s` are observable.** Let

> **`S` = { the current visible sequence } ∪ { the sequence of every LIVE snapshot }**

`S` is the entire set of sequences any caller can read at. A version that no `s ∈ S` can reach is
**unobservable**, and dropping the unobservable is the whole of what compaction is permitted to do.

### 1.2 The claim

For a user key `k` with versions `v₁ > v₂ > … > vₙ` ordered by sequence descending, define

> **`keep(k)` = { for each `s ∈ S`: the newest `vᵢ` with `seq(vᵢ) ≤ s` }**

— at most `|S|` entries, and the only ones any reader can ever return.

### 1.2a THE PHASE'S FIRST RESULT: the claim was wrong, and its own checker found it

**`keep(k)` as first written over-requires.** It demanded that the newest version at every observable
sequence survive, full stop. When that version is a **DELETION**, the answer at that sequence is
`kNotFound` — and **dropping the deletion preserves that answer exactly**, so long as nothing older
survives to be uncovered.

> **THE REQUIREMENT IS ON THE ANSWER AT EACH OBSERVABLE SEQUENCE, NOT ON ANY PARTICULAR ENTRY
> SURVIVING.**

**Why the stricter form is WRONG and not merely over-cautious, and this is the sentence a future
reader needs.** A tombstone whose masked versions have all been dropped has no work left to do. Under
the stricter claim it may never be dropped — so **every deletion the database has ever taken accretes
forever**, and the space a compaction can reclaim is bounded below by the tombstone count.
**COMPACTION STOPS TERMINATING IN SPACE.** A key written and deleted a million times would keep a
million tombstones under a claim whose whole purpose is to permit reclaiming them. That is not
caution; it is a claim that forbids the one drop the phase exists to make.

**THE CONSEQUENCE, WHICH IS THE PART WORTH RECORDING.** An adjudicator built to the uncorrected claim
would have **refused correct compactions** — and it would have presented as **a bug in the engine**.
That is `HARNESS-006`'s shape exactly: *a checker wrong in the direction that sends debugging to the
wrong component.* The difference is where it was caught.

> **THIS TIME IT WAS CAUGHT BEFORE THE COMPONENT EXISTED TO BE BLAMED.**

**The general form, and it is now twice observed:**

> **A CORRECTNESS CLAIM WRITTEN BEFORE ITS CHECKER IS A HYPOTHESIS. BUILDING THE CHECKER IS HOW IT
> GETS TESTED.**

Two phases now in which the observer corrected the observed **before the observed existed** — B2's
classifier finding that internal keys do not sort by `memcmp`, and B3's adjudicator finding that
`keep(k)` over-requires — and **both times the correction was in the CLAIM rather than in the code.**
That is what the observer-before-the-observed rule buys, and it is not the thing the rule is usually
argued for.

**A compaction may drop an entry `e` for key `k` if and only if BOTH hold:**

1. **`e ∉ keep(k)`**, where **`keep(k)` contains only VALUES** — for each `s ∈ S`, the newest version
   of `k` with `seq ≤ s`, *if that version is a value*. A deletion is never required; and
2. **if `e` is a deletion**, no version of `k` with a smaller sequence survives anywhere. Dropping a
   tombstone is permitted exactly when everything it masked is dropped with it.

Clause 2 subsumes what a per-input rule would say and is checkable from the durable image alone,
which is what lets the adjudicator judge without knowing which files a compaction chose.

Clause 2 is the tombstone rule, and it is the one that turns **input selection into a correctness
concern rather than a performance one**. A deletion dropped while an older value survives in a file
the compaction did not read **resurrects deleted data**. So the policy may not choose its inputs
freely: *the drop rule constrains the input set*, and §3 is written knowing that.

### 1.3 Why the snapshot floor is safe to compute once, at the start

`S` changes under compaction. Two directions, and only one is a hazard:

- **a snapshot is RELEASED** — `S` shrinks, more becomes droppable, and a compaction using the older
  larger `S` has merely kept too much. Safe.
- **a snapshot is CREATED** — it takes the *current* sequence, which is `≥` every sequence already in
  `S`, so it cannot reach anything the floor already permitted dropping. Safe.

**The floor only ever moves up.** So a floor read once at compaction start is conservative, and no
lock is held across the compaction. Stated here because the alternative — re-reading it — would be
both slower and wrong-looking-correct.

### 1.4 Why a surviving-state check cannot see a wrong drop

Two failure modes, and the first is invisible to every check B1 and B2 built:

| wrong drop | what a read at the CURRENT sequence sees | what actually broke |
|---|---|---|
| a version only a **snapshot** could reach | **nothing — identical state** | a read through that snapshot returns the wrong value or `kNotFound` |
| a tombstone dropped with an older version outside the inputs | an old value resurfaces | data returns from the dead |

**The first is the one this section exists for.** B2's recovery-equivalence test compares state at a
watermark; the sweep compares state after a kill; both read at one sequence. A compaction that
respects `keep(k)` for `s = current` and violates it for every snapshot passes all of them.

### 1.5 What follows for the checker, and it is a different shape of check

A state comparison is a comparison over **survivors**. The claim is about **drops**. So:

> **THE ORACLE COMPUTES THE PERMITTED DROP SET ITSELF, FROM ITS OWN RECORD, AND HOLDS THE ENGINE TO
> IT — in THREE directions.** `kept ⊇ required`, `dropped ⊆ permitted`, and
> **`survived ⊆ submitted`** — the engine may not hold a version the harness never wrote.

**THE DECISION IS DEMONSTRATED RATHER THAN ARGUED, and the demonstration is a test pair:**

> `DropCheck.ASnapshotMakesAnOlderVersionRequired` and
> `DropCheck.WithoutASnapshotTheSameDropIsPermitted`.
>
> **The images are BYTE-IDENTICAL. The state read at the current sequence is IDENTICAL. Only the
> snapshot set differs — and one is a violation while the other is correct.**

Cited rather than described, because the pair *is* the argument for why B2's state comparisons cannot
do this job, and a paragraph restating it would be the weaker copy.

**The third direction is B3.1's answer to the aliasing condition** — see §2.2b.

This is §7.6's harness-side adjudication applied to a new question, and both directions are needed
for the reason §7.6 gives: an engine that drops nothing is as wrong as one that drops too much — it
is a compaction that does not compact, and every state comparison in the tree would call it correct.

---

## 2. B3-D2 — how the drop claim is checked, and the one thing that makes it hard

### 2.1 The shape

The rig already records every submission (`SubmissionLog`). B3 adds:

- **every snapshot taken and released**, with its sequence — so the harness owns `S`;
- from those two, **the harness's own version set**: for every user key, every `(seq, type, value)`
  ever written. This is a fact about what the harness submitted and asks the engine nothing.

Before a compaction the harness computes `keep(k)` for every `k`; after it, it computes what actually
survived, and asserts both inclusions.

### 2.2 Reading what survived without using the path under test — RULED

"What survived" is a fact about **bytes in SSTables**, not about what `Get` returns — reading through
the API would filter by snapshot and hide exactly the failure this check exists for.

So something must parse SSTables, which appeared to collide with §7.4 condition 1: *an oracle that
interrogates the engine believes the lie*, enforced by `cpp-scan` part 2b as **`exactness_oracle.*`
includes nothing from `src/`**.

> **RULED, 2026-08-25.** *Oracle independence has never been about which code the checker links
> against. It is about whose account the verdict rests on. The lie is a CLAIM — what the engine says
> it holds, what it says is durable, what `Get` chooses to return. **Bytes on disk are not a claim.**
> They are the artifact the engine produced, and reading them with the engine's own reader is closer
> to a hex dump than to an interview, provided the reader answers only what is there.*
>
> *The rule as stated in `cpp-scan` is too coarse and is CORRECTED rather than excepted, because an
> exception here would be the thing this project has refused everywhere else:*
>
> > **AN ORACLE MAY PARSE THE ENGINE'S ARTIFACTS, AND MAY NOT CONSULT THE ENGINE'S BELIEFS.**
> > **Parsing shares a format; consulting shares a judgement.**
>
> *Then make the boundary MECHANICAL, because "may read bytes, may not ask beliefs" is a discipline
> and this project has watched five disciplines fail.*

### 2.2a The boundary, made mechanical — B3-D2a

A sentence is not a boundary. Three mechanisms, mirroring the `DECIDERS.txt` pattern, which is the
one construct in this tree that has already survived a phase:

**1. `engine-cpp/ARTIFACTS.txt` — the allow-list of headers an oracle may parse**, each with a
one-line justification of why it only decodes. An oracle's `src/` includes must all be listed.

**2. The mechanical mark of judgement, and it is the whole of the corrected rule:**

> **AN ARTIFACT HEADER DECLARES NOTHING THAT TAKES AN `Env*` AND NOTHING THAT TAKES A SNAPSHOT.**

Those two are exactly where decoding becomes judging. `Env*` means *it went and looked*, which is an
act with an opinion about what the current state is. A snapshot parameter means *it decided what a
caller should be allowed to see*, which is the engine's rule about visibility — the single rule this
checker exists to audit. A header with neither is bytes in, structure out.

**3. `engine-cpp/ORACLES.txt` plus a `RIFT_ORACLE` marker**, cross-checked both ways, so a new
verdict-producing file cannot land outside the rule by simply not being named in it.

**What the rule immediately costs, and the cost is a correction rather than a concession.** Two of
B2's headers are *mixed* and fail the mark, which is the rule doing its job on the first day:

| header | why it fails | what B3.0 does |
|---|---|---|
| `sst/table.h` | `Table::Open(Env*, …)` and `Table::Get(…, snapshot, …)` | nothing — the checker does not need it. `table_format.h`'s `ParseBlock`, `DecodeFooter` and `DecodeHandle` enumerate every entry in a table, and enumerating is the whole job |
| `sst/manifest.h` | `Manifest::Open(Env*, …)` | **split**: the pure codec — `EditKind`, `TableMeta`, `ManifestEdit`, `ManifestState`, `EncodeEdit`, `DecodeEdit`, the path helpers — moves to `sst/manifest_format.h`, mirroring `table_format.h` beside `table.h`. The `Manifest` class keeps `manifest.h` |

**And the checker never opens a file.** It is handed a `DurableImage` — the harness's own
path-to-bytes map, which `TestEnv` already produces and which no engine code touches — and parses it
with pure decoders. **No `Env`, no engine object, no beliefs, and no I/O.** That also makes it
drivable from hand-built bytes with no engine in the tree at all, which is B2-D6's ordering for the
third time.

### 2.2b The aliasing, named — and the three conditions on it

**THE COST IS REAL AND IT IS NAMED HERE RATHER THAN LEFT IMPLICIT.** The checker and the engine share
a parser, so a parser defect is a blind spot they share **at exactly the place the checker exists to
watch**. Ansh's three conditions:

**(i) Name the failure.** A reader defect that makes surviving bytes look absent, or absent bytes look
present, corrupts the `survived` set the whole verdict rests on.

**(ii) Land a mutant that plants the shared blind spot, and see what kills it — AMENDED 2026-08-25.**

The condition originally named *a reader change that hides a live record*. **That is the wrong
direction and the amendment is Ansh's:** hiding a record makes `survived` lose something `required`
still demands, so the verdict is a **FALSE VIOLATION** — loud and self-announcing. The direction that
produces a **FALSE PASS** is a reader that reports a record the bytes do not contain, because that
makes a real drop look survived.

> **THE MUTANT TO PLANT IS THE ONE THAT FABRICATES A RECORD. If nothing kills it, the aliasing is
> unacceptable and the rig gets its own parser.**

| mutant | what it does | measured at B3.1 |
|---|---|---|
| **`READER-shows-a-dropped-record`** | `ParseBlock` reports an entry the bytes do not contain | **KILLED.** 4 of 9 `DropCheck` tests, and 12 of the flush suite |
| `READER-hides-a-live-record` | `ParseBlock` silently skips an entry | **KILLED.** 6 of 9 `DropCheck` tests, and 12 of the flush suite |

**DISCHARGED, AND THE RESULT IS BETTER THAN "SOMETHING KILLED IT".** Both directions die **on the
drop checker's own assertions**, not on a distant count somewhere downstream — so the instrument that
makes the shared parser acceptable is *the checker itself*, which is the only answer that does not
put a third party in the trust chain.

That is not luck. **The third direction — `survived ⊆ submitted` — was added for exactly this**, once
working through the condition showed that directions one and two both ask whether something is
MISSING and neither can see a record that should not be there. The condition did what conditions are
for: it changed the design rather than merely auditing it.

**`FLOORS.txt` records the split labels**, per condition (iii): the named tests are load-bearing for
every drop verdict in the phase.

**(iii) The split label.** `FLOORS.txt` records the drop checker as **`covered-by:` whatever kills
those mutants — not by its own assertions**, because an instrument that certifies itself certifies
nothing. Whatever kills them is **load-bearing for every drop verdict in the phase** and is named as
such in the entry.

### 2.2c THE LOAD-BEARING DECISION, and it is as important as the two marks

> **`required` COMES FROM THE HARNESS'S SUBMISSION LOG AND NEVER FROM READING THE ENGINE'S FILES**,
> because computing it from the engine's own artifacts would hide a record from **both sides of the
> comparison** and make the aliasing **total rather than one-sided**.
>
> **That is ruling 4 applied to a checker that is permitted to parse: PERMISSION TO READ THE ARTIFACT
> IS NOT PERMISSION TO DERIVE THE EXPECTATION FROM IT.**

Stated this prominently on Ansh's instruction, and for his reason: **a future reader who has
internalised the two marks will be tempted to source `required` the same way.** The marks say what an
oracle may *touch*; this says what it may *conclude from*. They are different questions and only the
first is mechanical.

Concretely: if the checker computed *what was there before compaction* by reading the input files,
then a parser that fabricates or hides a record would do so on both sides — `required` and
`survived` — and the drop would be invisible with no assertion able to see it. Sourcing `required`
from what the harness **submitted** is what keeps the aliasing to one side, and it is why
`READER-shows-a-dropped-record` is expected to be catchable at all.

### 2.3 Both directions, and the mutant for each

| direction | mutant |
|---|---|
| `dropped ⊆ permitted` | one that drops a version a live snapshot can still reach |
| `kept ⊇ required` | one that drops a tombstone while an older version survives outside the inputs |
| *and the vacuous case* | one that **drops nothing at all** — every state comparison stays green and the engine has stopped compacting |

The third is the `BM15` shape — a check that has stopped checking rather than an engine that has
stopped working — and it is why `kept ⊇ required` is not the whole of it: a **space-amplification
floor** is what catches a compaction that ran and did nothing. See §8.

---

## 3. B3-D3 — the compaction policy

**Candidates.**

**(a) Single-run compaction.** When live tables exceed `N`, merge *all* of them into one. Simplest
possible; the drop rule's clause 2 is trivially satisfied because the inputs are everything.

**(b) Two levels: L0 (overlapping, from flushes) + L1 (non-overlapping run).** Compact L0 into L1
when L0 holds ≥ `K` files. Reads consult every L0 file, then binary-search L1. This is LevelDB's
structure truncated to two levels.

**(c) Multi-level leveled with per-level scoring** — CLAUDE.md's stated B3 target, and A6's STRETCH
item.

**Tradeoffs.**

| | write amplification | read amplification | space amplification | drop rule |
|---|---|---|---|---|
| (a) | **unbounded in the data size** — every compaction rewrites the whole database | 1 table after compaction, `N` before | best: one copy of everything | trivial: inputs are everything |
| (b) | bounded by the L0→L1 ratio; L1 rewritten per compaction, so still O(data) per compaction | `K` + 1 | one copy in L1 plus L0's overlap | **inputs must cover every L1 file overlapping the key range**, or tombstones may not be dropped |
| (c) | best: each level rewrites a bounded fraction | levels + 1 | tunable | per-level bottom-most rule |

**Recommendation: (b).**

(a) is rejected **not for being slow but for making the measurement meaningless**: the exit criterion
is *space and read amplification measured and recorded*, and a policy that rewrites everything on
every compaction has a write amplification that grows without bound in the data size, which makes any
recorded number a fact about the workload's size rather than about the engine.

(c) is rejected as **A6's STRETCH item, deliberately, and as a policy change on top of (b) rather
than a different design**. (b) already has: a merge over N sorted inputs, the drop rules, input
selection constrained by clause 2, range tombstones, snapshot pinning, and version lifetime. (c) adds
*which* files to pick and *when* — a scoring function over a structure (b) already builds.

**The measurement that reopens (c), stated as a number rather than a mood, per A6:** B5's standalone
numbers showing **write amplification above 10×** on a fillrandom workload at a data size B5 states,
**or** read amplification above `K+1` because L0 has grown past its trigger under sustained load —
each attributed by measurement rather than inferred.

**What (b) costs and I am not hiding:** L1 is a single run, so every L0→L1 compaction rewrites all of
L1 that overlaps. At a data size well beyond the flush threshold that is the write amplification
(c) exists to fix. The number above is where it stops being acceptable.

---

## 4. B3-D4 — the read path over two levels

L0 files overlap each other and must all be consulted, newest first. L1 is a non-overlapping run, so
at most one file can contain a given key and it is found by binary search over file key ranges.

`MergedIter` takes L0 files as individual sources; **L1 becomes one `ConcatenatingIter`** — a cursor
over a sorted sequence of non-overlapping tables — so `k` in the merge is `|L0| + 1` rather than
`|L0| + |L1|`. That matters for the linear `PickSmallest` scan B2 chose knowing `k` was small; it is
the change that keeps that choice honest.

**The point-read path keeps its structure and gains a level.** `VersionGet` walks: memtable, immutable
memtable, each L0 file newest first, then the one L1 file that can contain the key. The bloom filter
skips a whole file at each step — and B2's `BM55` survival is why the comment about *which* line
carries the newest-first property now sits on that walk (GF-7).

---

## 5. B3-D5 — snapshots, and version lifetime

B2's `Snapshot` holds `shared_ptr`s to the stores it read, which was enough because nothing was ever
deleted. **Compaction deletes files.** Two things must hold:

1. **A file a live snapshot needs is not deleted.** The `shared_ptr` in `Version` already does this
   for the *table objects*; what B3 adds is that the manifest edit removing a file and the file's
   deletion are separated by the last reference dropping — the same shape as B2's WAL retirement,
   which is why it is a small change rather than a new mechanism.
2. **The snapshot's sequence pins the drop rule**, per §1.3. `S` is read at compaction start.

The frozen interface says a snapshot *"holds its version against compaction until it is Closed"* —
trivially true in B1, refcount-true in B2, and **B3 is where it means what it says.** A test that
takes a snapshot, compacts twice, and reads through it is the gate.

---

## 6. B3-D6 — real range tombstones

`[A3]` requires them here. B2's `DeleteRange` expands to one point delete per live key, which is
correct, `O(keys)` per operation, and the reason `kMaxRecordBytes` is reachable by a legal call.

**Candidates.** (a) A dedicated range-tombstone block per SSTable holding sorted
`(start, end, seq)` triples, LevelDB/RocksDB style. (b) Ordinary entries with a reserved value type
at the start key. (c) Keep the expansion — **out**, `[A3]` rules it out and I2's numbers depend on it.

**Recommendation: (a).** (b) makes a range tombstone findable only by scanning to its start key, so a
read must scan backwards to discover a range that covers it — which is the wrong shape for a point
read and gets worse with range length. (a) costs a new block and a new classifier rule; B2.0's
classifier already has the shape to extend, and the `DELETE_RANGE` op kind has been reserved in the
WAL since B1 for exactly this.

**What it changes elsewhere:**

- **`Apply` stops expanding**, so `DeleteRange` becomes `O(1)` in the range — which retires B2-D7's
  requirement that whole SSTables be resident, and with it `table.h`'s memory cost;
- **the read path must consult range tombstones** in every table it touches, before trusting a value;
- **compaction must merge and drop them by §1.2's rules** — a range tombstone is droppable when it
  covers nothing older outside the inputs, which is clause 2 over a range rather than a key.

---

## 7. B3-D7 — iterators, and CF-3 applied

> **CF-3:** every loop that terminates by "the cursor strictly moves" asserts that movement rather
> than commenting it, using a property that does **not** depend on the comparator under test.

**B3 is where this gets tested for real**, because compaction's merge is the most loop-dense code in
the engine and its termination arguments are the hardest to state: a k-way merge that also drops
entries, skips versions, and consumes range tombstones has loops whose progress is not a single
cursor advancing.

Every such loop in the phase gets its assertion in the diff that adds it. Where progress cannot be
stated as "a key strictly advances", it is stated as a **bounded work counter** — and a counter is
the honest form when the loop's progress genuinely is not monotone in the key.

---

## 8. B3-D8 — what "space and read amplification measured and recorded" means

The exit criterion names two numbers. Stated here so the measurement is designed rather than
improvised:

| number | definition | why it is the one |
|---|---|---|
| **space amplification** | bytes on disk ÷ bytes of live data as the harness's own model computes it | the harness knows the live set exactly; asking the engine would be asking the thing under test |
| **read amplification** | tables consulted per point read, measured, **not** derived from the level structure | the bloom filter's whole purpose is to make the measured number smaller than the structural one |
| **write amplification** | bytes written ÷ bytes submitted | it is what decides whether (c) is reopened, so it is measured even though the exit criterion does not name it |

**A FLOOR ON SPACE AMPLIFICATION IS WHAT CATCHES A COMPACTION THAT DID NOTHING** — §2.3's vacuous
case. A compaction that never drops leaves every superseded version on disk, so the ratio rises and a
ceiling on it fails. This is the only check in the phase that catches "the engine stopped compacting"
without a drop-set comparison, and it is cheap.

---

## 9. B3-D9 — the landing sequence

The two ordering invariants hold unchanged: **the observer lands before the observed**, and **a gate
lands only once its failure has been induced and observed**.

| step | lands | why here |
|---|---|---|
| **B3.0** | the harness's version-set model and `S`; the drop-set adjudicator, both directions, from fixture data | B3-D2. It is the observer, and no compaction exists yet for it to be written to agree with |
| **B3.1** | CF-2 discharged: 47 classes get their standing measurement | it is bookkeeping, it is due, and doing it first means every class B3 adds joins a complete file |
| **B3.2** | the range-tombstone block format + classifier rules, from fixture bytes | B2-D6's ordering, third time |
| **B3.3** | the two-level structure, `ConcatenatingIter`, the read path | needed before anything can compact into it |
| **B3.4** | compaction: input selection, the merge, the drop rules | the drop adjudicator from B3.0 judges it |
| **B3.5** | range tombstones end to end; `Apply` stops expanding; `table.h`'s residency retired | needs both |
| **B3.6** | snapshots across compaction; version lifetime; file deletion after the last reference | needs compaction |
| **B3.7** | the sweep extended over compaction; amplification measured and recorded; floors re-measured | power is measured last, once the shape stops moving |

Every new evidentiary decider is registered in `DECIDERS.txt` at the step that lands it, with both
directions asserted. B3 is expected to add at least one: **whichever function decides whether a
compaction's drop set makes a run a violation or merely characterization** — because a run with a
non-default snapshot policy is a different regime, and the floor that decides it is exactly the shape
`HARNESS-006` was.

---

## 10. CF-2, discharged at B3.1

47 of 98 classes have no standing measurement. Every one carries a `covering-lane` and dies in the
mutant lane; **none carries a `covered-by` naming the assertion**, which is the difference that lets
a covering assertion be deleted with no lane going red.

By lane: 28 `cpp-test`, 12 `cpp-scan`, 2 `cpp-tsan`, 2 `cpp-ci`, 1 `cpp-asan`, 1 `cpp-ubsan`,
1 `cpp-campaign`. The work is naming the specific test for each, which is research and not
transcription — and it is why it lands as its own step rather than folded into a feature.

The check is mechanical and goes in the lane: `comm -23` of the patch basenames against `FLOORS.txt`'s
class column must print nothing.

---

## 11. Open questions for Ansh

**B3-Q1 — RULED.** (a), *with the rule sharpened rather than bent*: an oracle may parse the engine's
artifacts and may not consult its beliefs, made mechanical by §2.2a and conditioned by §2.2b. The
correction lands in `cpp-scan` as a rule, not an exception.

**B3-Q2 — NOT RULED; PROCEEDING ON MY READING AND FLAGGING IT.** The implementation instruction did
not address this one, so B3 proceeds on the reading below and it is called out here rather than
treated as settled. If the reading is wrong, §1.2's claim is wrong and the phase is wrong with it.

**Does `S` include sequences a snapshot COULD be taken at, or only live ones?** §1.1 says live
ones, which is what makes dropping possible at all. The stricter reading — never drop anything a
future snapshot might want — permits no compaction whatsoever, so this is really a question about
whether the frozen `Snapshot` contract promises anything about sequences no snapshot holds. I read it
as no. Worth one sentence from you, because §1.2's whole claim rests on it.

**B3-Q3 — RATIFIED by the implementation instruction** (*"policy chosen by measurement per Amendment
A6 with multi-level leveled recorded as an upgrade path"*), which is this reading. Retained for the
record.

**Is the two-level structure enough for the exit criterion's numbers to mean anything?** The
criterion is *measured and recorded*, not *good*. I read it as: the numbers must be real, honestly
obtained and reproducible, and (b)'s write amplification being worse than (c)'s is a recorded fact
rather than a failure. If you read it as requiring the numbers to be *competitive*, that is (c) and
A6's STRETCH line needs revisiting.

---

## 12. Decision summary

| id | decision | recommendation |
|---|---|---|
| **B3-D1** | the drop claim | stated before the policy; `keep(k)` over the live-observable sequence set `S`, plus the tombstone rule that constrains input selection |
| **B3-D2** | how it is checked | harness computes the permitted drop set and holds the engine to it, **both directions**, plus a vacuous-compaction guard |
| **B3-D2b** | `required`'s source | the harness's submission log, never the engine's files. Permission to read the artifact is not permission to derive the expectation from it |
| **B3-D2a** | the artifact/belief boundary | mechanical: `ARTIFACTS.txt`, `ORACLES.txt`, and the mark — *an artifact header declares nothing taking an `Env*` and nothing taking a snapshot*. Splits `manifest_format.h` out of `manifest.h` |
| **B3-D3** | compaction policy | **two levels**, L0 + L1; (a) rejected for making the measurement meaningless, (c) deferred as A6's STRETCH with a numeric threshold to reopen |
| **B3-D4** | read path | `ConcatenatingIter` for L1 so the merge's `k` stays small |
| **B3-D5** | snapshots | refcount + a sequence floor read once at compaction start |
| **B3-D6** | range tombstones | a dedicated block per SSTable; retires `Apply`'s expansion and `table.h`'s residency |
| **B3-D7** | iterators | CF-3 applied to every loop the phase adds; bounded work counters where progress is not monotone in the key |
| **B3-D8** | measurement | space, read **and** write amplification; a space-amplification ceiling is the vacuous-compaction guard |
| **B3-D9** | landing sequence | observer first, CF-2 second, format third, structure, compaction, tombstones, snapshots, power |

| id | question | my reading |
|---|---|---|
| **B3-Q1** | how the drop checker reads surviving bytes | **RULED**: (a), with the rule corrected to *parse artifacts, never consult beliefs*, made mechanical and conditioned on two aliasing mutants |
| **B3-Q2** | does `S` mean live snapshots only | **NOT RULED.** Proceeding on *yes*; flagged in §11 |
| **B3-Q3** | must the amplification numbers be competitive or merely real | **RATIFIED**: real |

**Nothing in B3 is written until this is ruled on.**
