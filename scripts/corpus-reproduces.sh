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
# **The mutated replay must produce the FINDING**: a violation, an inconclusive,
# a panic, or a refusal the run could not continue past. Not merely a diverged
# trace.
#
# All four are findings. BUG-015's was a REFUSAL -- `ApplyConfEntry` declining an
# illegal transition -- and no oracle fired at all; BUG-001's is an inconclusive;
# M40's is a panic. A criterion that only accepted violations would retire three
# real entries for not being the shape it expected.
#
# That criterion was loosened once and tightened back. Ansh's ruling at A6: "A
# diverging trace proves the bundle is sensitive to something; only the finding
# returning proves it is sensitive to the thing it was recorded for. That is
# exactly what the A5 fix established, and accepting trace divergence would
# reopen the thirteenth instance under a weaker test."
#
# And the finding must be one the MUTANT produced: a finding present in the
# RECORDING as well is not evidence of anything, because the mutation did not
# cause it. BUG-015's bundle came out green on exactly that -- an inconclusive
# its quiet schedule produces with or without the mutant, on a replay whose trace
# matched byte for byte.
#
# A divergence with no new finding is reported as WEAK rather than as ok, and it
# fails the lane. The remedy is to re-record the bundle at a seed where the
# finding returns; if no seed produces one, the bundle is RETIRED with the reason
# written down, never kept at a lower standard.
#
# An inconclusive counts, and has to: Amendment A4 makes it a first-class
# outcome, and BUG-001's entire finding is a green verdict over a history of
# unknowns, which the fix turned into an inconclusive rather than a violation.
#
# Bundles with no mutant are toy bundles and harness-defect bundles: the toy's
# flaw is a scenario flag carried in the plan, and a harness defect has no patch
# in sim/mutants. Both are skipped, and both are counted, so an empty lane cannot
# look like a passing one.
#
# usage: corpus-reproduces.sh [seeds-dir]
set -eu

