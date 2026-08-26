#!/usr/bin/env sh
# The mutant campaign, with a FLOOR under every planted flaw class.
#
# Track A built this after discovering a harness defect that quietly cut
# detection to a sixth with every lane green. Ansh approved Track B mirroring
# the actual construct rather than inventing a parallel one, so this is
# sim/hunt/floors.go's shape with KILL POINTS substituted for seeds.
#
#   Every planted flaw class carries a floor -- a minimum detection RATE and a
#   maximum KILL-POINTS-TO-DETECTION -- and this lane FAILS when a class drops
#   below its floor.
#
# BOTH BOUNDS, BECAUSE THEY DEGRADE INDEPENDENTLY. Track A lost M19 to a
# count-based floor that could not see a seeds-to-detection regression: a class
# can hold its rate while its first detection moves far later in the space, and
# that is what decides whether a cheap sweep would ever see it.
#
# FLOORS WITH MARGIN, NEVER EXACT VALUES. The sweep is deterministic, so an
# exact assertion is possible and is deliberately not used -- it would fail on
# any benign change (one more Env call, a different workload) and A LANE THAT
# CRIES WOLF IS A LANE PEOPLE DELETE. A floor passes drift and fails a collapse,
# which is the only failure worth a build break.
#
# The floor is set against the SUPPRESSED number where one is known, not just
# under today's measurement, so a regression OF THAT KIND fails loudly.
#
# usage: cpp-campaign.sh [--measure] [floors-file]
#   --measure prints what each class actually does and asserts nothing. It is
#   how the floors were derived: measured, not guessed.
set -eu

MEASURE=no
if [ "${1:-}" = "--measure" ]; then MEASURE=yes; shift; fi
FLOORS=${1:-engine-cpp/FLOORS.txt}
ROOT=$(pwd)

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

copy_tree() {
  mkdir -p "$1"
  tar cf - . | (cd "$1" && tar xf -)
  rm -rf "$1/.git" "$1/.github"
  find "$1/engine-cpp" -maxdepth 1 -type d -name 'build*' -exec rm -rf {} + 2>/dev/null || true
}

# ONE REGIME PER FLOOR, NEVER AGGREGATED. Section 8.4: a number measured at
# non-default caps never mixes with a default-cap number, and B2 gives that rule
# its first real work -- the flush regime exists because B2's gates on the flush
# path cannot be induced by a workload that never flushes, and the default
# regime exists because B1's floors were measured against it and must stay
# comparable. Comparing one against the other's floor is the aggregation the
# rule forbids, so the regime is a column in FLOORS.txt rather than a fact about
# whoever ran the lane.
# THE SAME WATCHDOG cpp-mutants CARRIES, AND FOR THE SAME REASON. A mutation
# that unbounds a loop makes the sweep spin instead of report, and a campaign
# waiting on it is indistinguishable from a campaign working. HARNESS-013.
LANE_TIMEOUT=${LANE_TIMEOUT:-1200}

kill_tree() {  # kill_tree <pid>
  for c in $(pgrep -P "$1" 2>/dev/null); do kill_tree "$c"; done
  kill -9 "$1" 2>/dev/null || true
}

with_timeout() {  # with_timeout <cmd...>  -> 124 on timeout
  "$@" & wt_pid=$!
  ( sleep "$LANE_TIMEOUT"; kill_tree "$wt_pid" ) >/dev/null 2>&1 &
  wt_killer=$!
  # `|| wt_rc=$?` under `set -e`; see cpp-mutants.sh for why.
  wt_rc=0
  wait "$wt_pid" || wt_rc=$?
  kill "$wt_killer" 2>/dev/null || true
  wait "$wt_killer" 2>/dev/null || true
  [ "$wt_rc" -ge 128 ] && return 124
  return "$wt_rc"
}

