#!/usr/bin/env sh
# Mutation-test the determinism pass itself.
#
# The pass is what every determinism claim in this repo rests on, and a pass
# that has quietly stopped checking something looks exactly like a pass with
# nothing to report. So each patch in tools/determinismcheck/blind/ blinds one
# rule, and the test declared in its header must fail. A mutation that survives
# means that rule is no longer under test, which is a harness regression whether
# or not the rule still works.
#
# This is the analyzer's half of CLAUDE.md Amendment A2, standing alongside the
# mutant suite that covers the protocol.
#
# Two pieces of machinery make a green result mean something, both of them
# ruled in after a lane that reported seven kills while one of the tests doing
# the killing was failing for its own reasons:
#
#   Baseline gate.  The unpatched tree must pass its whole suite before any
#   patch is applied. A red baseline makes every subsequent test failure
#   ambiguous, so the lane reports INVALID and refuses to report kills at all.
#
#   ALIVE canary.  One patch is deliberately declared against a test that does
#   not cover it, and must survive. If the canary dies, the lane cannot tell
#   "this rule is checked" from "this test fails regardless", and its kills are
#   worth nothing. Every run therefore proves both directions.
#
# A kill counts only against the test named in the patch header. Patches are
# applied to a scratch copy, never to the working tree, and a patch that no
# longer applies fails the lane: patch rot is the price of this design and
# detecting it is part of the job.
#
# usage: blind-analyzer.sh [patch-dir]
set -eu

GO=${GO:-go}
PKG=./tools/determinismcheck/
PATCHDIR=${1:-tools/determinismcheck/blind}
ROOT=$(pwd)

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

# copy_tree <dest> -- the working tree minus VCS and CI metadata.
copy_tree() {
  mkdir -p "$1"
  tar cf - --exclude=./.git --exclude=./.github . | (cd "$1" && tar xf -)
}

printf '\n  determinism pass, blinded one rule at a time\n'
printf '  ----------------------------------------------------------\n'

# ---------------------------------------------------------------- baseline
copy_tree "$scratch/baseline"
if ! (cd "$scratch/baseline" && $GO test -count=1 "$PKG" >"$scratch/baseline.log" 2>&1); then
  printf '   INVALID  the unpatched tree does not pass its own suite.\n\n'
  sed 's/^/     /' "$scratch/baseline.log" | head -30
  printf '\n  Every failure below would be unattributable, so no kills are reported.\n'
  printf '  Fix the baseline first: a lane has to be able to fail honestly before\n'
  printf '  its green means anything.\n\n'
  exit 2
fi
printf '   baseline ok: unpatched tree passes its whole suite\n'

# ---------------------------------------------------------------- mutations
mismatched=0
killed=0
canaries=0

for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  test_name=$(sed -n 's/^# covering-test: *//p' "$patch")
  expect=$(sed -n 's/^# expect: *//p' "$patch")
  blinds=$(sed -n 's/^# blinds: *//p' "$patch")

  if [ -z "$id" ] || [ -z "$test_name" ] || [ -z "$expect" ]; then
    printf '   ERROR    %s: header needs "# id:", "# covering-test:" and "# expect:"\n' "$patch"
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

  # A fresh copy per patch: mutations never stack, and the working tree is
  # never touched even if this script is interrupted.
  work="$scratch/$id"
  copy_tree "$work"

  if ! (cd "$work" && patch -p1 --silent --forward < "$abs"); then
    printf '   ROT      %s: patch no longer applies; the code moved and the mutation did not\n' "$id"
    mismatched=$((mismatched + 1))
    continue
  fi

  if (cd "$work" && $GO test -count=1 -run "$test_name" "$PKG" >/dev/null 2>&1); then
    got=alive
  else
    got=killed
  fi

  if [ "$got" != "$expect" ]; then
    mismatched=$((mismatched + 1))
    if [ "$expect" = killed ]; then
      printf '   ALIVE    %s: %s still passed with %s blinded\n' "$id" "$test_name" "$blinds"
    else
      printf '   DIED     %s: the canary was killed by %s, which does not cover %s\n' "$id" "$test_name" "$blinds"
    fi
    continue
  fi

  if [ "$expect" = alive ]; then
    canaries=$((canaries + 1))
    printf '   canary   %s survived %s, as it must (%s)\n' "$id" "$test_name" "$blinds"
  else
    killed=$((killed + 1))
    printf '   killed   %s by %s (blinds %s)\n' "$id" "$test_name" "$blinds"
  fi
done

printf '  ----------------------------------------------------------\n'
printf '   %d killed, %d canary alive, %d mismatched\n\n' "$killed" "$canaries" "$mismatched"

if [ "$killed" -eq 0 ]; then
  printf '  No blinding patch ran. An empty mutation lane proves nothing.\n\n'
  exit 2
fi

if [ "$canaries" -eq 0 ]; then
  printf '  No surviving canary. Without one, a lane that reports only kills cannot\n'
  printf '  distinguish a rule under test from a test that fails regardless.\n\n'
  exit 2
fi

if [ "$mismatched" -ne 0 ]; then
  printf '  A patch did not behave as its header declares.\n'
  printf '  Fix the test, not the patch: the patch is the specification of what\n'
  printf '  that rule is supposed to catch.\n\n'
  exit 1
fi
