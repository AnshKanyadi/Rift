# DESIGN-I1 — running Track A's stack on the C++ engine

**Status: RULED by Ansh, 2026-08-28. Implementing D2(b).** The candidates and their tradeoffs are kept
below as written, because the rejected alternatives and their reasons are the point of this file.

| question | ruling |
|---|---|
| the crash | **D2**, harness-owned close/discard/reopen, **frozen interface untouched** |
| the unsynced tail | **(b)**, harness-side directory copy per sync point |
| `VisibleSeq()` | **the store tracks it from what `Apply` returned.** It does not move onto the interface |
| determinism | claimed **"across processes on one build"**, with the cross-toolchain limit named where the claim is, and carried as an obligation rather than taken into I1's scope |

**Why D1 was refused, in Ansh's terms:** putting `Crash()` on the frozen interface makes *a real engine
implement a simulator concept* — the interface would then describe the harness rather than the storage
contract. Seven phases were built against that freeze and both changes argued for since were refused;
this one is not different in kind.

**Why D3 was refused:** *"a design that swaps the engine somewhere the faults do not reach is a swap
that proves the boundary compiles."*

---

## 1. The problem, stated from the tree rather than from the plan

I1's scope is *"Track A's stack running on the C++ engine through `riftcgo` instead of `engine/model`."*
The obstacle is not that the engine is defaulted to the model. **It is that the stack is concretely
typed to it, and reaches past the frozen interface to do so.**

**The stack holds a concrete type, not the interface:**

| site | field |
|---|---|
| `store/node.go:185` | `db *model.DB` |
| `store/machine.go:31` | `db *model.DB` |

**And it calls two methods that `engine.Engine` does not have.** Verified against the interface
verbatim — `Apply`, `DurableSeq`, `OnDurable`, `Get`, `NewIter`, `NewSnapshot`, `ApproximateDiskBytes`,
`Close`, and nothing else:

| method | on the interface? | on `riftcgo.DB`? | used at |
|---|---|---|---|
| `VisibleSeq() SeqNum` | **no** | **no** | `store/node.go:1450`, the restart guard |
| `Crash()` | **no** | **no** | `store/machine.go:355`, `sim/toy/toy.go:495,677` |

**Neither omission is an oversight in the interface, and that is the point.** A real engine has no
`Crash()` method: it crashes because the process dies. It has no `VisibleSeq()` because `Apply` already
*returns* the sequence at which the batch became visible — the caller knows it, so the engine need not
remember it. The interface is coherent. **What has no answer yet is how a *simulated* crash is
expressed against a *real* engine**, and that is a decision rather than a detail.

*(`DeleteRange` was checked and is not an instance of this: Amendment A3 puts it in the frozen
interface, and it is there — as a `Batch` operation, `engine/batch.go:71`, reaching the engine through
`Apply`. Consistent.)*

**Four construction sites** bind the engine today: `store/node.go:450`, `store/machine.go:61`,
`store/replay.go:96`, `sim/toy/toy.go:473`.

---

## 2. What was measured before any of this was designed

Grounding, so the candidates below argue with facts rather than with expectations.

**The poller is not in `riftcgo`, deliberately.** B1-Q11 at B5: the poller is harness, not engine
contract. `riftcgo` spawns **no goroutine** — it holds a `sync.Mutex` and nothing else. *"A production
embedder supplies its own poller."*

> **THIS IS THE MOST IMPORTANT FACT IN THIS DOCUMENT.** A simulator that drives `Sync()` itself, on its
> own event loop, at its own scheduled fsync-completion events, introduces no thread of control. The
> thing that would have made the C++ engine undeterministic under simulation is absent by a Track B
> ruling made before I1 existed, for unrelated reasons.

**No nondeterminism sources in `engine-cpp/src`.** Counted, excluding tests:

```
unordered_map 0   unordered_set 0   std::rand 0   std::random 0
time( 0           std::chrono 0     getpid 0      std::thread 0
```

**Directory order is already defended.** `posix_env.cc:260` — *"returns children in whatever order the
filesystem gives… recovery must sort by parsed file number"* — and `TestEnv` hands children back
**reverse-sorted on purpose** to catch any reliance on it. Track B built the defence and the probe for
it before I1 asked.

---

## 3. Candidates for the crash question

