# DESIGN-A7: read index, and linearizable reads that do not cost a log entry

**Status:** **RULED.** Written before the code, to the point of decisions; every decision below was
marked `[open]` and none was assumed. All thirteen open questions are now answered — §9 carries the
rulings, §8 carries the exit criteria they produce. **No A7 code is written yet**: the first commit is
the term-start no-op on its own, per ruling 6. **Author:** Claude. **Decider:** Ansh.
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

## 3. The candidates **[RULED]**

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

**Recommendation: A.** — **RULED: A** (§9.1), with the refusal to reopen leases held to the same
discipline as A6's TSO refusal: a deferral spent on a purpose it was not granted for is a mechanism
widening itself.

### D-A7-2: follower reads **[RULED: yes]**

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

**RULED: yes** (§9.2), and the sweep must exercise them **non-vacuously** — a run in which every read
was served by a leader has not tested them.

### D-A7-3: what the read index is taken from **[RULED: A, at arrival]**

Two candidates, and this is D-A7-1's detail rather than a separate decision, but it has its own
failure mode:

**A. `commitIndex` at the moment the read arrives**, confirmed afterwards. **B. `commitIndex` at the
moment the quorum confirms.**

A is correct and B is *more* than correct — B is a later point, so it is never stale, but it makes
reads that arrive together unable to share one round, because each would take a different index. A
is the standard and the batching is the reason.

**Recommendation: A**, with the index captured *before* the broadcast and carried with the round.

**RULED: A** (§9.3), **with a condition that is the ruling's substance**: a read arriving at index `i`
and confirmed later must be **provably not answerable at any index below `i`**, stated as an oracle
and **induced**. Arrival capture is the weaker of the two options by construction — confirmation-time
capture is a later point and can never be stale — so the safety of the cheaper choice is exactly the
claim that the stamped index is a sound floor, and a claim that load-bearing does not live in prose.

### D-A7-4: what happens to the existing read path **[RULED: B for the phase, decided at exit]**

Today a read is a log entry (`opGet`), stamped at propose, answered at apply. Under read index it
becomes a local answer with no entry at all.

**A. Replace it.** All reads go through read index.
**B. Keep both**, and choose per request.

**RULED: keep both for the phase, decide at exit** (§9.4) — and the decision is *made* at exit and
recorded, not left to lapse. A fallback nobody decided to keep is a second code path with no owner.

**Recommendation: B, temporarily, and A by the exit criteria.** Keeping both for the phase's
duration is what makes the staleness checker meaningful: the two paths answer the same question and
must agree, and a differential between them is the strongest oracle available for this phase. By
exit, the replicated-read path stays only as a fallback that the sweep exercises, or it goes — and
which of those is D-A7-4's real question.

---

### D-A7-5: the read mark — **A7's governing constraint** **[RULED: A]**

Ansh, on the A6 sign-off: *"BUG-022's fix rests on the read mark being a function of the log because
every read is a log entry, and read index exists to stop reads being log entries. So A7 cannot be
designed as if the fix is inherited. State it as the phase's governing constraint."*

**So it is not a decision among the others; it bounds what read index is allowed to serve, and every
other decision here is taken inside it.**

### D-A7-5a: the narrow reading does not work, and the reason is BUG-022's own timeline

The ruling offers *"read index is scoped to exclude keys under transactional locks"* as one of the two
branches. Taken literally — exclude a key that **currently holds a lock** — it is not sufficient, and
BUG-022's five log entries are the counterexample:

```
idx=107  txn-get   a00  at 7750000000.514   -> answered      <- no lock exists yet
idx=109  prewrite  a00  start 7480000000.1792              <- the lock arrives AFTER
idx=111  commit    a00  -> commit 7630000000.3072          <- below the answered read
```

**The read that must leave a mark is answered before any lock exists.** A rule keyed on the key being
locked at read time would have let that read through, left no mark, and BUG-022 would be back with the
guard still in place and still passing.

What the branch means once repaired is **the transactional keyspace**, not the locked subset: a key
that *can* be prewritten, whether or not it currently is. That is recommendation **A** below, and the
repair is worth stating because the narrow reading is the one a reader reaches for first and it fails
silently.

### D-A7-5b: and the totality argument expires with its premises

Also ruled: *"whichever way it goes, the guard's totality argument gets restated under A7's conditions
and re-induced, because a guard proven total under one set of premises is a guard whose proof expired
when the premises did."*

A6 §28.3's argument is: after the guard `readMark(key) <= startTS < commitTS`, so no read *before* the
prewrite was answered at or above the commit timestamp; and a read *after* the prewrite either sits at
or above `startTS` and blocks on the lock, or sits below `startTS` and so below `commitTS`.

**Every clause of that names the log.** *Before the prewrite* and *after the prewrite* are positions in
one log; *blocks on the lock* is a property of applying a read entry against applied state. Read index
answers reads that occupy no position. So the argument does not carry over — it has to be rewritten
against A7's premises, and the rewrite is an exit criterion rather than a paragraph:

> **A7 does not close until the three-guard totality argument is restated under read index and
> `M71`/`M72` are re-induced against the restated form.** A mutant that passes because the property it
> attacks moved is a mutant that has stopped meaning anything.

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

**RULED: A** (§9.8), and record B as the thing that would lift the restriction along with what it
would cost — **its handover argument named as a phase rather than a decision**, which is what stops it
being smuggled back in as one.

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

## 3a. D-A7-6: how the term-start no-op is REPRESENTED **[RULED: A]**

§2 says *"the fix is one entry: on becoming leader, append an empty entry in the new term."* It does
not say what that entry **is**, and the three ways to say it differ in one respect that matters:
**one of them is a frozen-interface change and two are not.**

This is written before the no-op's commit because ruling 6 puts that commit first, and a decision made
inside a commit that is supposed to move every number for exactly one reason is a decision nobody
sees.

**A. `EntryNormal`, empty `Data`, and the ZERO `ProposalID`.**

