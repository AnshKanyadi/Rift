# STRETCH

Work deliberately outside v1, with the reasoning that put it here preserved.

Ratified as **Amendment A6** (CLAUDE.md), 2026-08-11. Two rules govern this file:

- **Nothing here is ever claimed.** Not in the README, not on a resume, not in a conversation. Resume
  claims track v1 and only v1. A stretch item becomes claimable the day it is built, verified and
  signed off, and not one day earlier.
- **Nothing in the verification spine moved.** Scope came off the feature list, never off the
  machinery that checks it. The simulator, the injectors, the oracles, the mutant suite, the soak
  ledger and the reproduction guarantees are all v1.

The reason a scope cut is written down rather than quietly made: the interesting question about any
of these is not *"did you build it"* but *"why not"*, and an answer given six months later is a
reconstruction. These are the answers as given on the day.

---

## Consensus

### Joint consensus (was A3)

**Cut to:** single-node membership changes with learner catch-up.

Production etcd omits joint consensus too, which is the strongest available evidence that
single-node changes are sufficient in practice. The correctness surface joint consensus adds is
real — `C_old,new` quorum arithmetic, configuration-effective-on-append, config-in-snapshot,
overlapping-change rejection, leader stepdown on self-removal — and each of those is a place to be
wrong. That surface is not justified by v1's requirements, which need membership to change safely,
not to change in one atomic step.

Single-node changes still carry the interesting cases: learner catch-up before promotion,
configuration carried across a snapshot, and a leader removing itself.

**The API shape survives; the capability does not.** DESIGN-A0 D5 froze
`ProposeConfChange(id ProposalID, cc ConfChangeV2) error`, and `ConfChangeV2` is the name of the type
that *supports* joint consensus. A3 kept that shape rather than narrowing it — narrowing a frozen
type to match a narrower scope is the frozen-interface mistake this project has already made twice —
so `ConfChangeJointImplicit` and `ConfChangeJointExplicit` exist in the source today.

They are **refused**, and refused at every point where a configuration entry becomes a configuration:
`ProposeConfChange` for a caller, and `ApplyConfEntry` for one that arrives by replication. A
follower that applied whatever reached it would make the refusal a local courtesy rather than a rule,
and the unreachable path would become reachable the moment one peer disagreed about what it was
allowed to send.

`tools/d5conform`'s `TestJointFieldsArePresentButRefused` pins both halves: the transitions must
exist, and both funnels must test against `ConfChangeSimple`. Deleting a refusal fails the build.
Enabling joint consensus is therefore a deliberate act with a ruling behind it, which is the only way
a cut item should ever come back — and reading `ConfChangeJointExplicit` in the source is not evidence
that joint consensus is implemented. **It is evidence of the opposite: the name exists so the refusal
has something to name.**

### Parallel commits (was A9)

**Cut to:** Percolator-style two-phase commit only.

Parallel commits are a latency optimization: they remove one round trip by declaring a transaction
implicitly committed once all writes are durable at their sequence. The price is a recovery protocol
that every observer of a STAGING record must execute identically, and that protocol is where the
hard bugs live — coordinator death between staging and cleanup, idempotent resolution, races against
a coordinator finishing late.

The v1 position is that Percolator's extra round trip is a *measured* cost rather than an assumed
one: BENCHMARKS.md will state what it is. Paying a heavy recovery protocol to remove a cost nobody
has measured is the wrong order of operations.

### Leader leases (was A8)

**Cut to:** linearizable reads via read index.

Read index is correct without trusting clocks at all. Leases are a read optimization that trades
that property for latency, and the trade is only sound inside an explicit clock-skew envelope — which
is why A8 was going to need the envelope experiment, the lease-disjointness checker, and the stasis
rule.

The clock machinery that experiment needs **already exists**: A0.4 landed the two-clock model,
sustained holds at a chosen fraction of `maxOffset`, envelope schedules that deliberately exceed it,
and an exact skew checker. So this item is cut on scope, not on capability, and picking it up later
does not start from nothing.

---

## Placement

### Automatic load-based balancing (was A10)

**Cut to:** a manual rebalance command, riding with A4.