The injection question (`*model.DB` → `engine.Engine` at four sites) has one sane answer and is not
where the design content is. **The content is what `Crash()` becomes.**

### D1 — keep `Crash()` and add it to the frozen interface

`riftcgo.DB.Crash()` closes the C++ DB, discards the unsynced tail, and reopens.

- **For:** smallest diff. Every call site is unchanged. The sim's fault model is untouched.
- **Against:** it puts a method on the frozen interface that **exists only for the simulator**, on the
  one interface both tracks agreed to keep narrow. A production embedder would find a `Crash()` it must
  implement and can never call. This is B5's own §10.1 argument against `rift_db_open_on_env`, arriving
  one layer up: *permanent contract surface whose only caller is a test.*
- **And it is not obviously implementable.** Discarding the unsynced tail from *inside* a live DB is
  not "close and reopen" — it is asking the engine to forget what it has already made visible.

### D2 — a crash is a close, a discard, and a reopen, owned by the harness

The engine gains nothing. A `SimEngine` wrapper in the harness owns the directory, and a crash is:
close the DB, roll the directory back to its last-synced state, reopen. Recovery then runs **for real**
and produces the synced prefix by the engine's own recovery path.

- **For:** the frozen interface does not move. The crash is a real crash — the C++ recovery path
  executes, which is *precisely what CF-6 asks for* and cannot be got any other way.
- **For:** it makes the fault schedule cross the boundary as a **consequence** of the design rather
  than as something bolted on, which is CF-6's own stated expectation of I1.
- **Against:** "roll the directory back to its last-synced state" is the hard part. Doing it honestly
  needs the harness to know which bytes were synced — which is exactly what `TestEnv` knows and what
  `rift_db_open` **cannot be handed**, per CF-6. So D2 either needs the test-Env door B5 declined, or a
  cruder approximation.
- **Against:** reopen cost per crash. Crash storms are a normal workload here.

### D3 — the model keeps the crash; the C++ engine is swapped in *underneath* verification only

I1 runs the corpus and sweeps with `riftcgo` as the engine **for every operation except the fault
schedule's crash**, which continues to be modelled — with the divergence stated rather than hidden.

- **For:** cheapest, and it still tests the overwhelming majority of the boundary: every `Apply`,
  `Get`, `NewIter`, `NewSnapshot`, `ApproximateDiskBytes` under real workloads and real data.
- **Against, and it is disqualifying on its own terms:** it closes **none** of CF-6's three checks,
  because all three are about what the wrapper does *when killed*. It would let I1 report "the stack
  runs on the C++ engine" while the one gap I1 was scheduled to close stays open — and CF-6 predicted
  exactly this failure: *"'it happens incidentally' is how a gap stays open while looking closed."*

---

## 4. Recommendation

**D2, with the interface untouched, and with the directory-rollback question raised now rather than
discovered mid-phase.**

D1 buys a small diff by widening a frozen interface for the simulator's benefit; the project has
already refused that trade once, at a lower layer, for reasons that apply unchanged here. D3 is
cheapest and fails the phase's own purpose.

**But D2 has a dependency that must be settled before code, and it is the one CF-6 flagged:** an honest
"discard the unsynced tail" needs to know what was synced. Three ways, and I have no authority to pick:

| | how | cost |
|---|---|---|
| **a** | reopen `rift_db_open_on_env` — the test-only door B5 declined | permanent boundary surface; B5's argument against it stands |
| **b** | the harness copies the directory at each successful `Sync` and restores that copy on crash | no boundary change; O(data) per sync, which may be fine at sim scale and may not |
| **c** | crash = close + reopen with **no** rollback, accepting that the engine keeps everything it had written to the OS | **cheapest and dishonest**: it does not lose unsynced writes, which is the fault being injected. `BUG-005` is the bug this exact softening produced in A1 |

**(c) is named only to be refused.** It is what the code would drift into if the question were left
open, and `store/node.go`'s own comment already records the cost: *"a process that 'recovered' writes no
crash would have kept. It then answered for them… that is the precise inverse of the fault being
injected."*

**My recommendation is (b)**, measured before committed: at sim scale a node's directory is small, and
a copy per successful sync is a bounded, honest, boundary-free way to model exactly the loss the
constitution requires. If it is too slow, that is a **measurement** that then justifies (a) on evidence
rather than on preference.

