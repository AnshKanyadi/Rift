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

### 4.1r REPORTED AT CLOSE: the ten facts, and the count that matters is not ten of ten

**Ten facts named before the code. Ten held as derivations. Three defects landed in the read path and
not one of them is among the ten.**

| # | fact | verdict at close |
|---|---|---|
| 1 | the read index, captured **at arrival** | held — D-A7-3 ruled A and the index is carried through the round |
| 2 | leadership confirmed **at the broadcast's term** | held — `TestAReadIsNotConfirmedByAStaleTerm` |
| 3 | caught up = `applied >= THIS READ'S index` | held — and `M76` plants its removal and is killed |
| 4 | a follower's read reflects **the leader's** index | held — `takeReadStates` adopts it |
| 5 | the no-op's term is the term at `becomeLeader` | held — `noop.Term = r.term`, inside `becomeLeader` |
| 6 | leadership **at the confirming round**, not at arrival | held, on fact 2's mechanism |
| 7 | the applied index a read waits on is **the range's** | **taken from the right place, and the right place was not maintained** — BUG-032 |
| 8 | the read mark, as of every read by either path | held **by exclusion**: D-A7-5 keeps every mark-staging read on the log |
| 9 | which read path, by **promise** not by syntax | held — `!ReadTS.IsSet() && Txn == nil` |
| 10 | a follower holds **no** local mark | held, same exclusion as 8 |

**Exclusions stated:** facts 8 and 10 are held *by construction rather than by test*. D-A7-5 rules that
a read carrying a timestamp keeps its log entry, so nothing that stages a read mark ever reaches the
read-index path, and `M71` re-pointed is that boundary planted as a defect. They are not evidence that
the mark machinery works; they are evidence that read index never touches it.

#### Fact 7 is the honest one, and it is a third failure mode

BUG-032 is not the fact taken from the wrong place. `n.applied` **is** the range's applied index, per
`Replica`, exactly as the table names. The number at that place was not advanced when a snapshot
install moved the state machine.

> **The table asks where a fact is taken from. It does not ask whether that place is kept true.**

That is a dimension neither §4 nor §4.1 has a column for, and it is worth naming because the remedy is
different: the fact table is answered by reading a derivation, the assumption audit by reading an
argument, and this one only by asking **what else writes to the place this fact is read from** — which
for `n.applied` was one branch out of four.

#### And the headline is A6's result repeating exactly

A6 named six facts, none became a defect, and BUG-022 happened anyway. A7 named ten, none became a
defect, and **BUG-026, BUG-028 and BUG-032 happened anyway** — all three in the read path, none in the
table.

> **The table came out clean and the phase's defects were in the column it has no room for. Twice.**

That is the argument for §4.1 existing, and it is the argument that produced §5e: after the second
occurrence, "run a second audit in the form the miss would have been caught by" stops being a
per-phase device and becomes an enumeration the phase owes.

### 4.1s REPORTED AT CLOSE: the assumption audit, re-asked against the code that landed

Seven assumptions, three of which this system does not provide — **and that is still the count**. Each
re-asked against the code at close rather than against the code proposed:

| the assumption | still the verdict? |
|---|---|
| a leader knows its own commit index | **no**, unchanged — the term-start no-op is why, and `NoOpsApplied` is 1,440,422 across the exit run |
| a heartbeat quorum means *this* leader | yes **only at the broadcast's term**, and that is what the code checks |
| a read leaves no trace the system depends on | **no**, unchanged since BUG-022 — D-A7-5 is that assumption failing, and it holds by keeping timestamped reads on the log |
| a follower's applied index is a sound freshness bound | yes — *because a read index is a fact about a position rather than about a node* |
| the term-start no-op is committed before any read in that term | **not automatically** — `readFloor()` is `max(commitIndex, termStart)` and that is the fix, not a property |
| serving a read advances no replicated state | **no if the mark ever moves off the log** — B is a phase, not a decision, and it has not been taken |
| **P-NOOP: `Propose` never issues the zero `ProposalID`** | **yes, and it has NOT silently expired** |

#### P-NOOP, checked rather than assumed

`raft.go:1293` still refuses the zero `ProposalID`. **And the premise now expires loudly at the site
that depends on it**, which is what §4.1a asked for: `becomeLeader`'s no-op carries the sentence *"if
it is ever relaxed, this no-op stops being distinguishable from a client proposal and breaks
SILENTLY"*. The refusal's own doc comment still gives only the original reason — proposal identity —
so the second reason lives at the dependent rather than at the dependency. That is the weaker of the
two placements and it is stated here rather than quietly accepted.

#### The audit's own miss, which is the same shape as the audit's justification

**BUG-028 should have been a row and was not.** D-A7-5's ruling rests on one sentence — *a plain read
has no timestamp to protect* — and that is an assumption in exactly this table's form. Had it been a
row, the question *does this system provide it?* answers **no**: `serveReadyReads` gave the read a
timestamp, from the serving replica's own clock, at arrival.

> **An assumption named in a ruling and not in the audit is an assumption nobody re-asks.**

The audit was built because *naming every fact you take is not the same as naming every fact you
need*. Its own miss is one narrower step: **naming an assumption somewhere is not the same as putting
it where it gets re-asked.** §5e is the answer to both, and it is the answer because it enumerates a
class rather than a list.

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

## 4a. The read index, as two claims rather than as implementation notes

Both are claims this document is accountable for, stated where a reader looking for them will find
them rather than distributed through the code that implements them.

