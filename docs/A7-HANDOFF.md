# A7 handoff — written mid-run, for a session that knows nothing

**Written:** 2026-08-25 21:15, at commit `1f95db2`, with A7's exit run in its sixth hour and this
session at its context limit. **A7 is the last Track A phase and it is NOT complete.** Only Ansh
marks a phase complete, and he has not.

This file exists because the exit run has hours left and the session that launched it will not be the
session that reads it. Everything below is either a measurement or a pointer to one. Where something
is a projection rather than a reading it is labelled as such, because this phase spent real time
learning that a projection stated in a reading's voice is how five hours got mis-estimated once
already.

---

## 0. Read these first, in this order

| file | what it is |
|---|---|
| `CLAUDE.md` | the constitution. Amendments A1–A6 at the bottom, marked `[A<n>]` inline. |
| `docs/TRACK-A.md` | the stranger-facing wrap-up: what Track A is, what it verifies, and its limits. **Start here if you have never seen this repo.** |
| `docs/DESIGN-A7-readindex.md` | this phase. §8 is the exit criteria, §9 the thirteen rulings, §4/§4.1 the two audits owed at close. |
| `docs/CARRY-FORWARD.md` | measurements owed across phase boundaries. **M34 is owed here (§4 below).** |
| `BUGS.md` | 25 defects. BUG-025 is A7's, found by a five-step chain none of which was a code review. |

---

## 1. The exit run — identity, and how to tell whether it is still alive

**Started 2026-08-25 15:29. Commit `3d263e7`. 25,000 seeds, 8 contiguous shards.**

```
42477  sh -c make exit-run > <log> 2>&1; echo EXIT=$? >> <log>
42479   └─ make exit-run
42491       └─ sh scripts/exit-run.sh 25000 8 .exitrun     <-- THE RUN
42601..42608    └─ 8 × hunt.test                            <-- THE SHARDS
```

**Log:** `/private/tmp/claude-501/-Users-anshk-Desktop-Rift/57a3ad17-f0c7-426e-9268-cca7793a8aa3/scratchpad/exitrun.log`
— that path is the *launching session's* scratchpad. A fresh session gets a different scratchpad
directory; this absolute path is still readable on disk and is the only copy of the run's banner.

**Results land as** `.exitrun/*.json`, one per shard, written on shard completion.

### 1.1 Check that it is running — from the process, never from the launch

```sh
ps -o pid,etime,command -p 42491                    # the run
pgrep -f hunt.test | wc -l                          # expect 8 while running
ls .exitrun/*.json 2>/dev/null | wc -l              # shards landed, 0..8
tail -3 <log>                                       # 'EXIT=' appears only when make returns
```

**This rule is a finding, not a formality.** Earlier in this phase a run was believed to be running
for two hours when it had refused at launch. Ansh's ruling: *"'started' is read from the process
rather than from the launch."* Apply it here. If `42491` is gone and `EXIT=` is absent from the log,
the run died and did not report — see §3.2, which is exactly the gap that motivates the progress line.

### 1.2 Shard layout — 8 × 3,125, contiguous, half-open

```
shard 0  [     0,  3125)      shard 4  [ 12500, 15625)
shard 1  [  3125,  6250)      shard 5  [ 15625, 18750)
shard 2  [  6250,  9375)      shard 6  [ 18750, 21875)
shard 3  [  9375, 12500)      shard 7  [ 21875, 25000)
```

### 1.3 State at the time of writing

`0 of 8` shards landed at 5h42m elapsed. **No per-seed rate has been measured yet** — that is the
whole reason for §3.2. Do not report one until a shard lands and you can compute
`shard wall-clock ÷ 3,125`. Ansh asked specifically for the *measured* rate at the first landing,
"rather than projecting again."

### 1.4 The tree is TWO COMMITS AHEAD of the run, and that is a fact the aggregate has to carry

The run's binaries were compiled at `3d263e7`. Since launch:

| commit | time | what it touched |
|---|---|---|
| `3c2c696` | 17:01 | `scripts/exit-run.sh` banner, `sim/hunt/raftscenario.go` **(+35, additive)**, mutant declarations, BUG-024 bundle |
| `1f95db2` | 17:11 | `docs/` only |

The `raftscenario.go` change is `CurrentShapeName()` / `ShapeNameOf()` — **new functions, no change
to `A7Options()` or to any swept behaviour** (verify with `git show 3c2c696 -- sim/hunt/raftscenario.go`).
The running binaries sweep the same shape they would today.

**But the exit criterion says "exactly 25,000 seeds at one commit," and that commit is `3d263e7`,
not `HEAD`.** The aggregate must record `3d263e7`. Do not silently attribute this run to HEAD.

### 1.5 The banner in that log LIES, and the fix arrived too late to help it