GO=${GO:-go}
GOTAGS=${GOTAGS:-}
SEEDS=${1:-seeds}
ROOT=$(pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

printf '\n  corpus reproduction: every bundle still exercises its own defect\n'
printf '  ----------------------------------------------------------------\n'

failed=0
checked=0
skipped=0
notbundle=0
dirs=0

# # EVERY DROP PATH IS COUNTED, and one of them was not
#
# This loop leaves early in four places. Three of them incremented a counter and
# printed a line. The fourth -- `[ -f "$d/meta.json" ] || continue` -- did
# neither, so a directory without a meta.json was dropped in complete silence:
# not checked, not skipped, not printed, not in any total.
#
# It cost nothing until the A7/B5 merge put `seeds/differential/` on that path.
# Then the lane printed **"20 bundles checked, 4 skipped"** against a seeds/ that
# holds **25** directories, and the two numbers still summed to 24 because they
# had summed to the population back when 24 WAS the population.
#
# > **A COUNT TAKEN WHEN IT HAPPENED TO EQUAL THE POPULATION READS AS A
# > POPULATION FOREVER AFTER.**
#
# That is the found-by table's shape in a lane: a summary that does not add up to
# its own population, in a project whose whole argument is that verification must
# not be vacuous. The remedy is not to check the directory here -- it belongs to
# another owner, and cmd/simctl/corpus_test.go's registry says which -- it is to
# COUNT it, name it, and make the totals reconcile or fail.

for d in "$SEEDS"/*/; do
  name=$(basename "$d")
  dirs=$((dirs + 1))
  if [ ! -f "$d/meta.json" ]; then
    notbundle=$((notbundle + 1))
    printf '   other    %-12s not a bundle (no meta.json). Registered in corpus_test.go and
' "$name"
    printf '                         checked by its owner; counted here so the totals reconcile.
'
    continue
  fi
  # # A bundle may name a SET, because a defect need not be atomic
  #
  # The arrangement is *the bundle carries the schedule, the mutant carries the
  # defect*, and nothing in it requires one patch. BUG-021 needs both halves of
  # its fix removed -- the node tag and the minted restart timestamp -- because a
  # tree with either half still refuses the collision the other allows. Applying
  # one and calling the bundle stale would be blaming the schedule for the
  # arrangement.
  mutant=$(python3 -c "
import json
m=json.load(open('$d/meta.json'))
s=([m['mutant']] if m.get('mutant') else []) + list(m.get('mutants') or [])
print(' '.join(s))")
  if [ -z "$mutant" ]; then
    skipped=$((skipped + 1))
    printf '   skip     %-12s carries no mutant: its flaw is in the plan or it is a harness defect\n' "$name"
    continue
  fi

  work="$scratch/$name"
  mkdir -p "$work"
  (cd "$ROOT" && tar cf - --exclude=./.git --exclude=./.github .) | (cd "$work" && tar xf -)

  rot=""
  for one in $mutant; do
    (cd "$work" && patch -p1 --silent --forward < "$ROOT/$one" 2>/dev/null) || rot=$one
  done
  if [ -n "$rot" ]; then
    printf '   ROT      %-12s %s no longer applies\n' "$name" "$rot"
    failed=$((failed + 1))
    continue
  fi
  if ! (cd "$work" && $GO build ./... 2>/dev/null); then
    printf '   ROT      %-12s the tree does not build with %s\n' "$name" "$mutant"
    failed=$((failed + 1))
    continue
  fi
  label=$(for one in $mutant; do basename "$one"; done | paste -sd+ -)

  checked=$((checked + 1))
  # ENGINE, optional and empty by default, so this lane's behaviour is unchanged
  # unless a caller asks for a different one. I1 runs it with ENGINE=cgo to meet
  # its first exit criterion: every bundle must reproduce its FINDING against the
  # real engine, which is a stronger statement than a matching trace -- a bundle
  # whose defect is fixed matches and reports nothing.
  eng=""
  if [ -n "${ENGINE:-}" ] && [ "${ENGINE}" != "model" ]; then
    eng="--engine ${ENGINE} --engine-root ${work}/engine-root"
  fi
  out=$(cd "$work" && $GO run $GOTAGS ./cmd/simctl replay --bundle "$d" $eng 2>&1 || true)
  # # The finding has to be one the MUTANT produced
  #
  # Matching "a finding is present" is not enough, and passing a bundle on that
  # basis is how BUG-015 came out green on a replay whose trace MATCHED: its
  # schedule is quiet enough to be linearizability-inconclusive with or without
  # the mutant, so the finding was in the recording too and the mutation had
  # changed nothing at all.
  #
  # So what counts is a DIFFERENCE. `simctl replay` already says which it is --
  # "THE REPLAY PRODUCED A VIOLATION THE RECORDING DID NOT" against "violation
  # reproduced" -- and only the difference forms count here, plus a panic or a
  # harness error, neither of which a clean recording has.
  # "NOT REPRODUCED" is deliberately absent: it means the mutant made a finding
  # the recording had go AWAY, which is a difference but is not this bundle
  # reproducing its finding. For a bundle recorded on fixed code the recording is
  # clean, so the only honest pass is a finding appearing where there was none.
  if printf '%s' "$out" | grep -qE 'THE RECORDING DID NOT|WHERE THE RECORDING WAS NOT|panic|simctl:'; then
    printf '   ok       %-12s reproduces its FINDING under %s\n' "$name" "$label"
  elif printf '%s' "$out" | grep -qE 'DIVERGED'; then
    printf '   WEAK     %-12s diverges under %s but produces NO FINDING.\n' "$name" "$label"
    printf '            The bundle is sensitive to SOMETHING; only the finding returning proves\n'
    printf '            it is sensitive to the thing it was recorded for. Re-record it at a seed\n'
    printf '            where the finding returns, or retire it with the reason written down.\n'
    failed=$((failed + 1))
  else
    printf '   STALE    %-12s replays IDENTICALLY with %s applied.\n' "$name" "$label"
    printf '            The mutation changed nothing on this schedule, so the bundle no longer\n'
    printf '            carries its finding. Re-record it at a seed that does reproduce -- the\n'
    printf '            power lane will name one -- in the same commit that noticed.\n'
    failed=$((failed + 1))
  fi
done

printf '  ----------------------------------------------------------------\n'
printf '   %d directories in seeds/: %d checked, %d skipped, %d not bundles, %d failures\n' \
  "$dirs" "$checked" "$skipped" "$notbundle" "$failed"

# THE TOTALS MUST RECONCILE. Without this the counters are a description of what
# the loop felt like doing rather than an account of what it saw, and a fifth
# drop path added later would be as silent as the fourth one was.
accounted=$((checked + skipped + notbundle))
if [ "$accounted" -ne "$dirs" ]; then
  printf '\n  %d of %d directories are unaccounted for. A drop path in this loop does not\n' \
    "$((dirs - accounted))" "$dirs"
  printf '  count what it drops, which is how seeds/differential went past unseen.\n\n'
  exit 2
fi
printf '\n'

if [ "$checked" -eq 0 ]; then
  printf '  No bundle carries a mutant. This lane proved nothing.\n\n'
  exit 2
fi
[ "$failed" -eq 0 ] || exit 1
