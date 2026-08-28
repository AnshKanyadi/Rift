# DESIGN-B5: the cgo boundary, the poller, and the first honest table

**Status: APPROVED 2026-08-27** — the terms were given with B4's sign-off and are recorded here as
decisions rather than proposals. **B1-Q11 is RULED (§4).**
**Phase:** B5 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Depends on:** B4, signed 2026-08-27. **Blocks:** I1.
**Carries:** `CF-3` (standing).

---

## 0. What B5 is, and the one thing it is not

**Four deliverables**, and the phase text names them: an `extern "C"` batch API with **error codes and
no exceptions**, a Go wrapper implementing `engine.Engine`, **differential tests through the cgo
path**, and **standalone numbers for both engines in one honest table** with the boundary cost
measured explicitly.

**WHAT IT IS NOT: a claim about a poller we ship.** §4 rules the poller **harness-side**. Nothing in
B5's numbers is a statement about a poller in production, because there is no poller in production —
an embedder supplies its own.

---

## 1. B5-D1 — the C surface

**`extern "C"`, error codes, no C++ type crosses.** The phase text fixes this and it is not reopened.
What needs deciding is the *shape*, and the decisions are:

| decision | choice | why |
|---|---|---|
| handles | opaque `rift_db*`, `rift_iter*`, `rift_snapshot*` | a Go pointer must never be stored by C, and a C++ object must never be named by Go. An opaque handle is the only shape where neither is possible |
| errors | `int32` codes mapping 1:1 onto `Status::Code` | one enum, one meaning. A second numbering would be a second source of truth about one fact |
| **exceptions** | **none may cross, and it is enforced** | see §2 |
| strings out | caller-supplied buffer + required length | no allocation crosses, so no free crosses. A caller that under-sizes is told the length and asks again |
| batches | one call commits a whole `WriteBatch` | the phase text's whole point: per-call cgo overhead is real and the interface must amortise it |
| iterators | **blocks of N pairs per call** | the same reason. §3 |

---

## 2. B5-D2 — NO EXCEPTION CROSSES, AND THE ENFORCEMENT IS THE COMPILER

**An exception unwinding through a C frame into Go is undefined behaviour**, and the failure is not a
crash at the boundary — it is a corrupted Go stack, diagnosed anywhere.

### 2.1 THIS DECISION WAS WRITTEN WRONG AND THE BUILD CORRECTED IT

**What §2 said before the file compiled:** wrap every entry point in
`try { ... } catch (...) { return kInternal; }`, and induce the backstop with a function that throws
through the boundary on purpose.

**What the build said:** `error: cannot use 'try' with exceptions disabled`. The archive is compiled
**`-fno-exceptions`** — and has been since B1.

> **THERE IS NO EXCEPTION TO CATCH.** `throw` does not compile, `try` does not compile, and
> `operator new` **aborts** rather than throwing `std::bad_alloc`. The property the catch-all was
> going to provide **is already provided, by the compiler, more strongly.**

**This is `GF-31` arriving one phase later: a defect that cannot be written is better than one that is
caught.** A `catch (...)` converts an exception into a code and **loses what it was**; a flag that
makes the exception impossible loses nothing, because there is nothing to lose.

**THE DECISION IS CORRECTED HERE RATHER THAN THE FLAG BEING RELAXED TO FIT IT** — which is the
temptation worth naming, because the doc was written first and the *easy* repair is to make the code
match the doc.

### 2.2 What is enforced, and where

| claim | enforced by |
|---|---|
| no exception can cross the boundary | **`-fno-exceptions` on the archive** — a compiler flag, not a wrapper |
| the flag is still there tomorrow | **`cpp-scan`**, which asserts it on the archive's compile options, and **`BM115`**, which removes it |
| a null handle is `RIFT_INVALID_ARGUMENT`, never a dereference | **`Guard`**, kept for this — one place where every entry point's preconditions live |

**`rift_test_throw` is kept and cannot throw**, and the test that calls it asserts exactly that: the
code round-trips. **It never asserts that an exception was caught**, which would be a claim about a
mechanism this build does not have.

---

## 3. B5-D3 — buffer ownership, and the rule that makes it checkable

