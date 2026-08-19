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

**Re-enable the move-racing-churn interleaving.** DESIGN-A4 §10 records it as an unexercised
interleaving: the plan separates the two membership drivers in time because a move's add and an
unrelated removal are indistinguishable in a committed log. It is the interleaving a production
cluster produces constantly.

A6 reshapes the schedule mix anyway, which is the moment to try. What has to be solved first is
`rebalance-safety`'s **attribution** — it cannot tell whose removal it is looking at when both
drivers are live, which is what produced 252 false violations in 300 seeds (BUG-016). The
bidirectional assertion in `TestRaftExitCriteria` fails the day the interleaving becomes reachable,
so this cannot be forgotten silently; it can only be forgotten loudly.

*Ansh, on the A4 sign-off: "Record it in DESIGN-A4 as a named unexercised interleaving with the
reason, add it to the bidirectional-gap ledger so the day it becomes exercised the record is wrong
and says so, and put it on A6's checklist as a candidate once the schedule mix is being reshaped
anyway."*

---

## Owed by I2

**The undecodable-snapshot panic.** DESIGN-A4 §11 classifies it as correct today and wrong later: the
sim's transport reorders, duplicates and drops but never corrupts bytes, so an undecodable snapshot
can only be a harness or codec defect. A real transport can deliver a corrupt frame. Changing it
before a real transport exists would be guessing at the right behaviour.
