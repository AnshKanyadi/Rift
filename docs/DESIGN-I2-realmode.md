# DESIGN-I2 — real mode, chaos, and the final numbers

**Status: proposed. Waiting on Ansh.** Nothing here is implemented. I2 is v1's last phase, and it is
the one where the project stops verifying itself and starts making claims to other people.

---

## 1. What "real mode" means, precisely

**Half of it already exists and it is important to say which half.** `node/` is the real-mode driver
and has been since A0's close: one goroutine per node, wall-clock time, every cross-goroutine
interaction entering through a mailbox, and — the load-bearing part —

> **`Driver` drives a `sim.Node`: the same interface the simulator's loop drives, same
> `Handle(Event, Scheduler)` signature. No build tag, no `if realMode`, no second implementation of
> node logic.** The two modes differ only in *who calls Handle and when*.

That is what makes the corpus evidence about the shipping artifact rather than about a simulator-only
program. **I2 must not break it**, and every option below is judged against that first.

**What I2 adds, and each is a separate decision:**

| | sim today | `node/` today | I2 |
|---|---|---|---|
| **time** | virtual, from the event queue | wall clock | wall clock |
| **concurrency** | one goroutine, whole cluster | one goroutine per node | one goroutine per node |
| **processes** | one | **one** | **one per node — this is new** |
| **transport** | in-memory `Send(Envelope)` with plan-driven dice | in-memory, bounded queue | **a network — this is new** |
| **storage** | `engine/model`, or `simcgo` since I1 | `engine/model` | **`riftcgo` on a real filesystem** |
| **faults** | injected by the plan, from a seed | none | **inflicted from outside: signals, network, disk** |

**The two genuinely new things are processes and the network.** Everything else I2 inherits from a
phase that already signed it.

### 1.1 Which guarantees survive, and which are suspended

**This table is the phase's honesty surface and it belongs before any code.**

| guarantee | in I2 | why |
|---|---|---|
| **Deterministic replay from a seed** | **SUSPENDED** | wall time, OS scheduling, real network. There is no seed and no replay. `DESIGN-A0` §7 already scopes replay to sim runs on `engine/model`; I2 does not narrow it further, it simply is not in scope |
| **Byte-identical traces** | **SUSPENDED** | same reason |
| **Every safety invariant** — election safety, log matching, leader completeness, state-machine safety, committed-is-forever, linearizability per key, transaction atomicity, SI, epoch monotonicity, dedupe | **SURVIVE** | they are properties of a *history*, not of a schedule. A real run produces a history; the same checkers read it |
| **Non-vacuity counters** | **SURVIVE, and matter more** | there is no seed to re-run, so "the run exercised nothing" is undetectable by any other means |
| **The mailbox rule** | **SURVIVES** | `node/` enforces it and `-race` is the backstop. A real network makes it load-bearing rather than theoretical |
| **`engine/model` as control** | **SURVIVES in a reduced form** | it cannot run beside a real cluster, but a divergence found in I2 can be *reproduced against it* in sim, and that is the first thing to try |

> **DETERMINISM IS NOT AMONG THE SURVIVORS, AND THAT IS THE WHOLE DIFFICULTY OF THIS PHASE.** Every
> instrument Track A built assumes a failing run can be re-run. In I2 a violation is a thing that
> happened once.

---

## 2. What the chaos lane asserts, and what a violation there is evidence of

**The hard question, and it deserves to be answered before the lane exists rather than after it goes
red at 3am.**

### 2.1 What it cannot be

It cannot be *"run the chaos script and see if it crashes"*. A crash is a symptom with no address.
And it cannot borrow the sim's contract, because that contract is *"this seed reproduces this
violation"* and there is no seed.

### 2.2 What it is

> **THE CHAOS LANE ASSERTS THE SAFETY INVARIANTS OVER A HISTORY THE CLUSTER ACTUALLY PRODUCED.** The
> checkers are the same ones. What changes is the *provenance of the history*: in sim it comes from a
> schedule the harness authored; in chaos it comes from a cluster the harness only disturbed.

Concretely, three things must be true for a chaos run to mean anything, and each is a mechanism:

1. **The history is collected with the same fidelity as in sim.** Client operations recorded with
   invocation and response instants from a single monotonic source, so porcupine's input is the same
   *kind* of object. A history assembled from per-node clocks would be a different problem wearing the
   same name.
2. **The faults are recorded as they are inflicted**, with timestamps, so a violation has a fault
   context even though it has no seed. *What was done to the cluster* is the closest thing to a seed
   that exists here.
3. **The non-vacuity counters are asserted**, harder than in sim: leader changes, snapshots installed,
   ranges split, transactions committed, bytes written. **A chaos run that killed a leader every ten
   seconds and produced no elections is a broken harness, and it looks exactly like a clean run.**

### 2.3 What a violation there is evidence of

