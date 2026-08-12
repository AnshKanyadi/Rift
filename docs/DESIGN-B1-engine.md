# DESIGN-B1: Env, the WAL, the memtable, and the recovery contract

**Status:** **REVISION 2 — rulings of 2026-08-12 applied; awaiting rulings on §13.** Nothing is
self-ratified, so nothing is marked PROVISIONAL. No C++ file is written until §13 is ruled on and the
remote gate clears.
**Phase:** B1 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Blocks:** all of Track B. **Depends on:** the `engine/` interface frozen at A0.5, which this must
meet exactly — not approximately, because B4's differential rig defines "correct" as "byte-identical
to `engine/model`".

Decisions are numbered `B1-D<n>`. Twelve of them; **D3 and its sub-decisions are the WAL record
layout surface to be frozen**, and they are gathered in §5.3 so the freeze has one address.

---

## 0. Ruling echo

Rulings received since the last report — Ansh, 2026-08-12, on DESIGN-B1 revision 1. Quoted verbatim,
each followed by where it lands.

> Ruling echo and stop discipline: correct, and nothing self-ratified.

> The closing scoping note is the second best thing: exactness is a property against TestEnv, and
> against a real filesystem after kill -9 the guarantee degrades to recovered in [DurableSeq,
> VisibleSeq] because page-cached bytes survive. Ratified as stated, including the framing that this
> is a weaker observer and not a weaker engine. It goes in the verification-scope list beside
> DESIGN-A0 section 7, and it goes there before any implementation, not after, because a scope caveat
> written after the claim is a retraction.

→ §1.1 carries the paste-ready text. It is a Track A file, so §12 lists it as the one item that must
clear before B1.1 is written, rather than as a nice-to-have.

> Q4, and the thing it revealed. TSan: approved, added as a required lane. MSan: declined, agreed.
> But the lock-free skiplist is not approved and was never ruled on. Amendment A6 governs: the
> simplest correct thing wins v1 and the faster thing is a recorded upgrade path. B1 has no
> authorized concurrency requirement, Apply is non-blocking by contract rather than by parallelism,
> and the syncer and any poller are deferred to B5. A lock-free structure spends the project's
> scarcest resource, C++ correctness under fault injection, to buy throughput no measurement has
> asked for, and its failure mode is precisely the one this project exists to eliminate: a bug that
> appears on one machine, at one core count, one run in ten thousand, and does not replay. You
> already made the honest observation that no lane proves it correct and offered to downgrade the
> claim. Take the other branch instead. Ruling: the memtable is protected by a lock. Record lock-free
> as a rejected alternative in the decision, with the measurement that would justify revisiting it
> and the note that it is a candidate only after B5 produces standalone numbers showing the lock is
> the bottleneck. TSan stays regardless, because a locked structure with a wrong lock is still a race.

→ §6.3 (B1-D6c, RULED), §9.3 (TSan required), §10 (BM14 exists to prove the TSan lane is not
decoration).

> Q2, GoogleTest: FetchContent is declined. Vendor it. Every number reproducing from a clean clone by
> one script is a commitment I intend to publish, and a build step that reaches the network fails in
> exactly the situation where the claim matters most, which is a stranger checking our work. Vendor a
> pinned tree under third_party/ with the version and commit recorded in the doc, and no network call
> in any lane.

→ §9.2. Version and commit recorded there; the hash was read from the upstream remote, not recalled.

> Q1: approved as recommended. C++17, -fno-exceptions, -fno-rtti, -Werror, clang and gcc both pinned.
> -fno-exceptions is not only house style here, it is load-bearing for the cgo seam: no exception may
> cross into Go, ever, and the flag makes that structural rather than a review habit.

→ §9.1.

> Q3, the lying disk: characterization-only is approved, with a hard condition. A run with that
> injector enabled may never be reported as evidence for the recovery contract, in any column,
> ledger, or README sentence. Assertion (ii) being suspended means the contract was not under test,
> so the run is data about behavior and not a verification result. Give it its own column and a name
> that cannot be misread as a pass, and make the suppression mechanical: the outcome type carries the
> fact that (ii) was suspended, so no future summarizer can aggregate it in by accident. This is the
> same shape as inconclusive never counting as pass.

→ §7.5, the closed `RunOutcome` enum and its single policy method.

> Q5, unbounded WAL buffer: deferring policy to B5 is approved, unbounded is not. Unbounded in a
> fault-injected harness means an out-of-memory kill, which is the worst possible failure signal
> since it destroys the run that would have explained it. Set a hard cap in B1 that halts loudly with
> a named, closed-enum outcome when exceeded, and induce it in a test. The cap is a tripwire, not a
> policy: it exists so that when the syncer falls behind we get a named failure with a plan reference
> instead of a dead process. Status::Busy at B5 remains the leaning and is not ruled now.

→ §8.3 (the two caps and the ordering invariant between them), §7.5 (`kTripwire`), §10 (BM12).

> Q6: approved, implement DeleteRange over the memtable in B1 so finding 3's decision is exercised
> before B2 stacks on it.

→ §8.

> Finding 3, post-expansion recording: approved, the recovery circularity argument is correct and
> recording the raw op would leave recovery re-expanding against a state it is still rebuilding. Two
> conditions. State the maximum WAL record size and what happens when an expansion exceeds it,
> because DeleteRange(nil, nil) producing a record proportional to the keyspace will cross any block
> boundary you choose, and a multi-block record is exactly the case the torn-tail rule must handle
> without ambiguity. Show me that interaction explicitly: a torn multi-block record at the tail must
> be distinguishable from interior corruption by the same rule as a torn single-block one. And record
> that ruling 1's real range tombstones in B3 are what retires this cost, so the fragmentation path
> is a known-temporary consequence with a scheduled end rather than a permanent property.

→ §8.2 (the cap and its arithmetic), §5.4.2 (the fragment-chain rule, which is the "same rule"
generalized rather than a second rule), §8.4 (the scheduled end).

> Finding 2, short writes: accepted as reported, including the named cost that short writes never
> combine with a kill point in one run. The production Env's short-write loop gets its own injectable
> seam and its own unit test, and the doc states the uncovered combination in the fault matrix rather
> than leaving the cell blank.

→ §3.3; the short-write column now carries the reason in every cell and a footnote row, so the matrix
records the gap instead of implying there isn't one.

> Finding 5: approved. Make Apply's no-I/O property an assertion via a per-thread Env-call counter
> that must not move across Apply. An invariant that fails a test is worth more than a sentence in a
> design doc, and this is the same move as putting persist-before-reply inside raft/.

→ §8.3, strengthened past what was asked: the same counter carries a second assertion, that the DB
mutex is never held across an Env call, which is what makes the lock ruling above safe under a slow
`Sync`.

> Q7: confirmed, your reading is correct. A Sync that completes on the device but whose return the
> kill preempts makes the expected recovery point the two-element known set, and that is what "any
> watermark the sync-completion schedule can produce" means. Three conditions so the set never
> becomes an escape hatch. The set is derived from the harness's own records of what it issued, never
> from the engine or the manifest, per ruling 4. Each element is compared exactly and the verdict
> names which element matched. And both elements are individually induced by tests, so we have seen
> recovery land on each, because a two-element set where only one element has ever been observed is a
> one-element contract with a spare excuse attached.

→ §7.4, all three conditions, with the two induced tests named in §10.

> Q8: approved. engine-cpp/ in the rift-b worktree, rebased onto main. Do the rebase at the start of
> your next cycle and report the resulting HEAD. Linux for every lane plus a macOS cpp-test is right,
> and note in the doc that the macOS lane is our first cross-platform evidence for the Env seam, in
> the same spirit as the cross-architecture datapoint Track A is waiting on CI for.

→ §9.3. The rebase is a next-cycle action, not a this-cycle one; §12 records it as owed.

> First, the gate table where every gate names the failure that will be induced to prove it fires is
> the correct format and I want it kept, with one column added: which mutant class would catch a
> regression in that gate.

→ §10, and the mutant catalogue moved *above* the gate table so the column references something the
reader has already met.

> Second, state explicitly in the record format section where the sequence number lives, what the
> per-record checksum covers, and whether the checksum includes the length prefix. Recovery's ability
> to distinguish a torn tail from interior corruption depends entirely on what the checksum covers,
> and I did not see it stated.

→ §5.3.2. It does include the length, which is a deliberate divergence from LevelDB, and the reason
is exactly the one the question implies.

Rulings inherited from the prior cycle remain in force and are reproduced in §1.2.

---

## 1. Scope

### 1.1 The verification-scope entry, paste-ready

Ratified 2026-08-12 and required to land **before** B1.1. Written as item 7 of DESIGN-A0 §7's
numbered list, in that list's voice:

> 7. **The C++ engine's exactly-at-watermark recovery guarantee is a property against TestEnv, not
>    against a real filesystem.** TestEnv models power loss: a file's durable image advances only when
>    a `Sync` covering it returns, so a kill discards everything written since and recovery lands on
>    exactly the watermark that was promised. The production Env on a real filesystem does not provide
>    that. After a process kill, page-cached bytes survive, and recovery can legitimately return
>    **more** than the last promised watermark; the guarantee there is
>    `recovered ∈ [DurableSeq, VisibleSeq]`. The safety-critical half — `recovered ≥ DurableSeq`, which
>    is "committed is forever" — holds under both. This is a weaker **observer**, not a weaker engine:
>    the exactness we verify is real, and what we cannot verify outside TestEnv is the absence of a
>    conservative surplus, which no invariant depends on. See DESIGN-B1 §4.

And the one-sentence form for README's How It Is Verified section, where the list is prose:

> The storage engine's exact-recovery property is verified against a power-loss fault environment; on
> a real filesystem after a process kill the guarantee is that recovery returns at least the
> acknowledged-durable prefix, and possibly a conservative surplus of unsynced data that no invariant
> depends on.

### 1.2 Inherited rulings, verbatim

Carried from the prior cycle, still binding.

> **1.** DeleteRange is in the frozen Engine interface. This engine implements it internally as
> iterate-and-point-delete through B2; real range tombstones land before any published benchmark that
> exercises deletes.

