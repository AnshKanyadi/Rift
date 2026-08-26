#!/usr/bin/env sh
# The exit run, split into contiguous non-overlapping seed ranges.
#
# The phase is NOT written here. It is read from the options the sweep actually
# sweeps, because a banner beside a shape drifts from it -- this file printed
# "A6 exit run" over a sweep of A7's shape, which is the third instance of a
# label that stopped describing its subject (sim/hunt: ShapeNameOf).
#
# # Why splitting is legitimate, and what it is not allowed to do
#
# Ansh's ruling at A6, on the wall-time report: 25,000 seeds may run as
# contiguous non-overlapping ranges in separate invocations, aggregated, with the
# boundaries recorded so the union is provably the full set and no seed is
# counted twice or skipped. Not a reduced count, not a weaker workload.
#
# The argument it rests on is a property of the harness rather than a
# convenience: `MaterializeRaft(seed)` derives a whole plan from the seed alone,
# and the plan is the reproduction unit. Nothing about a run depends on which
# seeds ran before it in the same process, so a seed's verdict does not depend on
# which invocation ran it.
#
# What splitting must not do is lose seeds or double-count them, and that is
# CHECKED rather than assumed: TestRaftExitAggregate requires the shard censuses
# to sort into a contiguous cover of exactly [0, TOTAL), at one commit, each
# shard having finished the range it claims.
#
# # And it is what makes the run finishable
#
# 25,000 seeds at A6's ~3.75 s/seed is about 26 CPU-hours in one process. The
# same 26 CPU-hours across N processes is roughly 26/N wall-clock hours, which is
# the difference between a run that completes and a run that is always still
# going.
#
# usage: exit-run.sh [total] [shards] [outdir]
set -eu

GO=${GO:-go}
TOTAL=${1:-25000}
SHARDS=${2:-8}
OUT=${3:-.exitrun}
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)

if [ "$(git status --porcelain 2>/dev/null | wc -l)" -ne 0 ]; then
  printf 'exit-run: the tree is dirty. An exit run at an uncommitted tree names a commit\n'
  printf '          that does not contain what ran, which is a bundle that cannot be replayed\n'
  printf '          one level up.\n'
  exit 2
fi

mkdir -p "$OUT"
rm -f "$OUT"/shard-*.json "$OUT"/shard-*.log

SHAPE=$(${GO:-go} run ./cmd/shapename 2>/dev/null || echo "shape unknown")
printf '\n  exit run: %d seeds across %d shards at %s\n' "$TOTAL" "$SHARDS" "$COMMIT"
printf '  shape: %s\n' "$SHAPE"
printf '  ----------------------------------------------------------------\n'

# Contiguous by construction: each shard starts where the last ended, and the
# last one absorbs the remainder so the cover is exact rather than approximately
# exact.
i=0
from=0
while [ "$i" -lt "$SHARDS" ]; do
  to=$(( (i + 1) * TOTAL / SHARDS ))
  [ "$i" -eq $(( SHARDS - 1 )) ] && to=$TOTAL
  printf '   shard %-2d [%6d,%6d)\n' "$i" "$from" "$to"
  RAFT_FROM=$from RAFT_TO=$to RAFT_COMMIT=$COMMIT \
    RAFT_SHARD_OUT="$(pwd)/$OUT/shard-$(printf '%03d' "$i").json" \
    $GO test -count=1 -timeout 2400m -run TestRaftExitShard -v ./sim/hunt/ \
    > "$OUT/shard-$(printf '%03d' "$i").log" 2>&1 &
  from=$to
  i=$(( i + 1 ))
done

printf '  ----------------------------------------------------------------\n'
printf '  %d shards running. Aggregate with:\n' "$SHARDS"
printf '    RAFT_SHARD_DIR=%s RAFT_TOTAL=%d %s test -count=1 -run TestRaftExitAggregate -v ./sim/hunt/\n\n' \
  "$OUT" "$TOTAL" "$GO"
wait
printf '  all shards finished\n\n'
