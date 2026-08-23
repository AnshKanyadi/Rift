# A6 handoff

**Written for a session that knows nothing.** A6 is **not closed**. This says exactly what is left,
what is established, and what the next step is.

**Commit:** `caee53f`. Tree clean, all fast lanes green.

---

## 1. Who you are and how this works

You are **Session A**, the Track A (Go) implementation pair. **Ansh is the architect and sole
decider.** `CLAUDE.md` is the constitution; read it. `docs/CARRY-FORWARD.md` is the standing
obligations ledger; read it second. `docs/DESIGN-A6-transactions.md` is this phase's record and is
long because the phase was.

**Protocol, as trimmed on 2026-08-22:**

- **No ruling echo.** Dropped — it did its job and no paste went missing in five phases.
- **Keep the escape-hatch status line in every report** (see §7).
- **Do not restate a rule that is already recorded.** Record once, mechanise, move on. §22.6's
  sentence existed and was violated four times anyway; the lane fixed it, not the fifth restatement.
- **Proceed on your own recommendation and report it.** Assume ratification; Ansh overrules in the
  report if needed.
- **Four stop conditions, unchanged — stop and report, do not proceed:**
  1. a frozen interface,
  2. oracle independence,
  3. determinism scope,
  4. **a defect found in a signed phase.**

Standing rules that are not negotiable: no gate counts until its failure has been induced; no checker
is ever weakened to pass; sessions never mark a phase complete; a fact is recorded, never inferred.

---

## 2. What closes A6

Ansh, verbatim: *"When BUG-023 and BUG-022 are both fixed, mutant-classed, in BUGS.md, and the exit
run comes back clean, report the aggregate. A6 closes on that, not before."*

| # | item | state |
|---|---|---|
| 1 | **BUG-022 root-caused and fixed** | **the only blocker with real work left** — see §3 |
| 2 | BUG-024 mutant-classed and in BUGS.md | **owed** — fix landed, no mutant, no BUGS.md entry yet |
| 3 | The exit run re-run clean, 25,000 seeds | owed, ~6 h wall on 10 shards |
| 4 | The three solo measurements | owed — `make solo`, needs the machine to itself |
| 5 | `BUG-015`'s bundle | owed — red, blocked on the mutant power measurement in (4) |

Everything else A6 owed is done and recorded.

---

## 3. BUG-022 — established, not yet root-caused

**Do this first.** It is a real safety violation and it is the last one blocking the exit run.

### The finding, established

Seed **2521**, `bank-conservation`: the audit at `1600000008790243029.0` reads all eight accounts and
they sum to **-19**, not 0.

```
txn 38   start 1600000008260000000.514    commit a06 at 1600000008320000000.770
txn 8    start 1600000008280137801.1024   commit a06 at 1600000008442578171.768   <- survives
```

- **Both wrote `a06`.** The audit sees txn 8's value (`a06 = -26@8`).
- **Txn 8 started before txn 38 committed `a06`**, so its read of `a06` cannot have contained txn 38's
  write, and its write was computed from that read.
- **Neither guard fired.** It should have been refused by `ErrKeyIsLocked` if it prewrote while txn 38
  held the lock, or by `newestCommit > startTS` (`ErrWriteConflict`) if it prewrote after the commit.
- **No restarts are involved**, so the timestamps are genuine — this matters, see below.

### The next step, and only this

**Dump the committed log for `a06` on seed 2521 in the window between txn 38's prewrite and its
commit, and determine which of the two guards was bypassed and how.** The prewrite is at range 7
index 71 (`start=…8260000000.514, val="-13@38"`) and the commit record at index 72. Txn 8's prewrite
of `a06` is the entry to find and place relative to those.

Reproduce with a throwaway test in `sim/hunt` that runs `hunt.MaterializeRaft(2521)` +
`hunt.RunRaft`, then walks `r.Ledger.Ranges()` / `rl.Committed()` decoding with
`store.DecodeTxnCommand`. That is the technique that cracked BUG-018, BUG-019 and BUG-023.

**Delete the throwaway before running any lane.** A scratch file with a compile error in
`sim/hunt` breaks the package build, and every lane over that package then reports "did not run" —
which cost an hour here and nearly caused a working lane to be removed.

### Two wrong turns already taken — do not repeat them

1. **"A roll-forward or rollback applied against the wrong transaction's version."** Disconfirmed:
   seed 10303 had **zero** mispointed apply-resolutions and still failed.
2. **A lost update claimed on seed 10303 between txn 0 and txn 31.** Wrong, and the reason is a
   defect that has since been fixed: `RecordTxnBegin` recorded a transaction's **original** start
   timestamp and nothing updated it on restart, so the ledger showed a start the system had abandoned.
   Txn 0 had restarted and its real start was *after* the commit it appeared to precede.
   `RecordTxnRestart` and `TxnRecord.Restarts` now exist — **check `Restarts` before reasoning about
   any two transactions' relative start times.**

---

## 4. What was fixed this cycle

| bug | what it was | state |
|---|---|---|
| **BUG-018** | the apply loop staged a whole `Ready` into one batch, so a transaction step could not see the steps above it | fixed, `M59` |
| **BUG-019** | commit/rollback deleted *the* lock rather than *their* lock, orphaning a committed version | fixed, `M65`/`M66` |
| **BUG-020** | (harness) a transfer prewrote a balance it never read | fixed |
| **BUG-021** | two transactions minted at one start timestamp shared a key's lock and version | fixed, `M67`/`M68` |
| **BUG-023** | a split-born range started with a fresh HLC and stamped below versions it inherited | fixed, `M70` |
| **BUG-024** | a read answer from a pre-restart incarnation landed in the post-restart snapshot | **fixed, NOT yet mutant-classed or in BUGS.md** |

