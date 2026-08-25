# DESIGN-B1: Env, the WAL, the memtable, and the recovery contract

**Status:** **REVISION 5 — revision 4 ratified 2026-08-17; B1.0 through B1.4 ratified 2026-08-24 and
landed as code.** Nothing is self-ratified, so nothing is marked PROVISIONAL. **§13 is closed**:
B1-Q12 was ruled as recommended, and this revision records the ruling that revision 4 predated.
**Track B is out of design.** Five steps of §14's sequence exist as code on `rift-b`; §14.2 carries a
**Landed** line for each, naming what it added beyond its written scope, because the sequence is now
half history and a plan that does not admit what happened is a plan nobody checks against.

This revision is **the four owed items of §12.3 batched**, landed as B1.4 closed rather than as a
cycle of its own. Track B does not need another doc revision as an event.
**Phase:** B1 (Track B). **Author:** Claude (Session B). **Decider:** Ansh.
**Blocks:** all of Track B. **Depends on:** the `engine/` interface frozen at A0.5, which this must
meet exactly — not approximately, because B4's differential rig defines "correct" as "byte-identical
to `engine/model`".

Twelve decisions, `B1-D1`..`B1-D12`. **B1-D3 and §5.3 are the WAL record layout surface to be frozen.**

---

## 0. Ruling echo

Two sets. §0 and §0.1 carry Ansh's rulings of 2026-08-12 on revision 2, which revision 3 applied and
which remain in force. §0.2 carries the rulings of 2026-08-17 on revision 3.

Rulings — Ansh, 2026-08-12, on DESIGN-B1 revision 2. Verbatim, each followed by where it lands.

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

### 0.1 Standing principles

Two, both ruled by Ansh, both about the relationship between a ruling and the session implementing it,
and both binding on this document going forward.

#### Principle 1 — a ruling owns the failure modes it opens

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

#### Principle 2 — conflicting rulings are reported, never resolved

> **When two of Ansh's rulings conflict, the session reports the conflict and stops rather than
> resolving it in whichever direction looks cheaper.** A session that resolves the architect's
> contradictions is a session making architecture decisions without a record.

Ruled 2026-08-17, on revision 4's §12.4. The instance: "Session B operates in `rift-b`, only" and
"design docs live on `main`" were jointly unsatisfiable, because `main` is checked out in the worktree
the first ruling forbids. Both readings were available and both were cheap — write the Track A tree
anyway, or move the doc off `main` — and either one would have been a permanent architectural change
made silently by the session that happened to hit the contradiction first.

The operational form: **report the contradiction, enumerate the mechanisms that would resolve it, state
that the choice is not the session's, and stop on that thread while continuing every thread that does
not depend on it.** This is the same discipline as marking a recommendation rather than self-ratifying
it, applied one level up: there, the session declines to ratify its own proposal; here, it declines to
adjudicate between two of the architect's. The resolution of that instance is §12.2, and the reasoning
Ansh gave when resolving it is the reason the principle matters — *the original rule was wrong in its
reasoning, not merely in its consequence*, which is a correction only he could make and which a session
resolving the conflict cheaply would have buried.

### 0.2 Rulings on revision 3 — Ansh, 2026-08-17

Verbatim, each followed by where it lands. Revision 3 is ratified in full; the seven rulings it applied
are unchanged and are not restated here.

> Working directory: you are Session B and you operate in /Users/anshk/Desktop/rift-b, only. Running in
> the Track A worktree risks two sessions writing the same tree with neither one aware, which is a worse
> failure than anything in the design.

→ **§12.1**, as a standing constraint on this session rather than a note. It has a consequence the
ruling did not have to state and §0.1 obliges this document to find: design docs live on `main`, and
`main` is checked out in the Track A worktree, so the path by which a revision of *this file* reaches
`main` was, at revision 4, unspecified. The gap was reported rather than resolved; it is closed by
§12.2 and the principle that governed the reporting is §0.1 principle 2.

> Where the doc lives: design docs live on main, which is where revision 2 already sits, so revision 3
> lands there too rather than being moved. [...] Nothing else this cycle touches git.

→ **§12.4**, with the executed sequence and the resulting HEAD.

> B1-Q11: precondition, your recommendation is correct and the reasoning is right. A one-directional
> predicate makes clause 4 vacuous and reopens the escape hatch under a new name, which is the same
> failure D8 was overruled for. Ruling: kBusy lands at B5 only with a bidirectional predicate, and the
> constraint on B5's design is stated now, in the doc, as a forward binding: the rig drives the poller
> rather than observing it, because a rig that can only observe cannot construct the negative direction.
> Record the cost plainly, that this makes B5's rig strictly more work, and that we are paying it
> because a one-directional predicate is an oracle asking the engine whether it was justified.

→ **§7.6.1**, as a forward binding with the cost recorded, and the `kBusy` row of §7.6's table updated
from "to be established at B5" to a landed precondition. §13 is closed.

> Deliverable: a written landing sequence for B1.0 through B1.n, each step naming what lands, which
> gates and mutants come with it, and what would have to be true for that step to be reverted
> independently of its neighbors. [...] specifically which step first makes a lane able to fail
> honestly, because that step should be early rather than convenient.

→ **§14**, rewritten from a four-column table into the sequence, the revert map, and §14.1's answer to
the honesty question. Three steps split under the analysis; §14.4 gives the reasons.

### 0.3 Rulings on revision 4 — Ansh, 2026-08-17

The git sequence, §7.6.1's B1-Q11 landing, the three-threshold answer, the fourth revert class, and the
three splits are ratified as written and are not restated. What follows changed something.

> Record the general principle in the doc: when two of my rulings conflict, the session reports the
> conflict and stops rather than resolving it in whichever direction looks cheaper, because a session
> that resolves my contradictions is a session making architecture decisions without a record.

→ **§0.1, principle 2.**

> Resolving it: mechanism (a), amended. [...] The rule I gave you was wrong in its reasoning, not just
> its consequence: design docs do not live on main, they live wherever their track's session can commit
> them, and main is where they converge. So from now on, you commit DESIGN-B* on rift-b yourself, one
> file per commit, subject naming the revision, and you report the sha. You never write the Track A
> worktree again, including for docs. I handle every merge to main and I will tell you when one has
> landed.

→ **§12.2.** §12.4 of revision 4 is closed by this and replaced.

> Track A is now working actively in that tree, so any write of yours into it would have raced live
> edits. State in the doc that the two worktrees are single-writer by session, with rift-b yours and
> Rift Track A's, and that convergence happens only through me.

→ **§12.1**, restated as a single-writer property rather than as a prohibition on one session.

> Track A learned this week that a lane can be green for five checklist steps while the machinery
> underneath runs at a sixth of its power and nothing notices [...]. B1.1 as the ALIVE-canary moment,
> with every green between there and B1.9 uninterpretable without it, is the correct framing and it
> should be quoted in the doc as such.

→ **§14.1**, with the Track A instance recorded as the reason rather than as an aside, and the
ALIVE-canary sentence pulled out as a quoted rule.

> It is not mainly that early tests get rewritten. It is that an engine-account test which passes gets
> kept, and a kept engine-account test is the oracle interrogating the engine, permanently, with
> nothing marking it as such. It will look like coverage. Write it that way, and add the mechanical
> defense: any test written before its independent observer exists is either deleted or re-derived
> against the observer when that observer lands, and the landing step that introduces an observer names
> which earlier tests it must retire. Otherwise this is a discipline and we have both watched
> disciplines fail this month.

→ **§14.1's rejection**, rewritten to the stronger claim, plus **§14.1.1** stating the retirement rule,
plus a **Retires** line on every step in §14.2 that introduces an observer — B1.1, B1.3, B1.9a.

> Add one thing: name in each such step which specific published claim depends on it, so the retraction
> has a target. A rule that says retract the claim is weaker than a rule that says retract this sentence
> in this file.

→ **§14.2**, B1.4 and B1.9b each gain a **Claim it carries** line naming file and sentence.

> PosixEnv is the only component in the entire engine whose behavior is not verified by anything in B1,
> and it is the component that talks to the actual disk. [...] it is an idealization and it belongs in
> section 11 with the others, stated at least as plainly as you stated the kill -9 page-cache
> degradation. [...] Then say what would raise the confidence, and whether any of it is cheap enough to
> be worth doing at B1.

→ **§11 item 9**, and the confidence question is **B1-Q12** in §13, since answering it would add gates
and §10.2 is not mine to extend without a ruling.

> Two things to carry into the next revision. [...] I want the mutant catalogue re-checked against the
> split steps once, to confirm every mutant still has a landing step that introduces it [...]. And add
> the harness-power principle from Track A as a forward binding on B1.9b: every planted flaw class in
> the C++ sweep carries a floor, a minimum detection rate and a maximum seeds-to-detection, and the
> campaign lane fails when a class drops below its floor.

