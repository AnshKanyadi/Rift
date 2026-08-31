# Rift

A distributed, transactional, MVCC key-value database written from scratch. The distributed layer is
Go: multi-group Raft, range splitting, MVCC over hybrid logical clocks, Percolator-style
transactions, linearizable reads via read index. Underneath it is a C++ LSM storage engine reached
through a batch cgo interface.

The unusual part is not the feature list. It is that every safety claim here was produced by a
harness that is itself tested, and that the repository records what the verification cannot see in
the same document as the claims.

## Status: v1 is complete

Fifteen signed phases: A0 through A7 on the Go side, B1 through B5 on the C++ side, then the merge
between the two tracks, I1 (the Go stack running on the C++ engine) and I2 (three real processes
over TCP, chaos, benchmarks).

**[docs/V1.md](docs/V1.md) is the document to read.** It covers what exists, what was verified, how,
what the verification cannot see, and what is still open. Nothing below repeats it.

## The numbers

| | |
|---|---|
| signed phases | 15, plus the merge |
| A6 and A7 exit runs | 25,000 seeds each, 0 safety violations, 97 and 100 inconclusive |
| defects, every one reproducing | 67, of which 60 on Track A and integration, 7 on Track B |
| mutant classes, each with a covering test and a measured floor | 78 Go, 155 C++ |
| escape hatches in the determinism pass | 5, each one line with a written reason |
| times a checking mechanism reported success while checking nothing | 30, recorded and numbered |
| I2 chaos | 2,357 operations, linearizable, 3 leader kills of 3, 3 restarts |
| I2 steady state on the C++ engine | 97 to 119 ops/s, p50 66.9 to 81.8 ms |
| I2 under chaos | 86 ops/s, 88.8% of steady state, p99 122 ms |

I2 declared four thresholds before any number was taken. Two were met, one was not, and one was
never measured. None was adjusted. See [BENCHMARKS.md](BENCHMARKS.md) for methodology and
[docs/V1.md](docs/V1.md) section 7 for the verdicts.

The single result worth reading is BUG-060 in [BUGS.md](BUGS.md): a Raft liveness defect that
survived eight phases and 25,000 seeds. After a leader kill the cluster served nothing for fourteen
seconds with every safety oracle green, and correct to be green, because a cluster that elects
nobody does no wrong thing. It was found by a threshold declared in advance rather than by any
checker.

## Architecture

`raft/` is a pure state machine: `Step` and `Tick` in, a `Ready` struct out, with no goroutines, no
clock and no I/O. `store/` hosts many Raft groups over one transport and executes splits. `kv/` is
MVCC and Percolator transactions; `router/` is the client with the range cache and the transaction
coordinator; `clock/` and `hlc/` are the hybrid logical clock. `engine/` is the storage interface
both engines implement, with `engine/model` as the Go reference and `engine-cpp/` as the C++ LSM
(skiplist memtable, checksummed WAL, block-based SSTables with bloom filters, leveled compaction,
MANIFEST, range tombstones).

That purity is what makes the verification possible. `sim/` runs the whole distributed layer on one
thread against a virtual clock and a seeded PCG64, injecting drops, delays, duplicates, reorders,
partitions, crashes, restarts, lost unsynced writes and clock skew. The same seed gives the same
trace on the Go reference engine. `tools/determinismcheck` turns a wall-clock read, a global rand
draw, a map range or a goroutine in core scope into a build failure. `raftcheck/` holds the safety
oracles, which read a ledger of observed events and nothing else. `sim/mutants/` holds 78
deliberately broken versions of the code, each with a covering test that has to kill it.

| path | what |
|---|---|
| `raft/` | the consensus state machine |
| `store/` | multi-raft node: many groups over one transport, persist and apply loops, splits |
| `kv/` | MVCC and transactions |
| `router/` | client library, range cache, transaction coordinator |
| `clock/`, `hlc/` | hybrid logical clock with `maxOffset` |
| `engine/` | the storage interface; `engine/model` is the Go reference |
| `engine-cpp/` | the C++ LSM engine, its own test rig, and the cgo bindings |
| `sim/` | event loop, fault injectors, checkers, mutant patches |
| `raftcheck/` | the safety oracles |
| `net/`, `node/`, `cmd/riftnode` | real mode: TCP transport, mailbox driver, the node binary |
| `chaos/`, `bench/` | the chaos runner and the load generators |
| `cmd/simctl/` | `run`, `replay`, `hunt` |
| `internal/rng/` | a project-owned PCG64 with pinned test vectors |
| `tools/` | the vet passes and the lane pins |
| `seeds/` | the failing-seed corpus, one plan bundle per historical defect |

