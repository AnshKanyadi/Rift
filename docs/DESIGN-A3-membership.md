# DESIGN-A3: single-node membership changes with learner catch-up

**Status:** written before the code. Decisions marked **[assumed]** are taken under the cadence
ruling of 2026-08-18; decisions marked **[frozen]** touch a frozen interface and are explicitly *not*
riding on assumed ratification, per the same ruling. **Author:** Claude. **Decider:** Ansh.
**Phase:** A3. **Depends on:** A2, signed.

---

## 1. The problem, and the one thing that makes it safe

A cluster has to be able to replace a machine. Raft's general answer is **joint consensus**: move
through a combined configuration in which both the old and the new majority must agree, then out the
other side. Amendment A6 cut it — production etcd omits it too — and A3 ships the alternative:
**one server at a time**.

The whole safety of that alternative rests on a single fact, and §4 states it properly because it is
the reason this phase can exist without joint consensus at all.

The dangerous part is not the arithmetic. It is that a configuration change is a *log entry*, so it
can be uncommitted, overwritten, replayed after a crash, or compacted away — and every one of those
has to leave the cluster agreeing on who is in it.

---

## 2. D-A3-1: `ConfChangeV2`, whose name says joint and whose phase says not **[frozen]**

**The conflict, stated before it is resolved.** DESIGN-A0 D5 froze:

```go
ProposeConfChange(id ProposalID, cc ConfChangeV2) error
```

`ConfChangeV2` is etcd's name for the change type that *supports joint consensus*; V1 is the simple
one. So the frozen signature names a joint-shaped API for the phase that Amendment A6 explicitly
limits to single-node changes. D5 froze the name and nothing about the type's contents.

**Candidates.**

1. **Make `ConfChangeV2` a single-change struct** despite the name. Conforms to the letter, and
   leaves a type called V2 that cannot express what V2 means.
2. **Give it the general shape** — a list of changes and a transition mode — and **refuse** anything
   but a single change at A3, with an error naming A6.
3. **Ask for the freeze to be amended** to `ConfChangeSingle`.

**Recommendation, taken: (2).** It conforms to the frozen signature exactly, it keeps the type
honest about what it is, and the STRETCH item can be enabled later by deleting a refusal rather than
by changing a frozen signature. A3's refusal is explicit and cites A6, so the cut is visible at the
call site rather than implied by an absence.

**This is reported, not assumed.** The ruling is that nothing touching a frozen interface may ride on
assumed ratification, and this touches one. It is implemented so the phase can proceed, and it is the
first thing in the A3 report.

**Why it is not a stop condition.** The stop condition is a *contradiction* with a frozen interface.
Implementing the frozen signature, with the type it names, and refusing the variants a later
amendment cut, contradicts nothing: A6 constrains the semantics, D5 constrains the shape, and both
are satisfied at once.

---

## 3. D-A3-2: a configuration entry takes effect when it is APPENDED **[assumed]**

**Problem.** A configuration change is a log entry. Does a node start using the new configuration
when the entry is appended, or when it is committed?

**Candidates.** (1) On append. (2) On commit.

**Tradeoffs.** (2) is the intuitive one and it is wrong, in a way worth writing down. Suppose a
leader appends "remove C" and waits for it to commit. Commitment is counted under the *old*
configuration, which still contains C — so C's vote can be what commits its own removal, and a node
that has been removed goes on voting until it hears otherwise. Worse, if the entry is committed under
the old configuration but a later leader has not seen it, two different configurations are
simultaneously live with no rule about which wins.

(1) is the dissertation's answer and it is the one that makes the overlapping-quorum argument in §4
work: **a node uses the latest configuration in its log, committed or not.** A configuration entry
that is later overwritten takes its configuration with it, and truncation must therefore recompute
the active configuration — which is where the bugs live, and where A3's oracles look.

**Recommendation, taken: (1).**

**The consequence, recorded up front:** every path that changes the log — append, truncate, snapshot
install, restore — must recompute the active configuration from what the log now says. Not one of
them may forget, and CLAUDE.md's sharp-edges list already names two of them.

---

## 4. The safety argument, which is the whole reason A3 can skip joint consensus

**Claim.** With configurations differing by at most one server, any majority of `C_old` and any
majority of `C_new` overlap in at least one server.

