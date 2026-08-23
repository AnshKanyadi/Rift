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
| **Reproduce (seed)** | found at seed **1364**, 1 of 3000 seeds reaching it; `seeds/BUG-009` carries seed **105**, re-recorded at BUG-022's fix. The read mark moved every raft trace and seed 13 regenerated cleanly while no longer reaching `M34` at all — the search §16.3 warns a regeneration is. Found again at 105 of a 0–800 sweep, where the mutant panics `state machine safety failing` |
| **Invariant that caught it** | state machine safety, asserted inside `raft/` — *a truncation may not reach an entry this node was told was committed* |
| **Mutant class** | none existed — added `M34-append-from-zero-over-a-snapshot` |
| **Fix commit** | *(this commit)* |
| **Minimized?** | no — `simctl minimize` is STRETCH.md (Amendment A6) |

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
| **Reproduce (seed)** | found at seed **9595** of the 10,000-seed exit sweep. `seeds/BUG-015` carries seed **16** and **does not currently reproduce**: A6's workload moved the trace, `M46` detects at 1 in 3,000, and the replacement seed comes from the mutant power measurement under A6's shape (DESIGN-A6 §16.2). The entry is blocked, not retired |
| **Invariant that caught it** | none — a refusal, from `ApplyConfEntry` declining an illegal transition |
| **Mutant class** | none existed — added `M46-split-inherits-the-appended-configuration` |
| **Fix commit** | ebea8c5 |
| **Minimized?** | no |

**Symptom, verbatim:**

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
| **Reproduce (seed)** | seed **90004**: txn 14 and txn 29 both start at `1600000005840000000.26` |
| **Invariant that caught it** | transaction atomicity — a rolled-back transaction has no committed key |
| **Mutant class** | none existed — added **two**, `M67-minting-drops-the-node-tag` and `M68-restart-timestamp-derived-not-minted`, in the same commit as the fix |
| **Fix commit** | option A, both halves (DESIGN-A6 §22) |
| **Reproduce (bundle)** | **none, and the reason is structural.** The corpus arrangement is *bundle carries the schedule, mutant carries the defect*, and this defect is a **pair**: a tree with only `M67` applied still refuses the collision `M68` allows, and vice versa. No single mutant reintroduces the bug, so no bundle can name one that reproduces it. A 300-seed search under `M67` found nothing, and `M67`'s covering test is a **unit test in `./hlc/`** rather than a sweep — which said the same thing earlier, in a form nobody read as this |

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
| **Reproduce (seed)** | seed **2521**: the audit at `1600000008790243029.0` sums to **-19** |
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
| **Reproduce (seed)** | seed **10303**: the audit at `1600000005203989560.0` sums to **+10** |
| **First violating step** | the read answer that arrived after the restart, carrying the abandoned snapshot's timestamp; the guard now counts it as `StaleIncarnation`, and seed 10303 produces exactly **one** |
| **Invariant that caught it** | bank conservation over client-observed history |
| **Mutant class** | none existed — added `M73-a-read-answer-lands-in-any-incarnation`, in the same commit as this entry |
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