**cgo's pointer rules:** C may not store a Go pointer beyond the call. **The design that cannot
violate it** is the one where **C never receives a Go pointer it could store**:

- **into the engine:** Go passes `(ptr, len)` for the duration of one call, and the engine **copies at
  the boundary** — `WriteBatch` already owns its bytes, for the frozen `Batch`'s stated reason;
- **out of the engine:** the caller supplies the buffer. The engine writes into it and returns the
  length it needed. **Nothing the engine allocates ever reaches Go**, so nothing has to be freed
  across the boundary and no lifetime is shared.

**Iterators return BLOCKS**, `rift_iter_next(it, out_keys, out_vals, n)`, filling up to `n` pairs into
caller memory. **The block size is a parameter and not a constant**, because §8's measurement is
supposed to *find* it rather than assume it.

---

## 4. B5-D4 — THE POLLER IS HARNESS-SIDE. B1-Q11, RULED

**Ruled 2026-08-27.**

> **THE POLLER IS PART OF THE HARNESS, NOT THE ENGINE'S CONTRACT.**

**The reasoning, recorded because it is a scope decision under `[A5]` and not a convenience:**

**The frozen contract is `Apply` returning a `SeqNum` without blocking, a monotone `DurableSeq`, and
`OnDurable`.** Nothing in that requires the engine to own a thread that drives syncs. A poller
*inside* the engine would be **a thread of control the engine schedules** — which is exactly the
concurrency `[A5]` already refused at the memtable, and for the same reason: *code that needs a
goroutine is orchestration and lives outside the boundary, or the design is wrong.*

**And it is what makes `kBusy`'s negative direction constructible at all.** §7.6.1 already ruled that
the rig **drives** the poller rather than observing it, and gave the mechanical reason:

> *A rig that only watches the poller can never construct that state on purpose... So a rig that can
> only observe yields a one-directional predicate by construction — the two are the same decision, and
> ruling the predicate bidirectional IS ruling the rig a driver.*

**THE CONSEQUENCE, NAMED SO NO NUMBER IS MISREAD:** a production embedder **supplies its own poller**,
and **nothing in B5's benchmark table is a claim about a poller we ship.** The table measures the
engine and the boundary; the poller in it is the harness's, and its pacing is a harness input.

### 4.1 `kBusy`, bidirectional, per §7.6.1

**Three conditions, all binding:**

1. **BOTH DIRECTIONS ARE DIVERGENCES AND BOTH ARE INDUCED.**

   | direction | the harness asserts | outcome |
   |---|---|---|
   | **spurious** | the engine returned `kBusy` while the harness's record says no backpressure is owed | **divergence, the run fails** |
   | **missing** | the harness's record says backpressure **is** owed and the engine returned `kOk` | **divergence, the run fails** |

2. **`owed` IS COMPUTED FROM THE HARNESS'S OWN RECORD** — what it submitted, and what the poller it
   drives has drained — **never from asking the engine.** Ruling 4, in the phase that adds the code.

3. **THE SCOPE DECISION IS STATED IN THE DOC WITH ITS CONSEQUENCE**, which is §4 above.

---

## 5. B5-D5 — the Go wrapper, and what it may not do

`engine/riftcgo` implements `engine.Engine` over the C API.

**IT DOES NOT IMPLEMENT `OnDurable` BY A C-TO-GO CALLBACK.** `[A1]` prohibits them, and `db.h`'s
divergence 1 already records the shape: *"a callback can be built from a poller and a poller cannot be
built from a callback."* The wrapper exposes `Sync` and registers `OnDurable` handlers it invokes
**itself**, from whatever goroutine the embedder's poller runs on — so the callback still arrives on
the node loop in both modes, and no C frame ever calls into Go.

---

## 6. B5-D6 — differential through the cgo path

**B4's judge is reused unchanged**, and that is the criterion's whole content: *the path is the new
variable.*

**The rig gains one option — which engine produced the artifact** — and the artifact's `PROVENANCE`
already carries the regime, so **no format change**. A cgo-path run and a native run at the same seed
must produce **byte-identical artifacts**, which is a stronger check than both merely agreeing with
the model: it says the boundary changed nothing.

> **THAT COMPARISON IS THE BOUNDARY'S OWN TEST**, and it is available only because the artifact was
> frozen with byte-identity as a rule (`FORMAT-differential` §3, ascending sections).

