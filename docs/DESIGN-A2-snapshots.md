# DESIGN-A2: snapshots, pre-vote, leadership transfer

**Status:** written before the code, decisions taken under the standing cadence ruling of 2026-08-18
(*whole phases per cycle, no mid-phase check-ins, assume ratification for anything meeting the
standing rules*). Every decision below records its rejected alternatives so an overturn costs a
sentence rather than an excavation. **Author:** Claude. **Decider:** Ansh.
**Phase:** A2. **Depends on:** A1, signed.

---

## 0. The cadence ruling and CLAUDE.md's design gate

CLAUDE.md's Roles section says a phase begins with a design document and then *"STOP and wait for
Ansh's decision."* The cadence ruling says *"no mid-phase check-ins ... report at the end of A2, not
during ... assume ratification for anything meeting the standing rules."*

These cannot both be executed literally, and the newer ruling resolves it in one direction only: a
stop-and-wait after the design document **is** a mid-phase check-in. So this document is written
first, as the standing rule requires, with problem, candidates, tradeoffs and recommendation for each
decision — and then executed rather than paused on. The rejected alternatives are recorded in full
precisely because nobody ruled on them at the time.

Decisions taken this way are marked **[assumed]**. They are listed together in §7 so a ruling can
overturn any of them without reading the rest.

---

## 1. What A2 adds, and the one thing that makes it dangerous

Three features, and they are not equally risky.

**Snapshots** change the shape of the log. Every piece of index arithmetic in `raft/` currently
assumes `log[i-1]` is entry `i`; after compaction that is false, and the failure mode is an
off-by-one that silently reads the wrong entry. A1 has already produced two bugs of exactly this
family — a proposal identified by its log index (BUG-004) and durability inferred from a slice's
emptiness (BUG-005) — so the design here is chosen to make positional reasoning impossible rather
than careful.

**Pre-vote** adds messages that mutate nothing. It is the easy one, and its risk is the opposite:
that it gets asserted rather than measured.

**Leadership transfer** adds one message and one bypass of the election timeout. Its risk is that the
bypass is exactly the thing the election timeout protects.

**InstallSnapshot racing appends and restarts is the danger zone** (CLAUDE.md, A2). The design is
arranged so that race has a name, a gate and an oracle rather than a comment.

---

## 2. D-A2-1: the log under compaction — an explicit offset, not a dummy entry **[assumed]**

**Problem.** After compacting through index *S*, `raft` no longer holds entries `1..S`. Every
`log[i-1]`, `lastIndex`, `termAt`, `matches`, `truncateFrom` and the `Ready` slicing has to keep
working, and `termAt(S)` must still answer — the consistency check for the first entry after a
snapshot asks for exactly that.

**Candidates.**

1. **Dummy entry at position 0.** Keep `log[0]` as a placeholder carrying `(S, snapTerm)` with no
   data, so `log` is never empty and the existing arithmetic mostly survives. etcd/raft does this.
2. **Explicit offset fields.** `snapIndex`/`snapTerm` beside the slice; `log[0]` is entry
   `snapIndex+1`; every access goes through `at(i)` and `offset(i)` helpers.
3. **Index-keyed map.** `map[Index]Entry`. Removes arithmetic entirely.

**Tradeoffs.** (1) is the smallest diff and the most dangerous shape this repository has: a value
whose meaning is positional and whose presence is load-bearing, which is the class behind BUG-004
(*a log index is not a proposal identity*) and BUG-005 (*a fact inferred from an incidental
property*). A reader who forgets the dummy is off by one everywhere, and nothing objects. (3) is
disqualified outright: `raft/` is in core determinism scope and a map here would be iterated.

**Recommendation, taken: (2).** Two named fields, and every read of the slice goes through a helper
that converts an index to a position or reports that the index is compacted away. The arithmetic is
concentrated in three functions instead of scattered across nine call sites, and the compacted case
is a distinguishable answer rather than a wrong one.

**Consequence recorded up front:** `at(i)` must be able to say *"that index is behind the
snapshot"*, and every caller must handle it. A helper that returns a zero `Entry` for a compacted
index would reintroduce exactly the inference this design is avoiding.

---

## 3. D-A2-2: two persist streams, and the second answer `markFor` has been waiting for **[assumed]**

**Problem.** A follower that installs a snapshot must not acknowledge it before the snapshot is
durable. DR-8 enumerated this gate before `raft/` existed:

> **MsgAppResp following InstallSnapshot** — gated on: snapshot durably installed, including its
> config and applied index. Without it: node acks the snapshot, crashes before it is durable,
> restarts with an empty/older log while the leader has already advanced Next past the snapshot
> index ⇒ silent hole in the log.

**Candidates.**

1. **One mark.** The snapshot write and any log write share the pending mark.
2. **A separate snapshot mark.** The snapshot install has its own `PersistMark`, acknowledged
   independently.

**Tradeoffs.** (1) is smaller and is exactly BUG-006's shape one phase later: one token standing for
more than the acknowledgement means, so a completion for one write releases messages attesting to
the other. A2 would rediscover a bug A1 already paid for.

**Recommendation, taken: (2).** The snapshot install is a distinct durability point with its own
mark.

**This is the decision that makes A1's two-mark gate live, and that is the point.** `markFor(idx)`
has had one answer all through A1 — the open log mark — which is why the *later of the two marks*
gate in `stepApp` was reported as changing no verdict. With a snapshot in flight it has a second:

| index | covered by |
|---|---|
| `idx <= tail.persisted` | nothing; already durable |
| `idx <= snapIndex`, snapshot install not yet acknowledged | **the snapshot mark** |
| otherwise | the open log mark |

The case that bites is a **duplicate append arriving while a snapshot install is in flight**. It
dirties nothing, so the log mark is zero; it acks an index the snapshot covers; and without the
second answer the acceptance is released with the snapshot still unwritten. That is the gate, it is
now reachable, and A2 induces it with a planted violation rather than restating the rule.

---

## 4. D-A2-3: who owns snapshot bytes **[assumed]**

**Problem.** `raft/` is pure: no I/O, no state machine, no idea what a key is. Snapshots are a state
machine concept and a storage concept.

**Candidates.**

1. **raft holds the bytes**, handed in and out through `Step`/`Ready`.
2. **raft holds metadata only** — `(index, term, digest)` — and the driver owns the bytes in the
   engine, calling `raft.Compact(index, term)` once its own snapshot is durable.
3. **A `Storage` interface** raft calls into, etcd style.

**Tradeoffs.** (3) breaks purity: `raft` would call outward mid-`Step` and the deterministic
simulation argument rests on it not doing that. (1) keeps purity but puts an unbounded blob inside a
state machine that is copied, compared and asserted over, and it makes `Ready` carry a payload whose
size is a state machine detail.

**Recommendation, taken: (2).** `MsgSnap` carries metadata and an opaque `[]byte` the state machine
supplied; `raft` treats the bytes as opaque, surfaces them once in `Ready.Snapshot`, and never stores
them. `raft.Compact(index, term)` is the driver's statement that it has a durable snapshot at that
point and the prefix may go. **Compact is refused above the applied index** — discarding a log prefix
the state machine has not consumed is unrecoverable, and a refusal is cheaper than an invariant.

---

## 5. D-A2-4: pre-vote gets its own message types **[assumed]**

**Problem.** Pre-vote needs a request and a response that do not mutate persistent state.

**Candidates.** (1) A `PreVote bool` on `MsgVote`. (2) `MsgPreVote`/`MsgPreVoteResp` as new members
of the closed enum.

**Tradeoffs.** (1) is fewer types and one more field on a wire struct that is already fixed-width.
It also makes the message's meaning depend on a flag rather than on its type, which the codec rules
refused once already, and it makes the *ungated* case a special case of a gated one — the exact
adjacency where a future change silently starts gating or stops.

**Recommendation, taken: (2).** Separate closed-enum members, exhaustively switched, so the compiler
names every site that must decide what a pre-vote is.

**Pre-vote is deliberately NOT gated**, and DR-8 already carries the correctness argument: pre-vote
mutates no persistent state, so a pre-vote grant attests only to volatile facts a crash is permitted
to forget. `raft/` already has the test that keeps this honest — a pre-vote step must leave
`HardState` unchanged — and A2 makes it real rather than hypothetical.

### The single cut is why pre-vote exists, and A2 measures it

DESIGN-A0.7 blessed directed partitions with a forward binding, and A1 honoured half of it: *A1's
schedule mix weights the single-cut send-without-receive geometry.* A2 spends it.

