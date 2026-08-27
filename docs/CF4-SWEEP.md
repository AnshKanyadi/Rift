# CF-4 — the cross-contract sweep, discharged at B4.4

**Two conditions, asked once over the whole frozen surface rather than per decision — because
per-decision they have already been asked, and that is exactly the reading that misses them.**

**Why here:** B4 is the first point where the whole surface exists **and is being exercised against an
independent implementation**, so a derived fact two rules disagree about has somewhere to show up
rather than being a reading exercise.

---

## Condition 1 (`GF-15`) — of every fact one rule DERIVES, which other rule DEPENDS on it?

> **A RULE DERIVED FROM ONE CONTRACT IS NOT PERMISSION UNDER THE OTHERS, AND THE RULE WILL NOT SAY SO
> — because the contract it came from does not know the others exist.**

| derived fact | derived by | depended on by | do they agree? |
|---|---|---|---|
| a table's `largest_seq` | `ValidateTable`, from the table's own keys | the manifest (held to it at Open); **`Open`'s durable floor**; recovery's skip point | **YES, and it is `B3-D7b`'s instance** — the watermark pin exists precisely because the drop claim permitted destroying this. Already found, already fixed, already a class (`BM77`) |
| a table's `smallest`/`largest` **bounds** | `ValidateTable`, including tombstone extents | the manifest; **compaction's input selection**; `VerifyL1IsARun`; `L1FileFor` | **NO — this was `BUG-006`**, found by the differential at B4.2 and fixed by collapsing four sites onto one predicate |
| `largest_is_exclusive` | `ValidateTable`, from whether a tombstone's end took the bound | `VerifyL1IsARun` (touching is legal); `L1FileFor` (step past) | **YES.** Both read the flag; neither re-derives it |
| `unbounded_end` | `ValidateTable`, from the range block | `Version::NewestCovering` (the last-file look); the install assertion; `VerifyL1IsARun` | **YES**, and the position invariant is asserted at install and refused at Open — two edges, one fact |
| the range block's **size** | derived from the file length, not stored | the classifier; `Table::Open`'s parse | **YES**, and a wrong derivation fails the block's CRC rather than returning a wrong answer |
| the compaction **bound** (`Σ entries`) | `ValidateTable`'s entry counts | `RunCompaction`'s progress assertion | **YES — `GF-13`.** It cannot be raised without contradicting the classifier |
| `S`, the observable set | the snapshot registry + `seq_` | the drop claim (clauses 1 and 2); the tombstone verdict | **YES**, and `pin_seq ≤ max(S)` is asserted rather than argued |
| `bottom_most` | range disjointness over the inputs | clause 2 of the drop claim | **YES**, and it is computed rather than assumed even where true by construction |
| the durable **watermark** | `wal_->DurableSeq()` ∪ `flushed_through_` | the frozen `DurableSeq` contract; the differential's comparison | **YES**, and the differential now holds it to `[w, inFlight]` |
| the memtable's **memory usage** | arena + tombstone bytes | the flush threshold | **YES**, and the tombstones were added to it precisely because a memtable of nothing but range deletes would never flush |

**ONE DISAGREEMENT FOUND, AND IT WAS FOUND BY THE RIG BEFORE THIS TABLE WAS WRITTEN.** `BUG-006` is
condition 1's instance: `ValidateTable` derived a bound, four other rules depended on it, and the
writer computed it by a different comparison. **The sweep's value here is that it says the other nine
were checked** — an audit whose only finding is the one you already had is still an audit, and its
result is the nine rows that hold.

---

## Condition 2 (`GF-18`) — of each retired shim, what did it let us avoid deciding?

> **Listing the shims is not the sweep. Asking the question of each one is.**

### `DeleteRange`'s expansion (B2 → retired B3.5)

**What it let us avoid deciding:** *what a range deletion means as a durable, replayable fact.* One
point delete per live key needs no encoding, no replay rule and no notion of an unbounded end.

**Do the contracts agree now?** They did not — **this produced `B3-Q4`**, the frozen interface
requiring `DeleteRange(Unbounded, Unbounded)` against a format that could not express an unbounded
end. Ruled and fixed with a sentinel. **CF-4 paid before it was scheduled.**

### `Apply`'s expansion (B2 → retired B3.5)

**What it let us avoid deciding:** *whether replay-time expansion is correct against a state recovery
is still rebuilding.* §8.1 argued it was, on a premise that was about to move.

**Do the contracts agree now?** **Yes, and the premise is gone rather than defended** — `GF-20`. A
range tombstone means the same thing wherever it is replayed, so recovery inserts and computes
nothing. **The differential exercises exactly this**: 72 killed schedules replay `kDeleteRange`
records through recovery and compare against a model that expands natively.

### `table.h`'s residency-as-correctness (B2 → retired B3.6)

**What it let us avoid deciding:** *file lifetime.* While the whole image was resident, deleting a
file a reader still held was safe, so nothing had to decide when a file may go.

**Do the contracts agree now?** **Yes** — `CF-5` discharged by reference count, and residency is
demoted to a performance property with **no correctness claim resting on it.**

### B2's iterate-and-point-delete (`[A3]`, replaced at B3)

**What it let us avoid deciding:** *the semantics of a range delete against the model.* An expansion
is trivially equivalent to point deletes, so the two engines could not disagree about ranges.

**Do the contracts agree now?** **This is the one the differential is FOR**, and it is the strongest
evidence in the phase: the two engines implement `DeleteRange` by **entirely different mechanisms** —
natively in the model, by range tombstones in the C++ engine — and **96 runs across three regimes
agree**, including unbounded shapes and clear-everything.

### The `flush` regime's guard (B3.7a)

**What it let us avoid deciding:** nothing about a contract — it was a two-valued guard that became
wrong at three. Recorded here because `GF-28` came out of it and the sweep is where such things get
re-checked: **every remaining regime guard names what it admits.**

---

## What the sweep did NOT cover, stated rather than implied

- **Facts derived across the cgo boundary.** B5 adds them and they are not here.
- **Facts the Go side derives** — `engine/model`'s own internal derivations are Track A's surface, and
  this sweep is over the C++ engine and the frozen interface between them.
- **The differential's own derived facts.** `inFlight` is derived from the submission log; it is
  asserted in both directions but is not part of the engine's contract surface.
