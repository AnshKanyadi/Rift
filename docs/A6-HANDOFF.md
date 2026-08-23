# A6 handoff

**Written for a session that knows nothing.** A6 is **not closed** — sessions never close phases. What
changed since the last handoff is that the blocker is gone: **BUG-022 is root-caused and fixed, and
the 25,000-seed exit run came back clean.**

**Commit:** see `git log`. Tree clean.

---

## 1. Who you are and how this works

You are **Session A**, the Track A (Go) implementation pair. **Ansh is the architect and sole
decider.** `CLAUDE.md` is the constitution; read it. `docs/CARRY-FORWARD.md` is the standing
obligations ledger; read it second. `docs/DESIGN-A6-transactions.md` is this phase's record and is
long because the phase was.

**Protocol:**

- **No ruling echo.**
- **Keep the escape-hatch status line in every report** (see §7).
- **Do not restate a rule that is already recorded.** Record once, mechanise, move on.
- **Proceed on your own recommendation and report it.**
- **Four stop conditions — stop and report, do not proceed:** a frozen interface, oracle
  independence, determinism scope, **a defect found in a signed phase.**

Standing and non-negotiable: no gate counts until its failure has been induced; no checker is ever
weakened to pass; sessions never mark a phase complete; a fact is recorded, never inferred.

---

## 2. What A6 owed, and where each item stands

| # | item | state |
|---|---|---|
| 1 | **BUG-022 root-caused and fixed** | **done** — the read mark, `M71`/`M72`, BUGS.md, bundle |
| 2 | BUG-024 mutant-classed and in BUGS.md | **done** — `M73`, entry, bundle |
| 3 | The exit run re-run clean, 25,000 seeds | **done** — 0 violations, 97 inconclusive, at `611d0b9` |
| 4 | The three solo measurements | **two done, one owed** — see §4 |
| 5 | `BUG-015`'s bundle | **still red and still blocked** on the power measurement |
| 6 | `make mutant-covered` finished at full scale | **done** — 56 checked, 8 failures, §5 |

---

## 3. The exit run

```
aggregate:  10 shards covering [0,25000) at commit 611d0b9
verdicts:   pass=24903 violation=0 inconclusive=97 errors=0
cost:       8.33-8.47 s/seed, 5h47m-5h53m per shard, ~58 CPU-hours
```

Against **271** violations before BUG-021's fix and **184** after it. The inconclusive rate did not
move: 3.9‰ against 4.2‰ and 3.7‰, all unknown-dominated histories rather than checker timeouts.

BUG-022's two halves are non-vacuous at scale: **9,199,798** read marks staged, **226,660** prewrites
refused for a reader above the prewriter's snapshot. DESIGN-A6 §32.

**The per-seed cost more than doubled** — 8.4 s/seed against A6's mid-phase 3.75 — and the read mark
is part of it. **Measure the per-seed cost before planning the next sweep**: this one was planned
against 3.75 and took six hours.

---

## 4. The three measurements

**1. The unthrottled collector — DONE, solo.** 40 seeds, 49m32s:

```
unthrottled: 124437 collections proposed, 367263 applied, 6098 versions collected
throttled:     2549 collections proposed,   8194 applied, 5491 versions collected
ratio:       48.8x as many collections;  0 violations
```

**And the figure CARRY-FORWARD actually asked for, which that lane cannot produce.** The obligation is
about *detection*: DESIGN-A0 §7 item 9 records M53's class going from 1 detection in 60 seeds
unthrottled to 0 in 3,000 throttled. `TestPowerProbe` now takes `POWER_UNTHROTTLED=1`, and under A6's
shape at 200 seeds:

| | detections |
|---|---|
| M53, throttled | **0 of 200** |
| M53, unthrottled | **0 of 200** |

**A5's figure does not reproduce.** If the class were still 1-in-60 unthrottled, 200 seeds would have
found about three. The throttle is not what puts M53 out of reach at A6 — the schedule mix is, and
that is A2's M34 lesson again. What the two numbers do *not* establish is that the throttle costs
nothing; they establish that 200 seeds cannot tell.

**2. Mutant power floors and ceilings — RUNNING.** `POWER_JOBS=3 sh scripts/power-mutants.sh
--measure`. The critical path is `M46` at 3,000 seeds under `current`: about **7 hours**, which no
amount of parallelism shortens because one class is one sequential sweep. `POWER_JOBS` was added for
this and is verified to produce byte-identical output to a sequential run.

**Its result names three things**: `BUG-015`'s replacement bundle seed, whether `M67` becomes an
explicit opt-out and `M70` gets a real floor (§31 — the lane has been RED on both since they landed),
and every floor in the tree, which is still A5's.

