#!/usr/bin/env sh
# A lane that verifies an ABSENCE must run in a state where the thing could
# actually be present.
#
# This is HARNESS-002's general form, and it is stated here rather than only in
# BUGS.md because this script is the place someone will be standing when they
# need it. The instance: `make cpp-ci` claims no lane touches the network. It
# reused a build directory, CMake's FetchContent populates at configure time and
# SKIPS the download when _deps/ is already there, so any earlier networked
# build left behind exactly the artifact that makes a network dependency
# invisible. The lane was green. Mutant BM21 survived it. The mutant was right
# and the lane was wrong.
#
# The claim had been resting on a cache rather than on the absence of a fetch,
# and no amount of isolation would have found that -- the isolation worked
# perfectly and had nothing to block.
#
# So the cold cache is now part of what cpp-ci MEANS, and it is asserted at both
# ends rather than assumed at either:
#
#   before   the build root must not exist, and this script does NOT remove it.
#            That distinction is the whole of it. The first version of this
#            check ran after the lane's own `rm -rf`, so it asserted the absence
#            of something the lane had just deleted -- green forever, including
#            in the exact state HARNESS-002 occurred in. It was decoration, and
#            inducing it is what showed that: the gate did not fire.
#
#            So the removal moved to the END of a SUCCESSFUL run. A clean run
#            leaves nothing behind and the next one starts cold; a FAILED run
#            leaves its build tree for whoever has to debug it, and the next
#            `make cpp-ci` refuses until they clear it. Refusing is correct:
#            the lane cannot make its claim from a tree it did not build.
#
#   after    no _deps/ may have appeared. The absence checked DIRECTLY,
#            downstream of the isolation rather than in place of it: if the
#            isolation ever stops isolating, this still says so. Then, and only
#            on success, the build root is removed.
#
# Track A hit the cousin of this twice. The general form is worth more than the
# instance: an absence verified in a state that could not have contained the
# thing is not a verification, it is a tautology wearing a lane's clothes.
#
# usage: cpp-cold-cache.sh <build-root> before|after
set -eu

root=${1:?build root required}
phase=${2:?before|after required}

case $phase in
before)
  if [ -e "$root" ]; then
    printf '\n  ==========================================================\n'
    printf '   cpp-ci: BUILD ROOT IS NOT COLD\n'
    printf '  ----------------------------------------------------------\n'
    printf '   %s already exists.\n' "$root"
    printf '   A warm build directory can carry a populated FetchContent\n'
    printf '   cache, and this lane would then pass under isolation for the\n'
    printf '   one reason that has nothing to do with its claim. HARNESS-002.\n'
    printf '\n'
    printf '   This script will NOT delete it for you, on purpose: a check\n'
    printf '   that removes the thing it is checking for is a check that\n'
    printf '   cannot fail. A clean run removes its own tree at the end, so\n'
    printf '   this directory is here because the last run FAILED and left it\n'
    printf '   for you to look at.\n'
    printf '\n'
    printf '     rm -rf %s\n' "$root"
    printf '  ==========================================================\n\n'
    exit 2
  fi
  printf '   cold cache : %s does not exist; this run builds everything\n' "$root"
  ;;
after)
  if [ ! -d "$root" ]; then
    printf '\n  FAIL  %s does not exist after the lane set; nothing was built.\n\n' "$root"
    exit 2
  fi
  deps=$(find "$root" -maxdepth 3 -type d -name '_deps' 2>/dev/null | head -5 || true)
  if [ -n "$deps" ]; then
    printf '\n  ==========================================================\n'
    printf '   cpp-ci: SOMETHING WAS FETCHED\n'
    printf '  ----------------------------------------------------------\n'
    printf '   A FetchContent _deps tree appeared during the run:\n'
    printf '%s\n' "$deps" | sed 's/^/     /'
    printf '   The lane claims no lane touches the network. Either it does,\n'
    printf '   or the isolation stopped isolating -- and this check exists\n'
    printf '   downstream of the isolation so that either one is visible.\n'
    printf '  ==========================================================\n\n'
    exit 1
  fi
  printf '   cold cache : no _deps tree appeared during the run\n'
  rm -rf "$root"
  printf '   cold cache : build root removed, so the next run is cold too\n\n'
  ;;
*)
  echo "usage: cpp-cold-cache.sh <build-root> before|after" >&2; exit 2 ;;
esac