The running log reads **"A6 exit run: 25000 seeds across 8 shards at 3d263e7"**. It is sweeping
**A7's shape**. `3d263e7` moved the sweep to A7 and the hardcoded banner did not follow; `3c2c696`
fixed it by computing the name from the options struct (`cmd/shapename`, `ShapeNameOf`) — 40 minutes
after this run launched. A fresh session reading that log without this paragraph would conclude the
exit run swept the wrong phase. It did not.

That miss is the **third instance** of a label that stopped describing its subject (`power-config: a3`;
the one `power:` label carrying two opposite claims; this banner). Three is why the name is now
computed rather than maintained.

---

## 2. When the first shard lands

1. **Report the measured per-seed rate** — `shard wall-clock ÷ 3125` — as a reading.
2. Then wait for the rest. Do not aggregate a partial set.

## 2.1 When all eight land

```sh
RAFT_SHARD_DIR=.exitrun RAFT_TOTAL=25000 go test -count=1 -run TestRaftExitAggregate -v ./sim/hunt/
```

The aggregate must prove, and the report must state:

- **contiguity and non-overlap** — the eight ranges tile `[0, 25000)` exactly, no gap, no double-count;
- **one commit** — every shard JSON carries `3d263e7`;
- **exactly 25,000 seeds**, not "about";
- **zero safety violations**;
- **the inconclusive rate, explained rather than reported** (Amendment A4: an inconclusive is never a
  pass; a rising rate is answered by shrinking the history window or partitioning harder per key,
  **never** by loosening the checker).

---

## 3. Two changes prepared but NOT applied

Ansh: *"Land it before the next exit run rather than as part of this one; do not touch the tree
mid-run."* Both are designed and neither is written. Notes are in the launching session's scratchpad
at `pending/NOTES.md`; the substance is reproduced here so nothing depends on that path surviving.

### 3.1 GOMAXPROCS per shard, in `scripts/exit-run.sh`

**The finding, which is worth more than the fix:** *the shard count controls how the seed space is
divided, not how much parallelism the machine sees.* Those two were conflated. Eight shards were
never eight processes — each `hunt.test` runs Go's own parallelism, so eight shards on 11 cores
produced a **load average of 21**. Every wall-clock estimate derived from the shard count was wrong
for that reason: not arithmetic, but multiplying the wrong number.

**The fix:** set `GOMAXPROCS` explicitly per shard, so total parallelism is `SHARDS × GOMAXPROCS`
and **both halves are chosen rather than emergent**.

### 3.2 A synchronous per-seed progress callback

**The gap:** running and hung look identical from outside. A shard writes its JSON only on
completion, so for hours there is no evidence of progress at all — and a run that *dies* is exactly
the case the missing line cannot distinguish from a run that is merely slow. That is the **seventh
instance** of the observability family in this repo.

**Where it goes:** `TestRaftExitShard` → `hunt.SweepRaft(from, to)` → the seed loop inside
`SweepRaftWith`. That loop is the only place that knows how far it has got, so progress needs a hook
*there*: a callback invoked per seed, **defaulted nil**, with the shard writing `seed N of M`
incrementally.

> **Constraint, and it is not a style preference: it must NOT be a goroutine and must NOT be a
> ticker.** That loop is the deterministic simulator's own. A second thread of control anywhere near
> it trades an observability gap for a determinism risk, which is a bad trade in this repository
> specifically. A synchronous callback on a loop the caller already owns adds no scheduling and
> cannot affect a trace.

---

## 4. M34's owed measurement

`M34-append-from-zero-over-a-snapshot` has **no measurement under A7's shape.** Its probe was
launched during the exit run, then killed — deliberately — because it was competing with the run for
the machine (its wrapper `m34.sh`, plus an orphaned `hunt.test` child identified by grandparent pid 1
rather than by name).

Ansh: *"M34 is owed rather than lost. Take it after the exit run, under A7's shape, with the machine
to itself."*

- **Its disposition is deferred until it is measured.** Do not declare, exempt, or re-floor it first.
- Standing rule that binds the result: *an exclusion from a measurement pass may cite a measurement
  or an argument about reachability, **never** the excluded class's own declaration.*
- Note it is also the mutant `corpus-reproduces` uses to test BUG-009 (§5.4) — the same class is doing
  two jobs and the measurement should not be confused with that verdict.

---

## 5. A7's remaining exit criteria

`docs/DESIGN-A7-readindex.md` §8 is authoritative. What is still OPEN:

### 5.1 The 25,000-seed aggregate — §2.1 above. **OPEN, running.**

### 5.2 §4's ten facts, with the honest count