**3. The race-lane curve at 50/100/200 — OWED, and the arithmetic says it cannot run as written.**
`race-curve.sh` runs the whole `sim/hunt` package under `-race` at each count. At A5's 0.36 s/seed the
lane was 90 minutes; at A6's measured **8.4 s/seed** the same shape is roughly **35 hours at the
*smallest* point**. That is a prediction from the per-seed cost, **not a measurement**, and it must be
run before it is believed — but if it holds, the question CARRY-FORWARD asked ("which of `RACE_SEEDS`
or `RACE_TIMEOUT` moves") has a third answer: **neither; the lane has to be restructured**, because a
90-minute budget and a 50-seed floor cannot both survive a 23× per-seed cost increase.

**And its premise is still broken**, which §21.4 records: CARRY-FORWARD says the lane "has found real
races twice" and there is no record behind it. The measurement cannot be *"is what 200 catches still
caught at 50"* because the two catches cannot be identified.

---

## 5. `make mutant-covered` — finished, and it found four

`56 checked, 2 skipped, 8 failures`, and the eight are two different things (DESIGN-A6 §25.3c).

**Four genuinely mispointed covering tests**, which is the defect the lane exists for:
`M15` (`sim/oracle.go:279`), `M29` (`raft/raft.go:2543-2545`), `M55` (`kv/store.go:217`),
`M60` (`kv/txn.go:204-205`). The canary was correctly uncovered, which is what makes the four
credible. **None is fixed**: re-pointing a covering test changes what the mutant suite asserts, and
that should land with its own verification rather than in a batch.

**Four ERRORs that are a budget failure.** `M65` and `M66` name **`TestRaftExitCriteria`** as their
covering test — the exit run, about **23 hours** at 8.4 s/seed. No timeout could let that finish, and
`make mutants` dies of the same cause: its baseline runs every covering test in one invocation and hit
3600s with `TestLeaderCompletenessOracleReportsNothing` at 34 minutes.

> **A covering test that is a phase gate is not a covering test.**

`M65`/`M66` have precise cheap tests already sitting in `kv/txn_test.go`
(`TestARollbackDoesNotStealSomebodyElsesLock`, `TestACommitDoesNotStealSomebodyElsesLock`).

**`make mutants` has not been run to completion since BUG-022's fix**, for that reason. It is owed.

---

## 6. Machine-bound work, and how to run it

```sh
make exit-run                                   # 25,000 seeds, 10 shards; ~6 h at 8.4 s/seed
COVER_JOBS=8 sh scripts/mutant-covered.sh       # ~3 h; sequential it does not finish
POWER_JOBS=8 sh scripts/power-mutants.sh --measure   # critical path is one 3,000-seed sweep, ~7 h
sh scripts/race-curve.sh                        # solo; see section 4
LANE_SEEDS=40 go test -run TestUnthrottledCollector ./sim/hunt/   # ~50 min
```

**Both `POWER_JOBS` and `COVER_JOBS` parallelise on the same argument**: a detection count, a
first-detecting seed, and whether a test executes a line are all functions of the seed and the tree,
so a parallel run reaches identical verdicts and the report is printed in order afterwards so the text
is identical too. **Wall-time-to-detection does not survive parallelism** — Amendment A2's other half
is measured at `POWER_JOBS=1` or not claimed.

---

## 7. The escape hatch — restate this line in every report

**Shut, and its condition is unmet.** Amendment A6 pre-authorised the TSO fallback *if A6's
uncertainty machinery is not green by 2026-12-01*. It is green and sweep-exercised — 28,768
uncertainty restarts across the 25,000-seed exit run — and Dec 1 remains the decision point.

**BUG-022 is the strongest argument yet for the fallback, and it is still not a reason to spend it.**
The defect exists because per-node HLCs do not order a commit timestamp after every start timestamp
issued before it, which a single TSO does by construction. A TSO would have made BUG-021 and BUG-022
both impossible. The fix that landed does not need one, so the pre-authorisation stays unspent — but
the next reader should know that the case for it is now made of two shipped defects rather than of
one hypothetical.

---

## 8. Things a fresh session will otherwise get wrong

- **A seed costs 8.4 s**, not 3.75 and not 0.36. Every lane budget in the tree predates that.
- **`make test` is `-short`** and takes ~400 s.
- **There is no remote.** CI has never executed. This phase found **three** lanes that had stopped:
  `provcheck`, `make test`, and **`power-mutants`, which has been red since `M67` and `M70` landed**
  (§31) — verified on a worktree at the previous handoff's commit, so it is not something BUG-022's
  fix caused.
- **Never `git add -A` with a patch applied.** `make hygiene` fails on any tracked `.orig`/`.rej`.
- **The corpus regenerates as a SEARCH, not a re-record.** BUG-022's fix moved every raft trace;
  seventeen bundles were regenerated and three then no longer reached their defects. `BUG-009` and
  `BUG-019` were re-found at seeds 105 and 9. `BUG-015` is still blocked. **`BUG-021` gets no bundle
  at all** — its defect is a pair (`M67`+`M68`) and no single mutant reintroduces it.
- **Three entries have no bundle**: `BUG-017`, `BUG-020`, `BUG-021`. BUGS.md rule 2 says every entry.
- **A7's design doc is written and waiting for rulings** (`docs/DESIGN-A7-readindex.md`), now with
  **D-A7-5**: BUG-022's read mark is a function of the log *only because every read is a log entry*,
  and read index is the phase that stops that being true. **Do not start A7 implementation.**
- **`sim/hunt`'s `modelRecords` has no caller**, so the model's records are never digested against the
  store's. Reported, not deleted.