- *for*: no interface change at all. And the identity is not arbitrary — `Propose` **refuses** the zero
  `ProposalID`, with a reason already in the code: *"a proposal needs an identifier; the zero value is
  refused so a caller cannot fall back to matching on log index, which is not a proposal identity."*
  So no client proposal can ever carry it, and **the no-op is identified by holding the one identity
  the propose path is forbidden to issue.** That is a distinguishing mark nothing else can collide
  with, rather than a convention.
- *against*: every consumer of the apply path must handle an entry with no `Data` and no proposal
  identity, and any of them keying a map on `ProposalID` now sees the same key once per election. That
  is the hazard below and it is the whole of the work.

**B. A new `EntryType`, `EntryNoOp`.**

- *for*: the typed answer, and this project prefers typed answers — DESIGN-A5 §13's `At[Index, T]`
  discussion is the standing argument for them.
- *against*: **`Entry` and `EntryType` ride in `Ready`, so this is a change to the interface DESIGN-A0
  D5 froze.** Per the standing rule that makes it *a report, never an assumed ratification* — and
  CARRY-FORWARD already holds one such change (`raft.Configuration()` should take an index) that was
  deliberately not made on its own. Opening the frozen interface for a no-op, while a better candidate
  waits, is the wrong order.

**C. `EntryNormal` with a marker prefix**, on the split's pattern (`store/range.go`: *"it reuses raft's
`EntryNormal` envelope with a marker prefix, because raft has no business knowing what a range is"*).

- *for*: an existing, working precedent in this tree.
- *against*: **the precedent points the other way here.** The split's marker exists because the
  *state machine* owns the concept and raft must not. The term-start no-op is the opposite: **raft
  owns it**, appends it itself, and the state machine must not care. Borrowing the pattern would put a
  raft-level fact into a state-machine-level encoding.

**Recommendation: A**, and it is chosen partly to *avoid* B rather than because typing is unwelcome —
the frozen interface should be opened once, for the change that has been waiting for it, not for this.

> **RULED: A. `EntryNormal`, nil `Data`, zero `ProposalID`.** Ansh: *"The survey decides it, not
> preference. B's only remaining benefit is the name, because the behaviour it would buy is already
> present… A name is not worth opening a frozen interface. C is rejected on your argument, which is
> the better one: the split's marker exists because the state machine owns that concept and raft must
> not, and the no-op inverts that, so C puts a raft-level fact into a state-machine-level encoding."*

**And the frozen interface now has a standing rule** (CARRY-FORWARD): it opens **once**, for
`raft.Configuration()` taking an index — *a change with a defect behind it*, the site that made
BUG-015 possible. Anything else that wants it opened waits and rides with that change, and a request
to open it for convenience is refused.

### 3a.3 The condition A was ruled under, which is the whole cost of A

Ansh: *"Both existing guards acquire a second reason, and a guard whose stated purpose no longer
covers everything resting on it is exactly how BUG-022 happened."* Three things follow, all landed
with the no-op:

1. **Each guard's comment is REWRITTEN to state both reasons, not appended to.** `store/replay.go`'s
   `applyOne` skip and `store/node.go`'s `answerAt` zero-ID return each now name (i) what they
   originally protected and (ii) that raft appends one of these per term since A7. A reader deciding
   whether the line may go can see everything resting on it.
2. **A mutant per guard**, per §22.6b — `M74` and `M75`, **two and not one, because they are
   independently removable and a single mutant would pass on half the protection.**
3. **The two propositions are asserted rather than trusted**, §3a.2 below.

### 3a.2 What A makes true, counted on every run

Ansh: *"assert what A makes true rather than trusting it. No committed entry with empty Data and a
zero ID ever reaches a state-machine arm, and no such entry ever answers a client. Both are checkable,
both are cheap, and A's correctness is precisely those two propositions."*

| census field | must read | what it is |
|---|---|---|
| `NoOpsApplied` | **non-zero** | the non-vacuity half: one no-op per election, so zero means the entry is not being produced and the two below are green over nothing |
| `NoOpReachedArm` | **zero** | proposition 1 — `M74` |
| `NoOpAnswered` | **zero** | proposition 2 — `M75` |

All three are exit criteria. **The non-vacuity is listed first deliberately**: two zeros over a sweep
that never produced a no-op are this register's commonest entry.

### 3a.4 The induction found both unit tests vacuous, in one command

Written before the mutants, both propositions got a unit test, and **both passed under the mutation
they were supposed to catch.** Recorded because it is the induced-failure rule doing the only thing it
is for:

- **Proposition 1's test** asserted the three arms do not match a dataless entry. Removing
  `applyOne`'s `if len(e.Data) == 0 { return }` left it green — because `applyOne` **has a `default:`
  arm**, and what protects it there is `decodeCmd` returning an op the inner `switch op` matches
  nothing for. The early return is a fast path, not the guard. **The node path and the replay path
  protect one property by two different mechanisms**, and snapshot equivalence compares their results,
  so both are now asserted.
- **Proposition 2's test** read `if err := r.Propose(ProposalID{}, …); err == nil { fail }`. A
  zero-value `Raft` returns `ErrNotLeader` whatever the id is, so relaxing the zero-ID rule left it
  green. It is now asserted as a **difference** — the zero id must be refused for a reason a named id
  is not — and it fails when the rule goes.

> **Two tests, written carefully, both vacuous, both caught by one command that tried to make them
> fail.** Neither would have been caught by reading them.

### 3a.1 What A costs, surveyed rather than guessed

An earlier draft of this section asserted that A carried a real hazard against the sacred list's
*client request dedupe — retried requests apply at most once*: a zero `ProposalID` recurring once per
election, polluting anything keyed on it. **The survey says the hazard is already answered, and it is
answered by guards that are in the tree today with their reasons written beside them.**

**Every consumer of `Entry.ID` in the system packages:**

