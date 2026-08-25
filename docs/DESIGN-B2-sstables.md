# DESIGN-B2: SSTables, the bloom filter, and the manifest

**Status:** **PROPOSED, awaiting decision.** Nine decisions, `B2-D1`..`B2-D9`, and three open
questions. **No C++ file is written**: CLAUDE.md's phase rule is that a phase starts with this
document and stops here.
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

**Cost, stated:** a longer index. Shortening returns as an upgrade path at the same threshold as D1.

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
*then* the two are compared with each other. The second comparison catches a shared-assumption bug
only if it disagrees, which is a bonus; the first is what makes either result mean anything.

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

## 11. Questions remaining

**B2-Q1 — does the flush retire §7.2's gapless check, or replace it?** §6 recommends *replace*, and
the replacement is stated there, but the choice is a ruling: a weaker invariant here is how BM4's
kill point loses its teeth, and I would rather have it decided than inherited.

**B2-Q2 — is CF-1's expected rise a gate or an observation?** If BM2's measured rate does not rise
when the flush lands, is that a campaign failure or a recorded surprise? I lean **failure**: the
accident's expiry is predicted, and a prediction that does not come true is information about the
model. But a gate on a *rise* is unusual and I will not self-ratify it.

**B2-Q3 — how much of B4's crash rig arrives early?** The flush ordering in §6 has five steps and
four adjacent pairs, and the natural way to check them is the kill-point sweep B1.9b already has.
Extending it is cheap; building B4's full rig is not. My recommendation is to extend the existing
sweep at B2.6 and leave the differential rig to B4, but the boundary is worth ruling now rather than
drifting across.

---

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
