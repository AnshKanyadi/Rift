# DESIGN-B2: SSTables, the bloom filter, and the manifest

**Status:** **REVISION 2 — APPROVED 2026-08-25. SIGNED 2026-08-25. AMENDED 2026-08-26.** All nine
decisions ratified as reasoned; the three open questions ruled and folded in below.

> **AMENDMENT, 2026-08-26 — `BUG-003`, present since B2.5, found at B3.4, harmless until B3.4.**
> `DBImpl::Flush`'s early return on `imm_ != nullptr` reads as a serialiser and is not: `imm_` is
> assigned several steps after the first `AppendGroup`, so two concurrent `Sync` callers both pass
> it. It was undamaging while the manifest had **one** appender, which was true for the whole of B2.
> B3.4's compaction is the second appender. Fixed at B3.4 by enforcing `Sync`'s single-caller
> precondition (`SingleCaller`), not by adding a lock — see `BUGS.md` BUG-003 for why the wider fix
> was refused.
>
> **This amends what B2 VERIFIED, not whether B2 was signed.** A sign-off is a claim about what was
> checked; the defect was unreachable under B2's shape and no B2 lane could have found it. Second
> time a phase record has been amended by a later phase, and the mechanism was the same both times.
**Phase:** B2 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Depends on:** B1, signed 2026-08-25. **Blocks:** B3, B4, B5.
**Carries:** `CARRY-FORWARD.md` CF-1, which comes due in this phase.

**B2-D2 and §3 are the SSTable format surface to be frozen.**

---

## 0. What B1 hands over, and what it hands over *broken*

Three things arrive from B1 that shape every decision below, and the third is the one that matters.

**A proven record format and a classifier that judges it.** §5.3's framing, the CRC covering the
length, the fragment-chain state machine and §5.4's torn-tail rule are frozen, induced, and have a
reader that classifies illegal shapes from hand-built bytes. That reader was landed *before* the
writer on a ruling, and B2 has the same choice to make twice more.

**A rule about what the manifest may say.** B1-D7's forward binding: *the manifest may record which
files exist; it may never record a durable sequence the WAL cannot independently justify*, and
§7.2's `max+1` numbering **stops being safe the moment B2 deletes a flushed WAL**. B2 is where that
expires, which makes B2 the phase that has to keep the binding rather than inherit it.

**An accidental defence with an expiry date, and the date is this phase.** `CARRY-FORWARD.md` CF-1.
B1's recovery can apply BATCH records past the last `GROUP_END`; they land above the recovered
watermark where the snapshot hides them. **The flush ends that**: uncommitted records that were
merely unreadable become durable, visible and permanent in an SSTable. BM2's detection rate is
re-measured in this phase and expected to *rise*; a fall is a regression, not an expiry.

---

## 1. B2-D1 — the data block layout

**Candidates.** (a) LevelDB-shaped: shared-prefix compression with a restart array every *k* keys.
(b) **No prefix compression**: each entry carries its full key, with a restart array retained purely
as a binary-search index into the block. (c) Prefix compression with no restarts.

**Tradeoffs.** (c) is out: without restarts every lookup decodes the block from its start, and a
single corrupt entry makes every later key in the block silently *wrong rather than absent* — which
is the failure §5.4 spent a whole decision learning to distinguish.

The real axis between (a) and (b) is **space against decode independence**. Under (a) an entry's key
is a function of the entry before it, so a corrupted length inside a block can produce a
**structurally valid, wrong key** — the SSTable version of the failure that made B1 put the length
inside the CRC. Restarts bound the damage to one interval, which is why (a) is safe *with* them and
indefensible without.

Under (b) every entry is self-describing. A corrupt entry is a corrupt entry.

**Recommendation: (b), and Amendment A6 is why.** The simplest correct thing wins v1 and the faster
or smaller thing is a recorded upgrade path. Prefix compression is a **space** optimization with no
measurement asking for it, and B1-D6c is the standing warning about applying A6 unevenly: I applied
it to compaction policy and then violated it for the memtable's concurrency, and was overruled.
Blocks stay self-describing in v1.