| site | what it does | already guarded? |
|---|---|---|
| `store/node.go:771` `answerAt` | matches a committed entry to the client waiting on it | **yes** — `if e.ID.Zero() { return }` on the first line |
| `store/node.go:843` `answerTxn` | the same, for transaction steps | **yes** — same guard |
| `store/node.go:924` | the same, for the third answer path | **yes** — same guard |
| `store/codec.go:65` | encodes the ID into the log record | no guard needed; a zero ID round-trips as a zero ID |
| `raftcheck/ledger.go:410,448` | keyed on **transaction** IDs, not proposal IDs | not a consumer |

**And the apply path ignores a dataless entry by construction.** The committed-entry loop dispatches on
the CONTENT of `e.Data`, and its arms are `isTxnCommand(e.Data)`, `isSplitCommand(e.Data)`, and
`len(e.Data) > 0`. **There is no `default:`.** An entry with empty `Data` matches no arm and applies as
nothing — and the third arm's guard is what makes that true on purpose rather than by luck.

So A's real cost is not new guards. It is:

1. `becomeLeader` appends `Entry{Type: EntryNormal, Term: r.term, Index: lastIndex()+1, ID: ProposalID{}, Data: nil}`;
2. **the two existing guards become load-bearing for a new reason**, and that has to be said in both
   places, because a guard whose stated purpose no longer covers everything resting on it is how
   BUG-022 happened;
3. **a mutant removes each guard and must be killed.** Today `if e.ID.Zero() { return }` protects
   against a client op with no identity, which no path produces; after the no-op it protects against a
   raft-internal entry answering somebody's request. That is a second reason on one line, and §22.6b's
   rule applies — *a decision in two halves needs a mutant per half*.

### 3a.1b And the survey changes what B is worth

**No consumer switches exhaustively on `EntryType`.** Every one tests `== EntryConfChange` or
`!= EntryConfChange` — `raft/raft.go` at 1274, 1573, 2335, 2509, 2743 and `store/node.go` at 1616,
1693. A new `EntryNoOp` would therefore be routed down the **normal-command** path at every one of
those sites, and would still need exactly the same empty-`Data` handling to be ignored.

> **B buys a name, not a behaviour.** It does not make the state machine skip the no-op for free, and
> it costs a change to the interface DESIGN-A0 D5 froze. That is the trade in one sentence, and it is
> what the survey was for.

### 3a.2 And it needs the fact table's question asked of it

§4's discipline, applied to this decision before the code:

| the derived fact | the wrong place to take it | the right place |
|---|---|---|
| whether an applied entry is a client proposal | it has `Data` | **its `ProposalID` is non-zero** — a client proposal may legitimately carry empty `Data`, and a no-op never carries an identity |
| the term a no-op belongs to | the term when it is appended | the term at `becomeLeader` (§4 already carries this row, and A is where it becomes concrete) |

**Stopping here on D-A7-6.** It is the only decision in the no-op's path that is not already ruled, and
option B would touch the frozen interface, which is a report rather than a thing to assume.

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
| **`Propose` will never issue the zero `ProposalID`** | **D-A7-6's**, and load-bearing since the no-op landed | **yes today, by an explicit refusal in `Propose` — and that refusal is now holding up something it was not written for.** §4.1a |

**Seven assumptions, three of which this system does not provide.** That ratio is the argument for
running the audit at all: the table of facts came out clean at A6 and the phase's most expensive
defect was in the column the table has no room for.

### 4.1a P-NOOP, the named premise D-A7-6 rests on, written so it expires loudly

Ansh, ruling A: *"say plainly what the no-op's identity rests on… and add it to the assumption audit
as a named premise so it expires loudly, in the form §9a uses."*

> **P-NOOP. `Propose` refuses the zero `ProposalID`, so no client proposal can ever carry it.**

The refusal exists already, and its stated reason is about proposal identity:

> *"a proposal needs an identifier; the zero value is refused so a caller cannot fall back to matching
> on log index, which is not a proposal identity"*

**That sentence no longer covers everything resting on the line.** Since the term-start no-op landed,
the zero `ProposalID` is *the no-op's identity* — the one value nothing else in the system can produce,
which is exactly why it was chosen over a new `EntryType`.

| premise | breaks when | what happens |
|---|---|---|
| **P-NOOP** | the refusal in `Propose` is relaxed, or any path is added that mints a zero id | **the no-op stops being distinguishable from a client proposal.** `answerAt`'s guard then either withholds real answers or lets the no-op complete somebody's request — and **nothing errors.** It breaks SILENTLY |

**Asserted rather than trusted**, in the form the rest of this phase uses: `TestTheNoOpAnswersNobody`
requires the zero id to be refused **for a reason a named id is not**, which is a difference and not a
presence. The first version of that test asserted only `err != nil`, was satisfied by `ErrNotLeader`,
and passed under the exact mutation it existed to catch (§3a.4).

---

## 4.2 The measurement D-A7-5 turns on, taken before the ruling

Open question 9 asked for the share of read volume that is transactional, *"measurable now from A6's
own census without writing any A7 code"*. It is, and here it is — from the signed 25,000-seed exit run
at `611d0b9`:

| | reads | how it is obtained |
|---|---|---|
| transactional (`TxnReads`) | **7,489,025** | counted |
| second-pass stability probes | 2,517,352 | counted |
| audit reads | ~3.2 M started plus 1,910,147 re-asked | derived from 399,951 audits × 8 accounts |
| **plain (`k*`) reads** | **≈ 875,490** | **derived**: `SnapshotReads` is 350,196 and the workload takes a remembered timestamp on 400 per mille of plain reads, so the total is `350196 / 0.4` |

> **Plain reads are about one in ten.** Scoping read index to the plain path addresses roughly 10% of
> the read volume this sweep produces.

**Two honest qualifications, because the number is load-bearing.** The plain figure is *derived* from
a configured ratio rather than counted — a counted figure needs one census field and should be added
before the number is quoted in BENCHMARKS.md. And the mix is **this workload's**: the bank's audits
read every account on every pass and the second-pass probe reads them again, which is harness traffic
answering a checker rather than a client. A production mix is not this mix, and the 10% bounds the
sweep rather than the design.

### What it does to recommendation 8

It does not overturn it, and it changes what the recommendation is *for*.

