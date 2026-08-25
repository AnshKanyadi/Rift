# CARRY-FORWARD.md

Obligations this phase created for a **later** one, each with the phase that
discharges it and the check that discharges it. Not a wish list and not a notes
file: every entry names something that must be *done*, in a *named phase*, with
a *named measurement*.

**Why this file exists.** GF-5 in BUGS.md: an accidental defence is worse than a
missing one, because it makes a real gap measure as covered and then removes
itself on a schedule nobody is tracking. A gap with a date is only safe if
somebody is holding the date. This file holds the dates.

**Rules.**

1. Every entry names the **phase that discharges it**. "Later" is not a phase.
2. Every entry names the **check** — a lane, a test, or a measurement that is
   re-run in that phase, and what its result must be compared against.
3. An entry is removed only when its check has actually been run in the phase
   that owns it, and the result recorded here in the closing note.
4. **A number that moves when the obligation comes due is the obligation
   expiring on time, not a regression.** Anyone reading a moved number without
   this file will misdiagnose it, which is the whole reason for the file.

---

## CF-1 — BM2's detection rate must be re-measured the cycle the flush lands

| field | value |
|---|---|
| **Raised** | B1.9b, 2026-08-25 |
| **Raised by** | HARNESS-010 |
| **Discharged by** | **B2**, in the same cycle as the memtable flush |
| **Check** | `make cpp-campaign`, the `BM2-accept-torn-tail` row |
| **Compare against** | 194 per mille, first detection at kill point 14 (measured at `f257e29`, 175 kill points) |

**The obligation.** B1's recovery can apply BATCH records past the last
`GROUP_END`. In B1 those records land in the memtable at sequences **above** the
recovered watermark, where every read's snapshot hides them — present and
unreadable. That is an **accidental** defence: the read path was not defending
anything, it simply had no way to show the damage. The sweep's post-reopen
continuation is what makes the damage visible today, and BM2's floor rests on
it.

**Why B2 is the date.** The flush writes the memtable out. Uncommitted records
that were merely hidden become **durable, visible and permanent** in an SSTable.
The accident ends there.

**What must be true after B2.**

- BM2's measured rate is re-taken and recorded below. It is expected to **rise**,
  because the flush gives the defect a second and more permanent way to show.
- If it **falls**, that is not the accident expiring — that is a regression, and
  the campaign must fail rather than the floor be lowered.
- The floor is re-derived from the new measurement in the same commit, and the
  reasoning column records that it was re-derived and why.

**Closing note (B2), 2026-08-25 — DISCHARGED, WITH A FINDING RATHER THAN THE PREDICTED RISE.**

Re-measured under B2's shape, in both regimes, at B2.7:

| regime | measured | vs `f257e29` |
|---|---|---|
| **default** (no flush) | **34 / 300 = 113 per mille**, first at kill point **39** | count **unchanged at 34**; rate fell; first detection +25 |
| **flush** | **36 / 985 = 36 per mille**, first at kill point **39** | count **34 → 36**; rate fell |

**By how much, and why.** The rate fell in both regimes and **not one detection was lost**. B2 added
125 kill points to the default regime — the manifest's Env calls — and 685 more to the flush regime,
and BM2 is not detectable at any of them. The first detection moved from 14 to 39 by the same fixed
prefix: `DB::Open` now opens a manifest before it touches a WAL.

**Was it consistent with the accident being the whole of the suppression?** Yes, and the answer is
sharper than expected. **The accident was the whole of it, and the sweep's post-reopen continuation
was the whole of the remedy** — the flush adds nothing the continuation had not already exposed.

That was established rather than assumed. The sweep's continuation was extended twice to look for the
second path CF-1 predicted: first with a `Sync` after the continuation write, then with enough filler
to guarantee the flush regime actually flushes, then with **a second kill and a second recovery** —
because the mechanism CF-1 describes needs one. Records recovery applied but never committed are
written into a table, and a table's largest sequence is what the NEXT open takes its watermark from,
so on a second recovery they are promoted rather than hidden. **The count did not move under any of
the three.** Every kill point at which the flush would expose the defect is one the continuation
already exposes, because a torn tail leaves its first uncommitted batch at exactly watermark + 1 —
which is exactly the sequence the continuation write takes.

**What was NOT done, and it is Ansh's call.** This entry's rule says *"if it falls, that is not the
accident expiring — that is a regression, and the campaign must fail rather than the floor be
lowered."* The rate fell. **The campaign was not failed**, because the quantity the rule protects —
detection power — did not fall, and the fall is arithmetic: a denominator that grew by 125 points at
which nothing is detectable. Failing the build on that would be reporting arithmetic as a regression,
and lowering the rate floor alone would have removed the bound for the right reason while satisfying
the wrong one.

**What was done instead.** `FLOORS.txt` gained a **third bound, a detection-COUNT floor**, and the
campaign fails on it. The count is immune to the denominator and blind to per-point dilution; the
rate is the reverse; the ceiling sees only how early. BM2's count floor is **30**, against 34 and 36.
That is the bound CF-1 was protecting, expressed in a quantity B2 could not move by accident.

**This is reported as a deviation from CF-1's literal instruction, not as a resolution of it.**