Binds §8, which now also carries the record-size cap and the scheduled end of the cost.

> **2.** No serialized byte this engine ever sees carries a Mono instant. Keys and values are opaque
> bytes by construction; the engine never interprets time.

Binds §3.4 (Env has no clock), §5.3 (no timestamp in any record, header, or filename), §6.2 (the
comparator is bytewise and not pluggable), §7.2 (WAL files ordered by parsed file number, never by
mtime and never by `GetChildren` order).

> **3.** Recovery contract (the recovery-to-intermediate-sequence class from A0.5): crash recovery
> yields exactly the state at the durable watermark, for any watermark the sync-completion schedule
> can produce, including the dangerous direction where a lagging watermark recovers MORE than it
> promised. B4's rig compares recovered state against engine/model's state-at-seq; design the WAL and
> manifest so that comparison is exact.

Binds §4, §5.2, §5.4, all of §7. The sync group exists only to make the set of reachable recovery
points equal the set of promised watermarks.

> **4.** Oracle independence: the crash rig's verdicts come from its own op log, never from asking the
> engine what it believes it holds. The recorded sentence is "an oracle that interrogates the engine
> believes the lie."

Binds §7.3 and, now explicitly, Q7's two-element set: §7.4 derives it from the harness's own issue
log and forbids the engine and the manifest as sources.

> **5.** Amendment A5 applies with the Env seam as this language's enforcement mechanism: every
> syscall through Env, no wall-clock reads, no ambient randomness, no floats on any path that affects
> on-disk bytes.

Binds §3 and §9.4. The Env seam enforces the syscall clause and is structurally blind to the other
three; §9.4 is the mechanism for those.

> **6.** Compaction policy is a DESIGN-B3 decision per Amendment A6: the simplest correct policy wins
> v1, chosen with measurement; multi-level leveled is a recorded upgrade path, not a v1 requirement.

Binds §6.3 harder than it did in revision 1 — the lock ruling above is this ruling applied one level
down, to a place I had not thought to apply it.

> **7.** Build and hygiene per CLAUDE.md: CMake producing a static archive, ASan and UBSan lanes as
> definition of done for any code that eventually lands.

Binds §9, now with TSan added as required and GoogleTest vendored.

### 1.3 The two standing document requirements

> First, the Env surface is a fault-injection surface before it is a portability surface: state
> explicitly, per call, how TestEnv injects sync loss, torn writes, short writes, IO errors,
> disk-full, and a kill point, and if any call cannot express one of those, say which and why in the
> doc rather than omitting it.

§3.3, with the short-write gap now written into the matrix cells rather than left blank.

> Second, the WAL section must state its torn-tail rule as a decision with rejected alternatives: what
> a partially written trailing record means at recovery, how it is distinguished from corruption in
> the middle of the log, and why that distinction is safe under the exactly-at-watermark contract.

§5.4, extended in §5.4.2 to the multi-block case as one generalized rule rather than two rules.

---

## 2. The engine in one paragraph

`Apply` appends a collapsed, fully expanded op list to an engine-owned memory buffer and to a
mutex-protected skiplist memtable, and makes **zero Env calls**. `Sync` — called by a different
thread — takes the buffer, writes it to the WAL as a **sync group** terminated by a `GROUP_END`
record, fsyncs, and returns the group's high sequence as the new watermark. Recovery replays whole
groups and nothing else. Everything below is the consequence of wanting that last sentence to be true
under every kill point.

---

## 3. The Env abstraction

### 3.1 What Env is for, in priority order

1. **A fault-injection surface.** Every failure the B1 and B4 rigs need must be expressible as the
   behaviour of an Env call, and every Env call must be a kill point.
2. **The A5 boundary for syscalls.** Every syscall goes through it for the reason every clock read
   goes through `Clock`.
3. **Portability.** Third, and barely: Linux and macOS, nothing else.

The order decides ties. Where a portable-looking abstraction and an injectable-looking one differ, the
injectable one wins.

### 3.2 B1-D1 — the shape of the surface

**Candidates.** (a) LevelDB-shaped file objects — `Env` is a factory for `WritableFile`,
`SequentialFile`, `RandomAccessFile`, `Directory`. (b) Flat and syscall-shaped — one `Env` with
`Open`/`Read`/`Write`/`Sync`/`Rename`/`List`, files as opaque handles. (c) (a)'s vocabulary with every
call routed through one interception choke point inside Env.

**Tradeoffs.** (a) is proven and per-file fault state has an obvious home, but "inject at every call"
becomes N copies of one wrapper, which rot at different rates — and an injector that has silently
become unreachable looks exactly like an injector that found nothing. (b) makes injection uniform and
the kill counter trivial, but pushes file offsets into the engine and loses the compile-time
distinction between an append-only file and a seekable one, which is worth having when the WAL is
append-only by construction.

**Recommendation: (c).** Every method begins with `FaultController::Intercept(CallSite)`, which
returns "proceed" or the injected outcome. The controller owns the kill counter, the quota, the error
schedule and the ledger; file objects own only their bytes. Runtime virtual dispatch, not templates:
the differential and kill-point rigs construct a production DB and a TestEnv DB in one process, and
one virtual call per syscall is unmeasurable against a syscall.

**Rejected:** (a) alone — N wrappers is N places for a fault to be unreachable. (b) — the engine would
carry offsets and the WAL's append-only property would stop being a type-level fact.

### 3.3 The fault matrix

`✓` = TestEnv injects it here. Every other cell states why not, per the standing requirement — no cell
is blank. The kill-point column is `✓` throughout by construction: B1-D1(c)'s choke point counts every
call, so "kill at any syscall boundary" is a property of the seam rather than of anyone's diligence.

| call | sync loss | torn write | short write | IO error | disk full | kill point |
|---|---|---|---|---|---|---|
| `Env::NewWritableFile` | — nothing synced yet | — no bytes yet | — ⁽¹⁾ | ✓ `EACCES`, `EMFILE`, `EIO` | ✓ `ENOSPC` on inode allocation | ✓ |
| `Env::NewSequentialFile` | — read path | — read path | — ⁽¹⁾ | ✓ `ENOENT`, `EIO` | — read path | ✓ |
| `Env::NewRandomAccessFile` *(declared; first used B2)* | — read path | — read path | — ⁽¹⁾ | ✓ | — read path | ✓ |
| `Env::GetChildren` | — no durable state of its own | — no bytes | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::GetFileSize` / `FileExists` | — query only | — query only | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::DeleteFile` | ✓ the unlink lands in `content` and not in `durable` until the directory is synced | — a directory entry is all-or-nothing | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::RenameFile` *(declared; first used by B2's manifest swap)* | ✓ same as above — this is the injector that finds a missing directory sync around an atomic rename | — atomic at the filesystem level, which is the guarantee we are relying on | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::CreateDir` | ✓ | — no file bytes | — ⁽¹⁾ | ✓ | ✓ | ✓ |
| `Env::LockFile` / `UnlockFile` | — no durable state | — | — ⁽¹⁾ | ✓ `EAGAIN` (held), `EIO` | — | ✓ |
| `Directory::Sync` | ✓ **returns OK and does not promote directory entries to `durable`** | — a directory entry is all-or-nothing | — ⁽¹⁾ | ✓ | — | ✓ |
| `WritableFile::Append` | — buffered; nothing has reached the device | — nothing on the device to tear | — ⁽¹⁾ | ✓ | ✓ *optional eager-allocation mode; the default charges at `Flush`* | ✓ |
| `WritableFile::Flush` | — `Flush` promises visibility to other openers, not durability | ✓ a kill inside `Flush` leaves a prefix of the flushed extent in `content` | — ⁽¹⁾ | ✓ | ✓ `ENOSPC`, the default charge point | ✓ |
| `WritableFile::Sync` | ✓ **the primary site**; returns OK, `durable` not advanced (characterization-only — §7.5) | ✓ a kill inside `Sync` promotes a **prefix** of the newly covered extent to `durable`; the sole producer of torn tails (B1-D5) | — ⁽¹⁾ | ✓ `EIO`, including report-once-then-clear | ✓ `ENOSPC` surfacing here under delayed allocation | ✓ |
| `WritableFile::Close` | — no promotion of its own | — | — ⁽¹⁾ | ✓ **the dropped-close-error class**: `close(2)` reports `EIO` for writeback that failed after the last `Sync` | ✓ | ✓ |
| `SequentialFile::Read` | — read path | — read path | — a short read at EOF is normal and is not a fault | ✓ | — | ✓ |

⁽¹⁾ **Short writes are not expressible at this seam at all, and the gap is recorded rather than
implied.** `WritableFile::Append` is all-or-nothing by contract, so TestEnv — which never calls
`write(2)` — has nothing to shorten. The loop that must handle a short `write(2)` is real code and
gets its own seam one level down: `PosixWritableFile` takes an injectable raw-write function, and a
unit test drives it with a generator producing 1..n−1 bytes per call, `EINTR`, and a zero-byte return
that must not spin. B2 gets the same treatment for short `pread`.

**The cost of ⁽¹⁾, stated as the ruling requires:** short writes are covered by a *unit* test and are
absent from the kill-point sweep, so **a short write can never combine with a kill point, a quota
exhaustion, or a torn sync in one run.** The alternative — make `Append` short-returning and push the
loop into the engine — would put the fault inside the sweep, and is rejected because it duplicates the
loop at every call site and moves a syscall detail across the exact abstraction line Env exists to
draw. If the combination is ever wanted, the cheap route is a miniature sweep over the injectable
raw-write itself, not a wider engine contract.

Three rows earn a sentence.

**`Directory::Sync` is not decoration.** A WAL created, written and fsynced is still losable if the
directory entry naming it was never made durable: the bytes survive and the name does not. TestEnv
gives directory entries their own `content`/`durable` pair for exactly this, and §7.2's gapless-number
check is what turns the loss into a failed open instead of silence.

**`WritableFile::Sync` hosts two different faults.** Sync loss is a `Sync` that returns success and
promotes nothing — the device lied, the engine is blameless, and §7.5 makes such a run structurally
incapable of being counted as evidence. A torn `Sync` is a kill *inside* the call, which promotes a
prefix; the engine never observes it, and it is the sole producer of the torn tails §5.4 rules on.

