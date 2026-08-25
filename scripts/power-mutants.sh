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
# or ONE of the two exemptions, which used to be one label and are now two:
#
#   # power-covered-by: <instrument> -- <why>
#        this class is covered by a NAMED instrument that is not a sweep rate.
#
#   # power-unreachable: <detector> -- <why, including NO OTHER DETECTOR>
#        this class is claimed out of the sweep's reach, and the detector the
#        claim was taken against is named.
#
# # Why one label became two
#
# `# power: n/a -- <reason>` meant both *nothing can reach this* and *something
# better than a sweep already covers this*, which are opposites. The eight
# framework classes wearing it are killed by their covering tests in about a
# SECOND each -- the best-covered classes in the tree -- and they wore the same
# sentence as `M56`, whose reachability claim was false on the day it was
# written. **A label that collapses two opposite meanings is worse than no label,
# because it makes the well-covered case indistinguishable from the unexamined
# one.** DESIGN-A6 §43.
#
# The bare `# power:` is retired and survives only on a patch that must SURVIVE,
# where the exemption is earned by `expect: alive` rather than granted by the
# sentence. `scripts/power-refute.sh` re-measures the `power-unreachable` claims
# it can judge soundly and RUNS the instrument every `power-covered-by` names.
#
# A patch declaring neither FAILS the lane. That is the whole point: the previous
# arrangement let a class be uncovered by saying nothing, which is how thirty-one
# mutants ended up with four floors between them.
#
# usage: power-mutants.sh [--measure] [patch-dir]
#
# # POWER_JOBS: the lane's own budget problem, answered
#
# 36 classes measure under `current`, which is now A6's shape: about 14,700
# seed-runs at ~3.75 s/seed, or fifteen CPU-hours run one at a time. That is not
# a number anybody schedules, and the consequence of not scheduling it is the one
# CARRY-FORWARD already records -- every floor in the tree is still measured
# under A5's cost.
#
# The measurement parallelises exactly, and the reason is worth stating rather
# than assumed: **what this lane produces is a detection COUNT and a
# first-detecting SEED, and both are functions of the seed and the patch alone.**
# `MaterializeRaftWith(seed, opt)` derives a whole plan from the seed; nothing in
# a run depends on what else the machine is doing. So N mutants measured
# concurrently give byte-identical numbers to N measured in turn.
#
# What does NOT survive parallelism is wall-time-to-detection, which Amendment A2
# also asks for. That half is measured with POWER_JOBS=1 or not claimed, and the
# report says which -- a number taken under contention reported as a solo one is
# the shape of dishonesty this file exists to avoid.
#
# Output order is unchanged: every mutant writes its own result file and the
# report is printed in patch order afterwards, so a parallel run and a sequential
# run produce the same text.
set -eu

GO=${GO:-go}
MEASURE=no
if [ "${1:-}" = "--measure" ]; then MEASURE=yes; shift; fi
PATCHDIR=${1:-sim/mutants}
ROOT=$(pwd)
JOBS=${POWER_JOBS:-1}

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

copy_tree() {
  mkdir -p "$1"
  tar cf - --exclude=./.git --exclude=./.github . | (cd "$1" && tar xf -)
}

