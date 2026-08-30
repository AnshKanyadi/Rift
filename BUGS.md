# BUGS.md

Every bug caught by the simulator, the crash-consistency rig, or differential testing gets an entry
here. This file is the proof behind the verification claim: it is the difference between "we ran a
lot of tests" and "we can show you what the tests found and why they found it."

**What this file holds, and what it does not.** Entries here are defects in **Rift** — the real
system: `raft/`, `store/`, `kv/`, `router/`, `balancer/`, the C++ engine — found by the verification
machinery. Nothing else. The file's entire value is that a stranger can read any entry and know it is
a real defect in the real system, so anything that dilutes that reading is kept out:

- **Fixture defects** — bugs in `sim/toy`, the calibration protocol the harness is pointed at — live
  in `docs/TOY-FINDINGS.md`. The toy is not the system under test; it is scaffolding built to
  exercise the harness, and a defect in it is evidence about the *machinery*, not about Rift.
- **Harness defects** — bugs in the simulator, generator, or injectors themselves — are recorded in
  their fix commit and analysed in the relevant design doc, per DR-29.

Rift's system under test began existing at A1, and this file has been non-empty since.

**Rules**

1. Every bug found by a checker gets an entry. No exceptions for embarrassing ones — especially not
   for embarrassing ones.
2. Every entry must be reproducible: a seed (at the commit that contained the bug) **and** a plan
   bundle in `seeds/` (reproducible at any commit).
3. Every entry must answer **"which mutant class would have caught this?"** If none would have, a new
   mutant is added to `sim/mutants/` **in the same PR as the fix** — not as a follow-up.
   *(CLAUDE.md Amendment A2.)*
4. A bug that a checker did *not* catch — found by inspection, by a real-mode run, or by luck — is
   the most valuable entry in the file. It must additionally record what checker was missing and
   whether one was added.

**Counts:** 24 entries — 8 from A1, 1 from A2, 1 from A3, 6 from A4, 1 from A5, 7 from A6. *(The phase gate for A1 requires this file to be nonempty, because a
harness that finds nothing is a harness that is too weak. It is not a target: the gate is satisfied
by finding real defects, and every entry here is one.)*

**How a fixed bug reproduces.** Every entry names a bundle in `seeds/` and a patch in
`sim/mutants/`, and neither half reproduces the bug alone. The bundle carries the **schedule**; the
mutant carries the **defect**. Rift's defects are not flags — `raft/` has no flaw switch and must not
grow one — so once a bug is fixed its schedule replays clean, and the reproduction is two steps:

```
patch -p1 < sim/mutants/M25-restart-recovers-unsynced-writes.patch
go run ./cmd/simctl replay --bundle seeds/BUG-005
```

`meta.json` names the patch and `simctl replay` prints the second step, so the recipe is in the
bundle rather than only in this file. This is the same arrangement `docs/TOY-FINDINGS.md` uses, where
the flaw happens to be expressible as a scenario flag.

**Numbering.** `BUG-NNN` here, `TOY-NNN` in `docs/TOY-FINDINGS.md`, and the bundle directories match:
`seeds/BUG-001` belongs to this file and `seeds/TOY-001` does not.

**The seeds moved again at A3, for the same reason and by the same procedure.** A3 widened the mix
a second time — eight crashes and six partitions over fourteen seconds, and a four-node cluster with a
learner — because the power lane measured four classes dropping below their floors under A3's shape,
including the one that found BUG-009 falling to *zero of three thousand*. Every bundle was re-pinned
to a seed that still reproduces under its mutant, verified one by one. Bundles were already recording
their build, so each replays under the shape it was found in rather than today's.

**The seeds moved at A2, deliberately.** A2 widened the schedule mix — four crashes and five partitions over twelve seconds instead of two and three over eight — because pre-vote made the cluster calm enough that the harness stopped finding things (DESIGN-A2 §9). A seed names a run only relative to the generator it was drawn against, so every A1 seed named a different run afterwards and several stopped exhibiting their defect. Each bundle was re-pinned to a seed that does, verified by applying its mutant and watching the finding come back. Where a historical seed is part of the record it is kept beside the new one rather than overwritten.

**Bundles pin their build.** A raft bundle now records whether pre-vote, snapshots and transfers were on, and `replay` uses what the bundle recorded rather than today's defaults. Without that, A2 turning on three features silently changed what every stored bundle meant — not because the schedule moved but because the cluster did. BUG-001 through BUG-008 are pinned to the shape they were found in: no pre-vote, no snapshots, no transfers.

---


## Two id ranges, one file

This repository was built as two tracks in parallel and merged at I1. They numbered their defects
independently, so `BUG-004` meant two different things on the two branches until the merge.

| range | track | what it covers |
|---|---|---|
| `BUG-001` .. `BUG-037` | **Track A**, Go | the simulator, Raft, multi-raft, MVCC, transactions, read index, and the harness that checks them |
| `BUG-B001` .. `BUG-B007` | **Track B**, C++ | the LSM engine, its Env fault rig, the differential judge, and the cgo boundary |

**The letter is the point.** A second numeric block (101, 200) says nothing at a glance and the first
person to add an entry has to look up which block they are in. `B` says which track produced the
defect in the id itself, and it cannot be misread as a continuation of one sequence.

Track B's entries were renumbered at the merge and their internal cross-references rewritten in the
same commit — 43 inside the entries and 29 more in Track B's design docs, `.txt` registers, mutant
headers and C++ comments, which is the half that rots. Where a Track B entry cites `BUG-023` or
`BUG-032` it means **Track A's**, deliberately: those are the two places Track B found Track A had
already met the same class. The four `Track A's BUG-005` comments in `engine-cpp/` are cross-references
too, and say so in the text.

**The `GF-` sequence is NOT split, and that is deliberate.** All of `GF-1` .. `GF-40` were raised in
Track B, because Track B kept the register; Track A's general forms lived as prose in
`docs/TRACK-A.md` and the design docs, unnumbered. From `GF-41` the sequence is the repository's. A
general form is a claim about how this project works rather than about one track's code — `GF-42` was
raised from three instances in two tracks and one shared document — so splitting it by track would
mean numbering the same lesson twice. Bug ids split because a bug happens in a place; general forms do
not, because a general form is what survives the place.

## Template

Copy this block. Do not drop fields; write "n/a" with a reason instead.

```markdown
### BUG-NNN — <one-line symptom, in the voice of what an operator would see>

| field | value |
|---|---|
| **Found by** | sim / crash rig / differential / real-mode chaos / inspection |
| **Phase** | A1 |
| **Reproduce (seed)** | `simctl replay 8834127` at commit `<sha>` |
| **Reproduce (plan)** | `simctl run --plan seeds/BUG-NNN/plan.json` (any commit) |
| **Invariant that caught it** | e.g. Election Safety |
| **Mutant class** | e.g. `M2-ack-before-replicate`; or "none existed — added `M8-…` in this PR" |
| **Fix commit** | `<sha>` |
| **Minimized?** | yes — N fault entries, M ops, K nodes |

**Symptom.** What the checker reported, verbatim where possible.

**Root cause.** The actual mechanism, not the patch. Written so someone who has never seen this code
can follow it.

**Why the checkers caught it here and not earlier.** Which injector had to fire, in what order.

**What this would have caused in production.** Be concrete and be honest: silent data loss, a stalled
range, a lost acknowledged write, a duplicated transfer. If the answer is "nothing user-visible," say
that.

**Fix.** What changed and why that is the right fix rather than a narrower one that would also make
the seed pass.
```

---

## Entries

Newest last. Each is a defect in Rift found by the verification machinery.

---

### BUG-001 — the cluster never elected a leader, every request went unanswered, and every checker reported green

| field | value |
|---|---|
| **Found by** | sim — the **election census**, and nothing else |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M21-decode-off-by-one.patch && go run ./cmd/simctl replay --bundle seeds/BUG-001` (any commit) |
| **Reproduce (seed)** | every seed exhibited it; `seeds/BUG-001` carries seed 0 |
| **Invariant that caught it** | **none.** No safety oracle could see this, and none did |
| **Mutant class** | none existed — added `M21-decode-off-by-one` in this PR |
| **Fix commit** | `ff895a0` |
| **Minimized?** | n/a — the defect was schedule-independent; there was nothing to minimize |

**Symptom.** `store/codec.go`'s `decodeMessage` read eleven `uint64`s where `encodeMessage` wrote
ten. Every frame failed to decode, `Handle` returned early on every delivery, and **no message in the
cluster was ever received**. No node became leader. All forty client operations went unanswered.

Porcupine returned **PASS**.

**Root cause.** An off-by-one in a hand-rolled decoder: the loop bound was `[11]uint64` against ten
`putU64` calls, so `takeU64` ran out of bytes on the last field and returned `ok == false`. The
caller's response to an undecodable frame is to drop it, which is correct behaviour for a corrupt
frame and indistinguishable from a network that delivers nothing.

**Why the checkers caught it here and not earlier — and why only one did.** *A history of unknowns is
trivially linearizable.* Every one of those forty operations was still in flight when the run ended,
and an in-flight operation is a free choice for a linearizability checker: it may place the operation
in whichever world satisfies the rest. With no decided operations there is nothing to satisfy, so the
checker is free, and free means green. The four safety oracles were equally green and equally
correct: no node ever led, no log ever diverged, no entry was ever applied.

Total system failure, clean safety verdict. What caught it was the **election census** — a liveness
count, not a safety oracle — reporting zero elections won.

**What this would have caused in production.** Total unavailability presented as health. Every safety
monitor green, every dashboard clean, and not one write completing. It is the worst shape a bug can
have, because the verification machinery actively vouches for the failure.

**Fix.** The loop bound corrected to ten, and the codec's own round-trip test extended. But the fix
is not the lesson.

**The lesson, which is now a rule.** A safety oracle over an unknown-dominated history is **vacuous**,
so every safety claim in this project is paired with a liveness census proving the system did the
thing whose safety is being asserted. That rule is now structural in two places rather than a habit:
`sim.CheckAll` reports **inconclusive** for a history below `UnknownDominatedPerMille` decided, and
`hunt.markVacuousIfNoLeader` reports inconclusive for a run that elected nobody. Both are induced by
their own mutants (`M15`, `M16`), and the census is reported in full beside every sweep.

Replaying this bundle with the defect reintroduced prints the whole story in two lines:

```
census   recorded terms=2  elections-started=2  elections-won=1 split-votes=2
         replayed terms=79 elections-started=79 elections-won=0 split-votes=75
```

Seventy-nine terms, no leader, and not one violation reported.

---

### BUG-002 — a replica went silent after a restart and never spoke again; the cluster stalled with every checker green

| field | value |
|---|---|
| **Found by** | sim — `raft.AssertQuiescent` |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M14-epoch-check-removed.patch && go run ./cmd/simctl replay --bundle seeds/BUG-002` (any commit) |
| **Reproduce (seed)** | `seeds/BUG-002` carries seed **32** (was 40, and 66 before that; see *the seeds moved* below and §A5 rot) |
| **Invariant that caught it** | none of the four — this is a liveness stall, and `AssertQuiescent` is the mechanism that refuses to call one a clean run |
| **Mutant class** | `M14-epoch-check-removed` |
| **Fix commit** | `ff895a0`, made structural in `04d8d20` |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6); the bundle is the whole schedule |

**Symptom.** A node that had restarted twice stopped sending anything. It was up, it was stepping
messages, and every message it produced sat in the withheld queue forever. The cluster made no
progress and no checker objected.

**Root cause.** A restart arriving while a node was already up replaced the `*raft.Raft` but kept the
previous incarnation's `pendingMark` list. A sync completion for a write the *old* Raft had requested
then arrived and was turned into `AckPersisted(m)` on the *new* Raft — which had never issued mark
`m`. That acknowledgement closed nothing. The mark the new Raft was actually waiting on was never
acknowledged, and every message gated on it was withheld for the rest of the run.

The general shape: **a completion from a dead incarnation reaching a live component.** This project
found that same shape three times in three components (TOY-003, this, and the
`durability advanced to 35, past the last applied sequence 34` panic), each time patching it locally
with a different ad-hoc comparison.

**Why the checkers caught it here and not earlier.** They did not, and could not. A message gated on
a mark that is never acknowledged is **indistinguishable from a message that was never generated**:
the cluster simply stops, every safety oracle stays green because nothing unsafe happens, and the run
reports quiescent. `AssertQuiescent` exists precisely to make that state a failure instead of
silence, and it is the only thing that could have found this.

**What this would have caused in production.** A replica that silently stops participating after a
restart. The range keeps its nominal replication factor while actually running one voter short, so
the next failure takes the quorum with no warning that the margin was already gone.

**Fix.** The reset was the immediate fix. The real fix, in `04d8d20`, was `sim.Epoch`: every
incarnation carries a monotonically increasing epoch, every sync completion is stamped with the epoch
that issued it, and a cross-epoch delivery is dropped and counted. Epoch 0 means "no incarnation", so
a forgotten stamp is refused rather than defaulting into acceptance. The class became
unrepresentable rather than catchable — this project's house move, after Wall/Mono.

`M14` was declared in that commit and could not be validated then: it targets `./sim/hunt/`, whose
baseline was red while `TestRaftExitCriteria` failed, and the mutant lane correctly refuses to report
kills against a red baseline. It was validated at `01d6fe4`, and validating it found that the epoch
guard was itself being **counted and never asked about** — the sixth mechanism in this repository to
be declared, wired, and never invoked.

---

### BUG-003 — a node went quiet holding messages nobody would ever release

| field | value |
|---|---|
| **Found by** | sim — `raft.AssertQuiescent` |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M23-gated-messages-never-released.patch && go run ./cmd/simctl replay --bundle seeds/BUG-003` (any commit) |
| **Reproduce (seed)** | `seeds/BUG-003` carries seed **23**, re-recorded at A6 when the workload moved every raft trace (DESIGN-A6 §16); it carried seed 0 when the bug was found |
| **Invariant that caught it** | none of the four — a stall, again |
| **Mutant class** | none existed — added `M23-gated-messages-never-released` in this PR |
| **Fix commit** | `5264236` (`markHandedOff`) |
| **Minimized?** | no — same reason as BUG-002 |

**Symptom.** Same shape as BUG-002 from the outside, different mechanism inside. A node stopped
emitting, its withheld queue non-empty, with nothing outstanding that could ever drain it.

**Root cause.** A mark can end up covering **nothing**. `dirty()` opens a mark when persistent state
is mutated; a conflicting append then truncates the unstable tail, so the entries that opened the
mark are removed *before any `Ready` hands them over*. If the hard state did not also change, the
driver is handed a `Ready` carrying a mark and nothing to write. It never persists, so it never
acknowledges, so the mark never closes, so every message gated on it is withheld forever.

**Why the checkers caught it here and not earlier.** As BUG-002: safety oracles cannot see a stall.
`AssertQuiescent` is the instrument, and this is the second real defect it found.

**What this would have caused in production.** A leader that stops replicating mid-term after a log
conflict, which is exactly when the cluster least wants a silent participant.

**Fix.** `markHandedOff` records whether the driver has actually been given something to persist
under the current mark. A mark that has handed over nothing is satisfied by definition and is closed
in `Ready`, releasing what waited on it.

**A note on this entry's mutant, because the honest version is more interesting than the tidy one.**
The specific construction — a mark that covers nothing — is **no longer reachable**. Removing the
vacuous-close branch entirely produces zero stalls across 1000 seeds, because the BUG-005 fix changed
two things underneath it: a mark's coverage is now frozen at handover, and a truncation drops the
withheld acks that attested to the vanished entries. So the mutant plants the **class** instead of
the instance: a withheld message that is never released, which is what `AssertQuiescent` exists to
catch and what made this bug findable at all. A mutant that could never fire would be a mutant that
tells us nothing.

---

### BUG-004 — a client was told its write succeeded; no committed entry ever contained it

| field | value |
|---|---|
| **Found by** | sim — porcupine, over the client history |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M24-answer-by-position.patch && go run ./cmd/simctl replay --bundle seeds/BUG-004` (any commit) |
| **Reproduce (seed)** | `seeds/BUG-004` carries seed **2**; it has carried 0 and 17 before that — see *the seeds moved* below |
| **Invariant that caught it** | **Linearizability of single-key reads and writes**, not any of the four safety oracles |
| **Mutant class** | none existed — added `M24-answer-by-position` in this PR |
| **Fix commit** | `b1210cf` |
| **Minimized?** | no — same reason as BUG-002 |

**Symptom.** Node 1 proposed `put k04=v70`, it landed at index N in term 1, and node 1 was deposed at
term 4. Node 2 later placed a **different** command at index N. When index N applied, the driver
matched the client's outstanding proposal against the applied entry **by log index**, and told the
client its write had succeeded — for a slot somebody else's command occupied. No committed entry ever
contained that write.

Effect on the sweep: 300 seeds went from 26 violations to 3.

**Root cause.** *A log index is not a proposal identity.* An index is a position, and a later leader
may put a different command at the same position; that is legal Raft, not a defect. The identity of a
proposal is the identifier its proposer assigned it, which DESIGN-A0 D5 froze into
`Propose(id ProposalID, data []byte)` and the implementation had quietly dropped.

**Why the checkers caught it here and not earlier — and why the right ones stayed green.** The four
safety oracles were green **and correctly green**. An entry overwritten by a later leader violates
nothing: election safety, log matching, leader completeness and state machine safety all held
throughout. The Raft was correct and the driver lied about it. Only the client history could see the
difference, because the lie was told to a client and to nobody else.

Being able to say that out loud is what the ledger buys. The safety oracles watch the log; porcupine
watches what a client saw; and this is the case that proves those are different questions.

**What this would have caused in production.** An acknowledged write that never happened — the worst
outcome a transactional store has, and the one a user cannot detect or compensate for.

**Fix.** `ProposalID` restored to the frozen shape, threaded through `Entry`, persisted, and put on
the wire. The zero identifier is refused, so a caller cannot fall back to matching on index. A
proposal whose entry was overwritten is now **never answered**, and that is correct rather than a
gap: the client genuinely does not know, the history leaves the operation in flight, and the checker
treats it as may-or-may-not-have-happened. The honest answer.

**And a mechanism, because twice is not a slip.** Twice in A1 an interface the frozen design
explicitly rejected was implemented anyway, and both times the omission became the defect: `Advance`
instead of the gated queue, caught in review; `Propose` without `ProposalID`, caught only after it
produced this. `tools/d5conform` now pins every frozen D5 signature against the implementation's
exported surface, with `Advance` pinned as a required *absence*.

---

### BUG-005 — a follower acknowledged index 15 with 5 on disk; the leader could commit an entry that follower could still lose

| field | value |
|---|---|
| **Found by** | sim — the **persist-before-reply** oracle |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M25-restart-recovers-unsynced-writes.patch && go run ./cmd/simctl replay --bundle seeds/BUG-005` |
| **Reproduce (seed)** | found at seed **92**, instant **2592077256**, step 1086, at commit `f624c0a`. `seeds/BUG-005` carries seed **3**; it has carried 40 before that — see *the seeds moved* below |
| **Invariant that caught it** | persist-before-reply, which is DR-8's **first enumerated gate** |
| **Mutant class** | none existed — added `M25-restart-recovers-unsynced-writes` |
| **Fix commit** | `0c55e30` |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6); the bundle is the whole schedule |

**Symptom, verbatim from the oracle:**

```
persist-before-reply at instant 2592077256, step 1086: node 2 acked index 15 at instant 2592077256
with only 5 durable; the leader may commit an entry this node can still lose
```

**This is the project's thesis, demonstrated end to end, and it is why this entry keeps the number.**
DESIGN-A0's DR-8 enumerated this exact failure, in prose, **before `raft/` had a single line in it**.
It is the first gate in the enumeration, reproduced verbatim in the `Ready.Messages` doc comment:

> **MsgAppResp (accept)** — gated on: the appended entries AND HardState.Term durable. Without it:
> follower acks index i, leader counts it toward a quorum and commits; follower crashes, loses i,
> comes back and is elected with a shorter log ⇒ committed entry lost. Violates Leader Completeness
> and "committed is forever".

An oracle was then built *from that enumeration*, and it found *precisely that failure* in the
implementation, in a fault-injected run, reproducible from a single seed. **The enumerated gate and
the found violation match.** A design document predicting a specific failure and a checker then
finding exactly that failure is the entire argument this repository is making, and it is worth
saying plainly rather than leaving to be inferred.

**Root cause.** `store.restart` rebuilt the node by reading the engine, and an engine read returns
the **visible** state, which by construction includes batches applied and not yet synced — that
window is the whole point of the model (DR-15). A restart delivered to a node that was **not down**
therefore produced a process that recovered its own unsynced writes and then answered for them: the
precise inverse of the fault being injected. On seed 92 that is node 2 acking index 15 while the
engine's durable prefix ended at 5.

A restart is a death followed by a recovery, and the death half is not optional. The plan can
schedule a restart with no preceding crash, and a duplicated restart produces one too.

**Why the checkers caught it here and not earlier.** The gate needs a crash, a restart with a sync in
flight, and a leader still replicating into the window. The single-cut schedule mix produces that
combination, and 12 ms of modelled fsync against a 6 ms worst-case link is what makes the window
exist at all.

**What this would have caused in production.** A lost acknowledged write, by the exact mechanism
DR-8 wrote down. The follower acks an entry it has not written; the leader counts that ack toward a
quorum and commits; the follower crashes, loses the entry, and comes back with a shorter log. If it
then wins an election — and nothing stops it, because the up-to-date check compares against a log
that no longer contains the entry — the committed entry is gone. "Committed is forever" fails, and
the client was told the write succeeded.

**Fix.** A restart takes the crash first. Replaying this bundle with `M25` applied no longer reaches
the oracle at all: it is refused earlier, by `readDurable`'s precondition, which panics when the
engine is asked for its durable state while a write is in flight. The finding moved from a checker to
a structural refusal, which is the right direction.

**The first diagnosis was wrong, and the correction is worth keeping.** `f624c0a` attributed seed 92
to `persistedIndex` deriving durability from the *shape* of the unstable-entry slice — empty meant
"all durable", and `Ready()` clears that slice on **handover**, not on acknowledgement. That was a
real defect and its fix stands: `logTail` now records `persisted` and `handed` in fields with
different names, neither derived from the shape of anything else. It was not this bug. Seed 92
violated identically after that fix, at step 1085 instead of 1086, and the sweep went 3 → 2 rather
than to zero. **A fix that moves the number without reaching zero is a fix that has not been
attributed**, and taking the residue seriously rather than declaring victory is what found the four
defects this entry was split out of.

---

### BUG-006 — a follower acknowledged an index whose write was still in flight, because the durability token it waited on had grown

| field | value |
|---|---|
| **Found by** | sim — the **persist-before-reply** oracle, once it stopped reading the engine's own account (see the harness note below) |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M28-mark-coverage-grows-after-handover.patch && go run ./cmd/simctl replay --bundle seeds/BUG-006` |
| **Reproduce (seed)** | found at seed **0**, instant **4201040044**, step 1971. `seeds/BUG-006` still carries seed 0 |
| **Invariant that caught it** | persist-before-reply |
| **Mutant class** | none existed — added `M28-mark-coverage-grows-after-handover` |
| **Fix commit** | `0c55e30` |
| **Minimized?** | no — same reason as BUG-005 |

**Symptom.** `node 2 acked index 16 at instant 4201040044 with only 15 durable`. One entry in flight,
acknowledged. **257 of 300 seeds**, which is the largest single finding in A1.

**Root cause.** A `PersistMark` names a durability point, and `dirty()` reused an open one until it
was acknowledged. That is fine while nothing has been handed over; it stops being fine the moment the
driver starts writing. A reused mark's coverage **grows after the driver has already submitted the
first batch under it**, so the acknowledgement comes to mean strictly less than the messages gated on
it require: the driver reports batch one durable, and raft releases an append response attesting to
batch two.

It is also a liveness hazard on its own. Under a steady stream of appends a reused mark never stops
growing, so it never becomes fully durable, and every message gated on it waits behind a convoy that
never drains.

**Why the checkers caught it here and not earlier.** They could not, because the oracle was blind.
See the harness note below: while the ledger's durability record was an engine read-back it reported
15 as durable when 15 was merely *visible*, so the ack looked covered. This is the defect the silent
oracle was hiding.

**What this would have caused in production.** The same lost acknowledged write as BUG-005, reached
by a different route, plus a stall under sustained write load.

**Fix, and the measured redundancy that is worth recording.** A mark's coverage is now **frozen at
handover**: each `Ready` that hands something over takes its own mark, and anything mutated afterwards
gets a new one. Separately, the driver acknowledges a mark only when **every** write issued under it
is durable — written as defence in depth and documented as never firing.

It is not decorative. Measured over 300 seeds:

| defence removed | detected |
|---|---|
| raft freezing coverage at handover | **0** of 300 |
| the driver's all-writes-durable guard | **0** of 300 |
| both | **257** of 300 |

Either one alone prevents the defect entirely. `M28` therefore removes both, because a mutant that
removes one is not a mutant: it plants a defect two independent mechanisms already stop. That the
driver's guard turns out to be sufficient on its own is the interesting half — a comment saying "this
never fires" now has a number behind it saying what it would catch if the other half regressed.

---

### BUG-007 — a follower killed itself on a correct protocol step

| field | value |
|---|---|
| **Found by** | sim — the assertion fired on the first seed after the watermark it guarded started moving |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M29-truncation-refused-below-the-durable-watermark.patch && go run ./cmd/simctl replay --bundle seeds/BUG-007` |
| **Reproduce (seed)** | `seeds/BUG-007` carries seed **1**; it has carried 12 and 15 before that — see *the seeds moved* below |
| **Invariant that caught it** | `raft.truncateFrom`'s own assertion — a **false** one, which is the bug |
| **Mutant class** | none existed — added `M29-truncation-refused-below-the-durable-watermark` |
| **Fix commit** | `0c55e30` |
| **Minimized?** | no — same reason as BUG-005 |

**Symptom, verbatim:**

```
panic: raft: node 3 truncated to 13 with 13 already acknowledged durable; the driver
acknowledged an entry that later vanished
```

A node crashing, with a message confidently blaming the driver for a defect the driver did not have.

**Root cause. Durable is not committed.** The assertion refused *any* truncation reaching at or below
the durable watermark, reasoning that the driver would then have acknowledged an entry that later
vanished. That is a stronger claim than Raft makes and a false one. §5.3 of the paper has a follower
delete a conflicting entry and everything that follows it, and those entries are routinely already on
disk; a follower's persisted suffix being overwritten by a new leader is the protocol working, not
failing. What Raft guarantees is that a **committed** entry is never overwritten, because the
up-to-date check keeps a candidate missing one from ever winning.

**Why the checkers caught it here and not earlier — and this is the part worth reading.** They could
not, because the assertion was **unreachable**. `tail.persisted` was assigned nowhere in the package;
`persistedIndex()` had been returning 0 since it was written. A guard on a watermark that never moves
is a guard that never runs, and it sat in the tree looking like a safety property. It fired on the
first seed after the watermark started moving, which is the only reason anyone found out it was
false.

That is the same defect twice in one place: **a claim nothing exercised, guarding a watermark nothing
advanced.**

**What this would have caused in production.** A follower that panics on a legal conflicting append —
so it crash-loops precisely when a new leader is trying to bring it into line, which is exactly when
the cluster cannot afford to lose it. Worse, the panic message would have sent whoever read it to
look for a durability bug in the storage driver that was not there.

**Fix.** The assertion now says what Raft actually guarantees: a truncation at or below `commitIndex`
is a state machine safety failure and still panics. The gated queue is also swept on truncation — a
withheld append response attesting to an index that no longer exists has become a lie, and dropping
it is safe in a way that releasing it later is not.

---

### BUG-008 — recovery read back a log that was half one branch and half another

| field | value |
|---|---|
| **Found by** | sim — the driver's durability record compared against the engine's own account |
| **Phase** | A1 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M26-truncated-suffix-left-in-the-engine.patch && go run ./cmd/simctl replay --bundle seeds/BUG-008` |
| **Reproduce (seed)** | `seeds/BUG-008` carries seed **7**, re-recorded at A6 (DESIGN-A6 §16); it carried 12, and 84 before that — see *the seeds moved* below |
| **Invariant that caught it** | **Storage recovery** — "after any crash, the engine recovers exactly the acknowledged-synced prefix" |
| **Mutant class** | none existed — added `M26-truncated-suffix-left-in-the-engine`, and `M27-durable-record-ignores-a-clear` for the mirror direction |
| **Fix commit** | `0c55e30`, with the continuous cross-check in `56e3c18` |
| **Minimized?** | no — same reason as BUG-005 |

**Symptom, verbatim:**

```
panic: store: node 3 has made durable something its own record disagrees with:
recorded 19 durable entries, engine returned 20
```

**Root cause.** A `Set` overwrites only the keys it names. When a conflicting append truncated the log
from index *i* and raft handed over replacement entries *i..k*, the driver wrote exactly those keys —
and the engine went on holding the discarded branch's tail at *k+1..m*. Recovery then read back a new
prefix spliced onto a dead branch's tail and called the result a log. It is **gapless by index**, so
`raft.Restore`'s only structural check accepts it, and every entry above the cut belongs to a history
the cluster abandoned.

**Why the checkers caught it here and not earlier.** Nothing was watching. The engine's durable
content had no independent witness: the ledger was fed by reading the engine, so the engine agreed
with itself by construction. The checker that catches it did not exist until the driver started
keeping its own record of what it had made durable, and it needs a truncation of *already durable*
entries followed by the replacement becoming durable — measured at 53 clearing batches per 300 seeds,
of which 7 actually truncate durable entries.

**What this would have caused in production.** A replica that recovers a log it never had, passes
every structural check on the way up, and then serves and replicates it. Log matching fails silently
and the divergence is durable.

**Fix.** The batch clears the discarded suffix in the same atomic write, which is what `DeleteRange`
is in the frozen interface for (Amendment A3): the clear and the rewrite land together or not at all.

**And a checker, because the fix alone would have been unwitnessed.** The driver's durability record
is now compared against the engine's own account on **every** durability completion at which the
engine has nothing in flight — the one moment a read-back honestly *is* the durable state, and a
comparison that can only fail. 36,912 comparisons across 300 seeds. Moving it from recovery-only to
every-completion took this defect's seeds-to-detection from **905 to 84**, which is the difference
between a check that runs twice a run and one that runs whenever there is something to check.

---

### BUG-009 — a replica overwrote entries it had already applied and reported committed

| field | value |
|---|---|
| **Found by** | sim — the 10,000-seed exit run, through `raft.truncateFrom`'s assertion |
| **Phase** | A2 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M34-append-from-zero-over-a-snapshot.patch && go run ./cmd/simctl replay --bundle seeds/BUG-009` |
| **Reproduce (seed)** | **155** |
| **Invariant that caught it** | state machine safety, asserted inside `raft/` — *a truncation may not reach an entry this node was told was committed* |
| **Mutant class** | none existed — added `M34-append-from-zero-over-a-snapshot` |
| **Fix commit** | *(this commit)* |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

**Why 155 is the authoritative seed, and where the others went.** The
`Reproduce (seed)` field is parsed by `scripts/bundle-seeds.sh` and read by a
stranger following an instruction. **A field a script parses holds one value, not
a paragraph** — this one had accumulated three seeds and a search narrative, and
the first time the pre-push hook ran against a remote it refused the push over the
inconsistency. That refusal is correct and is the point of the check.

The history, which belongs here rather than in the field:

| seed | when it was authoritative | why it stopped being |
|---|---|---|
| **1364** | A2, at discovery — 1 of 3,000 seeds reaching the class | the trace space it indexed no longer exists |
| **13** | briefly, after BUG-022's read mark | regenerated **cleanly** — it replayed fine and no longer reached `M34` at all, which is the failure §16.3 warns a regeneration is |
| **105** | after BUG-022's fix, found in a 0–800 sweep | A7's term-start no-op moved every trace again; at HEAD it replays identically with `M34` applied |
| **155** | **now** | measured at HEAD: `detected=1 of=1` under A7's shape, and the bundle reproduces its finding under `M34` |

**And the search that found it is §5e.2b's rule in its first application.** `M34`
had two nulls against it — 0 of 3,000 in the gating lane and 0 of 6,000 in an
earlier re-pin search — and a third, 0 of 3,000 under `a2` at HEAD, taken here.
Before reading any of them as reach, the **aim** was varied along the four axes:

- **line** — `matches(idx == 0)` is the line that carries the property, and its
  comment says why. The aim is right. (Track B's `BM55` is the axis's origin.)
- **code position** — its covering test `TestSnapshotPrefixIsNotOverwritten`
  **passes with `M34` applied at HEAD**, so the mutant survives its own test.
- **role** — the receiver must be a node whose prefix is already in a snapshot.
- **time** — the snapshot must exist **before** the append arrives.

The last one is what everything turned on, and **the first measurement of it was
wrong in the same way the mutant was.** Counting "an append from index 1 arrived
at a node that has a snapshot *somewhere in the run*" reads **2,870 per 120
seeds** — abundant, and meaningless. Counting the ordering properly — the
snapshot existed **at or before** the message — reads:

| shape | appends from index 1 carrying entries | of those, receiver already had a snapshot |
|---|---|---|
| A7's shape (`current`) | 5,051 / 200 seeds | **1**, at seed **155** |
| `a3` | 649 / 200 seeds | **0** |
| `a2` | 636 / 200 seeds | **0** |

> **`M34`'s floor was declared under `a2`, and under `a2` the precondition does not
> occur at all.** The two nulls were not evidence about the harness's reach; they
> were taken under a shape in which the class is unreachable **by construction**,
> against a declaration that named that shape.

Three orders of magnitude separate the upper bound from the real precondition, and
the axis that separates them is **time** — the same axis `M79` turned on one
document over. A precondition measurement is a claim about aim too.

**What this leaves owed.** `M34`'s power declaration still says `power-config: a2`
with a pre-no-op measurement, and it should say A7's shape with 1 in 200. Its
covering test passes with the mutant applied and must not. Both are recorded in
CARRY-FORWARD; neither is a reason to hold the bundle, which now reproduces.

**Symptom, verbatim:**

```
panic: raft: node 3 truncated to 1 with commit index 6; an entry this node was told
was committed is being overwritten, which is state machine safety failing
```

**Root cause.** The append consistency check answered `true` for `PrevLogIndex == 0` unconditionally
— *"append from the very beginning"* is agreeable to a node that has a beginning, and before A2 every
node did. Once a prefix is inside a snapshot, that is no longer true: the node has already applied
those entries, has already told the cluster they were committed, and no longer holds them to compare
against.

A leader whose view of a follower has been reset sends exactly that append. The follower accepted it,
found index 1 conflicting with a log that starts above the snapshot, and truncated into its own
applied prefix.

**Why the checkers caught it here and not earlier.** It could not exist before A2: it needs a
snapshot, and A1 had none. It needed the 10,000-seed run to appear at all — 1 seed in 3000 — because
it takes a leader that has lost its position on a follower which has *also* compacted past that
position, which is a coincidence of two rare states.

**The instrument is the assertion BUG-007 corrected, and that is the part worth keeping.** BUG-007
was a false invariant: `truncateFrom` refused any truncation reaching the *durable* watermark, which
Raft does not guarantee, and it was unreachable for as long as that watermark never moved. The
correction pointed it at the *commit index*, which is what Raft actually guarantees. That correction
was made with no defect in hand. One phase later it caught this, and the old form would not have —
it fires on durability, which is legal to truncate, and would have missed a truncation into an
applied prefix while crying about ordinary ones.

**What this would have caused in production.** A replica silently rewriting committed history, then
serving and replicating it. Every structural check on the way up passes: the resulting log is gapless
and the snapshot is intact. It is the worst kind of corruption because the node has no way to notice.

**Fix.** `matches` answers `PrevLogIndex == 0` with *"only if I have no snapshot"*. Rejecting is safe
and self-correcting: the reject carries `lastIndex` as its hint, so the leader jumps forward, finds
the follower is past its own log, and sends a snapshot — which is what it should have done.

---

### BUG-010 — a leader killed itself when asked to hand over to a replica that had been removed

| field | value |
|---|---|
| **Found by** | sim — the first sweep after membership churn landed |
| **Phase** | A3 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M40-transfer-to-a-removed-node-panics.patch && go run ./cmd/simctl replay --bundle seeds/BUG-010` |
| **Reproduce (seed)** | seed **0**; 65 of 300 seeds reach it |
| **Invariant that caught it** | none — a panic, from an assertion that had become false |
| **Mutant class** | none existed — added `M40-transfer-to-a-removed-node-panics` |
| **Fix commit** | *(this commit)* |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

**Symptom, verbatim:**

```
panic: raft: node 2 was asked to transfer leadership to 1, which is not a peer
```

**Root cause.** A2 gave `TransferLeadership` the frozen D5 signature, which returns no error, and split
its failures deliberately: *not the leader* is a runtime condition and a no-op; *target is not a peer*
is a caller bug and panics. That split was correct when membership was fixed — a caller naming a
non-member had made a mistake it could have avoided.

A3 makes membership change under the caller's feet. A node scheduled for a transfer can be removed
from the configuration before the order is issued, by a change the caller neither made nor saw.
Nothing it could have checked would have told it, because the answer can change between the check and
the call.

**Why the checkers caught it here and not earlier.** It could not exist before A3: it needs a
configuration to change. It appeared on the first sweep after membership churn landed, on 65 of 300
seeds, because the plan schedules transfers and membership steps independently and they collide often.

**What this would have caused in production.** A leader crashing during a routine rebalance —
precisely the operation that removes replicas, so precisely the operation most likely to have a stale
target. An operator moving load off a machine would take the leader down with it, and the panic
message would send them looking for a caller bug that was not there.

**Fix.** *Not in the configuration* joins *not the leader* on the runtime side: a no-op. A learner
target is a no-op too, for a reason worth stating — a learner cannot win an election, so ordering it
to campaign would burn a term for nothing. **Transferring to yourself stays a panic**, because that
is a caller bug in any membership.

**And the lesson, which is about the earlier decision rather than this one.** The A2 split was
reasonable and became wrong without anybody touching it. A classification of "caller bug versus
runtime condition" is a statement about what the caller can know, and that changes when the system
gains a way to change its own state. It is worth re-asking at every phase that adds one.

### BUG-011 — a node re-applying a split after a crash kept the keys it had just given away

| field | value |
|---|---|
| **Found by** | sim — snapshot equivalence, on 178 of the first 300 A4 seeds |
| **Phase** | A4 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M43-extent-recovered-from-a-floating-key.patch && go run ./cmd/simctl replay --bundle seeds/BUG-011` |
| **Reproduce (seed)** | seed **0**, range 3, applied index 12 |
| **Invariant that caught it** | snapshot equivalence — a snapshot's contents are the state the committed log produces at its index |
| **Mutant class** | none existed — added `M43-extent-recovered-from-a-floating-key` |
| **Fix commit** | ebea8c5 |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

**Symptom, verbatim:**

```
range 3: node 2 took a snapshot at index 12 term 1 whose contents are not the state the committed
log produces there: snapshot digest 10893142299075315290, log digest 13438753892299047465
```

**Root cause.** A range's extent was written to its own engine key whenever a split applied, and a
restart read it back from there. The state machine, meanwhile, was rebuilt from the newest snapshot
and then re-applying the log tail above it.

Those two are not aligned. The descriptor key is a position in no log at all; the snapshot is a
position in exactly one. A node that applied a split at index 10, crashed, and recovered from a
snapshot at index 6 therefore rebuilt a state machine that had **not** split while holding an extent
that **had**. Re-applying the split entry then found its own effect already recorded in the extent,
judged the entry stale under the epoch guard, and skipped it — so the keys never moved. Nodes 0 and 1
applied the split once and held `{k02,k03}`; node 2 held `{k02,k03,k04,k05}` forever after.

**Why the checker caught it.** Because A4 had already moved the extent into the snapshot payload for
an unrelated reason, the digest snapshot equivalence compares covers the extent as well as the keys.
Without that, the divergence would have been invisible: the keys alone would have matched on any node
whose extent happened to agree.

**What this would have caused in production.** Two replicas of one range disagreeing about which keys
they own, permanently, with no error anywhere. Reads would return different answers depending on which
replica served them, and the range would never repair itself, because nothing in the protocol ever
re-derives an extent.

**Fix.** The extent is recovered only from a point that is aligned with the state machine: the
snapshot's descriptor, unconditionally, or the range's birth extent when there is no snapshot. The
descriptor key is demoted to what it can honestly be — a record that the range exists here — and the
left range's is no longer written at all.

**The general shape.** *Anything derived from a log position must be derived AT that position.* This
is the same sentence as BUG-004's "an identifier is not a position", and A4 produced four more
instances of it before the phase closed.

---

### BUG-012 — three replicas agreed with each other and the checker reported all three in violation

| field | value |
|---|---|
| **Found by** | inspection, while diagnosing BUG-011 — the model disagreed with every node at once |
| **Phase** | A4 |
| **Reproduce (plan)** | `go run ./cmd/simctl replay --bundle seeds/BUG-012` with the model's split rule reverted; the bundle carries the schedule, and the defect is in `sim/hunt`'s model rather than in a patchable source line |
| **Reproduce (seed)** | seed **2**, range 1 |
| **Invariant that caught it** | none — this is a defect in the checker, not the system |
| **Mutant class** | covered by `M42-a-split-child-is-born-one-key-wide`, which fails if the model stops modelling splits faithfully |
| **Fix commit** | ebea8c5 |
| **Minimized?** | no |

**Root cause.** The harness's model of the state machine applied **every** split entry it found in a
committed log. The system does not: a split entry names the extent it was computed against, and one
naming an extent the range has already moved past is refused by every replica. Two leaders can each
propose a split from the same extent and both entries can commit.

So on any seed containing a superseded split, the model computed a state no replica ever held and
reported a violation against three nodes that agreed with each other.

**Why this is in BUGS.md at all.** It is a harness defect, which DR-29 normally keeps out of this
file. It is here because it is inseparable from BUG-011: it was found inside that investigation, and
for one afternoon it made a real divergence look like three of them. A checker that manufactures
violations is not merely noise — it is the thing that trains you to disbelieve the checker.

**Fix.** The model restates the staleness rule in its own terms rather than sharing the
implementation's. Restating is the point: if the two ever drift, the oracle fires, which is exactly
what the harness is for.

---

### BUG-013 — a follower installed a snapshot, adopted a state machine that had split, and kept an extent that had not

| field | value |
|---|---|
| **Found by** | sim — snapshot equivalence, on 78 of 300 seeds after BUG-011 was fixed |
| **Phase** | A4 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M44-installed-snapshot-drops-the-extent.patch && go run ./cmd/simctl replay --bundle seeds/BUG-013` |
| **Reproduce (seed)** | seed **2**, range 1, index 24 |
| **Invariant that caught it** | snapshot equivalence |
| **Mutant class** | none existed — added `M44-installed-snapshot-drops-the-extent` |
| **Fix commit** | ebea8c5 |
| **Minimized?** | no |

**Root cause.** The extent travelled beside the snapshot rather than inside it: the storage layer
wrote `encodeSnapshot(meta, n.desc, data)`, taking `n.desc` from the **installing** node. A follower
that installed a snapshot therefore adopted the sender's keys and kept its own extent.

At index 18 that node's keys were right and its extent was two splits behind. The split entry at index
23 then named an extent the node could not see, was judged stale, and was skipped — and the node kept
serving `k02` after the range had given it to a sibling.

**What this would have caused in production.** Snapshot install is how a lagging replica catches up,
so this fires precisely on the replica that was already behind: it rejoins, looks healthy, and owns
the wrong keys.

**Fix.** The extent is part of the state machine's payload, not metadata about it. `encodeMachine`
serialises the extent and the keys together, so the extent travels on the wire with the snapshot, is
restored at the index it belongs to, and is covered by the digest snapshot equivalence compares. The
same defect is now a caught violation rather than a silence.

---

### BUG-014 — a range applied a write for a key it had already given away, and kept it forever

| field | value |
|---|---|
| **Found by** | sim — snapshot equivalence, on 13 of 300 seeds after BUG-013 was fixed |
| **Phase** | A4 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M45-apply-ignores-the-extent.patch && go run ./cmd/simctl replay --bundle seeds/BUG-014` |
| **Reproduce (seed)** | found at seed **28**, range 2, index 13; `seeds/BUG-014` carries seed **15**, re-recorded at A6 (DESIGN-A6 §16) |
| **Invariant that caught it** | snapshot equivalence; the invariant it breaks is *no request served under a stale descriptor epoch* |
| **Mutant class** | none existed — added `M45-apply-ignores-the-extent` |
| **Fix commit** | ebea8c5 |
| **Minimized?** | no |

**Root cause.** The extent was checked where a request **arrived** and nowhere else. A leader accepts
a request against the extent it has *applied*; a split entry it has already appended is not applied
yet. Between those two moments the leader accepts writes for keys the log has already given away, and
those entries commit behind the split.

Range 2 gave `[k06,∞)` to range 6 at index 9 and applied `put k07` at index 10. The key then sat in
the wrong range permanently: the next split at index 12 moved out only what **its** new right half
contained, and `k07` was outside both halves, so nothing ever moved it.

**Why it took three fixes to surface.** It was masked by the harness's own model, which deleted every
key at or above the cut point instead of keeping what the new extent covers. Those two rules agree
only when a range holds nothing outside its extent — exactly the assumption this bug violates — so the
model quietly repaired the range and the oracle went green on a state no replica ever had.

**What this would have caused in production.** Two ranges claiming one key with no error anywhere: the
router sends the read to range 6 and the write landed in range 2. Split traffic is when it happens,
which is when the system is already busy.

**Fix.** The extent is checked at the **log position**, where every replica reaches the same verdict
from the same state, and a command naming a key outside it is refused and the client re-routed. The
split path now asserts that its two halves partition what the range holds, so the residue that let
this survive is a loud failure instead of a slow divergence.

---

### BUG-015 — two replicas gave one new range two different birth configurations

| field | value |
|---|---|
| **Found by** | sim — a raft refusal, seed 9595 of the A4 exit sweep |
| **Phase** | A4 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M46-split-inherits-the-appended-configuration.patch && go run ./cmd/simctl replay --bundle seeds/BUG-015` |
| **Reproduce (seed)** | **16** — and it does **not** currently reproduce; see *The search, and why the null is a finding* below |
| **Invariant that caught it** | none — a refusal, from `ApplyConfEntry` declining an illegal transition |
| **Mutant class** | none existed — added `M46-split-inherits-the-appended-configuration` |
| **Fix commit** | ebea8c5 |
| **Minimized?** | no |

**The search, and why the null is a finding** *(2026-08-27, and it is OPEN)*.

`seeds/BUG-015` carries seed 16 and replays **identically** with `M46` applied. It was found at seed
**9595** of A4's 10,000-seed exit sweep; A6's workload moved the trace, then A7's term-start no-op
moved it again. Under the standing rule it is **not retired on a null** — the entry is blocked.

**§5e.2b's four axes were varied before any null was read as reach**, and three of them returned
measurements:

| axis | what was asked | what it returned |
|---|---|---|
| **line** | is `left.raft.ConfigurationAt(index)` the line that carries the property? | **yes** — `conf` flows straight into `snapMeta.Conf`, the right range's birth configuration, and into the ledger's `RecordRangeBase`. The aim is right |
| **code position** | does the covering test execute it? | **unresolved** — `TestSplitInheritsTheConfigurationAtItsIndex` is a **1,000-seed serial sweep** taking over an hour, and `scripts/mutant-covered.sh` has no per-mutant filter, so the question costs a full suite run. *That cost is itself the finding: a covering test nobody can afford to run inside a phase is not a covering test.* |
| **role** | must the applying replica hold a divergent appended tail? | **yes**, and it happens |
| **time** | must a configuration entry be appended **above** the split's index before the split applies? | **yes, and it occurs** — measured off the wire as a deliberate LOWER bound (a conf entry that arrived no later than the split entry is certainly appended when the split applies) |

```
current   2675 split entries applied / 200 seeds ->  8 met the precondition, first at seed 23
a4        1128 split entries applied / 200 seeds -> 25 met the precondition, first at seed  1
```

**The precondition occurs, and three times more often under `a4` than under the shape `M46`'s floor
names.** So the null is not about reach.

**Detection, against that:** 0 of 400 under `current`; 0 of 200 under `a4`; and **0 at seeds 23 and 1
specifically**, where the precondition demonstrably fired.

> **A null at a seed known to meet the precondition is a different object from a null over a range.**
> The first is the class failing where it should fire; the second is the class failing to arrive, and
> only the second is evidence about reach. Eight preconditions met and zero detections at those exact
> seeds is stronger evidence than any number of seeds, because it rules out the explanation that more
> searching would have helped.

> **The gap is between the precondition and the consequence.** A divergent birth configuration is not
> yet a defect. It becomes one only when the new range **then receives a membership entry** that the
> replica which started behind reads as an illegal transition — and that needs a conf change aimed at
> a range that was born moments earlier and has to survive long enough to receive it.

**What it would take to reach the class.** Not more seeds: a **workload** that drives a membership
change onto a newly born range. Every shape in the tree schedules conf changes against the cluster and
lets ranges be split out of it; none deliberately targets a just-split range. That is a property of
the schedule generator, not of the seed space, and 600 seeds across two shapes is enough evidence to
say a seed search is the wrong instrument.

**The ratio, measured rather than asserted.** The seed search cost **5h34m** and did not finish 3,000
seeds, because `M46` runs at ~50 s/seed against a clean 4.7 — illegal configuration transitions make
the cluster churn. The directed test that arranges both halves kills the same mutant in **0.3
seconds**.

> **Five and a half hours against three tenths of a second, for the same question.** That is the
> practical argument for a directed test over a search whenever a mutation is *disruptive* rather than
> subtle — and it compounds with the evidential one, because the classes hardest to reach by search
> are exactly the ones search is most expensive for.

> **Which is BUG-024's conclusion arriving again: a per-seed bundle may be the wrong artifact for this
> class.** A bundle pins one schedule. A class that needs two independent things to coincide — a
> divergent tail at a split, and a membership change onto the range that split produced — is better
> served by a directed test that arranges both than by a search hoping a seed does.

**Not retired.** `M46` is `expect: killed` and its covering test still exists; what is open is the
BUNDLE, and the honest state is that this corpus entry has no reproducing schedule at HEAD.

### The directed test, and what it answered

**Ansh, ruling: directed test, not a generator option.** *"A generator option is a claim about
reachability under `floors.go`'s rule, so it changes what every floor in the file means. A directed
test that arranges both halves changes nothing about the sweep's shape, costs the least, and answers
the actual question — does the defect exist when the two coincidences are made to coincide."*

`TestASplitDoesNotInheritAnUnappliedConfiguration` (`store/splitconf_test.go`) arranges both halves by
hand: a replica holding a configuration entry appended **above** the split's index and applied by
nobody, then the split applied at that index. No seed, no scheduler, no sweep.

**It kills `M46` in 0.3 seconds**, with a verdict that names the defect exactly:

```
the right range was born with configuration voters=[1 2]; the log at the split's index
says voters=[1 2 3]
```

> **So `M46` is aimed at a real defect.** The larger finding the search was leaving open — *that the
> mutant might be aimed at something that is not a defect at all* — does not apply. The class is
> real, the line is right, and the only thing missing was an instrument that could reach it.

**It carries its own vacuity guard**, and the guard earned its place immediately: it fails if the tail
it arranged is not actually divergent, and it caught two construction errors of mine before the
assertion could pass over nothing — a conf change that `ApplyConfEntry` refused, and a
`ConfChangeTransition` left at its zero value. **A directed test that arranges a coincidence must
assert that it arranged it**, or it is a sweep with one seed.

**And it replaces a covering test nobody could run.** `TestSplitInheritsTheConfigurationAtItsIndex`
was a 1,000-seed **serial** sweep, over an hour. `M46` now declares
`power-covered-by: TestASplitDoesNotInheritAnUnappliedConfiguration`, and
`make mutant-covered ONLY="M46-…"` answers in **2 seconds**.

### Two things this generalises, now that each has two instances

> **A bundle pins ONE schedule, so a class needing two independent coincidences has no single schedule
> to pin.** That is a statement about the corpus's coverage model rather than about BUG-015 or
> BUG-024, and two instances make it a property rather than a coincidence. The corpus is a good
> artifact for a defect a schedule *produces* and a poor one for a defect two schedules must *meet*
> to produce.

> **A mutant that slows the system tenfold makes a seed search cost tenfold.** `M46` runs at ~50 s per
> seed against a clean 4.7, because illegal configuration transitions make the cluster churn — so the
> first search spent 5h34m without finishing 3,000 seeds. **The classes hardest to find by search are
> exactly the ones search is most expensive for**, which is a practical argument for directed tests on
> top of the evidential one, and it generalises to any class whose mutation is disruptive rather than
> subtle.

**Symptom, verbatim:

```
node 2: range 4: raft: node 3 refused the configuration entry at index 5:
raft: node 2 is a voter and cannot be demoted to a learner in one step
```

**Root cause.** A range born from a split inherited `left.raft.Configuration()` — the **active**
configuration, which is effective on append (D-A3-2). That is not a function of the applied prefix.
Two replicas applying the same split entry with different appended tails therefore handed the new
range two different birth configurations, and the replica that started behind read the next membership
entry as an illegal transition.

**Why the checker caught it.** Not by an oracle: by raft refusing an entry it could not apply. The
refusal is the A3 machinery working — a configuration change that would violate the overlapping-quorum
argument is declined at the funnel rather than ignored downstream — and here it fired on a caller that
had asked the wrong question.

**What this would have caused in production.** A new range whose replicas disagree about who is in it,
which is a disagreement about who can win an election and what counts as a quorum. Every safety
argument in Raft is downstream of the replicas agreeing on the configuration.

**Fix.** `raft.ConfigurationAt(index)` — the reasoning `Compact` already had inline, exported for the
second caller that needed it. The split path asks for the configuration **at the split's index**.

---

### BUG-016 — the rebalance oracle blamed one move's removal on a different move

| field | value |
|---|---|
| **Found by** | the rebalance oracle, firing on 252 of 300 seeds and then on seed 103 |
| **Phase** | A4 |
| **Reproduce (plan)** | `go run ./cmd/simctl replay --bundle seeds/BUG-016` with the attribution window removed; the bundle carries the two-moves-one-source schedule |
| **Reproduce (seed)** | seed **103**, range 1 |
| **Invariant that caught it** | none — this is a defect in the checker |
| **Mutant class** | covered by `M41-rebalance-removes-before-it-adds`, which the corrected oracle still kills at 192 of 300 |
| **Fix commit** | 34b284d |
| **Minimized?** | no |

**Root cause, in two rounds.** A move is an intent, and no sequence of committed entries states one:
an add and a remove look exactly like two unrelated membership changes. The oracle attributed **every**
removal of a move's source after the move was ordered.

First round, 252 of 300 seeds: A3's membership churn ran concurrently and its removals were read as
moves. Second round, one seed: two moves on range 1 shared a source, and the second move's correct
removal was blamed on the first, which had stalled.

**Why this is in BUGS.md.** Same reason as BUG-012. It is the third instrument in this project to
catch itself, and the number that matters is that it reported 252 violations against a system that was
behaving correctly. A checker with a false-positive rate that high is worse than no checker: it is a
checker people learn to override.

**Fix.** Two things, and neither is a loosened check. The two membership drivers are separated in time
so a removal has one plausible author, which is a **recorded limitation** — no seed exercises a move
racing an unrelated membership change (DESIGN-A4 §7). And a move owns its range's membership only
until the next move on that range is ordered, a window the harness derives from its own record of what
it ordered rather than from anything the cluster says.

The mechanism gained something too: `RequestMove` takes an explicit `begin`, because a stateless move
cannot otherwise tell "the destination is already a voter because I just added it" from "the
destination was already there", and the second is not a move at all.


### BUG-017 — a follower advertised a term that was not on its disk, because an empty durability mark released an earlier one

| field | value |
|---|---|
| **Found by** | sim — persist-before-reply, seed 16 of the first A5 sweep |
| **Phase** | found in A5; **the defect has been present since A2** |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M53-empty-mark-releases-through.patch && go test -run TestAnEmptyMarkDoesNotReleaseAnEarlierOne ./raft/` |
| **Reproduce (seed)** | seed **16** of the unthrottled-collector shape for the first half; seed **6425** of the 10,000-seed exit run for the third |
| **Invariant that caught it** | persist-before-reply — term, vote and log durable before replying to any RPC |
| **Mutant class** | none existed — added `M53-empty-mark-releases-through` and `M56-term-gated-only-on-what-is-dirty-now` |
| **Fix commit** | *(this commit)* |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

**Symptom, verbatim:**

```
range 2: node 3 sent a app-resp advertising term 2 at instant 10410736157 while only term 1 was durable
```

**Root cause.** `Ready()` has a branch for a mark that covers nothing:

```go
if rd.Mark != 0 && !r.markHandedOff {
    r.releaseThrough(rd.Mark)     // the defect
}
```

A mark can be opened by a mutation and then be left covering nothing — a conflicting append truncates
the entries away, or a snapshot install replaces the log under them. Waiting for an acknowledgement
that will never come would stall every message gated on it, so the mark is closed here. That much is
right.

`releaseThrough` is not. **A durability acknowledgement is monotone** — the driver reporting mark *m*
durable implies every earlier mark, because writes reach the disk in order. **An empty mark has not
earned that implication.** It is satisfied because there is nothing to wait for, which is a statement
about *itself* and about no other mark.

So closing an empty mark 2 also released everything gated on mark 1 — whose write was in flight,
carrying the very term the released message advertised.

**Why it took until A5.** Nothing in the sequence needs MVCC. What A5 changed is the traffic: the
collection command proposes on every apply once the retention window has passed, so a mark is opened,
handed over, and followed by a second mark far more often. Measured: A5's shape reaches it on 1 seed
in 60; A5 with collection disabled, **0 in 60**. The defect was reachable since A2 and nothing had
gone looking down that particular corridor.

**What this would have caused in production.** The leader counts an append acknowledgement from a
follower that will come back from a crash not knowing it ever gave one — which is precisely the
amnesia the whole gating design exists to prevent, arriving through the one door left open.

**Fix, and it took two halves.** `closeEmptyMark` replaces `releaseThrough` at that call site. An
empty mark satisfies only itself, and it does not move the persisted watermark, because it is not
evidence about any write.

The first version of that fix **released** the messages gated on the mark in one pass — and stalled
**12 seeds in 200**. A message can carry a constraint in both streams, and the two land
independently: a one-pass release freed only those whose *snapshot* constraint was already met and
left the rest gated on a mark that no longer exists, so when their snapshot constraint landed a
moment later, `release()` checked `g.mark <= persisted`, found the empty mark still above the
watermark, and withheld them forever.

So the mark is **struck** from the messages it gated rather than swept: an empty mark constrains
nothing, and `release()` then decides on whatever constraints are actually left. **The old code had
the opposite pair** — it kept liveness by releasing through the mark, which is the unsound half.
Neither spelling is right alone, and the correction is not "release less" but "record that the
constraint is gone".

### The third half, found by the 10,000-seed exit run

The 2,000-seed mid-phase sweep was clean. The exit run found the same symptom again on **2 of 10,000
seeds**, first at seed 6425, and the state at the send says exactly what was wrong:

```
persisted=0 persistedSnap=0 dirtyMark=0 lastHanded=1 handedOff=false nextMark=2 gated=1
```

The hard state was handed over under mark 1 and never acknowledged (`lastHanded=1, persisted=0`), and
`dirtyMark` was **zero** — because a later mark 2 had opened and closed empty. Every gate on a term
claim was computed from `dirtyMark`, so a message created after that point found nothing to wait on.

**`dirtyMark` answers "is anything pending right now". A term claim needs "is this node's term on
disk", and those are different questions.** They coincide until a mark stops being current without
becoming durable — which is precisely what closing an empty mark does. The first half of this fix is
what exposed it: while an empty mark released *through* itself it also inflated `persisted`, so the
wrong belief was at least self-consistent.

The fix is `termMark()`, which names the hard state's own mark — the current one while the state is
dirty and unhanded, `hsMark` while it is handed and unacknowledged, zero when it is on disk.

**And that alone was not enough either, which is the part worth reading.** Collapsing the term's mark
into the index's with a `max` puts two constraints in one field, and they are satisfied by different
events: a message gated on `max(index mark 2, term mark 1)` waits on 2, and when mark 2 closes *empty*
the collapsed field is struck and the message leaves with the term still in flight under mark 1. So
`gatedMessage` carries the term's mark **separately**, and `closeEmptyMark` strikes only the field
that names it.

That is A2's rule — *a message attesting to state in two independent streams waits for both* — one
constraint further on. There are three, not two: the log, the snapshot, and the hard state.

**And the lesson, which is about the shape of the mistake.** Both spellings are one line and both look
like "this mark is satisfied". The difference is whether the satisfaction is evidence about other
marks, and the answer depends on *why* it is satisfied — durable, or empty. **A predicate that is true
for two different reasons does not license the same conclusion from both**, and the two reasons here
were separated by nothing but a function name.

The second half has its own: **a constraint that has been satisfied must be recorded as satisfied,
not swept once.** A sweep answers the question at one instant; anything whose *other* constraint
arrives later never asks again. That is A4's "a fact is recorded, never inferred" arriving in the
gate rather than in the ledger.

**What made the search short.** Two instruments, both added while hunting it and both kept. The
driver now asserts that a Ready reporting a mark it writes nothing under is a bug — it did not fire,
which retired a whole hypothesis in one run — and `raft.QuiesceDebug` prints which mark, whether the
driver was ever handed it, and whether it was acknowledged. Those three numbers ended the hunt: the
stall showed `lastHanded=0`, so the mark had never been handed, so the close had run and the release
had not.


### A note on BUG-002's seed, and the lane it produced

The bundle's seed has moved twice. The second move is the interesting one.

At A5, `make corpus` was green on BUG-002 and the bundle had **stopped carrying its finding**: with
M14 applied, the replay was byte-identical to the recording. The mutation changed nothing on that
schedule, so "apply the mutant, replay the bundle" reproduced a clean run.

`make corpus` cannot see that, and the reason is structural rather than an oversight. A fixed bug's
bundle records **no violation** — the schedule replays clean by design, and the reproduction is two
steps. So the lane compares a clean replay against a clean recording, matches, and reports green,
while the second step has quietly stopped working.

`scripts/corpus-reproduces.sh` is the missing half: it applies each bundle's mutant, replays, and
requires the result to DIFFER from the recording. A mutated replay that is byte-identical to an
unmutated one is a mutation that did nothing. The bundle was re-recorded at seed 32, which does
reproduce, in the commit that noticed.

**The general shape, and it is the same one as the eleventh vacuous-green:** the claim was checked at
the layer where it was cheap to check, not at the layer where it was made. "Every bug replays from a
single seed" is a claim about *bundle plus mutant*, and the lane was looking at the bundle.


### BUG-018 — two transactions owned one key, because the steps in one batch could not see each other

| field | value |
|---|---|
| **Found by** | sim — snapshot equivalence, on 12 of the first 30 A6 seeds, **on the first outing of `store.ReplayMachine`** |
| **Phase** | A6 |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M59-transaction-steps-batched-blind.patch && go run ./cmd/simctl replay --bundle seeds/BUG-018` |
| **Reproduce (seed)** | found on seed **5**, range 8, entries 9 through 16 applied in one Ready; the bundle carries seed **0**, where the mutant reproduces it under the workload as it stands today |
| **Invariant that caught it** | snapshot equivalence — a snapshot is the state the committed log produces at its index |
| **Mutant class** | none existed — added `M59-transaction-steps-batched-blind` |
| **Fix commit** | *(this commit)* |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

**Symptom, verbatim:**

```
range 8: node 1 took a snapshot at index 16 term 2 whose contents are not the state the committed
log produces there: snapshot digest 10885164352708667826, log digest 15917669729469132538
```

**Root cause.** Every transaction step reads the engine before it writes: a prewrite asks whether the
key is locked and whether a newer commit exists; a resolve reads the lock and the primary's record.
The apply loop staged all of a Ready's commands into one batch and wrote it once at the end, so a step
staged and not yet flushed was a step the next one could not see.

Two prewrites of the same key in one Ready therefore **both succeeded**, and the second overwrote the
first's lock. Two transactions owned one key and neither knew.

**Why the bisect was decisive.** Node 2 applied indices 1 through 8 one Ready at a time and matched
the replay exactly at every step; then entries 9 through 16 arrived together and the two answers
parted in a single jump. **The replica's state depended on how many entries happened to arrive
together, which is not a function of the log** — and "the state is a function of the log" is precisely
what snapshot equivalence asserts.

**What this would have caused in production.** Atomicity broken silently. The losing transaction's
lock is gone, so nothing resolves it and nothing rolls it back; its commit record can land later on a
key another transaction has already committed.

**It needs no crash, no partition, and no injected fault at all.** Every other entry in this file
required an engineered schedule: a crash at a particular instant, a partition, a lost unsynced write.
This one requires two steps of two transactions to arrive in one batch, which is what happens under
*load*. A defect reachable under ordinary operation is a different and more serious class than one
that needs a fault to reach, and it is worth separating them: the fault-requiring defects are found by
the injectors, and this one was found because the checker looks at something the injectors do not
control.

**The fix.** A step is flushed on both sides of itself: before, so it reads everything below it, and
after, so the next one reads it. That is the same correction a read needed at A5 — *a read must see
writes staged at lower indices in the same batch* — arriving in the write path, where the read is
hidden inside a precondition rather than being the point of the operation.

**And the finding about the checker, which is the reason to read this entry.** The A5 model would not
have caught this. It replayed logically, one command at a time, with no batching at all — so it had
no notion of a Ready and could not represent the state that produced the bug. `ReplayMachine` caught
it because it executes the real apply path against a real engine and therefore *can* differ from the
driver in exactly the ways the driver can be wrong.

**A verification mechanism was replaced under protest of losing a property, and the replacement
immediately found a defect the original was structurally blind to.** That is the argument for the
swap, made by the swap — and it is the reason the swap's own record (DESIGN-A6 §13) is worth more
than the mechanism.

### The method: batch-boundary bisection

Worth writing down as a technique, because A7's read index and B4's kill-point sweep will both need
it.

When a replica's state disagrees with a replay of its own log, print the state digest **after each
Ready** on the replica and after each **index** in the replay, and line them up:

```
node 2   through=1..8   matches the replay at every index
node 2   through=16     one jump, and the answers have parted
replay   through=9..16  eight separate values, all different from the node's one
```

The node's digests are per *Ready*; the replay's are per *entry*. Where the node's trace skips
indices, entries arrived together — and if the divergence appears exactly across such a skip, the
defect is in **what a batch does that a sequence does not**. That is a small and enumerable set:
staged writes not yet visible to later reads in the same batch, effects applied out of order within a
batch, and a flush boundary in the wrong place.

The technique's value is that it points at the batch boundary rather than at any of the sixteen
entries, which is where three hours of reading the entries would never have looked.


---

## The harness defect these four were hiding

Recorded here rather than as an entry, per DR-29 — it is a defect in the observer, not the observed.
The number belongs beside these findings anyway, because it says what the earlier green was worth.

The ledger that the persist-before-reply oracle judges every acknowledgement against was itself built
by **reading the engine back**. An `engine.Engine` read returns the VISIBLE state, which by
construction includes batches applied and not yet synced. So the oracle was comparing the system's
claims against the system's own account of itself, one layer of indirection removed.

It did not report false violations. It reported **nothing**: an inflated durability watermark makes
every acknowledgement look covered. Across 10,000 seeds the read-back was ahead of true durability
**44,911 times**. With the ledger corrected to record what the driver actually made durable, a
300-seed sweep went from 2 violations to **257** — and those 257 are BUG-006.

DESIGN-A1 §5c has the full account, including why this is the eighth instance of the vacuous-green
class and the first one inside an oracle. `internal/provenance` and `tools/provcheck` are the part of
the fix that a future change has to get past.

---

### BUG-019 — a transaction's rollback took another transaction's lock, and the money it was holding stopped existing

| field | value |
|---|---|
| **Found by** | sim — `bank-conservation`, on 2 of the first 20 A6 seeds, on the oracle's first outing |
| **Phase** | A6 |
| **Reproduce (unit)** | `go test ./kv -run 'StealSomebodyElsesLock'` |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M65-rollback-takes-any-lock.patch && go run ./cmd/simctl replay --bundle seeds/BUG-019` |
| **Reproduce (seed)** | found on seed **7** (the audit at `1600000003877395934.0` summed to **-9**); the bundle carries seed **9**, re-recorded at BUG-022's fix — the read mark moved every raft trace, and seed 41 regenerated cleanly while no longer reaching M65 at all, which is the search §16.3 warns a regeneration is |
| **Invariant that caught it** | bank conservation over client-observed history — the accounts sum to what they summed to at the beginning |
| **Mutant class** | none existed — added `M65-rollback-takes-any-lock` and `M66-commit-takes-any-lock` |
| **Fix commit** | *(this commit)* |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

**Symptom.** Nine units of money stopped existing. An audit reading all eight accounts at one
timestamp summed to -9 where every transfer in the run had moved an amount from one account to
another, so the total could only ever be zero.

**Root cause, and it is one word.** *The* lock.

A key holds at most one lock, so the lock record has no transaction in its address:
`LockKey(ns, key)` names **the slot**, not the holder. Both resolution paths ended with

```go
b.Delete(LockKey(s.ns, key))
```

which reads as *delete my lock* and executes as *delete whoever's lock*.

**The schedule, which needs no fault at all:**

1. T1 prewrites `k` and holds the lock.
2. T2 prewrites `k`, is refused because the key is locked — **this is first-committer-wins working
   correctly** — and aborts.
3. T2's abort rolls back its own key, and takes T1's lock with it.

T1 is now a committed transaction with an **orphaned version**: no lock, so no reader will ever
discover it and no resolver will ever finish it. The value it committed is invisible for the rest of
time. On seed 7 that value was nine units of somebody's balance.

**Fix.** `releaseLockOf` reads the lock and drops it only if the start timestamp matches. One place,
because both callers had the same defect for the same reason. The version delete never needed the
check — it is already addressed by start timestamp, and only the lock was ambiguous.

The read is only correct because the apply loop flushes around every transaction step, which is
**BUG-018's fix**. This is the first thing built on top of it.

**Why the surrounding oracles did not catch it.** Transaction atomicity checks that a transaction's
keys are all at its commit timestamp or nowhere; an orphaned version is *nowhere*, which satisfies it.
The Percolator invariants check that no state is internally contradictory; an orphan contradicts
nothing. Both are right, and both are blind to it, because the defect does not produce an inconsistent
state — **it produces a consistent state with the wrong amount of money in it.** Only a checker that
knows the workload's own conservation law can see that, which is exactly the argument for the bank.

**What it would have caused in production.** Silent data loss on any key two transactions contend
for, with no error anywhere and no inconsistency any internal check could detect. A committed write
that no reader can see is worse than a lost one: the transaction was acknowledged.

---

### BUG-020 — (harness) a transfer prewrote a balance it had never read, and the workload invented money

| field | value |
|---|---|
| **Found by** | sim — `bank-conservation`, seed 9, while investigating BUG-019 |
| **Phase** | A6 |
| **Reproduce (seed)** | seed **9**: the audit at `1600000006499641460.0` sums to **-23** |
| **Invariant that caught it** | bank conservation, and then the new one-line assertion in `prewrite` |
| **Mutant class** | not applicable — the defect is in the workload, not the system |
| **Fix commit** | *(this commit)* |

**In the observer, not the observed**, and recorded here anyway: it is the ninth harness defect, and
BUG-016's standing rule is that a checker firing on correct behaviour costs more than no checker.
Here the checker was right and the *client* was wrong, which is a third case worth naming.

**Two defects, both the same shape.**

1. **A counter of answers is not a count of distinct facts.** The transfer tracked read completion
   with `pending--`. Two answers for one key — which happens whenever two resolutions of the same
   lock both come back — counted as two keys read. The transaction then prewrote a key it had never
   read, using Go's zero value, and invented twenty-three units of money.
2. **A start timestamp is not a transaction identity.** Answers were routed to the waiting client by
   start timestamp. Percolator can do that because a single TSO issues them; **here every node has
   its own HLC and two nodes can mint the identical `(wall, logical)` pair**, so one transaction's
   read was delivered to another and landed in its read set under a key it did not own.

**Fixes.** Completion is a set keyed by the key, in every phase — the audit had this right from the
start with `seen`, and the two structures answer the same question. Routing is by an explicit request
identity (`TxnCommand.Origin`), never by a timestamp. And `prewrite` now asserts, in one line, that
every key it is about to write was read: the assertion caught a *third* instance the moment it landed.

**The database-side consequence, which is not a harness defect.** The same non-uniqueness reaches the
transaction record: it is addressed by `(primary, start timestamp)`, so two transactions sharing that
pair share a record, and the second's decision is refused as already made — it silently adopts the
first's fate. That is asserted at zero in the exit run and recorded in DESIGN-A6 §15 as a named gap
with the two fixes that would close it.

---

### BUG-021 — two transactions were minted at the same start timestamp, and shared a key's version

| field | value |
|---|---|
| **Found by** | sim — `transaction-atomicity`, seed **90004**, on a probe run taken to measure per-seed cost |
| **Phase** | A6 |
| **Reproduce (test)** | `go test ./sim/hunt -run TestBUG021` |
| **Found at seed** | **90004**: txn 14 and txn 29 both start at `1600000005840000000.26` |
| **Invariant that caught it** | transaction atomicity — a rolled-back transaction has no committed key |
| **Mutant class** | none existed — added **two**, `M67-minting-drops-the-node-tag` and `M68-restart-timestamp-derived-not-minted`, in the same commit as the fix |
| **Fix commit** | option A, both halves (DESIGN-A6 §22) |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M67-minting-drops-the-node-tag.patch && patch -p1 < sim/mutants/M68-restart-timestamp-derived-not-minted.patch && go run ./cmd/simctl replay --bundle seeds/BUG-021` |
| **Reproduce (seed)** | `seeds/BUG-021` carries seed **69**, found by a sharded search over `[0,3200)` with both halves of the fix removed: **49 detections in 3,200 seeds, first at 69** |
| **Correction, recorded because it was my claim and it was wrong** | This entry said no single mutant reintroduces the bug, so no bundle could name one. **Measured: `M68` alone reproduces it, on every one of the eight first-detecting seeds the search found, and `M67` alone reproduces it on none of them.** The asymmetry has a reason — `M68` makes a restarting transaction adopt a timestamp carrying another node's tag, and restarts are common, while `M67` needs two nodes to mint the identical `(wall, logical)` independently, which the pre-fix exit run saw 38 times in 25,000 seeds. The bundle names the **pair** because the pair is the defect's shape and the corpus lane's set support is then exercised by a real entry, and this line records that the pair is not a reproduction *necessity* |

**Symptom.** *"transaction 29 (start …840000000.26) is ROLLED BACK on its primary `a07`, and key
`a05` is committed at …840000000.59. Half of an aborted transaction is visible."*

**Root cause.** Two transactions were minted at the **same start timestamp** by **different nodes**:

| | start | primary | keys | outcome |
|---|---|---|---|---|
| txn 14 | `…000.26` | `a05` | `a05`, `a01` | committed at `…000.59` |
| txn 29 | `…000.26` | `a07` | `a07`, `a05` | rolled back |

Both wrote `a05`. A key's **lock owner**, its **data version** (`EncodeKey(ns, key, startTS)`) and its
**write record** are all addressed by the start timestamp — so for `a05` the two transactions share
every one of them. Txn 14 committed the version at `.26`; txn 29 was rolled back. The version belongs
to both, and no reader can tell whose it is.

The oracle is not confused. **The state is.**

**Why the guard I had did not see it.** DESIGN-A6 §15.6 predicted this class and asserted it at zero —
but keyed on `(primary, start timestamp)`, reasoning that that pair addresses the transaction
*record*. It does. It is not the only thing keyed by the start timestamp, and these two transactions
have **different primaries**, so the counter read zero on the seed that has the collision.

The assertion is now on the **start timestamp alone**, which is what the version and the lock are
addressed by. On seed 90004 it reports 1.

**Why it appeared now.** DESIGN-A6 §18 turned on clock holds, because A6's phase headline is hybrid
logical clocks and its sweep had been injecting no skew at all. Two nodes minting the same
`(wall, logical)` needs their clocks close together and their HLCs at the same logical counter —
which is exactly what a hold arranges. **The fault-mix fix found a real defect within hours**, which
is the strongest argument available for §18's rule.

**What it would have caused in production.** Two concurrent transactions touching a common key,
silently sharing that key's lock and version. One commits and the other aborts, and the key keeps a
value neither of them can be said to have written. No error, and no structural inconsistency — the
same shape as BUG-019, and caught by a different oracle for the same reason.

**The fix, and why it was reported before it was made.** The change is to the **timestamp source**,
which Amendment A6 legislated on directly, so DESIGN-A6 §22 set out three candidates and stopped.
Ansh ratified **A**: the low 8 bits of `Logical` carry the minting node's ordinal, and restart
timestamps are **minted rather than derived** — one decision in two halves, because `RestartAt =
commit.Next()` carries the tag of whoever minted that commit.

**Both mutants had to be earned.** `M67` was killed at once. `M68` survived twice: first because its
covering test was pinned to seed 90004, which found the bug and does not restart; then because the
counter answering it lived inside `nowAbove`, which `M68` deletes the call to — so the mutation
removed the guard along with the behaviour. It is killed now at 7 foreign tags across 10 restarts,
against 0 of 10 clean. §22.6 has the class both survivals belong to.

---

### BUG-022 — a transaction committed underneath an answer the database had already given

| field | value |
|---|---|
| **Found by** | sim — `bank-conservation`, the post-fix exit run |
| **Phase** | A6 |
| **Reproduce (test)** | `go test ./sim/hunt -run TestBUG022` |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M71-a-read-leaves-no-mark.patch && go run ./cmd/simctl replay --bundle seeds/BUG-022` |
| **Reproduce (seed)** | **seed 266** (re-pinned; originally seed 2521 — see below) |
| **First violating step** | range **1, index 111** — the commit record for `a00` at `1600000007630000000.3072`, written after the read at index 107 had been answered at `1600000007750000000.514` |
| **Invariant that caught it** | bank conservation over client-observed history |
| **Mutant class** | none existed — added **two**, `M71-a-read-leaves-no-mark` and `M72-prewrite-ignores-the-read-mark`, in the same commit as the fix, one per independently implementable half |
| **Fix commit** | the read mark: a fifth record kind, and a third first-committer-wins guard |

**Symptom.** *"the audit at 1600000008790243029.0 read all 8 accounts and they sum to -19, not 0."*

### ROOT CAUSE: a commit landed below a read that had already been answered

The whole finding is five entries of range 1's committed log, and `a00` is the key:

```
r1 idx=106  txn-get   a00  at 1600000007480000000.1792  -> "-15@4"    (txn 16)
r1 idx=107  txn-get   a00  at 1600000007750000000.514   -> "-15@4"    (txn 26)
r1 idx=109  prewrite  a00  start 1600000007480000000.1792  "4@16"
r1 idx=111  commit    a00  start ...1792 -> commit 1600000007630000000.3072
r1 idx=112  prewrite  a00  start 1600000007750000000.514   "-20@26"
```

Txn 26 was told `a00 = -15` **at 7.75**. Txn 16 then committed `a00 = 4` **at 7.63**, which is
*below* the timestamp txn 26 had already read at. Txn 26's snapshot therefore acquired a commit after
the fact: the value at its own timestamp was no longer the value it had been given, and the balance
it wrote — `-20`, computed from `-15` — silently discarded txn 16's transfer of 19 units. -19 is the
sum the audit saw, and 19 is the amount txn 16 moved.

**Nothing here is a fault.** No crash, no partition, no drop. Two transactions, one key, and two
clocks.

### Neither existing guard could fire, and that is the finding

`PrewriteInto` had two checks, and each is correct:

- **`ErrKeyIsLocked` covers LOG order.** It refuses a prewrite that arrives while somebody else's
  lock stands — here, the window `[109, 111)`. Txn 26's prewrite arrived at **112**, one entry after
  the lock was released.
- **`ErrWriteConflict` covers TIMESTAMP order.** It refuses a prewrite whose start timestamp sits
  below a commit already recorded. Txn 16's commit timestamp (7.63) is *below* txn 26's start
  (7.75), so there was nothing to refuse.

> **The two are total only where log order and timestamp order agree, and nothing in this system
> makes them agree.**

Percolator gets the agreement from its **single TSO**: a commit timestamp is drawn *after* the
prewrite, so it is above every start timestamp issued before it, and any read answered before the
prewrite is therefore below the commit. That is a property of the timestamp source, not of the
protocol — and it is nowhere in the protocol's own statement, which is why it survived being read
carefully three times.

Per-node HLCs do not give it. A transaction's timestamps come from `Node.Now()`, which reads
`m.replicas[0].hlc` — the **lowest-numbered range on that node**. Two nodes holding different ranges
therefore mint transaction timestamps from two clocks that **exchange no messages at all**: a range's
HLC advances only on messages for that range. They are coupled by physical time alone, and A6's clock
holds put them up to 90% of `maxOffset` — 450 ms — apart. Here the gap was 120 ms, and the read
landed inside it.

This is CARRY-FORWARD's **transaction identity gap** arriving from the other side. That entry
predicted two nodes minting the *same* timestamp; this is two nodes minting timestamps in the *wrong
order*, from the same cause.

### The fix: a read mark, and a third guard

First-committer-wins was checked against writers only. **A reader that has already been answered from
above my snapshot is as much a first committer as a writer is**, because my commit lands above my
start and can still land below their read.

Two halves, independently implementable, hence two mutants:

1. **The mark is recorded** (`M71`). A fifth record kind, `r <key> <^read_ts>`: the highest timestamp
   this range has been asked for this key at. Staged by `applyTxnTo`'s `OpTxnGet` case — the apply
   path the driver and the replay share — so it is a function of the log on both sides. **It is a
   function of the log only because in A6 every read IS a log entry**; A7 serves reads off-log via
   read index, and DESIGN-A7 has to say what replaces this before the first such read is answered.
2. **The mark is enforced** (`M72`). `PrewriteInto` refuses with `ErrWriteConflict` when the key's
   mark is **strictly** above the prewriter's start timestamp. Strictly, because a transaction reads
   its own keys at its own start timestamp, so `LessEq` would refuse every prewrite in the system.

**Why the three now compose, stated as the argument it is.** After the guard,
`readMark(key) <= startTS < commitTS`. So no read *before* the prewrite was answered at or above the
commit timestamp; and a read *after* the prewrite either sits at or above `startTS` and blocks on the
lock, or sits below `startTS` and so below `commitTS`. It rests on `startTS != commitTS`, which holds
because both are minted and two mints never collide — the node tag separates nodes, the logical
counter separates mints on one node, and `IdentityCollisions` asserts the cross-node half at zero on
every exit run.

**RE-PINNED at A7: seed 2521 → seed 266.** A7's term-start no-op (D-A7-6) adds one entry per election
per range, which moves every trace, and seed 2521 stopped carrying this finding. The corpus lane caught
it as **WEAK** rather than STALE — *diverges under `M71` but produces NO FINDING* — which is the
distinction that matters: the schedule still notices the mutation, and **sensitivity is not
reproduction**. Under the looser criterion considered at A5 (*any observable difference*) it would have
read `ok` (DESIGN-A6 §16.3c).

The new pin comes from a search over 600 seeds with `M71` applied and the mutation **verified present
in the tree** before the sweep: **2 of 600, first at seed 266.** Seed 266 reproduces the finding under
`M71` and replays clean without it. **The old seed is recorded rather than replaced** because the seed
moving is itself evidence about what the no-op changed.

**And this seed does double duty.** Ruling 13 requires the three-guard totality argument restated under
read index with `M71` and `M72` re-induced against the restated form — *a mutant that passes because
the property it attacks moved has stopped meaning anything* — and seed 266 is where that re-induction
runs.

**What the fix does to the same schedule.** Seed 2521 replays identically up to index 109, where txn
16's prewrite is now refused by `a00`'s mark, and txn 16 aborts explicitly at index 110. Txn 26 then
commits `-20@26` from a snapshot nothing contradicted, and the audit sums to zero.

**What it costs, measured rather than assumed.** Across 200 seeds: 71,933 marks staged, 1,802
prewrites refused by the new guard, and a commit rate of **0.611** against **0.624** and **0.615** on
the two pre-fix 25,000-seed runs. The refusals are almost entirely transactions that were losing
anyway by a slower route: `PrewriteBlocked` fell from 1,791 to 392 per 200 seeds as the refusal moved
earlier.

**The fifth kind is inherited like the other four.** `owns()`, `Records()` and `IngestRecordsInto`
carry it, so splits, snapshots and restarts move it without further code — which is what
`Records()`'s own comment promised a fifth kind would get. And its timestamp is in the **key** rather
than the value, so `kv.TimestampOf` reads it and BUG-023's clock invariant covers it. That is not
decoration: a read mark is the one record kind with **no companion data version at its timestamp**, so
the argument that excuses locks from that invariant does not reach it.

### The wrong-transaction-version hypothesis is DISCONFIRMED

Kept, because it was the first hypothesis and it was wrong. A roll-forward or rollback applied against
another transaction's version would move money either way depending on which side it landed on, which
fits the evidence. It is not what happens:

| seed | apply-resolutions | landed where the last prewrite was a different transaction |
|---|---|---|
| 2521 | 15 | 1 |
| 10303 | 5 | **0** |

Seed 10303 had **no** mispointed resolution at all and still created 10 units.

### The lead that replaced it was right in shape and wrong in every detail

The second hypothesis was *"first-committer-wins may not be holding"*, and it named seed 10303's txn 0
and txn 31 on `a04`: txn 0 appeared to start before txn 31 committed the key it overwrote. **The shape
was right and the instance was not.** Txn 0 had *restarted*, and the ledger was recording the start
timestamp it had abandoned — see BUG-024, which is both the reason that instance was misread and a
separate defect in its own right. Seed 10303's real cause is BUG-024; seed 2521's is this entry.

Two seeds, two causes, one symptom, and the only reason they were ever one line in a table is that
`bank-conservation` reports a number rather than a mechanism.

### Why seed 2521's 12 inversions touch no account — structural, not coincidence

The ruling asked for this to be explained rather than set aside, and the explanation is structural.

**Plain keys and bank accounts are stamped by different clocks.** A plain put or get is stamped at
propose by the *range's* HLC (`n.hlc.Now()` in `onClient`), so a key living in a split-born range gets
that range's clock — which is what BUG-023 was about. A transaction's timestamps come from
`Node.Now()`, which reads `m.replicas[0].hlc`: the **lowest-numbered** range on the node, which is
range 1, long-lived, and never a fresh child.

So a cross-range timestamp inversion can only appear on a `k*` key, and never on an `a*` account —
not because the accounts got lucky on that seed, but because nothing stamps them per-range. That is
also why BUG-023's fix left BUG-022 untouched, which is the prediction the evidence already
confirmed. **And it is the same fact that made this bug possible**: `replicas[0]` is a different range
on different nodes, so "the transaction clock" is several clocks.

**Independence from BUG-021, established rather than assumed.** Reproducing at a commit where
BUG-021 was live proves nothing on its own — the pre-fix run carried 38 collisions — so the seeds
were checked for shared start timestamps directly, using the WIDENED definition, since the pre-fix
counter used the narrow key that reads zero on collisions:

| seed | shared-start collisions pre-fix | pre-fix verdict |
|---|---|---|
| **2521** | **0** | **bank-conservation violation** |
| 10303 | 0 | **clean** |

So **seed 2521 establishes independence**: it violated before the fix, with no collision anywhere in
the run, so BUG-021 contributed nothing to it.

**What it would have caused in production.** A silent lost update between two transactions that never
touched each other's locks, on a cluster whose clocks were inside their advertised bound, with every
structural invariant of the database intact afterwards. There is no repair procedure, because there is
no record that anything happened: the database is a perfectly well-formed Percolator store with money
missing from it. It needs no fault to occur, and its rate scales with clock skew and with read volume
on contended keys.

### BUG-023 — a completed write was invisible to a later read, on a range at log index 1

| field | value |
|---|---|
| **Found by** | sim — **porcupine**, per-key linearizability, the post-fix exit run |
| **Phase** | A6 (defect is A4-shaped: reachable since A5, invisible until A6's fault mix) |
| **Reproduce (test)** | `go test ./sim/hunt -run TestBUG023` |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M70-ingest-does-not-seed-the-clock.patch && go run ./cmd/simctl replay --bundle seeds/BUG-023` |
| **Reproduce (seed)** | seed **12504**, key `k06` |
| **First violating step** | range **14, index 1** — a read of `k06` stamped `1600000002803920401.1280`, 92 ms of wall clock below the write it should have seen |
| **Invariant that caught it** | per-key linearizability — the A1 claim |
| **Mutant class** | none existed — added `M70-ingest-does-not-seed-the-clock`, in the same commit as the fix. Its sibling `M69` (the split entry's own floor) was **deleted with the half it removed**: that path was unreachable, and a mutant nothing can kill is a report of dead code rather than a gap (DESIGN-A6 §25.1) |
| **Fix commit** | every path that ingests records seeds the range's clock from the maximum timestamp among them |

The history is short and unambiguous:

```
c1/8  put "v9"   call=2.476s  return=2.500s  ok
c0/5  get ""     call=2.765s  return=2.789s  ok     <-- empty, 265ms after the put COMPLETED
```

A read that begins after a write completes must observe that write or a later one. This one observed
nothing.

### ROOT CAUSE: a split-born range starts with a fresh HLC

**Neither branch of the original hypothesis.** The birth payload is complete and
the extent check is correct — `k06` genuinely belongs to the child. The third
cause is the child's **clock**.

The log says it plainly:

```
range 2  idx 3  put   k06 "v9"  at 1600000002896384689.4096
range 2  idx 4  SPLIT at "k06" -> left=r2[k03,k06)@2  right=r14[k06,∞)@1
range 14 idx 1  get   k06       at 1600000002803920401.1280   -> EMPTY
```

The read is stamped **92 ms of wall clock BELOW the write it should have seen**.
MVCC then does exactly the right thing: at that timestamp the write does not
exist, so the answer is empty. The store is correct; the timestamp is wrong.

**Why.** There is one HLC per range — `store/machine.go` says so, and says why:
*"Two ranges on a node share a clock and not a logical counter, so a busy range
cannot inflate a quiet one's timestamps."* A split creates the child through
`newReplicaFor`, which calls `newReplica(m.cfg)`, which builds a **fresh
`hlc.Clock` whose `last` is zero**. Its first `Now()` therefore returns the local
*physical* wall.

The parent's HLC is not at the local physical wall. It is at the maximum of every
timestamp it has issued and every peer timestamp it has absorbed — so under clock
skew it sits **ahead** of local physical time. The child inherits the parent's
*versions*, stamped from that advanced clock, and none of its *clock*.

**Why nothing closes the gap.** A range's HLC only advances through `Update` on
messages **for that range**. The child's first messages come from its own leader,
stamped by the same fresh clock. **There is no path by which the parent's
timestamps ever reach the child's HLC.** The gap does not get closed; it expires,
once local physical time passes the parent's last stamp. Here that took 92 ms,
and the read arrived inside the window.

**Why A6 surfaced it.** §18 turned clock holds on. A hold puts one node up to 90%
of `maxOffset` — 450 ms — from another, so a parent HLC that has absorbed a fast
peer sits far above local physical time and the child's window is correspondingly
wide. Before the holds, the parent ran within microseconds of local physical time
and the window was too narrow to hit. **This is the fault-mix rule paying out a
second time** (DESIGN-A6 §18.3).

**Where the defect lives: A4's split path and A5's per-range clock, not A6.**
Reachable since A5, invisible until A6's mix. Reported before fixing, per Ansh's
ruling on defects in signed phases.

**The original lead, for the record.** The ledger records the answering read as:

```
READ node=0 index=1 at=1600000002803920401.1280 value="" found=false refused=false
```

**Index 1** — the read was answered as the *first entry of a range's log*, i.e. by a range born from a
split moments earlier. Either that range's birth state did not carry `k06`'s versions, or `k06` was
outside its extent and the request was answered instead of being refused and rerouted. Both are
A4-shaped, and both would have been reachable since A4; A6's mix (more splits, more ranges, clock
holds) is what surfaced it.

**BUG-022 is NOT this defect — they are two findings, not one.** The cross-range
timestamp inversion this bug leaves behind was counted on all three seeds:

| seed | verdict | inversions | on bank accounts |
|---|---|---|---|
| 12504 | linearizability | **2, both on `k06`** | 0 |
| 2521 | bank-conservation | 12 | **0** |
| 10303 | bank-conservation | **0** | 0 |

Seed 12504's inversions are on the failing key itself. Seed 2521 has inversions
but **none on an account**, and seed 10303 has none at all. Bank timestamps come
from `Node.Now()`, which reads `replicas[0].hlc` — the lowest-numbered range,
long-lived, never a fresh child — so the bank is structurally not exposed to this
defect. **BUG-022 has its own cause and its own fix** — a commit landing below a
read already answered — and the prediction this table makes was confirmed by
fixing this one and watching BUG-022 survive it untouched.

**It is not a plain-workload read at a remembered timestamp.** Those are excluded from the
linearizability history by construction, and the ledger shows this one carrying a node-tagged
timestamp — a live read, not a snapshot read.

**Independence from BUG-021, established.** Seed 12504 has **zero shared start timestamps** pre-fix
under the widened definition, and fails there anyway. BUG-021 contributed nothing to it.

### BUG-024 — a transaction computed its writes from two different snapshots

| field | value |
|---|---|
| **Found by** | sim — `bank-conservation`, while investigating BUG-022 |
| **Phase** | A6 |
| **Reproduce (test)** | `go test ./sim/hunt -run TestBUG024` |
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M73-a-read-answer-lands-in-any-incarnation.patch && go run ./cmd/simctl replay --bundle seeds/BUG-024` |

**RE-PINNED at A7: seed 10303 → seed 5042.** The term-start no-op moved every trace and seed 10303
stopped carrying this finding — the corpus lane reported it **STALE**: *replays identically with the
mutant applied*, the mutation changing nothing on that schedule.

**The search that found the new pin is the disposition's evidence.** 600 seeds under `current` found
nothing, and the honest reading was *not found at this budget* rather than *unreachable* — `M73`'s own
declaration is per-seed **0 of 200**, sweep-detected, so a per-seed search was looking for something
its own numbers call rare. Sharded over seeds 600–9000: **1 of 8,400, at seed 5042.** The rate is
roughly one in eight thousand, which is why six hundred seeds saw nothing.

*The bundle was never retired on the null.* Ansh, on the standing rule: a null at an underpowered
budget is not a measurement, and retiring a bundle on one would be the mistake this cycle exists to
avoid.
| **Reproduce (seed)** | **seed 5042** (re-pinned at A7; originally seed 10303 — see below) |
| **First violating step** | the read answer that arrived after the restart, carrying the abandoned snapshot's timestamp; the guard now counts it as `StaleIncarnation`, and seed 10303 produces exactly **one** |
| **Invariant that caught it** | bank conservation over client-observed history |
| **Mutant class** | none existed — added `M73-a-read-answer-lands-in-any-incarnation`, in the same commit as this entry. **It measured `0 of 200` and took an opt-out saying an honest floor would need a 24-hour sweep. That was the broken probe**: the class's detector is an aggregate assertion, not a per-seed verdict, and the fixed probe finds it at 60 seeds — `StaleIncarnation` goes 9–15 per fifty seeds to a flat zero, on the criterion *no read answer from a pre-restart incarnation was ever rejected* (DESIGN-A6 §42) |
| **Fix commit** | a read answer is rejected unless `cmd.ReadTS == t.startTS` |

**Symptom.** *"the audit at 1600000005203989560.0 read all 8 accounts and they sum to +10, not 0."*

**Root cause.** A transaction that finds a commit inside its uncertainty interval restarts: it takes a
**new** start timestamp and re-reads every key. The reads it issued *before* the restart are still in
flight. Their answers arrive afterwards, carrying the old snapshot's values, and nothing checked which
incarnation an answer belonged to — so they landed in the new snapshot's read set. The transfer then
computed its writes from **two different instants**, which conserves nothing, in whichever direction
the two snapshots happened to differ. Seed 10303 gained 10 units; seed 2521 lost 19 by a different
cause entirely (BUG-022).

**It is BUG-020's family: an answer accepted for the wrong incarnation.** The epoch guard in `store`
exists for the same shape one layer down — a durability completion from a dead incarnation arriving
after a restart — and the phrase is borrowed from it deliberately, because the fix is the same one:
stamp the request with the incarnation and check the stamp on the way back.

**The harness defect it was hiding, and why it is recorded here rather than only in a commit.** The
same investigation misread seed 10303 as a first-committer-wins failure between txn 0 and txn 31,
because `RecordTxnBegin` recorded a transaction's **original** start timestamp and nothing updated it
when the transaction restarted. The ledger therefore placed txn 0 before a commit it actually
followed. `TxnRecord.Restarts` existed as a field **nothing ever wrote**, so the note left for the
next reader — *check `Restarts` before reasoning about two transactions' relative start times* —
pointed at a number that is zero however many restarts occurred. That is the vacuous-evidence class
wearing the shape of a correction. It is fixed in the same commit: `Ledger.RecordTxnRestart` moves the
recorded start timestamp and counts the restart, and the exit run asserts the ledger's restart count
**equals the coordinator's**, so the day the recording path stops being called the run says so
instead of quietly describing transactions that never existed. Per DR-29 the harness defect belongs in
its fix commit and the design doc; it is named here because it is the reason this entry's own
investigation went wrong first, and an entry that hid that would be teaching the wrong lesson.

**What it would have caused in production.** A client library whose retry-on-uncertainty path accepted
late answers would corrupt a transaction's read set without any error anywhere, on a schedule with no
faults in it. The corruption is proportional to how much the two snapshots differ, so it is largest
exactly when the workload is busiest.

---

### BUG-025 — (harness) a follower forwarded every read and was answered by nobody

| | |
|---|---|
| **Symptom** | Follower reads were implemented, dispatched and forwarded, and not one was ever answered. No error, no timeout, no dropped-message count: the request went out and nothing came back. |
| **Found by** | ruling 2's census field reading **zero** — `follower=0` — after `served` proved reads were being taken. |
| **Reproduce (seed)** | any A7 seed with `FollowerReadPerMille > 0`; the count is the reproduction. |
| **Invariant that caught it** | none. **No invariant could.** A read nobody answers is indistinguishable from an unavailable replica, which this system treats as correct behaviour. |
| **Mutant class** | `TestMessageCodecCarriesEveryField` and `TestEveryMessageFieldIsCarried`, both added with the fix. |

**The defect.** `MsgReadIndex` and `MsgReadIndexResp` were added to `raft/` carrying `ReadCtx` and
`ReadIndex`. `store/codec.go`'s `encodeMessage` serialises a **fixed field list**, and neither field
was in it.

> **A message type added to `raft/` and not to the transport's field list arrives with its type byte
> intact and its payload ZEROED.** The message is delivered. The thing it exists to carry is gone. No
> error is raised, because nothing in the codec knows a field was expected.

So a follower forwarded a read whose context arrived empty — matchable to no request — and the
answer's index arrived as zero, which no replica can ever have applied past. Both directions failed
silently and the read simply never completed.

**Why every test passed.** The raft-level tests call `Step` directly and never cross this boundary.
`TestAFollowerAsksTheLeaderRatherThanConfirmingItself` verified the follower forwards and adopts the
leader's index — correctly, and entirely in memory.

> **The protocol was right and the wire was not, and nothing in the tree looked at the wire.**

**This is the second codec defect in this project hidden by tests that bypass serialisation.** The
first was A1's decode off-by-one (`M21`, BUG-001's bundle mutant), where the harness's own codec was
wrong and porcupine returned green over forty operations no node had answered. Same shape, six phases
apart, and the general rule is the one the second instance makes unavoidable:

> **A unit test that exercises a mechanism without its serialisation will pass over a wire that does
> not work.** The mechanism is not the protocol plus the encoding; it is the protocol *through* the
> encoding, and a test that stops at the boundary has tested the half that was never in doubt.

**The fix, and why it is two tests rather than two lines.** The two missing fields are two lines.
What was missing was any reason to notice, so:

- `TestMessageCodecCarriesEveryField` round-trips a message with **every** field populated and asserts
  each survives — induced by dropping `ReadCtx` from the encoder;
- `TestEveryMessageFieldIsCarried` reads `raft.Message` **by reflection** and requires every exported
  field to appear in `encodeMessage`'s body. It is a source scan and deliberately crude, for
  `TestOneApplyPath`'s reason: it fails the moment a field is added and the wire is forgotten.

The second covers the types that existed **before it did**, which is what makes it an answer rather
than a patch — verified by dropping `SnapConf`, an A2 field, and watching it fail naming `[SnapConf]`.
Audited at the time of the fix: **20 fields on `raft.Message`, 20 carried.**

**And it should have existed after A1.** The first codec defect produced the same class of silence and
the response was to fix the off-by-one. Nothing was built that would have caught the second, and the
second arrived in a different codec six phases later.

**What it would have caused in production.** Every follower read would hang forever while the cluster
reported perfect health: leader reads served normally, no error counters moving, no message losses
recorded. A client library with a timeout would see follower reads as universally slow and route
around them, and the feature would be quietly dead in a system whose own tests said it worked.

### BUG-026 — a read was answered by a range that no longer held the key

| | |
|---|---|
| **Symptom** | A client wrote a key, was acknowledged, read it back a second later and was told the key was absent. 526 of 25,000 seeds (2.10%) in A7's exit run. |
| **Found by** | per-key linearizability, in A7's 25,000-seed exit run — every one of the eight shards. First at seed 30. |
| **Reproduce (seed)** | `30` under A7's shape; `k06`, in 5 s. Disappears with `SplitThreshold = 0` at unchanged read volume (5 of 5 seeds → 0 of 5), which is the isolation. |
| **Invariant that caught it** | Linearizability of single-key reads and writes — and, after the oracle was given the half it was missing, `read-index-answers-match-the-log`. |
| **Mutant class** | `M78-read-answered-outside-its-extent`, added with the fix. |

**The defect.** `Node.onClient` routes a request to the replica whose extent covers the key **at
arrival**. A read-index read is then queued and answered later. Between those two moments the range
can apply a split, hand the key to the right-hand range and drop it locally — and
`serveReadyReads` answered from that state anyway, because it never asked whether the range still
owned the key.

```
put v5   call=533720516   ret=546804263    ok
get      call=1501478943  ret=1509850844   ok  value=""      <- a second after the ack
```

> **The answer describes the absence of a RANGE and was delivered to the client as the absence of a
> VALUE.**

**Why the replicated path never had this hole.** Because a read is an **entry** there. It applies at
a log position, the apply loop asks `n.desc.Contains` exactly as it does for every other command, and
an entry outside the extent goes to `rerouteAt` — which pointedly does **not** end the operation in
the history, because nothing told the client anything. Read index has no entry, so it reaches no
apply, so it reached no check.

**The oracle was right and silent, and that is the entry that pairs with §5b.** A7's differential,
`read-index-answers-match-the-log`, replays the range's committed log to the position the node
reached and compares. Range 1's log at that position genuinely no longer holds `k06` — the split
entry removed it. **So the live answer and the replay agreed, both saying "not found", and the only
instrument that can catch a stale read no client observed was silent while a client was being told a
key it had just written was gone.**

§5b's entry is an oracle that was *wrong* and a mutant that found it. This one is an oracle that was
*right about the property it checked* and silent about the property that was violated — and §4.1
states it exactly:

> **Naming every fact you take is not the same as naming every fact you need.**

Ownership was a fact `serveReadyReads` needed, did not take, and no instrument was ever asked for.
Together the two entries are the honest account of what an oracle is.

**The fix is two parts, and the narrow one is the smaller.** Narrow: `serveReadyReads` consults the
extent at answer time and reroutes rather than answering, never ending the operation OK — matching
`rerouteAt` exactly, including a separate counter (`ReadsOutOfExtent`) so the fix can be seen to have
run rather than assumed to have. Broad: **DESIGN-A7 §5e enumerates every check that currently runs
because a read is a log entry**, and marks each preserved, replaced or dropped with evidence. That
enumeration is an A7 exit criterion, and it immediately produced BUG-028.

**What it would have caused in production.** Every range split would silently manufacture a window in
which reads of moved keys returned "key not found" to clients that had just written them. It scales
with split rate, so it gets worse exactly as a cluster grows, and it is invisible to every health
metric: the read succeeded, quickly, with a well-formed answer.

### BUG-027 — the read-index wire advertised a term that was not on disk

| | |
|---|---|
| **Symptom** | A follower forwarded a read, or a leader answered one, carrying a term that had not been persisted. 118 of 25,000 seeds (0.47%). |
| **Found by** | `persist-before-reply`, unaided — an A1 oracle catching an A7 wire. |
| **Reproduce (seed)** | `99`, `155`, `375` under A7's shape (3 in 640). Requires follower reads: with `FollowerReadPerMille = 0` no `MsgReadIndex`/`MsgReadIndexResp` is ever sent. |
| **Invariant that caught it** | Persist before reply — term, vote and log durable before replying to any RPC. |
| **Mutant class** | `M80-a-new-message-type-joins-the-allow-list`, added with the fix. |

**The defect.** `MsgReadIndex` and `MsgReadIndexResp` were emitted through `r.send`, whose own doc
comment reads *"releases a message that attests to no persistent state"*. Both carry `Term: r.term`.

**It was described in advance, in the exact terms it occurred in.** One screen below the call site,
`sendGatedOn`'s comment:

> *The TERM's mark is added here rather than at each call site, because **every message that leaves
> this node carries r.term and therefore makes the same claim about it**. A call site that forgot
> would be a gate missing from one message type, which is precisely how the first fix for BUG-017
> covered two of the three paths that emit an append response.*

> **A comment that predicts a defect and does not prevent it is a comment doing the wrong job.**

**And the enumeration argued itself into it.** The normative gate table's stanza for this message
said, in full: *"gated on: a leadership-confirming quorum, **not** durability — without it: nothing.
Read index attests to a commit index, which is already durable by the time it is committed."* Every
word of that is true **about the payload**, and the message carries a second attestation on the same
wire. The argument covered one and not the other. That is BUG-017's first fix, one phase later, in
prose instead of in code.

**The fix is three parts.**

1. Both sites take `sendGated`, with `// GATE:` comments.
2. **`send` refuses by default.** It now accepts only the three types with a written non-gate
   argument (`MsgPreVote`, `MsgPreVoteResp`, `MsgTimeoutNow`) and panics on anything else, naming the
   remedy. The default is inverted: **a new message type is gated unless somebody enumerates it,
   rather than ungated unless somebody remembers.** The test is by TYPE rather than by term value,
   which is exact — a value test would false-accuse `MsgPreVoteResp`, which echoes the *requester's*
   proposed term and can legitimately equal this node's own.
3. `tools/gatepin` gains **the direction that was missing**. It walked `// GATE:` comments and
   required each to be discharged by a withholding send — keyed on the comment, so a gate that is
   *enumerated* and has no comment anywhere is invisible to it. That is precisely what
   `MsgReadIndexResp` was: in the pinned set, with a plain `r.send`, and no test connecting those two
   facts. Now the enumeration is walked too, and the pair is the invariant: **the set of documented
   gates and the set of marked gated sends are the same set.** Induced by removing a marker.

**`MsgTimeoutNow`: gated by consequence, not by luck.** The other two ungated term-bearing sites were
triaged rather than assumed. Both are reachable only from `RoleLeader` with a different voter as
target (`TransferLeadership` panics on self); becoming leader of a multi-voter configuration in term
T requires `MsgVote` for T, and that send is gated on T's durability — so T is durable before the
node is a leader, hence before either site. The single-voter case reaches neither. **The argument
holds and was written down nowhere**, and the paragraph that *was* written down argues about what the
order means, which is not what the oracle checks. An unwritten premise is how the second occurrence
of a class gets scheduled.

**A test was pinning the defect in place.** `TestAFollowerAsksTheLeaderRatherThanConfirmingItself`
drained `Ready()` immediately after `ReadIndex` and required the forward to be present, so adding the
gate turned it **red**. It did not merely fail to catch BUG-027:

> **A test that asserts a message goes out immediately is a test that will resist any gate ever being
> added to it.**

It now asserts both halves — withheld before `AckPersisted`, released after.

**What it would have caused in production.** A follower crashes after forwarding a read advertising
term T, restarts having forgotten T, and re-participates in a term it has already spoken in. It is
the canonical term-amnesia case, on a wire added by a *read* path — the one place nobody expects to
find a durability claim.

### BUG-028 — a plain read was answered at a clock reading, and missed a write it had already applied

| | |
|---|---|
| **Symptom** | A replica that had applied *past* the confirmed read index answered "not found" for a key its own committed log held. |
| **Found by** | **the §5e enumeration** — not by a sweep, not by a code review. Listing the checks a read gets for being a log entry produced "the read's timestamp is log-ordered" as the second entry, and asking whether read index preserved it answered no. |
| **Reproduce (seed)** | `22` with `SplitThreshold = 0` and follower reads on; `k06`. Measured 1 in 400 seeds with splits off, where BUG-026 does not mask it. |
| **Invariant that caught it** | `read-index-answers-match-the-log`, once its model was corrected to read the **latest** version for an off-log answer. |
| **Mutant class** | `M79-read-index-answers-at-a-clock-reading`, added with the fix. |

**The defect.** `serveReadyReads` answered with `n.mvcc.ReadAt(key, q.at)`, where `q.at` was
`n.hlc.Now()` taken on the serving replica **when the request arrived**. `ReadAt` is documented to
return *the newest version at or before ts*. Under skew inside `maxOffset` a follower's clock sits
below the leader's, so a write the leader stamped from its own clock is **invisible** to a read the
follower stamps from its own — however far past the confirmed index that follower has applied.

```
range 1: node 2 answered a read of "k06" at 1600000004930000000.770 OFF THE LOG with ""
(found=false), having applied to index 185 -- but that range's committed log at index 185
holds "v9" (found=true)
```

The read waited correctly and then asked the wrong question.

> **Copying the shape of a guarantee is how you lose it quietly.**
>
> `answerAt`'s comment says a replicated read is answered *"at its own timestamp… not at the newest
> version"*, and every word of it is true — **because the entry's timestamp is log-ordered**, stamped
> by the leader at propose and applied in log order, so it sits above every earlier write's. It is a
> guarantee with a reason. `serveReadyReads` copied *answered-at-a-timestamp* and inherited none of
> the reason.

**And the irony is sharper than a copied comment.** Read index exists in this project *because it is
correct without trusting clocks* — that is the sentence CLAUDE.md uses to cut leader leases to
STRETCH and keep this. The implementation then answered at a clock reading. **The mechanism whose
entire purpose is to remove the clock dependency reintroduced it**, in the one line where nobody was
looking for a clock.

**Neither site was wrong on its own, which is why no review would have found it.** `answerAt` is
correct and its comment is correct. `serveReadyReads` reads a plausible timestamp from a legitimate
clock and hands it to a documented API that does exactly what it says. The defect is the *relation*
between them — a property that lived in one site's context and was dropped by the other's — and a
relation is not visible from either end. It took a table with a row for the property.

**The argument that let the read leave the log is the argument it violated.** D-A7-5 rules that read
index may serve a plain read and nothing else, and the whole ruling rests on one sentence:

> *a **plain** read has no timestamp to protect. It is a linearizable read of the latest value, it
> participates in no transaction, and no prewrite's correctness depends on whether it happened.*

**The implementation then gave it a timestamp anyway.** And read index's entire claim — the reason
CLAUDE.md cuts leader leases and keeps this — is that it is *correct without trusting clocks*.
Answering at a clock reading put the clock back in, in the one mechanism chosen for not needing one.

**The fix.** `kv.Store.ReadLatest`: the newest version, no timestamp bound, and deliberately no GC
mark check — the mark refuses reads at a timestamp GC has passed, and a read of the latest version
names no timestamp for the mark to be below. The ledger records `At` as the **unset** timestamp for
off-log answers, and `raftcheck.ValueAtIndex` reads unset as "the latest version", so the model asks
the question the path actually asks.

**Correcting the oracle is what made it visible.** The model previously read at `r.At` — the same
clock reading the node used — so both sides were wrong in the same way and agreed. That is BUG-026's
blindness in a second dimension, and it is why the enumeration is an exit criterion rather than a
document.

**What it would have caused in production.** Every follower read is a coin-flip against clock skew: a
key written on a node whose clock runs fast reads as absent from a node whose clock runs slow, for as
long as the offset lasts. It is worst exactly where follower reads are most wanted — a large cluster,
spread out, with real offsets — and it produces *stale* answers rather than errors, so nothing
retries.

### BUG-029 — (harness) the seed count read double, and the wrong number agreed with what we already believed

| | |
|---|---|
| **Symptom** | Every shard of A7's 25,000-seed exit run reported 6,250 seeds for a 3,125-seed range, and `TestRaftExitAggregate` refused to aggregate the run after 6h35m. |
| **Found by** | reading the shard censuses. The aggregate's own coverage guard fired correctly; nothing had asked the same question at a scale anybody runs before launching. |
| **Reproduce** | `TestASweepCountsEachSeedOnce` — two real seeds, ten seconds. Reads 4. |
| **Invariant that caught it** | the exit aggregate's coverage assertion (contiguous, non-overlapping, exactly N). |
| **Mutant class** | the test above *is* the induction; re-adding the increment turns it red. |

**The defect.** `SweepRaftWith` did `c.Seeds++` and then folded in `CensusOf`, which sets
`c.Seeds = 1`. Introduced at `d8589a9` by extracting `CensusOf` and **leaving the increment behind in
the caller it was extracted from** — a known refactoring hazard whose tell is that two counting paths
existed and only one was updated. `TestPowerProbe`, which the extraction was *for*, calls `CensusOf`
correctly and was never affected.

**A6 is untouched, and the way that was established is the part worth keeping.** A6's signed exit run
is at `611d0b9`, seventeen hours before `d8589a9`. But the argument does not rest on dates: A6
reported pass 24903 + violation 0 + inconclusive 97 = **25,000 exactly**, and its aggregate
**passed** — running the same guard that refused A7's.

> **When asking whether a past result carries a defect, prefer a mechanism that would have caught it
> over a date.**

**The corroboration, which is the finding.** `shard_test.go`'s header quoted "A6's 3.75 s/seed". That
was A6's *planning* figure; A6's own exit run measured **8.4 s/seed** and said so in its commit
message. The stale number survived in the comment anyway — and then A7's shards printed **3.75
s/seed**, because a doubled denominator halved a true 7.5.

> **A corrupted value that contradicts expectation gets questioned. One that confirms a stale
> expectation is invisible. The most dangerous form of a wrong number is one that agrees with what
> you were already going to believe.**

Had the doubling produced 12.7 or 2.1 it would have been challenged on sight. It produced the number
already written in the file that everyone read first.

**Corollary, and it is actionable rather than rueful:** *a planning figure left in a comment becomes
the expectation a real measurement is checked against, so it should be deleted the day the first real
rate is taken.* Both stale figures are now gone from `shard_test.go` and `exit-run.sh`, and every
number that replaced them says which run measured it.

**Three checkers were reading a halved rate, and the direction matters.** None flipped on this run's
values — but a checker weakened is a checker weakened whether or not it mattered on the day, so each
site now says so:

| site | true | reported | direction |
|---|---|---|---|
| inconclusive per-mille, floor 30 | **5.88** | 2.94 | halved |
| seeds-with-contention, floor 10% | **99.96%** | 49.98% | halved |
| seeds-with-no-leader, **ceiling 20%** | 0% | 0% | **halved, i.e. easier to pass** |

The no-leader **ceiling** is the one to name: a halved value against a maximum **hides a failure**
rather than inventing one, which is the direction that does not announce itself.

### BUG-030 — (harness) a running script was edited, and a run's exit status stopped describing the run

| | |
|---|---|
| **Symptom** | A7's exit run logged `all shards finished`, then `line 75: im/hunt/: No such file or directory`, then `line 78: syntax error near unexpected token 'done'`, and `make` returned 2. |
| **Found by** | reading the run's log against the commits made during it. |
| **Reproduce** | induced: a padded script rewritten **in place** (same inode) one second into a run. Top-level form dies `syntax error near unexpected token 'do'`, EXIT=2. `perl -i` renames and does **not** reproduce it. |
| **Invariant that caught it** | none; this is the eighth instance of the observability family. |

**The defect.** POSIX `sh` reads a script **incrementally, by byte offset, while it runs**. `b5eba7e`
edited `scripts/exit-run.sh` forty minutes into the run to fix a banner. Six and a half hours later
the shell returned from `wait`, resumed reading at its saved offset — now pointing into the middle of
a file that had grown — executed a fragment of the loop body as a command (creating a spurious
`shard-008.log`, with `i` at 8), hit a bare `done`, and exited 2.

**The shards were unharmed**: already-forked children with their own binaries, their JSONs complete
and their ranges intact. That is what makes this the sharpest form of the family:

> **The run's own exit status stopped describing the run.** `EXIT=2` was evidence about the shell,
> not about the sweep, and nothing said so.

**The fix is two halves and the second is not decoration.** The body is now a `main()` function, so
`sh` parses it in full before executing anything. **Measured**: with the wrapper and a bare
`main "$@"`, the body ran correctly *and the shell then died 127 on the shifted tail*. Only
`main "$@"; exit $?` closes it — the wrapper protects the run, the explicit exit protects the status.
Separately, `wait` is now per-pid with the statuses counted, because a bare `wait` returns 0 whatever
the children did, which is the same family again.

"Do not touch the tree mid-run" remains the rule. This is what makes breaking it survivable.

### BUG-031 — (harness) two standing lanes were red on the tree, and the handoff reported neither

| | |
|---|---|
| **Symptom** | `make hatches` and `make corpus` — both CI lanes on every push, and `corpus` is A7 exit criterion §8.2.3 — were failing at `faad5a2`, before any work in this session. |
| **Found by** | running them. Confirmed pre-existing by stashing every local change and re-running. |

**The determinism lane.** `store/codec_readindex_test.go` — BUG-025's own fix — imports `os` to read
`codec.go`'s source text. `store/` is core scope, where that is a violation. **The precedent was
already written down in this repo**, in `tools/gatepin`'s header, explaining why the durability-gate
pin lives under `tools/` and not in `raft/`: *"it reads the source text… Reading a file to check a
contract is tooling, and tooling lives here."* The structural half moved to `tools/codecpin`; the
semantic half, which needs no source text, stayed. **Not hatched** — a hatch is a per-line escape for
code that must live where it is, not a way to legalise a misplaced file.

**The corpus lane.** Every stored bundle diverges from its recorded trace, recorded at
`c39a53adfb8c` — the commit immediately before A7's term-start no-op. The no-op moved every trace, as
DESIGN-A7 §7 said it would; BUG-022's and BUG-024's bundles were re-pinned during the phase and the
other twenty-two were not. `make corpus` has therefore been red since `965ec87`, A7's first commit.
Regenerated here with `simctl replay --rerecord`, which keeps each plan and re-records this commit's
observation — and then `make corpus-reproduces` is **read rather than assumed**, because they are
different questions and A5 paid to learn it.

**The finding is not either lane.** It is that a phase handoff written at the context limit reported
`corpus-reproduces` (18 ok, 4 skip, 2 WEAK) in detail and did not report that `corpus` itself was
red, and did not report the determinism lane at all.

> **A lane's state is only carried forward if somebody runs it. A handoff that lists what a lane
> found last time is not a statement about the lane's state now**, and the two read identically on
> the page.
>
> And the general form, which is the part that travels:
>
> **A rule written about one instance does not generalise itself to its siblings.**

**This is the ninth instance of the observability family**, and the more embarrassing half is that the
handoff **had a section for exactly this**. §5.4 is headed *"`corpus-reproduces` — state it, do not
assume it"*, it prints last run's numbers, and it says in bold that the verdict is *"read rather than
assumed"*. The instruction was written, correctly, about the lane next door — and the lane it was
written about was reported while the two beside it were not run at all.

> **A rule written about one instance does not generalise itself to its siblings.** The section that
> says "state it, do not assume it" was itself a statement about one lane, assumed to be the only one
> that needed it.

### BUG-032 — a snapshot moved the state machine forward and left `applied` behind

| | |
|---|---|
| **Symptom** | A replica answered a read with the correct, latest value while reporting a state-machine position several hundred entries below where it actually was. |
| **Found by** | `read-index-answers-match-the-log` **false-accusing a correct answer** — seed 36 under A7's shape with splits off. Porcupine was green on that seed, and it was right. |
| **Reproduce (seed)** | `36`, `SplitThreshold = 0`, key `k02`: node 2 answers `"v52"` (the latest write, and linearizable) while reporting `AppliedAt=512`, a position at which the log holds `"v73"`. |
| **Invariant that caught it** | none — this is the *instrument* being wrong about the system, which is BUG-016's standard in the other direction. |
| **Mutant class** | `M81-snapshot-install-leaves-applied-behind`. |

**The defect.** `n.applied` was written in exactly **one** place: the committed-entry branch of the
apply loop. Installing a snapshot ingests the whole state machine at the snapshot's index and never
touched it. `raft`'s own `appliedIdx` **is** advanced on install (`raft.go:1198`, `:1588`), so the two
were a shadowed fact updated on one path and not the other.

> **The same family as BUG-029: one fact, two places that maintain it, one of them updated.** There
> the count read high; here it reads low.

**Low is not harmless, and it is not only a reporting problem.**

- `serveReadyReads` waits on `n.applied`. A read whose confirmed index the *snapshot already covers*
  waits for a committed entry instead — and on a quiet range that entry may never arrive, so the read
  is never answered. An unanswered read is not a safety violation in this system, which is exactly
  why nothing found this for three phases.
- The ledger records it as `AppliedAt`, so the differential oracle replays the committed log to a
  position **below** the node's real state and reports a correct answer as stale.

**Three paths, not one.** The snapshot install during drain; a range that *starts* from a snapshot;
and restart recovery from a stored snapshot. All three put the state machine at an index no committed
batch covers, and all three now say so.

**What it took to see it.** Nothing in the tree compared a node's claimed position against its actual
state — and the oracle that could, could not, because both sides of its comparison were derived from
the same understated number until BUG-028's fix made the model read the *latest* version instead.
Correcting one instrument is what let the next defect speak.

> **A FALSE ACCUSATION IS A FINDING UNTIL PROVEN OTHERWISE.**
>
> The oracle accused a correct answer, and the reason it was wrong was a real defect in the system it
> was measuring.

**The alternative disposition is the one to name, because it is the natural one.** Seeing an oracle
report a violation on a run porcupine calls clean, the obvious move is *the instrument is wrong,
correct the instrument*. That move was available, it was cheap, and it would have worked: the
accusation would have gone away. It would also have left `n.applied` lagging behind every snapshot
install, reads stalling on quiet ranges after every rebalance and every restart, and the only
instrument that could see it permanently taught not to look.

**One fact in two places with one of them maintained** is the class, and it is the class BUG-029 is in
too — there the count read high, here it reads low. What made this one *visible* is that the second
place was **right**: `raft`'s own `appliedIdx` is advanced on install, so the disagreement existed and
something could disagree with. A shadowed fact where both copies are wrong together produces no
symptom at all, which is BUG-026's blindness in a different dimension.

**What it would have caused in production.** After any snapshot install — which is every replica that
falls behind, every new replica added by a rebalance, and every restart — read-index reads against
that replica stall until the next write commits. On a read-heavy range that has gone quiet, they
stall indefinitely, and the replica reports itself healthy throughout.

### BUG-033 — (harness) a killed measurement left a mutant applied, and every number after it read plausible

| | |
|---|---|
| **Symptom** | A mutant class measured **28 of 600, first at seed 30**. The true figure for that class, measured alone, is **0 of 400**. Nothing in the output looked wrong. |
| **Found by** | checking the tree, not the log — `grep` for the fixed line after the run, because the previous invocation had been killed by a timeout. |
| **Invariant that caught it** | none. This is the provenance family arriving **inside the instrument that measures provenance**. |

**The defect.** A measurement driver applies a mutant patch, sweeps, and reverts. A foreground
invocation was killed by a timeout mid-sweep, and the version in use had no trap, so **M78 stayed
applied**. The next invocation's `patch --forward` reported *"Ignoring previously applied patch"* and
returned non-zero; `set -eu` aborted that measurement silently, the loop moved on, and the following
three classes were measured **against two mutations at once**.

> **A measurement that ran against the wrong tree does not fail. It reports.** 28 of 600 with a first
> detection at seed 30 is a perfectly ordinary-looking floor, and it would have been written into a
> patch header and defended.

**What made it findable** was the same rule this phase already had for runs — *"started" is read from
the process, never from the launch* — applied one level down: **the tree state is read from the tree,
never from the fact that a revert was supposed to have happened.**

**The fix.** The driver snapshots `git diff` before applying, and after reverting compares them; a
mismatch prints `TREE DID NOT RESTORE -- every later measurement is suspect` and exits non-zero. It
also refuses to measure at all if the patch does not apply cleanly, rather than falling through. The
trap covers `EXIT INT TERM`, so a kill reverts.

**Why it belongs in BUGS.md rather than in a commit message.** Because the four floors this session
declares are the first numbers in the project taken by a driver that can prove which tree produced
them, and the three that were nearly declared without that proof are the argument for it. A floor is
a claim about the harness; a floor taken against an unknown tree is a claim about nothing.

### BUG-034 — (harness) the bundle format stopped carrying the build, one phase before anybody looked

| | |
|---|---|
| **Symptom** | Every bundle in `seeds/` replays with the **read-index path off**, whatever shape it was recorded on. `make corpus` is green over it, because the plan is identical either way. |
| **Found by** | trying to re-pin BUG-009 at a seed measured under A7's shape, and finding the bundle could not express that shape. |
| **Invariant that caught it** | none. The corpus lane cannot see this: it compares a replay against a recording made by the same incomplete writer. |

**The defect.** `simctl`'s `RaftMeta` pins the build a bundle replays against. It carries A2's four,
A3's two, **A4's two**, **A5's two**, **A6's two** — and not A7's. `ReadIndex` and
`FollowerReadPerMille` are on `hunt.RaftOptions`, set by `A7Options`, and absent from the struct that
records what ran.

**The struct predicted this, three times, in its own comments.** They are worth reading in order:

> *A4's two build parameters. A bundle that did not carry them would replay a single-range cluster
> against a schedule recorded on a split one, which is the "bundle that does not pin its build"
> failure this struct exists to prevent — and it would do it silently, since the plan is identical
> either way.*
>
> *A5's two. Same reasoning: a bundle that did not carry them would replay a single-version store
> against a schedule recorded on an MVCC one.*
>
> *A6's two. Same reasoning a third time, and the sharpest instance of it: a bundle that did not carry
> these would replay a cluster with NO TRANSACTIONS against a schedule recorded on one running the
> bank, and every A6 finding in `seeds/` would reproduce as a clean run.*

A7 added two options to the shape and none to the thing that pins the shape.

> **The failure has one signature every time: an option is added to the SHAPE and not to the thing
> that PINS the shape, and the corpus goes on being green about a cluster that is no longer the one
> under test.**

**This is the fourth occurrence, and the third time the reasoning was written down before it
happened.** BUG-027 is the same shape in `raft/` — `sendGatedOn`'s comment described the defect one
phase before it occurred. **A comment that predicts a defect and does not prevent it is a comment
doing the wrong job**, and this struct now has four such comments. The remedy that worked for BUG-027
was to invert the default so the compiler asks the question; the equivalent here is owed and is
recorded in CARRY-FORWARD rather than improvised at the end of a phase.

**What it invalidates, stated plainly.** Nothing in `seeds/` has ever exercised the read-index path,
including the twenty-four bundles re-recorded during this phase. `make corpus`'s green is honest
about what it checked and silent about what it could not: **a bundle records the shape its writer
knows how to write down.**

**The fix.** `ReadIndex` and `FollowerReadPerMille` join `RaftMeta`, the two `simctl run` flags, and
the replay path that reconstructs options from the bundle. Verified by round trip: a bundle recorded
at seed 155 carries `read_index: true, follower_read_per_mille: 333`, and BUG-009's re-pin — which is
measured under A7's shape and would have replayed A6-shaped — reproduces its finding.

### BUG-035 — (harness) a timeout that had not fired, on a clock that had not advanced

| | |
|---|---|
| **Symptom** | `make mutants`' baseline binary carried `-test.timeout=2h0m0s` and had been running **2h56m** without firing it. |
| **Found by** | reading it, and refusing to leave it as an unexplained observation. |
| **Reproduce** | any long lane on a Mac that is allowed to sleep: `ps -o etime=` and the Go test timeout disagree by exactly the time the machine spent asleep. |
| **Invariant that caught it** | none. This is a provenance question, and this project has spent six instances on those. |

**The two obvious explanations were both wrong**, and ruling them out is what made the third
findable:

- *reading the wrapper instead of the binary* — no: pid 40801 **is** `hunt.test`, started 13:37:05,
  carrying `-test.timeout=2h0m0s` on its own command line. That is the parent-versus-worker confusion
  the exit run produced (1.2 s of CPU over six hours), and it is not this;
- *elapsed starting before the binary* — no: the `go test` wrapper and the binary started one second
  apart.

**The actual cause.** `ps etime` is **wall clock** and advances while the machine sleeps. Go's test
timeout is a `time.AfterFunc`, backed by a **monotonic** clock, which on Darwin **stops** during
sleep. The machine had been entering *Maintenance Sleep* repeatedly:

```
total sleep since the binary started: 3,720 s = 62.0 minutes
2h56m wall  -  62m asleep  =  1h54m monotonic  <  2h timeout
```

The timeout had not fired because, on its own clock, it was not yet due.

> **A wall-clock elapsed and a timeout are measured on different clocks, and system sleep is the gap
> between them.** "The timeout has not fired" is not evidence that the timeout is broken — until you
> have checked which clock each side is counting on.

**Why this one matters more than its size**, and it is not a hypothetical: **a timeout that does not
fire when you expect it to is indistinguishable from one that will never fire, and the mutant lane's
entire defence against a hang is that timeout.** Track B lost eleven and a half hours to exactly
that. Being unable to tell those apart is the whole problem.

**The discriminator, and it was already available.** Read **CPU time**, not wall elapsed. CPU advances
only while the process runs, so a hung process shows flat CPU against climbing wall, and a sleeping
machine shows both moving slowly together. Measured here, live:

```
cpu at T      152:30.17
cpu at T+25s  153:03.71     -> 33 s of CPU in 25 s of wall: running, and multithreaded
```

### The general form, which is worth more than the timeout

**This project decided the wall/monotonic distinction in A0.4 and made it uncompilable.** `clock.Wall`
and `clock.Mono` are separate defined types; `Mono` has **no encoder and a poison `MarshalJSON`**
whose only job is to turn a silent success into a loud failure —

> *"a monotonic reading must never be serialized; its epoch is this boot of this node, so the value
> means nothing anywhere else"*

— and `determinismcheck` rejects a `clock.Mono` in any exported or tagged struct field outside the
package. Three mechanisms, one distinction, enforced by the compiler and a vet pass.

**And then the same distinction arrived in how the harness reads its own processes, where nothing
enforces it, and nobody recognised it.** `ps etime` is a Wall reading. A Go test timeout is a Mono
deadline. Comparing them is exactly the mistake A0.4 made unrepresentable inside `clock` — and it cost
an unexplained observation and two wrong hypotheses before anyone said the word "monotonic".

> **A distinction made uncompilable in one layer is not thereby known in another.** The type system
> defends the code that uses the types. It defends nothing about the shell command an engineer runs to
> ask whether the code is still alive, and that is where the same confusion went on living.

### The chain this belongs to, which is one rule and not three

Each was added after something misled somebody, and the shape is identical every time — **read the
thing, not the proxy for the thing**:

| what misled | the proxy that was read | the thing itself |
|---|---|---|
| an exit run believed to be running for two hours, which had refused at launch | the launch | **the process** |
| three mutant floors measured against a tree that still had an earlier mutant applied (BUG-033) | the revert that was supposed to have happened | **the tree** |
| a 2h timeout that had not fired at 2h56m (this entry) | the wall clock | **the CPU clock** |

**And the discriminator was available the whole time, in every one of them.** `ps` was there before the
first. `git diff` was there before the second. `ps -o time=` was there before the third.

> **The failure is not missing instrumentation. It is reading the number that was easier to reach.**
> `etime` is the first column anybody looks at; `time` is one flag away and answers a different
> question. Every entry in this chain is a case where the right number was one flag, one command, or
> one `grep` away, and the wrong one was already on the screen.

### BUG-036 — (harness) four tests that skipped when their precondition was unmet

| | |
|---|---|
| **Symptom** | Four tests reported success for runs in which they checked nothing, whenever the data they needed did not turn up. |
| **Found by** | Ansh, reading `sim/plan/plan_test.go:268`. The other three were found by grepping for the same shape. |
| **Reproduce** | make each skip's condition true; the test passes without executing its assertion. |
| **Invariant that caught it** | none. A skip is a success in every reporting mechanism this project has. |

**The class.**

> **A test that skips when its precondition is unmet reports success for a run that checked nothing —
> and the skip is LATENT until the data changes, at which point nothing announces it.**

That second clause is the dangerous half. A skip is not wrong on the day it is written; it is a green
that has quietly acquired a dependency on the workload, and the workload in this project moves. A7's
term-start no-op moved every trace at once. Nothing prints when a test stops asserting.

**The four:**

| | skipped when |
|---|---|
| `sim/plan/plan_test.go` | its seed produced no node-0 fault to delete |
| `cmd/simctl/corpus_test.go` | there was no bundle to damage |
| `cmd/simctl/freshprocess_test.go` | its plan produced no fault entry to perturb |
| `raft/readindex_test.go` | the term's no-op had already committed |

**`TestCorpusLaneDetectsRot` is the exemplar**, because its own purpose names the failure:

> **A rot detector that skips when there is nothing to damage is the thing it was built to catch.**

It exists because a lane over a corpus that currently reproduces cannot tell *"every bundle replays"*
from *"replay always says yes"*. With no bundle to damage it cannot tell that either, and it was
reporting success.

**The fixes are two shapes, and which one applies is a real distinction.** Where the precondition can
be *arranged*, arrange it and assert the arrangement: `readindex_test.go` never feeds an append
response, so the no-op cannot commit and the window exists by construction — if it committed anyway
the arrangement broke, and that is now a failure. Where the precondition depends on generated data,
**search for it and fail on exhaustion**: `plan_test.go` now scans seeds for one carrying a node-0
fault, bounded at 500, and `t.Fatal`s if none is found. Same rule the corpus lane already uses: a
search that finds nothing is a finding, not a pass.

**The honest measurement, taken before changing anything.** Seed 31337 carried 4 fault entries, **2 of
them on node 0**, so `plan_test.go`'s skip was **latent rather than active** — the test was running.
That is the more useful version of the finding: a green that *would* have come to depend on data,
caught before it did.

**And how the other three were found is the point.** The Track A wrap-up had just recorded, as a named
general form, that *a rule written about one instance does not generalise itself to its siblings* —
after a handoff carried a section headed "state it, do not assume it" about one lane while the two
beside it went unrun. Applying that rule to a ruling made about one test found three more the same
day. **That is the rule paying out on the day it was written down**, which is the strongest evidence
available that it was worth writing.

### BUG-037 — (harness) the determinism pass accepted a flag it did not forward

| | |
|---|---|
| **Symptom** | `determinismcheck -tags <tag> ./pkg/` produced output **byte-identical** to the run without the flag. Every package whose files carry a build tag was reported clean, having never been loaded. |
| **Found by** | classifying Track B's `engine/riftcgo` against Track A's lane set before a merge, rather than after. |
| **Reproduce** | a one-file package with `//go:build sometag` and an `os` import, inside core scope. |
| **Invariant that caught it** | none. The lane's output for an unloaded package and for a clean package are the same output. |

**The defect is in the pass, not in `riftcgo`.** `singlechecker.Main` registers `-tags` and does not
forward it to `packages.Load`, so the loader uses default tags, finds no files, and reports
`build constraints exclude all Go files` — which the driver treats as an error in the package and
skips. Measured on a probe with a deliberate violation:

```
default tags            build constraints exclude all Go files   ->  no finding
-tags=probetag          build constraints exclude all Go files   ->  no finding   (flag accepted, inert)
GOFLAGS=-tags=probetag  the os import is reported
```

> **An analyzer that accepts a flag it does not forward reports clean because nothing was loaded, and
> that is indistinguishable from clean.** The flag is worse than its absence: it looks like the
> question was asked.

**It generalises past `riftcgo`, which is why it is a defect and not a quirk.** Any future
build-tagged package inherits it on the day it lands, silently, and the lane that would have caught
its determinism violations is the lane that cannot see it.

**The fix is two tests rather than a flag**, because the flag was never the problem:

- **`TestTagForwardingActuallyReachesTheLoader`** asserts the premise first — a tagged package loads
  **zero** files by default — and then that `BuildFlags` reaches the loader. §8.1b's two numbers
  applied to a test's own setup: the zero is what makes the non-zero mean anything.
- **`TestEveryBuildTaggedPackageIsAnalysedOrNamed`** walks the tree for build tags, loads each tagged
  package with its tag, and requires every one to either analyse or appear **by exact import path** in
  a `notAnalysed` map with its reason.

**The same rule as `TestHatchRegistry`, one level up.** There, an exemption must appear in a
checked-in list rather than merely existing. Here, a package must be analysed or *named* as not
analysed. **Absent is not clean, and silence is not allowed to be the mechanism either way.**


---
---

### BUG-038 — (harness) a mutant was disarmed by an edit that changed no behaviour

| | |
|---|---|
| **Symptom** | `M77-a-snapshot-read-is-served-by-read-index` reported **ROT** — *patch no longer applies*. The line it mutates was byte-identical to the day it was written, and still is. |
| **Found by** | `make power-refute`, run for the first time at the A7/B5 merge. `A7-HANDOFF.md` §5.5 carried it **unrun**, and `make ci` includes it, so `make ci` had not completed since A7's fix commits landed. |
| **Reproduce** | `sh scripts/patch-rot-kind.sh --self-test` plants the shape. The historical hunk is pinned verbatim in `tools/anchorcheck`'s `TestTheRuleWouldHaveCaughtM77`. |
| **Invariant that caught it** | none existed. `make anchors` is the one that exists now. |
| **Mutant class** | new: `tools/anchorcheck`. There was no class for this, because the defect is in what a patch **matches on** rather than in any line of shipped code. |

**What actually happened.** `M77`'s hunk anchored on three comment lines:

```
 // D-A7-4, ruled: BOTH paths stay for the phase. The replicated path is the
 // differential oracle's other half, and a differential between them is the
 // only instrument that can catch a stale read no client observed.
-if n.cfg.ReadIndex && req.Op == "get" && !req.ReadTS.IsSet() && req.Txn == nil {
```

A7 rewrote that comment. **The mutated line and all three trailing context lines are byte-identical
in the tree today**; the prose above them is not, and the mutant stopped applying.

**THE IRONY IS EXACT AND IS RECORDED STRAIGHT.** The comment that disarmed it is the Ruling-4 text
Ansh asked for during A7 — *"a path kept for what it MEASURES rather than for what it SERVES looks
dead to anyone counting callers"* — written into `store/node.go` so a future reader would not delete
the replicated read path as unused. **Sharpening the argument disarmed the instrument that guards the
argument's subject.** Nothing was wrong with the edit. Nothing was wrong with the code. The lane that
would have said so costs hours and had not been run.

> **A MUTANT ANCHORED ON COMMENT LINES IS DISARMED BY PROSE EDITS THAT CHANGE NO BEHAVIOUR.**

**And A7 learned the sibling of this one axis over.** `M80`'s lesson was that a patch must not
**mutate** comment lines, because coverage never marks them and `mutant-covered` could therefore never
answer for it. This is the same mistake on the **matching** side rather than the changing side.

> **A RULE ABOUT WHAT A PATCH MAY REPLACE DID NOT GENERALISE TO WHAT IT MAY ANCHOR ON.**

That is the wrap-up's own siblings rule paying out again: the fix for `M80` was written against the
instance, and its sibling sat one axis away for a month. See **GF-41**.

**The remedy is mechanical, because the cause is constant.** In a repository whose comments carry the
arguments, prose edits happen every day. `make anchors` reads every patch's context and fails when a
hunk anchors on prose. Static, milliseconds, and it would have fired the day the comment changed.

**The threshold is measured, not asserted, and the measurement is the argument.** The obvious rule —
*no comment in any context line* — flags **47 of 71** patches, because `patch(1)` has fuzz. So fuzz was
measured on the toolchain the lanes actually use (`patch 2.0-12u11-Apple`), by rewriting the prose of a
hunk's leading context end to end and asking whether it still applied:

| leading all-prose context lines | result |
|---|---|
| 1 | applies (fuzz absorbs) |
| 2 | applies (fuzz absorbs) |
| **3** | **FAILS — `M77` exactly** |
| any *interior* prose context line | FAILS at any length; fuzz trims edges only |

Under the measured rule the catalogue flagged **17 of 71**. (19 is the count for all-prose sides of
*any* length, and it was briefly written into this entry as if it were the rule's number; corrected by
re-deriving every figure from `HEAD~1`'s patches rather than copying it forward. `M70` and `M78` carry
two-line prose sides and are deliberately not flagged, which is what the fuzz table is for.) A
threshold picked to make a count small
would be a weakened checker; this one is picked by what the tool in the lanes does, and `fuzzReach` is
a named constant with the table behind it so re-measuring is the obvious move if the toolchain changes.

**One further measurement constrained the remedy.** `patch 2.0-12u11-Apple` **refuses a hunk with zero
leading context**, even a trivially correct one on a five-line file; `git apply` accepts the same hunk.
So "trim the prose block to nothing and let the other side carry the anchor" is unavailable here, and
the lane does not advise it. `M77` is re-anchored by **narrowing to `-U2`** instead — its prose runs
for **36 consecutive lines**, so widening would only have anchored on more prose.

**All 17 flagged patches were re-anchored with an equivalence proof, not by inspection.** Each old and
new patch was applied to clean copies and the results required to be byte-identical: re-anchoring
changes where a patch matches, never what it does. 15 widened (`-U5` to `-U19`), 1 narrowed (`M77`), 1
regenerated (`M79`). All 71 patches apply, and both re-pointed mutations compile.

**A defect in the lane itself, found because two implementations of one rule disagreed.** The first
parser did not end a hunk at a `--- a/` file header, so a multi-file patch's first file kept
accumulating the second file's lines — and `M45`'s trailing comment was reported as an **interior**
one, under the wrong file's extension. It was found only because the migration script and the Go lane
were written separately and compared. Fixed in both, and the interior count went from 1 to 0.

---

### BUG-039 — (harness) one diagnosis for two causes, and it was false for the one it was printed for

| | |
|---|---|
| **Symptom** | `power-refute` and `mutants.sh` printed, for every failed patch: *"patch no longer applies; the code moved and the mutation did not."* For `M77` the code had not moved. |
| **Found by** | reading the two ROT lines from the same run beside the tree, rather than acting on them. |
| **Reproduce** | `sh scripts/patch-rot-kind.sh --self-test` — one planted rot of each kind, required to be told apart. |
| **Invariant that caught it** | none. A wrong explanation and a right one are the same exit code. |
| **Mutant class** | `TestBothRotSitesTellTheTwoCausesApart` in `tools/anchorcheck` refuses a return to one message. |

**The same run produced both causes, which is what made the collapse visible.** `M77` was **anchor
drift**: mutation site intact, prose moved. `M79` was **structural drift**: `pendingRead` genuinely
gained a field — `issuedAt clock.Instant`, from `BUG-035` — between `key string` and the `anyReplica`
comment, so hunk 1 of 3 could not match. Same message, opposite situations.

**The remedies differ, which is why the message mattered.** Anchor drift means re-anchor and stop
thinking about it; nothing in the code is wrong. Structural drift means regenerate — and first ask
whether the class still has a site at all, since a mutant whose site vanished may have nothing left to
claim. **A reader acting on the printed sentence would have gone looking for a behavioural change to
`M77` that never happened.**

> **A VERDICT IS EVIDENCE ABOUT A THING ONLY IF IT DESCRIBES WHAT HAPPENED TO THAT THING.** This
> project applies that to planted defects — a kill counts only if the verdict names the defect
> planted. It applies identically to a lane explaining its own failure.

**The discriminator is cheap.** Anchor drift iff every **non-comment** line the hunk matched on is
still present in the file **and** at least one comment context line is not: *the code is all still
there and the prose is not*. Anything else is structural, and structural is the default, because it is
the answer that makes someone look at the code.

---

### BUG-040 — (harness) two lanes were green over a package neither had ever opened, and each track reached the false claim by a different route

| | |
|---|---|
| **Symptom** | `make determinism` exited **0** having never loaded `engine/riftcgo`. Separately, `tools/determinismcheck`'s `notAnalysed` map named that package with a reason that was false, in an entry nothing ever consulted. |
| **Found by** | running Track A's full lane set against the merged tree, then reading each artifact's claim against a measurement instead of against the other artifact. |
| **Reproduce** | `determinismcheck -tags rift_cgo ./engine/riftcgo/` → `build constraints exclude all Go files`. Over `./...` → silence. `go build -tags rift_cgo ./engine/riftcgo/` with no archive → succeeds. |
| **Invariant that caught it** | none. Both failures produce the same silence, which is `GF-39`'s own sentence. |
| **Mutant class** | `TestAStaleNotAnalysedEntryIsRejected`, plus the end-to-end induction that restores the deleted entry and requires the lane to fail. |

**Track B's half.** `Makefile`'s `determinism` lane read `determinismcheck -tags rift_cgo ./...` under
a comment asserting *"the cgo engine is LOADED AND ANALYZED rather than vanishing from `./...`"*. It
vanished. `singlechecker.Main` accepts `-tags` and drops it, which is `BUG-037` — whose **fix was two
tests**, and this lane still carried the flag that does nothing. Measured:

```
determinismcheck -tags rift_cgo ./engine/riftcgo/   build constraints exclude all Go files
determinismcheck -tags rift_cgo ./...               SILENT: exit 0, zero mentions of riftcgo
GOFLAGS=-tags=rift_cgo determinismcheck ./...       loads clean
```

The `./...` form is the dangerous one: a tagged package is not in the list at all, so there is no error
to notice. The comment's **last** clause was true — `registry_test.go` passes the tag through
`packages.Config.BuildFlags`, which does reach the loader — so the **test** had been analysing the
package all along while the **lane** never had.

**Track A's half, pointing the opposite way.** `notAnalysed` named `riftcgo` because it *"cannot
type-check without the C++ static archive and rift.h."* It builds without one, measured with no archive
anywhere in the tree — and `registry_test.go`, fifty lines away, had been proving that on every run.

**And the entry was UNREACHABLE, which is why its falsity was undetectable.** The loop `continue`s on
`len(p.Syntax) > 0` — loaded and analysable — before it ever consults the map. Induced: the test passes
with no `not analysed, by name` line in `-v` output and no mention of `riftcgo` at all.

**`CARRY-FORWARD.md` claimed the rejection already existed:** *"will stop accepting the `notAnalysed`
entry the moment the package becomes loadable — because a named exemption is only honest while the
reason holds."* There was none. That is `blind-unused-hatch`'s rule one level up, in a file that had no
such rule. It has one now, and it is what makes the map safe to be **empty**: an empty list with a live
rejection is a claim; an empty list with nothing checking it is only an absence.

**This is `GF-39`'s second concrete instance, and it is a worse shape than the first.**

> **TWO TRACKS ARRIVED AT FALSE CLAIMS ABOUT THE SAME PACKAGE BY DIFFERENT ROUTES, AND NEITHER WAS
> DETECTABLE FROM ITS OWN SIDE.** Track B claimed the lane saw a package it could not see. Track A
> claimed the package could not be seen at all. Each track's claim was checkable only with the other
> track's artifact in the same tree, which is precisely the state that did not exist until the merge.

The first instance was an auto-merge pinning a table true on neither branch. This one needed no merge
mechanics at all: both claims were false on their own branches, and there was no vantage point from
which to notice.

**Fixed:** `GOFLAGS=-tags=rift_cgo` in the lane (measured to reach the loader), the `notAnalysed` entry
**deleted** rather than corrected, and the rejection added and induced both ways.

**Still open by design:** `engine/riftcgo`'s scope stays **provisional** in `scope.go`. The lane now
loads the package; the package is excluded; so the pass still applies no rule to it. What settles that
is I1, with the archive built — and the measurement above changes only the cost, not the schedule: the
pass never needed the archive, so the obstacle that entry named was never there.

---

### BUG-041 — (harness) a corpus lane whose premise was true when it was written

| | |
|---|---|
| **Symptom** | `make test`, `make race` and `make corpus` red on the merged tree. `differential is a directory in seeds/ with no meta.json`. All 24 Track A bundles replayed. |
| **Found by** | Track A's full lane set on the merged tree — the moment `GF-39` names. |
| **Reproduce** | `TestTheCorpusRegistryIsInduced` fires all four arms against a synthetic tree. |
| **Invariant that caught it** | the corpus lane itself, correctly: an unclassified directory under `seeds/` **is** a finding. |
| **Mutant class** | the four induction arms, plus `TestAnUnregisteredDirectoryIsAnError`. |

`corpus()` treated every directory under `seeds/` as a Track A bundle. That was true the day it was
written and stopped being true when `seeds/differential/`'s 22 format entries arrived with the merge.

**The finding is not the red. It is that three lanes walk `seeds/` and disagreed about what a
non-bundle directory means:**

| lane | what it did with `seeds/differential/` |
|---|---|
| `cmd/simctl/corpus_test.go` | **errored** — the only right answer |
| `scripts/corpus-reproduces.sh` | **skipped it silently** — see `BUG-042` |
| `scripts/bundle-seeds.sh` | never looked; it iterates `seeds/BUG-*/` |

**The cheap fix is the wrong one.** `if name == "differential" { continue }` is a hole with a comment
on it. What landed is a registry: a directory declares its **kind** and its **owner**, an unregistered
one is an error, and **the registry is asserted against the tree so a stale entry fails**. That last
clause is here because of `BUG-040`, found the same day: an exemption list with no rot check is how a
permission nobody granted stays granted. A registered directory must also be **non-empty** — a registry
entry is permission to be a different shape, not permission to be nothing.

**THE OBLIGATION PREDICTED A MERGE ITEM HERE AND PREDICTED IT BACKWARDS.** `CARRY-FORWARD.md`, written
while classifying Track B's artifacts:

> *"`engine/differential`'s tests read a fixture corpus from `seeds/differential/format` (22 entries).
> The package and the corpus have to move together or its tests fail on a missing directory, which is a
> merge-completeness item rather than a defect."*

They moved together. That failure did not occur. `engine/differential` passes in `make test`. **The lane
that went red was Track A's corpus lane, policing the directory the fixtures landed in** — which the
obligation could not see, because it was written from the package's side.

> **AN OBLIGATION WRITTEN FROM ONE VANTAGE PREDICTS THE FAILURES VISIBLE FROM THAT VANTAGE.** The
> author asked "what does this package need?" and answered correctly. Nobody asked "what already owns
> the place this is going?"

---

### BUG-042 — (harness) three of four drop paths counted what they dropped

| | |
|---|---|
| **Symptom** | `scripts/corpus-reproduces.sh` printed *"20 bundles checked, 4 skipped"* against a `seeds/` holding **25** directories. The 25th was not checked, not skipped, not printed, and not in any total. |
| **Found by** | reading the script while its known-red run was in flight, then confirming against its own output. |
| **Reproduce** | remove `notbundle=$((notbundle + 1))` and run the lane: *"1 of 25 directories are unaccounted for."* |
| **Invariant that caught it** | none, and the counters existed precisely to be that invariant. |
| **Mutant class** | the totals reconciliation itself: `checked + skipped + notbundle == dirs`, or exit 2. |

The loop leaves early in four places. Three incremented a counter and printed a line. The fourth —
`[ -f "$d/meta.json" ] || continue` — did neither.

**It cost nothing until the merge put a directory on that path**, and then the two numbers still summed
to 24, because they had summed to the population back when 24 **was** the population.

> **A COUNT TAKEN WHEN IT HAPPENED TO EQUAL THE POPULATION READS AS A POPULATION FOREVER AFTER.**

That is the found-by table's shape in a lane — a summary that does not add up to its own population, in
a project whose entire argument is that verification must not be vacuous.

**And the framing correction is part of the finding.** `corpus-reproduces` was red on this run, and it
would have been wrong to report that as a discovery: `CARRY-FORWARD.md` already recorded *"20 checked,
4 skipped, 1 failure, and the failure is `seeds/BUG-015`"*, ending *"Track A does not exit while
`corpus-reproduces` is red."* The run **reproduced a recorded red**. What was new is that the same
numbers now mean something different, and that is measured rather than read out of the code.

**Fixed:** the fourth path counts and names what it drops, the header line reports the population
(`25 directories in seeds/: …`), and the totals must reconcile or the lane exits 2 — so a fifth drop
path added later cannot be as silent as the fourth was.

**Its induction is the weakest of the five landed this day, and that is said rather than hidden.** The
reconciliation was induced by hand — counter removed, lane printed *"1 of 25 directories are
unaccounted for"* — and the standing instrument is a **source pin**
(`TestTheReproducesLaneAccountsForEveryDirectory`), not a re-run: the lane copies the whole tree once
per bundle, which is not affordable in a push lane. The pin catches **deletion** of the arithmetic and
would not catch a **wrong sum**. Every other fix landed today carries an instrument that re-executes
the thing it guards; this one does not, and a weaker instrument described as a strong one is this
repository's own worst failure mode.

---
### BUG-043 — (harness) two writers on one log, and the reader failed silently rather than the writer

| | |
|---|---|
| **Symptom** | `grep -n "=== LANE" lanes3.log` returned **nothing** while `tail` on the same file printed the lines. Every later read of that file was of a log two processes were writing at independent offsets. |
| **Found by** | Ansh asking *"is it even running?"* — which sent me to `ps` instead of to the log, and `ps` is where the second job was. |
| **Reproduce** | open a file with `>` in one shell, truncate it with `>` in another, write from both: the hole between the offsets is NUL, and BSD/GNU `grep` classifies the file as binary and suppresses output. |
| **Invariant that caught it** | none. `grep` exits 0 finding nothing, which is the same exit and the same silence as a file that genuinely does not contain the pattern. |
| **Mutant class** | none exists and none is added: the defect is in how a run was *observed*, not in the tree. What lands instead is the practice change below. |

**What was actually running.** An orphaned job from an earlier session — `{ make mutant-covered; make
power-mutants; } > lanes3.log` — had been alive for **10 hours 7 minutes**, unattached to any session,
holding `lanes3.log` open. A later lane run truncated the same path and wrote from offset 0. The orphan
then wrote at its own large offset, punching a **1,369-byte NUL hole** between them. Every other log in
the same directory had zero NULs, so the file was not the tool's fault and not the disk's; it was two
writers.

> **A LOG WITH TWO WRITERS DOES NOT FAIL IN THE WRITER. IT FAILS IN THE READER, AND IT FAILS THE ONE
> WAY A READER CANNOT DETECT: BY RETURNING NOTHING, SUCCESSFULLY.**

**The signal was there and was dismissed.** The `grep`-returns-nothing anomaly was hit, noticed, called
*"that's odd"*, and stepped past — twice — because `tail` still worked and the results still looked
sane. A checking tool that returns *no matches* and a checking tool that has silently given up produce
the identical observation, which is this repository's oldest theme arriving in the instrument used to
read the repository.

**And it violated a standing rule of this project, which is the part worth keeping.** The rule is *read
run state from the process, never from a watcher or a launch*. Six lanes were reported from a file
rather than from the thing that produced it. **Answering "is it even running?" with `ps` took one
command and found both the answer and the defect; the log could not have found either.**

**What was and was not damaged, checked rather than assumed.** Every reported result was
re-established from its own evidence before this entry was written:

| claim | verdict |
|---|---|
| `build lint test race blind` all 0 | **stands** — lines 3–124, contiguous from offset 0, written before the orphan's next write landed |
| `power-refute`: 3 confirmed, 0 refuted, 0 failures | **stands** — the orphan's command never invokes `power-refute`; only this run could have produced it |
| the `power-mutants` floor lines glimpsed scrolling past | **ambiguous between the two jobs** — and nothing was reported from them |
| `lanes4.log`, `cppci.log`, `mc.log`, `mc1.log` | **stand** — separate paths, single writer, **0 NUL bytes each**, verified |

Nothing reported was wrong. **That is luck, not method**, and the entry says so rather than closing on
the happy number.

**AND ONE OF THIS WEEK'S INSTRUMENTS PAID FOR ITSELF, which is worth stating plainly because
instrument-building can become self-perpetuating.** At I1, `BUG-013` came back `ROT` and the question
was which of three causes it was — the C++ engine, the wiring, or a bundle whose finding was
model-specific. `scripts/patch-rot-kind.sh`, built two days earlier for `BUG-039`, answered
**STRUCTURAL** in one command, and a check against `8a95e01^` confirmed it: the patch applies before
the I1 wiring and not after. **That is a question that would otherwise have been guessed at**, and the
guess would have been "the engine", because the engine is what changed most conspicuously that day.

The cause was the wiring, and specifically mine: `n.db.Apply` became `n.apply` when the stack moved
onto `store.Engine`. One command, one right answer, on the first occasion the instrument was needed for
something other than its own induction.

**Its sibling is `BUG-033`, and the pair is the point.** There, a killed measurement driver left a
mutant applied and three later floors were measured against a tree nobody had checked. Here, a killed
session left a *job* running and six lanes were read from a file nobody had checked.

> **THE INSTRUMENT USED TO OBSERVE A RUN IS PART OF THE RUN, AND IT IS THE PART NOBODY INSTRUMENTS.**

**Fixed, in practice rather than in code.** A lane log is named per invocation (`$$` in the path) so
two runs cannot share one; `ps` is the first check when a run's progress is in question, not the log;
and an orphan check runs before starting long work, because a background job that outlives its session
is invisible to everything except `ps` and competes for the machine the measurements are taken on. The
orphan was consuming a full core against `power-mutants` while a chunked `mutant-covered` ran beside
it — which is most of why the chunk looked stalled.

**Its own output was not worth saving, and that was established before it was killed.** The truncation
had already destroyed its first six hours irrecoverably; what survived was partial, interleaved, and
stamped by the lane itself as `tree DIRTY at start: verdicts are against uncommitted state`. A
NUL-stripped copy is kept as `lanes3.salvaged.log` for the record and is not a result.

---
### BUG-044 — (harness) a snapshot taken after an operation that deliberately does not touch the disk

| | |
|---|---|
| **Symptom** | `TestACrashRollsBackToTheHarnessDurablePoint` failed with `key not found` **on the write it was asserting survived**. The rollback lost data that was below the harness's own durable point. |
| **Found by** | the test written in the same commit as the mechanism, run before anything was built on it. |
| **Reproduce** | snapshot a `riftcgo` directory immediately after `Apply` with no intervening `Sync`, crash, restore: the write is gone. |
| **Invariant that caught it** | the directed test's own premise check, which had already confirmed the engine watermark was past the harness's durable point before it asserted anything about recovery. |
| **Mutant class** | none added, and the reason is stated below: the class is *"the harness assumed a guarantee the contract explicitly withholds"*, which is not a line to mutate but a contract to re-read. |

**What happened.** DESIGN-I1 D2(b) says a crash rolls the directory back to the last state the harness
considers durable, so `simcgo.Apply` snapshotted the directory at every sequence `Apply` returned. But
`Apply` **does not block on I/O** — `engine.Engine`'s own words: *"It never blocks on I/O… `sync=false`
leaves it buffered."* At snapshot time the bytes were still in the WAL buffer and the directory did not
contain them.

> **A SNAPSHOT IS A CLAIM ABOUT WHAT IS ON DISK. TAKING IT AFTER AN OPERATION THAT DELIBERATELY DOES
> NOT TOUCH THE DISK IS A CLAIM ABOUT NOTHING.**

**Why this is more than a bug, and it is the entry's point.** Nothing was wrong with the engine. The
non-blocking `Apply` is **correct and load-bearing** — it is what lets the simulator model an unsynced
window at all, and removing it would remove the fault this phase exists to inject. The contract states
it in the interface doc, unambiguously, in the sentence directly above the method.

> **A GUARANTEE THE ENGINE PROVIDES CAN BE A TRAP FOR THE HARNESS BUILT AGAINST IT — and the harness
> author is the one holding both halves.** The engine promises *"visible now, durable later"*; the
> harness needed *"on disk now"* and read the first as the second. Neither side is wrong on its own
> terms. The mistake exists only in the joint, and only one person is standing in it.

**And the defect is what made the measurement honest, which is the part that would have been lost.**
Had the test not caught it, B would have been benchmarked as *a directory copy* rather than as *an
fsync plus a directory copy*:

| what B would have measured | what B costs |
|---|---|
| copy only | ~2 ms per Apply |
| **fsync + copy** | **4.803 ms per Apply, 24×** |

The cheap version would have come in at roughly half the cost of the real one, on a decision made by
comparing that cost against ~4,000 Applies per seed. **A wrong number that points the same way is
still a wrong number, and this one pointed toward "affordable."** The correctness test decided the
affordability question, which is not the job it was written for.

**Fixed:** `simcgo.Apply` syncs before it snapshots, and the reason is recorded at the method rather
than in the commit message, because the next person to write a harness against a non-blocking contract
will reach for the same shortcut.

---
### BUG-045 — (harness) a rule written down in the repository is not a rule you have applied

| | |
|---|---|
| **Symptom** | Three defects in one afternoon's wiring, each caught immediately by a mechanism already in the tree, and **three of the four things gone wrong that day were written down in files the author had been reading.** |
| **Found by** | a panic from `store/node.go`'s restart guard; the `determinism` pass; and the wrapper's own first test run. |
| **Reproduce** | each is induced in place — see the three rows below. |
| **Invariant that caught it** | one runtime guard, one static pass, one directed test. **None of the three was looking for what it found.** |
| **Mutant class** | none added. The defect class is not a line; it is the distance between knowing a rule and having it reach the moment it applies. |

| # | the defect | the rule, and where it was already written |
|---|---|---|
| 1 | `visibleSeq` put on `Replica`, which shares one engine across many replicas — **one fact in N places** | `BUG-032`, invoked by Ansh in the ruling **ten lines above the code that broke it**: *"the store asking the engine to remember a number the store was handed is the one-fact-two-places shape, and BUG-032 cost Track A three cycles."* |
| 2 | `tracked.Crash()` did not move the tracked sequence | the model's own `Crash()` moves it, in the file the wrapper wraps |
| 3 | the source pin written in `store/`, which is core scope, importing `os` | `tools/gatepin`'s package comment, **read the same day**: *"It lives under tools/ rather than in raft/ because it reads the source text, and raft/ is in the determinism pass's core scope where importing os is a build failure."* |

> **A RULE WRITTEN DOWN IN THE REPOSITORY IS NOT A RULE YOU HAVE APPLIED.**

**And it should not be filed as carelessness, because that filing has no remedy.** Each rule was
known, recently, by the person who broke it — one of them had *just been quoted in the ruling being
implemented*. What failed was not memory of the rule but its arrival at the moment it applied.

#### The fourth instance, and the interval is the datum

At I1, a shell one-liner was written to check a build:

```sh
go vet -tags rift_cgo ./sim/hunt/ 2>&1 | grep -v "^ld:" | head -2; echo "vet clean"
```

**A `;`, not an `&&`.** *"vet clean"* printed over a package that did not compile, and two sweep chunks
were launched against it and died in seconds.

That is the same defect as the `DETERMINISTIC ACROSS PROCESSES ON ONE BUILD` line printed over three
failed subtests — **and it was committed two hours after that one was recorded as a general form and a
guard was written for it.**

| instance | interval from recording the rule |
|---|---|
| `BUG-032`'s shape reintroduced in `store` | ten lines below the ruling quoting it |
| `gatepin`'s comment ignored, pin written in core scope | same day |
| the unguarded `DETERMINISTIC` summary | — (this is the one recorded) |
| **the `; echo "vet clean"` line** | **~2 hours** |

> **THE INTERVAL IS THE DATUM.** A rule forgotten after a month is a memory problem and has obvious
> remedies. A rule broken two hours after being written down, by the person who wrote it, in the same
> session, is not — and it is the whole argument for mechanisms over discipline, which now has four
> instances behind it in one day.

> **THIS IS THE WHOLE ARGUMENT FOR MECHANISMS OVER DISCIPLINE, AND IT NOW HAS FOUR INSTANCES BEHIND IT
> IN ONE DAY.** Discipline is a claim about what someone will remember while thinking about something
> else. Every one of these was caught in seconds by something that was not remembering anything.

#### The guard, and what separates an invariant from a test

Defect 1 was caught by `store/node.go`'s restart guard:

```
panic: store: node 1 read the engine back with sequence 107 visible
       and only 106 durable
```

That guard was written for `BUG-005` — a follower acknowledging an index it did not have on disk. It
has nothing to do with interface refactors, engine wrappers, or shared ownership. It fired on the
first run anyway, and named the exact quantity that had drifted.

> **AN ASSERTION THAT ONLY EVER CATCHES THE THING IT WAS WRITTEN FOR IS A TEST. ONE THAT CATCHES
> SOMETHING ELSE IS AN INVARIANT.** The difference is not strength; it is whether the property is
> stated about the *system's state* or about the *scenario's outcome*. A guard on a state relation
> holds against defects nobody had imagined when it was written, which is the only kind of protection
> that survives a refactor.

#### Defect 2 is the subtler half

`tracked.Apply` recorded the sequence. `tracked.Crash()` did not reset it — so after a crash the
store believed a sequence the engine no longer had.

> **TRACKING A VALUE MEANS TRACKING EVERY TRANSITION OF IT, AND THE ONE THAT IS EASY TO MISS IS THE
> ONE THAT MOVES IT BACKWARDS.** Every transition, not every increment. The forward path is the one
> being written and is therefore the one in mind; the backward path belongs to a different operation
> in a different file, and is remembered only by someone asking *what else changes this?* rather than
> *what does this change?*

**Fixed:** the tracking lives with the engine it is a fact about (`store.tracked`), so replicas sharing
an engine share its tracking by construction; `Crash()` returns the value to what survived; and the pin
that enforces the single Apply path lives in `tools/enginepin`, where reading source text is allowed.

**Verified a no-op:** seed 7's trace hash is byte-identical to the run taken before any of this
existed — `3a0962294b837e3568ce22fcbf724e52750a71e2cb9b93169444ddca3ab0a07c`. The stash-based
comparison against the previous commit was *discarded rather than reported*, because an untracked file
survived the stash and compromised the control.

---
### BUG-046 — (harness) a byte-identical trace hash from an engine that was never opened

| | |
|---|---|
| **Symptom** | `simctl run --engine cgo` reported the raft workload complete in 24,622 steps with a trace hash **byte-identical to the model's**. The engine root contained **0 directories, 0 files, 0 bytes.** |
| **Found by** | listing the engine root instead of reading the hash — on Ansh's standing instruction to distrust a clean first result. |
| **Reproduce** | attach the engine factory to the plan-building options rather than to the run's; the plan drops it and the run uses the default. |
| **Invariant that caught it** | **none.** Every checker passed, every count was right, and the strongest available evidence pointed the wrong way. |
| **Mutant class** | the non-vacuity counter added with this entry: engine bytes written, asserted above zero on any run that named a non-default engine. |

**This entry sits above `BUG-B008`, the defect it was hiding**, because the hiding is worth more than
the defect. `BUG-B008` is a null pointer at a boundary. This is a result that would have been reported
as *"the C++ engine reproduces the model byte for byte on I1's first run."*

> **A MATCHING TRACE HASH IS THE MOST PERSUASIVE FORM A CLAIM ABOUT NOTHING CAN TAKE.** It is not a
> weak signal that happened to mislead. It is the strongest signal this project has, and it was
> perfectly true: the two runs *were* identical, because they were the same run on the same engine.

**What caught it was checking that the MECHANISM ran, not that the ANSWER was right.** That is `GF-25`
— *a gate on the mechanism and a test on the answer are two instruments, not one* — arriving at the top
of I1, in the most consequential place it has yet appeared. The answer was correct and told us nothing;
`ls` told us everything, in one command, and cost nothing.

#### The cause is a real design constraint, not a slip

`simctl run` builds a **plan**, and the plan is what drives the run.

> **A PLAN IS DATA. THAT IS WHAT MAKES IT A REPRODUCTION UNIT** — a bundle replays because everything
> the run needs was written down. A `func` field cannot be written down, so the engine factory was
> silently dropped at the plan boundary, and the run rebuilt its options from the plan with the
> default engine.

Nothing malfunctioned. The plan's data-only property is correct, is the reason bundles exist, and is
exactly what produced the trap for the person extending it. **Engine choice has to travel *beside* the
plan, never inside it**, and that is now stated at the line that attaches it.

**Same family as `BUG-044`** — the frozen contract's non-blocking `Apply`, correct and load-bearing,
making the naive harness wrong — and as `BUG-B008`, where null-means-unbounded is correct and
load-bearing for bounds and meaningless for a value.

> **THIRD INSTANCE THIS WEEK OF A CORRECT GUARANTEE BECOMING A DEFECT AT A BOUNDARY ITS AUTHOR DID NOT
> HAVE IN VIEW.** None of the three guarantees was wrong. Each is documented where it is defined. The
> defect is in the joint every time, and the person standing in the joint is the one extending the
> system rather than the one who wrote either half.

**Fixed:** the engine is attached at the run, and `simctl` now records `engine-bytes` on every run,
asserted above zero whenever a non-default engine was named. **A counter exists because there is now a
measured instance of what its absence looks like**, which is the only argument for a counter this
project accepts.

#### And the counter had this entry's own hole, twice, within an hour of being written

**First: it guarded one of three workloads.** `assertEngineWasUsed` was called inside `case
workloadRaft`. The toy and none paths skipped it — and those are exactly the paths where the engine is
never wired. `TOY-001` and `TOY-002` replayed with `--engine cgo`, used the model, and reported `MATCH`.

**Second: moved after the switch, it became unreachable.** Every case returns, so the code after the
switch never runs. The check silently stopped executing, and the only visible symptom was the footprint
line vanishing from output that had printed it minutes earlier.

> **THE MECHANISM BUILT TO CATCH "THE ENGINE WAS NEVER USED" WAS ITSELF NEVER USED — TWICE, WITHIN AN
> HOUR, IN TWO STRUCTURALLY DIFFERENT WAYS.** Once by covering a subset of paths, once by sitting where
> no path arrives. A check has two ways to be absent and they look nothing alike from the code.

**Both were found by reading what the mechanism DID — the per-bundle footprint, then its absence —
never the verdict.** Both times the verdict was `MATCH` and `exit=0`.

**The wrapper is the fix and the reason belongs at the code:** `execute` calls `executeInner` and then
asserts.

> **THERE IS NOWHERE FOR A RETURN TO GO AROUND IT.** A call placed *inside* control flow inherits every
> branch's ability to skip it. A wrapper has no branches.

**And the toy REFUSES rather than reporting a true, misleading number.** *"A non-default engine was
named and it wrote nothing"* would have been accurate and would have sent a reader hunting a defect
that does not exist. Instead: *"the `toy` workload runs on engine/model by construction and does not
honour `--engine`; it is A0's harness fixture rather than part of the stack I1 swaps."*

> **A FIXTURE THAT IS OUT OF SCOPE SHOULD SAY WHICH IT IS, NOT PRODUCE A TRUE NUMBER THAT IMPLIES A
> DEFECT.**

---
### BUG-047 — (harness) a crash replaced the engine and the durability callbacks went with it

| | |
|---|---|
| **Symptom** | After a simulated crash, `OnDurable` never fired again. A restarted node would never learn any write was durable. |
| **Found by** | **CF-6.2's directed check**, written at I1 because CF-6 insists incidental exposure is not closure. |
| **Reproduce** | `TestCF6_2_OnDurableReportsTheEnginesWatermarkNotARememberedOne`: register a callback, crash, apply, advance durability — the callback list is empty. |
| **Invariant that caught it** | none existed. The failure has no error, no panic and no wrong value. |
| **Mutant class** | remove the re-registration loop in `simcgo.Crash`; covered by CF-6.2's test, which asserts both that the callback fires again **and** that what it reports is not a pre-crash high-water mark. |

**The mechanism.** `simcgo.Crash()` closes the C++ DB, rolls the directory back, and calls
`riftcgo.Open` for a **new** `*riftcgo.DB`. Callbacks registered through `OnDurable` lived on the old
object and were not carried over.

> **A CRASH REPLACES THE ENGINE. ANYTHING REGISTERED ON THE OLD ONE IS GONE — AND WHAT IS GONE IS
> SILENT.** There is no error, no panic, and no missing value. There is a callback that stops being
> called, which is indistinguishable from a callback whose condition never recurs.

**Why the sweep could not have found it, and this is CF-6's own argument arriving as evidence.** I1's
raft workload crashes this wrapper thousands of times per run and **exercised this exact path on every
one of them.** The runs completed, the traces matched, the checkers passed. Exercising a path is not
checking what it did.

> CF-6, written at B5.4 before any of this existed: *"'it happens incidentally' is how a gap stays open
> while looking closed."* **It was open, it looked closed, and the thing that opened it was written
> two hours before the check that found it.**

**And the test found it pointing the other way from CF-6's wording.** CF-6.2 asks that the reported
watermark be *"the engine's and not a value the wrapper remembered across the crash"* — the failure
mode anticipated was **remembering too much**. What happened was the opposite: the wrapper remembered
*nothing*, because the object holding the memory was discarded. The check caught it anyway, because it
asserted the callback **fires at all** before asserting what it reports.

> **A CHECK THAT ASSERTS ONLY THE VALUE CANNOT DISTINGUISH A WRONG ANSWER FROM NO ANSWER.** CF-6.2's
> first assertion is that something happened; the second is what. Written the other way round, this
> defect would have read as a pass.

#### CF-6's vindication, stated plainly

I1 crashed this wrapper **thousands of times per run**. It exercised this exact path on every one of
them. **Every run completed, every trace matched the model's, every checker passed.**

> **A PHASE THAT CLOSED CF-6 INCIDENTALLY WOULD HAVE REPORTED IT CLOSED.** The corpus replayed, the
> sweep ran clean over 300 seeds, the fresh-process gate matched to the byte. Nothing in any of it
> would have said that durability notifications stop after a crash, because nothing in any of it asked.

CF-6, written at B5.4 before this wrapper existed, refused exactly that: *"'it happens incidentally' is
how a gap stays open while looking closed"*, and named **three checks that must actually be checked**.
Three directed tests are the only reason this was found.

**And the check caught it pointing the opposite way from CF-6's own wording.** CF-6.2 asks that the
reported watermark be *"the engine's and not a value the wrapper remembered across the crash"* — the
failure anticipated was **remembering too much**. What happened was the opposite: the wrapper
remembered *nothing*, because the object holding the memory had been replaced. The check caught it
because its first assertion is that the callback **fires at all**, before anything about what it
reports.

> **A CHECK THAT ASSERTS ONLY THE VALUE CANNOT DISTINGUISH A WRONG ANSWER FROM NO ANSWER.**

That ordering was not foresight — it was written that way because a test needs a premise before it has
a property. **The premise is what caught the defect the property was aimed at.**

**Fixed:** `simcgo` owns the callback list and re-registers it on the reopened engine.

---
### BUG-048 — (harness) Amendment A5's default was inverted in the pass, and unenforced from the day it was written

| | |
|---|---|
| **Symptom** | `scopeFor` returned `scopeOff` for any package matching no pattern. **Amendment A5 says "Unclassified packages default in."** Nineteen packages matched nothing, including `hlc`, `raftcheck` and `internal/provenance`. |
| **Found by** | adding `net/` at I2 — **the first new top-level package in months** — and distrusting a silent pass. |
| **Reproduce** | plant a map range in `hlc`: **0 findings**. Plant the same in `clock`: **1**. |
| **Invariant that caught it** | none. An unanalysed package and a clean one produce byte-identical output. |
| **Mutant class** | `TestEveryPackageIsClassifiedExplicitly`, induced by adding an unclassified package and watching the lane fail. |

**`defaultCore` was an allowlist of ten prefixes.** Anything outside them fell to the default, and the
default was OFF. So three core packages — the **hybrid logical clock**, the **oracle and ledger
package**, and the **witness types** — had never been analysed by the determinism pass at all.

#### `hlc` leads this entry, and it is the same shape as A6's sixteenth instance

**`hlc` is the mechanism this project's headline claims rest on** — snapshot isolation over hybrid
logical clocks, uncertainty intervals, the clock-skew envelope. It was outside the pass for the whole
of Track A.

| | the claim | what was not reaching it |
|---|---|---|
| **A6 §18**, the sixteenth instance | *"snapshot isolation over HLC under bounded skew"* | the sweep exercising it **injected no skew at all** |
| **`BUG-048`**, here | *"no `time.Now`, no map range, no float, in core scope"* | the **package implementing HLC** was not in core scope |

> **NEITHER WAS A DEFECT IN THE MECHANISM. BOTH WERE THE MECHANISM'S VERIFICATION NOT REACHING IT** —
> once by a workload that omitted the fault the phase was about, once by a scope list that omitted the
> package the amendment was about.

**AND IT IS A COVERAGE FINDING, NOT A CORRECTNESS ONE. Say it plainly, because a reader will otherwise
assume the worse thing:** when `hlc` was finally analysed it produced **one** finding, the
logical-overflow carry, which is necessary and correct and now carries a hatch. **No defect was found
in `hlc`.** What was wrong was that nobody could have known that.

> **A5'S LETTER HAS BEEN UNENFORCED SINCE THE DAY IT WAS WRITTEN**, and nothing in the tree
> contradicted it, because **no new top-level package was added after the pass existed.** Every package
> that would have exposed the inversion predates the check.

#### Why the test suite could not catch it, and it is `GF-51`'s sharpest instance

`TestScopeTable` pinned the default with one row: `{mod + "engine/wherever", scopeCore}` — a
**subpackage under an already-included prefix**. That is not the case A5's sentence is about.

> **THE PIN TESTED DEFAULT-IN FOR THE ONE SHAPE THAT WAS ALREADY COVERED BY A WILDCARD, AND NEVER FOR
> THE SHAPE THE RULE EXISTS FOR.** `engine/wherever` is core because `engine/...` matches it, not
> because of any default — **the row would pass with the default set to anything.**
>
> **A TEST THAT CANNOT DISTINGUISH THE TWO ANSWERS IS NOT TESTING THE QUESTION.**

**And the population is the point.** `GF-51` says a test covers the case that existed when it was
written. Here **every instance that predates the test IS the population the rule was written for**: a
rule about *new* packages, tested only against *old* ones, in a tree where nobody added a new one for
months. The first addition exposed it within an hour.

#### The reading error, recorded plainly

`go run determinismcheck ./net/` printed nothing. **I read that as clean.** It meant *never looked*.

That is `BUG-046` one layer up — a lane green over a package it never opened — with **the same tell
available and the same dismissal.** Four instances now, by the same reader:

| | the silence | what it meant |
|---|---|---|
| `BUG-046` | a trace hash matching with an empty engine root | the engine was never opened |
| `BUG-046` again | the anti-vacuity check, twice | the check never ran |
| `BUG-043` | `grep` returning nothing on a NUL-bearing log | the tool had given up |
| **`BUG-048`** | **the pass reporting no findings** | **the package was never analysed** |

> **THE PATTERN IS NOT THAT THE MECHANISM IS SUBTLE. IT IS THAT SILENCE IS READ AS A PASS BY DEFAULT,
> AND HAS TO BE ACTIVELY DISTRUSTED EVERY SINGLE TIME.** Each of the four had a one-command check
> available — `ls`, `-v`, `tr -d '\0'`, plant-a-violation — and in three of the four it was run only
> after something else prompted suspicion.

**And what the four have in common is structural rather than topical.** An empty result, an identical
hash, a check that did not run, a grep that returned nothing:

> **ALL FOUR ARE THE ABSENCE OF A SIGNAL, PRESENTED IN THE SAME SHAPE AS A GOOD RESULT.** Nothing
> about the output distinguishes "we looked and found nothing wrong" from "we did not look". They are
> the same bytes, the same exit code, and the same colour on a terminal — and the first reading is the
> one a reader arrives with, because it is the reading that is usually true.

#### Fixed in two parts, the mechanism first

**(c)** `TestEveryPackageIsClassifiedExplicitly`: every package in the module must match an explicit
pattern, so the default is unreachable. *An implicit answer is the problem, not the direction it
leans* — a silently-included package is a lane failing for a reason nobody chose, which is no better
than a silently-excluded one.

**(a)** The default is now `scopeCore`, and all nineteen are enumerated with their reasons.

**And (a) had a defect of its own, caught by an existing pin.** The first version returned `scopeCore`
unconditionally, making **`time`, `fmt` and every dependency core scope**. `TestScopeTable`'s row for
`"time"` — written for an unrelated reason — failed immediately.

> **"UNCLASSIFIED PACKAGES DEFAULT IN" IS ABOUT OUR PACKAGES.** A5 is a rule for code this project
> writes; the stdlib is not unclassified, it is somebody else's.

#### What the three newly-analysed packages reported

Three findings, and **none is a defect** — each is a legitimate case that now carries a named hatch
with its reason, which is what `HATCHES.txt` is for:

| | |
|---|---|
| `hlc/hlc.go:70` | the logical-overflow carry. `Logical` has saturated, so one wall nanosecond *is* the next representable timestamp |
| `raftcheck/oracles.go:1353` | a map **merge** — each source has unique keys, so the result is identical whatever order it visits |
| `raftcheck/oracles.go:1563` | building a **set** — membership is what is read, and a set is identical whatever order it was filled |

**That they are all benign is the honest outcome and not a disappointment.** The pass found what it was
supposed to find in code it had never seen; that the three turned out defensible is a fact about the
code, and the exemptions are now visible and reasoned instead of implicit and invisible.

---

### BUG-049 — (harness) the reality counters were green on traffic the harness generated itself

| | |
|---|---|
| **Symptom** | Every real-mode cluster this project has ever started ran with **Raft frozen**: `node.Driver` does not tick itself, in sim the *loop* owns the tick schedule, and `cmd/riftnode` posted no `KindTick`. No election timeout ever fired. |
| **Found by** | writing the client protocol and asking why no operation completed. |
| **Reproduce** | delete the ticker goroutine in `cmd/riftnode/main.go`; the cluster starts, connects, exchanges bytes, and elects nobody. |
| **Invariant that caught it** | none, and that is the entry. |
| **Mutant class** | the `LedTicks == 0` gate arm in `chaos.Run.Gate`, induced. |

**What hid it was a synthetic heartbeat.** `riftnode` sent a hand-made envelope to every peer every
50ms, for a stated and reasonable purpose: without traffic the reality counters cannot distinguish
"the network works" from "nobody said anything."

> **IT WORKED. The counters went green. They were measuring the HARNESS'S OWN TRAFFIC**, and they
> would have reported the same numbers if `store/` had been deleted.

**This is BUG-046's shape one level up.** BUG-046 was a test that measured nothing. This is a test
that measured something **real and irrelevant** — which is harder to see, because every number in it
is true.

> **A REALITY COUNTER FED BY A SOURCE THE SYSTEM UNDER TEST DOES NOT CONTROL IS NOT A REALITY
> COUNTER.** It is an instrument measuring the instrument.

**Fixed by deleting the synthetic traffic and posting real ticks.** The bytes the counters see are now
Raft's own heartbeats, so a frozen cluster reports zero. The immediate evidence: the same end-to-end
test that reported **1794 wire bytes** on synthetic traffic reported **330** on real traffic. The
larger number was the harness talking to itself.

**And a gate now stands where the counter could not.** `LedTicks == 0` fails the run: *a cluster that
never elected a leader observed nothing, and every checker over its history is green because nothing
happened.*

---

### BUG-050 — (harness) a cluster keyed in the wrong address space: node 2 reported `sent=0 dropped=36`

| | |
|---|---|
| **Symptom** | Nodes connected, exchanged bytes and elected nobody. Node 2 dropped **every** send; nodes 1 and 3 dropped exactly **half**. |
| **Found by** | the transport's own `sent`/`dropped` counters, before any code was read. |
| **Reproduce** | key `tcp.New`'s address map by the 1-based `--id` rather than by ordinal. |
| **Invariant that caught it** | none directly; the drop counter is what named it. |
| **Mutant class** | the arithmetic below is the reproduction, and `LedTicks == 0` is the gate. |

**`sim.NodeID` says what it is in its own doc comment:** *"an index into the run's node set, not an
address."* `store/` obeys that — every envelope carries `sim.NodeID(n.cfg.Ordinal)` and
`sim.NodeID(n.ordinalOf(m.To))` — and `riftnode` keyed its transport by the 1-based `--id`.

**The counters identified the bug before the code was read**, and the arithmetic is worth keeping:

| node | ordinals it addressed | in its 1-based map? | result |
|---|---|---|---|
| 1 | 1, 2 | 1 is *itself* (skipped), 2 present | `sent=18 dropped=18` |
| 2 | 0, 2 | 0 absent, 2 is *itself* (skipped) | `sent=0 dropped=36` |
| 3 | 0, 1 | 0 absent, 1 present | `sent=18 dropped=18` |

> **A COUNTER THAT SEPARATES "SENT" FROM "DROPPED" TURNS A SILENT MISCONFIGURATION INTO A DIAGNOSIS.**
> A single `messages` counter would have shown three healthy-looking nodes.

---

### BUG-051 — (harness) every Raft message the cluster ever exchanged was discarded at a type assertion

| | |
|---|---|
| **Symptom** | With addressing fixed, all three nodes campaigned every election timeout, each received the others' messages, and none ever saw one. `led=0` on every node across 182 ticks. |
| **Found by** | the `led`/`ticks` counters added while chasing BUG-050. |
| **Reproduce** | post `Payload: e` (a `sim.Envelope`) instead of `Payload: sim.Encode(e)` from the listener. |
| **Invariant that caught it** | none. The drop is a bare `return`. |
| **Mutant class** | `LedTicks == 0`, which fails the run. |

`store.Node`'s deliver arm reads `ev.Payload.([]byte)` and calls `sim.Decode` on it. Handed a
`sim.Envelope`, the assertion fails and **the arm returns silently**. `riftnode`'s listener — which
gets a parsed `sim.Envelope` from the transport, because the transport must parse in order to route —
posted the envelope.

> **A SILENT DROP ON AN UNEXPECTED SHAPE IS A DIAGNOSIS DELETED AT THE MOMENT IT WAS AVAILABLE.** The
> one place that knew the payload was wrong is the one place that said nothing.

This is the same family as BUG-042's uncounted drop paths and BUG-043's silent `grep`: **the absence
of a signal presented in the same shape as a good result.** It is now the fifth instance.

**Fixed by re-encoding at the listener.** One wire format read twice costs a memcpy; the alternative is
two representations on one mailbox, which is the same bug one layer up.

---

### BUG-052 — (harness) the leader panicked on its first answered operation: a return at instant zero

| | |
|---|---|
| **Symptom** | `panic: sim: operation returned at 0 before it was called at 3260430875`, in `History.End`, on the node that had just become leader. The supervisor reported `ExitedOther: 1`. |
| **Found by** | the supervisor's uninvited-exit counter, which is a gate arm. |
| **Reproduce** | drop `At:` from the events `cmd/riftnode` posts into the mailbox. |
| **Invariant that caught it** | `sim.History`'s own validity check, at the call site — which is exactly where GF-53 put it. |
| **Mutant class** | n/a; the panic *is* the detector, and it fired in the right place. |

**In sim the loop stamps every event's `At`; in real mode the poster must.** `riftnode` did not, so
every delivered message and every tick arrived stamped at the beginning of time — and the first
operation the leader answered produced a return **three seconds before its call**.

**The bug was in `cmd/riftnode`; the defect is the seam, and Ansh ruled it amended in `node/`.**

> `node.Driver.After` stamped the events **it** created. `node.Driver.Post` stamped nothing. **One
> contract with two entrances that behave differently**, and the failure mode was the worst
> available: an unstamped event is not an error at the call, it is a history that reads as nonsense
> later.

**Fixed as the asymmetry, and by the stronger of the two options.** Not "Post stamps too" — that
leaves the class *representable and merely handled*, and a caller could still pass a **wrong** time,
which is worse than a missing one because nothing downstream can tell it from a real observation.

```go
func (d *Driver) Post(kind sim.Kind, node sim.NodeID, payload any)
```

> **THE CALLER CANNOT SUPPLY A TIME BECAUSE IT CANNOT SUPPLY AN EVENT.** The driver owns its clock and
> its identity; the caller owns what happened. There is no argument left in which to be wrong.

**Induced three ways.** Post stamping nothing fails `TestEveryPostedEventCarriesTheDriversOwnTime`;
restoring `Post(sim.Event)` **fails to compile**, against a signature assertion written for that
purpose; and `After`'s predicted stamp has no path back. The second is the one that matters — the
class is now caught before any test runs.

**And the stamp moved from schedule-time to post-time.** `After` used to stamp with the instant it
*predicted* the timer would fire. Post-time is an observation rather than a prediction, and it makes
stamps non-decreasing in processing order, which a predicted stamp never was.

**What it says about the instrument:** the checker refused to record a nonsensical history *instead of
recording it and letting a checker downstream disagree later*. A history is the only artifact a chaos
run produces, and one that validates itself at the point of construction is worth the panic.

---

### GF-56 — a guard whose removal breaks nothing is not a guard, whatever its comment says

The client wire format's key and value limits were written as **allocation guards**, in the shape
`ReadFrame`'s `maxBodyBytes` has, with a test named for each. Then each was deleted to check the test
fired.

> **NOTHING BROKE.** The bounds check underneath refused the same bytes, with the same sentinel error,
> for a different reason. Both tests passed over a deleted guard.

**The distinction the code had lost:** `ReadFrame` reads from an `io.Reader` and *must* bound a claimed
length before allocating, because nothing has been read yet. `DecodeRequest` takes a `[]byte` that is
**already in memory** — a length claimed there can never cause an allocation larger than the frame it
arrived in.

So the limit is a **policy** limit, not an allocation guard, and the difference is testable: the only
case that separates them is a field that is genuinely **present**, inside a legal frame, and over the
limit. `maxBodyBytes` is 4MiB and the field limit is 1MiB, so a 2MiB key is exactly that case — and
building it costs a 2MiB slice, which is why the cheap absurd-length rows were written instead.

> **A TEST THAT PASSES FOR A REASON OTHER THAN THE ONE IT NAMES IS A TEST OF SOMETHING ELSE.** It will
> keep passing when the thing it names is gone.

**The general form**, and it is a rule for induction rather than for code:

> **WHEN A DELETION BREAKS NOTHING, THE QUESTION IS NOT "IS THE TEST WEAK" BUT "WHAT DID I THINK THIS
> CODE DID".** The answer here was that a correct-sounding comment described a mechanism one layer
> away, and the mechanism actually present was doing the work under a name that did not fit it.

Related: [GF-40]'s vacuous-green register, and BUG-046 — the same shape at test scope rather than at
guard scope.

---

### BUG-053 — (harness) the supervisor asked "is this node up" where the question was "was my process supposed to die"

| | |
|---|---|
| **Symptom** | Found by inspection while investigating OPEN-I2-1 below, not by a checker. |
| **Found by** | reading the reaper after a chaos run reported an uninvited exit for a node that had been deliberately killed. |
| **Reproduce** | **not achieved.** See below — this is stated rather than glossed. |
| **Invariant that caught it** | none. |
| **Mutant class** | `TestAKilledProcessIsNotReportedAsAnUninvitedExitAcrossARestart`, which is a smoke test rather than a reproduction. |

`Supervisor` kept a single `up` flag on `Node`, cleared by `Kill` before signalling so the reaper
would not count the death as unexpected. A restart then set it back to true **for the new process** —
and the old process's reaper, still blocked in `Wait`, would wake, read the flag, and report a
deliberately killed node as one that died on its own.

> **THE FLAG ANSWERED "IS THIS NODE UP". THE QUESTION THE REAPER HAS IS "WAS MY PROCESS SUPPOSED TO
> DIE".** Those are the same question only while there is one process, which is exactly the condition
> a chaos run removes.

**Fixed** by moving the expectation onto a per-process `launch` that each reaper closes over, so a
reaper can never read another process's status.

#### The reproduction was attempted and FAILED, and that is the important half

Three attempts, each measured:

| attempt | result |
|---|---|
| 8 kill/restart pairs against the buggy shape | 0 failures |
| 60 pairs | 0 failures |
| a fixture leaving a grandchild holding the inherited stderr, to make `Wait` slow on purpose | 0 failures — instrumenting the reaper showed it woke in **65–190µs** regardless, so the grandchild never delayed it |

> **A RACE THAT ONE SIDE ALWAYS WINS IS NOT REPRODUCED BY REPETITION**, and the third attempt was
> built on that. It did not work either: the premise that `cmd.Wait` would block on the held pipe was
> wrong, and the probe said so.

**So the honest position is:** the defect is real and the fix is right, and *it has not been shown to
be the cause of what was observed.* The two are recorded separately for that reason.

---

### BUG-054 — (harness) the restart raced the kernel releasing the dead node's socket, and this is OPEN-I2-1's cause

| | |
|---|---|
| **Symptom** | `chaos: node 3 (pid 12651) exited WITHOUT being killed: exit status 1`, twice in one run. |
| **Found by** | the `ExitedOther > 0` gate arm, which failed the run — and then by the **node stderr and kill-pid log added in response to OPEN-I2-1**, which named the cause on the next occurrence. |
| **Reproduce** | restart a node within a few ms of `SIGKILL`ing it; the successor reaches `net.Listen` before the kernel has finished with the predecessor. |
| **Invariant that caught it** | `ExitedOther > 0`. |
| **Mutant class** | the gate arm itself, plus `LeaderKills > Kills`, added below. |

The evidence, in three consecutive stderr lines:

```
chaos: killed node 3 (pid 12640)
riftnode: listen 127.0.0.1:61271: listen tcp 127.0.0.1:61271: bind: address already in use
```

`Restart` reused the address 2–3ms after the kill. The successor failed to bind, exited 1, and its
reaper reported it — **correctly**: that process did die without being killed.

> **SO_REUSEADDR DOES NOT HELP.** It lets a socket be rebound past `TIME_WAIT`; it does not let two
> *live* processes listen on one address, and for the milliseconds between `SIGKILL` and the kernel
> finishing with the old process, that is what this was asking for.

**Fixed by waiting for the predecessor's reaper**, which is also the more faithful chaos semantics and
would be the right shape with no bug at all: **a crashed node restarts after it is dead.** A restart
overlapping its own predecessor is not a fault this system claims to survive, so injecting it produced
a finding about the harness rather than about Rift.

#### A second gate arm, from a number that was visible and unread

The same run reported `leader-kills=3 of 2 kills`. More aims than signals means a kill was aimed at a
node that was **already down** — `Kill` found no live launch and returned without counting. The
arithmetic was on screen and said so, and nothing was asserting it.

> **A COUNT THAT CANNOT EXCEED ANOTHER IS A GATE ARM WAITING TO BE WRITTEN.** `LeaderKills > Kills`
> now fails the run.

#### What this closes, and what it does not

**OPEN-I2-1 is CLOSED for the `exit status 1` occurrences**, which are fully explained and fixed.

**It is NOT closed for the original `signal: killed`.** That message means SIGKILL, and a bind failure
exits 1 — different symptoms, so the same cause cannot be assumed. The narrowing from the failed
BUG-053 reproductions still stands and now has company: the reaper wakes in 65–190µs while
`Restart`'s fork/exec of a Go binary takes milliseconds, so the reaper **wins** that race and BUG-053
cannot fire in this configuration at all. Both hypotheses for the original observation are now
measured to be unlikely, and it remains one unexplained event.

**What would close it:** a reaper reporting `signal: killed` for a pid with no matching
`chaos: killed node N (pid P)` line. Both lines now print, so the next occurrence is decisive either
way.

> The instrument that produced this entry is the one added *because* OPEN-I2-1 could not be
> reproduced. **A red that cannot be closed can still be made cheaper to diagnose**, and that is what
> the stderr dump and the kill-pid log bought: the cause appeared on the very next occurrence.

---

### BUG-055 — (harness) the benchmark measured the CHECKER: every real node runs the simulator's oracle ledger

| | |
|---|---|
| **Symptom** | Throughput on a fault-free cluster fell **monotonically**: 1938 → 996 → 728 → 606 → 495 → 478 ops/s across six consecutive 5-second windows, with p50 rising 3.4ms → 16.3ms. No faults, no kills, nothing else running. |
| **Found by** | a steady-state baseline that came out **slower than the chaos run it was the denominator for** — 68 ops/s against 498 — producing a "728% of steady state" result that read as a pass. |
| **Reproduce** | run any sustained load against `cmd/riftnode` and watch `heap` in the counters file: ~80MB → ~190MB per node over 30 seconds, goroutines flat at 12. |
| **Invariant that caught it** | none. Every checker was green; the run was *correct* and getting steadily slower. |
| **Mutant class** | `bench.Result.SteadyEnough`, and the chaos-exceeds-steady baseline check, both induced. |

**`store.Config` REQUIRES a `raftcheck.Ledger`** — `cfg.Ledger == nil` is a construction error — so
`cmd/riftnode` supplies one, and every production node runs the simulator's oracle inside it. The
ledger retains, forever:

- every message **sent**, with the sender's durable hard state (`sent []sentRecord`)
- every message **received** (`recv []sentRecord`)
- every entry every node **applied**, in order (`applied [][]raft.Entry`)
- every **committed** entry (`committed []commitRecord`)

**Measured, not argued:** 100,000 recorded operations retain **87.5 MB — 875 bytes per operation.**
The leader sends ~3 messages per client operation and each is recorded at the sender and at every
receiver, which accounts for the observed per-node heap growth directly.

> **THE BENCHMARK WAS MEASURING THE ORACLE.** Every number taken from `riftnode` is a number about a
> node carrying a complete, unbounded audit log of its own history — and the degradation is not a
> Rift performance characteristic, it is the checker's retention showing up as latency.

#### Why this was invisible until now

The ledger is **correct** for what it was built for. A sim run is bounded: a few thousand events, then
the oracles read the ledger and the process exits. Unbounded retention is not a leak there, it is the
entire mechanism — the oracles need the whole history to answer questions like *"was this message
released before its term was durable?"*

> **A STRUCTURE WHOSE COST IS BOUNDED BY THE RUN IS FREE UNTIL THE RUN STOPS BEING BOUNDED.** Real
> mode is the first thing this project has built that does not end.

#### NOT fixed here, and specifically not by weakening anything

The ledger is an **oracle**. Trimming it, sampling it, or capping it would weaken a checker to get a
number, which is the one thing that is never done. The fix is to make the ledger **optional in
`store.Config`** so real mode can run without an oracle it is not consulting — and `store/` is signed,
so that is a ruling rather than an edit.

**Until then, every throughput and latency number from `riftnode` is reported with this entry beside
it.** The numbers are real measurements of a real cluster; they are not measurements of Rift.

#### And a second, smaller contaminant in code that IS mine

`cmd/riftnode`'s `serving` calls `hist.Begin` on the node's own `sim.History` for every request, and
nothing truncates it. That history is **discarded** — the authoritative one is the client's — and it
is used only as a completion channel. At ~100 bytes per event it is roughly **2.6 MB per 26,000
operations**: about 1/35th of the ledger's share, real, unbounded, and reported here rather than
fixed in passing while the larger one stands.

---

### GF-57 — a seam signed with no caller is a contract nobody has ever had to satisfy

`node/` was signed at A0 step 11. Its whole purpose was to give the mailbox rule *end-to-end teeth* —
`scope.go` said so in those words: *A0 does not exit until `node/` exists and the rule has real
teeth.* And from that day until `cmd/riftnode`, **every event the package ever handled came through
`After`, or through a test.** There was no real-mode caller.

So `Post`'s missing stamp was not a bug anybody could have noticed. The one entrance that was used
stamped; the one that was not, did not. **The asymmetry was invisible for the entire project and
surfaced on the first genuine posting** — as a panic, in another package, three seconds and one
leader election away from the line that caused it.

> **A CONTRACT WITH TWO ENTRANCES IS ONLY AS TESTED AS ITS BUSIEST ONE.** The unused entrance is not
> "less exercised"; it is *unspecified*, because nothing has ever had to satisfy it.

#### This is GF-49's shape a third time

GF-49 is about a **substitute that cannot express a class** — the stand-in is not merely less
faithful, it lacks the vocabulary for the thing you are trying to see:

| instance | the substitute | the class it could not express |
|---|---|---|
| first | `engine/model` under the full stack | anything the C++ engine does differently |
| second | the untagged build's engine name | that a run was not a storage result |
| **third** | **the simulator's loop as `node/`'s only caller** | **a caller that could OMIT what the loop supplied** |

The sim loop stamps every event itself. Node logic in sim mode has never seen an unstamped event, and
**no caller in sim mode has ever had the option to produce one** — the class of "forgot the time" is
not expressible from inside the loop. Driving `node/` exclusively from there was not a weaker test of
the mailbox contract; it was a test of a different contract that happens to share a name.

**And the fix takes the vocabulary away again, deliberately.** `Post(kind, node, payload)` makes the
class unrepresentable for *every* caller, so the property no longer depends on which entrance is busy.

---
# Track B — the C++ storage engine

*Everything below is Track B's `BUGS.md`, merged at I1. Its defect ids carry the `B` prefix; its
general forms (`GF-nn`) and mutant classes (`BM-nn`) are Track B's own numbering and do not collide
with Track A's.*

Every bug caught by the simulator, the crash-consistency rig, or differential testing gets an entry
here. This file is the proof behind the verification claim: it is the difference between "we ran a
lot of tests" and "we can show you what the tests found and why they found it."

**Rules**

1. Every bug found by a checker gets an entry. No exceptions for embarrassing ones — especially not
   for embarrassing ones.
2. Every entry must be reproducible: a seed (at the commit that contained the bug) **and** a plan
   bundle in `seeds/` (reproducible at any commit).
3. Every entry must answer **"which mutant class would have caught this?"** If none would have, a new
   mutant is added to `sim/toy/mutants` **in the same PR as the fix** — not as a follow-up.
   *(CLAUDE.md Amendment A2.)*
4. A bug that a checker did *not* catch — found by inspection, by a real-mode run, or by luck — is
   the most valuable entry in the file. It must additionally record what checker was missing and
   whether one was added.

**Counts:** 2 entries (engine bugs; the fenced harness-defect section below is counted separately and does not satisfy this gate). *(Both are Track B's, found by the kill-point sweep at B2. Track A's A1 gate is a separate obligation and is unaffected by them.)*

---

## Template

Copy this block. Do not drop fields; write "n/a" with a reason instead.

```markdown
### BUG-NNN — <one-line symptom, in the voice of what an operator would see>

| field | value |
|---|---|
| **Found by** | sim / crash rig / differential / real-mode chaos / inspection |
| **Phase** | A1 |
| **Reproduce (seed)** | `simctl replay 8834127` at commit `<sha>` |
| **Reproduce (plan)** | `simctl run --plan seeds/BUG-NNN/plan.json` (any commit) |
| **Invariant that caught it** | e.g. Election Safety |
| **Mutant class** | e.g. `M2-ack-before-replicate`; or "none existed — added `M8-…` in this PR" |
| **Fix commit** | `<sha>` |
| **Minimized?** | yes — N fault entries, M ops, K nodes |

**Symptom.** What the checker reported, verbatim where possible.

**Root cause.** The actual mechanism, not the patch. Written so someone who has never seen this code
can follow it.

**Why the checkers caught it here and not earlier.** Which injector had to fire, in what order.

**What this would have caused in production.** Be concrete and be honest: silent data loss, a stalled
range, a lost acknowledged write, a duplicated transfer. If the answer is "nothing user-visible," say
that.

**Fix.** What changed and why that is the right fix rather than a narrower one that would also make
the seed pass.
```

---

## FOR THE TRACK B WRAP-UP — marked by Ansh at sign-off

**Not the largest work of the phase; the best.** Recorded here so a wrap-up written months later does
not have to rediscover which findings carried weight.

**0. `GF-40` — THE ONE TO LEAD WITH. Marked by Ansh at B5's sign-off: *"GF-40 is the finding and it
belongs at the top of Track B's eventual wrap-up."***

The first full run of the mutant catalogue since B3 found **fourteen classes that were documented,
floored, counted, and unrunnable.** Their patches no longer applied; they had rotted across B3.5
through B4.2 and nothing noticed for two phases, because the catalogue costs hours and is therefore
run in `ONLY=` subsets — and every subset that does not name a rotted class is green.

**Three independent checks looked straight at all fourteen and agreed they were fine.** `cpp-scan`
asserts every class has a `FLOORS.txt` row; `FLOORS.txt` asserts every row carries a standing
measurement; every report counted them. Each of those checks was added *because an earlier defect
showed mutants can degrade silently.* All three range over the **record** of a mutant. None ranges
over the mutant.

> **A SET OF CHECKS THAT ALL VERIFY A THING'S DESCRIPTION WILL AGREE THAT IT IS FINE WHILE THE THING
> ITSELF IS GONE.**

**What closed it:** `make cpp-rot`, a dry-run apply of all 155 patches — **seconds against the
catalogue's hours** — which would have caught all fourteen on the day each rotted. **What it does not
close:** `BM16` would have applied cleanly and self-deadlocked, and a hang is neither a kill nor a
survival. A patch that applies is not thereby a patch that asks its question. `HARNESS-013` one layer
up, in the catalogue rather than the lane, where no watchdog helps.

**1. `BUG-B004`/`BUG-B005` and `GF-22` — two defects whose symptoms cancelled.** Invisible to every test
that asserts an **answer**, because the answer was right — arrived at by two errors that annihilate.
Separated only because **a mutant survived**, asking a question a test cannot: *is this line
load-bearing?* What makes it a demonstrated class rather than a hypothesis is the **asymmetric
evidence**: fixing `BUG-B004` alone turned **four passing tests red**; fixing `BUG-B005` alone would
have changed **nothing observable**. And `BM97`'s history is the citation for **a plausible label
being the dangerous one** — held out once when its workload did not exist, re-added rather than
relabelled, and the second survival is what opened both bugs. *"Covered by the compaction tests"*
would have been accepted by any reviewer.

**2. The observer-before-the-observed ordering, used eight times — and the two times it corrected the
SPECIFICATION rather than the code.** `keep(k)` over-requiring at B3.0, found by building the
adjudicator before any compaction existed; and the range-deletion semantics at B3.5a, fixed in the
model before a writer existed. **Both corrections were in the claim, not in an implementation**, which
is not the benefit the ordering is usually argued for. It is usually defended as *"a checker written
afterwards agrees with the implementation"*. Its actual yield here was **twice catching a wrong
specification before anything was built to it** — and once, at `HARNESS-020`, catching the author.

**3. B4's answer to why a sweep AND a differential both exist, as a measurement rather than a claim.**
With `BUG-B006` present in the tree: **377 tests passed, 4,840 kill points across three sweep regimes
reported zero violations, and 147 mutant classes were all killed.** **Eight clean differential runs
found it** — no crash schedule involved. No sweep workload ever produced a tombstone ending exactly on
the largest key, so no kill point could reach it.

> **A FAULT-INJECTION SWEEP AND A DIFFERENTIAL FIND DIFFERENT THINGS**, and this is the number that
> shows it rather than the sentence that asserts it.

**4. `GF-23` gaining a same-day instance.** *A remedy that is written down rather than built has the
defect's own shape and comes due on the defect's own schedule.* Written in the morning after
`HARNESS-019` cost a step's work; **broken by its own author that afternoon** — a `FLOORS.txt` row
containing `O(|S|)`, two delimiters — and **caught by a mechanism rather than by recall**, because
`HARNESS-017`'s remedy had been built into `cpp-scan` rather than merely recorded. The shortest
possible demonstration of the rule, arriving as its own evidence.

**5. FOUR RIGS, FOUR REAL DEFECTS FOUND ON THE FIRST OUTING. This is the number the wrap-up should
quote.** Marked at B5's close on Ansh's instruction, because it stopped being a run of luck somewhere
around the third one.

| rig | first outing | what it found |
|---|---|---|
| the **kill-point sweep** | its first swept regime | the three `HARNESS-006`-class classifier defects, each of which blamed the engine for the harness |
| the **crash-consistency rig** | its first sweep across the write path | `BUG-B001`, a database that had crashed once and refused to open ever again |
| the **B4 differential** | its first outing, `compact` seed 6, a **clean** run with no kill | `BUG-B006`, a table the engine wrote and could never open again — plus three defects in *itself* first |
| the **B5.2 parity suite** | the first run of its first version | `BUG-B007`, a value larger than the block buffer unreadable through the wrapper |

**WHAT THE PATTERN ACTUALLY SAYS, and it is not that the rigs are good.** Every one of these defects
was present in code that had already passed a lane. They were not found by running the existing
instruments harder; they were found by asking a question **nobody had asked once**. A first outing is
the only moment an instrument is guaranteed to be asking something new — after that it is regression
coverage, and its yield falls to whatever the next edit breaks.

> **THE YIELD IS IN THE QUESTION, NOT THE INSTRUMENT. A NEW RIG'S FIRST RUN IS THE HIGHEST-VALUE RUN
> IT WILL EVER HAVE, AND A PHASE THAT ADDS NO NEW QUESTION IS A PHASE THAT WILL FIND NOTHING NO MATTER
> HOW LONG ITS LANES RUN.**

The operational reading for I1 and I2: budget for the first run of every new instrument to **fail**,
and treat a new rig that comes up green on its first outing as a claim about the rig — `B4` §8's
expectation, now with four data points behind it. It has also gone the other way once, which is why
the claim is about the *question* and not the rig: the differential found **three defects in itself**
before it found one in the engine, and `GF-29`/`GF-30` are that lesson.

---

## Entries

Counts: 2 entries. Both were found by `make cpp-sweep` — the kill-point sweep — in the cycle that
wrote the code, before either reached a commit anyone else would have built. Neither is a bug in
signed work; both are recorded because rule 1 makes no exception for a defect a checker caught early,
and because **what they have in common is worth more than either**: an ordering argument that was
right about the state it named and wrong about the state it did not.

### BUG-B001 — a database that had crashed once refused to open ever again

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, first run in the default regime: **41 violations of 300 kill points** |
| **Phase** | B2.4, found at B2.6 — before the code was ever run outside a lane |
| **Reproduce** | `rift_sweep default` at the commit before the fix; every kill from ordinal 26 onward |
| **Invariant that caught it** | the exactness oracle, via `reopen failed: db/000002.log: named by the manifest and absent` |
| **Mutant class** | **BM59-wal-named-before-it-exists**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** After any crash between naming a WAL in the manifest and creating the file, every later
`Open` fails with *"named by the manifest and absent"* — permanently. The database is intact; it
simply cannot be opened again.

**Root cause.** B2-D5 replaces B1's gapless numbering check with *"every file number the manifest
names exists"*. Naming a WAL **before** creating it looks like the safer order — it guarantees no
file exists that the manifest has not heard of — and a crash in that window leaves **a name with no
file**. That name is durable. It persists into every subsequent manifest, so `named and absent`
stops meaning `lost directory entry` from that moment on, and the only available repair is an
exception ("the highest named number may be absent") that then has to be justified at every Open
forever after — and which the sweep also refused, because a *second* crash in the same window makes
the previously-absent name no longer the highest.

**The fix inverts the order.** A WAL is **created, then named, then written to**. A crash between
creation and naming leaves an *empty unnamed file*, which carries nothing and is deleted. Both halves
of the rule then hold with **no exception in either direction**: every named WAL exists, and nothing
in an unnamed one is above what the tables cover.

**What it would have caused in production.** Total unavailability after an ordinary power cut, on a
database with no data loss and no corruption. The worst kind: nothing to recover, nothing to
diagnose, and the engine insisting it was right.

**The general form.** *An ordering that looks safer because it prevents the state you can name may
create the state you cannot.* Both orders leave a crash window; the question is not which window is
smaller but **which one closes itself**. Create-then-name leaves a file that provably carries
nothing; name-then-create leaves a durable claim that nothing can ever discharge.

---

### BUG-B002 — recovery would have discarded committed records in a WAL a flush had retired

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, flush regime: **64 violations of 985 kill points** |
| **Phase** | B2.4, found at B2.6 |
| **Reproduce** | `rift_sweep flush` at the commit before the fix; every kill from ordinal 149 onward |
| **Invariant that caught it** | the exactness oracle, via `db/000002.log: present, not named by the manifest, and holding 46 committed batches` |
| **Mutant class** | **BM62-unnamed-wal-unchecked**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** After a crash between the manifest edit that retires a WAL and the deletion of the file,
`Open` refuses — reporting a WAL that is *supposed* to be there.

**Root cause.** BUG-B001's fix rested on an argument: *a present unnamed WAL is one caught between
creation and naming, so it is empty*. That is true of one of the two ways a WAL can be unnamed. The
other is a flush: the manifest drops a WAL's name in the same group that adds the table covering it,
so between that group and the file's deletion there is an unnamed WAL **full of records**. The
emptiness check was an assumption about how the state arose rather than a statement about the state.

**The fix states the property instead of the provenance.** *Nothing in an unnamed WAL may be above
what the SSTables cover.* An empty one satisfies it trivially; a retired one satisfies it because the
table covers it; and a WAL whose records nobody covers — the case worth refusing — still fails it.
This is B2-Q1's **"nothing covered twice"** seen from the file side, and it is a strictly stronger
check than the one it replaced.

**What it would have caused in production.** The same total unavailability as BUG-B001, in a narrower
window — and the check that produced it was the one added to *prevent* silent loss, which is the
shape worth noticing: **a safety rule stated in terms of how a state arises rather than what it
contains will refuse legitimate states the moment a second path reaches the same state.**


---

## Harness and lane defects

**This section is not the entry list above and does not substitute for it.** Entries here are defects
in *checkers* — lanes, rigs, mutation harnesses — found by other checkers. They are recorded because
rule 1 says every bug a checker finds gets an entry and makes no exception for embarrassing ones, and
they are fenced off because they are not engine bugs:

- they do **not** count toward the A1 phase gate's "BUGS.md is nonempty" requirement, which is about
  the simulator finding bugs in the *protocol*;
- they do **not** count toward the `[K] documented postmortems` figure in CLAUDE.md's resume lines,
  which is about faults injected into a running system;
- they **do** count as evidence that the induced-failure discipline works, which is the only reason
  either of these was visible at all.

Counts: 14 entries.

### The two general forms these entries taught

Recorded here rather than only inside the entries, because the instances are
cheap and the forms are not.

**GF-1 — a lane that verifies an ABSENCE must run in a state where the thing
could actually be present.** From HARNESS-002 and HARNESS-004. An absence
verified in a state that could not have contained the thing is not a
verification, it is a tautology wearing a lane's clothes. `cpp-ci` claims no
lane touches the network; it was resting on a warm FetchContent cache rather
than on the absence of a fetch, and the isolation it did have worked perfectly
and had nothing to block. Track A has hit the cousin of this twice. The cold
cache is now part of what `cpp-ci` MEANS and is asserted at both ends —
`scripts/cpp-cold-cache.sh`, induced by `COLD-fetch-despite-isolation`.

**GF-5 — AN ACCIDENTAL DEFENCE IS WORSE THAN A MISSING ONE.** From HARNESS-010,
and new to both tracks.

A missing defence measures as missing. An **accidental** one — a property that
holds for a reason nobody chose, in a component that was not trying to provide
it — makes a real gap **measure as covered**, and then removes itself on a
schedule nobody is tracking.

The instance: recovery applied records it never committed, and they were
invisible only because every read went through a snapshot pinned at the
recovered watermark. Correct state, correct lanes, correct counts — and the
whole sweep reported 175 passes on an engine with a live recovery defect. The
read path was not defending anything; it simply had no way to show the damage.
**And it expires at B2**, where the flush writes the memtable out and those
records become durable, visible and permanent.

Two obligations follow, and the second is the one that is easy to skip:

1. When a defence is found to be accidental, say so where it is measured — a
   floor derived from an accident is a floor measuring the accident.
2. **Put its expiry in `CARRY-FORWARD.md` as a dated obligation, not a note.**
   An accidental defence has a date, and the date is the phase that removes it.
   If nobody is holding that date, the gap reopens silently and the measurement
   that would have caught it is the one the accident was inflating.

**GF-4 — AN UNSATISFIABLE GATE. A classification that decides whether evidence
counts must be tested in BOTH directions, because the safe-looking direction is
the one no assertion notices.** From HARNESS-006.

Every other entry in this file is a check that COULD NOT FAIL. This is the
opposite shape and it needs its own name: a check that could not PASS. A
classifier that marks too much as non-evidence breaks nothing — the engine still
behaves correctly, every assertion still holds, every lane stays green — and the
only consequence is that a column stays empty. It is invisible precisely BECAUSE
it is conservative.

**The consequence is the sharpest part.** The cost does not arrive where the
defect is. It arrives one or two steps downstream, as a gate nothing can
satisfy: §7.4 condition 3 requires both elements of the two-element recovery set
to have been observed across the sweep, and a sweep whose runs are structurally
uncountable as evidence can never satisfy it. Found there, **it presents as a
bug in the engine rather than in the classifier, so the debugging starts in the
wrong component** — which is the expensive part, not the fix.

The audit this forced is in HARNESS-006. Closed by §7.5's registry holding
exactly its two named members, asserted both ways, and by every other
evidence-deciding function in Track B now being asserted both ways too.

**GF-3 — when an end-to-end test cannot distinguish two designs because both
fail the same way, assert the discriminating property directly on the unit where
the two differ.** From BM10. Our WAL checksum covers `length ‖ type ‖ payload`;
LevelDB's covers `type ‖ data`. End to end the two are nearly
indistinguishable — a corrupted length fails the checksum under either coverage —
because **the difference is not in what happens, it is in what is KNOWABLE at
the moment of failure**: with the length covered, the failure is at a known
offset and resync has a sound starting point; without it, the bytes consumed
before the failure are a function of data recovery has already decided not to
trust. So the property is asserted on `FragmentCrc` itself: *same type, same
payload, different length ⇒ different checksum*, which is false under upstream's
coverage and true under ours, in one line, with no log image involved.

The corollary is about who the defence is aimed at. **A deliberate divergence
from a well-known upstream is not attacked, it is helpfully corrected.** BM10 is
the one mutation in this catalogue a reviewer would most likely *approve*: it
introduces no bug, it removes two bytes of work, and it makes us match LevelDB,
whose header is byte-identical to ours. A defence written against a defect would
be pointed the wrong way. This one is pointed at a competent, well-meaning
reader — which is why the reasoning lives on the helper as a comment and not
only in a design document nobody re-reads before a cleanup.

**GF-2 — a two-field assertion where both fields read the same value under the
defect is not an assertion.** From HARNESS-003. Track A has recorded this shape
twenty-four times; it appeared in Track B's first cycle of real code, in a
different language and a different subsystem, written by someone who had read
all twenty-four. That is not a coincidence and it is not about C++ or about
ledgers: **the class is about how verification code gets written.** The reflex
it demands is to ask, of every assertion, "what value would this read if the
thing I am checking were broken?" — and if the answer is "the same one", the
assertion is decoration no matter how specific it looks.

### BUG-B004 and BUG-B005 — two defects in one step whose symptoms cancelled

| field | value |
|---|---|
| **Found by** | **a mutant surviving** — `BM97`, on its second induction |
| **Phase** | B3.5e, found and fixed before the step was signed |
| **Reproduce** | `Compaction.ASnapshotBelowARangeTombstoneKeepsTheVersionItHides` (BUG-B004); `RangeDelete.ARangeSurvivesTheCompactionThatMovesItToLevelOne` (BUG-B005) |
| **Invariant that caught it** | none directly — see below, that is the entry |
| **Mutant class** | `BM105` preserves BUG-B004; `BM97`/`BM101` cover BUG-B005 |
| **Fix commit** | this one |

**BUG-B004 — clause 1 asked about the wrong sequence.** It tested whether a range tombstone covered
the key at the **top of a version's interval**. That conflates two different sequences: a snapshot at
5 and a tombstone at 9 are both "inside the interval" of a version at 4, and **the tombstone is
invisible to the snapshot.** The version is the answer at 5, and it was being dropped.

> **DATA LOSS FOR A SNAPSHOT BELOW A RANGE DELETE** — and it passed every end-to-end test, because a
> live snapshot **holds the pre-compaction tables resident** and reads through them. The loss appears
> at the next Open, after the snapshot is gone.

**BUG-B005 — the sink was told its tombstones after it had written its files.** `RunCompaction` called
`SetTombstones` at the *end* of the merge. The sink closes output files *during* it, and a file closed
before it has been told the tombstones writes none. **The interface header already said "handed over
BEFORE the first entry"** — the contract was written down, in the same step, by the same author, and
the implementation violated it.

**THE ENTRY IS THAT THE TWO CANCELLED.** BUG-B004 dropped the versions a tombstone hid, so BUG-B005's
missing tombstone changed no answer: every read returned `<absent>`, **for the wrong reason**. Fixing
BUG-B004 alone turned four passing tests red. Fixing BUG-B005 alone would have changed nothing anyone
could observe.

> **A PAIR OF DEFECTS WHOSE SYMPTOMS CANCEL IS INVISIBLE TO EVERY TEST THAT ASSERTS AN ANSWER.** Both
> produce the right answer together. Only a question about a *mechanism* separates them.

**What found it was a mutant SURVIVING, which asks a different question.** A test asks *is the answer
right?* A mutant asks *is this line load-bearing?* `BM97` blinded the L1 tombstone lookup and nothing
failed — and it was **correct**: the lookup was not load-bearing, because nothing ever reached it.
The survival was true information about the engine, not a weak mutant.

**This is the strongest argument the catalogue has produced for the practice.** `BM97` had already
been held out once (its workload did not exist at B3.5d) and re-added deliberately. Relabelling it as
*"covered by the compaction tests"* would have been plausible, would have closed the file, and would
have left two defects in a shipped step. **GF-16's rule — reach the workload, never relabel — is what
kept the question open long enough to answer it.**

**Fix.** Clause 1 now tests **per observable sequence**: a version is required when some `s` both
sees it as newest *and* has no tombstone above it visible at that same `s`. The tombstone verdict now
runs **before the merge loop**, where the header always said it did; nothing in it depends on the
merge, so this is not a reordering for convenience.

---

### BUG-B003 — a guard that reads as a serialiser and never serialised anything

| field | value |
|---|---|
| **Found by** | inspection, while adding compaction's manifest append at B3.4 |
| **Phase** | **present since B2.5; found at B3.4; harmless until B3.4** |
| **Reproduce** | not reproducible by a lane: it needs two concurrent `DB::Sync` callers, which the contract forbids and no in-tree caller does. See *Why nothing caught it* |
| **Invariant that caught it** | none — that is the entry. It was found by asking what else appends to the manifest |
| **Mutant class** | **BM82-sync-precondition-unguarded**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** None, in any run that has ever been made. A second concurrent `Sync` would append
manifest records **inside another append's group**, producing a manifest whose group terminator
counts do not match the records that precede them — refused at the next `Open` as corruption, with
the durable data intact and unreachable.

**Root cause.** `DBImpl::Flush` opens with

```cpp
if (closed_ || imm_ != nullptr) return Status::Ok();
```

which reads as *"one flush at a time"* and is not. **`imm_` is assigned several steps later**, after
the file-number reservation, the new WAL's creation, and the **first `AppendGroup`**. Two callers
arriving together both observe `imm_ == nullptr`, both pass, and both append. The guard makes a flush
a no-op while one is **pending**; it has never made two flushes mutually exclusive.

**Why nothing caught it, and this is the honest part.** Until B3.4 the manifest had exactly **one**
appender, so two concurrent flushes could at worst duplicate work. The defect needed a **second
appender** to become damaging, and compaction is the first one. The TSan lane could not have found it
either: its authored pattern is one writer and one syncer, and the header says why — *"not more,
because more would be a claim the contract does not make."*

**What this would have caused in production.** A corrupt manifest after two concurrent `Sync` calls,
which the frozen contract does not permit — so: nothing, for a conforming caller. For a
**non-conforming** one, an engine that refuses to open and loses nothing, which is the good failure
mode and is still a failure nobody would have diagnosed from the message.

**Fix, and why not the wider one.** Not a lock, and not a third TSan pattern. Both would have
answered a question the contract does not ask, and a lock would have **converted a precondition into
a supported mode** — the shape where an engine grows a guarantee nobody decided to make. The
precondition is enforced instead: `SingleCaller` in `DB::Sync`, so a second concurrent caller aborts
at the call rather than leaving a manifest for the next `Open` to refuse. The flush guard keeps its
early return and its comment now states what it does.

**B2'S SIGN-OFF IS AMENDED IN PLACE, NOT REOPENED.** `docs/DESIGN-B2-sstables.md` carries a note
naming this entry. The reason is a distinction worth keeping:

> **A PHASE'S SIGN-OFF IS A CLAIM ABOUT WHAT WAS VERIFIED, NOT A CLAIM THAT THE CODE WAS CORRECT.**

**The second time a phase's record has been amended by a later phase, and the mechanism was the same
both times: a defect unreachable under the earlier phase's shape.** Track A amended A4 and A5 for
`BUG-023`. Neither amendment says the earlier verification was wrong; both say the earlier phase
could not have reached the defect, and name the later shape that did. An amendment that reads as an
accusation would make the next one less likely to be written.

---

### BUG-B006 — a table the engine wrote and could never open again

| field | value |
|---|---|
| **Found by** | **the B4 differential rig, on its first outing**, `compact` seed 6 — a CLEAN run, no kill |
| **Phase** | present since B3.5e; found at B4.2 |
| **Reproduce** | `SstWriter.ATombstoneEndingAtTheLargestDataKeyDoesNotMoveTheBound`; end to end, `rift_diff compact 6` |
| **Invariant that caught it** | `VerifyTables` — D4 §5.1 point 2, the manifest held to the classifier's derivation |
| **Mutant class** | **`BM113`**, added in the same PR as the fix |
| **Id note** | `BUG-B004` and `BUG-B005` are B3.5e's cancelling pair; this is the next free id |
| **Fix commit** | this one |

**THE SEVERITY IS A PERMANENT REFUSAL, NOT DATA LOSS, AND THAT DISTINCTION LEADS.**

> **A DEFECT THAT LOSES DATA IS DISCOVERED AND MOURNED. A DEFECT THAT REFUSES TO OPEN IS DISCOVERED
> AND CANNOT BE WORKED AROUND.**

The bytes are intact. Every table is valid, every checksum passes, the WALs are complete — and
`DB::Open` fails with `key bounds disagree with the manifest`, on this open and every open after it.
There is no retry, no partial recovery, and no way in.

**The triggering shape is unremarkable:** *any range delete whose end lands exactly on the highest key
present, at a lower sequence.* `compact` seed 6 produced it in 800 operations.

**Root cause.** `TableBuilder` and `ValidateTable` disagreed about when a range tombstone's end
widens a table's `largest`:

| | comparison |
|---|---|
| `TableBuilder::AddRangeTombstone` | `CompareInternalKey` — **internal keys** |
| `ValidateTable` | `.compare(ExtractUserKey(...))` — **user keys** |

The internal order is user key ascending and **tag descending**, so at one user key a *smaller tag
sorts later*. A tombstone ending at the largest data key with a lower sequence compares **greater** to
the writer and **equal** to the classifier. The writer widens; the classifier does not; the manifest
records the writer's and is held to the classifier's.

**THE PROVENANCE IS THE PART WITH THE MOST TEACHING IN IT.** §6.1a *is* this correction — *"The bound
is a statement about USER KEYS. It is compared as one."* — written at B3.5e, applied to the
classifier, **and never carried to the writer.** The comment explaining the fix sits ten lines from
the code that still had the bug.

> **A CORRECTION APPLIED TO THE SITE THAT FAILED RATHER THAN TO THE INVARIANT LEAVES EVERY OTHER
> IMPLEMENTATION OF THAT INVARIANT DEFECTIVE — AND THE COMMENT BESIDE THEM SAYS OTHERWISE.**

It is `GF-7`'s family: a claim attached near code that does not honour it. **Second time in this
engine** a correct comment has sat beside an incorrect line — `BM55` was the first.

**SO THE FIX WAS NOT APPLIED TO THE SITE. IT WAS APPLIED TO THE INVARIANT**, and the audit that
demands came next.

**Every place in the engine that compares a bound, and what it compares:**

| site | before | after |
|---|---|---|
| `TableBuilder::AddRangeTombstone` — largest | **internal** ✗ | user, via `WidensUpperBound` |
| `TableBuilder::AddRangeTombstone` — smallest | **internal** ✗ | user, via `WidensLowerBound` |
| `TableBuilder::AddUnboundedRangeTombstone` — smallest | **internal** ✗ | user, via `WidensLowerBound` |
| `ValidateTable` — largest | user ✓ | user, via `WidensUpperBound` |
| **`ValidateTable` — smallest** | **internal ✗ — A SECOND INSTANCE, FOUND BY THE AUDIT** | user, via `WidensLowerBound` |
| `VerifyL1IsARun`, `L1FileFor`, input selection, the L1 sort | user ✓ | unchanged |
| `Table::Iter::Seek`, block index, entry ordering | internal ✓ — **correctly**: these order ENTRIES, not bounds | unchanged |

**The fifth row is why the audit was worth doing.** `ValidateTable`'s *smallest* still compared
internal keys after B3.5e corrected its sibling **ten lines below**. It was latent only because the
writer compared internal keys too — **the two agreed while both were wrong**, which is a shared blind
spot rather than a disagreement, and no comparison between them could have found it.

> **ONE FACT, SEVERAL PLACES, ONE CORRECTED — the class `BUG-032` cost Track A.** The remedy is the
> one that class always wants: `WidensUpperBound` / `WidensLowerBound` are **one implementation**, and
> both the writer and the classifier call it. There is nothing left to keep in step.

**THE REACH ARGUMENT, AND IT IS THE DIFFERENTIAL'S JUSTIFICATION AS A MEASUREMENT RATHER THAN A
CLAIM.** With this defect present:

| instrument | result |
|---|---|
| 377 C++ tests | **all passed** |
| 3 sweep regimes, **4,840 kill points** | **0 violations** |
| 147 mutant classes | **all killed** |
| **8 clean differential runs** | **found it** |

No sweep workload ever produced a tombstone ending exactly on the largest key, so no kill point could
reach it. **A fault-injection sweep and a differential find different things, and this is the
measurement that shows it rather than the claim.** The differential needed **no crash schedule at
all** — the defect is in the write path and appears on a clean close.

**AND THE FIX MADE THE DEFECT UNREPRESENTABLE, WHICH THE MUTANT DISCOVERED BY SURVIVING.** `BM113`
was aimed first at `WidensUpperBound` itself and **survived** — correctly. The predicate takes a
**bare user key**, so comparing it against a full internal key behaves identically; there is no way to
express the bug through that signature.

> **A FIX THAT MAKES A DEFECT INEXPRESSIBLE IS STRONGER THAN ONE THAT MAKES IT WRONG — AND NO MUTANT
> CAN ASSERT THE DIFFERENCE AT THE SITE IT PROTECTS.** The survival is the evidence.

So `BM113` was re-aimed at the site where the class **can** still recur: a caller building its own
internal key and comparing for itself, which is what `TableBuilder` did before the fix and what an
inlining edit — *"why call a helper for two lines"* — would reintroduce.

**A SECOND CONSEQUENCE, AND IT IS A REAL TRADE RATHER THAN A FREE WIN.** Collapsing two
implementations into one removed the disagreement — and **removed the instrument that detected one.**
The reproduction asserts writer *equals* classifier, and with both calling one predicate they agree
even when the rule is wrong. What is left must be asserted against the **invariant** rather than
against the other implementation, which is why `BoundWideningIsAStatementAboutUserKeys` tests the
predicate directly, in **both tag directions** — a rule comparing internal keys would be right about
one of them by accident.

**What it would have cost in production:** an engine that accepts writes, acknowledges them, closes
cleanly, and cannot be restarted. Discovered at the worst possible moment, on a restart, with the
data intact and unreachable.

---

### BUG-B007 — a 100 KB value was unreadable through the Go wrapper, and a comment said it was fine

| field | value |
|---|---|
| **Found by** | **the B5.2 parity suite, on its first run** — `TestALargeValueCrossesIntact`, no fault of any kind |
| **Phase** | introduced and found inside B5.2 |
| **Reproduce** | `make cpp-cgo`; any value larger than `block*1024+4096` bytes, reached by iterating |
| **Invariant that caught it** | wrapper/model parity — the two engines must return the same state |
| **Mutant class** | **`BM116`** (the wrapper) and **`BM115`** (the boundary's half), both added in the same PR as the fix |
| **Fix commit** | this one |

**THE COMMENT IS THE ENTRY, NOT THE BUFFER.** The first version of `fill()` carried this, three lines
above the bug:

> *"Sized so an ordinary pair never round-trips twice. A short buffer is CORRECT — the C side holds the
> pair rather than dropping it — so this is a performance choice and not a correctness one, which is the
> only reason a guess is acceptable here."*

**Every clause is true of the boundary. None of it was true of the code beneath it.** The comment
describes a caller that grows; the code was a caller that gives up. And the reasoning it offers — *a
guess at the buffer size is acceptable because the failure mode is benign* — is precisely what made
the undersized buffer read as deliberate rather than unfinished. The comment did not merely fail to
describe the code; **it supplied an argument for the code being right.**

> **A CLAIM TRUE OF ONE SIDE OF A BOUNDARY AND FALSE OF THE CODE BENEATH IT, IN A LANGUAGE THE OTHER
> SIDE'S COMPILER CANNOT SEE, IS `GF-11` AT ITS WORST CASE: NO TYPE, NO TEST FILE, AND NO REVIEWER
> HOLDING BOTH HALVES AT ONCE.**

That is the full statement of why this one was invisible. In the C++ engine, a comment that overstates
its code is still read beside that code by someone who can also see the caller. Here the two halves of
one contract live in **different languages**: no compiler spans them, no type spans them, no single
file spans them, and the reviewer who knows the C side's holding behaviour is not the reviewer reading
the Go loop. The C++ suite passed. The Go build passed. **The property they jointly promise was
checked by neither** until a test crossed the boundary with a value big enough to matter.

**Symptom.** `iter.Error()` returns `riftcgo: buffer too small` and iteration stops. Every key below the
large value is returned correctly; the large one and *everything after it* is silently absent, because a
cursor that errors is a cursor that ends. A caller checking `Error()` sees a failure it cannot act on;
a caller that only ranges to exhaustion sees **a short database**.

**Root cause, and it is on the Go side.** `rift_iter_block` returns `RIFT_BUFFER_TOO_SMALL` *without
consuming anything*, precisely so a caller can grow and retry without losing its position. That is the
documented contract and the C++ side implements it exactly. The wrapper's `fill()` treated the status
as fatal — it never grew.

**WHAT CAUGHT IT: A RIG ON ITS FIRST OUTING, FOR THE FOURTH TIME IN THIS PROJECT.** The parity suite
was written in the same step as the wrapper and failed on the first run of its first version. That is
now a pattern with a count, and it is recorded as such in the wrap-up section above.

**THE FIX WAS TO BOTH SIDES, AND THE SECOND HALF IS THE MORE USEFUL ONE.** The wrapper now grows and
retries. But a caller told only *"too small"* has to **guess** how much to grow by, and a guess that is
still too small loops — so the loop would have had no terminating quantity, which `CF-3` forbids.
`rift_iter_block` now reports the capacities the pair **needs** on that path, exactly as `rift_db_get`
already did for point reads. One grow always suffices, and the wrapper *asserts* that a second refusal
is impossible rather than tolerating it.

> **THE BOUNDARY ALREADY HAD THE IDIOM. IT WAS APPLIED TO ONE OF THE TWO CALLS THAT NEEDED IT.**

An inconsistency inside one interface is not a style defect; it is a place where a caller's correct
instinct — *this call behaves like that one* — produces wrong code. `rift_db_get` taught the wrapper's
author to expect `needed`, and `rift_iter_block` did not provide it.

**Why the mutant class is two classes.** `BM116` is the defect as it occurred, on the Go side, and it
is the first mutant in this catalogue that patches Go — the runner always copied the whole tree and
never cared about the language, so all that was missing was `cpp-cgo`, a lane running the Go half of
the boundary. `BM115` is the half a Go-only fix would have left standing: a boundary that reports its
*used* bytes rather than its *needed* ones still refuses correctly, still consumes nothing, and passes
every C++ test that checks the status code. It is caught only because its covering test asserts the two
numbers **by name** rather than asserting that the call refused — `GF-25` at the boundary.

**What it would have caused in production.** Any embedder iterating a database containing a value
larger than its block buffer would read a truncated database, with an error most range loops never
check. The threshold is a function of the *block size*, so the same database is complete at one
setting and short at another — which is the worst version of this defect, because the natural
diagnosis is that the data is missing.

### HARNESS-001 — a mutation lane's scratch copy silently lost three files of the tree under test

| field | value |
|---|---|
| **Found by** | `make cpp-mutants`, its own baseline gate |
| **Phase** | B1.0 |
| **Reproduce** | n/a — not seed-driven. `tar cf - --exclude=./.github .` from the repo root, then count files under `third_party/googletest/.github` in the result: 0, not 3 |
| **Invariant that caught it** | vendored-tree integrity (DESIGN-B1 §9.2) — the hash check ran *inside* the scratch copy and disagreed with the recorded hash |
| **Mutant class** | none needed — the baseline gate is the mechanism, and it is the `blind`-lane pattern already required by Amendment A2 |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `INVALID — the unpatched tree does not pass lane "cpp-ci"`,
with the vendored-tree check inside the scratch copy computing a different tree hash and counting
247 files where the working tree has 250.

**Root cause.** `copy_tree` excluded VCS metadata with `tar --exclude=./.git --exclude=./.github`.
bsdtar matches an exclude pattern against any suffix of a stored path, not only against the root, so
`./.github` also matched `./third_party/googletest/.github` — three files that upstream tracks.
The scratch tree was therefore not the tree under test.

**Why it was caught here.** Only because a vendored dependency with its own `.github` directory
arrived in the same step as the lane, and only because the vendored-tree hash check runs inside the
copy rather than only in the working tree. Without that check the copy would have been wrong and
every mutant result would have been about a slightly different tree, indefinitely and invisibly.

**What this would have caused.** Mutant verdicts computed against a tree that is not the repository.
For BM21 the three missing files are inert, so the verdict would have been right by luck; nothing
about the mechanism guarantees the next one would be.

**Fix.** Copy everything, then delete the root paths by name. A glob that "usually" anchors is not an
anchor. Recorded at the call site, because the next person to add an exclusion needs the reason.

### HARNESS-002 — `cpp-ci` passed under network isolation because its build directory was warm

| field | value |
|---|---|
| **Found by** | mutant `BM21-network-in-build` |
| **Phase** | B1.0 |
| **Reproduce** | apply `engine-cpp/mutants/BM21-network-in-build.patch`, run `make cpp-lane-set` (network available), then `make cpp-ci`. Before the fix: green |
| **Invariant that caught it** | no lane touches the network (DESIGN-B1 §9.2) |
| **Mutant class** | `BM21-network-in-build` — it existed, it fired, and the lane was wrong rather than the mutant |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `ALIVE  BM21-network-in-build: cpp-ci stayed green`.

**Root cause.** `cpp-ci` reused `engine-cpp/build`. CMake's `FetchContent` populates at configure time
and skips the download when `_deps/` is already populated, so any earlier networked build left behind
exactly the artifact that makes a network dependency invisible. The lane's premise — that a clean
clone with no network can build — was true only when the build directory happened to be cold.

**Why it was caught here.** Because BM21 is bidirectional by construction: the same patched tree must
go green with a network and red without. The control direction ran first, warmed the cache, and the
covering direction then measured the cache instead of the network. Two bugs met — one in the lane,
one in the mutation harness sharing a tree between directions — and the mutant surviving was the only
signal either produced.

**What this would have caused.** `cpp-ci` reporting that no lane touches the network while a lane
touched the network. The failure would surface for the first time in the hands of the stranger the
lane exists to protect: a clean clone, offline, one script, red.

**Fix.** Two, because there were two defects. `cpp-ci` now builds in its own directory and deletes it
first, so the lane is cold by construction rather than by habit. And `cpp-mutants` gives each
direction of a mutant its own tree, because a control run and a covering run are independent
experiments and one must not be able to feed the other.

### HARNESS-003 — the ledger's promotion flag was never under test, and a mutant proved it

| field | value |
|---|---|
| **Found by** | mutant `LEDGER-always-promoted`, which **survived** |
| **Phase** | B1.3 |
| **Reproduce** | apply `engine-cpp/mutants/LEDGER-always-promoted.patch` against the tree at `cf12938` and run `make cpp-test`: green |
| **Invariant that caught it** | none — that is the entry. The mutant survived, and the survival is the finding |
| **Mutant class** | `LEDGER-always-promoted`, added at B1.3 alongside the ledger it blinds |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `ALIVE  LEDGER-always-promoted: cpp-test stayed green`.

**Root cause.** The lying-Sync test asserted on `LastPromisedBytes`, a helper that scans the ledger for
entries with `promoted == true` and returns the last `durable_bytes_after`. Under a suppressed
promotion `durable_bytes_after` is 0 whether or not the entry claims to have promoted, so the two
fields agreed at zero and the flag itself was never read by any assertion. The ledger's whole job is to
record what *happened* rather than what was *reported*, and the field carrying that distinction was
unchecked.

**Which of the three things a surviving mutant means.** A checker that cannot see it. Not a defence
that was never there — the flag was set correctly — and not unreachable code, which is the only one of
the three whose correct response is deletion. The response here is to strengthen the checker.

**What this would have caused.** Nothing in production; the engine does not read the ledger. It would
have cost the *oracle*: B1.9a's exactness assertions are required to derive their verdict from the
ledger and from nothing else, and a ledger whose promotion column had silently become a copy of the
Sync's return value is the engine's account of itself wearing harness clothing. It would have been
discovered, at best, as an unexplained pass at B1.9b.

**Fix.** The test now asserts on `promoted` directly, in both directions: a lying Sync's entry must
read `promoted == false` with `injection == kSyncLoss`, and a clean Sync's must read `promoted == true`
with the right byte count. A flag asserted in only one direction degenerates into a constant.

**The class, not the instance.** See GF-2 above. Both fields reading zero under the defect is the same
shape Track A has recorded twenty-four times, and it arrived in Track B's first cycle of code that does
anything. The lesson is not about ledgers, or about C++: it is about how verification code gets
written, and it will arrive again in B1.6's byte digest and B1.9a's oracle unless the question "what
would this read if the subject were broken?" is asked of every assertion.

### HARNESS-004 — the cold-cache check asserted the absence of something it had just deleted

| field | value |
|---|---|
| **Found by** | inducing the gate, by hand, in the same cycle that wrote it |
| **Phase** | B1.4 |
| **Reproduce** | at the first draft of `scripts/cpp-cold-cache.sh`: `mkdir -p engine-cpp/build-ci && make cpp-ci` — green |
| **Invariant that caught it** | none. The induced-failure rule caught it: the gate was written, run, and did not fire |
| **Mutant class** | `COLD-fetch-despite-isolation` covers the *after* half; the *before* half is induced by hand, `mkdir engine-cpp/build-ci && make cpp-ci` |
| **Fix commit** | this one |

**Symptom.** The gate written to prevent HARNESS-002 recurring was created, wired, and induced — and
the induction printed `*** GATE DID NOT FIRE ***`.

**Root cause.** `cpp-ci`'s recipe ran `rm -rf $(CPP_BUILD_CI)` and *then* called the check. The check
asserted that the build root did not exist, immediately after the lane had deleted it. It was green
unconditionally, including in the exact state HARNESS-002 occurred in.

**Why this one is worth an entry despite never being committed.**

**It is the first time in either track that a general form recurred inside its own remedy.** GF-1 was
being written down, in the same working session, by someone with the sentence in front of them — and
the gate written to enforce it violated it one line later. The draft and the fix were
**indistinguishable by reading**. They were distinguished **in four seconds by running**.

That is the entire lesson, and it is larger than this gate. **The induced-failure rule is not a
formality applied to gates once they are written; it is the only thing that distinguishes a fix from a
fix-shaped edit.** Every other check in this repository — review, the general form itself, the author's
attention — passed this draft. One `mkdir` and one `make` did not. A rule you can state and still
violate one line later is a rule that needs a mechanism, and this is the mechanism.

**Fix.** The check no longer removes what it checks for — *a check that removes the thing it is
checking for is a check that cannot fail*. `cpp-ci` refuses when the build root exists and says how to
clear it; a successful run removes its own tree at the end, so the next run is cold; a failed run
leaves its tree for whoever has to debug it.

### HARNESS-005 — a pointer-keyed container in `TestEnv`, and the split labels refusing to absorb it

| field | value |
|---|---|
| **Found by** | implementing the scan rule that catches it (`A5-ADDRESS`), at B1.4 |
| **Phase** | B1.3, found at B1.4 |
| **Reproduce** | at `3239469`: `std::map<const void*, std::string> handles_` in `engine-cpp/src/env/test/test_env.cc` |
| **Invariant that caught it** | §6.1 — "nothing may depend on an address — no pointer-keyed containers, no address-ordered anything", which §9.4 says the scan checks |
| **Mutant class** | `A5-ADDRESS`'s fixture and blind patch; `A5-ADDRINT` was added at B1.5 for the arithmetic half the rule was missing |
| **Fix commit** | `187a3eb` |

**Symptom.** The first run of the new rule over `engine-cpp/src` reported a violation in code ratified
the previous cycle.

**Root cause.** `TestEnv` mapped an Env handle's address to its path, in order to know which file a
fault was being injected against. §6.1 bans that outright. **No behaviour was wrong**: the map is
looked up and never iterated, so no address ordering was ever observable.

**The disposition is the finding, not the defect.** The obvious move was a registry entry, and the
registry would not take it. `covered-by` requires naming an instrument that catches the class instead,
and nothing caught it. `unreachable` requires naming a detector that would have seen it if it could
occur, and it *did* occur — the logic was right there. **A taxonomy that refuses to absorb a defect is
doing its job.** With a free-text reason field the entry would have written itself: "looked up, never
iterated, harmless" — true today, unexaminable tomorrow, and indistinguishable from the seventeen
single-labelled opt-outs Track A spent a full cycle re-deriving and found three of them wrong. **One
label absorbing two meanings is how a real gap comes to look accounted for.**

So it was fixed rather than exempted, which is the only remaining option once both labels decline.

**A determinism win falling out of a hygiene fix.** `HandleId` replaced the pointer through the whole
Env surface: an integer assigned sequentially by the creating Env, so the same workload assigns the
same ids on every run and every machine. That makes a kill point reportable as
`Sync(handle 3, 000001.log)` rather than `Sync(0x7f9c4a005e10)` — a bug report instead of a number that
means nothing on the second run. §9.5 asks for exactly that and would have had to build it separately;
here it arrived as a consequence of obeying §6.1.

**What this would have caused.** Nothing, until someone iterated the map — at which point the fault
schedule would have depended on the allocator, and a kill-point sweep would have injected against
different files on different runs while reporting the same ordinals.

### HARNESS-006 — a prefix-granular torn `Sync` was classified as exactness-suspending

| field | value |
|---|---|
| **Found by** | writing B1.7b's discard test, which needed a torn `Sync` and found it marked the run non-evidence |
| **Phase** | B1.3, found at B1.7b |
| **Reproduce** | at `dfba754`: `SuspendsExactness(Injection::kTornSync)` returns true |
| **Invariant that caught it** | none — no lane could catch it. The classification decides what a run may be *banked* as, and every run it mislabelled still passed every assertion |
| **Mutant class** | `REGISTRY-lying-sync-not-suspending`, which existed and could not see this: it checks that members ARE members, not that non-members are not |
| **Fix commit** | this one |

**Root cause.** B1-D5 rules two things and they were collapsed into one. Prefix granularity — a kill
inside `Sync` promoting `content[0:k)` — is **the contract model**, the thing §7.4's two-element set
`R ∈ {G_{k−1}, G_k}` describes, and the engine is held to exactness under it. Only the *sector-subset*
mode, where an arbitrary set of 4 KiB sectors is promoted, suspends: that is a device violating fsync's
own ordering guarantee, and holding the engine to exactness there would report the engine for the
disk's crime. B1.3 implemented one injector and mapped it onto the suspending registry member.

**A NEW SHAPE: an unsatisfiable gate.** This does not belong beside the vacuous-green entries and is
not filed with them. Every one of those is a check that *could not fail*. This is a check that *could
not pass*. It survived four ratified steps because it was **conservative**, and conservative is the
direction no assertion notices: the engine behaved correctly, every test held, every lane was green,
and the only symptom was that a column would have stayed empty. `REGISTRY-lying-sync-not-suspending`
existed and could not see it — it is pointed at a member that stops suspending, and nothing was
pointed at a non-member that starts.

**What it would have cost, and where.** Not here. At B1.9a: §7.4 condition 3 requires that *both*
elements of the two-element recovery set were observed across the sweep, and runs that are
structurally uncountable as evidence can never satisfy it. **Found there, it presents as a bug in the
engine rather than in the classifier, so the debugging starts in the wrong component.** That is the
expensive part. The fix is four lines.

**The general form is GF-4**, and §7.5 of DESIGN-B1 cross-references it: the registry holding exactly
its two named members, asserted in both directions, is what closes this.

**THE AUDIT IT FORCED, and its result.** The same question was asked of every place in Track B that
decides whether a run counts as evidence. Six exist:

| function | decides | both directions asserted before the audit? |
|---|---|---|
| `SuspendsExactness` | whether a run is characterization-only | **no** — this entry |
| `OutcomeFloor` | the same, one layer up | **no** — only ever called with `true` |
| `OutcomeForCapVerdict` | whether a cap verdict is a pass, a void or a violation | **no** — `kNormal` never asserted |
| `IsDivergence` | whether a cap verdict fails the run | **no** — only the two true cases |
| `CountsAsRecoveryEvidence` | what may be banked | yes, all five kinds |
| `AggregateRuns` | whether runs may be banked *together* | yes, both regimes |

**Three more instances, all the same shape**, and two of them reachable: `OutcomeFloor` returning
`kCharacterizationOnly` unconditionally, and `OutcomeForCapVerdict` filing a normal run as `kVoid`.
Both make the evidence column permanently empty with nothing going red. Mutants `FLOOR-always-suspends`
and `VERDICT-normal-is-void` now exist for exactly those, and all six functions are asserted in both
directions.

**Fix.** The injector is split: `kTornSync` (prefix, the contract model, does not suspend) and
`kSectorSubsetTornSync` (an arbitrary sector left unpromoted, suspends). The registry now has exactly
the two members §7.5 names. The test asserts **both directions** — that the two members suspend and
that the prefix mode does not — because a classification asserted in one direction is the shape GF-2
already names.

### HARNESS-008 and HARNESS-009 — the other two evidentiary deciders, side by side

Found by the audit HARNESS-006 forced. Recorded together because **the shape is identical and the
consequences differ in a way worth seeing beside each other** — that pair is the argument for the
both-directions rule in two lines.

| | HARNESS-008 | HARNESS-009 |
|---|---|---|
| **where** | `rig::OutcomeFloor` | `rig::OutcomeForCapVerdict` |
| **the untested direction** | `OutcomeFloor(false)` — it had only ever been called with `true` | `kNormal` — the two divergences and `kVoid` were asserted, a normal run never |
| **the defect it admits** | suspend unconditionally | file a normal run as `kVoid` |
| **what that costs** | **every run becomes unbankable** | **the evidence column empties permanently** |
| **what turns red** | nothing | nothing |
| **mutant** | `FLOOR-always-suspends` | `VERDICT-normal-is-void` |

Both are GF-4. Both are conservative, which is why neither is visible: the engine is correct, the
arithmetic is correct, every assertion holds, and the only symptom is a number that never appears.
One kills the evidence at the source and one kills it at the sink, and **the pair is the reason the
rule is "both directions" rather than "test the interesting case"** — there is no interesting case
here, only two boring ones whose absence is invisible.

Closed structurally: `engine-cpp/DECIDERS.txt` enumerates every function that decides evidentiary
status, `scripts/cpp-scan.sh` requires each to name the tests asserting **both** of its directions, and
a decider that lands without them fails the lane. Six is a small enough population to enumerate, and
enumerating it is what stops the seventh arriving in B2 and the audit being re-run by hand.

### A shape three of this cycle's defects shared

**HARNESS-006, `HEADER-conditional`, and the near half of BM2's survival are all the same thing: a
check placed somewhere that something else decides whether it runs.** The exactness classification ran
off an injector enum that had collapsed two ruled cases into one; the FILE_HEADER validation ran inside
a loop bounded by whether a `GROUP_END` existed; the discard assertion ran on a workload where the
records it was checking never reached a file. In each, the check itself was correct and its *reachability*
was decided elsewhere — so it passed, and reported on a situation that had not occurred.

Worth one sentence rather than three entries, because the remedy is one habit: when writing a check,
ask what has to be true for it to run at all, and assert that too.

### The shape behind every mutant that has survived its first induction

**THIS IS A STANDING PATTERN, NOT A LIST OF INCIDENTS — AND B2'S LAST RUN BROKE THE RUN OF ONE
MEANING, WHICH IS BETTER THAN AN UNBROKEN ONE.** Six have survived a first induction:

| class | meaning | shape |
|---|---|---|
| `LEDGER-always-promoted` | #1, a checker that cannot see it | the test never created the situation it was checking |
| `BM2-accept-torn-tail` | #1 | " |
| `BM7-drop-close-error` | #1 | " |
| `BM1-ack-before-sync` | #1 | " |
| `BM52-current-parsed-leniently` | #1 | " |
| **`BM55-tables-oldest-first`** | **#2, a defence that was never there** | **the patch was aimed at a line a comment claimed was load-bearing and was not** |

**Five of the six share one sentence.** The sixth is the first of a different kind in this track, and
it matters that the tally now has two entries rather than one: a classification that only ever
returns one answer is a classification nobody has tested. `BM55` is what shows the three meanings are
doing work.

`BM55`'s own story is short and is recorded at the code. It reversed the order sources are handed to
`MergedIter`, and every test stayed green **correctly** — the merge orders by KEY, sequences are
unique, so there are no ties for source order to break and the order it is given is irrelevant. The
comment beside that line said otherwise, in words that are true of the *point-read* path a hundred
lines below. **A comment asserting a load-bearing property for a line where it is not load-bearing is
worse than no comment**: it is where the next reader looks for the invariant, and it sends them to a
line nothing depends on. The patch is re-pointed at the walk that carries the property, and the
comment is corrected, in the same diff.

**All six are meaning #1 or #2. MEANING #3 HAS NEVER OCCURRED IN TRACK B**, and that is worth stating
precisely rather than as reassurance, because it is the only meaning whose correct response is to
**delete** something.

> If meaning #3 never occurs, either **the code has no dead paths**, or **the classification cannot
> see them**. We do not currently know which.

Nothing here distinguishes those two, and no lane is pointed at the distinction. A coverage
instrument would be — B4's differential rig is the phase where one becomes affordable — and until
then the honest statement is that meaning #3 is a category with no observations, not a category shown
to be empty. **A tired reader reaching for it is reaching for the one answer this catalogue has never
had evidence for.**

> **The test never created the situation it was checking.**

`BM7` is the cleanest exemplar: a Close test that only ever ran a *successful* Close cannot distinguish
a propagated error from a swallowed one, whatever it asserts about the return value.

`BM1` is the subtlest, and it sharpened the rig. The oracle learned the engine's watermark **only from
a `Sync`'s return value** — and a killed `Sync` returns nothing, so an engine that advanced the
watermark before writing a byte was invisible: the premature value died with the process. The fix is
not a bigger test but a wider definition of the promise. **`DurableSeq()` is a durability claim like
any other**, so the rig now records every value it is ever told and holds the engine to the highest,
and the induction runs the failure through an fsync that *errors* rather than one that kills — so the
process survives to be asked.

`BM52` is the fifth and the plainest, which is why it is worth recording that the shape did not have
to be rediscovered. Eight malformed `CURRENT` bodies were each asserted to be refused, and each was —
**because the manifest it named did not exist**, so a lenient parse failed too, for a reason with
nothing to do with parsing. One line fixed it: put a real `MANIFEST-000001` in the directory, and
`MANIFEST-1\n` becomes a body a lenient parse *resolves*.

This is **§22.6c's discriminator rule arriving in C++ independently** — a check must be run in a state
where the thing it discriminates could actually differ — and it is cited rather than restated. It is
also the same family as GF-1, one level in: GF-1 is about a *lane* verifying an absence, this is about
an *assertion* verifying a distinction. Filed once here; individual survivals are not entries.

**B3.4 ADDS FOUR, AND ONE OF THEM IS THE FIRST INSTANCE OF MEANING #3.**

`BM73` removed `L1FileFor`'s check that the file the binary search found actually *contains* the key,
and **nothing failed** — a key in the gap between two files of the run makes the search return the
next file along, whose `Get` cannot find it either, so the answer is identical and only a filter probe
is wasted. **The line is a cost guard, not a correctness one.** That is `BM55`'s question answered the
other way: the property *"a range test decides containment"* IS load-bearing, in the compaction's
**input selection**, where getting it wrong resurrects deleted data — so `BM80` was written there and
`BM73` was **deleted**. GF-7, second instance, and the first time this catalogue has deleted a mutant
rather than re-aimed it.

`BM76` and `BM79` are meaning #1 again, and `BM79` is a **sub-form worth naming**:

| mutant | why it survived | what the fixture was really watching |
|---|---|---|
| `BM76` | the tombstone sat at the **top sequence**, kept by the watermark pin for an unrelated reason | the pin, called the drop rule |
| `BM79` | with no live snapshot the drop rule leaves **one version per key**, so no key can span a file roll | a situation that could not occur |

> **A MUTANT THAT SURVIVES BECAUSE ITS PRECONDITION IS UNREACHABLE IS NOT A WEAK MUTANT; IT IS A
> WORKLOAD THE SUITE NEVER RAN.**

That is a sharper statement of meaning #1 and it points at the fix rather than the symptom: `BM79`'s
test now holds forty snapshots so that a key HAS many surviving versions, which is a workload the
suite had never run and which B3.6 is about.

**`BM84` IS MEANING #3's SECOND INSTANCE, AND IT ANSWERED A QUESTION RATHER THAN FINDING A BUG.**
Ansh asked for the shape a future optimization will produce to be planted deliberately: *read `S` as
late as possible so it is as small as possible so more can be dropped.* It was planted, and it
**survived — correctly.** Both directions of `S` movement are safe (a release only over-keeps; an
acquisition lands above every sequence the inputs hold), so **the timing of the read does not carry
the correctness.** `pin_seq ≤ max(S)` does, and that is now a `RIFT_CHECK`.

> **A MUTANT PLANTED TO ANSWER A QUESTION IS WORTH PLANTING EVEN WHEN IT SURVIVES — BUT IT IS NOT
> WORTH KEEPING AS A CLASS THAT CAN NEVER FAIL.** Deleted, with the answer moved to the call site and
> to `DESIGN-B3` §1.3, which is where the next person to propose the optimization will look.

`BM82` is meaning #2 — **`BM55`'s question, asked again and answered the same way.** It removes
`Sync`'s claim on the single-caller guard and leaves `SingleCaller` itself intact, and it survived a
pair of tests that construct the guard **directly**:

> **THOSE TESTS PROVE THE GUARD WORKS. THEY DO NOT PROVE THE GUARDED PATH USES IT.** Two different
> claims, and the second is the one the enforcement rests on.

The fix is a test that **re-enters `Sync` from the promotion hook** — which fires inside `Sync`, on
the durable image changing — so the guard is claimed twice on **one thread**, deterministically. The
alternative, racing two real `Sync`s, would induce it only *probably*, and this catalogue does not
count a gate induced probably. The hook fires **once** on purpose: without that, a build with the
claim removed would recurse until the stack gave out, and **a death test cannot tell a guard firing
from a crash** — the mutant would have passed for the wrong reason.

**AND THE TALLY'S OWN INSTRUMENT MISREAD ONE.** `BM78` was recorded as a survival on its first
induction because the script looked for a failing test — and the kill is an **abort**, so the process
died before the summary printed. `FLOORS.txt`'s header already warns of exactly this, and the
`RIFT PARTIAL RUN` marker was already in the output being read. **The remedy existed and was not
looked for**, which is the standing provenance rule one level up: a signal read without asking what
its absence would look like.

**THE STANDING QUESTIONS, now that the tally has two meanings in it.** A survival is a fork, not a
verdict, and the two questions are different:

1. *What must be true for this assertion to run at all?* — assert that too. In all five meaning-#1
   survivals that question was the whole of the fix.
2. *Is the line this patch is aimed at actually the line that carries the property?* — `BM55`'s
   question, and the one nobody asks while a comment is answering it for them.

Reach for meaning #3 last. Nothing in this catalogue has been unreachable code, and it is the only
meaning whose correct response is to delete something.

### HARNESS-007 — `Slice` bound silently to temporary strings, and a test dangled

| field | value |
|---|---|
| **Found by** | AddressSanitizer, inside `make cpp-mutants`'s **baseline gate** |
| **Phase** | B1.7b |
| **Reproduce** | at `dfba754`, with `Slice(std::string&&)` still permitted: `Op op; op.key = Slice("k");` |
| **Invariant that caught it** | none by design. The baseline gate ran `cpp-asan` on the unpatched tree before reporting any kill, and refused to report |
| **Mutant class** | none, and none is added: the fix is a compile error, so there is no runtime behaviour left for a mutant to blind |
| **Fix commit** | this one |

**Root cause.** `Slice` had `Slice(const std::string&)` and no `const char*` overload, so `Slice("k")`
constructed a **temporary `std::string`** and pointed into it. The Slice outlived the full expression;
the buffer did not.

**Why the baseline gate is the entry.** The mutant lane refuses to report kills until the unpatched
tree passes every lane a patch names — a rule borrowed from `make blind` after a lane there reported
seven kills while one of the tests doing the killing was failing for its own reasons. Here it turned an
unattributable run into a named ASan stack trace.

**Defects found by the baseline gate while doing its actual job: 2** (HARNESS-001, HARNESS-007). The
count is kept because it is the argument for the gate. Its stated purpose is to make kills
attributable; what it has actually done twice is find defects nothing else was looking for, in a tree
that every other lane called green. A mechanism that keeps paying outside its stated purpose is worth
more than the purpose.

**Fix, and why it is structural rather than local.** `Slice(std::string&&) = delete;` makes binding to
a temporary a **build failure**, and a `const char*` overload makes the literal case point at static
storage. Twenty call sites had to hoist their strings into named locals; every one of them was a latent
instance of the same bug, safe only by accident of lifetime. A class of dangling-pointer bug became a
class of compile error.

### HARNESS-010 — the sweep could not see BM2, because the snapshot was hiding the damage

| field | value |
|---|---|
| **Found by** | measuring the sweep's power against every class, per GF-4's sibling discipline in §10.3 |
| **Phase** | B1.9b |
| **Reproduce** | apply `BM2-accept-torn-tail`, run `make cpp-sweep` before the post-reopen continuation existed: 175 points, 175 passes |
| **Invariant that caught it** | none. The **measurement** caught it: a class that should be sweep-detectable measured **0 of 175** |
| **Mutant class** | `FLOOR-continuation-removed`, which makes the regression repeatable |
| **Fix commit** | this one |

**Root cause.** With BM2 applied, recovery applies BATCH records past the last `GROUP_END` — records that were
never committed. They land in the memtable at sequences **above** the recovered watermark, and every read goes
through a snapshot pinned at that watermark, so **they are present and unreadable**. The oracle compares the
visible state, which is correct, and passes.

That is not a defence. It is an accident of the read path, and it expires: at **B2 the flush writes the memtable
out**, and uncommitted records become durable, visible and permanent. The engine would have shipped a recovery
path that quietly retains data it never promised, with a 175-point sweep reporting 175 passes.

**Why the measurement found it and no assertion could.** Every lane was green and correct. The sweep visited every
kill point, observed both elements of the recovery set, and reported no violation — all true. What was wrong was
its **power**, and power is not a property any single run can assert about itself. §10.3 exists for exactly this,
and it is the first thing it found.

**Fix.** The sweep now **continues after reopening**: one write, then a comparison. A reopened database keeps
serving, and the new write takes the sequence the hidden records already occupy, so they become visible at exactly
the moment a real database would have resumed service. BM2 went from 0 to **194 per mille**, first detected at
kill point 14.

**The floor that keeps it.** `BM2`'s rate floor is 90 per mille — roughly half the measurement, and set against
**the suppressed number rather than under today's**: the value that matters is the 0 this class measured before the
continuation existed. `FLOOR-continuation-removed` induces exactly that regression, and its control is
`cpp-sweep` **staying green** — the lane whose job is finding defects is perfectly healthy while its power has
collapsed.

### HARNESS-011 — TestEnv's ledger under-reported what a torn `Sync` promoted

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, on its first run with torn modes enabled, against the **unpatched** tree |
| **Phase** | B1.3, found at B1.9b |
| **Reproduce** | before the fix: a torn `Sync` whose prefix covers the whole newly covered extent records `promoted=false` |
| **Invariant that caught it** | the exactness oracle — it reported a violation at kill point 35 |
| **Mutant class** | none added: the ledger field is now written from the durable image itself, so there is no separate flag to blind |
| **Fix commit** | this one |

**Symptom.** `VIOLATION at ordinal 35 (kWritableFileSync, before effect): recovery landed on sequence 6, a batch
boundary strictly inside a group.`

**Root cause.** `promoted` was set by `RecordPromotion`, which only runs when `DoSync` runs. A torn `Sync` kills
*instead of* running `DoSync` — so however much of the extent it actually promoted, the ledger said it promoted
nothing. When the prefix happened to cover the entire group, durability really had advanced, and the oracle,
reading `promoted=false`, refused to offer the in-flight element of the recovery set and **reported the engine for
landing exactly where the ledger's own bytes said it should**.

**The lesson, and it is not the one it looks like.** Ruling 4 says an oracle that interrogates the engine believes
the lie. This is one level in: **a harness record that under-reports is as damaging as an engine that
over-reports**, and it is worse in one way — it blames the engine. The ledger is now written from the durable image
before and after the injection, so it records what happened rather than which code path ran.

---

### HARNESS-012 — the oracle's durability fact rested on there being exactly one file

| field | value |
|---|---|
| **Found by** | `make cpp-sweep` in the flush regime, against the **unpatched** engine, after BUG-B001 and BUG-B002 were fixed |
| **Phase** | B1.9a, found at B2.6 |
| **Reproduce** | before the fix: `rift_sweep flush` reports violations at kill points 149, 159, 167, 178, and `rift_sweep default` at 59 |
| **Invariant that caught it** | the exactness oracle reporting the ENGINE for landing on a group the ledger's own bytes said was durable |
| **Mutant class** | **ORACLE-facts-last-sync**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** `VIOLATION at ordinal 149 (kWritableFileSync, before effect): recovery landed on sequence
46, a batch boundary strictly inside a group.` The engine was right; the harness was wrong three
different ways in one line of code.

**Root cause.** `FactsFrom` computed `in_flight_durability_applied` as *the `promoted` flag of the
last `kWritableFileSync` entry in the ledger*. Every clause of that was true only by accident:

1. **the last file, not the WAL.** A group lives in the WAL. Until B2 the WAL was the only file this
   engine ever synced, so "the last Sync" and "the WAL's Sync" were the same entry. The flush syncs
   three — the table, the manifest, and the WAL — and reading the last one reports the *manifest's*
   durability as the group's.
2. **only a `Sync`, not any call that promoted.** A torn injection at a `Flush` promotes a prefix,
   and the promotion is recorded on the **Flush** entry. A filter that looked only at Sync calls
   reported "not durable" about bytes that were.
3. **the last one, not any one.** Durability is not undone. The flush creates a *second* WAL inside
   the same `Sync`, and that empty file's own sync promotes nothing — so the last `.log` entry says
   "not durable" about a group made durable moments earlier by the WAL being retired.

**Why all three survived B1.** Without scoping the question to *this* `Sync`, one successful Sync
answers for every group after it. That masked (1) and (2) completely: the last `.log` Sync in the
whole run was almost always a successful earlier one, so the fact was accidentally `true` exactly
when it needed to be. **Scoping made the harness strict, and strictness is what exposed the other
two.** Fixing one defect is what made the others visible — the reverse of the usual order.

**The lesson.** HARNESS-011 said a harness record that under-reports is worse than an engine that
over-reports, because it blames the engine. This is the same shape one level up: **a harness FACT
derived from "the last event of a kind" is a fact about the world having one of that kind.** The
question the oracle asks is "did *this* Sync make *the WAL's* in-flight group durable"; the code now
asks exactly that, scoped, filtered, and monotone.

---

### HARNESS-014 — a registry cross-check that matched nothing, and would have passed forever

| field | value |
|---|---|
| **Found by** | `make cpp-scan`, on the first run of the rule it was part of |
| **Phase** | B3.0a, found the day it was written |
| **Reproduce** | before the fix: mark a file `RIFT_ORACLE`, leave it out of `ORACLES.txt`, and the scan says nothing |
| **Invariant that caught it** | the rule's own both-ways check, which fired on the OTHER direction and exposed this one |
| **Mutant class** | the four inductions in `B3.0a`'s commit, each observed and restored |
| **Fix commit** | `3e6d2c0` |

**Symptom.** `ARTIFACTS.txt` and `ORACLES.txt` were built, the registry cross-check was written, and
it **matched nothing** — every lookup fell through to the "not registered" branch. Two defects, and
only the first is interesting.

**Root cause 1, and it is a VACUOUS CHECK rather than a script bug.** The registry lists were built
with `grep | awk` and matched with `case " $list " in *" $item "*`. The lists are **newline**
separated and the pattern needs **spaces**, so no item ever matched. Had the check been written in
the direction that passes on no match, **it would have passed forever, on every tree, reporting a
boundary it was never testing.** It failed loudly here only because this particular direction reports
on *absence* of a match — an accident of which way round it was written, not a property of the check.

That is `GF-1`'s family at the level of a registry: *a check that cannot distinguish "nothing
violates this" from "I compared nothing" is not a check.* The remedy is the same one `GF-1` names —
run it where the thing could be present, and assert that it saw something. `cpp-scan` now prints
`parses 10 artifact(s)`, which is a count, and **a count nobody asserts is decoration**: it is
asserted by the four inductions.

**Root cause 2, recorded because it is cheap to record and expensive to re-find.** The new code used
a variable named `found`, which is part 3's `mktemp` path. Clobbering it made two *unrelated*
registry entries report as stale. **The lane doing to itself exactly what it does to the tree**, on
its first run.

**What it cost.** Nothing, because the rule was induced in four directions before being trusted —
which is the only reason either defect surfaced on day one rather than at the gate.

---

### HARNESS-013 — the mutant lane waited eleven and a half hours for a lane that was never going to report

| field | value |
|---|---|
| **Found by** | reading `ps` while answering "how long will the catalogue take" |
| **Phase** | B1.3 (the lane), found at B2's close |
| **Reproduce** | before the fix: `make cpp-mutants` on a tree where `BM35-tag-sorts-ascending` is reached; it never returns |
| **Invariant that caught it** | none — **nothing was watching**, which is the entry |
| **Mutant class** | none can be added: a permanent catalogue member that hangs would hang the catalogue. The mechanism was induced with a throwaway patch that makes a lane `sleep`, and the watchdog was seen to fire, report TIMEOUT and fail the lane |
| **Fix commit** | this one |

**Symptom.** The catalogue sat at `control BM35-tag-sorts-ascending: cpp-build still alive, as it
must` for **11 hours 34 minutes**, with `rift_engine_test` in that mutant's scratch tree burning
**690 minutes of CPU at 99.3%**. Nothing was wrong with the machine and nothing was logged. I read the
log's last line, saw a lane in progress, and gave an estimate for the remaining patches built entirely
on the assumption that progress was what I was looking at.

**Root cause, two halves.**

*The engine half.* `BM35` inverts the tag half of the internal key order. `IterImpl`'s advance and
retreat loops carry comments reading *"strictly advances, so this loop terminates"* — an invariant
that rests **entirely on the comparator being the order it claims to be**. Invert the comparator and
the loops no longer terminate. `Flush.ReadsSeeTheMemtableAndTheTablesTogether` is where it spins,
because that is the test with a backward scan over a merged view.

*The lane half, and it is the one that matters.* `run_lane` was `( cd "$1" && $MAKE "$2" ) >"$3" 2>&1`
with **no timeout**. A mutation that makes a lane HANG is neither a kill nor a survival: the lane
never reports. So the catalogue waits, forever, and **a waiting catalogue is indistinguishable from a
working one**.

**The fix, both halves.** The loops now `RIFT_CHECK` the progress their comments assert — the user key
is compared bytewise, which is a property of the merged order that does *not* depend on the tag
comparator, so the assertion can catch the comparator being wrong. `BM35` now aborts in **0 seconds**
instead of spinning for eleven hours. And `cpp-mutants` and `cpp-campaign` grew a per-lane watchdog
that kills the whole process tree and reports **TIMEOUT** as a distinct outcome, counted as broken,
failing the lane.

**THREE DEFECTS INSIDE THE REMEDY, ONE INTERACTION, ONE ENTRY.** Naming it once:

> **AN EXPECTED NON-ZERO EXIT UNDER `set -e` KILLS THE SCRIPT THAT WAS SUPPOSED TO INTERPRET IT.**

| # | where | what it did |
|---|---|---|
| 1 | `run_lane`'s `wait` | killed the hung lane correctly, then **died itself, printing nothing** |
| 2 | `run_lane`'s two call sites | the function returned 124 and `; rc=$?` never ran |
| 3 | `build_and_sweep`'s sweep call | **killed the campaign at its first class** |

**The third is the worst, and for the reason the first two are not.** A patched sweep exits non-zero
**by design** — that non-zero *is* the detection the campaign counts — so the failure lands at the
first class **with both baselines already printed and the log reading healthy**. It is the same tell
as the entry itself: a run that has stopped, wearing the appearance of one that has not.

The first two took two rounds of induction to see, because round one looked like *"the lane stopped"*,
which is what the watchdog was supposed to produce. **A watchdog that cannot report is the defect it
was written to fix** — GF-1 recurring inside its own remedy, for the second time in this track.

**The general form, and it is the sharpest one this project has produced about lanes rather than
code:**

> **A HANG IS NOT A FAILURE, AND EVERY LANE MUST BE ABLE TO DECIDE.** A lane reports pass or fail; a
> lane that can do neither has stopped being a lane, and it stops *silently*, wearing the appearance
> of work. This is Amendment A4's "inconclusive is a first-class outcome" arriving one level out: A4
> is about a checker that ran and could not conclude, and this is about a checker that never
> finished. Both must be named, neither may be waited on, and neither is evidence.

**THE SPECIFIC TELL, AND IT IS WHAT COST THE ELEVEN HOURS.** *A stalled log is indistinguishable from
a slow one from outside.* Both show a last line and no error. So **progress must be read from
something that ADVANCES — a counter, a timestamp, a CPU-time delta — and never from the absence of an
error.** Every estimate given during this run was arithmetic over an appearance of progress, and the
appearance was the whole of the evidence.

**THE SHARPER OF THE TWO ENGINE-SIDE FIXES IS THE `RIFT_CHECK`, AND IT IS SHARPER FOR A REASON WORTH
STATING SEPARATELY.** The loops carried comments reading *"strictly advances, so this loop
terminates"* — and that invariant rested **entirely on the comparator being the order it claims to
be**, which is exactly the thing the mutant changes.

> **A TERMINATION ARGUMENT THAT ASSUMES THE THING BEING MUTATED IS NOT A TERMINATION ARGUMENT.**

What replaces it is a progress property that does *not* depend on the comparator under test: the user
key is compared **bytewise**, which is a fact about the merged order rather than about the tag half
of it. So the assertion survives the comparator being wrong and can catch it being wrong — which a
check written in terms of the comparator could not.

**What it cost.** Eleven and a half hours of wall clock and one confidently wrong estimate given to
Ansh. What it did not cost: any result. The 30 patches that completed before the hang all reported
correctly, and the campaign that ran before it was green.

---

### The baseline gate's running tally — four defects, none of them what it was built to detect

The mutant lane's baseline gate exists for one reason: **every lane a patch is declared against must
pass on the UNPATCHED tree first**, because a red baseline makes every subsequent failure
unattributable. It has now found **four defects, and not one of them was an unattributable kill.**

| # | phase | what it caught |
|---|---|---|
| 1 | B1.0 | `HARNESS-001` — `tar --exclude` silently dropped three files from every scratch copy |
| 2 | B1.9a | `HARNESS-007` — a `Slice` bound to a temporary string, caught by ASan in the baseline run |
| 3 | B1.9a | the `-Werror` failures six direction controls separated from real kills |
| 4 | B2.7 | `HARNESS-013`'s third `set -e` defect — `cpp-campaign` red on the unpatched tree |

**The argument this is evidence for: GATES THAT CHECK PRECONDITIONS BEAT GATES THAT CHECK OUTCOMES.**
A gate on the *outcome* can only find the failure it was written to look for. A gate on the
*precondition* — "is this measurement even attributable?" — runs the whole machine in a known-good
configuration on every invocation, and so finds whatever is wrong with the machine, including the
things nobody thought to look for. Four for four, none of them the thing it was built to detect.

---

### A category worth watching: shell-dialect assumptions, not logic errors

Both defects in CF-2's own execution were **assumptions about which shell was running**, not mistakes
in reasoning:

| where | the assumption | what it produced |
|---|---|---|
| the labelling script, invoked from **zsh** | that `sh label.sh $IDS` word-splits an unquoted parameter — **zsh does not** | all 28 names arrived as ONE argument; the loop ended after a single `ROT` line, and *"1 of 28 done"* read as slow progress |
| `cpp-scan` part 6, running under **`sh`** | that `<(...)` is available — it is a **bashism** | the check printed its own heading and then died with a syntax error: **a lane that looks like it ran** |

**This is a category now, because the lanes are written in three dialects.** The `Makefile`'s recipes
and every `scripts/*.sh` run under POSIX `sh`; the shell these are authored and tried out in is
`zsh`; and `awk` is a fourth language inside `cpp-scan-rules.awk`. **A construct that works when
tried interactively may not run in a lane, and the reverse.**

**WHICH MECHANISM CATCHES WHICH, RECORDED EXPLICITLY, BECAUSE A LINTER THAT CATCHES ONE OF TWO
DIALECT FAILURES WILL BE TRUSTED FOR BOTH:**

| defect | `sh -n` | the provenance rule | why |
|---|---|---|---|
| `<(...)` under `sh` | **CATCHES** | — | it is a **syntax** error; the parser refuses it without running anything |
| `$IDS` unsplit in zsh | **CANNOT** | **CATCHES** | `sh label.sh $IDS` is **syntactically perfect**. It is semantically empty, and no parser can know that the caller meant 28 arguments |

**So `sh -n` is a defence against exactly one of these two, and the temptation is to treat a green
`sh -n` as covering "shell problems".** It does not cover the class where a construct is valid in
both dialects and *means* something different in each — which is the more dangerous class, because it
produces a running program that does the wrong thing rather than one that refuses to start.

For that class the defence is the standing provenance rule: **read progress from something that
advances.** The process not existing is what said "1 of 28" was not slowness.

---

### The standing rule: a signal read without its provenance

**Six instances now, and they are listed rather than counted so the number is checkable.** Each is a
signal that was read as if it meant one thing while its provenance made it mean another — and in
every case the misreading was *indistinguishable from the correct reading* without going and looking
at where the number came from.

| # | instance | the signal | what its provenance made it mean |
|---|---|---|---|
| 1 | `HARNESS-010` | BM2 detected at 0 per mille | not "the defect is unreachable" but "the snapshot was hiding the damage" |
| 2 | `HARNESS-012` | the last `Sync`'s `promoted` flag | not "the group is durable" but "some file's sync promoted, and until B2 there was only one file" |
| 3 | `HARNESS-013` | a log whose last line is not an error | not "still working" but *a stalled log is indistinguishable from a slow one* |
| 4 | `HARNESS-014` | a cross-check reporting no violations | not "nothing violates this" but "I compared nothing" |
| 5 | `GF-6` | a detection rate that fell | not "power was lost" but "the denominator grew into territory where the class is undetectable" |
| 6 | **the truncated suite, B3.1** | **no `DropCheck` test failing under either reader mutant** | not "the checker cannot see fabrication" but **"an earlier `RIFT_CHECK` killed the process before those tests ran"** |
| 7 | the labelling run, B3.1 | *1 of 28 done* | not "slow" but **"dead after one, and a rate computed from it claimed 396 minutes for a run already over"** — the THIRD rate computed over an appearance |
| 8 | `ORACLE-includes-engine`'s label | a scan reporting **NONE** | not "the rule no longer catches it" but **"my `grep` pattern did not match the line the rule printed"** — the rule catches it, and a mutant whose target moves is **re-pointed, not deleted** |

**The sixth nearly produced the opposite ruling.** The aliasing condition would have been reported as
**unacceptable** — requiring the rig to grow its own parser — on a zero that was an artifact. The
form it takes is the same as the third:

> **A TEST BINARY THAT ABORTS REPORTS FEWER FAILURES THAN EXIST, AND FEWER FAILURES REPORTED IS
> INDISTINGUISHABLE FROM FEWER FAILURES EXISTING.**

**The mechanical answer, and it is cheap: PUT THE PROVENANCE IN THE SIGNAL.** `RIFT_CHECK`'s failure
path now prints

```
*** RIFT PARTIAL RUN: aborted here, so any count above this line is a LOWER BOUND
    and any absence is unproven ***
```

so a count grepped out of that output **carries the fact that it is partial**. Induced against
`BM35`, which aborts. The same principle covers instance 3 — read progress from something that
*advances* — and instance 4, where the lane now prints `parses N artifact(s)` rather than nothing.

**The rule.** *Before reporting what a number means, establish that the run which produced it
completed, that the comparison it summarises actually compared something, and that its denominator is
the one the previous measurement used.* Three different questions, one shape: **a signal is not
evidence until its provenance is.**

---

### HARNESS-021 and HARNESS-022 — the measuring instrument's own two failures

Filed together because they are one instrument's two ways of lying, found hours apart, **and because
the second was only findable after the first was fixed.**

| field | value |
|---|---|
| **Found by** | inspection of the printed numbers, both times — **not by any instrument** |
| **Phase** | B3.7b, before the number was published |
| **Mutant class** | `BM110` preserves the first, `BM111` the second |
| **Fix commit** | `19f1d45` (both), classes at `e70951a` |

**HARNESS-021 — it returned zero where it should have returned bytes.** Write amplification is bytes
written over bytes submitted, and the harness summed `LedgerEntry::durable_bytes_after` over Append
calls. That field is **the size of a file after a Sync has promoted it**, and is left at zero for an
Append. The first run printed **write amplification 0.00**.

> **IT ANNOUNCED ITSELF ONLY BECAUSE ZERO CANNOT BE TRUE.** A field that returned a plausible-but-wrong
> number in the same slot — a partial count, a stale size — would have been **published in
> `BENCHMARKS.md` as the result that decides `B3-D3`.** The instrument was saved by the magnitude of
> its own error.

**Fix:** the ledger records `append_bytes` **at the call**, and `MeasureAmplification` `RIFT_CHECK`s
the sum is non-zero — so the class cannot return quietly.

**HARNESS-022 — it printed a number without the condition it was true under.** A workload that stops
with L0 partly full **has not paid for those files' compaction**, so its write amplification reads
*low*. That is precisely the direction that flatters the conclusion `(b)` was being measured for.

**And it was found only after HARNESS-021 was fixed**, because until then the number was `0.00` and
there was nothing to be suspicious of. **A broken instrument hides the questions you would ask about a
working one.**

**Fix:** `L0 left` is a printed column, and the conclusion states the caveat when it is non-zero.

> **A NUMBER WHOSE CONDITIONS ARE NOT PRINTED BESIDE IT INVITES THE READER TO ASSUME THE BEST ONES.**

**WHAT BOTH HAVE IN COMMON IS WHO CAUGHT THEM.** Neither was caught by a test, a lane, a mutant or a
checker — **both were caught by reading the output**, which is the least reliable instrument this
project has and the only one that was pointed at the measurement. That is `GF-26` one level over, and
it is why `BM110` and `BM111` exist: the number that decides a design question now has classes under
it, so the next such failure fails a lane instead of depending on someone noticing.

**The counterfactual, since it is what makes the argument concrete:** had `HARNESS-021` produced 4.2
instead of 0.00, `BENCHMARKS.md` would today carry a wrong write-amplification curve, `B3-D3` would
have been ruled on it, and nothing in the repository would disagree.

---

### HARNESS-020 — a test corrected an assumption the author had asserted

**Symptom.** `FileLifetime.AnOpenIteratorHoldsItsInputFilesToo` failed with `expected 200, got 50`.

**Root cause: the expectation, not the engine.** An `Iterator` captures its `Version` and its sequence
**when it is created**, so it sees the database as of that moment — the 150 keys written afterwards
are invisible to it by construction. **50 was right.** I had written 200, having assumed an iterator
tracks the live database.

**Why it is worth an entry rather than a silent fix.** The frozen interface says what an iterator is,
and I asserted the opposite **in a test I was writing to prove something else**. Had the engine
happened to behave that way, the test would have passed and **encoded a false claim about the frozen
contract** in the file a future reader consults for what iterators do.

> **THE FIXTURE-FIRST ORDERING CAUGHT THE AUTHOR RATHER THAN THE CODE**, which is the case for it that
> is easiest to forget: it is usually argued as *"a checker written afterwards agrees with the
> implementation"*. This is the other direction — **a checker written first disagrees with the
> author**, and the author is who was wrong.

**Related but distinct from `HARNESS-006`'s family.** There, a checker wrong in the direction that
sends debugging to the wrong component. Here the checker was *right* and my expectation of it was
wrong — which is cheaper, and only because the engine did not share my misunderstanding.

**Fix.** The expectation is 50, with the reason at the assertion, and the comment says what the first
version was asserting: *that an iterator is live rather than snapshotted, which is not what the frozen
interface says.*

---

### HARNESS-019 — the revert that ate a step, after its own entry had been written

**Symptom.** Five mutant patches reported `patch does not apply` at once, and B3.5e's uncommitted
source work was gone: the exclusive-bound derivation, the tombstone-carrying compaction, the roller's
split, the run check's move onto opened tables. Rewritten from scratch.

**Root cause.** The induction helper reverted a patch with `git checkout -- engine-cpp/src`, which
reverts **everything uncommitted under that path** — not the patch it had applied.

**AND THAT IS EXACTLY `HARNESS-016`'s SECOND INSTANCE, ALREADY RECORDED.** The entry says it: *"one
reverted a directory to undo a patch."* When it first fired it cost one comment. It had a written
entry, a named general form — *a helper's side effect must be no wider than its purpose* — and a
diagnosis. **What it did not have was a fix in the tool.**

> **AN ENTRY THAT FIXES THE RECORD AND NOT THE TOOL SCHEDULES THE SAME DEFECT AT A LARGER SIZE.** The
> second firing was not a new lesson. It was the same lesson, charged at the size of the work in
> flight.

**Fix, in the tool this time.** `scripts/cpp-induce.sh` reverts with **`git apply -R`** — the exact
inverse of the apply, whose side effect is no wider than its purpose — and **refuses to run at all on
a dirty tree**, because an induction reads which assertion fails, and on a dirty tree that answer is
about a tree nobody will build again. It also reports an **abort** as a kill rather than as a
survival, which `FLOORS.txt`'s header has warned about since B3.1.

**The general form is `GF-20`'s sibling and belongs beside it.** `GF-20`: correctness resting on a
moving premise is a scheduled defect. This one:

> **A DEFECT WHOSE REMEDY IS "REMEMBER NOT TO DO THAT" IS A SCHEDULED DEFECT TOO.** The remedy has to
> live somewhere that cannot forget.

**What it cost, stated plainly:** one step's uncommitted work, rewritten from the design and the
tests, which is the second time this session that a tool's blast radius exceeded its job. The first
cost a comment; this cost an afternoon. There is no third.

**THE GENERAL FORM, AND IT IS `GF-20`'s SIBLING:**

> **WHEN A DEFECT'S REMEDY IS WRITTEN DOWN RATHER THAN BUILT, THE REMEDY HAS THE DEFECT'S OWN SHAPE
> AND COMES DUE ON THE DEFECT'S OWN SCHEDULE.**

`GF-20` says correctness resting on a moving premise is a *scheduled defect*. This says a **remedy**
resting on someone remembering is one too — and worse, because the entry reads as closure. The
catalogue said *a helper's side effect must be no wider than its purpose*, named the instance, and
left the helper unchanged. The next firing was not a new lesson; it was **the same lesson, charged at
the size of the work in flight.**

**The test: after writing an entry, ask what would have to change for the second instance to be
impossible rather than merely recognised.** If the answer is "nothing — I would notice", the entry is
not finished. Filed as `GF-23`.

---

### HARNESS-018 — a temporary bound to a `const std::string&`, and every Slice into it dangling

**Symptom.** A fixture asserted a tombstone's start was `"m"`. It read back `"e"`, and the block's
`ok()` assertion had already **passed** — the parse succeeded, the counts were right, and one field
held a byte from somewhere else.

**Root cause.** The test called `Check(UnboundedBlock("m", DelTag(9)), &t)`. `UnboundedBlock` returns
a `std::string` **by value**; `Check` takes `const std::string&`. The temporary lives to the end of
the full expression and then dies — and `RangeTombstone::start` is a **`Slice` into that block**. Every
field of every parsed tombstone pointed into freed memory by the time the assertions ran.

**What it establishes about `HARNESS-007`'s fix, and this is the entry.** B1 deleted
`Slice(std::string&&)` so a `Slice` could not bind directly to a temporary. That closes **direct**
binding and **cannot close binding through a parameter**: the temporary here never touches a `Slice`
constructor at all — it binds to a `const std::string&`, which is legal, ordinary, and exactly what
every by-const-reference API in the codebase accepts.

> **THE DELETED CONSTRUCTOR NARROWS THE CLASS; IT DOES NOT ELIMINATE IT.** The residual is: a
> temporary bound to a reference parameter, from which a `Slice` is later derived. No overload
> resolution sees that, because by then the temporary is a named reference like any other.

Stated the way §3.2.1 states the NVI choke point's residual — *"the claim is therefore 'bypassing
requires defeating two independent checks in one diff', not 'bypassing is impossible', and the second
sentence would be false."* The claim here is **"a `Slice` cannot bind directly to a temporary"**, not
**"a `Slice` cannot outlive its bytes"**, and the second sentence would be false.

**What covers the residual, honestly.** Nothing mechanical. ASan catches it *when the freed byte
differs* — it did not fail here, because the read landed in a still-mapped allocation. What caught it
was **an assertion on the parsed content rather than on the verdict**: the test checked what the
tombstone *said*, not merely that parsing succeeded. That is the standing habit worth having, and it
is the same habit `GF-2` demands of two-field assertions.

**Fix.** The three new fixtures bind their block to a local, with the reason written at the first one
so the next person copying the pattern copies the binding too.

---

### HARNESS-017 — a delimiter with no escape, caught by luck in the one column that is validated

**Symptom.** The mutant campaign refused its own baseline with
`BAD BM85-range-block-is-not-last: unknown regime "determined at B3.5b by induction"` — a complaint
about the wrong column entirely.

**Root cause.** `FLOORS.txt` is pipe-delimited with **no escape**, and the campaign parses it
**positionally**. The row named the guard it dies to by quoting the C++ verbatim:

```
killed-by-guard: ... RIFT_CHECK(range_offset == 0 || offset_ == range_end)
```

**The `or` operator is the column delimiter, doubled.** One field became three, every column shifted
right by two, and the row still looked like a row — same shape, same leading class name, plausible
text in every position.

**WHY IT WAS CAUGHT, AND IT IS NOT A REASON TO RELAX.** The displaced value landed in `regime`, which
is validated against a known set (`default` / `flush`). Two columns further along and it would have
**parsed cleanly**:

> **A DELIMITER WITH NO ESCAPE IS CAUGHT BY LUCK IN ANY COLUMN THAT IS NOT VALIDATED.** In this file
> the unvalidated columns include `covered-by:` — the one field a reader consults before deleting an
> assertion, and the field `GF-7` already established is worse wrong than absent.

**Fix, and the bound is an UPPER one.** `cpp-scan` part 6 now refuses any row with **more than seven
fields**. Not *exactly* seven: the file's own header says trailing columns may be omitted — *"an
absent column means default"* — and 5, 6 and 7 field rows all exist and are legal. **Demanding
exactly seven would have been a checker that refuses the normal case in the name of the abnormal
one**, which is the inversion §5.4 rejected candidate (a) for. Nothing legitimate produces more than
seven; a doubled delimiter produces nine.

Induced both ways against a row carrying `RIFT_CHECK(a == 0 || b == 1)`, and every one of the 123
existing rows audited.

**The general shape, and it is not about pipes.** A positional format with no escape puts the burden
on every future writer to know which characters are structural — and the writers here are humans
recording a finding, at the moment they are thinking about the finding rather than about the file.

---

### HARNESS-016 — a helper's side effect wider than its purpose, three times in one step

**Symptom.** A compaction test read the manifest to count tables per level, and the engine's next
manifest append failed: `kIoError: appending to a vanished file: db/MANIFEST-000001`.

**Root cause.** The helper called `sst::Manifest::Open`. That is not a reader — **every Open rotates**:
it replays, writes a NEW manifest, installs a new `CURRENT`, and **deletes the one it replaced**. The
test destroyed the live manifest underneath a running engine.

**The point is that this was already written down, in the other direction.** `manifest_format.h`
exists because `manifest.h` failed B3-D2a's artifact mark on `Manifest::Open(Env*, ...)` — *"opening a
manifest is AN ACT WITH AN OPINION about what the current state is, and it verifies, rotates and
installs."* The rule was applied to oracles and not generalised.

> **THE ARTIFACT/BELIEF SPLIT IS USUALLY ARGUED AS A RULE ABOUT WHAT A VERDICT MAY REST ON. IT IS
> ALSO A RULE ABOUT WHAT AN OBSERVATION MAY COST.** A path with an opinion has side effects, and a
> test that observes through one is running the engine, not watching it.

**Fix.** `rig/manifest_image.h` — a pure replay of manifest bytes, no Env, no rotation, no install.
The oracle's private copy was folded into it, so there is one parse rather than two, and the test now
reads `CURRENT` and the image it names.

**A SECOND INSTANCE, THE SAME HOUR, IN A THROWAWAY SCRIPT.** The helper that applies one mutant,
reads the failing assertion and reverts undid the patch with `git checkout -- engine-cpp/src` — which
reverts **everything uncommitted under that path**, not the patch it applied. It silently discarded an
unrelated comment written minutes earlier. The comment's absence then shifted the context lines of a
mutant patch generated against it, and the next lane run reported that patch as **`ROT` — "the code
moved and the mutation did not."** The lane was right, and it named the situation exactly.

> **A HELPER'S SIDE EFFECT MUST BE NO WIDER THAN ITS PURPOSE.** Both instances are the same shape:
> one observed through a path that rotates, one reverted through a path that reverts a directory. In
> both, the wider effect was invisible at the call site and showed up as something else entirely — a
> vanished file, a rotten patch.

**A THIRD INSTANCE, IN THE REMEDY FOR THE SECOND.** The patch generator built its diff with
`git diff`, which compares against **HEAD** — so on a dirty tree it silently bundled every unrelated
edit into the mutation. Two patches were written that way and **both carried six hunks instead of
one**; both came back `ROT`. The generator's job is to describe one mutation, and its scope was the
whole working tree.

> **A HELPER'S SIDE EFFECT MUST BE NO WIDER THAN ITS PURPOSE.** Three instances in one step: one
> *observed* through a path that rotates, one *reverted* a directory to undo a patch, one *diffed*
> against HEAD to describe an edit. In every case the wider effect was invisible at the call site and
> surfaced as something else — a vanished file, a rotten patch, a rotten patch again.

**A FOURTH, AT B3.5c, AND IT IS THE SAME FAMILY WITH THE DUPLICATION ON THE OTHER SIDE.** The
torn-record test runs one workload **twice** — a probe to find the sync ordinal, then the killed run
that tears at it. Converting the test from a `DeleteRange` expansion to a large batch, I changed the
**probe's** workload and left the killed run issuing the old one. The recorded ordinal then named a
different Env call, and the kill never fired.

> **TWO COPIES OF A WORKLOAD THAT MUST MATCH BYTE FOR BYTE IS THAT BUG WAITING.** Written once now,
> as `FillBigBatch`, with the reason at the definition rather than at the call sites.

**AND IT FAILED LOUDLY BY LUCK, AGAIN.** The divergence happened to make the kill not fire at all, so
the test failed on `expected kKilled, got ok`. **A divergence that still produced a kill — at a
different ordinal, tearing a different record — would have passed**, and the test would have gone on
reporting that a multi-block record is discarded whole while tearing something else entirely. That is
the second time in this step that a defect announced itself only because the corruption happened to
land somewhere validated (see `HARNESS-017`).

---

The generator now diffs **file against file**, with the reason at the top of it.

Recorded because the second and third cost only regenerated patches **only because the lane already
had a `ROT` outcome to report them as** — *"the code moved and the mutation did not"*, which is
exactly what happened. Without that outcome they would have presented as mutants that mysteriously
stopped applying, and the debugging would have started in the patches.

---

### HARNESS-015 — the registry cross-check matched a file that says it is NOT an oracle

**Symptom.** `rig/image_fixture.h` — a constructor, not a judge — was reported as `carries RIFT_ORACLE
and is not in ORACLES.txt`. Its header line reads: *"it CONSTRUCTS; it never judges, so it carries no
RIFT_ORACLE marker."* The check was a substring `grep`, so **a file could not say what it is not.**

**Root cause.** The marker was treated as an *occurrence of a token* rather than as **a declaration**.
Every real oracle already declares it identically: `// RIFT_ORACLE` as the file's **first line**.

**Fix.** Both directions of the cross-check now read `head -1 | grep '^// RIFT_ORACLE'`. Induced in
all three directions before it counted: an unregistered file declaring it (BAD), a registered file
losing it (BAD), and a prose mention below line 1 (clean — `image_fixture.h` itself, live in the
tree, is the standing witness).

**The pair with HARNESS-014 is the point.** That one **matched nothing** and would have passed
forever; this one **matched too much** and failed loudly. Same instrument, opposite failures, and
only the loud one is self-announcing.

> **A REGISTRY CROSS-CHECK HAS TWO FAILURE MODES AND ONLY ONE OF THEM TELLS YOU.** Both directions
> get induced, or the quiet one is what you have.

---

### BUG-B008 — the cgo boundary rejected a value the model accepts, because one helper served two call sites with opposite requirements

| | |
|---|---|
| **Symptom** | `panic: store: node 3 cannot apply range 1's committed batch: riftcgo: invalid argument`, on the first raft seed the Track A stack ran against the C++ engine. |
| **Found by** | I1, running the stack on `riftcgo` — the first time the two halves ran as one system, which is what I1 is for. |
| **Reproduce** | `engine.NewBatch().Set([]byte("k"), nil)`: `engine/model` accepts it, `riftcgo` returns `invalid argument`. Isolated in three lines before anything was changed. |
| **Invariant that caught it** | none — an `Apply` error, surfaced by `store/node.go`'s refusal to continue past one. |
| **Mutant class** | new, and it must plant the *mapping* rather than the symptom: a patch pointing `Set`'s key and value back at the bound mapping, covered by a directed test applying a nil-valued batch through the cgo path. |
| **Track** | **Track B** (`engine/riftcgo`), found by Track A's stack. B5 is signed; this is a defect in signed work and was reported as one. |

**The mechanism.** `bytePtr` maps a nil slice to a **null pointer**, and `rift.cc` says why, deliberately:

> *"there is no way to pass 'unbounded' as bytes, so the distinction has to live in the pointer."*
> `BoundOf(p, n)` returns `Bound::Unbounded()` when `p == nullptr`.

That is correct and load-bearing for `DeleteRange` bounds and iterator bounds. **`Set` and `Delete`
used the same helper**, and `rift_batch_set` refuses a null value with `RIFT_INVALID_ARGUMENT` —
because for a key or a value there is no unbounded case for null to mean.

> **A MAPPING THAT IS CORRECT AND LOAD-BEARING IN ONE PLACE IS A TRAP IN ANOTHER, AND ONE HELPER
> SERVING TWO CALL SITES WITH OPPOSITE REQUIREMENTS IS HOW THE TRAP GETS SET.** The bound case *needs*
> nil to mean something. The value case needs it to mean nothing. `bytePtr` could only do one.

**It is `BUG-044`'s shape one layer down**, and the pair is worth reading together: there, the frozen
contract's non-blocking `Apply` was correct and made the naive harness wrong; here, the boundary's
null-is-unbounded convention is correct and made the batch path wrong. **Both times the guarantee was
right, documented, and load-bearing, and the defect lived in the joint.**

**WHY THE DIFFERENTIAL NEVER FOUND IT, which is the more valuable half.** `engine/differential` exists
to catch exactly this — two engines disagreeing on one input — and it has run continuously since B4.
Its generator:

```cpp
op.value = std::string(1 + rng.Below(40), 'v');   // engine-cpp/rig/differential_driver.cc:78
```

**A value is always 1 to 40 bytes. A zero-length value is outside the input space entirely**, so the
nil/empty distinction — the one distinction Go makes here and C does not — was never presented to
either engine.

> **AN ORACLE COMPARING TWO IMPLEMENTATIONS IS BOUNDED BY THE INPUTS IT GENERATES, AND THE INPUTS IT
> GENERATES ARE BOUNDED BY WHAT THEIR AUTHOR THOUGHT WORTH VARYING.** `1 + rng.Below(40)` is not a
> mistake; it is a decision to always have a value, made without noticing that "no value" is a case.

That is `GF-44` again — an enumeration bounded by where its author looked — and it is the fourth
instance in five days, this time in a generator rather than a table, a document, or a lane.

**Fixed:** `bytePtrExact` for keys and values, where nil and empty are the same thing; `bytePtr`
unchanged for bounds, with both now saying at the code which case they are for. **The generator's hole
is not fixed here** — a change to the C++ rig's input space is Track B's, and widening it to include
zero-length values is carried rather than done in passing.

---
### GF-23 — a remedy that is written down rather than built has the defect's own shape

**Raised by** `HARNESS-019`, which is `HARNESS-016`'s second instance firing at a hundred times the
cost — see that entry for the mechanism.

> **WHEN A DEFECT'S REMEDY IS WRITTEN DOWN RATHER THAN BUILT, THE REMEDY HAS THE DEFECT'S OWN SHAPE
> AND COMES DUE ON THE DEFECT'S OWN SCHEDULE.**

It is `GF-20`'s sibling. `GF-20`: correctness resting on a premise that moves is a **scheduled
defect**. This: a **remedy** resting on someone remembering is one too — and it is worse in one
specific way. **The entry reads as closure.** A moving premise at least announces itself in the
comment that names it; a written-down remedy looks like the problem is handled, and the catalogue
grows a row that says so.

**THE TEST, AND IT IS ONE QUESTION:**

> After writing an entry, ask **what would have to change for the second instance to be IMPOSSIBLE
> rather than merely RECOGNISED.** If the answer is *"nothing — I would notice"*, the entry is not
> finished.

**What it is not.** Not a demand that every entry ship a mechanism — some defects have no mechanical
remedy, and `§3.2.1`'s residual bypass is the model for saying so out loud. The rule is that the
**choice** be made and stated, not that it always come out the same way. What is forbidden is the
third thing: an entry that neither builds the remedy nor admits it did not.

**Instances in this catalogue, both ways:**

| entry | remedy | outcome |
|---|---|---|
| `HARNESS-013` (the 11½-hour hang) | **built** — `LANE_TIMEOUT`, TIMEOUT as a distinct outcome | no second instance |
| `HARNESS-014` (registry matched nothing) | **built** — the cross-check induced both directions | no second instance |
| `HARNESS-017` (delimiter with no escape) | **built** — `cpp-scan` refuses >7 fields | no second instance |
| `HARNESS-016` (helper's blast radius) | **written down only** | **fired again, at a step's cost** |

**The pattern in that table is the argument.** Three built remedies, no recurrence. One written-down
remedy, one recurrence — and the recurrence cost the largest single loss of work in Track B.

**AND THE ARGUMENT WAS DEMONSTRATED WITHIN HOURS OF BEING WRITTEN.** The `FLOORS.txt` row recording
`BM105` contained `O(|S|)` — **two delimiters** — which is `HARNESS-017` exactly, recurring on the
same day. **The lane refused the row.** Nobody remembered anything; the check that was *built* caught
it at the moment of writing.

> **THAT IS THE WHOLE DIFFERENCE BETWEEN A RULE AND A REMEDY, ARRIVING AS ITS OWN EVIDENCE.** The rule
> was written that morning by an author who then broke it that afternoon and was stopped by a
> mechanism rather than by recall.

**A THIRD SAME-DAY INSTANCE, AND IT BROKE IN THIS FILE.** A B3.7 report named `HARNESS-021` and
`HARNESS-022` in its summary block **before either entry had been written** — two ids that resolved to
nothing, in the file whose job is recording gaps. Caught by re-reading the report, which is `GF-29`'s
least reliable instrument, and filed at `07044c3`.

**Three instances, all in one day, all in the hands of the author who had just written the rule.**
That is not carelessness worth apologising for; it is the rule's own claim being demonstrated:
**a remedy that lives in someone's attention fails at the rate attention fails**, which is often, and
independently of how recently the rule was written.

---

### HARNESS-024, -025, -026 — the differential rig's three findings about itself

**All three arrived before the rig's first finding about the engine could be trusted, and every one of
them made that finding possible.** Filed together because they are one instrument learning to be one.

**HARNESS-024 — the rig could not tell a failed reopen from an empty recovery.**
The first divergence read *"the engine recovered nothing."* The truth was that `DB::Open` had refused
the reopened image and the driver, having no field for that, left `recovered` empty either way.

> **A RIG THAT CANNOT REPORT WHY IT COULD NOT LOOK WILL REPORT THE ENGINE.**

`HARNESS-006`'s shape, and the diagnosis could not begin until the reopen's status was recorded — at
which point the real message appeared in one line: `key bounds disagree with the manifest`. **The
finding was `BUG-B006` all along and was unreadable for the length of one edit.**

**HARNESS-025 — the judge compared against ONE watermark where the contract permits a RANGE.**
B1's exactness oracle had already worked this out as a two-element set: *"a Sync can complete on the
device with the kill preempting its return."* The differential inherited the problem and **not the
answer** — and it is WIDER here, because a `Sync` in this engine can run a **flush**, each step with
its own fsync, so a kill inside one can leave any prefix between the last completed watermark and the
in-flight target durable.

**It also named the wrong direction.** An empty recovered state matches sequence 0, and the judge
reported the **first** matching sequence — so a run that recovered *more* than promised, and happened
to be empty because a clear-everything ran above the watermark, was reported as *"recovered less."*
**A verdict that names the wrong direction sends the reader to the wrong component**, which is
`HARNESS-006`'s cost paid by the instrument built to avoid it.

**And the fix was nearly too permissive.** Widening to a range unconditionally would have forgiven the
defect the strict comparison exists to catch: **unsynced data surviving a clean shutdown.** A
hand-built test caught that — the widening applies only to a run cut short, which is a fact about the
**log** (ops after a kill carry sequence 0), not about the engine's opinion.

**HARNESS-026 — the rig issued one op per batch, so it never reached the intra-batch rules.**
Found by `BM114` surviving **both** lanes. A `DeleteRange` covering keys written earlier in the same
batch, a `Set` after one re-adding a key, two ranges merging to their union, batch atomicity — none of
it was exercised, and nothing said so.

> **§8's EXPECTATION WORKING IN THE OTHER DIRECTION: a rig that finds nothing has said something about
> itself.** Here a *mutant* that found nothing said it.

**Batches are now expressed by a shared sequence**, which needed no format change because that is what
a batch **is** — and a field recording what the sequences already say would be a second source of
truth about one fact.

---

**THE COMMON SHAPE, AND IT IS WORTH MORE THAN THE THREE ENTRIES.** Each was found by taking a result
the rig produced and asking *what would have to be true of the RIG for this to be what it says?*

- *"recovered nothing"* → **could the rig have failed to look?**
- *"recovered less than promised"* → **is one watermark the whole contract?**
- *"the mutant survived"* → **does the workload reach the thing it blinds?**

None of the three is discoverable by reading the rig. All three are discoverable by disbelieving one
of its outputs for one minute.

---

### HARNESS-023 — a shared-fixture check that asked whether the bytes parse, not what they say

**Symptom.** `BM112` swaps two provenance fields in **both** C++ ends — writer and reader together —
and the fixture corpus test **passed**.

**Root cause.** The check asserted `ok()`: *did these bytes parse?* With both ends swapped they parse
perfectly. The lengths are right, the section decodes, the checksum covers it — **the wrong string
simply lands in the wrong field.**

> **A SHARED-FIXTURE CHECK THAT ASKS "DOES IT PARSE?" CANNOT CATCH A DISAGREEMENT ABOUT WHAT THE BYTES
> MEAN — WHICH IS THE ONLY DISAGREEMENT IT EXISTS FOR.**

**It is `GF-25` in the fixture corpus:** an assertion about the **outcome** where one about the
**content** was needed. The corpus was built for exactly this class and could not see it.

**AND THE MUTANT IS WHAT FOUND IT, WHICH IS THE PART TO KEEP.** `BM112` was planted to *demonstrate*
the two-decoder pair's value, on the assumption the pair already worked. The first induction reported
it killed — by `AcceptsTheCanonicalArtifactAndReportsWhatItHolds`, an **in-test** fixture that does
assert values — and only checking *which* test fired showed the corpus test had passed.

> **A MUTANT KILLED BY THE WRONG INSTRUMENT IS A MUTANT THAT LOOKS COVERED.** The `covered-by:` label
> would have named a test that catches the class, while the check built for it stayed blind. That is
> `GF-7`'s shape in the label file, avoided only because the label is DETERMINED by induction and the
> determination reads *which* assertion failed.

**Fix.** Both decoders' corpus tests now assert **field by field** against values written in the
document's terms — `engine_commit == "abc123"`, `regime == "flush"`, the seed, the caps, the op, the
watermark, the recovered pair, the outcome — so a swap of any two fails even though the bytes are
structurally perfect.

**The residual, stated:** the two decoders still need not agree on *which* refusal fires when a file
breaks more than one rule, only that neither accepts it. That is deliberate — pinning the refusal
would couple the two implementations' internal ordering, which is the coupling the pair exists to
avoid.

---

### GF-33 — an assertion about the members present cannot fail on a member added

**Raised at B5.3**, by `Status::Code::kBusy` crossing the C boundary as an integer no header names.

Three declarations of one set exist: `Status::Code` in C++, `rift_status` in the C header, and
`codeError`'s switch in Go. The boundary held them together with **nine `static_assert`s, one per
code** — each pinning a value, all nine correct, and **all nine still correct after a tenth code was
added.** `ToC` was a `static_cast`, which agrees with anything. The engine gained a code, the boundary
compiled without a word, and `rift_status(9)` reached the wrapper.

> **AN ASSERTION ABOUT THE MEMBERS PRESENT CANNOT FAIL ON A MEMBER ADDED. ONLY AN ASSERTION ABOUT THE
> SET CAN.**

**It is `GF-25` one turn further.** `GF-25` was *assert the content, not the outcome*; this is *assert
the set, not its members*. Both are the same mistake about what an assertion ranges over, and both
produce the same symptom: a check that is individually true, collectively silent, and reads in review
as thorough **because there are so many of them**. Nine asserts look like more rigour than one switch.
They are less.

**The fix is a mechanism that already existed three files away.** `status.h` says *"NO `default:` ARM
MAY EVER SWITCH OVER THIS TYPE — `-Werror=switch` is what makes adding an enumerator a build failure
until somebody classifies it."* `ToC` is the one place two independently-declared enums must agree, and
it was the one place not using it. The C++ side now refuses to compile; the Go side has no
exhaustiveness check to borrow, so `TestEveryStatusTheHeaderDeclaresIsMapped` **parses `rift.h`** and
holds the wrapper to it — a test that derives its set from somewhere other than the code under test is
the only kind that can fail on an addition.

**The operational form, for I1 and I2:** whenever the same set is declared in more than one place, the
question is not *"is each member right"* but *"what makes adding one break something."* If the answer
is *"the reviewer notices"*, there is no check.

---

### GF-34 — a second implementation is only a check when something runs both

**Raised by `BM118`'s survival**, B5.3.

The rig computes the backpressure threshold independently of the engine, on purpose: `AdjudicateBusy`
is the harness's arithmetic, the engine has its own, and the pair exists so neither is believed. `BM118`
changed the engine's comparison from `>` to `>=`. It was aimed at the test asserting exactly that edge
— `TheBoundaryValueItselfIsNotOwed` — **and survived**, because that test calls `AdjudicateBusy`. It
asserts the harness's edge with great precision and says nothing whatever about the engine's.

> **TWO IMPLEMENTATIONS OF ONE RULE ARE A CHECK ONLY WHERE SOMETHING RUNS BOTH AND COMPARES THEM. A
> TEST OF EITHER ONE ALONE IS A TEST OF THAT ONE ALONE — AND IT LOOKS EXACTLY LIKE THE CHECK.**

This is the *reverse* of `BM113`. There, a fix made the defect unrepresentable through an API and the
mutant survived because there was nothing left to catch. Here nothing was fixed and nothing was
unrepresentable: **the covering test simply pointed at the wrong one of two things with the same
name.** Both survivals were the mutant working; only one of them meant the code was safe.

The workload was reached rather than the mutant relabelled (`GF-16`): the covering test now drives real
writes onto a threshold chosen as an exact multiple of one write's cost, so the backlog lands on
`busy_bytes` exactly — legal under `>`, refused under `>=`, and nothing else in the suite separates
them.

---

### GF-35 — a policy that latches is indistinguishable from one that works

**Raised by `BM119`**, B5.3, and it is a shape three mechanisms in this repo now share.

`BM119` never releases the in-flight charge, so the backlog only grows: after enough bytes every write
returns `kBusy` forever and no amount of draining clears it. **A database that refuses all writes and
reports a legitimate, documented reason for each one.** The rig's entire forward assertion passes under
it — backpressure is owed, backpressure is signalled, the predicate holds on every write.

> **THE INTERESTING HALF OF A MECHANISM THAT SAYS "NOT NOW" IS WHEN IT STARTS SAYING "NOW".**

The same shape as the seam's consecutive-zero-write bound, and the same shape a lease that never
expires would have. In each, the safety direction is asserted everywhere and the liveness direction is
asserted nowhere, because the safety direction is the one the mechanism is *for* — and a mechanism
stuck permanently in its own purpose looks, to every test of that purpose, like it is working
perfectly.

**Mechanically:** any test that asserts a refusal must be followed by the thing that clears it and a
write that succeeds. Two lines, and they are the only two lines in `BM119`'s covering test that do
anything.

---

### GF-36 — a lane that depends on an artifact it does not build reports the absence as success

**Raised by `BM120`'s survival**, B5.4, and the survival was worth more than the mutant.

`BM120` makes snapshots pin nothing. It is aimed at the cgo differential, which takes its workloads
from real `rift_diff` artifacts and — correctly, and with a comment saying so — **skips** when that
binary is absent. The `cpp-cgo` lane built `rift_capi` and not `rift_diff`. The mutant runner deletes
`engine-cpp/build` in the copied tree. So the test skipped, `go test` reported `ok`, the lane went
green, and the mutant survived.

> **A SKIP INSIDE A PASSING TEST BINARY IS A GREEN LANE. THE TEST KNEW IT HAD NOT RUN AND SAID SO; THE
> LANE HAD NO WAY TO HEAR IT.**

**The skip was right and the lane was wrong**, which is the part worth keeping. Every instinct on
finding this points at the skip — make it a failure, make it loud, make it conditional. All of that
would break a Go-only checkout for no reason. The defect is that a lane declared a dependency in a
comment and not in its recipe.

**And the failure mode is silent in the direction that matters.** A lane that cannot run its check
looks *exactly* like a lane whose check passed — same exit code, same absence of output, less time.
Nothing about a green `cpp-cgo` distinguished "the boundary agrees with the model across 18 seeded
workloads" from "the boundary was never asked."

**What found it was the mutant, and only the mutant.** Every human-facing signal said the lane was
fine: it was green on the real tree, where the binary happens to exist because some other lane built
it. The class of bug — *a lane passing for a reason unrelated to its claim* — is `HARNESS-002`'s (a
warm build directory) and `BM21`'s. Both were also found by a mutant surviving, and in all three the
mutant was the only thing in the repo that ran the lane in a state a developer never sees.

**Operationally, for I1 and I2:** a lane's recipe must build everything its tests refuse to run
without. Grep for `Skipf` in anything a lane runs and check the recipe produces what each one names.
Done at B5's close: two artifact-dependent skips exist in the tree, both now built by the lanes that
run them; the rest are `-short` guards.

#### PROMOTED at B5's close — this is the vacuous-green class in its LANE-DEPENDENCY form, and it is new

Vacuous green has appeared in this project three times before, and each time the lane was running the
right test in the wrong *state*: `HARNESS-002`, a warm build directory; `BM21`, which survived until
the directory was made cold; `GF-16`, a workload that could not reach a mutant's precondition. **This
one is different in kind: the lane was not running the test at all.**

> **THE THREE EARLIER INSTANCES WERE A CHECK THAT COULD NOT FAIL. THIS ONE IS A CHECK THAT WAS NEVER
> INVOKED — AND THE TWO ARE INDISTINGUISHABLE FROM OUTSIDE, BECAUSE A LANE REPORTS AN EXIT CODE AND
> NOT A COUNT OF QUESTIONS ASKED.**

`GF-37`'s three instances belong here, under the same heading, because they are the same failure
reaching the measurement side: an instrument silently configured by its surroundings — a `Debug`
build, a single unreplicated run, a `SIGPIPE`'d pipeline whose `grep` exited 0. In every one of the
four, **the artifact produced looked exactly like a correct one**, and in every one the only thing
that noticed was something that knew what the answer should roughly be.

---

### GF-37 — an instrument inherits the configuration of the lane that built it, and says nothing about it

**Raised at B5.5**, twice in one afternoon, in opposite directions.

**First: the build type.** The benchmark was built into `engine-cpp/build/test`, because that is where
every other Track B binary lives and the harness simply reused the path. That directory is configured
`Debug` — correct for every lane that uses it, assertions on and optimiser off. The table it produced
reported ~4 µs for a single memtable `Set` and a `readrandom` cost that **did not move with batch
size**, which is not a slow engine; it is a description of `-O0`.

> **A BENCHMARK FROM A DEBUG BUILD IS NOT A SLOW NUMBER. IT IS NOT A NUMBER.**

**Second: the run count.** The first tables reported boundary costs of **−23%** and **−16%** — the cgo
column beating the native one, which cannot happen, because the cgo column does everything the native
column does and *then* crosses a boundary. **The impossible number was the only reason the variance
was noticed.** Had the noise landed at +8% instead of −23%, it would have been published as a finding
and nothing in the table would have contradicted it.

**The two share a shape.** A measurement carries no record of the conditions it was taken under. A
correctness test that runs under the wrong configuration usually *fails*; a benchmark under the wrong
configuration **succeeds and prints a number**, and the number looks exactly like a right one. Every
signal that something was wrong here came from a reader knowing what the number should roughly be —
which is not a mechanism, and does not survive the first measurement of something unfamiliar.

**Mechanically, and it is what `BENCHMARKS.md` now requires:** the build type, the statistic, the run
count and the machine are part of the result and are printed beside it. The bench lane uses its **own
Release directory** rather than a flag on the shared one, deliberately — the sweep's kill-point counts
and every floor in `FLOORS.txt` are measured against `Debug` builds, and a lane that quietly changed
build type underneath them would move denominators nobody was watching.

**And the third instance, in the shell, the same day:** the first full table was taken with
`make cpp-bench | head -26`. `head` closed the pipe, `make` died of `SIGPIPE`, `grep` exited 0, and
the run reported success with two thirds of its rows missing. **A pipeline's exit status is its last
command's.** Same shape once more: the instrument was configured by its surroundings and reported
nothing about it.

---

### GF-38 — a periodic action guarded by a second condition fires only where the two coincide

**Raised at B5.5**, and it killed the same process twice for two different reasons.

`engine/model` copies its whole entry slice per apply and retains every version until durability
advances — correct for a reference engine, since a crash dropping `pending` *is* the mechanism. A
benchmark that never advanced it held 50,000 versions of up to 50,000 entries and was OOM-killed.

The fix drained periodically:

```go
if in == batch {
    seq, _ := e.Apply(b, false)
    if i%1024 == 0 { drain(seq) }   // <-- inside the batch-boundary branch
}
```

`i` is the **operation** index; the branch runs on **apply** boundaries. At `batch == 1` every
operation is an apply and the drain fires every 1024. At `batch == 8`, `i % 1024 == 0` requires
`i ∈ {0, 1024, 2048…}` and the branch requires `(i+1) % 8 == 0` — **the two never coincide.** At
`batch == 64`, never either. The drain existed, read correctly, was tested at `batch == 1`, and was
dead everywhere else. The process died again, at a different row, from the identical cause.

> **A PERIODIC ACTION NESTED INSIDE A CONDITIONAL MUST COUNT THE THING THAT ACTUALLY HAPPENS, NOT THE
> THING BEING ITERATED. `every N iterations` INSIDE `if (rare)` MEANS `every N iterations THAT ARE
> ALSO rare` — WHICH IS OFTEN NEVER.**

**It is `GF-16` in the code rather than in a test.** There, a workload could not reach a mutant's
precondition and the mutant survived. Here, a guard could not reach its own action and the action
never ran. Both are a condition that reads as satisfiable and is not, and in both cases the only
evidence was an outcome nobody could otherwise explain.

The counter now counts applies. It is one more variable and it is the variable the action is actually
periodic in.

---

### GF-39 — two tracks with separate lane sets accumulate an unbounded debt payable in one instant

**Raised at B5.2, promoted out of it by Ansh at B5's close as the finding with the most downstream
value in the phase.**

`make determinism` was **red**, and had been **since B4**. `engine/differential` sits under the
`engine/...` core pattern — which is what puts `engine/model` in scope — and was never classified.
Nothing noticed for two phases.

**Nothing was broken. That is the point.** The judge was correct, its own lane (`cpp-diff`) was green,
every C++ lane was green, and the package did exactly what it was designed to do. What had happened is
that **Track B runs `CPP_LANES` and Track A runs `ci`**, the two sets are disjoint, and Track B had
been adding **Go packages** for two phases.

> **TRACK A'S LANE SET WOULD HAVE MET EVERY GO PACKAGE TRACK B HAS EVER WRITTEN FOR THE FIRST TIME
> **AT MERGE**, ALL AT ONCE, ON A DAY DEDICATED TO SOMETHING ELSE.**

**The general form:**

> **TWO TRACKS WITH SEPARATE LANE SETS DO NOT DIVERGE GRADUALLY. THEY ACCUMULATE A DEBT THAT IS
> INVISIBLE WHILE IT GROWS, BOUNDED BY NOTHING, AND PAYABLE IN A SINGLE INSTANT — THE MERGE.**

**And the merge is the worst possible moment to discover it**, for three reasons that compound:

1. **Attribution is gone.** Nineteen files land together. A red lane names a symptom, and the commit
   that caused it is somewhere in two phases of work.
2. **The pressure is wrong.** A merge is a moment for integrating finished work, not for making
   scope decisions — and *"is `engine/differential` core scope?"* is a scope decision under `[A5]`,
   which is exactly the kind that should be made deliberately and recorded.
3. **The cheap fix is available and wrong.** With a merge blocked and a lane red, the fastest route
   is a package exclusion — and `A5` bans package exclusions specifically because they are what
   somebody reaches for under exactly this pressure.

**The remedy is not "run more lanes"; it is that the debt must be paid continuously.** Every Track B
step that adds a Go artifact runs Track A's lane set on it in that step. Cost: minutes. The
alternative is not a larger cost later — it is an **unbounded** one, since nothing caps how much
unclassified work can accumulate between merges.

**Confirmed at B5's close, on Ansh's instruction, that nothing else is outstanding.** Every Go file
Track B has added — nineteen, across `engine/differential/`, `engine/riftcgo/`, and
`tools/determinismcheck/` — was enumerated against `main`, and Track A's full push lane
(`build lint test race blind smoke mutants`, including the three lanes Track B had never run) is green
on `rift-b`.

**The classification itself, recorded because it is a scope decision and not a fix.** Both packages
are **exclusions** by `scope.go`'s own test — *"a package that needs a goroutine is an exclusion; a
package that needs one `time.Now` is a hatch"* — and `sync` is unhatchable in core scope:

- `engine/differential` is a **judge**. Nothing in it executes during a simulated run; it reads
  artifact files a finished run left behind. That is `sim/checker`'s situation exactly and it gets
  `sim/checker`'s answer. **Its independence is why it cannot be in core:** reading bytes neither
  engine handed it is the whole mechanism by which it is a second opinion rather than a mirror.
- `engine/riftcgo` needs `sync` for its durability callbacks and `unsafe` by construction — a cgo
  boundary *is* pointer identity and layout, which is what core scope exists to keep out. And it is
  the constitution's own scoping rather than a hole: *"deterministic-replay guarantees are scoped to
  sim runs on `engine/model`."*

**The build-tag half, and the distinction is exact.** `riftcgo` cannot link without the C++ archive,
so it carries a tag and Track A's `make test` skips it. A tag removes a package from `./...`
entirely — so it could sit unanalyzed forever with every lane green.

> **A LOAD GATE CATCHES A PACKAGE THAT FAILS TO LOAD. IT DOES NOT CATCH ONE THAT WAS NEVER OFFERED.
> BOTH FAILURES PRODUCE THE SAME SILENCE.**

`TestHatchRegistry` now asserts **by name** that the package was among those loaded, and the
determinism pass loads with the tag (it only typechecks, which needs no archive — that is what the
`${SRCDIR}` `CFLAGS` bought). Induced: dropping the tag from the test's `BuildFlags` fires the
assertion.

#### GF-39's first concrete instance, at the merge itself: a clean auto-merge that pinned a table true on neither branch

**Recorded because `GF-39` predicted a cost at the merge and this is what that cost actually looked
like.** It was not a red lane. It was a **green** one.

`tools/determinismcheck/scope.go` conflicted — both tracks had edited the exclusion list — and git
said so. `tools/determinismcheck/determinismcheck_test.go` **did not** conflict, and git said nothing,
because the two branches had edited **different lines of the same table**: Track A added the
`engine/differential` polarity rows near the bottom, Track B replaced the `engine/real` /
`engine/pump` reservation block near the top. Git spliced both edits in, produced a file with no
conflict marker, and the file **compiled**.

> **A THREE-WAY MERGE RECONCILES LINES. IT HAS NO OPINION ABOUT WHETHER THE RESULTING ASSERTIONS ARE
> TRUE OF THE RESULTING CODE.**

**And the table it produced was true on neither branch.** Measured, by holding the merged test fixed
and swapping each parent's `scope.go` underneath it:

| `scope.go` from | merged `TestScopeTable` | what it disagreed about |
|---|---|---|
| `main` (Track A) | **FAIL**, 1 row | `engine/riftcgo` wanted `scopeOff`; Track A had never heard of the package |
| `rift-b` (Track B) | **FAIL**, 4 rows | `engine/differential/inner` wanted core and got `off` (Track B's entry was `/...`); all three `node/...` rows wanted `off` and got core, a deletion Track B never made |
| the hand resolution | **pass** | — |

Both parents compile the merged test. Both fail it. **The auto-merge produced a specification that
had never been anybody's specification**, and it produced it silently.

**What caught it was not the merge and not a lane.** It was reading the merged file to see what it
would assert — done only because `scope.go`, its data file, had conflicted five lines away. Had the
conflict fallen anywhere else in the repository, the test would have been read by nobody, and the
first two rows of that table would have been carried forward as fact.

> **A CONFLICT MARKER MARKS WHERE TWO EDITS TOUCHED. IT DOES NOT MARK WHERE TWO EDITS *MEAN*
> SOMETHING TOGETHER THAT NEITHER MEANT ALONE. AN ASSERTION FILE AND THE FILE IT ASSERTS ABOUT ARE
> ALWAYS THAT SECOND CASE, AND MERGE TOOLS CANNOT SEE IT.**

**The general form, and it is not about merges.** `GF-39` said the debt is payable in a single
instant. This says something narrower and worse about *what* is payable:

> **THE DANGEROUS ARTIFACT AT A MERGE IS NOT THE ONE THAT CONFLICTS. IT IS THE ONE THAT PINS A CLAIM
> ABOUT A FILE THAT CONFLICTED, AND MERGED CLEAN.**

**Two things followed, both landed in the merge commit rather than promised:**

1. **The blind lane paid the same debt in the same instant and said so out loud.**
   `blind-differential-wildcard` came back **ROT** — *patch no longer applies* — because it mutated
   Track A's comment block for `engine/differential`, and the resolution replaced that block with
   Track B's better-argued one. A patch that no longer applies is a rule that is no longer defended,
   and the lane distinguishes `ROT` from `SURVIVED` precisely so that *"the code moved"* cannot be
   mistaken for *"the rule held"*. Repointed, and a second patch added, `blind-riftcgo-wildcard`,
   because **both** entries arrived from Track B wildcarded and both were narrowed at the merge, and
   one patch standing for two lines would leave one narrowing defended and the other merely believed.
   `make blind`: **20 killed, 1 canary alive, 0 mismatched.**
2. **Both prefix polarities pinned for both packages.** The merged table pinned
   `engine/differentialish` and `engine/riftcgonot` — a sibling whose name merely *starts with* the
   excluded one — but pinned `.../inner` for only one of the two. Four rows now, because a wildcard
   commits **both** kinds of over-reach, and these two packages sit directly beneath `engine/...`,
   the pattern that puts `engine/model` in scope. That makes a stray wildcard here the
   highest-consequence version of that mistake anywhere in the table.

**What is still open, and it is deliberate.** `engine/riftcgo`'s exclusion is **provisional**, marked
as such in `scope.go` with the thing that settles it named: the determinism pass has never actually
run against that package with the C++ archive built. The argument for excluding it is sound, is Track
B's, and is kept verbatim. Ansh's own prior ran the other way and is recorded beside it in `scope.go`
so the answer can be checked against the guess: *"the cgo wrapper is core-scope code with a hatch for
the boundary rather than orchestration, since it implements the frozen `Engine` interface and runs
inside simulated runs at I1 — but that is a prediction and the pass should decide it."* He ratified
Track B's argument over his own prior. **One of the two is wrong about this package**, and I1's
report says which. It is still an
**argued** property rather than a **measured** one, and this project does not let those two words
mean the same thing. It runs at I1.

---

### GF-40 — a catalogue only ever run in subsets rots in the parts no subset names

**Raised at B5's close**, by the first full run of the mutant catalogue since B3.

**140 killed, 0 survived, 15 ROT.** A `ROT` is a patch that no longer applies: *the code moved and the
mutation did not.* **Fourteen of the fifteen were already rotted when B4 signed**, verified by
replaying every one of them against the tree at `8179320`. They rotted across **B3.5 through B4.2** —
`BM79` at `5ef23f5`, `BM55` at `71aafba`, `BM105` at `e70951a`, `BM87` as late as `cd2b227` — and
nothing noticed for two phases. Only `BM16` was B5's, broken by `kBusy`'s in-flight accounting; it is
re-aimed and killed.

**Why nothing noticed is the finding, and it is not "we forgot to run it".** The catalogue costs hours
— each class needs a cold control build and a cold covering run — so it is run as `ONLY=` subsets, and
**every subset that does not name a rotted class is green.** Each phase ran its own new mutants, and
each phase's run was honest and complete about what it covered.

> **A ROTTED PATCH IS NOT A WEAKER MUTANT. IT IS *NO* MUTANT — AND IT LOOKS EXACTLY LIKE A HEALTHY ONE
> FROM EVERY DIRECTION EXCEPT ACTUALLY RUNNING IT.**

**THE PAPERWORK WAS COMPLETE FOR ALL FOURTEEN.** This is the part that generalises furthest:

| the check | what it asserted | did it fire |
|---|---|---|
| `cpp-scan` part 6 | every mutant class has a `FLOORS.txt` row | no — all fourteen had one |
| `FLOORS.txt` | every class has a standing measurement with a floor and a ceiling | no — all fourteen had one |
| the class count in every report | "155 classes" | no — it counted all fourteen |

Each of those was built *because* a previous defect showed that mutants can degrade silently. Together
they assert that a class is **documented**, **floored**, and **counted**. **Not one of them asserts
that the patch still applies** — so fourteen classes were fully certified and impossible to run, and
the number `155` was, for two phases, an overstatement nobody could have caught by reading anything.

> **EVERY CHECK ON THE CATALOGUE MEASURED ITS PAPERWORK. NONE MEASURED WHETHER IT COULD BE EXECUTED.**

#### The broader form, which is not about mutants

Fourteen classes were **documented, floored, counted, and unrunnable**, and three independent checks
looked directly at them and agreed they were fine. That is not a fact about catalogues:

> **A SET OF CHECKS THAT ALL VERIFY A THING'S *DESCRIPTION* WILL AGREE THAT IT IS FINE WHILE THE THING
> ITSELF IS GONE. ADDING MORE OF THEM MAKES THE AGREEMENT STRONGER AND THE EVIDENCE NO BETTER.**

The three checks here were **not redundant with each other** — one asserts a row exists, one asserts
the row carries a measurement, one counts. Each was added after a different defect. All three range
over the *record* of a mutant, and none over the mutant. Fourteen absences produced fourteen complete
sets of paperwork, and the total number of checks was, if anything, evidence *against* looking.

The question that separates the two kinds is short, and it is the one to ask of any registry, ledger,
or manifest this project keeps: **what here would fail if the thing being described did not exist?**
For `FLOORS.txt`, `DECIDERS.txt`, `HATCHES.txt` and the mutant catalogue, that question now has an
answer only for the last one.

#### `BM16` IS THE HARDER HALF, AND IT IS WHY THE FIX ABOVE IS NECESSARY AND NOT SUFFICIENT

B5.3 added a scope guard to `Wal::Sync` whose destructor takes **the same mutex `BM16` widens the
scope of**. The original patch would have applied here **perfectly cleanly** — `cpp-rot` would have
passed it, and every piece of paperwork would have been right — and then **self-deadlocked.**

> **A MUTANT THAT ROTS INTO NO FAILURE IS REFUSED BY NAME. ONE THAT ROTS INTO A DIFFERENT FAILURE IS
> SCORED.**

A `ROT` is loud: the runner names it, counts it, and fails. A hang is **neither a kill nor a
survival** — the lane produces no verdict at all, and the class silently stops asking its question
while every artifact about it stays correct.

**This is `HARNESS-013`'s lesson arriving one layer up.** There, a *lane* hung for eleven and a half
hours and looked exactly like progress, and the answer was a watchdog that turns a non-report into a
`TIMEOUT`. Here it is the **patch** rather than the lane, and **no watchdog helps**: the run is not
failing to finish, it is finishing while answering a question nobody asked. The only thing that
catches it is the mutant's own **direction control** — the control lane going red where it must stay
green — and that costs a full cold build, which is precisely the cost `cpp-rot` was built to avoid
paying.

> **THE CHEAP CHECK BOUNDS THE DAMAGE. IT DOES NOT CLOSE THE CLASS. A PATCH THAT APPLIES IS NOT
> THEREBY A PATCH THAT ASKS ITS QUESTION.**

That limit is recorded at `scripts/cpp-rot.sh`'s definition rather than only here, because it is the
kind of thing a future reader will need at the moment they are deciding whether a green `cpp-rot` is
enough.

#### What closed it

**The remedy is built rather than written down (`GF-23`), and it is cheap in exactly the way the thing
it protects is not.** `make cpp-rot` dry-run-applies all 155 patches. It applies nothing, builds
nothing, and runs nothing — it asks `patch --dry-run` and no more. It takes **seconds** where the
catalogue takes **hours**.

**That asymmetry is the entire argument for it existing separately:** *the expensive lane is the one
that gets skipped, so the cheap half of what it proves must not live inside it.* Fourteen classes
rotted precisely because the only thing that could detect them cost hours.

**It would have caught all fourteen on the day each one rotted** — `BM79` at `5ef23f5`, `BM55` at
`71aafba`, `BM105` at `e70951a`, `BM87` at `cd2b227` — each as a one-line failure naming the class and
the file whose code moved out from under it.

**It is deliberately NOT in `CPP_LANES` yet.** It is red, by fourteen, for a debt that predates the
branch waiting to merge — and putting a red in front of a merge for that is the exact pressure `GF-39`
identifies as the wrong moment to make this kind of decision. Carried as `CF-7`.

---

### GF-41 — a mutant anchored on comment lines is disarmed by prose edits that change no behaviour

**Raised at the A7/B5 merge from `BUG-038`, and promoted because the cause is not an incident.**

`M77` stopped applying. Its mutated line was byte-identical, its trailing context was byte-identical,
and three comment lines above it had been rewritten. The mutant was disarmed by an edit that changed
no behaviour at all.

> **IN A REPOSITORY WHOSE COMMENTS CARRY THE ARGUMENTS, PROSE EDITS ARE CONSTANT. THAT MAKES THIS A
> STANDING DISARMING MECHANISM RATHER THAN AN INCIDENT: EVERY TIME AN ARGUMENT IS SHARPENED, SOME
> PATCH SOMEWHERE SILENTLY STOPS APPLYING, AND NOTHING SAYS SO UNTIL A LANE NOBODY CAN AFFORD TO RUN
> GETS RUN.**

**The irony is the argument.** The comment that disarmed `M77` is the Ruling-4 text about a path kept
for what it *measures* rather than for what it *serves* — written precisely so a future reader would
not delete the replicated read path as unused. Sharpening the argument disarmed the instrument
guarding the argument's subject.

**And it is a sibling of a lesson already learned, one axis over.** `M80` taught that a patch must not
**mutate** comment lines, because coverage never marks them. `M77` is the same mistake on the
**matching** side.

> **A RULE ABOUT WHAT A PATCH MAY REPLACE DID NOT GENERALISE TO WHAT IT MAY ANCHOR ON.**

The `M80` fix was written against its instance rather than against its class, and the sibling sat one
axis away for a month. Every rule about a patch now gets asked in both directions: what does it
change, and what does it match on.

**Mechanised, because a standing mechanism needs a standing check.** `make anchors` reads every
patch's context statically and fails when a hunk anchors on prose. Milliseconds, and it would have
fired the day the comment changed rather than at the next full catalogue run.

**AND IT IS A CLASS RATHER THAN AN INCIDENT, which was measured the next day.** `tools/anchorcheck`
read `sim/mutants/` only. Its comment said blind patches *"live elsewhere and are checked by their own
lane."*

> **THAT SENTENCE WAS TRUE AND IRRELEVANT.** `make blind` asks whether a patch is **killed**, not
> whether it is still **anchored** — it reports a stopped-applying patch as `ROT` rather than
> preventing it. **A true statement that answers a different question is the most durable kind of
> wrong:** nothing about it ever looks false, so nothing prompts a re-read.

Measured over the directory the rule was not reading: **9 of 20 blind patches were prose-anchored**,
and `blind-riftcgo-wildcard` **rotted within a day of being written**, when the comment it matched on
was rewritten by the same hand that had written the rule. **One incident is an anecdote; nine of twenty
with one already rotted is a class.** All nine re-anchored with byte-identical proofs; the lane now
reads both directories.

**Its threshold is measured, and that is the part worth copying.** The obvious rule flags 47 of 71
patches; `patch(1)`'s fuzz absorbs one or two all-prose lines and not three, measured on the toolchain
the lanes actually use, so the rule is "three or more, or any interior" and it flagged 17 of 71. A threshold
picked to make a count small is a weakened checker. A threshold picked by what the tool does is a
measurement, and it is re-taken when the tool changes.

---

### GF-42 — an obligation records why it is blocked, nothing re-asks the blocker, and the reason rots while the obligation stands

**Raised at the A7/B5 merge, from three instances in one day.**

| where | the reason it recorded | what was true |
|---|---|---|
| `tools/determinismcheck` `notAnalysed` | `riftcgo` *"cannot type-check without the C++ static archive"* | it builds without one — and the entry was **unreachable**, so nothing could contradict it |
| `scripts/corpus-reproduces.sh` | *"20 bundles checked, 4 skipped"* | it summed to the population when written; the population grew |
| `CARRY-FORWARD.md` `BUG-015` | *"the covering test cannot be used to check any of this as it stands"* | both named blockers were gone — the covering test is directed now and `mutant-covered` has `ONLY` |

> **AN OBLIGATION'S BLOCKER IS A CLAIM ABOUT THE WORLD, AND IT IS THE ONLY PART OF AN OBLIGATION
> NOBODY REREADS: THE TASK IS WHY YOU OPEN THE ENTRY, AND THE BLOCKER IS WHY YOU CLOSE IT AGAIN.**

The task gets reread constantly — it is why anyone opens the entry. The blocker is read once, believed,
and used as the reason to stop reading. So a stale blocker does not merely sit there: it actively
turns readers away from work that has become cheap.

**All three were found by a person reading records beside lanes, not by any lane.** That is the
argument for a mechanism rather than for more care.

**The mechanism, and it is deliberately the cheapest thing that works.** An entry may declare:

```
<!-- BLOCKER
     what: one line naming what is blocked
     stale-when: <shell command>
-->
```

`make blockers` runs each condition from the repo root. **Exit 0 means the blocker has lifted** and the
lane fails, naming the entry. It does not try to check every blocker: one whose lifting is not a
machine-checkable condition carries no declaration and the lane is silent about it. **That limit is
stated rather than papered over** — what this buys is that the blockers somebody *could* express get
re-asked on every push, at the cost of one `grep` each.

**Its first live subject is honest about the cost it names:** `make power-mutants` is queued behind the
sweep-based covering tests, and the declaration names the largest of them as a tripwire. Verified still
blocked; induced by renaming the tripwire and requiring the lane to fail. *(The tripwire was re-pointed
the next day: it first named the largest of the **eight the table listed**, which `GF-44` showed was
only the second-largest in the repository. A tripwire on the second-largest sweep goes green while the
largest stands.)*

#### The sharper form, found the next day, and it is the one to keep

A fourth instance arrived immediately and it is worse than the first three, because the refuting
evidence was **not** somewhere else:

> **`CARRY-FORWARD.md`'s sweep-cost table said ~1,928 seeds. Three paragraphs below it, in the same
> entry, added later by the same author: *"`make mutants`' baseline still exceeded two hours of
> monotonic time and died on its own timeout."* Recorded as *firmer than the estimate above*. It is
> also inconsistent with it, and nobody reconciled them.**

At 1,928 seeds a two-hour death is a puzzle worth a paragraph. At the derived ~6,450 it is arithmetic.
The measurement was right, was written down, was labelled as superseding the estimate — and the
estimate stayed on the page, three paragraphs up, where every later reader would meet it first.

> **A MEASUREMENT THAT CONTRADICTS THE TABLE BESIDE IT DOES NOT CORRECT THE TABLE BY BEING TRUE.
> SOMEBODY HAS TO NOTICE THEY DISAGREE.**

**And the operational consequence names who that somebody is.** A stale blocker rots over months and
anyone might catch it. This is different and sharper:

> **WHEN A NUMBER AND A MEASUREMENT IN ONE DOCUMENT DISAGREE, THAT IS A FINDING AT THE MOMENT OF
> WRITING — AND THE WRITER IS THE ONLY PERSON POSITIONED TO SEE IT.** Nobody else will ever hold both
> halves in mind at once: the later reader meets the table first and stops there, and the reader who
> reaches the measurement has already accepted the table.

So the obligation is on the hand adding the measurement, not on a future audit: **write a number beside
an older one and you have taken on the job of reconciling them, or of saying in the text that you did
not.**

---
### GF-43 — a log with two writers fails in the reader, and it fails as silence

**Raised 2026-08-28 from `BUG-043`, and filed as a shape because the failure mode is the one this
project cares most about.**

An orphaned job from an earlier session held `lanes3.log` open for **10 hours 7 minutes**. A later run
truncated the same path and wrote from offset 0. The orphan then wrote at its own large offset,
punching a **1,369-byte NUL hole** between them. Every other log in the directory had zero NULs.

`grep` classifies a file containing NULs as binary and suppresses output. So:

```
grep -n "=== LANE" lanes3.log     ->  nothing, exit 0
tail lanes3.log                   ->  the lines are right there
```

> **A LOG WITH TWO WRITERS DOES NOT FAIL IN THE WRITER. IT FAILS IN THE READER, AND IT FAILS THE ONE
> WAY A READER CANNOT DETECT: BY RETURNING NOTHING, SUCCESSFULLY.**

*No matches* and *this tool has silently given up* are the same observation. That is the vacuous-green
register's own theme arriving in the instrument used to read the repository rather than in the
repository.

**The operational half, which is the finding rather than its decoration.** The anomaly was hit
**twice**. It was noticed both times — *"the grep for `=== LANE` returned nothing?! That's odd"* — and
stepped past both times, because `tail` still worked and the numbers still looked sane. It was
available, it cost one command to chase, and it was dismissed.

> **AN ANOMALY THAT IS CHEAP TO CHASE AND GETS DISMISSED IS NOT A SMALL MISTAKE. IT IS THE ONLY
> WARNING THE SILENT FAILURE MODE EVER GIVES.**

**What resolved it was the standing rule, applied late.** Ansh asked *"is it even running?"*. `ps`
answered in one command — and `ps` is where the second job was. The rule already existed and already
covered this:

> **READ RUN STATE FROM THE PROCESS, NEVER FROM A WATCHER OR A LAUNCH — AND NEVER FROM A LOG A SECOND
> PROCESS MIGHT BE HOLDING.**

The last clause is what this instance adds. Six lanes had been reported from a file rather than from
the thing producing it; every one of them survived re-checking, which is luck rather than method.

**Mechanised as practice, not code:** a lane log is named per invocation so two runs cannot share a
path; `ps` is the first check when progress is in question; and an orphan check runs before starting
long work, because a job that outlives its session is invisible to everything except `ps` and competes
for the machine the measurements are taken on.

---

### GF-44 — a derivation that finds one pattern reports completeness over that pattern, not over the population

**Raised 2026-08-28 from the sweep-cost table correction, which is the merge's most consequential
result because it changes the size of a signed obligation.**

`CARRY-FORWARD.md` recorded **eight** sweep-based covering tests totalling **~1,928 seeds**. The
derived figures are **~20** tests and **~6,450 seeds**. Three errors, and they have three different
causes, which is why they are recorded apart rather than as one bad table:

| | error | cause |
|---|---|---|
| 1 | `TestToySurvivesOneThousandSeeds` recorded as **64**; it runs **1,000** | **one wrong cell in an otherwise careful table** — five of the six other numbered rows are exact. Contradicted by the test's own *name*, which is the cheapest possible check and was never made |
| 2 | `TestSnapshotEquivalenceOracleReportsNothing` listed as `(sweep)`, no number, "A2, six classes" | **an unnumbered row reads as the heaviest.** It is **50 seeds**, the lightest of them. A missing number is not neutral; a reader fills it in from the surrounding rows |
| 3 | the whole `assertOracleSilent` family absent — **13** tests, **24** classes, including a **2,000-seed** sweep | **the derivation looked for one idiom.** It found every test sweeping with a local `const seeds` loop and reported that as the population |

**The third is the one that generalises.**

> **A DERIVATION THAT FINDS ONE PATTERN REPORTS COMPLETENESS OVER THAT PATTERN, NOT OVER THE
> POPULATION — AND THE REPORT LOOKS THE SAME EITHER WAY.**

The original table was not careless. It was a correct enumeration of one mechanism, presented as an
enumeration of the cost. Nothing in its output could have said *"and there is a second way to sweep."*

**So the limit of a derivation belongs inside the entry it produces**, naming the patterns searched, so
the next reader can ask whether there is a third. This entry's own limit is recorded with it: it finds
a `const seeds` loop and the `assertOracleSilent` family, and a third idiom would be missed exactly the
way the second was.

### The pattern, named rather than counted: three recurrences in four days, two inside the mechanisms built to record or enforce it

| # | where | the enumeration | what bounded it |
|---|---|---|---|
| 1 | `CARRY-FORWARD.md`'s sweep-cost table | eight covering tests, ~1,928 seeds | one sweeping **idiom** — a local `const seeds` loop |
| 2 | `DESIGN-I1` §1, **the document recording this form** | two off-interface methods | two **paths** its author had in mind — restart and crash |
| 3 | `tools/anchorcheck`, **the lane built to enforce `GF-41`** | one patch **directory** | where the author had been looking when the rule was written |

> **THE COMMON SHAPE: AN ENUMERATION BOUNDED BY WHERE ITS AUTHOR LOOKED, PRESENTED AS AN ENUMERATION OF
> THE POPULATION.**

**And every one of the three had a cheap mechanical derivation available at the time**, which is what
makes this a pattern with a remedy rather than an observation about fallibility:

| # | what would have caught it | cost |
|---|---|---|
| 1 | grep every `assertOracleSilent(t, "…", N)` with its enclosing function | seconds |
| 2 | `grep -ohE "\b(n\.db\|m\.db)\.[A-Z][A-Za-z]*" store/*.go sim/toy/*.go \| sort \| uniq -c` | seconds |
| 3 | walk the directories containing `*.patch` rather than naming one | seconds |

**Every one was written from what was in view instead.** That is the finding: not that the authors were
careless, but that *reasoning produces a plausible list and stops*, while a derivation produces a list
and can be asked what it searched. **Two of the three occurred inside artifacts built to record or
enforce this exact rule**, four days apart, which settles whether knowing the form is sufficient
protection against it.

**And the derivation was wrong the first time.** A misplaced `?` in `assertOracleSilentWith?\(` made it
report **one** sweep instead of thirteen. It was caught only because two scripts written minutes apart
disagreed — one step from correcting a signed record with a broken script.

> **THIS IS THE THIRD TIME A NUMBER WAS NEARLY REPORTED FROM AN INSTRUMENT NOBODY CHECKED.** The
> others: the killed measurement driver that left a mutant applied, so three floors were measured
> against a tree nobody had checked (`BUG-033`); and the tar-exclusion glob that did not anchor, so
> mutant verdicts were computable against a tree that was not the repository (recorded inside
> `BUG-B007`, against `BM21` — *"a glob that 'usually' anchors is not an anchor"*).

*(Ansh referred to that last one as "the M73 glob". Track A's `M73` is
`a-read-answer-lands-in-any-incarnation` and is unrelated; the glob is Track B's, in `BUG-B007`. Noted
rather than silently substituted, because a citation nobody checks is the same class as everything
above.)*

**The consequence for the obligation, which is why this is not a bookkeeping fix.** *"Eight sweeping
covering tests remain, and they are the whole of the residual cost"* is false. Converting all eight
would leave the 2,000-seed `TestLeaderCompletenessOracleReportsNothing` and twelve more sweeps in
place — the majority of the cost. The blocker tripwire watching *"the 1,000-seed one and the most
expensive"* was re-pointed for the same reason: **a tripwire on the second-largest sweep goes green
while the largest stands.**

---
### GF-45 — an argument about what a package contains is bounded by what its authors wrote, and a language with code generation has a second half reasoning cannot reach

**Raised at I1, 2026-08-28, when the determinism pass ran against `engine/riftcgo` with the C++ archive
built and settled a scope question that two people had argued from the source.**

**Two positions were on the record, and both were reasoned carefully.**

| | position | outcome |
|---|---|---|
| Track B | exclude it: it needs `sync` for durability callbacks and `unsafe` by construction, *"a cgo boundary is pointer identity and layout"* | **right in conclusion, incomplete in evidence** |
| Ansh | *"the cgo wrapper is core-scope code with a hatch for the boundary rather than orchestration, since it implements the frozen `Engine` interface and runs inside simulated runs at I1 — but that is a prediction and the pass should decide it"* | **wrong, and recorded as wrong at his instruction** |

**What the pass found:**

```
engine.go, iter.go        5 findings: 4x unsafe, 1x sync
the cgo-GENERATED file    3 findings: unsafe, syscall, runtime/cgo
```

**Both parties reasoned about the source. Two of the eight findings are in a file nobody wrote** — and
the decisive one is among them.

> **AN ARGUMENT ABOUT WHAT A PACKAGE CONTAINS IS AN ARGUMENT ABOUT WHAT ITS AUTHORS WROTE. A LANGUAGE
> WITH CODE GENERATION HAS A SECOND HALF THAT REASONING CANNOT REACH — NOT BECAUSE THE REASONER WAS
> CARELESS, BUT BECAUSE THE TEXT WAS NOT THERE TO BE READ.**

**And that sharpens why this project measures.**

> **THE REASON TO MEASURE RATHER THAN ARGUE IS NOT THAT ARGUMENTS ARE SLOPPY. IT IS THAT THEY ARE
> BOUNDED BY WHAT THE ARGUER CAN SEE.**

Track B's argument was not weak. It was *complete over the source*, which is the whole of what an
argument can be complete over. The pass is not smarter; it simply reads a file that exists only after
the toolchain has run.

**The structural half, which outlives this package.** The finding that decided it was not the *count*
of imports but the *addressability* of one file:

> `HATCHES.txt` is keyed `path:line`. The cgo-generated file lives at a go-build content hash —
> `~/Library/Caches/go-build/41/41630ef…-d` — that changes with any edit to the package, any Go
> version, any machine. **A hatch needs an address and there is none.**

So the choice was never between a good hatch and a good exclusion. **There was one option.** Combined
with Amendment A5's *"concurrency primitives are unhatchable in core scope"*, which refuses the `sync`
finding before any weighing begins, the hatch route was closed twice over.

> **EVERY HATCH IN THIS REPOSITORY ASSUMES A STABLE PATH. ANY FUTURE PACKAGE CARRYING GENERATED CODE
> MEETS THE SAME WALL, AND THE ANSWER IS THE SAME: EXCLUSION OR NOTHING.** The choice is forced by the
> registry's key, so *"write a better argument for a hatch"* is not an available move.

Recorded in `scope.go`'s own comment rather than only here, because the next person to hit it should
meet the answer at the site rather than re-derive it.

**Its sibling is `GF-44`**, raised the same week: a derivation that finds one pattern reports
completeness over that pattern, not over the population. `GF-44` is about an instrument that searched
for one idiom; this is about a *person* who read one half of a package. **The same failure, once in a
script and once in a mind, and neither reported any doubt** — which is why the standing answer in both
cases is to state the limit of the method inside the artifact the method produced.

---
### GF-46 — a ruling answers the question asked, and the axes it does not touch stay open whether or not anyone notices

**Raised at I1, 2026-08-28, by Ansh about his own ruling, and it belongs beside `GF-42` because both
are about a decision looking more complete than it is.**

D2(b) was ruled on **the rollback question**: when a simulated crash rolls a real engine's directory
back, how does the harness know what was durable? The ruling answered it — a directory copy per sync
point — and answered it correctly.

**Implementation then hit a second axis nobody had raised.** `AdvanceDurable(seq)` advances the model's
watermark to a *specific* sequence; `rift_db_sync` covers everything submitted and takes no prefix
argument. So the fault differed in **granularity**, not in rollback, and a crash on the C++ engine
would lose strictly less than the same crash on the model — `BUG-005`'s shape through a door the
ruling had not been pointed at.

> **A RULING ANSWERS THE QUESTION ASKED. THE AXES IT DOES NOT TOUCH STAY OPEN WHETHER OR NOT ANYONE
> NOTICES — and a ruled question reads, afterwards, as a settled area.**

Ansh, recording it against himself: *"I ruled on the rollback question because that was the question in
front of me, and granularity was a second axis nobody had raised."*

**Why it sits beside `GF-42`.** That one is about a written *reason* going stale while the obligation
stands. This is about a written *decision* covering less than it appears to. In both, the artifact
looks complete, nothing in it is false, and the incompleteness is invisible from inside it:

| | what looks settled | what is actually open |
|---|---|---|
| `GF-42` | an obligation, because its blocker is stated | whether the blocker still holds |
| `GF-46` | an area, because a question in it was ruled | every axis the question did not name |

**The operational consequence, and it is not "rule more broadly."** A ruling cannot enumerate the axes
it does not know about — that is `GF-45`'s bound, arriving at a decision instead of at an argument.
What it can do is be **read as narrow by whoever implements it**:

> **THE IMPLEMENTER IS THE FIRST PERSON TO MEET THE AXES A RULING DID NOT NAME, AND IS THEREFORE THE
> ONLY PERSON POSITIONED TO REPORT THEM BEFORE THEY ARE BUILT ON.** Reporting one costs a message.
> Building on one silently produces a phase that reports green with its fault made smaller.

That is what happened here, and it is the reason this entry records a near miss rather than a defect:
the gap was found while enumerating the store's calls for the implementation, and reported before any
of it was built on.

**And that is a division of labour, so it is written down as one rather than rediscovered each time.**

| role | sees | owes |
|---|---|---|
| **the architect** | the question, its stakes, the precedents it touches | a ruling on the axis asked about, and the reasons the alternatives were refused |
| **the implementer** | every axis the ruling did not name, because implementation is where they surface | **reporting them before building on them**, not resolving them |

The implementer does not get to pick between the axes a ruling left open — that is the architect's, and
picking silently is how a ruling comes to mean something nobody decided. The architect cannot enumerate
axes nobody has raised — `GF-45`'s bound, arriving at a decision instead of at an argument. **Neither
half is a failing of the other, and the arrangement only works if the first person to see an unnamed
axis says so at the moment they see it**, when it costs one message rather than a phase.

---

### GF-47 — a pinned literal is not a check; a derived population is

**Raised at I1, 2026-08-28, when a pin written to protect `BUG-042`'s fix turned out to be incapable of
catching `BUG-042`'s recurrence.**

`TestTheReproducesLaneAccountsForEveryDirectory` pinned the exact string:

```
accounted=$((checked + skipped + notbundle))
```

That is a **literal**. It fails when the line changes and passes when it does not — so it has to be
edited every time a bucket is added, **which is precisely the moment it was supposed to speak.** When
the two `ROT` paths were found incrementing `failed` and nothing accounted, the fix was to add a
`rotted` bucket, and the pin's response was to fail because its string had moved. It reported the
edit, not the omission.

> **A PINNED LITERAL ASSERTS THAT A LINE HAS NOT CHANGED. A CHECK ASSERTS THAT A PROPERTY STILL HOLDS.
> WHEN THE PROPERTY IS "EVERY BUCKET IS SUMMED", PINNING THE SUM IS ASSERTING THE ANSWER INSTEAD OF
> DERIVING IT — AND THE ANSWER IS THE THING THAT IS SUPPOSED TO BE ALLOWED TO CHANGE.**

**The derived form** reads every `name=$((name + 1))` out of the script and requires each to appear in
the reconciliation, excluding the two that are deliberately not buckets — `failed` counts outcomes,
`dirs` *is* the population. A new bucket is then covered the moment it is written, by nobody.

**This repository has several pinned literals and they are not all wrong.** `tools/gatepin` pins DR-8's
gate enumeration, and that is correct: the property *is* "this exact list, unchanged without a ruling",
so the literal and the property coincide. The distinction is not literal-versus-derived as a style:

> **PIN A LITERAL WHEN THE TEXT IS THE PROPERTY. DERIVE WHEN THE TEXT IS ONE INSTANCE OF THE PROPERTY.**
> Ask what should happen when someone legitimately extends the thing. If the answer is "a ruling", pin
> it. If the answer is "it should just keep working", pinning guarantees it will not.

**Its sibling is `GF-44`**, and the relationship is exact: `GF-44` is a derivation whose search was too
narrow; this is a check that did not derive at all. Both report completeness over the wrong set.

---

### GF-48 — a criterion that has changed the answer three times is not a formality

**Raised at I1 from the corpus rerun, and recorded because the strict-versus-loose distinction has now
paid out three separate times in three phases.**

| when | the loose criterion said | the strict one found |
|---|---|---|
| **A5** | `make corpus` **green in 102 seconds** | *"three bundles silently no longer carrying their findings"* |
| **A6/BUG-022** | the bundle diverged, so it reproduced | **WEAK** — *diverges under `M71` but produces NO FINDING* |
| **I1** | **24 of 24 traces MATCH** on the C++ engine | the criterion is that the **finding returns**, and a fixed bundle matches by design |

**At I1 it was `repro=0` that gave it away**, on every raft bundle — a number that reads like a failure
and means nothing of the sort, because a bundle whose defect is fixed replays identically and *says so*:
*"this schedule's defect is fixed; to see the finding, reintroduce it."*

> **CAUGHT BY READING WHAT A NUMBER MEANT RATHER THAN WHAT IT SAID.** 24 of 24 MATCH was true, was the
> strongest number available, and was not the criterion. Reporting it as the criterion met would not
> have been a lie; it would have been a weaker claim wearing a stronger one's clothes.

> **A DISTINCTION THAT HAS CHANGED THE ANSWER THREE TIMES, IN THREE PHASES, ON THREE DIFFERENT
> MECHANISMS, IS NOT A FORMALITY.** It has never once been the case that the loose criterion and the
> strict one agreed and the extra work was wasted.

---
### GF-49 — a reference implementation does not merely fail to exhibit a class of defect; it makes the class unexpressible

**Raised at I1's close, 2026-08-29, and it is the phase's most valuable result — above every defect it
found, because it says what six phases of green could not have said.**

I1 ran Track A's stack on the C++ engine for the first time. **Five defects surfaced within hours.
Three of them could not have existed on `engine/model` at all:**

| defect | why the model cannot express it |
|---|---|
| `newReplica` opened a real database per range and abandoned it | a model engine is a struct. **A throwaway allocation is free**, and there is no handle to leak and no directory to orphan |
| one engine root shared across a sweep, so each seed inherited the previous seed's data | a model engine **has no past**. `model.New()` is empty by construction; a directory is not |
| `BUG-047`: a crash replaced the engine and the durability callbacks went with it | a model crash is a method call on a live object. **There is no reopen**, so there is nothing for a registration to be lost across |

> **THE MODEL DOES NOT MERELY FAIL TO EXHIBIT THESE DEFECTS. IT MAKES THEM UNEXPRESSIBLE.** A model
> engine has no files, no handles and no past. Every one of these three is a statement about
> *resources that persist and are re-acquired*, and the reference implementation has no such resources
> to make a statement about.

**That is the difference between a gap in coverage and a gap in the language of the test.** A sweep can
be widened; a seed count can be raised; a workload can be taught a new operation. **None of that
reaches a defect whose precondition cannot occur in the model at all.** The three above were not
under-tested for six phases — they were untestable, and 25,000 exit-run seeds would not have moved
that number.

**And it is measured rather than argued.** The claim is not *"a model might miss things"*, which is
unfalsifiable and useless. It is: here are three specific defects, here is why each one's precondition
is absent from `engine/model` by construction, and here is the phase in which each surfaced — the first
one where a real engine was underneath.

**What it does NOT say, and the distinction is the whole value of the model.** `engine/model` is not
weak and is not retired. Every Track A safety property was measured against it, it is the control that
makes an I1 divergence a *finding* rather than an observation, and the two inconclusives I1 found were
classified as inherited in ~1.5 seconds each by running them on it. **A reference implementation is a
statement about semantics, and semantics is exactly what it is good for.**

> **WHAT A REFERENCE IMPLEMENTATION CANNOT VERIFY IS EVERYTHING THAT IS TRUE OF THE RESOURCE RATHER
> THAN OF THE SEMANTICS.** Files, handles, processes, and the past. The correct response is not to
> distrust the model; it is to know which half of the claim it carries, and to run the other half
> against the real thing before believing it.

**Recorded in `docs/TRACK-A.md`'s limits section as well as here**, because every Track A number was
taken against an engine that could not express this class, and a reader of those numbers is entitled to
know it at the number rather than in a bug ledger.

---
### GF-50 — three observations that agree are a coincidence until one is sought that disagrees

**Raised at I1's close, 2026-08-29, from a mechanism I invented, named, wrote into a carried record,
used to justify a ratified disposition, repeated twice — and then refuted with one observation.**

**What was claimed.** Background jobs kept being stopped. Three kills, at **59m**, **1h35m53s** and
**30m04s**. I called it *"the runtime's per-job duration ceiling"*, wrote it into
`docs/CARRY-FORWARD.md` as the mechanism behind `make mutant-covered` being unrunnable, sized sweep
chunks against it, and stated it twice in reports.

**What refuted it.** One kill at **4m35s**.

| | durations |
|---|---|
| completed | 3m, 17m04s, ~25m, 30m49s, 31m15s, 33m27s |
| killed | **4m35s**, 30m04s, 59m00s, 1h35m53s |

**The ranges overlap.** A job completed at 33m27s and one was killed at 4m35s, so *"long jobs get
killed"* is not what is happening.

> **THREE OBSERVATIONS THAT AGREE ARE A COINCIDENCE UNTIL ONE IS SOUGHT THAT DISAGREES.** All three
> kills were long. Nothing about them was wrong. What was wrong was treating "every case I have seen
> has property P" as "P is the mechanism", without ever asking what a case *without* P would look
> like — which was one line of arithmetic away the whole time, because the completions were in the
> same log.

**What survived and what did not**, separated because a retraction that takes the conclusion with the
mechanism is as careless as the original claim:

| | state |
|---|---|
| `make mutant-covered` cannot complete here | **stands** — on its own measurements: two runs, 14 of 71 and then fewer, both stopped |
| the disposition Ansh ratified from it | **stands** — it rested on the lane's cost and its stoppages, not on why |
| *"the runtime's per-job duration ceiling"* | **retracted** — the cause is **undetermined**, and both records now say so |

**The correction is cheap and the belief was not.** It cost one table. The belief had already been
written into a carried record, cited in a ruling, and used to size four separate runs.

#### Second instance, at I2: the wire-symmetry claim

The first end-to-end cluster reported **1794 bytes out, 1794 in**, and I wrote that equal counts are a
round trip and an asymmetry would be its own finding. Two later runs:

| run | out | in |
|---|---|---|
| first | 1794 | 1794 |
| second | 1850 | 1794 |
| third | 1005 | 1018 |

**Asymmetric in both directions. The equality was luck**, and one observation had been enough to state
a rule about the network.

**Replaced with a bound derived from the flush cadence** rather than from the sample: counters are
written every 100 ms by each node independently, so the gap is bounded by
`n(n−1) × 2 frames × ~29 B` ≈ **696 bytes** at n=3. Measured gaps across three runs: **94, 66, 95** —
comfortably inside, and now falsifiable. A gap far outside it, especially `out ≫ in`, is bytes claimed
written that nobody read.

> **BOTH TIMES THE CORRECTION CAME FROM TAKING MORE MEASUREMENTS RATHER THAN FROM REASONING HARDER,
> AND BOTH TIMES THE FIRST OBSERVATION WAS THE ONE THAT FELT CONCLUSIVE.** Three long kills felt like
> a duration ceiling. A perfect 1794/1794 felt like a round trip. Neither feeling survived a fourth
> data point, and in both cases the fourth was cheap to get and nobody had gone to get it.

#### The second hypothesis, and it is here on purpose

Immediately after the retraction, a pattern suggested itself: **both kills had struck jobs launched by
a Bash call that did something else first** — a `grep` of the previous chunk's log, *then* the launch.
The chunks that completed were launched by calls that only launched.

Two supporting cases. It was recorded **as a hypothesis**, with the note that the very next relaunch
was also a combined call, so it would be tested immediately either way.

**It completed. The hypothesis is refuted, and that is the entry.**

> **RAISING A HYPOTHESIS, NAMING THE OBSERVATION THAT WOULD REFUTE IT, AND REPORTING THE REFUTATION IN
> THE NEXT AVAILABLE RUN IS EXACTLY THE HANDLING THE FIRST ONE SHOULD HAVE HAD.** The difference is
> not care or intelligence. It is that the second was stated as a claim about the world with a named
> test, and the first was stated as a fact.

Both are in one entry deliberately: **the correction taking effect on the next occasion, rather than
being promised.** The first hypothesis cost a paragraph in a carried record and four mis-sized runs.
The second cost one sentence and was settled for free by work already scheduled.

---
### GF-51 — a signature change needs its own gate even when every existing gate still passes, especially then

**Raised at I1's close from the A7 amendment, and it is a coverage-shaped hole nobody would think to
look for.**

`SweepRaftWithProgress`'s hook was widened from `func(seed, done, total int)` to carry the running
census. Two tests cover that function and both are A7's:

| | what it asserts |
|---|---|
| `TestASweepCountsEachSeedOnce` | the final `Seeds` count, once per seed |
| `TestTheProgressHookSeesEverySeedInOrder` | the hook fires for every seed, in order |

**Both PASS when the new argument is replaced with a zero value.** Measured, by doing exactly that
before writing anything.

> **AN ARGUMENT ADDED TO A FUNCTION IS NOT COVERED BY THE TESTS THAT COVERED THE FUNCTION. WIDENING A
> SIGNATURE WIDENS THE SURFACE, AND THE OLD GATES KEEP PASSING OVER THE PART THAT DID NOT EXIST WHEN
> THEY WERE WRITTEN.**

**"Especially then" is the operative half.** A signature change that broke the existing tests would
announce itself — the compiler and the suite would both object, and nobody would ship it unexamined.
**A signature change that leaves every gate green is the dangerous one**, because green is exactly the
signal that says "covered", and here it meant "unchanged in the parts anyone had thought to check".

> **THE GREEN IS NOT WRONG. IT IS ANSWERING THE OLD QUESTION.**

**Its siblings are the day's other two, and the three together are one shape at three scales:**

| | the enumeration | what bounded it |
|---|---|---|
| `GF-44` | a derivation's search | one idiom |
| `GF-49` | a reference implementation's expressible defects | no files, handles, or past |
| **`GF-51`** | **a test suite's covered surface** | **the signature as it stood when the tests were written** |

Each reports completeness over the set it can see. **None of the three reports any doubt**, which is
why the remedy in every case is to state the method's boundary inside the artifact the method produced
— and, for a signature change specifically, to write the gate in the same commit that widens the
signature, before the widening is used anywhere.

**The gate that landed** asserts the census accumulates, matches the returned census at the last seed,
and is a copy. Its accumulation half is induced; its copy half is not and cannot be — Go passes structs
by value — and the test says so rather than letting an assertion the language guarantees be counted as
a check.

---
### GF-52 — a declared threshold can fail in two opposite directions, and a guess written down early is still a guess

**Raised at I2's design, 2026-08-29, when deriving four pre-declared thresholds changed two of them —
and the two were wrong in opposite directions, which is what makes the pair worth recording rather
than either alone.**

| | the guess | what derivation gave | the failure mode |
|---|---|---|---|
| chaos throughput | ~~≥ 50% of steady state~~ | **≥ (K−2.5E)/K**, = 75% at E=1s | **too loose** |
| cgo boundary cost | ~~> 25% absolute is a finding~~ | **no regression beyond +5 points** vs B5's signed figures | **too tight** |

**Too loose.** At `E = 1s`, 50% passes a cluster recovering in five seconds — two to three times worse
than its own timing parameters predict — and reports success.

> **A THRESHOLD LOOSER THAN THE DESIGN'S OWN PREDICTION CANNOT FAIL ANYTHING THE DESIGN WOULD CALL
> BROKEN.** It is not a weak check. It is not a check.

**Too tight.** An absolute 25% is already exceeded by numbers B5 measured and Ansh signed: **+33% at 8
pairs, +111% at 1**.

> **A THRESHOLD THAT FAILS THE CURRENTLY-SIGNED STATE IS NOT A THRESHOLD, IT IS A BUG IN THE
> THRESHOLD** — and its fate on first contact is to be *"fixed"* on the spot, **which retroactively
> makes the declaration worthless.** The ritual of declaring in advance survives; the protection does
> not.

**Both were picked from intuition. Both would have produced a comfortable first result** — one by
passing a broken system, one by being quietly relaxed. **The derivation caught both**, and neither was
visible by inspection: 50% and 25% are perfectly reasonable-looking numbers.

> **DECLARING IN ADVANCE IS ONLY WORTH SOMETHING IF THE DECLARATION IS DERIVED. A GUESS WRITTEN DOWN
> EARLY IS STILL A GUESS**, and writing it down early makes it *harder* to question, because it now
> carries the authority of having been declared.

#### The third amendment is a different fault: a category error in the metric

Threshold 3 was ~~*"chaos p99 within 10× of steady-state p99"*~~. Derived:

```
operations caught by a leader change wait out recovery R
fraction affected = R/K ≈ 15–25%,  far above the 1% p99 asks about
therefore p99(chaos) ≈ R ≈ 1.5–2.5E,  not a multiple of p99(steady)
```

Steady-state p99 measures the write path. Chaos p99 measures **how long a leader election takes.**

> **A THRESHOLD CAN BE WRONG BY COMPARING TWO QUANTITIES THAT DO NOT ANSWER THE SAME QUESTION, AND NO
> AMOUNT OF TUNING FIXES THAT.** The ratio is not a bad number; it is not a number about anything.
> 10× would have failed a healthy cluster; 500× would have passed a broken one; every value in between
> is equally meaningless.

The corrected form states the bound **against the quantity that determines it** — `p99 ≤ 3E`,
`p999 ≤ 5E` — which is the general remedy: **name the thing the metric actually depends on, and bound
it against that.**

#### And one operational detail worth keeping, from Threshold 1

`R ≥ K` is reported **specifically as the cluster never reaching steady state between kills**, never as
low throughput.

> **A PERMANENT-RECOVERY CONDITION THAT REPORTS AS A THROUGHPUT NUMBER IS A DIAGNOSIS POINTING AT THE
> WRONG COMPONENT.** The reader goes looking at the write path, the engine, the batch size — and the
> answer is that the cluster is never up. This project has paid for that shape before.

---
### GF-53 — a mechanism that responds correctly to the case you meant and to a case you did not mean has not distinguished them

**Raised at I2, from an induction that fired for the wrong reason.**

The supervisor's claim is that it delivers **`SIGKILL`**, not `SIGTERM`, and that claim is I2's whole
argument for separate processes: a graceful shutdown flushes, and a flushed node has lost nothing.

The induction was to swap the signal and require the test to fail. **It failed.** That reads as
verified — and it was worthless, because the fixture did not trap `SIGTERM`. An untrapping process dies
the same way under both signals, so the test was distinguishing *"the process is gone"* from *"the
process is here"*, which both signals satisfy.

> **THE TEST FIRED, WHICH READS AS VERIFIED, AND IT WOULD HAVE FIRED IDENTICALLY UNDER THE WEAKER
> CONDITION.** A mechanism that responds correctly to the case you meant and to a case you did not mean
> has not distinguished them. It has answered a question that happens to have the same answer.

**This is DESIGN-A7 §8.1b's two-numbers rule, arriving at an induction rather than at an oracle.** There
the rule is that an oracle must fire on its planted defect **and** be silent on a clean tree, because
either number alone is satisfiable by an instrument that is not discriminating. Here both numbers were
present — pass before, fail after — and the instrument still was not discriminating, because the
*mutation* did not isolate the property either.

> **THE TWO NUMBERS BOUND THE ORACLE. THEY DO NOT BOUND THE MUTATION.** An induction is a claim that
> *this* change breaks *that* property, and it is only evidence if no weaker change would have broken
> it the same way.

**What the corrected version proves.** The fixture now installs a `SIGTERM` handler that writes a
marker and exits 0, and the test asserts the marker is **absent** after a kill:

| | `SIGTERM` | `SIGKILL` |
|---|---|---|
| trapping process | **survives the signal**, runs its handler, leaves the marker | **dies**, leaves nothing |

So the test now distinguishes exactly the difference the configuration exists for — **a process that got
to run its deferred closes from one that did not** — rather than distinguishing alive from dead, which
is not in question.

**And the cost of getting this wrong is not hypothetical.** A chaos lane sending `SIGTERM` would report
that the database survives thousands of kills, and every one of them would be a clean shutdown. **The
headline claim would be true of a fault nobody is worried about.**

---
### GF-54 — a guarantee that is correct and load-bearing destroys the thing the harness built on top of it needs

**Raised at I2 with three instances in one week, each in a different layer, each with the guarantee
entirely correct.**

| the guarantee | correct because | what it destroyed |
|---|---|---|
| **`Apply` never blocks on I/O** (`engine.Engine`) | it is what lets the simulator model an unsynced window at all — removing it removes the fault the phase injects | a snapshot taken after `Apply` captured a directory the data had not reached. `BUG-044`: *a snapshot is a claim about what is on disk, and taking it after an operation that deliberately does not touch the disk is a claim about nothing* |
| **null means unbounded** (`rift.cc`) | *"there is no way to pass 'unbounded' as bytes, so the distinction has to live in the pointer"* | `Set(key, nil)` became a rejected argument. `BUG-B008` |
| **`SIGKILL` means kill** (`chaos/`) | a graceful shutdown flushes, and a flushed node has lost nothing — the whole reason I2 runs separate processes | a killed node reports nothing about having been killed. Three real pids, zero wire bytes, because every node died before it could say what it had done |

> **THE SHAPE: THE HARNESS NEEDS THE THING TO BE OBSERVABLE, AND THE GUARANTEE'S WHOLE CONTENT IS THAT
> IT IS NOT.**

The third is the purest statement of it. **The property that makes `SIGKILL` worth using is exactly the
property that erases the evidence it was used.** A signal a process can handle is a signal that leaves
a trace; one it cannot handle leaves none, and those are the same fact said twice.

**And none of the three is a defect in the guarantee.** Each is documented at its definition, each is
load-bearing, and weakening any of them would weaken the phase that depends on it. **The defect is
always in the joint**, and the joint is always owned by whoever is building on top.

#### The general remedy, and it is the same in all three

> **THE OBSERVATION LIVES OUTSIDE THE THING BEING KILLED, OR IT DOES NOT SURVIVE.**

| instance | where the observation moved to |
|---|---|
| non-blocking `Apply` | a `Sync` *before* the snapshot — the harness stops asking the disk a question the contract says it may not answer yet |
| null-means-unbounded | a *separate mapping* for keys and values, so the bound case keeps its meaning and the value case never asks for one |
| `SIGKILL` | counters written to disk **during** the run, every 100ms, by write-and-rename — outside the process, before the kill |

**The failure mode is always the same: putting the observation inside the thing whose destruction is
being observed.** A node reporting its own death, a snapshot asking a buffer what is on disk, a value
asking a pointer whether it is bounded. In each case the observer and the observed are the same object,
and the guarantee is what separates them.

**This is `GF-49`'s neighbour rather than its instance.** `GF-49` is about a substitute that cannot
express a class. This is about a real thing whose correctness makes a class unobservable — the model
could not express a lost handle; a killed process cannot report anything at all. **One is a limitation
of the stand-in, the other is a consequence of the real thing being real.**

---
### GF-55 — a claim that survived: one Node interface, two modes, asked of the real stack for the first time

**Recorded at I2, and it is a claim SURVIVING rather than an implementation note.**

`node/`'s package doc has said since A0:

> *"`Driver` drives a `sim.Node` — the same interface the simulator's loop drives, with the same
> `Handle(Event, Scheduler)` signature. There is no build tag, no `if realMode`, and no second
> implementation of node logic."*

And the argument for why that matters, in the same doc:

> *"If real mode needed its own copy of the protocol, the deterministic simulation would be verifying a
> program that never ships, and every seed in the corpus would be evidence about the wrong artifact."*

**It has been unexercised since it was written.** `node/` existed to make the mailbox rule stop being
provisional — `scope.go` carried it in those words, *"A0 does not exit until node/ exists and the rule
has end-to-end teeth"* — and it was exercised against **fixtures**, a counter and a toy.

**At I2 it was asked of the real stack, and it held:**

| | |
|---|---|
| `store.Node` | implements `sim.Node` |
| `node.Driver` | drives a `sim.Node` |
| `tcp.Transport` | implements `sim.Transport` |

**Nothing adapts one to another.** No shim, no wrapper, no mode branch. The Raft store that ran 25,000
seeded schedules is the same object, byte for byte, that now runs behind a mailbox over a TCP socket in
its own process.

> **A CLAIM MADE EARLY AND EXERCISED LATE IS A CLAIM THAT HAS BEEN CARRIED, NOT TESTED**, and this
> project has found four things this week that were carried and false. This one was carried and true,
> and that is worth recording with the same weight — the register is not a list of failures, it is a
> list of what measurement found.

**What would have shown a failure**, so this is falsifiable rather than a congratulation: any adapter
between the three types, any `if realMode` in `store/`, any second `Handle`. There are none, and the
determinism pass's scope table pins the split that keeps it that way — `node/` out, everything it
drives in.

---
### GF-44's first recurrence, inside its own document, four days after it was written

**Recorded here rather than as a new form, because it is `GF-44` exactly.**

`DESIGN-I1` §1 stated that the stack calls **two** methods the frozen interface lacks — `VisibleSeq()`
and `Crash()`. There are **three**. `AdvanceDurable()` is the third and is the one the whole §12 gap
turns on.

**The cause is `GF-44`'s, verbatim: a derivation that finds one pattern reports completeness over that
pattern.** The doc was written by reading the *restart path* and the *crash path* — the two paths its
author had in mind. The durability path was never read, so the method that drives it was never seen,
and §1's table presented an enumeration of two paths as an enumeration of the interface gap.

**What would have caught it takes seconds and is mechanical:**

```
grep -ohE "\b(n\.db|m\.db|d\.db|r\.db)\.[A-Z][A-Za-z]*" store/*.go sim/toy/*.go \
  | sed 's/.*\.//' | sort | uniq -c | sort -rn
```

```
12 Apply   5 Get   4 VisibleSeq   3 AdvanceDurable   2 NewIter   2 DurableSeq   2 Crash
```

That is the enumeration run *afterwards*, and it is what produced the correction.

> **A DESIGN DOCUMENT THAT NAMES AN INTERFACE GAP ENUMERATES THE CALLS. IT DOES NOT REASON FROM THE
> PATHS ITS AUTHOR HAD IN MIND.** The derivation-finds-one-pattern rule applies to a document exactly
> as it applies to a script, and a document is the more dangerous of the two, because nobody expects a
> prose table to have a search behind it that could have been wrong.

**Four days between `GF-44` being written and recurring in the next document its author wrote.** That
interval is the entry's real content: a general form is not a fix, and knowing the rule did not
produce the grep.

**And it recurred a second time the same day, in the lane built to enforce `GF-41`.** `tools/anchorcheck`
read `sim/mutants/` only, with a comment saying blind patches *"live elsewhere and are checked by their
own lane."* True and irrelevant: `make blind` checks whether a blind patch is **killed**, not whether it
is still **anchored**, and reports a stopped-applying patch as `ROT` rather than preventing it.

**Measured the day after the lane landed: 9 of 20 blind patches were prose-anchored**, and
`blind-riftcgo-wildcard` rotted within a day when the comment it matched on was rewritten — by the same
hand that had written the rule.

> **THE RULE'S OWN DEFECT OCCURRED IN THE DIRECTORY THE RULE DID NOT READ.** A check that covers one
> directory reports completeness over that directory, in exactly the way a derivation that finds one
> pattern reports completeness over that pattern.

The lane now reads both directories; all nine were re-anchored with the same byte-identical equivalence
proof the mutants got.

---
### GF-32 — a doc written before its code can specify a mechanism the code makes unnecessary

**Raised by `B5-D2`, B5.0 — the phase's first finding, and it is not a doc erratum.**

`DESIGN-B5` §2 specified a catch-all at every `extern "C"` entry point, plus a function that throws
through the boundary to induce it. The build refused: `cannot use 'try' with exceptions disabled`.
**The archive has been `-fno-exceptions` since B1.** `throw` does not compile, `try` does not compile,
and `operator new` **aborts** rather than throwing.

> **A DESIGN DOC WRITTEN BEFORE ITS CODE CAN SPECIFY A MECHANISM THAT THE CODE'S OWN CONSTRAINTS MAKE
> UNNECESSARY.**

**AND THE CHEAP REPAIR IS TO WEAKEN THE CODE UNTIL IT MATCHES THE DOC.** That direction is always
available, and it is always wrong. Here it was four small steps, each individually reasonable:

1. drop `-fno-exceptions` from `RIFT_CXX_FLAGS`;
2. add the `try`/`catch (...)` wrapper the doc specifies;
3. make `rift_test_throw` actually throw, so the backstop is induced;
4. ship a **weaker property that matches a paragraph.**

> **IT IS INVISIBLE IN REVIEW, BECAUSE THE RESULT IS A DOCUMENT AND AN IMPLEMENTATION IN AGREEMENT.**
> A future reader meeting a catch-all with a design citation beside it will assume it was reasoned —
> and it *was*, just before the reasoning met the build.

**WHAT THE WRAPPER WOULD HAVE COST, which is the half that makes this a finding rather than a
near-miss.** A `catch (...)` does not merely add a line: it **converts an exception into a code and
loses what the exception was.** A boundary that reports `RIFT_INTERNAL` for everything is a boundary
where **every failure looks the same** — allocation failure, a contract violation, a future
contributor's `throw` — and the first thing anyone debugging across it would want is the one thing it
discarded.

**So `GF-31` is confirmed from the other direction.** There, a fix made a defect unrepresentable and
the mutant's survival was the evidence. Here, **the compiler supplies the guarantee the wrapper would
have approximated**, and supplies it more strongly:

| | catch-all | `-fno-exceptions` |
|---|---|---|
| an exception crossing | caught, **identity lost** | **cannot exist** |
| enforcement | a rule each entry point remembers | a compiler flag |
| what a reviewer must check | every function has the wrapper | one line of the build |

**The rule that follows, and it is about where a claim lives:**

> **NOTHING IN THE SOURCE CAN ASSERT ITS OWN BUILD. A PROPERTY HELD BY A FLAG IS ENFORCED WHERE FLAGS
> LIVE, OR IT IS NOT ENFORCED.**

`cpp-scan` part 7 reads the compile options and refuses a tree where `-fno-exceptions` is missing or
does not reach `rift_capi` — induced by removing it.

**The tell, for next time.** When a doc specifies a *mechanism* rather than a *property*, the mechanism
is a guess about how the property will be obtained. **Write the property; discover the mechanism.**
§2 should have said *no exception crosses this boundary* and left how open — and it now does, with the
mechanism recorded as what the code turned out to already have.

---

### GF-31 — a fix that makes a defect unrepresentable costs the instrument that detected it

**Raised by** `BUG-B006`'s fix and `BM113`'s survival. **Both halves are recorded, because the free-win
reading is the wrong one.**

**THE STRENGTHENING.** `BUG-B006` was two implementations of one rule disagreeing. The fix was not to
correct the wrong one — it was to make the rule **one function**, `WidensUpperBound`, taking a **bare
user key**. Through that signature there is no internal key to compare against, so **the defect cannot
be written**.

> **A FIX THAT MAKES A DEFECT UNREPRESENTABLE IS STRONGER THAN ONE THAT MAKES IT WRONG.**

**AND NO MUTANT CAN ASSERT THE DIFFERENCE AT THE SITE IT PROTECTS.** `BM113` was aimed there and
**survived** — an equivalent mutant, because the mutated comparison behaves identically. **The
survival is the evidence for the strengthening**, which is a strange and useful thing: the class was
re-aimed at the caller, where an inlining edit could still reintroduce it.

**THE COST, AND IT IS REAL.** Two implementations of one rule can **disagree**, and a disagreement is
**detectable by comparison** — which is precisely what `VerifyTables` did, loudly, on every Open. One
implementation cannot disagree with itself.

| before the fix | after |
|---|---|
| two rules, comparable | one rule, nothing to compare |
| a disagreement is caught by `VerifyTables` | the rule can be **wrong in one place and consistent everywhere** |
| the reproduction asserts *writer equals classifier* | that assertion now passes under `BM113` |

> **COLLAPSING TWO IMPLEMENTATIONS INTO ONE REMOVES A FAILURE CLASS AND REMOVES THE INSTRUMENT THAT
> DETECTED IT.** What is left must be asserted **against the invariant**, not against a second
> implementation.

Hence `BoundWideningIsAStatementAboutUserKeys`, which tests the predicate directly and in **both tag
directions** — a rule comparing internal keys would be right about one of them by accident.

**AND IT IS NOT AN ARGUMENT AGAINST CONSOLIDATION.** `BUG-B006` cost a permanently unopenable database;
the instrument it removed only ever fired *because* the defect existed. The point is that **the ledger
has two entries, not one** — and a consolidation that does not add the invariant assertion has spent
an instrument and bought nothing to replace it.

**The tension with `B3-D2b` is deliberate and worth stating**, because these look contradictory: the
harness keeps a **second** implementation of `Covers` on purpose, and the engine collapses to **one**
implementation of `WidensUpperBound` on purpose.

> **THE DIFFERENCE IS WHO THE TWO IMPLEMENTATIONS BELONG TO.** Two inside one engine are a
> maintenance hazard whose disagreement is a bug. **One in the engine and one in the checker are an
> oracle** — and collapsing *those* is the shared blind spot `B3-D2b` forbids.

---

### GF-30 — disbelieve a new instrument's first three outputs

**Raised by** the differential rig's three findings about **itself** (`HARNESS-024`, `-025`, `-026`),
which arrived before its first finding about the engine could be trusted.

**THE METHOD, AND IT IS ONE QUESTION:**

> **TAKE A RESULT THE RIG PRODUCED AND ASK WHAT WOULD HAVE TO BE TRUE OF THE RIG FOR THIS TO BE WHAT
> IT SAYS.**

| the output | the question | what it was |
|---|---|---|
| *"recovered nothing"* | **could the rig have failed to look?** | the reopen had failed and the driver had no field for it |
| *"recovered less than promised"* | **is one watermark the whole contract?** | the contract permits a range; B1's oracle already knew |
| *"the mutant survived"* | **does the workload reach the thing it blinds?** | one op per batch, so the intra-batch rules were never exercised |

> **NONE OF THE THREE IS DISCOVERABLE BY READING THE RIG. ALL THREE ARE DISCOVERABLE BY DISBELIEVING
> ONE OF ITS OUTPUTS FOR ONE MINUTE.**

**Why reading does not find them.** Each is a **missing** case, not a wrong one: an unrecorded status,
an unmodelled second element, an unreached shape. Reading checks that what is there is right, and all
three rigs were right about everything they contained.

**STANDING PRACTICE FOR EVERY NEW INSTRUMENT — I1 and I2 included.** A rig's **first three outputs are
the cheapest place it will ever be wrong**: nothing depends on them yet, no result has been reported
from them, and the author still remembers what the code was supposed to do. The same doubt applied
after fifty runs costs a retraction.

**It composes with `GF-29`.** That one says a single instrument defect raises the prior on another —
so the first disbelief is not the last, and the moment to look again is immediately after the first
fix, while the output is being read closely for the first time.

---

### GF-29 — a broken instrument hides the questions you would ask about a working one

**Raised by** `HARNESS-021` and `HARNESS-022`, B3.7b — the same instrument, two defects, found hours
apart and **the second only findable after the first was fixed.**

`HARNESS-021` made write amplification print **`0.00`**. While that number stood there was **nothing
to be suspicious of**: no reason to ask what conditions it was true under, because it plainly was not
true at all. Only once it read `8.08` did the next question become askable — *is this a steady-state
value or a snapshot mid-cycle?* — and the answer was `HARNESS-022`.

> **FIXING ONE INSTRUMENT DEFECT IS WHAT MAKES THE NEXT ONE FINDABLE. SO A SINGLE DEFECT IN AN
> INSTRUMENT SHOULD RAISE THE PRIOR ON THERE BEING ANOTHER**, and the moment to look is immediately
> after the first fix, while the output is being read closely for the first time.

**AND THE COUNTERFACTUAL IS THE ARGUMENT, which is why it is recorded as a number and not a worry:**

| what the broken field returned | what would have happened |
|---|---|
| **`0.00`** — impossible | caught in one reading; the instrument was **saved by the magnitude of its own error** |
| **`4.2`** — plausible | `BENCHMARKS.md` would carry a wrong write-amplification curve, `B3-D3` would have been ruled on it, and **nothing in the repository would disagree** |

> **A VALUE THAT CANNOT BE TRUE IS A GIFT. THE DANGEROUS INSTRUMENT DEFECT IS THE ONE THAT RETURNS
> SOMETHING PLAUSIBLE.**

**What follows for how instruments are built.** Prefer a reading whose failure mode is *impossible*
over one whose failure mode is *merely wrong*: a count that must be non-zero, a ratio with a floor
that physics forbids crossing (*every byte is written to the WAL and again to a table, so write
amplification below 2 is the instrument, not the engine*), a bound the workload cannot exceed. Those
are the assertions `AmpInstrument.*` now carries, and they exist because **a plausible wrong number is
indistinguishable from a result.**

**THE OPERATIONAL CONSEQUENCE, WHICH IS THE PART THAT CHANGES WHAT YOU DO NEXT:**

> **WHEN AN INSTRUMENT IS FOUND WRONG, THE NEXT QUESTION IS WHAT IT WAS HIDING — NOT WHETHER IT IS
> FIXED.**

Fixing it is the easy half and feels like completion. The instrument was **producing readings the
whole time it was wrong**, and every one of them was accepted; so the fix does not end the incident,
it **starts the audit** of what those readings were used to conclude. At B3.7b that audit was one
question long — *is `8.08` steady-state?* — and it found `HARNESS-022`.

**It is `GF-26` from the other side.** `GF-26` says an instrument with no class floored against it has
unknown sensitivity. This says an instrument that is *itself* broken has unknown sensitivity **about
its own defects** — and both are answered the same way: put a class under the instrument, not only
under the thing it measures.

---

### GF-26 — a new regime is not landed until one class is floored against it

**Raised by** the `compact` sweep regime, B3.7a.

The regime landed at **3545 kill points, 0 violations**. That reads as a strong result and it was a
**green with unknown sensitivity**: the sweep visited every Env call the compaction makes, and
nothing in it had been shown capable of *detecting* anything there.

> **A SWEEP THAT VISITS A PATH PROVES THE ENGINE RECOVERS THERE. IT SAYS NOTHING ABOUT WHETHER A
> DEFECT THERE WOULD BE DETECTED.**

**IT IS THE VACUOUS-GREEN SHAPE AT REGIME GRANULARITY** rather than at checker granularity — `GF-1`'s
family, one level up. `GF-1` asks whether a *lane* verifies an absence; this asks whether a *whole
regime* does. The failure looks better than `GF-1`'s, because a large kill-point count and a zero
violation count read as thorough.

**`BM109` is what closed it**: remove the directory sync after the compaction's output files, and the
sweep reports **10 detections of 3530, first at kill point 663**. The regime now has a floor, so a
change that quietly stops it reaching the compaction fails the campaign instead of reporting 3545
green points over a path it no longer enters.

> **THE STANDING RULE: A NEW REGIME IS NOT LANDED UNTIL AT LEAST ONE CLASS IS FLOORED AGAINST IT.**

**Stated as standing because B4 will add regimes** — the differential rig against `engine/model`, the
crash-consistency sweep at other cap settings — and the same question arrives with each. The cost is
one mutant per regime, which is small beside a regime whose green means only that it ran.

---

### GF-27 — extending a regime is paid for by every floor already measured against it

**Raised by** §8.2a's decision at B3.7a.

Reaching the L0 compaction trigger needs four flushes — roughly four times the `flush` regime's whole
workload. Folding it into `flush` was the obvious move and would have been wrong:

> **AN EXTENSION THAT MULTIPLIES A REGIME'S KILL-POINT COUNT DILUTES EVERY CLASS ALREADY MEASURED
> AGAINST IT. THE COST OF EXTENDING A REGIME IS PAID BY EVERY EXISTING FLOOR.**

Every rate in `FLOORS.txt` is a fraction of that count. Quadruple the denominator and every B2 rate
falls, **while no class has lost any power at all** — the classes are exactly as detectable as they
were, at points that are now a smaller share of a larger space.

**B2 already paid this once**: the manifest took `default` from 175 to 300, every rate fell, not one
detection count did. The lesson then was `GF-6` — *keep a count floor beside every rate floor*. The
lesson now is the one before it: **do not move the denominator unless the work requires it.**

**So the answer is a separate regime, not a wider one.** `default` and `flush` stayed byte-identical
at 305 and 990; no floor moved; the re-measurement obligation was discharged **by being made
unnecessary rather than by being performed.**

**It is the same logic as the regime column itself** (§8.4, ratified at A6): a number measured at
non-default caps never aggregates with a default-cap number, because the two denominators are
incomparable. **A regime is the unit at which measurements are comparable** — so the way to add
coverage without invalidating measurements is to add a unit, not to widen one.

---

### GF-24 — a count with nothing to derive beats a threshold with a justification

**Raised by** B3.6's first attempt at file lifetime.

Retiring a compaction input, the question is *does anyone still hold this table?* The first version
compared `shared_ptr::use_count()` at the retirement site against a threshold **worked out by
reasoning**:

```cpp
// `t` is one reference, and the caller's `in_l0`/`in_l1` vector is another.
// Anything above two is a reader.
if (t.use_count() > 2) { ... }
```

**A snapshot's input file was deleted underneath it.**

**THE SPECIFIC ERROR IS SUBTLER THAN THE RULE, AND IT IS WHY THE RULE IS NEEDED.** `t` is declared
`const std::shared_ptr<sst::Table>&` — **a reference to the vector's element, not a copy.** It adds
nothing to the count. The arithmetic was correct about a world with one more reference in it than
this one has.

> **REASONING CANNOT CATCH THIS CLASS, BECAUSE THE REASONING IS WHAT IS WRONG.** Re-reading the
> justification confirms it. Every step follows; the premise about how many holders exist is the
> defect, and it is the same premise the re-reading uses.

**The remedy is structural, and it generalises past reference counts:**

> **PREFER A COUNT WITH NOTHING TO DERIVE OVER A THRESHOLD WITH A JUSTIFICATION. THE JUSTIFICATION IS
> THE PART THAT GOES STALE.**

Every retired table now goes on **one list**, and the count is taken in **one place** with **one
holder to subtract**: `use_count() == 1` on `obsolete_` means the list is the only holder. There is no
arithmetic, so there is nothing to get wrong — and, more to the point, **adding a local anywhere else
cannot move the answer.** The original threshold would have silently changed meaning the first time
someone introduced a variable between the vector and the call.

**The family.** It is `GF-13`'s cousin — *a bound derived from another instrument's measurement cannot
be raised* — with the derivation done in a comment rather than by an instrument. `GF-13` says where a
number should come from; this says a number you have to *argue for* is a number that will be wrong
later, whoever argues.

---

### GF-28 — a guard phrased as "not the other one" changes meaning when a third appears

**Raised by** the sweep workload's flush gate, B3.7a.

```cpp
if (regime != SweepRegime::kFlush) return;   // written when there were two
```

It reads *"only the flush regime continues"* and means *"every regime except flush stops"*. Those are
the same sentence with two regimes and different sentences with three. When `compact` arrived it
**silently returned** — so the first compaction sweep ran the six-key default workload and reported
**305 kill points with a census containing no compaction at all.**

> **THE FAILURE IS SILENT BECAUSE THE GUARD STILL EVALUATES.** Nothing is malformed, nothing throws,
> no case is unhandled. A closed `switch` would have failed the build on the new enumerator; a
> comparison against one member of that enum will not, because the expression stays valid and its
> meaning quietly changes.

**The fix is to name what the guard MEANS rather than what it excludes** — `if (regime ==
kDefault) return;`, *"the default regime stops here"* — which stays true whatever is added. The
general form:

> **PHRASE A GUARD BY WHAT IT ADMITS, NOT BY WHAT IT REJECTS. THE REJECTED SET GROWS WITHOUT
> TOUCHING THE CODE.**

**And the tell that it had happened was a NUMBER, not an error**: 305 kill points where thousands
were expected. The census — which lists Env calls by kind — is what made it diagnosable in one look,
because a compaction sweep with no `kEnvDeleteFile` entries is not a sweep of a compaction.

**`-Wmissing-field-initializers` deserves its line beside it.** Adding a member to `Driver` broke two
positional aggregate initialisers, and the compiler said so. **That is the compiler doing the job a
convention would otherwise have had to** — the call sites are designated-initialised now, so the next
member cannot silently land in the wrong slot.

---

### GF-25 — a gate on the mechanism and a test on the answer are two instruments, not one

**Raised by** B3.6's file-lifetime gate, and it is `GF-22` one level down.

`GF-22`: two defects whose symptoms cancel are invisible to every test that asserts an **answer**.
This is the same observation without needing two defects:

> **WHEN THE ANSWER IS RIGHT FOR AN ACCIDENTAL REASON, ONLY AN ASSERTION ABOUT THE MECHANISM CAN TELL
> YOU.**

**The instance.** A snapshot reading through a compacted-away table returns the correct value whether
or not the file still exists — `table.h` holds the image resident, so the bytes are there either way.
A test that read through the snapshot would have passed against a build that deleted the file
immediately, **and it did**: three of B3.6's four tests passed while the reference count was wrong.

**So the pair is:**

| instrument | asserts | catches |
|---|---|---|
| `FileLifetime.AnInputFileOutlivesTheCompactionWhileASnapshotHoldsIt` | **the file is on disk** | the mechanism failing while residency masks it |
| `...ASnapshotSurvivesTwoCompactionsAndReadsThroughThem` | **the read is right** | the mechanism working and the read still wrong |

**Neither is sufficient and neither is redundant.** Drop the first and the reference count can be
deleted entirely with every test green. Drop the second and the file can be kept alive while the
version it holds is wrong.

**It is the same shape as `covers-correctness:` versus `covered-by:` in `FLOORS.txt`** (`GF-12`), and
as the two instruments B3-D7a requires of every loop: **the danger is never that one instrument
fails, it is that one instrument passing feels like coverage it does not provide.**

---

### GF-22 — two defects whose symptoms cancel are invisible to every test that asserts an answer

**Raised by** `BUG-B004` and `BUG-B005`, B3.5e. Filed here rather than under either bug, because the
class is the **pair**, not either member.

**BUG-B004** dropped the point versions a range tombstone hid. **BUG-B005** failed to write the
tombstone into the output files. Each alone loses data. **Together, every read returns the right
answer** — the key is absent, which is what the caller asked for, arrived at by two errors that
annihilate.

**THE ASYMMETRIC EVIDENCE IS THE PROOF, AND IT IS WORTH STATING AS A MEASUREMENT:**

| | effect on the suite |
|---|---|
| fix `BUG-B004` alone | **four passing tests turn red** |
| fix `BUG-B005` alone | **nothing observable changes** |
| both present | **everything green** |
| both fixed | everything green |

> **A TEST SUITE CANNOT SEE THIS CLASS AT ALL.** Not a weak suite — *any* suite, however thorough,
> whose assertions are about **answers**. The answers are correct. There is no input on which the
> engine returns the wrong thing.

**What can see it is a question about a MECHANISM rather than an ANSWER.** A test asks *is the answer
right?* A mutant asks:

> **IS THIS LINE LOAD-BEARING?**

`BM97` blinded the L1 tombstone lookup, and **nothing failed** — because nothing reached it. That
survival was **true information about the engine**, not a gap in the catalogue. The distinction
matters: a survival is usually read as *the suite is too weak here*, and this one meant *this code is
unreachable, and the reason it is unreachable is a second bug.*

**How to find the pair once one member is suspected.** The tell is the asymmetry above: **fix one,
and if a previously-green test goes red rather than a red one going green, the other member is
there.** A single defect's fix does not turn passing tests red.

**And it is an argument for mutants having a place beside tests rather than being a coverage metric.**

**`BM104` ADDS A RULE ABOUT THE PATCHES THEMSELVES, learned the same day.** Blinding clause 1's
tombstone test by *deleting* the covering call left its helper unused, `-Werror` failed the build,
and the **control lane** was killed:

> **A MUTANT MUST REMOVE EXACTLY ONE BELIEF. A MUTATION THAT CHANGES THE BUILD IS NOT A MUTATION** —
> a patch that fails to compile blinds nothing, and the lane correctly refuses to attribute anything
> to it.

The fix is to keep the call and discard its answer, so the only thing removed is the *acting on* it.
The lane already had the machinery to catch this — the direction control is exactly the assertion
that the patch alone does not break the build — which is why it reported `BROKEN` rather than a
survival.
Coverage would have reported both lines executed. They were — with their effects cancelling.

---

### GF-21 — replacing a mechanism under a threshold: assert the replacement at the SAME threshold

**Raised by** `AnOverCapExpansionIsRefusedAndAppliesNothing` → `AClearEverythingIsOneSmallRecordWhateverTheDatabaseHolds`, B3.5c.

The old test filled 3000 keys, issued a clear-everything, and asserted the resulting **expansion** was
refused for exceeding the record cap — **at a deliberately lowered cap** (`max_record_bytes = 20000`),
because a tripwire nobody has watched fire is decoration.

B3.5 retired the expansion, so the test's subject no longer exists. **The question is what replaces
it**, and the weak answers are available and tempting: delete it; or assert the clear-everything now
succeeds, at the default cap, where it would succeed for reasons having nothing to do with the change.

> **KEEP THE THRESHOLD. CHANGE THE ASSERTION.** The replacement runs the **same workload** at the
> **same lowered cap under which the old mechanism was refused**, and asserts the new one fits. Then
> passing is a statement **about the change** and not about a cap that got roomier.

**Why it matters that the number is the old one.** At the default cap the new test would pass on a
build where the expansion was still happening — 3000 point deletes fit under 4 MiB. The lowered cap
is what makes the two mechanisms **distinguishable by the same measurement**, which is §22.6c's
discriminator rule applied to a replacement rather than to a parse.

> **THE TEMPLATE, WHENEVER A MECHANISM IS REPLACED UNDER A THRESHOLD: run the old workload at the old
> threshold, and assert the new outcome.** A replacement measured against a different bar is a
> replacement nobody has compared.

**And the assertion is a bound, not an equality.** `EXPECT_LT(grew, 200)` — the record's exact size is
a fact about the encoding that will change; that it is *nothing like 3000 point deletes* is the claim.
A floor with margin, for `FLOORS.txt`'s stated reason: an exact assertion fails on any benign change
and a lane that cries wolf is a lane people delete.

---

### GF-20 — correctness resting on an argument whose premise moves is a scheduled defect

**Raised by** §8.1's expansion, retired at B3.5c.

B2 recorded a `DeleteRange`'s **expansion** in the WAL rather than the range itself, and the reason
was exact:

> *"If the WAL recorded the raw DeleteRange, recovery would have to expand it again — **against a
> state recovery is still in the middle of rebuilding**. The expansion is a function of the state at
> the time it runs, so replay-time expansion is correct only if that state provably equals the state
> at original Apply time. It probably does today, for a reason that depends on the WAL's start point
> coinciding exactly with the flush boundary — **a property B2 is about to start changing.**"*

**THAT REASONING WAS CORRECT AND ITS CONCLUSION EXPIRED.** The expansion was never wrong. It was
correct **under a premise**, and B3.5 dissolved the premise: a range tombstone means the same thing
wherever it is replayed — it hides every version below its own sequence, and nothing about the
surrounding state enters into it. Recovery **inserts** it and computes nothing.

> **CORRECTNESS RESTING ON AN ARGUMENT WHOSE PREMISE MOVES IS A SCHEDULED DEFECT. THE FIX IS TO
> REMOVE THE PREMISE, NOT TO DEFEND IT BETTER.**

The tell is already in B2's own words — *"a property B2 is about to start changing"*. A comment that
names the thing that will invalidate it has done the hard part; what it has not done is fix it.

**Defending it better is the tempting alternative and it is a treadmill.** The available moves were:
prove the flush boundary coincidence holds, or assert it, or narrow recovery so the coincidence is
forced. Each buys correctness *for now*, adds a constraint to every later change, and leaves the
argument standing. **Removing the premise ends it.**

**It is `GF-18`'s question answered in the affirmative.** *What did this shim let us avoid deciding?*
The expansion let B2 avoid deciding what a range deletion means as a durable, replayable fact — and
answering that question is what made the premise unnecessary. `GF-18` says a retired shim is the
moment to re-check the contracts it stood between; this says **the moving premise is the marker for
which shim to retire first.**

**Two instances now, both at B3.5.** The other is `DBImpl`'s own note on `DeleteRange`'s expansion —
*"THAT IS CORRECTNESS BY ARGUMENT, AND THE ARGUMENT HAS A MOVING PREMISE"* — and the snapshot
registry (`B3.4`), which replaced *"a snapshot pins its stores, and residency makes that safe"* with a
registry, on exactly this reasoning before the form had a name.

---

### GF-19 — a name that describes one end of a structure will be read as describing the structure

**Raised by** `BM90-unbounded-covers-everything`, B3.5b.

**"Unbounded" is one word and a range has two ends.** `RangeTombstone::end_unbounded` says the *end*
has no bound; the misreading takes it to mean the *range* has none —

```cpp
if (end_unbounded) return true;   // BM90
```

— and that deletes **everything below the start**, which is data no `DeleteRange` ever named. Not a
typo. A one-line simplification that reads correctly aloud.

> **A NAME THAT DESCRIBES ONE END OF A STRUCTURE WILL BE READ AS DESCRIBING THE STRUCTURE.**

**It is `GF-7`'s family, in an identifier rather than in a comment.** `GF-7` is a *comment* asserting
a load-bearing property for a line that does not carry it; this is **a word attached to the wrong
scope** — true of the field it names, false of the object the reader applies it to. Both are invisible
to review for the same reason: **the words are true somewhere.**

**THE TELL, AND IT GENERALISES PAST NAMES.** The wrong reading passes every test that checks a key
*inside* the range. Both readings agree there, and agreement inside the bounds is exactly where a
careless test looks.

> **HALF A RANGE TEST IS NOT A RANGE TEST. ANY TEST OF A BOUNDED STRUCTURE ASSERTS OUTSIDE BOTH
> BOUNDS, NOT INSIDE.**

Which is why `AnUnboundedEndCoversEverythingAboveItsStartAndNothingBelow` asserts `"a"` and `"l"`
**below** the start, `"m"` **at** it — the inclusive edge — and `"zzzzzz"` far above. The inside is
the one place that proves nothing.

**The rule is not new to this repo and that is the argument for naming it.** B2's half-open bound was
asserted at both ends for the same reason (*"a fixture checking only the inside passes with either
convention"*), and `RangeModel.TheEndBoundIsExclusiveAndTheStartBoundIsNot` exists because of it.
`GF-19` is that habit stated once instead of rediscovered per boundary.

---

### GF-18 — a shim that makes a case unnecessary makes the gap it hides invisible

**Raised by** `B3-Q4`: the frozen `Engine` interface required a range deletion the frozen
range-tombstone format could not express, and **nothing noticed for a phase and a half.**

`[A3]` put `DeleteRange` in the interface for the clear-everything case. B2 implemented it as **one
point delete per live key**, which resolved `Bound::Unbounded()` against the *live set* before
anything was written — so **no format ever had to represent `[start, ∞)`**. When `[A3]` required the
expansion retired at B3, the gap it had been standing over became load-bearing in a single step.

> **AN EXPANSION OR A SHIM THAT MAKES A CASE UNNECESSARY ALSO MAKES THE GAP IT HIDES INVISIBLE. SO
> RETIRING A SHIM IS THE MOMENT TO RE-CHECK EVERY CONTRACT THE SHIM WAS STANDING BETWEEN.**

**Why the verification did not catch it, and this is the transferable part.** §6.1 specified the
format from the **block's** point of view and induced every refusal against hand-built bytes. That
exercise is thorough and it **never touches `Bound`, because the classifier never sees one.** Two
frozen artifacts, each internally consistent, each induced against its own rules — and **never
checked against each other.**

**It is `GF-15` between two frozen artifacts rather than inside one**, with one difference worth
stating: `GF-15`'s instance had a rule *granting permission* another contract did not grant. Here
**neither contract was wrong.** They were **unjoined** — and an unjoined pair produces no
contradiction to find until something asks both at once.

**What makes it findable.** The shim is the signal. A shim exists because some case was awkward; the
awkwardness is where two contracts meet; and the shim is what keeps them from having to agree. So the
question at retirement time is: *what did this let us avoid deciding?*

**Recorded as a second sweep condition on `CF-4`** — not only the frozen interface as a whole, but
**every place B2 or B3 removed an expansion.**

**And `CF-4` paid before it came due.** The sweep it schedules for B4 produced its first instance at
B3.5, where it cost a **design decision** instead of a differential failure against `engine/model`
with a corpus of tables already written to the wrong format. That is an argument for doing the sweep
rather than for deferring it.

---

### GF-17 — a reserved field sized by guess postpones the version bump by one; it does not avoid it

**Raised by** the SSTable footer's `reserved:[8]u8`, spent at B3.5b.

B2 left eight bytes in the footer with an explicit rationale: *"eight bytes now are free, a format
version bump at B3 is not."* B3 needed to name a range-tombstone block. **A `BlockHandle` is twelve
bytes** — `offset:u64` plus `size:u32` — so the natural shape did not fit the reserve at all.

**It fit only because the size turned out to be derivable.** The range block is written **last**,
immediately before the footer, so `range_size = file_size - kFooterBytes - range_offset`; only the
offset is stored, and eight bytes hold it. Had the block needed to sit anywhere else — or had the
extension been a second handle, a checksum, or anything with an independent length — **the reserve
would have bought nothing and B2's deferred version bump would have come due anyway.**

> **A RESERVED FIELD SIZED BY GUESS IS A BET THAT THE NEXT EXTENSION FITS. THIS ONE PAID OFF ONCE, ON
> A TECHNICALITY, AND THE NEXT EXTENSION PAYS FULL PRICE.**

**And there is a second cost B2 did not price.** The reserve had *two* properties, asserted together
in one test, and **only one of them can survive the reserve being spent**:

| property | what it was for | after B3.5b |
|---|---|---|
| **written zero** | an old file is recognisable | **still true**, and load-bearing for a new reason: a B2-era table decodes as `range_offset == 0`, meaning "no range block" |
| **not read** | a file from a *future* build still validates here | **gone, necessarily** — the reader now reads those bytes, so a file that put something else there is REFUSED |

> **SPENDING A RESERVE IS EXACTLY THE ACT THAT ENDS THE FORWARD COMPATIBILITY IT WAS ALSO
> PROVIDING.** A reserve is only forward-compatible while it is *readable and ignored*.

**The honest conclusion, stated so nobody repeats the reasoning at B4:** the reserve **did not avoid
the version bump — it postponed it by one**, and it spent the format's forward compatibility to do
so. Reserving sixteen bytes at B4 "because this worked" would be repeating a bet that happened to
land, not applying a lesson.

**What to do instead, when it next comes up.** Decide whether the format needs *extensibility* or
*version negotiation*. A reserve gives neither reliably; a length-prefixed footer with a declared
field count gives both, at the cost of the fixed-width property the footer was built around —
*"the one thing a classifier can read without trusting anything else in the file."* That is a real
trade and it should be made deliberately rather than inherited from a byte count somebody guessed.

**`BM33` survives the change and keeps its job**, re-aimed: it now blinds the range offset rather
than the reserve's zeroing, and the test it dies to was rewritten to state the new pair of
properties instead of being loosened to accommodate them.

---

### GF-16 — a mutant that survives because its precondition is unreachable is a claim about a workload

**Raised by three survivals in one step**, B3.4, and it is a sharper statement of the survival tally's
meaning #1 rather than a new meaning.

| mutant | the situation it breaks | why the suite never created it |
|---|---|---|
| `BM76` tombstone dropped over a snapshot | a tombstone the snapshot floor must keep | the fixture put the tombstone at the **top sequence**, where the watermark pin keeps it for an unrelated reason — so the test watched the pin and called it the drop rule |
| `BM79` roller rolls inside a user key | a key whose versions span a file roll | with **no live snapshot** the drop rule leaves one version per key, so no key is ever large enough to span one |
| `BM82` `Sync` no longer claims the guard | the guarded path being entered twice | the tests constructed the guard **directly**, so the path was never entered at all |

> **A MUTANT THAT SURVIVES BECAUSE ITS PRECONDITION IS UNREACHABLE IS NOT A WEAK MUTANT. IT IS A
> CLAIM ABOUT A WORKLOAD THE SUITE NEVER RAN — SO THE DISPOSITION IS TO REACH THE WORKLOAD, NOT TO
> RELABEL THE MUTANT.**

**All three were reached, and none was relabelled.** `BM76` got a fixture where the tombstone is not
the highest sequence, judged by `AdjudicateDrops` rather than by a count that is unremarkable either
way. `BM79` got **forty held snapshots**, so that a key genuinely has many surviving versions — a
workload this engine had never run, and the one `B3.6` is about. `BM82` got a **re-entrant `Sync`**
through the promotion hook.

**Why relabelling is the tempting wrong answer.** Every one of the three had a defensible-sounding
label available — *"covered by the pin"*, *"unreachable under the default policy"*, *"covered by the
guard's own tests"* — and each would have been **true and useless**: it names a reason the class is
not detected instead of an assertion that detects it. `GF-7`'s rule in the label file says a
`covered-by:` is determined by induction or not written, and a label invented to explain a survival
is exactly the inferred kind.

**What it cost, and why that is the argument.** Reaching the third workload found nothing wrong with
the engine — but reaching the first two required a fixture and a snapshot workload the suite did not
have, and **`B3.6`'s whole subject is the workload `BM79` forced into existence.** A relabelled
`BM79` would have deferred that discovery to the step that assumed it already worked.

---

**`BM97` IS THE STRONGEST DEMONSTRATION THIS CATALOGUE HAS, AND IT IS THE ONE TO CITE.** Its history
is three separate chances to close the file with a defensible sentence:

1. **B3.5d — held out.** Compaction did not yet emit tombstones into L1, so its workload did not
   exist. It was kept **out of `mutants/`** with its absence recorded in the commit, rather than
   admitted with a label explaining why it could not fire.
2. **B3.5e — re-added, and it survived again.** The available label was
   *"covered by the compaction tests"* — **plausible, defensible, and false.**
3. **The second survival is what opened `BUG-B004` and `BUG-B005`** — two data-loss defects whose
   symptoms cancelled (`GF-22`), invisible to every test in the suite.

> **A PLAUSIBLE LABEL IS THE DANGEROUS ONE.** An implausible label gets questioned. This one would
> have been accepted by any reviewer, closed the obligation, and shipped both defects.

**Cite this whenever an opt-out is proposed on the strength of an ARGUMENT rather than a
MEASUREMENT** — an exemption, a `covered-by:` that was reasoned to instead of induced, a mutant
excused because its class "is obviously covered elsewhere". The argument here was correct in every
particular except the conclusion.

---

### GF-15 — a rule derived from one contract is not permission under the others

**Raised by the watermark pin, B3.4.** `B3-D1` says a compaction **may** drop an entry that no reader
can observe. That "may" is permission **with respect to reads**, and reading it as permission full
stop breaks a promise `B3-D1` never mentions:

| contract | what it is about | what compaction owes it |
|---|---|---|
| the drop claim | **the ANSWER a reader gets** at each observable sequence | preserve every answer |
| `DurableSeq` | **a PROMISE ABOUT A SEQUENCE**, monotone non-decreasing | preserve the engine's proof of it |

`Open` re-derives the durable floor as the maximum `largest_seq` over the live tables, and it must:
D7's forward binding forbids the manifest from recording a durable sequence, so **the tables' own
bytes are the only place that number can come from**. Drop the highest-sequenced entry — a tombstone
with nothing left to mask, exactly what the claim permits — and the maximum falls. Every answer is
preserved; `DurableSeq` goes backwards across a restart.

> **A COMPONENT OBEYS MORE THAN ONE CONTRACT. A RULE DERIVED FROM ONE OF THEM IS NOT PERMISSION UNDER
> THE OTHERS, AND THE RULE WILL NOT SAY SO — because the contract it came from does not know the
> others exist.**

**What makes it findable rather than lucky:** the question is *what else does this operation touch
that someone has already been promised something about?* Compaction touches the set of live tables,
and the durable floor is derived from that set. The link is one step and nobody walks it while
reading a claim that is locally airtight.

**`BM77` plants it, and the mutant IS the faithful implementation of `B3-D1`.** Not a typo, not a
weakened check — what a careful reader of the claim would write. That is precisely the blind spot the
suite exists for, and it is why the mutant's header says so.

**RULED THE PHASE'S MOST TRANSFERABLE FINDING, AND IT CARRIES AN OBLIGATION.** A cross-contract
interaction is invisible in **either contract's own statement** — that is what makes it general, and
what makes it undiscoverable by reading one document carefully. It generalises to **every place this
engine derives a fact from one rule while another rule depends on that fact**, and this engine does
that in more than one place: the manifest's numbers are re-derived from table bytes, the durable
floor from `largest_seq`, the recovery skip point from the same maximum, `bottom_most` from range
disjointness.

> **THE QUESTION IS ASKED ONCE ACROSS THE FROZEN INTERFACE AS A WHOLE, AT B4** — not per-decision,
> where it has already been asked and answered locally. `CARRY-FORWARD.md` CF-4 carries it.

**AND IT HAS A SECOND INSTANCE ALREADY, BETWEEN TWO FROZEN ARTIFACTS RATHER THAN INSIDE ONE.**
`B3-Q4`: the frozen `Engine` interface requires `DeleteRange(Unbounded, Unbounded)`; the
range-tombstone format frozen at B3.2 cannot express an unbounded end. **The difference from the
first instance is the useful one** — there, a rule granted permission another contract did not grant.
Here **neither contract was wrong; they were unjoined**, and an unjoined pair produces no
contradiction to find until something asks both at once. See `GF-18` for what made it findable.

---

### GF-14 — a complementarity claim is asserted in both directions or it is folklore

**Raised by** `IsTheMergeOfItsInputs` and `AdjudicateDrops`, B3.4.

Two instruments are said to be complementary: one sees order and values, the other sees drops in any
durable image. **Stating that is not asserting it.** The test that carries the claim asserts *both*
halves against **one state**:

```
RefusesAnOutputWhoseValuesAreShiftedByOne
    the merge adjudicator  REFUSES  the shifted output
    the drop adjudicator   PASSES   the same state
```

**The first half alone is not the claim.** One instrument refusing says nothing about whether the
other was needed; only the second half — *this state gets past the other one* — makes "we need both"
a statement with a failing case.

> **A COMPLEMENTARITY CLAIM IS ASSERTED IN BOTH DIRECTIONS OR IT IS FOLKLORE.**

**What it protects against is silent degradation.** The day someone widens the drop adjudicator to
look at values — a reasonable change, and an improvement in isolation — the pair stops being
complementary and *nothing notices*, because a one-directional test still passes. With the second
half, that change fails a test whose message says exactly what to reconsider. **The claim gets
revisited rather than repeated.**

**This is Track A's bidirectional-gap discipline applied to two CHECKERS rather than to a gap.** The
same move: do not assert only that the thing fires, assert also that it was needed — and here
"needed" means *the other instrument does not cover this*.

---

### Fixture defects: one shape, twice, and the class made unreachable

Two fixtures produced verdicts that looked like checker bugs. **Both times the checker was right.**

| where | what the fixture omitted | what the checker correctly reported |
|---|---|---|
| B3.0, the drop adjudicator | a **directory sync** after writing the table — so its NAME was never durable | the image did not contain the table, so every version was **dropped** |
| B3.4, the merge adjudicator | **the manifest** naming the table — so it was an orphan | a table nothing refers to holds nothing, so every version was **dropped** |

> **A FIXTURE THAT DOES NOT DESCRIBE WHAT ITS AUTHOR MEANT PRODUCES A CORRECT VERDICT ABOUT THE WRONG
> THING, AND IT PRESENTS AS A CHECKER BUG** — which is the expensive way to find out, because the
> debugging starts in the wrong component.

**What the two omissions share is the useful part.** Neither was a typo. Each left out something
**the engine's own invariants require**: a durable table has a durable name; a live table is named by
the manifest. A fixture assembling an image by hand must reproduce every one of those invariants from
memory, and *will not*.

**So the class is made unreachable rather than remembered.** `rig/image_fixture.h` builds images
**through the engine's own construction path** — write, sync, **sync the directory**, open, validate,
name in the manifest — and both tests now go through it. There is one place that knows the whole
sequence, and a fixture cannot forget half of it. It was cheap: one file, and it removed a duplicated
key encoder on the way.

---

### GF-13 — a bound derived from another instrument's measurement cannot be raised

**Raised by** B3.4's merge, and it is a stronger property than the condition that asked for it.

Every loop needs a progress quantity and every unbounded quantity needs a bound. **The usual bound is
a CHOSEN number** — a timeout, a retry count, a maximum iteration — and a chosen number has one
predictable life: it is hit under some workload nobody anticipated, and it is **raised**. Not because
anyone is careless, but because the alternative is refusing a correct run, and a limit that refuses
correct runs loses that argument every time.

**The merge's bound is not chosen. It is DERIVED:**

> **`inputs_consumed ≤ Σ entries(f)` over the compaction's input files, counted by `ValidateTable`
> before the merge starts.**

A correct compaction consumes each input entry **exactly once**, so it terminates *at* the bound.
**Hitting it exactly is correct; exceeding it is the only failure** — and exceeding it can only mean
a source was rewound or an entry counted twice, which are the two ways a merge loops forever.

> **A BOUND DERIVED FROM ANOTHER INSTRUMENT'S MEASUREMENT CANNOT BE RAISED WITHOUT CONTRADICTING THAT
> INSTRUMENT, SO THE PRESSURE THAT NORMALLY ERODES A LIMIT HAS NOWHERE TO GO.**

To raise this one you must claim a table holds more entries than the classifier says it holds — and
the classifier's count is itself asserted (`SstClassifier.AcceptsACanonicalTable`). There is no
number in the source to edit. **That is a difference in KIND, not in degree**: a chosen bound is a
judgement that can be revised, and a derived bound is a consequence that can only be revised by
falsifying something else.

**Where to look for the pattern.** Prefer a bound that is *already being measured for another
reason*. `kMaxRecordBytes` and `kWalBufferBytes` are chosen and carry their derivations in prose
precisely because nothing measures them; this one needed no prose, because `ValidateTable` was
already counting.

---

### GF-12 — a termination assertion is not a correctness assertion

**Raised by** B3.3's CF-3 mutants, and it is the danger *inside* a rule that is working.

CF-3 requires every loop to assert the movement it terminates on, over a quantity it does not derive
from the thing it might be wrong about. `ConcatIter` does that, and the assertions hold. **Two of its
three mutants pass every one of them:**

| mutant | what it breaks | what the progress assertion does |
|---|---|---|
| `BM68-concat-seek-wrong-half` | the binary search takes the **wrong half** | **holds throughout.** `hi - lo` shrinks whichever direction is taken, so the loop terminates cleanly with its interval invariant intact — **and lands on the wrong file** |
| `BM69-concat-next-skips-a-file` | `Next` advances **two** files | **holds throughout.** `file_` strictly increases, so the walk terminates — **having silently dropped a whole table's contents** |

> **A LOOP WITH A PROVEN PROGRESS QUANTITY CAN ADVANCE MONOTONICALLY INTO A WRONG ANSWER.**

**THE DANGER IS NOT THAT CF-3 FAILS. IT IS THAT CF-3 SUCCEEDING FEELS LIKE COVERAGE IT DOES NOT
PROVIDE.** A reader who sees `RIFT_CHECK(hi - lo < before)` beside a search, and knows the phase made
a point of loop assertions, has every reason to think the loop is checked. It is — for termination,
and for nothing else. In `BM67`'s terms: **a checked-looking loop that returns wrong answers.**

**What actually covers the traversal, named so nobody reads the wrong green as evidence:**

| instrument | what it covers |
|---|---|
| `ConcatIter.EverySeekTargetLandsWhereALinearScanWould` | **the seek sweep** — every probe in and around the run compared against what a linear scan returns. A search wrong for one input class is wrong invisibly, because every other input still works |
| `ConcatIter.WalksTheWholeRunInOrder` and `.WalksBackwardsToTheSameSequence` | **the traversal** — and the pair matters, because forward and backward cross the same file boundaries in opposite orders |

`FLOORS.txt` labels these **`covers: correctness`** rather than leaving them as bare `covered-by:`
entries, so the distinction survives being read by someone in a hurry.

**THE SAME SHAPE ONE LEVEL UP: THE DROP ADJUDICATOR.** It is correct about what it checks and
**silent about what a reader assumes it covers.** It works over **sets** of `(user_key, seq)` — so it
is blind to ordering entirely, and blind to values.

> **A merge that emitted every required entry, in REVERSE ORDER, with EVERY VALUE SHIFTED BY ONE
> POSITION, would satisfy all three of its directions.**

That example is stated concretely on purpose: it is specific enough that nobody can talk themselves
out of it, which an abstract "it does not check ordering" would not be. `CompactionOutput.IsTheMergeOfItsInputs`
is what closes it — the harness merges the inputs itself, filters by the drop claim, and asserts the
output is **exactly that sequence, in order, with matching values.**

**The two are COMPLEMENTARY, NOT REDUNDANT, and the distinction is written down because someone will
later notice they overlap and propose deleting one:**

| instrument | runs where | sees |
|---|---|---|
| `IsTheMergeOfItsInputs` | **only** where the harness knows both the input and the output files — a compaction in isolation | order, values, and drops |
| `AdjudicateDrops` | **any durable image**, including one produced by a crash MID-COMPACTION | drops only |

Delete the second and every crash schedule loses its drop verdict. Delete the first and a merge can
reverse its output undetected.

---

**THE STANDING REQUIREMENT, and it is not a B3 rule:**

> **EVERY LOOP THIS ENGINE ADDS GETS TWO INSTRUMENTS, ANSWERING DIFFERENT QUESTIONS: *does it stop*,
> and *does it stop in the right place*.**

CF-3 is the first. **It was never the second**, and the phase that treats it as both ships `BM68`.
`FLOORS.txt` keeps them apart mechanically — `covered-by:` against `covers-correctness:` — so the
distinction survives a hurried reading rather than depending on one.

---

### BM67 — the phase's exemplar of a defect NO GENERAL CHECKER FINDS

**One character.** `<` becomes `<=` in `RangeTombstone::Covers`, and the range stops being half-open.

Closed and half-open **agree on every key except one**: the range's end. A tombstone for `[b, d)`
that has become `[b, d]` deletes `d` — one key more than the caller asked for, **forever, and
silently**, because nothing anywhere reports a key that was deleted slightly too eagerly.

**GO THROUGH THE ORACLES THIS PROJECT HAS BUILT AND NONE OF THEM SEES IT:**

| checker | why it is blind to this |
|---|---|
| the **drop adjudicator** | asks *what may be dropped* against the harness's version model. It is about DROPS, not about **bound arithmetic** — the tombstone is legitimately present and its sequence is legitimately required |
| **recovery equivalence** | compares WAL-only recovery against WAL-plus-tables. **Both sides use the same `Covers`**, so both delete `d` and the two agree perfectly |
| the **kill-point sweep** | injects crashes and torn syncs. **Nothing crashes.** Every kill point recovers to a promised watermark, because the watermark is right and only the CONTENT is one key short |
| **`ValidateTable`** | judges bytes against a format. The bytes are perfectly legal — this is a defect in what they MEAN, not in what they are |
| **the differential rig at B4** | *would* catch it, against `engine/model` — which is a phase away, and by then the convention would have been established by the code rather than chosen |

**What catches it is `RangeTombstone.TheBoundsAreHalfOpen`** — a fixture that probes `a`, `b`, `c`,
`d`, `e` around a `[b, d)` tombstone and asserts each answer individually, written **before any code
could establish a convention by accident**.

> **THIS IS B3.2's ORDERING ARGUMENT STATED AS A CONSEQUENCE RATHER THAN A PRINCIPLE.** A classifier
> written after a writer inherits the writer's convention and asserts it back. There is no general
> checker for "the engine is consistently wrong about a boundary" — consistency is exactly what a
> differential or equivalence check confirms. **The only defence is to fix the convention in a
> fixture before there is an implementation to read it off.**

---

### An empty condition is evidence only when the question was asked mechanically

CF-3 carries a condition: *if a loop cannot state a progress quantity independent of what it might be
wrong about, that is a finding to report before it is a loop to write.* At B3.3 it **came back
empty** — every loop the step adds has an independent quantity.

**An empty result is worth exactly as much as the procedure that produced it.** What makes this one
evidence rather than a shrug is that the question was asked **before the code existed**, in a table
with one row per loop and an explicit independence column:

```
loop            might be wrong about        progress quantity     independent?
Next / Prev     which table holds the key   file_ (an index)      YES
Seek            CompareInternalKey          hi - lo               YES  <- see below
```

Had it been asked by reading the finished code and nodding, "nothing to report" would have meant
"nothing noticed". **The table is what makes it the first rather than the second**, and it also
produced the phase's forward flag: B3.4's merge both advances a cursor *and* drops entries, so it has
no single cursor as a progress quantity, and its honest answer may be a bounded work count.

**The general shape: a check that returns "nothing" is only as good as the enumeration behind it.**
It is the same argument `HARNESS-006`'s audit made — enumerate the population first, then check each
member — applied to a question rather than to a set of functions.

---

### GF-11 — a rule stated in one place invites transfer to an adjacent place where it is false

**Raised by** the range-tombstone bounds, B3.2.

`table_check.h` refuses **a key too short to carry a tag**, and it is right: point entries are
internal keys. A range tombstone's bounds are **user keys** — the tag is a separate field — so the
**empty user key is a valid bound**, meaning "from the beginning".

Both rules are correct. **The danger is not either rule; it is the transfer.** A reader who has
internalised the first arrives at the second and applies it, because the two structures sit in the
same format, are decoded by the same block decoder, and are described in adjacent sections.

> **A FORMAT DECISION THAT CONTRADICTS A NEARBY ONE IS DOCUMENTED AT THE CONTRADICTION, NOT ONLY AT
> ITS OWN SITE.**

So `range_tombstone.h` does not merely say *bounds are user keys*. It says:

> *"THE BOUNDS ARE USER KEYS AND NOT INTERNAL KEYS, so the empty user key is a valid bound and there
> is no minimum length. This is the OPPOSITE of the point entry rule — which refuses a key too short
> to carry a tag — and it is stated here because a reader who has internalised that rule will assume
> it carries over. The tag is a separate field precisely so the bounds do not have to."*

**The last clause is what stops the transfer**, and it is the part a shorter comment would drop: it
does not just deny the other rule, it says *why the design is different*, which is the only form a
reader can check. `RangeTombstone.TheEmptyUserKeyIsAValidBound` asserts it, so the sentence has a
failing case.

**The relationship to GF-7.** GF-7 is a claim attached to the wrong line. This is a claim attached to
the right line **that a reader will carry to the wrong one** — the same damage by a different route,
and the remedy is different too: GF-7's is to move the comment, this one's is to write the
contradiction down where it can be met.

---

### GF-10 — a set of assertions all pointed the same way has a blind spot the size of its agreement

**Raised by** B3.1's aliasing condition, and it is a **decision** that came out of it rather than an
audit finding — see `B3-D2c`.

The drop adjudicator had two directions, and they looked complementary:

```
kept      >= required     nothing a reader can reach was dropped
dropped   <= permitted    no tombstone was dropped over what it masked
```

**Both ask whether something is MISSING.** So neither could see a reader that reports a record the
bytes do not contain — which makes a real drop look survived, and produces a **false pass**. The
agreement between them was not corroboration; **it was the shape of the hole.**

> **A SET OF ASSERTIONS ALL POINTED THE SAME WAY HAS A BLIND SPOT THE SAME SIZE AS ITS AGREEMENT.**
>
> **Asking what a checker CANNOT SEE is a different question from asking whether it works** — and the
> second question is the one that gets asked, because it is the one a green lane answers.

The third direction, `survived ⊆ submitted`, is what closed it, and it is stated as its own decision
rather than as defensive clutter precisely because a future reader will otherwise remove it: it never
fires in normal operation, and *nothing else in the tree asks its question*.

**How to ask the harder question.** Enumerate what each assertion *rules out* and look for the
direction none of them names. Here: "missing" was ruled out twice and "present but never written" was
ruled out zero times. It is the same move `HARNESS-006`'s audit made across the evidentiary deciders
— enumerate the population, then check each member in both directions — applied to assertions rather
than to functions.

---

### GF-9 — a correctness claim written before its checker is a hypothesis, and building the checker is how it gets tested

**Twice observed, both in Track B, and both times the correction was in the CLAIM rather than in the
code.**

| phase | the claim | what building its checker found |
|---|---|---|
| B2.0 | *entries in a block are strictly ascending* | ascending **by `memcmp`** is wrong: internal keys sort user-key-ascending, tag-DESCENDING, and the fixtures that could show it did not exist because they used keys with no tag |
| B3.0 | *`keep(k)` is the newest version at each observable sequence* | it **over-requires**: a deletion's answer is `kNotFound`, which dropping the deletion preserves exactly, and the stricter form forbids the one drop that makes compaction terminate in space |

**What makes this different from the ordering rule it comes from.** *The observer lands before the
observed* is usually argued as protection against a checker written to agree with the thing it
checks. That is real, and it is not what happened either time. **Both times the CLAIM was wrong, and
writing the enforcement is what tested it** — because a claim in prose has no failing case, and a
checker has to be handed inputs.

**The consequence worth recording, and it is `HARNESS-006`'s shape.** A checker built to an
over-strict claim **refuses correct behaviour**, and it presents as *a bug in the component being
checked*. `HARNESS-006` cost its debugging to the wrong component because it was found late. Both of
these were found **before the component existed to be blamed** — B2's before any writer, B3's before
any compaction — which is the same ordering rule buying something other than what it is usually sold
for.

---

### GF-8 — when a rule distinguishes two kinds of dependency, find the SIGNATURE that separates them, not the sentence

**Raised by** B3-D2a, correcting the oracle-independence rule at `B3-Q1`.

The rule to be enforced was *an oracle may parse the engine's artifacts and may not consult its
beliefs*. That sentence is correct and it is **a discipline**, and this catalogue records five
disciplines that failed. What replaced it is two greps:

> **AN ARTIFACT HEADER DECLARES NOTHING TAKING AN `Env*` AND NOTHING TAKING A SNAPSHOT.**

`Env*` means *it went and looked*, which is an act with an opinion about what the current state is. A
snapshot parameter means *it decided what a caller should be allowed to see*, which is the engine's
visibility rule. A header with neither is bytes in, structure out.

**The general form.** When a rule separates two kinds of dependency — permitted and forbidden,
parsing and consulting, reading and asking — **the enforceable version is a property of the
DECLARATION, not a description of the intent.** Look for the signature that differs. If no signature
differs, the two kinds are not actually distinct and the rule is describing a preference.

**Corroboration, and it is the stronger half.** The rule was written to draw a boundary and it
**found unnecessary coupling instead**: `sst/table.h` fails both marks, and it turned out the drop
checker never needed it — `table_format.h` enumerates every entry in a table, and enumerating is the
whole job. `sst/manifest.h` failed the `Env*` mark and split, which was a correction rather than a
concession.

> **A RULE THAT FINDS UNNECESSARY COUPLING IS DOING MORE THAN ENFORCING A BOUNDARY.** The signature
> test asks "does this dependency have the shape of a judgement", and a dependency that cannot answer
> is usually one that did not need to exist.

**The counterpart obligation, and it is the one a future reader will get wrong.** See `HARNESS-014`:
permission to *read* an artifact is not permission to *derive the expectation* from it.

---

### GF-7 — a misplaced invariant: a comment asserting a load-bearing property for a line that does not carry it

**Raised by** `BM55-tables-oldest-first`, B2's last catalogue run. The phase's best finding, and it is
not the vacuous-green class and not one of the three survival meanings as previously stated.

> **A COMMENT THAT ASSERTS A LOAD-BEARING PROPERTY FOR A LINE WHERE IT IS NOT LOAD-BEARING IS WORSE
> THAN NO COMMENT.** It is where someone looks for the invariant, and it sends them somewhere nothing
> depends on it.

**The instance.** `Version::Build`'s table loop carried *"NEWEST FIRST. Order is not cosmetic: a
deletion in a newer table must hide a value in an older one, so the first source holding a user key
wins."* Every word of that is true — **of `VersionGet`'s point-read walk, a hundred lines below.**
Where it stood it was false: `MergedIter` orders by KEY, sequences are unique, so there are no ties
for source order to break and the order it is handed is irrelevant.

**How it was found, and it could not have been found by reading.** A mutant was aimed at that line
*because the comment said so*. It survived — correctly — and the survival is the only thing that
distinguished "this line carries the property" from "a comment near this line says it does". **A
misplaced invariant is invisible to review by construction: the reviewer's question is whether the
comment is true, and it is.**

**The family, and this is why it is a named shape rather than an incident.** It is the same failure as
Track A's `power-config a3` and the A6 banner — **a name describing something other than what it is
attached to** — with one difference that makes it more dangerous: those are *labels*, and this is a
**correctness claim**. A wrong label misdirects; a wrong correctness claim gets *relied on*. The next
person to touch `Version::Build` would have preserved an order they did not need and, finding the
same words there, would have had no reason to look for the walk that actually needs it.

**The fix, and where the induction has to land.** Re-point the comment at the line that carries the
property, and **re-point the mutant there too** — a general form whose remedy is not itself induced is
a general form nobody has tested. `BM55` now reverses `VersionGet`'s walk, where the first table
holding the user key wins and the walk stops.

**The standing question it adds.** When a mutant survives, before reaching for any of the three
meanings: *is the line this patch is aimed at actually the line that carries the property, or is a
comment answering that question for me?*

---

**THE SAME SHAPE IN THE LABEL FILE, and it is where it does the most damage.** `FLOORS.txt`'s
`covered-by:` is a claim that a named assertion catches a named class. CF-2 landed 47 of them, and
**every one was DETERMINED — the patch applied, the tree built, the failing assertion read — never
INFERRED from what the patch says it blinds.**

Inferring would have been faster and would have produced entries that are *plausible and wrong*: the
`blinds` line describes the defect, not the assertion that notices it, and the two coincide often
enough to make the guess feel safe. Three of the 47 have **no failing test at all**.

> **A WRONG `covered-by` IS WORSE THAN NONE, because it is consulted precisely when someone is about
> to delete something.** That is GF-7 arriving in the label file rather than in a comment: a claim
> attached to the wrong thing, in the one place a reader trusts before removing an assertion.

**The rule: a label that names an instrument is determined by induction, or it is not written.**

---

**BM82 IS THE SAME QUESTION, ASKED OF A GUARD RATHER THAN A COMMENT.** `SingleCaller` enforces
`Sync`'s single-caller precondition, and two tests constructed it **directly** — claim it twice, it
aborts; claim it sequentially, it does not. Both pass. `BM82` removes `Sync`'s *claim* on the guard,
leaves the guard itself intact, and **survived them both.**

> **A TEST CONSTRUCTING A MECHANISM DIRECTLY TESTS THE MECHANISM AND NOT ITS WIRING. THEY PROVE THE
> GUARD WORKS. NOTHING PROVES THE PATH USES IT.**

Two claims that read as one, and the enforcement rests entirely on the second.

**This is the planted-violation-versus-fixture distinction this project has held since A0**, arriving
in C++ **against a guard rather than against an analyzer**. There, the rule was that a determinism
check must be proven by planting a violation *in code the check actually scans*, never by feeding the
checker a hand-built fixture that exercises its parser. Here the "fixture" is a directly constructed
`SingleCaller`: it exercises the mechanism's own logic and says nothing about whether the production
path is wired to it. **Same distinction, different decade of the stack.**

**The induction is deterministic and that was the second decision.** The guard is claimed twice on
**one thread**, by re-entering `Sync` from the promotion hook — which fires inside `Sync`, when the
durable image changes. Racing two real `Sync`s would have induced it *probably*.

> **THIS CATALOGUE DOES NOT COUNT A GATE INDUCED PROBABLY.**

The hook fires **once**, deliberately: without that, a build with the claim removed would recurse
until the stack gave out, and **a death test cannot tell a guard firing from a crash** — the mutant
would have passed for the wrong reason, which is `GF-1`'s shape hiding inside the remedy.

---

### GF-6 — a rate is a ratio, and a floor on one needs a floor on the count beside it

**Raised by** B2.7's re-measurement of every harness-power floor. **Second instance** of a rate
moving for a reason unrelated to detection power.

A floor on a detection RATE cannot tell a loss of power from **a denominator that grew into territory
where the class was never detectable**. B2 added a manifest, so the sweep visits 300 kill points where
it visited 175 — and every added point is one at which `BM2`, `BM4` and `BM5` cannot be detected.
**Every rate in `FLOORS.txt` fell. Not one detection count did.** A lane that broke the build on that
would be reporting arithmetic as a regression; a maintainer who then lowered the rate floor would have
lowered it for the wrong reason and lost the bound for the right one.

**The rule.** A rate floor needs one of two things beside it:

- **a floor on the COUNT** — immune to the denominator, blind to per-point dilution. The rate is the
  reverse, which is why this is a *third* bound and not a replacement; or
- **a REGIME LABEL** that stops incomparable denominators from being compared at all.

**Track A learned the regime half at A6. This is the count half.** Both are now columns in
`FLOORS.txt`.

**Where it bites hardest, and B2 has an example.** `BM4-missing-dir-sync` in the default regime now
measures **290 of 290, first at kill point 1** — its count rose from 80 to 290 because the manifest
NAMES the WAL, so a lost directory entry is refused at every kill point rather than only where the
loss mattered. That is a real strengthening **and it costs the class its usefulness as a measure of
sweep power**: a class detected everywhere measures only that the lane ran. Its rate and ceiling are
kept because a collapse would still cross them, and are recorded as no longer discriminating so that
nobody reads 1000 per mille as a result.