## Running it

`make help` lists every lane with a one-line description, and `make lanes` says which are real and
which are still stubs. Go version is pinned by `go.mod`; the C++ side needs CMake and vendors
GoogleTest at a pinned commit rather than fetching it.

```sh
make build          # compile everything
make test           # Go unit tests, -short: every path runs, no path is swept
make race           # -race over every package but sim/hunt
make lint           # vet, formatting, and the determinism pass
make smoke          # 500-seed smoke run
make corpus         # replay every bundle in seeds/
make mutants        # the mutant suite
make chaos-smoke    # I2's real-mode mechanisms, actually run
make cpp-ci         # the whole Track B lane set, with networking disabled
make ci             # everything the push lane runs
```

Lanes are tiered by cost. The every-change tier runs from the pre-push hook (`make hooks` installs
it). `make nightly` is the full-range covering tests and the 10,000-seed soak. `make solo` is the
three measurements that need the machine to themselves. `make exit-run` is the phase gate: 25,000
seeds across contiguous shards, hours per shard.

To replay a historical defect, `simctl replay --bundle seeds/BUG-0NN`. A seed reproduces at the
commit that found it; the plan bundle reproduces at any commit.

## Scope

In v1: from-scratch Raft with elections, replication, persistence, snapshots, pre-vote, leadership
transfer and single-node membership changes with learner catch-up; multi-raft with size-threshold
splits and a manual rebalance command; MVCC with hybrid logical clocks and uncertainty-interval
reads; Percolator-style snapshot-isolated transactions; linearizable reads via read index; the C++
LSM engine behind the batch cgo interface.

Deliberately out of scope: joint consensus, parallel commits, leader leases, automatic load-based
balancing. The reasoning is in [STRETCH.md](STRETCH.md), and none of them is claimed anywhere.

## Documents

- [docs/V1.md](docs/V1.md), the close document. Start here.
- [BUGS.md](BUGS.md), every defect: symptom, seed or kill point, root cause, fix, the invariant that
  caught it, and which mutant class would have caught it.
- [BENCHMARKS.md](BENCHMARKS.md), methodology first, numbers second. It records I2's full-stack
  numbers, B5.5's cgo boundary cost and B3.7b's compaction amplification, and says which of its
  tables are still placeholders.
- [docs/TRACK-A.md](docs/TRACK-A.md), the Go layer phase by phase.
- [REPORTS/](REPORTS/), I2's session reports, including the retractions.
- [SOAK.md](SOAK.md), the cumulative verification ledger with its inconclusive column.
- [STRETCH.md](STRETCH.md), what is deliberately outside v1.
- [HATCHES.txt](HATCHES.txt), the five determinism escape hatches, diffed against the tree by a test.
- [CLAUDE.md](CLAUDE.md), the project constitution and its amendment log.
- One design doc per phase, each with the rejected alternatives and the reasons:
  [A0](docs/DESIGN-A0-simulator.md),
  [A1](docs/DESIGN-A1-raft.md),
  [A2](docs/DESIGN-A2-snapshots.md),
  [A3](docs/DESIGN-A3-membership.md),
  [A4](docs/DESIGN-A4-multiraft.md),
  [A5](docs/DESIGN-A5-mvcc.md),
  [A6](docs/DESIGN-A6-transactions.md),
  [A7](docs/DESIGN-A7-readindex.md),
  [B1](docs/DESIGN-B1-engine.md),
  [B2](docs/DESIGN-B2-sstables.md),
  [B3](docs/DESIGN-B3-compaction.md),
  [B4](docs/DESIGN-B4-verification.md),
  [B5](docs/DESIGN-B5-cgo.md),
  [I1](docs/DESIGN-I1-engineswap.md),
  [I2](docs/DESIGN-I2-realmode.md).
  The A0 sub-designs (clock, engine, event loop, transport, plan, oracles, toy and simctl) are in
  [docs/](docs/) alongside them.

The simulator's idealizations, which is to say what it deliberately does not model, are in
[DESIGN-A0 section 7](docs/DESIGN-A0-simulator.md) and summarised in
[docs/V1.md](docs/V1.md) section 8.

## License

MIT, see [LICENSE](LICENSE).
