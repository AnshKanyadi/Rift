# DESIGN-B3 — compaction, iterators, and the first thing this engine deletes

**Status: REVISION 9. B3-Q1 RULED 2026-08-25; B3-Q3 ratified by the implementation instruction;
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
lock is held across the compaction.

**B3-Q2 RULED (2026-08-26): `S` IS THE LIVE SNAPSHOTS.** A retired snapshot has no reader that can
observe the version it pinned, so keeping that version required would make compaction **unable to
reclaim space nothing can see** — which is `keep(k)`'s over-requirement arriving a second time from a
different direction. Asserted in both directions at the engine and at the model
(`Compaction.AReleasedSnapshotStopsRequiringItsVersion`, `...TheModelDropsAReleasedSnapshotFromS`).

**THE SECOND ARGUMENT ABOVE RESTS ON A FACT ABOUT THE INPUTS, AND A FACT IS ASSERTABLE.** "A snapshot
created takes the current sequence, which is `≥` every sequence already in `S`" is only half of what
is needed. The half that matters is:

> **EVERY SEQUENCE THE INPUTS HOLD IS AT OR BELOW THE VISIBLE SEQUENCE ALREADY IN `S`** — written
> `pin_seq ≤ max(S)`, and asserted in `DoCompact`.

Given it, for any later `s`, `keep(k)` at `s` is *the newest version of k in the inputs*, which the
visible sequence **already required** — so a snapshot taken mid-compaction requires nothing new. It is
true today because tables hold only flushed data while `seq_` runs ahead of them, and it would stop
being true the day a compaction took the immutable memtable as an input. **That is a change someone
will propose, which is exactly why it is a `RIFT_CHECK` and not a paragraph.**

**AND THE MUTANT ANSWERED A QUESTION RATHER THAN FINDING A BUG.** `BM84` planted the shape a future
optimization will produce — *read `S` as late as possible so it is as small as possible so more can
be dropped* — and it **survived, correctly.** Both directions of `S` movement are safe, so the
**timing of the read does not carry the correctness**; `pin_seq ≤ max(S)` does.

The mutant was **deleted rather than kept as a class that can never fail**, and the answer lives at
the call site and here instead. `S` is still read once at the start, because *once-at-the-start* is
the property that survives if the inputs ever stop being frozen at selection time.

**`BM83` is the class that did fire**, and it is aimed at the ruling's dangerous misreading: *"live
snapshots only"* read as *"live snapshots **alone**"*. With no snapshot held — the ordinary case —
`S` would be empty, nothing would be observable, and **every version of every key would become
droppable**: a database that is structurally perfect and empty. It is killed by the non-empty guard
in `DoCompact`, not by a test, and carries `killed-by-guard:` for it.

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

### 2.1a B3-D2c — `survived ⊆ submitted`, a decision in its own right

**Stated as a decision and not as a consequence of the audit, because a future reader will otherwise
take it for defensive clutter.** It never fires in normal operation, and nothing else in the tree
asks its question — which is exactly the combination that gets deleted in a tidy-up.

> **THE ENGINE MAY NOT HOLD A VERSION NOBODY WROTE.**

**Why it had no checker until the aliasing condition forced the question.** The other two directions
looked complementary and are not:

```
kept    >= required     nothing a reader can reach was dropped
dropped <= permitted    no tombstone was dropped over what it masked
```

**Both ask whether something is MISSING.** A reader that reports a record the bytes do not contain
makes a real drop look survived — the **false-pass** direction — and neither of them can see it.
Their agreement was not corroboration; **it was the shape of the hole.** Recorded as `GF-10`:

> **A set of assertions all pointed the same way has a blind spot the same size as its agreement, and
> asking what a checker CANNOT SEE is a different question from asking whether it works.**

**What it now stands between.** `survived ⊆ submitted` is the only thing between a fabricating reader
and a green drop verdict — which is what makes B3-Q1's shared parser acceptable at all. It is
therefore load-bearing for every drop verdict in the phase, and `FLOORS.txt` names the tests that
hold it up.

**It also catches an engine that invents data**, which is a different failure with the same
signature, and one nothing else in the tree would notice.

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

**WHERE THE NUMBERS LIVE, AND WHY NOT HERE.** They are in `BENCHMARKS.md`; this document carries the
decision and the methodology with a pointer. Ruled at B3's sign-off:

> **A PUBLISHED CLAIM AND A DECISION RECORD HAVE DIFFERENT READERS AND DIFFERENT LIFETIMES.** I2 will
> restructure `BENCHMARKS.md` when it sets the reporting standard, and **a decision record should not
> have to be restructured because a reporting standard changed** — what was decided here, and on what
> evidence, does not change when the table around the evidence is reformatted.

`BENCHMARKS.md` carries a **provenance block** for exactly that transition: the commit the numbers were
taken at, the engine shape they describe, the caps, and what would invalidate them.

**RULED BY MEASUREMENT, B3.7b: (b).** Write amplification is **8.08 at the 128 MiB crossing point**,
below the 10× threshold this section fixed in advance — so `(b)` holds at the size the threshold
named and **wins on the measurement rather than on A6's rule alone.** `(c)` is not reopened. The
model over-predicted (10.00 predicted, 8.08 measured), which is the safe direction; the fitted
crossing is nearer 170 MiB. Full methodology, conditions and caveats: `BENCHMARKS.md`. Reproduce with
`make cpp-amp`.

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

### 6.1 The frozen surface, and WHAT THE CLASSIFIER REFUSES — written before the writer exists

