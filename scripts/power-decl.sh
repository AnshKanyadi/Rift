#!/usr/bin/env sh
# Every mutant's POWER DECLARATION is internally consistent, checked without
# running anything.
#
# # Why this exists
#
# `make power-mutants` went red the day `M67` and `M70` landed and stayed red for
# the back half of A6. Both declared `power-seeds: 1 / floor: 1 / ceiling: 1` with
# a note saying they are killed deterministically by their covering test -- and
# both covering tests are UNIT tests, while the probe measures SWEEP detection. A
# one-seed sweep floor is a claim the instrument can never satisfy.
#
# Nobody noticed because the lane costs fifteen CPU-hours, so nothing runs it: it
# is in `make ci`, the workflow has never executed, and the pre-push hook -- the
# only thing on this machine that runs automatically -- does not include it.
#
# **The declaration is checkable in milliseconds and the measurement is not.** So
# the cheap half goes where something runs it, and the expensive half keeps its
# tier. This catches the shape that actually failed without waiting for the sweep
# that would have caught it eventually.
set -eu
PATCHDIR=${1:-sim/mutants}

printf '\n  power declarations: consistent without running anything\n'
printf '  ----------------------------------------------------------------\n'
bad=0; checked=0
for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  na=$(sed -n 's/^# power: *//p' "$patch")
  seeds=$(sed -n 's/^# power-seeds: *//p' "$patch")
  floor=$(sed -n 's/^# power-floor: *//p' "$patch")
  ceiling=$(sed -n 's/^# power-ceiling: *//p' "$patch")
  detector=$(sed -n 's/^# power-detector: *//p' "$patch")
  measured=$(sed -n 's/^# power-measured: *//p' "$patch")
  [ -n "$detector" ] || detector=rate
  checked=$((checked + 1))

  say() { printf '   BAD      %-44s %s\n' "$id" "$1"; bad=$((bad + 1)); }

  if [ -n "$na" ]; then
    # An opt-out must carry a reason, and "n/a" alone is not one.
    if [ "$(printf '%s' "$na" | wc -c)" -lt 40 ]; then
      say "opts out with a reason of $(printf '%s' "$na" | wc -c) characters; an opt-out is a claim"
    fi
    continue
  fi

  [ -n "$seeds" ] || { say "declares neither an opt-out nor power-seeds"; continue; }

  # # A sweep expectation on a handful of seeds is not a measurement
  #
  # This is the rule that would have caught M67, M68 and M70 on the day they
  # landed. A class whose covering test is a unit test has no sweep rate; saying
  # "1 of 1" does not give it one, it just makes the lane red.
  if [ "$seeds" -lt 30 ]; then
    say "declares a sweep expectation over $seeds seeds; a sweep that short cannot measure a rate, and a class killed deterministically by a unit test wants an opt-out with that reason"
  fi

  case "$measured" in
    "")        say "carries no power-measured line, so its floor is a guess nobody has checked" ;;
    PENDING*)  say "carries power-measured: PENDING; a floor asserted against a measurement that was never taken is a number nobody chose" ;;
  esac

  if [ "$detector" = sweep ]; then
    [ -z "$floor" ] && [ -z "$ceiling" ] || \
      say "declares a sweep detector AND a rate floor; a sweep verdict has no rate"
    continue
  fi

  [ -n "$floor" ] || say "declares power-seeds and no power-floor"
  [ -n "$ceiling" ] || say "declares power-seeds and no power-ceiling"
  if [ -n "$ceiling" ] && [ "$ceiling" -gt "$seeds" ]; then
    say "declares a ceiling of $ceiling above its own sweep of $seeds seeds, which nothing can breach"
  fi
  if [ -n "$floor" ] && [ "$floor" -gt "$seeds" ]; then
    say "declares a floor of $floor above its own sweep of $seeds seeds, which nothing can meet"
  fi
done
printf '  ----------------------------------------------------------------\n'
printf '   %d declarations checked, %d inconsistent\n\n' "$checked" "$bad"
[ "$checked" -gt 0 ] || { printf '  No declarations. This lane proved nothing.\n\n'; exit 2; }
[ "$bad" -eq 0 ] || exit 1