### CLAIM 1: read index costs no additional message

> **In this implementation a heartbeat IS `MsgApp` with no entries, so the confirming round and the
> replication round are the SAME round.** A leader is already sending appends; the read rides one it
> was going to send. Several reads arriving together share it.

The mechanism is the whole of the claim, and it is worth stating because the usual description of read
index — *confirm leadership with a quorum, then read locally* — sounds like it adds a round trip. It
adds none. What it costs is **latency**, not messages: a read is never faster than the heartbeat RTT,
because it must wait for a round that is already in flight to come back.

**BENCHMARKS.md states it in those terms, beside ruling 9's number.** A7's throughput win is on
**one read in ten** (§4.2), so the honest framing is: read index is the CORRECTNESS mechanism
CLAUDE.md's fourth headline claim names, delivered on the path where linearizable reads live, and its
message cost is zero while its latency floor is one heartbeat.

### CLAIM 2: `readFloor()` is `max(commitIndex, termStart)`, and it depends on the no-op

> **A fresh leader never serves a read against an inherited commit index.**

The figure-8 rule forbids committing earlier-term entries by counting, so a leader that has just won an
election does not know its own commit index — it is whatever was inherited, and can be arbitrarily far
behind the true committed prefix. Reading against it misses writes committed before this leader took
office: a stale read produced by the mechanism whose entire job is preventing stale reads.

`readFloor()` returns `max(commitIndex, termStart)`. When the term-start no-op has not yet committed,
`termStart` is higher and the read is stamped there instead; the driver waits a little longer and the
answer is never below the point at which this term's commit index becomes true.

**The dependency is named as a premise, because it breaks silently:**

> **P-TERMSTART. `becomeLeader` appends a no-op in the current term, and `termStart` is its index.**

| premise | breaks when | what happens |
|---|---|---|
| **P-TERMSTART** | the term-start no-op is removed, or `becomeLeader` stops recording its index | `readFloor()` collapses to `commitIndex`, a fresh leader serves against an inherited value, and reads miss writes committed before it took office. **No error. The read succeeds and is wrong** |

A7 §2's argument is why the no-op exists at all; this is the second thing resting on it, and the two
are not the same claim. The no-op makes `commitIndex` become true; `readFloor` is what refuses to trust
it until it has. Remove either and reads go stale — one loudly over time, one immediately and
invisibly.

**It is asserted rather than trusted**:
`TestALeaderDoesNotServeAReadAgainstAnInheritedCommitIndex` requires every pending read's stamp to sit
at or above `termStart` while the no-op is uncommitted, and it is induced by dropping `termStart` from
the floor.

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

### 5a.4b Three instruments on one class, and they are not redundant

A reader counting instruments will see three things pointed at ruling 3 and suspect duplication. They
ask three different questions, and no two of them can answer each other's:

| instrument | where | the question |
|---|---|---|
| `TestAReadIsNeverAnsweredBelowItsStampedIndex` | `raft/` | **can a read be answered below its stamp?** A property of the protocol, at one schedule, deterministic |
| `TestReadIndexAtArrivalSpeaks` | `raftcheck/` | **can the oracle speak?** Seven cases over a synthetic ledger, milliseconds — the instrument's own induction, which must not wait on a sweep |
| `TestAReadStampIsNotBelowAnAcknowledgedWrite` | `store/` | **does the SYSTEM feed that oracle a sound stamp?** A real acknowledged write and a real read through `onClient`, reading the stamp `readFloor` actually chose |

And `read-index-at-arrival` itself is the fourth thing: **the property checked on every run of the
sweep**, which is what none of the three provides. The first is one schedule; the second judges the
judge; the third is one schedule again, at a different layer. Only the oracle is continuous.

> **An instrument that can speak and an instrument that is being fed the right input are separate
> claims**, and the class where they were conflated is §5b's — an oracle that killed its mutant with a
> verdict describing something the mutant does not do.

### 5a.4c `IssuedAt`, and why it is the subtle correctness point

§5a.2 is stated against *"every write acknowledged to a client **before that read was issued**"*. The
convenient timestamp is the one already on the record — `When`, the instant the read was **answered**
— and it is **a whole confirming round later**.

An oracle built on the answer instant would have compared each read against every write acknowledged
before it was *answered*, which includes writes acknowledged **during** the confirming round. Those
are writes the read is entitled to miss: they were not promised to anyone when the stamp was taken.
The check would still fire on `M83` and would still be silent on a clean tree — **and it would be
quietly weaker in a direction nothing would have flagged**, because it would have accepted a stamp
taken at any point up to the answer, which is precisely the option D-A7-3 did **not** rule.

> That is the **guarantee-copied-without-its-reason** class (§5b0, case 3) avoided by reading the
> ruling's own words rather than the timestamp that was already in the struct.

So `ReadRecord.IssuedAt` was added, carried from `onClient`'s arrival instant through `pendingRead`,
for one reason: **the ruling says *issued*, and the two instants differ by exactly the window the
gate is about.**

### 5a.5 The mutant

`i - 1` is the mutation: stamp the read one index low. It is the smallest possible version of the
defect and the one an optimisation would actually produce, and it must be killed by this oracle rather
than by porcupine — **because porcupine can only see it when some client observed the write that was
missed**, which is §5's stated weakness in property 1 and the whole reason property 2 exists.

---

## 5c. The chain: five mechanisms, none aimed at a codec bug, and none a code review

This is the phase's clearest argument for the standards it runs on, and it is worth reading as a
chain rather than as five findings.