**This is a SPECIFICATION, not a description.** Fourth use of B2-D6's ordering, and the first time it
judges a format **that does not exist yet**: there is no writer, no encoder and no compaction, so the
classifier **cannot be written to agree with an implementation** — there is nothing to agree with.
Every rule below is a decision about what the format *means*, and the writer will be checked against
rules that were fixed before it was designed.

```
range-tombstone block = tombstones || restart_array || restart_count:u32 || crc32c:u32
tombstone             = start_len:u32 || start_user_key || end_len:u32 || end_user_key || tag:u64
```

The block reuses the data block's framing exactly — same restart array, same trailer, same `crc32c`
— so `ParseBlock` decodes it and **there is one block decoder in the engine rather than two that can
drift.** §7.5's one mechanism, two users, for the third time.

The `tag` is a full internal-key tag, not a bare sequence: **a range tombstone is a version like any
other**, it is ordered against point versions by the same comparator, and its `ValueType` is the
reserved `kDeleteRange` that has sat in the WAL's `OpKind` since B1 for this.

**Bounds are `[start, end)`**, half-open, agreeing with `engine.InRange` by construction — the same
choice `Bound` made at B1 and for the same reason: two range conventions in one engine is a bug
waiting for a boundary key.

**WHAT THE CLASSIFIER REFUSES.** Each row is a shape a fixture builds by hand, and each is a
statement about the format rather than about any writer:

| refused | why it is a rule and not a preference |
|---|---|
| `end ≤ start` bytewise | An empty or inverted range covers nothing and no writer can mean it. Accepting it makes "covers nothing" and "covers everything before start" indistinguishable at the reader |
| tombstones not strictly ascending by `start` | The block is binary-searched to find the tombstones covering a key. An unsorted block does not fail — **it returns the wrong answer**, which is a resurrected key or a wrongly hidden one |
| two tombstones with the same `start` and the same `tag` | Two versions the merge order cannot distinguish, exactly as for point entries |
| a `tag` whose `ValueType` is not `kDeleteRange` | A point version in the range block is a version the point path will never see, so it is data that exists and is unreachable |
| a `start` or `end` shorter than… | **nothing** — range bounds are USER keys, not internal keys, and the empty user key is a valid key. The tag is separate. Stated because the point-entry rule is the opposite and a reader will assume it carries over |
| a range block whose declared length disagrees with its block | The filter's rule, one block over: a truncated block must announce itself rather than read as a smaller one |
| a table whose footer names a range block that overlaps the index or the filter | Every handle is range-checked before it is followed, as in B2.0 |

**AND ONE REFUSAL THAT IS NOT ABOUT THE BLOCK.** A table's range tombstones must not cover a key
range disjoint from the table's own `[smallest, largest]` bounds by more than the tombstone's own
extent — because the manifest records those bounds, and a compaction chooses inputs by them. **A
tombstone reachable only through a table whose bounds do not admit it is a tombstone no read will
consult**, which resurrects everything it was supposed to mask. This is B3-D1 clause 2 showing up in
the FORMAT: input selection is a correctness concern, so the bounds that drive it are too.

### 6.1b — B3-Q4: THE UNBOUNDED END, AND THE SHIM THAT WAS HIDING THE GAP

**THE FINDING IS BIGGER THAN THE FIX, so it leads.**

`[A3]` put `DeleteRange` in the frozen interface for the clear-everything case, and B2 implemented it
as **one point delete per live key**. That expansion is why unboundedness never needed an encoding:
`Bound::Unbounded()` was resolved against the *live set* before anything was written, so no format
ever had to represent `[start, ∞)`.

> **AN EXPANSION OR A SHIM THAT MAKES A CASE UNNECESSARY ALSO MAKES THE GAP IT HIDES INVISIBLE.**
> §6.1 specified this format from the **block's** point of view and induced every refusal against
> hand-built bytes — an exercise that never touches `Bound`, because the classifier never sees one.
> Two frozen artifacts, each internally consistent, **never checked against each other.**

**Neither contract was wrong. They were UNJOINED** — which is `GF-15` between two frozen artifacts
rather than inside one, and it is the second thing `CF-4` now sweeps for: not only the frozen
interface as a whole, but **every place B2 or B3 removed an expansion.**

**`CF-4` paid before it came due.** The sweep it schedules for B4 produced its first instance at
B3.5, and it cost a design decision instead of a differential failure against `engine/model`. That is
an argument for doing the sweep, not for deferring it.

**The fix, and why (b).** `end_len == kUnboundedEndLen` (`0xFFFFFFFF`) means no upper bound and the
record carries **no end bytes**. Only the END needed it: an unbounded *start* is `""`, since the empty
user key is the minimum and `["", end)` covers the same set.

The rejected candidate is the one worth keeping. **A flag bit in the tag** was available and cheaper
in bytes, and §6.1 says a range tombstone is *"a version like any other, ordered against point
versions by the same comparator"* —

> **A PROPERTY THAT HOLDS FOR EVERY ENTRY IS WORTH MORE THAN A BYTE.** A tag that stops being a plain
> internal-key tag surrenders that property everywhere, in exchange for one bit here.

**What the table records, and what it cannot.** An unbounded tombstone widens `smallest` and **cannot
widen `largest`** — there is no finite key to widen it to. `TableCheck::unbounded_end` carries the
fact instead: the table's range is `[smallest, ∞)`. Input selection must read the flag, not only the
bounds; a reader consulting the bounds alone would under-select and let clause 2 drop a deletion
whose masked value survives above. **Over-approximating the range costs a read; under-approximating
resurrects data**, so the flag is deliberately coarse.

**And it does not need a manifest field.** The fact is derived from the table's own bytes by the
classifier, and every live table is already open — so input selection asks the `Table`, not the
manifest, and D4 §5.1 point 2's "the manifest is never the sole authority for any number in it" holds
without a new number in it.

