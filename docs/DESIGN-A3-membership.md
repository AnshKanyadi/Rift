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