---

## 5. Determinism through the boundary, and where it is most likely to break

Ansh's criterion: *the same seed produces the same trace hash across separate process invocations with
the C++ engine underneath.*

**What is already in our favour**, from §2: no threads, no clocks, no RNG, no unordered containers, no
`getpid`, directory order defended and probed.

**What is not tested and is the stated risk.** The FMA catch (`DESIGN-A0.4` §Q4) was exactly this class
on the Go side: `off + slope*(t-start)` fuses to `FMADD` on arm64 and not on amd64 without FMA, so the
same seed could differ in the last bit on two machines, surfacing months later as an unreproducible
soak failure. **The C++ engine has never been compiled by a second compiler and had its output
compared.** Everything in §2 is a source-level absence of nondeterminism; none of it is evidence about
what two toolchains do to the same source.

**So the fresh-process gate is the right instrument and it is not sufficient by itself.** Two runs in
two processes on one machine share a compiler. The honest claim after I1, unless a second toolchain is
added, is *"deterministic across processes on one build"* — and it should be written that way rather
than as *"deterministic through the boundary."*

---

## 6. CF-6's three checks, and where each would actually be exercised

CF-6 is explicit that incidental exposure is not closure. Under **D2**:

| check | exercised by | verified how |
|---|---|---|
| 1. crash mid-`Apply` leaves no Go-side state claiming a sequence the engine never took | every crash in the fault schedule, since `Apply` returns a `SeqNum` before durability | a directed test crashing between `Apply` and `Sync`, asserting the reopened `DurableSeq` is below the returned `SeqNum` |
| 2. `OnDurable` fires from the Go side with the **engine's** watermark, not a remembered one | restart, which re-registers the callback against a reopened DB | a directed test asserting the post-restart watermark equals what recovery produced, not what the wrapper last saw |
| 3. an iterator holding a block across a crash must not be read as live | crash storms during scans | a directed test holding an open iterator across a crash and requiring the read to fail rather than return stale Go-side bytes |

**All three want directed tests.** None of them is safe to leave to the sweep, which is the mistake
CF-6 names in its own text.

---

## 7. Which claims rest on which engine — required by Ansh, and answerable now

| claim | rests on |
|---|---|
| every Raft, multi-raft, MVCC, transaction and read-index safety property; all 25,000 exit-run seeds; every historical bundle's *recorded* verdict; every power floor and ceiling | **`engine/model`.** These were measured on the reference engine and I1 does not re-earn them |
| byte-identical trace replay from a seed | **`engine/model` only.** The constitution scopes it there and I1 does not extend it |
| the C++ engine's own recovery, format, compaction and space/read amplification | **the C++ engine**, via its Env fault rig, kill-point sweeps and its own suite |
| *the two engines agree* — iterator output byte-identical under randomised workloads | **both**, via `engine/differential` |
| **new at I1:** the stack's safety properties hold when the storage underneath is the real engine | **the C++ engine, with `engine/model` as the reference that says what "hold" means** |

> **`engine/model` does not become a stepping stone at I1. It becomes the control.** Every I1 divergence
> is a difference between two engines, and it is only a *finding* because one of them is the engine every
> Track A number was measured on.

---

## 8. What Ansh must rule on

1. **D1, D2 or D3**, and if D2, then **(a), (b) or (c)** for the unsynced-tail question — noting (c) is
   named to be refused.
2. **Whether `VisibleSeq()` moves onto the frozen interface** or the store tracks visible sequence
   itself from what `Apply` returned. The second needs no interface change and looks right; it is
   listed because it touches a frozen artifact either way.
3. **Whether the determinism claim is stated as "across processes on one build"** until a second
   toolchain exists, or whether adding a second toolchain is in I1's scope.

## 9. The scope pass, run 2026-08-28 with the archive built — the one measurement I1 has taken so far

**The exclusion stands, and the argument for it is now measured rather than argued.** Archive built
(`librift_capi.a`, `librift_engine.a`), exclusion removed, pass run over `./engine/riftcgo/`:

| where | findings |
|---|---|
| `engine.go`, `iter.go` | **5** — 4× `unsafe`, 1× `sync` |
| the **cgo-generated** file | **3** — `unsafe`, `syscall`, `runtime/cgo` |
| test files | 29 |

