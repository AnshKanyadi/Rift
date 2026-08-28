# BUGS.md

Every bug caught by the simulator, the crash-consistency rig, or differential testing gets an entry
here. This file is the proof behind the verification claim: it is the difference between "we ran a
lot of tests" and "we can show you what the tests found and why they found it."

**Rules**

1. Every bug found by a checker gets an entry. No exceptions for embarrassing ones — especially not
   for embarrassing ones.
2. Every entry must be reproducible: a seed (at the commit that contained the bug) **and** a plan
   bundle in `seeds/` (reproducible at any commit).
3. Every entry must answer **"which mutant class would have caught this?"** If none would have, a new
   mutant is added to `sim/toy/mutants` **in the same PR as the fix** — not as a follow-up.
   *(CLAUDE.md Amendment A2.)*
4. A bug that a checker did *not* catch — found by inspection, by a real-mode run, or by luck — is
   the most valuable entry in the file. It must additionally record what checker was missing and
   whether one was added.

**Counts:** 2 entries (engine bugs; the fenced harness-defect section below is counted separately and does not satisfy this gate). *(Both are Track B's, found by the kill-point sweep at B2. Track A's A1 gate is a separate obligation and is unaffected by them.)*

---

## Template

Copy this block. Do not drop fields; write "n/a" with a reason instead.

```markdown
### BUG-NNN — <one-line symptom, in the voice of what an operator would see>

| field | value |
|---|---|
| **Found by** | sim / crash rig / differential / real-mode chaos / inspection |
| **Phase** | A1 |
| **Reproduce (seed)** | `simctl replay 8834127` at commit `<sha>` |
| **Reproduce (plan)** | `simctl run --plan seeds/BUG-NNN/plan.json` (any commit) |
| **Invariant that caught it** | e.g. Election Safety |
| **Mutant class** | e.g. `M2-ack-before-replicate`; or "none existed — added `M8-…` in this PR" |
| **Fix commit** | `<sha>` |
| **Minimized?** | yes — N fault entries, M ops, K nodes |

**Symptom.** What the checker reported, verbatim where possible.

**Root cause.** The actual mechanism, not the patch. Written so someone who has never seen this code
can follow it.

**Why the checkers caught it here and not earlier.** Which injector had to fire, in what order.

**What this would have caused in production.** Be concrete and be honest: silent data loss, a stalled
range, a lost acknowledged write, a duplicated transfer. If the answer is "nothing user-visible," say
that.

**Fix.** What changed and why that is the right fix rather than a narrower one that would also make
the seed pass.
```

---

## FOR THE TRACK B WRAP-UP — marked by Ansh at sign-off

**Not the largest work of the phase; the best.** Recorded here so a wrap-up written months later does
not have to rediscover which findings carried weight.

**1. `BUG-004`/`BUG-005` and `GF-22` — two defects whose symptoms cancelled.** Invisible to every test
that asserts an **answer**, because the answer was right — arrived at by two errors that annihilate.
Separated only because **a mutant survived**, asking a question a test cannot: *is this line
load-bearing?* What makes it a demonstrated class rather than a hypothesis is the **asymmetric
evidence**: fixing `BUG-004` alone turned **four passing tests red**; fixing `BUG-005` alone would
have changed **nothing observable**. And `BM97`'s history is the citation for **a plausible label
being the dangerous one** — held out once when its workload did not exist, re-added rather than
relabelled, and the second survival is what opened both bugs. *"Covered by the compaction tests"*
would have been accepted by any reviewer.

**2. The observer-before-the-observed ordering, used eight times — and the two times it corrected the
SPECIFICATION rather than the code.** `keep(k)` over-requiring at B3.0, found by building the
adjudicator before any compaction existed; and the range-deletion semantics at B3.5a, fixed in the
model before a writer existed. **Both corrections were in the claim, not in an implementation**, which
is not the benefit the ordering is usually argued for. It is usually defended as *"a checker written
afterwards agrees with the implementation"*. Its actual yield here was **twice catching a wrong
specification before anything was built to it** — and once, at `HARNESS-020`, catching the author.

**3. B4's answer to why a sweep AND a differential both exist, as a measurement rather than a claim.**
With `BUG-006` present in the tree: **377 tests passed, 4,840 kill points across three sweep regimes
reported zero violations, and 147 mutant classes were all killed.** **Eight clean differential runs
found it** — no crash schedule involved. No sweep workload ever produced a tombstone ending exactly on
the largest key, so no kill point could reach it.

> **A FAULT-INJECTION SWEEP AND A DIFFERENTIAL FIND DIFFERENT THINGS**, and this is the number that
> shows it rather than the sentence that asserts it.

**4. `GF-23` gaining a same-day instance.** *A remedy that is written down rather than built has the
defect's own shape and comes due on the defect's own schedule.* Written in the morning after
`HARNESS-019` cost a step's work; **broken by its own author that afternoon** — a `FLOORS.txt` row
containing `O(|S|)`, two delimiters — and **caught by a mechanism rather than by recall**, because
`HARNESS-017`'s remedy had been built into `cpp-scan` rather than merely recorded. The shortest
possible demonstration of the rule, arriving as its own evidence.

**5. FOUR RIGS, FOUR REAL DEFECTS FOUND ON THE FIRST OUTING. This is the number the wrap-up should
quote.** Marked at B5's close on Ansh's instruction, because it stopped being a run of luck somewhere
around the third one.

| rig | first outing | what it found |
|---|---|---|
| the **kill-point sweep** | its first swept regime | the three `HARNESS-006`-class classifier defects, each of which blamed the engine for the harness |
| the **crash-consistency rig** | its first sweep across the write path | `BUG-001`, a database that had crashed once and refused to open ever again |
| the **B4 differential** | its first outing, `compact` seed 6, a **clean** run with no kill | `BUG-006`, a table the engine wrote and could never open again — plus three defects in *itself* first |
| the **B5.2 parity suite** | the first run of its first version | `BUG-007`, a value larger than the block buffer unreadable through the wrapper |

**WHAT THE PATTERN ACTUALLY SAYS, and it is not that the rigs are good.** Every one of these defects
was present in code that had already passed a lane. They were not found by running the existing
instruments harder; they were found by asking a question **nobody had asked once**. A first outing is
the only moment an instrument is guaranteed to be asking something new — after that it is regression
coverage, and its yield falls to whatever the next edit breaks.

> **THE YIELD IS IN THE QUESTION, NOT THE INSTRUMENT. A NEW RIG'S FIRST RUN IS THE HIGHEST-VALUE RUN
> IT WILL EVER HAVE, AND A PHASE THAT ADDS NO NEW QUESTION IS A PHASE THAT WILL FIND NOTHING NO MATTER
> HOW LONG ITS LANES RUN.**

The operational reading for I1 and I2: budget for the first run of every new instrument to **fail**,
and treat a new rig that comes up green on its first outing as a claim about the rig — `B4` §8's
expectation, now with four data points behind it. It has also gone the other way once, which is why
the claim is about the *question* and not the rig: the differential found **three defects in itself**
before it found one in the engine, and `GF-29`/`GF-30` are that lesson.

---

## Entries

Counts: 2 entries. Both were found by `make cpp-sweep` — the kill-point sweep — in the cycle that
wrote the code, before either reached a commit anyone else would have built. Neither is a bug in
signed work; both are recorded because rule 1 makes no exception for a defect a checker caught early,
and because **what they have in common is worth more than either**: an ordering argument that was
right about the state it named and wrong about the state it did not.

### BUG-001 — a database that had crashed once refused to open ever again

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, first run in the default regime: **41 violations of 300 kill points** |
| **Phase** | B2.4, found at B2.6 — before the code was ever run outside a lane |
| **Reproduce** | `rift_sweep default` at the commit before the fix; every kill from ordinal 26 onward |
| **Invariant that caught it** | the exactness oracle, via `reopen failed: db/000002.log: named by the manifest and absent` |
| **Mutant class** | **BM59-wal-named-before-it-exists**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** After any crash between naming a WAL in the manifest and creating the file, every later
`Open` fails with *"named by the manifest and absent"* — permanently. The database is intact; it
simply cannot be opened again.

**Root cause.** B2-D5 replaces B1's gapless numbering check with *"every file number the manifest
names exists"*. Naming a WAL **before** creating it looks like the safer order — it guarantees no
file exists that the manifest has not heard of — and a crash in that window leaves **a name with no
file**. That name is durable. It persists into every subsequent manifest, so `named and absent`
stops meaning `lost directory entry` from that moment on, and the only available repair is an
exception ("the highest named number may be absent") that then has to be justified at every Open
forever after — and which the sweep also refused, because a *second* crash in the same window makes
the previously-absent name no longer the highest.

**The fix inverts the order.** A WAL is **created, then named, then written to**. A crash between
creation and naming leaves an *empty unnamed file*, which carries nothing and is deleted. Both halves
of the rule then hold with **no exception in either direction**: every named WAL exists, and nothing
in an unnamed one is above what the tables cover.

**What it would have caused in production.** Total unavailability after an ordinary power cut, on a
database with no data loss and no corruption. The worst kind: nothing to recover, nothing to
diagnose, and the engine insisting it was right.

**The general form.** *An ordering that looks safer because it prevents the state you can name may
create the state you cannot.* Both orders leave a crash window; the question is not which window is
smaller but **which one closes itself**. Create-then-name leaves a file that provably carries
nothing; name-then-create leaves a durable claim that nothing can ever discharge.

---

### BUG-002 — recovery would have discarded committed records in a WAL a flush had retired

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, flush regime: **64 violations of 985 kill points** |
| **Phase** | B2.4, found at B2.6 |
| **Reproduce** | `rift_sweep flush` at the commit before the fix; every kill from ordinal 149 onward |
| **Invariant that caught it** | the exactness oracle, via `db/000002.log: present, not named by the manifest, and holding 46 committed batches` |
| **Mutant class** | **BM62-unnamed-wal-unchecked**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** After a crash between the manifest edit that retires a WAL and the deletion of the file,
`Open` refuses — reporting a WAL that is *supposed* to be there.

**Root cause.** BUG-001's fix rested on an argument: *a present unnamed WAL is one caught between
creation and naming, so it is empty*. That is true of one of the two ways a WAL can be unnamed. The
other is a flush: the manifest drops a WAL's name in the same group that adds the table covering it,
so between that group and the file's deletion there is an unnamed WAL **full of records**. The
emptiness check was an assumption about how the state arose rather than a statement about the state.

**The fix states the property instead of the provenance.** *Nothing in an unnamed WAL may be above
what the SSTables cover.* An empty one satisfies it trivially; a retired one satisfies it because the
table covers it; and a WAL whose records nobody covers — the case worth refusing — still fails it.
This is B2-Q1's **"nothing covered twice"** seen from the file side, and it is a strictly stronger
check than the one it replaced.

**What it would have caused in production.** The same total unavailability as BUG-001, in a narrower
window — and the check that produced it was the one added to *prevent* silent loss, which is the
shape worth noticing: **a safety rule stated in terms of how a state arises rather than what it
contains will refuse legitimate states the moment a second path reaches the same state.**


---

## Harness and lane defects

**This section is not the entry list above and does not substitute for it.** Entries here are defects
in *checkers* — lanes, rigs, mutation harnesses — found by other checkers. They are recorded because
rule 1 says every bug a checker finds gets an entry and makes no exception for embarrassing ones, and
they are fenced off because they are not engine bugs:

- they do **not** count toward the A1 phase gate's "BUGS.md is nonempty" requirement, which is about
  the simulator finding bugs in the *protocol*;
- they do **not** count toward the `[K] documented postmortems` figure in CLAUDE.md's resume lines,
  which is about faults injected into a running system;
- they **do** count as evidence that the induced-failure discipline works, which is the only reason
  either of these was visible at all.

Counts: 14 entries.

### The two general forms these entries taught

Recorded here rather than only inside the entries, because the instances are
cheap and the forms are not.

**GF-1 — a lane that verifies an ABSENCE must run in a state where the thing
could actually be present.** From HARNESS-002 and HARNESS-004. An absence
verified in a state that could not have contained the thing is not a
verification, it is a tautology wearing a lane's clothes. `cpp-ci` claims no
lane touches the network; it was resting on a warm FetchContent cache rather
than on the absence of a fetch, and the isolation it did have worked perfectly
and had nothing to block. Track A has hit the cousin of this twice. The cold
cache is now part of what `cpp-ci` MEANS and is asserted at both ends —
`scripts/cpp-cold-cache.sh`, induced by `COLD-fetch-despite-isolation`.

**GF-5 — AN ACCIDENTAL DEFENCE IS WORSE THAN A MISSING ONE.** From HARNESS-010,
and new to both tracks.

A missing defence measures as missing. An **accidental** one — a property that
holds for a reason nobody chose, in a component that was not trying to provide
it — makes a real gap **measure as covered**, and then removes itself on a
schedule nobody is tracking.

The instance: recovery applied records it never committed, and they were
invisible only because every read went through a snapshot pinned at the
recovered watermark. Correct state, correct lanes, correct counts — and the
whole sweep reported 175 passes on an engine with a live recovery defect. The
read path was not defending anything; it simply had no way to show the damage.
**And it expires at B2**, where the flush writes the memtable out and those
records become durable, visible and permanent.

Two obligations follow, and the second is the one that is easy to skip:

1. When a defence is found to be accidental, say so where it is measured — a
   floor derived from an accident is a floor measuring the accident.
2. **Put its expiry in `CARRY-FORWARD.md` as a dated obligation, not a note.**
   An accidental defence has a date, and the date is the phase that removes it.
   If nobody is holding that date, the gap reopens silently and the measurement
   that would have caught it is the one the accident was inflating.

**GF-4 — AN UNSATISFIABLE GATE. A classification that decides whether evidence
counts must be tested in BOTH directions, because the safe-looking direction is
the one no assertion notices.** From HARNESS-006.

Every other entry in this file is a check that COULD NOT FAIL. This is the
opposite shape and it needs its own name: a check that could not PASS. A
classifier that marks too much as non-evidence breaks nothing — the engine still
behaves correctly, every assertion still holds, every lane stays green — and the
only consequence is that a column stays empty. It is invisible precisely BECAUSE
it is conservative.

**The consequence is the sharpest part.** The cost does not arrive where the
defect is. It arrives one or two steps downstream, as a gate nothing can
satisfy: §7.4 condition 3 requires both elements of the two-element recovery set
to have been observed across the sweep, and a sweep whose runs are structurally
uncountable as evidence can never satisfy it. Found there, **it presents as a
bug in the engine rather than in the classifier, so the debugging starts in the
wrong component** — which is the expensive part, not the fix.

The audit this forced is in HARNESS-006. Closed by §7.5's registry holding
exactly its two named members, asserted both ways, and by every other
evidence-deciding function in Track B now being asserted both ways too.

**GF-3 — when an end-to-end test cannot distinguish two designs because both
fail the same way, assert the discriminating property directly on the unit where
the two differ.** From BM10. Our WAL checksum covers `length ‖ type ‖ payload`;
LevelDB's covers `type ‖ data`. End to end the two are nearly
indistinguishable — a corrupted length fails the checksum under either coverage —
because **the difference is not in what happens, it is in what is KNOWABLE at
the moment of failure**: with the length covered, the failure is at a known
offset and resync has a sound starting point; without it, the bytes consumed
before the failure are a function of data recovery has already decided not to
trust. So the property is asserted on `FragmentCrc` itself: *same type, same
payload, different length ⇒ different checksum*, which is false under upstream's
coverage and true under ours, in one line, with no log image involved.

The corollary is about who the defence is aimed at. **A deliberate divergence
from a well-known upstream is not attacked, it is helpfully corrected.** BM10 is
the one mutation in this catalogue a reviewer would most likely *approve*: it
introduces no bug, it removes two bytes of work, and it makes us match LevelDB,
whose header is byte-identical to ours. A defence written against a defect would
be pointed the wrong way. This one is pointed at a competent, well-meaning
reader — which is why the reasoning lives on the helper as a comment and not
only in a design document nobody re-reads before a cleanup.

**GF-2 — a two-field assertion where both fields read the same value under the
defect is not an assertion.** From HARNESS-003. Track A has recorded this shape
twenty-four times; it appeared in Track B's first cycle of real code, in a
different language and a different subsystem, written by someone who had read
all twenty-four. That is not a coincidence and it is not about C++ or about
ledgers: **the class is about how verification code gets written.** The reflex
it demands is to ask, of every assertion, "what value would this read if the
thing I am checking were broken?" — and if the answer is "the same one", the
assertion is decoration no matter how specific it looks.

### BUG-004 and BUG-005 — two defects in one step whose symptoms cancelled

| field | value |
|---|---|
| **Found by** | **a mutant surviving** — `BM97`, on its second induction |
| **Phase** | B3.5e, found and fixed before the step was signed |
| **Reproduce** | `Compaction.ASnapshotBelowARangeTombstoneKeepsTheVersionItHides` (BUG-004); `RangeDelete.ARangeSurvivesTheCompactionThatMovesItToLevelOne` (BUG-005) |
| **Invariant that caught it** | none directly — see below, that is the entry |
| **Mutant class** | `BM105` preserves BUG-004; `BM97`/`BM101` cover BUG-005 |
| **Fix commit** | this one |

**BUG-004 — clause 1 asked about the wrong sequence.** It tested whether a range tombstone covered
the key at the **top of a version's interval**. That conflates two different sequences: a snapshot at
5 and a tombstone at 9 are both "inside the interval" of a version at 4, and **the tombstone is
invisible to the snapshot.** The version is the answer at 5, and it was being dropped.

> **DATA LOSS FOR A SNAPSHOT BELOW A RANGE DELETE** — and it passed every end-to-end test, because a
> live snapshot **holds the pre-compaction tables resident** and reads through them. The loss appears
> at the next Open, after the snapshot is gone.

**BUG-005 — the sink was told its tombstones after it had written its files.** `RunCompaction` called
`SetTombstones` at the *end* of the merge. The sink closes output files *during* it, and a file closed
before it has been told the tombstones writes none. **The interface header already said "handed over
BEFORE the first entry"** — the contract was written down, in the same step, by the same author, and
the implementation violated it.

**THE ENTRY IS THAT THE TWO CANCELLED.** BUG-004 dropped the versions a tombstone hid, so BUG-005's
missing tombstone changed no answer: every read returned `<absent>`, **for the wrong reason**. Fixing
BUG-004 alone turned four passing tests red. Fixing BUG-005 alone would have changed nothing anyone
could observe.

> **A PAIR OF DEFECTS WHOSE SYMPTOMS CANCEL IS INVISIBLE TO EVERY TEST THAT ASSERTS AN ANSWER.** Both
> produce the right answer together. Only a question about a *mechanism* separates them.

**What found it was a mutant SURVIVING, which asks a different question.** A test asks *is the answer
right?* A mutant asks *is this line load-bearing?* `BM97` blinded the L1 tombstone lookup and nothing
failed — and it was **correct**: the lookup was not load-bearing, because nothing ever reached it.
The survival was true information about the engine, not a weak mutant.

**This is the strongest argument the catalogue has produced for the practice.** `BM97` had already
been held out once (its workload did not exist at B3.5d) and re-added deliberately. Relabelling it as
*"covered by the compaction tests"* would have been plausible, would have closed the file, and would
have left two defects in a shipped step. **GF-16's rule — reach the workload, never relabel — is what
kept the question open long enough to answer it.**

**Fix.** Clause 1 now tests **per observable sequence**: a version is required when some `s` both
sees it as newest *and* has no tombstone above it visible at that same `s`. The tombstone verdict now
runs **before the merge loop**, where the header always said it did; nothing in it depends on the
merge, so this is not a reordering for convenience.

---

### BUG-003 — a guard that reads as a serialiser and never serialised anything

| field | value |
|---|---|
| **Found by** | inspection, while adding compaction's manifest append at B3.4 |
| **Phase** | **present since B2.5; found at B3.4; harmless until B3.4** |
| **Reproduce** | not reproducible by a lane: it needs two concurrent `DB::Sync` callers, which the contract forbids and no in-tree caller does. See *Why nothing caught it* |
| **Invariant that caught it** | none — that is the entry. It was found by asking what else appends to the manifest |
| **Mutant class** | **BM82-sync-precondition-unguarded**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** None, in any run that has ever been made. A second concurrent `Sync` would append
manifest records **inside another append's group**, producing a manifest whose group terminator
counts do not match the records that precede them — refused at the next `Open` as corruption, with
the durable data intact and unreachable.

**Root cause.** `DBImpl::Flush` opens with

```cpp
if (closed_ || imm_ != nullptr) return Status::Ok();
```

which reads as *"one flush at a time"* and is not. **`imm_` is assigned several steps later**, after
the file-number reservation, the new WAL's creation, and the **first `AppendGroup`**. Two callers
arriving together both observe `imm_ == nullptr`, both pass, and both append. The guard makes a flush
a no-op while one is **pending**; it has never made two flushes mutually exclusive.

**Why nothing caught it, and this is the honest part.** Until B3.4 the manifest had exactly **one**
appender, so two concurrent flushes could at worst duplicate work. The defect needed a **second
appender** to become damaging, and compaction is the first one. The TSan lane could not have found it
either: its authored pattern is one writer and one syncer, and the header says why — *"not more,
because more would be a claim the contract does not make."*

**What this would have caused in production.** A corrupt manifest after two concurrent `Sync` calls,
which the frozen contract does not permit — so: nothing, for a conforming caller. For a
**non-conforming** one, an engine that refuses to open and loses nothing, which is the good failure
mode and is still a failure nobody would have diagnosed from the message.

**Fix, and why not the wider one.** Not a lock, and not a third TSan pattern. Both would have
answered a question the contract does not ask, and a lock would have **converted a precondition into
a supported mode** — the shape where an engine grows a guarantee nobody decided to make. The
precondition is enforced instead: `SingleCaller` in `DB::Sync`, so a second concurrent caller aborts
at the call rather than leaving a manifest for the next `Open` to refuse. The flush guard keeps its
early return and its comment now states what it does.

**B2'S SIGN-OFF IS AMENDED IN PLACE, NOT REOPENED.** `docs/DESIGN-B2-sstables.md` carries a note
naming this entry. The reason is a distinction worth keeping:

> **A PHASE'S SIGN-OFF IS A CLAIM ABOUT WHAT WAS VERIFIED, NOT A CLAIM THAT THE CODE WAS CORRECT.**

**The second time a phase's record has been amended by a later phase, and the mechanism was the same
both times: a defect unreachable under the earlier phase's shape.** Track A amended A4 and A5 for
`BUG-023`. Neither amendment says the earlier verification was wrong; both say the earlier phase
could not have reached the defect, and name the later shape that did. An amendment that reads as an
accusation would make the next one less likely to be written.

---

### BUG-006 — a table the engine wrote and could never open again

