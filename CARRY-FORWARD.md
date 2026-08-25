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

**Closing note (B2):** *not yet discharged.*
