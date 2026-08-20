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

---

## 9. The oracles

Three new, and each names what it is forbidden from reading.

**`transaction-atomicity`** — for every transaction the harness observed issuing writes, either every
key's write record exists at one commit timestamp, or none does. Reads the committed logs of every
range and the harness's own record of which keys a transaction touched. Never asks a coordinator what
it thinks it did.

**`snapshot-isolation`** — no client read observes a partial commit. Judged over
**harness-observed client operations**: for each read at `T`, the set of values returned must be
exactly the set some single committed prefix produces at `T`. Never engine state.

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
