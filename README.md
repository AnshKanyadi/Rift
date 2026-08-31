# Rift

A distributed, transactional key-value database built from scratch. The distributed layer is Go —
multi-group Raft, range splitting, MVCC over hybrid logical clocks, Percolator-style transactions,
and linearizable reads. Underneath it is an LSM storage engine written in C++ and reached through a
batched cgo interface.

The whole system runs inside a deterministic simulator, so any crash, network partition, or clock
jump replays exactly from a single seed number.

## Why this is built the way it is

Distributed systems fail in ways that are hard to reproduce. A bug that needs a leader to crash
mid-write while a partition heals might show up once in ten thousand runs and never again.

So the simulator came first, before the database. Everything that runs during a simulated execution
is deterministic by construction: no wall clocks, no global randomness, no goroutines, no direct
I/O. A vet pass turns a violation of any of those into a build failure. Faults are scheduled from a
seeded PRNG, so the same seed produces a byte-identical execution every time, and a failure found on
one machine reproduces on another from one number.

That property is what makes the rest possible. Every bug ever found here has a stored plan that
still reproduces it today, and the test suite replays all of them on every change.

## What was found

25,000 fault-injected runs across the transaction and read-index layers, with no safety violations.
67 defects found and fixed, each with a stored reproduction. 233 deliberately broken versions of the
code, each paired with a test that has to catch it, so the checkers themselves are checked.

The result worth reading is
[BUG-060](BUGS.md): a Raft liveness bug that survived eight development phases and 25,000 simulated
runs. After a leader was killed the cluster served nothing for fourteen seconds — and every safety
checker stayed green, correctly, because a cluster that elects no leader has not violated anything.
It was caught by a performance threshold written down before the measurement was taken, not by any
correctness check.

## Numbers

| | |
|---|---|
| Simulated runs, transaction and read-index layers | 25,000 each, zero safety violations |
| Defects found, all reproducible | 67 |
| Mutation tests | 78 Go, 155 C++ |
| Throughput on real hardware, three processes over TCP | 97–119 ops/s, p50 67–82 ms |
| Under continuous leader kills | 86 ops/s, 89% of steady state, p99 122 ms |
| Write amplification at 128 MiB | 8.08 against a 10× budget set in advance |

The performance targets were written down before anything was measured. Two were met, one was
missed, one was never measured, and none of them was adjusted afterward.
[BENCHMARKS.md](BENCHMARKS.md) has the methodology and the misses.

## Architecture

`raft/` is a pure state machine — messages and ticks go in, a `Ready` struct comes out. No
goroutines, no clock, no I/O. That purity is what lets the whole distributed layer run
single-threaded against a virtual clock inside the simulator.

| path | what it is |
|---|---|
| `raft/` | the consensus state machine |
| `store/` | multi-raft node: many groups over one transport, persistence, splits |
| `kv/` | MVCC and Percolator transactions |
| `router/` | client library, range cache, transaction coordinator |
| `clock/`, `hlc/` | hybrid logical clock with a bounded offset |
| `engine/` | storage interface; `engine/model` is the Go reference implementation |
| `engine-cpp/` | the C++ LSM engine: skiplist memtable, WAL, SSTables, compaction |
| `sim/` | event loop, fault injection, checkers |
| `raftcheck/` | safety checkers, which read observed events and never ask a node anything |
| `net/`, `node/`, `cmd/riftnode` | real mode: TCP transport and the node binary |
| `chaos/`, `bench/` | chaos runner and load generators |
| `cmd/simctl/` | run, replay, and search for failing seeds |
| `seeds/` | one stored reproduction per historical bug |

## Running it

Go is pinned by `go.mod`. The C++ side needs CMake and vendors GoogleTest at a fixed commit rather
than downloading it, so a clean checkout builds offline.

```sh
make build          # compile everything
make test           # unit tests
make lint           # vet, formatting, and the determinism pass
make smoke          # 500-seed simulation run
make corpus         # replay every stored reproduction
make chaos-smoke    # real processes, real sockets, leader kills
make ci             # everything above
```

`make help` lists every target. To replay a specific historical bug:

```sh
simctl replay --bundle seeds/BUG-022
```

## A note on the CI badge

Some checks fail, on purpose, and each one is explained in
[docs/V1.md](docs/V1.md) section 9.

One stored reproduction is stale after a bug fix changed the schedule it recorded; re-recording it
would pin a run that no longer demonstrates anything, so it stays red until a replacement schedule
is found. Two lanes need more compute than a hosted runner allows and run locally instead. One
refuses to run at all unless it can disable network access, which a hosted runner cannot grant. And
one detects a real property of the shipped configuration that a faster machine stops hiding.

None of them is a defect in the database, and none was silenced to make the board green.

## Scope

Included: Raft with elections, replication, persistence, snapshots, pre-vote, leadership transfer,
and membership changes; multi-raft with automatic range splits; MVCC with hybrid logical clocks;
snapshot-isolated distributed transactions; linearizable reads; the C++ storage engine.

Deliberately excluded, with reasoning in [STRETCH.md](STRETCH.md): joint consensus, parallel
commits, leader leases, and automatic load balancing. None of them is claimed anywhere.

## Documents

- **[docs/V1.md](docs/V1.md)** — the full writeup: what exists, how it was verified, what the
  verification cannot see, and what is still open. Start here.
- [BUGS.md](BUGS.md) — every bug, with its symptom, reproduction, root cause, and the check that
  caught it.
- [BENCHMARKS.md](BENCHMARKS.md) — methodology first, numbers second.
- [docs/](docs/) — one design document per component, each recording the alternatives that were
  rejected and why.

## License

MIT — see [LICENSE](LICENSE).
