# DESIGN-A5: MVCC and the hybrid logical clock

**Status:** written before the code. Decisions marked **[assumed]** ride the cadence ruling of
2026-08-18; decisions marked **[frozen]** touch a frozen interface and are reported, never assumed.
**Author:** Claude. **Decider:** Ansh. **Phase:** A5. **Depends on:** A4, signed; A0.4, signed.

---

## 1. What changes, and the one thing that changes underneath everything

Through A4 the state machine was `map[string]string`. A write replaced a value and a read returned
whatever was there. A5 makes it versioned: every write records a version at a timestamp, and a read
names the timestamp it wants to see.

Three pieces:

- **HLC** — a hybrid logical clock wrapping `clock.Wall`, giving every write a timestamp that is
  consistent with causality across nodes whose physical clocks disagree by up to `maxOffset`.
- **MVCC** — versions keyed by timestamp on the engine interface, reads at a timestamp, and garbage
  collection below a low-water mark.
- **The timestamp source behind an interface** — HLC is one implementation; the pre-authorized TSO
  fallback (Amendment A6) is another, and neither the KV layer nor the oracles may know which.

**The hard part is that A4's class arrives one dimension over.** A4 found six instances of *anything
derived from a log position must be derived AT that position*. A5 introduces a second position — a
timestamp — and every fact derived from one is a candidate for the same defect. §7 applies the class
preemptively rather than waiting for the sweep to find six more.

---

## 2. D-A5-1: the HLC wraps `Wall`, and `Mono` is not reachable from it **[assumed]**

**Problem.** An HLC timestamp is a physical reading plus a logical counter. Which physical reading?

**Candidates.** (1) `Wall`. (2) `Mono`. (3) Both, with `Mono` for local ordering.

**Tradeoffs.** This is not a close question and it is written down because A0.4 already paid for the
answer. `Mono` is elapsed time on *this node's oscillator since this boot*: it is meaningful only as a
difference between two readings on the same node within one boot. An HLC timestamp is persisted, sent
on the wire, and compared across nodes and across restarts — every one of which is exactly the
monotonic-leakage bug class A0.4 made uncompilable.

**Recommendation, taken: (1), and the enforcement is inherited rather than rebuilt.** `hlc.Timestamp`
carries a `clock.Wall` and a logical counter. It cannot carry a `Mono`, because:

1. `Wall` and `Mono` are distinct defined types, so a `Mono` does not assign to a `Wall` field;
2. `Mono` has no encoder and its `MarshalJSON` fails loudly;
3. `determinismcheck` rejects a `clock.Mono` in an exported or tagged struct field outside `clock/`.

A5 adds nothing to that. **What A5 must not do is add a hatch**, and the exit criteria say so: the
uniform-`maxOffset` assertion stays live, and no `Mono` enters `hlc/` or `kv/`.

---

## 3. D-A5-2: the update rules, and what happens at the envelope edge **[assumed]**

The standard HLC, stated precisely because the oracles check exactly this:

```
Now():                                    Update(m):
  p := max(last.Wall, clock.Wall())         p := max(last.Wall, m.Wall, clock.Wall())
  if p == last.Wall { l := last.L + 1 }     l := 0
  else              { l := 0 }              if p == last.Wall && p == m.Wall { l := max(last.L, m.L) + 1 }
  last = {p, l}                             else if p == last.Wall           { l := last.L + 1 }
  return last                               else if p == m.Wall              { l := m.L + 1 }
                                            last = {p, l}
```

**The causality property, which is the thing worth checking:** if event *a* happens before event *b*
— same node in program order, or *a* is a send and *b* the matching receive — then `ts(a) < ts(b)`.
It is a property test under skew schedules, not a unit test, because the interesting failures need
two nodes disagreeing about physical time.

**Problem: a remote timestamp beyond the envelope.** A node whose clock has jumped forward past
`maxOffset` sends a timestamp the receiver cannot reconcile. Classic HLC takes the max regardless,
which silently drags the whole cluster's clock forward — one broken node redefines physical time.

