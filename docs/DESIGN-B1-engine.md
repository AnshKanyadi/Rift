# DESIGN-B1: Env, the WAL, the memtable, and the recovery contract

**Status:** **PROPOSED — awaiting rulings.** Nothing in this document is self-ratified, so nothing is
marked PROVISIONAL. No C++ file is written until the decisions below are ruled on and the remote gate
clears.
**Phase:** B1 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Blocks:** all of Track B. **Depends on:** the `engine/` interface frozen at A0.5, which this must
meet exactly — not approximately, because B4's differential rig defines "correct" as "byte-identical
to `engine/model`".

Decisions are numbered `B1-D<n>` and open questions `B1-Q<n>`, so they can be ruled on tersely
("D3b, D8c, rest as proposed").

---

## 0. Ruling echo

Rulings received since the last Session B report: **the seven inherited below, plus the two
requirements specific to this document.** They are reproduced verbatim in §1 and §2 and are the
constraints every decision here is argued against. No prior Session B report exists; this is the
first.

---

## 1. Scope: the inherited rulings, verbatim

Quoted exactly as received. Each is followed by what it binds in this document — not a restatement,
a pointer to where it does work.

> **1.** DeleteRange is in the frozen Engine interface. This engine implements it internally as
> iterate-and-point-delete through B2; real range tombstones land before any published benchmark that
> exercises deletes.

Binds §8. It has a consequence nobody has written down yet: iterate-and-point-delete must be
**expanded at `Apply` time and the expansion recorded in the WAL**, because expanding at recovery
time would run against a state recovery is still rebuilding. §8 argues this; it is the sharpest
correctness point in the document.

> **2.** No serialized byte this engine ever sees carries a Mono instant. Keys and values are opaque
> bytes by construction; the engine never interprets time.

Binds §3.4 (Env has no clock), §5.3 (no timestamp field in any record, header, or filename), §6.2
(the comparator is bytewise and not pluggable), and §7.2 (WAL files are ordered by parsed file
number, never by mtime and never by `GetChildren` order).

> **3.** Recovery contract (the recovery-to-intermediate-sequence class from A0.5): crash recovery
> yields exactly the state at the durable watermark, for any watermark the sync-completion schedule
> can produce, including the dangerous direction where a lagging watermark recovers MORE than it
> promised. B4's rig compares recovered state against engine/model's state-at-seq; design the WAL and
> manifest so that comparison is exact.

Binds §4, §5.2, §5.4 and all of §7. This is the ruling the whole design is shaped by: the sync group
exists **only** to make the set of reachable recovery points equal to the set of promised watermarks.
§7.4 states my reading of "any watermark the sync-completion schedule can produce" for the one case
the phrase has to cover and I want confirmed (B1-Q7).

> **4.** Oracle independence: the crash rig's verdicts come from its own op log, never from asking the
> engine what it believes it holds. The recorded sentence is "an oracle that interrogates the engine
> believes the lie."

Binds §7.3, which is written to satisfy it under adversarial reading: the oracle's two inputs are the
rig's own call log and TestEnv's own fault ledger, both harness-side. The engine's `Sync()` return
value is used only as *the promise being held to*, never as *the answer being checked* — §7.3 makes
that distinction load-bearing rather than rhetorical.

> **5.** Amendment A5 applies with the Env seam as this language's enforcement mechanism: every
> syscall through Env, no wall-clock reads, no ambient randomness, no floats on any path that affects
> on-disk bytes.

Binds §3 and §9.2. **Finding, raised rather than assumed:** the Env seam enforces the *syscall* half
of this ruling and is structurally incapable of enforcing the other three. A `double`, a `rand()`, or
a `chrono::steady_clock::now()` is invisible to a file-I/O interface. The Go side did not rely on a
seam either — it built `determinismcheck`. §9.2 (B1-D11) proposes the C++ analogue, because otherwise
three quarters of this ruling is enforced by review, which DR-16 already rejected once.

> **6.** Compaction policy is a DESIGN-B3 decision per Amendment A6: the simplest correct policy wins
> v1, chosen with measurement; multi-level leveled is a recorded upgrade path, not a v1 requirement.

Binds nothing in B1 except restraint. B1 has no flush, no SSTables and no compaction; where a B1
choice constrains B3 (memtable refcounting, WAL rotation, the `DeleteRange` op kind reserved in the
record format) it is called out as a forward binding and no more.

> **7.** Build and hygiene per CLAUDE.md: CMake producing a static archive, ASan and UBSan lanes as
> definition of done for any code that eventually lands.

Binds §9. The Makefile already carries `cpp-test`, `cpp-asan` and `cpp-ubsan` as stub lanes named
against B1; B1 un-stubs exactly those three. §9.3 asks about two additions (B1-Q4).

---

## 2. The two requirements specific to this document

> First, the Env surface is a fault-injection surface before it is a portability surface: state
> explicitly, per call, how TestEnv injects sync loss, torn writes, short writes, IO errors,
> disk-full, and a kill point, and if any call cannot express one of those, say which and why in the
> doc rather than omitting it.

§3.3 is the matrix. One fault has no home at this seam — short writes — and §3.3.1 says so, says why,
says where it is tested instead, and names what that costs.

> Second, the WAL section must state its torn-tail rule as a decision with rejected alternatives: what
> a partially written trailing record means at recovery, how it is distinguished from corruption in
> the middle of the log, and why that distinction is safe under the exactly-at-watermark contract.

§5.4, as B1-D4, with four candidates and the safety argument written as a proof obligation with its
premises named — including the premise that fails, and what happens then.

---

## 3. The Env abstraction

### 3.1 What Env is for, in priority order

1. **A fault-injection surface.** Every failure the B1 and B4 rigs need must be expressible as a
   behaviour of an Env call, and every Env call must be a kill point.
2. **The A5 boundary for syscalls.** Every syscall goes through it for the reason every clock read
   goes through `Clock`.
3. **Portability.** Third, and barely: we target Linux and macOS and nothing else.

The ordering matters because it decides ties. Where a portable-looking abstraction and an
injectable-looking one differ, the injectable one wins.

### 3.2 B1-D1 — the shape of the surface

**Candidates**

- **(a) LevelDB-shaped: file objects.** `Env` is a factory for `WritableFile`, `SequentialFile`,
  `RandomAccessFile` and `Directory`; per-file state lives on the object.
- **(b) Flat and syscall-shaped.** One `Env` with `Open`, `Read`, `PRead`, `Write`, `Sync`, `Rename`,
  `List`, `Close`, files identified by an opaque handle. Every injection point is one function.
- **(c) (a) for the engine's vocabulary, with every call routed through one interception choke point
  inside Env.**

**Tradeoffs.** (a) is proven and reads well, and per-file fault state (a quota, a torn-write
schedule, the durable image) has an obvious home. Its weakness is that "inject at every call" becomes
N implementations of the same wrapper, which rot at different rates. (b) makes injection uniform and
the kill-point counter trivial, but it pushes offset bookkeeping into the engine and loses the
compile-time distinction between a file you may append to and a file you may seek in — a distinction
that is worth having when the WAL is append-only by construction.

(c) is (a)'s vocabulary with (b)'s uniformity. Every method on every Env object begins with one call
to `FaultController::Intercept(CallSite)`, which returns either "proceed" or the injected outcome.
The controller owns the kill counter, the quota, the error schedule and the ledger; the file objects
own only their bytes.

