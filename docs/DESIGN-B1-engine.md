# DESIGN-B1: Env, the WAL, the memtable, and the recovery contract

**Status:** **REVISION 3 — rulings of 2026-08-12 (both sets) applied; awaiting rulings on §13.**
Nothing is self-ratified, so nothing is marked PROVISIONAL. No C++ file is written until §13 is ruled
on and the remote gate clears.
**Phase:** B1 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Blocks:** all of Track B. **Depends on:** the `engine/` interface frozen at A0.5, which this must
meet exactly — not approximately, because B4's differential rig defines "correct" as "byte-identical
to `engine/model`".

Twelve decisions, `B1-D1`..`B1-D12`. **B1-D3 and §5.3 are the WAL record layout surface to be frozen.**

---

## 0. Ruling echo

Rulings received since the last report — Ansh, 2026-08-12, on DESIGN-B1 revision 2. Verbatim, each
followed by where it lands.

> Ruling echo verbatim: correct, and noted specifically because it is the mechanism that survives
> everything else going wrong.

> Finding 6 is the best work in this cycle and I want it named precisely: you implemented my lock
> ruling and then caught the defect my ruling introduced. A Sync holding the DB mutex across an fsync
> blocks every reader, so the lock decision bought a latency bug that nobody would have found until
> B5 benchmarks looked mysteriously bad. You did not report it as a cost of my decision and proceed,
> you added the mutex-depth guard with BM16 behind it. Record the general principle in the doc: a
> ruling that constrains a design is responsible for the failure modes it opens, and the session that
> implements it is the one positioned to see them. Keep doing this. My rulings are not exempt from the
> induced-failure discipline.

→ **§0.1**, as a standing principle with its own name, cross-referenced from every decision that came
from a ruling rather than from a recommendation.

> Finding 1: ratified, and recording that you applied A6 correctly for compaction and then violated it
> three sections later for concurrency, in the doc rather than silently, is the version of this that
> has value. Uneven application of a principle is more dangerous than not having the principle,
> because the citation makes it look considered.

→ §6.3, unchanged, now with the general form recorded in §0.1.

> Finding 2: ratified. "Required lane" and "lane that can fail" are different claims and only the
> second is worth anything, which is the same sentence as the ALIVE canary in Track A's blind lane.
> BM14 proving the TSan harness is not decoration is the right binding, and the honest bound in
> section 11 item 6, one authored interleaving rather than a search, is the claim I want stated
> everywhere it applies. Do not let a future summarizer upgrade it to "race-free".

→ §6.4: the claim lives in one named constant, pinned by a test, printed by the lane; BM23 blinds the
pin. Upgrading the sentence now requires failing a test.

> Finding 3, fragment-chain legality: ratified. The discriminator being "does anything structurally
> valid follow the failure point" rather than "did a checksum fail" is the correct generalization, and
> the six-case table with illegal transitions failing the same way a bad CRC does means it is one rule
> extended, not two rules coexisting.

→ §5.4.2, unchanged.

> Finding 4, CRC covering the length: ratified, and the divergence from LevelDB is correct and well
> argued. A corrupt length outside the CRC makes recovery consume a byte count computed from data it
> has already decided not to trust, so the failure offset is unknown and resync has no sound starting
> point. State the divergence and its reason in the doc as a deliberate departure with the upstream
> behavior named, so nobody later "fixes" us back toward LevelDB by pattern match. BM10 blinding it is
> right.

→ §5.3.3, promoted to its own subsection headed **Deliberate departure from LevelDB**, with the
upstream coverage stated exactly and the same paragraph required as the code comment on the CRC
helper.

> Finding 5, the cap ordering invariant: ratified. A WAL buffer cap below the maximum legal record
> size makes the tripwire fire on legal input, which is the same inversion you rejected torn-tail
> candidate (a) for. Asserted at construction and gated is the right shape.

→ §8.3, unchanged.

> D2, D3, D4, D5, D7, D10, D11, D12: approved as described, with the conditions below.

→ Recorded in §15 as approved.

> D8 is where I am overruling. A tripwire that makes B4 treat the run as void is an escape hatch with
> the engine's hand on the lever. If the rig voids because the engine reported kTripwire, then an
> engine that spuriously trips the cap deletes the evidence of its own bug, and the oracle is
> believing the engine's account of itself, which is the one thing ruling 4 exists to prevent. Ruling:
> a void is legitimate only when the harness independently determines, from its own record of what it
> submitted, that the op exceeded the cap. The harness knows the record size it built. If the engine
> reports a tripwire on an op the harness computes as legal, that is a divergence and the run fails.
> If the engine does NOT report a tripwire on an op the harness computes as over cap, that is also a
> divergence. Both directions asserted, both induced, sibling of the bidirectional gap assertion Track
> A recorded this week. Void runs get their own column, are never banked, and their rate is tracked
> exactly like inconclusive: a rising void rate means something is wrong, never that the sweep is fine.

→ §7.6 (the general rule this is an instance of), §8.2 (the record cap), §8.3 (the buffer cap, which
had the identical defect and is fixed by the same rule). `RunOutcome::kTripwire` is gone; `kVoid`
replaces it and is reachable only through a satisfied harness-side predicate. BM19 and BM20 induce the
two directions.

> D1, the choke point: approved in substance, rejected as a convention. "Every method's first act is
> FaultController::Intercept" is a rule that lives in review discipline, and Track A already learned
> this lesson twice this month, most recently moving hold legality out of the generator because a
> generator-side rule is not a rule. A method added during B2 that forgets the call compiles, tests
> green, and silently leaves the fault surface, and nothing anywhere reports it. Make it structural:
> the interception happens in a non-virtual layer that dispatches to the virtual implementation, so a
> new method physically cannot bypass it, or give me the equivalent mechanism if that shape does not
> fit C++ cleanly here. Then add the enforcement test that makes it checkable: every Env call site is
> reachable by the fault controller, asserted by count, with a mutant that adds a bypassing method and
> must be killed. The kill counter living in one place is the right instinct; make the one place
> unavoidable rather than customary.

→ §3.2, rewritten. The non-virtual-interface shape fits C++ cleanly and is what I am proposing, with
the 1:1:1 correspondence asserted by count, the census asserting reachability, BM17 adding a
bypassing method, and — §3.2.1 — the honest statement of the one residual bypass and what covers it.

> D6, heights from fnv1a64(key): approved, and DR-12's argument transfers correctly. Two conditions.
> Pin the mapping from hash bits to tower height with golden vectors, the same way NextTick is pinned,
> since the memtable's shape is now a pure function of the key set and any change to that mapping is
> an on-disk-adjacent behavior change that should have to fail a vector to happen. And record the
> accepted cost explicitly: a key set that maps to a degenerate tower distribution is degenerate
> permanently, on every machine, forever, with no reseed available, because reproducibility from the
> key set alone is the property we chose. Note also that the function is public knowledge, so a
> constructed key set can force pathological heights. That is a performance property and not a safety
> one, it is fine, but it should be written down rather than discovered by a fuzzer at B5. Name what
> would fix it, a per-DB salt, and why we are not doing it.

→ §6.2, with the vectors, the permanence, the adversarial construction, and the per-DB salt named as
the fix that is declined and why.

> D5's sector-subset characterization mode: approved, and it gets the identical treatment I ruled for
> Q3's lying disk, through the same mechanism rather than a parallel one. The outcome type carries the
> fact that the exactness assertion was suspended, so no summarizer can aggregate it as
> recovery-contract evidence by accident. One suppression mechanism, two injectors, not two mechanisms
> that can drift apart.

→ §7.5, restated so the one-mechanism-two-injectors property is the text rather than an inference,
with a registry of exactness-suspending injectors that both entries live in.

> Q9: run-time with those defaults is approved, with the reason improved and one condition. Your
> reason is right, a tripwire nobody has watched fire is decoration. The condition comes from Track A's
> ablation this week, which found that lowering a harness parameter did not weaken detection, it
> removed the bug from existence entirely, so results across parameter regimes were not comparable at
> all. Same hazard here. A run with non-default caps is a different regime and its results may never
> aggregate with default-cap runs. Carry the actual cap values in the run record, mark non-default runs
> mechanically, and state in the doc that a tripwire observed firing at a lowered cap is evidence the
> tripwire works and is not evidence about the 64 MiB or 256 MiB regime. Both defaults are named
> constants with the derivation written at the definition site, not in prose elsewhere: 64 MiB carries
> the roughly 1.22 million point deletes at 50-byte keys calculation, and 256 MiB carries the 2x
> invariant.

→ §8.4: the regime key, the mechanical marking, the aggregation ban, and the derivations moved to the
definition site with this doc pointing at them rather than restating them. BM18 blinds the
aggregation key.

> Q10: deferral to B5 approved, and your observation that Q5 and Q10 are the same question is correct,
> so they get one rule rather than two. Draft that rule now even though it lands at B5, because it
> belongs to B4's rig design: the model never errors, so every error the engine can return is
> classified into a closed enum, and each class carries a harness-independent predicate that says when
> that error was legitimate. An engine error with no satisfied predicate is a divergence. That is the
> same structure as the D8 ruling above, and having one shape for "the engine did something the model
> cannot" is worth more than two locally reasonable answers.

→ §7.6, drafted in full, with the B1 error classes and their predicates enumerated, and a fifth clause
added under the rule's own reasoning that the ruling did not state — see §7.6.

> GoogleTest v1.17.0 at 52eb8108c5bdec04579160ae17225d66034bd723: approved, and checking it against
> the upstream remote rather than recalling it is the right reflex, stated the right way. Two
> conditions on the vendored tree: record the provenance in the doc including how the tree can be
> verified against that commit by someone who did not do the vendoring, and confirm after vendoring
> that no lane makes a network call, tested by running the full lane set with networking disabled. The
> claim is that a stranger reproduces every number from a clean clone with one script, so the test of
> that claim is doing it under the conditions a stranger might have.

