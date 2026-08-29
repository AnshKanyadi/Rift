# DESIGN-I2 — real mode, chaos, and the final numbers

**Status: four questions RULED by Ansh 2026-08-29; §3.2's thresholds sent for ratification.** Nothing
is implemented. I2 is v1's last phase, and the one where the project stops verifying itself and starts
making claims to other people.

| question | ruling |
|---|---|
| transport | **length-prefixed TCP** |
| processes | **one process per node** |
| chaos lane | **gate on counters, report the verdict** |
| §3.2 thresholds | **sent below with derivations, for ratification or amendment** |

> **I2'S CENTRAL SENTENCE: DETERMINISM AND BYTE-IDENTICAL TRACES ARE SUSPENDED; EVERY SAFETY INVARIANT
> SURVIVES, BECAUSE THEY ARE PROPERTIES OF A HISTORY RATHER THAN OF A SCHEDULE.** That is what makes
> the chaos lane meaningful at all: a real cluster cannot give us a schedule, but it can give us a
> history, and a history is what every checker in this repository reads.

**AND HALF OF REAL MODE ALREADY EXISTS, which concentrates the risk.** `node/` has driven a `sim.Node`
since A0 — one goroutine per node, wall time, mailbox, no build tag and no second implementation of
node logic. Time, concurrency and storage are inherited from phases that already signed them.

> **I2'S RISK IS CONCENTRATED IN TWO THINGS — PROCESSES AND A NETWORK — RATHER THAN SPREAD ACROSS THE
> PHASE.** Everything else has been exercised for six phases.

### The rulings, with their reasons

**Transport: length-prefixed TCP, and this NARROWS a pre-approval rather than refusing one.** CLAUDE.md
pre-approves gRPC for real-mode transport. It is declined here because `sim.Transport.Send(Envelope)`
returns **no error, deliberately, since A0.7** — *"an error signal is a covert failure detector, and
covert failure detectors are how consensus implementations accidentally become unsafe"*. gRPC's natural
shape is a unary call that returns something, so adapting it means discarding that error at the seam.

> **THAT SEAM IS EITHER CORRECT-AND-SUBTLE OR IT IS A HOLE, AND THIS PROJECT'S POSITION IS THAT
> CORRECT-AND-SUBTLE IS A THING YOU LATER DISCOVER WAS NEITHER.**

**The cost, stated honestly:** hand-rolled framing is code we own and must verify, against a library
that is already verified and shaped wrong. **We are choosing the code we can check over the code that
is already checked**, and the framing therefore needs its own tests — length prefix, partial reads,
oversized frames, a peer that closes mid-frame — rather than inheriting confidence from a dependency.

**Processes: one per node.** Two configurations is the right price for `kill -9` meaning what it means.
Goroutines-as-nodes cannot express a process dying with unsynced writes in flight — **which is exactly
the class `GF-49` was raised about at I1**, where a substitute that could not express a class made it
invisible rather than absent. If the goroutine configuration is kept for speed it is kept as a
**fixture with its limit stated**, and never as evidence about crash behaviour.

**Chaos: gate on counters, report the verdict.** A non-reproducible red is a bad gate; an ungated lane
rots. So the lane gates on the deterministic properties of the run having *happened* — nodes started,
faults injected, operations completed, histories non-vacuous — and reports checker verdicts as findings
needing judgement.

**And a chaos violation's disposition is a documented workflow, not a decision made in the moment:**

1. the history and the fault log are **captured** and attached to a `BUGS.md` entry;
2. the first question is **whether it reproduces in sim**;
3. if it does not, **that is a finding about the simulator's fault model** and goes in the record as
   one, rather than being dropped.

> **A CHAOS RED IS NEVER CLOSED BY RE-RUNNING UNTIL IT GOES AWAY.**

### What a chaos GREEN means, and what it does not

**Stated before the first run, because the usual recourse does not exist here.** Every green in this
project so far has been backed by a seed: a run that passed can be re-run, and a run that failed can be
handed to anyone with one command. **The first chaos run is the first time this system has run with no
deterministic replay behind it.**

> **A CHAOS GREEN IS A STATEMENT THAT NOTHING WAS OBSERVED. IT IS NOT A STATEMENT THAT NOTHING IS
> THERE.**

That distinction exists everywhere in verification and is usually softened by the ability to look
again. Here there is nothing to look again *at*: the schedule was the operating system's, the timing
was the network's, and neither is recoverable. A second run is a **different experiment**, not a
repetition — so it cannot confirm the first and cannot refute it.

**What a chaos green therefore supports, and what it does not:**

| | |
|---|---|
| **supports** | "under this fault schedule, on this hardware, for this duration, no invariant was observed to fail, and the counters show the faults landed" |
| **does not support** | "the system is correct under chaos" — a claim about all schedules, from evidence about one |
| **does not support** | "this result reproduces" — nothing about it reproduces, by construction |

**And it is worth less than a sim green, not more**, which is the opposite of how a real-cluster result
usually reads. A sim green covers a *named, enumerated, re-runnable* set of schedules. A chaos green
covers one unrepeatable sample of an unenumerated space. **The chaos lane's value is entirely in its
reds**: it reaches schedules the simulator's fault model does not generate, and a violation there is
the strongest evidence this project can produce precisely because nothing about the schedule was
chosen by us.