build_and_sweep() {  # build_and_sweep <tree> <regime> -> "points violations first"
  ( cd "$1" && cmake -S engine-cpp -B engine-cpp/build/test -DRIFT_SANITIZER=none >/dev/null 2>&1 \
      && cmake --build engine-cpp/build/test --target rift_sweep -j "${WORKERS:-8}" >/dev/null 2>&1 ) || {
    echo "BUILD_FAILED"; return 0; }
  sweep_out=$(mktemp)
  # `|| rc=$?` AND NOT A BARE CALL. A PATCHED sweep exits NON-ZERO BY DESIGN --
  # that is the detection this lane is here to count -- so under `set -e` a bare
  # call kills the campaign at its first class, after the baselines have already
  # printed and while the log still looks healthy. Third instance of this exact
  # interaction in one remedy; see HARNESS-013.
  sweep_rc=0
  with_timeout "$1/engine-cpp/build/test/rift_sweep" "$2" >"$sweep_out" 2>/dev/null || sweep_rc=$?
  if [ "$sweep_rc" -eq 124 ]; then rm -f "$sweep_out"; echo "TIMEOUT"; return 0; fi
  line=$(grep "^SWEEP regime=$2 " "$sweep_out" || true)
  rm -f "$sweep_out"
  [ -n "$line" ] || { echo "NO_OUTPUT"; return 0; }
  printf '%s %s %s\n' \
    "$(printf '%s' "$line" | sed 's/.*points=\([0-9]*\).*/\1/')" \
    "$(printf '%s' "$line" | sed 's/.*violations=\([0-9]*\).*/\1/')" \
    "$(printf '%s' "$line" | sed 's/.*first=\([0-9]*\).*/\1/')"
}

printf '\n  mutant campaign: a floor under every planted flaw class\n'
printf '  ----------------------------------------------------------\n'

# BASELINE GATE. The unpatched sweep must find nothing, or every number below is
# unattributable and no floor means anything.
copy_tree "$scratch/baseline"
for reg in default flush; do
  base=$(build_and_sweep "$scratch/baseline" "$reg")
  case $base in
    TIMEOUT)
      printf '   INVALID  the unpatched %s sweep did not finish in %ss.\n\n' "$reg" "$LANE_TIMEOUT"; exit 2 ;;
    BUILD_FAILED|NO_OUTPUT)
      printf '   INVALID  the unpatched tree does not build or run the %s sweep.\n\n' "$reg"; exit 2 ;;
  esac
  base_points=$(printf '%s' "$base" | awk '{print $1}')
  base_viol=$(printf '%s' "$base" | awk '{print $2}')
  if [ "$base_viol" != "0" ]; then
    printf '   INVALID  the unpatched %s sweep reports %s violations.\n' "$reg" "$base_viol"
    printf '   Every detection below would be unattributable.\n\n'; exit 2
  fi
  printf '   baseline ok (%s): %s kill points, 0 violations\n' "$reg" "$base_points"
done

