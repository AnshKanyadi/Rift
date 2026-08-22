#!/usr/bin/env sh
# Every lane the Makefile's `ci` target names must actually run in the workflow.
#
# # Why this exists
#
# `make ci` listed `assertions` and `corpus-reproduces` for a whole phase and
# .github/workflows/ci.yml ran neither. Nobody noticed, because there is no
# remote: the workflow has never executed, so the only thing that would have
# compared the two lists was a person reading both.
#
# That is the same failure as a checker that is never invoked, one level out. A
# lane list is a claim about what runs on every change; this makes the claim
# checkable from inside the repository, which is the only place it can be checked
# until a remote exists.
#
# Aggregate targets are expanded, because `lint` running is `vet`, `fmt-check`,
# `determinism`, `tooling-only` and `hatches` running.
set -eu

MK=${1:-Makefile}
WF=${2:-.github/workflows/ci.yml}

expand() {
  case "$1" in
    lint)  echo "vet fmt-check determinism tooling-only hatches hygiene" ;;
    power) echo "power" ;;
    *)     echo "$1" ;;
  esac
}

lanes=$(sed -n 's/^ci:[[:space:]]*\(.*\)##.*/\1/p' "$MK")
[ -n "$lanes" ] || { echo "lane-coverage: no ci: target found in $MK"; exit 2; }

missing=0
checked=0
for lane in $lanes; do
  for real in $(expand "$lane"); do
    checked=$((checked + 1))
    if ! grep -qE "run: make $real([[:space:]]|\$)" "$WF"; then
      printf '  MISSING  make %s is in the Makefile ci target and runs nowhere in %s\n' "$real" "$WF"
      missing=$((missing + 1))
    fi
  done
done

printf '  lane coverage: %d lanes checked, %d missing\n' "$checked" "$missing"
[ "$missing" -eq 0 ] || exit 1
