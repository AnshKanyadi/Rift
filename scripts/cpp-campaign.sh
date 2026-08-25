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

build_and_sweep() {  # build_and_sweep <tree> -> prints "points violations first"
  ( cd "$1" && cmake -S engine-cpp -B engine-cpp/build/test -DRIFT_SANITIZER=none >/dev/null 2>&1 \
      && cmake --build engine-cpp/build/test --target rift_sweep -j "${WORKERS:-8}" >/dev/null 2>&1 ) || {
    echo "BUILD_FAILED"; return 0; }
  line=$("$1/engine-cpp/build/test/rift_sweep" 2>/dev/null | grep '^SWEEP' || true)
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
base=$(build_and_sweep "$scratch/baseline")
case $base in
  BUILD_FAILED|NO_OUTPUT)
    printf '   INVALID  the unpatched tree does not build or run the sweep.\n\n'; exit 2 ;;
esac
base_points=$(printf '%s' "$base" | awk '{print $1}')
base_viol=$(printf '%s' "$base" | awk '{print $2}')
if [ "$base_viol" != "0" ]; then
  printf '   INVALID  the unpatched sweep reports %s violations.\n' "$base_viol"
  printf '   Every detection below would be unattributable.\n\n'; exit 2
fi
printf '   baseline ok: %s kill points, 0 violations\n' "$base_points"

fails=0
checked=0
while IFS= read -r line; do
  case $line in ''|\#*) continue ;; esac
  cls=$(printf '%s' "$line"  | awk -F'|' '{print $1}' | sed 's/^ *//; s/ *$//')
  rate=$(printf '%s' "$line" | awk -F'|' '{print $2}' | sed 's/^ *//; s/ *$//')
  ceil=$(printf '%s' "$line" | awk -F'|' '{print $3}' | sed 's/^ *//; s/ *$//')
  meas=$(printf '%s' "$line" | awk -F'|' '{print $4}' | sed 's/^ *//; s/ *$//')
  why=$(printf '%s' "$line"  | awk -F'|' '{print $5}' | sed 's/^ *//; s/ *$//')

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
  esac

  work="$scratch/$cls"
  copy_tree "$work"
  ( cd "$work" && patch -p1 --silent --forward < "$ROOT/$patch_file" ) || {
    printf '   ROT      %s: patch no longer applies\n' "$cls"; fails=$((fails + 1)); continue; }
  out=$(build_and_sweep "$work")
  case $out in
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
    printf '   measured %-30s %d/%d = %d per mille, first at kill point %s\n' \
      "$cls" "$viol" "$points" "$permille" "$first"
    continue
  fi

  bad=0
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
    printf '   holds    %-30s %d per mille (floor %s), first at %s (ceiling %s)\n' \
      "$cls" "$permille" "$rate" "$first" "$ceil"
  fi
done < "$FLOORS"

printf '  ----------------------------------------------------------\n'
printf '   %d class(es) measured, %d problem(s)\n\n' "$checked" "$fails"
if [ "$MEASURE" = yes ]; then exit 0; fi
if [ "$checked" -eq 0 ]; then
  printf '  No class was measured. A campaign that measures nothing proves nothing.\n\n'; exit 2
fi
[ "$fails" -eq 0 ] || exit 1
