# Rift

A distributed, transactional, MVCC key-value database: from-scratch multi-group Raft in Go over a
from-scratch C++ LSM storage engine, verified by deterministic simulation and a continuous soak farm.

> **Status: A0 (harness and interfaces), in progress.** Nothing below is claimed as working yet.
> This file states only what is true today, and every number is a bracketed placeholder until it can
> be reproduced from a clean clone by script. See [SOAK.md](SOAK.md) and
> [BENCHMARKS.md](BENCHMARKS.md) for the ledgers those numbers must come from.

---

## How it is verified

This section is above the feature list on purpose. The interesting claim about this project is not
that it implements Raft; it is that the implementation is checked by something that can actually
catch it being wrong.

**Deterministic simulation.** The distributed layer runs on a single-threaded, event-driven
simulator with a virtual clock. There are no goroutines, channels, locks, wall-clock reads, or
network and filesystem calls in any package that executes during a run — a custom vet pass
(`tools/determinismcheck`) rejects them at build time, including `range` over a map and the go1.23
iterator forms that look nothing like one. Its escape hatch requires a written reason, refuses to
sanction concurrency at all, and every use is listed in [HATCHES.txt](HATCHES.txt), which a test
diffs against the tree. The pass is itself mutation-tested: `make blind` blinds one rule at a time
and requires the rule's own test to fail. Same seed, same trace, byte for byte.

**Faults are the default, not a special mode.** Message drop, delay, duplication, reordering,
symmetric and asymmetric partitions, crashes, restarts, GC-style pauses, loss of unsynced writes, and
per-node clock drift and jumps bounded by `maxOffset`. Every injector counts its fires, and a run in
which an enabled injector never fired **fails** — a chaos suite that did not do anything is a chaos
suite that proved nothing.

**Every failure replays.** A seed materializes a *plan*: a complete, human-readable, serializable
description of the run. Plans carry keyed-PRF parameters rather than sequential RNG state, so a plan
reproduces its run with no live randomness at all — enforced by a poisoned RNG that panics if any
sequential draw is taken during plan execution. Seeds reproduce at the commit that produced them;
plans reproduce at any commit. Both are stored in [`seeds/`](seeds/).

**The harness is calibrated against known bugs.** A green test suite proves the harness *runs*, not
that it *catches*. `sim/mutants/*.patch` holds deliberately broken implementations — acknowledge
before fsync, acknowledge before replicating, apply a retried request twice, iterate a map, read the
wall clock, serve a stale read, restart from non-durable state — each applied to a scratch worktree
and each with a budget in seeds. (Patches rather than committed source: two of them must not
compile, which is the point of them.) A mutant
that survives its budget means the harness is too weak and the phase is not done. CI records
**kill-time per mutant**, so harness sensitivity is a monitored number rather than a belief. Every
entry in [BUGS.md](BUGS.md) names the mutant class that would have caught it; when none exists, a new
mutant lands in the same commit as the fix.

**Checkers are never loosened.** Linearizability checking is bounded, so a check that hits its
timeout is reported as *inconclusive* — never as a pass — and inconclusive results get their own
column in [SOAK.md](SOAK.md). When that rate rises, the response is to make the problem smaller
(shorter history windows, harder per-key partitioning), never to make the oracle weaker.