| # | step | the mechanism, and what it was added FOR |
|---|---|---|
| 1 | **ruling 2 demanded a non-vacuous census field** for follower reads | added because *a sweep in which every read was served by a leader has not tested them* — a coverage worry, nothing to do with wires |
| 2 | **the field read `follower=0`** | the count itself, which existed only because ruling 2 refused to trust the implementation |
| 3 | **the two-number standard said UNREACHED, not broken oracle** | added one turn earlier because `M76`'s differential had passed review while being wrong. `compared=657, served=657` **identical across both trees** is what pointed away from the oracle |
| 4 | **chasing "unreached" found the DISPATCH** | requests were broadcast to every node and only leaders acted, so no read could reach a follower |
| 5 | **fixing dispatch found the WIRE** | `encodeMessage` carries a fixed field list, and `ReadCtx`/`ReadIndex` were not in it: the message arrived with its type byte intact and its payload zeroed |

> **Five steps, not one of them a code review, and each mechanism was added for a different reason —
> none of them a codec bug.** A coverage rule, a count, a measurement discipline, a dispatch model and
> a serialisation test are five unrelated instruments, and the defect at the end was invisible to every
> test in the tree because every test stopped at the boundary the defect lived on.

**The defect was not subtle and it was not findable by reading.** `raft/` was right, `store/` was right,
and the two disagreed only about what survives a transport — which no test crossed. Careful reading of
either side would have confirmed it correct, because each side WAS correct. BUG-025 carries the general
rule: *a unit test that exercises a mechanism without its serialisation will pass over a wire that does
not work.*

**And it is the second time.** A1's decode off-by-one was the first codec defect hidden this way, six
phases and a different codec earlier. After it, the off-by-one was fixed and nothing was built that
would have caught the next one. The two tests added with BUG-025 are that thing, late.

---

## 5b0. Three ways an oracle fails, and this phase produced one of each

**Ansh, ordering the narrative:** *"the narrative with all three oracle entries in one section — not an
oracle that was wrong, not an oracle that was silent, but a guarantee that was copied without its
reason."*

An oracle is the only thing standing between a green run and a false claim, so the ways it can fail
are worth enumerating rather than meeting one at a time. A7 produced three, and they are genuinely
distinct — different symptoms, different detectors, different remedies.

| | what failed | how it showed | what found it | the remedy |
|---|---|---|---|---|
| **1. The oracle was WRONG** (§5b) | `read-index-answers-match-the-log` compared against `Index` rather than `AppliedAt`, so it would have failed **clean** runs | a mutant's kill carried a verdict describing state *ahead* of the confirmed index — which is not what the mutant plants | `M76`, by the discrepancy between the verdict and the planted defect | compare at the position the node reached; *too fresh is not stale* |
| **2. The oracle was RIGHT and SILENT** (BUG-026) | the same oracle checked *agreement with the log* and nothing checked *ownership of the key* | nothing. The live answer and the replay agreed, both saying "not found" | porcupine, and only because a client happened to observe the write | give it the half it was missing, and enumerate the class (§5e) |
| **3. The GUARANTEE was copied without its reason** (BUG-028) | `serveReadyReads` copied *answered-at-a-timestamp* from `answerAt`, where that is safe **because the entry's timestamp is log-ordered** | nothing either — and both the code and the model asked the same wrong question, so they agreed | **the §5e enumeration**, by listing the property and asking whether read index kept it | ask the question a plain read actually asks: the latest version |

### 5b0.1 Why the third is not a special case of the second

The second is an **instrument** problem: a property nobody assigned to a checker. The third is a
**code** problem that *produced* an instrument problem — and it is the more dangerous of the two,
because the wrong question propagated.

`ReadAt(key, ts)` returns *the newest version at or before ts*. The replicated path passes it the
entry's timestamp, which is log-ordered and therefore above every earlier write's, so the answer is
the latest. `serveReadyReads` passed it a clock reading, which is not. **Both sites look right in
isolation**, and `answerAt`'s comment — *"answered at its own timestamp… not at the newest version"* —
is true where it is written and false where it was copied.

> **Copying the shape of a guarantee is how you lose it quietly.**

And the model inherited the mistake: `ValueAtIndex` also read at the recorded timestamp, so the oracle
and the system were wrong in the same way and agreed. Correcting the model to read the **latest**
version is what made the defect speak — and the first thing it said was a **false accusation** on seed
36, which turned out to be BUG-032. Three instrument corrections in one phase, each of which exposed
the next.

### 5b0.2 What the three have in common, which is the part worth carrying

None of them was found by reading the code. §5b was found by a mutant whose kill was *too good*;
BUG-026 by a client that happened to observe the write it missed; BUG-028 by an enumeration; BUG-032
by chasing an accusation instead of tuning the accuser.

> **An oracle's failure is not visible from inside the oracle.** Every one of these was found by
> something outside it — a mutant, a client, a table, a contradiction — and the practical consequence
> is that an instrument needs an instrument, which is what §8.1b's two-numbers rule and §5e's
> enumeration are both for.

### 5b0.3 The third case has a property the other two lack, and it is why it is worse

In case 1 the oracle disagreed with a correct system. In case 2 it agreed with a broken one because it
was never asked the relevant question. **In case 3 the oracle and the system were wrong *identically*,
so there was no disagreement to be found** — the model read at the recorded timestamp because the code
read at the recorded timestamp, and comparing them returned "agree" every time.