**Proof.** Let `|C_old| = n`. A single-server change gives `|C_new| ∈ {n-1, n+1}`, and
`C_old ∪ C_new` has at most `n+1` members. A majority of `C_old` has at least `⌊n/2⌋+1` members; a
majority of `C_new` has at least `⌊|C_new|/2⌋+1`.

Take the larger case, `|C_new| = n+1`: the two majorities have at least `⌊n/2⌋+1` and `⌊(n+1)/2⌋+1`
members, drawn from a universe of `n+1`. Their sizes sum to at least

```
(⌊n/2⌋ + 1) + (⌊(n+1)/2⌋ + 1) = n + 2 > n + 1
```

so by pigeonhole they share a member. The smaller case, `|C_new| = n-1`, is the same argument with
`C_old` as the larger set and a universe of `n`.

**Why that is enough.** Every Raft safety property is proved from "any two quorums intersect": an
election in `C_new` cannot succeed without a server that also counts toward `C_old`, so it cannot
elect a leader that a `C_old` quorum has not endorsed, and a commit under one configuration is
visible to any leader elected under the other. Joint consensus exists precisely to restore this
overlap when configurations differ by *more than one* server, where the sums above no longer exceed
the universe. **Restricting to one server at a time is not a weaker version of joint consensus; it is
the condition under which joint consensus is unnecessary.**

**What it does not buy, stated so the cut is honest.** Replacing a server means two changes — add
then remove — and between them the cluster is one server larger, which changes its failure tolerance
for the duration. A five-node cluster mid-replacement is six nodes needing four to commit. Joint
consensus does it in one step. That is the price A6 accepted, and A4's rebalance rider pays it by
ordering add-then-remove and never the reverse.

---

## 5. D-A3-3: learners, and what "caught up" has to mean **[assumed]**

A new server starts as a **learner**: it receives entries and applies them, and it is counted in no
quorum and casts no vote. Promotion to voter is a second configuration change.

**Why the two-step is not ceremony.** Adding a slow server directly as a voter raises the quorum
immediately while that server can contribute nothing, so a cluster that could tolerate one failure
now tolerates none until the new server catches up. Under a snapshot-sized backlog that can be
minutes.

**Candidates for "caught up".** (1) The leader decides by eye. (2) The learner's match index is
within a bounded distance of the leader's commit index. (3) The learner must be exactly current.

**Recommendation, taken: (2), with the bound stated and the promotion refused while it lags.** (3) is
unreachable under continuous writes — the target moves. (1) is not checkable. The bound is a
configured number of entries; promotion is *refused*, with an error naming the gap, rather than
queued, because a queued promotion is a promotion whose preconditions were true at some point in the
past.

**And the catch-up is bounded in time as well as distance.** A learner that never catches up must not
hold a rebalance open forever; the caller retries or gives up, and A3 exposes the gap rather than
hiding it inside a wait.

---

## 6. D-A3-4: the configuration on disk **[assumed]**

The active configuration is derivable from the log — replay every configuration entry — for exactly
as long as the log goes back far enough. After compaction it does not.

**Recommendation, taken: the snapshot carries the configuration**, and recovery is *snapshot
configuration, then every configuration entry in the log tail, in order*. A node recovering from a
snapshot must know who it is talking to before it can do anything else, and CLAUDE.md's invariant
list says so directly: *snapshots carry the active config*.

This is why A2's snapshot format changes again at A3, and why the corpus will need regenerating a
second time — recorded now rather than discovered later.

---

## 7. D-A3-5: one change in flight at a time **[assumed]**

A configuration change is refused while an earlier one is not yet committed.

Without the rule, two changes can be in flight in different logs and a leader can be elected under a
configuration that never existed anywhere else. It is the same reasoning as §4 one level up: the
overlap argument is about *two* configurations, and three simultaneously live configurations have no
such guarantee.

---

## 9. What the implementation taught

### 9.1 A "caller bug versus runtime condition" split expires when the system gains a way to change itself

A2 classified a leadership transfer to an unknown target as a caller bug and panicked. A3 makes
membership change under the caller's feet, so it became a runtime condition — and the panic fired on
the first sweep after churn landed, on 65 of 300 seeds. BUGS.md **BUG-010**.