| | |
|---|---|
| **What it IS evidence of** | a real defect, in the system, reachable in a real deployment. It is the strongest evidence this project can produce, precisely because nothing about the schedule was chosen by us |
| **What it is NOT** | reproducible. Not by a seed, not by re-running the script, possibly not ever |
| **What it obliges** | the history and the fault log are the artifact. They are kept, verbatim, attached to a `BUGS.md` entry, and **the first move is to reproduce the shape in sim** — a schedule that produces the same violation makes it a seed and returns it to the world where every other instrument works |

> **A CHAOS VIOLATION IS A FINDING WITHOUT A REPRODUCTION, AND THE WORK IT CREATES IS TURNING IT INTO
> ONE.** If it cannot be reproduced in sim, that is itself a finding — about the simulator's fault
> model, which was supposed to cover this.

**The inverse — chaos running clean for hours — is worth much less and must be reported as worth
less.** It is the same "absence as evidence" problem A7 recorded: a clean chaos run is consistent with
a correct system and with a chaos script that never landed a blow, and only the counters distinguish
them.

---

## 3. Benchmark methodology, declared before any number is taken

**In B3.7b's shape**, which fixed its threshold in advance and then reported the measurement against
it (`8.08 < 10`, with the direction of the model's error stated). **The point of declaring first is
that a threshold chosen after seeing the number is not a threshold.**

### 3.1 What is measured

| | |
|---|---|
| **workloads** | YCSB-style A (50/50 r/w), B (95/5), C (100% r), plus the bank workload from A6 as the transactional case |
| **metrics** | throughput (ops/s sustained), latency p50/p99/p999 from HDR histograms, and recovery time after a leader kill |
| **conditions** | a fixed node count, a fixed key space, a stated warmup discarded, a stated measurement window, on hardware named in `BENCHMARKS.md` |
| **engines** | the C++ engine **and** `engine/model`, plus the cgo boundary cost measured separately — same workload, C++ engine called from Go versus from a native C++ harness, as CLAUDE.md requires |

### 3.2 What result would mean what — **declared now**

| result | reading |
|---|---|
| chaos throughput ≥ **50%** of steady-state, p99 within **10×** of steady-state p99 | the system degrades gracefully under leader churn. **This is the claim I2 exists to support** |
| chaos throughput **10–50%** of steady state | it survives but does not serve. Reportable, with the number, as *"available, not performant, under continuous leader loss"* |
| chaos throughput **< 10%** of steady state | **reported as inadequate.** The system is technically live and practically down, and the honest headline is that it does not sustain load under this fault rate |
| **any** safety violation under chaos | the benchmark section does not run. A performance number taken from a run that violated an invariant is a number about a broken system |
| recovery time after a leader kill **> 10s** with a 10s kill interval | **reported as inadequate**, and specifically as *the cluster never reaching steady state between kills* — a number that would otherwise read as low throughput when it is really permanent recovery |
| cgo boundary overhead **> 25%** on the mixed workload | reported as a **finding about the interface**, not a footnote, since the batch design exists to prevent exactly that |

> **THE "INADEQUATE" ROWS ARE THE POINT OF DECLARING IN ADVANCE.** Every other row has an obvious way
> to be reported generously after the fact. Writing down now what would count as a bad result is the
> only version of this that costs anything.

**And BENCHMARKS.md keeps its provenance block** — commit, engine shape, caps, and what would
invalidate the numbers — which B3 already established and I2 inherits rather than invents.

---

## 4. The open questions

**Nothing below is decided, and each changes what gets built.**

1. **Transport: gRPC or length-prefixed TCP?** CLAUDE.md names this as I2's decision and pre-approves
   gRPC. The constraint that matters is `sim.Transport`'s: `Send(Envelope)` returns **no error**,
   deliberately — *"an error signal is a covert failure detector, and covert failure detectors are how
   consensus implementations accidentally become unsafe"*. Real mode already gets that from a bounded
   per-peer queue that drops on overflow. **gRPC's natural shape is the opposite**: every RPC returns
   an error. Using it means discarding that error at the seam, which is either correct-and-subtle or a
   hole, and I would rather Ansh rule than have me choose.
2. **Processes: one per node, or goroutines in one process?** One process is far cheaper and reuses
   `node/` exactly. Separate processes are the only way `kill -9` means what it means, and the chaos
   claim is about killing processes. I lean to separate processes for the chaos lane and one process
   for the benchmark lane, but that is two configurations and it needs saying out loud.
3. **Does the chaos lane gate CI, or report?** A non-reproducible red that cannot be bisected is a bad
   gate. A lane nobody gates on is a lane that rots. There is a third option — gate on the *counters*
   and report the *verdict* — and I do not know which Ansh wants.
4. **The thresholds in §3.2 are mine.** They are declared in advance as the method requires, but the
   numbers are a proposal, and a threshold Ansh has not agreed to is not a threshold either.

**Nothing is implemented.** No transport, no chaos runner, no benchmark harness, and no number taken.