This is **`GF-22`, which is Track B's** — cited here across the track boundary and not owned by this
document. Track B states it as *two defects whose symptoms cancel are invisible to every test that
asserts an ANSWER, because the answer was right — arrived at by two errors that annihilate*, and
`GF-25` restates it one level down: **when the answer is right for an accidental reason, only an
assertion about the mechanism can tell you.**

A7's instance is the same shape with the errors pointing the **same** direction rather than opposite
ones, and the consequence is sharper than cancellation:

> **A failure in which both sides of a comparison are wrong the same way survives any check built on
> comparison.** Not because the check is weak — because the check's question has been answered
> consistently by two things that share the mistake.

**What it took to break it was a question asked from outside the pair.** Not a better differential, not
more seeds: a table with a row for *"the read's timestamp is log-ordered"* and the question *does read
index preserve this?* — which neither the code nor the model is consulted about. **That is what §5e's
enumeration is**, and it is why the enumeration is an exit criterion rather than a document.

### 5b0.4 The chain, stated as a count

Three instrument corrections in one phase, each exposing the next, and the order matters:

1. **`M76` corrected the oracle** — it had been comparing at `Index` rather than `AppliedAt` and would
   have failed clean runs (§5b).
2. **The §5e enumeration found BUG-028**, and fixing it required correcting the *model* to read the
   latest version rather than at a recorded timestamp — which broke the identical-wrongness above.
3. **The corrected model immediately made a false accusation** on seed 36, and chasing it down rather
   than tuning the accuser found **BUG-032**.

> Each correction made the next defect *speak*. An instrument that has just been fixed is the most
> informative thing in the repository, and the first thing it says is worth taking seriously even when
> — especially when — it appears to be wrong.

---

## 5b. A mutant found a defect in the instrument aimed at it

Every prior instance in this project is a planted defect revealing something about **coverage** or
**reachability** — a class nothing detects, a schedule that cannot produce a condition, a test that
goes around the path. `M76` is the first that revealed the **checker built to catch it was wrong**,
and wrong in the direction that would have failed clean runs.

### 5b.1 What happened, and what the signal was

`M76` plants §1.1's second condition removed: a read answered as soon as its leadership quorum
confirms, **without waiting for this replica's own applied index to reach the confirmed read index**.
`read-index-answers-match-the-log` — the differential built for exactly this — reported a violation.
Exit status said killed.

The verdict text said something else:

```
range 3: node 2 answered a read of "k06" OFF THE LOG with "v78" (found=true),
having confirmed it could serve at index 1 -- but the committed log at index 1
holds "" (found=false)
```

**The node answered with MORE than its confirmed index holds.** That is not what `M76` plants. `M76`
makes a node answer from state that is BEHIND; this verdict describes state that is AHEAD.

> **The kill was real and the reason was wrong.** The oracle was comparing the answer against the log
> at the CONFIRMED INDEX, so a node that had applied past that index and legitimately returned a newer
> version was reported as a violation. It would have failed correct runs.

### 5b.2 The general form

> **A planted defect tests the CHECKER as much as the code.** A kill is evidence about both, and it is
> only evidence about the code if the verdict describes the defect that was planted.
>
> **A kill whose verdict text does not describe the planted defect is a finding about the checker,
> regardless of the exit status.**

Nothing in the exit status carries that. `make mutants` reports `killed`, the lane goes green, and the
discrepancy lives entirely in prose that no lane reads. It was noticed because the message did not
match the format the oracle was written to print — which is a thin thread, and the reason this is
written down rather than fixed quietly.

### 5b.3 The property splits in two, and only one half is about agreement

The conflation is the defect. Stated as two named halves, each with its own induction:

| half | what it says | what breaks it |
|---|---|---|
| **`AppliedAt >= Index`** | **the read WAITED.** The confirming quorum establishes a POSITION — that this leader was still leader at or after the read arrived. It says nothing whatever about whether THIS node has got there | `M76` |
| **the answer matches the log at `AppliedAt`** | **the node's state is the log's state**, at the position the node ACTUALLY REACHED | a replica whose state machine has diverged from its own log |

**Why comparing against `Index` conflates them and makes the second half wrong.** `Index` is a
*floor*, not a description of the answer: the read may be served from any state at or above it. A
node that has applied past `Index` holds versions the log at `Index` does not, and returning one is
correct. Demanding equality at `Index` turns that correctness into a violation, which is BUG-016's
standard applied to the phase's own new oracle.

> **Too fresh is not a violation.** A later state is never a linearizability failure — the read
> reflects everything committed before it was issued, and more besides. **The next person to touch
> this oracle will reach for `Index`, because it is the number the protocol is named after**, so the
> case is kept as a permanent fixture rather than as a comment: *applied past the confirmed index,
> answered with the newer version — NOT a violation.*

### 5b.3b `M76`'s numbers, and the two readings that keep them from being misread

| tree, 40 seeds | caught | compared | served | follower |
|---|---|---|---|---|
| clean | **0** | 598 | 599 | 101 |
| `M76` | **1**, first at seed 27 | 588 | 599 | 101 |

**`served` and `follower` are IDENTICAL across the two trees.** The mutation changed only whether an
answer was *correct*, not how much work happened — so the difference between the runs is confined to
the thing under test rather than to a workload that drifted. A mutant that moved the workload would
make the comparison meaningless, and this one demonstrably did not.

**`compared` differs by exactly ten, and that is the HALT rather than a coverage gap.** The run stops
at the first violation, so the ten off-log answers after seed 27 were never reached. Read without that,
`588 < 598` looks like the oracle checking less on the tree where it matters most; read with it, the
shortfall is the oracle having already done its job.