**`Close` is a write call.** Treating it as bookkeeping is a known way to lose data (BM7).

### 3.4 What Env deliberately does not have

LevelDB's `Env` carries `NowMicros`, `SleepForMicroseconds`, `Schedule` (a thread pool) and
`NewLogger`. Ours carries none, and each omission is a ruling rather than a simplification.

- **No clock.** Ruling 2 and ruling 5. A wall-clock read is unobtainable by construction, so the C++
  analogue of `clock/real.go`'s one hatched `time.Now()` is **zero** hatched calls.
- **No sleep.** A sleep is a timing dependency in a rig whose entire value is that timing is authored.
- **No thread pool.** Background work scheduled by Env would make kill points unorderable: the sweep
  identifies a point by a call ordinal, and an ordinal is meaningless if an invisible thread draws
  from the same counter. Forward binding for B3: compaction's thread is the engine's, declared and
  joined explicitly, and visible to the sweep as its own ordinal stream.
- **No logger.** Diagnostics go to a caller-supplied sink; the engine does not open files to talk
  about itself.

**`GetChildren` is this language's map range.** Directory order is filesystem-dependent and therefore
nondeterministic; recovery sorts by parsed file number before doing anything else. TestEnv returns
children **reverse-sorted on purpose**, so an engine that forgot to sort fails on the first test rather
than on someone else's filesystem.

---

## 4. B1-D2 — what a kill leaves on disk

Every later contract is stated against this, so it is decided first.

**Candidates.** (a) Process-crash model: the page cache survives, `durable == content` always.
(b) Power-loss model: `durable` advances only when a covering `Sync` returns. (c) Both, selectable.

**Tradeoffs.** (a) is what `kill -9` actually does and is useless to us: under it an unsynced write is
never lost, which makes the frozen contract's entire unsynced window untestable — and it is *green*,
because an engine that never synced at all would pass every (a) test. (b) is strictly more adversarial
and is what the frozen contract already assumes ("buffered writes are readable and losable"); it is
also the honest model for the failure we actually fear, which is not a process dying but a machine
losing power mid-compaction. (c) means every contract in §7 acquires a qualifier, and qualified
contracts are the ones people misremember.

**Recommendation: (b), as the single model the contract is stated against.**

```
per file:   content[]   what a reader sees now
            durable[]   what a kill would leave

Append / Flush   content grows                     durable unchanged
Sync   (clean)   durable = content                 ledger records the covered extent
Sync   (loss)    durable unchanged, returns OK     ledger records "lied"    [characterization only]
Sync   (torn)    durable = content[0 : k)          ledger records k; the call never returns
kill             content = durable; all handles closed; all in-memory engine state abandoned
```

The symmetry with `engine/model` is exact and is what makes the differential comparison well-posed:
the model keeps `durable` plus an ordered `pending` list and reverts on `Crash()`; TestEnv keeps
`durable` plus the unsynced tail of `content` and reverts on kill. Two implementations of one idea,
which is what makes disagreement between them mean something.

**Rejected:** (a) — makes the unsynced window untestable and would pass an engine that never syncs.
(c) — a second model buys fidelity we do not need at the price of a qualifier on every safety sentence.

**The scoping consequence is ratified and lives in §1.1**, and must be in DESIGN-A0 §7 before B1.1.

---

## 5. The write-ahead log

### 5.1 B1-D3 — framing

**Candidates.** (a) Flat length-prefixed records. (b) LevelDB-shaped 32 KiB blocks with record
fragmentation (`FULL`/`FIRST`/`MIDDLE`/`LAST`). (c) (b) plus an explicit sync-group terminator in the
record stream.

**Tradeoffs.** The axis is not space, it is **resynchronization**. Under (a), a corrupt *length field*
makes every later byte unparseable, so recovery cannot tell "the log ends here" from "twenty valid
records follow and I can no longer find them". §5.4's rule depends entirely on telling those apart, so
(a) does not lose a nicety — it makes the torn-tail rule unsafe, because the safe-looking behaviour
(stop at the first bad record) silently discards promised data.

(b) buys the discrimination: damage is bounded to one block and recovery can always advance to the next
block boundary and ask whether anything valid lives there. Cost: 7 bytes per fragment plus up to 7
bytes of block padding.

(c) adds what the contract needs and (b) does not provide: **atomicity of a sync group at recovery**.
Without it, a torn `Sync` leaves recovery landing on whichever *batch* boundary happened to survive, so
the oracle's expected recovery point is "any of the k batch boundaries inside the in-flight group" and
ruling 3's comparison stops being exact. With it the expected set collapses to two known values (§7.4).
**The group marker is what turns a range check into an equality check** — that is its whole
justification.

**Recommendation: (c).**

**Rejected:** (a) — no resync, so the torn-tail rule would be unsafe in exactly the case it exists for.
(b) alone — correct, but leaves the oracle checking a range where ruling 3 asks for an equality. Also
rejected and recorded because it is the obvious alternative to the marker: **a small "durable extent"
file fsynced after each group.** It is correct and encodes the tail/interior boundary directly, and it
doubles the fsyncs on the commit path; a 2× write-latency tax to simplify an oracle's arithmetic is the
wrong trade in a database.

### 5.2 The sync group

A **group** is every batch appended between the start of one `Sync` and the start of the next. It is
the unit of three things at once, deliberately:

- **durability** — a `Sync` covers exactly one group and everything before it;
- **recovery** — a group is committed whole or not at all;
- **promise** — `DurableSeq` advances to a group's high sequence when, and only when, the `Sync`
  covering that group's `GROUP_END` returns success.

Because all three coincide, the set of reachable recovery points equals the set of promised watermarks.
That is the answer to ruling 3, and it is exactness **by construction** rather than by care — the same
move A0.5 made by retaining every intermediate version in the model instead of trying to round the
watermark correctly.

### 5.3 Record layout — the surface to be frozen

Fixed-width **little-endian**, no varints, no reflection, no timestamps. Little-endian rather than the
Go wire codec's big-endian because the WAL is never compared byte-for-byte across implementations — only
engine *state* is — and LE is a memcpy on both targets. A pinned byte-vector test freezes the encoding
regardless, so the choice cannot drift silently.

#### 5.3.1 Physical framing

```
block = 32768 bytes

fragment header = 7 bytes, little-endian
    offset 0    crc32c   u32
    offset 4    length   u16    payload bytes in THIS fragment; always >= 1
    offset 6    type     u8     0 = invalid (reserved)
                                1 = FULL   2 = FIRST   3 = MIDDLE   4 = LAST
```

If fewer than 8 bytes remain in a block, the remainder is **explicitly zero-filled** and the next
fragment starts in the next block. Combined with `length >= 1` and `type 0 = invalid`, a run of zeros —
padding, or a hole past the written extent — can never be mistaken for a record. That is what makes
§5.4's false-positive analysis work, so both reservations are load-bearing rather than tidy.

#### 5.3.2 Where the sequence lives, and what the checksum covers

The three questions asked, answered flatly, because recovery's ability to tell a tail from interior
corruption rests on the third.

| question | answer |
|---|---|
| Where does the sequence number live? | In the **logical record's payload**, at payload offset 1 — `BATCH.seq` and `GROUP_END.high_seq`. **Not** in the fragment header. |
| What does the per-fragment CRC cover? | `length ‖ type ‖ payload` — every byte of the fragment except the four CRC bytes themselves. |
| Does the CRC include the length prefix? | **Yes.** Deliberately, and this diverges from LevelDB, which covers `type ‖ data` only. |

**Why the length is inside the CRC.** With the length outside, a corrupted length field is not itself
detected: recovery reads a wrong-sized payload and the CRC then fails *for the wrong reason*, and the
number of bytes consumed before the failure is a function of corrupt data. The discriminator in §5.4
is "does anything structurally valid follow the failure point", and answering it requires the failure
point to be a known offset rather than one computed from bytes we have just decided not to trust. With
the length covered, a corrupt length is a CRC failure at a known offset, resync starts from the next
block boundary, and the discrimination is sound. The cost is two bytes of CRC input per fragment.

**Why the sequence is in the payload and not in the header.** The header is transport; the sequence is
content, and duplicating it into every `MIDDLE` fragment would cost 8 bytes per fragment to defend
against a fragment chain assembled from two different logical records. That cannot happen here: WAL
files are never recycled (§5.5), so every offset is written exactly once, and §5.4.2's chain-legality
check catches a structurally impossible sequence. The alternative is recorded as part of the recycling
upgrade path — **if recycling ever lands, a per-file nonce in every CRC and a per-record sequence in
every fragment both become required**, and that is the true cost of recycling rather than the file
creation it appears to save.

**A whole-logical-record CRC was considered and rejected as redundant**, by the same argument: with
per-fragment CRCs, chain legality, and no recycling, a chain of individually valid fragments that do
not belong together cannot be constructed. It returns to the table alongside recycling and not before.

#### 5.3.3 Logical records

Three kinds. Kind `0` is reserved-invalid for the same reason type `0` is.

```
FILE_HEADER   kind:u8 = 3   magic:[8]u8 = "RIFTWAL\0"   format_version:u32 = 1   file_number:u64
BATCH         kind:u8 = 1   seq:u64   op_count:u32   ops[op_count]
GROUP_END     kind:u8 = 2   high_seq:u64   batch_count:u32

op            op_kind:u8    0 = SET   1 = DELETE   2 = DELETE_RANGE (reserved; first written in B3)
              key_len:u32   key:bytes
              SET:            value_len:u32   value:bytes
              DELETE_RANGE:   end_len:u32     end:bytes
```

- **`FILE_HEADER` is the first logical record of every WAL**, rather than a raw file header, so block
  arithmetic still starts at offset 0. It catches an empty file, a truncated file, a foreign file, and
  a file whose name and contents disagree — recovery validates `file_number` against the filename.
- **`seq` is the `engine.SeqNum` that `Apply` returned**: one per batch, `+1` per `Apply` including
  empty ones, identical to `engine/model`'s counter. §8.5 argues why the two engines must share a
  sequence space rather than each merely being monotone.