**At B3.5e, splitting keeps L1 a run.** An unbounded tombstone is split at each output-file boundary
and only the **last** output keeps the unbounded piece — which has no successor to overlap.

---

### 6.1a — THE BOUNDS RULE, DEVIATED FROM AND STRENGTHENED (B3.5b)

**§6.1's last refusal cannot be implemented as a refusal, and the test that induced it is what
showed that.** The rule reads: *a tombstone the table's own bounds do not admit is one no compaction
will read, which resurrects everything it was supposed to mask.* Correct, and load-bearing — it is
clause 2 of the drop claim showing up in the format.

**But `ValidateTable` DERIVES those bounds from the file.** So whatever it derives is admissible by
definition, and a refusal there can only fire when the classifier disagrees with itself. The first
implementation refused on data-only bounds — and immediately refused **every table the writer
produces**, because the writer widens its recorded bounds to admit its own tombstones and the
classifier did not. *A writer and a classifier disagreeing about the same fact is the exact failure
the observer-before-observed ordering exists to catch, and it caught it here in one test.*

> **THE REQUIREMENT IS ABOUT WHAT THE MANIFEST RECORDS, NOT ABOUT WHAT THE FILE CONTAINS.**

So it is enforced where it can be:

1. **`ValidateTable` derives `smallest`/`largest` INCLUDING the tombstones' bounds.**
2. **`VerifyTables` already refuses any Open where the manifest disagrees with that derivation** —
   D4 §5.1 point 2, unchanged and now doing this job too.

**Which is strictly stronger than the refusal §6.1 asked for.** A refusal catches a bad *file*; this
catches a bad *manifest entry*, which is the artifact compaction actually reads. There is no way to
record bounds that fail to admit a tombstone, because the number is not the manifest's to choose.

**The end bound is exclusive and is included anyway.** Over-covering costs a file that did not need
reading; under-covering resurrects data. The directions are not symmetric, so the safe one is taken.

**One consequence, named now and settled at B3.5e.** A tombstone's exclusive end widens `largest`,
so two adjacent L1 files could appear to overlap and `VerifyL1IsARun` would refuse the Open. It
cannot happen yet — L1 tombstones arrive only when compaction emits them — and the fix that belongs
with them is **splitting a tombstone at the output-file boundary**, which is where it lands.

**`TableFault::kTombstoneOutsideTheTableBounds` was removed rather than left unreachable**, on the
same rule that deleted `BM73`: a fault nothing can produce is a fault nobody can induce.

---

**What it changes elsewhere:**

- **`Apply` stops expanding**, so `DeleteRange` becomes `O(1)` in the range;
- **the read path must consult range tombstones** in every table it touches, before trusting a value;
- **compaction must merge and drop them by §1.2's rules** — a range tombstone is droppable when it
  covers nothing older outside the inputs, which is clause 2 over a range rather than a key.

**AND ONE OF THOSE THREE IS NOW KNOWN TO BE WRONG AS WRITTEN.** Retiring the expansion does *not*
retire `table.h`'s residency — it retires **`Apply`'s dependency on it**. A snapshot or iterator
outliving a compaction still reads through tables whose **files have been deleted**, and that works
only because the bytes are in memory. **"No longer required by X" is not "no longer required."**
That dependency is B3.6's subject, and until B3.6 replaces it with reference-counted file lifetime it
is the reason the deletion at install step 5 is safe.

---

### 6.1c — WHAT B3.5e COST, AND THE ONE THING THAT FOUND IT

**Two defects, in the same step, whose symptoms cancelled exactly.** Both are in `BUGS.md` as
`BUG-B004`/`BUG-B005`; what belongs here is the consequence for the design.

| | what it did | why no test saw it |
|---|---|---|
| **BUG-B004** | clause 1 tested the tombstone at the **top of the interval** instead of at each observable sequence | a snapshot below a range delete lost its version — invisible, because the snapshot holds the pre-compaction tables **resident** and reads through them |
| **BUG-B005** | the sink was told its tombstones **after** it had written its files | the header already said *"handed over BEFORE the first entry"* — the contract was written and then not met |

> **THE FIRST DROPPED THE VERSIONS THE SECOND FAILED TO HIDE.** Together they produced the right
> answer at every read. Separately, each is a data-loss bug.

**What found it was a MUTANT SURVIVING, and that is a different question from a test's.** A test asks
*is the answer right?*; a mutant asks *is this line load-bearing?* `BM97` blinded the L1 tombstone
lookup and nothing failed — **correctly**, because nothing reached it.

**It had already been held out once.** At B3.5d its workload did not exist, and it was deliberately
kept out of the lane rather than relabelled. Re-added at B3.5e it survived again, and *that* second
survival is what opened both bugs. **Relabelling it as "covered by the compaction tests" would have
been plausible, would have closed the file, and would have shipped both defects.**

**The design consequence: BUG-B004's shape is preserved as `BM105` rather than only fixed.** It is what
a careful reader reaches by *simplifying* — the tombstone test looks independent of `s`, so it gets
hoisted out of the loop. A fix without the class is a fix that gets undone by someone tidying.

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

## 7.1 B3-D7a — the compaction merge: its progress quantity and its correctness instrument, NAMED BEFORE THE LOOP

**Ansh's condition, answered before the loop is written**, because a loop whose only progress measure
is the thing under test is a loop whose termination cannot be checked — and finding that out while
designing it is worth more than finding it from a lane that hangs.

### The progress quantity, and why no cursor will do

