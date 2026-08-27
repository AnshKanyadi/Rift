# BENCHMARKS.md

Methodology first, numbers second. A number without its methodology is not a result, and a number
that does not reproduce from a clean clone by one script does not go in this file.

> **Status: one measurement, B3.7b's amplification, recorded below. Everything else is a placeholder
> describing what will be measured and how, and is not claimed.**
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

## Planned measurements

| id | what | why it exists |
|---|---|---|
| `B-engine-fill` | fillrandom / fillseq, both engines | baseline write path |
| `B-engine-read` | readrandom / readseq / readmissing, both engines | read path and bloom-filter effectiveness |
| `B-engine-mixed` | YCSB-style A/B/C/D/F mixes, both engines | realistic mixes |
| `B-cgo-boundary` | identical workload, native C++ harness vs. through cgo | quantifies the boundary cost the batch API exists to amortize |
| `B-compaction` | space amplification and read amplification under sustained write load | the cost side of the LSM tradeoff |
| `B-txn-latency` | Percolator 2PC vs. parallel commit, commit latency distribution | the latency win is the entire justification for A9 |
| `B-chaos` | sustained throughput and p99 while killing the leader every 10s, including mid-compaction | the headline resilience claim |
| `B-recovery` | time to restored steady-state throughput after a leader kill | recovery, not just survival |

## Results

*(none yet)*
