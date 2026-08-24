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

**And then a seventh fact of exactly this class was never named at all, and it became BUG-022.**

| the derived fact | the wrong place to take it | the right place |
|---|---|---|
| whether somebody has already been answered about this key | *nowhere — the store did not keep it* | the key's **read mark**, the highest timestamp this range has answered a read of it at |

The table asks *where is this fact taken from*. It cannot ask about a fact nothing takes, and the
discipline as practised walks the code looking for derivations. A fact that is **absent** leaves no
line to walk to. BUG-022 is a commit landing below a read already answered, and the reason no row
covers it is that the read mark did not exist to be spelled wrongly.

> **Naming every fact you take is not the same as naming every fact you need. The first is an audit of
> the code; the second is an audit of the argument.**

The corresponding question, which A7's §7 asks in that form: *what does the protocol's correctness
argument assume about the timestamp source, and does this system's source provide it?* Percolator's
does — a single TSO makes a commit timestamp later than every start timestamp issued before it — and
per-node HLCs do not. That assumption is nowhere in Percolator's own statement of the protocol, which
is why reading it carefully three times did not surface it.

**The final count for A6, with the exclusions stated:**

| | count | |
|---|---|---|
| facts **named** before the code | 6 | four held exactly, one amended by experience (§15.1), one the table got right and the code got wrong (§15.2) |
| facts of this class that became defects **after being named** | 0 | the practice's whole claim, and it held |
| facts of this class that became defects **without being named** | **1** | BUG-022 — a fact nothing derived, so nothing walked to it |
| defects excluded as **not** this class | 4 | BUG-018 (batch visibility), BUG-019 (addressing), BUG-020 (harness), BUG-024 (incarnation) |
| defects excluded as **this class in a different dimension** | 1 | BUG-023 — a clock below the records it holds. A timestamp taken from the wrong *source* rather than the wrong *position*, and DESIGN-A5's table has no column for a source |

So: **six named and zero of them became defects; one of this class was missed entirely; and the miss
is the one that cost the phase.** The practice is worth keeping and it is not a proof.

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
> **Corrected at A6's close, and the correction is the entry now.** §34.3 first put a number on this
> — `M62`, `M63` and `M66` at 0 of 300 — and the number was wrong in all three directions. `M63` was
> not a class at all (§35.2). `M66` is unreached rather than undetected, proved by a byte-identical
> census (§35.3). And `M62` is **reachable and undetected**: 33 census fields move, `TxnLostToResolver`
> goes 0 → 2, and no oracle and no exit criterion speaks (§35.4). The measurement that produced the
> original zeros was itself blind to every aggregate detector, which is the register's twenty-second
> entry (§35.1).
>
> So the honest form of the gap is narrower and harder than the first statement: **for at least one
> class the list is not merely the primary instrument, it is the only one**, and the sweep has been
> measured not to see it.
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
>
> **And the second time, the hole was closed with a DETECTOR rather than with a longer list (§40).**
> `M62` was the sharper case: not a survivor an invariant over the final state could answer, but a
> class measured to be reachable *and* invisible to every client-facing oracle by construction. The
> answer is `resolution-only-breaks-expired-locks`, which reads the permission a resolver claimed out
> of the committed log and checks it against the decision in the recovered state. **For this class the
> list is no longer the only instrument**, which is the narrower and harder form of the gap being
> repaid rather than restated.
>
> That is what the burden the list inherited looks like when it is discharged: the list surfaces the
> hole, the hole gets a mechanism, and the record says which classes still have nothing but the list.
> Of the symmetric-apply classes, `M61` has invariant 5, `M60` has invariant 1, `M62` now has this
> oracle, `M63` was never a class and `M66` is unreached — which leaves **`M64` as the one covered by
> a mutant alone**, and that is a shorter and more useful sentence than the original claim was.

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

## 25. BUG-023's fix, and the mutant that had to be deleted

One floor, in `ingest`: **every path by which records arrive raises the range's
clock to the maximum timestamp among them.** A split's birth state, an installed
snapshot, a restart's recovery all pass through it, so a path added later cannot
bypass the invariant. `M70` removes it and is killed.

It started as two floors — the second being a value carried in the split entry —
and `M69` removed that one. **`M69` could not be killed, and the right response
was to delete the code rather than to find a better test.** The invariant is *no
range's clock sits below a version it **holds***. A child inheriting an empty half
holds none, so a low clock hides nothing; a child inheriting versions gets its
floor from those versions. In every case where the invariant can be violated, the
record floor already binds. `spec.ClockAt` was a mechanism whose absence nothing
could notice.

### 25.1b The third meaning has now happened twice, which changes what it is

`M69` was the first: the split path's own clock floor was unreachable, so the mutant that removed it
could not be killed, and the code went with the mutant.

**`M63` is the second, and it is better established.** It mutated
`s.Txn(l.Primary, …)` into `s.Txn(key, …)` inside `ResolveLock` — read the record of the *locked key*
rather than of the *primary the lock names*. Three facts settle it:

1. `ResolveLock`'s `key` parameter was **never read** anywhere in the function.
2. Its only production call site is the `OpResolveStatus` apply path, which passes **the same value**
   for `key` and `l.Primary`.
3. It does so **by construction**, not by luck: D-A6-9 splits resolution into two commands precisely
   so the deciding half is addressed to the primary's range. The property `M63` guarded is enforced by
   where the command is routed, one level up, which is stronger than a runtime check inside the
   function.

So the parameter and the mutant went together, and the power measurement's `0 of 300` for `M63` was
never a statement about reachability of a defect — it was a statement about a distinction the code
does not make.

> **Twice is a pattern.** The third meaning is not a curiosity that happened once to a clock floor: a
> mutant that cannot be killed is, often enough to expect it, a mutant aimed at a distinction the code
> has already made structurally. The check is cheap and it is the one this project keeps skipping —
> **before asking why a mutant survives, ask whether its two branches are ever different.**

### 25.1 A surviving mutant has three meanings, not two

Amendment A2's standing rule treats a survivor as a gap in the machinery. This
phase produced all three kinds, and only one of them is answered by writing a
better checker:

| the mutant survives because | what it is a finding about | correct response |
|---|---|---|
| **no checker can see the change** | the machinery | add the assertion (`M61` → invariant 5) |
| **the test goes around the changed path** | the machinery | route the test through it (`M68`, `M70`) |
| **the code it removes cannot be reached** | **the code** | **delete the code and the mutant** (`M69`) |

The third is the one worth adding to the record, because its response is the
opposite of the other two: a mutant reporting a dead path is not a coverage gap
to close, it is a defence that was never defending anything, and closing the
"gap" would mean writing a test to protect code that does nothing.

### 25.2 The four failures that were about the test, not the code

`M68` survived twice and `M70` once for the same reason, and the reason has a
name now: **the test called the guarded function inline rather than through the
path the mutant patches.** The mutation deletes a call site; the test replicates
that call; the test cannot fail. §22.6 states that in its own words three hours
before two of the four happened.

`M70`'s test then needed **two arithmetic corrections before it could fail at
all**: its timestamps started below the simulated clock, so the floor never
bound; then five seconds above it, where `Update` correctly refuses them as
beyond `maxOffset` — since a parent more than `maxOffset` ahead would itself have
been refused, so the case cannot arise in a run. A hundred milliseconds is the
realistic gap and the one that has to work.

**A test that cannot fail for arithmetic reasons is the same family as one that
cannot fail for path reasons**, and the induced-failure discipline caught both.
Every one of the six was caught by the mutant surviving — which means the suite
was the only thing between this repository and dead covering tests, and it only
notices after somebody runs the whole thing.

### 25.3b And the lane cannot be a per-push lane at A6's cost

Measured while re-running it after BUG-022: **16 of 59 mutants in 105 minutes**, and that is with the
explicit timeout in place so nothing is being reported as "did not run". It checks each covering test
by running it *at full seed ranges with coverage instrumentation*, which is the `make covering` tier's
workload once per mutant.

`make mutants` measured worse in the same sitting: its **baseline alone** — the unpatched tree running
every covering test in one invocation — did not finish inside `TEST_TIMEOUT=3600s`, with
`TestLeaderCompletenessOracleReportsNothing` at 34 minutes when the alarm fired. The lane reported
`INVALID` and refused to attribute anything, which is the lane behaving correctly: *a lane has to be
able to fail honestly before its green means anything.*

Both numbers were taken with other work on the machine, so they are upper bounds rather than solo
figures. The conclusion does not depend on the factor: **`mutant-covered` and `mutants` belong in the
nightly tier beside `covering`, not in the per-push lane.** That is the same conclusion CARRY-FORWARD
already reached for `power-mutants` — 14,700 seed-runs, ~15 CPU-hours — from the same cause, which is
that a seed now costs four seconds and every lane that sweeps seeds inherited a budget from when it
cost 0.36.

Amendment A2's requirement is that kill-time stays **monitored**, not that it is monitored on every
push. The tier is the answer and a shorter list of classes is not.

**And a tier still has to finish.** `COVER_JOBS` does for this lane what `POWER_JOBS` does for the
power lane, on the same argument: **whether a test executes a line is a function of the test and the
tree**, so a parallel run and a sequential run reach identical verdicts, and the report is printed in
patch order afterwards so they produce identical *text*. Verified on three mutants and the canary
before being used on sixty. What parallelism costs is per-mutant wall time, which this lane does not
claim and does not print — kill-time lives in `make mutants`, at `POWER_JOBS=1`, or it is not
claimed.

### 25.3c The lane's first complete run, and what it found

`56 checked, 2 skipped, 8 failures`, and the eight are two different things.

**Four verdicts that this section called genuinely mispointed covering tests, and they were not.**
**Corrected at §36**, which is the written case that all four are FALSE POSITIVES of the lane's own
rule, and at §36.4, which is the re-run under the repaired rule. Left standing here rather than
rewritten, because what this section got wrong is the more useful half: the lane produced four
confident findings on its first complete run and the confident part was the defect.

The four, as reported:

| mutant | covering test | line it never executes |
|---|---|---|
| `M15-vacuity-rule-removed` | `TestUnknownDominatedHistoryIsInconclusive` | `sim/oracle.go:279` |
| `M29-truncation-refused-below-the-durable-watermark` | `TestStateMachineSafetyOracleReportsNothing` | `raft/raft.go:2543-2545` |
| `M55-collection-takes-the-version-a-read-still-needs` | `TestMVCCReadCorrectnessOracleReportsNothing` | `kv/store.go:217` |
| `M60-commit-does-not-clear-its-lock` | `TestPercolatorInvariantsReportNothing` | `kv/txn.go:204-205` |

**And the canary was correctly uncovered**, which is what makes the four credible: the lane's one
deliberately mispointed patch reports `canary`, and the lane's own bidirectional check would have
failed if it had become covered.

**What the four claim, exactly.** Not *"this mutant cannot be killed"* — the lane's header is careful
about that, and it is the mutant suite's job. The claim is the necessary condition all four of A6's
dead covering tests violated: **part of what the mutant changes is never executed by the test named
against it.** A patch with several hunks can still be killed through the hunks that are covered, and
the class is then covered for a reason nobody wrote down. Each of the four needs its test routed
through the real call site, and each is a small investigation of its own.

> **And the small investigation is what overturned it.** Doing it for all four found the same shape
> four times — a closing brace, an assertion body, an error branch — none of which any test can
> execute on a tree where the assertion holds and the engine works. The paragraph above is right about
> what the lane *should* claim and wrong about what it *did* claim, and the gap between the two was
> the rule. §36.

**Four ERRORs, which are a budget failure rather than a coverage verdict:**

| mutant | covering test | what happened |
|---|---|---|
| `M19` | `TestLeaderCompletenessOracleReportsNothing` | hit the 3600s timeout |
| `M46` | `TestSplitInheritsTheConfigurationAtItsIndex` | hit the 3600s timeout |
| `M65`, `M66` | **`TestRaftExitCriteria`** | hit the 3600s timeout |

`M65` and `M66` are the sharp one and they are not fixable by raising a number: **their covering test
is the exit run.** At A6's measured 8.4 s/seed, `TestRaftExitCriteria` at its 10,000-seed default is
about **23 hours**. No timeout this lane could choose would let it finish, and `make mutants` has the
same problem from the same cause — its baseline runs every covering test in one invocation, and it
died at 3600s with `TestLeaderCompletenessOracleReportsNothing` at 34 minutes.