A **single** directed cut leaves a node that can send but not receive. It never hears a heartbeat, so
it campaigns; its `MsgVote` reaches the cluster and carries a term above everyone else's, so the
whole cluster steps down and re-elects; it still hears nothing, so it campaigns again, one term
higher. A symmetric cut cannot produce this — a cleanly isolated node's votes never arrive.

Pre-vote's value is therefore a **measured difference and not a claim**: with pre-vote off, repeated
single cuts inflate the term monotonically; with it on, the disrupted node's pre-votes are refused by
peers that have heard from a leader recently, and the term does not move. A2 reports both, from the
same seeds and the same cuts, as an ablation.

---

## 6. D-A2-5: leadership transfer is a command, not a policy **[assumed]**

**Problem.** A leader hands leadership to a named peer without waiting out an election timeout.

**Candidates.** (1) A driver-level `TransferLeadership(target)`. (2) Automatic transfer on planned
stepdown.

**Recommendation, taken: (1).** A2 ships the mechanism; policy is A4's manual rebalance rider
(Amendment A6). The leader stops accepting proposals while a transfer is pending, brings the target
up to date if it is behind, and then sends `MsgTimeoutNow`. The target campaigns **immediately and
without pre-vote** — it has been told by the current leader that it may, which is precisely the
evidence pre-vote exists to gather.

**The sharp edge, named:** a transfer that never completes must not disable the leader forever, so
the pending transfer expires after one election timeout and the leader resumes.

---

## 7. Decisions taken under assumed ratification