**The threshold that reopens it**, so the upgrade path is a number rather than a mood: B5's
standalone numbers showing SSTable size or block-cache pressure is a bottleneck, with the space cost
of full keys attributed by measurement rather than inferred.

**Rejected:** (a) — buys space, costs decode independence, unmeasured. (c) — gives up the resync
anchor entirely.

**THE CONNECTION, RECORDED BECAUSE IT IS THE SAME DECISION ONE LAYER UP.** B1's CRC covers the
length; LevelDB's does not. Prefix compression makes a key a function of the entry before it;
self-describing entries do not. **Both decisions are about the same question: does corruption produce
something the reader REJECTS, or something it ACCEPTS?** A length outside the CRC yields a
wrong-sized payload whose failure offset is unknown; a shared prefix yields a wrong key that parses
perfectly. In both cases the cheap option does not lose detection — it converts a detected fault into
an accepted one, which is strictly worse than an outage and is the failure mode §5.4 rejected
candidate (b) for. Anyone reverting either one should be made to answer the same question.

---

## 2. B2-D2 — the index, and a key that does not exist

**Candidates.** (a) One index entry per data block: `(separator, offset, size)`, the index itself a
block located from a fixed-width footer. (b) Two-level index. (c) Index entries in the footer.

(b) is premature: B2 tables are bounded by the flush threshold and the index is one block.
(c) makes the footer variable-length, and a **fixed-width footer read from the end of the file** is
what lets a classifier start anywhere; giving that up costs more than it saves.

**Recommendation: (a).** With one condition that is a decision in its own right.

**The separator is the block's LAST KEY, exactly — not a shortened one.** LevelDB computes the
shortest string that separates block *N* from block *N+1*, which is smaller and which **puts a key in
the index that is not in the table**. B4 defines correct as byte-identical to `engine/model`, and a
synthesized key is a byte no model produces; it also means an index entry can never be validated
against the table's own contents, because there is nothing to validate it against. Exact last keys
make the index **checkable**: every index entry must equal the last key of the block it names, and
that is an assertion the classifier can make from the bytes alone.

**COST, STATED SO NOBODY LATER READS IT AS AN OVERSIGHT: the index is larger.** LevelDB's shortened
separator is usually a few bytes where ours is a whole key, so our index grows with key length rather
than with key *distinguishability*. On a keyspace of long, similar keys — which is what A5's MVCC
encoding produces, since every internal key shares a user-key prefix — that difference is real and it
is being paid deliberately. It is not an omission and it is not a TODO. Shortening returns as an
upgrade path at the same threshold as D1: a measured index-size problem at B5, not before.

---

## 3. The format surface to be frozen

Fixed-width little-endian, no varints, no timestamps anywhere (ruling 2), no floats on any path that
reaches these bytes (ruling 5).

```
file    = data blocks ‖ filter block ‖ index block ‖ footer

block   = entries ‖ restart_array ‖ restart_count:u32 ‖ block_trailer
entry   = key_len:u32 ‖ key ‖ value_len:u32 ‖ value        (key is an INTERNAL key)
block_trailer = crc32c:u32                                 covering entries ‖ restarts ‖ count

index entry = last_key_len:u32 ‖ last_key ‖ offset:u64 ‖ size:u32

footer (fixed 48 bytes, read from EOF)
    filter_offset:u64  filter_size:u32
    index_offset:u64   index_size:u32
    format_version:u32
    magic:[8]u8 = "RIFTSST\0"
    crc32c:u32                                             covering the 36 bytes above
```

**The CRC covers every length in the block, for §5.3.3's reason, and the same paragraph is required
on the helper.** B1 has that helper already; B2 reuses it rather than growing a second one.

**What the classifier must reject**, and these are the six-case table's successor: a bad block CRC; a
footer whose magic or CRC is wrong; an index entry whose offset+size runs past the filter offset;
restart offsets outside their block; entries not strictly ascending within a block; an index whose
entries are not strictly ascending; an index entry that does not equal the last key of the block it
names; a filter block whose length disagrees with its own header.

---

## 4. B2-D3 — the bloom filter, and the float that cannot exist

