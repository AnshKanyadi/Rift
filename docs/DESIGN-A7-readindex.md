# DESIGN-A7: read index, and linearizable reads that do not cost a log entry

**Status:** written before the code, to the point of decisions. Every decision below is marked
**[open]** and waits for a ruling; none is assumed. **Author:** Claude. **Decider:** Ansh.
**Phase:** A7 — the last Track A phase. **Depends on:** A6.

**Revised after BUG-022.** A6's last defect put a dependency directly in this phase's path: the read
mark that stops a commit landing below an answered read is a function of the log **only because every
read is a log entry**, and read index is the phase that stops that being true. `D-A7-5` is the new
decision, `§4.1` is the audit that would have caught the class it belongs to, and neither existed in
the first draft.

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

### D-A7-5: the read mark, which A6's last fix put directly in this phase's path **[open]**

**This is the decision A7 exists to make and did not know it had.** It is not a refinement of the
others; it bounds what read index is allowed to serve.

**The dependency.** BUG-022's fix — the third first-committer-wins guard — rests on a **read mark**: a
record holding the highest timestamp at which a range has been asked for a key. `PrewriteInto` refuses
when the mark is above the prewriter's snapshot, and that is what stops a commit landing below an
answer already given. The mark is a function of the log for exactly one reason:

> **In A6 every read IS a log entry. Every replica applies it and stages the identical mark.**

Read index answers a read **off the log**. It stages nothing, no replica sees it, and the mark it
would have left does not exist. A prewrite then passes a guard that has nothing to consult — which is
`M71`, the mutant for the recording half of BUG-022's fix, reintroduced as a design consequence
rather than as a patch.

**The failure is silent and it is BUG-022 exactly.** No error, no divergence, no structural invariant
violated: a well-formed database with money missing from it, on a schedule with no faults in it.

**A. Read index serves the linearizable read path only; A6's snapshot reads keep their log entry.**

- *for*: BUG-022's fix is untouched, because the operation that leaves a mark still leaves one. The
  distinction is principled rather than a carve-out — a **plain read has no timestamp to protect**: it
  is a linearizable read of the latest value, it participates in no transaction, and no prewrite's
  correctness depends on whether it happened. A snapshot read at `T` is a promise about `T` that a
  later commit can break. Only the second needs a mark, and only the second pays.
- *against*: it is the smaller win. A6's transactions are seven replicated round trips and two are
  reads; this leaves both on the log, and BENCHMARKS.md must then say the saving is on the plain path
  and not claim it for transaction reads.

**B. A leaseholder-local timestamp cache, carried on the prewrite.**

The leader keeps the marks in memory and attaches its value to each prewrite it proposes; every
replica applies `max(recorded, carried)`. The carried-value pattern is already this codebase's — it is
what `ExpireAt` and the commit timestamp do, and for the same reason: *a fact derived at propose time
and carried, so every replica compares the same two values.*

- *for*: the full win. No read costs an entry, transaction reads included, and the apply path stays
  deterministic.
- *against*: **it needs a leadership-handover argument this phase cannot make.** A new leader's cache
  starts empty, and it must not start below anything the previous leader served. The known ways to
  bound that are (i) a lease with a clock bound — which is STRETCH, struck, and the whole reason read
  index is A7's mechanism — or (ii) a low-water mark carried through the log at term change, which is
  a new replicated protocol with its own recovery story. Either is a phase, not a decision.
- and the honest version of the objection: **it puts a clock back under a mechanism chosen for not
  needing one.** A6 §22.3's rule applies — a deferral spent on a purpose it was not granted for is a
  mechanism widening itself.

**C. Serve transaction snapshot reads by read index and refuse the prewrites that would be unsafe.**

Refuse any prewrite whose key *might* have been read off-log — which, without a cache, is every key.

- *for*: safe.
- *against*: it refuses everything. Listed because it is the reflex answer and it needs to be written
  down as unusable rather than left as a thing somebody proposes in week three.

**Recommendation: A**, and record B as the thing that would lift the restriction along with what it
would cost. The measurement that would change the recommendation is the share of the read volume that
is transactional: if the plain path is a small fraction, A buys little and B's price becomes worth
paying — and that share is measurable **now**, from A6's own census, before a line of A7 is written.

**And a consequence for the exit criteria either way.** `M71` must be re-pointed at A7's shape: a
mutant that makes a read-index read skip whatever maintains the mark. If the answer is A, that mutant
is *"a snapshot read is served by read index"* — the design decision itself, planted as a defect. That
is the strongest form available here, because it makes the boundary between the two read paths a thing
the suite kills rather than a thing the code remembers.

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