> **A covering test that is a phase gate is not a covering test.** It cannot run per push, it cannot
> run per mutant, and naming it means the class is verified only when somebody runs the gate.

`M65` and `M66` are BUG-019's two mutants and they have precise, cheap covering tests available —
`TestARollbackDoesNotStealSomebodyElsesLock` and `TestACommitDoesNotStealSomebodyElsesLock`, both
already in `kv/txn_test.go` and both written for exactly this. Re-pointing them is the fix, and it is
recorded rather than done because re-pointing a covering test changes what the mutant suite asserts,
which is a claim about coverage that should land with its own verification rather than in a batch.

### 25.3 So it is mechanical now: `make mutant-covered`

Restating the rule a fifth time would not have helped. Coverage answers it
without annotation:

> **Run the covering test on the UNMUTATED tree with coverage on, and require the
> lines the patch touches to have executed.**

A test that goes around the path leaves them at zero. It cannot be satisfied by
*claiming* an entry point, because coverage is produced by execution rather than
by assertion — which is what makes it mechanical rather than remembered.

Two details that matter:

- **The lines come from applying the patch, not from reading its `@@` header.**
  Headers go stale as files move and `patch` tolerates that with fuzz; the first
  version of this check read the header and reported a live path dead.
- **It does not check that the test would fail under the mutation.** That stays
  the mutant suite's job. This checks the necessary condition all six failures
  violated: the line has to run at all.

Induced against a reconstruction of the exact mistake — a test calling
`seedClockAtLeast` inline — which it reports `DEAD`. In `make ci` and in the
workflow.

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

---

## 28. BUG-022: the guard nobody wrote, because Percolator did not need it

The phase's last finding, and the one worth the most.

### 28.1 The finding, in five log entries

`bank-conservation` on seed 2521: the audit at `1600000008790243029.0` reads eight accounts and they
sum to **-19**. The whole cause is five entries of range 1's committed log, and the key is `a00`:

```
r1 idx=106  txn-get   a00  at 1600000007480000000.1792  -> "-15@4"    (txn 16)
r1 idx=107  txn-get   a00  at 1600000007750000000.514   -> "-15@4"    (txn 26)
r1 idx=109  prewrite  a00  start 1600000007480000000.1792  "4@16"
r1 idx=111  commit    a00  start ...1792 -> commit 1600000007630000000.3072
r1 idx=112  prewrite  a00  start 1600000007750000000.514   "-20@26"
```

Txn 26 was told `a00 = -15` **at 7.75**. Txn 16 then committed `a00 = 4` **at 7.63** — *below* the
timestamp txn 26 had already been answered at. Txn 26's snapshot therefore acquired a commit after the
fact. It wrote `-20`, computed from `-15`, and txn 16's transfer of 19 units stopped existing.

**No fault of any kind is involved.** Two transactions, one key, two clocks.

### 28.2 Two guards that are each correct and do not compose

`PrewriteInto` had two checks:

- **`ErrKeyIsLocked` covers LOG order.** It refuses a prewrite arriving while somebody else's lock
  stands — here the window `[109, 111)`. Txn 26's prewrite arrived at **112**, one entry late.
- **`ErrWriteConflict` covers TIMESTAMP order.** It refuses a prewrite whose start sits below a commit
  already recorded. Txn 16's commit timestamp is *below* txn 26's start, so there was nothing to
  refuse.

> **The two are total only where log order and timestamp order agree, and nothing in this system makes
> them agree.**

Percolator gets the agreement from its **single TSO**: a commit timestamp is drawn *after* the
prewrite completes, so it is above every start timestamp issued before it, and any read answered
before the prewrite is therefore below the commit. That is a property of the timestamp source. It is
**not stated anywhere in the protocol**, which is exactly why three careful readings of the protocol
did not surface it.

Per-node HLCs do not provide it, and the reason is sharper than "clocks differ". A transaction's
timestamps come from `Node.Now()`, which reads `m.replicas[0].hlc` — **the lowest-numbered range on
that node**. Two nodes holding different range sets therefore mint transaction timestamps from two
clocks that **exchange no messages at all**, because a range's HLC advances only on messages for that
range. They are coupled by physical time alone, and A6's holds put them up to 450 ms apart. Here the
gap was 120 ms and the read landed inside it.

This is §15.6's **transaction identity gap** arriving from the other side. That entry predicted two
nodes minting the *same* timestamp; this is two nodes minting timestamps in the *wrong order*, from
one cause. The counter §15.6 installed, `IdentityCollisions`, reads **zero** on seed 2521 and is right
to: nothing collided.

### 28.3 The fix: a reader is a first committer

First-committer-wins was checked against **writers only**. The correction is one sentence:

> **A reader that has already been answered from above my snapshot is as much a first committer as a
> writer is, because my commit lands above my start and can still land below their read.**

Two halves, independently implementable, and therefore two mutants:

1. **The mark is recorded** (`M71`). A fifth record kind — `r <key> <^read_ts>` — holding the highest
   timestamp this range has been asked for this key at. Staged by `applyTxnTo`'s `OpTxnGet` case,
   which is the apply path the driver and the replay **share**, so the mark is a function of the log
   on both sides rather than a fact one of them remembers.
2. **The mark is enforced** (`M72`). `PrewriteInto` refuses when the mark is **strictly** above the
   prewriter's start. Strictly, because a transaction reads its own keys at its own start timestamp:
   `LessEq` would refuse every prewrite in the system. That is not a detail — it is the difference
   between a guard and a deadlock, and it has its own case in the covering test.

**Why the three now compose, as an argument rather than a hope.** After the guard,
`readMark(key) <= startTS < commitTS`. So no read *before* the prewrite was answered at or above the
commit timestamp; and a read *after* the prewrite either sits at or above `startTS` and blocks on the
lock, or sits below `startTS` and so below `commitTS`. It rests on `startTS != commitTS`, which holds
because both are minted and two mints never collide — the node tag separates nodes, the logical
counter separates mints on one node, and `IdentityCollisions` asserts the cross-node half at zero on
every exit run. **BUG-021's fix is load-bearing for BUG-022's argument**, which is not a coincidence:
both are consequences of replacing a TSO with per-node clocks.

### 28.4 What it cost, measured rather than asserted

The obvious worry is that a guard which refuses prewrites refuses too many. Measured over 200 seeds
against the two pre-fix 25,000-seed runs:

| | pre-fix (`90382fc`) | pre-fix (`8e10220`) | post-fix (200 seeds) |
|---|---|---|---|
| commit rate | 0.615 | 0.624 | **0.611** |
| `WriteConflicts` per 200 seeds | 364 | 356 | 2,120 |
| `PrewriteBlocked` per 200 seeds | 1,784 | 1,791 | **392** |
| audits completing | 0.766 | 0.783 | 0.786 |
| violations per 200 seeds | 2.17 | 1.47 | **0** |

The refusals are almost entirely transactions that were losing anyway by a slower route: the refusal
moved **earlier**, from "met a live lock" to "lost to a reader", and the commit rate did not move.
That is the shape a correct first-committer-wins rule should have, and the reason to measure rather
than reason about it is that the opposite shape — a guard that halves throughput — would have been an
argument for the other design.

**The starvation shape, named because the measurement does not rule it out.** A key that is read
continuously at rising timestamps refuses every prewrite whose snapshot is older than the newest read.
In a workload where one key is read far more often than it is written, a writer can lose the race
repeatedly and make no progress — first-committer-wins becoming first-*reader*-wins, indefinitely. The
bank does not produce it: 200 seeds hold the commit rate at 0.611 against a pre-fix 0.615, so nothing
starved here. But *"this workload does not produce it"* is a much weaker statement than *"it cannot
happen"*, and it is the design that removes the shape rather than a bigger sweep: alternative B below
never refuses at all, so it has no starvation shape to have. **If A7's read-volume measurement makes B
the answer, this is a second reason for it.**

**The design that was not taken, and why.** The precise alternative is TiKV's `min_commit_ts`: the
prewrite *reports* the mark, and the coordinator mints the commit timestamp above the maximum reported,
so nothing is ever refused. It is strictly more permissive and it is what a latency-sensitive system
should do. It was rejected for A6 because it moves a safety-critical decision into the client protocol
and adds a field to the wire, where the refusal keeps the whole fix inside the state machine, in the
same function as the two guards it joins, with no protocol change at all. The measurement above is
what makes that a choice rather than a compromise. **If the commit rate had moved, the answer would
have been the other one.**

### 28.4b The audit now participates in first-committer-wins, and that has to be said out loud

The bank's audit is a client: it reads all eight accounts at one instant, and under the fix each of
those reads leaves a mark. So **the checker's own reads can now refuse a transaction's prewrite** —
any transaction whose snapshot predates an audit that touched one of its keys loses the race.

That is uncomfortable enough to state precisely, because it is adjacent to oracle independence.

**What it is not.** It is not the oracle judging with system-asserted facts, and it is not a path by
which a violation escapes detection. `bank-conservation` reads recorded audit results and judges them,
exactly as before. And the damage it detects is a **state** — a sum that has moved — which persists
after the transactions that caused it are long finished; a lost update is produced by two transactions
contending with *each other*, not with an audit, so suppressing contention *at audit time* does not
hide earlier damage.

**What it is.** A power question. Audits are frequent, so a mechanism that refuses transactions
concurrent with an audit reduces transaction-on-transaction contention across the whole run — which
is the condition every A6 defect needed. The honest bound on the effect is the measurement: **the
commit rate did not move** (0.611 against 0.615 and 0.624), and `ReadsBlocked`, `ForeignLocksKept`,
`RollForwards` and `RollBacks` are all in the same range as before. Contention is still there.

**And the alternative is worse, which is what settles it.** Exempting the audit's reads from the mark
would make an audit's own snapshot breakable: a commit could land below the timestamp the audit read
at, so the audit would observe half a transaction and report a conservation failure that never
happened. **An oracle whose reads are not protected by the rule it is checking reports false
violations**, and BUG-016 is this project's record of what that costs. The audit is a client and it is
treated as one.

### 28.5 The fifth record kind cost more than the fix

Three consequences, none of them in the fix's diff:

- **`Records()` promised this.** Its comment says the state machine is *everything under the
  namespace*, enumerated rather than listed, so *"a fifth record kind added later is carried
  automatically"*. It was: `owns()`, `IngestRecordsInto` and `UserKeyOf` are the whole integration,
  and splits, snapshots and restarts moved the mark without further code. **A promise made in a
  comment two phases ago paid out exactly as written**, which is the rarest event in this file.
- **The timestamp is in the KEY, and that is BUG-023's invariant.** `kv.TimestampOf` reads it, so a
  range ingesting a mark raises its clock past it. This is not symmetry for its own sake: a read mark
  is the **one record kind with no companion data version at its timestamp** — a read above every
  write leaves a mark and nothing else — so the argument that excuses locks from that invariant does
  not reach it. `store.TestARangeIngestingAReadMarkRaisesItsClock` covers it separately from the
  version case for that reason.
- **Every raft bundle in the corpus stopped replaying.** A read now stages a write, so every trace
  moved — seventeen bundles at once. §16.3's rule then did what it exists for: regeneration is a
  **search**, not a re-record, and after regenerating, three bundles no longer reached their defects
  at all. `BUG-019` found a replacement at seed 9. `BUG-009` and `BUG-015` did not, and both are now
  blocked on the same instrument, the mutant power measurement. Recorded as blocked rather than
  retired, because retiring a bug for being rare is the opposite of what a corpus is for.

### 28.5b The oracle that should have caught this, designed and NOT built

BUGS.md rule 4 asks what checker was missing. `bank-conservation` found this, and §29.3 is why that is
the weakest possible way to find it: its output is one integer, so it says *something is wrong* and
nothing else. Two unrelated defects sat in one BUGS.md row for a week because of it.

The oracle that would have named it directly is checkable and is worth stating exactly, so that
whether it gets built is a decision rather than an oversight:

> **`read-answers-match-the-history`** — for every read the ledger recorded an ANSWER for, the value
> the client was given must equal the value the final recovered state says was visible at that read's
> timestamp: the version named by the newest non-rollback commit record at or below it.

On seed 2521 it reads: *the read of `a00` at `7750000000.514` was answered `-15@4`, whose version is
committed at `4798954872.0`; the final state holds a commit for `a00` at `7630000000.3072`, which is
above that and at or below the read. The read was answered from a version the history says was
superseded.* That is the defect, in one sentence, with the key and both timestamps.