Replica movement is a *mechanism*: add replica, wait for catch-up, transfer lease and leadership,
remove replica. v1 ships that mechanism and exposes it as a command. Automatic balancing is a
*policy* on top of it — heartbeat-carried load statistics, a mean-plus-threshold heuristic, one
in-flight move per range — and a policy whose mechanism is untested is a policy nobody can debug.

The safety properties that mattered stay with the mechanism and stay checked: moves are
add-then-remove, quorum availability is never voluntarily reduced, and no request is served under a
stale range descriptor epoch.

---

## Transactions

### Observed-timestamps optimization

**Cut to:** uncertainty intervals derived from `maxOffset` alone.

Per-node observed timestamps shrink uncertainty intervals by remembering the highest timestamp seen
from each node. It is a latency optimization on the restart path, and it complicates the argument
for why an interval is safe. v1 keeps the simple, defensible version: the interval is the promised
bound, and a read that lands inside it restarts with a bumped timestamp.

Related and already recorded as an idealization (DESIGN-A0 §7.6): real deployments carry a *dynamic*
per-node error estimate from NTP; we model a static promised bound.

---

## Storage

### Multi-level leveled compaction beyond the v1 policy

**Cut to:** one compaction policy, chosen and measured in DESIGN-B3.

The engine needs *a* correct compaction policy with understood space and read amplification, not the
full RocksDB tuning surface. DESIGN-B3 picks one, states why, and measures it. Additional levels,
tiered/leveled hybrids and dynamic level sizing are tuning work that only pays once there is a
workload demanding it.

---

## Tooling

### `simctl minimize`

**Cut to:** `simctl run | replay | hunt`.

Delta-debugging over plan entries is genuinely valuable and the plan format was designed to make it
possible — entries are individually deletable precisely so ddmin is sound (DR-6). But a minimizer
with no failing corpus entry to minimize is a tool built against an imagined input.

**Build it when the first corpus bug earns it.** The design work is done and recorded; the
implementation waits for a real bug whose plan is too big to read.

---

## Timestamps: the one pre-authorized fallback

The timestamp source lands **behind an interface** in A5. If A6's uncertainty machinery is not green
by **2026-12-01**, a TSO (centralized timestamp oracle) fallback is pre-authorized: a single
allocator hands out timestamps, uncertainty intervals collapse, and the transaction layer keeps
working with a different clock story.

Pre-authorizing it now is the point. The decision is cheap today and expensive in November, and an
interface that was designed with the fallback in mind is not the same interface as one retrofitted
under time pressure.

**Status at A6, and it stayed shut.** BUG-021 needed the timestamp source changed, and the TSO was
the smallest available change. It was **refused**, in Ansh's words: *"the TSO fallback was
pre-authorized on the condition that uncertainty machinery is not green by Dec 1, and it is green and
sweep-exercised, so taking it for a different reason is a new decision rather than an application of
the old one. A pre-authorization consumed for a purpose it was not granted for is an escape hatch
widening itself."*

The condition is not met — the uncertainty machinery is green and, since A6, sweep-exercised rather
than only unit-green. The hatch stays shut and its status line is restated in every report.

---

## A time-independent transaction identity

**The correct long-term answer to BUG-021, deferred.** A transaction id in the transaction record's
key, in the lock, and in the data version — independent of the clock entirely, which is what
CockroachDB does and why it does it.

A6 fixed the defect by tagging the HLC's logical counter with the minting node's ordinal (DESIGN-A6
§22, option A). **A fixes the defect and C fixes the class, and those are different claims.** What A
buys is uniqueness under the clock model the phase already has; what it does not buy is independence
from that model.

**A's correctness rests on node ordinals being unique and stable.** Anything that recycles an
ordinal — a node removed and a new one taking its number, a rebuild that renumbers, a
configuration that reuses a slot — reopens BUG-021 exactly, and reopens it silently, because the
timestamps look perfectly well-formed. That is the standing condition on the fix and the reason C
remains the right answer eventually.

Deferred rather than done because C widens the MVCC key encoding, which A5 fixed and A6 has built an
entire phase of locks, write records and version scans on top of. Changing it mid-phase would put a
frozen-shaped decision under a bug fix, which is the trade this project keeps refusing.