- **`GROUP_END.batch_count`** is the number of `BATCH` records since the previous `GROUP_END`.
  Recovery checks it, which detects a dropped interior record without a whole-group checksum.
- **`DELETE_RANGE` is reserved and never written before B3.** Reserving the byte now is free; a format
  version bump at B3 is not.
- **No timestamps anywhere** — ruling 2. Not in a header, not in a record, not in a filename.
- **Maximum logical record: 64 MiB** — §8.2, where the number is argued and the overflow behaviour
  defined.

### 5.4 B1-D4 — the torn-tail rule

#### 5.4.1 The rule

**The question, restated so the answer is unambiguous.** Recovery reads a fragment and the read fails —
bad CRC, header truncated by EOF, payload truncated by EOF, a length running past its block, or (§5.4.2)
an illegal fragment transition. What does that mean, and what does recovery do?

**Candidates.** (a) Every checksum failure is fatal; the database refuses to open. (b) Every checksum
failure is end-of-log; truncate and open. (c) Position-based without resynchronization: a failure at
what appears to be the last record is a tail, anything earlier is corruption. (d) Resync-verified: a
failure is a tail **only if nothing structurally valid follows it**.

**Tradeoffs.** (a) is unusable — a torn tail is the *normal* outcome of a crash during a write, so (a)
converts the most common real-world event into an outage while buying nothing in the dangerous case.
(b) is the dangerous one and dangerously comfortable: correct whenever the failure really is the tail,
and silently discarding promised data whenever it is not. Silently is the operative word — no log line,
no error, no metric; the database opens, is short some committed writes, and nobody learns for weeks.
(c) sounds like (d) and is not, because "appears to be the last record" is undecidable without resync:
a corrupt length leaves recovery unable to locate the next record, so under (c) every corrupt length is
classified as a tail — which is (b) in exactly the case that matters. **(c) is (b) wearing a better
name**, and that is worth recording as the trap it is.

**Recommendation: (d).** Normatively:

> A recovery read that fails — bad CRC, truncated header, truncated payload, a length running past its
> block, or an illegal fragment transition — **terminates the log at that point**. Groups already
> closed by a `GROUP_END` stand; any `BATCH` records after the last `GROUP_END`, and any incomplete
> logical record, are **discarded**. This is not an error and is not reported as one.
>
> Recovery then **resynchronizes**: it advances to the next block boundary and scans forward for a
> *structurally valid* record — CRC-valid, `type ∈ {FULL, FIRST}`, `kind ∈ {BATCH, GROUP_END}`, and
> carrying a sequence **greater than the last committed group's**. If one is found, the log is
> **corrupt in the interior**: the open **fails**, reporting file, block, byte offset, and the sequence
> of the last committed group. No silent truncation, ever.

**Why the distinction is safe under the exactly-at-watermark contract.** Four steps, with the premises
named, because one of them is where the argument ends and that is the interesting part.

1. *A torn record lies strictly after the last durable `GROUP_END`.* Under B1-D2(b) a file's durable
   image advances only when a `Sync` returns, and by §5.2 a `Sync` covers a whole group ending in its
   `GROUP_END`. A torn record is by definition partially written, so it was in no returned `Sync`'s
   extent.
2. *Therefore discarding the tail never discards a promised byte.* The last durable `GROUP_END` is at
   or after the last promised watermark, and everything discarded lies after it. So `R ≥ W` — the
   safety-critical direction, "committed is forever", holds.
3. *And recovery cannot land above the in-flight group.* Recovery commits only complete groups, so `R`
   is a group boundary, and the highest one that can exist on disk is the group whose `Sync` was in
   flight at the kill. So `R ∈ {W, G_inflight}` — the two-element set §7.4 checks.
4. *So a valid record can follow an invalid one only if a premise failed.* A single append-only file is
   written in offset order and durability under B1-D2(b) is prefix-closed, so a crash cannot produce a
   valid record after a torn one. Media corruption can, and a device that reordered across an fsync
   can. Both falsify step 1 — and step 1 is what makes truncation safe. **When the premise fails,
   truncation is no longer safe, so recovery must not truncate.** That is the whole argument for (d)
   over (b), and it is why the response is a hard error rather than a best effort.

**The false-positive analysis, because (d)'s cost is spurious hard errors.** Resync must not mistake
garbage for a valid record and turn a normal torn tail into a refused open — an availability bug
manufactured by a safety rule. Four things make that essentially impossible: WAL files are never
recycled, so bytes past the written extent are zeros or absent; type `0` is reserved-invalid, so a
zero-filled header is rejected before its CRC is considered; a candidate must be CRC-valid *and* `FULL`
or `FIRST` at a block boundary; and it must carry a sequence above the last committed group's. A `2⁻³²`
accidental CRC match on non-zero garbage remains possible in principle, and its direction is right: the
failure mode is a refused open with a byte offset, which a human investigates, rather than a successful
open missing data, which nobody does.

**Rejected:** (a) — every unclean shutdown becomes an outage. (b) — silently discards promised data in
the interior-corruption case, in the dangerous direction, undetectably. (c) — undecidable without
resync, therefore identical to (b) exactly when it matters.

#### 5.4.2 The multi-block case, which is the same rule and not a second one

`DeleteRange(nil, nil)` produces a record proportional to the keyspace (§8), so multi-block logical
records are a routine path in B1, not an exotic one. The rule above must cover them without acquiring a
special case, and it does, once "structurally valid" is understood to include chain legality.

**The chain is a two-state machine, and its transitions are part of the frozen format:**

```
OUTSIDE  --FULL-->   OUTSIDE     (a complete single-fragment record)
OUTSIDE  --FIRST-->  INSIDE
INSIDE   --MIDDLE--> INSIDE
INSIDE   --LAST-->   OUTSIDE     (a complete multi-fragment record)

every other transition is ILLEGAL:
   OUTSIDE --MIDDLE-->  |  OUTSIDE --LAST-->  |  INSIDE --FULL-->  |  INSIDE --FIRST-->
```

An illegal transition is a read failure of the same kind as a bad CRC, and feeds the same rule. Working
through the cases a torn multi-block record can produce:

| what the kill left | what recovery sees | classification | why it is right |
|---|---|---|---|
| `FIRST, MIDDLE`, then EOF | valid chain, `INSIDE` at EOF | **torn tail** — discard the incomplete record | prefix truncation; nothing can follow, and by step 1 the whole record is past the last durable `GROUP_END` |
| `FIRST, MIDDLE, <torn MIDDLE>` | CRC failure while `INSIDE` | **torn tail** | identical to the single-fragment torn case; the failure is at a known offset because the length is inside the CRC (§5.3.2) |
| `FIRST`, then a block of zeros, then EOF | type `0` at the next fragment | **torn tail** | zeros are unambiguous by §5.3.1's two reservations |
| `FIRST`, garbage block, then a **valid `FULL` with a higher sequence** | CRC failure while `INSIDE`, then resync finds a structurally valid record | **interior corruption — open fails** | cannot arise from prefix truncation; step 1's premise is false, so truncation is unsafe |
| `FIRST` immediately followed by another `FIRST`, both CRC-valid | illegal transition `INSIDE --FIRST-->` | **interior corruption — open fails** | no crash produces it; it is a writer bug or corruption that landed on a fragment boundary |
| a bare `MIDDLE` or `LAST` found during resync | illegal start | **not a resync candidate** | which is why the resync predicate requires `FULL` or `FIRST`; accepting a bare `MIDDLE` would let garbage masquerade as interior corruption and manufacture a refused open |

The discriminator is therefore **not** "did a checksum fail" — it is "does anything structurally valid
follow the failure point", where structural validity for a multi-fragment record includes the chain. A
torn multi-block record at the tail is distinguishable from interior corruption by exactly the test that
distinguishes a torn single-block one, and BM11 exists to prove the chain half of it is actually
checked.

### 5.5 B1-D5 — torn-`Sync` granularity, and no recycling

**Torn-sync granularity.** Candidates: (a) **prefix** — a kill inside `Sync` promotes `content[0:k)`;
(b) **sector-subset** — an arbitrary set of 4 KiB sectors of the newly covered extent is promoted.

**Recommendation: (a) as the contract model, (b) available and labelled characterization-only.** (b)
can promote a `GROUP_END` while leaving an earlier record in the same group torn, which is a device
that violated fsync's own ordering guarantee. Against such a device the engine cannot be held to
exactness, and holding it there anyway would report the engine for the disk's crime. Under (b) the
engine's obligation is narrower and still real — **detect and refuse**, which §5.4(d) already does:
valid terminator, broken interior, hard error. So (b) is a *detection* test, not an *exactness* test,
and §7.5 makes it structurally uncountable as evidence for the recovery contract, exactly as the lying
`Sync` is.