# measure_one runs one patch's probe and writes "status<TAB>detail" to $2.
measure_one() {
  patch=$1; out=$2
  id=$(sed -n 's/^# id: *//p' "$patch")
  seeds=$(sed -n 's/^# power-seeds: *//p' "$patch")
  cfg=$(sed -n 's/^# power-config: *//p' "$patch"); [ -n "$cfg" ] || cfg=current
  case $patch in /*) abs=$patch ;; *) abs=$ROOT/$patch ;; esac
  work="$scratch/$id"
  copy_tree "$work"
  if ! (cd "$work" && patch -p1 --silent --forward < "$abs" 2>/dev/null); then
    printf 'ROT\t\n' > "$out"; rm -rf "$work"; return 0
  fi
  # # Keep the raw output, because "no measurement" is six different problems
  #
  # Everything that is not a clean POWER line lands as one word, ERROR, and the
  # report says *the probe produced no measurement*. That is true of a build
  # failure, a panic, a test timeout, a tree copied while somebody was editing
  # it, and a probe that ran and printed nothing -- and those want different
  # responses. Diagnosing one of them cost three runs of this lane, because the
  # only evidence was the word ERROR.
  #
  # So the raw tail is kept and the report prints it. It is the cheapest possible
  # version of the rule this project keeps re-learning: a verdict that cannot be
  # acted on is a verdict somebody has to reproduce by hand.
  (cd "$work" && POWER_SEEDS="$seeds" POWER_CONFIG="$cfg" \
    $GO test -count=1 -v -timeout 3600s -run TestPowerProbe ./sim/hunt/ >"$out.log" 2>&1) || true
  probe=$(grep '^POWER' "$out.log" || true)
  rm -rf "$work"
  rate=$(printf '%s\n' "$probe" | grep '^POWER ' || true)
  if [ -z "$rate" ]; then
    printf 'ERROR\t\n' > "$out"
    tail -6 "$out.log" 2>/dev/null | sed 's/^/            > /' > "$out.diag" || true
    return 0
  fi
  printf 'OK\t%s\n' "$rate" > "$out"
  # The sweep failures, one per line, so the report can diff them against the
  # unmutated tree's. A PRESENCE is not a detection -- DESIGN-A6 §16.4 is the
  # record of what believing one costs -- so only a failure the baseline does not
  # have counts.
  printf '%s\n' "$probe" | grep '^POWER-SWEEP ' | sed 's/^POWER-SWEEP //' >> "$out" || true
}

# baseline_sweep prints the unmutated tree's sweep failures for one (seeds, cfg),
# computed once and cached. Most classes share a shape, so this is a handful of
# runs rather than one per class.
baseline_sweep() {
  bseeds=$1; bcfg=$2
  cache="$scratch/baseline-$bseeds-$bcfg.sweep"
  if [ ! -f "$cache" ]; then
    POWER_SEEDS="$bseeds" POWER_CONFIG="$bcfg" \
      $GO test -count=1 -v -timeout 3600s -run TestPowerProbe ./sim/hunt/ 2>&1 \
      | grep '^POWER-SWEEP ' | sed 's/^POWER-SWEEP //' > "$cache" || true
    [ -f "$cache" ] || : > "$cache"
  fi
  cat "$cache"
}

# Phase one, when POWER_JOBS > 1: every measurable patch runs concurrently,
# throttled, into its own result file. The report below then reads the files
# instead of running the probe, so its shape and its order are unchanged.
if [ "$JOBS" -gt 1 ]; then
  running=0
  for patch in "$PATCHDIR"/*.patch; do
    id=$(sed -n 's/^# id: *//p' "$patch")
    [ -n "$(sed -n 's/^# power-covered-by: *//p' "$patch")" ] && continue
    [ -n "$(sed -n 's/^# power-unreachable: *//p' "$patch")" ] && continue
    [ -n "$(sed -n 's/^# power: *//p' "$patch")" ] && continue
    [ -n "$(sed -n 's/^# power-seeds: *//p' "$patch")" ] || continue
    measure_one "$patch" "$scratch/$id.result" &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
  done
  wait
fi

printf '\n  harness power, per mutant class\n'
printf '  ----------------------------------------------------------------\n'

failed=0
covered=0
optout=0

for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  na=$(sed -n 's/^# power: *//p' "$patch")
  coveredby=$(sed -n 's/^# power-covered-by: *//p' "$patch")
  unreach=$(sed -n 's/^# power-unreachable: *//p' "$patch")
  expect=$(sed -n 's/^# expect: *//p' "$patch")
  seeds=$(sed -n 's/^# power-seeds: *//p' "$patch")
  floor=$(sed -n 's/^# power-floor: *//p' "$patch")
  ceiling=$(sed -n 's/^# power-ceiling: *//p' "$patch")
  cfg=$(sed -n 's/^# power-config: *//p' "$patch")
  [ -n "$cfg" ] || cfg=current
  detector=$(sed -n 's/^# power-detector: *//p' "$patch")
  [ -n "$detector" ] || detector=rate

  # The two exemptions are reported by KIND, so the report itself stops
  # collapsing them. `covered` is a class with a better instrument than this
  # lane; `unreach` is a class making a claim this lane does not test and
  # `power-refute` does.
  if [ -n "$coveredby" ]; then
    optout=$((optout + 1))
    printf '   covered  %-44s %s\n' "$id" "$coveredby"
    continue
  fi
  if [ -n "$unreach" ]; then
    optout=$((optout + 1))
    printf '   unreach  %-44s %s\n' "$id" "$unreach"
    continue
  fi
  if [ -n "$na" ]; then
    if [ "$expect" = alive ]; then
      optout=$((optout + 1))
      printf '   n/a      %-44s %s\n' "$id" "$na"
      continue
    fi
    printf '   RETIRED  %-44s carries the bare power: opt-out.\n' "$id"
    printf '            One label meant both "nothing can reach this" and "something better than a\n'
    printf '            sweep covers this". Declare power-covered-by: or power-unreachable:.\n'
    failed=$((failed + 1))
    continue
  fi
  # In --measure mode only the seed count is needed: that mode exists to PRODUCE
  # the floor and the ceiling, so demanding them first would make a new class
  # unmeasurable until somebody guessed its numbers.
  if [ "$MEASURE" = yes ]; then
    [ -n "$seeds" ] || { printf '   UNCOVERED %s declares no power-seeds.\n' "$id"; failed=$((failed + 1)); continue; }
  elif [ "$detector" = sweep ] && [ -z "$seeds" ]; then
    printf '   UNCOVERED %s declares a sweep detector and no power-seeds.\n' "$id"
    failed=$((failed + 1))
    continue
  elif [ "$detector" != sweep ] && { [ -z "$seeds" ] || [ -z "$floor" ] || [ -z "$ceiling" ]; }; then
    printf '   UNCOVERED %s declares no power expectation.\n' "$id"
    printf '             Every mutant class carries a rate floor AND a seeds-to-detection ceiling,\n'
    printf '             or an explicit opt-out with a reason. Saying nothing is how thirty-one\n'
    printf '             classes ended up sharing four floors, and a rate with no ceiling is how a\n'
    printf '             kill-time regression went past this lane twice.\n'
    failed=$((failed + 1))
    continue
  fi

  # # ONE measurement path, because two of them drifted and a detector went blind
  #
  # This used to be an if/else: `POWER_JOBS > 1` read a result file written by
  # `measure_one`, and the sequential branch ran its own inline copy of the same
  # probe. The inline copy grepped `'^POWER '` -- with the trailing space, so the
  # RATE line only -- and never wrote the result file at all.
  #
  # Then `power-detector: sweep` landed (§35.1) and was taught to the shared
  # helper. It reads `$scratch/$id.result` for the `POWER-SWEEP` lines. In
  # sequential mode that file does not exist, so the mutated sweep failures were
  # always EMPTY, `comm -13` against the baseline always found nothing new, and
  # **every sweep-detector class reported BLIND.**
  #
  # `POWER_JOBS` defaults to 1 and `make power-mutants` sets nothing, so that was
  # the DEFAULT mode of the lane. DESIGN-A6 §43.9d is the written case.
  #
  # The fix is not to teach the second path the new detector. It is to delete the
  # second path: one function measures, both modes read its output, and a
  # detector added later cannot land in one of them.
  if [ "$JOBS" -le 1 ]; then
    measure_one "$patch" "$scratch/$id.result"
  fi
  # # HEAD -1, and the bug it fixes is why this comment is long
  #
  # The result file `measure_one` writes has a SHAPE:
  #
  #     line 1   <status>TAB<the POWER rate line>
  #     line 2+  one POWER-SWEEP failure per line
  #
  # This read was `cut -f1 "$file"`, which emits one field PER LINE -- so
  # `status` came back as "OK\n<sweep>\n<sweep>" whenever the probe reported any
  # sweep failure at all. `[ "$status" != OK ]` is then true, and the class was
  # reported as **ERROR -- the probe produced no measurement**, having measured
  # perfectly.
  #
  # At the seed counts this lane runs, a clean tree fails several NON-VACUITY
  # criteria (*no snapshot was ever taken*, *no prewrite met a live lock*), so
  # almost every class emits sweep lines. **The effect was that `POWER_JOBS > 1`
  # could not report a pass for essentially any class**, and it has been that way
  # since the sweep detector landed. Nothing noticed, because nothing runs this
  # lane (§37, RISK-1).
  #
  # DESIGN-A6 §43.9e is the written case. The rule: a file with a shape is parsed
  # by that shape, and `cut` does not know about line one.
  status=$(head -1 "$scratch/$id.result" 2>/dev/null | cut -f1 || true)
  out=$(head -1 "$scratch/$id.result" 2>/dev/null | cut -f2- || true)
  [ -n "$status" ] || status=ERROR
  if [ "$status" = ROT ]; then
    printf '   ROT      %s: patch no longer applies\n' "$id"
    failed=$((failed + 1))
    continue
  fi
  if [ "$status" != OK ] || [ -z "$out" ]; then
    printf '   ERROR    %s: the probe produced no measurement.\n' "$id"
    printf '            A power lane that cannot measure is a power lane reporting nothing -- and\n'
    printf '            "no measurement" covers a build failure, a panic, a timeout, and a tree\n'
    printf '            copied while it was being edited. The tail says which:\n'
    cat "$scratch/$id.result.diag" 2>/dev/null || printf '            > (no output captured)\n'
    failed=$((failed + 1))
    continue
  fi
  got=$(echo "$out" | sed -n 's/.*detected=\([0-9]*\).*/\1/p')
  first=$(echo "$out" | sed -n 's/.*first=\(-\{0,1\}[0-9]*\).*/\1/p')
  covered=$((covered + 1))

  if [ "$MEASURE" = yes ]; then
    sweepn=$(echo "$out" | sed -n 's/.*sweepfail=\([0-9]*\).*/\1/p')
    printf '   measure  %-44s %s of %s (%s) first=%s sweepfail=%s\n' \
      "$id" "$got" "$seeds" "$cfg" "$first" "${sweepn:-0}"
    if [ "${sweepn:-0}" != 0 ]; then
      tail -n +2 "$scratch/$id.result" 2>/dev/null | sed 's/^/              sweep: /'
    fi
    continue
  fi
  # # A class whose detector is an AGGREGATE assertion has no per-seed rate
  #
  # `M62` makes every lock look expired. Nothing about that is visible on a
  # single seed -- a transaction aborted by a competitor is a legal outcome --
  # and it is caught by the exit criteria over the whole sweep. A rate floor
  # cannot express that, and the version of this lane that only had rates
  # reported `0 of 300` and read it as "undetectable".
  #
  # So a patch may declare `power-detector: sweep`, and what is required is a
  # sweep failure the UNMUTATED tree does not have at the same seed count and
  # config. A difference, not a presence.
  if [ "$detector" = sweep ]; then
    baseline_sweep "$seeds" "$cfg" | sort > "$scratch/$id.base"
    tail -n +2 "$scratch/$id.result" 2>/dev/null | sort > "$scratch/$id.swept"
    newfail=$(comm -13 "$scratch/$id.base" "$scratch/$id.swept" | head -1)
    if [ -z "$newfail" ]; then
      printf '   BLIND    %-44s no exit criterion failed that the unmutated tree does not also fail\n' "$id"
      printf '            (%s seeds, %s). The class declares a SWEEP detector and the sweep did not\n' "$seeds" "$cfg"
      printf '            notice, so either the detector is not the one named or the class is out of\n'
      printf '            reach of this seed count.\n'
      failed=$((failed + 1))
    else
      printf '   sweep    %-44s noticed by an exit criterion the baseline passes (%s, %s):\n' "$id" "$seeds" "$cfg"
      printf '              %s\n' "$newfail"
    fi
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
printf '   %d classes floored and ceilinged, %d exempt by a named instrument or a named detector, %d failures\n\n' "$covered" "$optout" "$failed"

if [ "$covered" -eq 0 ]; then
  printf '  No class carries a floor. An empty power lane proves nothing.\n\n'
  exit 2
fi
[ "$failed" -eq 0 ] || exit 1