> **A number reported without its readings is a number somebody will misread later**, and these two are
> exactly the kind that invite the wrong conclusion — one suggesting drift that did not happen, one
> suggesting a gap that is a halt.

### 5b.4 And the standard every A7 oracle is held to

Ansh, on this: *every A7 oracle gets the same two-number treatment before it counts — fires on its
planted defect, silent on the clean tree, both at a stated seed count.*

Not because the others are doubted. Because **this one passed a design review, a ruling, and three
separate assertions that it worked**, and the only thing that distinguished it from a working oracle
was a number nobody had taken:

| claim made | what was actually true |
|---|---|
| *"the oracle exists and speaks"* | it was in a list nothing invokes — `sim.Oracle` has no `Check()`, and `compared` read **0** on every seed |
| *"`M76` is caught, 7 of 12"* | caught by `MVCCReadCorrectness`, which was consuming off-log records and comparing them against expectations keyed by unrelated read entries |
| *"the oracle is sound"* | it compared against `Index` and would have failed clean runs |

Three wrong claims, three mechanisms that caught them — the oracle's own non-vacuity counter, a
verdict format that did not match, and a mutant. **None was caught by reading the code**, and the code
was read carefully each time.

---

## 5d. A detection floor is a property of the class AND the shape, jointly

The phase's most useful methodological result, and it is a general statement rather than a story about
two mutants.

**The measurement.** `M71` and `M72` are BUG-022's two halves, each floored at 1 detection in 200
seeds.

| shape | measured | the lane's verdict |
|---|---|---|
| **A6** | **0 of 200** | `DROPPED` and `SLOWED` — the floor breached and no first detection at all |
| **A7** | **22 of 600, first at seed 30** | comfortably inside the old window |

Between those two rows, **both classes were killed by hand at seed 266** — on the re-pinned BUG-022
bundle, within the hour, by conservation failure. Neither was dead at any point.

> **A detection floor is not a property of a class. It is a property of the class and the SHAPE,
> jointly.** The same class measures dead under one shape and healthy under another, and a floor
> recorded without its shape is not a measurement — it is a number that was true of a sweep nobody
> can now identify.

### 5d.1 The ceiling is what surfaced it

A count-only lane reads a rate. `M71`'s rate under A6 was **zero**, and a rate of zero is
indistinguishable from a class that has stopped existing — which is exactly the conclusion a reader
reaches, and the conclusion that leads to an opt-out.

**The ceiling asked a different question**: *where is the FIRST detection?* Under A6 the answer was
`-1` — nowhere in 200 seeds — while the true first detection sat at 266 under one shape and 30 under
another. A rate cannot express "the class moved"; a first-detecting seed can.

> **Two live classes were one step from being opted out**, and the step was reasonable: a floor of 1
> in 200 measuring 0 of 200 twice over is ordinarily how you learn a class is unreachable. **That is
> the `M56` shape arriving through a floor rather than through a sentence** — a claim of
> unreachability, supported by a real number, and false.

This is the ceiling's **first independent catch**. A2's amendment added it after `M19`'s kill-time
regression was found by accident; here it caught one on its own, before anything was written down
about the class.

### 5d.2 What was done about it

Both re-declared from the current measurement, with their shape stated: `power-seeds: 600`,
`power-floor: 11`, `power-ceiling: 120` — half the measured rate and four times the first detecting
seed, the margins `M62` carries. **All 65 declarations now carry the shape their numbers came from**,
which is the fix for `power-config: a3`'s drift applied one level in: not just naming the shape a
number was taken under, but refusing a number that does not.

**And it changed how a later disposition was taken.** `M34`'s floor is unmet by two independent
instruments — 0 of 3000 in the gating lane, 0 of 6000 in BUG-009's re-pin search — which before this
result would have been enough to correct its declaration. It is instead being measured under A7's
shape first, on exactly the reasoning above: **an unmet floor is not evidence of an unreachable
class** until the shape has been varied.

---

## 5e. What a read gets for being a log entry — the enumeration **[EXIT CRITERION]**

**Ansh, on BUG-026:** *"At D-A7-5 I ruled that reads leaving the log was A7's governing constraint,
and we guarded the read mark because BUG-022 depended on reads being log entries. The ownership check
is a different member of that same class and nobody enumerated the class. One member cost 526 seeds;
I want the list rather than the next member."*

So this is the list. Every check that runs on a plain read **because the read is an entry in the log**,
and for each: does read index **preserve** it, **replace** it, or **drop** it.

**The marks are evidenced, not asserted.** *Preserved* means a test shows the check still runs under
read index. *Replaced* means the replacement is named and induced. *Dropped* means the consequence is
stated and either accepted with a reason or fixed. An enumeration whose entries are unevidenced is a
list, not a finding.