| field | value |
|---|---|
| **Found by** | **the B4 differential rig, on its first outing**, `compact` seed 6 — a CLEAN run, no kill |
| **Phase** | present since B3.5e; found at B4.2 |
| **Reproduce** | `SstWriter.ATombstoneEndingAtTheLargestDataKeyDoesNotMoveTheBound`; end to end, `rift_diff compact 6` |
| **Invariant that caught it** | `VerifyTables` — D4 §5.1 point 2, the manifest held to the classifier's derivation |
| **Mutant class** | **`BM113`**, added in the same PR as the fix |
| **Id note** | `BUG-004` and `BUG-005` are B3.5e's cancelling pair; this is the next free id |
| **Fix commit** | this one |

**THE SEVERITY IS A PERMANENT REFUSAL, NOT DATA LOSS, AND THAT DISTINCTION LEADS.**

> **A DEFECT THAT LOSES DATA IS DISCOVERED AND MOURNED. A DEFECT THAT REFUSES TO OPEN IS DISCOVERED
> AND CANNOT BE WORKED AROUND.**

The bytes are intact. Every table is valid, every checksum passes, the WALs are complete — and
`DB::Open` fails with `key bounds disagree with the manifest`, on this open and every open after it.
There is no retry, no partial recovery, and no way in.

**The triggering shape is unremarkable:** *any range delete whose end lands exactly on the highest key
present, at a lower sequence.* `compact` seed 6 produced it in 800 operations.

**Root cause.** `TableBuilder` and `ValidateTable` disagreed about when a range tombstone's end
widens a table's `largest`:

| | comparison |
|---|---|
| `TableBuilder::AddRangeTombstone` | `CompareInternalKey` — **internal keys** |
| `ValidateTable` | `.compare(ExtractUserKey(...))` — **user keys** |

The internal order is user key ascending and **tag descending**, so at one user key a *smaller tag
sorts later*. A tombstone ending at the largest data key with a lower sequence compares **greater** to
the writer and **equal** to the classifier. The writer widens; the classifier does not; the manifest
records the writer's and is held to the classifier's.

**THE PROVENANCE IS THE PART WITH THE MOST TEACHING IN IT.** §6.1a *is* this correction — *"The bound
is a statement about USER KEYS. It is compared as one."* — written at B3.5e, applied to the
classifier, **and never carried to the writer.** The comment explaining the fix sits ten lines from
the code that still had the bug.

> **A CORRECTION APPLIED TO THE SITE THAT FAILED RATHER THAN TO THE INVARIANT LEAVES EVERY OTHER
> IMPLEMENTATION OF THAT INVARIANT DEFECTIVE — AND THE COMMENT BESIDE THEM SAYS OTHERWISE.**

It is `GF-7`'s family: a claim attached near code that does not honour it. **Second time in this
engine** a correct comment has sat beside an incorrect line — `BM55` was the first.

**SO THE FIX WAS NOT APPLIED TO THE SITE. IT WAS APPLIED TO THE INVARIANT**, and the audit that
demands came next.

**Every place in the engine that compares a bound, and what it compares:**

| site | before | after |
|---|---|---|
| `TableBuilder::AddRangeTombstone` — largest | **internal** ✗ | user, via `WidensUpperBound` |
| `TableBuilder::AddRangeTombstone` — smallest | **internal** ✗ | user, via `WidensLowerBound` |
| `TableBuilder::AddUnboundedRangeTombstone` — smallest | **internal** ✗ | user, via `WidensLowerBound` |
| `ValidateTable` — largest | user ✓ | user, via `WidensUpperBound` |
| **`ValidateTable` — smallest** | **internal ✗ — A SECOND INSTANCE, FOUND BY THE AUDIT** | user, via `WidensLowerBound` |
| `VerifyL1IsARun`, `L1FileFor`, input selection, the L1 sort | user ✓ | unchanged |
| `Table::Iter::Seek`, block index, entry ordering | internal ✓ — **correctly**: these order ENTRIES, not bounds | unchanged |

**The fifth row is why the audit was worth doing.** `ValidateTable`'s *smallest* still compared
internal keys after B3.5e corrected its sibling **ten lines below**. It was latent only because the
writer compared internal keys too — **the two agreed while both were wrong**, which is a shared blind
spot rather than a disagreement, and no comparison between them could have found it.

> **ONE FACT, SEVERAL PLACES, ONE CORRECTED — the class `BUG-032` cost Track A.** The remedy is the
> one that class always wants: `WidensUpperBound` / `WidensLowerBound` are **one implementation**, and
> both the writer and the classifier call it. There is nothing left to keep in step.

**THE REACH ARGUMENT, AND IT IS THE DIFFERENTIAL'S JUSTIFICATION AS A MEASUREMENT RATHER THAN A
CLAIM.** With this defect present:

| instrument | result |
|---|---|
| 377 C++ tests | **all passed** |
| 3 sweep regimes, **4,840 kill points** | **0 violations** |
| 147 mutant classes | **all killed** |
| **8 clean differential runs** | **found it** |

No sweep workload ever produced a tombstone ending exactly on the largest key, so no kill point could
reach it. **A fault-injection sweep and a differential find different things, and this is the
measurement that shows it rather than the claim.** The differential needed **no crash schedule at
all** — the defect is in the write path and appears on a clean close.

**AND THE FIX MADE THE DEFECT UNREPRESENTABLE, WHICH THE MUTANT DISCOVERED BY SURVIVING.** `BM113`
was aimed first at `WidensUpperBound` itself and **survived** — correctly. The predicate takes a
**bare user key**, so comparing it against a full internal key behaves identically; there is no way to
express the bug through that signature.

> **A FIX THAT MAKES A DEFECT INEXPRESSIBLE IS STRONGER THAN ONE THAT MAKES IT WRONG — AND NO MUTANT
> CAN ASSERT THE DIFFERENCE AT THE SITE IT PROTECTS.** The survival is the evidence.

So `BM113` was re-aimed at the site where the class **can** still recur: a caller building its own
internal key and comparing for itself, which is what `TableBuilder` did before the fix and what an
inlining edit — *"why call a helper for two lines"* — would reintroduce.

**A SECOND CONSEQUENCE, AND IT IS A REAL TRADE RATHER THAN A FREE WIN.** Collapsing two
implementations into one removed the disagreement — and **removed the instrument that detected one.**
The reproduction asserts writer *equals* classifier, and with both calling one predicate they agree
even when the rule is wrong. What is left must be asserted against the **invariant** rather than
against the other implementation, which is why `BoundWideningIsAStatementAboutUserKeys` tests the
predicate directly, in **both tag directions** — a rule comparing internal keys would be right about
one of them by accident.

**What it would have cost in production:** an engine that accepts writes, acknowledges them, closes
cleanly, and cannot be restarted. Discovered at the worst possible moment, on a restart, with the
data intact and unreachable.

---

### BUG-007 — a 100 KB value was unreadable through the Go wrapper, and a comment said it was fine