→ **§12.3**, recorded as owed for revision 5 and deliberately **not** done here. §10.1 and §10.2 remain
untouched by this revision, which is the state the re-check must start from.

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
| `Env::NewDirectory` ⁽²⁾ | — nothing synced yet | — no bytes yet | — ⁽¹⁾ | ✓ `ENOENT`, `EIO`, `ENOTDIR` | — | ✓ |
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
| `RandomAccessFile::Read` ⁽²⁾ *(declared; first used B2)* | — read path | — read path | — a short read at EOF is normal and is not a fault | ✓ | — | ✓ |

⁽²⁾ **A CORRECTION TO THIS TABLE, NOT AN ADDITION TO THE SURFACE.** Both rows were missing from
revision 4 and were found at B1.2a by the act of enumerating the surface: the table lists
`Directory::Sync`, and a `Directory` has to be obtained by *some* syscall, which is `Env::NewDirectory`;
and it lists `Env::NewRandomAccessFile`, and a handle with no read is not a handle. Neither is a new
capability — they were always implied by rows that were already here, and the standing requirement is
that **no cell of this table is blank**, which a missing row satisfies only by not existing. Recorded
as a correction so nobody reads them as scope that grew.

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

#### 7.1.1 Everything C++ cannot express literally, in one list

Four, and they are kept together so the list of divergences from the frozen shape is one list rather
than a note here and a comment there. The first two are ruled above; the last two were found by
meeting the interface at B1.8 and are recorded rather than adapted quietly.

| # | what the frozen shape says | what C++ does | why |
|---|---|---|---|
| 1 | `OnDurable(func(SeqNum))` | **absent** | No C-to-Go callbacks, ever (DR-11). The wrapper's poller owns the blocking `Sync()` and posts to the node mailbox. `Sync()` is strictly more primitive: a callback can be built from a poller and a poller cannot be built from a callback |
| 2 | `Apply(b, sync bool)` | **no `sync` flag** | Its policy is a B5 decision about the pair. `Write()` never blocks on I/O whatever the caller asked for, so a flag promising otherwise is one the engine cannot honour |
| 3 | a `nil` bound means unbounded | an explicit **`Bound`** | Go's nil and an empty key are different things and **an empty key is a valid key here**, so `Slice()` cannot mean both. Conflating them would make `DeleteRange(nil, nil)` — the clear half of clear-then-ingest, the case **Amendment A3 was ruled for** — indistinguishable from a range that deletes nothing. That is what makes this a divergence rather than a style choice |
| 4 | "Approximate is in the name because the C++ engine answers from table metadata rather than by scanning" | **it scans**, exactly and in O(n) | B1 has no tables and therefore no metadata to answer from. **TEMPORARY, AND RETIRED BY B2**, which is where SSTable metadata first exists; recorded with its retiring phase so it does not become a permanent property by silence |


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

**The registry is asserted in BOTH directions, and that is not symmetry for its own sake.** Members
must suspend, and **non-members must not** — because the second direction is the one nothing else
notices. A prefix-granular torn `Sync` was classified as a member at B1.3 and stayed that way for four
ratified steps: the engine behaved correctly, every assertion held, every lane was green, and the only
consequence was that bankable runs were being marked characterization-only. Its cost would have
arrived here, at §7.4 condition 3, as a gate nothing can satisfy — *both elements observed across the
sweep* is unsatisfiable if every run is structurally uncountable as evidence — and it would have
presented as a bug in the engine rather than in the classifier. BUGS.md records it as HARNESS-006 and
generalizes it as **GF-4, an unsatisfiable gate**.

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
| `kBusy` | **B5** — the poller-backpressure policy; predicate defined with the policy, **bidirectional by precondition**, §7.6.1 | required before the code exists |

`kNotFound` is deliberately absent: it is the frozen interface's `ErrNotFound`, a normal result, and
`engine/model` produces it too — so it is not a place the engines can legally differ and it does not
belong in this table.

### 7.6.1 B1-Q11, ruled: `kBusy` is a precondition, and the constraint it puts on B5

**Ruled 2026-08-17.** `Status::kBusy` lands at B5 **only with a bidirectional predicate**. Not "lands
with a recorded gap", not "lands one-directional and is tightened later". The reason is the one the
ruling gives and it is worth keeping in the engine's own words: *a one-directional predicate is an
oracle asking the engine whether it was justified* — which is the identical failure B1-D8 was overruled
for, arriving under a new name. Clause 4 without clause 6 is not a weaker rule; it is a vacuous one for
whichever code first finds it inconvenient, and granting the first exception at the first inconvenience
is how the rule dies.

**The forward binding on B5, stated now while it is a design constraint.**

> **B5's poller rig drives the poller; it does not observe it.** Backpressure state — how many syncs are
> outstanding, how long the poller has been behind, where the queue depth sits against its threshold —
> is *set by the harness* and read back from the harness's own record, never inferred from the engine's
> behaviour or read out of the engine's counters.

The reason is mechanical, not stylistic. `kBusy`'s two directions are:

| direction | the harness must be able to assert |
|---|---|
| 1 (spurious) | the engine returned `kBusy` while the harness's own record says the backpressure condition was **not** met → divergence, the run **fails** |
| 2 (missing) | the harness's own record says the condition **was** met and the engine returned `kOk` → divergence, the run **fails** |

Direction 2 is the one that forces the design. To assert it, the harness must be able to *put* the
engine into a state where backpressure is unambiguously owed and then observe that it was not signalled.
A rig that only watches the poller can never construct that state on purpose: it can wait for a busy
moment to occur and check what the engine said, which tests direction 1 alone. So a rig that can only
observe yields a one-directional predicate by construction — the two are the same decision, and ruling
the predicate bidirectional *is* ruling the rig a driver.

**The cost, plainly.** This makes B5's rig strictly more work. Driving the poller means the poller's
pacing is a harness-controlled input rather than an emergent property: an injectable schedule, a way to
stall and release completions deterministically, and a record of the intended state at every point where
`kBusy` could be returned — none of which a passive observer needs. That is real additional design and
real additional code in B5, it is known now rather than discovered then, and **we are paying it on
purpose**, because the alternative buys a cheaper rig with an oracle that consults the subject.

**Consequence for §7.6 clause 6 generally:** clause 6 is now demonstrated as well as stated. The first
code to test it produced a genuine design constraint on a later phase rather than an exception, which is
the outcome the clause was written to produce. Any future `Status::Code` inherits the same treatment,
including the same willingness to let it constrain the phase that adds it.

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

### 10.1.1 Every mutant has a landing step, re-checked once

**Owed by §12.3 item 2(i), done here.** The three splits of revision 4 moved boundaries without moving
content, and a mutant with no introducing step is a mutant nobody is responsible for landing. Checked
against §14.2's thirteen steps:

| mutant | introduced at | status |
|---|---|---|
| `BM13` | B1.1 | **landed and killed** |
| `BM17` | B1.2a (static half), B1.3 (reachability half) | **landed and killed**, split into `BM17a`/`BM17b` (§10.1.2) |
| `BM21` | B1.0 | **landed and killed** |
| `BM6`, `BM14`, `BM22`, `BM23` | B1.5 | not yet reached |
| `BM4`, `BM9`, `BM12`, `BM16`, `BM18`, `BM19`, `BM20` | B1.6 | not yet reached |
| `BM3`, `BM8`, `BM10`, `BM11` | B1.7a | not yet reached |
| `BM2` | B1.7b | not yet reached |
| `BM7` | B1.8 | not yet reached |
| `BM1`, `BM5`, `BM15` | B1.9a | not yet reached |

Every catalogue mutant has exactly one introducing step and none is orphaned. `BM3`, `BM4`, `BM9` and
`BM11` appear in more than one step's **Mutants** line; that is a mutant re-*asserted* by a later step,
not a second landing, and the introducing step is the earliest one.

### 10.1.2 Mutants this catalogue did not name

Twelve exist that §10.1 does not list. They are recorded here rather than folded into the tables above,
because the catalogue is the *designed* flaw set and these are the ones the code asked for as it was
written — and the distinction is worth keeping visible.

