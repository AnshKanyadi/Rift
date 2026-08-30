# BENCHMARKS.md

Methodology first, numbers second. A number without its methodology is not a result, and a number
that does not reproduce from a clean clone by one script does not go in this file.

> **Status: two measurements recorded below — B3.7b's compaction amplification and B5.5's cgo boundary
> cost. Everything else is a placeholder describing what will be measured and how, and is not
> claimed.**
>
> The amplification numbers are **not** a performance claim and are not quoted as one. They exist to
> decide `B3-D3` — which compaction policy v1 ships — against a threshold fixed in `DESIGN-B3` §8.1
> **before any candidate ran.** They are in this file because they are numbers, and this file is where
> numbers live with their methodology.

## Rules

1. **Reproducible or absent.** Every table cell must be regenerable by `make bench-<name>` from a
   clean clone. If it cannot be, it does not appear.
2. **Hardware and configuration stated inline** with every table: CPU, core count, memory, storage
   device and filesystem, OS, Go toolchain, C++ compiler and flags, and whether the filesystem
   honors `fsync`.
3. **Methodology stated before results**: warmup duration, measurement duration, number of runs,
   which statistic is reported across runs (median of N, not best of N), key and value size
   distributions, key access distribution, and concurrency level.
4. **Latency reported as p50 / p99 / p999**, from HDR histograms, never as a mean alone. Throughput
   and latency come from the same run, not from separate favorable ones.
5. **Both engines, always.** `engine/model` and the C++ LSM engine appear in the same table, on the
   same workload. Comparisons across different workloads are not comparisons.
6. **The cgo boundary is measured explicitly**: the same workload driven by a native C++ harness and
   through the Go binding, so the boundary cost is a number rather than an assumption.
7. **Chaos numbers state the chaos.** Throughput-under-chaos means nothing without the kill cadence,
   what is killed, and whether kills land mid-compaction.

## B3.7b — compaction amplification, and the decision it was run to make

**REPRODUCE:** `make cpp-amp` from a clean clone. One binary, no arguments, deterministic.

**WHY IT EXISTS.** Amendment A6 says the simplest correct policy wins v1 and the measurement chooses
it. `DESIGN-B3` §3 recommended **(b) two levels** and named the number that would reopen **(c)
multi-level leveled**; §8.1 fixed that number, its derivation, and *what each possible outcome
means*, **before this program was written.** None of that is decided here.

### Methodology

| | |
|---|---|
| **workload** | fillrandom-shaped: distinct keys in a deterministic non-sorted order, 256-byte values, `Sync` every 64 writes |
| **why non-sorted** | keys in key order would let every flush produce a table disjoint from the last, so nothing would ever be rewritten and the number would be a fact about the workload |
| **caps** | **as shipped** — flush threshold 4 MiB, L0 compaction trigger 4. The crossing point is *derived from the caps*, so changing them moves the threshold with them |
| **sizes** | three, spanning the predicted crossing point: `crossing/8`, `crossing/2`, `crossing` |
| **space** | bytes on disk ÷ bytes of live data, **both counted by the harness** — asking the engine would be asking the thing under test |
| **write** | bytes appended ÷ bytes submitted, appended bytes taken from **the harness's own Env ledger** |
| **read** | **tables consulted per point read, MEASURED** — the harness opens the tables and counts, per sampled key, those whose bloom filter says *maybe* and whose range admits it. Not derived from the level structure, because the filter's whole purpose is to make the real number smaller than `\|L0\| + 1` |
| **conditions reported** | `L0 left` — uncompacted L0 files when the workload stopped. A run ending with L0 partly full has not paid for their compaction, so its write number would read low |

### Provenance — what a future reader needs to tell whether these are current

**Recorded now because I2 will restructure this file, and a number whose provenance has to be
re-derived is a number nobody can trust or retire.**

| | |
|---|---|
| **taken at commit** | `19f1d45` (Track B, branch `rift-b`) |
| **engine shape** | two levels; L0 from flushes, L1 a non-overlapping run of files capped at the flush threshold; compaction trigger `\|L0\| >= 4`; range tombstones in-table; file lifetime by reference count |
| **caps** | **as shipped**: flush threshold 4 MiB, WAL buffer 256 MiB, max record 32 MiB |
| **crossing point** | derived from the caps as `8 · K · F` = 128 MiB — **not a constant**, so a caps change moves it |
| **what would invalidate these** | any change to the compaction policy, the trigger, the flush threshold, or the output-file cap. A change to the READ path invalidates the read column only |
| **regenerate** | `make cpp-amp` |