**BUG-024 is the outstanding paperwork.** The fix is in `sim/hunt/bank.go`: a read answer is rejected
unless `cmd.ReadTS == t.startTS`, counted as `staleIncarnation`. `sim/hunt/bug022_test.go`
(`TestBUG024`) pins both seeds and currently fails on 2521 only. It needs a mutant that removes the
guard, and a BUGS.md entry. It is BUG-020's family: an answer accepted for the wrong incarnation.

---

## 5. `make mutant-covered` — built, working, and not yet trustworthy at full scale

**What it is.** A covering test must *execute* the line its mutant changes. It runs the covering test
on the **unmutated** tree with coverage on and requires the patched lines to have been executed.
Coverage is produced by execution, so it cannot be satisfied by claiming an entry point — which is
why it is this and not a sixth restatement of the rule.

Built because **four covering tests in one day called the guarded function inline rather than through
the path their mutant patches**, so the mutation could not affect them and they passed proving
nothing. Every one was caught only by the mutant surviving.

**Verified working:**
- **Induced** against a reconstruction of the exact mistake — a test calling `seedClockAtLeast`
  inline — which it reports `DEAD`.
- It **independently rediscovered `canary-mispointed`**, which declares `expect: alive` because it is
  deliberately aimed at a test that does not cover it. The lane now expects the canary
  **bidirectionally**: if the canary ever becomes covered, the lane fails and says so.
- Lines come from **applying the patch and diffing**, not from its `@@` header — headers go stale and
  `patch` tolerates it with fuzz; the first version read the header and reported a live path dead.
- **Multi-file patches fixed**: it copies every file the patch names, not only the first. Four
  patches were failing as "does not apply cleanly" when the ground had simply not been laid.

**Not trustworthy at full scale yet, and this is the known gap.** The full sweep **did not finish**.
It runs each covering test with no seed bound, so the heavy `sim/hunt` tests hit Go's 600-second
default and are reported `ERROR … did not run`. **It needs `RAFT_SEEDS` and an explicit `-timeout`
per invocation, exactly as `make test` and `make race` do** (see the tier comment at the top of the
`Makefile`). Until then a full run's error count says nothing.

It **is** in `make ci` and in `.github/workflows/ci.yml`. Given it cannot complete, **either bound it
or take it out of `ci`** before relying on a green there. The last partial run, on a clean tree, was
**1 DEAD (the canary) and 17 ok** before the timeouts began.

---

## 6. Machine-bound work, and how to run it

```sh
make exit-run     # 25,000 seeds, 10 contiguous shards, aggregate asserted; ~6 h
make solo         # the three owed measurements; needs the machine to ITSELF
make nightly      # covering tests at full seed ranges, then the 10k soak
```

- **The exit run is sharded and the split is proved sound.** `scripts/exit-run.sh` refuses a dirty
  tree, stamps every shard with the commit, and `TestRaftExitAggregate` requires the shards to sort
  into a contiguous cover of exactly `[0,25000)` at one commit. A gap, an overlap, or a short shard
  each fail by name. Proved end to end on 40 seeds across 4 shards before the real run.
- **The last full run was pre-fix and is a measurement, not a gate**: 271 violations (1.08%), 105
  inconclusive (4.2‰), first at seed 55. Preserved under
  `scratchpad/exitrun-prefix`. Fixing BUG-021 took it to 16 per 2,500.
- **`make solo`** is the unthrottled collector, the mutant power floors under A6's shape (~15
  CPU-hours), and the race curve at 50/100/200. All three are owed, all three need the machine alone,
  and **the power measurement is what names BUG-015's replacement seed**.

---

## 7. The escape hatch — restate this line in every report

**Shut, and its condition is unmet.** Amendment A6 pre-authorised the TSO fallback *if A6's
uncertainty machinery is not green by 2026-12-01*. It is green **and sweep-exercised** — 256
uncertainty restarts per 200 seeds, and the envelope refusal reached in its own 150% lane at 12,400
across 12 seeds. It was refused as BUG-021's fix for that reason: *a pre-authorisation consumed for a
purpose it was not granted for is an escape hatch widening itself.* Dec 1 remains the decision point.

---

## 8. Things a fresh session will otherwise get wrong

- **`make test` is `-short`** and takes ~398 s. Bounding the seed count was tried twice and failed;
  the cost is driven by the *number* of sweeping tests, not by any one bound. Full ranges are
  `make covering`, nightly.
- **There is no remote.** CI has never executed. `make hooks` installs a pre-push hook running the
  fast half. `make lane-coverage` keeps `make ci` and the workflow from drifting — two lanes were in
  one and not the other for a whole phase.
- **Never `git add -A` with a patch applied.** Three `.orig` files were committed at A1, A2 and A3
  and sat in the tree for four phases. `make hygiene` now fails on any tracked `.orig`/`.rej`.
- **A6's per-seed cost is ~4 s** against A5's 0.36. Plan sweeps accordingly.
- **A7's design doc is written** (`docs/DESIGN-A7-readindex.md`) to the point of decisions, with seven
  open questions each carrying a recommendation. **It is waiting for rulings. Do not start A7
  implementation.** Its §7 says A6's three owed measurements are taken *before* A7's first commit,
  because the term-start no-op moves the baseline they measure.
- **Three clock-dependent mechanisms are not established as exercised** (DESIGN-A6 §27.1). §27's third
  item — a snapshot read routed to a split-born range — needs `percolator-invariants` #6 as its
  oracle, because those reads are excluded from the linearizability history by construction; a lane's
  job there is to *reach* the state, not to judge it.
- **A surviving mutant has three meanings** (§25.1): no checker can see it, the test goes around the
  path, or **the code cannot be reached**. Only the third's response is deletion.