**A stays the recommendation for what A7 can ship**, because B's price is unchanged: a leaseholder-local
timestamp cache needs a leadership-handover argument, and the two known ways to make one are a lease's
clock bound — struck, and the whole reason read index is A7's mechanism — or a new replicated
low-water-mark protocol, which is a phase.

**But A7's value proposition moves**, and it should move in the doc rather than in the benchmark.
Under A, read index is **the correctness mechanism CLAUDE.md's fourth headline claim names**
("linearizable reads via read index"), delivered on the path where linearizable reads live — and its
*throughput* win is on one read in ten. BENCHMARKS.md must say that in those terms. A phase sold as a
latency win that removes 10% of the read traffic would be the kind of claim this project takes apart
in other people's work.

---

## 5. The oracle **[RULED: three properties, 3 is a fixture]**

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

**RULED: a fixture while both paths exist** (§9.5). It costs a second read per checked read, and
*being the only instrument that can catch a stale read no client observed is exactly why.* Property 1
catches a stale read only if some client observed the write it missed; in a quiet history nobody did,
and porcupine is green over a lie. **It carries a non-vacuity count**, because a differential that
compared nothing is this register's commonest entry.

---

## 5a. Ruling 3's oracle, specified before it is built

Ruling 3 approved arrival-capture **with a condition**: *a read arriving at index `i` and confirmed
later must be provably not answerable at any index below `i`, induced.* That condition is §5's
property 2 made concrete, and it is specified here on §40's pattern — assert, independence, induction
cases — because an oracle designed in the same commit that needs it is an oracle shaped to pass.

### 5a.1 Why this is the gate rather than a detail

Arrival-capture is the **weaker** of D-A7-3's two options *by construction*. Confirmation-time capture
takes a later commit index, so it can never be stale; arrival-capture takes an earlier one, and it is
chosen because reads arriving together can then share one confirmation round. **So the entire safety
case for the cheaper choice is the claim that the stamped index is a sound floor** — and a claim
carrying that much weight is not allowed to live in prose.

### 5a.2 What it asserts

> **For every read answered off the log at stamped index `i`: every write acknowledged to a client
> before that read was issued occupies a log index at or below `i`.**

That is the linearizable-read condition stated as a position rather than as a value — *a read index is
a fact about a position, not about a node* (D-A7-2), so the check is about positions too. The
derivation it protects is the standard one and it is worth writing out, because the oracle exists
exactly where the derivation could silently stop being true:

- a write completes only after it is committed, so at the instant it is acknowledged its index is at
  or below the leader's `commitIndex`;
- the read captures `i = commitIndex` **at arrival**, which is after that acknowledgement;
- `commitIndex` is monotonic, so `i >= ` that write's index.

Each of the three is a place the implementation can go wrong: acknowledging before commit, capturing
after the answer rather than at arrival, and a `commitIndex` that moves backwards across a term change
— which is §2's no-op, from the other side.

### 5a.3 Why it is independent of the thing it judges

Two inputs, neither of them produced by the read-index implementation:

| input | where it comes from | why that is independent |
|---|---|---|
| the call and return instants of every client operation | the harness's own client driver, which is what porcupine already consumes | it is a record of what the harness did, not of what the system says it did — `tools/provcheck`'s rule, and the reason `raft.tail.persisted` could not be read back from the engine (DESIGN-A1 §5c) |
| the log index of every acknowledged write | the `raftcheck` ledger's committed prefix, per range | the same walk `resolution-only-breaks-expired-locks` uses (§40.2): the supplier **decodes and stops**, and no comparison is re-run inside it |

The stamped index `i` rides in the answer, which is the carried-value pattern this codebase already
uses for `ExpireAt` and the commit timestamp, and for the same reason: *a fact derived at one point and
carried, so every observer compares the same two values.* **The oracle never recomputes `i`** — if it
did, it would be re-running the rule under test, which is precisely how the retired model failed
(DESIGN-A6 §13.1).

### 5a.4 The induction, which is what discharges the gate

Built directly, in milliseconds, before any seed search — because the sweep is the thing whose reach
this phase is uncertain about:

| case | required |
|---|---|
| a read stamped at `commitIndex` at arrival, served after confirmation | **silent** |
| a read stamped at `commitIndex - 1` | **violation**, naming the write it could miss and the index it was stamped at |
| a read stamped at arrival, answered after further commits have landed | **silent** — a later state is never stale, and the oracle must not mistake freshness for a fault |
| a read whose stamped index is above the leader's confirmed commit | **violation** — §5's property 2 in the other direction, and the one an over-eager optimisation produces |
| a follower read stamped by the leader, answered after the follower's apply reaches it | **silent** — the answer is pinned by the index, not by the follower's connectivity (D-A7-2) |
| a follower read answered **before** its apply reaches the stamped index | **violation** |
| a run in which no write was acknowledged before any read | **silent, and NOT counted** |

**The last row is the non-vacuity case**, and it is why the count exists: a sweep in which no read ever
had a preceding acknowledged write has exercised none of this and would report a silence that means
only that the workload did not overlap. The census carries the number of reads the oracle actually
judged, and `exitCriteriaFailures` refuses a sweep in which it is zero — the same construction
`ResolverDeclarations` got (§40.5), added against a measurement rather than by argument.

### 5a.5 The mutant

`i - 1` is the mutation: stamp the read one index low. It is the smallest possible version of the
defect and the one an optimisation would actually produce, and it must be killed by this oracle rather
than by porcupine — **because porcupine can only see it when some client observed the write that was
missed**, which is §5's stated weakness in property 1 and the whole reason property 2 exists.

---

## 6. What A7 does not do

- **Leader leases** — STRETCH, Amendment A6, and §3's D-A7-1B says why reconsidering them here would
  be a mechanism widening itself.
- **Observed timestamps** — STRETCH.
- **Reads at a past timestamp via read index.** A6's snapshot reads name an explicit instant and are
  already answerable from any replica holding the version; they do not need a read index and giving
  them one would conflate two questions (DESIGN-A6 §15.5).

---