| id | decision | rejected |
|---|---|---|
| D-A2-1 | explicit `snapIndex`/`snapTerm` offset | dummy entry at position 0 (positional meaning, BUG-004's class); index-keyed map (map iteration in core scope) |
| D-A2-2 | separate snapshot persist mark | one shared mark (BUG-006's shape one phase later) |
| D-A2-3 | raft holds snapshot metadata only | raft holds bytes (unbounded payload in a pure state machine); Storage interface (breaks purity) |
| D-A2-4 | `MsgPreVote`/`MsgPreVoteResp` as enum members | `PreVote bool` on `MsgVote` (meaning by flag, and the ungated case becomes a special case of a gated one) |
| D-A2-5 | transfer as a driver command | automatic transfer (policy, and A4's by Amendment A6) |

---

## 9. What the implementation taught, and what it contradicted

Recorded here rather than in commit messages alone, because four of these
contradict a decision in §2 to §6 and a design document that quietly stops
describing the code is worse than one that never existed.

### 9.1 A pre-vote RESPONSE must echo the proposed term, not the responder's own

D-A2-4 said pre-vote mutates no persistent state, so its messages need no gate.
That was right about the *request* and wrong about the *response*, which carried
`r.term` — the responder's real term, which is a claim about persistent state.
A node whose term bump was still in flight advertised a term it had not written.

**The persist-before-reply oracle found it on 95 of 200 seeds**, immediately, on
the first sweep after pre-vote landed. The fix is in the message, not the check:
a response now echoes the term it was asked about, so it says only *"yes, if you
campaigned for that term, I would vote for you"* — a statement about volatile
facts and nothing else, and DR-8's non-gate argument holds exactly as written
rather than nearly.

The cost is real and small: a candidate that is behind no longer learns the true
term from a pre-vote rejection. It learns it one round later from a real leader.
Buying that with a durability claim on the hot path of every election attempt is
the trade DR-8 already refused.

### 9.2 One counter with two names is not two streams

D-A2-2 said the snapshot install gets its own persist mark. The first
implementation gave it its own *field* and drew its value from the log's counter.
That is one stream with two names: marks stay monotone, so acknowledging a later
log mark implies the snapshot's, and the second answer collapses into the first.

**It was found because the mutant did not fire.** Removing the snapshot arm from
the gate changed nothing across 300 seeds. A planted violation that survives is
either a checker that cannot see it or a defence that was never there, and here
it was the second.

Two streams means two counters and two watermarks with no ordering assumed
between them, and the driver writing the snapshot in its own batch acknowledged
on its own completion. With that, removing the arm produces **37 violations in
300 seeds, first at seed 17**, with DR-8's exact message. The gate is live.

### 9.3 "The later of the two marks" generalises to "all of the marks"

A1's gate took the later of two marks because both lived in one ordered stream,
where later implies earlier. With two independent streams there is no ordering to
lean on, so a message attesting to state in both waits for **both**. A gated
message now carries one mark per stream and is released when every one has
landed; A1's rule is the single-stream special case.

### 9.4 Re-applying after a crash is correct, and the first oracle said otherwise

The apply-continuity oracle first asserted that a node applies each index exactly
once, in increasing order. It fired on the first seed and the system was right: a
node that crashes **without** a snapshot has an empty state machine when it
returns and must re-apply its whole log. The invariant is not monotonicity — it
is *no hole* and *rebuild determinism*, and §5c of DESIGN-A1's lesson applies
directly: an oracle that has never been wrong is usually an oracle nobody has
run.

### 9.5 Pre-vote made the cluster calmer, and that cost the harness its power

The finding this phase would most like to have missed.

Pre-vote stops a disrupted node inflating the term, so the cluster re-elects far
less. That is the feature working — and it means far fewer conflicting appends,
far fewer divergent tails, and far less for the fault injection to find. Measured
with `M18` planted, 500 seeds per arm:

| configuration | log-matching detections |
|---|---|
| no pre-vote, no snapshots | 10, first at seed 15 |
| pre-vote on | 0 |
| full A2 | 0 |

`M19` moved the same way: 228 of 300 under A1, 1 of 300 under A2.

**A feature that makes the system calmer makes the harness weaker, and nobody
chooses that trade — it arrives with the feature.** Two responses, both taken:
the schedule mix was widened (four crashes and five partitions over twelve
seconds, against two and three over eight), which restored the A1-shape rate from
2 to 10 per 500; and each oracle's *induction* now runs in a configuration where
it has a window, with the rate recorded beside it. Log matching and leader
completeness still run under compaction in the main sweep — that is a real but
weaker claim, and it is stated as one.

### 9.6 The mutant lane could not tell a deleted test from an uncaught defect

`M19` reported ALIVE. Its covering test had been **deleted** by an unrelated edit,
and `go test -run` exits zero when the pattern matches nothing — so the lane
reported the mutant as surviving and pointed the finger at the oracle. The lane
now requires the covering test to have actually run, and reports a mutant whose
test never executed as an ERROR rather than a verdict. Induced against a patch
naming a test that does not exist.

Seventh instance of the vacuous-green class by DESIGN-A1 §5c's register, and the
first one inside the mutant suite itself.

### 9.7 A2's own bug

BUGS.md **BUG-009**: a node whose applied prefix was inside a snapshot accepted an
append starting at index 1, because the consistency check treated `PrevLogIndex`
0 as agreeable to everybody — true before compaction, false after. It then
overwrote entries it had applied and reported committed. One seed in three
thousand; it took the 10,000-seed run to surface.

The instrument was the assertion **BUG-007 corrected**, with no defect in hand at
the time. Its old form fired on the durable watermark, which Raft permits
truncating; the corrected form fires on the commit index, which Raft does not.
One phase later it caught this, and the old form would have missed it while
complaining about legal truncations.

---

### 9.8 The power lane was silent through the power regression, because it was not watching

Asked directly after A2: *did the harness-power lane fire when pre-vote dropped
M18 from 10 detections in 500 to 0 and M19 from 228 in 300 to 1?*

**No. It could not have.** `sim/hunt/floors.go` floors four TOY flaw classes and
zero mutant classes, and by A2 there were thirty-one mutants. The lane was green
because it had never been looking at the classes whose power moved.

That is the tenth instance of the vacuous-green class by DESIGN-A1 §5c's register
and the second inside an instrument, after the mutant suite's inability to tell a
deleted covering test from an uncaught defect (§9.6). Two instruments in one
phase, which is itself the finding: the things that watch are the things nobody
watches.

`scripts/power-mutants.sh` closes it. Every mutant patch now declares a detection
floor with the range it was measured over, or an explicit opt-out with a reason,
and a patch declaring neither **fails the lane** -- because saying nothing is how
thirty-one classes came to share four floors. Seventeen classes carry floors;
fourteen opt out, each with its reason recorded in the patch header.

**Floors, measured at `bf10d04`**, set at roughly half the measured rate, which
is the margin rule the toy floors already use:

| class | measured | floor | first |
|---|---|---|---|
| M17 vote twice in one term | 101/300 | 50 | 3 |
| M18 prev-log check term-blind *(A1 shape)* | 29/300 | 14 | 15 |
| M19 vote for a shorter log | 4/1500 | 1 | 81 |
| M20 conflicting entry kept | 6/300 | 3 | 60 |
| M21 decode off-by-one | 300/300 | 150 | 0 |
| M23 gated messages never released | 300/300 | 150 | 0 |
| M24 answer by position | 53/300 | 26 | 10 |
| M14 epoch check removed | 13/300 | 6 | 28 |
| M25 restart recovers unsynced writes | 15/300 | 7 | 28 |
| M26 truncated suffix left in the engine | 4/300 | 2 | 129 |
| M27 durable record ignores a clear | 4/300 | 2 | 129 |
| M28 mark coverage grows after handover | 258/300 | 129 | 1 |
| M29 truncation refused below the durable watermark | 7/300 | 3 | 60 |
| M31 snapshot stream loses its gate | 50/300 | 25 | 9 |
| M32 apply stream skips an index | 300/300 | 150 | 0 |
| M33 state machine drops a command | 292/300 | 146 | 0 |
| M34 append from zero over a snapshot | 1/3000 | 1 | 1364 |

**Induced by reverting A2's mix widening**, which is the change that was supposed
to compensate for pre-vote. Three classes drop below their floors:

| class | wide mix | narrow mix |
|---|---|---|
| M14 epoch check removed | 13/300 | **4** |
| M25 restart recovers unsynced writes | 15/300 | **4** |
| M34 append from zero over a snapshot | 1/3000 | **0** |

The last line is the one to keep. With the narrower mix M34 is detected on zero
of three thousand seeds — **BUG-009, A2's own bug, would not have been findable
at all.** The mix widening was justified when it was made by an argument about
what pre-vote suppresses; this is the number behind that argument, and it only
exists because the lane now measures.

The UNCOVERED path is induced too: a patch with its power headers stripped fails
the lane by name.

---

## 8. Exit criteria

Ansh's, recorded verbatim so the report can be checked against them rather than against a paraphrase.

1. Snapshot install with the log compacted behind it, and recovery from a snapshot plus tail proving
   identical state to recovery from a full log.
2. Pre-vote, with this document's argument citing the single-cut send-without-receive geometry and
   the schedule mix weighting it, plus a census showing term inflation under repeated single-cut
   partitions with pre-vote off and its absence with pre-vote on.
3. Leadership transfer, with the transferred-to node winning without an election timeout and no
   committed entry lost across the transfer.
4. The two-mark gate induced for real, since the snapshot stream gives `markFor` a second answer.
5. Every new oracle induced before it counts.
6. Corpus lane green across the snapshot format change; if it breaks, the format change is what broke
   it and the bundles are regenerated deliberately with that recorded.
7. 10k seeds, zero safety violations, inconclusive tracked and explained.

### Status against them

Claude does not mark phases complete; this is evidence for a ruling.

| # | criterion | evidence |
|---|---|---|
| 1 | snapshot install with the log compacted behind it; recovery from snapshot plus tail identical to recovery from a full log | 204,315 snapshots taken and 29,097 installed across 10k seeds; `snapshot-equivalence` checks every one against the state the committed log independently produces, induced by `M33` |
| 2 | pre-vote, with the single-cut argument and a measured census | §5 carries the argument; ablation over the same 200 schedules: terms summed 2450 → 623, highest 57 → 16, elections started 2450 → 515, **started per win 2.07 → 1.06**, zero seeds without a leader in either arm |
| 3 | leadership transfer, target wins without an election timeout, no committed entry lost | 391 orders delivered, 390 took effect within a fifth of an election timeout, 385 went on to lead, 0 ran a pre-vote round; loss is covered by leader completeness, in-run |
| 4 | the two-mark gate induced for real | `M31` removes the snapshot arm: 37 violations in 300 seeds, first at seed 17. §9.2 records that the first implementation made it un-inducible |
| 5 | every new oracle induced before it counts | `apply-continuity` ← `M32` (300 of 300, seed 0); `snapshot-equivalence` ← `M33` (262 of 300, seed 0); `persist-before-reply`'s new arm ← `M31` |
| 6 | corpus green across the format change | it broke, as expected: all raft bundles diverged. Regenerated deliberately, re-pinned to seeds that still reproduce, and **bundles now record their build** so the next feature cannot silently rewrite what an old one means |
| 7 | 10k seeds, zero safety violations, inconclusive tracked | **pass 9995, violation 0, inconclusive 5, errors 0** — 0.5 per mille against a 30 per mille ceiling, each cause printed |

Mutant suite: 30 killed, 1 canary alive, 0 mismatched, 0 rotted.