fails=0
checked=0
while IFS= read -r line; do
  case $line in ''|\#*) continue ;; esac
  cls=$(printf '%s' "$line"  | awk -F'|' '{print $1}' | sed 's/^ *//; s/ *$//')
  rate=$(printf '%s' "$line" | awk -F'|' '{print $2}' | sed 's/^ *//; s/ *$//')
  ceil=$(printf '%s' "$line" | awk -F'|' '{print $3}' | sed 's/^ *//; s/ *$//')
  meas=$(printf '%s' "$line" | awk -F'|' '{print $4}' | sed 's/^ *//; s/ *$//')
  why=$(printf '%s' "$line"  | awk -F'|' '{print $5}' | sed 's/^ *//; s/ *$//')
  reg=$(printf '%s' "$line"  | awk -F'|' '{print $6}' | sed 's/^ *//; s/ *$//')
  [ -n "$reg" ] || reg=default
  # A THIRD BOUND, AND B2 IS WHAT EARNED IT: the DETECTION COUNT.
  #
  # A rate is a fraction, and B2 changed its denominator without changing any
  # class's power -- the manifest added Env calls, so the sweep visits 300 kill
  # points where it visited 175, and every one of them is a point at which these
  # classes are not detectable. Every rate fell; not one count did. A lane that
  # broke the build on that would be reporting arithmetic as a regression.
  #
  # The count is immune to the denominator and blind to per-point dilution; the
  # rate is the reverse. That is the same argument the rate and the kill-point
  # ceiling already make about each other -- BOTH BOUNDS, BECAUSE THEY DEGRADE
  # INDEPENDENTLY -- and it is why this is a third column rather than a
  # replacement.
  minn=$(printf '%s' "$line" | awk -F'|' '{print $7}' | sed 's/^ *//; s/ *$//')
  [ -n "$minn" ] || minn=0
  case $reg in default|flush) ;; *)
    printf '   BAD      %s: unknown regime "%s"\n' "$cls" "$reg"; fails=$((fails + 1)); continue ;;
  esac

  patch_file=engine-cpp/mutants/$cls.patch
  if [ ! -f "$patch_file" ]; then
    printf '   BAD      %s has a floor and no patch\n' "$cls"; fails=$((fails + 1)); continue
  fi
  if [ -z "$meas" ] || [ -z "$why" ]; then
    printf '   BAD      %s: a floor needs the measurement it came from and the reasoning\n' "$cls"
    fails=$((fails + 1)); continue
  fi

  # A class exempted from the sweep carries a SPLIT LABEL naming the instrument
  # that catches it instead. It is not measured here, and it is not silent.
  case $rate in
    covered-by:*)
      inst=$(printf '%s' "$rate" | sed 's/^covered-by: *//')
      if [ -z "$inst" ]; then
        printf '   BAD      %s: covered-by names no instrument\n' "$cls"; fails=$((fails + 1))
      else
        printf '   exempt   %-30s covered-by %s\n' "$cls" "$inst"
      fi
      continue ;;
    killed-by-guard:*)
      # A DIFFERENT INSTRUMENT, SO A DIFFERENT LABEL. These die when a guard
      # ABORTS the process, so the test suite reports NO FAILING TEST -- which
      # reads as a survival unless the reader knows. "No failing test" and
      # "killed by a guard" are opposite conclusions from identical output.
      inst=$(printf '%s' "$rate" | sed 's/^killed-by-guard: *//')
      if [ -z "$inst" ]; then
        printf '   BAD      %s: killed-by-guard names no guard\n' "$cls"; fails=$((fails + 1))
      else
        printf '   exempt   %-30s ABORTED BY %s\n' "$cls" "$inst"
      fi
      continue ;;
  esac

  work="$scratch/$cls"
  copy_tree "$work"
  ( cd "$work" && patch -p1 --silent --forward < "$ROOT/$patch_file" ) || {
    printf '   ROT      %s: patch no longer applies\n' "$cls"; fails=$((fails + 1)); continue; }
  out=$(build_and_sweep "$work" "$reg")
  case $out in
    TIMEOUT)
      printf '   TIMEOUT  %s [%s]: the sweep did not finish in %ss\n' "$cls" "$reg" "$LANE_TIMEOUT"
      printf '            The mutation makes the sweep HANG rather than detect. A lane that\n'
      printf '            does not report has measured nothing, and a measurement of nothing\n'
      printf '            is not a floor.\n'
      fails=$((fails + 1)); continue ;;
    BUILD_FAILED|NO_OUTPUT)
      printf '   BROKEN   %s: the patched tree does not build or run\n' "$cls"
      fails=$((fails + 1)); continue ;;
  esac
  points=$(printf '%s' "$out" | awk '{print $1}')
  viol=$(printf '%s'   "$out" | awk '{print $2}')
  first=$(printf '%s'  "$out" | awk '{print $3}')
  permille=$(( viol * 1000 / points ))
  checked=$((checked + 1))

  if [ "$MEASURE" = yes ]; then
    printf '   measured %-30s [%s] %d/%d = %d per mille, first at kill point %s\n' \
      "$cls" "$reg" "$viol" "$points" "$permille" "$first"
    continue
  fi

  bad=0
  if [ "$viol" -lt "$minn" ]; then
    printf '   BELOW FLOOR  %s: %d detections < count floor %s\n' "$cls" "$viol" "$minn"
    bad=1
  fi
  if [ "$permille" -lt "$rate" ]; then
    printf '   BELOW FLOOR  %s: detection %d per mille < floor %s\n' "$cls" "$permille" "$rate"
    bad=1
  fi
  if [ "$viol" -eq 0 ] || [ "$first" -gt "$ceil" ]; then
    printf '   BELOW FLOOR  %s: first detection at kill point %s > ceiling %s\n' "$cls" "$first" "$ceil"
    bad=1
  fi
  if [ "$bad" -eq 1 ]; then
    # THE FIRST QUESTION ON A RED LANE IS "is the floor wrong, or is the
    # harness?", and the answer has to be here or the lane gets edited instead
    # of investigated.
    printf '                derived from: %s\n' "$meas"
    printf '                reasoning   : %s\n' "$why"
    fails=$((fails + 1))
  else
    printf '   holds    %-30s [%s] %d detections (floor %s), %d per mille (floor %s), first at %s (ceiling %s)\n' \
      "$cls" "$reg" "$viol" "$minn" "$permille" "$rate" "$first" "$ceil"
  fi
done < "$FLOORS"

printf '  ----------------------------------------------------------\n'
printf '   %d class(es) measured, %d problem(s)\n\n' "$checked" "$fails"
if [ "$MEASURE" = yes ]; then exit 0; fi
if [ "$checked" -eq 0 ]; then
  printf '  No class was measured. A campaign that measures nothing proves nothing.\n\n'; exit 2
fi
[ "$fails" -eq 0 ] || exit 1
