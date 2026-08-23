# DESIGN-A6: Percolator transactions, uncertainty, and the bank

**Status:** written before the code. Decisions marked **[assumed]** ride the cadence ruling of
2026-08-18; decisions marked **[frozen]** touch a frozen interface and are reported, never assumed.
**Author:** Claude. **Decider:** Ansh. **Phase:** A6. **Depends on:** A5, signed.

---

## 0. The result of this phase, stated first

**BUG-018.** Every transaction step reads the engine before it writes. The apply loop staged a whole
`Ready` into one batch and wrote it once at the end, so a step could not see the steps above it. Two
prewrites of one key in one `Ready` both succeeded, and the second overwrote the first's lock —
**leaving two transactions owning one key and neither knowing**.

> **The replica's state depended on how many entries happened to arrive together, which is not a
> function of the log — and that it *is* a function of the log is exactly what snapshot equivalence
> asserts.**

It needs **no crash, no partition, and no injected fault at all**: only two steps arriving in one
batch, which is what happens under load. Every other entry in BUGS.md required an engineered
schedule.

And the retired model could not have found it, because it replayed logically one command at a time
with no notion of a `Ready`. **A verification mechanism was replaced under protest of losing a
property, and the replacement immediately found a defect the original was structurally blind to.**
That is the argument for the swap, made by the swap. §13 is the swap; §14 is the bug, the method that
found it, and the three corrections it produced.

**And then the bank found the second one.** BUG-019 — *the* lock rather than *my* lock — also needs no
fault, and is invisible to every structural checker in the repository: the state it produces is a
perfectly well-formed Percolator database with nine units of money missing from it. §19 is why that
makes the bank workload the only instrument that could see it, and is the standing argument for
oracles that know what a domain's numbers mean rather than only what shapes its records may take.

Two of A6's three defects required **no injected fault at all**, which is a different risk profile
from every phase before it.

---

## 1. What changes, and where the failures actually live

A5 gave every write a timestamp and every read a timestamp to ask at. A6 makes a *set* of writes
atomic across ranges: either every key a transaction touched moves to its commit timestamp, or none
does, and no reader ever sees half.

The mechanism is Percolator: prewrite every key with a lock pointing at a **primary**, then commit
the primary, then the secondaries. The primary's write record is the single point at which the
transaction becomes committed.

**None of the hard parts are in the happy path.** A coordinator that runs to completion is
bookkeeping. Everything interesting is in what a *different* transaction does when it finds a lock
whose owner may be dead:

- the owner committed and the secondary's commit record was never written — **roll forward**;
- the owner died before committing — **roll back**;
- the owner is alive and slow — **leave it alone**, or the cleanup itself breaks atomicity.

Those three are the phase. §5 is the decision that makes them decidable, §9 is the oracle that judges
them, and the exit criteria require both roll-forward and roll-back to be *exercised and asserted*
rather than merely reachable.

---

## 2. D-A6-1: the transaction record lives on the primary key's range **[assumed]**

**Problem.** A transaction's commit status must be readable by anybody who finds one of its locks.
Where does it live?

**Candidates.** (1) A dedicated transaction range. (2) The primary key's own range, keyed by the
primary key. (3) Replicated to every range the transaction touched.

**Tradeoffs.** (3) is atomicity's own problem restated — the thing being built cannot be its own
foundation. (1) makes every resolution a cross-range read to one hot range, and makes that range a
single point of failure for progress on every transaction in the cluster. (2) puts the record where a
resolver is already going: it holds a secondary lock, the lock names the primary key, and the primary
key routes to a range.

**Recommendation, taken: (2).** CLAUDE.md names it: *transaction records on the primary key's range
with epoch checks so splits cannot orphan them.*

**The split hazard, and why the epoch check is not enough on its own.** A split can move the primary
key to a new range *after* a secondary's lock was written. The lock names the primary KEY, not the
range, so resolution re-routes and still finds it — this is why the lock must carry the key rather
than a range id, and A4's BUG-011 class is the reason to say so out loud: a range identifier recorded
in a lock is a position that ages, and the key is not.

---

## 3. D-A6-2: what a lock and a write record are **[assumed]**

Three record kinds per key, in the namespace A5 reserved (`kv.MetaKey`):

