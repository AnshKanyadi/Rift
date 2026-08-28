#!/bin/sh
# Every mutant patch still applies to the tree it mutates.
#
# WHY THIS EXISTS. At B5's close the full catalogue ran for the first time since
# B3 and reported 15 ROT -- patches that no longer apply. Fourteen of them had
# rotted during B3.5 through B4.2 and NOTHING NOTICED, because the catalogue is
# expensive and had only ever been run as `ONLY=` subsets, and a subset that does
# not name a rotted class is green.
#
# THE PAPERWORK WAS COMPLETE FOR EVERY ONE OF THEM. `cpp-scan` asserts that every
# mutant class has a FLOORS.txt row. FLOORS.txt asserts that every class has a
# standing measurement. Neither asserts THAT THE PATCH STILL APPLIES -- so a
# class could be fully documented, fully floored, counted in "155 classes", and
# impossible to run.
#
#   A CATALOGUE THAT IS ONLY EVER RUN IN SUBSETS ROTS IN THE PARTS NO SUBSET
#   NAMES, AND EVERY SUBSET STAYS GREEN WHILE IT DOES.
#
# This check costs SECONDS -- it applies nothing and builds nothing, it only asks
# `patch --dry-run` -- where the catalogue costs hours. That asymmetry is the
# whole argument for it existing separately: the expensive lane is the one that
# gets skipped, so the cheap half of what it proves should not be inside it.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
PATCHDIR=${1:-engine-cpp/mutants}

printf '\n  every mutant patch still applies\n'
printf '  ----------------------------------------------------------\n'

total=0
rot=0
for p in "$PATCHDIR"/*.patch; do
  total=$((total + 1))
  id=$(basename "$p" .patch)
  if patch -p1 --silent --forward --dry-run < "$p" >/dev/null 2>&1; then
    continue
  fi
  target=$(grep -m1 '^--- a/' "$p" | sed 's|--- a/||' | awk '{print $1}')
  printf '   ROT    %-46s %s\n' "$id" "$target"
  rot=$((rot + 1))
done

printf '  ----------------------------------------------------------\n'
printf '   patches       : %d\n' "$total"
printf '   rotted        : %d\n' "$rot"
if [ "$rot" -ne 0 ]; then
  printf '\n  A rotted patch is not a weaker mutant. It is NO mutant: the class is\n'
  printf '  documented, floored, and counted, and cannot be run. Re-aim it at the\n'
  printf '  code that moved, or delete it and say why -- never leave it counted.\n\n'
  exit 1
fi
printf '   ok  every class in the catalogue can actually be run\n\n'
