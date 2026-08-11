# BENCHMARKS.md

Methodology first, numbers second. A number without its methodology is not a result, and a number
that does not reproduce from a clean clone by one script does not go in this file.

> **Status: no numbers yet.** A0 is in progress. Every figure below is a placeholder describing what
> will be measured and how. Nothing here is claimed.

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