| # | what the entry buys | read index | evidence |
|---|---|---|---|
| 1 | **extent ownership** — apply asks `desc.Contains`; outside it, `rerouteAt` | **was DROPPED → now REPLACED** | BUG-026. `serveReadyReads` checks the extent at answer time and reroutes. Induced by `M78`; caught by `read-index-answers-match-the-log` HALF THREE, itself induced in `TestReadIndexAgreementSpeaks` |
| 2 | **the read's timestamp is log-ordered** — the leader stamps the entry, entries apply in order, so a read's stamp is above every earlier write's | **was DROPPED → now REPLACED** | BUG-028. A plain read names no timestamp: `kv.ReadLatest`. Induced by `M79`; caught by the same oracle once its model read *latest* for an off-log answer |
| 3 | **the answer matches a proposal by identity, never by position** (BUG-004) | **REPLACED** | `raft.ReadIndex` refuses an empty context precisely so arrival order cannot become an identity; `TestReadIndexRefusesAnEmptyContext` |
| 4 | **staged writes are flushed before a get is answered** — a read at a position sees that position's state | **PRESERVED** | `serveReadyReads` runs after the apply block's own `flushApply`, so no batch is outstanding when it reads. `TestOneApplyPath` pins the single apply path this rests on |
| 5 | **the GC-mark refusal** — a read below the mark is refused, not answered with an older state | **REPLACED, and deliberately narrower** | A plain read names no timestamp, so there is nothing for the mark to be below; `ReadLatest` does not consult it. The mark still refuses every *snapshot* read, which is what it exists for |
| 6 | **the read mark** — BUG-022's third first-committer-wins guard, staged as an applied effect | **NOT APPLICABLE, and this is the one D-A7-5 already guarded** | Read marks are staged only in `kv/txn.go`: a plain read stages none. A read carrying a timestamp keeps its log entry, so nothing that stages a mark reaches this path. `M71` re-pointed is this boundary negated |
| 7 | **total order against concurrent writes** — the entry sits at a definite position | **REPLACED** | The confirmed index plus `applied >= index`. Both halves are asserted by the oracle; `M76` removes the second and is killed |
| 8 | **leadership** — only a leader may propose | **REPLACED** | The confirming quorum, and for a follower the leader's index. `D-A7-1`; a read recorded under a term this node has left is not confirmed (`TestAReadIsNotConfirmedByAStaleTerm`) |
| 9 | **the descriptor epoch at arrival** | **PRESERVED — shared** | Both paths enter through `Node.onClient`, which refuses a stale descriptor before either branch. `StaleEpochRefusals` is nonzero on every sweep |
| 10 | **durability of the answer across a crash** | **NOT APPLICABLE** | A read mutates nothing. The messages that *carry* a read index do attest to a term, which is BUG-027 |

### 5e.1 What the enumeration cost and what it returned

Entry 1 was the defect that prompted it: 526 seeds, found by porcupine. **Entry 2 was found by
writing entry 2** — listing the property and asking whether read index kept it. It had been live
since `90a4844`, it is invisible to porcupine in a quiet history, and it was masked in the full sweep
by entry 1's much higher rate. No further sweep found it and no code review did.

> **A class is cheaper to enumerate than to meet one member at a time.** The first member costs a
> 25,000-seed run; the second costs a table row.

### 5e.2 The shape the two dropped entries share

Both are checks the replicated path gets **for free from the apply loop**, and both were dropped by
the same structural fact — *read index has no entry, so it reaches no apply, so it reaches no check.*
Entry 2 is the sharper of the two because the implementation did not merely omit the property, it
**kept the form and lost the content**: `answerAt`'s comment says a replicated read is answered *"at
its own timestamp… not at the newest version"*, which is right there because the entry's timestamp is
log-ordered. `serveReadyReads` copied "answered at a timestamp" and inherited none of what made it
safe.

> **Copying the shape of a guarantee is how you lose it quietly.** The code looked like the code that
> was correct.

### 5e.2b Four axes, one question: is this mutant aimed where the defect lives?

Two of the four classes this phase declares measured **zero** on their first attempt, and neither zero
was about the harness's reach:

| class | first attempt | true figure | the claim was aimed at the wrong… |
|---|---|---|---|
| `M80` | response side only, **0 of 400** | 3 of 400 with the request side | **role** — the response is sent by a leader, whose term is already durable |
| `M79` | clock read at answer time, **0 of 600** | 74 of 600 at arrival time | **point in time** — by answer time the replica has absorbed the leader's clock |

> **A mutant is a claim about where a defect lives, and a zero can mean the claim is aimed at the
> wrong point rather than at the wrong line.**

**This is not a new question, and the oldest instance of it is not in this track.**

**`BM55-tables-oldest-first` is Track B's** — B2's version-set work, cited here across the track
boundary and not owned by this document. Its own record states it as *"the patch was aimed at a line a
comment claimed was load-bearing and was not"*, and the question it left behind is: **is the line this
patch is aimed at actually the line that carries the property?** The comment asserted a correctness
guarantee for a line that turned out to be a cost guard.

So there are four axes and they are siblings rather than one thing:

| axis | the question | who asks it |
|---|---|---|
| **line** | is the line this patch changes the line that carries the property? | `BM55` — **Track B**, by hand, after a surviving mutant |
| **code position** | does the covering test *execute* the line the patch changes, or go around it? | `make mutant-covered`, mechanically, every push |
| **role** | is the defect reachable on the actor this patch modifies? | nothing — `M80` found it by a second attempt |
| **time** | is the defect reachable at the moment in the run this patch modifies? | nothing — `M79` found it by a second attempt |

> **Four axes, one question — *is this mutant aimed where the defect lives* — and no single lane asks
> more than one of them.**

`make mutant-covered` is the only one mechanised, and it covers the axis that is easiest to mechanise
because coverage data already answers it. The other three are answered, when they are answered at all,
by somebody being unconvinced by a zero and attacking the same class from a different direction. That
happened twice this phase and once in Track B, which is three occurrences and not a coincidence.

