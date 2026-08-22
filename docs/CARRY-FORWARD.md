# CARRY-FORWARD.md

Obligations a phase inherits from an earlier one, with the ruling that created them.

This file exists because the alternative failed. Commitments made in a design doc's §9 are read once,
by the person who wrote them, on the day they were written. A phase boundary is exactly where an
obligation goes quiet — and this project's most expensive defects have all been things that went
quiet while looking green.

Each entry names the phase that owes it, what is owed, and where the reasoning lives. An entry is
deleted only when the thing is done, and the commit that deletes it says so.

---

## Owed by A5

**~~The `At[Index, T]` proposal.~~ DISCHARGED at A5** — attempted, and the conclusion is recorded in
DESIGN-A5 §13. Typing the answer pays only where the caller does not already hold the position, and
A5's dimension has no such site because §7's discipline pushed the timestamp into the data instead.
Carrying the position in the data and typing it into the answer solve the same problem; the first is
available whenever you control the wire format. `raft.Configuration()` remains the case that would
pay and remains a frozen-interface change — see below.

**~~Every floored mutant class carries a seeds-to-detection ceiling.~~ DISCHARGED at A5** — every
class carries both, the lane fails on either, and the numbers were re-measured under A5's shape.

---

## Owed by the phase that next has reason to touch the frozen raft interface

**`raft.Configuration()` should take an index.** DESIGN-A5 §13: it is the one site where a caller
holds a position and asks a question that does not take one, which is what made BUG-015 possible. The
fix is a frozen-interface change (DESIGN-A0 D5), so it is a **report, never an assumed
ratification**, and it is not worth opening that interface for on its own.

---

## Owed by A6

**One reduced-seed unthrottled garbage-collection run.** DESIGN-A0 §7 item 9 records what the A5
collection throttle costs: M53's class goes from 1 detection in 60 seeds to 0 in 3,000. The figure
must be re-measured under A6's shape rather than inherited from A5's, at a reduced seed count so the
run is affordable.

*Ansh, on the A5 sign-off: "put one unthrottled run on the A6 checklist at a reduced seed count, so
the number gets re-measured under A6's shape rather than inherited from A5's."*

**Bound the race lane, with a measurement.** It is 90 minutes at A5 and will be four hours by A7. Its
value is concentrated in `sim/hunt`, where the driver, mailbox and simulator meet, and it has found
real races twice, so it stays — but the seed count has never been measured, only inherited. Run
`sim/hunt` under `-race` at 50, 100 and 200 seeds and report whether the two races it caught
historically are still caught at the lower counts. Bound it at the smallest count that catches
everything 200 catches, with the measurement recorded. **Do not guess the number** — same discipline
as the window curve.

*Ansh, on the A5 sign-off: "Before A6 closes, report what race seed count is actually needed... If
100 catches everything 200 catches, bound it there with the measurement recorded, which is the same
discipline as the window curve. Do not guess the number."*

**Re-enable the move-racing-churn interleaving.** DESIGN-A4 §10 records it as an unexercised
interleaving: the plan separates the two membership drivers in time because a move's add and an
unrelated removal are indistinguishable in a committed log. It is the interleaving a production
cluster produces constantly.

A6 reshapes the schedule mix anyway, which is the moment to try, and the A5 sign-off says to attempt
it there. What has to be solved first is
`rebalance-safety`'s **attribution** — it cannot tell whose removal it is looking at when both
drivers are live, which is what produced 252 false violations in 300 seeds (BUG-016). The
bidirectional assertion in `TestRaftExitCriteria` fails the day the interleaving becomes reachable,
so this cannot be forgotten silently; it can only be forgotten loudly.

*Ansh, on the A4 sign-off: "Record it in DESIGN-A4 as a named unexercised interleaving with the
reason, add it to the bidirectional-gap ledger so the day it becomes exercised the record is wrong
and says so, and put it on A6's checklist as a candidate once the schedule mix is being reshaped
anyway."*

---

## Standing, from A6

**The symmetric-apply gap.** DESIGN-A6 §13.4: an apply path wrong identically on every replica is not
caught by replay equivalence, and is covered by a list of mutant classes rather than by a mechanism.
A list is a claim, not a proof. Any BUGS.md entry of that shape which no mutant caught invalidates the
record, and the response is a new class in §13.3, not a footnote.

*Ansh, on the A6 stop: "the day a defect of this shape reaches BUGS.md without a mutant having caught
it, the record is wrong and says so."*

**Status: tested once, already.** `M61` (a rollback that leaves the version) survived its first run —
symmetric, so replay equivalence left the same version; invisible to clients, because no commit record
pointed at the orphan. It was answered with `percolator-invariants` #5 rather than a tuned test. The
list is therefore a claim under active test rather than an assertion, which is the most that can
honestly be said for it.

**Corpus regeneration is a search, not a re-record.** DESIGN-A6 §16. Whenever the workload changes
enough to move traces, `TestEveryStoredBundleReplays` will fail and the fix is to regenerate — but a
regenerated bundle must be re-verified by `CORPUS_REINTRODUCE=1 go test ./cmd/simctl`, because a
schedule that no longer reaches its defect regenerates exactly as happily as one that does. Measured:
16 of 16 bundles proved nothing after one blind regeneration.

**The mutant lane's budget under A6's shape.** 36 of the 39 mutants that declare a power expectation
measure under `current`, which is now A6's shape: **14,700 seed-runs at ~3.75 s/seed, about 15
CPU-hours**, on a lane CI runs on every push. It was affordable when a seed cost 0.36 s. Either the
per-mutant seed counts are re-derived under the new cost, or the lane moves to the nightly tier — and
Amendment A2 says the choice has to keep kill-time monitored either way. Re-measure before A7 widens
the shape again.

**The batch-boundary technique.** DESIGN-A6 §14.4. When a replica disagrees with a replay of its own
log, digest per `Ready` on the node and per entry in the replay and look for the divergence across a
**skip** in the node's indices. A7 (read index, where a lease or an index read can be answered
mid-batch) and B4 (kill points, where a kill lands inside a batch) will both want it.

---

## Owed by I2

**The undecodable-snapshot panic.** DESIGN-A4 §11 classifies it as correct today and wrong later: the
sim's transport reorders, duplicates and drops but never corrupts bytes, so an undecodable snapshot
can only be a harness or codec defect. A real transport can deliver a corrupt frame. Changing it
before a real transport exists would be guessing at the right behaviour.