**What it costs.** `RecoveredVersion` carries `Key` and `At` and not the value, so the comparison
needs the value added — read by the harness's own replay from a fresh engine, which is the same
provenance status the rest of the recovered state already has. The read's answer is already a boundary
observation. Nothing system-asserted enters a verdict that can come out green.

**Three exclusions it must state, or it reports false violations.** Reads at or below the final
collection mark, whose versions may legitimately be gone; reads that were answered `Locked`,
`Uncertain` or `Refused`, which are outcomes rather than answers; and rollback tombstones, which are
commit records that make nothing visible.

**Why it is not in this commit.** The exit run that closes A6 is in flight at the commit this
paragraph is in. An oracle added afterwards is an oracle the exit run did not run with, and *"25,000
seeds clean"* would then be a claim about a different set of checkers than the ones the repository
has. Adding it means re-running the exit run, which is a decision about six hours of machine time and
about what the phase's green means — so it is Ansh's, and it is recorded rather than slipped in.

### 28.6 The read mark is a function of the log **only because A7 has not happened yet**

The whole mechanism rests on one property: **in A6 every read is a log entry**, so every replica
applies it and stages the identical mark, and the mark is derived from the log exactly as the versions
are.

A7 serves reads via **read index**, off the log. A read answered that way stages nothing and no
replica sees it, so the mark stops being maintainable this way the moment the first such read is
answered. This is not a detail to discover at A7's exit: it is a correctness dependency of A6's fix on
A7's design, and DESIGN-A7 carries it as an open question with a recommendation rather than as a note.

The general form is worth keeping, because it will recur at every optimisation that takes work off the
log:

> **A fact maintained by the apply path is a function of the log. The moment an operation is answered
> off the log, every fact that operation used to maintain becomes a fact somebody has to maintain
> somewhere else — and the place it used to live will still compile.**

---

## 29. BUG-024, and the ledger that recorded a timestamp the system had abandoned

### 29.1 The defect

A transaction that finds a commit inside its uncertainty interval restarts: new start timestamp,
re-read everything. The reads it issued *before* the restart are still in flight. Their answers arrive
afterwards carrying the **old** snapshot, and nothing checked which incarnation an answer belonged to,
so they landed in the new snapshot's read set. The transfer then computed its writes from two
different instants, which conserves nothing — in whichever direction the two snapshots happened to
differ. Seed 10303 gained ten units.

It is **BUG-020's family: an answer accepted for the wrong incarnation**, and the phrase is borrowed
from `store`'s epoch guard deliberately, because that guard exists for the identical shape one layer
down — a durability completion from a dead incarnation arriving after a restart. The fix is the same
one: stamp the request with the incarnation, check the stamp on the way back.

### 29.2 The harness defect it was hiding, which is the more useful half

The same investigation had already produced a **confidently wrong finding** from seed 10303: a lost
update between txn 0 and txn 31, established from the ledger's recorded start timestamps. It was
wrong, and the reason is a defect in the ledger:

`RecordTxnBegin` recorded the timestamp a transaction was **first** minted at, and nothing updated it
when the transaction restarted. So the ledger placed txn 0 before a commit it actually followed, and
every inference drawn from that placement was about a transaction that never existed.

The correction written at the time said: *check `Restarts` before reasoning about any two
transactions' relative start times.*

**`TxnRecord.Restarts` was a field nothing ever wrote.** It read zero however many restarts occurred.
The correction pointed the next reader at a number that could only ever confirm the mistake it was
warning about.

> **A field that looks like evidence and is never written is worse than no field: it converts "I do
> not know" into "no".**

That is the **twentieth entry in the vacuous-green register**, and it is a new shape again. Nineteen
was a guard watching a key one field too wide — it ran, and measured the wrong thing. Twenty is a
*correction* that was itself vacuous: the process by which this project protects itself from a repeat
mistake, failing in the same way as the mechanisms it protects.

**The fix is the record, not a stronger warning.** `Ledger.RecordTxnRestart` moves the recorded start
timestamp and counts the restart, and the exit run asserts `LedgerRestarts == UncertaintyRestarts` —
two counts of one fact, kept apart, so the day the recording path stops being called the run says so
instead of quietly describing transactions that never happened. Per DR-29 the harness defect lives in
its fix commit and here; it is named in BUG-024's entry as well because it is why that entry's own
investigation went wrong first.

### 29.3 Two seeds, two causes, one number

Seeds 2521 and 10303 sat in one BUGS.md row for a week as *"the bank still loses and creates money"*,
because `bank-conservation` reports a **number** and a number cannot distinguish mechanisms. They are
two unrelated defects: 2521 is a commit landing below an answered read, 10303 is a transaction mixing
two snapshots. Both directions of the symptom — losing 19, creating 10 — were used as evidence for a
single cause, and both readings were wrong.

> **A conservation law is the strongest thing that can notice, and the weakest thing that can
> attribute. Its output is one integer, and one integer is one bit of attribution: something is
> wrong.**

That is not an argument against the bank — §19 is why the bank is the only instrument that could see
BUG-019 at all. It is an argument for what has to happen next: the committed log for the failing key,
decoded per entry, which is the technique that cracked BUG-018, BUG-019, BUG-023 and now both of
these. §14.4's batch-boundary bisection and this are the same instrument at two resolutions, and both
belong in A7's toolkit before A7 needs them.

---

## 30. The phase, summarised

### 30.1 Seven defects, and what each needed to be reachable

| bug | what it was | fault required | found by |
|---|---|---|---|
| **BUG-018** | a whole `Ready` staged into one batch, so a step could not see the steps above it | **none** | snapshot equivalence, first sweep after the ReplayMachine swap |
| **BUG-019** | commit and rollback deleted *the* lock rather than *their* lock | **none** | `bank-conservation` — nine units missing |
| **BUG-020** | (harness) a transfer prewrote a balance it never read | **none** | `bank-conservation` — the workload inventing money |
| **BUG-021** | two transactions minted at one start timestamp shared a key's lock and version | clock holds | `transaction-atomicity` |
| **BUG-022** | a commit landed below a read that had already been answered | **none** (clock skew widens the window) | `bank-conservation` — 19 units missing |
| **BUG-023** | a split-born range started with a fresh HLC and stamped below the versions it inherited | clock holds | porcupine, per-key linearizability |
| **BUG-024** | a read answer from a pre-restart incarnation landed in the post-restart snapshot | clock holds (to cause the restart) | `bank-conservation` — ten units created |

**Four of seven needed no injected fault at all.** Every entry in BUGS.md before this phase required
an engineered schedule. That is a different risk profile, and the reason is structural: A6 is the
first phase where two *clients* contend, and contention needs no help from the injectors.

**Three of seven needed clock skew, which the phase's own plan did not generate until §18.** A
clock-sensitive phase ran with `cfg.Holds = 0` for its first half. Turning holds on produced BUG-021,
BUG-023 and BUG-024 within days. §18.3's rule — *ask at every phase gate whether the fault mix covers
the phase's own mechanisms* — was paid for three times over in one phase.

### 30.2 What changed about how this phase is verified

- **Snapshot equivalence judges an independent EXECUTION, not an independent model** (§13). The model
  produced five divergences in one sitting and all five were the model's own defects; the replacement
  found BUG-018 on its first sweep, and the retired model was structurally incapable of finding it.
  The surrendered property — an apply path wrong identically on every replica — is covered by a
  **list** of mutant classes, which is a claim rather than a proof, and it is a claim under active
  test: `M61` survived its first run and was answered with a new invariant rather than a tuned test.
- **`percolator-invariants`** — five assertions about what no correct final state can look like,
  whatever produced it. The fifth exists because a mutant survived.
- **`make mutant-covered`** (§25.3) — a covering test must *execute* the line its mutant changes.
  Built because four covering tests in one day called the guarded function inline rather than through
  the path their mutant patches, so the mutation could not affect them and they passed proving
  nothing. It cannot be satisfied by claiming an entry point, because coverage is produced by
  execution.
- **The vacuous-green register reached twenty**, four of them in this phase: a clock-sensitive phase
  with no clock skew (16), `provcheck` red across a commit (17), `make test` unrunnable since A1 (18),
  a guard keyed one field too wide (19), and a correction whose own evidence field was never written
  (20).

### 30.3 What A6 leaves open, stated as debts rather than as done

- **`BUG-009` and `BUG-015` bundles are red and blocked** on the mutant power measurement under A6's
  shape. Both are 1-to-2-in-3,000 classes; a short search proves nothing against them.
- **The symmetric-apply gap** (§13.4) is covered by a list.
- **Three clock-dependent mechanisms are not established as exercised** (§27.1).
- **Three A6 decisions still have one mutant where they need two** (§22.6b).
- **`modelRecords` in `sim/hunt` is unreferenced.** Found while adding the fifth record kind to the
  model: the function that renders the model's logical state into engine records has no caller, so
  the model's records are never digested against the store's. By §25.1's third meaning it is code that
  cannot be reached, and the response to that is deletion — but deleting it is a change to what the
  harness *could* check, so it is reported rather than done.
- **The move-racing-churn interleaving was not re-enabled.** CARRY-FORWARD puts it on A6's checklist
  as a candidate *"once the schedule mix is being reshaped anyway"*, and what has to be solved first
  is `rebalance-safety`'s attribution — it cannot tell whose removal it is looking at when both
  membership drivers are live, which produced 252 false violations in 300 seeds (BUG-016). It is not
  attempted here for a reason that is about sequencing rather than difficulty: **re-enabling it
  changes the fault mix, which moves every trace, which invalidates the exit run that closes the
  phase.** `MovesRacingChurn` reads 0 across 200 seeds, the bidirectional assertion in
  `TestRaftExitCriteria` still holds, and the record is therefore still correct — which is the
  property that arrangement exists to guarantee: it can be forgotten loudly and not quietly.
- **There is still no remote**, and this phase found **three** lanes that had stopped: `provcheck`
  red across a commit and `make test` unrunnable since A1 (§20), and `power-mutants` red since the day
  `M67` and `M70` were added (§31). Two more lanes were in `make ci` and in no workflow job. Every
  phase that ships without a remote should expect to find another one, and this phase found five.

### 30.4 The sentence this phase is for

> **A signed phase is signed against a fault mix and against a set of oracles, not against the world.**

A6 says it twice, from two directions. §26 says it about BUG-023: A4 and A5 were signed on 10,000
seeds each, and BUG-023 was **unreachable** in both because no clock skew was injected — a defect
occurring in roughly one seed in ninety, invisible for four phases, because the mix could not produce
the condition. §28 says it about BUG-022 from the other side: the *oracle* set was complete and the
*protocol* carried an assumption nobody had written down, so no mix could have helped. Widening the
mix finds what the old mix could not reach. Nothing widens a mix into finding an assumption you did
not know you were making; only reading the argument for what it needs, rather than for what it says,
does that.

---

## 31. `make power-mutants` has been red since M67 and M70 landed

Found while making the lane affordable, not while looking for it, which is the only reason it was
found at all.

**The state.** At `087229a` — the commit the handoff describes as *"tree clean, all fast lanes
green"* — `power-mutants` fails on two classes:

```
DROPPED  M67-minting-drops-the-node-tag       rate 0 of 1, floor 1 (current)
SLOWED   M67-minting-drops-the-node-tag       first detection at seed -1, ceiling 1 (current)
DROPPED  M70-ingest-does-not-seed-the-clock   rate 0 of 1, floor 1 (current)
SLOWED   M70-ingest-does-not-seed-the-clock   first detection at seed -1, ceiling 1 (current)
```

Measured on a worktree at that commit, so this is **not** something BUG-022's fix caused. Both
mutants landed in this phase, with BUG-021's and BUG-023's fixes. The lane went red the day they were
added and stayed red, in `make ci` and in the workflow, through every subsequent report.

**The cause is a category error in the declaration, not a power regression.** Both patches declare

```
# power-seeds: 1
# power-floor: 1
# power-ceiling: 1
# power-measured: ... killed deterministically by its covering test ...
```

and both covering tests are **unit tests** — `TestTwoNodesNeverMintTheSameTimestamp` in `./hlc/`,
`TestARangeIngestingRecordsRaisesItsClock` in `./store/`. `TestPowerProbe` measures **sweep**
detection: it runs `MaterializeRaftWith`/`RunRaftWith` over a seed range and counts the seeds that
notice. A class covered deterministically by a unit test has no sweep detection at seed 0, so a
one-seed sweep floor of one is a claim the probe can never satisfy.