**Neither argument reached the reason.** Track B said `sync` and `unsafe`, from the source: right in
conclusion, incomplete in evidence. Ansh predicted core scope with a hatch at the boundary: wrong. The
pass added `syscall` and `runtime/cgo` **from a file nobody wrote**, and the decisive finding is in
that half.

The hatch route is unavailable rather than unattractive: `sync` is unhatchable by A5's own text, and
`HATCHES.txt` is keyed `path:line` while the generated file lives at a go-build content hash that
changes with every edit, Go version and machine — **a hatch needs an address and there is none.** That
is a fact about this project's mechanisms: any future package carrying generated code meets the same
wall, **exclusion or nothing**, forced by the registry's key.

> **THE REASON TO MEASURE RATHER THAN ARGUE IS NOT THAT ARGUMENTS ARE SLOPPY. IT IS THAT THEY ARE
> BOUNDED BY WHAT THE ARGUER CAN SEE.** (`BUGS.md` GF-45.)

Recorded in `scope.go` in place of the provisional marker, and in `CARRY-FORWARD.md` as closed.

---

## 10. `(b)`'s cost, stated as an idealization rather than as a footnote

Per Ansh's ruling, this is now `DESIGN-A0` §7 item 3's first sub-item:

> **A simulated crash on the C++ engine recovers from a state the HARNESS constructed, not from a state
> a kernel left.** The harness copies a node's directory at each successful sync point and restores it
> on crash, because `rift_db_open` takes a path and cannot be handed a `TestEnv`. What is verified is
> recovery from a directory the harness rolled back; a real power loss leaves a directory no harness
> writes — partial sectors, reordered metadata, a rename that landed while its data did not.

Those live in Track B's Env fault rig, where the injection is at the syscall and the recovery is the
engine's own. **The two halves are complementary and neither subsumes the other.** I1's claim is
bounded by that line: the fault schedule crosses the cgo boundary carrying *the harness's model of
durability* rather than *the device's*.

---

## 11. CORRECTION to §1: there are THREE off-interface methods, not two

Found while enumerating the store-side contract for the implementation. §1's table listed
`VisibleSeq()` and `Crash()`. The full set, derived by extracting every `*.db.Method` call in `store/`
and `sim/toy/`:

```
12 Apply    5 Get    4 VisibleSeq    3 AdvanceDurable    2 NewIter    2 DurableSeq    2 Crash
```

**`AdvanceDurable(seq)` is the third, and it is the one that matters.** The design doc was written from
a reading of the restart path and the crash path; the durability path was not enumerated, so the method
that drives it was not seen. **`GF-44`'s own shape, four days old, in the document that records it** —
a derivation that finds one pattern reports completeness over that pattern.

---

## 12. THE GAP D2(b) DOES NOT CLOSE, found in implementation and reported before building on it

**`AdvanceDurable(seq)` advances the watermark to a *specific* sequence. `riftcgo` has no way to express
that, and the C API cannot be asked for it.**

Three measurements, each verifiable in one command:

1. **The model advances to a prefix.** `engine/model/model.go:157` — *"the model's stand-in for an fsync
   completing, and the only way the watermark moves."* It takes a `seq`.
2. **The simulator schedules that sequence at APPLY time and fires it later.**
   `store/node.go:631` and `store/machine.go:911`:
   `s.At(at+SyncLatency, KindDurable, node, Stamp(epoch, seq))`. In the `SyncLatency` interval the node
   applies more batches, so at fire time `tok.Value` is **below** the visible sequence. This is the
   normal case, not an edge one.
3. **`riftcgo.Sync()` covers everything, and the C API has no alternative.**
   `rift_db_sync(rift_db*, uint64_t* watermark)` — **no prefix argument**, on a frozen boundary. Track B
   states the semantic and their differential *asserts* it: *"a completed Sync must cover EVERYTHING
   SUBMITTED"*, with `if w != ref.VisibleSeq() { fatal }`.

**So on `riftcgo`, a scheduled fsync completion for sequence 5 also makes 6…10 durable.** The unsynced
tail collapses to empty at every sync point, where on the model it does not.

**What that does, stated exactly.** It is **not** a safety break. A replica still believes only
`tok.Value` is durable, and believing *less* durable than reality is conservative — it can never
produce an over-acknowledgement, which is the direction `BUG-005` failed in. **What it does is weaken
the fault:** a crash on `riftcgo` loses strictly less than the same crash on the model.

