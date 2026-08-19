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
#   # power-ceiling: K    and require the FIRST of them to be at or before seed K
#   # power-config: a1    optionally, the build to measure under (a1 | a2 | a3 |
#                        a4 | a5); the default is `current`, which is whatever
#                        shape the sweep runs today
#
# # The default used to be spelled `a3`, and the label went stale silently
#
# It meant "what the sweep runs", and it was written when A3 was what the sweep
# ran. The probe had no case for "a3" at all, so it fell through to
# CurrentOptions -- correct behaviour under a label that had stopped describing
# it. Every measurement in every patch header therefore says `(a3)` and means
# `(current)`, which is exactly the kind of quiet drift this lane exists to catch
# in the system under test.
#
# The default is now named `current` and the report prints that. A patch that
# genuinely needs an older shape names it, and the probe now has a case for each
# one -- so a name means what it says for as long as the patch says it.
#
# # The ceiling exists because the floor could not see the regression twice
#
# A2's words are "seeds-to-detection AND wall-time-to-detection", and this lane
# measured neither -- it measured a rate. A rate is blind to the regression that
# actually happens: the class stays exactly as detectable and the detection moves
# far later in the seed space, so every smoke lane and every mid-phase iteration
# stops finding it while the nightly number looks unchanged.
#
# It has now happened twice, and the second time is the one that names it. A4's
# client-routing change took M19 from first-detection at seed 145 to seed 553.
# Its RATE barely moved -- 10 to 7 per 1500, against a floor of 4 -- so this lane
# was green. The mutant lane caught it, by accident, because M19's covering test
# ran 500 seeds and the first detecting seed had moved past the end of it.
#
# So a class declares both, and breaching either fails the lane. A floor with no
# ceiling is half an instrument, and this project has now shipped the half twice.
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
  ceiling=$(sed -n 's/^# power-ceiling: *//p' "$patch")
  cfg=$(sed -n 's/^# power-config: *//p' "$patch")
  [ -n "$cfg" ] || cfg=current

  if [ -n "$na" ]; then
    optout=$((optout + 1))
    printf '   n/a      %-44s %s\n' "$id" "$na"
    continue
  fi
  # In --measure mode only the seed count is needed: that mode exists to PRODUCE
  # the floor and the ceiling, so demanding them first would make a new class
  # unmeasurable until somebody guessed its numbers.
  if [ "$MEASURE" = yes ]; then
    [ -n "$seeds" ] || { printf '   UNCOVERED %s declares no power-seeds.\n' "$id"; failed=$((failed + 1)); continue; }
  elif [ -z "$seeds" ] || [ -z "$floor" ] || [ -z "$ceiling" ]; then
    printf '   UNCOVERED %s declares no power expectation.\n' "$id"
    printf '             Every mutant class carries a rate floor AND a seeds-to-detection ceiling,\n'
    printf '             or an explicit opt-out with a reason. Saying nothing is how thirty-one\n'
    printf '             classes ended up sharing four floors, and a rate with no ceiling is how a\n'
    printf '             kill-time regression went past this lane twice.\n'
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
  bad=no
  if [ "$got" -lt "$floor" ]; then
    printf '   DROPPED  %-44s rate %s of %s, floor %s (%s)\n' "$id" "$got" "$seeds" "$floor" "$cfg"
    printf '            The defect is still there and the harness notices it less often. That is a\n'
    printf '            regression in the machine that finds bugs, which is worth more than any one\n'
    printf '            bug it would have found.\n'
    bad=yes
  fi
  if [ "$first" -lt 0 ] || [ "$first" -gt "$ceiling" ]; then
    printf '   SLOWED   %-44s first detection at seed %s, ceiling %s (%s)\n' "$id" "$first" "$ceiling" "$cfg"
    printf '            The rate may be fine and the class has still moved out of reach of every\n'
    printf '            short run: a smoke lane and a mid-phase iteration stop finding it while the\n'
    printf '            nightly number looks unchanged. Amendment A2 calls this a harness regression\n'
    printf '            even while every mutant is still killed.\n'
    bad=yes
  fi
  if [ "$bad" = yes ]; then
    failed=$((failed + 1))
  else
    printf '   ok       %-44s %s of %s (floor %s), first=%s (ceiling %s) (%s)\n' \
      "$id" "$got" "$seeds" "$floor" "$first" "$ceiling" "$cfg"
  fi
done

printf '  ----------------------------------------------------------------\n'
printf '   %d classes floored and ceilinged, %d opted out with a reason, %d failures\n\n' "$covered" "$optout" "$failed"

if [ "$covered" -eq 0 ]; then
  printf '  No class carries a floor. An empty power lane proves nothing.\n\n'
  exit 2
fi
[ "$failed" -eq 0 ] || exit 1