> **THE COUNTERS ARE WHAT SEPARATE A GREEN FROM A NON-EVENT**, which is why they gate and the verdict
> does not. A chaos run that killed a leader every ten seconds and recorded no elections is a broken
> harness, and it is indistinguishable from a clean run by every other signal.

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

### 3.2 What result would mean what — **thresholds with derivations, sent for ratification**

**Deriving these rather than picking them changed two of the four**, which is the argument for the
exercise. The originals are shown struck through so the amendment is visible rather than silent.

#### The safety GATE, which is not a threshold at all

> **ANY SAFETY VIOLATION UNDER CHAOS IS INADEQUATE AT ANY NUMBER. THE BENCHMARK SECTION DOES NOT RUN.**

Written as a gate and placed above the table on purpose. In a table of numbers it reads as tunable, and
it is not: a performance number taken from a run that violated an invariant is a number about a broken
system, and there is no throughput that redeems it.

#### The parameters everything else is derived from

| symbol | what | value |
|---|---|---|
| `E` | election timeout, real time | **configured by I2**, not fixed today — `Election: 10` ticks, and the tick's real duration is I2's to choose |
| `K` | chaos kill interval | **10 s**, from CLAUDE.md's headline claim |
| `R` | recovery: kill → restored steady-state throughput | **derived below** |

**Recovery, derived.** A follower detects a dead leader after at most one election timeout; an election
costs one round trip; the winner then catches up and resumes serving:

```
R  =  detection (≤ E)  +  election (1 RTT, « E on a LAN)  +  catch-up
R  ≈  1.5E  to  2.5E
```

> **THRESHOLD 1 — RECOVERY.** `R ≤ 2.5E`. Above that, the cluster is taking longer than its own timing
> parameters predict and the excess is unexplained. **Reported as inadequate if `R ≥ K`**, and
> specifically as *the cluster never reaching steady state between kills* — which would otherwise be
> read as low throughput when it is really permanent recovery.

**Chaos throughput, derived rather than picked.** The cluster is unavailable for `R` out of every `K`:

```
expected ratio  =  (K − R) / K
with E = 1s, R ≈ 1.5–2.5s, K = 10s  ->  75% to 85%
```

> **THRESHOLD 2 — CHAOS THROUGHPUT.** `≥ (K − 2.5E)/K` of steady state, computed from the configured
> `E` and `K` rather than asserted. At `E = 1s` that is **≥ 75%**.
>
> ~~≥ 50% of steady state~~ — **AMENDED.** 50% was picked, not derived. At `E = 1s` it would pass a
> system recovering in **5 seconds**, which is 2–3× worse than its own parameters predict, while
> reporting success. **A threshold looser than the design's own prediction cannot fail anything the
> design would call broken.**
>
> **Reported as inadequate below 10%** — the system is technically live and practically down.

**Chaos p99, and this is the one I had most wrong.** An operation in flight during a leader change
waits out the recovery. The fraction of operations affected is `R/K` ≈ 15–25%, which is far above the
1% that p99 asks about — so **p99 under chaos is dominated by `R`, not by steady-state latency**:

```
p99(chaos)  ≈  R  ≈  1.5–2.5E
p99(steady) ≈  single-digit ms
ratio       ≈  300× to 500×
```

> **THRESHOLD 3 — CHAOS LATENCY.** `p99 ≤ 3E` and `p999 ≤ 5E`, stated **against the election timeout**,
> because that is what determines them.
>
> ~~p99 within 10× of steady-state p99~~ — **AMENDED, and it was wrong by roughly a factor of 30–50.**
> It compares two quantities that measure different things: steady-state p99 measures the write path,
> chaos p99 measures how long a leader election takes. **A ratio between them is not a number about
> anything**, and 10× would have failed a perfectly healthy cluster on its first run.

**cgo boundary cost — already measured, so this is a regression bound, not a fresh threshold.** B5's
numbers, signed:

| pairs per crossing | boundary cost |
|---:|---:|
| 1 | +111% |
| 8 | +33% |
| 64 | +21% |
| 512 | +24% |

> **THRESHOLD 4 — BOUNDARY COST.** No **regression** beyond **+5 percentage points** against the B5
> figure *at the same block size*, and the block size must be stated with any number quoted.
>
> ~~> 25% overhead is a finding about the interface~~ — **AMENDED.** An absolute 25% is already exceeded
> by numbers B5 measured and Ansh signed (+33% at 8 pairs, +111% at 1). **A threshold that fails the
> currently-signed state is not a threshold, it is a bug in the threshold** — and it would have fired
> on I2's first run, against a result that was never in question.

#### What ratification means

These four plus the gate are what I2 will be measured against. **A number that comes in outside them is
reported at its value with the threshold quoted beside it** — never adjusted afterwards, which is the
whole reason for declaring first.

**Two of the four originals were wrong in the same direction**: both were picked from intuition rather
than derived from the system's own parameters, and both would have produced a *comfortable* first
result — one by passing a system 3× worse than predicted, one by failing a healthy one so obviously
that the threshold would have been "fixed" on the spot. **Declaring in advance is only worth anything
if the declaration is derived; a guess written down early is still a guess.**

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