The two numbers were written as *"detection is immediate and the floor is exact rather than
statistical"* — which is true of the unit test and false of the instrument that reads the header.
**The header and the lane were describing two different measurements with one vocabulary.**

**Why this is the vacuous-green class again, and a new shape of it.** The lane's job is to notice
when a class stops being detectable. It has been shouting since it was given two classes it cannot
measure — and the shouting was invisible, because nothing ran it. So the failure mode is not "a lane
reported green while blind" but "**a lane reported red into a room with nobody in it**", which is what
having no remote turns every honest failure into. It is §20's finding again, on a third lane.

**The resolution is not made here, deliberately.** Two different answers, and each needs the
measurement that is already owed:

- **M67 → an explicit opt-out with the reason.** Its defect is two nodes minting one timestamp; the
  pre-fix exit run carried 38 collisions in 25,000 seeds, so sweep detection is order 1 in a thousand
  and a unit test is the correct instrument. `# power: n/a -- ...` is the mechanism the lane provides
  for exactly this, and the class stays covered by `make mutants`.
- **M70 → a real sweep floor.** BUG-023 occurs in roughly one seed in ninety under holds (§26), so
  this class *is* sweep-detectable and should carry a measured floor and ceiling rather than an
  opt-out.

Both changes turn a red lane green, which is the one direction that needs the measurement to land
first rather than the argument. They are taken in the solo slot with the rest of the power
re-measurement, and not before.

**And it is why `POWER_JOBS` exists.** The lane is 14,700 seed-runs, about fifteen CPU-hours run one
at a time, and the reason its numbers are all still A5's is that nobody schedules fifteen hours. The
measurement parallelises **exactly** — a detection count and a first-detecting seed are functions of
the seed and the patch alone, because `MaterializeRaftWith(seed, opt)` derives a whole plan from the
seed — so `POWER_JOBS=N` produces byte-identical output to `POWER_JOBS=1`, verified on two classes
before being used on sixty. What does *not* survive parallelism is Amendment A2's
wall-time-to-detection, and that half is either measured at `POWER_JOBS=1` or not claimed.

---

## 32. The exit run, re-run after BUG-022

At `611d0b9`, 10 contiguous non-overlapping shards, aggregated:

```
aggregate:    10 shards covering [0,25000) at commit 611d0b9
verdicts:     pass=24903 violation=0 inconclusive=97 errors=0
contention:   25000 seeds contended, 0 seeds never elected anybody
```

| | pre-BUG-021 (`90382fc`) | post-BUG-021 (`8e10220`) | post-BUG-022 (`611d0b9`) |
|---|---|---|---|
| seeds | 25,000 | 25,000 | 25,000 |
| **violations** | **271** | **184** | **0** |
| inconclusive | 105 (4.2‰) | 93 (3.7‰) | **97 (3.9‰)** |
| transactions committed | 614,337 | 623,913 | 619,176 |
| commit rate | 0.615 | 0.624 | **0.619** |

**The union is proved rather than asserted.** `TestRaftExitAggregate` requires the shard censuses to
sort into a contiguous, non-overlapping cover of exactly `[0,25000)`, at one commit, each shard having
finished the range it claims. A gap, an overlap, a short shard, or two commits each fail by name. The
boundaries are `[0,2500) [2500,5000) … [22500,25000)`, ten shards of 2,500, and every one reported
2,500 seeds.

**Cost:** 5h47m–5h53m per shard, 8.33–8.47 s/seed, about **58 CPU-hours** wall-clock across ten
processes on eleven cores. That is over twice A6's mid-phase 3.75 s/seed figure, and the read mark is
part of it: every snapshot read now stages a record. **The next phase should measure the per-seed cost
before planning a sweep, not after** — this run was planned against 3.75 and took six hours.

**The inconclusive rate did not move**, which is the number Amendment A4 requires alongside the
violation count: 3.9‰ against 4.2‰ and 3.7‰ on the two pre-fix runs. All 97 are the same cause — an
unknown-dominated history, below the 250-per-mille decided floor — and none is a checker timeout.

**BUG-022's two halves are non-vacuous at scale:** 9,199,798 read marks staged and **226,660**
prewrites refused because somebody had already been answered above their snapshot. The refusal is not
a rare corner; it is the third of first-committer-wins that was missing, firing about nine times per
seed.

**And the identity assertions still read zero:** 0 transactions shared a `(primary, start)` identity,
0 foreign-tag starts, 0 peer timestamps refused for exceeding maxOffset. BUG-021's fix holds across
25,000 seeds, which matters here because BUG-022's safety argument rests on it (§28.3).

---

## 33. The race-lane measurement, and the answer it gives

CARRY-FORWARD asks: *run `sim/hunt` under `-race` at 50, 100 and 200 seeds, report whether what 200
catches is still caught at the lower counts, and bound it at the smallest count that catches
everything 200 catches.* §21.4 already recorded that the first half of that question is unanswerable —
the lane's claimed two historical race findings have no record behind them, and both recorded
race-lane failures were clocks (DESIGN-A4 §9.5).

**The second half now has an answer, and it is neither of the two the question offered.**

| point | budget | result |
|---|---|---|
| `RAFT_SEEDS=50`, `RACE_TIMEOUT=5400s` | the lane's current budget | **did not finish** — `panic: test timed out after 1h30m0s`, with `TestRestartsMintTheirOwnStartTimestamp` alone at **36m20s** |
| data races reported | — | **0** |

> **The lane does not fit its own budget at its smallest point.** So the question "does `RACE_SEEDS`
> move or does `RACE_TIMEOUT` move" has a third answer: **neither is enough on its own.**

**Why, in the one number that explains it.** `TestRestartsMintTheirOwnStartTimestamp` swept 50 seeds
in 36m20s — about **43 s/seed instrumented**, against **8.4 s/seed** uninstrumented on the same
workload. `sim/hunt` holds roughly fifteen tests that each sweep a seed range, so the package at
`RAFT_SEEDS=50` is on the order of **nine hours**, and 200 seeds is four times that. A 90-minute
budget and a 50-seed floor cannot both survive A6's per-seed cost; lowering the seed count to fit
would mean single digits, which is not a seed search.

**What this measurement is not.** It ran while the power measurement had three cores, so the wall
times are **upper bounds**. That direction matters and it is the honest one: a run that had *fitted*
under contention would have proved the budget sufficient, and a run that did not fit proves only that
it did not fit *here*. The per-seed figure is the durable part, and it is a ratio against a
same-machine baseline rather than an absolute.

**And zero data races at 50 seeds.** With the premise problem in §21.4, that leaves the lane resting
on its **structural** argument — that a cross-goroutine reach into node state would be caught — and
not on a detection history. A lane whose value is structural should be sized by what makes the
structure exercised, which is the real-mode driver's tests plus a handful of simulated seeds, and not
by a seed count inherited from when a seed cost 0.36 s.

**The recommendation, which is Ansh's to take or refuse.** Split the lane in two rather than tune one
number:

- **`race` (per push)**: the real-mode driver's tests and the unit tests under `-race`, plus a
  single-digit seed search — minutes, and it exercises every cross-goroutine path the mailbox rule is
  about.
- **`race-soak` (nightly, sharded like the exit run)**: the seed search at whatever count the nightly
  tier can afford.

That keeps the structural claim on every push and puts the expensive half where every other expensive
lane is now going (§25.3b, §31). **What it must not do is quietly become a smaller number**, because
the recorded scope — *"a few hundred simulated seeds answer this lane's question"* — was a ruling, and
replacing it with "eight" because eight fits is trading a scope for a budget without saying so.

---

## 34. The power measurement under A6's shape

`POWER_JOBS=3 sh scripts/power-mutants.sh --measure`, about **6h40m** wall. **42 classes measured, 17
opted out with a reason, 3 that could not be measured at all.** Every floor in the tree before this was
A5's.

### 34.1 The three that could not be measured, and why it is the same cause as everything else

| class | seeds it declares | what happened |
|---|---|---|
| `M46-split-inherits-the-appended-configuration` | 3,000 under `current` | **no measurement** |
| `M19-vote-for-a-shorter-log` | 1,500 under `current` | **no measurement** |
| `M60-commit-does-not-clear-its-lock` | 300 under `current` | **no measurement** |

The probe runs under a **3600s** timeout. At A6's measured 8.4 s/seed, 3,000 seeds is **seven hours**
and 1,500 is three and a half. The declarations were written when a seed cost 0.36 s, where 3,000
seeds was eighteen minutes.

> **The timeout is now the binding constraint on how rare a class this lane is able to floor**, and no
> class that needs a 3,000-seed sweep can be floored at A6's cost inside it.

**`BUG-015` therefore stays blocked, for a new and more precise reason.** It was blocked on *"the power
measurement will name a seed"*; the power measurement ran and **could not measure `M46` at all**. The
options are a raised probe timeout (7+ hours for one class), a sharded probe on the exit run's model,
or accepting that a 1-in-3,000 class cannot carry a bundle at this cost. That is a ruling, and it is
the third time this phase that A6's per-seed cost has turned a lane parameter into a design question.

### 34.2 What held, and the one that matters

**`M34` reproduced its recorded figure exactly**: `2 of 3000 (a2), first=2065`, against a header
reading *"2 of 3000, first at seed 2065, under a2 at commit A5-close"*. A class measured under a named
historical shape gives the same number a phase later, which is what naming the shape was for.

**`M65` measured `2 of 300, first=9`** — and seed 9 is exactly where the independent bundle search
found `BUG-019` reproducing. Two instruments, one seed, neither told about the other.

### 34.3 Three A6 classes have zero sweep detection, and that is the symmetric-apply gap with a number

| class | measured |
|---|---|
| `M62-lock-expiry-off-by-one` | **0 of 300** |
| `M63-resolution-reads-the-wrong-primary` | **0 of 300** |
| `M66-commit-takes-any-lock` | **0 of 300** |

All three are killed by their covering tests — precise unit tests in `kv/` — so the classes are
covered. What is now measured is that **the sweep does not notice them at all**: 900 seeds of the full
A6 workload, with the bank and every oracle running, and not one of the three produced a finding.

That is §13.4's surrendered property with an actual number under it. The replay-equivalence trade gave
up *an apply path wrong identically on every replica*, and these three are exactly that shape: a lock
expiring one tick early, a resolver reading the wrong primary, a commit taking somebody else's lock —
each symmetric, each leaving a self-consistent database. §13.4 says the list of mutant classes is *"a
claim, not a proof"*. **The claim is now measured, and the measurement is that for these three the
list is the only thing standing between the repository and the defect.** A unit test that pins the
mechanism is a real instrument; it is not the sweep, and the record should not read as though it were.

### 34.4 BUG-022's own two classes

| class | measured |
|---|---|
| `M71-a-read-leaves-no-mark` | **1 of 200, first at seed 148** |
| `M72-prewrite-ignores-the-read-mark` | **1 of 200, first at seed 148** |
| `M73-a-read-answer-lands-in-any-incarnation` | **0 of 200** → explicit opt-out with the reason |

**The same seed and the same rate for both halves**, which is what two halves of one guard should look
like: neither half alone changes what the sweep can see, so the sweep sees the same schedule fail
either way. Floored at detected-at-all with the ceiling set to the sweep's own end rather than a
widened number, because widening past the range that was measured would be declaring a value nobody
measured.

`M73` is reachable — seed 10303 is the seed that found it — and its rate is below what 200 seeds can
measure, with the first known detecting seed at 10303. A floor honest enough to assert would need a
24-hour sweep, so it takes the opt-out the lane provides, with the number that justifies it written
down.

### 34.5 And `M67`, `M68` and `M70` measured `0 of 1`, which is §31 confirmed

All three declare a one-seed sweep floor and all three are covered by tests that are not sweeps. The
measurement says what §31 said from the declaration: the header and the lane are describing two
different measurements with one vocabulary. `M67` and `M70`'s resolution is now backed by a number
rather than by an argument — and `M68` joins them, which §31 did not know.

---

## 35. The three classes that measured zero, resolved one at a time

§34.3 reported `M62`, `M63` and `M66` at **0 of 300** and read it as one thing. It was three
different things, and the instrument that produced the number was itself part of the answer.

### 35.1 The instrument was broken, and that is the twenty-second vacuous-green entry

