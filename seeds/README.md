# seeds/ -- the failing-seed corpus

Every bug this project has ever found lives here as a **bundle**, and every bundle reproduces.

A bundle is a directory:

```
seeds/BUG-007/
  plan.json    complete, self-contained schedule (see below)
  meta.json    seed, commit sha, config, failing checker, first violating step, virtual time
  trace.jsonl.zst   optional; only for bugs where the trace itself is the explanation
```

## Two levels of reproduction, and the difference matters

- **From the seed** -- `simctl replay <seed>` -- reproduces the run exactly, *at the commit recorded
  in `meta.json`*. Seeds regenerate everything, so any code change that shifts random consumption
  produces a different (still deterministic) run.
- **From the plan** -- `simctl run --plan seeds/BUG-007/plan.json` -- reproduces the schedule **at
  any commit**. Plans carry keyed-PRF parameters rather than sequential RNG state, so plan execution
  takes no live randomness at all: a poisoned RNG panics if any sequential draw is attempted.

That second property is why the corpus is a regression suite rather than a museum. It is also what
makes minimization sound: deleting one fault entry leaves every other dice outcome bit-identical, so
delta-debugging tests what it thinks it is testing.

## Rules

- Bundles are **minimized** before they land. An unminimized bundle is a bug report nobody will read.
- Every bundle is referenced by a [BUGS.md](../BUGS.md) entry, and every BUGS.md entry references a
  bundle.
- The whole corpus reruns in verification mode at I1 against the C++ engine, where trace identity is
  not required but every checker must still pass.

*(empty -- A0 in progress)*