---

## 7. CF-3, and GF-26

**Every loop B5 adds** carries its progress quantity and a separate correctness instrument. The block
iterator is the one to watch: its bound is the caller's `n`, which is an input rather than a derived
value, so **the loop must not depend on the engine's iterator to stop.**

**Any regime B5 adds is floored before it lands** (`GF-26`), and the cgo path is a regime in the sense
that matters: a lane whose sensitivity is unmeasured is a green that proves only that it ran.

---

## 8. B5-D7 — the numbers, and what the table must contain

**One table, three columns, same workload:**

| | `engine/model` | C++ native | C++ through cgo |
|---|---|---|---|
| fillrandom | | | |
| readrandom | | | |
| mixed | | | |

**THE BOUNDARY COST IS THE DIFFERENCE BETWEEN THE LAST TWO COLUMNS**, measured on the same workload
in the same process shape — which is the only way it is a cost rather than two unrelated numbers.

**And the batch/block sizes are swept rather than assumed**, because the interface exists to amortise
per-call overhead and *how much* is the measurement, not the premise.

**`BENCHMARKS.md`'s rules bind**: methodology first, provenance block, and **what the numbers are
not**. Per B3's precedent the numbers live there and this document keeps the decision.

---

## 9. Landing sequence

| step | lands |
|---|---|
| **B5.0** | the C header, its error-code mapping, and the no-exception wrapper — with the backstop induced |
| **B5.1** | the C implementation over `DB`, buffers and handles |
| **B5.2** | the Go wrapper implementing `engine.Engine`; unit parity against `engine/model` |
| **B5.3** | the poller rig and `kBusy`, both directions induced |
| **B5.4** | the differential through cgo; byte-identity against the native path |
| **B5.5** | the numbers, swept sizes, `BENCHMARKS.md` with provenance |

**Stop condition, unchanged:** an engine defect stops and reports; everything else keeps to the close.

---

## 10. What landed, and where it departed from what was decided

**Written at B5.5's close, before sign-off, because a design document that records only its decisions
and not their fate is a record of intentions.**

### 10.1 `B5-D6` — the differential through cgo. **DELIVERED DIFFERENTLY.** Ruled by Ansh, 2026-08-27.

§6 asked for **byte-identical artifacts** from a cgo-path run and a native run at the same seed. That
is not what landed, and Ansh ruled the outcome correct rather than the criterion missed.

**Why byte-identity is unreachable, and the reason is structural rather than incidental:**

> **`rift_db_open` takes a PATH.** The C boundary cannot be handed a `TestEnv`, and *every*
> differential schedule — including the clean control — runs on one. A cgo run therefore cannot be
> given the same faults, cannot be killed at the same ordinal, and cannot produce the same artifact.

#### The two remedies, declined twice — by the session, and then by Ansh on the same reasoning

**(a) A test-only `rift_db_open_on_env`.**

> *"A test-only `rift_db_open_on_env` adds a production entry point that exists for a test — the
> measurement hook in the interface both engines implement, which you refused at B3.7b and were right
> to."* — Ansh

It is `B3.7b`'s question arriving at a different interface. The C boundary is the surface B5 exists to
keep narrow; an entry point on it whose only caller is a test is permanent, is available to every
future caller, and is justified by nothing a shipping embedder needs.

**(b) A Go artifact encoder.**

> *"A third implementation of the artifact encoder buys byte-identity by adding a third thing that can
> disagree."* — Ansh

The format has **two** implementations because Ansh weighed that cost and paid it deliberately, for
independence. A third is not more independence; it is one more thing to keep in step, bought to make a
comparison come out even.

#### What landed

The C++ engine, reached **through cgo**, compared against `engine/model` on the differential's own
workloads — 3 regimes × 6 seeds × 200 seeded operations, with the operations read from real
`rift_diff` artifacts' `SUBMISSION` sections, so the two runs **cannot** have submitted different
things. With no kill, the recovery **range** the contract permits does not apply, and the comparison
is **exact after every batch**.

#### WHAT IS AND IS NOT CLAIMED — stated plainly, per the ruling