## 7. The change that moves every number, and what to do about it **[RULED: A]**

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

**RULED: A** (§9.6) — *one reason per moved number.*

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

## 7a. The re-measurement, and what one extra entry per election actually moved

Ruling 6: *the no-op lands alone, on a committed baseline, with a full re-measurement and one reason
per moved number.* This is that measurement.

### 7a.1 Thirty seeds could not attribute anything, and the control is how that was established

A first pass compared pre and post at 30 seeds: **55 of 80 census fields moved.** Naming a reason for
each would have been false precision, because once the trace diverges at the first election every later
count comes from a different run. So a **control** was run — the *unchanged* tree over a different
30-seed window — to measure how much these fields move with no change at all.

**They move a lot.** 34 of 51 comparable fields sat inside a single window shift, including
`Inconclusive 1 → 0` and `ConfRecoveries −14` (window Δ **−15**). And the field whose direction the
causal story predicts most cleanly, `SnapshotsTaken +497`, had a window Δ of **+538** — larger than the
effect. **A correct causal story does not make a number attributable.**

> **Ruling 6 is not satisfiable at 30 seeds**, and the answer is more seeds rather than weaker
> attribution. Sums over `N` seeds grow like `N` and their noise like `√N`, so relative noise falls as
> `1/√N`; with treatment ≈ noise at 30, a 3× margin needs `√(N/30) = 3`, i.e. **N ≈ 270**.

### 7a.2 The measurement at 300 seeds, with three control contrasts

Pre and post at seeds 0–300, plus the unchanged tree at 300–600 and 600–900, giving **three control
contrasts** so the bar is a measured spread rather than one difference. A field attributes only if its
movement exceeds the widest of the three.

**7 of 55 attribute. 48 do not**, and the 48 are reported with their bands rather than dropped —
they are the evidence that the bar exists.

| attributed | pre → post | no-op Δ | control band |
|---|---|---|---|
| `GCApplied` | 64841 → 70227 | **+5386** | 519 |
| `SnapshotsTaken` | 75733 → 79540 | **+3807** | 558 |
| `VersionsCollected` | 43267 → 46264 | **+2997** | 376 |
| `GCProposed` | 19568 → 21138 | **+1570** | 249 |
| `SnapshotsApplied` | 6755 → 7227 | **+472** | 302 |
| `SplitsApplied` | 5596 → 5703 | **+107** | 69 |
| `ConfRecoveries` | 304 → 259 | **−45** | 10 |

**One reason names all seven.** One extra entry per election per range → the applied-entry snapshot
threshold (`SnapshotThreshold: 6`) is reached sooner → more snapshots and more truncation, hence more
GC proposed and applied, more versions collected, more splits applied per unit of log, and **fewer
configuration entries surviving in a recovered log tail** — which is `ConfRecoveries` falling.

### 7a.3 The field that failed at 30 and cleared at 300 is the evidence the bar is real

`SnapshotsTaken` is **inside** the band at 30 seeds and **6.8× outside** it at 300. `ConfRecoveries` is
`−14` against a band of `−15` at 30, and `−45` against a band of `10` at 300.

> **A bar that only ever confirms is a bar chosen to confirm.** These two failed it and then cleared it
> at a seed count derived from the noise rather than picked — which is the difference between a
> threshold that is measured and one that is set where the answer already is.

`Inconclusive` moved `2 → 1` at 300 against a band of `1` and **remains unattributed**, which is the
same verdict it got at 30 for a much better reason. `StaleEpochRefusals` is the sharpest case for the
bar: **+5081**, the largest raw movement in the census, against a control band of **6155**.

---

## 8. Exit criteria — **RULED**

Ansh sets these. What follows is the ruled set, not the proposal: *the thirteen answers in §9, plus
the standing set.* The proposal it replaces is preserved in the git history of this file.

### 8.1 The thirteen, as gates

Each ruling in §9 is an exit gate. Five of them are gates a run can fail rather than statements a
reader can agree with, and those five are listed here with the shape of their evidence:

| # | gate | evidence that discharges it |
|---|---|---|
| 3 | the read index is captured at ARRIVAL | **a read arriving at index `i` and confirmed later is provably not answerable at any index below `i`** — stated as an oracle over the ledger and **induced**, not argued in prose |
| 6 | the term-start no-op lands **separately and first** | two commits, and a re-measurement between them in which **every moved number has exactly one reason** |
| 11 | `M71` is re-pointed at A7's shape | the boundary between the two read paths is planted as a defect — *a snapshot read served by read index* — and killed by a conservation failure |
| 13 | the three-guard totality argument is restated under read index | the restated argument in this document, and `M71`/`M72` **re-induced against the restated form** |
| 5 | the differential oracle is a fixture while both paths exist | a sweep in which it ran, with its non-vacuity asserted; and D-A7-4's fate decided at exit rather than assumed |

The other eight are decisions with consequences the code has to match, and §9 states each one's.

### 8.1a A7's own exemptions use the split labels from the start

**Ansh, adding one item to the thirteen:** *"A7's own opt-outs use the split labels and the
named-detector field from the start, so the phase does not create a new cohort of the thing you just
spent a cycle refuting."*

Every mutant A7 lands either carries a measured floor and ceiling, or **one** of:

- **`# power-covered-by: <instrument> -- <why a sweep is not the instrument>`** — and the instrument is
  named precisely enough that `make power-refute` can run it. A covering test must match the patch's
  `covering-test:`; a floor must exist in `floors.go`. The pass executes it and it must kill.
- **`# power-unreachable: <detector> -- <why, including NO OTHER DETECTOR>`** — naming the detector the
  number was taken against, and arguing that nothing else in the exit-criteria list sees the class more
  often. `make power-decl` refuses the declaration without that clause, in milliseconds.

The bare `# power:` does not exist in this phase. It is retired, and it survives in the tree only on a
patch that must SURVIVE, where the exemption is earned by `expect: alive`.

