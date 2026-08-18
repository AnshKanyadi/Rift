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