**Candidates.** (a) One filter block per table over all keys. (b) Per-block filters, LevelDB's
2 KiB-range shape. (c) None in B2 — **out**, the exit criteria require bloom filters.

**Recommendation: (a).** One filter, one block, one lookup. (b) buys locality for large tables and
B2's tables are bounded by the flush threshold; it returns with multi-level compaction at B3.

**The hash is `Fnv1a64`, already in the tree and already pinned by golden vectors.** Two 32-bit
halves drive double hashing: `probe_i = (h1 + i*h2) mod bits`. No PRNG (ruling 5), no per-DB salt —
§6.2's ruling on tower heights applies unchanged, including its accepted cost: the function is public
so a key set that defeats the filter can be constructed, that is a performance property and not a
safety one, and the declined fix is the same per-DB salt for the same reason.

**A5 FORBIDS THE OPTIMAL-`k` CALCULATION.** The textbook probe count is `ln 2 × bits_per_key`, and
that is a **float on a path that decides on-disk bytes**. So `kBitsPerKey` and `kProbes` are **pinned
integer constants** with the arithmetic done once, by hand, at the definition site: 10 bits/key gives
an optimal *k* of 6.93, and we pin **7**. No floating point appears anywhere in the filter.

**The two properties are not the same kind of claim, and must not be asserted the same way.**

| property | kind | how it is checked |
|---|---|---|
| **no false negatives** | safety | **exact**: every key in the table must probe present, asserted over the whole key set |
| false positive rate | performance | **a ceiling, measured** — §10.3's shape. Pinning an exact rate would assert noise and redden on any workload change |

---

## 5. B2-D4 — the manifest

**Candidates.** (a) LevelDB's: an append-only edit log plus a `CURRENT` file replaced by atomic
rename. (b) A single state file rewritten whole and atomically renamed. (c) **The WAL's physical
framing**, with manifest-specific logical record kinds, plus `CURRENT`.

**Tradeoffs.** All three need `rename` and a directory sync around it, so none of them avoids that
cost — and §3.3 already names that as the injector "that finds a missing directory sync around an
atomic rename", so B2 is where `Env::RenameFile` stops being declared-and-unused.

The axis is **how many frozen formats this engine has**. (a) and (b) each need their own encoding,
their own torn-tail rule, and their own classifier judging illegal shapes — Ansh's exit criteria
require exactly that for the SSTable format, and requiring it a *third* time for the manifest is
three formats to keep frozen and three classifiers to keep induced.

**Recommendation: (c).** The manifest is a WAL-framed log: same blocks, same fragment chain, same
CRC-covers-length, same §5.4 torn-tail rule, judged by the reader B1.7a already landed and induced.
`CURRENT` names the live manifest and is replaced by rename + directory sync, which is where the
ruled-for fault surface gets exercised.

This is §7.5's "one mechanism, two users, not two mechanisms that drift apart", applied to a format
instead of an injector.

**Rejected:** (a) — a second format and a second classifier for incrementality B2 does not yet need.
(b) — a third format, *and* a whole-file rewrite that is atomic only via the same rename, so it pays
(a)'s cost without (a)'s benefit.

### 5.1 D7's forward binding, made structural rather than intended

> The manifest may record which files exist; it may **never** record a durable sequence the WAL
> cannot independently justify.

Three mechanisms, because "intended" is what this document is supposed to stop:

1. **No manifest record has a watermark field.** The binding is not enforced by review; it is
   enforced by there being nothing to write it into. A closed record enum, `-Werror=switch`, and a
   scan rule that no manifest encoder references a durability watermark.

   > **AMENDED AT B2's CLOSE.** This is not what landed, and what landed is stronger. Reusing the
   > WAL's framing means reusing `GROUP_END`, which carries a sequence field, so a manifest record
   > structurally has one. What holds instead: **no manifest EDIT has one**, `ManifestState` has
   > nowhere to receive one, and **a non-zero one fails the open**, asserted both ways. Absence is a
   > property of the current schema; a rejecting assertion is a property of every future one. See
   > §13.3.
