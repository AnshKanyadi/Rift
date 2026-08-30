# I2 — real mode, chaos, and the final numbers

*Written for somebody who has not read I2-1 through I2-5. Every number comes from a run recorded at
the time it was taken. Where a number is a criterion rather than a measurement, it says so.*

---

## 1. What was built

Real mode: the same `raft/`, `store/` and `kv/` that eight phases of deterministic simulation
verified, running as **three operating-system processes talking over TCP sockets**, killed with
`SIGKILL`, restarting against whatever the kill left on disk.

| piece | what it is |
|---|---|
| `net/` | a length-prefixed frame codec, in the determinism pass's core scope, pure |
| `net/tcp/` | one transport, per-peer queues that **drop rather than block** — `sim.Transport` returns no error, deliberately, and real mode must not invent one |
| `net/clientwire.go` | the client protocol: the only wire an *operation* crosses |
| `cmd/riftnode` | one node, one process: transport + store + `node.Driver` + the C++ engine |
| `chaos/` | process supervision, the fault schedule, the client that builds the history, the gate, the report |
| `bench/` | a log-linear histogram and a closed-loop load driver |

**Transport: length-prefixed TCP, not gRPC** (ruled). gRPC's natural shape returns an error per RPC,
and discarding it at the seam is either correct-and-subtle or a hole.

**One process per node** (ruled). Separate processes are the only way `kill -9` means what it means.

---

## 2. What was measured

Three `riftnode` processes on `engine/riftcgo` — the C++ LSM through the cgo batch interface — with
one client process, closed-loop, YCSB-A shape, 512 keys, 8 workers. `E = 500ms` (Election 10 ticks ×
50ms), `K = 10s` from CLAUDE.md's headline.

**Thresholds were declared in `DESIGN-I2` §3.2 before any number was taken**, derived from `E` and `K`
rather than picked. Two of the four originals were wrong and were amended *before* measurement, with
the originals struck through.

| threshold | verdict | result |
|---|---|---|
| **T1 recovery** `R ≤ 2.5E = 1.25s` | **NOT MET** | worst **R = 1.75s** over 2 kills, 0 that never recovered |
| **T2 chaos throughput** `≥ (K−2.5E)/K = 87.5%` | **MET** | **88.8%** (86 of 97 ops/s) |
| **T3 chaos latency** `p99 ≤ 3E`, `p999 ≤ 5E` | **MET** | p99 **122 ms**; p999 2.005 s |
| **T4 boundary cost** `≤ +5pp vs B5` | **NOT MEASURED** | no native C++ harness result to compare against |

Steady state, three runs, as a range rather than a best: **97–119 ops/s, p50 66.9–81.8 ms, p99
101–123 ms.** `engine/model` under the identical harness is ~940 ops/s, p50 8 ms.

> **Two of four not met, with nothing adjusted, is a stronger result than four of four met.** A
> threshold that fires is a threshold that was capable of firing.

Full methodology and provenance: `BENCHMARKS.md` §I2.

---

## 3. What was found

**Twelve defects. Eleven were in the harness. One was in the system, and it is the headline.**

### BUG-060 — the cluster could not re-elect a leader

After any leader kill, all three nodes sat at `follower`, `term=2`, **forever**. Ticks advanced, bytes
flowed, requests were admitted, nothing was ever served again.

`stepPreVote` refuses a round while `r.leader != 0 && r.electionElapsed < r.randomizedElectionTimeout`
— *"I heard from a leader inside one election timeout"*. **`preCampaign` reset `electionElapsed`
without clearing `r.leader`,** so after the reset the pair read as *"I heard from the leader recently"*
when what had happened was *"I campaigned recently"* — and `r.leader` still named the node that had
just died. Three followers each doing that refused each other indefinitely.

**Fix: one line.** Before it, chaos throughput was 37 ops/s with drift 0.00. After, 86 ops/s, drift
0.86, and two thresholds moved from NOT MET to MET.

**It survived eight phases of simulation because nothing in this project gates liveness.** The safety
oracles ask whether a *wrong* thing happened; a cluster that elects nobody does no wrong thing.
Porcupine is green over a history of timeouts, because a timeout is *may-or-may-not-have-happened*.

> **The defect was found by a threshold declared in advance, not by any checker** — the second time
> this session, and the first time such a threshold found a defect in the **system** rather than in
> the harness. That is §3.2's discipline, now evidenced rather than asserted.

**The diagnosis, as method** — three steps, each eliminating a layer, the third decisive: arithmetic on
numbers already printed (`ok=1172` ≈ 10s × 119 ops/s → "stopped at the first kill"); per-node counters
(`ldr=0` for 14s while ticks advanced → not engine recovery, not the driver, not transport); raft state
per node (all `follower` at `term=2` → decisive).

### The harness defects, and what each cost

