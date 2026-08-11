#!/usr/bin/env sh
# Mutation-test the determinism pass itself.
#
# The pass is what every determinism claim in this repo rests on, and a pass
# that has quietly stopped checking something looks exactly like a pass that has
# nothing to report. So each patch in tools/determinismcheck/blind/ blinds one
# rule, and the named test must fail. A mutation that survives means that rule
# is no longer under test, which is a harness regression whether or not the rule
# still works.
#
# This is the analyzer's half of CLAUDE.md Amendment A2, standing alongside the
# mutant suite that covers the protocol.
#
# Patches are applied to a scratch copy, never to the working tree. A patch that
# no longer applies fails the lane: patch rot is the price of this design and
# detecting it is part of the job.
#
# usage: blind-analyzer.sh [patch-dir]
set -eu

GO=${GO:-go}
PATCHDIR=${1:-tools/determinismcheck/blind}
ROOT=$(pwd)

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

survived=0
killed=0

printf '\n  determinism pass, blinded one rule at a time\n'
printf '  ----------------------------------------------------------\n'

for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  must=$(sed -n 's/^# must-fail: *//p' "$patch")
  blinds=$(sed -n 's/^# blinds: *//p' "$patch")

  if [ -z "$id" ] || [ -z "$must" ]; then
    printf '   ERROR  %s: patch header must carry "# id:" and "# must-fail:"\n' "$patch"
    exit 2
  fi

  # A fresh copy per patch: mutations never stack, and the working tree is
  # never touched even if this script is interrupted.
  work="$scratch/$id"
  mkdir -p "$work"
  tar cf - --exclude=./.git --exclude=./.github . | (cd "$work" && tar xf -)

  case $patch in
    /*) abs=$patch ;;
     *) abs=$ROOT/$patch ;;
  esac

  if ! (cd "$work" && patch -p1 --silent --forward < "$abs"); then
    printf '   ROT    %s: patch no longer applies; the code moved and the mutation did not\n' "$id"
    survived=$((survived + 1))
    continue
  fi

  if (cd "$work" && $GO test -count=1 -run "$must" ./tools/determinismcheck/ >/dev/null 2>&1); then
    printf '   ALIVE  %s: %s still passed with %s blinded\n' "$id" "$must" "$blinds"
    survived=$((survived + 1))
  else
    printf '   killed %s by %s (blinds %s)\n' "$id" "$must" "$blinds"
    killed=$((killed + 1))
  fi
done

printf '  ----------------------------------------------------------\n'
printf '   %d killed, %d survived\n\n' "$killed" "$survived"

if [ "$killed" -eq 0 ]; then
  printf '  No patches ran. An empty mutation lane proves nothing.\n\n'
  exit 2
fi

if [ "$survived" -ne 0 ]; then
  printf '  A blinded rule went unnoticed by its own tests.\n'
  printf '  Fix the test, not the patch: the patch is the specification of what\n'
  printf '  that rule is supposed to catch.\n\n'
  exit 1
fi