Ten facts were named **before** the code (§4's table, 10 rows). Owed at close: the before-and-after,
**with exclusions stated**, and an honest count of how many became defects — *including any that did.*

The honesty is the entire value. A6 named six and **it was not six of six**, and saying so is what
makes the number credible at all. A clean count reported cleanly would be worth nothing.

### 5.3 §4.1's assumption audit

Seven assumptions, **three of which this system does not provide** — re-asked against the code that
actually landed, not against the code that was proposed.

**Why there are two audits and not one** (ruling 10): they fail differently. The fact table asks
*where is this fact taken from*, and BUG-022 was a fact **nothing took** — there was no derivation to
walk to, so the table could not see it. *Naming every fact you take is not the same as naming every
fact you need.* The audit is the second one in the form the miss would have been caught by.

`§4.1a` names **P-NOOP** — the premise D-A7-6 rests on (`Propose` never issues the zero `ProposalID`)
— written to expire loudly. Check that it has not silently expired.

### 5.4 `corpus-reproduces` — **state it, do not assume it**

Last full run: **18 `ok`, 4 `skip`, 2 `WEAK`.**

| bundle | state | note |
|---|---|---|
| BUG-024 | **`ok`** | re-pinned this phase, 10303 → 5042 |
| BUG-022 | `ok` | re-pinned this phase, 2521 → 266 |
| BUG-009 | **`WEAK`** | diverges under `M34-append-from-zero-over-a-snapshot` but the reproduction is not tight |
| BUG-015 | **`WEAK`** | diverges under `M46-split-inherits-the-appended-configuration` |

**Neither BUG-009 nor BUG-024 is retired on a null.** That is Ansh's standing instruction: a search
that finds nothing is not a verdict that there is nothing.

`make corpus` (every bundle still replays) and `make corpus-reproduces` (every bundle still
*exercises its defect*) are **different questions**, and A5 paid to learn it. The no-op moved every
trace, so regeneration is a **search** and its reproduction verdict is **read rather than assumed**.

### 5.5 Power floors and ceilings under A7's shape, refutation pass reported

Every floored class measured against the shape the no-op produces — **not inherited from A6's** —
and `make power-refute` run with its result stated: how many claims re-measured, how many refuted,
which are exempt and on what argument. **Every `power-covered-by` instrument is run, not read.**
A7's own exemptions use the split labels (`power-covered-by:` / `power-unreachable:`) from the start;
the bare `power:` is retired and survives only on a patch that must SURVIVE (`expect: alive`).
`make power-decl` checks declarations in milliseconds; the sweep costs ~15 CPU-hours.

### 5.6 Already discharged this phase — do not redo

- The term-start no-op landed **alone**, on a committed baseline, with a 300-seed re-measurement and
  **one reason per moved number** (7 of 55 attributed against three control contrasts).
- Read index implemented under all thirteen rulings; the differential oracle built, and **corrected
  via M76 after it was wrong three times**.
- BUG-025 found and fixed (codec dropped `ReadCtx`/`ReadIndex`).
- The refutation pass built: **3 of 17 opt-outs were false** (M56, M30, M67); M30 planted a
  first-tier safety defect (leader completeness).
- The 59 gating-path verdicts closed.

---

## 6. Standing constraints — these bind every A7 report until Ansh closes the phase

1. **Ruling echo verbatim at the top of every A7 report.** The text is D-A7-6, quoted in full in
   `docs/DESIGN-A7-readindex.md` §3a — echo it from there, do not paraphrase.
2. **Escape-hatch line every report.** State whether the Dec-1 TSO trigger, the lease deferral, and
   the freeze have been touched.
3. **No gate counts until its failure has been induced.**
4. **No checker is ever weakened to pass** — not the timeout, not the model, not the operation set.
5. **Never mark a phase complete.** Ansh does.
6. **Stop and report on exactly four conditions:** contradiction with a frozen interface; with oracle
   independence; with determinism scope; or a defect found in a signed phase. Otherwise continue.
7. **The frozen interface opens once**, for `raft.Configuration()` taking an index — the site that
   made BUG-015 possible. Anything else waits and rides with it. *A request to open it for
   convenience is refused.*
8. **An exclusion from a measurement pass may cite a measurement or an argument about reachability,
   never the excluded class's own declaration.**
9. **A pre-ruling may fix what a result MEANS. It may not fix that before the result's PROVENANCE is
   established.**
10. **"Started" is read from the process, never from the launch.**

**Worktree boundary:** Track A is `/Users/anshk/Desktop/Rift`, Track B is
`/Users/anshk/Desktop/rift-b`. Single-writer each. **Never write Track B work into this tree.**
Track B is parked at DESIGN-B1 rev 4.

---

## 7. Escape hatch

**Unused.** The Dec-1 TSO fallback trigger is untouched; ruling 1 holds the lease deferral shut; the
freeze earned itself — A7 landed on shapes DESIGN-A0 D5 already specified.
