# DESIGN-A0.9: the oracle framework, as landed

**Status:** landed for review (checklist step 6). **Author:** Claude. **Decider:** Ansh.
**Decides nothing new.** The design is DESIGN-A0 D12, DR-18 and Amendment A4, all approved. This
records what landed, the dependency pin, and which mutant classes will target this framework when
step 10 rebuilds the mutant suite as patches.

---

## 0. The general shape: can the code stop existing?

**When a rule collides with code, the first question is whether the code can stop existing. A hatch
is only considered after that answer is no.**

The `at_frac` cycle is the worked example. A float in the serialized plan was proposed with a hatch;
the ruling refused it, and the answer to "can this code stop existing?" turned out to be yes — the
authoring vocabulary became exact integer constructors, `clock/frac.go` was deleted outright, and
there was no boundary left to concentrate. Then `internal/rng`'s float surface was found to have
**zero consumers** once the clock and plan paths were integers, and it went too, taking four more
hatches with it.

`HATCHES.txt` went from **11 entries to 2** during a cycle that added features. That direction is the
outcome to want: a growing exception list means rules are being worked around, and a shrinking one
means they are being satisfied.

(It rose to 4 by the end of the next cycle, for the hunt harness's wall-clock timer — a report metric
that measures the harness from outside every run. Every entry earns itself or leaves.)

## 1. The scope split, and why it is a split

**History collection runs in-sim.** `sim.History` is dependency-free, obeys every determinism rule,
and lives inside Amendment A5's boundary. **Checking runs after the run**, imports porcupine, and
needs a timeout to bound an NP-hard search — so `sim/checker` sits outside the boundary by ruling and
is named in `determinismcheck`'s exclusion list.

Those are different jobs with different constraints. Putting them in one package would have forced
one of the two to be wrong: either the collector would have inherited an external dependency and a
wall-clock timeout, or the checker would have been denied both.

**Dependency pin: `github.com/anishathalye/porcupine v1.3.0`**, one of the four CLAUDE.md
pre-approves. It is used for exactly one thing: deciding whether a client-observed history is
linearizable.

---

## 2. Three verdicts, and the one that keeps the ledger honest

`Verdict` is `pass | violation | inconclusive`, plus a rejected zero value.

**Inconclusive is not a shade of pass.** A check that hit its timeout has established nothing, and
"zero violations across N seeds" is quietly false if a fraction of those seeds never finished
checking (Amendment A4). `Verdict.CountsAsPass` is the single banking site, in the shape blessed for
`OutcomeKind`: policy on one method of the type it belongs to, never at call sites.

**The zero verdict is rejected.** A checker that forgot to set one is converted to inconclusive by
`CheckAll` rather than defaulting to green — the same discipline as the unset hold realization and
the zero wall epoch. A forgotten field is never a decision.

---

## 3. The floor: an empty history is inconclusive, never pass

A checker handed zero operations returns green **by construction**. That is the silent-success
failure the count-not-presence rule exists to catch, and it would poison the soak ledger from the
first banked hour, because nothing distinguishes *"checked and found nothing wrong"* from *"checked
nothing"*.

So every checker declares a floor (`MinOps`), `CheckAll` enforces it before the checker is ever
called, and every report carries `Consumed` and `Min` — **including on a pass**, so a report that
consumed almost nothing is visible without being hunted for. The linearizability floor is two: one
operation is linearizable by inspection and proves nothing.

---

## 4. Independence, and what a safety oracle may not do

`View` is the read-only façade an oracle sees: `Now`, `Steps`, `Down`. It offers **no way to reach
node state**, and that is the point — the interface *is* the oracle-independence rule, rather than a
convention someone has to remember. porcupine consumes client-observed histories only.

**A timeout is not a violation.** A partitioned cluster that stops answering is behaving correctly,
and an oracle that scores that as a safety failure trains everyone to ignore it. An operation that
timed out or never returned stays in the history as *unknown with an open-ended return*, so the
checker considers both the world where it happened and the world where it did not. Dropping such
operations instead would silently make every partition look clean — which is worse than either
answer.

Liveness is measured separately and never gates a safety claim.

---

## 5. Histories carry simulated time only

Call and return instants are `clock.Instant`. A wall-clock timestamp anywhere in a history would be
the determinism rule failing in the one place we are least likely to notice, because a history is
consumed post-run by a checker whose output nobody reads when it passes.
`TestHistoryCarriesNoWallClock` pins it structurally: if either field becomes a `time.Time`, it stops
compiling.

---

## 6. The gate, induced in both halves

Per standing policy, a gate is not landed until its failure has been induced and observed.

- **Fixture proves the checker.** `TestNonLinearizableHistoryIsAViolation`: two writes complete in
  order, then a read strictly after both observes the overwritten value. Reported as a violation
  naming the key and the operation count.
- **Planted hit proves the wiring.** `TestPlantedViolationHaltsTheRun`: an oracle fires mid-run, the
  loop stops, and the outcome is `OutcomeHalted`, which does not count toward soak hours. The dump:

```
VIOLATION planted violation for the wiring test
  plan:   seed 4242 / plan.json
  at:     instant 50000000, step 5
  census: tick=5
```

The plan reference is the load-bearing line. A dump that says a checker fired without saying which
plan produced it is a bug report nobody can reproduce, which is the same as no bug report.

- **The third verdict is induced too.** `TestTimeoutIsInconclusiveNotPass` runs the checker with a
  one-nanosecond budget so the search cannot finish, and asserts the result is inconclusive and does
  not count as a pass.

Halting is at the **first** violation, not the last: a checker firing after the system has already
gone wrong is reporting a consequence, and the investigator wants the cause.

---

## 7. Idealization, for DESIGN-A0 §7

**Linearizability checking is bounded per key and per run.** Histories are partitioned by key — they
are independent, so a wide key space is many small problems rather than one large one — and each key
is checked with a timeout. A run whose history exceeds the budget is reported inconclusive rather
than checked partially and called clean.

The consequence, stated: we do not check cross-key linearizability, and we do not check a run's whole
history as one problem. Single-key linearizability is what the invariant list claims, and it is what
this checks.

---

## 8. Mutant classes that will target this framework (step 10)

Specified now so the work is already described when the mutant suite is rebuilt as patches:

| mutant | injected bug | must be caught by |
|---|---|---|
| `M8-floor-ignored` | `CheckAll` runs a checker below its floor | the empty-history test: an empty history reports pass |
| `M9-timeout-as-pass` | porcupine's `Unknown` mapped to `VerdictPass` | the timeout test |
| `M10-oracle-not-wired` | `OnStep` violations discarded by the loop | the planted-violation test: outcome is deadline, not halted |
| `M11-halt-on-last` | the loop records the last violation rather than the first | the first-violation test: step count is wrong |
| `M12-timeout-as-violation` | an unavailable operation scored as a safety failure | the unavailability test |
| `M13-dump-without-plan` | the violation dump omits the plan reference | the dump test's plan-reference assertion |

Each gets a covering-test header in the same shape the blind patches use: `# id`,
`# covering-test`, `# expect`, `# blinds`.