→ §9.2, with the verification recipe, the reason it is deliberately not a lane, and the
network-isolated lane run as a gate with its own induced failure (BM21).

> Items I am carrying to Track A, so you stay off their files: the section 1.1 verification-scope text
> into DESIGN-A0 section 7 and README, and the Makefile plus cpp.yml changes covering the three
> un-stubbed lanes and the new cpp-tsan. Paste-ready text in each file's voice is exactly the right
> way to hand those over. Do not touch them.

→ §12. §1.1 keeps the paste-ready text as the handoff artifact; no Track A file is touched.

> Owed next cycle: rebase rift-b onto main from 1390969 and report the resulting HEAD.

→ §12 item 4.

Rulings from the prior cycle remain in force and are reproduced in §1.2.

### 0.1 The standing principle ruled this cycle

> **A ruling that constrains a design is responsible for the failure modes it opens, and the session
> that implements it is the one positioned to see them.** Rulings are not exempt from the
> induced-failure discipline.

Ruled 2026-08-12, on the lock decision. The instance: the memtable lock (§6.3) is correct and removes
a whole class of unreproducible bug — and it opens a new one, because a `Sync` holding the DB mutex
across an fsync blocks every reader for the fsync's duration. That defect is a *consequence of the
ruling*, invisible until B5's benchmarks looked inexplicably bad, and the correct response was not to
note it as a cost and proceed but to add the guard (§8.3) and the mutant behind it (BM16).

The operational form, which is how it binds this document going forward: **every decision that arrives
as a ruling rather than as a recommendation carries an obligation to search for the failure mode it
opened, and to record either the mechanism that closes it or the reason there is none.** Three
decisions in this revision arrive that way and each carries the search:

| ruling | failure mode it opened | what closes it |
|---|---|---|
| the memtable lock (§6.3) | a `Sync` under the mutex blocks readers for an fsync | the mutex-depth guard, §8.3, BM16 |
| heights from the key, no PRNG (§6.2) | a degenerate key set is degenerate permanently, with no reseed, and can be constructed on purpose | nothing closes it; it is accepted and written down, with the declined fix named (§6.2) |
| harness-adjudicated voids (§7.6) | the harness now reimplements a size formula, so harness and engine can disagree about what the cap even means | the formula is frozen in §5.3.4 and the disagreement is bidirectionally asserted, BM19/BM20 |

The middle row is the important one: the honest outcome of this search is sometimes "nothing closes
it", and the value is that the sentence exists.

---

## 1. Scope

### 1.1 The verification-scope entry, paste-ready

Ratified 2026-08-12 and **carried to Track A by Ansh** (§12). Written as item 7 of DESIGN-A0 §7's
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

> **1.** DeleteRange is in the frozen Engine interface. This engine implements it internally as
> iterate-and-point-delete through B2; real range tombstones land before any published benchmark that
> exercises deletes.

Binds §8, including the record-size cap and the scheduled end of its cost.

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

Binds §4, §5.2, §5.4, all of §7.

> **4.** Oracle independence: the crash rig's verdicts come from its own op log, never from asking the
> engine what it believes it holds. The recorded sentence is "an oracle that interrogates the engine
> believes the lie."

Binds §7.3, §7.4, and — after the D8 overrule — §7.6, which is this ruling applied to the one place I
had let the engine keep its hand on the lever.

> **5.** Amendment A5 applies with the Env seam as this language's enforcement mechanism: every
> syscall through Env, no wall-clock reads, no ambient randomness, no floats on any path that affects
> on-disk bytes.

Binds §3 and §9.4. The Env seam enforces the syscall clause and is structurally blind to the other
three.

> **6.** Compaction policy is a DESIGN-B3 decision per Amendment A6: the simplest correct policy wins
> v1, chosen with measurement; multi-level leveled is a recorded upgrade path, not a v1 requirement.

Binds §6.3 — the lock ruling is this ruling applied one level down, to a place I had not thought to
apply it.

> **7.** Build and hygiene per CLAUDE.md: CMake producing a static archive, ASan and UBSan lanes as
> definition of done for any code that eventually lands.

Binds §9, with TSan added as required and GoogleTest vendored.

### 1.3 The two standing document requirements

> First, the Env surface is a fault-injection surface before it is a portability surface: state
> explicitly, per call, how TestEnv injects sync loss, torn writes, short writes, IO errors,
> disk-full, and a kill point, and if any call cannot express one of those, say which and why in the
> doc rather than omitting it.

§3.3, with the short-write gap written into the matrix cells rather than left blank.

> Second, the WAL section must state its torn-tail rule as a decision with rejected alternatives: what
> a partially written trailing record means at recovery, how it is distinguished from corruption in
> the middle of the log, and why that distinction is safe under the exactly-at-watermark contract.

§5.4, extended in §5.4.2 to the multi-block case as one generalized rule.

---

## 2. The engine in one paragraph

`Apply` appends a collapsed, fully expanded op list to an engine-owned memory buffer and to a
mutex-protected skiplist memtable, and makes **zero Env calls**. `Sync` — called by a different
thread — takes the buffer, writes it to the WAL as a **sync group** terminated by a `GROUP_END`
record, fsyncs, and returns the group's high sequence as the new watermark. Recovery replays whole
groups and nothing else. Everything below is the consequence of wanting that last sentence to be true
under every kill point, and of wanting every departure from it to be adjudicated by the harness rather
than reported by the engine.

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

### 3.2 B1-D1 — the shape of the surface, and making the choke point unavoidable

**Candidates.** (a) LevelDB-shaped file objects, each method calling the fault controller by
convention. (b) Flat and syscall-shaped — one `Env`, files as opaque handles. (c) **Non-virtual
interface (NVI): the public surface is non-virtual and performs the interception; the implementation
surface is private and pure-virtual.**

**Why (a) was rejected as a convention, in the ruling's words.** "Every method's first act is
`FaultController::Intercept`" lives in review discipline. A method added during B2 that forgets the
call compiles, tests green, and silently leaves the fault surface, and nothing anywhere reports it.
Track A moved hold legality out of the generator for the same reason: a generator-side rule is not a
rule.

**Recommendation: (c).** The shape fits C++ cleanly, which is the condition the ruling attached:

```cpp
class WritableFile {                      // callers hold this type and only this type
 public:
  Status Append(Slice data);              // non-virtual: Intercept(kAppend, this) then DoAppend
  Status Flush();
  Status Sync();
  Status Close();
  virtual ~WritableFile();
 private:
  virtual Status DoAppend(Slice) = 0;     // implementations override only these
  virtual Status DoFlush()       = 0;
  virtual Status DoSync()        = 0;
  virtual Status DoClose()       = 0;
};
```

A `PosixWritableFile` or a `TestWritableFile` can override only the private `Do*` methods, so **it is
not possible for an implementation to expose a public entry point that skips the interception** — the
public surface belongs to the base class, and callers never see the derived type. That is the
structural half.

The checkable half is a **1:1:1 correspondence** — one public wrapper, one private `Do*` virtual, one
`CallSite` enumerator — asserted three ways, none of them by review:

1. **Count, in the scan lane (§9.4).** The number of public non-virtual methods declared across the
   Env headers, the number of `Do*` pure virtuals, and the cardinality of `CallSite` must be equal.
   Any of the three drifting is a lane failure with the three counts printed.
2. **Reachability, by census.** A workload exercising every operation asserts that **every `CallSite`
   enumerator was observed at least once**. A `CallSite` that exists and is never reached is an
   injector nobody can fire, which is A0.7's fire-count argument at the seam instead of at the network.
3. **BM17**, which adds a public method that bypasses — implemented as a public *virtual* on the base,
   the one shape that still bypasses — and must be killed by (1).

**Rejected:** (a) — a convention, per the ruling. (b) — the engine would carry file offsets, and the
WAL's append-only property would stop being a type-level fact; NVI gives (b)'s uniformity without
that cost.

Runtime virtual dispatch rather than templates, unchanged: the differential and kill-point rigs
construct a production DB and a TestEnv DB in one process, and one virtual call per syscall is
unmeasurable against a syscall.

#### 3.2.1 The residual bypass, stated rather than implied

NVI makes bypass impossible **from an implementation**. It does not make it impossible from an edit to
the base class: adding a public *virtual* to `Env` or `WritableFile` would bypass, and that is exactly
what BM17 does. What stops it is count assertion (1), and the residual after that is that assertion
(1) could be weakened in the same diff that adds the method.

That residual is covered the way every other enforcement surface in this project is covered: the scan
lane carries a **blind patch per rule** (§9.4), so a lane that has stopped checking the count fails its
own mutation test. The claim is therefore "bypassing requires defeating two independent checks in one
diff", not "bypassing is impossible", and the second sentence would be false.

### 3.3 The fault matrix

`✓` = TestEnv injects it here. Every other cell states why not, per the standing requirement — no cell
is blank. The kill-point column is `✓` throughout by construction: §3.2's interception is in the
non-virtual layer every call passes through, so "kill at any syscall boundary" is a property of the
type system rather than of anyone's diligence.