Every loop CF-3 has met so far advances **one cursor**. The compaction merge does not: a single
iteration may **emit an entry, or drop one, or consume a range tombstone, or advance a source that
produces nothing at all** — so "the output key strictly advances" is false for a correct merge, and
"the input cursor strictly advances" is false whenever a version is dropped without being emitted.

> **THE HONEST QUANTITY IS `inputs_consumed` — a COUNT OF INPUT ENTRIES TAKEN FROM THE MERGE, which
> increases by exactly one per iteration and never decreases.**

Every iteration takes exactly one entry from the merged input and then decides what to do with it.
That is true whether the entry is emitted, dropped, or absorbed into a range tombstone's shadow.

### What bounds it, and why a correct compaction cannot reach the bound

**A bound a correct run can hit is a bound that will be raised** — so the bound is not a tuning
number, it is a fact about the inputs:

> **`inputs_consumed ≤ Σ entries(f) for f in the compaction's input files`**, a quantity **counted
> from the inputs before the merge starts**, using `ValidateTable`'s entry count — which is already
> computed when each table is opened and validated.

A correct compaction consumes each input entry **exactly once**, so it terminates at exactly the
bound and can never exceed it. Reaching the bound is not a failure; **exceeding it is**, and it can
only mean a source was rewound or an entry counted twice — the two ways a merge loops forever.

This is a **derived** bound, not a chosen one. There is no number to raise: raising it would require
claiming a table holds more entries than the classifier says it holds.

### The correctness instrument, named separately because GF-12 says it must be

The progress assertion bounds termination and says **nothing** about whether the output is right.
Two different questions, two different instruments:

| question | instrument |
|---|---|
| does the merge stop? | `inputs_consumed` against the derived bound, asserted in the loop |
| **is what may be dropped, dropped?** | **the drop adjudicator** — `AdjudicateDrops`, already built at B3.0, three directions |
| **do the survivors come out in the right ORDER with the right VALUES?** | **`CompactionOutput.IsTheMergeOfItsInputs`** — see below |

**The third row is the gap Ansh named, and nothing existing covers it.** The drop adjudicator works
over **sets** of `(user_key, seq)` — it is blind to ordering entirely, and blind to values. A merge
that emitted every required entry, in reverse order, with every value shifted by one position, would
satisfy all three of its directions.

> **`CompactionOutput.IsTheMergeOfItsInputs`**: the harness independently merges the input tables'
> entries — a plain sorted merge of what `ValidateTable` enumerated, with no drop rules applied —
> filters that sequence by the drop claim, and asserts the compaction's output is **exactly that
> sequence, in order, with matching values.**

It is a **stronger** check than the adjudicator and does not replace it: it can only run where the
harness knows both input and output files, which is a compaction in isolation. The adjudicator runs
against **any durable image**, including one produced by a crash mid-compaction, and that is where it
earns its place.

Both are written **before the merge**, per the ordering that has now paid four times.

---

## 7.2 B3-D7b — the watermark pin: an obligation the drop claim does not state

**Found before the loop ran, by asking what else a compaction touches.** The drop claim and the
durability promise are about different things, and only one of them is written down in §1.2:

> **THE DROP CLAIM IS ABOUT THE ANSWER A READER GETS. THE WATERMARK IS A PROMISE ABOUT A SEQUENCE.**
> A compaction can preserve every answer exactly and still destroy the engine's only proof of a
> promise it has already made.

**The mechanism, concretely.** `DB::Open` recomputes the durable floor as the **maximum `largest_seq`
over the live tables**, and `largest_seq` is re-derived from each table's own bytes. It has to be:
D7's forward binding says *the manifest may never record a durable sequence*, so there is nowhere
else for that number to come from. Now suppose the highest-sequenced entry in the database is a
**tombstone with nothing left to mask** — precisely what §1.2a established a compaction *may* drop.
Dropping it lowers the maximum, and after a restart `DurableSeq` reports a **smaller** number than it
reported before. The frozen contract says monotone non-decreasing.

**The fix, and its price.** Exactly one entry at the inputs' highest sequence is kept when the rules
would have dropped them all. It is **over-keeping**, which the claim explicitly permits — *may drop*,
never *must* — and it costs at most one entry per compaction.

**`BM77` is the mutant, and what makes it worth planting is that THE MUTANT IS THE CORRECT
IMPLEMENTATION OF THE DROP CLAIM.** It is not a typo or a weakened check; it is what a careful reader
of §1.2 would write. That is the shape of blind spot the mutant suite exists for.

---

## 7.3 B3-D3a — the output is a RUN, not a file

**Discovered while implementing (b) and reported rather than absorbed.** A compaction that writes one
output file makes L1 a single file, and then:

> **EVERY COMPACTION REWRITES THE WHOLE DATABASE — WHICH IS CANDIDATE (a), WEARING (b)'s NAME.**

§3 rejected (a) *not for being slow but for making the measurement meaningless*. So a one-file L1
would have made §8's write-amplification number a fact about the workload's size, and the phase's
recorded number would have been the number of the policy the phase rejected.

**The size cap is derived, not chosen.** An output file is capped at the **flush threshold**
(`caps.flush_bytes`), so an L1 file is the same order as the L0 file that produced it. That ties it
to a number that already has a derivation rather than inventing a second one, and it **moves with the
caps** — the crash sweep, which sets the flush threshold low so that flushes are reachable in a short
run, gets a multi-file L1 for free instead of needing a second knob.

**One rule on the roll: a sink may roll ONLY at a user key boundary.** Two files of one run that
share a user key are not a run; `L1FileFor` would find one of them and the other's versions would be
unreachable — a deletion that stops hiding a value. `BM79` plants the size-only roll, which is the
obvious implementation.