`TestPowerProbe`'s `noticed()` counted a seed as detected on: a materialise error, a run error, an
end-of-run violation, a report verdict, a panic, or no leader. That is **a hand-listed subset of the
harness's detectors.** The exit run asserts a whole list of criteria over the census — forty-six
when this was written, and the number moving is the point rather than a detail — *no snapshot was
ever taken*, *no resolver ever left a live owner alone*, *the inconclusive rate is under thirty per
mille* — and the probe consulted none of them.

> **The power lane could not measure any class whose detector is an aggregate assertion rather than a
> per-seed verdict, and it reported zero for those classes as though zero meant unreachable.**

That is the register's **twenty-second** entry and the first one *inside the power lane* — the
instrument whose whole job is noticing when detection drops was itself not looking everywhere.

**The fix is structural, not a longer list.** `exitCriteriaFailures(census)` is now the single
statement of the criteria; `assertExitCriteria` reports them to a test and the probe evaluates them
over the accumulated sweep. A new criterion is covered by construction. The census the probe
accumulates comes from `CensusOf`, extracted from `SweepRaftWith` for the same reason `AddCensus` was
written out: two places summing one thing is a number that reads low.

The probe now prints `sweepfail=N` and the failing criteria, and a patch may declare
`power-detector: sweep`. **A sweep failure counts only if the unmutated tree at the same seed count
does not have it** — a difference, not a presence, which is §16.4's lesson applied before it could
cost anything.

### 35.2 `M63` was never a class: the distinction does not exist

Settled by reading, not by sweeping. `ResolveLock`'s `key` parameter was **never read**; its only
production caller passes the same value for `key` and `l.Primary`; and it does so **by construction**,
because D-A6-9 splits resolution into two commands so the deciding half is addressed to the primary's
range. §25.1's third meaning, for the second time. Parameter and mutant deleted together.

### 35.3 `M66` is UNREACHED, and the census proves it rather than arguing it

Applied to the tree, `M66` changes **nothing the harness counts**: 40 seeds, and every field of the
census byte-identical to the unmutated run.

The decisive field is `ForeignLocksKept`. It increments exactly when a commit or a rollback finds
somebody else's lock and leaves it alone, `M66` removes that check **from the commit path only**, and
the count is unchanged — so across 40 seeds **`CommitInto` never once met a foreign lock**. The
condition the defect needs is not rare in that sweep; it does not occur.

That is `M47`'s disposition and the envelope refusal's: prove the mix cannot produce the condition,
then build the lane that does. `TestALateCommitDoesNotOrphanTheNextTransactionsVersion` is that lane,
and the interleaving it forces needs no fault beyond a **duplicate delivery**, which the simulator
already models and which a resolver rolling a transaction forward produces on its own: T1 commits, T2
prewrites the key legitimately, and a duplicate of T1's commit takes T2's lock. The lane asserts
invariant 7's predicate over the resulting state and dies under the mutant in **one second**, against
the twenty-three hours its old covering test would have cost.

### 35.4 `M62` is REACHABLE AND UNDETECTED, and that is the finding

The census is the evidence in the other direction: **33 of the census's fields move**, and the one
that names the defect is `TxnLostToResolver` going **0 → 2** — live coordinators losing their
transactions to a resolver that had no right to kill them. The schedule diverges downstream:
snapshots, splits, collections, uncertainty restarts all shift.

And **nothing says anything**. `detected=0 of 300`, `0 of 100`, `sweepfail=0` at 100 seeds. (At 30
seeds the inconclusive-rate criterion did cross its threshold — one inconclusive in thirty is 33 per
mille — which was small-sample noise and not a detection; the 100-seed run is what settles it. A
threshold that fires on sample size is a threshold that will fire on nothing, and it is worth knowing
that this one does that at thirty seeds.)

> **This is §13.4's surrendered property, realised.** A resolver that kills live transactions produces
> only states the correct code can also produce, because aborting a transaction is a legal outcome. So
> every client-facing oracle is blind by construction — not by omission — and the only thing standing
> between the repository and this defect is a unit test in `kv/`.

**The list is therefore not sufficient in the sense §13.4 hoped.** The claim was that the
symmetric-apply classes are covered by a list of mutants *and* that the sweep would eventually see
them; the second half is now measured false for this class.

**The detector it needs, stated so that building it is a decision.**

> **`resolution-only-breaks-expired-locks`** — for every resolve the ledger recorded, if the verdict
> declared the owner dead, the resolver's `ExpireAt` must be strictly above the lock's `Deadline`.

Both values are already carried in the command — they are carried precisely so every replica compares
the same two numbers (D-A6-10) — so the oracle reads two recorded fields and needs no state at all. It
is not built here for the reason invariant 7 was not built as `M66`'s remedy: it lands as its own
decision, with its own induction, and A6 is signed on the checkers it was signed with.

> **BUILT. Ansh's ruling: now, not deferred — "unlike invariant 7 this one is not a remedy in search
> of a class, the class is established and measured."** §40 is the section, and the disposition above
> is what changes: `M62` stops being *reachable and undetected* and becomes *reachable and detected*,
> by an oracle rather than by a unit test in `kv/`.

---

## 36. The written case that `make mutant-covered` is itself wrong

**Reported, not acted on — and then ACCEPTED, and then landed.** CLAUDE.md: *a failing checker means
the code is wrong until proven otherwise; if you believe a checker is itself buggy, stop and make the
written case first.* This is that case, and it contradicted the premise of a ruling, so it was Ansh's
to re-take.

> **Ansh, re-taking it:** *"your case is accepted and my premise is struck. Closing braces attributed
> to no span, a panic message reachable only when safety breaks, and an error return no unit test can
> force are not covering-test defects, and a rule that asks `M29` for a test violating state machine
> safety is a rule that cannot be satisfied on that shape."*

The rule below is landed, with both checks run and reported (§36.4). The rest of this section is left
as it was written, because the argument is the record.

The ruling was: *"M15, M29, M55 and M60 are the defect the lane exists for and get fixed, with the
lane green afterward."* On inspection **none of the four is that defect.** All four are the lane
reporting a false positive, and they are two kinds of it.

### 36.1 The evidence, line by line

Every deleted line of each patch, with whether the covering test executed it:

| mutant | hunk's first line | uncovered | what the uncovered lines are |
|---|---|---|---|
| `M15-vacuity-rule-removed` | `sim/oracle.go:267` **covered** | `279` | `}` — the block's closing brace |
| `M29-truncation-refused-below-the-durable-watermark` | `raft/raft.go:2541` **covered** | `2543-2545` | the panic's message, inside an assertion body |
| `M55-collection-takes-the-version-a-read-still-needs` | `kv/store.go:214` **covered** | `217` | `}` — the block's closing brace |
| `M60-commit-does-not-clear-its-lock` | `kv/txn.go:203` **covered** | `204-205` | `return err` and its `}` — an error branch |

In every case the **mutation's entry point is covered**, every executable line of the mutated region
is covered, and the only uncovered lines are ones that cannot be covered:

- **A closing brace is not a statement.** Go's coverage profile records blocks as
  `file:startLine.col,endLine.col count`, and the block ends at the last statement — so the `}` that
  closes it belongs to no span and reads as uncovered forever. **Every patch that deletes a block
  deletes a closing brace**, so every such patch is a candidate false positive.
- **An assertion body only runs when the assertion fails.** `M29` removes a `panic` that fires when
  state machine safety breaks. On a tree where safety holds, those lines cannot execute, and the
  advice the lane prints — *"route the test through the real call site"* — asks for a covering test
  that breaks the invariant.
- **An error branch only runs when the engine errors.** `M60`'s `return err` comes from an engine read
  that no unit test makes fail.

**Zero of the four is a test going around the path.** The lane's first complete run has, on this
evidence, no true findings.

### 36.2 Independent corroboration, from earlier the same day

Three patches were narrowed by hand this cycle to get past exactly this: `M72` (the uncovered line was
`return err`), and `M65` and `M66` after re-pointing (uncovered lines `return err` and its brace).
Each narrowing was a workaround for the same defect, applied before it was understood. That the same
shape came up four more times unprompted is what makes it a rule rather than four coincidences.

**Ansh, ratifying: the three hand-narrowings are the evidence that the shape is real rather than a
rationalisation of four inconvenient cases**, and they belong in the record for that reason. The point
is the ORDER. The narrowings came *first*, before anyone had named the shape — three separate times
somebody looked at a `DEAD` verdict on a `return err` or a closing brace, decided the verdict was
wrong, and edited the patch to make it go away. A rule inferred from four cases that arrived after the
hypothesis is a rule fitted to its evidence. **Three cases that arrived before it, and were each
worked around rather than argued with, are not.**

Seven instances in one day, of two kinds and in two directions — three worked around by hand, four
reported by the lane — is the whole basis for changing the rule, and it is a stronger basis than the
lane's original sentence ever had.

### 36.3 The proposed rule, and why it is not a weakening

> **Require the FIRST line of each contiguous deleted-or-replaced run to be covered**, rather than
> every line of it.

That is the faithful mechanisation of the lane's own sentence — *"the line has to run at all"* — where
"the line" is the point at which the mutation takes effect. A hunk whose first line never runs is a
hunk the mutation cannot reach, which is precisely the failure the lane was built for: **a test that
calls the guarded function inline never executes the call site, so the hunk's first line is
uncovered.** All four original failures the lane exists for still fail under it, and so does the
canary, which is aimed at a test that covers none of it.

**Two checks before it is believed, neither of them run here:**

1. **Re-induce it.** Reconstruct the original mistake — a test calling `seedClockAtLeast` inline — and
   require the proposed rule to report `DEAD`. The lane's header records that induction; it has to
   survive the rule change or the rule is wrong.
2. **Re-run the full lane under both rules and report every verdict that moves.** Anything that goes
   from DEAD to ok is a claim that needs reading one at a time, not a number.

**What I have not done:** changed the script, changed any of the four covering tests, or marked the
lane green. Tuning four tests to satisfy a rule that asks an assertion body to execute on a passing
tree would be the exact move this project forbids, one level up from the tests.

### 36.4 The two checks, run

**Check 1 — the original induction, re-run.** `store/clockinherit_test.go` was reverted to the
mistake it records in its own comment: `r.seedClockAtLeast(maxVersionTimestamp(...))` inline instead
of `r.ingest(recs, ...)`. The reconstructed test **passes** — it is a covering test that cannot fail
under its mutant — and the lane says so under the new rule:

```
DEAD  M70-ingest-does-not-seed-the-clock  TestARangeIngestingRecordsRaisesItsClock
                                          never executes store/node.go:1328
```

Restored, it reports `ok`. The canary reports `canary — correctly uncovered by
TestLinearizableHistoryPasses`. **The induction the lane's header records survives the rule change.**

**And the four original failures still fail, by proof rather than by re-running them.** All four were
the same mistake on two mutants — `M68` twice and `M70` once, §25.2 — and both patches are
**single-line replacements**: `M68` is `sim/hunt/bank.go:443`, `M70` is `store/node.go:1328`. A
one-line run has one line, so its first line *is* its whole set. **Old required set = new required
set = `{443}` and `{1328}`.** The rule change cannot alter those verdicts in either direction. The
canary is the same shape: `sim/checker/porcupine.go:145`, old = new = `{145}`.

**Check 2 — the full lane under both rules.** Done in two halves, and the first half turned out to
answer most of it.

**(a) The static half, which is a proof and not a sample.** The new required set is *by construction*
a subset of the old, so **no verdict can move from `ok` to `DEAD`** — the rule can only demand less.
And where the two sets are **equal**, no verdict can move at all, whatever the coverage says. Computed
over all 61 patches by applying each one and diffing:

> **48 of 61 patches have identical old and new required sets. Their verdicts are provably unchanged
> and no coverage run can alter that.** Thirteen differ, and those thirteen are the entire space in
> which a verdict can move.

That includes both of the lane's known budget failures: **`M19` and `M46` are IDENTICAL**
(`raft/raft.go:1683`, `store/machine.go:852`), so the two covering tests that cannot finish inside
`TEST_TIMEOUT` contribute nothing to this question — worth stating, because it would otherwise look
like two hours of unexamined lane.

**(b) The measured half: the thirteen that can move.** Run with the covering tests bounded to 25
seeds, which is sound in one direction and that is the direction needed: **coverage is monotone in
seeds** — running more seeds executes a superset of lines — so a line covered at 25 is covered at
500, and `miss` measured here is a **superset** of `miss` at full range. An empty `miss` here is
therefore an empty `miss` there.

