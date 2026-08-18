# DESIGN-A1: single-group Raft

**Status:** proposed, code follows this document. **Author:** Claude. **Decider:** Ansh.
**Phase:** A1. **Depends on:** A0, closed — the harness, oracles, mutant suite and real-mode driver.

---

## 0. The ruling that shapes everything else

Recorded before a line of `raft/` was written, because it is the constraint the rest of the design
bends around.

> **Ansh, 2026-08-17.** *The four safety oracles read from the Ready stream and from what each node
> persisted, never from `raft.Raft` internals. Oracle independence is the thesis of this entire
> project and the recorded sentence is that an oracle which interrogates the engine believes the lie.
> An oracle reading `raft.Raft` state directly cannot detect a Raft whose in-memory state disagrees
> with what it persisted or emitted, which is precisely the bug class Raft implementations actually
> have. You are right that leader completeness is materially harder this way, since it is defined over
> committed entries across configurations; build the harness-side ledger that makes it expressible
> rather than reaching into the node, and if some property turns out genuinely inexpressible from
> outside, stop and report it as a written case rather than quietly reaching in.*

**What that forbids, concretely.** No oracle may call a method on `*raft.Raft`, read its role, its
term, its log, its commit index, or its match table. The oracles are handed a **ledger** built from
two streams and nothing else:

1. **What each node emitted** — every `Ready` the driver observed: hard state, entries to persist,
   entries to apply, messages to send.
2. **What each node persisted** — every write that reached the `Engine`, and when it became durable.

**Why this is not pedantry.** The interesting Raft bugs are exactly disagreements between memory and
durability: a node that votes, replies, and crashes before the vote is durable; a leader whose
in-memory commit index runs ahead of what its followers acknowledged; a term bump applied to memory
and lost on restart. An oracle reading `r.term` sees the intent. An oracle reading the ledger sees
what the cluster actually did, which is the thing the next leader will act on.

**The cost, accepted.** Leader completeness is defined over *committed* entries and *all future
leaders*, neither of which is a field anywhere. §5 builds both from the outside.

---

## 1. `raft/` is pure, and it is in core determinism scope from the first commit

No goroutines, no channels, no clocks, no I/O. Input is `Step(Message)` and `Tick()`; output is a
`Ready`. This is the property that makes deterministic simulation possible and it is not retrofitted:
the package lands inside the determinism pass's core scope, so `math/rand`, `time.Now`, map iteration
and concurrency primitives are build failures there from the beginning.

**Randomised election timeouts** are the one thing a pure Raft cannot produce for itself. `Raft` holds
an integer `randomizedElectionTimeout` and exposes `SetElectionTimeout(int)`; the *driver* supplies it
from a plan-carried PRF keyed by `(node, term)`. No live draw, no `math/rand`, and the value is
reproducible from the plan alone — the same discipline as the transport's per-message dice.

---

## 2. Persistence goes through the `Engine`, and persist-before-reply is a `Ready` contract

Every Raft persistent-state write — term, vote, log entries — goes through the frozen `engine.Engine`
interface. Not a side table, not a test shortcut. A crash therefore takes exactly what a crash should
take, and recovery is the real path: `raft.Restore` reads the engine back and rebuilds the state
machine, so the restart path is exercised by every crash the harness injects rather than by a
bespoke test.

**The contract.** A `Ready` carries `HardState` and `Entries` to persist, and `Messages` to send. The
driver must make the first durable before it sends the second. That ordering is:

- **stated** in the `Ready` doc comment as a requirement on the driver;
- **checked** by an oracle, from the ledger, rather than trusted — for every message a node sent, the
  hard state that message asserts (its term, its vote) must already have been durable at the instant
  it was sent.

Stating it makes it a convention. Checking it from the outside makes it a property, and the
difference is the entire lesson of A0's audit.

---

## 3. The four safety oracles

Each is an in-run `Oracle`, halting the run at the first violation. Each is induced by a planted
violation landing in `sim/mutants/` **in the same commit**, per Amendment A2, so none counts until it
has been shown to fail.

| oracle | property | expressed from |
|---|---|---|
| election safety | at most one leader per term | messages: a node that sends `MsgApp` in term T acted as leader in T |
| log matching | two logs agreeing on (index, term) agree on every entry before it | persisted entries per node |
| leader completeness | an entry committed in term T is present in every later leader's log | the committed ledger, §5 |
| state machine safety | no two nodes apply different entries at the same index | apply streams |

None of these reads node state. Election safety in particular is deliberately expressed over *emitted
messages* rather than over a role field: a node whose role says follower while it is still sending
append entries is a real bug, and the message stream is where it is visible.

---

## 4. Schedule mix: the single-cut geometry is weighted

DESIGN-A0.7 blessed directed partitions with a forward binding — *A1's schedule mix weights the
single-cut send-without-receive geometry.* That binding is honoured here. A symmetric cut is two
directed cuts and produces a cleanly isolated node; a **single** cut produces a node that can send but
not receive, which is where the interesting consensus bugs live: it campaigns, bumps terms, and never
learns it lost. Symmetric partitions never generate it.

---

## 5. The harness-side ledger, which is what makes leader completeness expressible

Leader completeness is *"if an entry is committed in term T, it appears in the log of every leader of
every term greater than T."* From outside a node, none of those three things is directly visible, so
each is reconstructed:

- **Committed.** A node's `Ready.Committed` is what it applied. An entry is *committed* the first time
  any node applies it — a node only applies what its leader told it was committed, so this is a sound
  outside witness. The ledger records `(index, term, data, first-applied-at)`.
- **A leader of term T.** Any node that emitted an `MsgApp` bearing term T. Recorded as
  `(term, node)`; election safety separately asserts this is a function rather than a relation.
- **That leader's log.** What that node had *persisted* at the instant it first acted as leader.

So the check is: for every committed `(index, term)` and every leader of a later term, that leader's
persisted log at the moment it began leading contains that exact entry. Expressible entirely from the
two streams, with no reach into the node.

**If something turns out inexpressible from outside, the ruling is to stop and make the written
case.** Nothing has, so far.

---

## 6. Exit criteria

All true before this is reported ready for sign-off:

1. 10k seeds green, zero safety violations.
2. Porcupine over client histories, inconclusive tracked separately and never counted as a pass, with
   the count *and cause* reported.
3. Election safety, log matching, leader completeness and state machine safety each an oracle, not a
   test.
4. Each of those four induced by a planted violation before it counts.
5. Schedule mix weighting the single-cut send-without-receive geometry.
6. Persist-before-reply inside `raft/`, checked from the ledger.
7. Every Raft persistent-state write through the `Engine`; recovery the real path.
8. **Elections observed actually contending**, not merely completing: a census of terms, elections
   started, elections won and split votes across the 10k. A run where the leader is never challenged
   proves nothing, and a mix that produces one is a mix that needs fixing.
9. Every bug found entering BUGS.md, each answering which mutant class would have caught it, with a
   new mutant landing in the same commit as the fix if none would have.