| record | key | holds |
|---|---|---|
| **data** | `d <key> <^ts>` | the value written at its **start** timestamp (A5's encoding, unchanged) |
| **lock** | `l <key>` | primary key, start timestamp, TTL deadline, kind |
| **write** | `w <key> <^commit_ts>` | "the version at `start_ts` is committed here", or a rollback tombstone |

A read at timestamp `T` therefore does two things A5's did not: it consults the **write** records to
find which start timestamp is visible at `T`, and it refuses to proceed past a **lock** whose start
timestamp is at or below `T` until that lock is resolved.

**Why the data is keyed by START and the write record by COMMIT.** A prewrite happens before the
commit timestamp exists, so the value has to land under something already known. The indirection is
Percolator's, and it is what makes commit a single small write per key rather than a rewrite of the
value.

---

## 4. D-A6-3: the commit point, stated exactly **[assumed]**

> **A transaction is committed if and only if the write record for its PRIMARY key exists.**

Everything else is derived. A secondary with a lock and no write record is not "maybe committed" —
its status is whatever the primary says, and any observer may look. This is the sentence the recovery
protocol is a consequence of, and it is the one an oracle can check directly.

Two orderings that are not negotiable:

1. **Every prewrite is durable before the primary's write record is proposed.** Otherwise a committed
   transaction can lose a value it promised.
2. **The primary's write record is committed in the Raft sense before any secondary's is proposed.**
   Otherwise a secondary commit record exists for a transaction that never committed, and a reader
   that trusts it sees a write from a transaction that will roll back.

Both are orderings *between* replicated operations, which A5's machinery already expresses: a
transaction step is a command in a range's log, and "durable before" is the same gate raft already
enforces. What A6 adds is that the coordinator must not advance its own state machine on an
un-committed step, which is a driver obligation and gets an assertion.

---

## 5. D-A6-4: resolution, and the decision that makes it decidable **[assumed]**

A transaction T2 finds a lock owned by T1. What may it do?

**Candidates.** (1) Wait for the TTL, always. (2) Read T1's primary record and act on it. (3) Abort
T2.

**Recommendation, taken: (2), with (1) as the tie-break and a hard rule about who may write what.**

```
resolve(lock owned by T1):
  read T1's primary write record
    exists, commit_ts C   -> ROLL FORWARD: write the secondary's write record at C, drop the lock
    rollback tombstone    -> ROLL BACK:    drop the lock and its data version
    absent, TTL not expired  -> LEAVE IT ALONE
    absent, TTL expired      -> ROLL BACK T1 by writing a rollback tombstone on its PRIMARY first
```

**The hard rule: a resolver may only ever make T1's primary record *exist*.** It may write a rollback
tombstone if there is none; it may never delete or overwrite one. That is what makes resolution
idempotent and race-safe against a coordinator finishing late: two resolvers reach the same verdict
because the first write wins and the second reads it.

**The TTL is expiry, not permission.** A resolver that finds an expired TTL does not conclude "T1 is
dead" — it *makes* T1 dead, by committing the rollback tombstone through Raft. A coordinator that
wakes up and tries to commit then finds its own primary tombstoned and aborts. There is no window in
which both outcomes are believed, because both go through one range's log and one of them is first.

**This is where A4's class applies again.** The TTL deadline is compared against a timestamp, and it
must be the *resolver's read timestamp*, not its clock now: two replicas resolving the same lock at
the same log position must reach the same verdict, or they diverge. §8 lists this among the
timestamp-derived facts.

---

## 6. D-A6-5: uncertainty intervals **[assumed]**

A read at timestamp `T` may encounter a committed value with `commit_ts` in `(T, T + maxOffset]`. That
value *might* have been written before the read began in real time — the clocks disagree by up to
`maxOffset` and nothing can tell. Returning the older value would be a stale read that snapshot
isolation forbids.

**The rule:** such a value raises `ReadWithinUncertaintyInterval`, and the transaction **restarts at a
timestamp strictly above the observed value's commit timestamp** — not at `T + maxOffset`, and not at
"now". CLAUDE.md's sharp-edge list says it: *uncertainty restarts must bump past the observed value's
timestamp.*

**`maxOffset` comes from the advertised bound, fixed at construction and cluster-uniform.** A0.4's
`clock.AssertUniformMaxOffset` already enforces uniformity every run, and A5's `hlc.Source` exposes
`MaxOffset()` so a TSO fallback carries its own uncertainty rather than borrowing a node's clock.

**The envelope checker consumes plan-derived offsets, never node-reported clocks.** The plan knows
each node's timeline because it built it; asking a node what its offset is would be asking the system
under test to grade its own skew, which is the provenance rule (`Reported[T]` may not feed a verdict
that can come out green).

**Per-node observed timestamps to shrink intervals are STRETCH** (Amendment A6) and are not in A6.

---

## 7. D-A6-6: the bank workload, and what it is allowed to see **[assumed]**

N accounts, each a key. A transfer reads two accounts at the transaction's start timestamp and writes
both. Total balance is invariant.

**The conservation check is over client-observed history only.** Not over engine state, not over the
ledger's view of committed writes: a snapshot read issued *by the workload* at a timestamp, whose
answer the client received. That is the strongest form of the claim and the only one that is not
circular — a database that conserved balance internally while showing clients something else has
failed at the only thing conservation is for.

The consequence, stated: the checker can only assert conservation at timestamps the workload actually
read at, so the workload must issue whole-bank snapshot reads deliberately rather than hoping. Those
reads are the evidence, and a sweep in which none completed is asserted against.

---

## 8. The timestamp-position class, applied preemptively again

§7 of DESIGN-A5 is the practice this project now has evidence for (DESIGN-A5 §16.1): name the class,
list every fact it governs with the wrong spelling beside the right one, *before* the code. A6's
table:

| the derived fact | the wrong place to take it | the right place |
|---|---|---|
| whether a lock has expired | the resolver's clock now | the **resolver's read timestamp**, so two replicas at one log position agree |
| whether a value is visible | the newest write record | the newest write record **at or before the read's timestamp** |
| whether a value is uncertain | `commit_ts > now` | `commit_ts` in **`(read_ts, read_ts + maxOffset]`** |
| the restart timestamp | `now`, or `read_ts + maxOffset` | **strictly above the observed value's commit timestamp** |
| a transaction's commit timestamp | the coordinator's clock at the end | the timestamp in the **primary's write record**, which is in the log |
| which range holds the primary record | the range id recorded in the lock | the **primary key**, re-routed at resolution time (A4's BUG-011 class) |

### 8.1 The result, with the exclusions stated

**Six facts named before the code. Zero became defects.** A5 named four and got zero; A6 named six and
got zero. The practice has now closed ten facts across two phases without one of them producing a bug,
which is the second phase in a row where the class was closed *before* it could.

What the count does **not** cover, stated because a count is only worth what its exclusions are:

- **BUG-018** is a batch-visibility defect, not a timestamp-position one. A step could not see the
  steps above it in the same `Ready`. No entry in the table above would have caught it, and pretending
  otherwise would make the ten look like ten out of ten.
- **BUG-019** is an addressing defect — *the* lock rather than *my* lock — and it has no timestamp in
  it at all.
- **BUG-020** is the harness's.
- Row three moved after the fact: the interval's top is the **ceiling fixed at the transaction's first
  snapshot**, not `read_ts + maxOffset` recomputed per read (§15.1). The row was right about where the
  fact comes from and wrong about how long it lives, and the correction came from running it rather
  than from reading it. That is one of six **amended**, not one of six failed, and it is recorded as
  such rather than quietly rewritten.
- Row five was **wrong in the code and right in the table**: §15.2 found `commitTS = startTS.Next()`,
  which is not "the timestamp in the primary's write record" but a value derived from the start. The
  table said the right thing and the implementation did not follow it. That is the one place where
  naming the fact in advance did not prevent the defect — it only made it one line to describe once a
  checker pointed at it.

So the honest form of the claim: **six named, four held exactly, one amended by experience, and one
that the table got right and the code got wrong.** Not six for six.

---

## 9. The oracles

Four new, and each names what it is forbidden from reading.

**`transaction-atomicity`** — for every transaction the harness observed issuing writes, either every
key's write record exists at one commit timestamp, or none does. Reads the committed logs of every
range and the harness's own record of which keys a transaction touched. Never asks a coordinator what
it thinks it did.

**`snapshot-isolation`** — judged over **harness-observed client operations** and nothing else. Two
properties, and §15 explains why they ended up being these two rather than the one first written here:

1. **a snapshot is stable** — the same key read at the same timestamp answers the same thing, forever.
   This is what catches a transaction committing into its own past, which no care in the write path
   prevents (§15.2). It is only non-vacuous because the workload deliberately re-asks: audits run a
   **second pass** over every account at their own timestamp, since the same `(key, timestamp)` pair
   is otherwise almost never asked twice and the property would run over an empty set.
2. **a read only blocks on a lock it could have seen** — a read at `T` reports a lock only if that
   lock's transaction began at or below `T`, and that lock names a primary.

Deliberately *not* asserted here: that a read sees every transaction committed below its timestamp.
The harness knows which transactions it issued but not which committed — that is read from the logs
by `transaction-atomicity`, and importing it would put the two oracles one derivation apart.

**`uncertainty-envelope`** — every uncertainty ceiling is inside the bound the **PLAN** advertises,
every restart names a commit strictly inside its interval, and every restart timestamp is strictly
above the commit that caused it. The bound is plan-derived because a checker that took it from the
node it is checking would agree with any bound that node had drifted to, and the arithmetic is written
out in the oracle rather than borrowed from `kv` for the same reason.

**`bank-conservation`** — total balance over every whole-bank snapshot read is the starting total.
Client-observed history only (§7).

**Both recovery directions are asserted, not merely available.** The exit criteria require roll
forward and roll back to be *exercised*, so both are counted and both counts are asserted nonzero —
the printed-count rule from A4, which A5's §12 gaps show is now standard.

---

## 10. What A6 does not do

- **Parallel commits** — STRETCH (Amendment A6). Percolator's extra round trip is the measured cost.
- **Per-node observed timestamps** — STRETCH.
- **Read index and leases** — A7 and STRETCH respectively.

---

## 11. The escape hatch, surfaced rather than discovered

CLAUDE.md Amendment A6: *"The timestamp source lands behind an interface in A5; TSO fallback is
pre-authorized if A6's uncertainty machinery is not green by Dec 1."*

A5 landed the interface **and exercised it** (`store.TestATimestampSourceCanBeSwapped`), so the
fallback is a construction change rather than a project. The report will state the position on the
uncertainty machinery explicitly against that date rather than leaving it to be discovered.

---

## 12. Exit criteria

Ansh's, verbatim.

1. Percolator two-phase commit with primary-lock semantics, lock cleanup by a competing transaction,
   and rollforward and rollback both exercised and both asserted.
2. Snapshot isolation asserted by an oracle over harness-observed client operations, never engine
   state.
3. The bank workload with the invariant that total balance is conserved, checked from client-observed
   history only.
4. Uncertainty-interval reads deriving from the advertised `maxOffset`, fixed at construction and
   cluster-uniform, with the envelope checker consuming plan-derived offsets and never node-reported
   clocks.
5. The A5 escape hatch stated as still available, with the decision point surfaced in the report.
6. Every count the exit run prints asserted or deleted.
7. Every new oracle induced; every bug in BUGS.md with its mutant class; both corpus lanes green;
   power floors and ceilings under A6's shape; the move-racing-churn interleaving attempted; one
   reduced-seed unthrottled GC run.
8. **25,000 seeds** at exit, zero violations, inconclusive explained. Mid-phase iteration stays at
   2,000.

### 12.1 Status against each, at the time of writing

| # | criterion | state |
|---|---|---|
| 1 | 2PC, primary locks, cleanup by a competitor, rollforward and rollback both exercised and asserted | **done** — §15.3's two-step resolution; 890 roll-forwards and 1,038 roll-backs across 200 seeds, both asserted nonzero |
| 2 | SI over harness-observed client operations, never engine state | **done** — §9; stability made non-vacuous by the second pass, and asserted to have compared something |
| 3 | the bank, conservation from client-observed history only | **done** — and it found BUG-019 (§19) |
| 4 | uncertainty from the advertised `maxOffset`, envelope checker on plan-derived offsets | **done** — `uncertainty-envelope` takes the bound from the plan and does its own arithmetic; and §18 fixed the sweep that was not producing any skew for it to bound |
| 5 | the escape hatch stated, decision point surfaced | **done, and restated every report** |
| 6 | every printed count asserted or deleted | **done** |
| 7a | every new oracle induced | **done** |
| 7b | every bug in BUGS.md with its mutant class | **done** — BUG-019 added `M65`/`M66` in the same commit as the fix |
| 7c | both corpus lanes green | **one entry red**: BUG-015, blocked on the mutant power measurement, recorded rather than retired (§16.2) |
| 7d | power floors and ceilings under A6's shape | **owed** — §21.3, needs the solo slot |
| 7e | the move-racing-churn interleaving attempted | **done and reached** — 3 of 9 moves raced, 1 unattributable, 0 violations |
| 7f | one reduced-seed unthrottled GC run | **owed** — §21.3, starved by the exit run, first in the solo slot |
| 8 | 25,000 seeds at exit | **running** — ten contiguous shards, aggregate asserted to cover exactly `[0,25000)` |

Two owed measurements and one red corpus entry. None of them is a thing to
discover at sign-off, which is why they are here rather than in a report only.

---

## 13. Snapshot equivalence judges an independent EXECUTION, not an independent model

Ratified by Ansh on the A6 stop. This section is the record of why a verification
mechanism was replaced, which is worth more than the mechanism.

### 13.1 What was there, and why it stopped working

Since A2, snapshot equivalence has judged the state machine against a model the harness
**reimplements**: given a range's birth state and its committed log, the harness computes what the
state should be, and a snapshot must equal it. The point was precise — *a defect in applying commands
cannot cancel out on both sides of the comparison* — and it was cheap while a command was a put.

A6 makes a command a step of a two-phase commit with locks, write records, transaction records,
expiry, and a resolution whose answer depends on which range holds a primary. A model of that is a
second implementation of Percolator.

**The evidence, and it is the argument.** In one sitting, that model produced five divergences, and
**every one was a defect in the model**:

1. a split-born range's inherited **locks, write records and transaction records** dropped — the model
   read the birth payload as data only;
2. **versions a rollback removes** left in place;
3. **locks and commit records a split gives away** left behind on the parent, when only the data
   versions were moved;
4. **resolution of a primary the range cannot see** — the model decided from an absence that only
   meant "not on this range";
5. the birth payload decoded in the **parent's namespace** rather than the child's.

By BUG-016's own standard — a checker that reports false violations is worse than none — that model
is not fit for this phase.

### 13.2 What replaced it, and what that keeps

`store.ReplayMachine` rebuilds the state from the birth payload and the committed log **in a fresh
engine that has never been anything else**, through the real apply path, with no access to any running
node.

| kept | given up |
|---|---|
| a snapshot taken at the wrong index | — |
| a snapshot that drops a record kind | — |
| an install that loses state | — |
| two replicas of one range diverging | — |
| a state that depends on how entries were *batched* rather than on the log (**BUG-018**) | — |
| — | **an apply path wrong the same way on every replica** |

It paid for itself immediately: BUG-018 was found on its first sweep, and **the removed model could
not have found it** — the model replayed logically, one command at a time, with no notion of a Ready,
so it could not represent the state that produced the bug.

### 13.3 The surrendered property, and the two things that replace it

> **An apply path wrong identically on every replica is not caught by replay equivalence.**

A cluster that mishandles lock expiry the same way everywhere agrees with itself, replays
consistently, and satisfies snapshot isolation and bank conservation for as long as the error stays
symmetric. Client-facing oracles catch **asymmetric** error, and this class is symmetric by
construction. So it is covered by two narrower things instead:

**One: mutant classes.** A mutant per symmetric-apply defect, each perturbing the apply path
identically on all replicas — `M60` a commit that does not clear its lock, `M61` a rollback that leaves
the version, `M62` lock expiry off by one, `M63` resolution reading the wrong primary, `M64` a
secondary committing at its own timestamp. A survivor is the gap made visible, not a test to tune.

**Two: invariants over the recovered state.** Not a reimplementation — assertions about what no
correct final state can look like, whatever produced it (`percolator-invariants`):

1. a key is never both committed and locked for one transaction;
2. a commit record implies a committed transaction record somewhere;
3. at most one version at or below the collection mark, per key;
4. every lock names a primary some range covers;
5. a rolled-back version does not exist.

**The fifth exists because a mutant survived.** M61 was killed by nothing on its first run: symmetric,
so replay equivalence left the same version; invisible to clients, because no commit record points at
it. That is exactly the shape the surrendered property describes, and it was answered with a new
assertion rather than a tuned test.

### 13.4 The gap, in the ledger's form

> **UNCAUGHT BY REPLAY EQUIVALENCE: an apply path wrong identically on every replica.**
>
> It is caught by the symmetric-apply mutant classes above, to the extent those classes are complete —
> which is a claim about a list, not a proof. **The day a defect of this shape reaches BUGS.md without
> a mutant having caught it, this record is wrong and says so**, and the response is a new class here
> rather than a note.
>
> **The claim is being actively tested, and has already been tested once.** `M61` survived its first
> run — the list produced a survivor rather than a green tick — and the survivor was answered with a
> new invariant (§14.5). A list that has caught nothing and a list that has surfaced a hole are
> different kinds of claim, and this one is the second.

---

## 14. BUG-018, the method that found it, and three corrections

### 14.1 The chain, in full

1. **Every transaction step reads the engine before it writes.** A prewrite asks whether the key is
   locked and whether a newer commit exists. A commit reads the lock to learn the primary and the
   start timestamp. A resolve reads the lock *and* the primary's transaction record. This is not
   incidental to Percolator — it is what Percolator *is*: conflict detection at the record level.
2. **The apply loop staged a whole `Ready` into one batch and wrote it once at the end.** That is the
   correct shape for A1 through A5, where a command was a put: puts do not read.
3. **So a step could not see the steps above it in the same batch.** The engine reads a step performed
   returned the state as of the *start* of the `Ready`.
4. **Two prewrites of one key in one `Ready` both succeeded.** Each read no lock, because the other's
   lock was still staged.
5. **The second overwrote the first's lock.** One lock record per key, last writer wins.
6. **Two transactions owned one key and neither knew.** The loser's lock is gone, so nothing resolves
   it and nothing rolls it back; its commit record can land later on a key another transaction has
   already committed. **Atomicity broken silently** — no error, no divergence at the client, until a
   balance is wrong.

### 14.2 The finding

> **The replica's state depended on how many entries happened to arrive together.**

That is not a function of the log. How many entries a `Ready` carries depends on arrival timing,
batching, and how far behind the replica is — none of which is replicated. **That the state *is* a
function of the log is exactly what snapshot equivalence asserts**, which is why the assertion caught
it and nothing else did.

### 14.3 The class: no fault required

It needs **no crash, no partition, no clock skew, no injected fault at all**. It needs load.

This is worth separating from the rest of BUGS.md, where every entry needed an engineered schedule —
a crash at a particular instant, a partition, a lost unsynced write. Those are found *by the
injectors*, and the injectors are aimed. **A defect reachable under ordinary operation is a different
and more serious class**, because nothing has to go wrong for a user to reach it, and because the
harness that finds it is not the fault machinery but the checker looking at something the fault
machinery does not control.

### 14.4 The method: batch-boundary bisection

Worth writing down as a technique. A7's read index and B4's kill-point sweep will both want it.

When a replica's state disagrees with a replay of its own log, print a state digest **after each
`Ready`** on the replica and **after each index** in the replay, and line them up:

```
node 2   through 1..8    matches the replay at every index
node 2   through 16      one jump, and the answers have parted
replay   through 9..16   eight separate values, none equal to the node's one
```

The node's digests are per `Ready`; the replay's are per entry. Where the node's trace **skips
indices**, entries arrived together. If the divergence appears exactly across such a skip, the defect
is in **what a batch does that a sequence does not** — a small, enumerable set: writes staged but not
visible to later reads in the same batch; effects applied out of order within a batch; a flush
boundary in the wrong place.

Its value is that it indicts the **boundary** rather than any of the sixteen entries. Three hours of
reading those entries would never have looked at the boundary, because the boundary is not in the log.

### 14.5 Correction one: M61 survived, and that is the ledger working

`M61` — a rollback that leaves the version behind — was killed by nothing on its first run. Symmetric,
so replay equivalence leaves the same version on every replica; invisible to clients, because no
commit record points at the orphan.

That is precisely the shape §13.4 surrenders, and it happened in the same cycle the surrender was
recorded. **It is the first demonstration that the mutant-class list is a claim being actively tested
rather than an assertion** — the list did not merely sit in the doc as a promise, it produced a
survivor, and the survivor produced a new invariant (`percolator-invariants` #5, *a rolled-back
version does not exist*) rather than a tuned test.

### 14.6 Correction two: invariant 1's first form was an eventual property

The first form was **"a committed transaction leaves no lock behind"**, taken from the ruling's
wording. It fired on **60 of 60 runs**, and the runs were correct.

The property is **eventual**. A transaction commits its primary, then its secondaries, then clears
locks; a run that ends mid-cleanup has a committed transaction with a lock outstanding and has done
nothing wrong. **An eventual property cannot be a per-step or end-of-run assertion.** This is the same
shape as a safety oracle never counting unavailability as a violation: the system being *not yet
finished* is not the system being *wrong*, and a checker that cannot tell them apart reports the
first as the second.

The reform is the instantaneously-true pair, which catches the class the eventual form was aimed at:

- a key is never both **committed and locked for the same transaction** — the lock and the commit
  record of one transaction on one key cannot coexist, at any instant;
- a commit record implies a **committed transaction record** somewhere — no commit without a decision.

Recorded rather than silently fixed, because the wording that produced the error was the decider's,
and a future reader should see the correction and not only the corrected form.

### 14.7 Correction three: the forward-only cursor changed what the checker could see

Snapshot equivalence rebuilt the state from the birth payload on every snapshot, which is quadratic in
the log. Replacing it with a resumable cursor was worth 2.8 s/seed — and introduced a defect.

**A snapshot skipped because the range's committed prefix was incomplete becomes checkable later**,
once the prefix fills in. A forward-only cursor has by then advanced past that index, and answered the
shorter prefix with **the state at the later index**. Ten seeds in a hundred.

> **An optimization that changes what a checker can see is a checker change wearing a performance
> change's clothes.**

Fixed by restarting from a fresh replay whenever a request goes backwards: the cursor is an
optimization for the forward case only, and it is never allowed to answer a question it has already
passed.

---

## 15. Five decisions the oracles forced, and one gap they left

Everything in this section was written **after** the oracles ran. §8's discipline — name the fact
before the code — closed four timestamp-position facts with zero defects. These are the ones nothing
predicted, and each was produced by making a checker actually watch something.

### 15.1 D-A6-7: the uncertainty ceiling is fixed at the transaction's first snapshot

A read at `T` restarts on a commit in `(T, T+maxOffset]`, above the observed commit. Restart, and the
new read is at `T' > T` — so what is its window?

If it is `(T', T'+maxOffset]`, **the top moves up by exactly as much as the bottom**. The set of
commits that can restart the transaction again never shrinks, and under steady write traffic the
transaction restarts forever.

So the ceiling is `T₀ + maxOffset`, computed once at the first snapshot and carried unchanged through
every restart. Each restart then strictly reduces the interval, and a transaction can restart at most
as many times as there are commits inside its first window. `kv.UncertaintyCeiling` holds the
argument; `TestTheUncertaintyCeilingDoesNotMoveWhenAReadRestarts` holds the counterfactual.

Measured before it was carried: audits restarted **202 times and completed 13** across 20 seeds.

**And the ceiling must be learned from ANY answer, not only a successful one.** The first version
learned it from a read that returned a value, so a transaction whose first answer was *uncertain*
restarted without one — and the node, seeing no ceiling, computed a fresh window from the new
timestamp. The fix for a moving window was in place and the one path that needed it most walked
around it.

### 15.2 D-A6-8: the commit timestamp is allocated at commit time

It was `startTS.Next()`, which is a transaction **committing into its own past**: a reader whose
snapshot sits between the two sees a write appear below a timestamp it has already read at.

Percolator allocates the commit timestamp after prewrite for exactly this reason. Allocating it here
is also what makes an uncertain commit possible at all — a commit derived from a start timestamp is
never ahead of anybody, so with the old rule **the uncertainty machinery was unreachable by
construction**. It fired 0 times across 20 seeds before, and fires in every sweep since.

### 15.3 D-A6-9: resolution is two commands, because a primary can be on another range

The single `OpResolve` read the lock, read the primary's record, and applied the verdict — all on one
range. When the primary was elsewhere it returned *wait*, forever: **a cross-range lock could never be
cleared by anybody**, and after a split most locks are cross-range.

`OpResolveStatus` runs on the primary's range and decides (reading the record, or *making* the
decision by writing a rollback record when the owner is past its deadline). `OpApplyResolution` runs
on the locked key's range and applies a verdict it carries rather than re-deriving — which is what
lets it work at all, and is the same rule every other A6 command follows.

Measured: completed audits went from **44 in 320 to 235 in 320** on the same seeds.

### 15.4 D-A6-10: the expiry timestamp is not the reader's snapshot

The resolve carried one timestamp, used both as the reader's snapshot and as the value a lock's TTL
was judged against. A lock's deadline is fixed and a snapshot is fixed, so **a resolver that waited
once waits forever**, however long the owner has been dead — and an audit reading a past instant could
never expire anything.

The determinism requirement was never that the two be the same value. It is that the value be
**carried** rather than read from a clock at apply time, and both of them are. `ReadTS` is the
snapshot; `ExpireAt` is chosen at propose time and carried. Measured before separating them: **8977
waits and 9 completed audits** across 20 seeds.

### 15.5 D-A6-11: an audit names its instant, so its uncertainty window is empty

An audit reads every account at one timestamp, deliberately in the recent past. It is not asking
"what is true now" — it names the instant, the way a read `AS OF SYSTEM TIME` does, and the way the
plain workload's snapshot reads have always done.

Applying an uncertainty window to it makes every commit of the following half-second uncertain, so
nearly every audit restarts and each restart is a fresh round of N reads. The interval was being
applied to a question it is not the answer to. **Transfers** exercise uncertainty, because their
snapshots are at now, which is the case it exists for.

### 15.6 The gap: an HLC start timestamp is not a transaction identity

> **UNCAUGHT BY CONSTRUCTION: two transactions can share a `(primary, start timestamp)`.**

Percolator identifies a transaction by its start timestamp, and the transaction record is addressed by
`(primary key, start timestamp)`. That is safe there because **a single TSO issues start timestamps**.

Here every node has its own HLC, and two nodes can mint the identical `(wall, logical)` pair. Two
transactions that also share a primary would then share a record: the second's decision is refused as
already made, and it **silently adopts the first's fate** — committed when it should have aborted, or
the reverse.

It is not hypothetical at one remove: the *harness* routed answers by start timestamp and delivered
one transaction's read to another within the first twenty seeds (BUG-020). The database-side collision
needs the primaries to match as well, which is rarer, and has not been observed.

**So it is asserted rather than assumed.** `IdentityCollisions` counts every transaction whose
`(primary, startTS)` matches an earlier one, and the exit run fails if it is ever nonzero. The day it
fires, the fix is the **identity** — a transaction id in the record key, or the TSO fallback
Amendment A6 pre-authorises — and never the assertion.

### 15.7 What this section is evidence for

Four oracles were wired in one cycle. Between them they produced one database bug (BUG-019), one
harness bug in three parts (BUG-020), four design decisions above that no one had written down, and
one named gap. None of it came from reading the code again.

The reason is stated in §7 and is worth repeating with the numbers attached: **the conservation check
is over client-observed history only.** Not one of these was visible in the engine's state, which was
internally consistent throughout. BUG-019 produced a perfectly coherent database with nine units of
money missing from it.

---

## 16. The corpus, a duplicated lane, and a criterion worth a ruling

A6 changed the workload, which moved every raft trace, and the corpus lane did
exactly what it was built for: sixteen bundles stopped replaying, loudly, in the
commit that moved them. The prescribed remedy is in the lane's own comment —
*regenerate the bundle in the same commit that moved it* — and regenerating them
turned it green again.

### 16.1 The correction, first

I then built `TestEveryBundleStillFindsItsBug` on the belief that nothing checked
whether a regenerated bundle still **reaches** its defect.

**That was wrong.** `scripts/corpus-reproduces.sh` has checked it since A5, is
wired into `make ci`, and has already caught one instance — BUG-002's schedule
stopped exercising M14 entirely, and the bundle was re-recorded at a seed that
does reproduce. The lane's own header says all of this.

Recorded as a process error rather than quietly deleted, because it is the
cheapest kind to avoid and I did not: **before building an instrument, search for
the one that already exists.** Two lanes for one property is worse than one — the
weaker rots behind the stronger and nobody notices which is which.

### 16.2 What the duplicate did establish: the two criteria are not the same

The existing lane requires the mutated replay to **differ from the recording in
some observable way** — a violation, a panic, an error, or a diverged trace. The
one I wrote requires **the finding to come back**: a violation, an inconclusive,
or a panic.

Those are different claims, and the gap between them is measurable. The existing
lane reported six failures on the A6 tree: `BUG-009`, `BUG-014` and `BUG-015`
**STALE** — the mutation changes nothing at all on their schedules — plus
`BUG-018` and `BUG-019` **ROT**, which was my own error recording the mutant as a
bare name where the lane expects a path.

The stricter criterion adds `BUG-003` and `BUG-008`: their mutants still make the
replay diverge, so the existing lane passes them, but the divergence never reaches
a checker.

Four of the five were re-recorded at seeds where the mutant reproduces, which is
what the existing lane's own message prescribes:

| bundle | mutant | new seed |
|---|---|---|
| BUG-003 | M23 gated messages never released | 23 |
| BUG-008 | M26 truncated suffix left in the engine | 7 |
| BUG-009 | M34 append from zero over a snapshot | 13 |
| BUG-014 | M45 apply ignores the extent | 15 |
| BUG-015 | M46 split inherits the appended configuration | **still red — see below** |

**BUG-015 is the one entry still failing, and it is not retired.** Its finding is
neither a violation nor an inconclusive but a **refusal** — `ApplyConfEntry`
declining an illegal transition, with no oracle involved at all — and `M46`'s own
header records the class as FRAGILE at **1 detection in 3,000 seeds**, first at
seed 215, floored at detected-at-all with a ceiling of 900. A 300-seed search
found nothing, which against a 1-in-3,000 class **proves nothing**: it is a
quarter of one expected detection.

So the honest state is *blocked*, not *unreachable*. The seed comes from the
mutant power measurement under A6's shape — which is one of the three owed
measurements and needs the machine to itself (§21.3) — and the lane's own message
has said so all along: *the power lane will name one*. Retiring the entry on a
300-seed search would be retiring a bug for the crime of being rare, which is the
opposite of what a corpus is for.

The strict criterion accepts a refusal alongside a violation, an inconclusive and
a panic. All four are findings: BUG-015's is a refusal, BUG-001's is an
inconclusive, M40's is a panic. A criterion that took only violations would retire
three real entries for not being the shape it expected.

### 16.4 And the criterion was wrong once more, in the other direction

The first tightened version matched *a finding is present*, and the whole corpus
came out **16 of 16 green — including BUG-015, which had been red an hour
earlier.**

It was false. BUG-015's replay produces `inconclusive reproduced: linearizability:
only 6 of 60 operations were decided` — and the trace **MATCHES**. Its schedule is
quiet enough to be unknown-dominated with or without the mutant, so the finding
was in the **recording** too and the mutation had changed nothing at all. A
criterion that asks whether a finding is present cannot tell that from a finding
the mutant caused.

So the criterion is a **difference**: the mutated replay must produce a finding
the recording does not have. `simctl replay` already distinguishes them by name —
*THE REPLAY PRODUCED A VIOLATION THE RECORDING DID NOT* against *violation
reproduced* — and only the difference forms count, plus a panic or a harness
error, neither of which a clean recording has.

**This is the third time this lane's matcher has been wrong**: case-sensitivity,
then a path joined onto itself, then this. The first two reported false failures
and cost an hour each. This one reported a **false pass**, which is the expensive
direction, and the only reason it did not stand is that a bundle I knew to be red
came out green and that was worth two minutes of looking.

> **A green that contradicts something you knew an hour ago is evidence about the
> instrument, not about the system.**

### 16.5 And the prose beside the bundles had drifted too

`BUGS.md`'s **Reproduce (seed)** line is the instruction a stranger follows; the
bundle is what they get. Nothing compared the two. Four entries had drifted by A6
— BUG-004 said 0 and carried 2, BUG-005 said 40 and carried 3, BUG-007 said 12
and carried 1, BUG-015 said 215 and carried 16 — because a bundle is re-recorded
whenever the workload moves under it and the sentence beside it is not.

`make bundle-seeds` compares them, and it is in `ci` and in the workflow. Its
first version demanded one exact phrasing and called **seven correct entries
drifted**, which is a checker enforcing a sentence shape rather than a fact —
the fourth matcher I have got wrong today, and the reason each one is now induced
before it is believed. It matches the seed as a number in whatever wording the
entry uses, and its header states the scope honestly: a drift check, not a
proof, foolable by a line that happens to contain the seed as an index.

That is the smallest claim in the repository and the one a reader tests first.

BUG-003 and BUG-008 were re-recorded even though the loose criterion passed them,
because a seed where the finding returns is strictly better evidence than one
where only the trace moves, and it cost the same search. **Whether the criterion
itself should tighten is still Ansh's**: it is one line in
`scripts/corpus-reproduces.sh`, and tightening it silently would be changing a
ruled-on lane while nobody was looking.

### 16.3 The general form, which survives the correction

> **A lane that verifies an artifact reproduces must say WHAT it reproduces.**

"The run" and "the finding" are different artifacts, and a corpus is evidence for
the second. A5 already knew this — that is why the reproduction lane exists — and
the thing worth carrying forward is that the distinction has to be re-checked
whenever the workload moves, because regeneration re-records the schedule and a
schedule that no longer reaches the defect regenerates just as happily as one that
does.

---

## 17. `simctl` could express two of the three outcomes

Amendment A4 made *inconclusive* a first-class result: a checker reports pass, violation, or
inconclusive, and an inconclusive is never counted as a pass. SOAK.md carries the column, the sweep
census carries the count, and the public claim quotes the rate.

`simctl run` could not say it. `Meta` had a `violation` field and nothing else, so a run whose entire
finding was *the checker could not tell* came out of the tool looking like a clean run — and was
recorded in a bundle as one.

**BUG-001's bundle is exactly that shape.** Its finding is a green linearizability verdict over a
history of unknowns; the fix made that an inconclusive rather than a pass. Reintroduce M21 today and
the run produces no violation at all — it produces an inconclusive, which the bundle could not
record and the tool did not print. Searching for a seed that "reproduces the finding" therefore
reported the class **unreachable in 80 seeds**, when it was reachable in the first one.

`Meta.Inconclusive` and an `INCONCLUSIVE` line close it. The general form is worth keeping, because it
is the same shape as the corpus lane one section up:

> **A tool that cannot express one of a checker's outcomes reports two-thirds of the truth, and the
> third it drops is the one the amendment exists for.**

### 17.1 And the lane that found it was wrong first

`TestEveryBundleStillFindsItsBug` reported **16 of 16 bundles broken** on its first run. Every one was
a false report: the lane matched the lowercase `violation` a normal replay prints, and the message a
*reintroduced* defect produces is `THE REPLAY PRODUCED A VIOLATION THE RECORDING DID NOT` — the
success case, in capitals.

Recorded rather than quietly fixed, because it is BUG-016's rule arriving on the same day the lane
did: *a checker that reports false violations costs more than no checker.* The lane was believed for
about ten minutes, and the only reason it was not believed longer is that one of the sixteen had been
verified by hand an hour earlier and could not be broken.

---

## 18. The sixteenth instance: A6's clock-sensitive phase ran with no clock skew

`RaftGenConfig` carried one line since A1:

```go
cfg.Holds = 0 // A1 Raft has no clock-sensitive logic; holds land with leases
```

It was true when it was written. **A6 made it false and nothing noticed.**

Uncertainty intervals, lock TTL expiry and the HLC envelope are all
clock-sensitive, and the sweep that exercises them injected **no skew at all** —
only free oscillator drift of at most ±200 ppm, which over a fourteen-second run
is under three milliseconds against a five-hundred-millisecond bound.

So A6's headline mechanism was being exercised by **HLC ordering alone**: a commit
timestamp allocated after a read timestamp is above it on one node's monotone
clock, whether or not any two clocks disagree. The 256 uncertainty restarts across
200 seeds were real restarts against no real skew.

### 18.1 What turning it on changed

Two holds at 90% of maxOffset, which is inside the envelope and is what CLAUDE.md
means by *bounded by maxOffset in safety runs*:

| | holds off | holds on |
|---|---|---|
| uncertainty restarts, 20 seeds | 17 | **23** |
| envelope refusals | 0 | **0** |
| violations | 0 | 0 |

The refusal staying at zero is the prediction in §18.2 confirmed, not a
disappointment.

### 18.2 Why the refusal is unreachable inside the envelope, exactly

The HLC refuses a peer whose timestamp is more than `maxOffset` ahead of this
node's physical clock. Every generated hold targets **90%** of the bound and every
one has the **same sign**, so the widest pairwise skew a safety plan can produce
is 90% of `maxOffset`. The refusal is short by a tenth of the bound, by
construction — which is why it has read zero across every sweep since A5, and why
that zero is the envelope holding rather than a mechanism failing to fire.

`TestEnvelopeExperiment` holds a pair at **150%** and the refusal fires: **12,400
refusals across 12 seeds**. Outside the envelope the run still produced zero
safety violations, which is the refusal doing its job — the peer whose clock has
left the assumption is rejected rather than absorbed, so the bound the rest of the
cluster rests on does not ratchet outward. That is the whole argument in
`hlc.ErrBeyondEnvelope`'s comment, now with a number under it.

### 18.2b The rule paid for itself in hours, and here is the chain

The strongest evidence for §18.3's rule is not an argument; it is the elapsed
time.

| | |
|---|---|
| `cfg.Holds = 0` had been in the tree since | **A1** |
| turned on | this cycle |
| BUG-021 surfaced | **within hours**, on a five-seed probe taken to measure something else |

The chain is exact. The sweep had been injecting **no clock skew at all** — only
±200 ppm of free drift, under three milliseconds against a five-hundred
millisecond bound. BUG-021 requires two nodes to mint the identical
`(wall, logical)` pair, which requires their walls to be close *and* their HLCs
to sit at the same logical counter. **That is exactly what a hold arranges**: it
pulls a pair of clocks together and holds them there while traffic keeps both
counters moving.

Before the holds went on, the class was not rare in the sweep. It was
**unreachable** — the same distinction `sim/hunt/floors.go` makes about M34,
where widening the schedule mix "did not make the numbers look better; it made a
real defect reachable".

So the rule's value is not that a stale comment is untidy. It is that a fault mix
which does not reach a phase's own mechanisms converts real defects into absent
ones, and the conversion is invisible from every instrument in the repository —
including a printed count asserted at zero, which is what `EnvelopeRefusals` was
doing correctly and for the wrong reason.

### 18.3 The rule, stated to be re-asked

> **A configuration comment asserting that a mechanism is unnecessary is a claim
> with an expiry date, and it gets re-asked at every phase that adds a
> mechanism.**

`cfg.Holds = 0 // A1 Raft has no clock-sensitive logic` carried its own
justification and the justification expired one phase before anybody read it
again. Nothing in the repository connects *this phase's mechanism is
clock-sensitive* to *this phase's plan generates clock faults*: the fire-count
machinery catches an injector that stopped working, and this injector was never
asked to work.

**This is the second time a stale-but-once-true statement has silently narrowed
the sweep**, which is what makes it a rule rather than an incident. The first is
BUG-010's caller-bug classification — a panic that was the right response to a
condition that could only be a programming error, and the wrong response the
moment the condition became something a peer could cause. Same shape: a
justification that is a fact about the *current* phase, written as if it were a
fact about the code.

So the question is asked at the gate, out loud, in the same place the exit
criteria live: **which mechanisms does this phase add, and does the fault mix
reach them?** A7 adds read index, which is not clock-sensitive; STRETCH's leases
are, and would need this asked again.

The narrowest thing that would have caught it here: A6's exit criteria require
every printed count to be asserted, and `EnvelopeRefusals` was printed and
asserted **at zero** — correctly, and for the wrong reason. The assertion was
reading a bound that held; it could not tell that from a bound nothing had ever
pushed against.

### 18.4 The method: proving a path unreachable before going looking for it

The refusal reads zero. There are two reasons a count can read zero, and they
want opposite responses:

| | response |
|---|---|
| the mechanism never fires because nothing reaches it | **find what reaches it** |
| the mechanism never fires because the bound it guards holds | **prove that, and leave it alone** |

The proof here is arithmetic, not a sweep: every generated hold targets **90%** of
`maxOffset` and every one has the **same sign**, so the widest pairwise skew a
safety plan can produce is 90% of the bound. The refusal needs more than 100%. It
is short by a tenth of `maxOffset`, **by construction rather than by luck**.

That is the **M47 pattern applied a second time**: a lane reporting nothing, where
the right answer was a written argument for why nothing is what it must report,
rather than a change to make something appear.

The path is then reached by a lane that leaves the assumption on purpose — 150% of
the bound — and it fires: **12,400 refusals across 12 seeds, with zero safety
violations**.

> **That is the refusal doing its job rather than the bound ratcheting outward.**

A future reader who finds this zero and reaches for `maxOffset` to make the count
move needs to meet that sentence. Widening `maxOffset` would make the refusal
fire by making the *envelope* looser — the peer with the jumped clock would be
accepted, its timestamp absorbed into every other node's HLC, and every bound
that rests on the envelope would quietly follow it outward. The count would go up
and the guarantee would go away. The mechanism is supposed to read zero inside
the envelope; that is what "the envelope holds" looks like from the inside.

---

## 19. Why the bank exists: structural invariants bound the shape, conservation laws bound the content

BUG-019's aliasing is the smaller half of the finding. The larger half is **what
saw it, and what could not**.

`transaction-atomicity` asks whether a transaction's keys are all at its commit
timestamp or nowhere. An orphaned version is *nowhere*, so it passes.
`percolator-invariants` asks whether any state is internally contradictory. An
orphan contradicts nothing — no lock points at it, no commit record points at
it, nothing anywhere claims it should be visible. It passes too.

**Both are correct.** The state BUG-019 produces is a perfectly well-formed
Percolator database. It simply has nine units of somebody's money missing from
it.

> **Structural invariants bound the SHAPE of the state. Conservation laws bound
> its CONTENT. A system can be perfectly well-formed and still have lost your
> money, and no amount of structural checking will notice, because nothing about
> the shape is wrong.**

That is the standing argument for domain-specific oracles, and it is worth more
here than in a design doc because **it was found by a bug rather than asserted**.
The bank workload was justified in §7 on the grounds that conservation is the
strongest non-circular claim available. It has now justified itself a second way:
it is the only instrument in the repository that could see this class at all.

The generalisation for later phases: every phase that adds a *domain* — ranges,
transactions, secondary indexes, whatever I2 brings — needs at least one oracle
that knows what the domain's numbers are supposed to mean, not merely what shapes
its records may take. A checker written only from the data model can only ever
catch violations of the data model.

### 19.1 And two of A6's three defects needed no fault at all

| bug | fault required |
|---|---|
| BUG-018 | **none** — two steps in one `Ready` |
| BUG-019 | **none** — two transactions contending for one key |
| BUG-020 | none (harness) |

Every entry in BUGS.md before this phase required an engineered schedule: a crash
at a particular instant, a partition, a lost unsynced write. Those are found *by
the injectors*, and the injectors are aimed.

A6's are found under **load**. That is a more serious class — nothing has to go
wrong for a user to reach it — and it says something about where the remaining
risk in this phase lives. The fault machinery is not what found them and is not
what will find the next one; the checkers looking at things the fault machinery
does not control are.

---

## 20. A lane that is not run is not a lane

`tools/provcheck` — the lane that enforces *every input to a verdict that can
come out green must be something the harness witnessed* — was **red across a whole
commit**. It went red when A6's transaction commands added `RecordTxnCommit` with
three loose arguments, and it stayed red until somebody typed `make provenance`.

Fixing the defect it named is not the finding. The finding is that it took a
phase to look.

### 20.1 The audit

| lane | runs on every change? |
|---|---|
| `build` `vet` `fmt-check` `tidy-check` `determinism` `tooling-only` `hatches` | in the workflow |
| `test` `race` `blind` `power` `mutants` `smoke` `corpus` `provenance` | in the workflow |
| **`assertions`** | **in `make ci`, in no workflow job** |
| **`corpus-reproduces`** | **in `make ci`, in no workflow job** |
| `exit-run`, `soak`, `bench` | deliberately not per-change |

And underneath all of it: **the workflow has never executed.** There is no remote.
`make ci` is a list of lanes that runs when a person types it, one lane at a time,
and nothing has ever run the list.

That is what produced three separate findings in one sitting:

1. `provcheck` red across a commit (register instance 17);
2. `make test` **unrunnable since A1** — `go test ./...` with nothing set runs the
   exit sweep at its 10,000-seed default, roughly 26 hours at A6's cost and dead
   on Go's ten-minute timeout long before that (register instance 18);
3. two lanes in `make ci` that the workflow does not contain, so even a remote
   would not have run them.

### 20.2 The second cost of having no remote, recorded as such

The first cost is already in the record: nothing runs on push, so every lane is
run on memory. This is the second, and it is sharper — **the lanes have drifted
from the thing that runs them, and drift is invisible from either side.** Reading
the Makefile tells you `assertions` is in `ci`. Reading the workflow tells you the
jobs it has. Neither tells you they disagree.

Three fixes, all inside the repository, because that is the only place a fix can
live until a remote exists:

- **`make test` is `-short`**, and getting there took two attempts worth
  recording. The first fix bounded the seed count at 200 and it **still timed
  out**. Measured at A6's cost: `TestRaftExitCriteria` alone takes **233 seconds
  at twenty-five seeds**, and `sim/hunt` holds some fifteen covering tests that
  each sweep a range — so **the cost is driven by the number of sweeping tests,
  not by any one bound**, and no value of `RAFT_SEEDS` makes the lane fast.

  CLAUDE.md had the answer already: *Go unit plus race on every push; 500-seed
  smoke on every push; 10k-seed soak nightly.* The every-change lane is unit
  tests. `boundSeeds` now caps every covering search to a handful under `-short`,
  which is honest about what that proves — **that the path runs**, not that it is
  silent over three thousand seeds — and `make covering` runs the full ranges in
  the nightly tier, named rather than lost.

  The same measurement settles a second thing: `TestRaftExitCriteria` **fails** at
  twenty-five seeds, because its assertions are about mechanisms that need
  thousands to fire. A lane bound low enough to be fast would have been a lane
  that failed for being fast.

  And `-short` alone was not enough either — the third attempt. `boundSeeds`
  capped three of `sim/hunt`'s nine seed loops; six others swept their own
  uncapped ranges, and the lane timed out again at 1800s. The fix distinguishes
  two kinds of covering test, per test rather than as a blanket:

  | | treatment | why |
  |---|---|---|
  | asserts **silence** (`…ReportsNothing`) | capped to a handful | fewer seeds, still silent, path still runs |
  | asserts **volume** (*"the guard never fired across the whole range, so this test asserts nothing"*) | skipped | capping turns it into a test that **fails for being fast**, which is worse than not running it: it trains people to ignore the lane |

  Three tests are in the second class — the epoch guard, the pre-vote ablation,
  and leadership transfer — and `make covering` runs them at full range nightly.

  **Result: `make test` passes in 398 seconds**, of which `sim/hunt` is 396, and
  it is CPU-bound rather than contended. That is the first time the every-change
  lane has been runnable since A1.
- **The two missing jobs are in the workflow.**
- **`make lane-coverage`** parses the `ci` target's prerequisites, expands the
  aggregates, and requires each one to appear as a `run: make <lane>` in
  `.github/workflows/ci.yml`. It is itself in `ci`, and it was induced against an
  empty workflow before being believed: 17 lanes checked, 17 missing.

- **`make hooks`** installs a pre-push hook that runs the fast half of the list —
  build, lint, lane-coverage, assertions, provenance, test, corpus — on the
  machine doing the pushing. `race`, `power`, `mutants`, `soak` and the exit run
  stay out of it, because a hook expensive enough to bypass is a hook that gets
  bypassed. `RIFT_SKIP_HOOK=1` overrides it and prints that it did.

None of that substitutes for the remote. The first three make the *list* honest;
the hook makes some of it *run*, which is the part of a remote that can be had
without one. The lanes it leaves out are exactly the ones a remote is for.

---

## 21. The exit run, split, and the three measurements owed a solo slot

### 21.1 Why splitting is sound

Ansh's ruling on the wall-time report: 25,000 seeds may run as contiguous
non-overlapping ranges in separate invocations, aggregated, with the boundaries
recorded so the union is provably the full set. Not a reduced count, not a weaker
workload.

**The argument, stated rather than left implicit.** `MaterializeRaft(seed)`
derives an entire plan from the seed alone, and the plan is the reproduction unit
— a bundle carries it, `simctl replay` re-executes it, and no run depends on
which seeds preceded it in the same process. **So a seed's verdict does not
depend on which invocation ran it.** Splitting by seed range is therefore not an
approximation of the full run; it is the full run, evaluated in a different
order.

**What splitting must not do is lose seeds or double-count them**, and that is
checked rather than assumed. `TestRaftExitAggregate` requires the shard censuses
to sort into a contiguous cover of exactly `[0, TOTAL)`, at one commit, each
shard having completed the range it claims — so "25,000 seeds" is a verified
partition rather than a sum of numbers somebody hoped were disjoint. A gap fails
the lane by name; so does an overlap; so does a shard that reports fewer seeds
than its range.

`AddCensus` folds the shards. It is written out field by field because Terms and
Ranges are maxima, `FirstViolation` is an earliest-of, and everything else is a
total — a distinction reflection would erase. The cost of writing it out is that
a counter added later gets silently left at zero, which is a number that reads
**low**, so `TestAddCensusCoversEveryField` uses reflection for the part it is
good at: every numeric field must move when two censuses are folded, with
non-totals exempted **by name and with a reason**.

**The run's commit is recorded and checked.** `exit-run.sh` refuses a dirty tree —
an exit run at an uncommitted tree names a commit that does not contain what ran
— and stamps every shard census with the commit, which the aggregate requires to
match across all of them. An aggregate across two builds is two experiments
reported as one.

### 21.1b The run that was in flight is a MEASUREMENT, not a gate

The sharded run launched at `90382fc` **is not the exit run**, and this paragraph
has been rewritten twice as that became clearer, which is itself the point.

- It first said "no Go source at all changed since" — false the moment the
  `-short` tiering landed, which touched four `_test.go`.
- It then said "no *non-test* Go source changed" — true when written, and false
  the moment BUG-021's fix landed in `hlc/`, `store/` and `sim/hunt/`.

**The honest statement is the third one: that run executed code carrying
BUG-021.** It is evidence about how often the identity collision occurs across
`[0,25000)` and about per-seed cost, and it is evidence about nothing else. The
real exit run is the one taken after the fix.

Ansh's framing, and the reason it is worth finishing rather than killing: if the
frequency turns out to be **one seed in 25,000**, that number belongs beside
`M34`'s 1-in-3000 as another measurement of what a sweep can and cannot reach —
and a defect at that rate is one a 2,000-seed mid-phase policy would never have
found.

A claim about what a run's result covers is exactly the kind that has to survive
being checked, and this one did not survive twice. The rule the third version
follows: **state the commit, and re-derive the diff at the moment of reporting
rather than at the moment of launching.**

### 21.2 And it is what makes the run finishable

26 CPU-hours in one process is a run that is always still going. The same 26
CPU-hours across ten processes is roughly three hours of wall clock. The seed
count did not move.

### 21.3 The three owed measurements need a solo slot, scheduled

None of these can share a machine with the exit run — they are all seed sweeps,
and ten shards already own every core. They are scheduled after it, in this
order:

| measurement | why it needs the machine to itself | what it decides |
|---|---|---|
| **the unthrottled collector**, reduced seeds | it writes a collection entry per apply, which is the shape A5's throttle replaced; it is the slowest seed for its count in the repository | whether the throttle was hiding anything |
| **mutant power floors and ceilings under A6's shape** | 14,700 seed-runs, ~15 CPU-hours | every floor in `sim/mutants/*.patch`, which are all still measured under A5's cost |
| **the race lane at 50, 100 and 200** | ~20× instrumentation on a 3.75 s/seed workload | whether `RACE_SEEDS` or `RACE_TIMEOUT` is the thing that moves |

The mutant lane's ~15 CPU-hours is recorded as a **scheduling problem to solve
when the remote lands**, not as a reason to cut classes. Amendment A2 requires
kill-time to stay monitored either way, so the answer is a tier — nightly rather
than per-push — and not a shorter list.

**The slot is a command, not a plan.** `make solo` runs the three in order and
prints that nothing else should be running. A schedule that lives only in a
design document is a schedule that gets rediscovered rather than kept, and the
whole reason these three are still owed is that they lost the machine to
something else.

### 21.4 The race measurement has a premise problem, stated before the numbers

CARRY-FORWARD says the race lane "has found real races twice". **That claim has no
record behind it.** Both recorded race-lane failures were clocks rather than races
(DESIGN-A4 §9.5), and DR-29 keeps tooling defects out of BUGS.md — so a race
found in tooling, like the announcement-writer race A0.3's own test caught, would
leave no entry anywhere but a commit message.

So the measurement cannot be *does the lower count still catch the two races*,
because the two races cannot be identified. `scripts/race-curve.sh` measures what
is measurable and says so in its header: wall time at 50, 100 and 200, and
whether any data race is reported at any of them. If the honest answer turns out
to be "zero at all three", then the lane's value rests on its *structural*
argument — that a cross-goroutine reach into node state would be caught — and not
on a detection history, and the seed count should be set from cost rather than
from a power curve that has nothing to curve.

---

## 22. D-A6-12: the start timestamp must be unique, and that is the timestamp source's job **[REPORTED, not assumed]**

BUG-021. Two transactions minted at the same `(wall, logical)` by two nodes,
both writing one key, sharing that key's lock and its data version. §15.6
predicted the class and guarded the wrong pair.

**Reported rather than assumed** because Amendment A6 legislated on the timestamp
source by name — *"the timestamp source lands behind an interface in A5; TSO
fallback is pre-authorized if A6's uncertainty machinery is not green by Dec
1"* — so changing it engages a decision already made and conditioned.

### 22.1 The requirement, stated exactly

Three things are addressed by a transaction's start timestamp:

| addressed by | where |
|---|---|
| the transaction record | `(primary key, startTS)` |
| the lock's owner | `Lock.StartTS` |
| the data version | `EncodeKey(ns, key, startTS)` |

So the requirement is not that `(primary, startTS)` be unique. It is that
**`startTS` be unique cluster-wide**, because two transactions sharing one and
touching one key in common share that key's version slot whatever their
primaries are. Percolator gets this free from a single TSO.

### 22.2 The candidates

**A. Tag the logical counter with the node ordinal.** Reserve the low *k* bits of
`Logical` for the minting node: `Logical = counter<<k | nodeID`. Two nodes cannot
produce the same timestamp at the same wall, monotonicity per node is unchanged,
and the total order is unchanged.

- *for*: uniqueness by construction, no coordination, and it **keeps timestamps
  multi-source** — which is what makes the uncertainty interval mean anything
  (§15.2: with one source, a commit above a read timestamp is one the reader
  provably need not see, and the interval is theatre).
- *against*: touches `hlc.Clock` and its constructor, so every node build site
  changes; `Logical` loses *k* bits of counter; and **derived timestamps stop
  being safe as identities** — `RestartAt = CommitTS.Next()` increments `Logical`
  by one and so carries another node's tag. Restarts would have to *mint*
  (`Update(restartAt)` then `Now()`) rather than derive, which is what
  `hlc.Source.Update` is for and is a two-line change at the call site.

**B. Take the TSO fallback.** One source issues every transaction timestamp; the
`hlc.Source` interface that A5 built for exactly this makes it a construction
change.

- *for*: pre-authorised, smallest diff, uniqueness by monotonicity, and it is
  what Percolator actually does.
- *against*: it retires the multi-source property mid-phase, and with it most of
  what the uncertainty machinery is testing. The condition Amendment A6 attached
  to the fallback — *if A6's uncertainty machinery is not green by Dec 1* — is
  **not met**: the machinery is green and now sweep-exercised. Taking the
  fallback for a different reason than the one it was authorised for is a
  decision, not an application of an existing one.

**C. Give the transaction its own identity.** A transaction id in the record key,
the lock and the version key, independent of time.

- *for*: correct regardless of the clock, and CockroachDB does exactly this.
- *against*: it widens the MVCC key encoding, which A5 fixed and A6 has built a
  phase on; it is the largest of the three by a distance; and the version key
  ordering (`^ts` newest-first) has to keep meaning what it means.

### 22.3 Why B is refused, recorded verbatim

Ansh's ruling, in his words, because the reasoning is the point and paraphrasing
it would soften exactly the part that matters:

> *"the TSO fallback was pre-authorized on the condition that uncertainty
> machinery is not green by Dec 1, and it is green and sweep-exercised, so taking
> it for a different reason is a new decision rather than an application of the
> old one. A pre-authorization consumed for a purpose it was not granted for is
> an escape hatch widening itself, and this project has spent six phases refusing
> to let mechanisms drift like that. Keep the hatch closed and keep restating its
> status line."*

The mechanism-drift shape is the same one this document has caught three times in
other clothes: a justification that was true when written, applied later to a
case it was never about (§18.3); a mechanism declared and never invoked (§20); a
criterion loosened by one word and passing a bundle that proved nothing (§16.4).
An authorisation spent on the wrong purpose is that shape at the level of the
plan.

### 22.4 Recommendation

**A.** It is the only candidate that fixes the defect without giving up the
property the phase exists to test. B is a bigger retreat than the bug warrants
and its authorising condition is not met. C is right and is A7-or-later work.

The two-part shape of A matters and is easy to under-implement: node-tagged
minting **and** minted-rather-than-derived restart timestamps. Without the second
half the same collision returns through `RestartAt`, one level further out —
which is precisely how §15.6's guard came to be watching the wrong pair.

### 22.5 Ratified, and what landed

Ansh ratified **A**, both halves as one decision. What landed:

| half | change | induced by |
|---|---|---|
| node-tagged minting | the low `hlc.IDBits` of `Logical` carry the minting node's ordinal; `Now` and `Update` both route through one `nextTagged` | `TestTwoNodesNeverMintTheSameTimestamp`, `TestAbsorbingAPeerKeepsThisNodesTag` |
| minted, not derived | `store.NowAbove(lb)` folds the bound in with `Update` and then mints; restarts no longer adopt `RestartAt` | `TestARestartTimestampIsMintedNotDerived`, `TestRestartsMintTheirOwnStartTimestamp` |

**`Update` is where the tag is easiest to lose**, and the version this replaced
would have lost it: four cases, three of which set `c.last` to the peer's value
outright. One absorbed message and every timestamp afterwards carries a foreign
tag — so the property would hold in a quiet unit test and fail in a cluster.
That is why `TestAbsorbingAPeerKeepsThisNodesTag` exists separately from the
minting test.

### 22.6 M68 survived, which is the ratification's own warning coming true

The ruling said to induce the second half specifically, *"since a partial
implementation of A looks exactly like a complete one until a restart lands on a
foreign tag."*

It does. `M68` — a restart adopting `RestartAt` instead of minting above it —
was pinned to `TestBUG021`, the schedule that found the bug, and **survived**:
**seed 90004 does not restart.** The mutant passed cleanly on the exact seed the
class was discovered on.

Answered as §13.4 requires, with an assertion rather than a better-chosen seed.
The assertion is the property: **a start timestamp carries the tag of the node
the client asked for it**, counted as `ForeignTagStarts`, with
`TestRestartsMintTheirOwnStartTimestamp` sweeping for it and failing if the sweep
contained no restarts at all — because a test that passes on a workload with no
restarts is how M68 survived the first time.

**Then it survived a second time, for a different reason, and the second reason
is the more instructive.**

The counter was incremented inside `nowAbove` — the minting helper. M68 deletes
the *call* to `nowAbove`. So the mutation removed the guard along with the
behaviour, and `ForeignTagStarts` stayed at zero because nothing was left to
increment it. The lane was green about a defect that was present and executing.

That is §22.7's class one level out. There the guard's **key** was drawn from
what the concept feels owned by rather than from what the structure is addressed
by; here the guard's **placement** was drawn from where the mechanism lives
rather than from where the fact lives. Same mistake, different axis:

> **An assertion belongs where the FACT is observable, not where the MECHANISM
> that produces it lives. A guard inside the code path a defect removes is
> removed by that defect.**

The fact is a property of `t.startTS`. The check now sits wherever `t.startTS` is
assigned — first snapshot and restart, minted or derived — so no single deletion
can take both the behaviour and its witness.

The mutant itself had to be repointed too: it originally deleted the whole
`nowAbove` block, a shape the corrected code cannot express, so it now mutates
the derivation directly. **A mutant that no longer describes a reachable defect
is a mutant that tests nothing**, and repointing it is not the same as tuning it
— the defect it represents is unchanged.

Second surviving mutant of the phase, after `M61`, and the only one to survive
twice. The list has now made a hole visible three times, which is the most that
can honestly be said for a list.

### 22.6b A decision in two halves needs a mutant per half

The two halves of option A were induced **independently**, and that is what makes
it a complete decision rather than a fix with a note attached:

| half | mutant | measured |
|---|---|---|
| node-tagged minting | `M67` | two clocks over one physical clock mint `0.256` twice; killed immediately |
| minted, not derived | `M68` | **7 foreign tags across 10 uncertainty restarts**, against **0 of 10** on the clean tree, same seeds |

The reason this is not bookkeeping: **a partial implementation of option A is
indistinguishable from a complete one until a restart lands on a foreign tag.**
Every timestamp a node mints carries its own ordinal, every test of minting
passes, and the property holds — right up to the first restart. A single mutant
phrased as *option A is implemented* would have been satisfied by the minting
half alone and would have passed on half the fix.

> **A decision that lands in two halves needs a mutant per half. One mutant for
> the decision tests whichever half the schedule happens to reach.**

That generalizes past this fix. A6 has three others of the same shape — the
commit point is *primary record exists* **and** *secondaries follow*; resolution
is *decide on the primary's range* **and** *apply on the key's*; the uncertainty
ceiling is *fixed at the first snapshot* **and** *learned from any answer*. Each
is one decision whose halves fail independently, and each is one mutant short of
being covered the way this one now is. Recorded rather than fixed here, because
the right moment to write a mutant is the moment its blind spot is precise, and
these three are precise now: they go in the same commit as the next change that
touches them.

### 22.7 The guard that read zero, and the class it belongs to

§15.6 predicted this class, guarded it, and **guarded the wrong pair.** The
counter keyed on `(primary, startTS)` on the reasoning that that pair addresses
the transaction record — which it does. It is not what the *version* is addressed
by, and txn 14 and txn 29 have different primaries, so the counter read **zero**
on precisely the seed that had the collision.

Ansh's general form, and it is a class this project had not catalogued:

> **An assertion keyed on a compound identity is only as strong as the narrowest
> thing the identity actually addresses. The key must be derived from what the
> data structure is addressed by, not from what the concept feels like it is
> owned by.**

A transaction *feels* owned by its primary — that is where its record lives and
where every resolution routes. But `EncodeKey(ns, key, startTS)` has no primary
in it, and neither does `Lock.StartTS`. The concept has two fields; the structure
is addressed by one.

**This is the nineteenth entry in the vacuous-green register, and it is a
different shape from the eighteen before it.** Those were mechanisms that ran and
measured nothing, or did not run at all. Here the prediction was *right*, the
guard was *written*, and it was watching a key one field too wide — so it
reported green with full confidence about the one seed it should have caught.

Widened to the start timestamp alone, it reports 1 on seed 90004.

---

## 23. Three files that should never have been in the tree

`raft/raft.go.orig`, `store/node.go.orig`, `store/codec.go.orig` — **committed at
A1, A2 and A3, and still there at A6.** A hundred and thirty-seven kilobytes of
stale duplicate source, including an entire second copy of `raft/raft.go` frozen
at A3.

`patch` writes `<file>.orig` when it applies with fuzz. This project applies
patches constantly — every mutant, every corpus reproduction, every power
measurement — so the litter is produced routinely, and a `git add -A` while one
is applied commits it.

**That process error is already in the record.** A5 caught a mutant patch
committed by `git add -A` and recorded it. The mutant was reverted; the `.orig`
files that the same class of accident had already left behind at A1, A2 and A3
were not, because nobody was looking for them and nothing would have said so:

- Go ignores a non-`.go` extension, so no build ever failed;
- no lane inspected the file list;
- and the contents are *plausible* — a reader opening `raft.go.orig` would find a
  complete-looking Raft implementation that is silently a phase and a half out of
  date.

`make hygiene` fails on any tracked `.orig` or `.rej`, is in `lint` and in the
workflow, and was induced against a planted one before being believed.

The pattern this belongs to is the one §20 is about: **the fix for a process
error is a mechanism, not a resolution.** A5 recorded "do not `git add -A` while
a patch is applied" and the recording changed nothing, because it was advice.
A lane changes it.

---

## 24. The pre-fix measurement, and what it says about §18

The sharded run that was in flight when BUG-021 was found is **not the exit run**
(§21.1b). It executed code carrying the defect, and its value is as a measurement
of how often the class occurs.

**It occurs about once in ninety seeds.**

| | |
|---|---|
| seeds | 25,000, in ten contiguous shards |
| violations | **~1.1% of seeds** |
| wall clock per shard | ~6 hours, ten in parallel |

Ansh's framing when he told me to let it finish: *"If the frequency turns out to
be one seed in 25,000, that number is worth recording beside M34's 1-in-3000: it
is another measurement of what a sweep can and cannot reach, and a defect at that
rate is one a 2,000-seed policy would never have found."*

**The answer inverts the conclusion.** At one in ninety, a 2,000-seed mid-phase
sweep would have found this defect roughly twenty times over. The seed policy was
never the constraint. What kept it hidden for an entire phase was `cfg.Holds = 0`
— the fault mix not reaching the phase's own mechanism (§18).

So the number belongs beside M34's 1-in-3000 for the opposite reason. M34 is the
case where **widening the mix made a rare defect reachable**. This is the case
where a mix that did not reach a mechanism hid a **common** one, and no amount of
seeds would have helped:

> **Seed count buys depth within the schedules a mix can produce. It buys nothing
> at all outside them.** A defect at one in ninety and a defect at zero in
> infinity look identical from inside a sweep that cannot reach either.

### 24.1 What the number does not yet establish

The 30 identity collisions the run counted are from the **old, narrow** assertion
— the `(primary, startTS)` form that §22.7 records as keyed one field too wide,
and which read zero on the very seed that had the collision. So the counter
undercounts by construction and the 223-in-20,000 violations are not yet
*attributed* to BUG-021, only correlated with it.

**The post-fix exit run settles it.** If the violations go to zero, they were this
class. If they do not, what remains is a second finding and A6 does not close.
That is the honest form of the claim until the run lands, and it is why the
attribution is stated as pending rather than asserted.

---

## 25. BUG-023's fix, and the mutant that survives it

`hlc.NewAt` and `Replica.seedClockAtLeast` are the fix, in two floors that are
separately implementable and were given a mutant each per §22.6b:

| floor | mutant | verdict |
|---|---|---|
| the child seeds from the value the split **entry** carries | `M69` | **SURVIVES** |
| every path that **ingests records** raises the clock to their maximum | `M70` | killed |

**M69 survives, and I think the half it removes is redundant.** The invariant is
*no range's clock sits below a version it **holds***. A child that inherits an
empty half holds none, so there is nothing for a low clock to hide; and a child
that inherits versions gets a floor from those versions directly. In every case
where the invariant can be violated, the record-derived floor already binds.

So the question for a ruling is whether `spec.ClockAt` is defence in depth worth
keeping or a mechanism nothing would notice the absence of. **This project's own
standard says the second is not worth keeping**, and I have not removed it
because the ruling that asked for two floors is more recent than my analysis.

**Two failed attempts at killing M69 are worth recording**, because both were the
same mistake and it is the third and fourth time today:

1. `TestBUG023` — the seed the bug was found on. Both mutants survived it, because
   on that schedule the child inherits records *and* carries the entry's value, so
   the two floors are redundant there and removing either changes nothing.
2. `TestASplitChildWithNoRecordsStillInheritsTheClock` — which calls
   `seedClockAtLeast` **inline** rather than through `applySplit`. The mutant
   removes the call site; the test replicates it; the test cannot fail.

That second one is exactly §22.6's class — *a guard inside the code path a defect
removes is removed by that defect* — written down three hours earlier and repeated
anyway. M70 needed the same correction and got it: it now goes through the real
`ingest`.

**And M70's test needed two arithmetic corrections before it could fail at all**:
its timestamps started below the simulated clock, so the floor never bound; then
five seconds above it, so `Update` refused them as beyond `maxOffset` — correctly,
since a parent more than `maxOffset` ahead would itself have been refused. A
hundred milliseconds is the realistic gap and the one that has to work.

---

## 26. What a phase sign-off means

BUG-023 is a defect in A4's split path and A5's per-range clock, found at A6. Both
phases were signed on 10,000 seeds with zero violations. Ansh's ruling was that the
**sign-offs stand and the record is amended rather than retracted**, and the
reasoning generalises past this bug:

> **A verification claim is bounded by the fault mix it ran under. A signed phase
> is signed against a mix, not against the world.**

Those sweeps were not wrong. Nothing in them failed, and no oracle missed what it
was pointed at. What was bounded is what the mix could *reach* — and this project
has said since A0 that a schedule mix is a claim about reachability, not a
configuration detail.

It has now demonstrated that twice, with numbers:

| | narrow mix | wide mix |
|---|---|---|
| **M34** (BUG-009's class) | **0** detections in 3,000 seeds | 1 in 3,000 |
| **BUG-023** | **unreachable** — no clock skew injected at all | ~1 seed in 90 once holds were on |

M34 is the case where widening a mix made a rare defect reachable. BUG-023 is the
sharper one: a **common** defect, one in ninety, invisible for four phases because
the mix could not produce the condition. Seed count buys depth inside the
schedules a mix can produce and nothing outside them.

**A stranger reading BUGS.md should meet this framing before inferring that a
green phase means a correct phase.** It does not. It means the phase survived a
stated mix with stated oracles, and both halves of that sentence are load-bearing.

---

## 27. What else was verified in a regime where clocks could not disagree

Every phase before A6 ran with `cfg.Holds = 0`. This project's headline is clocks.
So the retroactive question is not only about splits: **which mechanisms in the
signed phases behave differently depending on clock state, and does A6's mix now
exercise them?**

Reported as a list, nothing fixed. Anything not exercised is a candidate for
BUG-023's class and gets a targeted lane rather than a hope — the 150% envelope
lane is the model.

| # | mechanism | phase | depends on clocks how | exercised by A6's mix? |
|---|---|---|---|---|
| 1 | **election / heartbeat timeouts** | A1 | ticks, not wall clock — `Tick()` counts, and the loop drives it | **N/A**, and that is by design: `raft/` reads no clock at all |
| 2 | **`syncLatency` and the persist-before-reply window** | A1 | a simulated duration, not a clock reading | N/A |
| 3 | **HLC `Update` and the envelope refusal** | A5 | directly | **yes** — holds at 90%, and the 150% lane reaches the refusal (§18.2) |
| 4 | **`Now()`'s physical-regression path** | A5 | fires when the physical clock reads behind the last issue | **yes** — `PhysicalRegressions` is nonzero under holds; it was near-zero before |
| 5 | **per-range HLC on a split-born range** | A4×A5 | directly — **BUG-023** | **now yes**, and it found the bug |
| 6 | **per-range HLC on a snapshot-built range** | A2×A5 | same shape as 5: a range acquired by snapshot, never applying the split | **partially** — `M70` covers the floor; whether the sweep *reaches* a snapshot-built range whose records outrank its clock is **not established** |
| 7 | **GC mark derivation** (`now - retention`) | A5 | directly: the mark is a clock reading | **yes** — reads refused below the mark are nonzero, but whether skew makes two replicas derive *different* marks is **not checked**; the mark travels in the entry, which should make it position-derived, and that is an argument rather than a test |
| 8 | **lock TTL expiry** | A6 | `ExpireAt` vs `Deadline`, both carried | **yes** — 930 owners declared dead per 200 seeds |
| 9 | **uncertainty interval and its ceiling** | A6 | directly | **yes** — 256 restarts per 200 seeds |
| 10 | **transaction start/commit timestamp allocation** | A6 | directly — **BUG-021** | **yes** — holds are what made the collision reachable |
| 11 | **snapshot-read timestamps in the plain workload** | A5 | a remembered wall reading from a node's timeline | **yes**, but only against range 1's clock; a snapshot read routed to a split-born range is the same shape as 5 and is **not separately checked** |
| 12 | **`clock.Sim` step vs slew realization** | A0.4 | the two are physically different and both are generated | **yes** — the generator alternates, so a corpus contains both |

### 27.1 The three that are not established

Rows **6**, **7** and **11**, and each gets a lane rather than an argument:

- **6 — a snapshot-built range whose records outrank its clock.** `M70` proves the
  floor works when called; nothing proves the sweep *produces* the condition. A
  targeted lane forces a snapshot install into a range whose records were stamped
  by a faster node.
- **7 — two replicas deriving different GC marks under skew.** The mark is
  proposed by the leader and carried in the entry, which should make it
  position-derived like every other A5 fact. That is an argument, and §8's whole
  discipline is that arguments about timestamps get written down and then checked.
- **11 — a snapshot read routed to a split-born range.** The plain workload's
  remembered timestamps come from node 0's timeline; a range born on another node
  with a lower clock is BUG-023's shape at a read that is excluded from the
  linearizability history by construction, so porcupine would never see it.
  `percolator-invariants` #6 now would — but only if the sweep reaches it.

**None of these is a claim that a defect exists.** Each is a claim that the
absence of one has not been demonstrated, which after BUG-023 is a distinction
worth keeping.