| id | what it plants | why it exists |
|---|---|---|
| `LANE-cpp-test/asan/ubsan/tsan` | one defect only that lane can see | B1.1's four reds, one per lane. A red per lane with a control that stays green is what makes a red attributable rather than four counts of one broken build |
| `BM17a`, `BM17b` | a public virtual on the base; a wrapper with no `CallSite` | the two directions of the 1:1:1 assertion, which §14.2 names but §10.1 counts as one mutant |
| `BM17c` | `env.cc` intercepts with a *neighbour's* `CallSite` | **the scan structurally cannot see this.** It reads declarations; the enumerator a wrapper actually passes lives in `env.cc`. Its control is cpp-scan staying *green*, which is the demonstration that the two halves are not redundant |
| `BM17d` | an enumerator dropped from `AllCallSites()` | `AllCallSites()` is a fourth artifact the correspondence must bind, added at B1.3 because the census iterates it |
| `SEAM-*` (3) | short-count ignored; `EINTR` fatal; the zero-return bound removed | B1.2b's three gates, made repeatable. §14.2 says "Mutants: none" for that step, which is a statement about the **BM catalogue** — `PosixEnv` is outside the fault matrix and gets no BM number — not a statement that its gates need no induction |
| `MODEL-*` (3), `LEDGER-*`, `REGISTRY-*`, `CENSUS-*` (2) | the power-loss model, the ledger's promotion column, the registry, both censuses | B1.3's gates |
| `SCAN-*` (5), `COLD-*` | the scope-scan rules, the registry, the claims, the cold cache | B1.4's gates |

**A standing consequence, and it is the reason this subsection exists rather than a note.** Two of
these — `BM17c` and `BM17d` — were written because building the checker exposed something the checker
could not see. Amendment A2 requires a new mutant class in the same PR as the gap it names, and that
obligation reads most naturally as applying to *bugs*. It applies to **checkers** identically: the
moment a checker is written is the only moment anyone has a precise description of its blind spot.

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

### 10.3 The harness-power floor, as a forward binding on B1.9b

**Owed by §12.3 item 2(ii), done here.** Track A built this after discovering a harness defect that
quietly cut detection to a sixth with every lane green; Ansh approved Track B mirroring the actual
construct rather than inventing a parallel one, so what follows is `sim/hunt/floors.go`'s shape with
kill points substituted for seeds (§10.1: kill points are the C++ analogue of seeds-to-detection).

> **Every planted flaw class in the C++ sweep carries a floor — a minimum detection rate and a maximum
> kill-points-to-detection — and the campaign lane FAILS when a class drops below its floor.**

Six properties, each of which Track A's version has for a reason:

1. **A table, not a threshold.** One entry per flaw class carrying the minimum rate, the maximum
   kill-points-to-detection, **the measurement the floor was derived from**, and **the reasoning for
   where it sits**. The first question on a red lane is "is the floor wrong, or is the harness?", and
   the answer has to be in the output or the lane gets edited instead of investigated.
2. **Both bounds, because they degrade independently.** A class can hold its detection rate while its
   first detection moves far later in the kill-point space, and that is exactly what decides whether a
   cheap sweep would ever see it.
3. **Floors with margin, never exact values.** The sweep is deterministic, so an exact assertion is
   *possible* and is deliberately not used: it would fail on any benign change — one more Env call, a
   different workload — and **a lane that cries wolf is a lane people delete**. A floor passes drift
   and fails a collapse, which is the only failure worth a build break.
4. **The number that matters is the suppressed one.** Track A floors `ack-before-sync` at roughly half
   its measured rate because the defect had held it at 82 per mille against a measured 504. The floor
   is set to fail loudly on a regression *of that kind*, not to sit just under today's number.
5. **A weak class is floored at "detected at all" plus a kill-point bound**, never at a rate derived
   from a single detection — that asserts noise and breaks whenever a change reshuffles which point
   happens to hit it. What must not silently change is that the class remains *reachable*.
6. **Every flaw class must have a floor, asserted.** Track A's `TestEveryObservableFlawHasAFloor` stops
   the table falling behind the flaw set: a class added without a floor is a class with no standing
   measurement, which is how a bug class drifts back into being uncatchable one flaw at a time.
   Exemptions are **named and reasoned**, not silent.

**And it is induced rather than described**, per standing policy: restoring a known harness defect must
drop a class below both bounds and fail the lane with the floor, the measurement and the reasoning
printed. B1.9b does not close until that has been observed.

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
9. **`PosixEnv` is unverified by every lane in B1, and it is the component that talks to the actual
   disk.** Ruled into this list 2026-08-17. Every B1 test runs on `TestEnv`, so the one piece of the
   engine whose behaviour B1 never checks is the piece every production durability claim runs *through*
   — and the piece B1's verification runs *around*. Stated at full strength, because it is easy to read
   the revert map's "only unqualified leaf" as good news and it is not: B1.2b is a leaf precisely because
   nothing in B1 depends on it being right.

   The division of labour is still correct — `TestEnv` is where fault injection belongs, and a
   fault-injecting production Env would be a second implementation of the thing under test — so this is
   an idealization to state, not a defect to fix here. What the correctness of `PosixEnv` currently rests
   on, exactly and exhaustively: **(a)** the thinness of the implementation, each public wrapper being a
   mechanical mapping onto one syscall with no logic beyond a retry loop; **(b)** the short-write,
   `EINTR` and zero-return unit tests at the internal raw-write seam (§3.3 ⁽¹⁾, idealization 2); and
   nothing else. There is no lane in B1 that would notice `PosixEnv` calling `pwrite` where it promised
   `write`, syncing the wrong descriptor, or dropping a flag.

   **Updated by B1-Q12's ruling (§13).** Clause **(a)** is no longer a belief: the thinness rule is
   stated at B1.2b and enforced at B1.4, so logic can no longer accumulate here unnoticed. A third leg
   was added by B1.4 that this text predates — the readdir enumeration moved to the raw seam and
   acquired four tests, because it was the one function in `PosixEnv` with real logic and no
   instrument, and the thinness rule has no honest label for that. **The first real evidence about
   `PosixEnv` now arrives at B1.8**, not B5, with the semantics suite run against it on a real
   filesystem — fault-free, making no durability claim, never entering the recovery ledger. The first
   *adversarial* evidence still arrives in I2's chaos lane.

   **What has not changed, and is the sentence to keep:** `PosixEnv` has **zero executed lines** in
   B1. Every B1 test runs on `TestEnv`. The single thing verified about it today is that
   `NewPosixEnv` instantiates the concrete class, so a signature that drifted from the Env surface is
   a compile error. A component with zero coverage and a written argument for why is acceptable; a
   component with zero coverage and no argument is not, and this item is the argument.

   Four things would raise the confidence, ordered by cost. §13's **B1-Q12** asks which, if any, are
   cheap enough to be worth doing at B1; my recommendation is there rather than here, because acting on
   it would add gates to §10.2 and that is not mine to do unruled.

---

## 12. Coordination

### 12.1 The worktrees are single-writer by session

**Ruled 2026-08-17.** Two worktrees over one repository, and **each has exactly one writer**:

| worktree | branch | sole writer |
|---|---|---|
| `/Users/anshk/Desktop/rift-b` | `rift-b` | **Session B (this session)** |
| `/Users/anshk/Desktop/Rift` | `main` | **Track A's session** |

**Convergence happens only through Ansh.** No session merges, rebases onto, or writes another session's
tree; the branches meet when he merges them and he says when one has landed.

This is stated as a property of the trees rather than as a prohibition on one session because the
prohibition form invites the question "may I, just this once, for a doc" — and the answer has to be no
for a reason that survives the special case. The reason is concrete and was observed the same day the
rule was made: within four hours of the ruling, the Track A worktree went from one modified doc to five
modified `sim/` files plus an untracked `sim/hunt/scenario.go`. Any write of mine into that tree would
have raced live edits by a session with no way to know I was there. **Two sessions writing one tree
corrupts the evidence, not merely the code**, and evidence is the entire deliverable of this project.

### 12.2 How a Track B design doc reaches `main`

**Ruled 2026-08-17, mechanism (a) amended**, closing and replacing revision 4's §12.4. The amendment is
the load-bearing part and Ansh's own words carry it: *design docs do not live on `main`, they live
wherever their track's session can commit them, and `main` is where they converge.* The original rule
was wrong in its reasoning and not only in its consequence, which is exactly the correction a session
resolving the contradiction cheaply would have buried — see §0.1 principle 2.

The procedure, binding from revision 4 forward:

1. Session B commits `docs/DESIGN-B*.md` **on `rift-b`, itself**.
2. **One file per commit.** The subject names the revision.
3. Session B reports the sha in that cycle's report.
4. **Ansh performs every merge to `main`** and tells Session B when one has landed.
5. **Session B never writes the Track A worktree again, docs included.**

### 12.3 Owed — all four items closed by revision 5