**The rule this yields is narrow and it binds now:** *a zero is not an opt-out until the aim has been
varied*, in the same way §5d says *a floor is not a measurement until the shape has been named*. An
unmet floor already had to survive a change of shape before becoming an exemption; it now has to
survive a change of aim, and the four axes above are the checklist.

### 5e.3 The standing obligation this creates

Any future path that answers a client without going through the log — and D-A7-4 keeps both paths for
exactly this reason — **restates this table before it ships**. The table is the deliverable, not the
two fixes.

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

### 8.1b Every A7 oracle carries two numbers before it counts

Ansh, after `M76` found a defect in the oracle aimed at it:

> **Every A7 oracle gets the same two-number treatment before it counts: fires on its planted defect,
> silent on the clean tree, both at a stated seed count.**

Not a restatement of the induced-failure rule. That rule asks only the first number, and the
differential oracle **passed it** — it fired, on a real mutant, with a real verdict — while being
wrong in a way that would have failed clean runs. The second number is what separates *an oracle that
fires* from *an oracle that is right*, and it is BUG-016's standard applied where it is hardest to
apply: to one's own instrument.

| oracle | fires on | silent on the clean tree |
|---|---|---|
| `read-index-answers-match-the-log` | `M76` | required at a stated seed count |
| the ledger-side read-index bound (§5a) | a read stamped one index low | required |
| `M71` re-pointed (ruling 11) | a snapshot read served by read index | required |

**A green with no baseline is not a result** (§16.3b) is the same sentence one level in: an oracle that
has never been shown silent has no baseline for its own firing.

#### The two demonstrations, both from A7

**One — it distinguished an unreached mutant from a broken oracle.** `M76` read `caught=0`. On its own
that is a checker that does not work. With the second number it was not:

```
clean:  caught=0  compared=657  served=657
M76:    caught=0  compared=657  served=657
```

**Identical in every figure**, which says the mutation changed nothing observable — UNREACHED, not
undetected. That sent the investigation to dispatch instead of to the oracle, and dispatch led to the
wire (§5c). Had the first number been read alone, the oracle would have been rewritten and the codec
defect would still be there.

**Two — it caught an oracle that fires and is wrong.** The differential passed the induced-failure rule
on its first try: it fired, on a real mutant, with a real verdict. It was still comparing against
`Index` instead of `AppliedAt` and **would have failed clean runs** (§5b). The induced-failure rule
asks only *does it fire*. The second number asks *is it silent when it should be*, and only the second
separates an oracle that works from one that merely speaks.

> **The first demonstration is the one people will quote and the second is the one that matters.**
> Distinguishing unreached from broken is a diagnostic convenience. Catching a checker that would fail
> correct runs is the difference between a verification claim and a false one.

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
7. **§5e's enumeration, with every entry evidenced.** Added by Ansh on BUG-026: *"The ownership check
   is a different member of that same class and nobody enumerated the class… One member cost 526
   seeds; I want the list rather than the next member."* Every check a plain read gets **because it is
   a log entry**, marked preserved / replaced / dropped — where *preserved* means a test shows the
   check still runs, *replaced* means the replacement is named and induced, and *dropped* means the
   consequence is stated and either accepted with a reason or fixed. **An enumeration whose entries
   are unevidenced is a list, not a finding.** It has already returned BUG-028, which no sweep and no
   review found.

### 8.2r RULED: `make mutants` is INVALID at close, and the gate is ruled rather than waived

**Ansh, 2026-08-27:** *"A7 signs without `make mutants` green, and the gate is ruled rather than
waived. The distinction matters, so here is the reasoning on the record."*

**The state, stated plainly.** `make mutants` reports:

```
INVALID  the unpatched tree does not pass the packages these mutants target.
         panic: test timed out after 2h0m0s
```

**Four things the next person needs, and they are separable:**

**1. It is INVALID because its baseline cannot finish — not because a mutant survived.** The lane
refuses to attribute anything to a tree it could not first watch pass, which is the lane working. No
mutant has been reported ALIVE. The failure is upstream of the question the lane exists to ask.

**2. No A7 class is unverified by it.** Each of the nine classes this phase added — `M74`, `M76`–`M82`
and `M34`/`M46` re-pointed — was **induced by hand and confirmed individually**, and
`make mutant-covered ONLY="<id>"` is green for each. What the lane cannot do is run all seventy in one
pass inside any timeout a person would set. That is a statement about the *suite*, not about any
class in it.

**3. The cause is inherited, and it is measured.** Eight sweep-based covering tests from A1 through
A6 — ~1,928 seeds, listed by name and class in CARRY-FORWARD — put the baseline over **two hours of
monotonic time**, before any of the seventy per-mutant runs. A7 converted nine covering tests from
sweeps to directed tests, taking the lane from **six days to a two-hour baseline**; it did not create
the residue and cannot honestly remove it, because **writing directed replacements for classes whose
defects you have not studied is how a covering test comes to name the wrong assertion** — which is
exactly the failure this phase spent itself finding in `M34` and `M46`.

**4. What would make it green.** Converting those eight, each at the phase that next touches its
class, alongside work that already requires understanding the defect. That is recorded as a named,
dated obligation with the measured number attached rather than an estimate.

#### Why the 8-hour timeout was refused

`scripts/mutants.sh` prescribes its own remedy — *"The remedy is to give it enough time, never to
shorten the sweeps"* — and a future reader **will** reach for it, so this is written where they will
be standing:

> **Raising the timeout would make the number pass without making the claim true.**