**Why this is an exit criterion and not a style note.** A7 is the phase most likely to produce
exemptions of exactly the refuted kind. Read index adds a read path with no log entry, so several
mutants will attack code whose defect is *invisible to the sweep by construction* — and that sentence
is `M56`'s, `M30`'s and `M67`'s sentence. The three failure modes are all live here:

| the mode | what it looks like in A7 |
|---|---|
| **false when written**, reasoned by analogy (`M56`) | *"as the other read-path mutant — the sweep cannot produce a stale read"*, citing a class's number instead of taking one |
| **stale**, a claim about a schedule mix that moved (`M30`) | any reachability measured **before** the term-start no-op lands, since ruling 6 moves every trace by design |
| **bounded to its detector** (`M67`) | *"porcupine did not catch it"* — which is a claim about porcupine, and §5's whole point is that a stale read nobody observed is invisible to porcupine and visible to the differential |

The third is the one to watch, because A7's own oracle design already says the per-key checker is the
**weakest** of its three properties. A declaration citing it and stopping there would be `M67` written
by somebody who had just read the argument against it.

### 8.2 The standing set

1. **Every new oracle induced.** No gate counts until its failure has been induced. This covers the
   staleness properties in §5 and the ledger-side invariant, and it covers the read-index-at-arrival
   oracle from ruling 3.
2. **Every bug found in this phase is in BUGS.md with its mutant class**, and where no existing class
   would have caught it, the new class lands **in the same PR as the fix** — Amendment A2, not a
   follow-up issue.
3. **Both corpus lanes green.** `make corpus` (every bundle still replays) and `make corpus-reproduces`
   (every bundle still *exercises its defect*). They are different questions and A5 paid to learn it;
   the no-op moves every trace, so regeneration is a **search** and its reproduction verdict is read
   rather than assumed (DESIGN-A6 §16.3).
4. **Power floors and ceilings re-measured under A7's shape, with the refutation pass reported.**
   Every floored class is measured against the shape the no-op produces, not inherited from A6's; and
   `make power-refute` is run and its result stated — how many reachability claims were re-measured,
   how many were refuted, and which classes are exempt with the unmeasurable-here argument. Every
   `power-covered-by` instrument is **run**, not read. DESIGN-A6 §43.
5. **25,000 seeds, zero safety violations, and the inconclusive rate explained** rather than reported.
   Amendment A4: an inconclusive is never a pass, and a rising rate is answered by shrinking the
   window or partitioning harder, never by loosening the checker.
6. **§4's fact count and §4.1's assumption audit both reported at close.** Ten facts named before the
   code, reported before-and-after with exclusions stated; six assumptions re-asked against the code
   that landed. **They fail differently and that is why there are two** — ruling 10.

### 8.3 Seed count and sequencing

25,000, sharded, as A6 ran. `make exit-run` is one command and the machinery is built.

The sequencing is fixed by ruling 6 and it is not negotiable within the phase: **the term-start no-op
is its own commit, on a committed baseline, with a full re-measurement, and one reason per moved
number.**

**One thing stands between here and that commit: D-A7-6 (§3a) is open.** §2 says *append an empty
entry* and does not say what the entry is, and one of the three answers — a new `EntryType` — is a
change to the interface DESIGN-A0 D5 froze, which is a report rather than an assumption. The
recommendation is A (`EntryNormal`, empty `Data`, the zero `ProposalID`), chosen partly so the frozen
interface is opened once for the change already waiting on it rather than for this. It carries a real
obligation either way: the zero ID recurs once per election, so every structure keyed on `ProposalID`
must skip it explicitly, and a mutant must remove that skip and be killed. A6's three owed
measurements are discharged (CARRY-FORWARD), so nothing blocks the first commit — but the no-op moves
the baseline they were taken against, which is why criterion 4 above asks for the power numbers again
rather than citing A6's.

---

## 9. The thirteen rulings

Every one of these was a decision I did not make. Each is recorded with the ruling, and then with what
the ruling **obliges** — because a ruling recorded without its consequence is a ruling that gets
re-litigated when the consequence arrives.

### 1. D-A7-1 — how is leadership confirmed?

**Ruled: A, heartbeat-confirmed read index.** And on the refusal to reopen leases: *refusing on the
grounds that a deferral would be spent on a purpose it was not granted for is the same discipline as
the TSO refusal.*

That pairing is the useful part of the ruling. A6 §22.3 refused a timestamp source that would have
solved a real problem, because the authorisation to use one was granted for a different contingency.
Here the same shape appears as a *deferral* rather than an authorisation: leases are deferred to
STRETCH, and reaching for them because they would make A7 faster spends the deferral on a purpose it
was not granted for. **A mechanism that widens itself is the same defect whichever direction it
widens in**, and naming the two instances together is what makes it a rule rather than two refusals.

*Obliges:* nothing in A7 may depend on a clock for read correctness. The envelope experiment stays
STRETCH's and the clock machinery that landed at A0.4 stays unused by this phase.

### 2. D-A7-2 — do followers serve reads?

**Ruled: yes.** It is CLAUDE.md's scope for the phase.

*Obliges:* the sweep must **exercise** them and the exercise must be non-vacuous — a run in which
every read was served by a leader has not tested follower reads, and a census field has to say so.

### 3. D-A7-3 — is the read index captured at arrival or at confirmation?

**Ruled: A, at arrival**, so reads arriving together share one confirmation round — **with the
condition that a read arriving at index `i` and confirmed later must be provably not answerable at any
index below `i`, induced.**

The condition is the whole ruling. Capturing at arrival is the *weaker* of the two options by
construction: confirmation-time capture is a later point and therefore never stale, and arrival-time
capture is chosen for batching. So the safety of the cheaper choice is exactly the claim that the
index a read was stamped with is a sound floor, and a claim that load-bearing is not allowed to live
in prose. **It is an oracle, over the ledger, and it is induced before it counts.**

*Obliges:* a ledger-side invariant asserting, for every read answered off the log, that no write
committed at an index below the read's stamped index was invisible to it — and a planted defect that
serves a read one index low, killed by that invariant.

### 4. D-A7-4 — does the replicated read path survive the phase?

