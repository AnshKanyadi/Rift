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
in §9 with the ones that moved.

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