The prescription is **right in direction and does not scale**. It is correct that shortening a sweep
is worse than waiting for it; it is not correct that waiting is a remedy for a lane whose cost grows
with the *number* of sweeping tests. The Makefile's own header already says why: *the cost is driven
by the number of sweeping tests, not by any one bound.* At eight sweeps the answer is hours; the
answer to the next eight is not a bigger number.

#### And the difference this section exists to make

> **A gate that cannot pass for a stated reason, recorded, is different from a gate quietly excluded —
> and the difference is that this one is written where the next person meets it.**

Not in a commit message, not in a handoff that expires, and not in the sign-off's silence: in §8, next
to the criteria it belongs to, with the cause, the scope, the measurement and the remedy.

---

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

## 10. What this document still owes — CLOSED AT EXIT, with one item OPEN

The pre-code version of this list ended *"No A7 code is written."* It is closed here item by item,
and one of them is **not** discharged.

| owed | state at close |
|---|---|
| the restated three-guard totality argument (ruling 13) | **written**, §9a — and `M71`/`M72` re-induced against the restated form, both measured at **22 of 600, first at seed 30**, under A7's shape |
| **D-A7-6 (§3a)** — how the no-op is represented | **ruled A** and landed: `EntryNormal`, nil `Data`, the zero `ProposalID`. The frozen interface was not opened. `P-NOOP` is a named premise in §4.1a and has been checked at close (§4.1s) |
| the counted plain-read census field (ruling 9) | **landed** — `ReadsServed`, `FollowerReads`, `ReadAgreeCompared`, `ReadsOutOfExtent`, all reported by the exit run |
| the fate of the replicated read path (ruling 4) | **decided: it stays.** It is the differential oracle's other half, and BUG-026 and BUG-028 are both cases where the comparison between the two paths is the only thing that could have spoken. Removing it would have removed the instrument |
| **the ledger-side oracle for ruling 3's condition (§5a), and its `i - 1` mutant** | **BUILT AND INDUCED at exit** — `read-index-at-arrival`, `M83`. See §10.1 |

### 10.1 The item that was open, and the rule that stays regardless

**It was open, it was flagged rather than folded into a sign-off, and Ansh ruled: build it.** Keeping
the sentence here regardless of the outcome, because it is the general form and it will apply again at
I1 and I2:

> **A gate that is half met and describes itself as met is worse than one that is half met and says
> so.**

**What was missing.** §8.1 lists ruling 3 as one of the five gates a run can fail, with its evidence
*"an oracle over the ledger and induced, not argued in prose."* The condition was induced
(`TestAReadIsNeverAnsweredBelowItsStampedIndex`), and the oracle was not built — §5a.2's property was
asserted by none of the eighteen oracles in `raftcheck`, and the `i - 1` mutant of §5a.5 was not
planted. A directed unit test establishes the property **at one schedule**, and §5's argument for
wanting property 2 at all is that *porcupine sees a stale read only when some client observed the
write it missed*.

**What was built.**

- **`read-index-at-arrival`**, an oracle over the ledger asserting §5a.2 exactly: *for every read
  answered off the log at stamped index `i`, every write acknowledged to a client before that read was
  issued occupies a log index at or below `i`.* It needed two new boundary observations —
  `WriteRecord` (the log position that answered a client, taken from the entry) and `ReadRecord.
  IssuedAt` (arrival, which in this simulator is issue) — both recorded where the answer crosses to
  the client, so §5a.3's independence argument holds: nothing is read back out of a state machine.
- **Scope: same range only.** Log indices are per-range; after a split the right-hand range starts a
  fresh log, and comparing across them would manufacture violations out of correct behaviour.
- **`M83-the-read-index-is-stamped-one-low`**, §5a.5's mutation exactly: `readFloor()` returns one
  index below the commit index.

**§8.1b's two numbers, both at 60 seeds:**

| | |
|---|---|
| fires on the plant | **58 of 60, first at seed 0** |
| silent on the clean tree | **0 of 60** |
| non-vacuity beside the silence | **3,469** (read, already-acknowledged-write) pairs compared across those 60 clean seeds |

The third row is the one that makes the second mean anything: a silence over comparisons that happened
rather than over an oracle that never reached one.

**Two things the lane corrected while this was built**, both worth keeping:

1. The first version of `M83` **only added lines**, and `mutant-covered` reported `SKIP`.
   **That is the lane answering correctly, not failing to answer** — an addition-only patch has no
   deleted-or-replaced run, so there is no line whose coverage can be required, and the question
   *"does the covering test execute the line this patch changes"* has no subject. **A mutant that only
   adds is asking a different question from one that replaces.**

   > **Recorded because `SKIP` reads like a gap.** A future author will see it, take it for a hole in
   > the lane, and add a rule that makes the lane answer a question the patch did not ask — which
   > would turn a precise verdict into a false one. The remedy when `SKIP` appears is to look at the
   > patch, not at the lane.

   Rewritten as a replacement of `return r.commitIndex`, which poses the question.
2. The covering test was first pointed at `store/`, where a directed test drives a real acknowledged
   write and a real read through `onClient`. `mutant-covered` **skips a cross-package pair** — the
   patch is in `raft/` — so the declaration names ruling 3's own induction, in the patch's own
   package, and the store-level test stands beside it as a third instrument.

**Three instruments now stand on this class, asking three different questions:** can a read be
answered below its stamp (`raft/`), can the oracle speak (`raftcheck/`, seven cases in milliseconds),
and does the system feed that oracle a sound stamp (`store/`, through the real `readFloor`).