---

## 7.4 What the induction found, and the two mutants that survived first

**`BM73` was deleted rather than re-aimed, and that is GF-7 for the second time.** It removed
`L1FileFor`'s check that the file found by the binary search actually *contains* the key. **Nothing
failed** — a key falling in the gap between two files of the run makes the search return the next
file along, whose `Get` cannot find the key either, so the answer is identical and only a filter
probe is wasted. The line is a **cost guard, not a correctness one**, and the comment now says so.

The property *"a range test decides containment"* **is** load-bearing — in the compaction's **input
selection**, where getting it wrong resurrects deleted data — and `BM80` is the mutant that says so.
*The mutant went where the property lives instead of where it merely appears.*

**`BM76` and `BM79` survived their first induction, and both for reasons worth keeping:**

| mutant | why it survived | what the fixture was actually watching |
|---|---|---|
| `BM76` | the tombstone sat at the **top sequence**, where §7.2's pin keeps it for an unrelated reason | the watermark pin, called the drop rule |
| `BM79` | with no live snapshots the drop rule leaves **one version per key**, so no key is ever large enough to span a roll | a situation that could not occur |

> **A MUTANT THAT SURVIVES BECAUSE ITS PRECONDITION IS UNREACHABLE IS NOT A WEAK MUTANT; IT IS A
> WORKLOAD THE SUITE NEVER RAN.** `BM79`'s test now holds forty snapshots to make the situation exist.

---

## 7.5 THE SCOPE OF THE CURRENT GREEN — a statement, not a to-do

**READ THIS AS A BOUNDARY ON WHAT THE LANES CURRENTLY ESTABLISH, NOT AS AN ITEM TO BE TICKED OFF.**
It is the sentence that keeps a green lane from being quoted as something it does not say. It is
**rewritten when a lane's reach changes** — never deleted because someone got to it.

### 7.5.1 What it said until B3.7, kept for the record

> *"The compaction install ordering is ARGUED and NOT YET SWEPT... the `flush` regime's workload
> crosses the flush threshold once, so `|L0| = 1` and the trigger is 4. Every Env call the compaction
> makes is therefore invisible to the lane today."*

**That is no longer true, and the replacement is below rather than the section being emptied.**

### 7.5.2 What the lanes establish now

**The compaction install ordering IS swept.** A third regime, `compact`, drives the workload past the
L0 trigger — **3545 kill points, 0 violations**.

**AND IT HOLDS A SNAPSHOT ACROSS THE COMPACTION, WHICH IS A DESIGN DECISION AND NOT A DETAIL.**
Without a live reader the drop rule reclaims everything and the input files retire **inside** the
compaction — the short path, in which `RetireTable` and `DropObsolete` run back to back and the
reference count never has to hold anything. With one, the inputs go on the obsolete list and are
collected by a **later** call, which is the path where a crash can land between the two.

> **A WORKLOAD CHOSEN TO REACH THE HARDER HALF IS A DESIGN DECISION.** Left to the obvious workload,
> the sweep would have covered B3.6's file lifetime only where it is trivially correct, and reported
> 3545 green points for it.

**And its POWER is measured, which is the half that makes the green mean anything.** `BM109` removes
the directory sync after the compaction's output files: **10 detections of 3530, first at kill point
663.** A sweep that visits a path proves the engine recovers there; it says nothing about whether a
defect there would be *detected*. Now it does.

**THE REGIME WAS ADDED RATHER THAN `flush` GROWN, AND THE REASON IS §8.2a's.** Reaching the trigger
needs four flushes — about four times the `flush` regime's whole workload — so folding it in would
have multiplied that regime's kill-point count, **the denominator of every rate in `FLOORS.txt`**, and
diluted every B2 class measured against it for no reason but that one workload reached two paths.
**`default` and `flush` are byte-identical to before: 305 and 990, unchanged, and no floor moved.**

### 7.5.3 What the lanes still do NOT establish

- **No class is measured at `compact` except `BM109`.** The regime's power is established *at the
  install ordering* and nowhere else in it; a defect in the merge or the drop rules would be caught
  by `cpp-test`, not here, and the `covered-by:` labels say which.
- **The `compact` workload holds ONE snapshot across ONE compaction.** Concurrent readers across
  several compactions, and a reader whose snapshot outlives two generations of obsolete files, are
  covered by unit tests and not by a kill-point sweep.
- **Amplification is measured at v1's scale only** — see §8.2b, and the pre-declared outcome that
  applies if that scale sits below the crossing point.

---

## 7.6 A DEFECT IN SIGNED WORK, REPORTED RATHER THAN ABSORBED

**`Flush`'s early return on `imm_ != nullptr` does not serialise flushes.** `imm_` is set several
steps *after* the first `AppendGroup`, so two concurrent `Sync` calls both pass the guard. It is B2
code, and it has always looked like a serialiser without being one.

**It was harmless until B3.4 and is not any more.** Until this step the manifest had **one** appender.
Compaction is the second, and `Manifest::AppendGroup` takes no lock — so two maintenance paths
appending at once would interleave records inside one another's groups and write a manifest no reader
can replay.

**WHAT WAS NOT DONE, AND WHY.** Not a lock, and not a third TSan pattern. The contract is
single-caller and says so twice — `db.h`'s *"B5's poller owns this"*, and the TSan harness in the
strongest form available:

> *"One writer and one syncer, matching the shape the frozen interface forces... **Not more, because
> more would be a claim the contract does not make.**"*

Adding a lock and a pattern for two Syncs would test behaviour the contract does not promise and
would quietly convert a precondition into a supported mode. **So the precondition is enforced instead
of widened**: `SingleCaller` in `DB::Sync`, and a second concurrent caller aborts at the mistake
rather than leaving a corrupt manifest for the next Open to refuse.