Three more, from D-A7-5, because the read mark is now a fact this phase has to place:

| the derived fact | the wrong place to take it | the right place |
|---|---|---|
| whether a key has been read above a snapshot | the mark **as of the last logged read** | the mark as of **every read served, by either path** — a read index answer that leaves no mark makes this fact a lie rather than stale |
| which read path an operation may take | whether the client asked for a timestamp | whether the operation's answer is a **promise a later commit could break**: a plain read is not, a snapshot read at `T` is |
| a follower's read mark, if the mark is ever kept off the log | the follower's own memory | **nowhere a follower can hold it** — a follower serving reads and keeping a local mark that no prewrite consults is D-A7-5's failure with a second copy |

### 4.1 The count, and the audit the count does not perform

**Ten facts, named before the code**, against A5's four and A6's six. The before-and-after is reported
at phase end with its exclusions stated. The honesty of the count is the only thing that makes it
worth anything: A6's was *not* six of six, and saying so is what makes the four credible.

**And A6 finished by demonstrating what this table cannot do.** Six facts were named, none of them
became a defect, and BUG-022 happened anyway — because the table asks *where is this fact taken from*,
and BUG-022's fact was one **nothing took**. There was no derivation to walk to. The discipline is an
audit of the code; a missing fact is invisible to it.

> **Naming every fact you take is not the same as naming every fact you need.**

So A7 runs a second audit alongside the table, in the form the miss would have been caught by — **what
does this mechanism's correctness argument assume, and does this system provide it?**

| the assumption | whose argument makes it | does this system provide it? |
|---|---|---|
| a leader that has won an election knows its own commit index | read index's, implicitly | **no** — the figure-8 rule forbids counting earlier-term entries, which is §2 and is why the no-op is not optional |
| a quorum of heartbeat responses means *this* leader is still leader | read index's | only if the responses are **at the broadcast's term**; the assumption is stated in the table and is the one etcd's implementation is careful about |
| a read that has been answered leaves no trace the rest of the system depends on | read index's, silently | **no, since BUG-022** — an A6 snapshot read leaves a read mark, and D-A7-5 is that assumption failing |
| a follower's applied index is a sound freshness bound | follower reads' | yes, **because a read index is a fact about a position rather than about a node** (D-A7-2) |
| the term-start no-op is committed before any read is served in that term | read index's | **not automatically** — a leader can receive a read between `becomeLeader` and the no-op's commit, and the read must wait for it rather than take the inherited `commitIndex` |
| serving a read locally does not advance any replicated state | read index's | **no, if the mark is ever moved off the log** (D-A7-5B), which is why B is a phase and not a decision |

**Six assumptions, three of which this system does not provide.** That ratio is the argument for
running the audit at all: the table of facts came out clean at A6 and the phase's most expensive
defect was in the column the table has no room for.

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
6. §4's **ten** facts reported before-and-after with exclusions stated, and §4.1's six assumptions
   re-asked against the code that landed.
6b. **`M71` re-pointed at A7's shape** (D-A7-5): a mutant that serves a mark-leaving read off the log,
   killed by a conservation failure. BUG-022's guard is the thing A7 is most able to break silently.
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
8. **D-A7-5 — may read index serve A6's transaction snapshot reads, given that those reads leave a
   read mark BUG-022's guard depends on?** *Recommendation: no. Read index serves the linearizable
   read path; snapshot reads keep their log entry, because a plain read makes no promise a later
   commit can break and a snapshot read at `T` does. Record the leaseholder-local timestamp cache as
   the design that would lift the restriction, together with the fact that its handover argument needs
   either a lease's clock bound or a new replicated low-water-mark protocol — a phase, not a
   decision.*
9. **D-A7-5 — is the share of read volume that is transactional measured before this is ruled on?**
   *Recommendation: yes, and it is measurable now from A6's own census without writing any A7 code. If
   the plain path is a small fraction of reads, recommendation 8 buys little and the case for the
   timestamp cache becomes a real one rather than a deferral. A recommendation that a measurement
   could overturn should say so before the measurement, not after.*
10. **§4.1 — does the assumption audit become standing practice, alongside the fact table?**
    *Recommendation: yes. The fact table came out clean at A6 and the phase's most expensive defect
    was an assumption in the protocol's own correctness argument that the table has no column for.
    Two audits, and they fail differently.*
11. **§8.1 — is `M71` re-pointed at A7's shape as part of this phase?** *Recommendation: yes, and the
    mutant is the design decision itself planted as a defect — a snapshot read served by read index.
    That makes the boundary between the two read paths something the suite kills rather than something
    the code remembers.*

**Stopping here for rulings, as the protocol requires.** No A7 code is written.