| call | sync loss | torn write | short write | IO error | disk full | kill point |
|---|---|---|---|---|---|---|
| `Env::NewWritableFile` | — nothing synced yet | — no bytes yet | — ⁽¹⁾ | ✓ `EACCES`, `EMFILE`, `EIO` | ✓ `ENOSPC` on inode allocation | ✓ |
| `Env::NewSequentialFile` | — read path | — read path | — ⁽¹⁾ | ✓ `ENOENT`, `EIO` | — read path | ✓ |
| `Env::NewRandomAccessFile` *(declared; first used B2)* | — read path | — read path | — ⁽¹⁾ | ✓ | — read path | ✓ |
| `Env::GetChildren` | — no durable state of its own | — no bytes | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::GetFileSize` / `FileExists` | — query only | — query only | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::DeleteFile` | ✓ the unlink lands in `content` and not in `durable` until the directory is synced | — a directory entry is all-or-nothing | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::RenameFile` *(declared; first used by B2's manifest swap)* | ✓ same as above — this is the injector that finds a missing directory sync around an atomic rename | — atomic at the filesystem level, which is the guarantee we rely on | — ⁽¹⁾ | ✓ | — | ✓ |
| `Env::CreateDir` | ✓ | — no file bytes | — ⁽¹⁾ | ✓ | ✓ | ✓ |
| `Env::LockFile` / `UnlockFile` | — no durable state | — | — ⁽¹⁾ | ✓ `EAGAIN` (held), `EIO` | — | ✓ |
| `Directory::Sync` | ✓ **returns OK and does not promote directory entries to `durable`** | — a directory entry is all-or-nothing | — ⁽¹⁾ | ✓ | — | ✓ |
| `WritableFile::Append` | — buffered; nothing has reached the device | — nothing on the device to tear | — ⁽¹⁾ | ✓ | ✓ *optional eager-allocation mode; the default charges at `Flush`* | ✓ |
| `WritableFile::Flush` | — `Flush` promises visibility to other openers, not durability | ✓ a kill inside `Flush` leaves a prefix of the flushed extent in `content` | — ⁽¹⁾ | ✓ | ✓ `ENOSPC`, the default charge point | ✓ |
| `WritableFile::Sync` | ✓ **the primary site**; returns OK, `durable` not advanced — an exactness-suspending injector, §7.5 | ✓ a kill inside `Sync` promotes a **prefix** of the newly covered extent to `durable`; the sole producer of torn tails (B1-D5) | — ⁽¹⁾ | ✓ `EIO`, including report-once-then-clear | ✓ `ENOSPC` surfacing here under delayed allocation | ✓ |
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

- **No clock.** Rulings 2 and 5. A wall-clock read is unobtainable by construction, so the C++
  analogue of `clock/real.go`'s one hatched `time.Now()` is **zero** hatched calls.
- **No sleep.** A sleep is a timing dependency in a rig whose entire value is that timing is authored.
- **No thread pool.** Background work scheduled by Env would make kill points unorderable: the sweep
  identifies a point by a call ordinal, and an ordinal is meaningless if an invisible thread draws
  from the same counter. Forward binding for B3: compaction's thread is the engine's, declared and
  joined explicitly, and visible to the sweep as its own ordinal stream.
- **No logger.** Diagnostics go to a caller-supplied sink; the engine does not open files to talk
  about itself.

**`GetChildren` is this language's map range.** Directory order is filesystem-dependent and therefore
nondeterministic; recovery sorts by parsed file number before anything else. TestEnv returns children
**reverse-sorted on purpose**, so an engine that forgot to sort fails on the first test rather than on
someone else's filesystem.

---

## 4. B1-D2 — what a kill leaves on disk

**Approved 2026-08-12.** Every later contract is stated against it, so it is decided first.

**Candidates.** (a) Process-crash model: the page cache survives, `durable == content` always.
(b) Power-loss model: `durable` advances only when a covering `Sync` returns. (c) Both, selectable.

**Tradeoffs.** (a) is what `kill -9` actually does and is useless to us: under it an unsynced write is
never lost, which makes the frozen contract's entire unsynced window untestable — and it is *green*,
because an engine that never synced would pass every (a) test. (b) is strictly more adversarial and is
what the frozen contract already assumes ("buffered writes are readable and losable"); it is also the
honest model for the failure we fear, which is not a process dying but a machine losing power
mid-compaction. (c) means every contract in §7 acquires a qualifier, and qualified contracts are the
ones people misremember.

**Ruled: (b), as the single model the contract is stated against.**

```
per file:   content[]   what a reader sees now
            durable[]   what a kill would leave

Append / Flush   content grows                     durable unchanged
Sync   (clean)   durable = content                 ledger records the covered extent
Sync   (loss)    durable unchanged, returns OK     ledger records "lied"   [exactness-suspending]
Sync   (torn)    durable = content[0 : k)          ledger records k; the call never returns
kill             content = durable; all handles closed; all in-memory engine state abandoned
```

The symmetry with `engine/model` is exact and is what makes the differential comparison well-posed: the
model keeps `durable` plus an ordered `pending` list and reverts on `Crash()`; TestEnv keeps `durable`
plus the unsynced tail of `content` and reverts on kill. Two implementations of one idea, which is what
makes disagreement between them mean something.

**Rejected:** (a) — makes the unsynced window untestable and would pass an engine that never syncs.
(c) — a second model buys fidelity we do not need at the price of a qualifier on every safety sentence.

**The scoping consequence is ratified, lives in §1.1, and is being carried to DESIGN-A0 §7 by Ansh.**

---

## 5. The write-ahead log

### 5.1 B1-D3 — framing

**Approved 2026-08-12.**

**Candidates.** (a) Flat length-prefixed records. (b) LevelDB-shaped 32 KiB blocks with fragmentation
(`FULL`/`FIRST`/`MIDDLE`/`LAST`). (c) (b) plus an explicit sync-group terminator in the record stream.

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

**Ruled: (c).**

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
Go wire codec's big-endian because the WAL is never compared byte-for-byte across implementations —
only engine *state* is — and LE is a memcpy on both targets. A pinned byte-vector test freezes the
encoding regardless, so the choice cannot drift silently.

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

| question | answer |
|---|---|
| Where does the sequence number live? | In the **logical record's payload**, at payload offset 1 — `BATCH.seq` and `GROUP_END.high_seq`. **Not** in the fragment header. |
| What does the per-fragment CRC cover? | `length ‖ type ‖ payload` — every byte of the fragment except the four CRC bytes themselves. |
| Does the CRC include the length prefix? | **Yes.** See §5.3.3. |

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

#### 5.3.3 Deliberate departure from LevelDB: the CRC covers the length

Recorded as a departure with the upstream behaviour named, so that nobody later "fixes" us back toward
LevelDB by pattern match. **This paragraph is also required as the comment on the CRC helper**, so the
argument is at the code rather than only in a document.

> **Upstream (LevelDB `log_format.h` / `log_writer.cc`):** the record header is
> `crc32c : u32 ‖ length : u16 ‖ type : u8`, and the CRC is computed over **`type ‖ data` only**. The
> length is *not* covered.
>
> **Here:** the CRC is computed over **`length ‖ type ‖ payload`**. The length *is* covered.
>
> **Why.** With the length outside the CRC, a corrupted length field is not itself detected: recovery
> reads a wrong-sized payload, the CRC then fails *for the wrong reason*, and the number of bytes
> consumed before the failure is a function of data recovery has already decided not to trust. §5.4's
> discriminator is "does anything structurally valid follow the failure point", and answering that
> requires the failure point to be a **known offset**. With the length covered, a corrupt length is a
> CRC failure at a known offset, resync starts from the next block boundary, and the discrimination is
> sound. The cost is two bytes of CRC input per fragment.
>
> LevelDB can afford the weaker coverage because its reporter treats interior corruption and a torn
> tail the same way; we cannot, because §5.4 rejects exactly that conflation. **Reverting this to
> upstream's coverage silently weakens the torn-tail rule.** BM10 is the mutant that blinds it.

#### 5.3.4 Logical records, and the size formula the harness reimplements

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
  empty ones, identical to `engine/model`'s counter (§8.5).
- **`GROUP_END.batch_count`** is the number of `BATCH` records since the previous `GROUP_END`.
  Recovery checks it, which detects a dropped interior record without a whole-group checksum.
- **`DELETE_RANGE` is reserved and never written before B3.** Reserving the byte now is free; a format
  version bump at B3 is not.
- **No timestamps anywhere** — ruling 2. Not in a header, not in a record, not in a filename.

**The size formula, frozen here because §7.6 requires the harness to compute it independently:**

```
record_bytes(batch) = 1 + 8 + 4                       // kind, seq, op_count
                    + Σ over ops of  1 + 4 + |key|    // op_kind, key_len, key
                                   + (SET:          4 + |value|)
                                   + (DELETE_RANGE: 4 + |end|)
```

**The cap applies to this logical payload, not to the framed size**, so the harness's predicate is a
sum over the ops it submitted and does not have to model fragmentation. That is a deliberate choice in
favour of the harness being able to compute the quantity it adjudicates on — §0.1's middle row is the
failure mode this opened, and freezing the formula here is what closes it.

### 5.4 B1-D4 — the torn-tail rule

**Approved 2026-08-12.**

#### 5.4.1 The rule

**The question.** Recovery reads a fragment and the read fails — bad CRC, header truncated by EOF,
payload truncated by EOF, a length running past its block, or (§5.4.2) an illegal fragment transition.
What does that mean, and what does recovery do?

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
name.**

**Ruled: (d).** Normatively:

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
named, because one of them is where the argument ends.

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
   over (b), and why the response is a hard error rather than a best effort.

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
records are a routine path in B1, not an exotic one. The rule above covers them without a special case,
once "structurally valid" is understood to include chain legality.

**The chain is a two-state machine, and its transitions are part of the frozen format:**

```
OUTSIDE  --FULL-->   OUTSIDE     (a complete single-fragment record)
OUTSIDE  --FIRST-->  INSIDE
INSIDE   --MIDDLE--> INSIDE
INSIDE   --LAST-->   OUTSIDE     (a complete multi-fragment record)

every other transition is ILLEGAL:
   OUTSIDE --MIDDLE-->  |  OUTSIDE --LAST-->  |  INSIDE --FULL-->  |  INSIDE --FIRST-->
