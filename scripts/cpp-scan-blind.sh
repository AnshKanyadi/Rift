#!/usr/bin/env sh
# Mutation-test the scope scan itself.
#
# The scope scan is what every sentence in engine-cpp/CLAIMS.txt rests on, and a
# scan that has quietly stopped checking something looks EXACTLY like a scan with
# nothing to report. So each patch in engine-cpp/scan-blind/ blinds one rule, and
# the fixture that rule is declared against must stop being rejected.
#
# This is the C++ half of DR-27 and of Amendment A2's pairing: one instrument
# checking the protocol, one checking the instrument. `make blind` is the Go
# half, and this borrows both of its hard-won guards:
#
#   Baseline gate.  The unpatched tree must pass its own fixture check first. A
#   red baseline makes every subsequent failure unattributable, so the lane
#   reports INVALID and refuses to report kills at all.
#
#   ALIVE canary.  One patch is deliberately declared against a fixture that
#   does not cover it, and must survive. If the canary dies, the lane cannot
#   tell "this rule is under test" from "the check fails regardless", and its
#   kills are worth nothing. Every run therefore proves both directions.
#
# Patches are applied to a scratch copy, never to the working tree. A patch that
# no longer applies fails the lane: patch rot is the price of this design and
# detecting it is part of the job.
#
# usage: cpp-scan-blind.sh [patch-dir]
set -eu

PATCHDIR=${1:-engine-cpp/scan-blind}
ROOT=$(pwd)

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

copy_tree() {
  mkdir -p "$1"
  # Copy everything, then delete the ROOT paths we do not want -- never
  # tar --exclude, whose patterns match any suffix of a stored path and would
  # also drop third_party/googletest/.github. HARNESS-001.
  tar cf - . | (cd "$1" && tar xf -)
  rm -rf "$1/.git" "$1/.github" "$1/engine-cpp/build"
  rm -rf "$1"/engine-cpp/build-*
}

printf '\n  scope scan, blinded one rule at a time\n'
printf '  ----------------------------------------------------------\n'

patches=$(ls "$PATCHDIR"/*.patch 2>/dev/null || true)
if [ -z "$patches" ]; then
  printf '   INVALID  no patches in %s. An empty mutation lane proves nothing.\n\n' "$PATCHDIR"
  exit 2
fi

copy_tree "$scratch/baseline"
if ! ( cd "$scratch/baseline" && ./scripts/cpp-scan.sh --fixtures ) >"$scratch/baseline.log" 2>&1; then
  printf '   INVALID  the unpatched scan does not pass its own fixture check.\n\n'
  sed 's/^/     /' "$scratch/baseline.log" | tail -20
  printf '\n  Every failure below would be unattributable, so no kills are reported.\n'
  printf '  Fix the baseline first: a lane has to be able to fail honestly before\n'
  printf '  its green means anything.\n\n'
  exit 2
fi
printf '   baseline ok: every rule fires on its own fixture\n'

killed=0
canaries=0
mismatched=0

for patch in $patches; do
  id=$(sed -n 's/^# id: *//p' "$patch" | head -1)
  blinds=$(sed -n 's/^# blinds: *//p' "$patch" | head -1)
  fixture=$(sed -n 's/^# covering-fixture: *//p' "$patch" | head -1)
  expect=$(sed -n 's/^# expect: *//p' "$patch" | head -1)

  if [ -z "$id" ] || [ -z "$blinds" ] || [ -z "$fixture" ] || [ -z "$expect" ]; then
    printf '   ERROR    %s: header needs id, blinds, covering-fixture and expect\n' "$patch"
    exit 2
  fi
  case $expect in killed|alive) ;; *)
    printf '   ERROR    %s: expect must be killed or alive, got %s\n' "$patch" "$expect"; exit 2 ;;
  esac

  case $patch in /*) abs=$patch ;; *) abs=$ROOT/$patch ;; esac
  work="$scratch/$id"
  copy_tree "$work"
  if ! ( cd "$work" && patch -p1 --silent --forward < "$abs" ); then
    printf '   ROT      %s: patch no longer applies; the scan moved and the blinding did not\n' "$id"
    mismatched=$((mismatched + 1))
    continue
  fi

  if ( cd "$work" && ./scripts/cpp-scan.sh --fixtures "$fixture" ) >/dev/null 2>&1; then
    got=alive
  else
    got=killed
  fi

  if [ "$got" != "$expect" ]; then
    mismatched=$((mismatched + 1))
    if [ "$expect" = killed ]; then
      printf '   ALIVE    %s: %s still rejected with %s blinded\n' "$id" "$(basename "$fixture")" "$blinds"
      printf '            The rule is no longer under test, whether or not it still works.\n'
    else
      printf '   DIED     %s: the canary was killed by a fixture that does not cover %s\n' "$id" "$blinds"
      printf '            Without a surviving canary this lane cannot tell a checked rule\n'
      printf '            from a check that fails regardless, and its kills mean nothing.\n'
    fi
    continue
  fi

  if [ "$expect" = alive ]; then
    canaries=$((canaries + 1))
    printf '   canary   %s survived %s, as it must (blinds %s)\n' "$id" "$(basename "$fixture")" "$blinds"
  else
    killed=$((killed + 1))
    printf '   killed   %s by %s (blinds %s)\n' "$id" "$(basename "$fixture")" "$blinds"
  fi
done

printf '  ----------------------------------------------------------\n'
printf '   %d killed, %d canary alive, %d mismatched\n\n' "$killed" "$canaries" "$mismatched"

if [ "$killed" -eq 0 ]; then
  printf '  No blinding patch ran. An empty mutation lane proves nothing.\n\n'; exit 2
fi
if [ "$canaries" -eq 0 ]; then
  printf '  No surviving canary. Without one, a lane that reports only kills cannot\n'
  printf '  distinguish a rule under test from a check that fails regardless.\n\n'; exit 2
fi
if [ "$mismatched" -ne 0 ]; then
  printf '  A patch did not behave as its header declares.\n'
  printf '  Fix the rule, not the patch: the patch is the specification of what\n'
  printf '  that rule is supposed to catch.\n\n'; exit 1
fi
