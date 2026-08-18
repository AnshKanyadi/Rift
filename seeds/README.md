# seeds/ -- the failing-seed corpus

Every bug this project has ever found lives here as a **bundle**, and every bundle reproduces.

A bundle is a directory:

```
seeds/BUG-007/
  plan.json      complete, self-contained schedule (see below)
  meta.json      seed, commit sha, workload, trace hash and per-step hashes, outcome,
                 and the finding: checker, detail, virtual time, first violating step
  history.json   the client history the checker judged
```

**Naming.** A bundle directory is named after the entry it belongs to, and the two numbering
spaces are separate: `seeds/BUG-NNN` for a [BUGS.md](../BUGS.md) entry -- a defect in Rift --
and `seeds/TOY-NNN` for a [docs/TOY-FINDINGS.md](../docs/TOY-FINDINGS.md) entry, which is a
defect in the calibration fixture rather than in the system under test. They were briefly
sharing the `BUG-` prefix, which meant a stranger following a BUGS.md reference could land on a
toy bundle and read it as a Rift defect. That is precisely the confusion BUGS.md's own
front matter exists to prevent.

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

## The lane

`make corpus` replays every bundle in this directory and fails when one does not reproduce.

It checks two claims, which are not the same claim:

- **the verdict** -- the finding the bundle exists to carry is found again. This is what the
  corpus promises and what a stranger checks.
- **the trace** -- the run is bit-identical to the recorded one. That is a property of the
  *harness* at the recorded commit, so a deliberate harness change legitimately moves it.

Both fail the lane. A moved trace hash is not automatically a defect, but it is never a
non-event: it means the corpus and the code have diverged, and the resolution is to regenerate
the bundle **in the same commit that moved it** -- the same discipline as the fresh-process
hash for seed 4242, which was moved once and recorded.

Corpus rot is silent by nature. The directory is still there, the JSON still parses, and nobody
runs `simctl replay` on a months-old entry unless a lane makes them. When this lane was written
both stored bundles had already rotted -- same finding, different trace -- and the only reason
anyone noticed is that somebody typed the command by hand.

`cmd/simctl/corpus_test.go` holds the lane and its induction: a bundle is copied, damaged one
way at a time, and the replay is required to refuse it.
