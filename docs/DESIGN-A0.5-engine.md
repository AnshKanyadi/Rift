# DESIGN-A0.5: the Engine interface, as landed

**Status:** **RATIFIED** by Ansh, 2026-08-11, in full.
**Phase:** A0.5 (Track A). **Author:** Claude. **Decider:** Ansh.
**Decides nothing new.** The interface is DESIGN-A0 D7 and the model is D8, both approved. This
records what landed, the one bug class the work named, and the forward bindings that now depend on
it — because the interface is **frozen** and Track B builds to it.

---

## 1. What froze

`engine.Engine`, `engine.Batch`, `engine.Iterator`, `engine.Snapshot`, `engine.SeqNum`,
`engine.IterOptions`, `engine.ErrNotFound`. `engine/model` is the deterministic Go reference.

The durability contract is exactly a WAL's, which is the whole reason the interface has this shape:

- `Apply` makes writes visible immediately and never blocks on I/O.
- `DurableSeq` is a watermark that advances *behind* visibility.
- A crash reverts state to exactly `DurableSeq`.

Buffered writes are readable and losable. That gap is not an implementation detail to be minimized;
it is the window in which acknowledged-but-unsynced data exists, and a soak whose aggregate
lost-write count is zero is reported as **not having tested durability** regardless of how green it
is (DR-15).

---

## 2. Named bug class: recovery-to-intermediate-sequence

**The class.** Crash recovery must yield exactly the state at the durable watermark, **for any
watermark the sync schedule can produce** — not only for watermarks that happen to coincide with the
most recent write.

**How it nearly shipped.** The first implementation kept two versions, `visible` and `durable`.
That representation cannot express a watermark sitting at an intermediate sequence: asked to advance
to a sequence between the last durable one and the current visible one, it had no version to advance
*to*, and silently did nothing. The observable consequence is the dangerous direction — a crash
would have recovered **more** state than the engine had promised was durable, because the watermark
lagged the truth. An engine that recovers more than it promised passes every naive test and breaks
exactly one invariant: *committed is forever* has a converse, and *uncommitted is gone* is the half
that catches lost-write bugs.

**The fix.** Every version between the durable watermark and the visible one is retained. Versions
are immutable and copy-on-write, so retaining them costs one pointer each, and advancing the
watermark to any applied sequence is exact by construction rather than by care.

**How it was caught.** By its own oracle, before landing: the durability property test replays the
harness's operation log to compute the expected state at `DurableSeq` and compares. It stays out of
BUGS.md under the harness-bug rule — that file is for bugs the verification machinery caught in the
system under test — and this is its record.

**Forward binding, and why this matters beyond A0.5.** B4's differential rig compares the C++
engine's recovered state against `engine/model`'s **state-at-seq**. That comparison is only
well-defined because the model can produce the state at an arbitrary durable watermark. Had the
two-version representation shipped, the differential rig would have been comparing against a model
that quietly rounded the watermark up, and every disagreement would have been blamed on the C++
engine.

---

## 3. Other decisions worth recording

**`DeleteRange` is native** (Amendment A3). Half-open `[start, end)`, with the unbounded form
`DeleteRange(nil, nil)` as the clear half of snapshot application's clear-then-ingest — the case the
amendment overruled the original recommendation for. The C++ engine implements it internally as
iterate-and-point-delete through B2 so B4's differential tests exercise the semantics early; real
range tombstones are a B3 deliverable, landing before any I2 benchmark number is taken.

**Iterator `Key` and `Value` are valid only until the next positioning call**, and the model honours
that despite being able to return stable slices safely. Code written against the comfortable
contract would compile here and break at I1, which is the wrong place to find out.

**`Close` reports open iterators and snapshots as an error.** The model has nothing to leak; the C++
engine pins versions against compaction. The more forgiving of two engines is the one that teaches
the habit that fails in production.

**Batches copy their arguments.** An engine whose behaviour depends on whether a caller reused a
buffer reproduces on one machine and not another, which is the one thing this project cannot afford.

**`ApproximateDiskBytes` is exact in the model and approximate in the C++ engine**, which answers
from table metadata. Any test depending on the exact byte count is testing the model rather than the
system, and is stated here so nobody writes one by accident.

---

## 4. Exit criteria, met

1. **Crash recovers exactly the durable prefix.** 200 seeds × 40 batches, the watermark advancing
   behind visibility at an integer ratio, checked against the oracle after every step and again
   after the crash.
2. **The unsynced window is real and losable.** A write that `Apply` made visible, that a read
   returns, and that a crash takes.
3. **`OnDurable` fires once per advance**, reporting the watermark rather than the requested
   sequence, and not at all when the watermark does not move.
4. **The watermark cannot outrun visibility** — advancing past the last applied sequence panics.
5. **Semantics:** half-open `DeleteRange`, unbounded clear-then-ingest, in-batch ordering with
   last-write-wins, iterator bounds and seeks, snapshot isolation across writes, iterator stability
   across writes, leak reporting at `Close`, argument copying, range-scoped size.

The oracle judges from the harness's own operation log throughout, never by asking the engine what
it thinks it had — see DESIGN-A0's oracle-independence principle, which this work promoted.