**What the simulator does not model** is written down, not discovered later: computation is
instantaneous unless slowness is explicitly injected; the network is per-link i.i.d. latency with no
congestion model; deterministic replay is scoped to sim runs on `engine/model`; Byzantine faults are
out of scope. The full list is
[DESIGN-A0 §7](docs/DESIGN-A0-simulator.md#7-known-idealizations-these-go-in-readmes-verification-scope-section).

**Current verification totals:** see [SOAK.md](SOAK.md). No claim is made here that the ledger does
not back.

---

## What it is

*(Each item lands with a design doc, an exit criterion signed off by the architect, and a test lane.
Unchecked means not built. Scope deliberately outside v1 — joint consensus, parallel commits, leader
leases, automatic balancing — is in [STRETCH.md](STRETCH.md) with the reasoning preserved, and is
never claimed here.)*

- [ ] From-scratch Raft: elections, log replication, persistence, snapshots, pre-vote, leadership
      transfer, single-node membership changes with learner catch-up
- [ ] Multi-raft: one Raft group per range, dynamic size-threshold splitting, manual replica movement
- [ ] Distributed transactions over MVCC: Percolator-style 2PC, snapshot isolation, hybrid logical
      clocks with uncertainty intervals
- [ ] Linearizable reads via read index
- [ ] From-scratch C++ LSM engine: skiplist memtable, WAL, SSTables with bloom filters and block
      index, leveled compaction, MANIFEST, behind a batch-oriented cgo interface

## Layout

| path | what |
|---|---|
| `raft/` | pure consensus state machine — `Step`/`Tick` in, `Ready` out. No I/O, no clock, no goroutines. |
| `store/` | multi-raft node: many groups over one transport, persist/apply loops, splits |
| `kv/` | MVCC and transactions |
| `router/` | client library, range cache, transaction coordinator |
| `clock/` | hybrid logical clock with `maxOffset`; sim and real implementations |
| `balancer/` | load-based rebalancing |
| `engine/` | storage interface both engines implement; `engine/model` is the deterministic Go reference |
| `engine-cpp/` | the C++ LSM engine plus cgo bindings (Track B) |
| `sim/` | event-loop simulator, fault injectors, checkers, toy protocol and mutants |
| `cmd/simctl/` | `run` / `replay` / `hunt` / `minimize` |
| `internal/rng/` | project-owned PCG64 with pinned test vectors and named sub-streams |
| `internal/sorted/` | the only map iteration in the repo, so key order is never an accident |
| `tools/determinismcheck/` | the vet pass that makes the determinism rules build failures |
| `bench/`, `chaos/`, `soak/` | load generators, real-mode chaos, the soak-farm runner |

## Building

```sh
make help      # every lane, and which are real vs. still stubs
make hooks     # install the pre-push hook that runs the every-change lanes
make test      # Go unit tests (seed searches bounded; full coverage is soak and the exit run)
make race      # Go unit tests under -race
make lint      # vet, formatting, the determinism pass, tooling-only dependencies
make blind     # mutation-test the determinism pass itself
make smoke     # 500-seed simulator smoke
make ci        # everything the push lane runs
```

Three tiers, and the boundary between them is cost rather than importance:

| tier | lanes | how it runs |
|---|---|---|
| **every change** | `make ci` — build, lint, lane coverage, assertions, provenance, test, corpus, corpus reproduction, blind, power, smoke, mutants, race | the pre-push hook runs the fast half; the rest wait on a remote |
| **nightly** | the 10,000-seed soak, the differential engine lane, the crash-consistency sweep | not yet scheduled — there is no remote |
| **solo** | `make solo` and `make exit-run` — the unthrottled collector, mutant power floors, the race curve, and the 25,000-seed exit run | hours each, and none of them may share a machine |

`make lane-coverage` requires every lane named in the `ci` target to actually
appear in `.github/workflows/ci.yml`. Two of them did not, for a phase — a lane
that is not run is not a lane.

Go is pinned by `go.mod`'s `toolchain` directive and CI runs exactly that version.

## Documents

- [CLAUDE.md](CLAUDE.md) — the project constitution and its amendment log
- [docs/](docs/) — one design doc per phase: candidates, tradeoffs, the decision, and the rejected
  alternatives with reasons
- [BUGS.md](BUGS.md) — every bug found, with its seed or kill point, root cause, and the invariant
  and mutant class that caught it
- [SOAK.md](SOAK.md) — the cumulative verification ledger
- [STRETCH.md](STRETCH.md) — what is deliberately outside v1, and why. Never claimed
- [HATCHES.txt](HATCHES.txt) — every determinism exemption in the repo, with its reason
- [BENCHMARKS.md](BENCHMARKS.md) — methodology first, numbers second, both engines

## License

MIT — see [LICENSE](LICENSE).