**THE SHAPE IS RECORDED BECAUSE THE NUMBERS ARE ABOUT IT.** Write amplification of 8.08 is a fact
about *this* policy at *these* caps; quoted without them it is not a fact about anything. When I2 sets
the reporting standard, this row is what tells a reader whether the table survived the change or must
be re-run.

### Results

**Hardware:** Apple M1, macOS (Darwin 25.3.0), APFS. Single process, single thread, `TestEnv`
in-memory Env — **so these are I/O-count ratios, not device numbers**, which is what amplification is.

| live data | on disk | space amp | **write amp** | read amp | L0 left |
|---:|---:|---:|---:|---:|---:|
| 16 MiB | 17.2 MiB | 1.08 | 3.04 | 1.00 | 1 |
| 64 MiB | 69.0 MiB | 1.08 | 5.40 | 1.00 | 0 |
| **128 MiB** | 138.0 MiB | **1.08** | **8.08** | **1.00** | **0** |

### The threshold, and the conclusion

**THE NUMBER:** write amplification **8.08** at 128 MiB of live data.

**THE THRESHOLD**, from §8.1 and fixed in advance: `(b)` crosses **10×** at `D > 8·K·F` = **128 MiB**,
from `WA ≈ 1 (WAL) + 1 (flush) + D/(K·F)`.

**THE CONCLUSION:** **8.08 < 10 at the size the threshold named, so `(b)` holds, and it wins on the
measurement rather than on A6's rule alone.** `(c)` is not reopened. The model **over-predicted** —
it predicted 10.00 where 8.08 was measured — which is the safe direction for a threshold to be wrong;
fitting the three points puts the real crossing nearer **170 MiB**.

**`L0 left` is 0 at the deciding size**, so that number is a steady-state value and not a snapshot
mid-cycle. The 16 MiB row ends with one L0 file outstanding and is therefore *slightly* under-charged;
it is not the deciding row and the caveat is recorded rather than left to be discovered.

**What the pre-declared outcome would have been, had the sizes landed below the crossing point:** the
question would have been *not decidable on evidence*, `(b)` would have won on A6's rule, and that
would have been **a result recorded as one** — not an *inconclusive*, and not an invitation to run it
bigger until it discriminated. §8.2b fixed that in advance precisely so it could not be reached by
accident. It was not reached; the measurement ran *at* the threshold and answered.

### What these numbers are not

- **Not a performance claim.** No throughput, no latency, no device I/O. A ratio of bytes written to
  bytes submitted says nothing about how fast either happened.
- **Not a comparison between engines.** `engine/model` has no compaction to measure.
- **Not measured beyond 128 MiB.** The curve is three points and the extrapolation to ~170 MiB is
  arithmetic on those three, stated as such.

---

## B5.5 — the cgo boundary, and the block interface that exists to amortise it

**REPRODUCE:** `make cpp-bench` from a clean clone.

**WHY IT EXISTS.** `CLAUDE.md` requires the boundary cost to be measured explicitly — *"same workload,
C++ engine called from Go versus from a native C++ harness"* — and requires the block-oriented
iterator to justify itself: *"per-call cgo overhead is real; the interface must amortise it."* Those
are two different questions and this table answers both. The first is a number. The second is a
**shape**, and the shape is the more useful half.

### Methodology

