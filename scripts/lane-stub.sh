#!/usr/bin/env sh
# Loudly announce a CI lane that exists but does not yet check anything.
#
# A lane that silently passes is worse than a missing lane: it manufactures
# confidence. This script makes every stub impossible to miss in a build log,
# and `make lanes` lists which lanes are still stubs.
#
# No lane may remain a stub past the phase named in its second argument. That
# is an exit criterion, not a preference.
#
# usage: lane-stub.sh <lane-name> <phase-it-lands-in> [note]
set -eu

lane=${1:?lane name required}
phase=${2:?phase required}
note=${3:-}

printf '\n'
printf '  ==========================================================\n'
printf '   LANE STUB -- THIS LANE CHECKS NOTHING\n'
printf '  ----------------------------------------------------------\n'
printf '   lane   : %s\n' "$lane"
printf '   status : not implemented; lands in %s\n' "$phase"
if [ -n "$note" ]; then
  printf '   note   : %s\n' "$note"
fi
printf '   A pass here is not evidence. Do not cite it.\n'
printf '  ==========================================================\n'
printf '\n'