2. **Every sequence the manifest *does* record is verified against the file that justifies it.** An
   SSTable's entry carries the highest sequence it contains — needed so recovery can skip WAL records
   already flushed — and **recovery re-derives it from the table's own largest internal key and fails
   the open on disagreement.** The manifest is never the sole authority for any number.
3. **A tampered manifest cannot move the watermark.** A test edits the manifest's recorded sequences
   and asserts the recovered watermark is unchanged, because it comes from the WAL. A mutant makes
   recovery read the manifest's number instead, and must be killed.

---

## 6. B2-D5 — the flush, and the ordering that loses data if reversed

**The trigger** is memtable memory ≥ `kFlushBytes`, a named constant whose derivation lives at the
definition site (§8.4's rule, unchanged). The engine's exact accounting (B1-D6a) is what makes this
answerable.

**The ordering, and there is only one correct one:**

```
1. write the SSTable, Sync it
2. Directory::Sync              (its NAME becomes durable, §3.3)
3. append the manifest edit, Sync it
4. Directory::Sync              (the manifest's extent is durable)
5. only now: delete the WAL(s) fully covered, then Directory::Sync
```

Reversing 3 and 5 loses data outright. Reversing 1 and 3 names a file that may not exist. **Every
adjacent pair is a kill point the sweep will visit**, and the crash-consistency claim for B2 is that
every one of them recovers to a promised watermark.

**Candidates for what a kill between steps leaves.** (a) Strict ordering as above, with recovery
tolerating the intermediate states: an SSTable not named by the manifest is an **orphan** and is
ignored and deleted; a WAL still present but fully covered is replayed and its records skipped by
sequence. (b) A two-phase marker inside the manifest. (c) Delete WALs lazily at the next Open only.

**Recommendation: (a), with (c) folded in** — deletion is idempotent and doing it at Open as well
costs nothing and removes a class of leak.

**`max+1` numbering expires here, exactly as B1-D7 said it would.** The file-number counter moves
into the manifest, because once a flushed WAL is deleted the highest surviving number is no longer
the highest ever issued. §7.2 step 3's gapless assertion must be **replaced, not deleted**: the new
invariant is that every file number the manifest names exists, and every WAL present is either named
or above the manifest's counter. Deleting the gapless check without replacing it is how the
directory-sync kill point loses its teeth, and that is what BM4 exists to catch.

---

## 7. B2-D6 — the classifier lands before the writer, twice

Ruled once already at B1, and it applies unchanged: *the torn-tail rule and chain legality are the
freeze surface, so their gates are induced before the writer is trusted.* B2 has two formats to
freeze — the SSTable and the manifest's logical records — and the same ordering:

**The SSTable classifier lands first**, judging §3's illegal shapes from hand-built byte images, with
no writer in the tree. Then the writer's output is checked against rules already seen to reject every
illegal shape, rather than against a decoder written to agree with it.

The manifest needs no new classifier, which is D4(c)'s whole argument.

---

## 8. B2-D7 — `DeleteRange` over a merged view

Ruling 1 stands: iterate-and-point-delete through B2, with real range tombstones at B3. What changes
is **what it iterates**. At B1 the expansion read the memtable; at B2 it must read the merged view —
memtable plus every live SSTable — or a `DeleteRange` will silently miss keys that have been flushed.

Two consequences, and the second is a genuine risk:

1. §8.1's argument is unchanged: the expansion happens at `Apply`, the WAL records the expansion, and
   recovery replays point deletes. Reading SSTables at `Apply` **does not break "Apply makes no Env
   call"** only if the SSTables are already open and cached — which is a real constraint on the flush
   path, not a detail, and the Env-call counter asserts it.
2. **The record cap becomes reachable in normal use.** At B1 only a large memtable could produce an
   over-cap expansion; at B2 the live key count is the whole database. §8.6's scheduled end (B3's
   tombstones) is what retires this, and until then the cap's behaviour under a merged view needs the
   same both-directions adjudication B1 gave it.

---

## 9. B2-D8 — recovery equivalence, and the trap in asserting it

The exit criterion: *recovery from WAL plus SSTables proves identical state to recovery from WAL
alone at the same watermark.*