**Ruled: keep it for the phase as the differential oracle's other half, and decide its fate at exit.**

*Obliges:* the decision is *made* at exit and recorded, not left to lapse. A fallback nobody decided
to keep is a second code path with no owner.

### 5. §5 — is the differential oracle a lane or a fixture?

**Ruled: a fixture while both paths exist**, and: *being the only instrument that can catch a stale
read no client observed is exactly why.*

Property 1 — per-key linearizability — catches a stale read only if some client observed the write it
missed. In a quiet history nobody observed it and porcupine is green over a lie. The differential is
the only instrument in this phase that does not need a witness.

*Obliges:* it is on in the sweep, not in a nightly lane; and it carries a non-vacuity count, because
a differential that compared nothing is this register's commonest entry.

### 6. §7 — does the no-op land separately?

**Ruled: yes, first, with a full re-measurement**, and: *one reason per moved number.*

*Obliges:* the no-op commit and the read-index commits are separate, and between them every count the
exit run prints is re-measured. A single commit in which every number moved for two reasons at once is
the shape that makes a power regression unattributable, and Amendment A2 exists because unattributable
regressions are how detection power is lost quietly.

### 7. §7 — are A6's three owed measurements taken before A7 starts?

**Ruled: yes — and all three are taken.** The unthrottled collector (40 seeds, 49m32s, 48.8× the
collections, zero violations, and A5's detection figure not reproducing); the race-lane bound (which
produced a *third* answer — the lane split by what it is for, with a budget per half); and the mutant
power measurement under A6's shape (§34, and the five zeros it re-took at §42).

*Obliges:* nothing blocks the first commit. But the no-op moves the baseline all three were taken
against, so the power half is re-taken under A7's shape at exit — §8.2 criterion 4.

### 8. D-A7-5 — may read index serve A6's transaction snapshot reads?

**Ruled: no.** Read index serves the linearizable read path; A6's snapshot reads keep their log entry.
**With the leaseholder-local timestamp cache recorded as the design that lifts the restriction, and
its handover argument named as a phase rather than a decision.**

The distinction is principled rather than a carve-out: **a plain read makes no promise a later commit
can break, and a snapshot read at `T` does.** Only the second needs a mark, and only the second pays
for one.

*Obliges:* B is written down here as a design with a price, not left as a thing somebody re-proposes
in week three. Its price is a leadership-handover argument, and the two known ways to make one are a
lease's clock bound — struck, and the whole reason read index is A7's mechanism — or a replicated
low-water mark carried at term change, which is a new protocol with its own recovery story. **Either
is a phase.** Naming it as a phase is what stops it being smuggled in as a decision.

### 9. D-A7-5 — is the transactional share of read volume measured first?

**Ruled: taken, and the reframing is right.** §4.2: plain reads are about **one in ten** of this
sweep's read volume. It does not overturn ruling 8, and it changes what A7 is *for*:

> **Read index is the correctness mechanism CLAUDE.md's fourth headline claim names — *linearizable
> reads via read index* — delivered on the path where linearizable reads live. Its throughput win is
> on one read in ten.**

*Obliges:* **BENCHMARKS.md says exactly that, in those terms, including both qualifications.** The
plain figure is *derived* from a configured ratio rather than counted, so a counted census field is
added before the number is quoted; and the mix is **this workload's**, whose audits are a checker
reading rather than a client. A phase sold as a latency win that removes 10% of read traffic would be
the kind of claim this project takes apart in other people's work.

### 10. §4.1 — does the assumption audit become standing practice?

**Ruled: yes, beside the fact table.** And the reason is the argument: *the table came out clean at A6
while the phase's most expensive defect was an assumption in the protocol's own correctness argument
that the table has no column for.* **Two audits that fail differently.**

The fact table asks *where is this fact taken from* and walks derivations that exist. BUG-022's fact
was one **nothing took** — there was no derivation to walk to. The assumption audit asks *what does
this mechanism's correctness argument assume, and does this system provide it*, and a missing
provision is visible to it precisely because it is asked about the argument rather than about the
code.

*Obliges:* both are reported at every phase close from A7 onward, with their exclusions stated. Six
assumptions are on the table in §4.1 and **three of them this system does not provide** — that ratio
is the argument for running it at all.

### 11. §8.1 — is `M71` re-pointed at A7's shape?

**Ruled: yes** — and **the boundary decision itself is planted as a defect, so the suite kills it
rather than the code remembering it.**

The mutant is *a snapshot read is served by read index*: ruling 8's decision, applied as a patch. If
the boundary between the two read paths is a thing a future reader can erase by simplifying a
conditional, then it is a comment; if erasing it fails a run, it is a mechanism.

*Obliges:* `M71` is re-pointed in the phase, with its covering test, its floor and its ceiling, and it
is killed by a **conservation failure** — the bank losing money — rather than by a structural check,
because that is what BUG-022 actually looked like.

### 12. D-A7-5a — is "exclude keys under transactional locks" repaired to "exclude the transactional keyspace"?

**Ruled: yes, and it is not a wording change.** *BUG-022's read was answered before any lock existed,
so a rule keyed on currently-locked would have let it through and left no mark. The property is
whether the key **can** be prewritten.*

The five log entries are the counterexample and they are in §3 above: the `txn-get` at idx 107 is
answered, the prewrite arrives at idx 109, the commit lands at idx 111 **below the answered read**.
A rule keyed on the key being locked *at read time* passes that read straight through, records
nothing, and BUG-022 is back with the guard still in place and still green.

*Obliges:* the scoping predicate is written against the transactional keyspace, and the narrow
reading is recorded here as a repaired defect rather than deleted — because it is the reading a reader
reaches for first and it **fails silently**.

### 13. D-A7-5b — does exit require the totality argument restated and `M71`/`M72` re-induced?

**Ruled: yes, as an exit criterion rather than a paragraph.** And the sentence goes in the doc:

> **A mutant that passes because the property it attacks moved has stopped meaning anything.**

