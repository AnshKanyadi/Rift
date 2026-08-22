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

- **`make test` is bounded** — `TEST_SEEDS=200`, `LANE_SEEDS=12`, an explicit
  timeout, and the numbers stated in the Makefile rather than hidden in a skip.
  Full seed coverage was always `soak` and the exit run; the every-change lane
  now matches what it is for.
- **The two missing jobs are in the workflow.**
- **`make lane-coverage`** parses the `ci` target's prerequisites, expands the
  aggregates, and requires each one to appear as a `run: make <lane>` in
  `.github/workflows/ci.yml`. It is itself in `ci`, and it was induced against an
  empty workflow before being believed: 17 lanes checked, 17 missing.

None of that substitutes for the remote. It makes the *list* honest, which is the
part that can be made honest without one.

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
