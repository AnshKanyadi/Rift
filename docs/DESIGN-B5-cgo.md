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

## 2. B5-D2 — NO EXCEPTION CROSSES, AND IT IS ENFORCED RATHER THAN PROMISED

**An exception unwinding through a C frame into Go is undefined behaviour**, and the failure is not a
crash at the boundary — it is a corrupted Go stack, diagnosed anywhere.

**Every `extern "C"` entry point is wrapped**, and the wrapper is one place:

```
try { ... } catch (...) { return kInternal; }
```

**AND `catch (...)` IS NOT THE WHOLE ANSWER**, which is the part worth writing down. It converts an
exception into a code and **loses what it was**. So the engine's own discipline stands: this engine
does not throw, `RIFT_CHECK` aborts rather than throws, and `Status` is the error channel everywhere.
**The catch is a backstop for `std::bad_alloc` and for a future contributor**, not the design.

> **A BACKSTOP THAT NOBODY HAS SEEN FIRE IS A BACKSTOP NOBODY HAS TESTED.** So a test throws through
> a boundary function deliberately and asserts the code comes back — and a mutant removes the catch.

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
