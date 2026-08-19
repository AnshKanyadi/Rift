#!/usr/bin/env sh
# The harness-power lane, per mutant class.
#
# `make power` has stood since A0 as the lane that fails when detection power
# drops. It covered four toy flaw classes and ZERO mutant classes. So when
# pre-vote landed and M18's log-matching detections fell from 10 in 500 to 0, and
# M19's from 228 in 300 to 1, the lane was green -- not because it judged the drop
# acceptable but because it had never been looking. A lane whose whole purpose is
# to catch a power regression, silent through the largest power regression in the
# project, is not a lane. This is the missing half.
#
# Amendment A2 already required it: "CI runs the suite on every push and records
# kill-time per mutant (seeds-to-detection and wall-time-to-detection); a
# regression in kill-time is treated as a harness regression even while every
# mutant is still killed." The mutant lane recorded seconds. Seconds measure the
# machine. Seeds measure the harness.
#
# Every patch must declare its power expectation. Either:
#
#   # power-seeds: N      sweep this many seeds against the mutated tree
#   # power-floor: M      and require at least M of them to notice
#   # power-config: a1    optionally, the build to measure under (a1 | a2)
#
# or an explicit opt-out with a reason:
#
#   # power: n/a -- <why>
#
# A patch declaring neither FAILS the lane. That is the whole point: the previous
# arrangement let a class be uncovered by saying nothing, which is how thirty-one
# mutants ended up with four floors between them.
#
# usage: power-mutants.sh [--measure] [patch-dir]
set -eu

GO=${GO:-go}
MEASURE=no
if [ "${1:-}" = "--measure" ]; then MEASURE=yes; shift; fi
PATCHDIR=${1:-sim/mutants}
ROOT=$(pwd)

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

copy_tree() {
  mkdir -p "$1"
  tar cf - --exclude=./.git --exclude=./.github . | (cd "$1" && tar xf -)
}

printf '\n  harness power, per mutant class\n'
printf '  ----------------------------------------------------------------\n'

failed=0
covered=0
optout=0

for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  na=$(sed -n 's/^# power: *//p' "$patch")
  seeds=$(sed -n 's/^# power-seeds: *//p' "$patch")
  floor=$(sed -n 's/^# power-floor: *//p' "$patch")
  cfg=$(sed -n 's/^# power-config: *//p' "$patch")
  [ -n "$cfg" ] || cfg=a2

  if [ -n "$na" ]; then
    optout=$((optout + 1))
    printf '   n/a      %-44s %s\n' "$id" "$na"
    continue
  fi
  if [ -z "$seeds" ] || [ -z "$floor" ]; then
    printf '   UNCOVERED %s declares no power expectation.\n' "$id"
    printf '             Every mutant class carries a floor or an explicit opt-out with a reason.\n'
    printf '             Saying nothing is how thirty-one classes ended up sharing four floors.\n'
    failed=$((failed + 1))
    continue
  fi

  case $patch in
    /*) abs=$patch ;;
     *) abs=$ROOT/$patch ;;
  esac
  work="$scratch/$id"
  copy_tree "$work"
  if ! (cd "$work" && patch -p1 --silent --forward < "$abs" 2>/dev/null); then
    printf '   ROT      %s: patch no longer applies\n' "$id"
    failed=$((failed + 1))
    continue
  fi

  out=$(cd "$work" && POWER_SEEDS="$seeds" POWER_CONFIG="$cfg" \
    $GO test -count=1 -v -timeout 3600s -run TestPowerProbe ./sim/hunt/ 2>&1 | grep '^POWER ' || true)
  if [ -z "$out" ]; then
    printf '   ERROR    %s: the probe produced no measurement.\n' "$id"
    printf '            A power lane that cannot measure is a power lane reporting nothing.\n'
    failed=$((failed + 1))
    continue
  fi
  got=$(echo "$out" | sed -n 's/.*detected=\([0-9]*\).*/\1/p')
  first=$(echo "$out" | sed -n 's/.*first=\(-\{0,1\}[0-9]*\).*/\1/p')
  covered=$((covered + 1))

  if [ "$MEASURE" = yes ]; then
    printf '   measure  %-44s %s of %s (%s) first=%s\n' "$id" "$got" "$seeds" "$cfg" "$first"
    continue
  fi
  if [ "$got" -lt "$floor" ]; then
    printf '   DROPPED  %-44s %s of %s, floor %s (%s)\n' "$id" "$got" "$seeds" "$floor" "$cfg"
    printf '            The defect is still there and the harness notices it less often. That is a\n'
    printf '            regression in the machine that finds bugs, which is worth more than any one\n'
    printf '            bug it would have found.\n'
    failed=$((failed + 1))
  else
    printf '   ok       %-44s %s of %s, floor %s (%s) first=%s\n' "$id" "$got" "$seeds" "$floor" "$cfg" "$first"
  fi
done

printf '  ----------------------------------------------------------------\n'
printf '   %d classes floored, %d opted out with a reason, %d failures\n\n' "$covered" "$optout" "$failed"

if [ "$covered" -eq 0 ]; then
  printf '  No class carries a floor. An empty power lane proves nothing.\n\n'
  exit 2
fi
[ "$failed" -eq 0 ] || exit 1