```

An illegal transition is a read failure of the same kind as a bad CRC and feeds the same rule.

| what the kill left | what recovery sees | classification | why it is right |
|---|---|---|---|
| `FIRST, MIDDLE`, then EOF | valid chain, `INSIDE` at EOF | **torn tail** — discard the incomplete record | prefix truncation; nothing can follow, and by step 1 the whole record is past the last durable `GROUP_END` |
| `FIRST, MIDDLE, <torn MIDDLE>` | CRC failure while `INSIDE` | **torn tail** | identical to the single-fragment torn case; the failure is at a known offset because the length is inside the CRC (§5.3.3) |
| `FIRST`, then a block of zeros, then EOF | type `0` at the next fragment | **torn tail** | zeros are unambiguous by §5.3.1's two reservations |
| `FIRST`, garbage block, then a **valid `FULL` with a higher sequence** | CRC failure while `INSIDE`, then resync finds a structurally valid record | **interior corruption — open fails** | cannot arise from prefix truncation; step 1's premise is false, so truncation is unsafe |
| `FIRST` immediately followed by another `FIRST`, both CRC-valid | illegal transition `INSIDE --FIRST-->` | **interior corruption — open fails** | no crash produces it; it is a writer bug or corruption that landed on a fragment boundary |
| a bare `MIDDLE` or `LAST` found during resync | illegal start | **not a resync candidate** | which is why the resync predicate requires `FULL` or `FIRST`; accepting a bare `MIDDLE` would let garbage masquerade as interior corruption and manufacture a refused open |

The discriminator is therefore **not** "did a checksum fail" — it is "does anything structurally valid
follow the failure point", where structural validity for a multi-fragment record includes the chain. A
torn multi-block record at the tail is distinguishable from interior corruption by exactly the test that
distinguishes a torn single-block one, and BM11 proves the chain half is actually checked.

### 5.5 B1-D5 — torn-`Sync` granularity, and no recycling

**Approved 2026-08-12, with the characterization mode routed through §7.5's single mechanism.**

**Torn-sync granularity.** Candidates: (a) **prefix** — a kill inside `Sync` promotes `content[0:k)`;
(b) **sector-subset** — an arbitrary set of 4 KiB sectors of the newly covered extent is promoted.

**Ruled: (a) as the contract model, (b) as an exactness-suspending injector.** (b) can promote a
`GROUP_END` while leaving an earlier record in the same group torn, which is a device that violated
fsync's own ordering guarantee. Against such a device the engine cannot be held to exactness, and
holding it there anyway would report the engine for the disk's crime. Under (b) the engine's obligation
is narrower and still real — **detect and refuse**, which §5.4(d) already does.

**(b) is registered in the same exactness-suspending injector registry as the lying `Sync` (§7.5), not
in a parallel mechanism.** One registry, one outcome kind, two injectors — because two mechanisms that
mean the same thing drift apart, and the one that drifts is the one nobody is looking at.

**WAL files are never recycled in B1.** Recycling (RocksDB's `recycle_log_file_num`) saves a file
creation and a directory fsync per rotation and pays by leaving stale, CRC-valid records past the tail —
which breaks §5.4's false-positive analysis outright and forces a per-file nonce into every CRC plus a
per-fragment sequence (§5.3.2). A deliberate non-goal with its full cost, its upgrade path, and the
condition that would earn it reconsideration: a *measured* rotation-rate problem at I2, not before.

---

## 6. The memtable

### 6.1 B1-D6a — structure

Arena-allocated skiplist, `kMaxHeight = 12`, LevelDB's shape. Nodes and key bytes come from a bump
allocator and the whole arena dies with the memtable: exact memory accounting, which B2's flush
threshold needs and a general allocator cannot provide cheaply, and no per-node free path to get wrong
under a kill. **Nothing may depend on an address** — no pointer-keyed containers, no address-ordered
anything — which is the C++ restatement of the map-iteration rule and is checked by §9.4's scan.

### 6.2 B1-D6b — the height source, its golden vectors, and the cost we are accepting

**Approved 2026-08-12 with two conditions.**

**Candidates.** (a) A PRNG, as LevelDB does. (b) Derived from the key:
`height = 1 + min(ntz(fnv1a64(key)) / 2, kMaxHeight − 1)`. (c) Derived from an insertion ordinal.

**Ruled: (b).** DR-12's argument transfers: `engine/model`'s treap priorities come from `fnv1a64(key)`
and not from RNG so that engine internals stay decoupled from any random stream. Ruling 5 makes it
mandatory here, and (b) buys more than determinism — the same key always gets the same height, so the
structure is a pure function of the key *set*, and a shape-dependent bug reproduces from the workload
alone.

**Rejected:** (a) — banned by ruling 5, and makes any shape-dependent bug irreproducible. (c) —
reproducible only under identical insertion order, the case we least need.

**Condition 1 — golden vectors, pinned the way `NextTick` is.** `TestHeightVectors` pins
`(key → fnv1a64 → height)` for a fixed key list covering every reachable height, every tower boundary,
the empty key, and a key whose hash has all low bits set. The memtable's shape is now a pure function
of the key set, so **any change to the mapping is an on-disk-adjacent behaviour change and must fail a
vector to happen.** Per A0's rule about signed packages, the vectors never change in the same commit as
the code they pin. BM22 shifts the mapping (`/2` → `/3`) and must be killed by the vectors.

**Condition 2 — the accepted cost, written down rather than discovered at B5.**

- **A degenerate key set is degenerate permanently.** If a key set maps to a pathological tower
  distribution, it does so on every machine, in every run, forever. There is **no reseed**, because
  reproducibility from the key set alone is the property we chose and a reseed is exactly what would
  destroy it. This is the direct cost of the ruling and it is accepted, not mitigated.
- **The function is public knowledge, so the degenerate set can be constructed.** `fnv1a64` and the
  tower mapping are in this document. An adversary who chooses keys — and in a KV database, clients
  choose keys — can force towers of height 1 and turn the skiplist's expected `O(log n)` into `O(n)`.
- **This is a performance property, not a safety one.** No invariant depends on tower height; ordering,
  visibility, snapshots and recovery are all height-independent. The consequence of the attack is a
  slow memtable, not a wrong one.
- **What would fix it, and why we are not doing it:** a **per-DB salt** mixed into the hash. It defeats
  the constructed key set, and it costs exactly the property we bought — the shape would become a
  function of `(key set, salt)` rather than of the key set, so the same keys in a different DB build a
  different structure and a shape-dependent bug no longer reproduces from the workload alone. It would
  also have to be recorded and replayed, which means the corpus carries one more thing. **Declined for
  v1.** The upgrade path, if a fuzzer or a real workload ever makes it matter: derive the salt from the
  DB's creation file number and record it in B2's manifest, which restores adversary-resistance at the
  cost of cross-DB shape reproducibility — a trade worth making only once there is a measurement.

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

**Ruled by Ansh, 2026-08-12.** The memtable is protected by the DB mutex.

**The concurrency contract B1 must meet**, since it is what made the question look open: the frozen
interface has `Apply` running on the node loop while a separate thread owns the blocking `Sync`
(DR-11). So the engine **is** called from two threads and must be internally synchronized. What does
*not* follow, and what I wrongly treated as following, is that the memtable needs to be lock-free.

**Rejected: a lock-free single-writer/multi-reader skiplist** (LevelDB's, with
release-store/acquire-load on next pointers). Amendment A6 governs and I applied it to compaction policy
while missing it here: the simplest correct thing wins v1 and the faster thing is a recorded upgrade
path. B1 has no authorized concurrency requirement; `Apply` is non-blocking **by contract, not by
parallelism** — §8.3's invariant is that it makes no Env call, which a mutex does not threaten — and
the syncer and poller are B5's. A lock-free structure spends this project's scarcest resource, C++
correctness under fault injection, to buy throughput no measurement has asked for, and its failure mode
is the one the project exists to eliminate: a bug that appears on one machine, at one core count, one
run in ten thousand, and does not replay.

**The measurement that would reopen it**, recorded so the upgrade path is a threshold rather than a
mood: **B5's standalone numbers showing the memtable mutex is the bottleneck** — a `readrandom` mix
whose throughput scales sublinearly with reader threads while the same workload against `engine/model`
does not, with lock contention attributed by profile rather than inferred. Absent that number, the lock
stays.

**Per §0.1, the failure mode this ruling opened** is a `Sync` holding the mutex across an fsync,
blocking every reader for its duration. §8.3's mutex-depth guard closes it and BM16 proves the guard
fires.

### 6.4 The concurrency claim, and the one place it lives

TSan is required regardless of the lock, because a locked structure with a wrong lock is still a race.
B1's engine is single-threaded until somebody calls it from two threads, so **the TSan lane runs a
dedicated multi-threaded harness test** — `Apply`/`Get` on one thread, `Sync` on another, for a fixed op
count — rather than the ordinary unit suite. A TSan lane over single-threaded tests is a green lane that
proves nothing; BM14 exists to prove this one is not that.

**The claim the lane supports is bounded, and the bound is mechanical.** Ruled: do not let a future
summarizer upgrade it to "race-free". So the claim lives in exactly one constant, is printed by the
lane, and is pinned by a test:

```cpp
// The ONLY sanctioned wording for what the TSan lane establishes.
inline constexpr char kConcurrencyClaim[] =
    "TSan observed no data race across one authored interleaving pattern "
    "(Apply/Get against Sync); this is not a proof of race-freedom.";