**Induced in both directions** (GF-14) — `SyncPrecondition.ASecondConcurrentClaimAborts` and
`...SequentialClaimsAreFine` — deterministically, by claiming the guard twice rather than by racing
two real `Sync`s, which would induce it only *probably*. `BM82` is the class.

**What is left for Ansh:** whether B2's misleading guard also earns its own `BUGS.md` entry, given it
was signed with the defect latent and harmless. The guard's comment now says what it is (*a flush is
a no-op while one is pending*) rather than what it looked like.

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

### 8.1 THE MEASUREMENT DESIGN, WRITTEN BEFORE ANY CANDIDATE RUNS

Ansh's condition, and it is the whole reason this subsection exists ahead of the numbers:

> **A6 says the simplest correct policy wins v1 and the measurement chooses it — WHICH MEANS THE
> MEASUREMENT MUST BE CAPABLE OF RETURNING "THE SIMPLE ONE IS ADEQUATE."** That outcome is designed
> in and stated in advance, so it cannot be reached by accident and cannot be avoided by sizing the
> benchmark until it discriminates.

#### The thresholds, as numbers, with their derivations

`F` is the flush threshold (4 MiB, `caps.h`), `K` the L0 compaction trigger (4, `db.cc`), `D` the
live data size.

**Write amplification.** Each compaction ingests `K·F` bytes of L0 and rewrites every L1 file its
inputs overlap. **Under uniform-random keys every L1 file overlaps**, so a compaction rewrites all of
L1 — about `D` bytes — for every `K·F` bytes ingested. Adding the WAL copy and the flush copy:

```
    WA  ≈  1 (WAL) + 1 (flush) + D / (K·F)
```

§3 already fixed the reopening threshold at **WA > 10×**. Solving:

```
    D / (K·F) > 8      ⇒      D > 8 × 16 MiB  =  128 MiB
```

> **THE NUMBER: (b) IS ADEQUATE UP TO ABOUT 128 MiB OF LIVE DATA PER ENGINE, AND CROSSES 10× ABOVE
> IT.** Stated now, in advance, with the arithmetic visible so it can be attacked.

**Read amplification.** The structural bound is `|L0| + 1`, and `|L0| ≤ K` at steady state, so
`≤ 5`. The measured number should be **below** it, because the bloom filter is what makes an absent
key cost a probe rather than a block read. The threshold is not the structural bound but whether the
structure holds:

> **IF THE STEADY-STATE L0 FILE COUNT EXCEEDS `K` AT ANY POINT UNDER SUSTAINED WRITES, COMPACTION IS
> NOT KEEPING UP** and read amplification is above its bound for a reason no bloom filter fixes.

**Space amplification.** One copy in L1 plus L0's overlap, so the bound is `1 + K·F/D`, which is
**≤ 2× for any `D ≥ K·F`** — 16 MiB. Above 2× at a data size past that means the drop rules are not
reclaiming, which is §2.3's vacuous case showing up as a ratio.

#### The workload that would make (b) inadequate, named in advance

Sustained writes against a live set **substantially larger than 128 MiB per engine**, with keys
spread widely enough that every compaction overlaps most of L1. Narrow the key spread and (b) gets
better, not worse: input selection reads only the overlapping part of the run, which is the whole of
what (b) buys over (a) (§7.3, and `Compact.ASecondCompactionRewritesOnlyTheOverlappingPartOfTheRun`
is the assertion that it is real).

#### What is run

Three data sizes **spanning the predicted crossing point** — below it, at it, above it — because a
single point cannot distinguish "the model is right" from "the number happened to land there". The
deliverable is the **curve and the crossing point**, not one figure. Each run records WA, RA and SA
by the definitions in the table above, at fixed `F` and `K`, on the fillrandom workload B5 defines.

#### THE PRE-DECLARED HONEST OUTCOME

**Whether Rift produces the inadequate workload is not yet a fact.** It depends on A4's range-split
threshold and on how many ranges a store hosts — neither decided — and on I2's benchmark
configuration, which is a choice rather than a measurement.

> **IF I2 RUNS BELOW THE CROSSING POINT, THE MEASUREMENT CANNOT DISCRIMINATE, AND (b) WINS ON A6's
> RULE RATHER THAN ON A BENCHMARK.** That is a legitimate result and it is recorded as one: *"the
> question is not decidable on the evidence at v1's scale; the crossing point is 128 MiB and the
> measured curve is consistent with the model."*

It is stated here, before the first run, for the reason Ansh gave: **a benchmark sized until it
discriminates is not evidence, it is a decision already made.** The crossing point is fixed now, so
the only honest ways to reach "(c) is needed" are to measure past it or to show the arithmetic wrong.

---

## 8.2 B3.7's TWO OBLIGATIONS, WRITTEN BEFORE EITHER IS RUN

**They are separate work with one property in common: each moves a number that something else is
compared against, so each carries the thing it invalidates in the same diff.**

### 8.2a The sweep extension, and why the floors re-measure in the same change

§7.5 states the boundary: **compaction has no kill-point coverage.** The `flush` regime crosses the
threshold once, so `|L0| = 1` against a trigger of 4, and every Env call the compaction makes is
invisible to the lane.

Extending the workload past the trigger adds kill points — **and the kill-point count is the
denominator of every rate in `FLOORS.txt`.**

> **B2 ALREADY PAID FOR THIS LESSON AND IT IS NOT PAID TWICE.** The manifest took the count from 175
> to 300; **every rate fell and not one detection count did.** A lane that failed the build on that
> would have reported arithmetic as a regression, and a maintainer lowering the rate floors in
> response would have lowered them for the wrong reason and lost the bound for the right one.

