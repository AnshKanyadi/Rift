# DESIGN-A4: multi-raft, size-threshold splits, and the manual rebalance rider

**Status:** written before the code. Decisions marked **[assumed]** ride the cadence ruling of
2026-08-18; decisions marked **[frozen]** touch a frozen interface and are reported, never assumed.
**Author:** Claude. **Decider:** Ansh. **Phase:** A4. **Depends on:** A3, signed.

---

## 1. What changes, and the one thing that changes underneath everything

Until now the cluster has been **one** Raft group. A4 makes it many: the key space is cut into
ranges, each range is its own group, and every node hosts a replica of several of them over one
transport and one engine.

Three features ride on that — range descriptors with epochs, size-threshold splits, and a manual
rebalance command (the A10 collapse, Amendment A6) — but none of them is the hard part.

**The hard part is that every single-group assumption in the harness becomes a per-range assumption,
silently.** An oracle that compared "node 0's log" against "node 1's log" was comparing one thing;
over many ranges it compares a bag of unrelated logs and the comparison stops meaning what it said.
§6 is that audit, and it is the section this phase will be judged on.

---

## 2. D-A4-1: a node hosts replicas; a replica is what used to be a node **[assumed]**

**Problem.** `store.Node` is one Raft group plus its engine, durability record and state machine.

**Candidates.** (1) One `store.Node` per range, several per machine. (2) `store.Node` becomes a
container of `Replica`s sharing the machine's engine and transport.

**Tradeoffs.** (1) is a smaller diff and the wrong shape: the engine, the durability bookkeeping and
the crash boundary belong to the *machine*, not to the range. A crash takes every replica on the
machine, and modelling it as independent crashes of independent nodes would make the simulator
generate schedules the real system cannot produce — the harness lying in the system's favour.

**Recommendation, taken: (2).** `Replica` is what `Node` was; `Node` owns the engine, the epoch
guard, the mailbox and the set of replicas. One crash, one restart, one durability stream, many
groups.

**Determinism consequence, recorded up front:** the replica set is a **sorted slice keyed by
RangeID**, never a map. A map range here would be the classic leak, and it would leak into message
ordering, which is the one place it would be hardest to notice.

---

## 3. D-A4-2: ranges, descriptors and epochs **[assumed]**

A `RangeDescriptor` is `{ID, Start, End, Epoch}` over `[Start, End)`. The key space starts as one
range covering everything, and a split cuts it in two.

**Epochs are the anti-staleness device.** A client routes a request using a cached descriptor and
sends the epoch it routed under. A replica refuses a request whose epoch is behind its own, and the
refusal is a *typed* answer — `StaleRangeEpoch` — not a silent drop, because a silent drop is
indistinguishable from a partition and the client would retry forever against the same stale cache.

CLAUDE.md's invariant list names this directly: *no request served under a stale descriptor epoch.*
It becomes an oracle in §6.

**Engine keys are namespaced per range** — `r/<id>/hs`, `r/<id>/e/<index>`, `r/<id>/snap`,
`r/<id>/desc` — so a replica's state is a contiguous keyspace that can be written, cleared and
recovered without touching another's. That is also what makes `DeleteRange` earn its place a second
time (Amendment A3).

---

## 4. D-A4-3: the split is a Raft operation **[assumed]**

**Problem.** A split changes two ranges at once: the left shrinks, the right is born. Every replica
of the left range must do it, at the same point, and survive a crash in the middle.

**Candidates.**

1. **A coordinator does it out of band** — the leader creates the right-hand range and tells the
   others.
2. **A log entry.** The leader proposes an `EntrySplit`; every replica applies it at the same log
   index.

**Tradeoffs.** (1) has no agreed point at which the split happened, so a crash mid-way leaves
replicas that disagree about which range owns a key — and nothing in the log says who is right. (2)
gives the split a *position*: the split happened at index N, every replica applies it at index N, and
recovery replays the log to the same place. A crash cannot leave a replica between the two views,
because there is no between: the entry is applied or it is not.

