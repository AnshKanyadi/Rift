# SOAK.md — cumulative verification ledger

Append-only. Written by the soak runner (`soak/`), not by hand. Every public claim about seeds,
operations, or CPU-hours quotes this file and nothing else.

## Reading this file

- **Violations** — a checker reported a safety violation. Any nonzero value halts feature work on
  that track until root-caused, with a [BUGS.md](BUGS.md) entry.
- **Inconclusive** — a checker could not decide within its budget; almost always a linearizability
  check that hit its timeout. **Never counted as a pass.** *(CLAUDE.md Amendment A4.)* Rising
  inconclusive rates are fixed by shrinking per-run history windows or partitioning harder per key —
  never by loosening a checker, raising a timeout, or narrowing a model.
- **CPU-hours** — wall-clock seconds × workers actually running, summed. Not an estimate.
- **Ops** — client operations executed by the workload, not simulator events.

## Totals

| metric | value |
|---|---|
| Seeds executed | 0 |
| Operations executed | 0 |
| CPU-hours | 0.0 |
| Safety violations | 0 |
| Inconclusive results | 0 |
| Inconclusive rate | n/a |

*A0 is in progress; the soak runner does not exist yet. These totals are zero because nothing has
run, not because everything passed. That distinction is the entire point of this file.*

## Ledger

| date | commit | lane | workload | seeds | ops | CPU-h | violations | inconclusive | notes |
|---|---|---|---|---|---|---|---|---|---|
| — | — | — | — | — | — | — | — | — | no runs yet |
