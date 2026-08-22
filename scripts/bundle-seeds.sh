#!/usr/bin/env sh
# Every bundle's seed matches what BUGS.md says it carries.
#
# # Why this exists
#
# BUGS.md's "Reproduce (seed)" line is the instruction a stranger follows, and
# the bundle is what they get. Nothing compared the two. Four entries had drifted
# by A6 -- BUG-004 said 0 and carried 2, BUG-005 said 40 and carried 3, BUG-007
# said 12 and carried 1, BUG-015 said 215 and carried 16 -- because a bundle is
# re-recorded whenever the workload moves under it, and the prose beside it is
# not.
#
# It is the same shape as everything else this phase found: a claim with nothing
# checking it. The claim here is small, and it is the one on the résumé line --
# every bug ever found replays from a single seed -- so it is the sentence a
# reader would test first.
set -eu

SEEDS=${1:-seeds}
BUGS=${2:-BUGS.md}

printf '\n  bundle seeds: BUGS.md against the bundles\n'
printf '  ----------------------------------------------------------------\n'
bad=0
checked=0
for d in "$SEEDS"/BUG-*/; do
  [ -f "$d/meta.json" ] || continue
  name=$(basename "$d")
  seed=$(python3 -c "import json;print(json.load(open('$d/meta.json'))['seed'])")
  checked=$((checked + 1))
  # The entry's block runs from its heading to the next one.
  block=$(awk -v n="### $name " 'index($0,n)==1{f=1;next} f&&/^### BUG-/{exit} f' "$BUGS")
  line=$(printf '%s' "$block" | grep 'Reproduce (seed)' | head -1)
  # # The seed as a standalone number, in whatever wording the entry uses
  #
  # The first version required the exact phrase "carries seed **N**" and called
  # seven correct entries drifted, because they say "seed **0**, range 3, applied
  # index 12" and other perfectly clear things. A checker that enforces a
  # sentence shape rather than a fact reports style as error, and the report is
  # then worth nothing.
  #
  # The honest scope: this is a DRIFT check, not a proof. It catches a bundle
  # re-recorded without its prose being updated, which is the failure that
  # actually happened four times. A line that happens to contain the seed as some
  # other number -- an index, a term -- would fool it, and that is an acceptable
  # miss for a check this cheap.
  if printf '%s' "$line" | grep -qE "(^|[^0-9])$seed([^0-9]|\$)"; then
    printf '   ok       %-12s seed %s\n' "$name" "$seed"
  else
    printf '   DRIFTED  %-12s the bundle carries seed %s; the entry says: %s\n' "$name" "$seed" "$line"
    bad=$((bad + 1))
  fi
done
printf '  ----------------------------------------------------------------\n'
printf '   %d entries checked, %d drifted\n\n' "$checked" "$bad"
[ "$checked" -gt 0 ] || { printf '  No bundles. This lane proved nothing.\n\n'; exit 2; }
[ "$bad" -eq 0 ] || exit 1