**The obvious way to assert it is wrong.** Running both paths and comparing them is agreement between
two paths, and §13.4b is exactly that: agreement is not either path being right, and two paths that
share an assumption agree most confidently where they are both wrong. The flush path and the
replay path share the memtable, the comparator and the internal key encoding.

**Recommendation: assert both, in this order.** Each path is compared against **the harness's own
reference state** at the watermark — the B1.9a oracle, unchanged, which asks the engine nothing — and
*then* the two are compared with each other. **Their agreement is a THIRD CHECK, not the check.**

**This is §13.4b arriving in C++ before it could cost anything, and it was inherited rather than
paid for.** Track A learned that two paths sharing an assumption agree most confidently exactly where
they are both wrong; here the two paths share the memtable, the comparator and the internal key
encoding, so they would agree for reasons unrelated to either being correct. Writing that down before
the code exists is the whole value of a shared constitution — the lesson arrives as a design
constraint rather than as a postmortem.

---

## 10. B2-D9 — the landing sequence

The two ordering invariants from §14 hold unchanged: **the observer lands before the observed**, and
**a gate lands only once its failure has been induced and observed**.

| step | lands | why here |
|---|---|---|
| **B2.0** | the SSTable format + its classifier, from fixture bytes only | B2-D6. No writer exists to be trusted yet |
| **B2.1** | the bloom filter, no-false-negative asserted exactly, FP rate measured | it is a pure function; it needs no table to be checked |
| **B2.2** | the SSTable writer, byte digest pinned | now checkable against a classifier already induced |
| **B2.3** | the manifest, `CURRENT`, rename + directory-sync faults | first use of `Env::RenameFile`; §3.3's rename row stops being hypothetical |
| **B2.4** | the flush path, the ordering of §6, WAL retirement, numbering into the manifest | the step that discharges CF-1 |
| **B2.5** | merged reads; `DeleteRange` over the merged view | needs both stores |
| **B2.6** | recovery equivalence; the sweep extended over the flush path | needs everything |
| **B2.7** | floors re-measured under B2's shape; **CF-1 closed with its number** | power is measured last, once the shape stops moving |

**Every new evidentiary decider is registered in `DECIDERS.txt` at the step that lands it**, with both
directions asserted, per the category closed at B1. B2 is expected to add at least one: whichever
function decides whether an orphan SSTable makes a run void or a violation.

---

## 11. The three questions, ruled

**B2-Q1 — the flush REPLACES §7.2's gapless check. It does not retire it.**

Gaplessness was a property of *the WAL being the only durable record*. Once the flush exists the same
property has to hold **across the pair**, and it is stated in that form:

> Let `W` be the recovered watermark and `S` the highest sequence the SSTables hold. Recovery
> contributes `[1, S]` from the tables and `(S, W]` from the surviving WALs. **Those intervals must
> partition `[1, W]` exactly: nothing covered twice, nothing missing.**

Three obligations, and the second is the one a weaker version would drop:

1. **Asserted directly, not inferred from recovery succeeding.** Recovery computes the interval each
   source contributes and checks the partition. A recovery that happens to produce plausible state is
   not evidence that its sources tile the sequence space — that is exactly the inference the old check
   was there to avoid.
2. **Induced BOTH WAYS: a gap and an overlap.** A gap is a WAL deleted before the table that covers
   it was durable. An overlap is WAL records replayed that a table already holds. They are different
   defects with different causes and the same symptom of "recovery still worked".
3. **A replaced check has to be shown to cover what the old one covered.** BM4 stays pointed at the
   directory-sync failure it was written for, and must still be killed after the replacement — a
   retired check is a check nobody is watching.

**B2-Q2 — CF-1's predicted rise is a GATE, and the direction matters.**

The campaign **fails** if BM2's rate does not rise when the flush lands. The prediction is the content
of GF-5's claim: an accidental defence expires on a date, and this is the date. An unmet prediction
means either the accident was not what we thought it was, or the flush did not do what we thought it
did. **Both are findings**, and neither is a reason to adjust a floor.

Reporting is specified, not left to judgement:

- The new number is reported **against 194 per mille and first detection at kill point 14**.
- If it rises, the report says **by how much**, and whether that is consistent with the accidental
  defence having been **the whole** of the suppression or **only part** of it.
- **A rise to something well short of what an unsuppressed class should measure is its own finding**
  — it means something else is still suppressing detection, and the campaign has found the next thing
  before anyone went looking.

**B2-Q3 — bring forward only what B2's own gates cannot be induced without, each named with the gate
that requires it.**

The rule is the one this project has used since A0: *a gate is not landed until its failure has been
induced.* So anything B2 needs to induce **its own** gates is B2 work regardless of which design
document scheduled it, and anything beyond that waits for B4, where the rig is designed as a whole
rather than accreted.

The list, with its justifications, is §11.1. Anything not on it is B4's.

### 11.1 What B2 brings forward, and why each piece

| piece | the B2 gate that cannot be induced without it | verdict |
|---|---|---|
| the kill-point sweep's workload **extended to contain a flush** | §6's ordering has five steps and four adjacent pairs; every pair is a gate and none can be induced by a workload that never flushes | **B2 work.** The sweep exists; this is a longer workload, not a new rig |
| **nothing else** | — | — |

Everything else B2 needs is already standing and was built for B1:

- **Fault injection at `RenameFile`** — `TestEnv` injects at any `CallSite` already, and `RenameFile`
  has been a registered call site since B1.2a. B2.3 is its first *use*, not its first *support*.
- **Byte-level fixture images for the SSTable classifier** — B2.0's gates are driven from hand-built
  bytes exactly as B1.7a's were. No rig is involved in either.
- **Manifest tampering for §5.1's third mechanism** — `TestEnvironment::FromImage` already reopens a
  modified durable image; that is how B1.7b's watermark probe works.
- **Orphan tables and covered WALs** — these are states the sweep *produces* by killing between
  §6's steps, not states a rig has to construct.

**So the honest answer is that B2 borrows one thing from B4: a longer workload.** If that reads as
accretion the boundary is worth ruling again, but the alternative is a phase whose gates cannot be
induced, and that is the one thing this project does not accept.

## 12. Decision summary

| # | decision | recommendation |
|---|---|---|
| B2-D1 | data block layout | **(b)** self-describing entries, restarts for search only; prefix compression is an upgrade path with a measured threshold |
| B2-D2 | index shape | **(a)** one entry per block, **exact last key** as separator, fixed-width footer |
| B2-D3 | bloom filter | **(a)** one filter block; `Fnv1a64` double hashing; **pinned integer** bits/key and probes, because the optimal-*k* formula is a float A5 forbids; no false negatives asserted exactly, FP rate floored |
| B2-D4 | manifest | **(c)** WAL framing reused, manifest-specific logical kinds, `CURRENT` + rename; one frozen format instead of three |
| B2-D5 | flush ordering | **(a)+(c)** strict ordering, orphans ignored and deleted, lazy deletion at Open; `max+1` replaced by a manifest counter with the gapless check **replaced, not dropped** |
| B2-D6 | classifier before writer | ruled at B1, applied unchanged |
| B2-D7 | `DeleteRange` | merged-view expansion; cap becomes reachable in normal use; both-directions adjudication unchanged |
| B2-D8 | recovery equivalence | each path against the harness's reference **first**, the two against each other **second** |
| B2-D9 | landing sequence | eight steps, observer before observed, CF-1 discharged at B2.4 and closed at B2.7 |

| # | question | ruling |
|---|---|---|
| B2-Q1 | gapless check | **REPLACED, not retired.** The pair's intervals must partition `[1, W]`; asserted directly; induced as a **gap** and as an **overlap**; BM4 must still be killed |
| B2-Q2 | CF-1's predicted rise | **A GATE.** The campaign fails if BM2's rate does not rise. Report against 194 per mille / kill point 14, say by how much, and say whether it is consistent with the accident being the whole of the suppression or only part |
| B2-Q3 | how much of B4 arrives early | **One thing: a longer sweep workload**, because §6's four adjacent pairs cannot be induced by a workload that never flushes. Everything else B2 needs already exists (§11.1) |