```

`TestConcurrencyClaimWording` pins the string. **Strengthening the sentence therefore requires failing a
test, and the rule is that the harness must be strengthened in the same diff that strengthens the
claim** — a systematic interleaving search would earn a stronger sentence; nothing else would. BM23
edits the constant toward "race-free" and must be killed. §11 item 6 is the same bound stated as an
idealization.

### 6.5 Unbounded growth in B1

No flush until B2, so the memtable grows without bound and old WALs are never deleted. B1's tests are
sized accordingly, and the constraint is recorded because it is also what makes §7.2's
gapless-file-number check sound.

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
2. `GetChildren`, parse `NNNNNN.log`, **sort by parsed number** — never directory order, never mtime.
3. Assert numbering is **gapless**. In B1 no file is ever deleted, so a gap means a lost directory
   entry — the missing-`Directory::Sync` bug — and it is a hard error. This is what gives the
   directory-sync kill point teeth; without it the loss is silent.
4. Replay each file in order into a fresh memtable, committing group by group (§5.4).
5. `recovered_seq` = the highest committed `GROUP_END.high_seq`. Assert monotone across files.
6. Create WAL `max+1` and **`Directory::Sync` before `Open` returns**.
7. `DurableSeq = VisibleSeq = recovered_seq`.

**B1-D7 — no manifest in B1. Approved 2026-08-12.** Candidates: (a) none, file numbers from
`max existing + 1`; (b) a minimal manifest recording the WAL number and durable sequence; (c) build B2's
MANIFEST early. **Ruled: (a).** There are no SSTables, so no version state to be inconsistent with, and
a manifest recording a durable sequence would be a **second authority on the watermark that could
disagree with the log** — the exact shape of the A0.5 bug, rebuilt in C++. The single source of truth is
the log: `recovered_seq` is a fact about bytes, derived, never stored. Forward binding to B2: **the
manifest may record which files exist; it may never record a durable sequence the WAL cannot
independently justify**, and `max+1` numbering stops being safe the moment B2 deletes a flushed WAL,
which is where the file-number counter moves into the manifest. **Rejected:** (b) — a second authority.
(c) — B2 scope.

### 7.3 The oracle, written so it never asks the engine anything

The rig's inputs are **its own call log** — every `Write`, every `Sync`, in issue order, with return
values — and **TestEnv's fault ledger** — for each `Sync`, whether durability was applied fully, not at
all, or as a prefix. Both are harness-side. The engine's on-disk state is never parsed by the oracle,
and the engine is never asked what it believes it holds.

From the call log alone the rig knows the group decomposition: group *k* is the set of `Write`s between
the start of `Sync` *k−1* and the start of `Sync` *k*, with high sequence `G_k`. **No byte-level parsing
is required**, which is the point — an oracle that parsed the WAL would be a second implementation of
the reader, and a second implementation can be wrong in the same direction as the first.

Let `W` be the highest watermark the engine ever *returned* to the rig before the kill. The oracle
asserts two things:

- **(i) Exactness.** `recovered_state == model_state_at(R)`, byte for byte over a full iteration, where
  `R` is the group boundary TestEnv's ledger justifies (§7.4).
- **(ii) No over-promise.** `W ≤ R`. An engine that advanced its watermark before the data was durable
  fails here, and this is the assertion the whole rig exists for.

Over-reporting fails (ii); under-reporting fails (i), because the ledger justifies more than the promise
did. The `Sync` return value appears only in (ii), as *the promise being held to* — the "client-observed
response" A0's oracle rule permits — never as the answer being checked.

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
   only engine-facing inputs are the iterator it compares and the `Sync` return it holds the engine to.
   §7.2's B1-D7 removes the manifest as a possible source by not having one.
2. **Each element is compared exactly, and the verdict names which element matched.** Mechanism: the
   verdict is `{matched: G_{k−1} | G_k, seq: <n>, compared: <key count>}`, not a boolean. A verdict that
   cannot say which element it matched is a failure of the oracle, not a pass of the engine.
3. **Both elements are individually induced by tests**, because *a two-element set where only one
   element has ever been observed is a one-element contract with a spare excuse attached.* Mechanism:
   two named tests in §10, plus a sweep-level assertion that **across the full kill-point sweep, both
   elements were observed at least once**, so the pair cannot silently degenerate into one as the code
   moves. BM15 blinds the set-width check.

### 7.5 `RunOutcome`, and the single exactness-suspending injector registry

Ruled: a run with an exactness-suspending injector enabled may never be reported as evidence for the
recovery contract, in any column, ledger, or README sentence, and the suppression must be mechanical.
Ruled further: the sector-subset torn `Sync` gets the **identical** treatment through the **same**
mechanism — one suppression mechanism, two injectors, not two mechanisms that can drift apart.

**The registry.** Exactly one list, and both entries live in it:

```cpp
// Injectors that suspend assertion (ii). Adding one here is the ONLY way to
// suspend it; there is no per-injector flag anywhere else.
enum class ExactnessSuspendingInjector { kLyingSync, kSectorSubsetTornSync };
```

Enabling any member sets the run's outcome to `kCharacterizationOnly` at the point of enabling, not at
the point of reporting, so a run cannot be enabled into characterization mode and then summarized as
something else.

**The outcome type.** This is A0.6's `Outcome` enum in a second setting, and the same reasoning about
closed enums applies.

```cpp
enum class RunOutcome {          // closed; no default arm anywhere, enforced by -Werror=switch
  kContractPass,                 // (i) and (ii) both asserted, both held
  kContractViolation,            // (i) or (ii) failed -- a bug
  kCharacterizationOnly,         // an ExactnessSuspendingInjector ran: (ii) was SUSPENDED
  kInconclusive,                 // the checks did not complete
  kVoid,                         // §7.6: an engine error whose HARNESS-SIDE predicate was satisfied
};