| mutant | `miss` under the OLD rule | `miss` under the NEW rule | verdict |
|---|---|---|---|
| `M15-vacuity-rule-removed` | `279` | — | **DEAD → ok** |
| `M55-collection-takes-the-version-a-read-still-needs` | `217` | — | **DEAD → ok** |
| `M60-commit-does-not-clear-its-lock` | `204,205` | — | **DEAD → ok** |
| `M29-truncation-refused-below-the-durable-watermark` | `2543,2544,2545` | **`2543`** | **DEAD → DEAD** |
| `M24`, `M25`, `M30`, `M31`, `M38`, `M44`, `M47`, `M72`, `M73` | — | — | ok → ok, nine of them |

**Three of the four move. `M29` does not, and that is the finding.**

### 36.5 `M29` stays DEAD, and the rule is not what is wrong

The proposed rule was argued from §36.1's table, which lists `M29`'s uncovered lines as
*"the panic's message, inside an assertion body"* and therefore predicted it would clear. It does not,
and the reason is mechanical and was invisible from the table:

```
2541  if r.commitIndex >= i {                                          <- REPLACED, and covered
2542      panic(fmt.Sprintf(                                           <- unchanged
2543          "raft: node %d truncated to %d with commit index %d; ..  <- REPLACED, not covered
2544              "was committed is being overwritten, ...",           <- REPLACED
2545          r.id, i, r.commitIndex))                                 <- REPLACED
```

Line 2542 is **unchanged between the two edits**, so the hunk is **two** contiguous replaced runs, not
one — `{2541}` and `{2543,2544,2545}` — and the new rule asks for the first line of *each*. 2543 is
the first line of the second run and it sits inside the panic's argument list, which only evaluates
when state machine safety has already failed. **So the lane is still asking for a covering test that
breaks the invariant**, which is precisely the thing Ansh's ruling struck.

**The rule is not what is wrong here. The patch is.** `M29`'s class is *truncation refuses below the
DURABLE watermark instead of below the COMMIT index* — the class is the **condition**. Rewriting the
panic's message alongside it is cosmetic, and it is the sole reason the hunk has two runs. So the
patch is narrowed to the condition line and the message is left alone:

```diff
-	if r.commitIndex >= i {
+	if r.tail.persisted >= i {
 		panic(fmt.Sprintf(
 			"raft: node %d truncated to %d with commit index %d; ...
```

One run, first line `2541`, **covered**. The mutation's behaviour is unchanged — the guard still tests
the wrong watermark and still panics on legitimate truncations — and the message it would print is now
stale when it fires, which costs nothing because a mutant that fires is a mutant being killed.