**Candidates.** (1) Take the max, as the paper says. (2) Panic (CockroachDB's choice). (3) Refuse the
update, return a typed error, count it.

**Recommendation, taken: (3).** (1) is the failure mode that makes clock skew unbounded in practice
and it is exactly the thing the envelope exists to bound. (2) is a caller-bug classification for a
runtime condition — a peer's clock is not something the caller can check — and BUG-010 is the
standing lesson about getting that wrong. (3) refuses, reports, and counts; the count is asserted
nonzero in skew runs and zero in bounded runs, which makes it **two** assertions rather than a
number nobody reads (the A4 rule).

**What is deliberately NOT here.** Uncertainty intervals and `ReadWithinUncertaintyInterval`
restarts are A6. A5 delivers the clock those need and the property tests that say it works.

---

## 4. D-A5-3: versioned key encoding **[assumed]**

**Problem.** The engine interface is an ordered byte-keyed store. MVCC needs "the newest version of
key K at or before timestamp T" to be one seek.

**Candidates.**

1. **Key suffix, ascending timestamp** — `d/<key>/<ts>`. A read seeks to `d/<key>/<T>` and steps
   *backwards*.
2. **Key suffix, descending timestamp** — `d/<key>/<^ts>`, timestamp bits inverted. A read seeks to
   `d/<key>/<^T>` and the *first* record at or after it is the answer.
3. **A value holding all versions** — one engine key per user key, versions inside.

**Tradeoffs.** (3) makes a write O(versions) and makes GC rewrite the whole record; it also puts an
unbounded value behind a single key, which the C++ engine would hate at B3. (1) and (2) differ in one
way that matters: (1) needs `SeekLT` plus a bounds check that the key prefix still matches, and (2)
needs `SeekGE` and the same check. **(2) is chosen because the newest version is the common read and
it is the first record found**, so the hot path never steps.

**Recommendation, taken: (2).** `d/<key>/<^wall><^logical>`, both big-endian, so byte order is
reverse timestamp order within a key. The inversion is arithmetic on the encoding, never on the
timestamp: a `Timestamp` is never negated, only its bytes.

**The separator problem, stated because it is the classic MVCC encoding bug.** A user key may contain
any byte, so `d/<key>/<ts>` is ambiguous unless the key is escaped or its length is prefixed. Length
prefix, not escaping: escaping makes the encoded length data-dependent and the comparison rules
subtle, and this is a place where subtle means a key sorts into another key's version chain.

---

## 5. D-A5-4: the timestamp source is an interface, per the A6 escape hatch **[assumed]**

CLAUDE.md, Amendment A6: *"The timestamp source lands behind an interface in A5; TSO fallback is
pre-authorized if A6's uncertainty machinery is not green by Dec 1."*

```go
// Source hands out timestamps. HLC is one implementation; a TSO is another.
type Source interface {
    Now() Timestamp
    Update(Timestamp) error
    MaxOffset() time.Duration
}
```

**What the interface is for, and what it must not become.** It exists so that swapping HLC for a TSO
is a construction change rather than a rewrite of `kv/`. It must not become a place where the KV
layer asks *which* source it has: a `if _, ok := src.(*HLC)` anywhere in `kv/` defeats the entire
point, and the exit criteria treat it as a defect.

`MaxOffset` is on the interface rather than fetched from a clock, because a TSO's uncertainty is a
property of the TSO and not of any node's local clock. A5 ships HLC; the interface is what makes the
fallback a decision rather than a project.

---

## 6. D-A5-5: garbage collection, and the read that must be REFUSED **[assumed]**

GC removes versions below a low-water mark so that version chains do not grow without bound.

**The dangerous part is not the deletion.** It is what happens to a read at a timestamp below the
mark afterwards. Before GC, a read at T returns the version visible at T. After GC, the versions that
*were* visible at T are gone, and a naive implementation returns whatever is left — an *older-looking
state that never existed*, or a newer one. Either way the read is silently wrong, and no checker
downstream can tell, because the answer is a perfectly plausible value.

**Recommendation, taken: refuse, with a typed error.** A read at a timestamp at or below the GC mark
returns `ErrBelowGCMark` and no value. The exit criteria name this directly: *a test proving a read
at a timestamp below the mark is refused rather than silently answered wrong.*

**The mark is applied state.** It advances by an applied command, not by a background timer, so every
replica has the same mark at the same log position — which is A4's class again (§7). A mark that
advanced locally would let two replicas disagree about whether a read is answerable, which is a
divergence that looks like a client error.

**Two invariants, both checked:**

- **GC never removes a version a live read could still need**: the mark only advances, and it never
  advances past the oldest timestamp any in-flight read named.
- **A refusal is a refusal on every replica**: the same read at the same timestamp against the same
  log position is refused by all of them, or none.

---

## 7. The log-position class, one dimension over

A4's class, restated for A5:

> **Anything derived from a timestamp must be derived AT that timestamp.**

A4 found six instances of the log-position version *after* they became bugs. This section is the
attempt to find A5's before they do. Every timestamp-derived fact in the design, with where it must
be taken:

| the derived fact | the wrong place to take it | the right place |
|---|---|---|
| which version a read sees | "the newest version", or the version at `Now()` | the newest version at or before **the read's timestamp** |
| whether a read is answerable | the GC mark now | the GC mark **at the read's log position** |
| whether a write conflicts | the newest version | the newest version **at or after the write's timestamp** |
| what a snapshot contains | versions above the current mark | versions above the mark **at the snapshot's index** |
| a node's HLC state after a receive | the clock read at handling time | the reading taken **when the message was stepped** |

**The structural attempt, and it is a report rather than an assumption.** The A4 carry-forward asks
whether typing the answer would make the class *impossible* rather than merely caught. A5 is the
place to try, because A5's dimension is new and nothing about it is frozen:

```go
// At[T] is a value together with the timestamp it was taken at. A bare T is
// not constructible outside the package that produces it.
type At[T any] struct { ts Timestamp; v T }
```

`kv.ReadAt(ts)` returns `At[Value]`, and anything that consumes a value must state the timestamp it
believes it is consuming it at; a mismatch is a compile error, not a comment. **This is attempted in
`kv/` only.** Extending it to `raft.Configuration()` — the A4 case — is a frozen-interface change and
is reported in §12, never assumed.

---

## 8. The oracle, and what it is forbidden from reading

**MVCCReadCorrectness:** a read at timestamp T returns exactly the value the harness's own record of
committed writes says was visible at T.

The ledger records every committed write as `(key, value, timestamp)` — **observed** as the bytes
crossing the apply boundary, exactly as A4's entries are. The oracle replays them and answers the
read itself. It never asks the engine and it never asks a `kv.Store`: the exit criteria say
*"judged by an oracle from harness-observed writes and never from engine state"*, and
`internal/provenance` already makes the wrong version fail to compile.

**A refused read is a verdict too.** A read that returned `ErrBelowGCMark` is checked against the
harness's own mark record: if the harness says the timestamp was above the mark, a refusal is a
violation, and if the harness says it was below, an *answer* is a violation. Both directions, because
a checker that only catches wrong answers cannot catch a store that refuses everything.

---

## 9. What A5 does not do

- **Transactions.** No locks, no write records, no prewrite/commit. A6.
- **Uncertainty intervals and read restarts.** A6. A5 gives them a clock and property tests.
- **Multi-key snapshot isolation.** A read at a timestamp is per key; the bank workload is A6's.
- **Leases.** STRETCH (Amendment A6).

---

## 10. Exit criteria

Ansh's, verbatim.

1. HLC wrapping `Wall` only, never `Mono`, with the clock work from A0.4 as its foundation and the
   uniform `maxOffset` assertion still live.
2. MVCC versions keyed by HLC timestamp with garbage collection below a low-water mark, and a test
   proving a read at a timestamp below the mark is refused rather than silently answered wrong.
3. The timestamp source behind an interface per the A5 escape hatch, so the TSO fallback stays
   available.
4. Reads at a timestamp returning the version visible at that timestamp, judged by an oracle from
   harness-observed writes and never from engine state.
5. The log-position class applied preemptively: every timestamp-derived fact derived at the timestamp
   it belongs to.
6. Every count the exit run prints either asserted or deleted.
7. Power floors with rate and kill-time under A5's shape.
8. Every new oracle induced, every bug in BUGS.md with its mutant class, corpus green or deliberately
   regenerated, 10k seeds zero violations with inconclusive explained.

---

## 11. What the implementation taught

### 11.1 The log-position class in A5's dimension, and the four instances it caught early

§7 wrote the class down before the code: *anything derived from a timestamp must be derived AT that
timestamp*. Writing it down first is the point of the exercise, and it is checkable — every one of
these is a line that would otherwise have been the obvious spelling:

| the fact | the obvious spelling | what §7 forced |
|---|---|---|
| a command's timestamp | each replica stamps when it applies | the **leader** stamps at propose; the timestamp travels in the entry |
| a read's answer | the newest version | the newest version **at or before the read's timestamp** |
| the collection mark | advanced by a timer | advanced by an **applied command**, so every replica refuses the same read |
| a range's inherited mark | the child starts at zero | the mark **travels with the versions** a split moves |

A replica stamping at apply is not a near miss: two replicas apply the same entry at different wall
times, so the same value gets two timestamps and every subsequent read sees different history
depending on who served it. A mark advanced by a timer is the same failure in the answerability
dimension — one replica refuses a read another answers, which surfaces as a client error rather than
as a divergence, and is the hardest kind to attribute.

**None of these became a bug.** That is what the preemptive section was for, and it is the only
evidence available that writing it down was worth anything: the phase that found six instances of the
class after the fact found zero of them this time, in a dimension that offers just as many.

### 11.2 The three defects A5 did produce, and every one was caught by an assertion this project already had

- **A machine hosts many replicas over one engine**, so rebuilding one replica's state machine wrote
  to the engine another had not read yet — and the read-back assertion behind BUG-005 fired on it,
  correctly. Every replica now reads before any replica rebuilds.
- **A split reads the range's whole version set and rewrites it**, so writes staged earlier in the
  same Ready had to be flushed first. Caught by `applySplit`'s partition assertion, which A4 added for
  exactly this shape: a range whose extent was `[,k02)` holding `k02`, so the next split cut at `k02`
  and produced an empty range.
- **A read must see writes staged at lower indices in the same batch.** Otherwise the driver
  manufactures a stale read the protocol never produced.

The pattern is worth naming: **the state machine moving from a Go map into the engine turned three
latent ordering assumptions into engine-visible facts**, and every one of them landed on an assertion
written for a different reason two phases earlier.

### 11.3 BUG-017 was in `raft/`, and A5 only changed the traffic

The one real protocol defect this phase found has nothing to do with MVCC (BUGS.md BUG-017). It is a
branch in `Ready()` that has been there since A2. What A5 changed is the shape of the traffic that
reaches it.

Both halves of the correction are recorded in BUGS.md, and the second is the one worth repeating
here: **a constraint that has been satisfied must be recorded as satisfied, not swept once.** A sweep
answers the question at one instant; anything whose *other* constraint arrives later never asks again.
That is A4's "a fact is recorded, never inferred" arriving in the gate rather than in the ledger.

### 11.4 The collection throttle bought a 30x runtime and cost a reachability

The first collector proposed whenever the retention window had passed, which after the first
collection is true on essentially every apply. A 200-seed sweep took **25 minutes**; throttled to one
collection in flight and a mark that has moved by a quarter of the window, it takes **49 seconds**.
A 10,000-seed exit run went from most of a day to viable.

**The throttle cost something, and the number is recorded rather than the intention.** M53 plants
BUG-017's defect. Measured with the unthrottled collector: **1 detection in 60 seeds**. With the
throttle: **0 in 300**. The class that found the phase's only protocol bug is no longer reachable in
the shape the sweep runs.

That is the standing lesson from A2's M34, arriving from the other side: *a schedule mix is a claim
about reachability, not a configuration detail.* The mutant is opted out of the rate lane with that
measurement as its reason, and it is killed by a targeted raft test instead — which is the honest
arrangement when a class is unreachable by argument rather than by luck.

---

## 12. Limitations, recorded

1. **UNEXERCISED: a write below the collection mark.** Every write in A5's workload is stamped at
   propose, so its timestamp is above the mark by construction. A6's transactions write at a timestamp
   chosen when the transaction began, which can fall behind the mark while the transaction is in
   flight — which is precisely what the refusal exists for. Exercised by
   `kv.TestWritingBelowTheMarkIsRefused`, asserted at **zero** in the sweep, and bidirectional: the
   day a workload reaches it, the exit run says the record is stale.
2. **Snapshot reads are outside the linearizability history.** A read at a past timestamp is not a
   linearizable operation on the current value, and feeding one to porcupine as if it were would
   manufacture violations out of correct behaviour. They are judged by `mvcc-read-correctness`
   instead. The consequence, stated: the history the linearizability checker sees is **smaller** than
   the workload, by the share of reads that are snapshot reads (400 per mille of gets).
3. **The envelope refusal is unexercised by construction.** The schedule mix keeps skew inside
   `maxOffset`, so no peer is ever refused; the count is asserted at zero and the refusal is exercised
   by `hlc.TestATimestampBeyondTheEnvelopeIsRefused`. Deliberately exceeding `maxOffset` is the
   envelope experiment, which is STRETCH (Amendment A6).
4. **One HLC per range, not per node.** Two ranges on a node share a physical clock and not a logical
   counter. Nothing depends on the counters being shared, and keeping them separate stops a busy range
   inflating a quiet one's timestamps — but it does mean a node's timestamps are not totally ordered
   across ranges, which A6 must not assume.

---

## 13. The `At[Index, T]` attempt, and what it concluded

The A4 carry-forward asked A5 to **attempt** typing the position into the answer — an `At[Index, T]`
the way `Observed[T]` and `Reported[T]` carry provenance — so that a position-free question would
fail to compile rather than fail in a sweep. It was attempted. The conclusion is not the one the
proposal expected, and it is worth more than the wrapper would have been.

**Typing the answer pays only where the caller does not already hold the position.** That is what
made BUG-015 possible: `left.raft.Configuration()` was called from a site that *had* an index — the
entry it was applying — and asked a question that did not take one. The type would have refused.

A5's dimension has no such site, and not by luck. §7's discipline pushed the timestamp **into the
data**: a command carries the timestamp its leader stamped it with, a read carries the timestamp it
named, the collection mark travels with the versions a split moves. Every consumer therefore receives
the position as an argument and cannot ask a position-free question, because there is no
position-free question left to ask. Wrapping those answers in `At[Timestamp, T]` would restate an
argument the caller already passed.

**So the finding is a rule about which fix applies where:**

> Carrying the position **in the data** and typing it **into the answer** solve the same problem, and
> the first one is available whenever you control the wire format. Reach for the type only where you
> do not — which is exactly the frozen-interface case, and exactly why A4's instance was the one that
> got through.

`raft.Configuration()` remains the case that would pay, and it remains a frozen-interface change
(D5). It is reported here, not assumed, and it stays on the carry-forward for the phase that has a
reason to touch that interface anyway.