**WAL files are never recycled in B1.** Recycling (RocksDB's `recycle_log_file_num`) saves a file
creation and a directory fsync per rotation and pays by leaving stale, CRC-valid records past the tail —
which breaks §5.4's false-positive analysis outright and forces a per-file nonce into every CRC plus a
per-fragment sequence (§5.3.2). Recorded as a deliberate non-goal with its full cost, its upgrade path,
and the condition that would earn it reconsideration: a *measured* rotation-rate problem at I2, not
before.

---

## 6. The memtable

### 6.1 B1-D6a — structure

CLAUDE.md specifies a skiplist, so the structure is not open; arena-allocated, `kMaxHeight = 12`,
LevelDB's shape. Nodes and key bytes come from a bump allocator and the whole arena dies with the
memtable: exact memory accounting, which B2's flush threshold needs and a general allocator cannot
provide cheaply, and no per-node free path to get wrong under a kill. **Nothing may depend on an
address** — no pointer-keyed containers, no address-ordered anything — which is the C++ restatement of
the map-iteration rule and is checked by §9.4's scan.

### 6.2 B1-D6b — the height source, and the comparator

**Candidates.** (a) A PRNG, as LevelDB does (`rnd_.OneIn(kBranching)`). (b) Derived from the key:
`height = 1 + min(ntz(fnv1a64(key)) / 2, kMaxHeight − 1)`. (c) Derived from an insertion ordinal.

**Recommendation: (b).** This is DR-12's ruling one language over: `engine/model`'s treap priorities
come from `fnv1a64(key)` and not from RNG, so engine internals stay decoupled from any random stream.
Ruling 5 makes it mandatory here — a PRNG is ambient randomness — and (b) buys more than determinism:
the same key always gets the same height, so the structure is a pure function of the key *set*, and a
shape-dependent bug reproduces from the workload alone. Hash bits are uniform, so the height
distribution matches (a)'s in expectation; an adversarial key set could skew it, and that skew would be
deterministic, which is the only property we need from it.

**Rejected:** (a) — banned by ruling 5, and makes any shape-dependent bug irreproducible. (c) —
reproducible only under identical insertion order, which is the case we least need.

**Internal keys.**

```
internal_key = user_key ‖ ((seq << 8) | value_type)   as u64 little-endian
```

Ordering: user key ascending by `memcmp`, then `seq` **descending**, so the newest version sorts first
and a snapshot read is one seek. Multiple versions per key are required — `NewSnapshot` pins a sequence
and a read through it must skip newer versions — so the memtable is append-only and never overwrites.

**The comparator is bytewise and is not pluggable in v1.** A pluggable comparator is the door through
which the storage engine learns what a key *means*, and A5 puts MVCC timestamps inside keys. Ruling 2
says the engine never interprets time; a fixed bytewise comparator makes that uncompilable rather than
remembered. The cost is named: B3 cannot implement a timestamp-aware compaction filter, and does not
need to, because version GC is A5's job on the Go side.

### 6.3 B1-D6c — concurrency. **RULED: a lock.**

**Ruled by Ansh, 2026-08-12.** The memtable is protected by the DB mutex. Recorded with its rejected
alternative because the rejection is the substance.

**The concurrency contract B1 must meet**, since it is what made the question look open: the frozen
interface has `Apply` running on the node loop while a separate thread owns the blocking `Sync` (DR-11).
So the engine **is** called from two threads and must be internally synchronized. What does *not*
follow, and what I wrongly treated as following, is that the memtable needs to be lock-free.

**Rejected: a lock-free single-writer/multi-reader skiplist** (LevelDB's, with release-store/acquire-load
on next pointers). Amendment A6 governs and I applied it to compaction policy while missing it here:
the simplest correct thing wins v1 and the faster thing is a recorded upgrade path. B1 has no authorized
concurrency requirement; `Apply` is non-blocking **by contract, not by parallelism** — §8.3's invariant
is that it makes no Env call, which a mutex does not threaten — and the syncer and poller are B5's. A
lock-free structure spends this project's scarcest resource, C++ correctness under fault injection, to
buy throughput no measurement has asked for, and its failure mode is the one the project exists to
eliminate: a bug that appears on one machine, at one core count, one run in ten thousand, and does not
replay.

**The measurement that would reopen it**, recorded so the upgrade path is a threshold rather than a
mood: **B5's standalone numbers showing the memtable mutex is the bottleneck** — specifically, a
`readrandom` mix whose throughput scales sublinearly with reader threads while the same workload against
`engine/model` does not, with lock contention attributed by profile rather than inferred. Absent that
number, the lock stays.

**TSan is required regardless**, because a locked structure with a wrong lock is still a race, and
because the property that actually needs proving is not inside the skiplist — it is §8.3's
buffer-swap: the DB mutex must never be held across an Env call, and the syncer must not touch memtable
state. B1's engine is single-threaded until somebody calls it from two threads, so **the TSan lane runs
a dedicated multi-threaded harness test** — `Apply`/`Get` on one thread, `Sync` on another, for a fixed
op count — rather than the ordinary unit suite. A TSan lane over single-threaded tests is a green lane
that proves nothing, and BM14 exists to prove this one is not that.

**Unbounded growth in B1.** No flush until B2, so the memtable grows without bound and old WALs are
never deleted. B1's tests are sized accordingly, and the constraint is recorded because it is also what
makes §7.2's gapless-file-number check sound.

---

## 7. The recovery contract

### 7.1 Mapping the frozen Go interface onto the C++ engine

| `engine.Engine` (frozen, A0.5) | C++ engine | who bridges |
|---|---|---|
| `Apply(b, sync) (SeqNum, error)` — never blocks on I/O | `Write(batch) -> (seq, Status)`; appends to the memtable and the engine-owned WAL buffer; **makes no Env call** (§8.3) | direct |
| `DurableSeq() SeqNum` | `DurableSeq()` — advances only when a `Sync` returns | direct |
| `OnDurable(func(SeqNum))` | **absent by design** — no C→Go callbacks (DR-11) | the Go wrapper's per-engine poller owns the blocking `Sync()` and posts to the node mailbox |
| — | `Sync() -> (seq, Status)` — blocking; covers everything appended so far | B5 |
| `Get`, `NewIter`, `NewSnapshot`, `ApproximateDiskBytes`, `Close` | same shapes, `Status` instead of `error` | B5 |

The `sync` flag's *policy* — how eagerly the poller wakes — is a B5 decision about the pair, not a B1
decision about the engine. B1 guarantees only that `Sync()` covers everything appended before it and
returns the watermark it established.

**`Close` does not sync**, deliberately. The watermark is the engine's only durability promise; a
`Close` that synced would make clean shutdown a hidden durability event that `engine/model`'s `Close`
does not have, and the two engines would then disagree in precisely the differential rig. The
consequence is a good test: **close-then-reopen must be indistinguishable from kill-then-reopen.**

### 7.2 Open

1. Acquire `LOCK`.
2. `GetChildren`, parse `NNNNNN.log`, **sort by parsed number** — never directory order, never mtime
   (ruling 2).
3. Assert numbering is **gapless**. In B1 no file is ever deleted, so a gap means a lost directory
   entry — the missing-`Directory::Sync` bug — and it is a hard error. This is what gives the
   directory-sync kill point teeth; without it the loss is silent.
4. Replay each file in order into a fresh memtable, committing group by group (§5.4).
5. `recovered_seq` = the highest committed `GROUP_END.high_seq`. Assert monotone across files.
6. Create WAL `max+1` and **`Directory::Sync` before `Open` returns**.
7. `DurableSeq = VisibleSeq = recovered_seq`.

**B1-D7 — no manifest in B1.** Candidates: (a) none, file numbers from `max existing + 1`; (b) a
minimal manifest recording the WAL number and the durable sequence; (c) build B2's MANIFEST early.
**Recommendation: (a).** There are no SSTables, so no version state to be inconsistent with, and a
manifest recording a durable sequence would be a **second authority on the watermark that could
disagree with the log** — the exact shape of the A0.5 bug, rebuilt in C++. The single source of truth
is the log: `recovered_seq` is a fact about bytes, derived, never stored. Forward binding to B2,
recorded so it cannot be forgotten: **the manifest may record which files exist; it may never record a
durable sequence the WAL cannot independently justify**, and `max+1` numbering stops being safe the
moment B2 deletes a flushed WAL, which is where the file-number counter moves into the manifest.
**Rejected:** (b) — a second authority on the watermark. (c) — B2 scope.

### 7.3 The oracle, written so it never asks the engine anything

The rig's inputs are **its own call log** — every `Write`, every `Sync`, in issue order, with return
values — and **TestEnv's fault ledger** — for each `Sync`, whether durability was applied fully, not at
all, or as a prefix. Both are harness-side. The engine's on-disk state is never parsed by the oracle,
and the engine is never asked what it believes it holds.

From the call log alone the rig knows the group decomposition: group *k* is the set of `Write`s between
the start of `Sync` *k−1* and the start of `Sync` *k*, with high sequence `G_k`. **No byte-level parsing
is required for this**, which is the point — an oracle that parsed the WAL would be a second
implementation of the reader, and a second implementation can be wrong in the same direction as the
first.

Let `W` be the highest watermark the engine ever *returned* to the rig before the kill. The oracle
asserts two things:

- **(i) Exactness.** `recovered_state == model_state_at(R)`, byte for byte over a full iteration, where
  `R` is the group boundary TestEnv's ledger justifies (§7.4).
- **(ii) No over-promise.** `W ≤ R`. An engine that advanced its watermark before the data was durable
  fails here, and this is the assertion the whole rig exists for.

Both directions are covered and neither consults the engine's opinion. Over-reporting fails (ii);
under-reporting fails (i), because the ledger justifies more than the promise did. The `Sync` return
value appears only in (ii), as *the promise being held to* — the "client-observed response" A0's oracle
rule explicitly permits — never as the answer being checked.

### 7.4 The two-element set, and the three conditions that keep it from being an escape hatch

`R = G_k` when `Sync` *k* was applied fully; `R ∈ {G_{k−1}, G_k}` when `Sync` *k* was in flight or torn
at the kill. A `Sync` can complete on the device with the kill preempting its return: the bytes are
durable, the caller never learned it. No design removes that — it is "did the RPC commit?", one layer
down — and ruling 3's "**any watermark the sync-completion schedule can produce**" is what covers it,
confirmed 2026-08-12.

Three conditions, ruled, each with the mechanism that enforces it:

1. **The set is derived from the harness's own record of what it issued** — its `Write`/`Sync` call log
   and TestEnv's ledger — **never from the engine and never from the manifest** (ruling 4). Mechanism:
   the oracle is compiled against a header that does not include the engine's internal state at all; its
   only engine-facing input is the iterator it compares, and the `Sync` return it holds the engine to.
   §7.2's B1-D7 removes the manifest as a possible source by not having one.
2. **Each element is compared exactly, and the verdict names which element matched.** Mechanism: the
   verdict is `{matched: G_{k−1} | G_k, seq: <n>, compared: <key count>}`, not a boolean. A verdict that
   cannot say which element it matched is treated as a failure of the oracle, not a pass of the engine.
3. **Both elements are individually induced by tests**, because *a two-element set where only one
   element has ever been observed is a one-element contract with a spare excuse attached.* Mechanism:
   two named tests in §10, `RecoveryLandsOnPreviousGroupWhenSyncIsTorn` (kill inside `Sync`, durability
   not applied) and `RecoveryLandsOnInFlightGroupWhenSyncCompletesButIsPreempted` (durability applied,
   kill before the return), plus a sweep-level assertion that **across the full kill-point sweep, both
   elements were observed at least once** — so the pair cannot silently degenerate into one as the code
   moves. BM15 blinds the set-width check.

### 7.5 `RunOutcome`: the mechanical suppression

Ruled 2026-08-12: a run with the lying-`Sync` injector enabled may never be reported as evidence for the
recovery contract, in any column, ledger, or README sentence, and the suppression must be mechanical.
This is A0.6's `Outcome` enum in a second setting, and the same reasoning about closed enums applies.

```cpp
enum class RunOutcome {          // closed; no default arm anywhere, enforced by -Werror=switch
  kContractPass,                 // (i) and (ii) both asserted, both held
  kContractViolation,            // (i) or (ii) failed -- a bug
  kCharacterizationOnly,         // (ii) SUSPENDED by an authored device lie; NOT evidence
  kInconclusive,                 // the checks did not complete
  kTripwire,                     // a bounded resource hit its cap; the run is void and names the cap
};

bool CountsAsRecoveryEvidence(RunOutcome);   // the ONLY place this policy lives
```

| kind | when | counts as recovery-contract evidence? |
|---|---|---|
| `kContractPass` | (i) and (ii) asserted and held | **yes — only this one** |
| `kContractViolation` | either assertion failed | no; it is a bug with a kill point |
| `kCharacterizationOnly` | lying `Sync` (§3.3) or sector-subset torn `Sync` (§5.5) enabled: **(ii) was suspended, so the contract was not under test** | no; data about behaviour |
| `kInconclusive` | a check did not complete | no — Amendment A4's shape, one language over |
| `kTripwire` | the WAL-buffer or record-size cap fired (§8.2, §8.3) | no; the run is void |

Three things make the suppression mechanical rather than remembered. `CountsAsRecoveryEvidence` is the
single place the policy lives, so adding a kind forces a decision *there* rather than defaulting to
"sure, count it" at whichever summarizer forgot — Amendment A0's "policy lives in exactly one method on
the type it belongs to". `-Werror=switch` over a scoped enum with **no `default:` arm** is the C++
compiler already implementing A0.6's `exhaustive` rule for free, and §9.4's scan bans `default:` arms
over `RunOutcome` so nobody buys the omission back. And the ledger's column is literally headed
**`characterization (not evidence)`**, which cannot be misread as a pass by someone skimming. BM13
blinds the policy method and must be caught by the ledger test.

---

## 8. `DeleteRange` through B2: expansion, its cost, and the caps

### 8.1 The expansion happens at `Apply` and the WAL records the expansion

Iterate-and-point-delete must read current state to find the keys to delete, and `Apply` is what makes
the deletion visible — so the expansion happens at `Apply`. What goes in the log is the consequential
part.

**If the WAL recorded the raw `DeleteRange`, recovery would have to expand it again — against a state
recovery is still in the middle of rebuilding.** The expansion is a function of the state at the time it
runs, so replay-time expansion is correct only if that state provably equals the state at original
`Apply` time. It probably does today, for a reason that depends on the WAL's start point coinciding
exactly with the flush boundary — a property B2 is about to start changing. That is correctness by
argument, and the argument has a moving premise.

**Recording the post-expansion op list makes it correctness by construction.** Recovery replays point
deletes; there is nothing left to compute; the circularity is gone. Approved 2026-08-12.

Intra-batch semantics come out right: at the `DeleteRange` op, the expansion covers the current state
*and* keys written earlier in the same batch, and a `Set` after it in the same batch re-adds the key,
which is the model's rule reproduced.

### 8.2 B1-D8 — the record-size cap, and what happens when an expansion exceeds it

`DeleteRange(nil, nil)` — the clear half of snapshot application's clear-then-ingest, the case Amendment
A3 was ruled for — expands to one point delete per live key, in a single record, and batches are atomic
so it cannot be chunked.

**Maximum logical record: 64 MiB (67,108,864 bytes), configurable, default as stated.** The arithmetic
so the number is a judgement rather than a magic constant: a point delete costs `op_kind(1) +
key_len(4) + key`, so 55 bytes at a 50-byte key, giving **≈1.22 M keys per maximal record**. That is
2048 blocks and 2048 fragments, which is why §5.4.2 treats multi-block records as a routine path.

**Exceeding it is `Status::kRecordTooLarge`: the batch applies nothing, atomically, and the rig records
`RunOutcome::kTripwire` naming the cap.** It is a tripwire on the same reasoning as Q5's buffer cap —
it exists so that a pathological expansion produces a named halt with a plan reference instead of an
out-of-memory kill that destroys the run which would have explained it.

Two consequences to state rather than discover:

- **B4's differential rig must treat a tripwire as a void run, not as a divergence.** `engine/model` has
  no cap, so it will accept a batch the C++ engine refuses. That is the harness hitting its own bound,
  not the engines disagreeing, and `kTripwire` failing `CountsAsRecoveryEvidence` is what keeps it out
  of the evidence column.
- **A tripwire that fires on legal input is worse than no tripwire**, which is the same inversion §5.4
  rejected candidate (a) for. §8.3's ordering invariant is what prevents it here.

### 8.3 B1-D9 — the WAL buffer: ownership, the cap, and the assertions

**Ownership.** LevelDB's `WritableFile::Append` flushes to the OS when its internal buffer fills, so a
write can perform I/O at an unpredictable moment; "unpredictable moment" is not a way to satisfy "never
blocks on I/O". **The WAL buffer is therefore the engine's own memory.** `Apply` appends to it and makes
zero Env calls. The syncer takes the DB mutex only long enough to swap in a fresh buffer, then performs
`Append` + `Sync` on the old one with the mutex released.

**Two assertions, not one sentence** (finding 5, approved, plus the one the lock ruling makes necessary).
TestEnv keeps a per-thread Env-call counter, and:

1. **The counter does not move across `Apply`.** BM9 blinds it.
2. **The DB mutex is never held across an Env call.** This is what makes B1-D6c's lock safe under a slow
   `Sync`: without it, a 10 ms fsync would block every reader for 10 ms and the lock ruling would have
   bought a latency bug. Mechanism: a debug-build guard object that records mutex depth on the current
   thread and is checked at the top of `Intercept`.

Same move as putting persist-before-reply inside `raft/` — an invariant that fails a test is worth more
than a sentence in a design doc.

**The cap.** `Apply` returning while the syncer falls behind means the buffer grows; unbounded growth in
a fault-injected harness means an OOM kill, which is the worst possible failure signal because it
destroys the run that would have explained it. **Default cap 256 MiB; exceeding it is
`Status::kWalBufferFull` and `RunOutcome::kTripwire`, halting loudly and naming the cap and the plan.**

**The ordering invariant, asserted at construction:** `wal_buffer_cap ≥ 2 × max_record_size`. A cap below
the maximum legal record would make the tripwire fire on legal input, which is the inversion §8.2 just
named. The default pair, 256 MiB and 64 MiB, satisfies it with margin.

The cap is a tripwire, not a policy. `Status::Busy` as the *policy* remains the leaning for B5 and is
not ruled now; the three candidates are recorded there so B5 inherits them rather than rediscovering
them.

### 8.4 The scheduled end of this cost

Ruling 1's real range tombstones in B3 retire all of it: the record becomes O(1) in the range rather
than O(keys), the multi-block path stops being routine, and the caps stop being reachable by a legal
`DeleteRange`. **Recording that here makes the fragmentation path a known-temporary consequence with a
scheduled end rather than a permanent property of the format** — and it is why `DELETE_RANGE` is a
reserved op kind in §5.3.3 from day one, so B3 writes a tombstone without a format version bump.

What does *not* retire: §5.4.2's chain rule and the fragmentation code itself, since a large batch can
still exceed a block. They become a rare path instead of a routine one, which is an argument for keeping
them exercised by a dedicated test after B3 rather than relying on `DeleteRange` to exercise them.

### 8.5 B1-D10 — one sequence per batch, collapsed, sharing the model's sequence space

**Candidates.** (a) Collapse the batch to at most one op per key before insertion; one internal sequence
per batch, equal to `engine.SeqNum`. (b) LevelDB's scheme: the internal sequence advances per *op* and
`engine.SeqNum` is the batch's last internal sequence. (c) Pack `(batch_seq, op_index)` into the internal
key.

**Recommendation: (a).** Under (b) the C++ engine's sequences jump (1, 5, 9, …) while `engine/model`'s
advance by one per `Apply`. That is contract-legal — the frozen interface requires only monotonicity —
and still wrong, because B4's rig would then need a per-engine map from operation index to sequence in
order to sync both engines "to the same point", and a rig that needs a translation table is a rig with a
place to be wrong. (c) keeps the spaces aligned but widens every internal key for a case (a) removes.

(a) costs a sort of the batch's ops by key — which §8.1's expansion already requires a pass over — and
it makes an invariant assertable: **no two memtable entries ever share a `(user_key, seq)` pair.**

**Rejected:** (b) — divergent sequence spaces put a translation table inside B4's oracle. (c) — wider
internal keys to preserve a distinction (a) removes.

---

## 9. Build, toolchain, lanes, and the half of A5 the Env seam cannot see

### 9.1 Toolchain — ruled

C++17. `-fno-exceptions`, `-fno-rtti`, `-Wall -Wextra -Werror`, and `-Werror=switch` (which is §7.5's
exhaustiveness rule, already in the compiler). `Status` return codes throughout. clang and gcc both
pinned in CI, for the same reason DR-26 pinned the Go toolchain: a version should be a decision, not an
accident of what is installed.

`-fno-exceptions` is load-bearing rather than stylistic: **no exception may cross into Go, ever**, and
the flag makes that structural instead of a review habit. It also rules out the obvious in-process kill
mechanism, which §9.5 addresses.

### 9.2 GoogleTest — vendored, ruled

FetchContent declined: a build step that reaches the network fails in exactly the situation where
"every number reproduces from a clean clone by one script" matters most, which is a stranger checking
our work.

**Vendored under `third_party/googletest/`, pinned to `v1.17.0`, commit
`52eb8108c5bdec04579160ae17225d66034bd723`.** The hash was read from the upstream remote rather than
recalled. `third_party/googletest/VERSION` records the tag, the commit, and the date of vendoring, and
CMake resolves GoogleTest from that path only — **no `FetchContent`, no `find_package` fallback, no
network call in any lane.** A lane asserts the vendored tree's tracked content hashes to a value pinned
in `VERSION`, so a local edit to a vendored dependency is a test failure rather than a mystery.

### 9.3 Lanes

`make cpp-test`, `make cpp-asan`, `make cpp-ubsan` un-stub with B1, and **`make cpp-tsan` is added as
required**. Per §6.3, the TSan lane runs a dedicated multi-threaded harness test rather than the unit
suite, because a TSan lane over single-threaded tests is a green lane that proves nothing.

MSan remains declined: it needs an instrumented libc++, and its value here — uninitialized bytes reaching
the disk — is already covered by §10's byte-digest gate at a fraction of the cost.

Platform matrix: **Linux for every lane** (best sanitizer support), **plus a macOS `cpp-test` lane**.
The macOS lane is not convenience. It is **our first cross-platform evidence for the Env seam** — the
first time `PosixEnv` runs against a kernel whose `fsync`, `rename` and directory semantics differ from
the one it was written on — in the same spirit as the cross-architecture datapoint Track A is waiting on
CI for. It also means Track B builds on the development machine, and a track that only builds in CI is a
track nobody runs locally.

Editing the shared `Makefile` and the CI workflows is Track A's file; §12 records it as coordination.

### 9.4 B1-D11 — enforcing the non-syscall half of ruling 5

The Env seam cannot see a `double`, a `rand()`, a `steady_clock::now()`, or a raw `::open` that bypassed
it. Something else has to.

**Candidates.** (a) A source-scan lane with a checked-in exception registry. (b) clang-tidy with custom
matchers. (c) The Env seam plus review.

**Recommendation: (a) now, (b) as an upgrade if the scan gets noisy.** A scan over `engine-cpp/src`
banning `<random>`, `rand(`, `<chrono>`, `time(`, `clock(`, `float`, `double`, `getenv`, `<fstream>`,
`default:` over `RunOutcome`, and direct `open(`/`write(`/`fsync(`/`rename(` outside `env/posix/`, with a
`CPP-HATCHES.txt` registry diffed against the tree by the lane — `HATCHES.txt`'s structure one language
over, **including the rule that an unused entry fails**, because a drifted hatch means something is
unguarded while its author believes otherwise. Banning direct syscalls outside `env/posix/` is the
mechanical form of "every syscall through Env"; without it, the seam is enforced by nobody.

**Rejected:** (c) — DR-16's argument verbatim: the answer to "how do you know a `steady_clock::now()`
didn't sneak in?" must be a build failure, not a promise. (b) as the first step — a real clang-tidy
check is a day of work and a toolchain dependency for a job a grep does today; it earns its place when
the registry starts carrying arguments a grep cannot express.

### 9.5 B1-D12 — how a kill point kills, and how it is identified

**The mechanism.** Candidates: (a) real `_exit(0)` inside the Env call, re-running the workload per kill
point; (b) in-process, via a dead flag; (c) both, sampled.

(a) is maximally faithful — no destructor runs, no heap survives — and costs a full workload re-run per
point, making a complete sweep unaffordable. (b) sweeps thousands of points per second and has one
specific blind spot: the engine keeps running, so a bug in which "recovery" reads live memory instead of
disk could be masked.

The mechanism for (b) matters and `-fno-exceptions` rules out the obvious one. `throw` is unavailable,
and would be wrong regardless: unwinding runs destructors, and a destructor that flushes would write
after the crash. Instead: **the fault controller sets a dead flag; every subsequent Env call is a no-op
returning `Status::kKilled`, and TestEnv freezes its durable image.** Code that ignores the `Status` can
still only touch a frozen Env, so it cannot affect what recovery reads — the only dimension a crash has.
The rig then destroys the DB object and reconstructs from a **fresh** TestEnv seeded only from the frozen
durable image, so a stale pointer faults under ASan rather than silently working. A cap on post-kill Env
calls stops a runaway loop.

**Recommendation: (c)** — (b) for the sweep, (a) for a stated sample (proposal: every 32nd point, plus
every point that has ever produced a failure), so the blind spot is measured rather than assumed.

**Rejected:** (a) alone — a complete sweep becomes unaffordable, and an incomplete sweep is the thing B4
exists not to be. (b) alone — its blind spot is exactly "recovery accidentally reads live memory".

**The identity.** Candidates: (a) a global Env-call ordinal swept 1..N; (b) named points declared in
engine code; (c) both. **Recommendation: (c), with (a) load-bearing and (b) as a label.** The ordinal is
complete by construction — nothing to annotate, therefore nothing to forget — and a static label at each
call site turns "kill 47 failed" into "kill 47 = `Sync(000001.log)`, group 12, after 3 appends", which is
a bug report. Plus a **kill-point census**: the sweep records how many points it visited, per call kind,
and a change in the census is surfaced. A new Env call nobody swept is otherwise invisible; this is
A0.6's step census in a second setting.

---

## 10. How B1 proves itself

### 10.1 Mutant catalogue

Per Amendment A2, stored as patches applied to a scratch tree (DR-27) — and not only for consistency:
BM6 includes `<random>` and BM14 removes a lock, both of which §9.4's scan lane rejects, so they cannot
exist as committed files for the same reason M4 and M5 cannot.

Budgets are in **kill points**, the C++ analogue of seeds-to-detection; wall-time-to-detection is
recorded alongside, per A2. A mutant surviving its budget means the rig is too weak and B1 is not done,
regardless of what the clean runs say.

| mutant | injected bug | must be caught by | budget |
|---|---|---|---|
| `BM1-ack-before-sync` | advance the watermark before `Sync` returns | exactness (ii) | ≤ 5 kill points |
| `BM2-accept-torn-tail` | commit `BATCH` records with no `GROUP_END` | exactness (i) | ≤ 20 |
| `BM3-silent-interior-truncate` | stop at the first bad record; never resync | corruption test + exactness (i) | immediate |
| `BM4-missing-dir-sync` | skip `Directory::Sync` after creating a WAL | gapless numbering | ≤ 50 |
| `BM5-swallow-sync-error` | treat `Sync`'s `EIO` as success | exactness (ii) | ≤ 20 |
| `BM6-prng-heights` | PRNG skiplist heights | scan lane (compile) + structural digest | immediate |
| `BM7-drop-close-error` | ignore `Close`'s error return | exactness (ii) | ≤ 100 |
| `BM8-skip-crc` | do not verify fragment CRCs at recovery | corruption test | immediate |
| `BM9-apply-does-io` | flush inside `Apply` | Env-call counter assertion | immediate |
| `BM10-crc-excludes-length` | compute the CRC over `type ‖ payload` only, as LevelDB does | corrupt-length test | immediate |
| `BM11-accept-illegal-chain` | accept `FIRST→FIRST` and bare `MIDDLE` during resync | fragment-chain test | immediate |
| `BM12-no-tripwire` | remove the WAL-buffer cap | tripwire test — must halt, not OOM | immediate |
| `BM13-characterization-counted` | make `CountsAsRecoveryEvidence` return true for `kCharacterizationOnly` | ledger test | immediate |
| `BM14-drop-the-lock` | write the memtable without holding the DB mutex | **TSan lane** | ≤ 3 runs |
| `BM15-widen-the-set` | let the recovery oracle accept any batch boundary inside the in-flight group | exactness (i) on a multi-batch group | ≤ 10 |
| `BM16-mutex-across-env` | hold the DB mutex across `Sync` | mutex-depth guard | immediate |

### 10.2 Gates

Per the standing protocol, **every gate is landed only once its failure has been induced and observed**,
and the induced failure is what the entry records. A gate that has only ever been green has demonstrated
the cheap half. The last column is the mutant class that catches a *regression* in the gate, so the
catalogue is specified from the start rather than retrofitted.

| gate | what proves it can fail | regression caught by |
|---|---|---|
| recovery exactness (§7.3 i) | make recovery accept records past the last `GROUP_END` | `BM2`, `BM15` |
| no over-promise (§7.3 ii) | advance the watermark before `Sync` returns | `BM1`, `BM5`, `BM7` |
| **lands on `G_{k−1}`** (§7.4 cond. 3) | `RecoveryLandsOnPreviousGroupWhenSyncIsTorn` — kill inside `Sync`, durability not applied | `BM2` |
| **lands on `G_k`** (§7.4 cond. 3) | `RecoveryLandsOnInFlightGroupWhenSyncCompletesButIsPreempted` — durability applied, kill before the return | `BM15` |
| both elements observed in the sweep | run the sweep with the in-flight case suppressed; the assertion must fire | `BM15` |
| the verdict names its element (§7.4 cond. 2) | return a boolean verdict; the oracle's own test must reject it | `BM15` |
| torn-tail rule, single block (§5.4.1) | make recovery accept `BATCH` records after the last `GROUP_END` | `BM2` |
| torn-tail rule, multi-block (§5.4.2) | truncate mid-`MIDDLE` and assert the tail is discarded, then plant a valid `FULL` after the gap and assert the open fails | `BM11`, `BM3` |
| illegal fragment transitions | plant `FIRST` immediately followed by `FIRST`, both CRC-valid | `BM11` |
| CRC covers the length (§5.3.2) | corrupt only the length field of a fully synced fragment; the CRC must fail at a known offset | `BM10` |
| interior-corruption detection | flip one byte inside a fully synced group; the open must fail with an offset | `BM8`, `BM3` |
| interior corruption is not truncated | make recovery stop at the first bad record; the planted corruption must go from "refused open" to "silent data loss" | `BM3` |
| gapless numbering (§7.2 step 3) | delete a WAL file; the open must fail | `BM4` |
| directory sync | kill between file creation and `Directory::Sync`; the gapless check must fire | `BM4` |
| `Apply` performs no I/O (§8.3) | move the WAL buffer into `WritableFile`; the per-thread counter assertion must fire | `BM9` |
| mutex never held across an Env call (§8.3) | hold the DB mutex across `Sync`; the depth guard must fire | `BM16` |
| memtable is actually locked (§6.3) | remove the mutex from the write path; the TSan harness must report a race | `BM14` |
| WAL-buffer tripwire (§8.3) | stall the syncer past the cap; the run must halt as `kTripwire`, not OOM | `BM12` |
| record-size tripwire (§8.2) | `DeleteRange(nil, nil)` over a keyspace exceeding 64 MiB of expansion | `BM12` |
| cap ordering invariant (§8.3) | construct with `wal_buffer_cap < 2 × max_record_size`; construction must fail | `BM12` |
| characterization is not evidence (§7.5) | make `CountsAsRecoveryEvidence` accept `kCharacterizationOnly`; the ledger test must fire | `BM13` |
| deterministic on-disk bytes | leave one padding byte uninitialized; the WAL byte-digest must differ across runs | `BM6` |
| deterministic memtable shape | swap in a PRNG height source; the structural digest must differ across runs | `BM6` |
| the A5 scan lane (§9.4) | add a raw `::open` in the engine; the lane must fail | — the blind-patch set is the lane's own mutation test, per DR-27 |
| vendored-tree integrity (§9.2) | edit one byte of the vendored GoogleTest; the content-hash lane must fail | — |
| kill-point census (§9.5) | add an Env call and do not update the census; the sweep must report the change | — |

**The byte-digest gate earns its own line.** Same workload, same WAL bytes, SHA-256 pinned. It is the
C++ analogue of the trace hash, and it catches three things for one test: ambient randomness,
uninitialized padding, and any float that reached a serialization path. It is also the reason MSan stays
declined.

---

## 11. Known idealizations

Stated so no claim is broader than the evidence. Item 1 is ratified and belongs in DESIGN-A0 §7 per §1.1;
the rest are B1-local and live here.

1. **The exactness half of the recovery contract is a property against TestEnv, not against a real
   filesystem** (§4, §1.1). Against production, page-cached bytes can survive a process kill and recovery
   can legitimately return more than the last promised watermark; the guarantee there is
   `recovered ∈ [DurableSeq, VisibleSeq]`. The safety-critical half holds in both. A weaker observer, not
   a weaker engine.
2. **Short writes are unit-tested at the production Env's internal raw-write seam and are absent from the
   kill-point sweep** (§3.3 ⁽¹⁾), so they never combine with another injected fault in one run.
3. **The in-process kill keeps the process alive** (§9.5), so a bug that would have crashed the process
   post-kill is caught by ASan/UBSan rather than by the rig; the sampled real-`_exit` lane bounds that
   gap, and the sample rate is the honest measure of it.
4. **Torn `Sync` is prefix-granular in contract mode** (§5.5). A device that reorders across an fsync is
   exercised in characterization mode, where the engine's obligation is detection rather than exactness,
   and where §7.5 makes the run structurally uncountable as evidence.
5. **B1 has no flush**, so the memtable and the WAL set grow without bound and every B1 test is small.
   Nothing in B1 exercises recovery across a flush boundary; that arrives with B2, which is also where
   §7.2's `max+1` numbering rule expires.
6. **Concurrency coverage is one authored interleaving pattern, not a search.** The TSan harness (§6.3)
   drives `Apply`/`Get` against `Sync` for a fixed op count; it is not a systematic exploration of
   interleavings, and TSan reports the races it observes rather than the ones that exist. This is the
   honest bound on the lock's verification, and it is a much smaller surface to bound than a lock-free
   structure's would have been — which is a point in the ruling's favour, recorded as such.

---

## 12. Findings and coordination

Items 1–3 need someone with ownership of a Track A file; item 4 is owed by me next cycle.

1. **The §1.1 verification-scope text must land in DESIGN-A0 §7 and README before B1.1.** Ruled: "it goes
   there before any implementation, not after, because a scope caveat written after the claim is a
   retraction." I do not own either file, so the text is paste-ready in §1.1 and this is the item that
   gates the first B1 commit.
2. **`Makefile` and `.github/workflows/cpp.yml`**: `cpp-test`, `cpp-asan`, `cpp-ubsan` un-stub with B1,
   and `cpp-tsan` is a new required lane. Track A's files; coordination, not a blocker for this document.
3. **`engine/model/model.go` lines 24–26** still describe the pre-fix two-version representation. Ansh is
   carrying the fix to Track A; recorded here only so the thread closes.
4. **Owed next cycle:** rebase the `rift-b` worktree (`/Users/anshk/Desktop/rift-b`, currently at
   `1390969`) onto `main` and report the resulting HEAD, per the Q8 ruling. `.gitignore` already carries
   `engine-cpp/build/`, so nothing else is needed to receive the tree.

---

## 13. Questions remaining

Two, both small, both created by this revision rather than carried from the last.

> **B1-Q9.** "Are 64 MiB (maximum logical record) and 256 MiB (WAL buffer cap) the right tripwire
> values, and should they be compile-time constants or run-time configuration?"

**Recommendation: the values as proposed, as run-time configuration with those defaults, and the
`cap ≥ 2 × max_record` invariant asserted at construction.** The arithmetic behind 64 MiB is in §8.2
(≈1.22 M point deletes at a 50-byte key), which is more than any single range's clear-then-ingest should
ever produce, and the pair leaves 4× headroom. Run-time rather than compile-time so the sweep can set
them *low* deliberately — a tripwire nobody has watched fire is the same decoration this project rejects
everywhere else, and §10's two tripwire gates need to reach them cheaply. I am asking because these are
the only two magic numbers in the design and they bound a failure mode rather than a policy.

> **B1-Q10.** "Does `Status::kRecordTooLarge` propagate to the Go `Apply` as an error at B5, or is it a
> `CHECK` failure that aborts?"

**Recommendation: propagate as an error, and decide the Go-side handling at B5 with Q5's
`Status::Busy`.** They are the same question — what does the frozen interface's `error` return mean when
the model never errors — and answering them together at B5 is better than answering half of it now with
no poller to test against. The B1 obligation either way is unchanged: nothing is applied, atomically, and
the rig records `kTripwire`. I flag it because "the engine returns an error the model cannot" is a
divergence class B4's rig needs a rule for, and I would rather that rule be written once for both cases.

---

## 14. Landing plan

Small diffs, each with its own gate, none started before §13 is ruled and the remote gate clears. §12.1
gates B1.1.

| PR | contents | gate |
|---|---|---|
| B1.0 | vendored GoogleTest at the pinned commit; `third_party/googletest/VERSION`; content-hash lane | the vendored-tree integrity gate, induced by a one-byte edit |
| B1.1 | CMake skeleton, static archive, `Status`, `RunOutcome` + `CountsAsRecoveryEvidence`, `make cpp-test/asan/ubsan/tsan` un-stubbed | lanes run and fail loudly on a planted failure; `BM13` |
| B1.2 | `Env` interface, `PosixEnv`, the raw-write seam and its short-write unit test | short-write, `EINTR`, zero-return tests green |
| B1.3 | `TestEnv`: `content`/`durable`, the fault controller, the ledger, the kill mechanism, the census | the durability model's own tests; the ledger's induced failures |
| B1.4 | the A5 scan lane, `CPP-HATCHES.txt`, the blind-patch set | planted `::open` fails the lane; an unused registry entry fails it |
| B1.5 | skiplist memtable under the DB mutex, arena, deterministic heights, structural digest | structural digest stable; `BM6`; `BM14` under TSan |
| B1.6 | WAL writer: framing, fragmentation, groups, the caps, byte-digest test | pinned bytes; fragmentation across a block boundary; both tripwire gates |
| B1.7 | WAL reader and recovery: the torn-tail rule, chain legality, resync | the seven recovery and corruption gates, each with its induced failure |
| B1.8 | `Open`/`Close`/`Write`/`Get`/iterator/snapshot; `DeleteRange` over the memtable | semantics suite mirroring `engine/model`'s |
| B1.9 | the kill-point sweep, the exactness oracle, the two-element verdict | full sweep green; both set elements observed; every mutant killed in budget |

---

## 15. Decision summary

The twelve. **B1-D3 and its sub-decisions (§5.3.1–§5.3.3) are the WAL record layout surface to be
frozen.** B1-D6c is ruled; the rest await rulings and none is self-ratified.

| # | decision | recommendation | state |
|---|---|---|---|
| B1-D1 | Env surface shape | file objects, one interception choke point, runtime virtual | proposed |
| B1-D2 | what a kill leaves on disk | power-loss model only; `durable` advances only on a returned `Sync` | proposed; its scoping consequence ratified |
| B1-D3 | WAL framing and record layout | 32 KiB blocks with fragmentation, plus a `GROUP_END` terminator; CRC over `length ‖ type ‖ payload`; sequence in the payload | **freeze surface** |
| B1-D4 | the torn-tail rule | resync-verified; a tail only if nothing structurally valid follows; interior corruption fails the open; chain legality generalizes it to multi-block | proposed |
| B1-D5 | torn-`Sync` granularity and recycling | prefix in contract mode, sector-subset as characterization; never recycle WAL files | proposed |
| B1-D6 | memtable: structure (a), height source (b), **concurrency (c)** | arena skiplist; heights from `fnv1a64(key)`; **the DB mutex, lock-free rejected** | (a)(b) proposed; **(c) RULED** |
| B1-D7 | manifest in B1 | none; the log is the single authority on the watermark | proposed |
| B1-D8 | record-size cap and overflow | 64 MiB; `kRecordTooLarge`, nothing applied, `kTripwire` | proposed (B1-Q9) |
| B1-D9 | WAL buffer: ownership, cap, assertions | engine-owned so `Apply` makes zero Env calls; 256 MiB cap; two assertions; `cap ≥ 2 × max_record` | proposed (B1-Q9) |
| B1-D10 | sequence space | collapse the batch; one sequence per `Apply`, identical to `engine/model`'s | proposed |
| B1-D11 | enforcing the non-syscall half of A5 | a scan lane with a checked-in registry, `HATCHES.txt`-shaped | proposed |
| B1-D12 | kill mechanism and identity | dead-flag in-process for the sweep, sampled real `_exit`; global ordinal plus labels plus a census | proposed |