**So the extension and the re-measurement are ONE DIFF, and the ordering inside it is fixed:**

1. extend the workload, and **record the new kill-point count per regime**;
2. re-measure **every** class the sweep reaches — not only the ones that look affected, because the
   denominator moved for all of them;
3. update `FLOORS.txt`'s rates in the same commit, with the new count stated in the header;
4. **detection COUNTS and CEILINGS are compared against the old ones**, because those are the two
   bounds the denominator does not touch (`GF-6`). A count that fell is a real regression; a rate
   that fell alone is arithmetic.

**A third regime is likely and is decided by measurement, not in advance.** If reaching the trigger
requires so much workload that the `flush` regime's own kill points are diluted past usefulness, the
compaction path gets its own regime rather than sharing one — and §8.4's rule applies unchanged: a
number measured at non-default caps never aggregates with a default-cap number.

### 8.2b The amplification measurement, and the outcome that is not "try harder"

> **RUN, B3.7b. The measurement reached the crossing point and answered: 8.08 < 10, so `(b)` wins on
> evidence.** The pre-declared fallback below was NOT reached — recorded because a rule that is only
> ever quoted when it is convenient is not a rule, and this one held while it cost nothing.

§8.1 fixed the threshold **before any candidate ran**: `(b)` crosses 10× write amplification at
**≈128 MiB** of live data, from `WA ≈ 2 + D/(K·F)` with `K·F = 16 MiB`. Three data sizes spanning the
crossing point; the deliverable is the curve and where it crosses.

**AND THE PRE-DECLARED OUTCOME STANDS EXACTLY AS WRITTEN:**

> **IF v1 RUNS BELOW THE CROSSING POINT, THE QUESTION IS NOT DECIDABLE ON EVIDENCE, AND `(b)` WINS ON
> A6's RULE RATHER THAN ON A BENCHMARK.** That is a **result**, recorded as one.

**What it must not be allowed to become.** A measurement that comes back below the threshold is
**not** an inconclusive one to be retried at a larger size until it discriminates:

| the honest reading | the reading to refuse |
|---|---|
| *"at v1's scale the policies are not distinguishable; the crossing point is 128 MiB; `(b)` wins on A6"* | *"inconclusive — run it bigger"* |

**Because "run it bigger" is how a benchmark gets sized until it says something**, which is the thing
§8.1 exists to prevent, and the reason the number was fixed in advance. **The only honest ways to
reach "(c) is needed" are to measure past the crossing point on a workload Rift actually produces, or
to show the arithmetic wrong.** Neither is "the first answer was boring."

**Amendment A4's vocabulary applies and settles it.** `inconclusive` is a real outcome in this
project — a checker that hit its timeout — and it means *the instrument did not finish*. A
measurement that finished and returned a number below a threshold is a **pass with a recorded value**,
not an inconclusive. Calling it inconclusive would be the second thing A4 forbids: quietly counting
something that did not conclude, in the opposite direction.

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
| *B3.3b, folded in* | `level` in the manifest, the L1 run check at Open, the two-level read path | needed before anything could compact into L1 |
| *B3-D3a, folded in* | the output run, capped at the flush threshold | without it §8 would measure the policy §3 rejected |
| *B3-D7b, folded in* | the watermark pin, and the snapshot registry the drop rule needs | both are correctness the step cannot be right without |
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

**B3-Q2 — RULED, 2026-08-26: `S` IS THE LIVE SNAPSHOTS ONLY.** A retired snapshot has no reader that
can observe the version it pinned, so keeping that version required would make compaction unable to
reclaim space **nothing can see** — which is `keep(k)`'s over-requirement (§1.2a) arriving a second
time from a different direction.

**AND THE RULING CAME WITH THE CONDITION THAT MAKES IT A FACT RATHER THAN A PARAGRAPH.** `S` moves
under a running compaction, and §1.3 argued both directions safe. The half that matters is not an
argument at all — it is `pin_seq ≤ max(S)`, **asserted in `DoCompact`**:

> Every sequence the inputs hold is at or below the visible sequence already in `S`. Given that, for
> any later `s`, `keep(k)` at `s` is *the newest version of k in the inputs* — **which the visible
> sequence already required.** A snapshot taken mid-compaction requires nothing new.

It holds today because tables carry only flushed data while `seq_` runs ahead of them, and it stops
holding the day a compaction takes the immutable memtable as an input. **That is a change someone
will propose, which is why it is a `RIFT_CHECK` and not a sentence.** `BM83` and the deleted `BM84`
are the two mutants that bound the reading; §1.3 carries the full record.

**B3-Q4 — RULED (b), 2026-08-26: the sentinel `end_len == 0xFFFFFFFF`, unboundedness in the bytes
rather than in a convention.** Landed at B3.5b with three conditions, all met — its own induced
refusal (a record claiming the sentinel while carrying end bytes), `end > start` restated at the code
as a rule about **finite** ends so the sentinel reads as a different case and not an exception, and
fixtures for the unbounded form specifically, because every existing refusal was induced against
finite ends and this is a shape they have never seen. Retained below as raised.

**The frozen `Engine` interface requires a range deletion the frozen range-tombstone format cannot
express.**

| what | where | says |
|---|---|---|
| `DeleteRange(Bound, Bound)` | A0.5's frozen interface, `db.h` divergence 3 | either bound may be **UNBOUNDED**, and `DeleteRange(Unbounded, Unbounded)` is §8.2's clear-everything case — *"the clear half of snapshot application's clear-then-ingest, the case Amendment A3 was ruled for"* |
| the range-tombstone block | §6.1, frozen at B3.2, every refusal induced | `start_len:u32 \|\| start \|\| end_len:u32 \|\| end \|\| tag:u64`, and the classifier **refuses `end ≤ start` bytewise** |

