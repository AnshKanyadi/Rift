#!/usr/bin/env sh
# The corpus REPRODUCTION lane: every bundle still exercises the defect it exists
# to carry.
#
# # Why this exists, and what `make corpus` cannot see
#
# `make corpus` replays every bundle on the UNMUTATED tree and compares the trace
# and the verdict. That catches a bundle whose schedule has drifted. It cannot
# catch the thing the corpus actually promises.
#
# A fixed bug's bundle records no violation -- the schedule replays clean, and the
# reproduction is two steps: apply the mutant, replay the bundle. So a bundle can
# keep matching its recorded trace perfectly while the mutant it names has stopped
# producing anything on that schedule. The claim "every bug ever found replays
# from a single seed" is then false, and every instrument in the repository says
# green.
#
# It happened. At A5, BUG-002's schedule stopped exercising M14 entirely: the
# trace matched WITH the mutant applied, which means the mutation changed
# nothing. The bundle was re-recorded at a seed that does reproduce, and this lane
# is what stops the next one going unnoticed.
#
# # What counts as reproducing
#
# The mutated replay must differ from the recording in some observable way: a
# violation, a panic, an error, or a diverged trace. A mutated replay that is
# byte-identical to the unmutated recording is a mutation that did nothing.
#
# Bundles with no mutant are toy bundles and harness-defect bundles: the toy's
# flaw is a scenario flag carried in the plan, and a harness defect has no patch
# in sim/mutants. Both are skipped, and both are counted, so an empty lane cannot
# look like a passing one.
#
# usage: corpus-reproduces.sh [seeds-dir]
set -eu

GO=${GO:-go}
SEEDS=${1:-seeds}
ROOT=$(pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

printf '\n  corpus reproduction: every bundle still exercises its own defect\n'
printf '  ----------------------------------------------------------------\n'

failed=0
checked=0
skipped=0

for d in "$SEEDS"/*/; do
  [ -f "$d/meta.json" ] || continue
  name=$(basename "$d")
  mutant=$(python3 -c "import json,sys;print(json.load(open('$d/meta.json')).get('mutant',''))")
  if [ -z "$mutant" ]; then
    skipped=$((skipped + 1))
    printf '   skip     %-12s carries no mutant: its flaw is in the plan or it is a harness defect\n' "$name"
    continue
  fi

  work="$scratch/$name"
  mkdir -p "$work"
  (cd "$ROOT" && tar cf - --exclude=./.git --exclude=./.github .) | (cd "$work" && tar xf -)

  if ! (cd "$work" && patch -p1 --silent --forward < "$mutant" 2>/dev/null); then
    printf '   ROT      %-12s %s no longer applies\n' "$name" "$mutant"
    failed=$((failed + 1))
    continue
  fi
  if ! (cd "$work" && $GO build ./... 2>/dev/null); then
    printf '   ROT      %-12s the tree does not build with %s\n' "$name" "$mutant"
    failed=$((failed + 1))
    continue
  fi

  checked=$((checked + 1))
  out=$(cd "$work" && $GO run ./cmd/simctl replay --bundle "$d" 2>&1 || true)
  if printf '%s' "$out" | grep -qE 'DIVERGED|VIOLATION|violation reproduced|panic|simctl:'; then
    printf '   ok       %-12s reproduces under %s\n' "$name" "$(basename "$mutant")"
  else
    printf '   STALE    %-12s replays IDENTICALLY with %s applied.\n' "$name" "$(basename "$mutant")"
    printf '            The mutation changed nothing on this schedule, so the bundle no longer\n'
    printf '            carries its finding. Re-record it at a seed that does reproduce -- the\n'
    printf '            power lane will name one -- in the same commit that noticed.\n'
    failed=$((failed + 1))
  fi
done

printf '  ----------------------------------------------------------------\n'
printf '   %d bundles checked, %d skipped, %d failures\n\n' "$checked" "$skipped" "$failed"

if [ "$checked" -eq 0 ]; then
  printf '  No bundle carries a mutant. This lane proved nothing.\n\n'
  exit 2
fi
[ "$failed" -eq 0 ] || exit 1