---

## 13. Revision 3 — what implementation changed, and why

**A ruling that is not written into the artifact it governs has not landed.** This section records
every place B2's implementation diverged from B2's design, and every decision the design left to
implementation that turned out to have a wrong answer available. Nothing here is a change to a
ruling; each is a report against one.

### 13.1 The internal key order, and the defect B2-D6's ordering caught before a writer existed

§3's rejection list says *entries not strictly ascending within a block*. The classifier was drafted
comparing entries with `Slice::compare`, and that is **wrong for the format it was validating**:
internal keys sort *user key ascending, tag DESCENDING*, and the tag is stored little-endian, so a
bytewise comparison orders two versions of one user key **the wrong way round**. A table validated
that way looks perfectly well formed while a reader seeking a snapshot finds the *oldest* visible
version.

It was invisible because the fixtures used single-letter keys with no tag, for which the two orders
agree — the one case where the disagreement can be written down is two versions of one user key, and
that fixture did not exist yet. It was found by **building the fixtures the format actually
requires**, which is B2-D6's ordering paying off one step earlier than the plan expected: the point
of landing the classifier first is that its own construction interrogates the format.

`src/internal_key.{h,cc}` is the remedy — one encoder and one comparator, moved out of the
memtable's private helpers the moment the layout acquired a second holder. **BM34** (a caller
substituting memcmp) and **BM35** (the comparator itself inverted) are what keep it found.

### 13.2 B2-D4: the manifest names the live WALs, and it creates before it names

§6 states the replacement for B1's gapless check as *"every file number the manifest names exists,
and every WAL present is either named or above the manifest's counter."* Implementing it produced
two findings, both from the kill-point sweep, both engine defects that never shipped.

**The order matters more than the rule.** Naming a WAL *before* creating it is the order that
prevents the state you can name — a file the manifest has not heard of — and it creates the state
you cannot: **a durable name with no file**. That name persists into every later manifest, so
`named and absent` stops meaning `lost directory entry` from that moment on. The sweep refused it 41
times. **Creating first inverts it into a window that closes itself**: nothing is written to a WAL
before its name is durable, so a crash between creation and naming leaves an *empty* unnamed file.
Both halves of the rule then hold with no exception in either direction.

**An unnamed WAL is not necessarily empty.** The emptiness argument covers one of the two ways a WAL
becomes unnamed; the other is a flush, which drops a WAL's name in the same manifest group that adds
the table covering it. The sweep found that as 64 more violations. The rule is now stated as a
property of the state rather than of its provenance: **nothing in an unnamed WAL may be above what
the SSTables cover.** That is B2-Q1's *nothing covered twice* from the file side, and it is strictly
stronger than the check it replaced.

Both are recorded as **BUG-001** and **BUG-002** — the first two entries in BUGS.md's engine list.

### 13.3 D7's forward binding: the letter does not hold, and what holds is stronger

> **RULED, 2026-08-25.** Approved as implemented, and B1-D7's own text amended to say so rather than
> left with its letter unmet. *"What matters is that no manifest record carries a watermark the WAL
> cannot justify, and a structurally-present-but-always-zero field that fails the open if non-zero
> satisfies that more strongly than absence would, because absence is a property of the current
> schema and a rejecting assertion is a property of every future one."*

§5.1 mechanism 1 reads *"no manifest record has a watermark field ... enforced by there being
nothing to write it into."* B2-D4(c) reuses the WAL's framing, and that framing's `GROUP_END`
carries a `high_seq`. **So the sentence is not literally true of the manifest.** What is true, and
is what the mechanism was for:

- no manifest **edit** has such a field — `EditKind`'s payloads carry file numbers, sizes, key
  bounds and a per-table largest sequence, and nothing else;
- `ManifestState` has **nowhere to receive one**;
- a manifest `GROUP_END` whose sequence is non-zero **fails the open**. Writer writes zero, reader
  refuses anything else, both directions asserted, **BM47** induces it.

### 13.4 B2-Q1: the first implementation of the partition invariant was unsound

