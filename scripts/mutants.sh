#!/usr/bin/env sh
# The mutant suite: break the system under test on purpose, one defect at a time,
# and require the checker that claims to cover it to notice.
#
# This is CLAUDE.md Amendment A2's standing obligation, and the protocol half of
# the pair whose other half is scripts/blind-analyzer.sh. The blind lane
# mutation-tests the determinism analyzer; this one mutation-tests the oracles,
# the loop's wiring, and the toy protocol.
#
# It answers a different question from the assertion-invocation lane, and the two
# are deliberately separate instruments:
#
#   assertion lane   was the check called?
#   mutant suite     does the check, having been called, actually catch anything?
#
# A repository that only asks the first ends up with checks that run and prove
# nothing. A repository that only asks the second ships checks that would catch a
# defect if anything ever invoked them -- which this repository did, five times.
#
# Mutants are patches applied to a scratch copy, never committed source (DR-27).
# sim/toy and sim/ are in the determinism pass's core scope, so several of these
# mutants cannot exist as committed Go files: the tree would not build, which is
# exactly what those mutants mean.
#
# Two pieces of machinery make a green result mean something, both carried over
# from the blind lane because both were earned there:
#
#   Baseline gate.  The unpatched tree must pass every covering test before any
#   patch is applied. A red baseline makes every subsequent failure ambiguous,
#   so the lane reports INVALID and refuses to report kills at all.
#
#   ALIVE canary.  One patch is deliberately declared against a test that does
#   not cover it, and must survive. If the canary dies, the lane cannot tell
#   "this defect is caught" from "this test fails regardless", and its kills are
#   worth nothing.
#
# A kill counts only against the test named in the patch header. A patch that no
# longer applies is reported as ROT and fails the lane: patch rot is the price of
# this design and detecting it is part of the job.
#
# Kill time is recorded per mutant, per Amendment A2: a regression in kill-time
# is a harness regression even while every mutant is still killed.
#
# usage: mutants.sh [patch-dir]
set -eu

GO=${GO:-go}
PATCHDIR=${1:-sim/mutants}
ROOT=$(pwd)

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

copy_tree() {
  mkdir -p "$1"
  tar cf - --exclude=./.git --exclude=./.github . | (cd "$1" && tar xf -)
}

now_ms() { date +%s000 2>/dev/null || echo 0; }

printf '\n  mutant suite: the protocol broken one defect at a time\n'
printf '  ----------------------------------------------------------\n'

# ---------------------------------------------------------------- baseline
copy_tree "$scratch/baseline"
baseline_pkgs=$(sed -n 's/^# package: *//p' "$PATCHDIR"/*.patch | sort -u | tr '\n' ' ')
if ! (cd "$scratch/baseline" && $GO test -count=1 $baseline_pkgs >"$scratch/baseline.log" 2>&1); then
  printf '   INVALID  the unpatched tree does not pass the packages these mutants target.\n\n'
  sed 's/^/     /' "$scratch/baseline.log" | head -30
  printf '\n  Every failure below would be unattributable, so no kills are reported.\n'
  printf '  A lane has to be able to fail honestly before its green means anything.\n\n'
  exit 2
fi
printf '   baseline ok: unpatched tree passes every targeted package\n'

killed=0
canaries=0
mismatched=0
rotted=0

for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  test_name=$(sed -n 's/^# covering-test: *//p' "$patch")
  pkg=$(sed -n 's/^# package: *//p' "$patch")
  expect=$(sed -n 's/^# expect: *//p' "$patch")
  mutates=$(sed -n 's/^# mutates: *//p' "$patch")

  if [ -z "$id" ] || [ -z "$test_name" ] || [ -z "$pkg" ] || [ -z "$expect" ] || [ -z "$mutates" ]; then
    printf '   ERROR    %s: header needs id, covering-test, package, expect and mutates\n' "$patch"
    exit 2
  fi
  case $expect in
    killed|alive) ;;
    *) printf '   ERROR    %s: "# expect:" must be killed or alive, got %s\n' "$patch" "$expect"; exit 2 ;;
  esac

  case $patch in
    /*) abs=$patch ;;
     *) abs=$ROOT/$patch ;;
  esac

  work="$scratch/$id"
  copy_tree "$work"

  if ! (cd "$work" && patch -p1 --silent --forward < "$abs" 2>/dev/null); then
    printf '   ROT      %s: patch no longer applies; the code moved and the mutation did not\n' "$id"
    rotted=$((rotted + 1))
    continue
  fi

  # The covering test must EXIST and RUN. `go test -run` exits 0 when the pattern
  # matches nothing, so a deleted or renamed test reports the mutant as ALIVE --
  # indistinguishable from "the defect is not caught", and pointing at the
  # checker instead of at the missing test. That happened: M19's covering test
  # was removed by an unrelated edit and the lane blamed the oracle.
  #
  # So the run is verified before its result is read. A mutant whose covering
  # test did not execute is an ERROR, not a verdict.
  start=$(now_ms)
  rc=0
  # `|| rc=$?` rather than a bare call: set -e would abort the whole lane on the
  # first killed mutant, which is the expected outcome for most of them.
  (cd "$work" && $GO test -count=1 -v -run "^${test_name}\$" "$pkg" >"$scratch/$id.log" 2>&1) || rc=$?
  end=$(now_ms)
  if ! grep -q "^=== RUN[[:space:]]*${test_name}\$" "$scratch/$id.log"; then
    printf '   ERROR    %s: its covering test %s never ran.\n' "$id" "$test_name"
    printf '            A test that does not exist cannot fail, so the lane would have reported\n'
    printf '            this mutant ALIVE and blamed the checker for a missing test.\n'
    sed 's/^/     /' "$scratch/$id.log" | head -10
    exit 2
  fi
  if [ $rc -eq 0 ]; then
    got=alive
  else
    got=killed
  fi
  elapsed=$(( (end - start) / 1000 ))

  if [ "$got" != "$expect" ]; then
    mismatched=$((mismatched + 1))
    if [ "$expect" = killed ]; then
      printf '   ALIVE    %s survived %s: %s is not caught\n' "$id" "$test_name" "$mutates"
    else
      printf '   DIED     %s: the canary was killed by %s, which does not cover it\n' "$id" "$test_name"
    fi
    continue
  fi

  if [ "$expect" = alive ]; then
    canaries=$((canaries + 1))
    printf '   canary   %s survived %s, as it must\n' "$id" "$test_name"
  else
    killed=$((killed + 1))
    printf '   killed   %-24s by %-42s %3ss\n' "$id" "$test_name" "$elapsed"
  fi
done

printf '  ----------------------------------------------------------\n'
printf '   %d killed, %d canary alive, %d mismatched, %d rotted\n\n' \
  "$killed" "$canaries" "$mismatched" "$rotted"

if [ "$killed" -eq 0 ]; then
  printf '  No mutant was killed. An empty mutation lane proves nothing.\n\n'
  exit 2
fi

if [ "$canaries" -eq 0 ]; then
  printf '  No surviving canary. Without one, a lane that reports only kills cannot\n'
  printf '  distinguish a defect that is caught from a test that fails regardless.\n\n'
  exit 2
fi

if [ "$mismatched" -ne 0 ] || [ "$rotted" -ne 0 ]; then
  printf '  A patch did not behave as its header declares, or no longer applies.\n'
  printf '  Either the checker stopped covering the defect, or the mutation stopped\n'
  printf '  expressing it. Both are harness regressions.\n\n'
  exit 1
fi