| | |
|---|---|
| BUG-049 | the reality counters were green on the harness's **own** synthetic traffic; Raft had never ticked |
| BUG-050 | transport keyed by node id where `store/` addresses by **ordinal** |
| BUG-051 | every Raft message discarded at a **silent type assertion** |
| BUG-052 | events posted with no `At` — a return landed three seconds before its call |
| BUG-053 | a kill expectation stored per *node* where the reaper's question is per *process* |
| BUG-054 | restart raced the kernel releasing the socket |
| BUG-055 | **the benchmark measured the oracle** — 875 bytes retained per operation, measured |
| BUG-056 | a restart on `engine/model` is not a crash: **total amnesia**, which broke Raft's persistence assumption |
| BUG-057 | *"it does not link on this machine"* was an unread error. **It links.** |
| BUG-058 | the leader-kill counter counted **intent**, not delivery |
| BUG-059 | a restarted node never ran the recovery path — and its cross-check assumed an in-process predecessor |

**Three greens were retracted** — 2731, 2461 and 23,010 operations, each reported as *"linearizability
green, 3 leader kills of 3"*. Every restart in them was an amnesiac node (BUG-056), and the kill
figures came from a counter now known to over-report (BUG-058), so **how many leaders those runs
killed is not known.**

---

## 4. What I2 does not claim

### The chaos green is weaker than any green Track A ever reported

**There is no seed.** The schedule was the operating system's and the timing was the network's, and
neither is recoverable — so a second run is a *different experiment*, not a repetition. It can neither
confirm a result nor refute it. Every chaos green in this repository prints that sentence in the same
bytes as the result.

### T1 is unexplained

Worst `R = 1.75s` against a threshold of 1.25s, with nothing failing to recover. **Measured,
unexplained, reported at its value.** The threshold was not adjusted and the excess was not chased.

### Two named obligations, with the configuration each requires

| obligation | needs |
|---|---|
| **T4 — cgo boundary cost under chaos** | a native C++ harness result for the same workload at the same block size, compared against the cgo path. **Unmeasured, not passed** |
| **Clock skew in real mode** | a real-mode clock with injectable per-node offset and jump schedule, plus the uncertainty-envelope oracle reading a real-mode ledger |

The second is the larger one. **A6's headline — snapshot isolation over hybrid logical clocks — rests
on clock machinery that three separate mechanisms were supposed to exercise, and none did:** A6's
sweep ran with skew off; `hlc` was never analysed until BUG-048; real mode cannot produce skew at all.
Each was defensible alone. *A claim covered by three mechanisms is not three times as safe if nothing
checks that any of them fired.* This is a **coverage** statement, not a correctness one:
`hlc`'s implementation has been analysed since BUG-048 and A6's oracles are correct and were induced
individually.

### Nothing in this tree gates liveness

The safety oracles cover anything that makes a **wrong** thing happen. **Nothing covers anything that
makes the right thing stop happening.** BUG-060 is the demonstration: a cluster serving zero operations
for fourteen seconds, with every oracle green and correct to be green.

The shape of the answer is a liveness floor derived from `Election` and `Heartbeat` the way `R ≤ 2.5E`
was, then measured against real schedules before it could gate. `LedTicks == 0` is the tree's only
liveness assertion, it is binary, and it would not have caught BUG-060 either — a cluster with two live
nodes still elects a leader. **Named obligation, with that derivation sketch.**

### The real-mode path is newly under the regime, not long under it

`chaos/`, `bench/`, `net/` and `cmd/riftnode` had **zero mutants and appeared in no CI lane** until
this phase closed. Seven `chaos/` tests guarded on `testing.Short()` and therefore never ran in the
push lane at all. `make chaos-smoke` now runs them; a calibration fixture (`riftnode --fault=...`)
breaks the cluster's guarantees on purpose so every mechanism has a positive control; seven mutants
score against it, all killed.

**That is one phase of coverage, not eight.**

---

## 5. Status

| | |
|---|---|
| **lanes** | `build`, `lint`, `vet`, `chaos-smoke`, `raft`, `store`, `node`, `net`, `bench` green |
| **defects** | 60 in `BUGS.md`; 12 from I2, 11 of them in the harness |
| **escape hatches** | **5**, unchanged since A5 |
| **session reports** | `REPORTS/I2-1.md` … `I2-5.md`, with the retractions |
| **numbers** | `BENCHMARKS.md` §I2 |
| **limits** | `docs/TRACK-A.md` §8.3 |

**Open, carried:** T1's excess; OPEN-I2-1's one `signal: killed`; the rebalance driver reading
`ledger.Ranges()`; `./sim/hunt/` exceeding a 3000s test timeout at 40 seeds (lane cost, not a failure);
`BUG-015` STALE; the differential generator's zero-length-value hole; cross-toolchain determinism
unmeasured.