| field | value |
|---|---|
| **Found by** | **the B5.2 parity suite, on its first run** — `TestALargeValueCrossesIntact`, no fault of any kind |
| **Phase** | introduced and found inside B5.2 |
| **Reproduce** | `make cpp-cgo`; any value larger than `block*1024+4096` bytes, reached by iterating |
| **Invariant that caught it** | wrapper/model parity — the two engines must return the same state |
| **Mutant class** | **`BM116`** (the wrapper) and **`BM115`** (the boundary's half), both added in the same PR as the fix |
| **Fix commit** | this one |

**THE COMMENT IS THE ENTRY, NOT THE BUFFER.** The first version of `fill()` carried this, three lines
above the bug:

> *"Sized so an ordinary pair never round-trips twice. A short buffer is CORRECT — the C side holds the
> pair rather than dropping it — so this is a performance choice and not a correctness one, which is the
> only reason a guess is acceptable here."*

**Every clause is true of the boundary. None of it was true of the code beneath it.** The comment
describes a caller that grows; the code was a caller that gives up. And the reasoning it offers — *a
guess at the buffer size is acceptable because the failure mode is benign* — is precisely what made
the undersized buffer read as deliberate rather than unfinished. The comment did not merely fail to
describe the code; **it supplied an argument for the code being right.**

> **A CLAIM TRUE OF ONE SIDE OF A BOUNDARY AND FALSE OF THE CODE BENEATH IT, IN A LANGUAGE THE OTHER
> SIDE'S COMPILER CANNOT SEE, IS `GF-11` AT ITS WORST CASE: NO TYPE, NO TEST FILE, AND NO REVIEWER
> HOLDING BOTH HALVES AT ONCE.**

That is the full statement of why this one was invisible. In the C++ engine, a comment that overstates
its code is still read beside that code by someone who can also see the caller. Here the two halves of
one contract live in **different languages**: no compiler spans them, no type spans them, no single
file spans them, and the reviewer who knows the C side's holding behaviour is not the reviewer reading
the Go loop. The C++ suite passed. The Go build passed. **The property they jointly promise was
checked by neither** until a test crossed the boundary with a value big enough to matter.

**Symptom.** `iter.Error()` returns `riftcgo: buffer too small` and iteration stops. Every key below the
large value is returned correctly; the large one and *everything after it* is silently absent, because a
cursor that errors is a cursor that ends. A caller checking `Error()` sees a failure it cannot act on;
a caller that only ranges to exhaustion sees **a short database**.

**Root cause, and it is on the Go side.** `rift_iter_block` returns `RIFT_BUFFER_TOO_SMALL` *without
consuming anything*, precisely so a caller can grow and retry without losing its position. That is the
documented contract and the C++ side implements it exactly. The wrapper's `fill()` treated the status
as fatal — it never grew.

**WHAT CAUGHT IT: A RIG ON ITS FIRST OUTING, FOR THE FOURTH TIME IN THIS PROJECT.** The parity suite
was written in the same step as the wrapper and failed on the first run of its first version. That is
now a pattern with a count, and it is recorded as such in the wrap-up section above.

**THE FIX WAS TO BOTH SIDES, AND THE SECOND HALF IS THE MORE USEFUL ONE.** The wrapper now grows and
retries. But a caller told only *"too small"* has to **guess** how much to grow by, and a guess that is
still too small loops — so the loop would have had no terminating quantity, which `CF-3` forbids.
`rift_iter_block` now reports the capacities the pair **needs** on that path, exactly as `rift_db_get`
already did for point reads. One grow always suffices, and the wrapper *asserts* that a second refusal
is impossible rather than tolerating it.

> **THE BOUNDARY ALREADY HAD THE IDIOM. IT WAS APPLIED TO ONE OF THE TWO CALLS THAT NEEDED IT.**

An inconsistency inside one interface is not a style defect; it is a place where a caller's correct
instinct — *this call behaves like that one* — produces wrong code. `rift_db_get` taught the wrapper's
author to expect `needed`, and `rift_iter_block` did not provide it.

**Why the mutant class is two classes.** `BM116` is the defect as it occurred, on the Go side, and it
is the first mutant in this catalogue that patches Go — the runner always copied the whole tree and
never cared about the language, so all that was missing was `cpp-cgo`, a lane running the Go half of
the boundary. `BM115` is the half a Go-only fix would have left standing: a boundary that reports its
*used* bytes rather than its *needed* ones still refuses correctly, still consumes nothing, and passes
every C++ test that checks the status code. It is caught only because its covering test asserts the two
numbers **by name** rather than asserting that the call refused — `GF-25` at the boundary.

**What it would have caused in production.** Any embedder iterating a database containing a value
larger than its block buffer would read a truncated database, with an error most range loops never
check. The threshold is a function of the *block size*, so the same database is complete at one
setting and short at another — which is the worst version of this defect, because the natural
diagnosis is that the data is missing.

### HARNESS-001 — a mutation lane's scratch copy silently lost three files of the tree under test

| field | value |
|---|---|
| **Found by** | `make cpp-mutants`, its own baseline gate |
| **Phase** | B1.0 |
| **Reproduce** | n/a — not seed-driven. `tar cf - --exclude=./.github .` from the repo root, then count files under `third_party/googletest/.github` in the result: 0, not 3 |
| **Invariant that caught it** | vendored-tree integrity (DESIGN-B1 §9.2) — the hash check ran *inside* the scratch copy and disagreed with the recorded hash |
| **Mutant class** | none needed — the baseline gate is the mechanism, and it is the `blind`-lane pattern already required by Amendment A2 |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `INVALID — the unpatched tree does not pass lane "cpp-ci"`,
with the vendored-tree check inside the scratch copy computing a different tree hash and counting
247 files where the working tree has 250.

**Root cause.** `copy_tree` excluded VCS metadata with `tar --exclude=./.git --exclude=./.github`.
bsdtar matches an exclude pattern against any suffix of a stored path, not only against the root, so
`./.github` also matched `./third_party/googletest/.github` — three files that upstream tracks.
The scratch tree was therefore not the tree under test.

**Why it was caught here.** Only because a vendored dependency with its own `.github` directory
arrived in the same step as the lane, and only because the vendored-tree hash check runs inside the
copy rather than only in the working tree. Without that check the copy would have been wrong and
every mutant result would have been about a slightly different tree, indefinitely and invisibly.

**What this would have caused.** Mutant verdicts computed against a tree that is not the repository.
For BM21 the three missing files are inert, so the verdict would have been right by luck; nothing
about the mechanism guarantees the next one would be.

**Fix.** Copy everything, then delete the root paths by name. A glob that "usually" anchors is not an
anchor. Recorded at the call site, because the next person to add an exclusion needs the reason.

### HARNESS-002 — `cpp-ci` passed under network isolation because its build directory was warm

| field | value |
|---|---|
| **Found by** | mutant `BM21-network-in-build` |
| **Phase** | B1.0 |
| **Reproduce** | apply `engine-cpp/mutants/BM21-network-in-build.patch`, run `make cpp-lane-set` (network available), then `make cpp-ci`. Before the fix: green |
| **Invariant that caught it** | no lane touches the network (DESIGN-B1 §9.2) |
| **Mutant class** | `BM21-network-in-build` — it existed, it fired, and the lane was wrong rather than the mutant |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `ALIVE  BM21-network-in-build: cpp-ci stayed green`.

**Root cause.** `cpp-ci` reused `engine-cpp/build`. CMake's `FetchContent` populates at configure time
and skips the download when `_deps/` is already populated, so any earlier networked build left behind
exactly the artifact that makes a network dependency invisible. The lane's premise — that a clean
clone with no network can build — was true only when the build directory happened to be cold.

**Why it was caught here.** Because BM21 is bidirectional by construction: the same patched tree must
go green with a network and red without. The control direction ran first, warmed the cache, and the
covering direction then measured the cache instead of the network. Two bugs met — one in the lane,
one in the mutation harness sharing a tree between directions — and the mutant surviving was the only
signal either produced.

**What this would have caused.** `cpp-ci` reporting that no lane touches the network while a lane
touched the network. The failure would surface for the first time in the hands of the stranger the
lane exists to protect: a clean clone, offline, one script, red.

**Fix.** Two, because there were two defects. `cpp-ci` now builds in its own directory and deletes it
first, so the lane is cold by construction rather than by habit. And `cpp-mutants` gives each
direction of a mutant its own tree, because a control run and a covering run are independent
experiments and one must not be able to feed the other.

### HARNESS-003 — the ledger's promotion flag was never under test, and a mutant proved it

| field | value |
|---|---|
| **Found by** | mutant `LEDGER-always-promoted`, which **survived** |
| **Phase** | B1.3 |
| **Reproduce** | apply `engine-cpp/mutants/LEDGER-always-promoted.patch` against the tree at `cf12938` and run `make cpp-test`: green |
| **Invariant that caught it** | none — that is the entry. The mutant survived, and the survival is the finding |
| **Mutant class** | `LEDGER-always-promoted`, added at B1.3 alongside the ledger it blinds |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `ALIVE  LEDGER-always-promoted: cpp-test stayed green`.

**Root cause.** The lying-Sync test asserted on `LastPromisedBytes`, a helper that scans the ledger for
entries with `promoted == true` and returns the last `durable_bytes_after`. Under a suppressed
promotion `durable_bytes_after` is 0 whether or not the entry claims to have promoted, so the two
fields agreed at zero and the flag itself was never read by any assertion. The ledger's whole job is to
record what *happened* rather than what was *reported*, and the field carrying that distinction was
unchecked.

**Which of the three things a surviving mutant means.** A checker that cannot see it. Not a defence
that was never there — the flag was set correctly — and not unreachable code, which is the only one of
the three whose correct response is deletion. The response here is to strengthen the checker.

**What this would have caused.** Nothing in production; the engine does not read the ledger. It would
have cost the *oracle*: B1.9a's exactness assertions are required to derive their verdict from the
ledger and from nothing else, and a ledger whose promotion column had silently become a copy of the
Sync's return value is the engine's account of itself wearing harness clothing. It would have been
discovered, at best, as an unexplained pass at B1.9b.

**Fix.** The test now asserts on `promoted` directly, in both directions: a lying Sync's entry must
read `promoted == false` with `injection == kSyncLoss`, and a clean Sync's must read `promoted == true`
with the right byte count. A flag asserted in only one direction degenerates into a constant.

**The class, not the instance.** See GF-2 above. Both fields reading zero under the defect is the same
shape Track A has recorded twenty-four times, and it arrived in Track B's first cycle of code that does
anything. The lesson is not about ledgers, or about C++: it is about how verification code gets
written, and it will arrive again in B1.6's byte digest and B1.9a's oracle unless the question "what
would this read if the subject were broken?" is asked of every assertion.

### HARNESS-004 — the cold-cache check asserted the absence of something it had just deleted

| field | value |
|---|---|
| **Found by** | inducing the gate, by hand, in the same cycle that wrote it |
| **Phase** | B1.4 |
| **Reproduce** | at the first draft of `scripts/cpp-cold-cache.sh`: `mkdir -p engine-cpp/build-ci && make cpp-ci` — green |
| **Invariant that caught it** | none. The induced-failure rule caught it: the gate was written, run, and did not fire |
| **Mutant class** | `COLD-fetch-despite-isolation` covers the *after* half; the *before* half is induced by hand, `mkdir engine-cpp/build-ci && make cpp-ci` |
| **Fix commit** | this one |

**Symptom.** The gate written to prevent HARNESS-002 recurring was created, wired, and induced — and
the induction printed `*** GATE DID NOT FIRE ***`.

**Root cause.** `cpp-ci`'s recipe ran `rm -rf $(CPP_BUILD_CI)` and *then* called the check. The check
asserted that the build root did not exist, immediately after the lane had deleted it. It was green
unconditionally, including in the exact state HARNESS-002 occurred in.

**Why this one is worth an entry despite never being committed.**

**It is the first time in either track that a general form recurred inside its own remedy.** GF-1 was
being written down, in the same working session, by someone with the sentence in front of them — and
the gate written to enforce it violated it one line later. The draft and the fix were
**indistinguishable by reading**. They were distinguished **in four seconds by running**.

That is the entire lesson, and it is larger than this gate. **The induced-failure rule is not a
formality applied to gates once they are written; it is the only thing that distinguishes a fix from a
fix-shaped edit.** Every other check in this repository — review, the general form itself, the author's
attention — passed this draft. One `mkdir` and one `make` did not. A rule you can state and still
violate one line later is a rule that needs a mechanism, and this is the mechanism.

**Fix.** The check no longer removes what it checks for — *a check that removes the thing it is
checking for is a check that cannot fail*. `cpp-ci` refuses when the build root exists and says how to
clear it; a successful run removes its own tree at the end, so the next run is cold; a failed run
leaves its tree for whoever has to debug it.

### HARNESS-005 — a pointer-keyed container in `TestEnv`, and the split labels refusing to absorb it

| field | value |
|---|---|
| **Found by** | implementing the scan rule that catches it (`A5-ADDRESS`), at B1.4 |
| **Phase** | B1.3, found at B1.4 |
| **Reproduce** | at `3239469`: `std::map<const void*, std::string> handles_` in `engine-cpp/src/env/test/test_env.cc` |
| **Invariant that caught it** | §6.1 — "nothing may depend on an address — no pointer-keyed containers, no address-ordered anything", which §9.4 says the scan checks |
| **Mutant class** | `A5-ADDRESS`'s fixture and blind patch; `A5-ADDRINT` was added at B1.5 for the arithmetic half the rule was missing |
| **Fix commit** | `187a3eb` |

**Symptom.** The first run of the new rule over `engine-cpp/src` reported a violation in code ratified
the previous cycle.

**Root cause.** `TestEnv` mapped an Env handle's address to its path, in order to know which file a
fault was being injected against. §6.1 bans that outright. **No behaviour was wrong**: the map is
looked up and never iterated, so no address ordering was ever observable.

**The disposition is the finding, not the defect.** The obvious move was a registry entry, and the
registry would not take it. `covered-by` requires naming an instrument that catches the class instead,
and nothing caught it. `unreachable` requires naming a detector that would have seen it if it could
occur, and it *did* occur — the logic was right there. **A taxonomy that refuses to absorb a defect is
doing its job.** With a free-text reason field the entry would have written itself: "looked up, never
iterated, harmless" — true today, unexaminable tomorrow, and indistinguishable from the seventeen
single-labelled opt-outs Track A spent a full cycle re-deriving and found three of them wrong. **One
label absorbing two meanings is how a real gap comes to look accounted for.**

So it was fixed rather than exempted, which is the only remaining option once both labels decline.

**A determinism win falling out of a hygiene fix.** `HandleId` replaced the pointer through the whole
Env surface: an integer assigned sequentially by the creating Env, so the same workload assigns the
same ids on every run and every machine. That makes a kill point reportable as
`Sync(handle 3, 000001.log)` rather than `Sync(0x7f9c4a005e10)` — a bug report instead of a number that
means nothing on the second run. §9.5 asks for exactly that and would have had to build it separately;
here it arrived as a consequence of obeying §6.1.

**What this would have caused.** Nothing, until someone iterated the map — at which point the fault
schedule would have depended on the allocator, and a kill-point sweep would have injected against
different files on different runs while reporting the same ordinals.

### HARNESS-006 — a prefix-granular torn `Sync` was classified as exactness-suspending

| field | value |
|---|---|
| **Found by** | writing B1.7b's discard test, which needed a torn `Sync` and found it marked the run non-evidence |
| **Phase** | B1.3, found at B1.7b |
| **Reproduce** | at `dfba754`: `SuspendsExactness(Injection::kTornSync)` returns true |
| **Invariant that caught it** | none — no lane could catch it. The classification decides what a run may be *banked* as, and every run it mislabelled still passed every assertion |
| **Mutant class** | `REGISTRY-lying-sync-not-suspending`, which existed and could not see this: it checks that members ARE members, not that non-members are not |
| **Fix commit** | this one |

**Root cause.** B1-D5 rules two things and they were collapsed into one. Prefix granularity — a kill
inside `Sync` promoting `content[0:k)` — is **the contract model**, the thing §7.4's two-element set
`R ∈ {G_{k−1}, G_k}` describes, and the engine is held to exactness under it. Only the *sector-subset*
mode, where an arbitrary set of 4 KiB sectors is promoted, suspends: that is a device violating fsync's
own ordering guarantee, and holding the engine to exactness there would report the engine for the
disk's crime. B1.3 implemented one injector and mapped it onto the suspending registry member.

**A NEW SHAPE: an unsatisfiable gate.** This does not belong beside the vacuous-green entries and is
not filed with them. Every one of those is a check that *could not fail*. This is a check that *could
not pass*. It survived four ratified steps because it was **conservative**, and conservative is the
direction no assertion notices: the engine behaved correctly, every test held, every lane was green,
and the only symptom was that a column would have stayed empty. `REGISTRY-lying-sync-not-suspending`
existed and could not see it — it is pointed at a member that stops suspending, and nothing was
pointed at a non-member that starts.

**What it would have cost, and where.** Not here. At B1.9a: §7.4 condition 3 requires that *both*
elements of the two-element recovery set were observed across the sweep, and runs that are
structurally uncountable as evidence can never satisfy it. **Found there, it presents as a bug in the
engine rather than in the classifier, so the debugging starts in the wrong component.** That is the
expensive part. The fix is four lines.

**The general form is GF-4**, and §7.5 of DESIGN-B1 cross-references it: the registry holding exactly
its two named members, asserted in both directions, is what closes this.

**THE AUDIT IT FORCED, and its result.** The same question was asked of every place in Track B that
decides whether a run counts as evidence. Six exist:

| function | decides | both directions asserted before the audit? |
|---|---|---|
| `SuspendsExactness` | whether a run is characterization-only | **no** — this entry |
| `OutcomeFloor` | the same, one layer up | **no** — only ever called with `true` |
| `OutcomeForCapVerdict` | whether a cap verdict is a pass, a void or a violation | **no** — `kNormal` never asserted |
| `IsDivergence` | whether a cap verdict fails the run | **no** — only the two true cases |
| `CountsAsRecoveryEvidence` | what may be banked | yes, all five kinds |
| `AggregateRuns` | whether runs may be banked *together* | yes, both regimes |

**Three more instances, all the same shape**, and two of them reachable: `OutcomeFloor` returning
`kCharacterizationOnly` unconditionally, and `OutcomeForCapVerdict` filing a normal run as `kVoid`.
Both make the evidence column permanently empty with nothing going red. Mutants `FLOOR-always-suspends`
and `VERDICT-normal-is-void` now exist for exactly those, and all six functions are asserted in both
directions.

**Fix.** The injector is split: `kTornSync` (prefix, the contract model, does not suspend) and
`kSectorSubsetTornSync` (an arbitrary sector left unpromoted, suspends). The registry now has exactly
the two members §7.5 names. The test asserts **both directions** — that the two members suspend and
that the prefix mode does not — because a classification asserted in one direction is the shape GF-2
already names.

### HARNESS-008 and HARNESS-009 — the other two evidentiary deciders, side by side

Found by the audit HARNESS-006 forced. Recorded together because **the shape is identical and the
consequences differ in a way worth seeing beside each other** — that pair is the argument for the
both-directions rule in two lines.

| | HARNESS-008 | HARNESS-009 |
|---|---|---|
| **where** | `rig::OutcomeFloor` | `rig::OutcomeForCapVerdict` |
| **the untested direction** | `OutcomeFloor(false)` — it had only ever been called with `true` | `kNormal` — the two divergences and `kVoid` were asserted, a normal run never |
| **the defect it admits** | suspend unconditionally | file a normal run as `kVoid` |
| **what that costs** | **every run becomes unbankable** | **the evidence column empties permanently** |
| **what turns red** | nothing | nothing |
| **mutant** | `FLOOR-always-suspends` | `VERDICT-normal-is-void` |

Both are GF-4. Both are conservative, which is why neither is visible: the engine is correct, the
arithmetic is correct, every assertion holds, and the only symptom is a number that never appears.
One kills the evidence at the source and one kills it at the sink, and **the pair is the reason the
rule is "both directions" rather than "test the interesting case"** — there is no interesting case
here, only two boring ones whose absence is invisible.

Closed structurally: `engine-cpp/DECIDERS.txt` enumerates every function that decides evidentiary
status, `scripts/cpp-scan.sh` requires each to name the tests asserting **both** of its directions, and
a decider that lands without them fails the lane. Six is a small enough population to enumerate, and
enumerating it is what stops the seventh arriving in B2 and the audit being re-run by hand.

### A shape three of this cycle's defects shared

**HARNESS-006, `HEADER-conditional`, and the near half of BM2's survival are all the same thing: a
check placed somewhere that something else decides whether it runs.** The exactness classification ran
off an injector enum that had collapsed two ruled cases into one; the FILE_HEADER validation ran inside
a loop bounded by whether a `GROUP_END` existed; the discard assertion ran on a workload where the
records it was checking never reached a file. In each, the check itself was correct and its *reachability*
was decided elsewhere — so it passed, and reported on a situation that had not occurred.

Worth one sentence rather than three entries, because the remedy is one habit: when writing a check,
ask what has to be true for it to run at all, and assert that too.

### The shape behind every mutant that has survived its first induction

**THIS IS A STANDING PATTERN, NOT A LIST OF INCIDENTS — AND B2'S LAST RUN BROKE THE RUN OF ONE
MEANING, WHICH IS BETTER THAN AN UNBROKEN ONE.** Six have survived a first induction:

| class | meaning | shape |
|---|---|---|
| `LEDGER-always-promoted` | #1, a checker that cannot see it | the test never created the situation it was checking |
| `BM2-accept-torn-tail` | #1 | " |
| `BM7-drop-close-error` | #1 | " |
| `BM1-ack-before-sync` | #1 | " |
| `BM52-current-parsed-leniently` | #1 | " |
| **`BM55-tables-oldest-first`** | **#2, a defence that was never there** | **the patch was aimed at a line a comment claimed was load-bearing and was not** |

**Five of the six share one sentence.** The sixth is the first of a different kind in this track, and
it matters that the tally now has two entries rather than one: a classification that only ever
returns one answer is a classification nobody has tested. `BM55` is what shows the three meanings are
doing work.

`BM55`'s own story is short and is recorded at the code. It reversed the order sources are handed to
`MergedIter`, and every test stayed green **correctly** — the merge orders by KEY, sequences are
unique, so there are no ties for source order to break and the order it is given is irrelevant. The
comment beside that line said otherwise, in words that are true of the *point-read* path a hundred
lines below. **A comment asserting a load-bearing property for a line where it is not load-bearing is
worse than no comment**: it is where the next reader looks for the invariant, and it sends them to a
line nothing depends on. The patch is re-pointed at the walk that carries the property, and the
comment is corrected, in the same diff.

**All six are meaning #1 or #2. MEANING #3 HAS NEVER OCCURRED IN TRACK B**, and that is worth stating
precisely rather than as reassurance, because it is the only meaning whose correct response is to
**delete** something.

> If meaning #3 never occurs, either **the code has no dead paths**, or **the classification cannot
> see them**. We do not currently know which.

Nothing here distinguishes those two, and no lane is pointed at the distinction. A coverage
instrument would be — B4's differential rig is the phase where one becomes affordable — and until
then the honest statement is that meaning #3 is a category with no observations, not a category shown
to be empty. **A tired reader reaching for it is reaching for the one answer this catalogue has never
had evidence for.**

> **The test never created the situation it was checking.**

`BM7` is the cleanest exemplar: a Close test that only ever ran a *successful* Close cannot distinguish
a propagated error from a swallowed one, whatever it asserts about the return value.

`BM1` is the subtlest, and it sharpened the rig. The oracle learned the engine's watermark **only from
a `Sync`'s return value** — and a killed `Sync` returns nothing, so an engine that advanced the
watermark before writing a byte was invisible: the premature value died with the process. The fix is
not a bigger test but a wider definition of the promise. **`DurableSeq()` is a durability claim like
any other**, so the rig now records every value it is ever told and holds the engine to the highest,
and the induction runs the failure through an fsync that *errors* rather than one that kills — so the
process survives to be asked.

`BM52` is the fifth and the plainest, which is why it is worth recording that the shape did not have
to be rediscovered. Eight malformed `CURRENT` bodies were each asserted to be refused, and each was —
**because the manifest it named did not exist**, so a lenient parse failed too, for a reason with
nothing to do with parsing. One line fixed it: put a real `MANIFEST-000001` in the directory, and
`MANIFEST-1\n` becomes a body a lenient parse *resolves*.

This is **§22.6c's discriminator rule arriving in C++ independently** — a check must be run in a state
where the thing it discriminates could actually differ — and it is cited rather than restated. It is
also the same family as GF-1, one level in: GF-1 is about a *lane* verifying an absence, this is about
an *assertion* verifying a distinction. Filed once here; individual survivals are not entries.

**B3.4 ADDS FOUR, AND ONE OF THEM IS THE FIRST INSTANCE OF MEANING #3.**

`BM73` removed `L1FileFor`'s check that the file the binary search found actually *contains* the key,
and **nothing failed** — a key in the gap between two files of the run makes the search return the
next file along, whose `Get` cannot find it either, so the answer is identical and only a filter probe
is wasted. **The line is a cost guard, not a correctness one.** That is `BM55`'s question answered the
other way: the property *"a range test decides containment"* IS load-bearing, in the compaction's
**input selection**, where getting it wrong resurrects deleted data — so `BM80` was written there and
`BM73` was **deleted**. GF-7, second instance, and the first time this catalogue has deleted a mutant
rather than re-aimed it.

`BM76` and `BM79` are meaning #1 again, and `BM79` is a **sub-form worth naming**:

| mutant | why it survived | what the fixture was really watching |
|---|---|---|
| `BM76` | the tombstone sat at the **top sequence**, kept by the watermark pin for an unrelated reason | the pin, called the drop rule |
| `BM79` | with no live snapshot the drop rule leaves **one version per key**, so no key can span a file roll | a situation that could not occur |

> **A MUTANT THAT SURVIVES BECAUSE ITS PRECONDITION IS UNREACHABLE IS NOT A WEAK MUTANT; IT IS A
> WORKLOAD THE SUITE NEVER RAN.**

That is a sharper statement of meaning #1 and it points at the fix rather than the symptom: `BM79`'s
test now holds forty snapshots so that a key HAS many surviving versions, which is a workload the
suite had never run and which B3.6 is about.

**`BM84` IS MEANING #3's SECOND INSTANCE, AND IT ANSWERED A QUESTION RATHER THAN FINDING A BUG.**
Ansh asked for the shape a future optimization will produce to be planted deliberately: *read `S` as
late as possible so it is as small as possible so more can be dropped.* It was planted, and it
**survived — correctly.** Both directions of `S` movement are safe (a release only over-keeps; an
acquisition lands above every sequence the inputs hold), so **the timing of the read does not carry
the correctness.** `pin_seq ≤ max(S)` does, and that is now a `RIFT_CHECK`.

> **A MUTANT PLANTED TO ANSWER A QUESTION IS WORTH PLANTING EVEN WHEN IT SURVIVES — BUT IT IS NOT
> WORTH KEEPING AS A CLASS THAT CAN NEVER FAIL.** Deleted, with the answer moved to the call site and
> to `DESIGN-B3` §1.3, which is where the next person to propose the optimization will look.

`BM82` is meaning #2 — **`BM55`'s question, asked again and answered the same way.** It removes
`Sync`'s claim on the single-caller guard and leaves `SingleCaller` itself intact, and it survived a
pair of tests that construct the guard **directly**:

> **THOSE TESTS PROVE THE GUARD WORKS. THEY DO NOT PROVE THE GUARDED PATH USES IT.** Two different
> claims, and the second is the one the enforcement rests on.

The fix is a test that **re-enters `Sync` from the promotion hook** — which fires inside `Sync`, on
the durable image changing — so the guard is claimed twice on **one thread**, deterministically. The
alternative, racing two real `Sync`s, would induce it only *probably*, and this catalogue does not
count a gate induced probably. The hook fires **once** on purpose: without that, a build with the
claim removed would recurse until the stack gave out, and **a death test cannot tell a guard firing
from a crash** — the mutant would have passed for the wrong reason.

**AND THE TALLY'S OWN INSTRUMENT MISREAD ONE.** `BM78` was recorded as a survival on its first
induction because the script looked for a failing test — and the kill is an **abort**, so the process
died before the summary printed. `FLOORS.txt`'s header already warns of exactly this, and the
`RIFT PARTIAL RUN` marker was already in the output being read. **The remedy existed and was not
looked for**, which is the standing provenance rule one level up: a signal read without asking what
its absence would look like.

**THE STANDING QUESTIONS, now that the tally has two meanings in it.** A survival is a fork, not a
verdict, and the two questions are different:

1. *What must be true for this assertion to run at all?* — assert that too. In all five meaning-#1
   survivals that question was the whole of the fix.
2. *Is the line this patch is aimed at actually the line that carries the property?* — `BM55`'s
   question, and the one nobody asks while a comment is answering it for them.

Reach for meaning #3 last. Nothing in this catalogue has been unreachable code, and it is the only
meaning whose correct response is to delete something.

### HARNESS-007 — `Slice` bound silently to temporary strings, and a test dangled

| field | value |
|---|---|
| **Found by** | AddressSanitizer, inside `make cpp-mutants`'s **baseline gate** |
| **Phase** | B1.7b |
| **Reproduce** | at `dfba754`, with `Slice(std::string&&)` still permitted: `Op op; op.key = Slice("k");` |
| **Invariant that caught it** | none by design. The baseline gate ran `cpp-asan` on the unpatched tree before reporting any kill, and refused to report |
| **Mutant class** | none, and none is added: the fix is a compile error, so there is no runtime behaviour left for a mutant to blind |
| **Fix commit** | this one |

**Root cause.** `Slice` had `Slice(const std::string&)` and no `const char*` overload, so `Slice("k")`
constructed a **temporary `std::string`** and pointed into it. The Slice outlived the full expression;
the buffer did not.

**Why the baseline gate is the entry.** The mutant lane refuses to report kills until the unpatched
tree passes every lane a patch names — a rule borrowed from `make blind` after a lane there reported
seven kills while one of the tests doing the killing was failing for its own reasons. Here it turned an
unattributable run into a named ASan stack trace.

**Defects found by the baseline gate while doing its actual job: 2** (HARNESS-001, HARNESS-007). The
count is kept because it is the argument for the gate. Its stated purpose is to make kills
attributable; what it has actually done twice is find defects nothing else was looking for, in a tree
that every other lane called green. A mechanism that keeps paying outside its stated purpose is worth
more than the purpose.

**Fix, and why it is structural rather than local.** `Slice(std::string&&) = delete;` makes binding to
a temporary a **build failure**, and a `const char*` overload makes the literal case point at static
storage. Twenty call sites had to hoist their strings into named locals; every one of them was a latent
instance of the same bug, safe only by accident of lifetime. A class of dangling-pointer bug became a
class of compile error.

### HARNESS-010 — the sweep could not see BM2, because the snapshot was hiding the damage

| field | value |
|---|---|
| **Found by** | measuring the sweep's power against every class, per GF-4's sibling discipline in §10.3 |
| **Phase** | B1.9b |
| **Reproduce** | apply `BM2-accept-torn-tail`, run `make cpp-sweep` before the post-reopen continuation existed: 175 points, 175 passes |
| **Invariant that caught it** | none. The **measurement** caught it: a class that should be sweep-detectable measured **0 of 175** |
| **Mutant class** | `FLOOR-continuation-removed`, which makes the regression repeatable |
| **Fix commit** | this one |

**Root cause.** With BM2 applied, recovery applies BATCH records past the last `GROUP_END` — records that were
never committed. They land in the memtable at sequences **above** the recovered watermark, and every read goes
through a snapshot pinned at that watermark, so **they are present and unreadable**. The oracle compares the
visible state, which is correct, and passes.

That is not a defence. It is an accident of the read path, and it expires: at **B2 the flush writes the memtable
out**, and uncommitted records become durable, visible and permanent. The engine would have shipped a recovery
path that quietly retains data it never promised, with a 175-point sweep reporting 175 passes.

**Why the measurement found it and no assertion could.** Every lane was green and correct. The sweep visited every
kill point, observed both elements of the recovery set, and reported no violation — all true. What was wrong was
its **power**, and power is not a property any single run can assert about itself. §10.3 exists for exactly this,
and it is the first thing it found.

**Fix.** The sweep now **continues after reopening**: one write, then a comparison. A reopened database keeps
serving, and the new write takes the sequence the hidden records already occupy, so they become visible at exactly
the moment a real database would have resumed service. BM2 went from 0 to **194 per mille**, first detected at
kill point 14.

**The floor that keeps it.** `BM2`'s rate floor is 90 per mille — roughly half the measurement, and set against
**the suppressed number rather than under today's**: the value that matters is the 0 this class measured before the
continuation existed. `FLOOR-continuation-removed` induces exactly that regression, and its control is
`cpp-sweep` **staying green** — the lane whose job is finding defects is perfectly healthy while its power has
collapsed.

### HARNESS-011 — TestEnv's ledger under-reported what a torn `Sync` promoted

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, on its first run with torn modes enabled, against the **unpatched** tree |
| **Phase** | B1.3, found at B1.9b |
| **Reproduce** | before the fix: a torn `Sync` whose prefix covers the whole newly covered extent records `promoted=false` |
| **Invariant that caught it** | the exactness oracle — it reported a violation at kill point 35 |
| **Mutant class** | none added: the ledger field is now written from the durable image itself, so there is no separate flag to blind |
| **Fix commit** | this one |

**Symptom.** `VIOLATION at ordinal 35 (kWritableFileSync, before effect): recovery landed on sequence 6, a batch
boundary strictly inside a group.`

**Root cause.** `promoted` was set by `RecordPromotion`, which only runs when `DoSync` runs. A torn `Sync` kills
*instead of* running `DoSync` — so however much of the extent it actually promoted, the ledger said it promoted
nothing. When the prefix happened to cover the entire group, durability really had advanced, and the oracle,
reading `promoted=false`, refused to offer the in-flight element of the recovery set and **reported the engine for
landing exactly where the ledger's own bytes said it should**.

**The lesson, and it is not the one it looks like.** Ruling 4 says an oracle that interrogates the engine believes
the lie. This is one level in: **a harness record that under-reports is as damaging as an engine that
over-reports**, and it is worse in one way — it blames the engine. The ledger is now written from the durable image
before and after the injection, so it records what happened rather than which code path ran.

---

### HARNESS-012 — the oracle's durability fact rested on there being exactly one file

| field | value |
|---|---|
| **Found by** | `make cpp-sweep` in the flush regime, against the **unpatched** engine, after BUG-001 and BUG-002 were fixed |
| **Phase** | B1.9a, found at B2.6 |
| **Reproduce** | before the fix: `rift_sweep flush` reports violations at kill points 149, 159, 167, 178, and `rift_sweep default` at 59 |
| **Invariant that caught it** | the exactness oracle reporting the ENGINE for landing on a group the ledger's own bytes said was durable |
| **Mutant class** | **ORACLE-facts-last-sync**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** `VIOLATION at ordinal 149 (kWritableFileSync, before effect): recovery landed on sequence
46, a batch boundary strictly inside a group.` The engine was right; the harness was wrong three
different ways in one line of code.

**Root cause.** `FactsFrom` computed `in_flight_durability_applied` as *the `promoted` flag of the
last `kWritableFileSync` entry in the ledger*. Every clause of that was true only by accident:

1. **the last file, not the WAL.** A group lives in the WAL. Until B2 the WAL was the only file this
   engine ever synced, so "the last Sync" and "the WAL's Sync" were the same entry. The flush syncs
   three — the table, the manifest, and the WAL — and reading the last one reports the *manifest's*
   durability as the group's.
2. **only a `Sync`, not any call that promoted.** A torn injection at a `Flush` promotes a prefix,
   and the promotion is recorded on the **Flush** entry. A filter that looked only at Sync calls
   reported "not durable" about bytes that were.
3. **the last one, not any one.** Durability is not undone. The flush creates a *second* WAL inside
   the same `Sync`, and that empty file's own sync promotes nothing — so the last `.log` entry says
   "not durable" about a group made durable moments earlier by the WAL being retired.

**Why all three survived B1.** Without scoping the question to *this* `Sync`, one successful Sync
answers for every group after it. That masked (1) and (2) completely: the last `.log` Sync in the
whole run was almost always a successful earlier one, so the fact was accidentally `true` exactly
when it needed to be. **Scoping made the harness strict, and strictness is what exposed the other
two.** Fixing one defect is what made the others visible — the reverse of the usual order.

**The lesson.** HARNESS-011 said a harness record that under-reports is worse than an engine that
over-reports, because it blames the engine. This is the same shape one level up: **a harness FACT
derived from "the last event of a kind" is a fact about the world having one of that kind.** The
question the oracle asks is "did *this* Sync make *the WAL's* in-flight group durable"; the code now
asks exactly that, scoped, filtered, and monotone.

---

### HARNESS-014 — a registry cross-check that matched nothing, and would have passed forever

| field | value |
|---|---|
| **Found by** | `make cpp-scan`, on the first run of the rule it was part of |
| **Phase** | B3.0a, found the day it was written |
| **Reproduce** | before the fix: mark a file `RIFT_ORACLE`, leave it out of `ORACLES.txt`, and the scan says nothing |
| **Invariant that caught it** | the rule's own both-ways check, which fired on the OTHER direction and exposed this one |
| **Mutant class** | the four inductions in `B3.0a`'s commit, each observed and restored |
| **Fix commit** | `3e6d2c0` |

**Symptom.** `ARTIFACTS.txt` and `ORACLES.txt` were built, the registry cross-check was written, and
it **matched nothing** — every lookup fell through to the "not registered" branch. Two defects, and
only the first is interesting.

**Root cause 1, and it is a VACUOUS CHECK rather than a script bug.** The registry lists were built
with `grep | awk` and matched with `case " $list " in *" $item "*`. The lists are **newline**
separated and the pattern needs **spaces**, so no item ever matched. Had the check been written in
the direction that passes on no match, **it would have passed forever, on every tree, reporting a
boundary it was never testing.** It failed loudly here only because this particular direction reports
on *absence* of a match — an accident of which way round it was written, not a property of the check.

That is `GF-1`'s family at the level of a registry: *a check that cannot distinguish "nothing
violates this" from "I compared nothing" is not a check.* The remedy is the same one `GF-1` names —
run it where the thing could be present, and assert that it saw something. `cpp-scan` now prints
`parses 10 artifact(s)`, which is a count, and **a count nobody asserts is decoration**: it is
asserted by the four inductions.

**Root cause 2, recorded because it is cheap to record and expensive to re-find.** The new code used
a variable named `found`, which is part 3's `mktemp` path. Clobbering it made two *unrelated*
registry entries report as stale. **The lane doing to itself exactly what it does to the tree**, on
its first run.

**What it cost.** Nothing, because the rule was induced in four directions before being trusted —
which is the only reason either defect surfaced on day one rather than at the gate.

---

### HARNESS-013 — the mutant lane waited eleven and a half hours for a lane that was never going to report

| field | value |
|---|---|
| **Found by** | reading `ps` while answering "how long will the catalogue take" |
| **Phase** | B1.3 (the lane), found at B2's close |
| **Reproduce** | before the fix: `make cpp-mutants` on a tree where `BM35-tag-sorts-ascending` is reached; it never returns |
| **Invariant that caught it** | none — **nothing was watching**, which is the entry |
| **Mutant class** | none can be added: a permanent catalogue member that hangs would hang the catalogue. The mechanism was induced with a throwaway patch that makes a lane `sleep`, and the watchdog was seen to fire, report TIMEOUT and fail the lane |
| **Fix commit** | this one |

**Symptom.** The catalogue sat at `control BM35-tag-sorts-ascending: cpp-build still alive, as it
must` for **11 hours 34 minutes**, with `rift_engine_test` in that mutant's scratch tree burning
**690 minutes of CPU at 99.3%**. Nothing was wrong with the machine and nothing was logged. I read the
log's last line, saw a lane in progress, and gave an estimate for the remaining patches built entirely
on the assumption that progress was what I was looking at.

**Root cause, two halves.**

*The engine half.* `BM35` inverts the tag half of the internal key order. `IterImpl`'s advance and
retreat loops carry comments reading *"strictly advances, so this loop terminates"* — an invariant
that rests **entirely on the comparator being the order it claims to be**. Invert the comparator and
the loops no longer terminate. `Flush.ReadsSeeTheMemtableAndTheTablesTogether` is where it spins,
because that is the test with a backward scan over a merged view.

*The lane half, and it is the one that matters.* `run_lane` was `( cd "$1" && $MAKE "$2" ) >"$3" 2>&1`
with **no timeout**. A mutation that makes a lane HANG is neither a kill nor a survival: the lane
never reports. So the catalogue waits, forever, and **a waiting catalogue is indistinguishable from a
working one**.

**The fix, both halves.** The loops now `RIFT_CHECK` the progress their comments assert — the user key
is compared bytewise, which is a property of the merged order that does *not* depend on the tag
comparator, so the assertion can catch the comparator being wrong. `BM35` now aborts in **0 seconds**
instead of spinning for eleven hours. And `cpp-mutants` and `cpp-campaign` grew a per-lane watchdog
that kills the whole process tree and reports **TIMEOUT** as a distinct outcome, counted as broken,
failing the lane.

**THREE DEFECTS INSIDE THE REMEDY, ONE INTERACTION, ONE ENTRY.** Naming it once:

> **AN EXPECTED NON-ZERO EXIT UNDER `set -e` KILLS THE SCRIPT THAT WAS SUPPOSED TO INTERPRET IT.**

| # | where | what it did |
|---|---|---|
| 1 | `run_lane`'s `wait` | killed the hung lane correctly, then **died itself, printing nothing** |
| 2 | `run_lane`'s two call sites | the function returned 124 and `; rc=$?` never ran |
| 3 | `build_and_sweep`'s sweep call | **killed the campaign at its first class** |

**The third is the worst, and for the reason the first two are not.** A patched sweep exits non-zero
**by design** — that non-zero *is* the detection the campaign counts — so the failure lands at the
first class **with both baselines already printed and the log reading healthy**. It is the same tell
as the entry itself: a run that has stopped, wearing the appearance of one that has not.

The first two took two rounds of induction to see, because round one looked like *"the lane stopped"*,
which is what the watchdog was supposed to produce. **A watchdog that cannot report is the defect it
was written to fix** — GF-1 recurring inside its own remedy, for the second time in this track.

**The general form, and it is the sharpest one this project has produced about lanes rather than
code:**

> **A HANG IS NOT A FAILURE, AND EVERY LANE MUST BE ABLE TO DECIDE.** A lane reports pass or fail; a
> lane that can do neither has stopped being a lane, and it stops *silently*, wearing the appearance
> of work. This is Amendment A4's "inconclusive is a first-class outcome" arriving one level out: A4
> is about a checker that ran and could not conclude, and this is about a checker that never
> finished. Both must be named, neither may be waited on, and neither is evidence.

**THE SPECIFIC TELL, AND IT IS WHAT COST THE ELEVEN HOURS.** *A stalled log is indistinguishable from
a slow one from outside.* Both show a last line and no error. So **progress must be read from
something that ADVANCES — a counter, a timestamp, a CPU-time delta — and never from the absence of an
error.** Every estimate given during this run was arithmetic over an appearance of progress, and the
appearance was the whole of the evidence.

**THE SHARPER OF THE TWO ENGINE-SIDE FIXES IS THE `RIFT_CHECK`, AND IT IS SHARPER FOR A REASON WORTH
STATING SEPARATELY.** The loops carried comments reading *"strictly advances, so this loop
terminates"* — and that invariant rested **entirely on the comparator being the order it claims to
be**, which is exactly the thing the mutant changes.

> **A TERMINATION ARGUMENT THAT ASSUMES THE THING BEING MUTATED IS NOT A TERMINATION ARGUMENT.**

What replaces it is a progress property that does *not* depend on the comparator under test: the user
key is compared **bytewise**, which is a fact about the merged order rather than about the tag half
of it. So the assertion survives the comparator being wrong and can catch it being wrong — which a
check written in terms of the comparator could not.

**What it cost.** Eleven and a half hours of wall clock and one confidently wrong estimate given to
Ansh. What it did not cost: any result. The 30 patches that completed before the hang all reported
correctly, and the campaign that ran before it was green.

---

### The baseline gate's running tally — four defects, none of them what it was built to detect

The mutant lane's baseline gate exists for one reason: **every lane a patch is declared against must
pass on the UNPATCHED tree first**, because a red baseline makes every subsequent failure
unattributable. It has now found **four defects, and not one of them was an unattributable kill.**

| # | phase | what it caught |
|---|---|---|
| 1 | B1.0 | `HARNESS-001` — `tar --exclude` silently dropped three files from every scratch copy |
| 2 | B1.9a | `HARNESS-007` — a `Slice` bound to a temporary string, caught by ASan in the baseline run |
| 3 | B1.9a | the `-Werror` failures six direction controls separated from real kills |
| 4 | B2.7 | `HARNESS-013`'s third `set -e` defect — `cpp-campaign` red on the unpatched tree |

**The argument this is evidence for: GATES THAT CHECK PRECONDITIONS BEAT GATES THAT CHECK OUTCOMES.**
A gate on the *outcome* can only find the failure it was written to look for. A gate on the
*precondition* — "is this measurement even attributable?" — runs the whole machine in a known-good
configuration on every invocation, and so finds whatever is wrong with the machine, including the
things nobody thought to look for. Four for four, none of them the thing it was built to detect.

---

### A category worth watching: shell-dialect assumptions, not logic errors

Both defects in CF-2's own execution were **assumptions about which shell was running**, not mistakes
in reasoning:

| where | the assumption | what it produced |
|---|---|---|
| the labelling script, invoked from **zsh** | that `sh label.sh $IDS` word-splits an unquoted parameter — **zsh does not** | all 28 names arrived as ONE argument; the loop ended after a single `ROT` line, and *"1 of 28 done"* read as slow progress |
| `cpp-scan` part 6, running under **`sh`** | that `<(...)` is available — it is a **bashism** | the check printed its own heading and then died with a syntax error: **a lane that looks like it ran** |

**This is a category now, because the lanes are written in three dialects.** The `Makefile`'s recipes
and every `scripts/*.sh` run under POSIX `sh`; the shell these are authored and tried out in is
`zsh`; and `awk` is a fourth language inside `cpp-scan-rules.awk`. **A construct that works when
tried interactively may not run in a lane, and the reverse.**

**WHICH MECHANISM CATCHES WHICH, RECORDED EXPLICITLY, BECAUSE A LINTER THAT CATCHES ONE OF TWO
DIALECT FAILURES WILL BE TRUSTED FOR BOTH:**

| defect | `sh -n` | the provenance rule | why |
|---|---|---|---|
| `<(...)` under `sh` | **CATCHES** | — | it is a **syntax** error; the parser refuses it without running anything |
| `$IDS` unsplit in zsh | **CANNOT** | **CATCHES** | `sh label.sh $IDS` is **syntactically perfect**. It is semantically empty, and no parser can know that the caller meant 28 arguments |

**So `sh -n` is a defence against exactly one of these two, and the temptation is to treat a green
`sh -n` as covering "shell problems".** It does not cover the class where a construct is valid in
both dialects and *means* something different in each — which is the more dangerous class, because it
produces a running program that does the wrong thing rather than one that refuses to start.

For that class the defence is the standing provenance rule: **read progress from something that
advances.** The process not existing is what said "1 of 28" was not slowness.

---

### The standing rule: a signal read without its provenance

**Six instances now, and they are listed rather than counted so the number is checkable.** Each is a
signal that was read as if it meant one thing while its provenance made it mean another — and in
every case the misreading was *indistinguishable from the correct reading* without going and looking
at where the number came from.

| # | instance | the signal | what its provenance made it mean |
|---|---|---|---|
| 1 | `HARNESS-010` | BM2 detected at 0 per mille | not "the defect is unreachable" but "the snapshot was hiding the damage" |
| 2 | `HARNESS-012` | the last `Sync`'s `promoted` flag | not "the group is durable" but "some file's sync promoted, and until B2 there was only one file" |
| 3 | `HARNESS-013` | a log whose last line is not an error | not "still working" but *a stalled log is indistinguishable from a slow one* |
| 4 | `HARNESS-014` | a cross-check reporting no violations | not "nothing violates this" but "I compared nothing" |
| 5 | `GF-6` | a detection rate that fell | not "power was lost" but "the denominator grew into territory where the class is undetectable" |
| 6 | **the truncated suite, B3.1** | **no `DropCheck` test failing under either reader mutant** | not "the checker cannot see fabrication" but **"an earlier `RIFT_CHECK` killed the process before those tests ran"** |
| 7 | the labelling run, B3.1 | *1 of 28 done* | not "slow" but **"dead after one, and a rate computed from it claimed 396 minutes for a run already over"** — the THIRD rate computed over an appearance |
| 8 | `ORACLE-includes-engine`'s label | a scan reporting **NONE** | not "the rule no longer catches it" but **"my `grep` pattern did not match the line the rule printed"** — the rule catches it, and a mutant whose target moves is **re-pointed, not deleted** |

**The sixth nearly produced the opposite ruling.** The aliasing condition would have been reported as
**unacceptable** — requiring the rig to grow its own parser — on a zero that was an artifact. The
form it takes is the same as the third:

> **A TEST BINARY THAT ABORTS REPORTS FEWER FAILURES THAN EXIST, AND FEWER FAILURES REPORTED IS
> INDISTINGUISHABLE FROM FEWER FAILURES EXISTING.**

**The mechanical answer, and it is cheap: PUT THE PROVENANCE IN THE SIGNAL.** `RIFT_CHECK`'s failure
path now prints

```
*** RIFT PARTIAL RUN: aborted here, so any count above this line is a LOWER BOUND
    and any absence is unproven ***
```

so a count grepped out of that output **carries the fact that it is partial**. Induced against
`BM35`, which aborts. The same principle covers instance 3 — read progress from something that
*advances* — and instance 4, where the lane now prints `parses N artifact(s)` rather than nothing.

**The rule.** *Before reporting what a number means, establish that the run which produced it
completed, that the comparison it summarises actually compared something, and that its denominator is
the one the previous measurement used.* Three different questions, one shape: **a signal is not
evidence until its provenance is.**

---

### HARNESS-021 and HARNESS-022 — the measuring instrument's own two failures

Filed together because they are one instrument's two ways of lying, found hours apart, **and because
the second was only findable after the first was fixed.**

| field | value |
|---|---|
| **Found by** | inspection of the printed numbers, both times — **not by any instrument** |
| **Phase** | B3.7b, before the number was published |
| **Mutant class** | `BM110` preserves the first, `BM111` the second |
| **Fix commit** | `19f1d45` (both), classes at `e70951a` |

**HARNESS-021 — it returned zero where it should have returned bytes.** Write amplification is bytes
written over bytes submitted, and the harness summed `LedgerEntry::durable_bytes_after` over Append
calls. That field is **the size of a file after a Sync has promoted it**, and is left at zero for an
Append. The first run printed **write amplification 0.00**.

> **IT ANNOUNCED ITSELF ONLY BECAUSE ZERO CANNOT BE TRUE.** A field that returned a plausible-but-wrong
> number in the same slot — a partial count, a stale size — would have been **published in
> `BENCHMARKS.md` as the result that decides `B3-D3`.** The instrument was saved by the magnitude of
> its own error.

**Fix:** the ledger records `append_bytes` **at the call**, and `MeasureAmplification` `RIFT_CHECK`s
the sum is non-zero — so the class cannot return quietly.

**HARNESS-022 — it printed a number without the condition it was true under.** A workload that stops
with L0 partly full **has not paid for those files' compaction**, so its write amplification reads
*low*. That is precisely the direction that flatters the conclusion `(b)` was being measured for.

**And it was found only after HARNESS-021 was fixed**, because until then the number was `0.00` and
there was nothing to be suspicious of. **A broken instrument hides the questions you would ask about a
working one.**

**Fix:** `L0 left` is a printed column, and the conclusion states the caveat when it is non-zero.

> **A NUMBER WHOSE CONDITIONS ARE NOT PRINTED BESIDE IT INVITES THE READER TO ASSUME THE BEST ONES.**

**WHAT BOTH HAVE IN COMMON IS WHO CAUGHT THEM.** Neither was caught by a test, a lane, a mutant or a
checker — **both were caught by reading the output**, which is the least reliable instrument this
project has and the only one that was pointed at the measurement. That is `GF-26` one level over, and
it is why `BM110` and `BM111` exist: the number that decides a design question now has classes under
it, so the next such failure fails a lane instead of depending on someone noticing.

**The counterfactual, since it is what makes the argument concrete:** had `HARNESS-021` produced 4.2
instead of 0.00, `BENCHMARKS.md` would today carry a wrong write-amplification curve, `B3-D3` would
have been ruled on it, and nothing in the repository would disagree.

---

### HARNESS-020 — a test corrected an assumption the author had asserted

**Symptom.** `FileLifetime.AnOpenIteratorHoldsItsInputFilesToo` failed with `expected 200, got 50`.

**Root cause: the expectation, not the engine.** An `Iterator` captures its `Version` and its sequence
**when it is created**, so it sees the database as of that moment — the 150 keys written afterwards
are invisible to it by construction. **50 was right.** I had written 200, having assumed an iterator
tracks the live database.

**Why it is worth an entry rather than a silent fix.** The frozen interface says what an iterator is,
and I asserted the opposite **in a test I was writing to prove something else**. Had the engine
happened to behave that way, the test would have passed and **encoded a false claim about the frozen
contract** in the file a future reader consults for what iterators do.

> **THE FIXTURE-FIRST ORDERING CAUGHT THE AUTHOR RATHER THAN THE CODE**, which is the case for it that
> is easiest to forget: it is usually argued as *"a checker written afterwards agrees with the
> implementation"*. This is the other direction — **a checker written first disagrees with the
> author**, and the author is who was wrong.

**Related but distinct from `HARNESS-006`'s family.** There, a checker wrong in the direction that
sends debugging to the wrong component. Here the checker was *right* and my expectation of it was
wrong — which is cheaper, and only because the engine did not share my misunderstanding.

**Fix.** The expectation is 50, with the reason at the assertion, and the comment says what the first
version was asserting: *that an iterator is live rather than snapshotted, which is not what the frozen
interface says.*

---

### HARNESS-019 — the revert that ate a step, after its own entry had been written

**Symptom.** Five mutant patches reported `patch does not apply` at once, and B3.5e's uncommitted
source work was gone: the exclusive-bound derivation, the tombstone-carrying compaction, the roller's
split, the run check's move onto opened tables. Rewritten from scratch.

**Root cause.** The induction helper reverted a patch with `git checkout -- engine-cpp/src`, which
reverts **everything uncommitted under that path** — not the patch it had applied.

**AND THAT IS EXACTLY `HARNESS-016`'s SECOND INSTANCE, ALREADY RECORDED.** The entry says it: *"one
reverted a directory to undo a patch."* When it first fired it cost one comment. It had a written
entry, a named general form — *a helper's side effect must be no wider than its purpose* — and a
diagnosis. **What it did not have was a fix in the tool.**

> **AN ENTRY THAT FIXES THE RECORD AND NOT THE TOOL SCHEDULES THE SAME DEFECT AT A LARGER SIZE.** The
> second firing was not a new lesson. It was the same lesson, charged at the size of the work in
> flight.

**Fix, in the tool this time.** `scripts/cpp-induce.sh` reverts with **`git apply -R`** — the exact
inverse of the apply, whose side effect is no wider than its purpose — and **refuses to run at all on
a dirty tree**, because an induction reads which assertion fails, and on a dirty tree that answer is
about a tree nobody will build again. It also reports an **abort** as a kill rather than as a
survival, which `FLOORS.txt`'s header has warned about since B3.1.

**The general form is `GF-20`'s sibling and belongs beside it.** `GF-20`: correctness resting on a
moving premise is a scheduled defect. This one:

> **A DEFECT WHOSE REMEDY IS "REMEMBER NOT TO DO THAT" IS A SCHEDULED DEFECT TOO.** The remedy has to
> live somewhere that cannot forget.

**What it cost, stated plainly:** one step's uncommitted work, rewritten from the design and the
tests, which is the second time this session that a tool's blast radius exceeded its job. The first
cost a comment; this cost an afternoon. There is no third.

**THE GENERAL FORM, AND IT IS `GF-20`'s SIBLING:**

> **WHEN A DEFECT'S REMEDY IS WRITTEN DOWN RATHER THAN BUILT, THE REMEDY HAS THE DEFECT'S OWN SHAPE
> AND COMES DUE ON THE DEFECT'S OWN SCHEDULE.**

`GF-20` says correctness resting on a moving premise is a *scheduled defect*. This says a **remedy**
resting on someone remembering is one too — and worse, because the entry reads as closure. The
catalogue said *a helper's side effect must be no wider than its purpose*, named the instance, and
left the helper unchanged. The next firing was not a new lesson; it was **the same lesson, charged at
the size of the work in flight.**

**The test: after writing an entry, ask what would have to change for the second instance to be
impossible rather than merely recognised.** If the answer is "nothing — I would notice", the entry is
not finished. Filed as `GF-23`.

---

### HARNESS-018 — a temporary bound to a `const std::string&`, and every Slice into it dangling

**Symptom.** A fixture asserted a tombstone's start was `"m"`. It read back `"e"`, and the block's
`ok()` assertion had already **passed** — the parse succeeded, the counts were right, and one field
held a byte from somewhere else.

**Root cause.** The test called `Check(UnboundedBlock("m", DelTag(9)), &t)`. `UnboundedBlock` returns
a `std::string` **by value**; `Check` takes `const std::string&`. The temporary lives to the end of
the full expression and then dies — and `RangeTombstone::start` is a **`Slice` into that block**. Every
field of every parsed tombstone pointed into freed memory by the time the assertions ran.

**What it establishes about `HARNESS-007`'s fix, and this is the entry.** B1 deleted
`Slice(std::string&&)` so a `Slice` could not bind directly to a temporary. That closes **direct**
binding and **cannot close binding through a parameter**: the temporary here never touches a `Slice`
constructor at all — it binds to a `const std::string&`, which is legal, ordinary, and exactly what
every by-const-reference API in the codebase accepts.

> **THE DELETED CONSTRUCTOR NARROWS THE CLASS; IT DOES NOT ELIMINATE IT.** The residual is: a
> temporary bound to a reference parameter, from which a `Slice` is later derived. No overload
> resolution sees that, because by then the temporary is a named reference like any other.

Stated the way §3.2.1 states the NVI choke point's residual — *"the claim is therefore 'bypassing
requires defeating two independent checks in one diff', not 'bypassing is impossible', and the second
sentence would be false."* The claim here is **"a `Slice` cannot bind directly to a temporary"**, not
**"a `Slice` cannot outlive its bytes"**, and the second sentence would be false.

**What covers the residual, honestly.** Nothing mechanical. ASan catches it *when the freed byte
differs* — it did not fail here, because the read landed in a still-mapped allocation. What caught it
was **an assertion on the parsed content rather than on the verdict**: the test checked what the
tombstone *said*, not merely that parsing succeeded. That is the standing habit worth having, and it
is the same habit `GF-2` demands of two-field assertions.

**Fix.** The three new fixtures bind their block to a local, with the reason written at the first one
so the next person copying the pattern copies the binding too.

---

### HARNESS-017 — a delimiter with no escape, caught by luck in the one column that is validated

**Symptom.** The mutant campaign refused its own baseline with
`BAD BM85-range-block-is-not-last: unknown regime "determined at B3.5b by induction"` — a complaint
about the wrong column entirely.

**Root cause.** `FLOORS.txt` is pipe-delimited with **no escape**, and the campaign parses it
**positionally**. The row named the guard it dies to by quoting the C++ verbatim:

```
killed-by-guard: ... RIFT_CHECK(range_offset == 0 || offset_ == range_end)
```

**The `or` operator is the column delimiter, doubled.** One field became three, every column shifted
right by two, and the row still looked like a row — same shape, same leading class name, plausible
text in every position.

**WHY IT WAS CAUGHT, AND IT IS NOT A REASON TO RELAX.** The displaced value landed in `regime`, which
is validated against a known set (`default` / `flush`). Two columns further along and it would have
**parsed cleanly**:

> **A DELIMITER WITH NO ESCAPE IS CAUGHT BY LUCK IN ANY COLUMN THAT IS NOT VALIDATED.** In this file
> the unvalidated columns include `covered-by:` — the one field a reader consults before deleting an
> assertion, and the field `GF-7` already established is worse wrong than absent.

**Fix, and the bound is an UPPER one.** `cpp-scan` part 6 now refuses any row with **more than seven
fields**. Not *exactly* seven: the file's own header says trailing columns may be omitted — *"an
absent column means default"* — and 5, 6 and 7 field rows all exist and are legal. **Demanding
exactly seven would have been a checker that refuses the normal case in the name of the abnormal
one**, which is the inversion §5.4 rejected candidate (a) for. Nothing legitimate produces more than
seven; a doubled delimiter produces nine.

Induced both ways against a row carrying `RIFT_CHECK(a == 0 || b == 1)`, and every one of the 123
existing rows audited.

**The general shape, and it is not about pipes.** A positional format with no escape puts the burden
on every future writer to know which characters are structural — and the writers here are humans
recording a finding, at the moment they are thinking about the finding rather than about the file.

---

### HARNESS-016 — a helper's side effect wider than its purpose, three times in one step

**Symptom.** A compaction test read the manifest to count tables per level, and the engine's next
manifest append failed: `kIoError: appending to a vanished file: db/MANIFEST-000001`.

**Root cause.** The helper called `sst::Manifest::Open`. That is not a reader — **every Open rotates**:
it replays, writes a NEW manifest, installs a new `CURRENT`, and **deletes the one it replaced**. The
test destroyed the live manifest underneath a running engine.

**The point is that this was already written down, in the other direction.** `manifest_format.h`
exists because `manifest.h` failed B3-D2a's artifact mark on `Manifest::Open(Env*, ...)` — *"opening a
manifest is AN ACT WITH AN OPINION about what the current state is, and it verifies, rotates and
installs."* The rule was applied to oracles and not generalised.

> **THE ARTIFACT/BELIEF SPLIT IS USUALLY ARGUED AS A RULE ABOUT WHAT A VERDICT MAY REST ON. IT IS
> ALSO A RULE ABOUT WHAT AN OBSERVATION MAY COST.** A path with an opinion has side effects, and a
> test that observes through one is running the engine, not watching it.

**Fix.** `rig/manifest_image.h` — a pure replay of manifest bytes, no Env, no rotation, no install.
The oracle's private copy was folded into it, so there is one parse rather than two, and the test now
reads `CURRENT` and the image it names.

**A SECOND INSTANCE, THE SAME HOUR, IN A THROWAWAY SCRIPT.** The helper that applies one mutant,
reads the failing assertion and reverts undid the patch with `git checkout -- engine-cpp/src` — which
reverts **everything uncommitted under that path**, not the patch it applied. It silently discarded an
unrelated comment written minutes earlier. The comment's absence then shifted the context lines of a
mutant patch generated against it, and the next lane run reported that patch as **`ROT` — "the code
moved and the mutation did not."** The lane was right, and it named the situation exactly.

> **A HELPER'S SIDE EFFECT MUST BE NO WIDER THAN ITS PURPOSE.** Both instances are the same shape:
> one observed through a path that rotates, one reverted through a path that reverts a directory. In
> both, the wider effect was invisible at the call site and showed up as something else entirely — a
> vanished file, a rotten patch.

**A THIRD INSTANCE, IN THE REMEDY FOR THE SECOND.** The patch generator built its diff with
`git diff`, which compares against **HEAD** — so on a dirty tree it silently bundled every unrelated
edit into the mutation. Two patches were written that way and **both carried six hunks instead of
one**; both came back `ROT`. The generator's job is to describe one mutation, and its scope was the
whole working tree.

> **A HELPER'S SIDE EFFECT MUST BE NO WIDER THAN ITS PURPOSE.** Three instances in one step: one
> *observed* through a path that rotates, one *reverted* a directory to undo a patch, one *diffed*
> against HEAD to describe an edit. In every case the wider effect was invisible at the call site and
> surfaced as something else — a vanished file, a rotten patch, a rotten patch again.

**A FOURTH, AT B3.5c, AND IT IS THE SAME FAMILY WITH THE DUPLICATION ON THE OTHER SIDE.** The
torn-record test runs one workload **twice** — a probe to find the sync ordinal, then the killed run
that tears at it. Converting the test from a `DeleteRange` expansion to a large batch, I changed the
**probe's** workload and left the killed run issuing the old one. The recorded ordinal then named a
different Env call, and the kill never fired.

> **TWO COPIES OF A WORKLOAD THAT MUST MATCH BYTE FOR BYTE IS THAT BUG WAITING.** Written once now,
> as `FillBigBatch`, with the reason at the definition rather than at the call sites.

**AND IT FAILED LOUDLY BY LUCK, AGAIN.** The divergence happened to make the kill not fire at all, so
the test failed on `expected kKilled, got ok`. **A divergence that still produced a kill — at a
different ordinal, tearing a different record — would have passed**, and the test would have gone on
reporting that a multi-block record is discarded whole while tearing something else entirely. That is
the second time in this step that a defect announced itself only because the corruption happened to
land somewhere validated (see `HARNESS-017`).

---

The generator now diffs **file against file**, with the reason at the top of it.

Recorded because the second and third cost only regenerated patches **only because the lane already
had a `ROT` outcome to report them as** — *"the code moved and the mutation did not"*, which is
exactly what happened. Without that outcome they would have presented as mutants that mysteriously
stopped applying, and the debugging would have started in the patches.

---

### HARNESS-015 — the registry cross-check matched a file that says it is NOT an oracle

**Symptom.** `rig/image_fixture.h` — a constructor, not a judge — was reported as `carries RIFT_ORACLE
and is not in ORACLES.txt`. Its header line reads: *"it CONSTRUCTS; it never judges, so it carries no
RIFT_ORACLE marker."* The check was a substring `grep`, so **a file could not say what it is not.**

**Root cause.** The marker was treated as an *occurrence of a token* rather than as **a declaration**.
Every real oracle already declares it identically: `// RIFT_ORACLE` as the file's **first line**.

**Fix.** Both directions of the cross-check now read `head -1 | grep '^// RIFT_ORACLE'`. Induced in
all three directions before it counted: an unregistered file declaring it (BAD), a registered file
losing it (BAD), and a prose mention below line 1 (clean — `image_fixture.h` itself, live in the
tree, is the standing witness).

**The pair with HARNESS-014 is the point.** That one **matched nothing** and would have passed
forever; this one **matched too much** and failed loudly. Same instrument, opposite failures, and
only the loud one is self-announcing.

> **A REGISTRY CROSS-CHECK HAS TWO FAILURE MODES AND ONLY ONE OF THEM TELLS YOU.** Both directions
> get induced, or the quiet one is what you have.

---

### GF-23 — a remedy that is written down rather than built has the defect's own shape

**Raised by** `HARNESS-019`, which is `HARNESS-016`'s second instance firing at a hundred times the
cost — see that entry for the mechanism.

> **WHEN A DEFECT'S REMEDY IS WRITTEN DOWN RATHER THAN BUILT, THE REMEDY HAS THE DEFECT'S OWN SHAPE
> AND COMES DUE ON THE DEFECT'S OWN SCHEDULE.**

It is `GF-20`'s sibling. `GF-20`: correctness resting on a premise that moves is a **scheduled
defect**. This: a **remedy** resting on someone remembering is one too — and it is worse in one
specific way. **The entry reads as closure.** A moving premise at least announces itself in the
comment that names it; a written-down remedy looks like the problem is handled, and the catalogue
grows a row that says so.

**THE TEST, AND IT IS ONE QUESTION:**

> After writing an entry, ask **what would have to change for the second instance to be IMPOSSIBLE
> rather than merely RECOGNISED.** If the answer is *"nothing — I would notice"*, the entry is not
> finished.

**What it is not.** Not a demand that every entry ship a mechanism — some defects have no mechanical
remedy, and `§3.2.1`'s residual bypass is the model for saying so out loud. The rule is that the
**choice** be made and stated, not that it always come out the same way. What is forbidden is the
third thing: an entry that neither builds the remedy nor admits it did not.

**Instances in this catalogue, both ways:**

| entry | remedy | outcome |
|---|---|---|
| `HARNESS-013` (the 11½-hour hang) | **built** — `LANE_TIMEOUT`, TIMEOUT as a distinct outcome | no second instance |
| `HARNESS-014` (registry matched nothing) | **built** — the cross-check induced both directions | no second instance |
| `HARNESS-017` (delimiter with no escape) | **built** — `cpp-scan` refuses >7 fields | no second instance |
| `HARNESS-016` (helper's blast radius) | **written down only** | **fired again, at a step's cost** |

**The pattern in that table is the argument.** Three built remedies, no recurrence. One written-down
remedy, one recurrence — and the recurrence cost the largest single loss of work in Track B.

**AND THE ARGUMENT WAS DEMONSTRATED WITHIN HOURS OF BEING WRITTEN.** The `FLOORS.txt` row recording
`BM105` contained `O(|S|)` — **two delimiters** — which is `HARNESS-017` exactly, recurring on the
same day. **The lane refused the row.** Nobody remembered anything; the check that was *built* caught
it at the moment of writing.

> **THAT IS THE WHOLE DIFFERENCE BETWEEN A RULE AND A REMEDY, ARRIVING AS ITS OWN EVIDENCE.** The rule
> was written that morning by an author who then broke it that afternoon and was stopped by a
> mechanism rather than by recall.

**A THIRD SAME-DAY INSTANCE, AND IT BROKE IN THIS FILE.** A B3.7 report named `HARNESS-021` and
`HARNESS-022` in its summary block **before either entry had been written** — two ids that resolved to
nothing, in the file whose job is recording gaps. Caught by re-reading the report, which is `GF-29`'s
least reliable instrument, and filed at `07044c3`.

**Three instances, all in one day, all in the hands of the author who had just written the rule.**
That is not carelessness worth apologising for; it is the rule's own claim being demonstrated:
**a remedy that lives in someone's attention fails at the rate attention fails**, which is often, and
independently of how recently the rule was written.

---

### HARNESS-024, -025, -026 — the differential rig's three findings about itself

**All three arrived before the rig's first finding about the engine could be trusted, and every one of
them made that finding possible.** Filed together because they are one instrument learning to be one.

**HARNESS-024 — the rig could not tell a failed reopen from an empty recovery.**
The first divergence read *"the engine recovered nothing."* The truth was that `DB::Open` had refused
the reopened image and the driver, having no field for that, left `recovered` empty either way.

> **A RIG THAT CANNOT REPORT WHY IT COULD NOT LOOK WILL REPORT THE ENGINE.**

`HARNESS-006`'s shape, and the diagnosis could not begin until the reopen's status was recorded — at
which point the real message appeared in one line: `key bounds disagree with the manifest`. **The
finding was `BUG-006` all along and was unreadable for the length of one edit.**

**HARNESS-025 — the judge compared against ONE watermark where the contract permits a RANGE.**
B1's exactness oracle had already worked this out as a two-element set: *"a Sync can complete on the
device with the kill preempting its return."* The differential inherited the problem and **not the
answer** — and it is WIDER here, because a `Sync` in this engine can run a **flush**, each step with
its own fsync, so a kill inside one can leave any prefix between the last completed watermark and the
in-flight target durable.

**It also named the wrong direction.** An empty recovered state matches sequence 0, and the judge
reported the **first** matching sequence — so a run that recovered *more* than promised, and happened
to be empty because a clear-everything ran above the watermark, was reported as *"recovered less."*
**A verdict that names the wrong direction sends the reader to the wrong component**, which is
`HARNESS-006`'s cost paid by the instrument built to avoid it.

**And the fix was nearly too permissive.** Widening to a range unconditionally would have forgiven the
defect the strict comparison exists to catch: **unsynced data surviving a clean shutdown.** A
hand-built test caught that — the widening applies only to a run cut short, which is a fact about the
**log** (ops after a kill carry sequence 0), not about the engine's opinion.

**HARNESS-026 — the rig issued one op per batch, so it never reached the intra-batch rules.**
Found by `BM114` surviving **both** lanes. A `DeleteRange` covering keys written earlier in the same
batch, a `Set` after one re-adding a key, two ranges merging to their union, batch atomicity — none of
it was exercised, and nothing said so.

> **§8's EXPECTATION WORKING IN THE OTHER DIRECTION: a rig that finds nothing has said something about
> itself.** Here a *mutant* that found nothing said it.

**Batches are now expressed by a shared sequence**, which needed no format change because that is what
a batch **is** — and a field recording what the sequences already say would be a second source of
truth about one fact.

---

**THE COMMON SHAPE, AND IT IS WORTH MORE THAN THE THREE ENTRIES.** Each was found by taking a result
the rig produced and asking *what would have to be true of the RIG for this to be what it says?*

- *"recovered nothing"* → **could the rig have failed to look?**
- *"recovered less than promised"* → **is one watermark the whole contract?**
- *"the mutant survived"* → **does the workload reach the thing it blinds?**

None of the three is discoverable by reading the rig. All three are discoverable by disbelieving one
of its outputs for one minute.

---

### HARNESS-023 — a shared-fixture check that asked whether the bytes parse, not what they say

**Symptom.** `BM112` swaps two provenance fields in **both** C++ ends — writer and reader together —
and the fixture corpus test **passed**.

**Root cause.** The check asserted `ok()`: *did these bytes parse?* With both ends swapped they parse
perfectly. The lengths are right, the section decodes, the checksum covers it — **the wrong string
simply lands in the wrong field.**

> **A SHARED-FIXTURE CHECK THAT ASKS "DOES IT PARSE?" CANNOT CATCH A DISAGREEMENT ABOUT WHAT THE BYTES
> MEAN — WHICH IS THE ONLY DISAGREEMENT IT EXISTS FOR.**

**It is `GF-25` in the fixture corpus:** an assertion about the **outcome** where one about the
**content** was needed. The corpus was built for exactly this class and could not see it.

**AND THE MUTANT IS WHAT FOUND IT, WHICH IS THE PART TO KEEP.** `BM112` was planted to *demonstrate*
the two-decoder pair's value, on the assumption the pair already worked. The first induction reported
it killed — by `AcceptsTheCanonicalArtifactAndReportsWhatItHolds`, an **in-test** fixture that does
assert values — and only checking *which* test fired showed the corpus test had passed.

> **A MUTANT KILLED BY THE WRONG INSTRUMENT IS A MUTANT THAT LOOKS COVERED.** The `covered-by:` label
> would have named a test that catches the class, while the check built for it stayed blind. That is
> `GF-7`'s shape in the label file, avoided only because the label is DETERMINED by induction and the
> determination reads *which* assertion failed.

**Fix.** Both decoders' corpus tests now assert **field by field** against values written in the
document's terms — `engine_commit == "abc123"`, `regime == "flush"`, the seed, the caps, the op, the
watermark, the recovered pair, the outcome — so a swap of any two fails even though the bytes are
structurally perfect.

**The residual, stated:** the two decoders still need not agree on *which* refusal fires when a file
breaks more than one rule, only that neither accepts it. That is deliberate — pinning the refusal
would couple the two implementations' internal ordering, which is the coupling the pair exists to
avoid.

---

### GF-33 — an assertion about the members present cannot fail on a member added

**Raised at B5.3**, by `Status::Code::kBusy` crossing the C boundary as an integer no header names.

Three declarations of one set exist: `Status::Code` in C++, `rift_status` in the C header, and
`codeError`'s switch in Go. The boundary held them together with **nine `static_assert`s, one per
code** — each pinning a value, all nine correct, and **all nine still correct after a tenth code was
added.** `ToC` was a `static_cast`, which agrees with anything. The engine gained a code, the boundary
compiled without a word, and `rift_status(9)` reached the wrapper.

> **AN ASSERTION ABOUT THE MEMBERS PRESENT CANNOT FAIL ON A MEMBER ADDED. ONLY AN ASSERTION ABOUT THE
> SET CAN.**

**It is `GF-25` one turn further.** `GF-25` was *assert the content, not the outcome*; this is *assert
the set, not its members*. Both are the same mistake about what an assertion ranges over, and both
produce the same symptom: a check that is individually true, collectively silent, and reads in review
as thorough **because there are so many of them**. Nine asserts look like more rigour than one switch.
They are less.

**The fix is a mechanism that already existed three files away.** `status.h` says *"NO `default:` ARM
MAY EVER SWITCH OVER THIS TYPE — `-Werror=switch` is what makes adding an enumerator a build failure
until somebody classifies it."* `ToC` is the one place two independently-declared enums must agree, and
it was the one place not using it. The C++ side now refuses to compile; the Go side has no
exhaustiveness check to borrow, so `TestEveryStatusTheHeaderDeclaresIsMapped` **parses `rift.h`** and
holds the wrapper to it — a test that derives its set from somewhere other than the code under test is
the only kind that can fail on an addition.

**The operational form, for I1 and I2:** whenever the same set is declared in more than one place, the
question is not *"is each member right"* but *"what makes adding one break something."* If the answer
is *"the reviewer notices"*, there is no check.

---

### GF-34 — a second implementation is only a check when something runs both

**Raised by `BM118`'s survival**, B5.3.

The rig computes the backpressure threshold independently of the engine, on purpose: `AdjudicateBusy`
is the harness's arithmetic, the engine has its own, and the pair exists so neither is believed. `BM118`
changed the engine's comparison from `>` to `>=`. It was aimed at the test asserting exactly that edge
— `TheBoundaryValueItselfIsNotOwed` — **and survived**, because that test calls `AdjudicateBusy`. It
asserts the harness's edge with great precision and says nothing whatever about the engine's.

> **TWO IMPLEMENTATIONS OF ONE RULE ARE A CHECK ONLY WHERE SOMETHING RUNS BOTH AND COMPARES THEM. A
> TEST OF EITHER ONE ALONE IS A TEST OF THAT ONE ALONE — AND IT LOOKS EXACTLY LIKE THE CHECK.**

This is the *reverse* of `BM113`. There, a fix made the defect unrepresentable through an API and the
mutant survived because there was nothing left to catch. Here nothing was fixed and nothing was
unrepresentable: **the covering test simply pointed at the wrong one of two things with the same
name.** Both survivals were the mutant working; only one of them meant the code was safe.

The workload was reached rather than the mutant relabelled (`GF-16`): the covering test now drives real
writes onto a threshold chosen as an exact multiple of one write's cost, so the backlog lands on
`busy_bytes` exactly — legal under `>`, refused under `>=`, and nothing else in the suite separates
them.

---

### GF-35 — a policy that latches is indistinguishable from one that works

**Raised by `BM119`**, B5.3, and it is a shape three mechanisms in this repo now share.

`BM119` never releases the in-flight charge, so the backlog only grows: after enough bytes every write
returns `kBusy` forever and no amount of draining clears it. **A database that refuses all writes and
reports a legitimate, documented reason for each one.** The rig's entire forward assertion passes under
it — backpressure is owed, backpressure is signalled, the predicate holds on every write.

> **THE INTERESTING HALF OF A MECHANISM THAT SAYS "NOT NOW" IS WHEN IT STARTS SAYING "NOW".**

The same shape as the seam's consecutive-zero-write bound, and the same shape a lease that never
expires would have. In each, the safety direction is asserted everywhere and the liveness direction is
asserted nowhere, because the safety direction is the one the mechanism is *for* — and a mechanism
stuck permanently in its own purpose looks, to every test of that purpose, like it is working
perfectly.

**Mechanically:** any test that asserts a refusal must be followed by the thing that clears it and a
write that succeeds. Two lines, and they are the only two lines in `BM119`'s covering test that do
anything.

---

### GF-36 — a lane that depends on an artifact it does not build reports the absence as success

**Raised by `BM120`'s survival**, B5.4, and the survival was worth more than the mutant.

`BM120` makes snapshots pin nothing. It is aimed at the cgo differential, which takes its workloads
from real `rift_diff` artifacts and — correctly, and with a comment saying so — **skips** when that
binary is absent. The `cpp-cgo` lane built `rift_capi` and not `rift_diff`. The mutant runner deletes
`engine-cpp/build` in the copied tree. So the test skipped, `go test` reported `ok`, the lane went
green, and the mutant survived.

> **A SKIP INSIDE A PASSING TEST BINARY IS A GREEN LANE. THE TEST KNEW IT HAD NOT RUN AND SAID SO; THE
> LANE HAD NO WAY TO HEAR IT.**

**The skip was right and the lane was wrong**, which is the part worth keeping. Every instinct on
finding this points at the skip — make it a failure, make it loud, make it conditional. All of that
would break a Go-only checkout for no reason. The defect is that a lane declared a dependency in a
comment and not in its recipe.

**And the failure mode is silent in the direction that matters.** A lane that cannot run its check
looks *exactly* like a lane whose check passed — same exit code, same absence of output, less time.
Nothing about a green `cpp-cgo` distinguished "the boundary agrees with the model across 18 seeded
workloads" from "the boundary was never asked."

**What found it was the mutant, and only the mutant.** Every human-facing signal said the lane was
fine: it was green on the real tree, where the binary happens to exist because some other lane built
it. The class of bug — *a lane passing for a reason unrelated to its claim* — is `HARNESS-002`'s (a
warm build directory) and `BM21`'s. Both were also found by a mutant surviving, and in all three the
mutant was the only thing in the repo that ran the lane in a state a developer never sees.

**Operationally, for I1 and I2:** a lane's recipe must build everything its tests refuse to run
without. Grep for `Skipf` in anything a lane runs and check the recipe produces what each one names.
Done at B5's close: two artifact-dependent skips exist in the tree, both now built by the lanes that
run them; the rest are `-short` guards.

#### PROMOTED at B5's close — this is the vacuous-green class in its LANE-DEPENDENCY form, and it is new

Vacuous green has appeared in this project three times before, and each time the lane was running the
right test in the wrong *state*: `HARNESS-002`, a warm build directory; `BM21`, which survived until
the directory was made cold; `GF-16`, a workload that could not reach a mutant's precondition. **This
one is different in kind: the lane was not running the test at all.**

> **THE THREE EARLIER INSTANCES WERE A CHECK THAT COULD NOT FAIL. THIS ONE IS A CHECK THAT WAS NEVER
> INVOKED — AND THE TWO ARE INDISTINGUISHABLE FROM OUTSIDE, BECAUSE A LANE REPORTS AN EXIT CODE AND
> NOT A COUNT OF QUESTIONS ASKED.**

`GF-37`'s three instances belong here, under the same heading, because they are the same failure
reaching the measurement side: an instrument silently configured by its surroundings — a `Debug`
build, a single unreplicated run, a `SIGPIPE`'d pipeline whose `grep` exited 0. In every one of the
four, **the artifact produced looked exactly like a correct one**, and in every one the only thing
that noticed was something that knew what the answer should roughly be.

---

### GF-37 — an instrument inherits the configuration of the lane that built it, and says nothing about it

**Raised at B5.5**, twice in one afternoon, in opposite directions.

**First: the build type.** The benchmark was built into `engine-cpp/build/test`, because that is where
every other Track B binary lives and the harness simply reused the path. That directory is configured
`Debug` — correct for every lane that uses it, assertions on and optimiser off. The table it produced
reported ~4 µs for a single memtable `Set` and a `readrandom` cost that **did not move with batch
size**, which is not a slow engine; it is a description of `-O0`.

> **A BENCHMARK FROM A DEBUG BUILD IS NOT A SLOW NUMBER. IT IS NOT A NUMBER.**

**Second: the run count.** The first tables reported boundary costs of **−23%** and **−16%** — the cgo
column beating the native one, which cannot happen, because the cgo column does everything the native
column does and *then* crosses a boundary. **The impossible number was the only reason the variance
was noticed.** Had the noise landed at +8% instead of −23%, it would have been published as a finding
and nothing in the table would have contradicted it.

**The two share a shape.** A measurement carries no record of the conditions it was taken under. A
correctness test that runs under the wrong configuration usually *fails*; a benchmark under the wrong
configuration **succeeds and prints a number**, and the number looks exactly like a right one. Every
signal that something was wrong here came from a reader knowing what the number should roughly be —
which is not a mechanism, and does not survive the first measurement of something unfamiliar.

**Mechanically, and it is what `BENCHMARKS.md` now requires:** the build type, the statistic, the run
count and the machine are part of the result and are printed beside it. The bench lane uses its **own
Release directory** rather than a flag on the shared one, deliberately — the sweep's kill-point counts
and every floor in `FLOORS.txt` are measured against `Debug` builds, and a lane that quietly changed
build type underneath them would move denominators nobody was watching.

**And the third instance, in the shell, the same day:** the first full table was taken with
`make cpp-bench | head -26`. `head` closed the pipe, `make` died of `SIGPIPE`, `grep` exited 0, and
the run reported success with two thirds of its rows missing. **A pipeline's exit status is its last
command's.** Same shape once more: the instrument was configured by its surroundings and reported
nothing about it.

---

### GF-38 — a periodic action guarded by a second condition fires only where the two coincide

**Raised at B5.5**, and it killed the same process twice for two different reasons.

`engine/model` copies its whole entry slice per apply and retains every version until durability
advances — correct for a reference engine, since a crash dropping `pending` *is* the mechanism. A
benchmark that never advanced it held 50,000 versions of up to 50,000 entries and was OOM-killed.

The fix drained periodically:

```go
if in == batch {
    seq, _ := e.Apply(b, false)
    if i%1024 == 0 { drain(seq) }   // <-- inside the batch-boundary branch
}
```

`i` is the **operation** index; the branch runs on **apply** boundaries. At `batch == 1` every
operation is an apply and the drain fires every 1024. At `batch == 8`, `i % 1024 == 0` requires
`i ∈ {0, 1024, 2048…}` and the branch requires `(i+1) % 8 == 0` — **the two never coincide.** At
`batch == 64`, never either. The drain existed, read correctly, was tested at `batch == 1`, and was
dead everywhere else. The process died again, at a different row, from the identical cause.

> **A PERIODIC ACTION NESTED INSIDE A CONDITIONAL MUST COUNT THE THING THAT ACTUALLY HAPPENS, NOT THE
> THING BEING ITERATED. `every N iterations` INSIDE `if (rare)` MEANS `every N iterations THAT ARE
> ALSO rare` — WHICH IS OFTEN NEVER.**

**It is `GF-16` in the code rather than in a test.** There, a workload could not reach a mutant's
precondition and the mutant survived. Here, a guard could not reach its own action and the action
never ran. Both are a condition that reads as satisfiable and is not, and in both cases the only
evidence was an outcome nobody could otherwise explain.

The counter now counts applies. It is one more variable and it is the variable the action is actually
periodic in.

---

### GF-39 — two tracks with separate lane sets accumulate an unbounded debt payable in one instant

**Raised at B5.2, promoted out of it by Ansh at B5's close as the finding with the most downstream
value in the phase.**

`make determinism` was **red**, and had been **since B4**. `engine/differential` sits under the
`engine/...` core pattern — which is what puts `engine/model` in scope — and was never classified.
Nothing noticed for two phases.

**Nothing was broken. That is the point.** The judge was correct, its own lane (`cpp-diff`) was green,
every C++ lane was green, and the package did exactly what it was designed to do. What had happened is
that **Track B runs `CPP_LANES` and Track A runs `ci`**, the two sets are disjoint, and Track B had
been adding **Go packages** for two phases.

> **TRACK A'S LANE SET WOULD HAVE MET EVERY GO PACKAGE TRACK B HAS EVER WRITTEN FOR THE FIRST TIME
> **AT MERGE**, ALL AT ONCE, ON A DAY DEDICATED TO SOMETHING ELSE.**

**The general form:**

> **TWO TRACKS WITH SEPARATE LANE SETS DO NOT DIVERGE GRADUALLY. THEY ACCUMULATE A DEBT THAT IS
> INVISIBLE WHILE IT GROWS, BOUNDED BY NOTHING, AND PAYABLE IN A SINGLE INSTANT — THE MERGE.**

**And the merge is the worst possible moment to discover it**, for three reasons that compound:

1. **Attribution is gone.** Nineteen files land together. A red lane names a symptom, and the commit
   that caused it is somewhere in two phases of work.
2. **The pressure is wrong.** A merge is a moment for integrating finished work, not for making
   scope decisions — and *"is `engine/differential` core scope?"* is a scope decision under `[A5]`,
   which is exactly the kind that should be made deliberately and recorded.
3. **The cheap fix is available and wrong.** With a merge blocked and a lane red, the fastest route
   is a package exclusion — and `A5` bans package exclusions specifically because they are what
   somebody reaches for under exactly this pressure.

**The remedy is not "run more lanes"; it is that the debt must be paid continuously.** Every Track B
step that adds a Go artifact runs Track A's lane set on it in that step. Cost: minutes. The
alternative is not a larger cost later — it is an **unbounded** one, since nothing caps how much
unclassified work can accumulate between merges.

**Confirmed at B5's close, on Ansh's instruction, that nothing else is outstanding.** Every Go file
Track B has added — nineteen, across `engine/differential/`, `engine/riftcgo/`, and
`tools/determinismcheck/` — was enumerated against `main`, and Track A's full push lane
(`build lint test race blind smoke mutants`, including the three lanes Track B had never run) is green
on `rift-b`.

**The classification itself, recorded because it is a scope decision and not a fix.** Both packages
are **exclusions** by `scope.go`'s own test — *"a package that needs a goroutine is an exclusion; a
package that needs one `time.Now` is a hatch"* — and `sync` is unhatchable in core scope:

- `engine/differential` is a **judge**. Nothing in it executes during a simulated run; it reads
  artifact files a finished run left behind. That is `sim/checker`'s situation exactly and it gets
  `sim/checker`'s answer. **Its independence is why it cannot be in core:** reading bytes neither
  engine handed it is the whole mechanism by which it is a second opinion rather than a mirror.
- `engine/riftcgo` needs `sync` for its durability callbacks and `unsafe` by construction — a cgo
  boundary *is* pointer identity and layout, which is what core scope exists to keep out. And it is
  the constitution's own scoping rather than a hole: *"deterministic-replay guarantees are scoped to
  sim runs on `engine/model`."*

**The build-tag half, and the distinction is exact.** `riftcgo` cannot link without the C++ archive,
so it carries a tag and Track A's `make test` skips it. A tag removes a package from `./...`
entirely — so it could sit unanalyzed forever with every lane green.

> **A LOAD GATE CATCHES A PACKAGE THAT FAILS TO LOAD. IT DOES NOT CATCH ONE THAT WAS NEVER OFFERED.
> BOTH FAILURES PRODUCE THE SAME SILENCE.**

`TestHatchRegistry` now asserts **by name** that the package was among those loaded, and the
determinism pass loads with the tag (it only typechecks, which needs no archive — that is what the
`${SRCDIR}` `CFLAGS` bought). Induced: dropping the tag from the test's `BuildFlags` fires the
assertion.

---

### GF-32 — a doc written before its code can specify a mechanism the code makes unnecessary

**Raised by `B5-D2`, B5.0 — the phase's first finding, and it is not a doc erratum.**

`DESIGN-B5` §2 specified a catch-all at every `extern "C"` entry point, plus a function that throws
through the boundary to induce it. The build refused: `cannot use 'try' with exceptions disabled`.
**The archive has been `-fno-exceptions` since B1.** `throw` does not compile, `try` does not compile,
and `operator new` **aborts** rather than throwing.

> **A DESIGN DOC WRITTEN BEFORE ITS CODE CAN SPECIFY A MECHANISM THAT THE CODE'S OWN CONSTRAINTS MAKE
> UNNECESSARY.**

**AND THE CHEAP REPAIR IS TO WEAKEN THE CODE UNTIL IT MATCHES THE DOC.** That direction is always
available, and it is always wrong. Here it was four small steps, each individually reasonable:

1. drop `-fno-exceptions` from `RIFT_CXX_FLAGS`;
2. add the `try`/`catch (...)` wrapper the doc specifies;
3. make `rift_test_throw` actually throw, so the backstop is induced;
4. ship a **weaker property that matches a paragraph.**

> **IT IS INVISIBLE IN REVIEW, BECAUSE THE RESULT IS A DOCUMENT AND AN IMPLEMENTATION IN AGREEMENT.**
> A future reader meeting a catch-all with a design citation beside it will assume it was reasoned —
> and it *was*, just before the reasoning met the build.

**WHAT THE WRAPPER WOULD HAVE COST, which is the half that makes this a finding rather than a
near-miss.** A `catch (...)` does not merely add a line: it **converts an exception into a code and
loses what the exception was.** A boundary that reports `RIFT_INTERNAL` for everything is a boundary
where **every failure looks the same** — allocation failure, a contract violation, a future
contributor's `throw` — and the first thing anyone debugging across it would want is the one thing it
discarded.

**So `GF-31` is confirmed from the other direction.** There, a fix made a defect unrepresentable and
the mutant's survival was the evidence. Here, **the compiler supplies the guarantee the wrapper would
have approximated**, and supplies it more strongly:

| | catch-all | `-fno-exceptions` |
|---|---|---|
| an exception crossing | caught, **identity lost** | **cannot exist** |
| enforcement | a rule each entry point remembers | a compiler flag |
| what a reviewer must check | every function has the wrapper | one line of the build |

**The rule that follows, and it is about where a claim lives:**

> **NOTHING IN THE SOURCE CAN ASSERT ITS OWN BUILD. A PROPERTY HELD BY A FLAG IS ENFORCED WHERE FLAGS
> LIVE, OR IT IS NOT ENFORCED.**

`cpp-scan` part 7 reads the compile options and refuses a tree where `-fno-exceptions` is missing or
does not reach `rift_capi` — induced by removing it.

**The tell, for next time.** When a doc specifies a *mechanism* rather than a *property*, the mechanism
is a guess about how the property will be obtained. **Write the property; discover the mechanism.**
§2 should have said *no exception crosses this boundary* and left how open — and it now does, with the
mechanism recorded as what the code turned out to already have.

---

### GF-31 — a fix that makes a defect unrepresentable costs the instrument that detected it

**Raised by** `BUG-006`'s fix and `BM113`'s survival. **Both halves are recorded, because the free-win
reading is the wrong one.**

**THE STRENGTHENING.** `BUG-006` was two implementations of one rule disagreeing. The fix was not to
correct the wrong one — it was to make the rule **one function**, `WidensUpperBound`, taking a **bare
user key**. Through that signature there is no internal key to compare against, so **the defect cannot
be written**.

> **A FIX THAT MAKES A DEFECT UNREPRESENTABLE IS STRONGER THAN ONE THAT MAKES IT WRONG.**

**AND NO MUTANT CAN ASSERT THE DIFFERENCE AT THE SITE IT PROTECTS.** `BM113` was aimed there and
**survived** — an equivalent mutant, because the mutated comparison behaves identically. **The
survival is the evidence for the strengthening**, which is a strange and useful thing: the class was
re-aimed at the caller, where an inlining edit could still reintroduce it.

**THE COST, AND IT IS REAL.** Two implementations of one rule can **disagree**, and a disagreement is
**detectable by comparison** — which is precisely what `VerifyTables` did, loudly, on every Open. One
implementation cannot disagree with itself.

| before the fix | after |
|---|---|
| two rules, comparable | one rule, nothing to compare |
| a disagreement is caught by `VerifyTables` | the rule can be **wrong in one place and consistent everywhere** |
| the reproduction asserts *writer equals classifier* | that assertion now passes under `BM113` |

> **COLLAPSING TWO IMPLEMENTATIONS INTO ONE REMOVES A FAILURE CLASS AND REMOVES THE INSTRUMENT THAT
> DETECTED IT.** What is left must be asserted **against the invariant**, not against a second
> implementation.

Hence `BoundWideningIsAStatementAboutUserKeys`, which tests the predicate directly and in **both tag
directions** — a rule comparing internal keys would be right about one of them by accident.

**AND IT IS NOT AN ARGUMENT AGAINST CONSOLIDATION.** `BUG-006` cost a permanently unopenable database;
the instrument it removed only ever fired *because* the defect existed. The point is that **the ledger
has two entries, not one** — and a consolidation that does not add the invariant assertion has spent
an instrument and bought nothing to replace it.

**The tension with `B3-D2b` is deliberate and worth stating**, because these look contradictory: the
harness keeps a **second** implementation of `Covers` on purpose, and the engine collapses to **one**
implementation of `WidensUpperBound` on purpose.

> **THE DIFFERENCE IS WHO THE TWO IMPLEMENTATIONS BELONG TO.** Two inside one engine are a
> maintenance hazard whose disagreement is a bug. **One in the engine and one in the checker are an
> oracle** — and collapsing *those* is the shared blind spot `B3-D2b` forbids.

---

### GF-30 — disbelieve a new instrument's first three outputs

**Raised by** the differential rig's three findings about **itself** (`HARNESS-024`, `-025`, `-026`),
which arrived before its first finding about the engine could be trusted.

**THE METHOD, AND IT IS ONE QUESTION:**

> **TAKE A RESULT THE RIG PRODUCED AND ASK WHAT WOULD HAVE TO BE TRUE OF THE RIG FOR THIS TO BE WHAT
> IT SAYS.**

| the output | the question | what it was |
|---|---|---|
| *"recovered nothing"* | **could the rig have failed to look?** | the reopen had failed and the driver had no field for it |
| *"recovered less than promised"* | **is one watermark the whole contract?** | the contract permits a range; B1's oracle already knew |
| *"the mutant survived"* | **does the workload reach the thing it blinds?** | one op per batch, so the intra-batch rules were never exercised |

> **NONE OF THE THREE IS DISCOVERABLE BY READING THE RIG. ALL THREE ARE DISCOVERABLE BY DISBELIEVING
> ONE OF ITS OUTPUTS FOR ONE MINUTE.**

**Why reading does not find them.** Each is a **missing** case, not a wrong one: an unrecorded status,
an unmodelled second element, an unreached shape. Reading checks that what is there is right, and all
three rigs were right about everything they contained.

**STANDING PRACTICE FOR EVERY NEW INSTRUMENT — I1 and I2 included.** A rig's **first three outputs are
the cheapest place it will ever be wrong**: nothing depends on them yet, no result has been reported
from them, and the author still remembers what the code was supposed to do. The same doubt applied
after fifty runs costs a retraction.

**It composes with `GF-29`.** That one says a single instrument defect raises the prior on another —
so the first disbelief is not the last, and the moment to look again is immediately after the first
fix, while the output is being read closely for the first time.

---

### GF-29 — a broken instrument hides the questions you would ask about a working one

**Raised by** `HARNESS-021` and `HARNESS-022`, B3.7b — the same instrument, two defects, found hours
apart and **the second only findable after the first was fixed.**

`HARNESS-021` made write amplification print **`0.00`**. While that number stood there was **nothing
to be suspicious of**: no reason to ask what conditions it was true under, because it plainly was not
true at all. Only once it read `8.08` did the next question become askable — *is this a steady-state
value or a snapshot mid-cycle?* — and the answer was `HARNESS-022`.

> **FIXING ONE INSTRUMENT DEFECT IS WHAT MAKES THE NEXT ONE FINDABLE. SO A SINGLE DEFECT IN AN
> INSTRUMENT SHOULD RAISE THE PRIOR ON THERE BEING ANOTHER**, and the moment to look is immediately
> after the first fix, while the output is being read closely for the first time.

**AND THE COUNTERFACTUAL IS THE ARGUMENT, which is why it is recorded as a number and not a worry:**

| what the broken field returned | what would have happened |
|---|---|
| **`0.00`** — impossible | caught in one reading; the instrument was **saved by the magnitude of its own error** |
| **`4.2`** — plausible | `BENCHMARKS.md` would carry a wrong write-amplification curve, `B3-D3` would have been ruled on it, and **nothing in the repository would disagree** |

> **A VALUE THAT CANNOT BE TRUE IS A GIFT. THE DANGEROUS INSTRUMENT DEFECT IS THE ONE THAT RETURNS
> SOMETHING PLAUSIBLE.**

**What follows for how instruments are built.** Prefer a reading whose failure mode is *impossible*
over one whose failure mode is *merely wrong*: a count that must be non-zero, a ratio with a floor
that physics forbids crossing (*every byte is written to the WAL and again to a table, so write
amplification below 2 is the instrument, not the engine*), a bound the workload cannot exceed. Those
are the assertions `AmpInstrument.*` now carries, and they exist because **a plausible wrong number is
indistinguishable from a result.**

**THE OPERATIONAL CONSEQUENCE, WHICH IS THE PART THAT CHANGES WHAT YOU DO NEXT:**

> **WHEN AN INSTRUMENT IS FOUND WRONG, THE NEXT QUESTION IS WHAT IT WAS HIDING — NOT WHETHER IT IS
> FIXED.**

Fixing it is the easy half and feels like completion. The instrument was **producing readings the
whole time it was wrong**, and every one of them was accepted; so the fix does not end the incident,
it **starts the audit** of what those readings were used to conclude. At B3.7b that audit was one
question long — *is `8.08` steady-state?* — and it found `HARNESS-022`.

**It is `GF-26` from the other side.** `GF-26` says an instrument with no class floored against it has
unknown sensitivity. This says an instrument that is *itself* broken has unknown sensitivity **about
its own defects** — and both are answered the same way: put a class under the instrument, not only
under the thing it measures.

---

### GF-26 — a new regime is not landed until one class is floored against it

**Raised by** the `compact` sweep regime, B3.7a.

The regime landed at **3545 kill points, 0 violations**. That reads as a strong result and it was a
**green with unknown sensitivity**: the sweep visited every Env call the compaction makes, and
nothing in it had been shown capable of *detecting* anything there.

> **A SWEEP THAT VISITS A PATH PROVES THE ENGINE RECOVERS THERE. IT SAYS NOTHING ABOUT WHETHER A
> DEFECT THERE WOULD BE DETECTED.**

**IT IS THE VACUOUS-GREEN SHAPE AT REGIME GRANULARITY** rather than at checker granularity — `GF-1`'s
family, one level up. `GF-1` asks whether a *lane* verifies an absence; this asks whether a *whole
regime* does. The failure looks better than `GF-1`'s, because a large kill-point count and a zero
violation count read as thorough.

**`BM109` is what closed it**: remove the directory sync after the compaction's output files, and the
sweep reports **10 detections of 3530, first at kill point 663**. The regime now has a floor, so a
change that quietly stops it reaching the compaction fails the campaign instead of reporting 3545
green points over a path it no longer enters.

> **THE STANDING RULE: A NEW REGIME IS NOT LANDED UNTIL AT LEAST ONE CLASS IS FLOORED AGAINST IT.**

**Stated as standing because B4 will add regimes** — the differential rig against `engine/model`, the
crash-consistency sweep at other cap settings — and the same question arrives with each. The cost is
one mutant per regime, which is small beside a regime whose green means only that it ran.

---

### GF-27 — extending a regime is paid for by every floor already measured against it

**Raised by** §8.2a's decision at B3.7a.

Reaching the L0 compaction trigger needs four flushes — roughly four times the `flush` regime's whole
workload. Folding it into `flush` was the obvious move and would have been wrong:

> **AN EXTENSION THAT MULTIPLIES A REGIME'S KILL-POINT COUNT DILUTES EVERY CLASS ALREADY MEASURED
> AGAINST IT. THE COST OF EXTENDING A REGIME IS PAID BY EVERY EXISTING FLOOR.**

Every rate in `FLOORS.txt` is a fraction of that count. Quadruple the denominator and every B2 rate
falls, **while no class has lost any power at all** — the classes are exactly as detectable as they
were, at points that are now a smaller share of a larger space.

**B2 already paid this once**: the manifest took `default` from 175 to 300, every rate fell, not one
detection count did. The lesson then was `GF-6` — *keep a count floor beside every rate floor*. The
lesson now is the one before it: **do not move the denominator unless the work requires it.**

**So the answer is a separate regime, not a wider one.** `default` and `flush` stayed byte-identical
at 305 and 990; no floor moved; the re-measurement obligation was discharged **by being made
unnecessary rather than by being performed.**

**It is the same logic as the regime column itself** (§8.4, ratified at A6): a number measured at
non-default caps never aggregates with a default-cap number, because the two denominators are
incomparable. **A regime is the unit at which measurements are comparable** — so the way to add
coverage without invalidating measurements is to add a unit, not to widen one.

---

### GF-24 — a count with nothing to derive beats a threshold with a justification

**Raised by** B3.6's first attempt at file lifetime.

Retiring a compaction input, the question is *does anyone still hold this table?* The first version
compared `shared_ptr::use_count()` at the retirement site against a threshold **worked out by
reasoning**:

```cpp
// `t` is one reference, and the caller's `in_l0`/`in_l1` vector is another.
// Anything above two is a reader.
if (t.use_count() > 2) { ... }
```

**A snapshot's input file was deleted underneath it.**

**THE SPECIFIC ERROR IS SUBTLER THAN THE RULE, AND IT IS WHY THE RULE IS NEEDED.** `t` is declared
`const std::shared_ptr<sst::Table>&` — **a reference to the vector's element, not a copy.** It adds
nothing to the count. The arithmetic was correct about a world with one more reference in it than
this one has.

> **REASONING CANNOT CATCH THIS CLASS, BECAUSE THE REASONING IS WHAT IS WRONG.** Re-reading the
> justification confirms it. Every step follows; the premise about how many holders exist is the
> defect, and it is the same premise the re-reading uses.

**The remedy is structural, and it generalises past reference counts:**

> **PREFER A COUNT WITH NOTHING TO DERIVE OVER A THRESHOLD WITH A JUSTIFICATION. THE JUSTIFICATION IS
> THE PART THAT GOES STALE.**

Every retired table now goes on **one list**, and the count is taken in **one place** with **one
holder to subtract**: `use_count() == 1` on `obsolete_` means the list is the only holder. There is no
arithmetic, so there is nothing to get wrong — and, more to the point, **adding a local anywhere else
cannot move the answer.** The original threshold would have silently changed meaning the first time
someone introduced a variable between the vector and the call.

**The family.** It is `GF-13`'s cousin — *a bound derived from another instrument's measurement cannot
be raised* — with the derivation done in a comment rather than by an instrument. `GF-13` says where a
number should come from; this says a number you have to *argue for* is a number that will be wrong
later, whoever argues.

---

### GF-28 — a guard phrased as "not the other one" changes meaning when a third appears

**Raised by** the sweep workload's flush gate, B3.7a.

```cpp
if (regime != SweepRegime::kFlush) return;   // written when there were two
```

It reads *"only the flush regime continues"* and means *"every regime except flush stops"*. Those are
the same sentence with two regimes and different sentences with three. When `compact` arrived it
**silently returned** — so the first compaction sweep ran the six-key default workload and reported
**305 kill points with a census containing no compaction at all.**

> **THE FAILURE IS SILENT BECAUSE THE GUARD STILL EVALUATES.** Nothing is malformed, nothing throws,
> no case is unhandled. A closed `switch` would have failed the build on the new enumerator; a
> comparison against one member of that enum will not, because the expression stays valid and its
> meaning quietly changes.

**The fix is to name what the guard MEANS rather than what it excludes** — `if (regime ==
kDefault) return;`, *"the default regime stops here"* — which stays true whatever is added. The
general form:

> **PHRASE A GUARD BY WHAT IT ADMITS, NOT BY WHAT IT REJECTS. THE REJECTED SET GROWS WITHOUT
> TOUCHING THE CODE.**

**And the tell that it had happened was a NUMBER, not an error**: 305 kill points where thousands
were expected. The census — which lists Env calls by kind — is what made it diagnosable in one look,
because a compaction sweep with no `kEnvDeleteFile` entries is not a sweep of a compaction.

**`-Wmissing-field-initializers` deserves its line beside it.** Adding a member to `Driver` broke two
positional aggregate initialisers, and the compiler said so. **That is the compiler doing the job a
convention would otherwise have had to** — the call sites are designated-initialised now, so the next
member cannot silently land in the wrong slot.

---

### GF-25 — a gate on the mechanism and a test on the answer are two instruments, not one

**Raised by** B3.6's file-lifetime gate, and it is `GF-22` one level down.

`GF-22`: two defects whose symptoms cancel are invisible to every test that asserts an **answer**.
This is the same observation without needing two defects:

> **WHEN THE ANSWER IS RIGHT FOR AN ACCIDENTAL REASON, ONLY AN ASSERTION ABOUT THE MECHANISM CAN TELL
> YOU.**

**The instance.** A snapshot reading through a compacted-away table returns the correct value whether
or not the file still exists — `table.h` holds the image resident, so the bytes are there either way.
A test that read through the snapshot would have passed against a build that deleted the file
immediately, **and it did**: three of B3.6's four tests passed while the reference count was wrong.

**So the pair is:**

| instrument | asserts | catches |
|---|---|---|
| `FileLifetime.AnInputFileOutlivesTheCompactionWhileASnapshotHoldsIt` | **the file is on disk** | the mechanism failing while residency masks it |
| `...ASnapshotSurvivesTwoCompactionsAndReadsThroughThem` | **the read is right** | the mechanism working and the read still wrong |

**Neither is sufficient and neither is redundant.** Drop the first and the reference count can be
deleted entirely with every test green. Drop the second and the file can be kept alive while the
version it holds is wrong.

**It is the same shape as `covers-correctness:` versus `covered-by:` in `FLOORS.txt`** (`GF-12`), and
as the two instruments B3-D7a requires of every loop: **the danger is never that one instrument
fails, it is that one instrument passing feels like coverage it does not provide.**

---

### GF-22 — two defects whose symptoms cancel are invisible to every test that asserts an answer

**Raised by** `BUG-004` and `BUG-005`, B3.5e. Filed here rather than under either bug, because the
class is the **pair**, not either member.

**BUG-004** dropped the point versions a range tombstone hid. **BUG-005** failed to write the
tombstone into the output files. Each alone loses data. **Together, every read returns the right
answer** — the key is absent, which is what the caller asked for, arrived at by two errors that
annihilate.

**THE ASYMMETRIC EVIDENCE IS THE PROOF, AND IT IS WORTH STATING AS A MEASUREMENT:**

| | effect on the suite |
|---|---|
| fix `BUG-004` alone | **four passing tests turn red** |
| fix `BUG-005` alone | **nothing observable changes** |
| both present | **everything green** |
| both fixed | everything green |

> **A TEST SUITE CANNOT SEE THIS CLASS AT ALL.** Not a weak suite — *any* suite, however thorough,
> whose assertions are about **answers**. The answers are correct. There is no input on which the
> engine returns the wrong thing.

**What can see it is a question about a MECHANISM rather than an ANSWER.** A test asks *is the answer
right?* A mutant asks:

> **IS THIS LINE LOAD-BEARING?**

`BM97` blinded the L1 tombstone lookup, and **nothing failed** — because nothing reached it. That
survival was **true information about the engine**, not a gap in the catalogue. The distinction
matters: a survival is usually read as *the suite is too weak here*, and this one meant *this code is
unreachable, and the reason it is unreachable is a second bug.*

**How to find the pair once one member is suspected.** The tell is the asymmetry above: **fix one,
and if a previously-green test goes red rather than a red one going green, the other member is
there.** A single defect's fix does not turn passing tests red.

**And it is an argument for mutants having a place beside tests rather than being a coverage metric.**

**`BM104` ADDS A RULE ABOUT THE PATCHES THEMSELVES, learned the same day.** Blinding clause 1's
tombstone test by *deleting* the covering call left its helper unused, `-Werror` failed the build,
and the **control lane** was killed:

> **A MUTANT MUST REMOVE EXACTLY ONE BELIEF. A MUTATION THAT CHANGES THE BUILD IS NOT A MUTATION** —
> a patch that fails to compile blinds nothing, and the lane correctly refuses to attribute anything
> to it.

The fix is to keep the call and discard its answer, so the only thing removed is the *acting on* it.
The lane already had the machinery to catch this — the direction control is exactly the assertion
that the patch alone does not break the build — which is why it reported `BROKEN` rather than a
survival.
Coverage would have reported both lines executed. They were — with their effects cancelling.

---

### GF-21 — replacing a mechanism under a threshold: assert the replacement at the SAME threshold

**Raised by** `AnOverCapExpansionIsRefusedAndAppliesNothing` → `AClearEverythingIsOneSmallRecordWhateverTheDatabaseHolds`, B3.5c.

The old test filled 3000 keys, issued a clear-everything, and asserted the resulting **expansion** was
refused for exceeding the record cap — **at a deliberately lowered cap** (`max_record_bytes = 20000`),
because a tripwire nobody has watched fire is decoration.

B3.5 retired the expansion, so the test's subject no longer exists. **The question is what replaces
it**, and the weak answers are available and tempting: delete it; or assert the clear-everything now
succeeds, at the default cap, where it would succeed for reasons having nothing to do with the change.

> **KEEP THE THRESHOLD. CHANGE THE ASSERTION.** The replacement runs the **same workload** at the
> **same lowered cap under which the old mechanism was refused**, and asserts the new one fits. Then
> passing is a statement **about the change** and not about a cap that got roomier.

**Why it matters that the number is the old one.** At the default cap the new test would pass on a
build where the expansion was still happening — 3000 point deletes fit under 4 MiB. The lowered cap
is what makes the two mechanisms **distinguishable by the same measurement**, which is §22.6c's
discriminator rule applied to a replacement rather than to a parse.

> **THE TEMPLATE, WHENEVER A MECHANISM IS REPLACED UNDER A THRESHOLD: run the old workload at the old
> threshold, and assert the new outcome.** A replacement measured against a different bar is a
> replacement nobody has compared.

**And the assertion is a bound, not an equality.** `EXPECT_LT(grew, 200)` — the record's exact size is
a fact about the encoding that will change; that it is *nothing like 3000 point deletes* is the claim.
A floor with margin, for `FLOORS.txt`'s stated reason: an exact assertion fails on any benign change
and a lane that cries wolf is a lane people delete.

---

### GF-20 — correctness resting on an argument whose premise moves is a scheduled defect

**Raised by** §8.1's expansion, retired at B3.5c.

B2 recorded a `DeleteRange`'s **expansion** in the WAL rather than the range itself, and the reason
was exact:

> *"If the WAL recorded the raw DeleteRange, recovery would have to expand it again — **against a
> state recovery is still in the middle of rebuilding**. The expansion is a function of the state at
> the time it runs, so replay-time expansion is correct only if that state provably equals the state
> at original Apply time. It probably does today, for a reason that depends on the WAL's start point
> coinciding exactly with the flush boundary — **a property B2 is about to start changing.**"*

**THAT REASONING WAS CORRECT AND ITS CONCLUSION EXPIRED.** The expansion was never wrong. It was
correct **under a premise**, and B3.5 dissolved the premise: a range tombstone means the same thing
wherever it is replayed — it hides every version below its own sequence, and nothing about the
surrounding state enters into it. Recovery **inserts** it and computes nothing.

> **CORRECTNESS RESTING ON AN ARGUMENT WHOSE PREMISE MOVES IS A SCHEDULED DEFECT. THE FIX IS TO
> REMOVE THE PREMISE, NOT TO DEFEND IT BETTER.**

The tell is already in B2's own words — *"a property B2 is about to start changing"*. A comment that
names the thing that will invalidate it has done the hard part; what it has not done is fix it.

**Defending it better is the tempting alternative and it is a treadmill.** The available moves were:
prove the flush boundary coincidence holds, or assert it, or narrow recovery so the coincidence is
forced. Each buys correctness *for now*, adds a constraint to every later change, and leaves the
argument standing. **Removing the premise ends it.**

**It is `GF-18`'s question answered in the affirmative.** *What did this shim let us avoid deciding?*
The expansion let B2 avoid deciding what a range deletion means as a durable, replayable fact — and
answering that question is what made the premise unnecessary. `GF-18` says a retired shim is the
moment to re-check the contracts it stood between; this says **the moving premise is the marker for
which shim to retire first.**

**Two instances now, both at B3.5.** The other is `DBImpl`'s own note on `DeleteRange`'s expansion —
*"THAT IS CORRECTNESS BY ARGUMENT, AND THE ARGUMENT HAS A MOVING PREMISE"* — and the snapshot
registry (`B3.4`), which replaced *"a snapshot pins its stores, and residency makes that safe"* with a
registry, on exactly this reasoning before the form had a name.

---

### GF-19 — a name that describes one end of a structure will be read as describing the structure

**Raised by** `BM90-unbounded-covers-everything`, B3.5b.

**"Unbounded" is one word and a range has two ends.** `RangeTombstone::end_unbounded` says the *end*
has no bound; the misreading takes it to mean the *range* has none —

```cpp
if (end_unbounded) return true;   // BM90
```

— and that deletes **everything below the start**, which is data no `DeleteRange` ever named. Not a
typo. A one-line simplification that reads correctly aloud.

> **A NAME THAT DESCRIBES ONE END OF A STRUCTURE WILL BE READ AS DESCRIBING THE STRUCTURE.**

**It is `GF-7`'s family, in an identifier rather than in a comment.** `GF-7` is a *comment* asserting
a load-bearing property for a line that does not carry it; this is **a word attached to the wrong
scope** — true of the field it names, false of the object the reader applies it to. Both are invisible
to review for the same reason: **the words are true somewhere.**

**THE TELL, AND IT GENERALISES PAST NAMES.** The wrong reading passes every test that checks a key
*inside* the range. Both readings agree there, and agreement inside the bounds is exactly where a
careless test looks.

> **HALF A RANGE TEST IS NOT A RANGE TEST. ANY TEST OF A BOUNDED STRUCTURE ASSERTS OUTSIDE BOTH
> BOUNDS, NOT INSIDE.**

Which is why `AnUnboundedEndCoversEverythingAboveItsStartAndNothingBelow` asserts `"a"` and `"l"`
**below** the start, `"m"` **at** it — the inclusive edge — and `"zzzzzz"` far above. The inside is
the one place that proves nothing.

**The rule is not new to this repo and that is the argument for naming it.** B2's half-open bound was
asserted at both ends for the same reason (*"a fixture checking only the inside passes with either
convention"*), and `RangeModel.TheEndBoundIsExclusiveAndTheStartBoundIsNot` exists because of it.
`GF-19` is that habit stated once instead of rediscovered per boundary.

---

### GF-18 — a shim that makes a case unnecessary makes the gap it hides invisible

**Raised by** `B3-Q4`: the frozen `Engine` interface required a range deletion the frozen
range-tombstone format could not express, and **nothing noticed for a phase and a half.**

`[A3]` put `DeleteRange` in the interface for the clear-everything case. B2 implemented it as **one
point delete per live key**, which resolved `Bound::Unbounded()` against the *live set* before
anything was written — so **no format ever had to represent `[start, ∞)`**. When `[A3]` required the
expansion retired at B3, the gap it had been standing over became load-bearing in a single step.

> **AN EXPANSION OR A SHIM THAT MAKES A CASE UNNECESSARY ALSO MAKES THE GAP IT HIDES INVISIBLE. SO
> RETIRING A SHIM IS THE MOMENT TO RE-CHECK EVERY CONTRACT THE SHIM WAS STANDING BETWEEN.**

**Why the verification did not catch it, and this is the transferable part.** §6.1 specified the
format from the **block's** point of view and induced every refusal against hand-built bytes. That
exercise is thorough and it **never touches `Bound`, because the classifier never sees one.** Two
frozen artifacts, each internally consistent, each induced against its own rules — and **never
checked against each other.**

**It is `GF-15` between two frozen artifacts rather than inside one**, with one difference worth
stating: `GF-15`'s instance had a rule *granting permission* another contract did not grant. Here
**neither contract was wrong.** They were **unjoined** — and an unjoined pair produces no
contradiction to find until something asks both at once.

**What makes it findable.** The shim is the signal. A shim exists because some case was awkward; the
awkwardness is where two contracts meet; and the shim is what keeps them from having to agree. So the
question at retirement time is: *what did this let us avoid deciding?*

**Recorded as a second sweep condition on `CF-4`** — not only the frozen interface as a whole, but
**every place B2 or B3 removed an expansion.**

**And `CF-4` paid before it came due.** The sweep it schedules for B4 produced its first instance at
B3.5, where it cost a **design decision** instead of a differential failure against `engine/model`
with a corpus of tables already written to the wrong format. That is an argument for doing the sweep
rather than for deferring it.

---

### GF-17 — a reserved field sized by guess postpones the version bump by one; it does not avoid it

**Raised by** the SSTable footer's `reserved:[8]u8`, spent at B3.5b.

B2 left eight bytes in the footer with an explicit rationale: *"eight bytes now are free, a format
version bump at B3 is not."* B3 needed to name a range-tombstone block. **A `BlockHandle` is twelve
bytes** — `offset:u64` plus `size:u32` — so the natural shape did not fit the reserve at all.

**It fit only because the size turned out to be derivable.** The range block is written **last**,
immediately before the footer, so `range_size = file_size - kFooterBytes - range_offset`; only the
offset is stored, and eight bytes hold it. Had the block needed to sit anywhere else — or had the
extension been a second handle, a checksum, or anything with an independent length — **the reserve
would have bought nothing and B2's deferred version bump would have come due anyway.**

> **A RESERVED FIELD SIZED BY GUESS IS A BET THAT THE NEXT EXTENSION FITS. THIS ONE PAID OFF ONCE, ON
> A TECHNICALITY, AND THE NEXT EXTENSION PAYS FULL PRICE.**

**And there is a second cost B2 did not price.** The reserve had *two* properties, asserted together
in one test, and **only one of them can survive the reserve being spent**:

| property | what it was for | after B3.5b |
|---|---|---|
| **written zero** | an old file is recognisable | **still true**, and load-bearing for a new reason: a B2-era table decodes as `range_offset == 0`, meaning "no range block" |
| **not read** | a file from a *future* build still validates here | **gone, necessarily** — the reader now reads those bytes, so a file that put something else there is REFUSED |

> **SPENDING A RESERVE IS EXACTLY THE ACT THAT ENDS THE FORWARD COMPATIBILITY IT WAS ALSO
> PROVIDING.** A reserve is only forward-compatible while it is *readable and ignored*.

**The honest conclusion, stated so nobody repeats the reasoning at B4:** the reserve **did not avoid
the version bump — it postponed it by one**, and it spent the format's forward compatibility to do
so. Reserving sixteen bytes at B4 "because this worked" would be repeating a bet that happened to
land, not applying a lesson.

**What to do instead, when it next comes up.** Decide whether the format needs *extensibility* or
*version negotiation*. A reserve gives neither reliably; a length-prefixed footer with a declared
field count gives both, at the cost of the fixed-width property the footer was built around —
*"the one thing a classifier can read without trusting anything else in the file."* That is a real
trade and it should be made deliberately rather than inherited from a byte count somebody guessed.

**`BM33` survives the change and keeps its job**, re-aimed: it now blinds the range offset rather
than the reserve's zeroing, and the test it dies to was rewritten to state the new pair of
properties instead of being loosened to accommodate them.

---

### GF-16 — a mutant that survives because its precondition is unreachable is a claim about a workload

**Raised by three survivals in one step**, B3.4, and it is a sharper statement of the survival tally's
meaning #1 rather than a new meaning.

| mutant | the situation it breaks | why the suite never created it |
|---|---|---|
| `BM76` tombstone dropped over a snapshot | a tombstone the snapshot floor must keep | the fixture put the tombstone at the **top sequence**, where the watermark pin keeps it for an unrelated reason — so the test watched the pin and called it the drop rule |
| `BM79` roller rolls inside a user key | a key whose versions span a file roll | with **no live snapshot** the drop rule leaves one version per key, so no key is ever large enough to span one |
| `BM82` `Sync` no longer claims the guard | the guarded path being entered twice | the tests constructed the guard **directly**, so the path was never entered at all |

> **A MUTANT THAT SURVIVES BECAUSE ITS PRECONDITION IS UNREACHABLE IS NOT A WEAK MUTANT. IT IS A
> CLAIM ABOUT A WORKLOAD THE SUITE NEVER RAN — SO THE DISPOSITION IS TO REACH THE WORKLOAD, NOT TO
> RELABEL THE MUTANT.**

**All three were reached, and none was relabelled.** `BM76` got a fixture where the tombstone is not
the highest sequence, judged by `AdjudicateDrops` rather than by a count that is unremarkable either
way. `BM79` got **forty held snapshots**, so that a key genuinely has many surviving versions — a
workload this engine had never run, and the one `B3.6` is about. `BM82` got a **re-entrant `Sync`**
through the promotion hook.

**Why relabelling is the tempting wrong answer.** Every one of the three had a defensible-sounding
label available — *"covered by the pin"*, *"unreachable under the default policy"*, *"covered by the
guard's own tests"* — and each would have been **true and useless**: it names a reason the class is
not detected instead of an assertion that detects it. `GF-7`'s rule in the label file says a
`covered-by:` is determined by induction or not written, and a label invented to explain a survival
is exactly the inferred kind.

**What it cost, and why that is the argument.** Reaching the third workload found nothing wrong with
the engine — but reaching the first two required a fixture and a snapshot workload the suite did not
have, and **`B3.6`'s whole subject is the workload `BM79` forced into existence.** A relabelled
`BM79` would have deferred that discovery to the step that assumed it already worked.

---

**`BM97` IS THE STRONGEST DEMONSTRATION THIS CATALOGUE HAS, AND IT IS THE ONE TO CITE.** Its history
is three separate chances to close the file with a defensible sentence:

1. **B3.5d — held out.** Compaction did not yet emit tombstones into L1, so its workload did not
   exist. It was kept **out of `mutants/`** with its absence recorded in the commit, rather than
   admitted with a label explaining why it could not fire.
2. **B3.5e — re-added, and it survived again.** The available label was
   *"covered by the compaction tests"* — **plausible, defensible, and false.**
3. **The second survival is what opened `BUG-004` and `BUG-005`** — two data-loss defects whose
   symptoms cancelled (`GF-22`), invisible to every test in the suite.

> **A PLAUSIBLE LABEL IS THE DANGEROUS ONE.** An implausible label gets questioned. This one would
> have been accepted by any reviewer, closed the obligation, and shipped both defects.

**Cite this whenever an opt-out is proposed on the strength of an ARGUMENT rather than a
MEASUREMENT** — an exemption, a `covered-by:` that was reasoned to instead of induced, a mutant
excused because its class "is obviously covered elsewhere". The argument here was correct in every
particular except the conclusion.

---

### GF-15 — a rule derived from one contract is not permission under the others

**Raised by the watermark pin, B3.4.** `B3-D1` says a compaction **may** drop an entry that no reader
can observe. That "may" is permission **with respect to reads**, and reading it as permission full
stop breaks a promise `B3-D1` never mentions:

| contract | what it is about | what compaction owes it |
|---|---|---|
| the drop claim | **the ANSWER a reader gets** at each observable sequence | preserve every answer |
| `DurableSeq` | **a PROMISE ABOUT A SEQUENCE**, monotone non-decreasing | preserve the engine's proof of it |

`Open` re-derives the durable floor as the maximum `largest_seq` over the live tables, and it must:
D7's forward binding forbids the manifest from recording a durable sequence, so **the tables' own
bytes are the only place that number can come from**. Drop the highest-sequenced entry — a tombstone
with nothing left to mask, exactly what the claim permits — and the maximum falls. Every answer is
preserved; `DurableSeq` goes backwards across a restart.

> **A COMPONENT OBEYS MORE THAN ONE CONTRACT. A RULE DERIVED FROM ONE OF THEM IS NOT PERMISSION UNDER
> THE OTHERS, AND THE RULE WILL NOT SAY SO — because the contract it came from does not know the
> others exist.**

**What makes it findable rather than lucky:** the question is *what else does this operation touch
that someone has already been promised something about?* Compaction touches the set of live tables,
and the durable floor is derived from that set. The link is one step and nobody walks it while
reading a claim that is locally airtight.

**`BM77` plants it, and the mutant IS the faithful implementation of `B3-D1`.** Not a typo, not a
weakened check — what a careful reader of the claim would write. That is precisely the blind spot the
suite exists for, and it is why the mutant's header says so.

**RULED THE PHASE'S MOST TRANSFERABLE FINDING, AND IT CARRIES AN OBLIGATION.** A cross-contract
interaction is invisible in **either contract's own statement** — that is what makes it general, and
what makes it undiscoverable by reading one document carefully. It generalises to **every place this
engine derives a fact from one rule while another rule depends on that fact**, and this engine does
that in more than one place: the manifest's numbers are re-derived from table bytes, the durable
floor from `largest_seq`, the recovery skip point from the same maximum, `bottom_most` from range
disjointness.

> **THE QUESTION IS ASKED ONCE ACROSS THE FROZEN INTERFACE AS A WHOLE, AT B4** — not per-decision,
> where it has already been asked and answered locally. `CARRY-FORWARD.md` CF-4 carries it.

**AND IT HAS A SECOND INSTANCE ALREADY, BETWEEN TWO FROZEN ARTIFACTS RATHER THAN INSIDE ONE.**
`B3-Q4`: the frozen `Engine` interface requires `DeleteRange(Unbounded, Unbounded)`; the
range-tombstone format frozen at B3.2 cannot express an unbounded end. **The difference from the
first instance is the useful one** — there, a rule granted permission another contract did not grant.
Here **neither contract was wrong; they were unjoined**, and an unjoined pair produces no
contradiction to find until something asks both at once. See `GF-18` for what made it findable.

---

### GF-14 — a complementarity claim is asserted in both directions or it is folklore

**Raised by** `IsTheMergeOfItsInputs` and `AdjudicateDrops`, B3.4.

Two instruments are said to be complementary: one sees order and values, the other sees drops in any
durable image. **Stating that is not asserting it.** The test that carries the claim asserts *both*
halves against **one state**:

```
RefusesAnOutputWhoseValuesAreShiftedByOne
    the merge adjudicator  REFUSES  the shifted output
    the drop adjudicator   PASSES   the same state
```

**The first half alone is not the claim.** One instrument refusing says nothing about whether the
other was needed; only the second half — *this state gets past the other one* — makes "we need both"
a statement with a failing case.

> **A COMPLEMENTARITY CLAIM IS ASSERTED IN BOTH DIRECTIONS OR IT IS FOLKLORE.**

**What it protects against is silent degradation.** The day someone widens the drop adjudicator to
look at values — a reasonable change, and an improvement in isolation — the pair stops being
complementary and *nothing notices*, because a one-directional test still passes. With the second
half, that change fails a test whose message says exactly what to reconsider. **The claim gets
revisited rather than repeated.**

**This is Track A's bidirectional-gap discipline applied to two CHECKERS rather than to a gap.** The
same move: do not assert only that the thing fires, assert also that it was needed — and here
"needed" means *the other instrument does not cover this*.

---

### Fixture defects: one shape, twice, and the class made unreachable

Two fixtures produced verdicts that looked like checker bugs. **Both times the checker was right.**

| where | what the fixture omitted | what the checker correctly reported |
|---|---|---|
| B3.0, the drop adjudicator | a **directory sync** after writing the table — so its NAME was never durable | the image did not contain the table, so every version was **dropped** |
| B3.4, the merge adjudicator | **the manifest** naming the table — so it was an orphan | a table nothing refers to holds nothing, so every version was **dropped** |

> **A FIXTURE THAT DOES NOT DESCRIBE WHAT ITS AUTHOR MEANT PRODUCES A CORRECT VERDICT ABOUT THE WRONG
> THING, AND IT PRESENTS AS A CHECKER BUG** — which is the expensive way to find out, because the
> debugging starts in the wrong component.

**What the two omissions share is the useful part.** Neither was a typo. Each left out something
**the engine's own invariants require**: a durable table has a durable name; a live table is named by
the manifest. A fixture assembling an image by hand must reproduce every one of those invariants from
memory, and *will not*.

**So the class is made unreachable rather than remembered.** `rig/image_fixture.h` builds images
**through the engine's own construction path** — write, sync, **sync the directory**, open, validate,
name in the manifest — and both tests now go through it. There is one place that knows the whole
sequence, and a fixture cannot forget half of it. It was cheap: one file, and it removed a duplicated
key encoder on the way.

---

### GF-13 — a bound derived from another instrument's measurement cannot be raised

**Raised by** B3.4's merge, and it is a stronger property than the condition that asked for it.

Every loop needs a progress quantity and every unbounded quantity needs a bound. **The usual bound is
a CHOSEN number** — a timeout, a retry count, a maximum iteration — and a chosen number has one
predictable life: it is hit under some workload nobody anticipated, and it is **raised**. Not because
anyone is careless, but because the alternative is refusing a correct run, and a limit that refuses
correct runs loses that argument every time.

**The merge's bound is not chosen. It is DERIVED:**

> **`inputs_consumed ≤ Σ entries(f)` over the compaction's input files, counted by `ValidateTable`
> before the merge starts.**

A correct compaction consumes each input entry **exactly once**, so it terminates *at* the bound.
**Hitting it exactly is correct; exceeding it is the only failure** — and exceeding it can only mean
a source was rewound or an entry counted twice, which are the two ways a merge loops forever.

> **A BOUND DERIVED FROM ANOTHER INSTRUMENT'S MEASUREMENT CANNOT BE RAISED WITHOUT CONTRADICTING THAT
> INSTRUMENT, SO THE PRESSURE THAT NORMALLY ERODES A LIMIT HAS NOWHERE TO GO.**

To raise this one you must claim a table holds more entries than the classifier says it holds — and
the classifier's count is itself asserted (`SstClassifier.AcceptsACanonicalTable`). There is no
number in the source to edit. **That is a difference in KIND, not in degree**: a chosen bound is a
judgement that can be revised, and a derived bound is a consequence that can only be revised by
falsifying something else.

**Where to look for the pattern.** Prefer a bound that is *already being measured for another
reason*. `kMaxRecordBytes` and `kWalBufferBytes` are chosen and carry their derivations in prose
precisely because nothing measures them; this one needed no prose, because `ValidateTable` was
already counting.

---

### GF-12 — a termination assertion is not a correctness assertion

**Raised by** B3.3's CF-3 mutants, and it is the danger *inside* a rule that is working.

CF-3 requires every loop to assert the movement it terminates on, over a quantity it does not derive
from the thing it might be wrong about. `ConcatIter` does that, and the assertions hold. **Two of its
three mutants pass every one of them:**

| mutant | what it breaks | what the progress assertion does |
|---|---|---|
| `BM68-concat-seek-wrong-half` | the binary search takes the **wrong half** | **holds throughout.** `hi - lo` shrinks whichever direction is taken, so the loop terminates cleanly with its interval invariant intact — **and lands on the wrong file** |
| `BM69-concat-next-skips-a-file` | `Next` advances **two** files | **holds throughout.** `file_` strictly increases, so the walk terminates — **having silently dropped a whole table's contents** |

> **A LOOP WITH A PROVEN PROGRESS QUANTITY CAN ADVANCE MONOTONICALLY INTO A WRONG ANSWER.**

**THE DANGER IS NOT THAT CF-3 FAILS. IT IS THAT CF-3 SUCCEEDING FEELS LIKE COVERAGE IT DOES NOT
PROVIDE.** A reader who sees `RIFT_CHECK(hi - lo < before)` beside a search, and knows the phase made
a point of loop assertions, has every reason to think the loop is checked. It is — for termination,
and for nothing else. In `BM67`'s terms: **a checked-looking loop that returns wrong answers.**

**What actually covers the traversal, named so nobody reads the wrong green as evidence:**

| instrument | what it covers |
|---|---|
| `ConcatIter.EverySeekTargetLandsWhereALinearScanWould` | **the seek sweep** — every probe in and around the run compared against what a linear scan returns. A search wrong for one input class is wrong invisibly, because every other input still works |
| `ConcatIter.WalksTheWholeRunInOrder` and `.WalksBackwardsToTheSameSequence` | **the traversal** — and the pair matters, because forward and backward cross the same file boundaries in opposite orders |

`FLOORS.txt` labels these **`covers: correctness`** rather than leaving them as bare `covered-by:`
entries, so the distinction survives being read by someone in a hurry.

**THE SAME SHAPE ONE LEVEL UP: THE DROP ADJUDICATOR.** It is correct about what it checks and
**silent about what a reader assumes it covers.** It works over **sets** of `(user_key, seq)` — so it
is blind to ordering entirely, and blind to values.

> **A merge that emitted every required entry, in REVERSE ORDER, with EVERY VALUE SHIFTED BY ONE
> POSITION, would satisfy all three of its directions.**

That example is stated concretely on purpose: it is specific enough that nobody can talk themselves
out of it, which an abstract "it does not check ordering" would not be. `CompactionOutput.IsTheMergeOfItsInputs`
is what closes it — the harness merges the inputs itself, filters by the drop claim, and asserts the
output is **exactly that sequence, in order, with matching values.**

**The two are COMPLEMENTARY, NOT REDUNDANT, and the distinction is written down because someone will
later notice they overlap and propose deleting one:**

| instrument | runs where | sees |
|---|---|---|
| `IsTheMergeOfItsInputs` | **only** where the harness knows both the input and the output files — a compaction in isolation | order, values, and drops |
| `AdjudicateDrops` | **any durable image**, including one produced by a crash MID-COMPACTION | drops only |

Delete the second and every crash schedule loses its drop verdict. Delete the first and a merge can
reverse its output undetected.

---

**THE STANDING REQUIREMENT, and it is not a B3 rule:**

> **EVERY LOOP THIS ENGINE ADDS GETS TWO INSTRUMENTS, ANSWERING DIFFERENT QUESTIONS: *does it stop*,
> and *does it stop in the right place*.**

CF-3 is the first. **It was never the second**, and the phase that treats it as both ships `BM68`.
`FLOORS.txt` keeps them apart mechanically — `covered-by:` against `covers-correctness:` — so the
distinction survives a hurried reading rather than depending on one.

---

### BM67 — the phase's exemplar of a defect NO GENERAL CHECKER FINDS

**One character.** `<` becomes `<=` in `RangeTombstone::Covers`, and the range stops being half-open.

Closed and half-open **agree on every key except one**: the range's end. A tombstone for `[b, d)`
that has become `[b, d]` deletes `d` — one key more than the caller asked for, **forever, and
silently**, because nothing anywhere reports a key that was deleted slightly too eagerly.

**GO THROUGH THE ORACLES THIS PROJECT HAS BUILT AND NONE OF THEM SEES IT:**

| checker | why it is blind to this |
|---|---|
| the **drop adjudicator** | asks *what may be dropped* against the harness's version model. It is about DROPS, not about **bound arithmetic** — the tombstone is legitimately present and its sequence is legitimately required |
| **recovery equivalence** | compares WAL-only recovery against WAL-plus-tables. **Both sides use the same `Covers`**, so both delete `d` and the two agree perfectly |
| the **kill-point sweep** | injects crashes and torn syncs. **Nothing crashes.** Every kill point recovers to a promised watermark, because the watermark is right and only the CONTENT is one key short |
| **`ValidateTable`** | judges bytes against a format. The bytes are perfectly legal — this is a defect in what they MEAN, not in what they are |
| **the differential rig at B4** | *would* catch it, against `engine/model` — which is a phase away, and by then the convention would have been established by the code rather than chosen |

**What catches it is `RangeTombstone.TheBoundsAreHalfOpen`** — a fixture that probes `a`, `b`, `c`,
`d`, `e` around a `[b, d)` tombstone and asserts each answer individually, written **before any code
could establish a convention by accident**.

> **THIS IS B3.2's ORDERING ARGUMENT STATED AS A CONSEQUENCE RATHER THAN A PRINCIPLE.** A classifier
> written after a writer inherits the writer's convention and asserts it back. There is no general
> checker for "the engine is consistently wrong about a boundary" — consistency is exactly what a
> differential or equivalence check confirms. **The only defence is to fix the convention in a
> fixture before there is an implementation to read it off.**

---

### An empty condition is evidence only when the question was asked mechanically

CF-3 carries a condition: *if a loop cannot state a progress quantity independent of what it might be
wrong about, that is a finding to report before it is a loop to write.* At B3.3 it **came back
empty** — every loop the step adds has an independent quantity.

**An empty result is worth exactly as much as the procedure that produced it.** What makes this one
evidence rather than a shrug is that the question was asked **before the code existed**, in a table
with one row per loop and an explicit independence column:

```
loop            might be wrong about        progress quantity     independent?
Next / Prev     which table holds the key   file_ (an index)      YES
Seek            CompareInternalKey          hi - lo               YES  <- see below
```

Had it been asked by reading the finished code and nodding, "nothing to report" would have meant
"nothing noticed". **The table is what makes it the first rather than the second**, and it also
produced the phase's forward flag: B3.4's merge both advances a cursor *and* drops entries, so it has
no single cursor as a progress quantity, and its honest answer may be a bounded work count.

**The general shape: a check that returns "nothing" is only as good as the enumeration behind it.**
It is the same argument `HARNESS-006`'s audit made — enumerate the population first, then check each
member — applied to a question rather than to a set of functions.

---

### GF-11 — a rule stated in one place invites transfer to an adjacent place where it is false

**Raised by** the range-tombstone bounds, B3.2.

`table_check.h` refuses **a key too short to carry a tag**, and it is right: point entries are
internal keys. A range tombstone's bounds are **user keys** — the tag is a separate field — so the
**empty user key is a valid bound**, meaning "from the beginning".

Both rules are correct. **The danger is not either rule; it is the transfer.** A reader who has
internalised the first arrives at the second and applies it, because the two structures sit in the
same format, are decoded by the same block decoder, and are described in adjacent sections.

> **A FORMAT DECISION THAT CONTRADICTS A NEARBY ONE IS DOCUMENTED AT THE CONTRADICTION, NOT ONLY AT
> ITS OWN SITE.**

So `range_tombstone.h` does not merely say *bounds are user keys*. It says:

> *"THE BOUNDS ARE USER KEYS AND NOT INTERNAL KEYS, so the empty user key is a valid bound and there
> is no minimum length. This is the OPPOSITE of the point entry rule — which refuses a key too short
> to carry a tag — and it is stated here because a reader who has internalised that rule will assume
> it carries over. The tag is a separate field precisely so the bounds do not have to."*

**The last clause is what stops the transfer**, and it is the part a shorter comment would drop: it
does not just deny the other rule, it says *why the design is different*, which is the only form a
reader can check. `RangeTombstone.TheEmptyUserKeyIsAValidBound` asserts it, so the sentence has a
failing case.

**The relationship to GF-7.** GF-7 is a claim attached to the wrong line. This is a claim attached to
the right line **that a reader will carry to the wrong one** — the same damage by a different route,
and the remedy is different too: GF-7's is to move the comment, this one's is to write the
contradiction down where it can be met.

---

### GF-10 — a set of assertions all pointed the same way has a blind spot the size of its agreement

**Raised by** B3.1's aliasing condition, and it is a **decision** that came out of it rather than an
audit finding — see `B3-D2c`.

The drop adjudicator had two directions, and they looked complementary:

```
kept      >= required     nothing a reader can reach was dropped
dropped   <= permitted    no tombstone was dropped over what it masked
```

**Both ask whether something is MISSING.** So neither could see a reader that reports a record the
bytes do not contain — which makes a real drop look survived, and produces a **false pass**. The
agreement between them was not corroboration; **it was the shape of the hole.**

> **A SET OF ASSERTIONS ALL POINTED THE SAME WAY HAS A BLIND SPOT THE SAME SIZE AS ITS AGREEMENT.**
>
> **Asking what a checker CANNOT SEE is a different question from asking whether it works** — and the
> second question is the one that gets asked, because it is the one a green lane answers.

The third direction, `survived ⊆ submitted`, is what closed it, and it is stated as its own decision
rather than as defensive clutter precisely because a future reader will otherwise remove it: it never
fires in normal operation, and *nothing else in the tree asks its question*.

**How to ask the harder question.** Enumerate what each assertion *rules out* and look for the
direction none of them names. Here: "missing" was ruled out twice and "present but never written" was
ruled out zero times. It is the same move `HARNESS-006`'s audit made across the evidentiary deciders
— enumerate the population, then check each member in both directions — applied to assertions rather
than to functions.

---

### GF-9 — a correctness claim written before its checker is a hypothesis, and building the checker is how it gets tested

**Twice observed, both in Track B, and both times the correction was in the CLAIM rather than in the
code.**

| phase | the claim | what building its checker found |
|---|---|---|
| B2.0 | *entries in a block are strictly ascending* | ascending **by `memcmp`** is wrong: internal keys sort user-key-ascending, tag-DESCENDING, and the fixtures that could show it did not exist because they used keys with no tag |
| B3.0 | *`keep(k)` is the newest version at each observable sequence* | it **over-requires**: a deletion's answer is `kNotFound`, which dropping the deletion preserves exactly, and the stricter form forbids the one drop that makes compaction terminate in space |

**What makes this different from the ordering rule it comes from.** *The observer lands before the
observed* is usually argued as protection against a checker written to agree with the thing it
checks. That is real, and it is not what happened either time. **Both times the CLAIM was wrong, and
writing the enforcement is what tested it** — because a claim in prose has no failing case, and a
checker has to be handed inputs.

**The consequence worth recording, and it is `HARNESS-006`'s shape.** A checker built to an
over-strict claim **refuses correct behaviour**, and it presents as *a bug in the component being
checked*. `HARNESS-006` cost its debugging to the wrong component because it was found late. Both of
these were found **before the component existed to be blamed** — B2's before any writer, B3's before
any compaction — which is the same ordering rule buying something other than what it is usually sold
for.

---

### GF-8 — when a rule distinguishes two kinds of dependency, find the SIGNATURE that separates them, not the sentence

**Raised by** B3-D2a, correcting the oracle-independence rule at `B3-Q1`.

The rule to be enforced was *an oracle may parse the engine's artifacts and may not consult its
beliefs*. That sentence is correct and it is **a discipline**, and this catalogue records five
disciplines that failed. What replaced it is two greps:

> **AN ARTIFACT HEADER DECLARES NOTHING TAKING AN `Env*` AND NOTHING TAKING A SNAPSHOT.**

`Env*` means *it went and looked*, which is an act with an opinion about what the current state is. A
snapshot parameter means *it decided what a caller should be allowed to see*, which is the engine's
visibility rule. A header with neither is bytes in, structure out.

**The general form.** When a rule separates two kinds of dependency — permitted and forbidden,
parsing and consulting, reading and asking — **the enforceable version is a property of the
DECLARATION, not a description of the intent.** Look for the signature that differs. If no signature
differs, the two kinds are not actually distinct and the rule is describing a preference.

**Corroboration, and it is the stronger half.** The rule was written to draw a boundary and it
**found unnecessary coupling instead**: `sst/table.h` fails both marks, and it turned out the drop
checker never needed it — `table_format.h` enumerates every entry in a table, and enumerating is the
whole job. `sst/manifest.h` failed the `Env*` mark and split, which was a correction rather than a
concession.

> **A RULE THAT FINDS UNNECESSARY COUPLING IS DOING MORE THAN ENFORCING A BOUNDARY.** The signature
> test asks "does this dependency have the shape of a judgement", and a dependency that cannot answer
> is usually one that did not need to exist.

**The counterpart obligation, and it is the one a future reader will get wrong.** See `HARNESS-014`:
permission to *read* an artifact is not permission to *derive the expectation* from it.

---

### GF-7 — a misplaced invariant: a comment asserting a load-bearing property for a line that does not carry it

**Raised by** `BM55-tables-oldest-first`, B2's last catalogue run. The phase's best finding, and it is
not the vacuous-green class and not one of the three survival meanings as previously stated.

> **A COMMENT THAT ASSERTS A LOAD-BEARING PROPERTY FOR A LINE WHERE IT IS NOT LOAD-BEARING IS WORSE
> THAN NO COMMENT.** It is where someone looks for the invariant, and it sends them somewhere nothing
> depends on it.

**The instance.** `Version::Build`'s table loop carried *"NEWEST FIRST. Order is not cosmetic: a
deletion in a newer table must hide a value in an older one, so the first source holding a user key
wins."* Every word of that is true — **of `VersionGet`'s point-read walk, a hundred lines below.**
Where it stood it was false: `MergedIter` orders by KEY, sequences are unique, so there are no ties
for source order to break and the order it is handed is irrelevant.

**How it was found, and it could not have been found by reading.** A mutant was aimed at that line
*because the comment said so*. It survived — correctly — and the survival is the only thing that
distinguished "this line carries the property" from "a comment near this line says it does". **A
misplaced invariant is invisible to review by construction: the reviewer's question is whether the
comment is true, and it is.**

**The family, and this is why it is a named shape rather than an incident.** It is the same failure as
Track A's `power-config a3` and the A6 banner — **a name describing something other than what it is
attached to** — with one difference that makes it more dangerous: those are *labels*, and this is a
**correctness claim**. A wrong label misdirects; a wrong correctness claim gets *relied on*. The next
person to touch `Version::Build` would have preserved an order they did not need and, finding the
same words there, would have had no reason to look for the walk that actually needs it.

**The fix, and where the induction has to land.** Re-point the comment at the line that carries the
property, and **re-point the mutant there too** — a general form whose remedy is not itself induced is
a general form nobody has tested. `BM55` now reverses `VersionGet`'s walk, where the first table
holding the user key wins and the walk stops.

**The standing question it adds.** When a mutant survives, before reaching for any of the three
meanings: *is the line this patch is aimed at actually the line that carries the property, or is a
comment answering that question for me?*

---

**THE SAME SHAPE IN THE LABEL FILE, and it is where it does the most damage.** `FLOORS.txt`'s
`covered-by:` is a claim that a named assertion catches a named class. CF-2 landed 47 of them, and
**every one was DETERMINED — the patch applied, the tree built, the failing assertion read — never
INFERRED from what the patch says it blinds.**

Inferring would have been faster and would have produced entries that are *plausible and wrong*: the
`blinds` line describes the defect, not the assertion that notices it, and the two coincide often
enough to make the guess feel safe. Three of the 47 have **no failing test at all**.

> **A WRONG `covered-by` IS WORSE THAN NONE, because it is consulted precisely when someone is about
> to delete something.** That is GF-7 arriving in the label file rather than in a comment: a claim
> attached to the wrong thing, in the one place a reader trusts before removing an assertion.

**The rule: a label that names an instrument is determined by induction, or it is not written.**

---

**BM82 IS THE SAME QUESTION, ASKED OF A GUARD RATHER THAN A COMMENT.** `SingleCaller` enforces
`Sync`'s single-caller precondition, and two tests constructed it **directly** — claim it twice, it
aborts; claim it sequentially, it does not. Both pass. `BM82` removes `Sync`'s *claim* on the guard,
leaves the guard itself intact, and **survived them both.**

> **A TEST CONSTRUCTING A MECHANISM DIRECTLY TESTS THE MECHANISM AND NOT ITS WIRING. THEY PROVE THE
> GUARD WORKS. NOTHING PROVES THE PATH USES IT.**

Two claims that read as one, and the enforcement rests entirely on the second.

**This is the planted-violation-versus-fixture distinction this project has held since A0**, arriving
in C++ **against a guard rather than against an analyzer**. There, the rule was that a determinism
check must be proven by planting a violation *in code the check actually scans*, never by feeding the
checker a hand-built fixture that exercises its parser. Here the "fixture" is a directly constructed
`SingleCaller`: it exercises the mechanism's own logic and says nothing about whether the production
path is wired to it. **Same distinction, different decade of the stack.**

**The induction is deterministic and that was the second decision.** The guard is claimed twice on
**one thread**, by re-entering `Sync` from the promotion hook — which fires inside `Sync`, when the
durable image changes. Racing two real `Sync`s would have induced it *probably*.

> **THIS CATALOGUE DOES NOT COUNT A GATE INDUCED PROBABLY.**

The hook fires **once**, deliberately: without that, a build with the claim removed would recurse
until the stack gave out, and **a death test cannot tell a guard firing from a crash** — the mutant
would have passed for the wrong reason, which is `GF-1`'s shape hiding inside the remedy.

---

### GF-6 — a rate is a ratio, and a floor on one needs a floor on the count beside it

**Raised by** B2.7's re-measurement of every harness-power floor. **Second instance** of a rate
moving for a reason unrelated to detection power.

A floor on a detection RATE cannot tell a loss of power from **a denominator that grew into territory
where the class was never detectable**. B2 added a manifest, so the sweep visits 300 kill points where
it visited 175 — and every added point is one at which `BM2`, `BM4` and `BM5` cannot be detected.
**Every rate in `FLOORS.txt` fell. Not one detection count did.** A lane that broke the build on that
would be reporting arithmetic as a regression; a maintainer who then lowered the rate floor would have
lowered it for the wrong reason and lost the bound for the right one.

**The rule.** A rate floor needs one of two things beside it:

- **a floor on the COUNT** — immune to the denominator, blind to per-point dilution. The rate is the
  reverse, which is why this is a *third* bound and not a replacement; or
- **a REGIME LABEL** that stops incomparable denominators from being compared at all.

**Track A learned the regime half at A6. This is the count half.** Both are now columns in
`FLOORS.txt`.

**Where it bites hardest, and B2 has an example.** `BM4-missing-dir-sync` in the default regime now
measures **290 of 290, first at kill point 1** — its count rose from 80 to 290 because the manifest
NAMES the WAL, so a lost directory entry is refused at every kill point rather than only where the
loss mattered. That is a real strengthening **and it costs the class its usefulness as a measure of
sweep power**: a class detected everywhere measures only that the lane ran. Its rate and ceiling are
kept because a collapse would still cross them, and are recorded as no longer discriminating so that
nobody reads 1000 per mille as a result.