**This is re-pointing, not tuning** (§22.6's distinction): the defect the patch represents is
identical, and what changed is that the patch now describes it without the incidental edit. It landed
with its own verification rather than in a batch, as §25.3c requires, and both halves were run:

```
killed   M29-truncation-refused-below-the-durable-watermark
         by TestStateMachineSafetyOracleReportsNothing   6s

ok       M29-truncation-refused-below-the-durable-watermark
         TestStateMachineSafetyOracleReportsNothing runs it
1 checked, 0 skipped, 0 unchecked, 0 dead, 474s of 9000s budget
```

**So all four of the lane's original DEAD verdicts are resolved — three by the rule and one by the
patch — and none of the four was resolved by touching a covering test**, which was the outcome the
written case argued for and the outcome the ruling struck the old premise to allow.

> **And the general lesson is about mutant patches rather than about the lane: an incidental edit
> inside a hunk splits it into runs, and a run that begins inside an expression begins somewhere no
> test can reach. Keep a mutant's diff to the thing it is a mutant OF.**

### 36.6 The whole lane under the new rule, and the budget it cost to find out

The lane was re-run over **all 61 patches** with the covering tests bounded to 25 seeds — sound in the
direction that matters, because coverage is monotone in seeds and an `ok` at 25 is an `ok` at 500:

```
57 reported ok
canary-mispointed   correctly uncovered (expect: alive), first_miss=145
M23, M48            addition-only: no original line to cover
M14                 ERROR -- see below
```

**Nothing is DEAD.** The lane went from `56 checked, 2 skipped, 8 failures` to no coverage failures
at all, and it got there by fixing the rule and one patch rather than by touching a covering test.

`M14`'s `ERROR` is an artefact of this pass and not a verdict: `TestStaleDurabilityCompletionIsRefused`
carries a **hardcoded `const seeds = 200`** and does not go through `boundSeeds`, so `RAFT_SEEDS` does
not reach it and it blew the 900s cap this pass ran under. Its required set is IDENTICAL under both
rules (`store/node.go:277`), so its verdict is unchanged by construction, and it was not among the
four.

**And the cost, measured rather than assumed.** At `COVER_JOBS=6` and the lane's real per-test
timeout, the unbounded run spent **89 minutes reaching 3 of its 11 batches**, with two of those three
holding a covering test that ran out the entire 3600s. That is the number `COVER_BUDGET` exists to
put a bound on, and the bound is **21,600s** — six hours, against a pathological worst case of eleven.

> **The lane's cost is not the number of mutants. It is that a handful of covering tests sweep 500 or
> 1,500 seeds, at ~5s a seed and twice that under coverage instrumentation. If the budget ever fires,
> the fix is those tests — the same fix `M65` and `M66` got when their covering test turned out to be
> the exit run — and not a larger number.**

---

## 37. Which lanes actually run, and the cheap half of the one that did not

§31 recorded that `power-mutants` had been red since `M67` and `M70` landed. The ruling asked for the
fix **and** for the list: which lanes run automatically, and which run when somebody remembers.

### 37.1 The list

| | lanes |
|---|---|
| **Run automatically** — the pre-push hook, the only executor on this machine | `build` `lint` `lane-coverage` `bundle-seeds` `assertions` `provenance` `test` `corpus` — **8** |
| **Configured to run on push** — `make ci` and `.github/workflows/ci.yml`, which **has never executed** | those 8 plus `race` `blind` `power` `smoke` `mutants` `mutant-covered` `corpus-reproduces` — **15** |
| **Run when somebody remembers** | the 7 in `ci` that the hook does not run, plus `covering` `exit-run` `soak` `nightly` `solo` `race-soak` |

> **Zero lanes run automatically in the sense the workflow means. Eight run because a hook exists.**

**And every mutation-testing lane is in the remembered column.** `power` — which is `power-toy` plus
`power-mutants` — `mutants`, and `mutant-covered` are all outside the hook, because between them they
cost roughly twenty CPU-hours. `power-mutants` went red the day two mutants landed and stayed red for
the back half of a phase, and the reason is not carelessness: **the only thing that executes here does
not run it, and the thing that would costs fifteen hours.**

That is the **third cost of having no remote**, in those words. The first was that every lane runs on
memory. The second (§20.2) was that the lanes and the thing that runs them drift apart invisibly. The
third is that **a lane too expensive for the hook has no executor at all**, so its tier is a label
rather than a schedule.

### 37.2 The fix that fits in the hook: check the declaration, not the measurement

The failure that actually happened was not a detection regression. It was a **declaration nobody could
satisfy**: `power-seeds: 1 / floor: 1 / ceiling: 1` on a class whose covering test is a unit test,
against a probe that measures sweeps.

**The measurement costs fifteen CPU-hours. The declaration costs milliseconds.** So `make power-decl`
checks every patch's declaration for internal consistency without running anything, and it is in the
pre-push hook:

- an opt-out must carry a reason of real length — *"n/a"* alone is not one;
- a class declares an opt-out, or seeds with a floor and a ceiling, or seeds with `power-detector:
  sweep`;
- **`power-seeds` under 30 is not a sweep**, which is the rule that would have caught `M67`, `M68` and
  `M70` on the day they landed;
- `power-measured` must exist and must not say `PENDING`;
- a floor or ceiling above its own sweep length is unsatisfiable or unbreachable.

On its first run it found **six** inconsistent declarations: the three from §31 plus `M60`, `M61` and
`M64` still carrying `PENDING` — floors asserted against measurements nobody had taken.

**This is the twenty-first vacuous-green entry** and the shape is worth naming: *a lane too expensive
to run is a lane whose claims are unchecked, and the cheap invariant over its inputs is worth more
than the expensive measurement nobody schedules.*

### 37.2b A mutant changes the cost of the sweep, so the probe's timeout is sized for the wrong tree

`M60` reported `ERROR -- the probe produced no measurement` twice, at its declared 300 seeds. It is
not a rare class: measured at 60 seeds it is **60 of 60, first at seed 0.** Every seed notices.

What it is, is **expensive**. `M60` leaves every commit's lock standing, so locks pile up, readers
block on them, resolvers churn, and a seed costs about **2.5× the clean tree's**. Three hundred seeds
ran for two hours without finishing, against a probe timeout of 3600s chosen from the clean tree's
per-seed cost.

> **A probe timeout sized on the unmutated tree is not sized for the mutated one, and the classes it
> will time out on are the ones whose defect makes the system do more work — which is a large share of
> what a mutant is.**

The declaration now says 60 seeds, and the reason is in it: a class detected on every seed does not
need three hundred of them. That is the second time this phase that A6's cost has turned a lane
parameter into a design question, and it is a different mechanism from §34.1's — there the seeds were
too many for the budget, here the *seconds per seed* were.

### 37.3 What the classes measured, now that the probe can see them

| class | before | after |
|---|---|---|
| `M67-minting-drops-the-node-tag` | `1 of 1` declared, `0 of 1` measured | **opt-out with the number**: 0 of 60 as a rate, and its sweep detector `IdentityCollisions` fired 38 times in 25,000 pre-fix — 1 in 660 — so an honest floor needs thousands of seeds. A unit test in `./hlc/` is the right instrument |
| `M68-restart-timestamp-derived-not-minted` | `1 of 1` declared, `0 of 60` measured | **`power-detector: sweep`, detected**: 52 foreign-tag starts and 65 stale restarts at 60 seeds |
| `M70-ingest-does-not-seed-the-clock` | `1 of 1` declared, `0 of 1` measured | **a real floor**: 1 of 200, first at seed 55, floored at detected-at-all with a ceiling of 150 |
| `M61`, `M64` | `PENDING` | 232 of 300 and 300 of 300, both first at seed 0 |
| `M60-commit-does-not-clear-its-lock` | `PENDING`, and `ERROR` twice | **60 of 60, first at seed 0** — measured once the seed count came down to one the probe can reach (§37.2b) |

**`M68` is the one that pays for the probe fix.** It could not be measured at all until three counters
the exit run had been collecting — `ForeignTagStarts`, `StaleRestarts`, `StaleIncarnation` — were
asserted rather than merely carried. `ForeignTagStarts` is **BUG-021's second half**: `IdentityCollisions`
watches that two nodes do not mint one timestamp, and nothing watched that a restart mints its own
rather than adopting `RestartAt`. That is §22.6b's two-halves rule in the assertion dimension — the
decision had two halves and the exit run watched one — and all three were safe to add against the
signed run rather than by argument, because its own shard censuses carry `ForeignTagStarts=0`,
`StaleRestarts=0` and `StaleIncarnation=5195` across 25,000 seeds.

---

## 38. A bundle may name a mutant SET

The corpus's arrangement is *the bundle carries the schedule, the mutant carries the defect*, and
nothing in it requires the defect to be atomic. `Meta.Mutant` being a single string was an assumption
that had never been tested, and BUG-021 is the case that tests it: its defect is a **pair**, and a tree
with either half of the fix still refuses the collision the other allows, so no single patch
reintroduces it.

With a single-patch field the options were a bundle that cannot reproduce or no bundle at all, and
BUGS.md rule 2 asks every entry for one. So `--mutant` repeats, `Meta.Mutants` carries the set,
`MutantSet()` reads either shape, and `corpus-reproduces.sh` applies all of them and labels the
verdict `M67+M68`. Every existing bundle's `meta.json` is unchanged, because one patch still writes the
single field.

### 38.1 The correction: BUG-021 did not need a set after all

The mechanism was landed on a premise, and the premise was mine, and it is false.

I wrote — and BUGS.md carried — that no single mutant reintroduces BUG-021, because *"a tree with only
the first still collides on restarts"*. That was an inference from the fix's own prose, not a
measurement. The measurement, from a sharded search over `[0,3200)` with both halves removed:

| | result |
|---|---|
| the pair | **49 detections in 3,200 seeds**, first at seed 69 |
| `M68` alone, on all eight first-detecting seeds | **reproduces on 8 of 8** |
| `M67` alone, on the same eight | **reproduces on 0 of 8** |

The asymmetry has a reason. `M68` makes a restarting transaction adopt a timestamp carrying another
node's tag, and restarts are common — 42 per 40 seeds. `M67` needs two nodes to mint the identical
`(wall, logical)` independently, which the pre-fix exit run saw **38 times in 25,000 seeds**. One is a
near-certainty per sweep and the other is one in six hundred, so the pair's rate is `M68`'s rate and
the set was never load-bearing.

**The bundle still names the pair**, because the pair is the defect's shape and it keeps the lane's set
support exercised by a real entry rather than by nothing, and BUGS.md now records that the pair is not
a reproduction *necessity*. **The mechanism therefore has no entry that requires it**, which by §25.1's
third meaning is a question about the mechanism rather than about the entry — and it is on the record
that way rather than quietly kept, because the alternative is a capability whose justification nobody
can find later.

### 38.2 And the lane's behaviour on HALF a set is induced, not assumed

*No gate counts until its failure has been induced.* The set support's failure mode is *a bundle that
names one of the pair*, so that is what was built: two copies of `BUG-021`'s bundle, identical in
every byte except `meta.mutants`, one naming `M67` and one naming `M68`, run through
`scripts/corpus-reproduces.sh`:

```
STALE    BUG-021-M67only replays IDENTICALLY with M67-minting-drops-the-node-tag.patch applied.
         The mutation changed nothing on this schedule, so the bundle no longer
         carries its finding.
ok       BUG-021-M68only reproduces its FINDING under M68-restart-timestamp-derived-not-minted.patch
 2 bundles checked, 0 skipped, 1 failures
```

**The lane gives a verdict on half a set, and it is the correct verdict in both directions.** Red for
the half that does not reproduce, green for the half that does — which is §38.1's asymmetry
independently reproduced by a different instrument, and it is the induction that makes "the bundle
names a set" a checked property rather than a described one.

Two things this settles that the prose could not. The set is applied as a **conjunction** — every
patch in it, and a patch that fails to apply is `ROT` and fails the bundle — so a set cannot silently
degrade to a subset. And a bundle naming *only* the necessary half would still be green, which is why
the entry names the pair on the grounds of the defect's SHAPE rather than on the grounds of
necessity, and says so.

---

## 39. The race lane, split, with the budgets it was measured at

§33's recommendation, landed.

| lane | what it asks | measured | budget |
|---|---|---|---|
| **`race`** (per push) | *does any cross-goroutine interaction reach node state off the mailbox* (Amendment A1) — every package **except `sim/hunt`**, which is `raft/`, `store/`, `node/`, `kv/`, `sim/` and the real-mode driver | **191 s** | **900 s**, about five times the measurement |
| **`race-soak`** (nightly, sharded) | the seed search: `sim/hunt` under `-race`, 200 seeds across 8 shards | ~43 s/seed instrumented, so ~9 h in one process | the nightly tier, beside `covering` and `soak` |

**What the split gives up, stated rather than absorbed.** The per-push lane no longer instruments the
simulator driver, which is where CARRY-FORWARD says the lane's value is concentrated. A race
introduced there is caught nightly instead of on push. That is a real reduction, and it is the honest
one available: the alternative was a seed count in single digits, which A1's ruling — *a few hundred
simulated seeds answer this lane's question* — does not authorise. **Shrinking a recorded scope to fit
a budget without saying so is the move this file exists to prevent**, so the scope is kept and moved to
a tier that can hold it.

---

## 40. `M62`'s detector, built: `resolution-only-breaks-expired-locks`

§35.4 stated a detector and did not build it, on the reasoning that it should land as its own
decision with its own induction. Ansh's ruling: **build it now** — unlike invariant 7 it is not a
remedy in search of a class, because the class is established and measured.

### 40.1 What it asserts

> **A rolled-back transaction record that nobody proposed must have a resolve behind it carrying
> `Deadline < ExpireAt`.**

A rolled-back record exists for exactly two reasons. Somebody proposed it -- `OpPutTxnRecord`, a
coordinator abandoning its own transaction -- which needs no permission at all. Or a resolver declared
the owner dead, which does: D-A6-5's rule is that **the TTL is expiry, not opinion**, so a resolver
may only make an owner dead once the deadline the owner published has passed the timestamp the
resolver judged it against.

Both numbers are already in the command. They ride there for D-A6-10's reason -- every replica must
compare the same two values rather than each consulting its own clock -- and that is what makes the
oracle possible at all: **the permission a resolver claimed is written down in the log, so it can be
read back and checked against what the resolve did.**

### 40.2 Why it is not the production predicate restated

Production uses the two numbers to **decide**. The oracle reads the **decision** out of the recovered
final state and the **two numbers** out of the committed log, and asks whether anything in the log
authorised what the state shows. No code is shared with `kv.ResolveLock` for a verdict to cancel out
against, and the harness supplier it takes -- `resolutions` -- **decodes and stops**. Deciding in the
supplier which resolve had declared an owner dead would have re-run the rule under test, which is
precisely how the removed model failed (§13.1).

It is also cluster-wide on the log side and per-range on the state side, for a reason A6 has paid for
before: a transaction's primary can be on any range and **a split moves it**, so the command that
authorised a rollback may sit in the parent's log while the record it produced is in the child's
inherited state.

**And the second arm — *"no committed command anywhere accounts for this record"* — is sound only
because the two halves come from one list**, which is worth writing down because the obvious reading
of it is unsound. The ledger does **not** promise a complete committed prefix in general;
`committedPrefix` exists precisely to report when it has not witnessed one, and an oracle asking *"is
this command anywhere"* against a partial log would accuse a correct run, which is BUG-016's lesson.
Here the question is closed: the recovered state is `store.ReplayMachine` over `rl.Base()` and
`rl.Committed()`, and this walk reads `rl.Committed()` for every range, so **the state cannot contain
what the log this walk reads did not produce** — with ancestors covered because ranges are born by
splitting inside the run and the ledger records each one's base.

**What it does not say, and the one place it can be masked.** It does not say a resolve was needed,
or that waiting would have been better, or that the owner was in fact alive. A coordinator that died
the instant before its deadline is legitimately killed and this is silent about it. The only thing
refused is a killing nothing in the log gave permission for.

And it takes **any** authorising resolve as sufficient, not the earliest one. That is not laziness —
it is the only sound reading available without an ordering the log does not give: a correct run may
contain several resolves for one transaction, early ones that waited and a later one that expired it,
and demanding that the *first* be authorised would fire on every ordinary resolution. The cost is a
masking case: a transaction killed unlawfully at `t1` whose log also contains a lawful resolve at
`t2 > deadline` reads as authorised. **It is stated rather than measured** — closing it needs the
creation event, and the record's creation is not observable in the log at all, only its inputs and its
result. The measured rate below is a lower bound for that reason.

### 40.3 Induced first, in milliseconds, because the sweep is the thing under suspicion

`M62`'s whole finding is that no sweep detects it. So establishing that the oracle **speaks** could
not wait on a seed search: `raftcheck/resolveauthority_test.go` builds the log and the final state
directly and asserts seven cases, including the two that decide whether the oracle is worth having:

| case | required |
|---|---|
| deadline 300, resolver judged at 200, record rolled back, nobody proposed | **violation** |
| deadline 300, resolver judged at **exactly** 300 | **violation** -- `ExpireAt <= Deadline` means WAIT, and the boundary has to agree with production or the two differ by one tick in the direction that kills live transactions |
| deadline 300, resolver judged at 301 | silent, and the declaration **counted** |
| three resolves, only the last past the deadline | silent -- one authorised resolve is enough |
| the coordinator proposed its own rollback | silent, and **not** counted as a declaration |
| the transaction COMMITTED | silent -- no permission is needed to commit |
| rolled back with neither a proposal nor any resolve | violation, reported **differently**: a record no command produced is a different defect |

### 40.4 The measurement, old against new

Induced against `M62`, 200 seeds, `current` shape, sharded four ways, with the unmutated tree measured
at the same 200 seeds as the baseline the difference is taken against (§35.1's rule: **a sweep failure
counts only if the unmutated tree at the same seed count does not have it**).

| | before the detector | after |
|---|---|---|
| `M62`, per-seed | **0 of 300**, then **0 of 100** with the sweep detector on | **18 of 200, first at seed 20** |
| `M62`, sweep verdict | `sweepfail=0` | **`SAFETY VIOLATION` in all four shards** — 2, 3, 6 and 7 |
| the clean tree, same 200 seeds | — | **0 of 200, `sweepfail=0`** in all four shards |
| what named it | nothing. `TxnLostToResolver` went 0 → 2 and no checker spoke | `resolution-only-breaks-expired-locks`, naming the transaction, the deadline and the timestamp the resolver judged it at |

The verdict text on the first detecting seed, which is the thing worth reading rather than the count:

```
seed 20  VIOLATION resolution-only-breaks-expired-locks: range 1: the transaction that
started at 1600000007678719459.0 on primary "a01" is ROLLED BACK by a RESOLVER -- nobody
proposed that record -- and every one of the 1 resolves that named it judged an unexpired
lock. The nearest carried expire-at 1600000007744393667.3 against a deadline of
1600000008178719459.0, which is not above it.
```

**434 milliseconds of lock still to run**, and the owner was killed. That is the defect stated in the
units the design doc argues in.

**The declaration.** `power-seeds: 200`, `power-floor: 9`, `power-ceiling: 80` — half the measured
rate and four times the first detecting seed, the same margin `M14` and `M45` carry. `M62` stops
being an opt-out with a finding in it and becomes a floored class.

**And the covering test does not move.** `TestTheDeadlineIsComparedAgainstTheReadTimestamp` in `kv/`
still kills it in one second, which is the right instrument for a class detected at 9 per hundred
seeds; the oracle is what makes the class *visible to a sweep*, which is a different job. Re-verified:
`1 killed, 0 canary alive, 0 mismatched, 0 rotted`.

**Non-vacuity is now an exit criterion, and it was added against a measurement.** `ResolverDeclarations`
reads **64, 67, 67 and 72** across the four clean shards — robustly non-zero — so
`exitCriteriaFailures` refuses a sweep in which the oracle judged nothing. The probe therefore covers
it by construction, which is §35.1's structural fix doing the thing it was built for on the first new
criterion added after it.

### 40.4b And it is a function of the log, which is a debt A7 should be told about

The oracle reads the permission out of the committed log. That is only possible because **a resolve
is a log entry**, and every resolve is a log entry today for the same reason every read was until A7:
because nothing has taken the work off the log yet.

This is D-A7-5's general form in a new place, and it is worth writing down before it costs anything:

> **A fact maintained by the apply path is a function of the log. The moment an operation is answered
> off the log, every fact that operation used to maintain becomes a fact somebody has to maintain
> somewhere else — and the place it used to live will still compile.**

An optimisation that resolved a lock without proposing a command — a leaseholder deciding locally,
say — would leave this oracle reading an empty permission set and reporting silence. **It would not
break; it would stop looking**, and `ResolverDeclarations` going to zero is the only thing that would
say so, which is why that count is an exit criterion rather than a log line. A7 does not do this;
STRETCH's leases are where the shape lives.

### 40.5 Non-vacuity, because a green over nothing is this register's commonest entry

`Declarations()` counts the rolled-back records the oracle attributed to a resolver. A run in which
every rollback was self-proposed exercises none of this, and a sweep of such runs would report a
silence that means only that resolution never fired. It is carried in the census as
`ResolverDeclarations` so the number is visible rather than assumed.

**And the opportunity is rarer than `ResolveWaits` suggests, which is worth stating because it is
what sets the detection rate.** At seed 0 the whole run produces **six** rolled-back decisions and
**five of them are self-proposed** -- coordinators abandoning transactions the cluster refused. Only
one was a resolver's declaration, and it was authorised. An unauthorised kill needs a resolver to
reach a primary while the transaction is genuinely live and genuinely undecided, and that window is
narrow. The rate below is the rate of that window, not a weakness of the oracle.

---

## 41. The retired model's leftovers, swept for rather than noticed

`modelRecords` was found while adding a record kind, reported rather than deleted, and then deleted
on the ruling (§30.3). That is one leftover found by accident, which says nothing about how many
there are. So the question was asked mechanically instead: **for every exported and unexported
identifier in the system packages, is there a caller anywhere in the tree including tests?**

### 41.1 What the sweep found

| identifier | what it was | disposition |
|---|---|---|
| `kv.EncodeLockValue`, `kv.EncodeWriteValue`, `kv.EncodeTxnValue` | **the retired model's own leftovers.** Their doc comment says so in as many words: *"The value codecs, exported for the harness's model."* They existed so `modelRecords` could render the model's logical state into engine records; the model was retired at §13 and `modelRecords` was deleted after it, and these outlived both | **deleted.** The unexported `encodeLock`/`encodeWrite`/`encodeTxn` are the production path and are untouched. The **decoders** stay and are not symmetry: a split-born range inherits records, so `recoveredStates` has to read what the harness did not write |
| `coordinator.resolves` / `Resolves()` | a **duplicate counter**: `c.resolves++` and `c.readerResolves++` are incremented at the same two lines, and only `ReaderResolves()` is ever read | **deleted**, field and accessor |
| `raftcheck.Ledger.Rev()` | an exported accessor for `l.rev`. Every reader of `rev` is `base.stale()`, inside the package. It has had no caller in any commit | **deleted** |
| `store.codec.encodeKV` / `decodeKV` | the serialiser for the state machine **when the state machine was a Go map**. Both halves lost their callers at `e8b258c`, *"A5: MVCC is the replicated state machine"*, and neither has been called by anything since, tests included | **deleted.** It was `store/`'s only use of `internal/sorted`, and that import went with it |
| `raftcheck.rangeLedger.holds` | a durable-coverage helper written at A2. `git log -G` finds the commit that wrote it and the commit that moved it from `Ledger` to `rangeLedger` at A4, and **no commit that ever added or removed a call to it** | **deleted**, with its reasoning kept in prose where it was |
| `store.Replica.TxnRefused()` | a live counter — `txnRefused` increments at four sites — whose accessor nobody reads | **reported, not deleted** — see below |

### 41.2 Five deleted, one reported, and the difference is not caution

The five are the same case in five costumes: **code whose stated purpose no longer exists.** The
encoders say *"for the harness's model"* and the model is retired; `encodeKV` serialises a map state
machine that became MVCC at A5; `coordinator.resolves` duplicates a counter that is read; `Rev()` and
`holds` were never called at all. §25.1's third meaning says the response to code that cannot be
reached is to delete the code, and applying it to four of the five and not the fifth would be applying
a rule where it is comfortable.

**Two things were kept out of the deletions on purpose**, because they are the part worth having:
`encodeKV`'s note on why its key ordering was load-bearing (a snapshot's bytes are compared against an
independent expectation, so a map range would make one state produce different bytes on different
runs), and `holds`'s note on what its snapshot arm assumes (*a snapshot is taken from an applied
prefix, so an index it covers is one this node applied* — sound exactly as far as state machine safety
holds, which is another oracle's verdict). Both survive as comments where the code was. **A deletion
that takes the reasoning with it is how the same thing gets rediscovered.**