bool CountsAsRecoveryEvidence(RunOutcome);   // the ONLY place this policy lives
```

| kind | when | evidence? |
|---|---|---|
| `kContractPass` | (i) and (ii) asserted and held | **yes — only this one** |
| `kContractViolation` | either assertion failed | no; a bug with a kill point |
| `kCharacterizationOnly` | a registered exactness-suspending injector ran, so **the contract was not under test** | no; data about behaviour |
| `kInconclusive` | a check did not complete | no — Amendment A4's shape, one language over |
| `kVoid` | §7.6's adjudication found a legitimate engine error | no; tracked like inconclusive |

Three things make the suppression mechanical rather than remembered. `CountsAsRecoveryEvidence` is the
single place the policy lives, so adding a kind forces a decision *there* rather than defaulting to
"sure, count it" at whichever summarizer forgot. `-Werror=switch` over a scoped enum with **no
`default:` arm** is the C++ compiler implementing A0.6's `exhaustive` rule for free, and §9.4's scan
bans `default:` arms over `RunOutcome` so nobody buys the omission back. And the ledger columns are
literally headed **`characterization (not evidence)`** and **`void (not evidence)`**, which cannot be
misread by someone skimming. BM13 blinds the policy method.

### 7.6 The engine-error classification rule

**Drafted now, lands at B5, binding on B4's rig design.** It is the general form of the D8 overrule and
of Q5/Q10, which are one question and get one rule.

`engine/model` never errors. Every error the C++ engine can return is therefore a place where the two
engines can legally differ, and every such place must be **closed and adjudicated harness-side**. The
failure this prevents, in the ruling's words: *"an engine that spuriously trips the cap deletes the
evidence of its own bug, and the oracle is believing the engine's account of itself."*

1. **`Status::Code` is a closed enum**, `-Werror=switch`, no `default:` arm.
2. **Each code carries a harness-independent predicate** — a function of the harness's own submission
   log, its reference state, and TestEnv's ledger, and **never of the engine's report**.
3. **An engine error whose predicate is not satisfied is a divergence and fails the run.**
4. **A satisfied predicate with no error returned is also a divergence and fails the run.**
5. **A satisfied predicate with the matching error makes the operation `kVoid`**: its own column, never
   banked, rate tracked exactly like inconclusive. *A rising void rate means something is wrong, never
   that the sweep is fine.*

**A sixth clause, added under the rule's own reasoning rather than from the ruling**, because clause 4
is not free: a predicate that cannot be stated in both directions means the code is too coarse and must
be split. **That bidirectionality is the acceptance test for adding a `Status::Code` at all.** Without
it, someone adds a code whose predicate is one-directional, clause 4 becomes vacuous for that code, and
the escape hatch reopens under a new name.

The B1 codes and their predicates:

| `Status::Code` | harness-side predicate (never consults the engine) | bidirectional? |
|---|---|---|
| `kOk` | — | — |
| `kRecordTooLarge` | `record_bytes(submitted batch)` by §5.3.4's frozen formula, with `DeleteRange` expanded against the harness's **own** reference key set, exceeds `max_record_bytes` | yes |
| `kWalBufferFull` | Σ `record_bytes` of batches submitted since the last `Sync` **start**, exceeds `wal_buffer_bytes` | yes |
| `kIoError` | TestEnv's ledger shows an injected IO error on a call made during this operation | yes |
| `kDiskFull` | TestEnv's quota ledger shows the quota exhausted during this operation | yes |
| `kCorruption` | the harness planted corruption in a region §5.4 requires recovery to read | yes — the region qualifier is what makes the converse statable, and is the sixth clause doing its job |
| `kKilled` | the fault controller's dead flag is set | yes |
| `kInvalidArgument` | the harness deliberately submitted an argument outside the frozen contract | yes |
| `kBusy` | **B5** — the poller-backpressure policy; predicate defined with the policy, subject to clause 6 | to be established at B5 |

`kNotFound` is deliberately absent: it is the frozen interface's `ErrNotFound`, a normal result, and
`engine/model` produces it too — so it is not a place the engines can legally differ and it does not
belong in this table.

---

## 8. `DeleteRange` through B2: expansion, the caps, and their adjudication

### 8.1 The expansion happens at `Apply` and the WAL records the expansion

**Approved 2026-08-12.** Iterate-and-point-delete must read current state to find the keys to delete,
and `Apply` is what makes the deletion visible — so the expansion happens at `Apply`. What goes in the
log is the consequential part.

**If the WAL recorded the raw `DeleteRange`, recovery would have to expand it again — against a state
recovery is still in the middle of rebuilding.** The expansion is a function of the state at the time it
runs, so replay-time expansion is correct only if that state provably equals the state at original
`Apply` time. It probably does today, for a reason that depends on the WAL's start point coinciding
exactly with the flush boundary — a property B2 is about to start changing. That is correctness by
argument, and the argument has a moving premise.

**Recording the post-expansion op list makes it correctness by construction.** Recovery replays point
deletes; there is nothing left to compute; the circularity is gone.

Intra-batch semantics come out right: at the `DeleteRange` op, the expansion covers the current state
*and* keys written earlier in the same batch, and a `Set` after it in the same batch re-adds the key,
which is the model's rule reproduced.

### 8.2 B1-D8 — the record-size cap. **OVERRULED on adjudication; the harness decides, not the engine.**

`DeleteRange(nil, nil)` — the clear half of snapshot application's clear-then-ingest, the case Amendment
A3 was ruled for — expands to one point delete per live key in a single record, and batches are atomic
so it cannot be chunked.

**The cap:** `kMaxRecordBytes`, default 64 MiB, run-time configurable (§8.4). Exceeding it returns
`Status::kRecordTooLarge` and **applies nothing, atomically**.

**What was overruled.** Revision 2 said the rig treats such a run as void because the engine reported a
tripwire. That is an escape hatch with the engine's hand on the lever: an engine that spuriously trips
the cap would delete the evidence of its own bug, and the oracle would be believing the engine's account
of itself — the one thing ruling 4 exists to prevent.

**The rule, per §7.6.** The harness computes `record_bytes` itself, from its own record of the batch it
submitted, using §5.3.4's frozen formula — and for `DeleteRange`, expanding against **its own reference
key set**, which it has because it is driving `engine/model` in parallel. Then:

| harness computes | engine reports | verdict |
|---|---|---|
| ≤ cap | no error | normal run; assertions proceed |
| ≤ cap | `kRecordTooLarge` | **divergence — the run fails.** The engine tripped on legal input. |
| > cap | no error | **divergence — the run fails.** The engine accepted an over-cap record. |
| > cap | `kRecordTooLarge` | `kVoid` — own column, never banked, rate tracked like inconclusive |

Both divergence directions are asserted and both are induced: **BM19** makes the engine trip on legal
input, **BM20** makes it accept an over-cap record. Sibling of the bidirectional gap assertion Track A
recorded this week, in the ruling's own framing.

**The failure mode this opened, per §0.1:** the harness now reimplements a size formula, so harness and
engine can disagree about what the cap *means* — and a disagreement in the formula would present as a
divergence in the engine. What closes it: the formula is frozen in §5.3.4, the cap applies to the
logical payload rather than the framed size so fragmentation never enters it, and the two directions
above catch a formula drift in whichever direction it goes.

### 8.3 B1-D9 — the WAL buffer: ownership, the cap, and the assertions

**Ownership.** LevelDB's `WritableFile::Append` flushes to the OS when its internal buffer fills, so a
write can perform I/O at an unpredictable moment; "unpredictable moment" is not a way to satisfy "never
blocks on I/O". **The WAL buffer is therefore the engine's own memory.** `Apply` appends to it and makes
zero Env calls. The syncer takes the DB mutex only long enough to swap in a fresh buffer, then performs
`Append` + `Sync` on the old one with the mutex released.

**Two assertions, not one sentence.** TestEnv keeps a per-thread Env-call counter, and:

1. **The counter does not move across `Apply`.** BM9 blinds it.
2. **The DB mutex is never held across an Env call.** This is §0.1's first row: it is what makes
   B1-D6c's lock safe under a slow `Sync`, because without it a 10 ms fsync would block every reader for
   10 ms and the lock ruling would have bought a latency bug. Mechanism: a debug-build guard object
   recording mutex depth on the current thread, checked in the non-virtual interception layer (§3.2) —
   which is the same choke point, so the guard cannot be bypassed for the same reason the fault
   controller cannot. BM16 blinds it.

**The cap.** `kWalBufferBytes`, default 256 MiB. Unbounded growth in a fault-injected harness means an
OOM kill, which is the worst possible failure signal because it destroys the run that would have
explained it. Exceeding it returns `Status::kWalBufferFull`.

**Adjudicated exactly like the record cap, by §7.6, and for the same reason.** The harness knows what it
submitted since the last `Sync` start, so it computes the occupancy itself; engine-reports-full on a
legal occupancy and engine-accepts-past-full are both divergences that fail the run. This had the
identical defect the D8 overrule found and is fixed by the same rule rather than by a parallel one.

**The ordering invariant, asserted at construction:** `kWalBufferBytes ≥ 2 × kMaxRecordBytes`. A cap
below the maximum legal record would make the tripwire fire on legal input, which is the inversion §5.4
rejected candidate (a) for. The default pair satisfies it with 4× margin.

The cap is a tripwire, not a policy. `Status::kBusy` as the *policy* remains the leaning for B5, and
§7.6 clause 6 is now its acceptance test: it does not land until its predicate is statable in both
directions.

### 8.4 Cap regimes: runs at non-default caps never aggregate with default-cap runs

**Ruled 2026-08-12**, from Track A's ablation this week, which found that lowering a harness parameter
did not weaken detection — it removed the bug from existence entirely, so results across parameter
regimes were not comparable at all. The same hazard applies here, and the mechanism is the same shape as
§7.5's.

- **The defaults are named constants with the derivation at the definition site**, not in prose here:
  `kMaxRecordBytes` carries the ≈1.22 M-point-deletes-at-50-byte-keys calculation in its own comment,
  and `kWalBufferBytes` carries the `≥ 2 × kMaxRecordBytes` invariant in its own comment. This document
  points at them and does not restate the arithmetic, so there is one place to correct.
- **Every run record carries the actual cap values**, and a `regime` field computed as `default` if and
  only if both equal the named constants.
- **Aggregation is keyed on regime.** A summarizer that combines rows of differing regime fails a test.
  BM18 removes the regime from the aggregation key and must be killed.
- **Stated so nobody has to infer it:** *a tripwire observed firing at a lowered cap is evidence that
  the tripwire works. It is not evidence about the 64 MiB or 256 MiB regime, and its run may not be
  banked with runs that are.*

Run-time configurability exists precisely so the sweep can set the caps low and watch the tripwire fire
— a tripwire nobody has watched fire is the decoration this project rejects everywhere else — and this
section is what stops that convenience from contaminating the numbers it makes reachable.

### 8.5 B1-D10 — one sequence per batch, collapsed, sharing the model's sequence space

**Approved 2026-08-12.**

**Candidates.** (a) Collapse the batch to at most one op per key before insertion; one internal sequence
per batch, equal to `engine.SeqNum`. (b) LevelDB's scheme: the internal sequence advances per *op* and
`engine.SeqNum` is the batch's last internal sequence. (c) Pack `(batch_seq, op_index)` into the internal
key.

**Ruled: (a).** Under (b) the C++ engine's sequences jump (1, 5, 9, …) while `engine/model`'s advance by
one per `Apply`. That is contract-legal — the frozen interface requires only monotonicity — and still
wrong, because B4's rig would then need a per-engine map from operation index to sequence in order to
sync both engines "to the same point", and a rig that needs a translation table is a rig with a place to
be wrong. (c) keeps the spaces aligned but widens every internal key for a case (a) removes.

(a) costs a sort of the batch's ops by key — which §8.1's expansion already requires a pass over — and it
makes an invariant assertable: **no two memtable entries ever share a `(user_key, seq)` pair.**

**Rejected:** (b) — divergent sequence spaces put a translation table inside B4's oracle. (c) — wider
internal keys to preserve a distinction (a) removes.

### 8.6 The scheduled end of this cost

Ruling 1's real range tombstones in B3 retire all of it: the record becomes O(1) in the range rather than
O(keys), the multi-block path stops being routine, and the caps stop being reachable by a legal
`DeleteRange`. **The fragmentation path is therefore a known-temporary consequence with a scheduled end
rather than a permanent property of the format** — and it is why `DELETE_RANGE` is a reserved op kind
from day one, so B3 writes a tombstone without a format version bump.

What does *not* retire: §5.4.2's chain rule and the fragmentation code, since a large batch can still
exceed a block. They become a rare path instead of a routine one, which is an argument for keeping them
exercised by a dedicated test after B3 rather than relying on `DeleteRange` to exercise them.

---

## 9. Build, toolchain, lanes, and the half of A5 the Env seam cannot see

### 9.1 Toolchain — ruled

C++17. `-fno-exceptions`, `-fno-rtti`, `-Wall -Wextra -Werror`, and `-Werror=switch` (which is §7.5's
and §7.6's exhaustiveness rule, already in the compiler). `Status` return codes throughout. clang and gcc
both pinned in CI, for the same reason DR-26 pinned the Go toolchain: a version should be a decision,
not an accident of what is installed.

`-fno-exceptions` is load-bearing rather than stylistic: **no exception may cross into Go, ever**, and
the flag makes that structural instead of a review habit. It also rules out the obvious in-process kill
mechanism, which §9.5 addresses.

### 9.2 GoogleTest — vendored, with verifiable provenance and an offline gate

**Ruled: vendor it.** FetchContent declined — a build step that reaches the network fails in exactly the
situation where "every number reproduces from a clean clone by one script" matters most, which is a
stranger checking our work.

**Provenance.**

| field | value |
|---|---|
| upstream | `https://github.com/google/googletest` |
| tag | `v1.17.0` |
| commit | `52eb8108c5bdec04579160ae17225d66034bd723` |
| vendored at | `third_party/googletest/` |
| content | **the complete upstream tree at that commit, unmodified** |
| recorded in | `third_party/googletest/VERSION` — tag, commit, tree hash, date of vendoring |

The tree is vendored **whole and unmodified on purpose**: any pruning would make the verification below
a diff against a subset rather than an equality, and a verification that requires judgement is one
people skip.

**How a stranger verifies it**, which is the condition attached to the ruling — someone who did not do
the vendoring can confirm the tree is that commit:

```sh
# 1. What is in the repo, computed without network:
git -C third_party/googletest-verify init -q .          # or: git hash-object over the tree
#    the recorded tree hash is in third_party/googletest/VERSION

# 2. What upstream says that commit's tree is:
git init -q /tmp/gt && git -C /tmp/gt remote add origin https://github.com/google/googletest
git -C /tmp/gt fetch -q --depth 1 origin 52eb8108c5bdec04579160ae17225d66034bd723
git -C /tmp/gt cat-file -p 52eb8108c5bdec04579160ae17225d66034bd723^{tree} | head -1

# 3. The two tree hashes must be equal.
```

`scripts/verify-vendored-gtest.sh` automates it. **It is deliberately not a lane**, because it requires
network access, and putting it in a lane would reintroduce exactly the dependency the ruling removed. It
is a one-time provenance check, run on purpose, by whoever wants to check our work. The *lane* checks
the vendored tree against the hash recorded in `VERSION`, entirely offline, so a local edit to a
vendored dependency is a test failure rather than a mystery.

**The offline gate.** After vendoring, `make cpp-ci` runs the full lane set **with networking disabled**
(`unshare -rn` on the Linux CI runner) and must pass. This is the test of the claim under the conditions
a stranger might have, rather than under ours. Induced failure: **BM21** adds a `FetchContent_Declare`
to the CMake build, which passes with networking and must fail under isolation.

### 9.3 Lanes

`make cpp-test`, `make cpp-asan`, `make cpp-ubsan` un-stub with B1; **`make cpp-tsan` is added as
required** and runs §6.4's dedicated multi-threaded harness rather than the unit suite; `make cpp-ci`
adds the network-isolated run of the whole set.

MSan remains declined: it needs an instrumented libc++, and its value here — uninitialized bytes reaching
the disk — is covered by §10's byte-digest gate at a fraction of the cost.

Platform matrix: **Linux for every lane** (best sanitizer support), **plus a macOS `cpp-test` lane**. The
macOS lane is not convenience. It is **our first cross-platform evidence for the Env seam** — the first
time `PosixEnv` runs against a kernel whose `fsync`, `rename` and directory semantics differ from the one
it was written on — in the same spirit as the cross-architecture datapoint Track A is waiting on CI for.
It also means Track B builds on the development machine, and a track that only builds in CI is a track
nobody runs locally.

The `Makefile` and `.github/workflows/cpp.yml` changes are being carried by Ansh (§12).

### 9.4 B1-D11 — enforcing the non-syscall half of ruling 5

**Approved 2026-08-12.** The Env seam cannot see a `double`, a `rand()`, a `steady_clock::now()`, or a
raw `::open` that bypassed it. Something else has to.

**Candidates.** (a) A source-scan lane with a checked-in exception registry. (b) clang-tidy with custom
matchers. (c) The Env seam plus review.

**Ruled: (a) now, (b) as an upgrade if the scan gets noisy.** A scan over `engine-cpp/src` banning
`<random>`, `rand(`, `<chrono>`, `time(`, `clock(`, `float`, `double`, `getenv`, `<fstream>`, `default:`
over `RunOutcome` and `Status::Code`, and direct `open(`/`write(`/`fsync(`/`rename(` outside
`env/posix/`, with a `CPP-HATCHES.txt` registry diffed against the tree by the lane —
`HATCHES.txt`'s structure one language over, **including the rule that an unused entry fails**, because
a drifted hatch means something is unguarded while its author believes otherwise.

The scan also carries the three assertions §3.2 depends on: the public-method / `Do*`-virtual /
`CallSite` count equality, and the address-dependence ban of §6.1.