The A2 decision was reasonable and became wrong without anybody touching it. A classification of what
a caller *could have known* is a statement about what the system can change behind it, and it needs
re-asking at every phase that adds one.

### 9.2 The membership oracles were induced against a configuration with no membership

Five mutants reported ALIVE. Their covering tests called `assertOracleSilent`, whose default was
still A2's options — which schedule **zero** membership changes. The inductions ran in a
configuration that could not produce the defect, which proves the same nothing as no induction.

Caught because the power lane measured the same mutants under the sweep's real configuration and got
different numbers. Two instruments disagreeing is how either of them gets checked.

### 9.3 A configuration must be checkable against its own log, continuously

`recomputeConf` is where §3 says the bugs live, and the first check compared a node's recovered
configuration against one derived from the same recovered bytes — at *recovery only*. A mutant that
drops the recompute on truncation was detected **0 of 300 seeds**, because something later almost
always repaired it: a subsequent configuration entry, a snapshot install, a restart.

`AssertConfConsistent` recomputes from the log and compares, and the driver runs it at every drain.
Detection went 0 → **12 of 300, first at seed 1**. A defect repaired before anybody looks is a defect
nobody finds.

### 9.4 The power lane immediately earned itself

A3's shape — four nodes, one of them a learner, membership churn — dropped four classes below their
floors: M14 15→1, M25 15→2, M19 4→2 of 1500, and **M34 1→0 of 3000**, the class that found BUG-009.
The mix was widened again (eight crashes and six partitions over fourteen seconds), restoring M14 to
15, M25 to 16 and M19 to 9.

M34 did not come back and could not be made to: it needs a leader that has *not* compacted sending
`PrevLogIndex 0` to a follower that has, and A3's cluster compacts sooner because four nodes share the
same client traffic. Its floor is pinned to the A2 shape, with the measurement recorded, exactly as
M18's is pinned to A1's.

**This is the lane doing the job it was silent through in A2.** Every number above would have been
invisible one phase ago.

---

## 8. Exit criteria

Ansh's, verbatim.

1. Membership changes one node at a time, with the joint-consensus alternative recorded as cut per
   STRETCH.
2. A learner that catches up before promotion, with the catch-up bounded and the promotion refused
   while it lags, and a test proving a promotion during catch-up cannot lose a committed entry.
3. The single-node change safety argument stated here with the overlapping-quorum reasoning.
4. Configuration changes surviving crash and restart, including a node that crashes mid-change and
   recovers into either configuration.
5. Snapshots carrying the configuration.
6. Every new oracle induced before it counts; every bug into BUGS.md with its mutant class; corpus
   green or deliberately regenerated with the reason recorded; 10k seeds with zero safety violations
   and inconclusive explained.
7. The power lane covering every mutant class before the phase closes.

### Status against them

Claude does not mark phases complete; this is evidence for a ruling.

| # | criterion | evidence |
|---|---|---|
| 1 | one server at a time, joint consensus recorded as cut | `ProposeConfChange` refuses joint transitions and multi-server changes by name, citing A6; `single-server-change` checks it from outside, induced by `M35` (293/300) |
| 2 | learner catch-up bounded, promotion refused while lagging, promotion during catch-up loses nothing | 24,075 changes proposed across 10k seeds, 1,289 refused, **475 of those a lagging learner**; `TestPromotionIsRefusedWhileTheLearnerLags` and `TestPromotedLearnerCannotWinWithoutTheCommittedEntries` |
| 3 | the overlapping-quorum argument stated | §4, with the pigeonhole step and what the cut costs |
| 4 | configurations survive crash and restart | 7,846 restarts recovered a log carrying a configuration change; 70,923 recoveries cross-checked against an independent derivation; `M38` and `M39` induce both halves |
| 5 | snapshots carry the configuration | `Snapshot.Conf` and `SnapshotMeta.Conf`, on the wire and on disk; `M39` removes it and is caught 96/300 |
| 6 | oracles induced, bugs in BUGS.md, corpus regenerated with the reason, 10k green | **pass 9988, violation 0, inconclusive 12, errors 0**; BUG-010 with `M40`; ten bundles re-pinned and verified |
| 7 | the power lane covering every mutant class | 21 classes floored, 14 opted out with reasons, and a patch declaring neither fails the lane |

Mutant suite: 34 killed, 1 canary alive, 0 mismatched, 0 rotted.