Every clause of A6 §28.3's argument names a log position — *before the prewrite*, *after the
prewrite*, *blocks on the lock* — and read index answers reads that occupy none. The proof expired
with its premises. A guard proven total under one set of premises is a guard whose proof expired when
the premises did.

*Obliges:* the restated argument lands in this document, and `M71` and `M72` are re-induced **against
the restated form** — not re-run against the old one and observed to still pass.

---

## 9a. The three-guard totality argument, restated under read index

Ruling 13 requires this before A7 closes, and it is written here rather than deferred because every
other decision in this phase is taken inside it (D-A7-5). **`M71` and `M72` are re-induced against
*this* form, not against A6 §28.3's.**

### 9a.1 Why the old argument does not carry

A6 §28.3, in full:

> After the guard, `readMark(key) <= startTS < commitTS`. So no read **before** the prewrite was
> answered at or above the commit timestamp; and a read **after** the prewrite either sits at or above
> `startTS` and blocks on the lock, or sits below `startTS` and so below `commitTS`.

**Every clause names a position in one log.** *Before the prewrite* and *after the prewrite* are
orderings of log entries. *Blocks on the lock* is a property of applying a read entry against applied
state. And the mark itself is a function of the log for one reason only: **in A6 every read IS a log
entry, and every replica applies it and stages the identical mark.**

Read index answers reads that occupy no position. So the argument is not weakened — it is **not
stated about this system any more**. A guard proven total under one set of premises is a guard whose
proof expired when the premises did.

### 9a.2 The premise ruling 8 supplies

The restatement is possible at all because D-A7-5 was ruled **A**: read index serves the linearizable
read path, and A6's snapshot reads keep their log entry. So the system has two read paths, and the
argument needs exactly one fact about the boundary between them:

> **P. Every read that names a timestamp — every operation whose answer is a promise a later commit
> could break — is a log entry, applied by every replica, staging the identical mark.**

`P` is not an assumption about the implementation; it is the decision, and §8.1's gate makes it a
thing the suite kills rather than a thing the code remembers (ruling 11). **`M71` re-pointed is `P`
negated**: a snapshot read served by read index.

### 9a.3 The restatement

Take a prewrite of key `k` by a transaction with start timestamp `startTS`, committing at `commitTS`.
The three guards are: the lock check, the write-conflict check, and BUG-022's read mark.

**Every read of `k` is one of two kinds, and the split is exhaustive by `P`.**

**Kind 1 — a read that names a timestamp** (`OpTxnGet`, a snapshot read at `T`). By `P` it is a log
entry, so it holds a position in `k`'s range's log and it staged `readMark(k) >= T`. The old argument
applies verbatim to this kind, because the old argument's premises are exactly `P`:

- the guard gives `readMark(k) <= startTS < commitTS`, so any such read **before** the prewrite was
  answered strictly below `commitTS`;
- any such read **after** the prewrite either sits at or above `startTS` and blocks on the lock, or
  sits below `startTS` and hence below `commitTS`.

`startTS != commitTS` holds for BUG-021's reason: both are minted, and two mints never collide — the
node tag separates nodes, the logical counter separates mints on one node, and `IdentityCollisions`
asserts the cross-node half at zero on every exit run.

**Kind 2 — a read that names no timestamp** (a plain read, served by read index). It is answered off
the log, stages no mark, and occupies no position. **The claim is that it needs none**, and the reason
is not that it is harmless but that it makes no promise a later commit can break:

> A plain read is a linearizable read of the **latest** value. Its correctness condition is that it
> reflects every write acknowledged before it was issued — a statement about a *prefix*, discharged
> entirely by the read index protocol (§1.1: leadership confirmed at or after arrival, applied index
> at or past the captured commit index). It asserts nothing about any *future* commit, so there is no
> `commitTS` that could land below it and make its answer retroactively false.
>
> A snapshot read at `T` is the opposite: it is a promise that the state at `T` is what it said, and a
> commit at `commitTS <= T` landing afterwards **breaks that promise**. That is BUG-022, and it is why
> only Kind 1 needs a mark.

**Therefore the three guards are total over both kinds:** Kind 1 is covered by the mark, whose premises
`P` restores; Kind 2 needs no guard, and the argument for that is a property of what a plain read
claims rather than of where it was answered.

### 9a.4 What the restatement rests on, listed so it can expire loudly

Three premises, each with the thing that would break it:

| premise | breaks when |
|---|---|
| **`P`** — every timestamped read is a log entry | a snapshot read is served by read index. **`M71` re-pointed is exactly this**, killed by a conservation failure |
| the read index protocol is correct for the prefix property | the term-start no-op is missing (§2), or leadership is confirmed at the wrong term, or a read is served against an index below the one it arrived at (ruling 3's induced oracle) |
| `startTS != commitTS` | two mints collide — BUG-021's class, asserted at zero as `IdentityCollisions` |

**And the honest limit, stated because it is the shape D-A7-5B would change.** This argument buys
nothing about a *future* phase in which the mark moves off the log. A leaseholder-local timestamp
cache (D-A7-5B) reinstates the problem in a new place: the mark would then be a fact one node
remembers rather than a function of the log, and every clause above that says *staged by every
replica* would need a leadership-handover argument to replace it. That is why B is a phase and not a
decision, and why this restatement is written against A rather than against both.

---

## 10. What this document still owes

Written here rather than in §9 so that the list is short and checkable at exit:

- ~~the **restated** three-guard totality argument (ruling 13)~~ — **written, §9a.** What remains is
  the induction: `M71` re-pointed at `P` negated, and `M72` re-induced against the restated form;
- **D-A7-6 (§3a), open** — how the no-op is represented; it gates the first commit, and option B
  would touch the frozen raft interface;
- ~~the ledger-side oracle for ruling 3's condition~~ — **specified, §5a**, with its independence
  argument and seven induction cases. What remains is building it and the `i - 1` mutant;
- the counted plain-read census field (ruling 9), before any number reaches BENCHMARKS.md;
- the fate of the replicated read path (ruling 4), decided at exit and recorded here.

**No A7 code is written.** The next commit is the term-start no-op, alone, with the re-measurement it
requires.