1. **CLOSED, and reassigned.** §1.1's verification-scope text into DESIGN-A0 §7 and README remains
   Ansh's to carry, along with `engine/model/model.go` lines 24–26. The `Makefile` and
   `.github/workflows/cpp.yml` half is **no longer his**: it was committed on `rift-b` by this session
   at B1.0/B1.1, under §12.2's amended mechanism.

   **Why the reassignment rather than a request.** §12.3 was written while the rule was "Session B
   never writes a file that lives on `main`", and §12.2 replaced that with "Session B commits on
   `rift-b` and Ansh merges" — for the reason Ansh gave, that *design docs do not live on `main`, they
   live wherever their track's session can commit them*. The same reasoning covers lane definitions
   exactly: **a lane this session cannot run is a lane this session cannot induce**, and the whole
   discipline is that no gate counts until its failure has been observed. Carrying the lane file across
   a worktree boundary would have put every Track B induced failure behind someone else's merge.
   Ratified 2026-08-24.
2. **CLOSED.** (i) is **§10.1.1** — every catalogue mutant has exactly one introducing step and none
   is orphaned; §10.1.2 additionally records the twelve mutants the catalogue did not name. (ii) is
   **§10.3**, mirroring Track A's `sim/hunt/floors.go` construct rather than inventing a parallel one,
   with kill points substituted for seeds. This is the second time Track A's experience has arrived as
   a forward binding rather than as a lesson Track B had to repeat, and that is the point of a shared
   constitution.
3. Revision 4 is committed on `rift-b` per §12.2; the sha is in the cycle report.

### 12.4 Done

**2026-08-24 — Track B out of design.** Five steps of §14's sequence landed as code on `rift-b`, plus
one correction, plus this revision:

| sha | step |
|---|---|
| `f57ad9b` | **B1.0** — GoogleTest vendored at `52eb8108…` (v1.17.0), the offline hash lane, `cpp-ci` |
| `646fca5` | **B1.1** — closed enums, four lanes un-stubbed, the ALIVE canary |
| `29bebd2` | **B1.2a** — the Env choke point, zero implementations |
| `cf12938` | **B1.2b** — `PosixEnv` and the raw-write seam |
| `3239469` | **B1.3** — `TestEnv`, the content/durable split, threshold 3 |
| `787307d` | correction — B1-Q12 is ruled, not open; the thinness rule stated |
| `187a3eb` | **B1.4** — the scope scan, the split-label registry, `CLAIMS.txt`, the blind-patch set |

The `Makefile` and `.github/workflows/cpp.yml` lane definitions are in those commits, on `rift-b`, per
§12.3 item 1's reassignment. Merges to `main` remain Ansh's.

**2026-08-17.** Revision 3 committed as `60e4ced`, one file, subject `DESIGN-B1 revision 3: the NVI
choke point, the two-element oracle, one registry`. `rift-b` fast-forwarded from `1390969` onto `main`,
**resulting HEAD `60e4ced`**, both ranges empty, doc byte-identical in both worktrees at blob `10f56bd`.
Track A's untracked `docs/A0-CHECKLIST.md` was left untracked. That commit was the **last** write this
session makes to the Track A worktree, under §12.2 item 5.

---

## 13. Questions remaining

**None. §13 is closed.**

B1-Q12 was **ruled as recommended**: measures 1 and 2 land at B1, measure 3 at B4, measure 4 declined
for B1 as already scheduled into I2.

> **Measure 1 — thinness as a checked property.** Each private `Do*` in `env/posix/` is a mechanical
> mapping onto one syscall: no branching beyond a documented retry loop, no state beyond the descriptor
> and the path, no arithmetic on offsets or lengths beyond what the public wrapper passed, and a
> per-method statement cap. **Rule stated at B1.2b, enforced at B1.4.**
>
> **Measure 2 — B1.8's semantics suite run against `PosixEnv` on a real filesystem.** Approved **under
> the scoping condition**, which is part of the ruling and not a footnote to it: it injects no faults,
> makes no durability claim, and its runs are **not evidence** under §7.5 — the same distinction that
> separates characterization from evidence, applied to a lane rather than to a run. It is a correctness
> lane about a mapping, and describing it as anything more would be the exact overstatement §11 exists
> to prevent.

**A drift worth recording, because the ruling-echo discipline used to catch exactly this.** The ruling
postdates revision 4, and revision 4 was never revised, so §13 printed B1-Q12 as *open* for a week
after it had been decided. B1.2b was written against the document and stated in its own header that the
question was open and that acting on it would extend §10.2 unruled — a comment that misdescribed the
state of a ruling, which is the kind that gets believed. It was corrected in `787307d`, in its own
commit, before B1.4 enforced anything.

**The general form, ruled 2026-08-24:**

> **A ruling that is not written into the artifact it governs has not landed, regardless of who
> received it.**

Not "is at risk of being forgotten" — *has not landed*. A ruling with two versions has a losing one,
and the losing one is whichever the next session reads; since sessions read the artifact and not the
conversation, the artifact is the only version that exists.

This is the **same class** as the paste that went missing on D-A7-6, one cycle after the ruling echo
was retired as ceremony. Two tracks, two artifacts, one mechanism: the echo was the thing writing
rulings into the documents they govern, and both losses date from its removal. §0's ruling-echo
sections exist for this, and the cycles that skipped them are the cycles it happened in.

## 14. Landing sequence

**Five of the thirteen have landed** (`f57ad9b`, `646fca5`, `29bebd2`, `cf12938`, `3239469`,
`187a3eb` — six commits, one of them a correction). §13 is closed, §12.3 is closed, and the remote gate
was lifted on 2026-08-24; there is still no `origin` and CI has still never run, which is why every
lane here is one a person runs by hand and why each carries its own induced failure rather than a
promise that someone else's runner will notice.

Each landed step below carries a **Landed** line naming its sha and what it added beyond its written
scope. Recording that is not bookkeeping: this sequence is now half plan and half history, and **a plan
that does not admit what happened is a plan nobody checks against.** Thirteen landings across ten
numbers — §14.4 gives the reason for the three splits.

**Two ordering invariants govern the whole sequence, and everything below is a consequence of them.**

1. **The observer lands before the observed.** Every mechanism that can contradict the engine —
   un-stubbed lanes, the Env choke point, TestEnv's durability model, the scope scan — lands before the
   first line of engine code at B1.5. Not "early". *Before.*
2. **A gate lands only once its failure has been induced and observed** (§10.2), and per §0.1 that
   applies to gates that exist because of a ruling exactly as it applies to the rest. So the unit of
   landing is never "the code" — it is *the code plus the demonstration that its check can go red*.

### 14.1 Which step first makes a lane able to fail honestly

Asked directly, so answered directly. There are three distinct honesty thresholds and they are not the
same event:

| # | threshold | step | what is true after it that was not before |
|---|---|---|---|
| 1 | **lane honesty** | **B1.0** | a lane in this repo can go red at all. Both of B1.0's gates are induced-failure-first — a one-byte edit to the vendored tree must fail the hash lane, and `BM21`'s `FetchContent_Declare` must pass with networking and fail under `unshare -rn`. This happens *before a single line of our C++ exists* |
| 2 | **our-code honesty** | **B1.1** | a failing C++ test of ours turns CI red. This is the un-stubbing, and it is the ALIVE-canary moment: "required lane" becomes "lane that can fail". Every green between here and B1.9 is uninterpretable without it |
| 3 | **durability honesty** | **B1.3** | a lane can fail *for the right reason about durability*. Before `TestEnv`'s `content`/`durable` split there is no observer that distinguishes written from durable, so no durability test can fail correctly — it can only fail loudly |

Threshold 2 is the one to quote, and Ansh ruled it be quoted as such:

> **B1.1 is the ALIVE-canary moment: "required lane" becomes "lane that can fail". Every green between
> B1.1 and B1.9 is uninterpretable without it.**

**Why that is a rule and not fastidiousness.** Track A learned it this week the expensive way: a lane
stayed green across **five checklist steps** while the machinery underneath was running at **a sixth of
its power**, and nothing noticed, because a green lane reports the health of the lane and not the power
of the harness behind it. Landing the observer before the observed is the only reason any subsequent
green means anything — and the failure mode is not a red that gets ignored, it is a green that is true
and worthless.

**The engine's first line lands at B1.5, two steps after threshold 3.** That is the whole ordering claim
in one sentence: by the time there is anything to be wrong, everything that could catch it is already
standing and has already been seen to go red.

**The convenient order, and why it is rejected.** The convenient order is memtable → WAL writer → reader
→ *then* build the rig: it shows visible progress first, each piece is testable in isolation, and the
expensive harness work is deferred until there is something to point it at. It is rejected for a reason
much sharper than "we would find bugs later", and sharper than "those tests get rewritten" — that
framing understates it, because rewriting is the *good* outcome.

