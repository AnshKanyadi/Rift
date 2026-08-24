#!/usr/bin/env sh
# sim/hunt under -race, split into contiguous non-overlapping seed ranges.
#
# # Why sharded, and why that is legitimate here
#
# The same argument the exit run rests on: `MaterializeRaft(seed)` derives a
# whole plan from the seed alone, so a seed's verdict does not depend on which
# invocation ran it. What splitting must not do is lose seeds or double-count
# them, and the boundaries are printed so the union can be read.
#
# It is measured at about 43 s/seed instrumented, so 200 seeds in one process is
# roughly nine hours and across eight it is roughly one.
#
# usage: race-soak.sh [seeds] [shards]
set -eu
GO=${GO:-go}
TOTAL=${1:-200}
SHARDS=${2:-8}
OUT=${3:-.racesoak}
mkdir -p "$OUT"; rm -f "$OUT"/shard-*.log

printf '\n  race soak: sim/hunt under -race, %d seeds across %d shards\n' "$TOTAL" "$SHARDS"
printf '  ----------------------------------------------------------------\n'
i=0; from=0; rc=0
while [ "$i" -lt "$SHARDS" ]; do
  to=$(( (i + 1) * TOTAL / SHARDS ))
  [ "$i" -eq $(( SHARDS - 1 )) ] && to=$TOTAL
  printf '   shard %-2d [%4d,%4d)\n' "$i" "$from" "$to"
  RAFT_FROM=$from RAFT_SEEDS=$to LANE_SEEDS=6 \
    $GO test -race -count=1 -timeout 2400m ./sim/hunt/ > "$OUT/shard-$i.log" 2>&1 &
  from=$to; i=$(( i + 1 ))
done
wait || rc=1
races=$(cat "$OUT"/shard-*.log 2>/dev/null | grep -c 'DATA RACE' || true)
printf '  ----------------------------------------------------------------\n'
printf '   data races reported: %s\n' "$races"
if [ "$races" != "0" ]; then
  grep -A 20 'DATA RACE' "$OUT"/shard-*.log | head -60
  exit 1
fi
grep -l 'FAIL' "$OUT"/shard-*.log 2>/dev/null | sed 's/^/   FAILED shard: /' || true
[ "$rc" -eq 0 ] || exit 1
printf '   clean\n\n'