**Recommendation: (c).** Runtime virtual dispatch, not templates: the differential and kill-point
rigs want to construct a production DB and a TestEnv DB in one process and swap them, and one virtual
call per syscall is unmeasurable against the syscall.

**Rejected:** (a) alone — N wrappers is N places for a fault to be silently unreachable, and an
unreachable injector looks exactly like an injector that found nothing. (b) — the engine would carry
file offsets, and the append-only property of the WAL would stop being a type-level fact.

### 3.3 The fault matrix

`✓` = TestEnv injects it at this call. `—` = not expressible here, with the reason.
Every row's kill-point column is `✓` by construction: the choke point of B1-D1(c) counts every call,
so "kill at any syscall boundary" is a property of the seam rather than of anyone's diligence.

| call | sync loss | torn write | short write | IO error | disk full | kill point |
|---|---|---|---|---|---|---|
| `Env::NewWritableFile` | — nothing synced yet | — no bytes yet | — see §3.3.1 | ✓ `EACCES`, `EMFILE`, `EIO` | ✓ `ENOSPC` on inode allocation | ✓ |
| `Env::NewSequentialFile` | — read path | — read path | — | ✓ `ENOENT`, `EIO` | — | ✓ |
| `Env::NewRandomAccessFile` *(declared, first used B2)* | — | — | — | ✓ | — | ✓ |
| `Env::GetChildren` | — | — | — | ✓ | — | ✓ |
| `Env::GetFileSize` / `FileExists` | — | — | — | ✓ | — | ✓ |
| `Env::DeleteFile` | ✓ the unlink is applied to `content` and not to `durable` until the directory is synced | — a directory entry is all-or-nothing | — | ✓ | — | ✓ |
| `Env::RenameFile` *(declared, first used B2's manifest swap)* | ✓ same as above — this is the injector that finds a missing directory sync around an atomic rename | — atomic at the filesystem level, by the guarantee we are relying on | — | ✓ | — | ✓ |
| `Env::CreateDir` | ✓ | — | — | ✓ | ✓ | ✓ |
| `Env::LockFile` / `UnlockFile` | — | — | — | ✓ `EAGAIN` (already locked), `EIO` | — | ✓ |
| `Directory::Sync` | ✓ **returns OK and does not promote directory entries to `durable`** | — | — | ✓ | — | ✓ |
| `WritableFile::Append` | — buffered; nothing has reached the device | — nothing on the device to tear | — see §3.3.1 | ✓ | ✓ *(optional eager-allocation mode; the default charges at `Flush`)* | ✓ |
| `WritableFile::Flush` | — `Flush` promises visibility to other openers, not durability | ✓ a kill inside `Flush` leaves a prefix of the flushed extent in `content` | — see §3.3.1 | ✓ | ✓ `ENOSPC` — the default charge point | ✓ |
| `WritableFile::Sync` | ✓ **the primary site** — returns OK, `durable` not advanced | ✓ a kill inside `Sync` promotes a **prefix** of the newly covered extent to `durable`; this is the only way a torn record ever reaches the recovery path (B1-D5) | — see §3.3.1 | ✓ `EIO`, including the report-once-then-clear behaviour that has eaten data in real systems | ✓ `ENOSPC` surfacing here under delayed allocation | ✓ |
| `WritableFile::Close` | — | — | — | ✓ **the dropped-close-error class**: `close(2)` can report `EIO` for writeback that failed after the last `Sync` | ✓ | ✓ |
| `SequentialFile::Read` | — | — | — a short read at EOF is normal and is not a fault | ✓ | — | ✓ |

Three rows deserve a sentence each.

**`Directory::Sync` is not decoration.** A WAL file created, written and fsynced is still losable if
the directory entry naming it was never made durable: the bytes survive and the name does not.
TestEnv models directory entries with their own `content`/`durable` pair for exactly this, and §7.2's
gapless-numbering check is what turns the loss into a failed open instead of silence.

**`WritableFile::Sync` is where sync loss and torn writes both live**, and they are different faults.
Sync loss is a `Sync` that returns success and promotes nothing — the disk lied, and whether the
exactness oracle is held to cover it is B1-Q3. A torn `Sync` is a kill *inside* the call, which
promotes a prefix; the engine never observes it, and it is the sole producer of the torn tails §5.4
exists to rule on.

**`Close` is a write call.** Treating it as bookkeeping is a known way to lose data.

#### 3.3.1 Short writes have no home at this seam — and that is a decision, not an omission

`WritableFile::Append` is all-or-nothing by contract: it consumes the entire slice or returns an
error. A short `write(2)` is therefore a condition of the *production* Env's implementation, not of
the interface the engine sees — and TestEnv, which never calls `write(2)`, has nothing to shorten.

The loop that must handle it is real code and gets its own seam one level down. `PosixWritableFile`
takes an injectable raw-write function; a unit test drives it with a generator producing 1..n−1 bytes
per call, `EINTR`, and a zero-byte return that must not spin. Same treatment for `pread` short reads
in B2.

**What that costs, stated rather than hidden:** short writes are covered by a unit test and not by the
kill-point sweep, so a short write cannot be *combined* with a kill point, a quota exhaustion, or a
torn sync in one run. The alternative is to make `Append` short-returning and push the loop into the
engine, which would put the fault inside the sweep. Rejected: it duplicates the loop at every call
site, and it moves a syscall detail across the exact abstraction line Env exists to draw. If a sweep
of the interaction is ever wanted, the cheaper route is to give `PosixWritableFile`'s injectable
raw-write its own miniature sweep rather than to widen the engine's contract.

### 3.4 What Env deliberately does not have

LevelDB's `Env` carries `NowMicros`, `SleepForMicroseconds`, `Schedule` (a background thread pool),
and `NewLogger`. **Ours carries none of them**, and each omission is a ruling, not a simplification.

- **No clock.** Ruling 5. A wall-clock read is unobtainable by construction, not by discipline, so
  the C++ analogue of `clock/real.go`'s single hatched `time.Now()` is *zero* hatched calls.
- **No sleep.** A sleep is a timing dependency in a rig whose whole value is that timing is authored.
- **No thread pool.** Background work scheduled by Env would make kill points unorderable: the sweep
  identifies a kill point by a call ordinal, and an ordinal is meaningless if an invisible thread is
  drawing from the same counter. Forward binding for B3: compaction's thread is the *engine's*,
  declared explicitly, joined explicitly, and visible to the sweep as its own ordinal stream.
- **No logger.** Diagnostics go to a caller-supplied sink; the engine does not open files to talk
  about itself.

**`GetChildren` is this language's map range.** Directory order is filesystem-dependent and therefore
nondeterministic; recovery sorts by parsed file number before doing anything. TestEnv returns children
in a *deliberately hostile* order (reverse-sorted) so that an engine which forgot to sort fails on the
first test rather than on someone else's filesystem.

---

## 4. B1-D2 — what a kill leaves on disk

This is the most consequential decision in the document, because every later contract is stated
against it.

**Candidates**

- **(a) Process-crash model.** The page cache survives, so `durable == content` always; only a
  power-loss injector would differ.
- **(b) Power-loss model.** A file's `durable` image advances only when a `Sync` covering it returns;
  a kill discards everything written since.
- **(c) Both, selectable per run.**

**Tradeoffs.** (a) is what `kill -9` actually does, and it is useless to us: under (a) an unsynced
write is never lost, which makes the Go-side contract's entire unsynced window untestable and turns
the `sync_loss` injector into decoration. Worse, it is *green* — an engine that never syncs at all
would pass every (a) test.

(b) is strictly more adversarial and is what the frozen `Engine` contract already assumes: "buffered
writes are readable and losable". It also happens to be the honest model for the failure we actually
fear, which is not a process dying but a machine losing power mid-compaction.

(c) is tempting and I am recommending against it, because two durability models means every contract
in §7 acquires a qualifier, and qualified contracts are the ones people misremember.

**Recommendation: (b), as the single model the contract is stated against.**

```
per file:  content[]   what a reader sees now
           durable[]   what a kill would leave
Append/Flush:  content grows.                       durable unchanged.
Sync (clean):  durable  = content.                  ledger records the extent.
Sync (loss):   durable  unchanged.                  Sync returns OK anyway.
Sync (torn):   durable  = content[0 : k), k injected.  kill fires; Sync never returns.
kill:          content  = durable.  All handles closed. All in-memory engine state abandoned.
```

The symmetry with `engine/model` is exact and it is the reason the differential comparison is
well-posed at all: the model keeps `durable` plus an ordered list of `pending` versions and reverts to
`durable` on `Crash()`; TestEnv keeps `durable` plus the un-synced tail of `content` and reverts on
kill. Two implementations of one idea, which is what makes disagreement between them meaningful.

**Rejected:** (a) — makes the unsynced window untestable and would pass an engine that never syncs.
(c) — a second model buys fidelity we do not need and costs a qualifier on every safety sentence.

**Honest scoping, stated once here and repeated in §11:** the production Env against a real
filesystem does *not* provide (b). After a real `kill -9`, page-cached bytes survive and recovery can
legitimately return more than the last promised watermark. So the exactness half of the contract is a
property of the engine **against TestEnv**, and against production the guarantee degrades to
`recovered ∈ [DurableSeq, VisibleSeq]`. That is not a weaker engine; it is a weaker *observer*. It
goes in README's verification-scope section next to A0's idealization list.

---

## 5. The write-ahead log

### 5.1 B1-D3 — framing

**Candidates**

- **(a) Flat length-prefixed records.** Header, payload, repeat. Simplest possible.
- **(b) LevelDB-shaped: 32 KiB blocks with record fragmentation** (`FULL` / `FIRST` / `MIDDLE` /
  `LAST`), records never crossing a block boundary unfragmented.
- **(c) (b) plus an explicit sync-group boundary in the record stream.**

**Tradeoffs.** The interesting axis is not space, it is **resynchronization**. Under (a), a corrupt
*length field* makes every subsequent byte unparseable: recovery cannot tell "the log ends here"
from "there are twenty valid records after this and I can no longer find them". §5.4's whole rule
depends on being able to tell those apart, so (a) does not merely lose a nicety — it makes the
torn-tail rule unsafe, because the safe-looking behaviour (stop at the first bad record) silently
discards promised data.

(b) buys the discrimination: damage is bounded to one block, and recovery can always advance to the
next block boundary and ask whether anything valid lives there. The cost is 7 bytes per fragment plus
up to 6 bytes of block padding.

(c) adds the second thing the contract needs and (b) does not provide: **atomicity of a sync group at
recovery**. Without it, a torn `Sync` leaves recovery landing on whichever *batch* boundary happened
to survive, so the oracle's expected recovery point is "any of the k batch boundaries inside the
in-flight group" and the comparison in ruling 3 stops being exact. With it, the expected set collapses
to two known values (§7.3). That collapse is the entire argument for the marker and it is worth
stating as a slogan: **the group marker is what turns a range check into an equality check.**

**Recommendation: (c).**

**Rejected:** (a) — no resync, so the torn-tail rule would be unsafe in exactly the case it exists
for. (b) alone — correct, but leaves the oracle checking a range where ruling 3 asks for an equality.
Also rejected, and recorded because it is the obvious alternative to the marker: **a separate small
"durable extent" file fsynced after each group.** It is correct and it directly encodes the
tail/interior boundary, but it doubles the fsyncs on the commit path, and a 2× write-latency tax to
make an oracle's arithmetic easier is the wrong trade in a database.

### 5.2 The sync group

A **group** is every batch appended between the start of one `Sync` and the start of the next. Groups
are the unit of three things at once, deliberately:

- the unit of **durability** — a `Sync` covers exactly one group and everything before it;
- the unit of **recovery** — a group is committed whole or not at all;
- the unit of **promise** — `DurableSeq` advances to a group's high sequence when, and only when, the
  `Sync` covering that group's terminator returns success.

Because all three coincide, the set of reachable recovery points and the set of promised watermarks
are the same set. That is the design's answer to ruling 3, and it is exactness **by construction**
rather than by care — the same move A0.5 made when it retained every intermediate version in the
model instead of trying to round the watermark correctly.

### 5.3 Record format

Fixed-width **little-endian**, no varints, no reflection, no timestamps. Little-endian rather than the
Go wire codec's big-endian because the WAL is never compared byte-for-byte across implementations —
only engine *state* is — and LE is a memcpy on both targets. A pinned byte-vector test freezes the
encoding regardless, so the choice cannot drift silently.

**Physical framing (LevelDB's, unchanged, so the fragmentation logic is the well-trodden one):**

```
block = 32768 bytes
header = crc32c : u32      // over [length ‖ type ‖ payload]
         length : u16      // payload bytes in THIS fragment
         type   : u8       // 0 = invalid (reserved), 1 = FULL, 2 = FIRST, 3 = MIDDLE, 4 = LAST
```

If fewer than `sizeof(header) + 1` bytes remain in a block, the remainder is **explicitly
zero-filled** and the next record starts in the next block. Type `0` is reserved-invalid precisely so
that a run of zeros — padding, or a hole past the written extent — can never be mistaken for a
record. That reservation is what makes §5.4's false-positive analysis work.

**Logical records.** Three kinds. Kind `0` is reserved-invalid for the same reason.

```
FILE_HEADER   kind:u8=3  magic:u64  format_version:u32  file_number:u64
BATCH         kind:u8=1  seq:u64  op_count:u32  ops[op_count]
GROUP_END     kind:u8=2  high_seq:u64  batch_count:u32
op            kind:u8  { 0=SET, 1=DELETE, 2=DELETE_RANGE (reserved, first written in B3) }
              key_len:u32  key:bytes
              [ value_len:u32  value:bytes ]   // SET only
              [ end_len:u32    end:bytes   ]   // DELETE_RANGE only
```

- **`FILE_HEADER` is the first logical record of every WAL**, rather than a raw file header, so block
  arithmetic still starts at offset 0. It catches an empty file, a truncated file, a foreign file, and
  a file whose name and contents disagree — recovery validates `file_number` against the filename.
- **`seq` is the `engine.SeqNum` that `Apply` returned.** One per batch, `+1` per `Apply` including
  empty ones, identical to `engine/model`'s counter. §8.2 argues why the two engines must share a
  sequence space rather than merely each being internally monotone.
- **`DELETE_RANGE` is reserved and never written before B3.** Reserving the byte now is free; a
  format version bump at B3 is not.
- **No timestamps anywhere** — ruling 2. Not in the header, not in a record, not in a filename.

### 5.4 B1-D4 — the torn-tail rule

**The question, restated so the answer is unambiguous.** Recovery reads a record and the read fails —
bad checksum, header truncated by EOF, payload truncated by EOF, or a length that runs past the end of
its block. What does that mean, and what does recovery do?

**Candidates**

- **(a) Every checksum failure is fatal.** The database refuses to open.
- **(b) Every checksum failure is end-of-log.** Truncate there, open successfully. (LevelDB's
  behaviour with `paranoid_checks` off.)
- **(c) Position-based, without resynchronization.** A failure at what appears to be the last record
  is a tail; anything earlier is corruption.
- **(d) Resync-verified.** A failure is a tail **only if nothing structurally valid follows it**;
  otherwise it is corruption and the open fails.

**Tradeoffs**

(a) is unusable. A torn tail is the *normal* outcome of a crash during a write, so (a) converts the
single most common real-world event into an outage. It also inverts the risk: it fails loudly in the
safe case and buys nothing in the dangerous one.

(b) is the dangerous one, and dangerously comfortable. It is correct whenever the failure really is
the tail, and it silently discards promised data whenever it is not. Silently is the operative word —
there is no log line, no error, no metric; the database opens, is short some committed writes, and
nobody learns anything until a consistency check somewhere else fires weeks later.

(c) sounds like (d) but is not, because "appears to be the last record" is not decidable without
resynchronization. A corrupt *length field* leaves recovery unable to locate the next record at all,
so under (c) every corrupt length is classified as a tail — which is exactly (b) in the case that
matters. (c) is (b) wearing a better name, and that is the trap worth recording.

**Recommendation: (d).** The rule, normatively:

> A recovery read that fails — bad CRC, truncated header, truncated payload, or a length running past
> its block — **terminates the log at that point**. Groups already closed by a `GROUP_END` stand; any
> `BATCH` records after the last `GROUP_END` are **discarded**. This is not an error and is not
> reported as one.
>
> Recovery then **resynchronizes**: it advances to the next block boundary and scans forward for a
> structurally valid record. If it finds one, the log is **corrupt in the interior**. The open
> **fails**, reporting file, block, byte offset, and the sequence of the last committed group. No
> silent truncation, ever.

**Why the distinction is safe under the exactly-at-watermark contract.** Four steps, with the
premises named because one of them is where the argument ends.

1. *A torn record lies strictly after the last durable `GROUP_END`.* Under B1-D2(b), a file's durable
   image advances only when a `Sync` returns, and by §5.2 a `Sync` covers a whole group ending in its
   `GROUP_END`. A torn record is by definition partially written, so it was not in any returned
   `Sync`'s extent.
2. *Therefore discarding the tail never discards a promised byte.* The last durable `GROUP_END` is at
   or after the last promised watermark, and everything discarded lies after it. So `R ≥ W`: the
   safety-critical direction, "committed is forever", holds.
3. *And recovery cannot land above the in-flight group.* Recovery commits only complete groups, so `R`
   is a group boundary; the highest one that can exist on disk is the group whose `Sync` was in flight
   when the kill landed. So `R ∈ {W, G_inflight}` — the two-valued set §7.3 checks against.
4. *So the only way a valid record can follow an invalid one is that something violated a premise.* A
   single append-only file is written in offset order, and durability under B1-D2(b) is prefix-closed,
   so a crash cannot produce a valid record after a torn one. Media corruption can, and a device that
   reordered across an fsync can. Both mean the premise of step 1 is false — and step 1 is what makes
   truncation safe. **When the premise fails, truncation is no longer safe, so recovery must not
   truncate.** That is the whole argument for (d) over (b), and it is why the response is a hard error
   rather than a best effort.

**The false-positive analysis, because (d)'s cost is spurious hard errors.** Resync must not mistake
garbage for a valid record and turn a normal torn tail into a refused open — an availability bug
manufactured by a safety rule. Three things make that essentially impossible here:

- WAL files are **never recycled** (§5.5), so bytes past the written extent are zeros or absent.
- Type `0` is reserved-invalid, so a zero-filled header is rejected before its CRC is even considered.
- A candidate must pass CRC-32C over its own payload *and* be `FULL` or `FIRST` at a block boundary.

A `2⁻³²` accidental CRC match on non-zero garbage remains possible in principle. That is acceptable
and its direction is right: the failure mode is a refused open with a byte offset, which a human
investigates, rather than a successful open missing data, which nobody does.

**Rejected:** (a) — makes every unclean shutdown an outage. (b) — silently discards promised data in
the interior-corruption case, in the dangerous direction, undetectably. (c) — undecidable without
resync, and therefore identical to (b) exactly when it matters.

### 5.5 B1-D5 — how a torn `Sync` is modelled, and WAL recycling

**Torn-sync granularity.** Candidates: **(a) prefix** — a kill inside `Sync` promotes `content[0:k)`;
**(b) sector-subset** — an arbitrary set of 4 KiB sectors of the newly covered extent is promoted.

**Recommendation: (a) as the contract model, (b) available and labelled as characterization-only.**
(b) can promote a `GROUP_END` while leaving an earlier record in the same group torn, which is a
device that violated fsync's own ordering guarantee. Against such a device the engine cannot be held
to exactness, and holding it to exactness anyway would mean the oracle reports the engine for the
disk's crime. Under (b) the engine's obligation is narrower and still real: **detect and refuse**,
which is exactly what §5.4(d) already does — valid terminator, broken interior, hard error. So (b)
becomes a *detection* test rather than an *exactness* test, and it is run and reported as one.

**WAL files are never recycled in B1.** Recycling (RocksDB's `recycle_log_file_num`) avoids a file
creation and a directory fsync per rotation, and pays for it by leaving stale, CRC-valid records past
the tail — which breaks §5.4's false-positive analysis outright and would force a per-file nonce mixed
into every record's CRC. Recorded as a deliberate non-goal with its cost, its upgrade path, and the
condition under which it earns reconsideration (a measured rotation-rate problem at I2, not before).

---

## 6. The memtable

### 6.1 B1-D6 — structure and, specifically, the height source

CLAUDE.md specifies a skiplist, so the structure is not open. The decision inside it is: **where do
node heights come from?**

**Candidates**

- **(a) A PRNG, as LevelDB does** (`rnd_.OneIn(kBranching)`).
- **(b) Derived from the key:** `height = 1 + min(clz-or-ntz(fnv1a64(key)) / 2, kMaxHeight-1)`.
- **(c) Derived from an insertion ordinal** owned by the DB.

**Recommendation: (b).** This is DR-12's ruling one language over: `engine/model`'s treap priorities
come from `fnv1a64(key)` and not from RNG, so that engine internals stay decoupled from any random
stream. The same argument applies here and ruling 5 makes it mandatory — a PRNG is ambient randomness.
(b) also gives a stronger property than mere determinism: the same key always gets the same height, so
the *structure* is a pure function of the key set, and a shape-dependent bug reproduces from the
workload alone.

(c) is deterministic per run but not per key, so replaying the same keys in a different order builds a
different structure — the reproducibility we would be buying is the weaker half.

**Rejected:** (a) — banned by ruling 5, and it makes any shape-dependent bug irreproducible.
(c) — reproducible only under identical insertion order, which is the case we least need.

The distribution question is worth one line: hash bits are uniform, so (b)'s height distribution
matches (a)'s in expectation. An adversarial key set could skew it; that skew would be
*deterministic*, which is the only property we need from it.

### 6.2 Internal keys, and the comparator that stays bytewise

```
internal_key = user_key ‖ ( (seq << 8) | value_type )  as u64 little-endian
```

Ordering: user key ascending by `memcmp`, then `seq` **descending** so the newest version of a key
sorts first and a snapshot read is a single seek.

Multiple versions per key are required — `NewSnapshot` pins a sequence and a read through it must skip
newer versions — so the memtable is append-only and never overwrites an entry.

**The comparator is bytewise and is not pluggable in v1.** A pluggable comparator is the door through
which the storage engine learns what a key *means*, and A5 puts MVCC timestamps inside keys. Ruling 2
says the engine never interprets time; a fixed bytewise comparator is that ruling made
uncompilable rather than remembered. The cost is real and named: B3 cannot implement a
timestamp-aware compaction filter, and does not need to, because version GC is A5's job on the Go
side.

### 6.3 Arena, memory, and concurrency

**Arena allocation.** Nodes and key bytes come from a bump allocator; the whole arena dies with the
memtable. Two reasons: exact memory accounting, which B2's flush threshold needs and which a
general allocator cannot provide cheaply; and no per-node free path to get wrong under a kill.
Nothing may depend on an address — no pointer-keyed containers, no address-ordered anything — which
is the C++ restatement of the map-iteration rule and is checked by §9.2's scan.

**Concurrency.** One writer at a time under the DB mutex; readers lock-free via
release-store/acquire-load on next pointers, linking a new node from level 0 upward so a reader that
misses it at a high level still finds it at level 0. This is LevelDB's design and it is correct for
the same reasons; what matters here is that **ASan and UBSan cannot see a data race**, so the claim is
unproven without a TSan lane. That is B1-Q4.

**Unbounded growth in B1.** There is no flush until B2, so the memtable grows without bound and old
WALs are never deleted. B1's tests are sized accordingly, and the constraint is recorded because it is
also what makes §7.2's gapless-file-number check sound.

---

## 7. The recovery contract

### 7.1 Mapping the frozen Go interface onto the C++ engine

| `engine.Engine` (frozen, A0.5) | C++ engine | who bridges |
|---|---|---|
| `Apply(b, sync) (SeqNum, error)` — never blocks on I/O | `Write(batch) -> (seq, Status)`; appends to the memtable and to the engine-owned WAL buffer; **makes no Env call** (§8.1) | direct |
| `DurableSeq() SeqNum` | `DurableSeq()` — advances only when a `Sync` returns | direct |
| `OnDurable(func(SeqNum))` | **absent by design** — no C→Go callbacks (DR-11) | the Go wrapper's per-engine poller owns the blocking `Sync()` and posts to the node mailbox |
| — | `Sync() -> (seq, Status)` — blocking; covers everything appended so far | B5 |
| `Get`, `NewIter`, `NewSnapshot`, `ApproximateDiskBytes`, `Close` | same shapes, `Status` instead of `error` | B5 |

The `sync` flag's *policy* — how eagerly the poller wakes — is a B5 decision about the pair, not a B1
decision about the engine. B1 only guarantees that `Sync()` covers everything appended before it and
returns the watermark it established.

**`Close` does not sync**, and that is deliberate. The watermark is the engine's only durability
promise; a `Close` that synced would make clean shutdown a hidden durability event that
`engine/model`'s `Close` does not have, and the two engines would then disagree in precisely the
differential rig. The consequence is a good test: **close-then-reopen must be indistinguishable from
kill-then-reopen.**

### 7.2 Open

1. Acquire `LOCK`.
2. `GetChildren`, parse `NNNNNN.log`, **sort by parsed number** — never by directory order, never by
   mtime (ruling 2).
3. Assert numbering is **gapless**. In B1 no file is ever deleted, so a gap means a lost directory
   entry — the missing-`Directory::Sync` bug — and it is a hard error. This check is what gives the
   directory-sync kill point teeth; without it the loss is silent.
4. Replay each file in order into a fresh memtable, committing group by group (§5.4).
5. `recovered_seq` = the highest committed `GROUP_END.high_seq`. Assert it is monotone across files.
6. Create WAL `max+1` and **`Directory::Sync` before `Open` returns**.
7. `DurableSeq = VisibleSeq = recovered_seq`.

**B1-D7 — no manifest in B1.** Candidates: (a) none, file numbers from `max existing + 1`; (b) a
minimal manifest recording the WAL number and durable sequence; (c) build B2's MANIFEST early.
**Recommendation: (a).** There are no SSTables, so there is no version state to be inconsistent with,
and a manifest recording a durable sequence would be a *second* authority on the watermark that could
disagree with the log — the exact shape of the A0.5 bug, rebuilt in C++. The single source of truth is
the log itself: `recovered_seq` is a fact about bytes, derived, never stored. Forward binding to B2,
recorded so it cannot be forgotten: **the manifest may record which files exist; it may never record a
durable sequence the WAL cannot independently justify**, and `max+1` numbering stops being safe the
moment B2 deletes a flushed WAL, which is where the file-number counter moves into the manifest.
**Rejected:** (b) — a second authority on the watermark. (c) — B2 scope.

### 7.3 The oracle, stated so that it never asks the engine anything

The rig's inputs are **its own call log** — every `Write`, every `Sync`, in order, with return values
— and **TestEnv's fault ledger** — for each `Sync`, whether durability was applied fully, not at all,
or as a prefix. Both are harness-side. The engine's on-disk state is never parsed by the oracle, and
the engine is never asked what it believes it holds.

From the call log alone the rig knows the group decomposition: group *k* is the set of `Write`s
between the start of `Sync` *k−1* and the start of `Sync` *k*, with high sequence `G_k`. No byte-level
parsing is needed for this, which is the point — an oracle that parsed the WAL would be a second
implementation of the reader, and a second implementation can be wrong in the same direction as the
first.

Let `W` be the highest watermark the engine ever *returned* to the rig before the kill. The oracle
asserts **two** things:

- **(i) Exactness.** `recovered_state == model_state_at(R)` byte for byte over a full iteration, where
  `R` is the group boundary TestEnv's ledger justifies: `R = G_k` if `Sync` *k* was applied fully, and
  `R ∈ {G_{k−1}, G_k}` if `Sync` *k* was in flight or torn at the kill. Under §5.2's group atomicity
  that set has **at most two** elements and each is a known number — an equality check against a small
  known set, not a range check.
- **(ii) No over-promise.** `W ≤ R`. An engine that advanced its watermark before the data was durable
  fails here, and this is the assertion the whole rig exists for.

Both directions are covered and neither consults the engine's opinion. Over-reporting fails (ii);
under-reporting fails (i), because the ledger justifies more than the promise did. The `Sync` return
value appears only in (ii), as *the promise being held to* — the "client-observed response" A0's
oracle rule explicitly permits — never as the answer being checked.

### 7.4 B1-Q7 territory: the in-flight `Sync`

The two-valued case in (i) deserves to be surfaced rather than buried, because it is the one place my
reading of ruling 3 does real work.

A `Sync` can complete on the device and the kill can land before its return value reaches the caller.
The bytes are durable; the caller never learned it. Recovery yields `G_k`; the rig's log shows the
promise still at `G_{k−1}`. This is not an engine defect and no design removes it — it is the same
shape as "did the RPC commit?", one layer down.

My reading: ruling 3's "**for any watermark the sync-completion schedule can produce**" already covers
it, because `G_k` is precisely such a watermark. The contract is therefore an equality against a
two-element known set rather than against a single value, and the exactness ruling 3 asks for is
preserved in full — the set is known in advance, from the harness's own records, and each element is
an exact state. **B1-Q7 asks for confirmation of that reading rather than proposing a change to it.**

---

## 8. `DeleteRange` through B2, and the sequence space

### 8.1 The expansion must happen at `Apply`, and the WAL must record the expansion

Iterate-and-point-delete has to read current state to find the keys to delete, and `Apply` is what
makes the deletion visible — so the expansion happens at `Apply`. The consequential part is what goes
in the log.

**If the WAL recorded the raw `DeleteRange`, recovery would have to expand it again — against a state
recovery is still in the middle of rebuilding.** The expansion is a function of the state at the time
it runs, so replay-time expansion is only correct if that state provably equals the state at original
`Apply` time. It probably does today, for a reason that depends on the WAL's start point coinciding
exactly with the flush boundary — a property B2 is about to start changing. That is correctness by
argument, and the argument has a moving premise.

**Recording the post-expansion op list makes it correctness by construction.** Recovery replays point
deletes; there is nothing left to compute, and the circularity is gone. The cost is honest and worth
naming, because it is the price of ruling 1's staging: **a `DeleteRange(nil, nil)` — the clear half of
snapshot application's clear-then-ingest, which is the case Amendment A3 was ruled for — expands to a
point delete per live key, and all of them go into one WAL record proportional to the keyspace.**
Batches are atomic, so it cannot be chunked. This is precisely what B3's range tombstones fix, and it
is why B1-D3's block framing must fragment large records correctly from day one rather than treating
fragmentation as a rare path.

Intra-batch semantics come out right: at the `DeleteRange` op, the expansion covers the current state
*and* keys written earlier in the same batch, and a `Set` after it in the same batch re-adds the key —
which is the model's rule, reproduced.

### 8.2 B1-D8 — one sequence per batch, collapsed, sharing the model's sequence space

**Candidates**

- **(a) Collapse the batch to at most one op per key before insertion**; one internal sequence per
  batch, equal to `engine.SeqNum`.
- **(b) LevelDB's scheme:** the internal sequence advances per *op*, and `engine.SeqNum` is reported
  as the batch's last internal sequence.
- **(c) Pack `(batch_seq, op_index)` into the internal key.**

**Recommendation: (a).** Under (b) the C++ engine's sequence numbers jump (1, 5, 9, …) while
`engine/model`'s advance by one per `Apply`. That is contract-legal — the frozen interface only
requires monotonicity — and it is still the wrong choice, because B4's differential rig would then
have to maintain a per-engine map from operation index to sequence in order to sync both engines "to
the same point", and a rig that needs a translation table is a rig with a place to be wrong. (c) keeps
the spaces aligned but widens every internal key for a case that (a) removes entirely.

(a) costs a sort of the batch's ops by key — which §8.1's expansion already requires a pass over — and
it makes an invariant assertable: **no two memtable entries ever share a `(user_key, seq)` pair.**

**Rejected:** (b) — divergent sequence spaces would put a translation table inside B4's oracle.
(c) — wider internal keys to preserve a distinction (a) removes.

---

## 9. Threading, build, and the half of A5 the Env seam cannot see

### 9.1 B1-D9 — the WAL buffer belongs to the engine, not to `WritableFile`

LevelDB's `WritableFile::Append` flushes to the OS when its internal buffer fills, which means a write
can perform I/O at an unpredictable moment. Under the frozen contract, `Apply` **never blocks on
I/O**, and "unpredictable moment" is not a way to satisfy that.

**Recommendation:** the WAL buffer is the engine's own memory. `Apply` appends to it and makes **zero
Env calls**. The syncer path takes the mutex only long enough to swap in a fresh buffer, then performs
`Append` + `Sync` on the old one with the mutex released.

This turns the contract into a checkable invariant instead of a promise: TestEnv keeps a per-thread
call counter, and a test asserts it does not move across `Apply`. The parallel is deliberate — it is
the same move D5 made when it put persist-before-reply inside `raft/` rather than in every driver.

The precise reading of the guarantee, stated because someone will ask: **`Apply` may block on a mutex
for the duration of a pointer swap; it never blocks on an Env call.** Backpressure when the syncer
falls behind is B1-Q5.

### 9.2 B1-D10 — enforcing the non-syscall half of ruling 5

The Env seam cannot see a `double`, a `rand()`, a `steady_clock::now()`, or a raw `::open` that
bypassed it. Something else has to.

**Candidates:** (a) a source-scan lane with a checked-in exception registry; (b) clang-tidy with custom
matchers; (c) the Env seam plus review.

**Recommendation: (a) now, (b) as an upgrade if the scan gets noisy.** A scan over `engine-cpp/src`
banning `<random>`, `rand(`, `<chrono>`, `time(`, `clock(`, `float`, `double`, `getenv`, `<fstream>`,
and direct `open(`/`write(`/`fsync(` outside `env/posix/`, with a `CPP-HATCHES.txt` registry diffed
against the tree by the lane — `HATCHES.txt`'s structure, one language over, including the rule that
an **unused** entry fails. Banning direct syscalls outside `env/` is the mechanical form of "every
syscall through Env"; without it, the seam is enforced by nobody.

**Rejected:** (c) — this is DR-16's argument verbatim: the answer to "how do you know a
`steady_clock::now()` didn't sneak in?" must be a build failure, not a promise. (b) as the first
step — a real clang-tidy check is a day of work and a toolchain dependency for a job a grep does
today; it earns its place when the registry starts carrying arguments a grep cannot express.

### 9.3 Build and lanes

CMake produces `libriftengine.a`, a static archive, plus a test binary. `-fno-exceptions` and
`-fno-rtti` (an exception must never reach an `extern "C"` frame, and B5's boundary is that frame);
`Status` return codes throughout; `-Wall -Wextra -Werror`. No cgo in B1 — the C API is B5's design.

`make cpp-test`, `make cpp-asan`, `make cpp-ubsan` un-stub with B1. Editing the shared Makefile is
Track A's file; §12 flags it as coordination rather than doing it.

### 9.4 B1-D11 — how a kill point actually kills

**Candidates:** (a) real `_exit(0)` inside the Env call, re-running the workload per kill point;
(b) in-process: a dead flag; (c) both, sampled.

**Tradeoffs.** (a) is maximally faithful — no destructor runs, no heap survives — and costs a full
workload re-run per point, which makes a complete sweep unaffordable. (b) sweeps thousands of points
per second, and has one specific blind spot: the engine keeps running, so a bug in which "recovery"
reads live memory instead of disk could be masked.

The mechanism for (b) matters and rules out the obvious one. `throw` is unavailable under
`-fno-exceptions`, and would be wrong anyway: unwinding runs destructors, and a destructor that
flushes would write after the crash. Instead: **the fault controller sets a dead flag; every
subsequent Env call is a no-op returning `Status::Killed`, and TestEnv freezes its durable image.**
Code that ignores the `Status` can still only touch a frozen Env, so it cannot affect what recovery
reads — which is the only dimension a crash has. The rig then destroys the DB object and reconstructs
from a **fresh** TestEnv seeded only from the frozen durable image, so a stale pointer faults under
ASan rather than silently working. A cap on post-kill Env calls stops a runaway loop.

**Recommendation: (c)** — (b) for the sweep, (a) for a stated sample (proposal: every 32nd point plus
every point that has ever produced a failure), so the blind spot is measured rather than assumed.

**Rejected:** (a) alone — a complete sweep becomes unaffordable, and an incomplete sweep is the thing
B4 exists not to be. (b) alone — its blind spot is exactly "recovery accidentally reads live memory",
which is a real bug and would be invisible.

### 9.5 B1-D12 — kill-point identity

**Candidates:** (a) a global Env-call ordinal, swept 1..N; (b) named kill points declared in engine
code; (c) both.

**Recommendation: (c), with (a) load-bearing and (b) as a label.** The ordinal is complete by
construction — there is nothing to annotate and therefore nothing to forget — and the static label
attached at each call site turns the report from "kill 47 failed" into "kill 47 = `Sync(000001.log)`,
group 12, after 3 appends", which is a bug report. Plus a **kill-point census**: the sweep records how
many points it visited, per call kind, and a change in the census is surfaced. A new Env call that
nobody swept is otherwise invisible, and this is A0.6's step-census idea in a second setting.

---

## 10. How B1 proves itself

Per the standing protocol, **every gate below is landed only once its failure has been induced and
observed**, and the induced failure is what the entry records. A gate that has only ever been green
has demonstrated the cheap half.

| gate | what proves it can fail |
|---|---|
| recovery exactness (§7.3 i) | advance the watermark before `Sync` returns; the oracle must fire on `W ≤ R` |
| no over-promise (§7.3 ii) | make `Sync` return the *visible* sequence instead of the covered one |
| torn-tail rule (§5.4) | make recovery accept `BATCH` records after the last `GROUP_END`; exactness must fire |
| interior-corruption detection | flip one byte inside a fully synced group; the open must fail with an offset |
| interior corruption is not truncated | make recovery stop at the first bad record instead of resyncing; the planted corruption must go from "refused open" to "silent data loss", and the exactness oracle must catch it |
| gapless numbering (§7.2 step 3) | delete a WAL file; the open must fail |
| directory sync | kill between file creation and `Directory::Sync`; the gapless check must fire |
| `Apply` performs no I/O (§9.1) | move the WAL buffer into `WritableFile`; the per-thread Env-call assertion must fire |
| deterministic on-disk bytes | leave one padding byte uninitialized; the WAL byte-digest must differ across runs |
| deterministic memtable shape | swap in a PRNG height source; the skiplist structural digest must differ across runs |
| the A5 scan lane (§9.2) | add a raw `::open` in the engine; the lane must fail |
| kill-point census (§9.5) | add an Env call and do not update the census; the sweep must report the change |

**The byte-digest gate deserves its own line.** Same workload, same WAL bytes, SHA-256 pinned. It is
the C++ analogue of the trace hash, and it catches three things at once for one test: ambient
randomness, uninitialized padding, and any float that reached a serialization path. It is also why I
am *not* proposing an MSan lane (B1-Q4): MSan needs an instrumented libc++ and buys, for this code,
mostly the hazard the digest already catches.

**Mutants (Amendment A2).** Stored as patches applied to a scratch tree, per DR-27 — and not only for
consistency: `BM6` includes `<random>`, which §9.2's scan lane rejects, so it cannot exist as a
committed file for the same reason M4 and M5 cannot.

| mutant | injected bug | must be caught by | budget |
|---|---|---|---|
| `BM1-ack-before-sync` | advance the watermark before `Sync` returns | exactness (ii) | ≤ 5 kill points |
| `BM2-accept-torn-tail` | commit `BATCH` records with no `GROUP_END` | exactness (i) | ≤ 20 kill points |
| `BM3-silent-interior-truncate` | stop at the first bad record; never resync | corruption test + exactness (i) | immediate |
| `BM4-missing-dir-sync` | skip `Directory::Sync` after creating a WAL | gapless numbering | ≤ 50 kill points |
| `BM5-swallow-sync-error` | treat `Sync`'s `EIO` as success | exactness (ii) | ≤ 20 kill points |
| `BM6-prng-heights` | PRNG skiplist heights | scan lane (compile) + structural digest | immediate |
| `BM7-drop-close-error` | ignore `Close`'s error return | exactness (ii) | ≤ 100 kill points |
| `BM8-skip-crc` | do not verify record CRCs at recovery | corruption test | immediate |
| `BM9-apply-does-io` | flush inside `Apply` | Env-call assertion | immediate |

Budgets are in **kill points**, the C++ analogue of seeds-to-detection; wall-time-to-detection is
recorded alongside, per A2. A mutant surviving its budget means the rig is too weak and B1 is not done
regardless of what the clean runs say.

---

## 11. Known idealizations

Stated here so no claim is ever broader than the evidence, and destined for README's
verification-scope section alongside A0 §7.

1. **The exactness half of the recovery contract is a property against TestEnv, not against a real
   filesystem** (§4). Against production, page-cached bytes can survive a process kill and recovery
   can legitimately return more than the last promised watermark; the guarantee there is
   `recovered ∈ [DurableSeq, VisibleSeq]`. The safety-critical half — `recovered ≥ DurableSeq` — holds
   in both.
2. **Short writes are unit-tested at the production Env's internal seam and are absent from the
   kill-point sweep** (§3.3.1), so they never combine with another injected fault in one run.
3. **The in-process kill (§9.4) keeps the process alive**, so a bug that would have crashed the
   process post-kill is caught by ASan/UBSan rather than by the rig; the sampled real-`_exit` lane is
   what bounds that gap, and the sample rate is the honest measure of it.
4. **Torn `Sync` is prefix-granular in contract mode** (§5.5). A device that reorders across an fsync
   is exercised in characterization mode and the engine's obligation there is detection, not
   exactness.
5. **B1 has no flush**, so the memtable and the WAL set both grow without bound and every B1 test is
   small. Nothing in B1 exercises the interaction between recovery and a flush boundary; that arrives
   with B2 and is where §7.2's `max+1` numbering rule expires.

---

## 12. Findings from reading the tree

Reported, not acted on — three of the four are Track A's files.

1. **`engine/model/model.go`'s package comment still describes the representation that DESIGN-A0.5 §2
   records as the bug that nearly shipped.** Lines 24–26 say "Two versions are kept: visible (every
   applied batch) and durable (batches at or below DurableSeq)"; the `DB` struct comment at line 61
   correctly describes the fix (`durable` plus an ordered `pending` list, which is what lets the
   watermark sit at an intermediate sequence). The code
   is right and the doc comment is the pre-fix one. It matters more than a stale comment usually does,
   because this is the file B4's differential rig defines "state at seq" against, and the stale
   sentence describes exactly the behaviour the rig must not assume. Track A's file; flagging only.
2. **`Makefile` carries `cpp-test`, `cpp-asan`, `cpp-ubsan` as stubs named against B1**, and
   `killpoints` / `differential` named against B4. B1 must un-stub the first three, which means
   editing a file Track A owns. Coordination item, not a blocker for this document.
3. **`.gitignore` already excludes `engine-cpp/build/`**; nothing to add there.
4. **`engine-cpp/` does not exist yet** in either worktree, and the `rift-b` worktree at
   `/Users/anshk/Desktop/rift-b` is on commit `1390969`, several commits behind `main`. Where the
   first B1 commit lands is B1-Q8.

---

## 13. Open questions

Each is quoted as asked, with my recommendation. None is self-ratified; all await a ruling.

> **B1-Q1.** "Which C++ standard and compiler pin, and is `-fno-exceptions -fno-rtti` the house
> style?"

**Recommendation: C++17, `-fno-exceptions -fno-rtti`, `-Wall -Wextra -Werror`, pinned to a specific
clang and gcc in CI.** C++17 is universally available on both targets and gives `string_view` and
`optional`; C++20's concepts and modules buy nothing for an LSM and cost toolchain availability. No
exceptions because one must never cross the `extern "C"` frame B5 builds, and enforcing that at the
boundary alone is a weaker guarantee than not having them. The pin is the analogue of DR-26's
toolchain directive: a version should be a decision, not an accident of what is installed.

> **B1-Q2.** "How is GoogleTest acquired, given that every number must reproduce from a clean clone by
> one script?"

**Recommendation: CMake `FetchContent` pinned to an exact tag *and* commit hash, with a
`third_party/` cache directory and `FETCHCONTENT_SOURCE_DIR` honoured so a clean clone builds offline
once the cache is warm.** Options rejected: a system package (unpinned, differs per machine, and the
reproduce-from-clean-clone claim would be false); a vendored submodule (pinned and offline, but
submodules are a recurring operational tax and GoogleTest is large). If offline-from-cold is a hard
requirement rather than a preference, vendoring is the right answer and I will take that ruling.

> **B1-Q3.** "Does the exactness oracle cover a `Sync` that returns success and makes nothing durable
> — the lying disk — or is that injector characterization-only?"

**Recommendation: characterization-only, and reported separately.** A lying `Sync` makes the engine
over-promise through no fault of its own, so holding assertion (ii) against it would report the engine
for the device's behaviour. Under a lying `Sync` the engine's obligation is narrower and still worth
testing: it must lose *exactly* the un-durable suffix and recover a consistent state — assertion (i)
still applies, against the ledger's authored durability rather than against the promise. So: (i) yes,
(ii) suspended, and the run is reported in its own column rather than folded into the pass count. The
alternative — treat the lie as the engine's problem — is rejected because it makes a green result
depend on the engine compensating for a device that violated its own contract, which is not a property
any engine has.

> **B1-Q4.** "Which sanitizer lanes beyond the mandated ASan and UBSan?"

**Recommendation: add TSan; decline MSan.** The memtable is a lock-free single-writer/multi-reader
skiplist and ASan and UBSan are both blind to data races, so §6.3's correctness claim is currently
unproven by any lane. TSan is the only instrument that can prove it. MSan requires an instrumented
libc++ and its main value here — uninitialized bytes reaching the disk — is already covered by §10's
byte-digest gate for a fraction of the cost. If the answer is "ASan and UBSan only", §6.3's claim
should be downgraded in the doc to "argued, not verified" rather than left standing.

> **B1-Q5.** "What happens when the syncer falls behind and the engine-owned WAL buffer grows without
> bound?"

**Recommendation: unbounded in B1, with the policy decided at B5.** Backpressure is a property of the
engine-plus-poller pair, and B1 has no poller. The three candidates, recorded now so B5 inherits them:
return `Status::Busy` above a limit (expressible — `Apply` returns an error — but the model never
errors, so B4's rig must treat it as a legal divergence); block `Apply` above a limit (violates the
non-blocking contract); unbounded (risks OOM under a stalled syncer). I lean toward the first at B5.
The risk of deferring is recorded rather than accepted silently.

> **B1-Q6.** "Does B1 implement `DeleteRange` over the memtable, or does the whole op wait for B2?"

**Recommendation: implement it in B1, over the memtable.** Ruling 1 says iterate-and-point-delete
through B2; over a memtable-only engine that is a range scan and a set of point deletes, roughly
thirty lines. The value is that §8.1's expansion-recorded-in-the-WAL decision — the one with the real
correctness argument behind it — gets tested from B1 rather than arriving with B2's SSTable iteration
on top of it. Deferring would mean landing the interesting decision and its first exercise in the same
change.

> **B1-Q7.** "Confirm the reading of ruling 3 in §7.4: that a `Sync` which completes on the device but
> whose return the kill preempts makes the expected recovery point a **two-element known set**
> `{G_{k−1}, G_k}` rather than a single value, and that this is what 'any watermark the
> sync-completion schedule can produce' already means."

**Recommendation: confirm.** No design removes the case, the set is known in advance from the
harness's own records, and each element is compared exactly — so nothing about ruling 3's exactness is
given up. I am asking rather than assuming because the alternative reading would require the oracle to
assert a single value, and under that reading the contract would be unsatisfiable rather than merely
harder.

> **B1-Q8.** "Where does the first B1 commit land — this worktree's `engine-cpp/`, or the `rift-b`
> worktree, which is currently several commits behind `main` — and do the C++ lanes run on Linux only
> or on Linux and macOS?"

**Recommendation: `engine-cpp/` in a `rift-b` worktree rebased onto current `main`; CI on Linux for
every lane, plus a macOS `cpp-test` lane.** DESIGN-A0 §4 names `engine-cpp/` as Track B's tree and
excludes it from Track A's lanes, and `.gitignore` already carries `engine-cpp/build/`, which
anticipates the in-repo path.
Linux carries the sanitizers because they are best supported there; the macOS lane exists because that
is the development machine, and a Track B that only builds in CI is a Track B nobody runs locally.

---

## 14. Landing plan

Small diffs, each with its own gate, none of them started before the rulings.

| PR | contents | gate |
|---|---|---|
| B1.1 | CMake skeleton, static archive, `Status`, GoogleTest wiring, `make cpp-test/asan/ubsan` un-stubbed | lanes run and fail loudly on a planted failure |
| B1.2 | `Env` interface, `PosixEnv`, the raw-write seam and its short-write unit test | short-write, `EINTR` and zero-return tests green |
| B1.3 | `TestEnv`: `content`/`durable`, the fault controller, the ledger, the kill mechanism | the durability model's own tests; §10's induced failures for the ledger |
| B1.4 | the A5 scan lane and `CPP-HATCHES.txt` | planted `::open` fails the lane; an unused registry entry fails it |
| B1.5 | skiplist memtable, arena, deterministic heights, structural digest | structural digest stable; `BM6` killed |
| B1.6 | WAL writer: framing, fragmentation, groups, byte-digest test | pinned bytes; fragmentation across a block boundary |
| B1.7 | WAL reader and recovery, including the torn-tail rule and resync | §10's four recovery gates, each with its induced failure |
| B1.8 | `Open`/`Close`/`Write`/`Get`/iterator/snapshot over the memtable; `DeleteRange` (B1-Q6) | semantics suite mirroring `engine/model`'s |
| B1.9 | the kill-point sweep, the census, the exactness oracle | full sweep green; every mutant killed in budget |

---

## 15. Decision summary

Everything below awaits a ruling. Nothing is PROVISIONAL because nothing has been self-ratified.

| # | decision | recommendation |
|---|---|---|
| B1-D1 | Env surface shape | file objects, one interception choke point, runtime virtual |
| B1-D2 | what a kill leaves on disk | power-loss model only; `durable` advances only on a returned `Sync` |
| B1-D3 | WAL framing | 32 KiB blocks with fragmentation, **plus** an explicit sync-group terminator |
| B1-D4 | the torn-tail rule | resync-verified: tail only if nothing valid follows; interior corruption fails the open |
| B1-D5 | torn-`Sync` granularity and recycling | prefix in contract mode, sector-subset as characterization; never recycle WAL files |
| B1-D6 | memtable height source | derived from `fnv1a64(key)`, per DR-12's argument |
| B1-D7 | manifest in B1 | none; the log is the single authority on the watermark |
| B1-D8 | sequence space | collapse the batch; one sequence per `Apply`, identical to `engine/model`'s |
| B1-D9 | WAL buffer ownership | the engine's, so `Apply` provably makes zero Env calls |
| B1-D10 | enforcing the non-syscall half of A5 | a scan lane with a checked-in registry, `HATCHES.txt`-shaped |
| B1-D11 | kill mechanism | dead-flag in-process for the sweep, sampled real `_exit` for fidelity |
| B1-D12 | kill-point identity | global call ordinal, static labels, plus a census |