The invariant was implemented as *the replayed WALs' batch sequences form one contiguous run*, which
would have caught a lost file. **It is wrong**: a Write whose `Apply` is refused by a cap consumes
its sequence and writes no batch — deliberately, per the Write path's own note that the contract
requires monotonicity and not density — so a legitimate hole is indistinguishable from a lost file,
and the check would have refused the normal case in the name of the abnormal one. That is the
inversion §5.4 rejected candidate (a) for. **A B1 test caught it**, because its fixture writes
batches at sequences 4 and 9.

What sequences can answer is **order** (each file's first batch above the previous file's last) and
**the join** (the first replayed batch above what the tables cover). What they cannot answer is
*nothing missing*, which is a question about files and is answered with file identities — §13.2.

**THE ERROR HAS A NAME AND A PRECEDENT.** It is a conflation: two quantities that agree on every input
the system normally produces, treated as one. Track A made the same one in the read-index oracle,
which compared against `Index` where it meant something else, and **both were caught by an existing
test rather than by review** — here, a B1 fixture that happens to write batches at sequences 4 and 9
because it was probing something unrelated. A conflation survives review precisely because the two
quantities *are* equal in every example anyone looks at; only a test whose fixture had a different
reason to separate them can tell.

### 13.5 The sweep has two regimes, and that is the whole of B2-Q3's borrow list

`default` is the caps as shipped: no flush occurs, so it is the **direct successor** of the sweep
B1's floors were measured against. `flush` sets a low threshold and appends a workload tail that
crosses it, and is the only regime in which the flush path has kill points at all. They are run
separately and never aggregated (§8.4), and the regime is a column in `FLOORS.txt` rather than a
fact about whoever ran the lane.

The borrowed item, with the gate that required it: **a sweep workload long enough to contain a
flush**, required by every gate on B2-D5's ordering — steps 1 through 5 have no kill points
otherwise. Nothing else was brought forward.

### 13.6 Three harness facts the flush broke, and why they survived all of B1

`FactsFrom` computed *did this Sync make the in-flight group durable* as **the `promoted` flag of the
last `kWritableFileSync` in the ledger**. Every clause was true only by accident: the WAL was the
only file this engine ever synced; a torn injection at a `Flush` records its promotion on the
**Flush** entry; and the flush creates a second WAL inside the same `Sync` whose empty file promotes
nothing. Recorded as **HARNESS-012**, with **ORACLE-facts-last-sync** as the mutant.

The order in which they were found is the interesting part: **scoping the question to *this* `Sync`
is what exposed the other two.** Without scoping, one successful Sync answered for every group after
it, and the fact was accidentally correct exactly when it mattered.

### 13.7 Smaller divergences, recorded so none is a silent adaptation

| what | why |
|---|---|
| `src/sst/format.h` is named `table_format.h` | Every directory under `src/` is on the include path, so two headers sharing a basename resolve by SEARCH ORDER. This one worked only because every file that included it happened to live beside it. |
| `kFlushBytes` lives in `wal::Caps` | §6 puts its derivation at the definition site; joining Caps is what lets the sweep set it low, and makes a run at a non-default value a different regime by construction. |
| `Recover` no longer takes the directory lock | B2 must read the manifest under the same lock, and a function that both locks and recovers cannot be composed with one that must run inside the lock. `DB::Open` locks. |
| `Write` holds the lock across `wal_->Apply` | The flush replaces the WAL *and* the memtable; in B1's gap a Write could name the old WAL and apply to the new memtable — a lost write with no corruption. `Apply` makes no Env call, so the mutex-depth guard is unaffected. |
| `DurableSeq` is a maximum over the WAL and the flushed tables | Rolling the WAL gives the new one a durable sequence of zero, so reading the WAL alone would send the watermark backwards at every flush. |
| Reads capture shared pointers to their stores | `memtable.h`'s note said B2 must revisit arena lifetime "the moment a memtable can be retired". Refcounts, not lifetime by argument. |
| `kConcurrencyClaim` widened to two patterns | In the diff that widened the harness and not before it, per the ruling at the constant. The flush is the engine's first operation that replaces the WAL and the memtable under a concurrent writer. |
