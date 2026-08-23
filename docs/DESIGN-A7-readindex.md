# DESIGN-A7: read index, and linearizable reads that do not cost a log entry

**Status:** written before the code, to the point of decisions. Every decision below is marked
**[open]** and waits for a ruling; none is assumed. **Author:** Claude. **Decider:** Ansh.
**Phase:** A7 — the last Track A phase. **Depends on:** A6.

---

## 1. What A7 changes, and what it costs today

Every read in this system goes through the log.

```go
// Reads go through the log, exactly like writes.
//
// Serving a read from the leader's own applied state is a **stale read**: a
// leader that has been deposed and does not yet know it will answer happily
// from state a newer leader has already moved past. Porcupine found this on
// seed 4 the moment the four safety oracles were green.
//
// The cheap fix is read index (A7). That is not A1's scope, so A1 pays the
// honest price and replicates reads.
```

That comment has been in `store/node.go` since A1 and A7 is the phase that makes good on it. The
price it names is a full replication round per read: a proposal, a quorum of appends, a commit, an
apply. **BENCHMARKS.md will state the cost this removes**, which is the point of measuring it before
removing it.

A6 made the price worse in a way worth stating. A transaction is now seven replicated round trips —
two snapshot reads, two prewrites, the primary's record, two commits — and **two of the seven are
reads**. Add the audits (N reads each) and the bank's second-pass stability probe (N more), and
reads are the majority of what the log carries. A6's measured cost is **~4 s/seed against A5's
0.36**, and the read path is a large share of the difference.

### 1.1 What read index is

The leader can answer a read from its own applied state *if* it can establish two things:

1. **it was still the leader at some point at or after the read arrived** — otherwise a newer leader
   may already have committed writes this read would miss;
2. **its state machine has applied everything committed as of that point** — otherwise it is reading
   its own past.

Read index is the protocol for establishing both without appending anything:

```
  read arrives at the leader
    readIndex := commitIndex                  the point the answer must reflect
    broadcast a heartbeat round               confirm leadership with a quorum
    on quorum of responses at this term:      leadership confirmed AS OF the broadcast
      wait for appliedIndex >= readIndex
      serve the read from local state
```

Nothing is appended. The cost is one round of heartbeats — which the leader is sending anyway — and
several reads arriving together share one round.

---

## 2. The term-start no-op, which is not optional

CLAUDE.md's sharp-edge list names it: *"Read index needs the term-start no-op."* Here is why, in this
codebase's own terms.

`maybeCommit` implements the figure-8 rule, and the implementation is explicit about it:

> *Only entries from the CURRENT term are committed by counting. An entry from an earlier term that
> happens to be replicated on a majority is not committed on that basis, because a later leader with
> a shorter log could still overwrite it.*

So a leader that has just won an election **does not know its own commit index**. Its log may hold
entries from previous terms that are replicated on a majority, and it may not count them. Until it
commits something of its own, `commitIndex` is whatever it inherited — which can be arbitrarily far
behind the true committed prefix.

`readIndex := commitIndex` at that moment is therefore **too low**, and a read served against it can
miss writes that were committed before this leader took office. That is a stale read produced by the
mechanism whose entire job is preventing stale reads.

The fix is one entry: on becoming leader, append an empty entry in the new term. Committing it
commits everything below it, and `commitIndex` becomes true. `becomeLeader` does not do this today —

```go
func (r *Raft) becomeLeader() {
	r.role = RoleLeader
	...
	r.broadcastAppend()
}
```

— and A7 adds it.

**This is the phase's most dangerous change**, because it is one line in the hottest path in the
system and every existing count moves. Election churn produces one extra entry per election; the
10,000-seed sweep counts elections in the tens of thousands; snapshot thresholds, log-length
assertions, split thresholds and every fire count shift underneath it. §7 is about that.

---

## 3. The candidates **[open]**

### D-A7-1: how leadership is confirmed