Every test written in that order is necessarily an **engine-account test**: it drives the engine and asks
the engine what happened, because no independent observer exists yet. §7.3 exists to forbid exactly that,
and the ruling behind §7.6.1 names the failure — *an oracle asking the engine whether it was justified.*
The tests that fail get rewritten and are therefore harmless. **The danger is the ones that pass.** A
passing engine-account test gets kept. Kept, it is the oracle interrogating the engine — permanently,
with nothing in the file marking it as such, sitting in the suite next to real tests, indistinguishable
from them, counted in the totals. **It will look like coverage.** That is the whole objection: not
wasted work, but a verification claim quietly diluted by tests that can only confirm.

#### 14.1.1 The retirement rule

A discipline is not a defense, and we have both watched disciplines fail this month. So the rule is
mechanical:

> **Any test written before its independent observer exists is either deleted or re-derived against that
> observer when the observer lands. The landing step that introduces an observer names, in its own
> entry, which earlier tests it retires.**

Three steps in §14.2 introduce an observer, and each carries a **Retires** line stating what it must
account for: **B1.1** (the lanes themselves), **B1.3** (`TestEnv`, the durability observer), and
**B1.9a** (the exactness oracle). "Nothing" is a legal and expected answer where the sequence has
already prevented the situation — that is what an ordering invariant is *for*, and recording the empty
case is what makes the non-empty case credible when it appears.

**One more consequence worth stating.** Thresholds 1 and 2 are cheap and land at steps 0 and 1. Threshold
3 is expensive and lands at step 3 of thirteen. The expensive one is early *because* it is expensive: a
harness deferred is a harness scoped to the code that already exists.

### 14.2 The sequence

Each entry: what lands, the gates that come with it and the failure induced to prove each can go red, the
mutants, what it depends on, and the revert condition. Mutant IDs are §10.1's; gate rows are §10.2's.

---

**B1.0 — the vendored tree and the offline gate.** *Gated on: the remote gate.*

- **Lands:** `third_party/googletest/` at `52eb8108c5bdec04579160ae17225d66034bd723` (v1.17.0);
  `third_party/googletest/VERSION` recording the tree hash; `scripts/verify-vendored-gtest.sh` (§9.2);
  the offline hash lane; `cpp-ci` running the full lane set under `unshare -rn`.
- **Gates + induced failure:** vendored-tree integrity — *edit one byte of the vendored tree, the hash
  lane must fail*. No lane touches the network — *add `FetchContent_Declare`; it must pass with
  networking available and fail under isolation*.
- **Mutants:** `BM21`.
- **Depends on:** nothing.
- **Landed `f57ad9b`.** Beyond its written scope: `cpp-vendor-build` as a lane whose claim is only that
  the vendored tree configures and builds; the `cpp-mutants` runner itself, with **direction controls**
  — a patch may declare a lane it must NOT break, so "the lane caught it" and "the patch broke the
  build" stop being the same exit code; and the isolation wrapper's own positive control, which proves
  it isolated (reachable outside, blocked inside) before trusting itself, and reports INVALID rather
  than green when it did not. macOS has no `unshare`; `sandbox-exec` is the mechanism here and the
  Linux branch has never been executed.
