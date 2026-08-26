#!/usr/bin/env sh
# The exit run, split into contiguous non-overlapping seed ranges.
#
# The phase is NOT written here. It is read from the options the sweep actually
# sweeps, because a banner beside a shape drifts from it -- this file printed
# "A6 exit run" over a sweep of A7's shape, which is the third instance of a
# label that stopped describing its subject (sim/hunt: ShapeNameOf).
#
# # Why the whole script is a function, and it is not style
#
# POSIX sh reads a script INCREMENTALLY, by byte offset, while it runs. A7's exit
# run was launched, and forty minutes later a commit edited this file to fix that
# banner. Six and a half hours after that the shell returned from `wait`, resumed
# reading at its saved offset -- now pointing into the middle of a file that had
# grown -- executed a fragment of the loop body as a command, hit a bare `done`,
# and exited 2. The shards were unharmed: they were already-forked children with
# their own binaries. But `EXIT=2` in the log said nothing about the sweep, which
# is the eighth instance of the observability family in this repo and its
# sharpest form: THE RUN'S OWN EXIT STATUS STOPPED DESCRIBING THE RUN.
#
# Wrapping the body in a function fixes it structurally: sh parses a function
# definition in full before executing anything, so an edit to this file mid-run
# cannot corrupt the read offset. "Do not touch the tree mid-run" is still the
# rule; this is what makes breaking it survivable.
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
# # Cost, measured rather than planned
#
# A6's exit run measured 8.4 s/seed and about 58 CPU-hours (cb4937d). A7's shape
# measured 7.5 s/seed per shard across eight shards. The figure this file used to
# quote -- 3.75 s/seed -- was A6's PLANNING number, and A6's own run had already
# superseded it; leaving it here is how A7's halved rate went unquestioned.
#
# usage: exit-run.sh [total] [shards] [outdir]
set -eu

main() {
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

  # # Total parallelism is CHOSEN, not emergent
  #
  # The shard count divides the SEED SPACE. It does not divide the machine: each
  # shard is a `go test` process running Go's own scheduler, so eight shards on
  # eleven cores produced a load average of 21. Every wall-clock estimate derived
  # from the shard count was wrong for that reason -- not arithmetic, but
  # multiplying the wrong number.
  #
  # So both halves are set here and total parallelism is SHARDS x GOMAXPROCS,
  # printed with the banner so the number that produced a wall clock is on the
  # record beside it.
  CORES=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 8)
  if [ "${SHARD_PROCS:-}" = "" ]; then
    SHARD_PROCS=$(( CORES / SHARDS ))
    [ "$SHARD_PROCS" -lt 1 ] && SHARD_PROCS=1
  fi

  mkdir -p "$OUT"
  rm -f "$OUT"/shard-*.json "$OUT"/shard-*.log "$OUT"/shard-*.progress

  SHAPE=$($GO run ./cmd/shapename 2>/dev/null || echo "shape unknown")
  printf '\n  exit run: %d seeds across %d shards at %s\n' "$TOTAL" "$SHARDS" "$COMMIT"
  printf '  shape: %s\n' "$SHAPE"
  printf '  parallelism: %d shards x GOMAXPROCS=%d = %d, on %d cores\n' \
    "$SHARDS" "$SHARD_PROCS" "$(( SHARDS * SHARD_PROCS ))" "$CORES"
  printf '  ----------------------------------------------------------------\n'

  # Contiguous by construction: each shard starts where the last ended, and the
  # last one absorbs the remainder so the cover is exact rather than
  # approximately exact.
  i=0
  from=0
  pids=''
  while [ "$i" -lt "$SHARDS" ]; do
    to=$(( (i + 1) * TOTAL / SHARDS ))
    [ "$i" -eq $(( SHARDS - 1 )) ] && to=$TOTAL
    printf '   shard %-2d [%6d,%6d)\n' "$i" "$from" "$to"
    GOMAXPROCS=$SHARD_PROCS RAFT_FROM=$from RAFT_TO=$to RAFT_COMMIT=$COMMIT \
      RAFT_SHARD_OUT="$(pwd)/$OUT/shard-$(printf '%03d' "$i").json" \
      $GO test -count=1 -timeout 2400m -run TestRaftExitShard -v ./sim/hunt/ \
      > "$OUT/shard-$(printf '%03d' "$i").log" 2>&1 &
    pids="$pids $!"
    from=$to
    i=$(( i + 1 ))
  done

  printf '  ----------------------------------------------------------------\n'
  printf '  %d shards running. Progress (written per seed, from the sweep loop):\n' "$SHARDS"
  printf '    tail -n +1 %s/shard-*.progress\n' "$OUT"
  printf '  Aggregate with:\n'
  printf '    RAFT_SHARD_DIR=%s RAFT_TOTAL=%d %s test -count=1 -run TestRaftExitAggregate -v ./sim/hunt/\n\n' \
    "$OUT" "$TOTAL" "$GO"

  # # Wait per shard, so the exit status describes the run
  #
  # A bare `wait` discards every child's status and returns 0, so "all shards
  # finished" printed identically whether they passed or failed. That is the same
  # family as the byte-offset corruption above: a status that stops describing
  # its subject. Each shard is waited on by pid and its status counted.
  failed=0
  for p in $pids; do
    if ! wait "$p"; then
      failed=$(( failed + 1 ))
    fi
  done

  if [ "$failed" -ne 0 ]; then
    printf '  %d of %d shards FAILED. Their logs are in %s/shard-*.log\n\n' \
      "$failed" "$SHARDS" "$OUT"
    return 1
  fi
  printf '  all %d shards finished clean\n\n' "$SHARDS"
  return 0
}

# `exit` explicitly, and that half is not decoration.
#
# Measured, by inducing the failure rather than reasoning about it: with the body
# in a function and a bare `main "$@"` at the end, the body RAN CORRECTLY and the
# shell then resumed reading the shifted tail and died 127 on a fragment. The
# wrapper protects the run; only the explicit exit protects the STATUS, and the
# status was the thing that stopped describing the run. Both halves, or the fix
# is half a fix.
#
# (The induction needs an in-place truncating rewrite -- the same inode the
# running shell holds a descriptor on. `perl -i` renames and does NOT reproduce
# it, which is worth knowing before concluding the hazard is not real.)
main "$@"; exit $?