> **THERE IS NO BYTE STRING GREATER THAN EVERY BYTE STRING, SO `[start, ∞)` HAS NO REPRESENTATION IN
> THE BLOCK — AND `(−∞, ∞)` IS EXACTLY THE CASE `[A3]` PUT `DeleteRange` IN THE INTERFACE FOR.**

**Only the END is affected.** An unbounded *start* is expressible as `""`: the empty user key is the
minimum, so `["", end)` and `[unbounded, end)` cover the same set. The problem is one-sided.

**Why B2 did not have it, and why that is the interesting part.** B2's `DeleteRange` expanded to one
point delete per live key, so "unbounded" never needed an encoding — **the expansion was hiding the
gap**, and `[A3]` requires the expansion retired at B3. The two halves are each internally consistent
and were never checked against each other: §6.1 specified the format from the *block's* point of
view and induced every refusal against hand-built bytes, an exercise that never touches `Bound`
because the classifier never sees one. **That is `GF-15` again — a rule derived from one contract is
not permission under the others — arriving between the frozen `Engine` interface and a format frozen
inside this phase, which is precisely what `CF-4` exists to sweep for at B4.**

**Candidates.**

**(a) A flag bit in the tag.** The tag's low byte is the `ValueType` and has spare bits; one could
mean *end is unbounded*. **Rejected as a recommendation:** §6.1 says a range tombstone is *"a version
like any other, ordered against point versions by the same comparator"*, and a tag that is no longer
a plain internal-key tag gives that up for a bit of space.

**(b) A sentinel `end_len`.** `end_len == 0xFFFFFFFF` means unbounded above, and `end` then occupies
zero bytes. `end > start` stays a rule about *finite* ends; one refusal is added — a sentinel
`end_len` with a non-empty `end`. Self-describing in the bytes, no change to the tag's meaning, and
every existing refusal keeps its force.

**(c) Keep the expansion for unbounded-end ranges only.** No format change, and it reintroduces
`O(live keys)` **exactly at the clear-everything case** — the one `[A3]` named as the reason
`DeleteRange` is in the interface at all. Strictly worse than (b) at the only place it matters.

**(d) Change the frozen interface to require a finite end.** Not mine to propose as a fix: it breaks
`[A3]`'s stated rationale, and A0.5's freeze is the thing both tracks build against.

**Recommendation: (b).** Smallest change that leaves every induced refusal meaningful, keeps the tag
a tag, and puts the unboundedness in the bytes rather than in a convention a reader has to know.

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
| **B3-D2c** | `survived ⊆ submitted` | a decision, not defensive clutter: the only thing between a fabricating reader and a green drop verdict, and the direction two assertions pointed the same way could not see (GF-10) |
| **B3-D2b** | `required`'s source | the harness's submission log, never the engine's files. Permission to read the artifact is not permission to derive the expectation from it |
| **B3-D2a** | the artifact/belief boundary | mechanical: `ARTIFACTS.txt`, `ORACLES.txt`, and the mark — *an artifact header declares nothing taking an `Env*` and nothing taking a snapshot*. Splits `manifest_format.h` out of `manifest.h` |
| **B3-D3** | compaction policy | **two levels**, L0 + L1; (a) rejected for making the measurement meaningless, (c) deferred as A6's STRETCH with a numeric threshold to reopen |
| **B3-D4** | read path | `ConcatenatingIter` for L1 so the merge's `k` stays small |
| **B3-D5** | snapshots | refcount + a sequence floor read once at compaction start |
| **B3-D6** | range tombstones | a dedicated block per SSTable; retires `Apply`'s expansion and `table.h`'s residency |
| **B3-D7** | iterators | CF-3 applied to every loop the phase adds; bounded work counters where progress is not monotone in the key |
| **B3-D7a** | the merge's two instruments | `inputs_consumed` against a **derived** bound for termination; `AdjudicateMerge` and `AdjudicateDrops` for correctness. Named before the loop |
| **B3-D7b** | the watermark pin | one entry at the inputs' highest sequence is kept whatever the drop claim permits: the claim is about the ANSWER, the watermark is a PROMISE ABOUT A SEQUENCE (GF-15) |
| **B3-D3a** | the output is a run | output files capped at the flush threshold. One output file would be candidate (a) wearing (b)'s name, and would have made §8's number a fact about the workload's size |
| **B3-D8** | measurement | space, read **and** write amplification; a space-amplification ceiling is the vacuous-compaction guard |
| **B3-D8a** | the adequacy threshold | **stated before any candidate runs**: (b) crosses 10× write amplification at about **128 MiB** of live data. If v1 runs below it the question is **not decidable on evidence** and (b) wins on A6's rule — declared in advance so it cannot be reached by accident |
| **B3-D9** | landing sequence | observer first, CF-2 second, format third, structure, compaction, tombstones, snapshots, power |

| id | question | my reading |
|---|---|---|
| **B3-Q1** | how the drop checker reads surviving bytes | **RULED**: (a), with the rule corrected to *parse artifacts, never consult beliefs*, made mechanical and conditioned on two aliasing mutants |
| **B3-Q2** | does `S` mean live snapshots only | **NOT RULED.** Proceeding on *yes*; flagged in §11 |
| **B3-Q3** | must the amplification numbers be competitive or merely real | **RATIFIED**: real |

**Nothing in B3 is written until this is ruled on.**