- **Revert:** **Surface** (`gtest`'s API). Independently *replaceable* at any time — re-pin to another
  commit and nothing above notices, because everything above depends on the framework's API and not on
  the vendoring. Independently *revertible* only if nothing above has landed. The distinction matters:
  "we can change this" is true forever; "we can remove this" expires at B1.1.

---

**B1.1 — the skeleton, the closed enums, and the un-stubbing.** *Gated on: §12.3 item 1's Track A items.*

- **Lands:** CMake skeleton producing the static archive; `Status::Code` (closed, `-Werror=switch`, no
  `default:` arm); `RunOutcome`; `CountsAsRecoveryEvidence` as the single policy site (§7.5);
  `cpp-test`/`cpp-asan`/`cpp-ubsan` un-stubbed and `cpp-tsan` live.
- **Gates + induced failure:** every lane fails loudly — *plant a failing test in each of the four lanes
  and observe four reds*, one per lane, not one red taken as evidence for four. Characterization is not
  evidence — *make `CountsAsRecoveryEvidence` accept `kCharacterizationOnly`*. The closed-enum property
  — *add an enumerator and omit its arm; the build must fail*, which is what makes a future suppression
  reason a compile error until someone classifies it.
- **Mutants:** `BM13`.
- **Depends on:** B1.0.
- **Retires (§14.1.1):** **nothing, and that is the ordering invariant working rather than luck.** B1.1
  is the first step at which any test of ours exists, so there is no earlier body to retire. B1.0's two
  checks are about the vendored tree and the network and cannot be engine-account tests, there being no
  engine — recorded so the empty answer is a classification and not an omission.
- **Landed `646fca5`.** Beyond its written scope: each sanitizer lane **asserts at compile time** that
  it has the sanitizer it claims, and `cpp-test` asserts it has none — a lane that lost its
  `-fsanitize` flag now fails to build rather than passing quietly, which is a guarantee no canary can
  give because it cannot be reached by a test that was skipped. Also `cpp-build` (build everything, run
  nothing), which exists **solely** as a control lane for mutants and has since separated a caught
  mutant from a broken patch three times; and `rift_rig` as an archive separate from `rift_engine`, so
  the oracle is not reachable from the thing it judges.
- **Revert:** **Chokepoint, and the widest one in B1.** `Status::Code` and `RunOutcome` are consumed by
  every step above; nothing above survives their removal. The mitigation is not that the revert gets
  cheaper — it is that the enum can only *grow* under §7.6 clause 6, so the surface stays small enough
  that a replacement is conceivable. `CountsAsRecoveryEvidence` is separately revertible, being one
  function with one caller; that is deliberate and is why §7.5 insists on the single policy site.

---

**B1.2a — the Env choke point, with no implementation behind it.**

- **Lands:** the `Env` NVI surface (public non-virtual intercepts, private pure virtuals implement); the
  `CallSite` enumeration; the 1:1:1 count assertion binding public wrappers to private virtuals to
  `CallSite` enumerators (§3.2).
- **Gates + induced failure:** the count assertion — *add a public virtual to the base, bypassing
  interception*; and *delete one `CallSite` registration*, the other direction of the same 1:1:1.
- **Mutants:** `BM17` (its static half; the reachability half is B1.3's).
- **Depends on:** B1.1.
- **Landed `29bebd2`.** Beyond its written scope: §3.3 gained two rows it had always implied but never
  printed (`Env::NewDirectory`, `RandomAccessFile::Read`) — recorded there as a **correction**, not an
  addition. The scan parses under a **strict grammar**: a line it cannot classify is a lane failure,
  never a line it skips, because a parser that silently ignores what it does not understand reports the
  health of its own grammar and calls it coverage. And `BM17c`, which is not in the catalogue: the scan
  reads *declarations*, so it cannot see which `CallSite` a wrapper actually passes to `Intercept`, and
  a copy-paste slip there satisfies 1:1:1 perfectly while aiming a kill point at the wrong call. Its
  control is cpp-scan staying green.
- **Revert:** **Chokepoint.** Every Env implementation, every fault injection point and every kill point
  above stands on it.
- **Note:** it lands with **zero implementations**, and can, because the count assertion is structural.
  That is the entire reason it is separable from B1.2b — see §14.4.

---

**B1.2b — `PosixEnv` and the raw-write seam.**

- **Lands:** `PosixEnv` implementing the private virtuals; the internal raw-write seam; its short-write,
  `EINTR` and zero-return unit tests.
- **Gates + induced failure:** each of the three seam tests, induced by returning a short count, `EINTR`,
  and zero from the raw seam respectively.
- **Mutants:** none. `PosixEnv` is outside the fault matrix by §11 idealization 2 — short writes are
  unit-tested here and are deliberately absent from the kill-point sweep, so they never combine with
  another injected fault in one run.
- **Depends on:** B1.2a.
- **Landed `cf12938`.** Beyond its written scope: the three seam gates exist as repeatable `SEAM-*`
  patches rather than a one-time hand induction — there is no CI here, and an induction nobody can
  re-run decays into a sentence in a commit message. §14.2's "Mutants: none" is a statement about the
  **BM catalogue**, not a statement that the gates need no induction. The seam test's fake writer also
  **caps its own call count**, so a `WriteFully` that fails to terminate fails a test instead of
  hanging a lane: "must not spin" is a liveness property, and a lane that hangs while checking one
  reports nothing at all.
- **Revert:** **Leaf — the only unqualified one in B1.** No B1 test uses `PosixEnv`; every B1 test runs
  on `TestEnv`. It becomes load-bearing at B5's cgo path and at I1, and until then it is the single step
  in this sequence that can be removed at any moment with nothing above it noticing.

---

**B1.3 — `TestEnv`, and threshold 3.**

- **Lands:** `TestEnv` with the `content`/`durable` split implementing §4's power-loss model; the fault
  controller and the fault matrix of §3.3; the ledger; the kill mechanism of §9.5 (in-process dead flag
  plus the sampled real `_exit`); the `CallSite` reachability census and the kill-point census.
- **Gates + induced failure:** the durability model — *a kill discards everything written since the last
  covering `Sync` returned*, asserted against the ledger rather than against the engine. `CallSite`
  reachability — *delete a public wrapper's registration; the census must report an unvisited
  enumerator*. Kill-point census — *add an Env call and do not update the census; the sweep must report
  the change*. The ledger's own induced failures.
- **Mutants:** `BM17` (reachability half).
- **Depends on:** B1.2a. **Not** on B1.2b.
- **Retires (§14.1.1):** **nothing, subject to one classification this step must perform rather than
  assume.** `TestEnv` is the durability observer, so every earlier test must be shown to make no
  durability claim. The only earlier tests are B1.2b's three seam tests, and they are statements about
  a syscall's **return value** — short count, `EINTR`, zero — not about durable state. B1.3 records that
  classification explicitly; had any of them asserted that bytes were on disk, it would be retired here,
  because before this step nothing could have justified the assertion.
- **Landed `3239469`.** Beyond its written scope: `AllCallSites()` became a **fourth** artifact bound
  by the correspondence, because the census iterates it and a hand-written array beside a hand-written
  enum is a drift waiting to happen (`BM17d`). `TestEnvironment::ContentNow` was added because
  `content = durable` on a kill is otherwise unobservable — after a kill the Env is dead and cannot be
  read through — and an unobservable line of a model is a line that rots. The real-`_exit` half needed
  somewhere to put the durable image, and that somewhere could not be `TestEnv`: writing a file from
  `engine-cpp/src` outside `env/posix/` is exactly what the A5 scan bans, so `TestEnv` offers the image
  through a hook and `rig/durable_mirror` owns the file. `rig::OutcomeFloor` is the mechanical link
  from "a registry injector ran" to `kCharacterizationOnly`.
- **Revert:** **Chokepoint.** Every gate from B1.5 upward runs through it. It is also the step whose
  revert is least visible and most damaging: the tree still builds without it, and everything above
  quietly stops being able to fail correctly.

---

**B1.4 — the scope scan.**

- **Lands:** the A5 scan lane (§9.4); `CPP-HATCHES.txt` as the checked-in registry; the blind-patch set
  (DR-27).
- **Gates + induced failure:** *plant a raw `::open` in an engine source; the lane must fail*. *Add an
  unused registry entry; the lane must fail* — no dead hatches. Each blind patch must be caught.
- **Mutants:** none of its own — and it is the reason two others cannot exist as committed files. `BM6`
  includes `<random>` and `BM14` removes a lock, both of which this lane rejects at scan time, so they
  exist only as patches applied to a scratch tree.
- **Depends on:** B1.1 (lanes), B1.2a (the seam it is scanning for).
- **Claim it carries — the retraction target, named to the sentence:**
  - `CLAUDE.md`, *Determinism and fault injection, C++ side*, preamble: *"Env is the C++ side of the same
    boundary the Go determinism pass enforces: every syscall goes through it for the reason every clock
    read goes through `Clock`."*
  - `CLAUDE.md`, same section, bullet 1: *"The engine performs all file operations through an `Env`
    abstraction (open, read, write, sync, rename, list), LevelDB style."*
  - `CLAUDE.md`, **Amendment A5**: *"every syscall on the C++ side through Env"*, and *"C++ through the
    Env seam"*.
  - This document, **§3.2.1** and **§11 item 8**: the *"two independent checks"* residual — which becomes
    one check, and then zero.
- **Landed `187a3eb`.** Beyond its written scope, and the first item is a **defect in ratified work
  found by implementing the rule that catches it**: B1.3's `TestEnv` held
  `std::map<const void*, std::string>`, a pointer-keyed container, which §6.1 bans outright and says
  this scan checks. Nothing behaved wrongly — the map is looked up and never iterated — but there is no
  honest split label for it, so it was **fixed rather than exempted**: `HandleId`, an integer assigned
  sequentially by the creating Env, now threads the whole surface, which also makes a kill point
  reportable as `Sync(handle 3, 000001.log)` instead of an address that means nothing on the second
  run. The readdir enumeration moved to the raw seam and gained four tests for the same reason — it was
  the one function in `PosixEnv` with real logic and no instrument, and the thinness rule has no true
  label for that. Also `cpp-scan-blind` (ten rules, ten fixtures, one surviving canary),
  `cpp-cold-cache` (§14.5's note), and `CLAIMS.txt`. **Retires: nothing** — the scan observes source,
  not the engine's account of itself.
- **Revert:** **Mechanically a leaf; epistemically a chokepoint.** Nothing consumes the lane, so
  reverting it breaks no build and reddens no test — and it reduces every sentence above from a property
  to programmer discipline. **This class is the dangerous one**, because it is the class that can be
  dropped under schedule pressure with no immediate signal. Standing rule: *a step in this class may not
  be reverted without the claim it supports being retracted in the same diff* — and the claim is the four
  named sentences, not the general idea, because a rule that says retract the claim is weaker than one
  that says retract this sentence in this file. B1.9b is the other member.

---

**B1.5 — the memtable. The first engine code in the project.**

- **Lands:** the skiplist memtable under the DB mutex (B1-D6c); the arena; deterministic tower heights as
  a pure function of the key (B1-D6b) with their golden vectors; the structural digest;
  `kConcurrencyClaim` as the single sanctioned wording (§6.4).
- **Gates + induced failure:** height golden vectors — *shift the tower mapping `/2` → `/3`*.
  Deterministic memtable shape — *swap in a PRNG height source; the structural digest must differ across
  runs*. The memtable is actually locked — *remove the mutex from the write path; the TSan harness must
  report a race*, which is what proves the TSan lane is not decoration. The claim is not upgraded —
  *edit `kConcurrencyClaim` toward "race-free"*.
- **Mutants:** `BM6`, `BM14`, `BM22`, `BM23`.
- **Depends on:** B1.1, B1.3 (the TSan harness drives `Apply`/`Get` against `Sync`).
- **Revert:** **Surface** (the ordered-map interface B1.7b applies into and B1.8 reads through).
  Replaceable, but a replacement invalidates the golden vectors and the structural digest, because both
  are specific to this height mapping — so the revert is "implementation plus two fixture sets", not
  "implementation".
- **Note:** B1.5 and B1.6 are **the only pair in the sequence with no dependency in either direction**.
  They are the only two that could land in the other order, and the only two that can be reverted without
  reference to each other.

---

**B1.6 — the WAL writer.**

- **Lands:** framing and the frozen record layout (§5.3); fragmentation across block boundaries; sync
  groups and `GROUP_END`; the engine-owned WAL buffer (B1-D9); `kMaxRecordBytes` and `kWalBufferBytes`
  with the `cap ≥ 2 × max_record` ordering invariant; the `regime` field and regime-keyed aggregation
  (§8.4); the byte-digest test.
- **Gates + induced failure:** the pinned WAL byte digest — *leave one padding byte uninitialized; the
  digest must differ across runs*. Fragmentation across a block boundary. `Apply` performs no I/O —
  *move the WAL buffer into `WritableFile`; the per-thread Env-call counter must fire*. The mutex is
  never held across an Env call — *hold the DB mutex across `Sync`; the depth guard must fire*. The
  buffer tripwire halts — *stall the syncer past the cap; the run must halt as `kVoid`, not OOM*. The cap
  ordering invariant — *construct with `kWalBufferBytes < 2 × kMaxRecordBytes`; construction must fail*.
  Record-cap and buffer-cap adjudication, **both directions each** — *trip on a legal-size record* and
  *accept an over-cap record*; both must fail the run, neither may void it. Regimes never aggregate —
  *summarize a lowered-cap run together with a default-cap run*. Directory sync — *kill between file
  creation and `Directory::Sync`*.
- **Mutants:** `BM4`, `BM6`, `BM9`, `BM12`, `BM16`, `BM18`, `BM19`, `BM20`.
- **Depends on:** B1.1, B1.3.
- **Landed `dfba754`, and AFTER B1.7a rather than before it.** Ruled: the torn-tail rule and
  fragment-chain legality are the freeze surface, so their gates are induced *before the writer is
  trusted*. That is not a re-ordering — B1.7a's own entry says it depends on B1.1 alone, and §14.4
  gives the reason the split exists. Taking that freedom means the writer's output is checked against
  rules already seen to reject every illegal shape, rather than against a decoder written to agree
  with it. Beyond its written scope: §8.3's two assertions live in the interception layer as
  `env_guard.h`, so they cannot be bypassed for the same reason the fault controller cannot; `DbLock`
  ties the mutex-depth marker to the mutex itself, because a separate marker would be left behind by
  exactly the edit BM16 makes; the cap-ordering invariant is a `Status` rather than an abort, since
  §10.2's induced failure has to be *observed* and an abort is observable here only through a death
  test; and the guard has a settable violation handler for the same reason `RawWriteFn` exists — a
  path whose only outcome is `abort()` cannot be exercised by four sanitizer lanes.
- **Revert:** **Chokepoint.** B1.7a and B1.7b read what it writes and B1.9a's oracle is defined against
  its groups. It also carries the determinism spine: the byte digest is the C++ analogue of Track A's
  trace hash, catching ambient randomness, uninitialized padding and any float on a serialization path in
  one test, and is why MSan stays declined. Reverting B1.6 retires that too.

---

**B1.7a — the WAL reader, drivable from bytes alone.**

- **Lands:** fragment decode; CRC verification over `length ‖ type ‖ payload` (the stated departure from
  LevelDB, §5.3.3); fragment-chain legality and its six-case table (§5.4.2); resync; interior-corruption
  detection with offsets.
- **Gates + induced failure:** CRC covers the length — *corrupt only the length field of a fully synced
  fragment; the CRC must fail at a known offset*. Illegal fragment transitions — *plant `FIRST`
  immediately followed by `FIRST`, both CRC-valid*. Interior-corruption detection — *flip one byte inside
  a fully synced group; the open must fail with an offset*. Interior corruption is not truncated — *make
  recovery stop at the first bad record; the planted corruption must go from "refused open" to "silent
  data loss"*, which is the induced failure that names the actual consequence.
- **Mutants:** `BM3`, `BM8`, `BM10`, `BM11`.
- **Depends on:** B1.1 only, for its gates.
- **Landed `e287627`, BEFORE B1.6.** Carries the frozen record layout with it, because a decoder is
  defined against a format and cannot precede one. The CRC divergence from LevelDB lives on the
  `FragmentCrc` helper as §5.3.3 requires, and reader and writer call that one helper so the covered
  range has a single definition. BM10 is the one mutation in the catalogue a reviewer would most
  likely *approve* — it introduces no bug, it aligns us with upstream — which is why the property is
  asserted **directly** on the helper (same type, same payload, different length ⇒ different
  checksum) rather than inferred from end-to-end behaviour, where a corrupted length fails the
  checksum under either coverage and only the *knownness of the failure offset* differs.
- **Revert:** **Surface** (the decode interface B1.7b consumes).
- **Note:** **every gate here is drivable from hand-built byte images** — no writer, no memtable, no Env
  faults, just fixture bytes. That makes this the cheapest place in the sequence to induce failures
  exhaustively, and it is why the reader is separated from recovery. See §14.4.

---

**B1.7b — recovery.**

- **Lands:** the torn-tail rule, single-block (§5.4.1) and multi-block (§5.4.2); gapless WAL numbering and
  §7.2's `max+1` rule; the watermark computation; apply-into-memtable.
- **Gates + induced failure:** torn tail, single block — *make recovery accept `BATCH` records after the
  last `GROUP_END`*. Torn tail, multi-block — *truncate mid-`MIDDLE` and assert the tail is discarded;
  then plant a valid `FULL` after the gap and assert the open fails*. Gapless numbering — *delete a WAL
  file; the open must fail*.
- **Mutants:** `BM2`, `BM3`, `BM4`, `BM11`.
- **Depends on:** B1.3, B1.5, B1.6, B1.7a — the most-depended step in the sequence.
- **Landed `a634266`.** Two things it added that its written scope did not name, both because a test
  demanded them. The FILE_HEADER is validated **unconditionally**, before the committed-records loop:
  written inside that loop it was conditional on a GROUP_END existing, so a WAL with the wrong name
  passed whenever it held no closed group — and §5.3.4 lists a foreign file and a file whose name and
  contents disagree among the things that record exists to catch. And `Slice(std::string&&)` is
  **deleted**, after ASan caught a dangling Slice in the mutant lane's baseline gate; twenty call
  sites were latent instances of the same bug, safe only by accident of lifetime.
- **Corrects B1.3.** `SuspendsExactness` classified a **prefix-granular** torn `Sync` as
  exactness-suspending. B1-D5 rules prefix as *the contract model* — §7.4's two-element set is that
  exact case and the engine is held to exactness under it — and only the sector-subset mode suspends.
  The error was conservative and therefore invisible to every lane: mislabelled runs still passed
  every assertion, they were merely unbankable. At B1.9a it would have made §7.4 condition 3
  **unreachable**, since "both elements were observed across the sweep" cannot be satisfied by runs
  that are structurally uncountable as evidence. The injector is split; the registry now holds exactly
  the two members §7.5 names.
- **Revert:** **Chokepoint.** B1.8 and B1.9 both consume it.

---

**B1.8 — the frozen interface, met exactly.**

- **Lands:** `Open`, `Close`, `Write`, `Get`, the iterator, snapshots; `DeleteRange` over the memtable
  with §8.1's expansion at `Apply` and the WAL recording the expansion.
- **Gates + induced failure:** the semantics suite mirroring `engine/model`'s. `Apply` performs no I/O,
  re-asserted here because `DeleteRange` expansion is the operation most likely to violate it — *move a
  read to the file layer*. `Close`'s error return is not dropped — *ignore it*, which is an
  exactness-(ii) failure, not a tidiness one.
- **Mutants:** `BM7`, `BM9`.
- **Depends on:** B1.5, B1.7b.
- **Landed `4611e50`.** **Two divergences from the frozen shape recorded for the first time**, beyond
  the two §7.1 already rules (`OnDurable` absent, `sync` flag absent):
  - **Go's nil-versus-empty has no `Slice` equivalent**, and the frozen interface depends on it —
    `InRange` treats a nil bound as unbounded, and *an empty key is a valid key* in this engine, so
    `Slice()` cannot mean both. Bounds are a `Bound`, explicitly one or the other.
    `DeleteRange(At(""), At(""))` deletes nothing; `DeleteRange(Unbounded, Unbounded)` is §8.2's
    clear-everything case. Conflating them would have made the case A3 was ruled for unreachable.
  - **`ApproximateDiskBytes` scans in B1.** The frozen comment says it answers from table metadata
    rather than by scanning; B1 has no tables. Exact and O(n) until B2 gives it something to
    approximate from — a performance property that will change, not a semantic one.
- **§8.1 exercised by a real expansion, not a fixture.** An over-cap `DeleteRange` at a lowered cap is
  refused and applies nothing, with the run mechanically marked non-default regime; and a 3000-key
  expansion spanning blocks is torn mid-record, classified as a torn tail, and discarded **whole**. So
  §5.4.2's multi-block rule is met by the operation that makes multi-fragment records routine rather
  than only by hand-built bytes.
- **Revert:** **Surface.** B1.9's sweep drives this API and has nothing to drive without it.
- **Note:** "correct" for B4 means byte-identical to `engine/model`, so this step's real acceptance test
  is not its own suite but B4's differential rig. The suite here exists to make B4's failures debuggable,
  not to substitute for them.

---

**B1.9a — the oracle and the two-element verdict.**

- **Lands:** the exactness oracle of §7.3, built from the harness's submission log, its reference state
  and TestEnv's ledger, and **asking the engine nothing**; the two-element recovery set of §7.4; the
  verdict type that names which element it landed on.
- **Gates + induced failure:** exactness (i) — *make recovery accept records past the last `GROUP_END`*.
  No over-promise (ii) — *advance the watermark before `Sync` returns*. Lands on `G_{k−1}` —
  `RecoveryLandsOnPreviousGroupWhenSyncIsTorn`, kill inside `Sync`, durability not applied. Lands on
  `G_k` — `RecoveryLandsOnInFlightGroupWhenSyncCompletesButIsPreempted`, durability applied, kill before
  the return. The verdict names its element — *return a boolean verdict; the oracle's own test must
  reject it*.
- **Mutants:** `BM1`, `BM2`, `BM5`, `BM15`.
- **Depends on:** B1.3, B1.7b, B1.8.
- **Retires (§14.1.1):** **a real, named set — this is the one non-empty case in the sequence.** B1.7b's
  recovery assertions land four steps before the oracle does, and they divide in two. Those that compare
  recovery's result against a **fixture the harness built** are sound and survive: the expectation came
  from the harness's own construction, not from the engine. Those of the self-consistent form — *read
  the engine's reported watermark, then check the engine's recovered contents agree with it* — are
  engine-account tests, they pass, and they would look like coverage. **B1.9a deletes or re-derives every
  assertion of that second form**, replacing it with one against the harness's submission log and
  TestEnv's ledger per §7.3. B1.7b must therefore tag which of its assertions are which as it writes
  them; an untagged recovery assertion is treated as the second form.
- **Revert:** **Surface.** B1.9b is defined in terms of it.

---

**B1.9b — the sweep, the adjudication, and the mutant campaign.**

- **Lands:** the full kill-point sweep across every Env call in the write path; §7.6's adjudication wired
  so every engine error is closed harness-side; §7.5's single suppressing-injector registry in use; the
  mutant campaign with kill-point budgets and per-mutant kill-time recording.
- **Gates + induced failure:** both set elements observed — *run the sweep with the in-flight case
  suppressed; the assertion must fire*, which is what stops a sweep from passing while only ever
  exercising one of the two legal outcomes. Both suspending injectors use one mechanism — *enable the
  sector-subset torn `Sync` and assert the outcome is `kCharacterizationOnly` without a second flag
  existing*. Every mutant killed within its budget, with seeds-to-detection and wall-time-to-detection
  recorded per A2.
- **Mutants:** the full catalogue.
- **Depends on:** everything.
- **Claim it carries — the retraction target, named to the sentence:**
  - `CLAUDE.md`, **Mission**, headline claim 6: *"Zero safety violations across [N] seeded fault
    schedules, [M] operations, and [H] cumulative CPU-hours of fault-injected soak (tracked in
    SOAK.md)…"* — the storage-engine half of it.
  - `CLAUDE.md`, **Resume lines**, bullet 3: *"Zero safety violations across [N] seeded fault schedules…
    spanning crashes, partitions, reordering, bounded clock skew, torn writes, and lost unsynced
    writes"* — *torn writes* and *lost unsynced writes* are this step's words and nobody else's.
  - `CLAUDE.md`, **Track B phase B4** exit: *"kill-point sweep green across the full write path"*.
  - `CLAUDE.md`, **Amendment A2**: *"Every BUGS.md root cause must answer 'which mutant class would have
    caught this.'"* — unanswerable with no campaign.
  - `SOAK.md`: every Track B row, and the inconclusive column beside it.
  - This document, **§10, "How B1 proves itself"** — the section title becomes false, not merely thinner.
- **Revert:** **Mechanically a leaf; epistemically the whole B1 claim** — the second member of B1.4's
  class, and the standing rule applies identically, against the six targets above. **A mutant surviving
  its budget means the rig is too weak and B1 is not done, regardless of what the clean runs say.**

### 14.3 The revert map

| step | class | revertible alone when | what its removal actually costs |
|---|---|---|---|
| B1.0 | Surface (`gtest` API) | nothing above landed; *replaceable* always | the framework, and the offline-clean-clone claim |
| B1.1 | **Chokepoint** (widest) | never, once anything is above | everything |
| B1.2a | **Chokepoint** | never, once B1.3 is above | the Env seam and every kill point |
| B1.2b | **Leaf** | **always** | nothing until B5 |
| B1.3 | **Chokepoint** | never, once B1.5 is above | every gate's ability to fail for the right reason |
| B1.4 | mech. leaf / **epist. chokepoint** | mechanically always — **forbidden without retracting the claim** | the Env-seam claim becomes discipline, not a property |
| B1.5 | Surface (ordered map) | B1.7b and B1.8 rewritten against a replacement | the map, plus its golden vectors and structural digest |
| B1.6 | **Chokepoint** | never, once B1.7 is above | the WAL, plus the C++ determinism spine |
| B1.7a | Surface (decode) | B1.7b rewritten against a replacement | the corruption gates |
| B1.7b | **Chokepoint** | never, once B1.8 is above | the recovery contract |
| B1.8 | Surface (the frozen `engine/` API) | B1.9 has nothing to drive | interface conformance and B4's differential basis |
| B1.9a | Surface (verdict type) | B1.9b rewritten | the oracle |
| B1.9b | mech. leaf / **epist. whole claim** | mechanically always — **forbidden without retracting the claim** | all of B1's verification value |

Three readings worth stating plainly. **One:** the sequence has exactly one unqualified leaf, B1.2b, and
it is production-Env code that no B1 test touches — so "we can back this out cheaply" is true in
precisely one place, and it is not a place anyone would want to back out. **Two:** four of thirteen steps
are chokepoints and they cluster low (B1.1, B1.2a, B1.3) with one in the middle (B1.6), which is the
correct shape: the irreversible commitments are the early ones and they are the cheap ones to get right,
because they are interfaces rather than implementations. **Three:** the two steps that *can* be reverted
freely at any time, B1.4 and B1.9b, are the two whose removal costs the most and signals the least. No
mechanism can catch that — a lane that is gone cannot fail — so they get a rule instead, and the rule is
made enforceable by naming its object: each of those two steps carries a **Claim it carries** list of
specific sentences in specific files, and reverting the step means deleting those sentences in the same
diff. *Retract the claim* is an instruction nobody can be held to; *delete this line of `CLAUDE.md`* is
one that shows up in review.

### 14.4 The three splits, and why revision 3's table was wrong to bundle them

- **B1.2 → 2a + 2b.** Revision 3 bundled the Env NVI surface with `PosixEnv`. They are a chokepoint and
  the sequence's only leaf, which is the worst possible pairing: bundled, the leaf inherits the
  chokepoint's revert cost and looks load-bearing when it is not. Separable because the 1:1:1 count
  assertion is **structural** and needs no implementation to run, so 2a can land and be proven able to
  fail with nothing behind it.
- **B1.7 → 7a + 7b.** The reader's gates are drivable from hand-built byte images; recovery's are not,
  needing the writer, the memtable and TestEnv's kills. Bundled, the cheap exhaustive half is gated on
  the expensive half's dependencies, which pushes the corruption gates later than they need to be for no
  reason but packaging.
- **B1.9 → 9a + 9b.** The oracle must exist before the sweep, or the sweep degenerates into "did it
  crash". Separating them also puts §7.4 condition 3's two named single-kill tests — the induced failures
  for both elements — ahead of the sweep that is later asserted to have visited both.

Each split is a repackaging. **No gate, mutant or decision moves, and none is added or dropped;** §10.1
and §10.2 are unchanged by this revision.

### 14.4.1 A property of `cpp-ci` the sequence did not name

**A lane that verifies an ABSENCE must run in a state where the thing could actually be present.**

`cpp-ci` claims no lane touches the network. It was resting on a warm `FetchContent` cache rather than
on the absence of a fetch — the isolation worked perfectly and had nothing to block — and mutant BM21
survived it. The mutant was right and the lane was wrong. Track A has hit the cousin of this twice.

So the cold cache is now part of what `cpp-ci` **means**, asserted rather than assumed: the lane
refuses when its build root exists and does **not** delete it, a successful run removes its own tree so
the next one is cold, and a failed run leaves its tree for whoever has to debug it. After the run it
checks directly that no `_deps` tree appeared — downstream of the isolation rather than in place of it,
so a failure of the isolation is still visible.

The gate took two attempts, and the first one is the reason this subsection exists rather than a code
comment. The draft asserted the build root's absence *immediately after the lane's own `rm -rf`*:
green unconditionally, including in the exact state the defect occurred in. It is the general form
recurring inside the fix for the general form, written with the form in front of me. **A rule you can
state and still violate one line later is a rule that needs a mechanism** — and the mechanism is that
the gate was run and printed `GATE DID NOT FIRE`.

### 14.5 What is deliberately not in this sequence

No flush, so the memtable and WAL set grow without bound and every B1 test is small; nothing here
exercises recovery across a flush boundary, and §7.2's `max+1` numbering rule expires at B2 when it does
(§11 item 5). No manifest — B1-D7, the log is the single authority. No SSTable, no compaction, no
iterator merge. `DeleteRange` is memtable-only; real range tombstones are B3 per Amendment A3, and must
land before any I2 benchmark number is taken. `kBusy` and the poller are B5, under §7.6.1's binding.

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

**B1-Q12** (not a `B1-D`, since it was opened by a ruling rather than proposed): **ruled as
recommended and closed.** `PosixEnv` thinness becomes a checked scan rule, stated at B1.2b and enforced
at B1.4; B1.8's semantics suite runs against `PosixEnv` on a real filesystem, **scoped as not
evidence**; the differential Env lane is B4's; real crash testing stays at I2. §13.
