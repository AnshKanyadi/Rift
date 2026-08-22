#!/usr/bin/env sh
# What race seed count is actually needed.
#
# # The question, and the honest form of the answer
#
# Ansh, at the A5 sign-off: run sim/hunt under -race at 50, 100 and 200 seeds and
# report whether what 200 catches is still caught at the lower counts. Bound it at
# the smallest count that catches everything 200 catches, with the measurement
# recorded. Do not guess the number.
#
# There is a wrinkle to state before the numbers: the premise "it has found real
# races twice" appears in CARRY-FORWARD and has no record behind it. Both recorded
# race-lane failures were CLOCKS, not races (DESIGN-A4 §9.5), and DR-29 keeps
# tooling defects -- like the announcement-writer race A0.3's own test caught --
# out of BUGS.md, so a race found in tooling would leave no entry. So this script
# cannot compare detection against two known findings, and does not pretend to.
# What it measures is what is measurable: wall time at each count, and whether any
# data race is reported at any of them.
#
# # Why the number matters now
#
# RACE_SEEDS is 200 and RACE_TIMEOUT is 5400s. At A5's 0.36 s/seed that was
# comfortable. A6 costs ~3.75 s/seed uninstrumented and -race is roughly 20x, so
# 200 seeds no longer fits its own budget. Either the count moves or the budget
# does, and this is the measurement that decides which.
set -eu

GO=${GO:-go}
COUNTS=${1:-"50 100 200"}

printf '\n  race lane curve: sim/hunt under -race\n'
printf '  ----------------------------------------------------------------\n'
for n in $COUNTS; do
  start=$(date +%s)
  if out=$(RAFT_SEEDS=$n LANE_SEEDS=6 $GO test -race -count=1 -timeout 600m ./sim/hunt/ 2>&1); then
    verdict=clean
  else
    verdict=FAILED
  fi
  end=$(date +%s)
  races=$(printf '%s' "$out" | grep -c 'DATA RACE' || true)
  printf '   %4d seeds   %6ds   %-7s   data races: %s\n' "$n" "$((end - start))" "$verdict" "$races"
  if [ "$races" != "0" ]; then
    printf '%s\n' "$out" | grep -A 20 'DATA RACE' | head -40
  fi
done
printf '  ----------------------------------------------------------------\n'
printf '  Record the table in DESIGN-A6 and set RACE_SEEDS from it, not from habit.\n\n'