**Recommendation, taken: (2).**

**The right-hand range is derived, not transferred.** On applying the split entry, every replica
computes the new range's initial state from its own applied state — the keys at or above the split
key — and creates the replica locally. Nothing is sent, so nothing can be lost in transit, and every
replica constructs the same thing because they all applied the same prefix. That is the property
state machine safety already guarantees, spent here.

**Idempotence is required, not optional.** `appliedIdx` is not persisted (A1's decision: a node must
not recover claiming an entry was committed on the word of its own memory), so recovery re-applies
from the snapshot index and will re-apply the split entry. Applying it twice must be the same as
applying it once, or a recovering node destroys a range that has moved on. So the apply checks
whether the right-hand range already exists.

---

## 5. D-A4-4: rebalance is a command, and it is two operations **[assumed]**

`Rebalance(range, from, to)` is add-then-remove, never the reverse, and it moves leadership out
before removing a leader:

1. add `to` as a learner, wait for catch-up (A3's bound),
2. promote it to voter,
3. if `from` is the leader, transfer leadership away,
4. remove `from`.

**Add-then-remove is the safety property, not the ordering preference.** Remove-then-add drops the
cluster to a smaller configuration first, so a failure during the move costs quorum. CLAUDE.md's
invariant list says it: *replica moves are add-then-remove; quorum availability is never voluntarily
reduced.*

Automatic placement is STRETCH (Amendment A6). A4 ships the mechanism and the command.

---

## 6. The oracle audit, which is what this phase will be judged on

**Every existing oracle is asked one question: does it still mean what it says over many ranges?**
There are three possible answers and each has to be given explicitly, because the dangerous one is
the one nobody notices.

| oracle | reads | over many ranges | resolution |
|---|---|---|---|
| election safety | leaders per term | **false positives** — two ranges legitimately have different leaders in term 1 | per range |
| log matching | durable logs pairwise | **weaker, silently** — logs of unrelated ranges rarely share an (index, term), so the comparison finds nothing and reports green | per range |
| leader completeness | committed entries vs later leaders' logs | **false positives** — range A's entry is not in range B's leader's log | per range |
| state machine safety | applied streams pairwise | **false positives** — index 3 of two ranges are different entries | per range |
| apply continuity | one node's apply stream | **false positives** — interleaved ranges look like gaps | per range |
| snapshot equivalence | snapshot digest vs replayed log | **false positives** — the wrong log | per range |
| persist-before-reply | acks vs durable log | **weaker, silently** — the durable "log" is a merge, so the watermark is wrong in both directions | per range |
| single-server change | configuration entries | correct either way, but attribution is lost | per range |
| linearizability | per-key client history | **still correct** — a key belongs to exactly one range at a time, so per-key is finer than per-range | unchanged, and stated |

**The two marked "weaker, silently" are the eleventh vacuous-green instance waiting to happen**, and
they are why this table exists rather than a sentence saying the ledger became per-range. A false
positive announces itself on the first seed. A checker that quietly stops finding things does not,
and this project has now recorded ten of those.

**One new oracle:** *range epoch monotonicity* — no request is served under an epoch behind the
serving replica's own, and a range's epoch never decreases. It is the invariant CLAUDE.md names and
the thing splits and rebalances can break.

---

## 7. The caller-bug versus runtime re-ask, per BUG-010

BUG-010's lesson generalises and the ruling made it standing: **a classification of "caller bug" is a
statement about what the system can change behind the caller, so it is re-asked at every phase that
adds a way to change something.** A4 adds two: a range can split under a caller, and a replica can
move. Every panic in `raft/` and `store/` is re-examined against those, and the answers are recorded
in §11 with the ones that moved.

---

## 8. Exit criteria

Ansh's, verbatim.

1. Multiple raft groups on the same node with per-range independence, and the two-mark generalisation
   from A2 applied wherever a message attests to state in two independent streams.
2. Size-threshold splits with the split itself being a raft operation, so a split survives crash and
   restart and a node recovering mid-split lands in exactly one consistent view.
3. Manual rebalance moving a replica by conf change plus leadership transfer, with nothing committed
   lost across the move.
4. Oracles extended to judge per-range, with every existing oracle either made per-range or proven
   still meaningful cluster-wide.
5. The caller-bug versus runtime classification re-asked across `raft/` and `store/` per BUG-010.
6. Power lane floors re-measured under A4's shape, with any class dropping below its floor
   investigated before the phase closes.
7. Every new oracle induced; every bug in BUGS.md with its mutant class; corpus green or deliberately
   regenerated with the reason; 10k seeds zero violations with inconclusive explained.

---

## 9. What the implementation taught

Written after the code, and the entries are ordered by what they cost.

### 9.1 One sentence, five times: a fact derived from a log position must be derived AT that position

A4 produced five defects and they are the same defect. Each time, something was computed from state
that was *near* the right log position instead of *at* it.

| bug | the thing derived | where it was taken from | where it had to come from |
|---|---|---|---|
| BUG-011 | a range's extent at recovery | a descriptor key aligned with no index | the snapshot, which is aligned with exactly one |
| BUG-013 | a range's extent after an install | the installing node's own extent | the snapshot that arrived |
| BUG-014 | whether a key belongs to this range | the extent when the request *arrived* | the extent at the entry's index |
| BUG-015 | a split-born range's configuration | the **active** configuration, effective on append | the configuration at the split's index |
| BUG-012 | whether a split entry took effect | every split entry, unconditionally | the extent the range had when it reached that entry |

This is BUG-004's sentence — *an identifier is not a position* — arriving from five directions in one
phase. The structural answer is that the extent is no longer something the storage layer keeps
**about** the state machine; it is part of what the state machine **is**. `encodeMachine` serialises
the extent and the keys together, so it travels with every snapshot, is restored at the index it
belongs to, and is covered by the digest snapshot equivalence compares. The class did not become
impossible; it became **caught**.

### 9.2 The new oracle is not the one §6 predicted, and the reason is worth recording

§6 promised *range epoch monotonicity*. It was written before the extent moved into the state machine
payload, and afterwards the promised oracle would have checked the harness's own model rather than the
system: the model replays committed entries and advances the epoch by construction, so asking it
whether the epoch decreased is asking it about itself.

What actually covers epoch monotonicity is snapshot equivalence, and it covers it **against every
node's real snapshot**: the digest includes the extent, so a replica whose epoch or bounds went
backwards diverges from the model at that index and the oracle fires. That is not a downgrade — it is
the same claim, checked against a system fact instead of a harness fact. BUG-011 and BUG-013 are both
instances of it firing.

Two oracles were built instead, and both read facts the system produced:

- **`split-partition`** compares the split entry in the parent's committed log against the birth state
  the child's replicas actually wrote, and requires that a split every replica refused created
  nothing. This is the one failure no per-range oracle can see: a parent and a child each internally
  consistent and disagreeing with each other.
- **`rebalance-safety`** reads the committed configuration entries and requires that a move's addition
  commits before its removal. One-server-at-a-time makes order the whole property: the committed voter
  count goes N → N+1 → N and never dips, which is *quorum availability is never voluntarily reduced*.

### 9.3 The rebalance mechanism is stateless, and that is a design decision with a reason

The obvious shape is a small state machine on the leader: adding, promoting, transferring, removing.
It has a hole with a name — the leader can lose leadership between the add and the remove, and the
next leader has no idea a move was under way, so the move stalls with an extra replica nobody will
remove.

So `RequestMove` carries no state. Each order reads the configuration and does whichever step is next,
which makes the operation idempotent, replayable, and completable by whatever node happens to be
leading when it is next asked. Ordering the same move repeatedly is how it finishes.

Statelessness costs exactly one thing, and it is paid explicitly rather than assumed away: the
mechanism cannot tell *"the destination is already a voter because I just added it"* from *"the
destination was already there"*. The first is the middle of a move; the second is not a move at all,
and treating it as one goes straight to removing the source. The caller knows which order is the first
one, so the caller says so — `RequestMove(from, to, begin)` — and the precondition is stated instead of
inferred.

**A stalled move is not a violation and the oracle says so.** It leaves the range with an extra
replica and no removal, which is wasteful and completely safe: it is the direction the invariant wants
to fail in. What stops "every move stalled" from reading as evidence is the sweep's non-vacuity check,
which requires completed moves.

### 9.4 The third instrument to catch itself, and the number that makes it matter

`rebalance-safety` reported **252 violations in 300 seeds** against a system that was behaving
correctly, then one more on seed 103 after the first cause was fixed. Both were the oracle, not the
cluster (BUG-016).

The root cause is that a move is an *intent*, and no sequence of committed entries states one: an add
and a remove look exactly like two unrelated membership changes. The oracle had no way to tell whose
removal it was looking at.

Two fixes, neither of which is a loosened check:

1. **The two membership drivers are separated in time.** Churn runs in the first half of a run,
   rebalance in the second, so a removal has one plausible author. The alternative was to tag
   configuration entries with a move identifier, which changes a wire format for the convenience of a
   checker. **The cost is recorded in §7 below and it is real: no seed exercises a move racing an
   unrelated membership change.**
2. **A move owns its range's membership changes only until the next move on that range is ordered.**
   The window comes from the harness's record of what it ordered — not from anything the cluster says.

The reason this is written down at length is the rate. A checker that reports 252 false violations in
300 seeds is worse than no checker: it is a checker people learn to override.

### 9.4b The eleventh vacuous-green instance was predicted in the right class and the wrong place

§6 named the two oracles that would go *weaker, silently* over many ranges and called them "the
eleventh vacuous-green instance waiting to happen". Making the ledger per-range fixed both before
they could be.

The eleventh happened somewhere else entirely. The 10,000-seed exit run reported **0 requests refused
for a stale descriptor epoch** — not a low number, zero — because the sweep's clients carried no
routing at all, so the check was skipped on every request they ever made. The mechanism was declared,
wired, tested by nothing, and guarding an invariant CLAUDE.md names by name.

It was visible the whole time. The exit run *printed* the number and nothing *asserted* it, which is
the difference between a report and a lane. Three assertions now exist — stale-epoch refusals,
out-of-extent refusals, and completed moves must all be nonzero — and the third is the one worth
naming: **a stalled move is safe**, so the rebalance oracle is perfectly green over a sweep in which
every single move stalled.

Every request now routes the way a client with a cold cache does: believing the whole key space is
the first range at epoch one. After a split, requests for moved keys name a range that no longer owns
them and requests for kept keys name an epoch that has moved on. 55,154 refusals across 300 seeds,
zero violations. Induced: reverting the routing fails the run with the epoch message.

**The lesson is about where to look, not about this mechanism.** The audit that found the two
silently-weaker oracles was looking at oracles, because that is where the previous ten instances
were. The eleventh was in a mechanism the oracles depend on, one layer below where anybody was
checking.

### 9.4c Criterion 1's two-stream question, answered

A2's rule is that a message attesting to state in two independent streams waits
for **both**. A4's candidate pair is a split: the left range's log holds the entry, and the right
range's birth snapshot is a separate batch under a different range's keys.

**The honest answer is that they are not independent.** The log entry is written first and the
engine advances durability in sequence order, so the right range's birth state cannot be durable
while the entry that justifies it is not. There is no second mark to wait on, because there is no
second stream — there is one stream with an ordering guarantee.

That is an argument standing on a property of the engine, so it gets a check rather than a comment.
`split-partition` gained a third clause: **every range that exists was created by a split entry in
some committed log**. If the ordering ever fails — a crash that keeps the birth snapshot and loses
the entry — a range exists that no log created, claiming keys the left range still claims. Nothing
else would see it: the child is consistent with its own history and the parent is consistent with
its own, and only the pair is wrong.

**The clause is redundant against every mutation this system admits, and that is recorded rather
than hidden.** M48 plants exactly the defect — the leader creates the right-hand range out of band,
candidate design (1) from §4 — and is caught on 276 of 300 seeds, never by this clause. An
out-of-band range derives its birth state at a different moment from every other replica's, so
snapshot equivalence reports the divergence first. So the clause is induced directly, against a
hand-built ledger, in `raftcheck/splitpartition_test.go`; the reasoning is written there. It is kept
because "something else gets there first" is a coincidence of how ranges derive state, and one pass
over the ledger is cheap insurance against that coincidence ending.

### 9.5 The mutant lane's baseline gate fired, and it was right

The lane reported `INVALID` and refused to attribute a single kill. The cause was mundane — A4's
sweeps pushed the baseline package past Go's default ten-minute test timeout — and the behaviour was
exactly right: it would not report kills against a tree it could not first watch pass. The fix is an
explicit `TEST_TIMEOUT`, never a shorter sweep.

---

## 10. Limitations, recorded

1. **Moves and unrelated membership churn never race.** §9.4. The rebalance oracle cannot attribute a
   removal when both drivers are live, so the plan separates them in time. A move racing a churn
   removal is unexercised, and the honest description of the rebalance evidence is *"safe against
   crashes, partitions, leader churn and splits; not yet against a concurrent membership change."*
2. **Range merges are out of scope**, per CLAUDE.md A4. Extents therefore only ever shrink, and the
   `split-partition` oracle leans on that: a range once born is never unborn.
3. **A move is best-effort.** Leadership moving mid-move abandons it (§9.3). Safety is unaffected;
   completion is not guaranteed by any single order.

---

## 11. The caller-bug versus runtime re-ask, answered

Every `panic` in `raft/` and `store/` re-examined against A4's two new ways for the system to change
state behind a caller: **a range can split**, and **a replica can move**. Twenty sites. Three moved,
one is new-and-deliberate, and one is flagged for I2.

### The two A4 changes, and what they touch

| a caller could hold a stale... | who holds one | answer |
|---|---|---|
| range extent | a client routing from a cached descriptor | typed `StaleRangeEpoch` refusal and retry — never a panic (§3) |
| range extent | the apply path, one log position behind a split | refuse the command at the log position and re-route (BUG-014) |
| range identity | `RequestMove(rng, …)` for a range this machine no longer hosts | `replicaOf` returns nil, the order is a no-op |
| configuration | a move's `from`/`to` removed by something else | no-op, per BUG-010's ruling |
| leadership | `maybeSplit`, `RequestMove` after a step-down | guarded by `IsLeader`, no-op |

**None of A4's new state transitions produced a new caller-bug panic**, and that is the answer the
re-ask was for. Every one of them is a *runtime condition*: the caller could not have checked, because
the answer can change between the check and the call.

### The sites, classified

| site | classification | changed by A4? |
|---|---|---|
| `TransferLeadership` to self | caller bug | no — a caller bug in any membership |
| `TransferLeadership` to a non-member | runtime | no — moved at A3, BUG-010 |
| `Ready` applying behind the snapshot | internal invariant | no |
| `marksFor` with no mark open | internal invariant | no |
| `append` leaving a gap | internal invariant | no |
| `truncate` below the commit index | internal invariant | no |
| `truncate` below the snapshot | internal invariant | no |
| `suffixFrom` below the snapshot | internal invariant | no |
| `ConfigurationAt` out of range | **error, not panic** | **new** — see below |
| `applySplit`: a key in neither half | **internal invariant** | **new** — see below |
| `restart`: a split-born range with no snapshot | internal invariant | new |
| store durability cross-checks (×5) | internal invariant | no |
| store recovery cross-checks (×3) | internal invariant | no |
| `Ready`: an undecodable snapshot | **flagged for I2** | see below |

### The three worth naming

**`raft.ConfigurationAt` returns an error rather than panicking.** Its only caller today asks about an
index it is in the middle of applying, so a failure there *is* a caller bug — but that is a fact about
today's caller, not about the function. Returning an error keeps the classification at the call site,
where the next caller has to make it again. `store.applySplit` converts it to a panic, which is the
honest local answer: it asked about an entry it is holding.

**`applySplit`'s partition assertion is new and it is deliberately loud.** The two halves of a split
partition the extent, so every key the range holds lands in exactly one of them. A key in neither means
the range was holding a key it did not own — BUG-014's residue, which survived a whole phase precisely
because nothing complained about it. Tolerating it is how that bug lived; panicking is the fix's other
half.

**The undecodable snapshot is a panic that will not survive I2.** The sim's transport reorders,
duplicates and drops, but it never corrupts bytes, so an undecodable snapshot today can only be a
harness or codec defect and a panic is right. A real transport can deliver a corrupt frame. This is
recorded here rather than fixed now because changing it before there is a real transport would be
guessing at the right behaviour — but it is on I2's list, and this table is where it was written down.

---

## 11b. The power re-measurement, and the two classes that dropped

Twenty-eight classes floored, fifteen opted out with a reason, **zero below floor**. Twenty-two
classes were re-measured against A4's shape and their headers now carry both numbers, so the trail
survives the next phase.

Most classes got *stronger* — M39 went from 96 to 258, M31 from 100 to 151 — because more ranges mean
more snapshots, more recoveries and more configuration entries per seed. **Two got weaker:**

| class | A3 | A4 | floor |
|---|---|---|---|
| M28 mark coverage grows after handover | 280 | 246 | 140 |
| M33 state machine drops a command | 290 | 266 | 145 |

Neither is below its floor, and the criterion only demands an investigation of a class that is. Both
were investigated anyway, because a 10% drop with no explanation is a number waiting to become a
surprise.

**The obvious explanation is wrong.** A4 does not run a quieter schedule: across the same 300 seeds
snapshots taken went 7835 → 8218, snapshots installed 1235 → 1639, and committed entries 18235 →
19336. Every one of those moved the right way.

**The cause is splitting, and it is measured rather than argued.** Running each mutant against A4's
options with one feature removed at a time:

| | M28 | M33 |
|---|---|---|
| A4 | 246 | 241 |
| A4 with splits off | **284** | **290** |
| A4 with rebalance off | 244 | 239 |
| A3 | 274 | 290 |

Turning splits off restores the A3 number exactly. Rebalance is neutral.

**Why splitting hides these two.** Both defects are detected by comparing a range against its own
history — a snapshot digest, a durability watermark. A split *removes keys and log positions from a
range*, so a corrupted write or an over-covered mark can leave the range before anything compares it
there. The range that would have exposed the defect stops being the range that holds it.

That is a real reachability loss, it is the price of the feature, and it is now a recorded number
instead of an unexplained dip. It is also the first thing to re-check if either class ever approaches
its floor.

---

## 12. Exit criteria status

| # | criterion | status |
|---|---|---|
| 1 | multiple groups per node, per-range independence, two-mark generalisation | met — `Node` hosts `Replica`s over one engine and one crash boundary; §9.1 and the durability note in `applySplit` |
| 2 | size-threshold splits as raft operations, surviving crash and restart | met — five defects found and fixed getting here (BUG-011, -013, -014, -015 and the model's BUG-012) |
| 3 | manual rebalance by conf change plus leadership transfer | met — stateless mechanism (§9.3), 116 moves completed across 300 seeds, `rebalance-safety` green |
| 4 | oracles per-range, or proven meaningful cluster-wide | met — §6's table, all nine per-range, linearizability unchanged and stated |
| 5 | caller-bug versus runtime re-asked across `raft/` and `store/` | met — §11 |
| 6 | power floors re-measured under A4's shape | met — see the report |
| 7 | new oracles induced, BUGS.md with mutant classes, corpus, 10k seeds | met — see the report |