**A. Heartbeat-confirmed read index (etcd's).** The leader broadcasts a heartbeat carrying a read
context, and serves once a quorum has answered *in the current term*.

- *for*: correct without trusting any clock, which is the property that makes it A7's scope and
  leases STRETCH's. Costs no entry. Batches naturally: reads arriving during one round all ride it.
- *against*: one network round trip per batch of reads, so a read is never faster than a heartbeat
  RTT. Under partition the leader cannot confirm and reads block rather than answering stale — which
  is correct and is what the staleness checker will be watching.

**B. Leader lease.** Serve locally while a lease derived from the last confirmed heartbeat is valid,
so most reads cost nothing at all.

- *for*: the fast one, and the reason anybody builds leases.
- *against*: **struck from the active plan** (Amendment A6). It is correct only inside a clock-skew
  envelope, and read index is correct without trusting clocks. Considering it here would be
  reopening a decision, and the escape-hatch rule from A6 §22.3 applies: an authorisation — or a
  deferral — spent on a purpose it was not granted for is a mechanism widening itself.

**C. Keep replicating reads.** The status quo.

- *for*: already correct, already tested, zero risk.
- *against*: it is the cost A7 exists to remove, and CLAUDE.md's headline claims linearizable reads
  *via read index*, which is a claim about a mechanism and not about an outcome.

**Recommendation: A.**

### D-A7-2: follower reads **[open]**

CLAUDE.md scopes A7 as *"Full protocol including the term-start no-op requirement; follower reads via
read index."* So followers serve reads too: a follower asks the leader for a read index, waits for
its own `appliedIndex` to reach it, and answers locally.

The two questions that are not obvious:

**Does the follower need its own confirmation?** No, and the reason is worth writing down. The
leader's heartbeat round establishes *the leader was leader as of the broadcast*. A follower that
receives a read index derived from that round and then waits for its own apply to reach it is
answering a state at least as new as the confirmed commit point. It does not matter whether the
follower is still in contact with the leader afterwards — the answer's freshness is pinned by the
index, not by the follower's connectivity. **A read index is a fact about a position, not about a
node**, which is A4's log-position class in a new dimension.

**What does a follower do while behind?** It waits, and if it waits past a deadline the request is
simply not answered — the same shape as every other unanswered request in this system, and the same
reason: a client that gets no answer knows nothing, which is honest, whereas a client that gets a
stale answer has been lied to.

**Recommendation: implement it.** It is in scope and it is the case that makes read index worth
having in a multi-range system, where the leaseholder-equivalent is not always the nearest replica.

### D-A7-3: what the read index is taken from **[open]**

Two candidates, and this is D-A7-1's detail rather than a separate decision, but it has its own
failure mode:

**A. `commitIndex` at the moment the read arrives**, confirmed afterwards. **B. `commitIndex` at the
moment the quorum confirms.**

A is correct and B is *more* than correct — B is a later point, so it is never stale, but it makes
reads that arrive together unable to share one round, because each would take a different index. A
is the standard and the batching is the reason.

**Recommendation: A**, with the index captured *before* the broadcast and carried with the round.

### D-A7-4: what happens to the existing read path **[open]**

Today a read is a log entry (`opGet`), stamped at propose, answered at apply. Under read index it
becomes a local answer with no entry at all.

**A. Replace it.** All reads go through read index.
**B. Keep both**, and choose per request.

**Recommendation: B, temporarily, and A by the exit criteria.** Keeping both for the phase's
duration is what makes the staleness checker meaningful: the two paths answer the same question and
must agree, and a differential between them is the strongest oracle available for this phase. By
exit, the replicated-read path stays only as a fallback that the sweep exercises, or it goes — and
which of those is D-A7-4's real question.

---

## 4. The timestamp-position class, applied preemptively a third time

DESIGN-A5 §7 and DESIGN-A6 §8 both named every fact of this shape before the code, and A6's result
was four of six held exactly, one amended by experience, one the table got right and the code got
wrong. A7's table:

| the derived fact | the wrong place to take it | the right place |
|---|---|---|
| the read index | the leader's `commitIndex` when the quorum answers | **the `commitIndex` captured when the read arrived**, carried through the round |
| whether leadership is confirmed | a majority of heartbeat responses | a majority **at the term the round was broadcast in**; a response from an older term confirms nothing |
| whether the state machine is caught up | `appliedIndex >= commitIndex` now | `appliedIndex >= THIS READ'S index`, which may be below the current commit |
| what a follower's read reflects | the follower's own `commitIndex` | the **index the leader gave it**, which is the only value pinned to a confirmed leadership |
| the term a no-op belongs to | the term when it is appended | the term at `becomeLeader`, since a term change between the two means this leader is no longer the one confirming |
| whether a read may be served during a transfer | leadership at arrival | leadership **at the confirming round**; `TransferLeadership` can intervene, and A2's transferee is exactly the case |
| the applied index a read waits on | the replica's applied index | the **range's** applied index — one node hosts many ranges, and A4's per-range ledger exists because this was got wrong once |

**Seven facts, named before the code.** The before-and-after count is reported at phase end with its
exclusions stated, as A5's four and A6's six were. The honesty of the count is the only thing that
makes it worth anything: A6's was *not* six of six, and saying so is what makes the four credible.

---

## 5. The oracle **[open]**

CLAUDE.md's exit criterion: *"staleness checker green under partitions and leader churn."*

**What it must assert.** A read served by read index reflects every write acknowledged before the
read was issued. That is linearizability restricted to reads, and this project already has the
instrument for it: **porcupine, per key**, over the client-observed history.

**What is new.** Today's reads are linearizable *because they go through the log*, so porcupine has
never been testing the read path so much as the log. Under read index the read path becomes an
independent thing that can be wrong on its own, and the checker has to be able to see the difference.

Three properties, and the third is the one that needs the most care:

1. **Per-key linearizability including read-index reads** — porcupine, as today, with reads no longer
   carrying a log index. This is the headline and the weakest of the three, because a stale read is
   only caught if some client observed the write it missed.
2. **A read's index is never above the leader's confirmed commit** — a ledger-side invariant, checked
   at the position the read was answered at, in the A4 style.
3. **Differential against the replicated path** (D-A7-4B). The same key read both ways at the same
   moment must agree. This is the property that catches a stale read *nobody else observed*, which
   is the class property 1 cannot see, and it is available only while both paths exist.

**Open question:** whether 3 is a lane or a permanent fixture. It costs a second read per checked
read, and it is the only oracle here that can catch a stale read in a quiet history.

---

## 6. What A7 does not do

- **Leader leases** — STRETCH, Amendment A6, and §3's D-A7-1B says why reconsidering them here would
  be a mechanism widening itself.
- **Observed timestamps** — STRETCH.
- **Reads at a past timestamp via read index.** A6's snapshot reads name an explicit instant and are
  already answerable from any replica holding the version; they do not need a read index and giving
  them one would conflate two questions (DESIGN-A6 §15.5).

---

## 7. The change that moves every number, and what to do about it **[open]**

The term-start no-op adds **one entry per election**. Every count in the exit run shifts, and several
assertions are phrased against counts:

- snapshot thresholds (`SnapshotThreshold: 6` applied entries) trigger sooner;
- split thresholds are keyed on range size, not entries, but the log grows faster;
- `power-mutants` floors are measured in seeds-to-detection against a schedule mix that has just
  moved — the exact situation `floors.go` records for M34 and M18/M19;
- the mid-phase and exit sweeps' fire counts all move together.

Two ways to handle it, and this is a genuine decision rather than a chore:

**A. Land the no-op first, re-measure everything, then build read index on the new baseline.**
**B. Build read index behind a flag, measure both, land together.**

**Recommendation: A.** The no-op is a correctness requirement for the mechanism and it is one line;
separating it means the re-measurement has exactly one cause. B produces a single commit in which
every number moved for two reasons at once, which is the shape that makes a power regression
unattributable — and A2's kill-time amendment exists because unattributable regressions are how
detection power is lost quietly.

**And the measurement is owed anyway.** A6 closes with three measurements outstanding — the race-lane
curve, mutant power floors under A6's shape, the unthrottled collector. The no-op moves the baseline
those are taken against, so the order matters: **A6's owed measurements are taken before A7's first
commit**, or they measure a shape that no longer exists.

---

## 8. Exit criteria, proposed **[open]**

Ansh sets these; this is the proposal.

1. Read index implemented with the **term-start no-op**, and the no-op induced — a mutant that
   removes it must be killed by a read that goes stale.
2. **Follower reads** via read index, exercised in the sweep and asserted non-vacuous: a sweep in
   which every read was served by a leader has not tested them.
3. Per-key linearizability green with reads served by read index, under partitions and leader churn.
4. The **differential** against the replicated path green, or its removal decided and recorded.
5. Every count the exit run prints asserted or deleted, and every new oracle induced.
6. §4's seven facts reported before-and-after with exclusions stated.
7. A6's three owed measurements taken **before** A7's first commit (§7).
8. Seed count at exit: Ansh's call. A6 ran 25,000 sharded; the machinery for that is now built and
   `make exit-run` is one command.

---

## 9. Open questions, verbatim, each with a recommendation

Every one of these is a decision I am not making.

1. **D-A7-1 — how is leadership confirmed?** *Recommendation: heartbeat-confirmed read index. Leases
   are struck and reconsidering them here would be a deferral spent on a purpose it was not granted
   for.*
2. **D-A7-2 — do followers serve reads?** *Recommendation: yes; it is in CLAUDE.md's scope for the
   phase and it is what makes read index worth having in a multi-range system.*
3. **D-A7-3 — is the read index captured at arrival or at confirmation?** *Recommendation: at
   arrival, so reads arriving together share one confirmation round.*
4. **D-A7-4 — does the replicated read path survive the phase?** *Recommendation: keep it for the
   phase as the differential oracle's other half, and decide its fate at exit rather than now.*
5. **§5 — is the differential oracle a lane or a fixture?** *Recommendation: a fixture while both
   paths exist, because it is the only instrument that can catch a stale read no client observed.*
6. **§7 — does the no-op land separately?** *Recommendation: yes, first, with a full re-measurement,
   so that when every number moves there is exactly one reason.*
7. **§7 — are A6's three owed measurements taken before A7 starts?** *Recommendation: yes. They
   measure a baseline the no-op is about to move.*

**Stopping here for rulings, as the protocol requires.** No A7 code is written.