**`TxnRefused()` is the one that is reported, and it is the sharp one.** It contradicts a comment
three lines above it: *"The transaction counters. Every one is asserted somewhere in the exit run: a
count nobody asserts on is decoration that looks like evidence."* `TxnRefused` is not asserted
anywhere and is not even carried in the census. Deletion is the **wrong** response — a refusal count
in the apply path is evidence worth having, and the counter is live — so the right one is to carry it
and assert it, which is what §37.3 did for `ForeignTagStarts`, `StaleRestarts` and `StaleIncarnation`.
**An exit criterion is added against a measurement and not by argument** (§21.1b), and no measurement
of `TxnRefused` exists, so it is on the list rather than in the exit run.

### 41.3 The general form

> **A leftover is found by a sweep or it is found by accident, and this project has now done both on
> the same class of thing. The accident found one. The sweep found six more: two whose stated purpose
> was retired at a phase boundary and which outlived it, one duplicate counter, two that no commit has
> ever called, and one live counter with no reader.**

The sweep is four lines of shell and it is not a lane, because a lane that fails on an unused
identifier fails on the day somebody writes the producer before the consumer. It belongs at a phase
boundary, where "what did this phase leave behind" is a question somebody is already asking.

---

## 42. Every class that read zero under the broken probe, re-measured

Ansh, on the post-A6 list: *"Re-measure every class that was reading zero under the old probe and
report old against new, since `M62` was found this way and it will not be the only one."*

It was not the only one.

### 42.1 Which classes qualify, and which were already answered by a stronger instrument

The probe's blindness was specific: `noticed()` consulted a hand-listed subset of the harness's
detectors and could not see any class whose detector is an **aggregate assertion over a sweep** rather
than a per-seed verdict (§35.1). So the classes to re-measure are the ones whose zero came from
**that instrument**.

Two classes had already been settled by something stronger and are not re-measured:

- **`M30`** — *"measured trace-identical over 10k seeds"*. A trace-identity claim is stronger than
  anything the probe reports; a mutation that changes no trace changes no census.
- **`M66`** — settled at A6's close by a **byte-identical census** across 40 seeds with
  `ForeignLocksKept` included (§35.3). Same instrument, same strength.

`M67` and `M68` were re-measured when the probe was fixed (§37.3) and `M70` and `M60` with them. That
leaves four: **`M73`, `M53`, `M56` and `M47`** — plus `M62`, which the ruling had already turned into
a detector (§40).

**`M56` is in that list for a reason worth separating from the others.** It was not *reading* zero. It
had **never been measured at all**: its declaration is an opt-out, and `power-mutants.sh` does not run
the probe for an opt-out. It is included because the question *"which classes have a zero nobody has
re-taken"* has a wider answer than *"which classes measured zero"*, and §42.3 is what that widening
found.

Every measurement below is against **the unmutated tree at the same seed count and the same shard
shape**, because a sweep failure is a difference and not a presence (§16.4).

### 42.2 The results

| class | old | new | disposition |
|---|---|---|---|
| **`M62`** lock expiry off by one | 0 of 300, then 0 of 100 with the sweep detector, `sweepfail=0` | **18 of 200, first at seed 20**; clean 0 of 200 | **detector built** (§40); opt-out → floored class, `power-floor: 9`, `power-ceiling: 80` |
| **`M73`** a read answer lands in any incarnation | 0 of 200, `sweepfail=0`; opt-out saying *"a floor honest enough would need a 24-hour sweep"* | per-seed still 0 of 200 — and **`sweepfail=1` in all four shards**, on one criterion: *no read answer from a pre-restart incarnation was ever rejected*. `StaleIncarnation` 9–15 per fifty seeds → **flat zero** | **opt-out → `power-detector: sweep`, 60 seeds.** Found by the probe fix |
| **`M53`** empty mark releases through | 0 in 300, 0 in 3,000 at A5; 0 of 200 throttled *and* unthrottled at A6 | **0 of 300, `sweepfail=0`**, against a clean baseline of 0 and `sweepfail=0` at the same 300 | **unchanged, and now confirmed by the repaired instrument.** 34 census fields drift, so the mutated code RUNS — this is not `M66`'s byte-identical unreached — but the condition the defect needs does not arise. Opt-out stands |
| **`M56`** term gated only on what is dirty now | **never measured at all** — an opt-out reasoned by analogy with `M53` | **280 of 300, first at seed 0 in every shard**; **59 of 60** on a second run; **28 of 30 under A5's own shape**, the shape the opt-out was written against. `persist-before-reply` fires on seed 0 | **the opt-out was false on the day it was written.** → a floored class, `power-floor: 30`, `power-ceiling: 5` |
| **`M47`** superseded split applied anyway | *"zero firings in 300 A4 seeds"*, recorded as an unexercised path | **0 of 300, `sweepfail=0`, and the census is BYTE-IDENTICAL to the clean tree in all four shards — 0 of 76 fields move** | **upgraded, not merely confirmed.** It joins `M66` and `M30`: UNREACHED and proved, rather than undetected |

**Two of the five moved, one was upgraded, one was confirmed, and one was found to be false.** That is
the whole point of re-measuring a set of zeros: they were five different things wearing one number.

### 42.2b What `M47`'s upgrade is worth, since "still zero" sounds like nothing happened

`M47`'s old declaration said *zero firings in 300 seeds*, which is a **detection** claim and therefore
compatible with the defect happening and nothing noticing — which is exactly what `M62` turned out to
be. The new measurement is a **census-identity** claim: across 300 seeds, all 76 fields of the
accumulated census are byte-identical to the unmutated tree's. **The mutation changes nothing the
harness counts.** That is the difference between *no oracle spoke* and *there was nothing to speak
about*, and it is the same instrument that settled `M66` (§35.3) and `M30`.

Its argued reason survives contact with the number, which is worth saying because arguments usually
do not: Raft's figure-8 rule serialises the schedule `M47` needs, because a new leader must commit an
entry of its own before it can commit the previous term's split, and committing it means applying it,
which moves the extent before a second split can be proposed from it. **The argument predicted
byte-identity and byte-identity is what the census shows.**

### 42.3 `M56`: an opt-out is a claim, and the instrument that could refute it is switched off BY the opt-out

This one is not a probe-blindness finding, and that is what makes it the more useful of the two.

`M56`'s declaration read: *"n/a — the same schedule `M53` needs, and unreachable for the same measured
reason: the throttled collector does not produce a mark that opens and closes empty while an earlier
handed mark is outstanding."* It is **reasoned by analogy**. It cites `M53`'s measurement, not its own.

And `M56` is not `M53`. Both are in `raft/raft.go` and that is where the resemblance ends. `M53`
swaps `closeEmptyMark` for `releaseThrough` on an empty mark, so it needs **that mark to exist** —
an empty mark opening and closing while an earlier one is outstanding, which the throttled collector
does not produce. `M56` replaces `m := r.hsMark` with `m := PersistMark(0)`, which ungates the term
**whenever `hardStateDirty` is false** — which is most of the time. It needs no interleaving at all.
The analogy was drawn from BUG-017 naming both halves, not from what either mutation does.

Measured: **280 of 300, first at seed 0 of every shard** — and **59 of 60** in a separate two-shard
run, against a clean tree at **0 of 60** and **0 of 300**. It is caught by `persist-before-reply` with
the plainest verdict in the repository:

```
seed 0  VIOLATION persist-before-reply: range 1: node 2 sent a vote-resp advertising
term 1 at instant 114891944 while only term 0 was durable
```

**The mechanism that let it stand is structural and worth naming.** `scripts/power-mutants.sh` skips
any patch carrying a `# power:` line — it does not measure it, by design, because an opt-out is
supposed to mean *there is nothing to measure*. So:

> **An opt-out is a claim about reachability, and it exempts itself from the only instrument that
> could refute it. A floored class is re-measured every time the lane runs; an opted-out class is
> re-measured never.**

That is §37's shape one level in — *a lane too expensive to run is a lane whose claims are unchecked*
— except here it is not cost that switches the instrument off, it is the claim itself.

**And it is worse than a claim that went stale, which is what I expected to find.** The obvious story
is that the schedule mix moved under a reachability claim written for an earlier mix — A2's kill-time
amendment exists for exactly that. So the claim was re-measured under **A5's own shape**, the shape it
was written against, at commit `7a809a4`:

| | detections |
|---|---|
| `M56` under `POWER_CONFIG=a5`, 30 seeds | **28 of 30, first at seed 0** |
| the clean tree, same shape, same seeds | **0 of 30** |

> **The opt-out was false on the day it was written.** Nothing drifted. A claim that the sweep could
> not reach this class was made about a sweep that reaches it on nine seeds in ten, and it stood for
> a phase and a half because writing `power: n/a` is what turns the measurement off.

**The remedy, proposed and NOT built, with the reason.** A periodic **refutation pass**: measure every
opted-out class at a small seed count and fail if any of them detects. It is cheap — seventeen classes
at thirty seeds is under an hour of CPU — and it would have caught this the day the shape moved. What
stops it being a five-minute change is **scope**: several opt-outs patch the oracle framework itself
(`M8`–`M13`, `M15`, `M16`), and running the raft probe against a mutated *checker* will report
differences for reasons that are not refutations. A pass that reports false refutations is worse than
no pass (BUG-016's standard), so the scope rule is a decision rather than an implementation detail,
and it is on the record as a proposal rather than landed on my own judgement.