| | |
|---|---|
| **workloads** | `fillrandom` (n sets of random 16-byte keys, 100-byte values), `readrandom` (n random point reads over the filled set), `mixed` (50/50 read/write, same key stream), `scan` (one full forward iteration) |
| **n** | 50,000 operations per measured run |
| **key stream** | splitmix64, **written twice and pinned in both languages** (`engine-cpp/src/bench_keys.h`, `riftcgo`'s `TestTheKeyStreamMatchesTheNativeHarness`) |
| **swept** | write batch size ∈ {1, 8, 64, 512}; iterator block size ∈ {1, 8, 64, 512} |
| **statistic** | **median of 3**, never best-of |
| **timed region** | the operation loop only. Open, pre-fill and close are outside it |
| **pre-fill** | identical in all three columns, and **unsynced in all three** — `engine.Engine` has no `Sync`; this project drives durability, so a column that synced would be doing work outside its timed region that another column cannot do |
| **build** | **Release**, in its own build directory (`engine-cpp/build/bench`) |
| **concurrency** | single-threaded throughout. No poller runs |

**THE BUILD TYPE IS METHODOLOGY, NOT CONFIGURATION.** The first table taken here came from the shared
`Debug` directory — right for every other lane, assertions on and optimiser off — and reported ~4 µs
for a single memtable `Set` and a `readrandom` cost that did not move with batch size. Those numbers
describe `-O0` and nothing about this engine.

> **A BENCHMARK FROM A DEBUG BUILD IS NOT A SLOW NUMBER. IT IS NOT A NUMBER.**

It is a separate directory rather than a flag on the shared one, so that nothing else silently starts
running `Release`: the sweep's kill-point counts and every floor in `FLOORS.txt` are measured against
`Debug` builds, and a lane that quietly changed build type underneath them would move denominators
nobody was watching.

**AND MEDIAN-OF-3 IS ALSO METHODOLOGY.** The first table reported boundary costs of **−23%** and
**−16%** — the cgo column beating native, which cannot be true, since the cgo column does everything
the native column does and *then* crosses a boundary. Those were variance. A single run cannot tell
variance from a finding.

### Provenance

| | |
|---|---|
| **engine tree** | `62e213a` (Track B, branch `rift-b`) — B5.5 adds only the harness; no engine code changed to take these |
| **hardware** | Apple M3 Pro, 11 cores, 18 GB, APFS on internal NVMe, macOS 26.3.1 |
| **toolchains** | Go 1.26.5, Apple clang 17.0.0, `-O2` via CMake `Release` |
| **caps** | as shipped: flush 4 MiB, WAL buffer 256 MiB, max record 64 MiB, busy 192 MiB |
| **fsync** | APFS on this device honours `F_FULLFSYNC`; **no workload here syncs**, so it does not enter these numbers |
| **what would invalidate these** | any change to the C boundary's marshalling, the Go wrapper's buffering, or the engine's write/read path. A change to `DefaultBlock` changes only which row is the default |
| **regenerate** | `make cpp-bench` |

### Results

**Point workloads.** `ns/op` is the whole measured loop divided by n.

| workload | batch | model | C++ native | C++ via cgo | boundary |
|---|---:|---:|---:|---:|---:|
| fillrandom | 1 | 509,091 | 1,140 | 2,069 | +82% |
| fillrandom | 8 | 92,106 | 927 | 1,226 | +32% |
| fillrandom | 64 | 29,472 | 868 | 1,050 | +21% |
| fillrandom | 512 | 15,581 | 1,201 | 1,627 | +35% |
| readrandom | 1 | 494 | 852 | 1,179 | +39% |
| readrandom | 8 | 471 | 1,018 | 1,329 | +31% |
| readrandom | 64 | 479 | 843 | 1,131 | +34% |
| readrandom | 512 | 460 | 843 | 1,089 | +29% |
| mixed | 1 | 177,484 | 1,455 | 1,839 | +26% |
| mixed | 8 | 23,257 | 1,052 | 1,317 | +25% |
| mixed | 64 | 3,583 | 1,112 | 1,531 | +38% |
| mixed | 512 | 857 | 1,121 | 1,767 | +58% |

**Scans, swept over iterator block size — and this is the table that answers the design question.**

Each block size was measured four times (the `batch` sweep does not affect a scan: it changes only
the pre-fill, which is outside the timed region). Those four rows are **replicates**, and their spread
is this table's only variance estimate.

| pairs per boundary crossing | native ns/op | cgo ns/op | boundary cost, 4 replicates | median |
|---:|---:|---:|---|---:|
| **1** | 76–105 | 166–200 | 97%, 108%, 114%, 118% | **+111%** |
| **8** | 80–93 | 107–140 | 29%, 32%, 34%, 52% | **+33%** |
| **64** | 80–105 | 97–123 | 17%, 18%, 23%, 24% | **+21%** |
| **512** | 79–94 | 98–117 | 17%, 23%, 24%, 31% | **+24%** |

### What the numbers say

**1. The block interface does what it was built to do, and the size of the effect is the result.**
One pair per crossing costs **+111%** — the boundary roughly doubles the cost of iterating. Sixty-four
pairs per crossing costs **+21%**. That is ~90 percentage points removed by batching alone, and it is
the justification for the interface existing rather than a per-entry cursor across `extern "C"`.

**2. It saturates by 64, and 512 buys nothing.** The block=512 replicates (17–31%) sit inside the
block=64 replicates' neighbourhood, so the two are not distinguishable at this precision. The cost
curve is essentially flat past 64. `DefaultBlock = 64` is set there for that reason and not by taste.

**3. Batching does the same thing on the write side.** `fillrandom` goes from **+82%** at one
operation per `Apply` to **+21%** at 64. Same mechanism, same conclusion: the boundary's cost is
per-crossing, so the interface's job is to make crossings rarer.

**4. Point-operation boundary cost is roughly 20–40%** once anything is batched, and the table is not
precise enough to say more than that.

### Variance, stated rather than implied

The four scan replicates at block=8 span **29%–52%**. At block=64 they span **17%–24%**. So a single
cell here is good to roughly **±5 percentage points at best**, and worse at small block sizes.

> **NO CELL IN THIS TABLE SHOULD BE READ TO ONE DECIMAL, AND NO TWO CELLS WITHIN ~10 POINTS OF EACH
> OTHER SHOULD BE READ AS DIFFERENT.**

Two consequences are visible in the point table and are **not** explained away here: `fillrandom` and
`mixed` are **non-monotonic in batch size** — both are worse at 512 than at 64. That may be memtable
flushes landing inside the timed loop at larger batches, or it may be noise. **This table cannot tell
those apart, so it does not claim to.** Resolving it needs per-operation histograms, which is I2's
instrument and not this one.

### What these numbers are not

- **Not a throughput or latency claim.** Single-threaded, one process, no sync, no concurrency, no
  device pressure. `ns/op` here is a cost of crossing and computing, not a rate this database sustains.
- **Not p50/p99/p999.** This table reports a median of three whole-loop timings, not a latency
  distribution. Rule 4 of this file applies to the I2 tables and is not met here; nothing in this
  section is quoted as a latency result.
- **NOT A THREE-WAY ENGINE COMPARISON, and the model column especially is not one.** `engine/model` is
  an in-memory Go reference with immutable versions and a full entry-slice copy per apply. It is
  **orders of magnitude slower on writes** (that copy) and **an order of magnitude faster on scans**
  (a sorted slice, no blocks, no SSTables, no disk). Neither of those is a finding about either
  engine. The model column is here because `B5-D7` asks for it and because it is the reference every
  correctness verdict in this project is defined against — not because it is a competitor.
- **Not a claim about a poller.** `B1-Q11` ruled the poller **harness-side**: a production embedder
  supplies its own. No poller runs in any row here.
- **Not precise to one decimal.** See the variance note under the results.

---

## Planned measurements

| id | what | why it exists |
|---|---|---|
| `B-engine-fill` | fillrandom / fillseq, both engines | baseline write path |
| `B-engine-read` | readrandom / readseq / readmissing, both engines | read path and bloom-filter effectiveness |
| `B-engine-mixed` | YCSB-style A/B/C/D/F mixes, both engines | realistic mixes |
| ~~`B-cgo-boundary`~~ | **DONE at B5.5, above.** Identical workload, native C++ harness vs. through cgo | quantifies the boundary cost the batch API exists to amortize |
| `B-compaction` | space amplification and read amplification under sustained write load | the cost side of the LSM tradeoff |
| `B-txn-latency` | Percolator 2PC vs. parallel commit, commit latency distribution | the latency win is the entire justification for A9 |
| `B-chaos` | sustained throughput and p99 while killing the leader every 10s, including mid-compaction | the headline resilience claim |
| `B-recovery` | time to restored steady-state throughput after a leader kill | recovery, not just survival |

## I2 — the full stack on the C++ engine, first measurement

### Methodology

Three `riftnode` processes on one machine over loopback TCP, one client process,
closed-loop load. **Every number below is from the same binary and the same run.**

| | |
|---|---|
| **engine** | `engine/riftcgo` — the C++ LSM through the cgo batch interface |
| **workload** | YCSB-A shape (50/50 read/write), 512 keys, 8 concurrent workers |
| **loop** | **closed** — each worker issues one operation and waits. Offered load falls when the cluster slows, so this measures a system at fixed concurrency, not at a fixed arrival rate. **A closed loop cannot produce a queue**, so it cannot show a latency collapse an open loop would find |
| **warmup** | 2s, issued and discarded |
| **window** | 15s |
| **histogram** | project-owned log-linear, 128 sub-buckets, **measured** worst-case relative error < 0.79% against a keep-everything reference, and it errs HIGH |
| **ledger** | **OFF** (`--unobserved`). BUG-055: the oracle retains 875 bytes per operation, forever. Measured here to make no difference — see below — but the configuration is stated because which one produced a number is not something a reader should infer |
| **timing** | `Election` 10 ticks x 50ms tick = **E = 500ms**; chaos kill interval **K = 10s** |

### Provenance

- Commit: the one that adds this section. Hardware: Apple Silicon laptop, macOS 25.3.
- Reproduce: `go test ./chaos/ -run TestI2Numbers -count=1` with the C++ archive built
  (`make cpp-cgo` or `make chaos-smoke`).
- **What would invalidate these:** a different tick interval (every threshold is computed from `E`), a
  different worker count (closed loop), a different key count, or an engine built with sanitizers.

### Results — steady state

**Three runs, reported as a range rather than as a best.** Run-to-run variance on a shared laptop is
real and quoting one run would imply a precision this does not have.

| | |
|---|---|
| **throughput** | **97 – 119 ops/s** |
| **p50** | **66.9 – 81.8 ms** |
| **p99** | **101 – 123 ms** |
| **p999** | **115 – 173 ms** |
| drift across the window | 1.02 – 1.09 — flat in every run, so a mean describes each one |

**For scale, `engine/model` under the identical harness: ~940 ops/s, p50 8 ms.** That ratio is **the
real engine's cost, measured for the first time.** It is not a defect and not a regression against
anything — there is no prior number for this configuration. It is the price of real fsyncs and a real
LSM where the reference engine keeps versions in memory.

**`ledger=on` and `ledger=OFF` were indistinguishable** at the chaos rate (86 and 96 ops/s across the same pair of phases). BUG-055's
per-operation retention is real and was measured at 875 B/op; against a C++ engine doing real fsyncs it
is **not the bottleneck.**

### Results — under chaos (leader killed every K = 10s)

| | |
|---|---|
| **throughput** | **86 ops/s, 88.8% of steady state** — threshold `(K - 2.5E)/K = 87.5%`, **MET** |
| **p99** | **122 ms** — threshold `3E = 1.5s`, **MET** |
| **p999** | **2.005 s** — threshold `5E = 2.5s`, MET, **but the 2s operation timeout CENSORS the tail at its own value**, so the true p999 is unknown and at least this |
| **recovery** | **worst R = 1.75 s** over 2 measurable kills, 0 that never recovered — threshold `R ≤ 2.5E = 1.25s`, **NOT MET.** Reported at its value; the excess over what `E` predicts is unexplained |

**These are post-BUG-060 numbers.** Before that one-line fix the same configuration produced **37 ops/s
with drift 0.00** — throughput fell to zero after the first kill and stayed there, because no cluster
ever re-elected a leader. Every safety oracle was green over those runs.

### What these numbers are not

- **Not a tuned result.** No batching beyond what the store already does, no group commit, no
  compaction tuning. First measurement, not best.
- **Not a claim about a cluster of machines.** Three processes on one host share a page cache, a disk
  and a scheduler.
- **Not comparable to the B5.5 boundary numbers.** Those measure the cgo crossing in isolation; this
  measures the whole stack.
- **T4 (boundary cost under chaos) is UNMEASURED, not passed.** It needs a native C++ harness result
  for the same workload at the same block size. Carried as a named obligation.

## Results

*(none yet beyond the sections above)*