> **AND THAT IS THE THING THIS PROJECT DOES NOT DO.** `BUG-005` is the precedent — an injector softened
> into something easier to satisfy — and the rule since has been that a checker or an injector is never
> weakened to pass. This weakening would arrive through *granularity* rather than through the rollback
> question `(b)` already answered, which is why `(b)`'s ruling did not catch it.

**Two ways out, and the choice is Ansh's.**

| | approach | cost |
|---|---|---|
| **B** | the harness copies the directory at **every Apply**, keyed by sequence, and on crash restores the copy matching the last *fired* `tok.Value` — so durability-for-crash-purposes is defined by the harness, exactly as the model defines it | a copy per Apply rather than per sync. **Unmeasured**; at sim scale it may be nothing or it may dominate. It also means crash recovery no longer starts from the engine's *own* durable point, which is a fidelity loss of a different kind |
| **C** | call `Sync()` only at durability events and accept its full coverage: the unsynced window becomes *[last sync, now]* rather than *[tok.Value, now]* | free, and honest only if stated as an idealization. The injector still injects — crashes between syncs still lose writes — but it loses less than the model would, and the difference is not bounded by anything we measure |

**RESOLVED: B was built, measured, and is not affordable. C stands, forced by arithmetic.**

### B, built and measured

`engine/simcgo` implements it and is kept — the correctness test passes and the cost test is the
number, so both can be retaken rather than believed.

**A defect in B, found by its own test.** The first version snapshotted straight after `Apply`. `Apply`
does not block on I/O — the frozen contract says so — so the snapshot captured *a directory the data
had not reached yet*, and the test asserting a kept write survives a crash failed with `key not found`
on the write it was meant to keep.

> **A SNAPSHOT IS A CLAIM ABOUT WHAT IS ON DISK. TAKING IT AFTER AN OPERATION THAT DELIBERATELY DOES
> NOT TOUCH THE DISK IS A CLAIM ABOUT NOTHING.**

So correct-B costs an **fsync *and* a tree copy per `Apply`**. That is what was measured.

| | |
|---|---|
| Apply only | 209 µs per Apply |
| Apply + fsync + snapshot | **5.012 ms** per Apply |
| **overhead** | **4.803 ms, 24×** |
| Applies per raft seed (seeds 7 / 42 / 1234) | **3,707 / 4,829 / 3,577**, mean ≈ 4,038 |
| overhead per seed | **≈ 19.4 s** |
| 500-seed smoke | ≈ 2.7 h |
| 25,000-seed exit run | ≈ **135 CPU-hours** |

A raft seed on the model completes in well under a second today. **B multiplies the sim's cost by
orders of magnitude, and the answer is arithmetic rather than preference.**

### C, and the bound that was not going to stay unstated

C's cost was written as *"less than the model would, by an amount nothing bounds."* Ansh refused the
unbounded clause. Measured on the model, at every durability event, how far the sequence it carries
lags what has been applied — that lag **is** the extra a C++-engine crash keeps:

| seed | durability events | mean lag | max lag |
|---|---|---|---|
| 7 | 1,901 | 2.09 | 16 |
| 42 | 2,394 | 2.82 | 39 |
| 1234 | 1,652 | 2.51 | 16 |
| 99 | 1,813 | 2.42 | 25 |
| 555 | 2,140 | 2.68 | 24 |

> **A crash on the C++ engine keeps, on average, ~2.5 more sequences than the same crash on the model,
> and at most 39 across these seeds.** The unsynced window is not removed — a crash between syncs still
> loses everything applied since the last one — it is **narrower by that measured amount**.

**A stated idealization with a measured gap is honest; one with an unmeasured gap is a hope.** The
idealization is in `DESIGN-A0` §7 beside the other two, and it carries both numbers: the lag it costs,
and the 135 CPU-hours that made it forced rather than chosen.

**Nothing about the direction changes:** the difference is conservative — a replica believes less
durable than reality, which cannot over-acknowledge — but *conservative and correct are different
properties*, which is why B was built and measured before C was accepted.

---

## 13. State

Ruled, recorded, and stopped on §12. The corpus has not been rerun and no sweep has been run on
`riftcgo`.
