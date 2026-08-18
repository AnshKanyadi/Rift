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

**Counts:** 5 entries, all A1. *(The phase gate for A1 requires this file to be nonempty, because a
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
| **Reproduce (seed)** | `seeds/BUG-002` carries seed 66, the first seed in the range that produces the completion-outlives-incarnation race |
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
| **Reproduce (seed)** | `seeds/BUG-003` carries seed 0 |
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
| **Reproduce (seed)** | `seeds/BUG-004` carries seed 17 |
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
| **Reproduce (plan)** | `patch -p1 < sim/mutants/M25-restart-recovers-unsynced-writes.patch && go run ./cmd/simctl replay --bundle seeds/BUG-005` (any commit) |
| **Reproduce (seed)** | seed **92**, violating at instant **2592077256**, step 1086 |
| **Invariant that caught it** | persist-before-reply, which is DR-8's **first enumerated gate** |
| **Mutant class** | none existed — added `M25-restart-recovers-unsynced-writes` in this PR |
| **Fix commit** | `f624c0a` (first half), `0c55e30` (the residue) |
| **Minimized?** | no — same reason as BUG-002 |

**Symptom, verbatim from the oracle:**

```
persist-before-reply at instant 2592077256, step 1086: node 2 acked index 15 at instant 2592077256
with only 5 durable; the leader may commit an entry this node can still lose
```

**This is the project's thesis, demonstrated end to end, and that is why this entry exists in the
shape it does.** DESIGN-A0's DR-8 enumerated this exact failure, in prose, **before `raft/` had a
single line in it**. It is the first gate in the enumeration, and the enumeration is reproduced
verbatim in the `Ready.Messages` doc comment:

> **MsgAppResp (accept)** — gated on: the appended entries AND HardState.Term durable. Without it:
> follower acks index i, leader counts it toward a quorum and commits; follower crashes, loses i,
> comes back and is elected with a shorter log ⇒ committed entry lost. Violates Leader Completeness
> and "committed is forever".

An oracle was then built *from that enumeration*, and it found *precisely that failure* in the
implementation, in a fault-injected run, reproducible from a single seed. **The enumerated gate and
the found violation match.** A design document predicting a specific failure and a checker then
finding exactly that failure is the entire argument this repository is making, and it is worth saying
plainly rather than leaving to be inferred.

**Root cause, first half (`f624c0a`).** `persistedIndex` derived durability from the *shape* of the
unstable-entry slice: empty meant "everything is durable". But `Ready()` clears that slice on
**handover**, not on acknowledgement. Between `Ready` and `AckPersisted` the state machine therefore
believed everything it had just handed the driver was already on disk, and released an append
response acking entries the driver had not yet written.

The error is not the arithmetic, it is the shape. **A fact inferred from an incidental property is a
fact that silently becomes wrong the moment the property changes for an unrelated reason.** The slice
emptied for a reason that had nothing to do with durability. Identifying a proposal by its log index
was the same error one subsystem over (BUG-004). `logTail` now records `persisted` and `handed` in
fields with different names, neither derived from the shape of anything else.

**Root cause, residue (`0c55e30`).** Two violations survived that fix, and the residue was not in the
gate at all.

*A restart with no crash recovered writes no crash would have kept.* `store.restart` rebuilt the node
by reading the engine, and an engine read returns the **visible** state, which by construction
includes batches applied and not yet synced — that window is the whole point of the model (DR-15). A
restart delivered to a node that was not down therefore produced a process that recovered its own
unsynced writes and then answered for them. On seed 92 that is exactly the node acking index 15 with
5 durable. A restart is a death followed by a recovery, and the death half is not optional.

*A mark's coverage grew after handover.* `dirty()` reused an open mark after the driver had already
started writing it, so the acknowledgement came to mean strictly less than the messages gated on it
required: the driver reports batch one durable, raft releases an append response attesting to batch
two. It is also a convoy — under a steady stream of appends a reused mark never stops growing and
never completes. Coverage is now frozen at handover.

*The engine kept the entries a conflicting append discarded.* A `Set` overwrites only the keys it
names, so after a truncation the engine still held the dead branch's tail above the new last index,
and recovery read back a new prefix spliced onto it — gapless by index, so `Restore` accepted it, and
wrong in every entry above the cut. The batch now clears the suffix atomically, which is what
`DeleteRange` is in the frozen interface for (Amendment A3).

**Why the checkers caught it here and not earlier.** The gate needed a crash, a restart with a sync
in flight, and a leader still replicating into the window — the single-cut schedule mix produces that
combination, and 12 ms of modelled fsync against a 6 ms worst-case link is what makes the window
exist at all.

**A harness defect the fix exposed, recorded here because it changes how much the earlier green was
worth.** The ledger the persist-before-reply oracle reads was itself fed by that same visible-state
read-back, so its durable watermark was inflated: across 10,000 seeds the read-back was ahead of
durability **44,911 times**. An inflated watermark does not make the oracle noisy, it makes it
**silent** — every ack looks covered. With the ledger corrected to record what the driver actually
made durable, the 300-seed sweep went from 2 violations to 257, and all 257 were the mark-coverage
defect above. Per DR-29 the harness defect itself is recorded in its fix commit rather than as an
entry here, but the number belongs in this entry: the oracle that caught BUG-005 was weaker than it
looked while it was catching it.

**What this would have caused in production.** A lost acknowledged write, by the exact mechanism
DR-8 wrote down. A follower acks an entry it has not written; the leader counts that ack toward a
quorum and commits; the follower crashes, loses the entry, and comes back with a shorter log. If it
then wins an election — and nothing stops it, because the up-to-date check compares against a log
that no longer contains the entry — the committed entry is gone. "Committed is forever" fails, and
the client was told the write succeeded.

**Fix.** Beyond the four causes above, one structural refusal: `markFor` now **panics** when asked
for the mark covering an index that is neither durable nor covered by an open mark. That state has no
gate to wait on, so any message attesting to it would be released immediately — silently, and only on
the schedules where the gap is reachable. Refusing it where it is constructed is the fourth time this
project has fixed a class by making it unrepresentable rather than catchable, after Wall/Mono, the
epoch stamp and the D5 conformance check.

The truncation assertion was corrected in the same pass, and the correction is worth recording
because it was a *false* invariant sitting in the code: it refused any truncation at or below the
durable watermark, on the reasoning that the driver would then have acknowledged an entry that later
vanished. That is a stronger claim than Raft makes. §5.3 has a follower delete a conflicting entry
and everything after it, and those entries are routinely already on disk; a follower's persisted
suffix being overwritten by a new leader is the protocol working. What may never be truncated is a
**committed** entry, and that is what it asserts now. The false assertion was unreachable for exactly
as long as the durable watermark never moved — the same defect twice over: a claim nothing exercised,
guarding a watermark nothing advanced.