**Blind patches, one per rule** (DR-27's shape), so a lane that has stopped checking something fails its
own mutation test. §3.2.1's residual bypass is covered by exactly this.

**Rejected:** (c) — DR-16's argument verbatim: the answer to "how do you know a `steady_clock::now()`
didn't sneak in?" must be a build failure, not a promise. (b) as the first step — a real clang-tidy check
is a day of work and a toolchain dependency for a job a grep does today; it earns its place when the
registry starts carrying arguments a grep cannot express.

### 9.5 B1-D12 — how a kill point kills, and how it is identified

**Approved 2026-08-12.**

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

**Ruled: (c)** — (b) for the sweep, (a) for a stated sample (every 32nd point, plus every point that has
ever produced a failure), so the blind spot is measured rather than assumed.

**The identity.** Ruled: **a global Env-call ordinal, with static labels, plus a census.** The ordinal is
complete by construction — nothing to annotate, therefore nothing to forget — and a static label at each
call site turns "kill 47 failed" into "kill 47 = `Sync(000001.log)`, group 12, after 3 appends", which is
a bug report. The **kill-point census** records how many points the sweep visited, per call kind, and
surfaces any change. A new Env call nobody swept is otherwise invisible; this is A0.6's step census in a
second setting, and it composes with §3.2's `CallSite` census — one proves every call site is *reachable*
by the controller, the other proves every one was *visited* by the sweep.

---

## 10. How B1 proves itself

### 10.1 Mutant catalogue

Per Amendment A2, stored as patches applied to a scratch tree (DR-27) — and not only for consistency:
BM6 includes `<random>` and BM14 removes a lock, both of which §9.4's scan rejects, so they cannot exist
as committed files for the same reason M4 and M5 cannot.

Budgets are in **kill points**, the C++ analogue of seeds-to-detection; wall-time-to-detection is
recorded alongside, per A2. A mutant surviving its budget means the rig is too weak and B1 is not done,
regardless of what the clean runs say.

**Engine mutants.**

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
| `BM10-crc-excludes-length` | revert to LevelDB's `type ‖ payload` coverage | corrupt-length test | immediate |
| `BM11-accept-illegal-chain` | accept `FIRST→FIRST` and bare `MIDDLE` during resync | fragment-chain test | immediate |
| `BM14-drop-the-lock` | write the memtable without holding the DB mutex | **TSan lane** | ≤ 3 runs |
| `BM16-mutex-across-env` | hold the DB mutex across `Sync` | mutex-depth guard | immediate |
| `BM19-spurious-tripwire` | trip `kRecordTooLarge` on a legal-size record | §8.2 adjudication, direction 1 | immediate |
| `BM20-missing-tripwire` | accept a record above `kMaxRecordBytes` | §8.2 adjudication, direction 2 | immediate |
| `BM22-height-mapping-shift` | change the tower mapping `/2` → `/3` | height golden vectors | immediate |

**Harness and claim-integrity mutants** — the second half of Amendment A2's pairing, one instrument
checking the protocol and one checking the instrument.

| mutant | injected bug | must be caught by | budget |
|---|---|---|---|
| `BM12-no-buffer-cap` | remove the WAL-buffer cap | tripwire test — must halt, not OOM | immediate |
| `BM13-characterization-counted` | `CountsAsRecoveryEvidence` accepts `kCharacterizationOnly` | ledger test | immediate |
| `BM15-widen-the-set` | the recovery oracle accepts any batch boundary inside the in-flight group | exactness (i) on a multi-batch group | ≤ 10 |
| `BM17-bypassing-env-method` | add a public virtual to the Env base, bypassing interception | §3.2 count assertion | immediate |
| `BM18-regime-blind-aggregation` | drop `regime` from the ledger's aggregation key | cross-regime aggregation test | immediate |
| `BM21-network-in-build` | add `FetchContent_Declare` to CMake | the network-isolated `cpp-ci` run | immediate |
| `BM23-upgrade-the-claim` | edit `kConcurrencyClaim` toward "race-free" | `TestConcurrencyClaimWording` | immediate |

### 10.2 Gates

**Every gate is landed only once its failure has been induced and observed**, and the induced failure is
what the entry records. A gate that has only ever been green has demonstrated the cheap half. Per §0.1,
this applies to gates that exist because of a ruling exactly as it applies to the rest.

| gate | what proves it can fail | regression caught by |
|---|---|---|
| recovery exactness (§7.3 i) | make recovery accept records past the last `GROUP_END` | `BM2`, `BM15` |
| no over-promise (§7.3 ii) | advance the watermark before `Sync` returns | `BM1`, `BM5`, `BM7` |
| **lands on `G_{k−1}`** (§7.4 cond. 3) | `RecoveryLandsOnPreviousGroupWhenSyncIsTorn` — kill inside `Sync`, durability not applied | `BM2` |
| **lands on `G_k`** (§7.4 cond. 3) | `RecoveryLandsOnInFlightGroupWhenSyncCompletesButIsPreempted` — durability applied, kill before the return | `BM15` |
| both elements observed in the sweep | run the sweep with the in-flight case suppressed; the assertion must fire | `BM15` |
| the verdict names its element (§7.4 cond. 2) | return a boolean verdict; the oracle's own test must reject it | `BM15` |
| torn-tail rule, single block (§5.4.1) | make recovery accept `BATCH` records after the last `GROUP_END` | `BM2` |
| torn-tail rule, multi-block (§5.4.2) | truncate mid-`MIDDLE` and assert the tail is discarded; then plant a valid `FULL` after the gap and assert the open fails | `BM11`, `BM3` |
| illegal fragment transitions | plant `FIRST` immediately followed by `FIRST`, both CRC-valid | `BM11` |
| CRC covers the length (§5.3.3) | corrupt only the length field of a fully synced fragment; the CRC must fail at a known offset | `BM10` |
| interior-corruption detection | flip one byte inside a fully synced group; the open must fail with an offset | `BM8`, `BM3` |
| interior corruption is not truncated | make recovery stop at the first bad record; the planted corruption must go from "refused open" to "silent data loss" | `BM3` |
| gapless numbering (§7.2 step 3) | delete a WAL file; the open must fail | `BM4` |
| directory sync | kill between file creation and `Directory::Sync`; the gapless check must fire | `BM4` |
| `Apply` performs no I/O (§8.3) | move the WAL buffer into `WritableFile`; the per-thread counter must fire | `BM9` |
| mutex never held across an Env call (§8.3) | hold the DB mutex across `Sync`; the depth guard must fire | `BM16` |
| memtable is actually locked (§6.3) | remove the mutex from the write path; the TSan harness must report a race | `BM14` |
| the concurrency claim is not upgraded (§6.4) | edit the constant toward "race-free" | `BM23` |
| Env interception is unbypassable (§3.2) | add a public virtual to the base; the count assertion must fire | `BM17` |
| every `CallSite` is reachable | delete a public wrapper's `CallSite` registration; the census must report an unvisited enumerator | `BM17` |
| record-cap adjudication, direction 1 (§8.2) | make the engine trip on a legal-size record; the run must **fail**, not void | `BM19` |
| record-cap adjudication, direction 2 (§8.2) | make the engine accept an over-cap record; the run must fail | `BM20` |
| buffer-cap adjudication, both directions (§8.3) | the same two edits against `kWalBufferFull` | `BM19`, `BM20` |
| WAL-buffer tripwire halts (§8.3) | stall the syncer past the cap; the run must halt as `kVoid`, not OOM | `BM12` |
| cap ordering invariant (§8.3) | construct with `kWalBufferBytes < 2 × kMaxRecordBytes`; construction must fail | `BM12` |
| characterization is not evidence (§7.5) | make `CountsAsRecoveryEvidence` accept `kCharacterizationOnly` | `BM13` |
| both suspending injectors use one mechanism (§7.5) | enable the sector-subset torn `Sync` and assert the outcome is `kCharacterizationOnly` without a second flag existing | `BM13` |
| regimes never aggregate (§8.4) | summarize a lowered-cap run together with a default-cap run | `BM18` |
| height golden vectors (§6.2) | shift the tower mapping | `BM22` |
| deterministic on-disk bytes | leave one padding byte uninitialized; the WAL byte-digest must differ across runs | `BM6` |
| deterministic memtable shape | swap in a PRNG height source; the structural digest must differ across runs | `BM6` |
| the A5 scan lane (§9.4) | add a raw `::open` in the engine; the lane must fail | the blind-patch set, per DR-27 |
| vendored-tree integrity (§9.2) | edit one byte of the vendored GoogleTest; the offline hash lane must fail | — |
| no lane touches the network (§9.2) | add `FetchContent_Declare`; `cpp-ci` under `unshare -rn` must fail | `BM21` |
| kill-point census (§9.5) | add an Env call and do not update the census; the sweep must report the change | `BM17` |

**The byte-digest gate earns its own line.** Same workload, same WAL bytes, SHA-256 pinned. It is the C++
analogue of the trace hash and catches three things for one test: ambient randomness, uninitialized
padding, and any float that reached a serialization path. It is also why MSan stays declined.

---

## 11. Known idealizations

Item 1 is ratified and is being carried into DESIGN-A0 §7 by Ansh (§1.1); the rest are B1-local.

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
   exercised as an exactness-suspending injector, where the engine's obligation is detection rather than
   exactness and §7.5 makes the run structurally uncountable as evidence.
5. **B1 has no flush**, so the memtable and the WAL set grow without bound and every B1 test is small.
   Nothing in B1 exercises recovery across a flush boundary; that arrives with B2, which is also where
   §7.2's `max+1` numbering rule expires.
6. **Concurrency coverage is one authored interleaving pattern, not a search** (§6.4). The TSan harness
   drives `Apply`/`Get` against `Sync` for a fixed op count; it is not a systematic exploration of
   interleavings, and TSan reports the races it observes rather than the ones that exist. The sanctioned
   wording is `kConcurrencyClaim` and nothing else; strengthening it requires strengthening the harness in
   the same diff.
7. **A degenerate memtable shape is permanent and constructible** (§6.2). Tower heights are a pure
   function of the key set with no reseed, so a pathological key set is pathological on every machine
   forever, and the mapping is public so such a set can be built on purpose. Performance only; no
   invariant depends on tower height. The declined fix is a per-DB salt.
8. **Env interception is unbypassable from an implementation, not from an edit to the base class**
   (§3.2.1). Bypassing requires defeating the count assertion in the same diff that adds the method, which
   the scan lane's blind patches cover. The honest claim is "two independent checks", not "impossible".

---

## 12. Coordination

1. **Ansh is carrying to Track A:** §1.1's verification-scope text into DESIGN-A0 §7 and README, and the
   `Makefile` plus `.github/workflows/cpp.yml` changes covering `cpp-test`/`cpp-asan`/`cpp-ubsan`
   un-stubbing, the new `cpp-tsan`, and `cpp-ci`'s network-isolated run. **No Track A file has been
   touched by this session.**
2. **`engine/model/model.go` lines 24–26** — the stale pre-fix comment — is being carried by Ansh.
   Recorded only so the thread closes.
3. **Owed by me next cycle:** rebase the `rift-b` worktree (`/Users/anshk/Desktop/rift-b`, currently at
   `1390969`) onto `main` and report the resulting HEAD. `.gitignore` already carries `engine-cpp/build/`,
   so nothing else is needed to receive the tree.

---

## 13. Questions remaining

One, and it is new.

> **B1-Q11.** "Should `Status::kBusy`'s bidirectional predicate (§7.6 clause 6) be a precondition for
> landing backpressure at B5, or may B5 land the policy with a one-directional predicate and a recorded
> gap?"

**Recommendation: a precondition.** Clause 6 exists because a one-directional predicate makes clause 4
vacuous for that code and reopens the escape hatch under a new name; granting the first exception to it
at the moment it is first inconvenient is how the rule dies. The concrete difficulty is real and worth
naming: "the engine was legitimately busy" is a statement about the poller's pacing, which is harness-side
at B5 only if the harness owns the poller — so the precondition is really a constraint on B5's design,
namely that the rig drives the poller rather than observing it. I would rather bind that now, while it is
a design constraint, than discover it as an exception request later.

Everything else in this document is either ruled or awaiting a ruling on a recommendation already stated;
nothing new was opened by this revision.

---

## 14. Landing plan

None started before §13 is ruled and the remote gate clears. B1.1 is gated on §12.1's Track A items.

| PR | contents | gate |
|---|---|---|
| B1.0 | vendored GoogleTest at the pinned commit; `VERSION`; offline hash lane; `verify-vendored-gtest.sh`; `cpp-ci` network isolation | vendored-tree integrity and no-network gates, induced by a one-byte edit and by `BM21` |
| B1.1 | CMake skeleton, static archive, `Status::Code`, `RunOutcome`, `CountsAsRecoveryEvidence`, lanes un-stubbed | lanes fail loudly on a planted failure; `BM13` |
| B1.2 | `Env` NVI surface, `PosixEnv`, the raw-write seam and its short-write unit test | 1:1:1 count assertion; short-write, `EINTR`, zero-return tests; `BM17` |
| B1.3 | `TestEnv`: `content`/`durable`, fault controller, ledger, kill mechanism, both censuses | the durability model's tests; `CallSite` reachability; the ledger's induced failures |
| B1.4 | the A5 scan lane, `CPP-HATCHES.txt`, the blind-patch set | planted `::open` fails the lane; an unused registry entry fails it |
| B1.5 | skiplist memtable under the DB mutex, arena, deterministic heights + golden vectors, structural digest | vectors; `BM6`, `BM22`; `BM14` under TSan; `BM23` |
| B1.6 | WAL writer: framing, fragmentation, groups, caps, regime field, byte-digest test | pinned bytes; fragmentation across a block boundary; `BM12`, `BM18`, `BM19`, `BM20` |
| B1.7 | WAL reader and recovery: torn-tail rule, chain legality, resync | the seven recovery and corruption gates, each with its induced failure |
| B1.8 | `Open`/`Close`/`Write`/`Get`/iterator/snapshot; `DeleteRange` over the memtable | semantics suite mirroring `engine/model`'s |
| B1.9 | the kill-point sweep, the exactness oracle, the two-element verdict, §7.6's adjudication | full sweep green; both set elements observed; every mutant killed in budget |

---

## 15. Decision summary

**B1-D3 and §5.3 are the WAL record layout surface to be frozen.**

| # | decision | outcome |
|---|---|---|
| B1-D1 | Env surface shape and the choke point | **approved in substance, rejected as a convention.** Now NVI: public non-virtual intercepts, private pure virtuals implement; 1:1:1 count, `CallSite` census, BM17, and §3.2.1's residual stated |
| B1-D2 | what a kill leaves on disk | **approved.** Power-loss model only |
| B1-D3 | WAL framing and record layout | **approved. Freeze surface.** Blocks + fragmentation + `GROUP_END`; CRC over `length ‖ type ‖ payload`, a stated departure from LevelDB; sequence in the payload; size formula frozen |
| B1-D4 | the torn-tail rule | **approved.** Resync-verified; chain legality generalizes it to multi-block |
| B1-D5 | torn-`Sync` granularity and recycling | **approved**, with the sector-subset mode routed through §7.5's single registry |
| B1-D6 | memtable: structure, heights, concurrency | **(a) approved; (b) approved with golden vectors and the accepted degeneracy cost; (c) RULED — the DB mutex, lock-free rejected with its reopening threshold** |
| B1-D7 | manifest in B1 | **approved.** None; the log is the single authority |
| B1-D8 | the record-size cap | **OVERRULED on adjudication.** The cap stands; the harness computes `record_bytes` itself and both divergence directions fail the run. Only a satisfied harness-side predicate produces `kVoid` |
| B1-D9 | WAL buffer: ownership, cap, assertions | engine-owned so `Apply` makes zero Env calls; mutex-depth guard; cap adjudicated by §7.6 exactly as D8 is; `cap ≥ 2 × max_record` |
| B1-D10 | sequence space | **approved.** Collapse the batch; one sequence per `Apply` |
| B1-D11 | enforcing the non-syscall half of A5 | **approved.** Scan lane with a checked-in registry and blind patches |
| B1-D12 | kill mechanism and identity | **approved.** Dead-flag in-process plus sampled real `_exit`; ordinal, labels, census |
