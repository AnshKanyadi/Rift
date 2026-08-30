# Rift

A distributed, transactional, MVCC key-value database. The distributed layer is from-scratch
multi-group Raft in Go. Underneath it is a from-scratch C++ LSM storage engine. Both are checked by
deterministic simulation rather than by hand testing.

## Status: v1 is complete

Fifteen signed phases — A0 through A7, B1 through B5, I1 and I2 — plus the merge where the two tracks
met. The Go distributed layer, the C++ storage engine underneath it, and the whole thing running as
three real processes over real sockets while a chaos script kills the leader.

**[docs/V1.md](docs/V1.md) is the one document to read.** It covers what exists, what was verified,
how, and — in the same file, not an appendix — what the verification cannot see and what is still
open.

## The numbers

| | |
|---|---|
| A6 and A7 exit runs | **25,000 seeds each, zero safety violations**, inconclusive rate 4.0 per mille |
| defects found, every one reproducing from a seed | **67** |
| mutant classes with a covering test and a measured floor | **78 Go, 155 C++** |
| times a checking mechanism reported success while checking nothing | **30**, and the register is the most useful thing in the repo |
| escape hatches in the determinism pass | **5**, each one line with a written reason, unchanged since A5 |
| I2 chaos | **2,357 operations linearizable**, 3 leader kills of 3, 3 restarts, 0 uninvited exits |
| I2 steady state on the C++ engine | **97–119 ops/s, p50 67–82 ms** |
| I2 under chaos | **88.8% of steady state, p99 122 ms** |

I2's four thresholds were declared before any number was taken. **Two were met and two were not, and
none was adjusted** — a threshold that fires is a threshold that was capable of firing.

**The single result worth reading is [BUG-060](BUGS.md):** a Raft liveness bug that survived eight
phases and 25,000 seeds. After a leader kill the cluster served nothing for fourteen seconds with
every safety oracle green — and correct to be green, because a cluster that elects nobody does no
wrong thing. It was found by a threshold declared in advance rather than by any checker.

## How it is verified

This part matters more than the feature list. Implementing Raft is not the hard bit. Knowing whether
your implementation is wrong is.

**Deterministic simulation.** The whole distributed layer runs on one thread, on an event loop, with a
virtual clock. No goroutines, channels, locks, wall-clock reads, or real network and disk calls in any
package that runs during a simulation. A custom vet pass (`tools/determinismcheck`) turns those into
build failures, including `range` over a map. Exemptions need a written reason and are listed in
[HATCHES.txt](HATCHES.txt), which a test diffs against the tree. The vet pass is itself mutation
tested by `make blind`. Same seed, same trace, every time.

**Faults are on by default.** Dropped, delayed, duplicated and reordered messages, symmetric and
asymmetric partitions, crashes, restarts, pauses, lost unsynced writes, and per-node clock drift and
jumps inside `maxOffset`. Every injector counts how often it fired, and a run where an enabled
injector never fired is a failure, because a chaos test that did nothing proves nothing.

**Everything replays.** A seed produces a plan, which is a readable, serializable description of the
entire run. Plans carry keyed-PRF parameters instead of RNG state, so replaying takes no live
randomness at all. Seeds reproduce at the commit that found them, and plans reproduce at any commit.
Both live in [`seeds/`](seeds/).

**The harness is tested against known bugs.** A passing test suite tells you the harness runs, not
that it catches anything. `sim/mutants/` holds 71 deliberately broken versions of the code: acknowledge
before fsync, serve a stale read, apply a retried request twice, and so on. Each has a covering test
that has to kill it, and a measured detection rate. If a mutant survives, the harness is too weak and
the phase is not done. Every entry in [BUGS.md](BUGS.md) names the mutant class that would have caught
it, and if none existed, a new one lands with the fix.

**Checkers do not get loosened.** Linearizability checking is bounded, so a check that runs out of
budget is reported as inconclusive and never as a pass. Inconclusive results get their own column in
[SOAK.md](SOAK.md). If that rate goes up, the fix is to make the problem smaller, not the checker
weaker.

**What the simulator does not model** is written down rather than left to be discovered: computation
is instant unless slowness is injected, the network has no congestion model, replay determinism is
scoped to the Go reference engine, and Byzantine faults are out of scope. Full list in
[DESIGN-A0 §7](docs/DESIGN-A0-simulator.md).

## What it does

- From-scratch Raft: elections, replication, persistence, snapshots, pre-vote, leadership transfer,
  single-node membership changes with learner catch-up
- Multi-raft: one Raft group per range, size-threshold splitting, manual replica movement
- Distributed transactions over MVCC: Percolator-style 2PC, snapshot isolation, hybrid logical clocks
  with uncertainty intervals
- Linearizable reads via read index, including follower reads
- C++ LSM engine behind a batched cgo interface (Track B, in progress)

Joint consensus, parallel commits, leader leases and automatic load balancing are deliberately out of
scope for v1. The reasoning is in [STRETCH.md](STRETCH.md) and none of them is claimed anywhere.

## Layout

| path | what |
|---|---|
| `raft/` | the consensus state machine. `Step` and `Tick` in, a `Ready` struct out. No I/O, no clock, no goroutines. |
| `store/` | multi-raft node: many groups over one transport, persist and apply loops, splits |
| `kv/` | MVCC and transactions |
| `router/` | client library, range cache, transaction coordinator |
| `clock/` | hybrid logical clock with `maxOffset` |
| `engine/` | the storage interface both engines implement. `engine/model` is the Go reference. |
| `engine-cpp/` | the C++ LSM engine and its cgo bindings |
| `sim/` | event loop, fault injectors, checkers, and the mutant patches |
| `raftcheck/` | the safety oracles, which read a ledger of observed events and nothing else |
| `cmd/simctl/` | `run`, `replay`, `hunt` |
| `internal/rng/` | a project-owned PCG64 with pinned test vectors |
| `tools/` | the vet passes and the lane pins |

## Building

```sh
make help      # every lane, and which are real vs still stubs
make test      # unit tests, seed searches bounded
make race      # unit tests under -race
make lint      # vet, formatting, the determinism pass
make smoke     # 500-seed smoke run
make ci        # everything the push lane runs
make exit-run  # 25,000 seeds across contiguous shards
```

Lanes come in three tiers, split by cost rather than importance. Every-change lanes run from the
pre-push hook. Nightly lanes are the full-range covering tests and the 10,000-seed soak.
Solo lanes need the machine to themselves and take hours, which is the exit run and the mutant power
floors.

Go version is pinned by `go.mod`.

## Documents

- **[docs/V1.md](docs/V1.md) is the place to start.** It spans both tracks: what was built, what was
  found, how it is verified, what the verification cannot see, and what is still open.
- [docs/TRACK-A.md](docs/TRACK-A.md) is the Go layer in more depth, phase by phase.
- [REPORTS/](REPORTS/) has I2's session reports, including the retractions.
- [CLAUDE.md](CLAUDE.md) is the project constitution and its amendment log
- [docs/](docs/) has one design doc per phase, with the rejected alternatives and why
- [BUGS.md](BUGS.md) has every defect, its seed, its root cause, and the invariant that caught it
- [SOAK.md](SOAK.md) is the cumulative verification ledger
- [STRETCH.md](STRETCH.md) is what is deliberately out of scope
- [BENCHMARKS.md](BENCHMARKS.md) is methodology first, numbers second

## License

MIT, see [LICENSE](LICENSE).