| | |
|---|---|
| **NOT claimed** | that the cgo path produces **identical artifacts** |
| **Claimed** | that the cgo path produces **identical state** |
| **NOT claimed** | that the cgo path is verified **under faults**. Nothing in B5 tests that; `CF-6` carries it to I1 |

> *"What landed [...] is a weaker claim than byte-identity and a real one."* — Ansh

**It was measured rather than assumed to have teeth.** `BM114` reddens it and it names the batch —
`compact/4 after seq 307 (3 ops)` — where `cpp-diff` reports a recovered-state disagreement at the
end. `BM120` floors it (`GF-26`): snapshots that pin nothing, a defect **no other lane in this repo
can see**, because every other snapshot test reads its snapshot immediately and a snapshot that pins
nothing passes all of them.

### 10.2 `B5-D4` — `kBusy`. **DISCHARGED AS RULED, and the quantity was not the obvious one.**

§7.6.1 bound B5 to a bidirectional predicate and a rig that *drives*. Both landed. The finding inside
it is worth keeping in this document because it is a fact about the engine and not about the rig:

> **`Wal::Sync` zeroes `buffered_bytes_` at the swap and then does its I/O with the mutex released.**
> For the whole duration of an fsync the bytes are resident, undrained, and counted by nothing. A
> threshold charged against `buffered_` alone reports **no backlog** exactly while the poller is
> behind — the one moment backpressure means anything.

**THE IDENTITY IS THE RESULT, and it is what to write down:**

> **`buffered_bytes_ + in_flight_bytes_`  ≡  `submitted − drained`**
>
> The left side is the engine's account of what it has taken and not yet made durable. The right side
> is the harness's account of what it handed over and what its own `Sync` calls have covered. **They
> are the same number computed from two disjoint sets of facts** — and *that* is what lets §7.6.1's
> predicate be stated in both directions without asking the engine anything. A threshold on
> `buffered_` alone is not merely less accurate; it is not equal to anything the harness can compute,
> so no bidirectional predicate exists over it at all.

`BM117` is that defect. It passes every sequential test: withhold the poller and `buffered_` alone
crosses the threshold, so the patched engine answers correctly.

**Reaching the window is B1.9a's move, one layer up, and it should be read as the same one.**
`FaultController::AfterEffect` exists because §7.4's in-flight element — *"a `Sync` can complete on
the device with the kill preempting its return"* — cannot be expressed by an injector that runs
*before* the effect; it runs after the implementation and before the Status reaches the caller, and it
consumes no ordinal because **it is the second half of one Env call, not a second call.** The
backpressure window is the identical shape: it exists strictly between a `Sync`'s effect and its
return, and the promotion hook is where the rig gets in. Both could have been reached with a second
thread. Neither is.

> **A MOMENT THAT LIVES BETWEEN AN EFFECT AND ITS RETURN IS A PLACE, AND A RIG SHOULD ENTER IT AS ONE.
> A SECOND THREAD TURNS THE SAME MOMENT INTO A RACE — REACHED SOMETIMES, ASSERTED NEVER.**

### 10.3 `B5-D7` — the numbers. **IN `BENCHMARKS.md`, per B3's precedent.**

This document keeps the decision and the methodology's *reasons*; the table and its provenance live in
`BENCHMARKS.md` §B5.5. The headline the table was run to produce:

> **One pair per boundary crossing costs +111%. Sixty-four costs +21%, and it saturates there.**

That is the measurement that justifies the block interface existing rather than a per-entry cursor
across `extern "C"` — ~90 percentage points removed by batching — and it is why `DefaultBlock = 64` is
64 and not a taste.

### 10.4 The two smaller departures

- **`B5-D2`'s catch-all** was corrected at B5.0 rather than the `-fno-exceptions` flag being relaxed
  to fit it. Recorded as `GF-32`; §2.1 carries the correction.
- **The poller's package name.** `scope.go` reserved `engine/real` and `engine/pump` for it under
  A0.5, "whichever of these two names A0.5 and B5 settle on, the other line goes." **Both went.**
  B1-Q11 ruled the poller harness-side, so neither package will exist, and a reservation for one that
  will not is a hole in the determinism boundary held open for nothing. `engine/riftcgo` — an `Engine`
  implementation, which is a different thing from a poller — took its place, excluded for `sync` and
  `unsafe`, neither hatchable.
