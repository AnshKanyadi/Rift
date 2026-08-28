#!/usr/bin/env sh
# Track B mutant catalogue: every patch must redden the lane it names.
#
# CLAUDE.md Amendment A2, and the C++ half of the pairing `make blind` already
# provides for the determinism pass. A mutant that survives its budget means the
# rig is too weak and the phase is not done, regardless of what the clean runs
# say.
#
# A SURVIVING MUTANT HAS THREE MEANINGS, NOT TWO: a checker that cannot see it,
# a defence that was never there, or code that cannot be reached. Only the third
# one's correct response is deletion. This lane reports the survival; it does
# not decide which of the three it is, and neither should whoever reads it
# without looking.
#
# Two pieces of machinery make a green result mean something, both inherited
# from blind-analyzer.sh, which earned them:
#
#   Baseline gate.  Every lane a patch is declared against must pass on the
#   UNPATCHED tree first. A red baseline makes every subsequent failure
#   unattributable, so the lane reports INVALID and refuses to report kills.
#
#   Direction control.  A patch may declare a `control-lane` that it must NOT
#   break. Without one, "the covering lane went red" cannot be distinguished
#   from "the patch broke the build", and those are very different results.
#   BM21 is the case that makes this concrete: the same patched tree must go
#   green with a network and red without one, and only the pair is evidence.
#
# Patches are applied to a scratch copy, never to the working tree. A patch that
# no longer applies fails the lane: patch rot is the price of this design and
# detecting it is part of the job.
#
# usage: cpp-mutants.sh [patch-dir] [id ...]
set -eu

PATCHDIR=${1:-engine-cpp/mutants}
[ $# -gt 0 ] && shift || true
ONLY="$*"
ROOT=$(pwd)
MAKE=${MAKE:-make}

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

# copy_tree <dest> -- the working tree minus VCS metadata and build output.
# Build directories are excluded because a CMakeCache.txt carries the absolute
# path it was configured for, and a stale one would fail every lane below for a
# reason that has nothing to do with any mutant.
copy_tree() {
  mkdir -p "$1"
  # Copy everything, then delete the ROOT paths we do not want.
  #
  # Not tar --exclude. Its patterns match any suffix of a stored path, so
  # `--exclude=./.github` silently also drops third_party/googletest/.github --
  # three files -- and the scratch tree stops being the tree under test. This
  # lane's own baseline gate caught exactly that on its first run, by way of the
  # vendored-tree hash check disagreeing inside the copy. Anchoring at the root
  # by deleting afterwards is unambiguous; a glob that "usually" anchors is not.
  tar cf - . | (cd "$1" && tar xf -)
  rm -rf "$1/.git" "$1/.github" "$1/engine-cpp/build"
  rm -rf "$1"/engine-cpp/build-*
}

# A LANE THAT CAN HANG IS A LANE THAT CAN SILENTLY STOP BEING A LANE.
#
# BM35 inverts the internal key comparator, and under it a merged-iterator loop
# that rests on "the cursor strictly advances" stops advancing. The test binary
# span at 99% of a core for ELEVEN AND A HALF HOURS while this lane waited, and
# the whole time it looked exactly like progress -- a running catalogue and a log
# that had simply not reached the next line yet. HARNESS-013.
#
# A hang is not a failure and it is not a pass. It is the lane declining to
# report, which is Amendment A4's "inconclusive" arriving in the mutation
# harness: never counted as a kill, never counted as a survival, and never
# silently waited on.
#
# There is no GNU `timeout` on macOS, so this is built by hand. The whole
# process TREE is killed, because `make` spawns cmake which spawns the binary
# and killing only the shell leaves the spinner running.
LANE_TIMEOUT=${LANE_TIMEOUT:-1200}

kill_tree() {  # kill_tree <pid>
  for c in $(pgrep -P "$1" 2>/dev/null); do kill_tree "$c"; done
  kill -9 "$1" 2>/dev/null || true
}

# Returns 0 on pass, 1 on fail, 124 on TIMEOUT -- the same code GNU timeout uses,
# so a reader who knows one knows the other.
run_lane() {  # run_lane <tree> <lane> <logfile>
  ( cd "$1" && $MAKE "$2" ) >"$3" 2>&1 &
  lane_pid=$!
  ( sleep "$LANE_TIMEOUT"; kill_tree "$lane_pid" ) >/dev/null 2>&1 &
  killer=$!
  # `|| rc=$?` AND NOT `; rc=$?`. Under `set -e` a failing `wait` exits the
  # script before the next command runs -- so the first version of this watchdog
  # killed the hung lane correctly and then died itself, printing nothing. A
  # watchdog that cannot report is the defect it was written to fix.
  rc=0
  wait "$lane_pid" || rc=$?
  kill "$killer" 2>/dev/null || true
  wait "$killer" 2>/dev/null || true
  # SIGKILL shows as 137, and the only thing that sends it here is the watchdog.
  if [ "$rc" -ge 128 ]; then return 124; fi
  return "$rc"
}

printf '\n  Track B mutants, one lane reddened at a time\n'
printf '  ----------------------------------------------------------\n'

patches=$(ls "$PATCHDIR"/*.patch 2>/dev/null || true)
if [ -z "$patches" ]; then
  printf '   INVALID  no patches in %s. An empty mutation lane proves nothing.\n\n' "$PATCHDIR"
  exit 2
fi

# ------------------------------------------------------------- baseline gate
lanes=$(for p in $patches; do
          sed -n 's/^# covering-lane: *//p' "$p"
          sed -n 's/^# control-lane: *//p' "$p"
        done | sort -u)

copy_tree "$scratch/baseline"
for lane in $lanes; do
  if ! run_lane "$scratch/baseline" "$lane" "$scratch/baseline-$lane.log"; then
    printf '   INVALID  the unpatched tree does not pass lane "%s".\n\n' "$lane"
    sed 's/^/     /' "$scratch/baseline-$lane.log" | tail -30
    printf '\n  Every failure below would be unattributable, so no kills are reported.\n'
    printf '  Fix the baseline first: a lane has to be able to fail honestly before\n'
    printf '  its green means anything.\n\n'
    exit 2
  fi
  printf '   baseline ok: unpatched tree passes %s\n' "$lane"
done

# ---------------------------------------------------------------- mutations
killed=0
survived=0
broken=0
controls=0
n=0

for patch in $patches; do
  id=$(sed -n 's/^# id: *//p' "$patch" | head -1)
  lane=$(sed -n 's/^# covering-lane: *//p' "$patch" | head -1)
  expect=$(sed -n 's/^# expect: *//p' "$patch" | head -1)
  blinds=$(sed -n 's/^# blinds: *//p' "$patch" | head -1)
  clane=$(sed -n 's/^# control-lane: *//p' "$patch" | head -1)
  cexpect=$(sed -n 's/^# expect-control: *//p' "$patch" | head -1)

  if [ -z "$id" ] || [ -z "$lane" ] || [ -z "$expect" ]; then
    printf '   ERROR    %s: header needs "# id:", "# covering-lane:" and "# expect:"\n' "$patch"
    exit 2
  fi
  case $expect in killed|alive) ;; *)
    printf '   ERROR    %s: "# expect:" must be killed or alive, got %s\n' "$patch" "$expect"; exit 2 ;;
  esac
  if [ -n "$ONLY" ]; then
    case " $ONLY " in *" $id "*) ;; *) continue ;; esac
  fi
  n=$((n + 1))

  case $patch in /*) abs=$patch ;; *) abs=$ROOT/$patch ;; esac

  # ONE TREE PER DIRECTION. The control run and the covering run are separate
  # experiments and sharing a tree lets the first contaminate the second: the
  # control run of BM21 populated a FetchContent cache that the covering run
  # then found already present. Independent trees, or the second result is
  # about the first run rather than about the mutant.
  work="$scratch/$id"
  cwork="$scratch/$id-control-tree"
  copy_tree "$work"
  if ! ( cd "$work" && patch -p1 --silent --forward < "$abs" ); then
    printf '   ROT      %s: patch no longer applies; the code moved and the mutation did not\n' "$id"
    broken=$((broken + 1))
    continue
  fi
  if [ -n "$clane" ]; then
    copy_tree "$cwork"
    ( cd "$cwork" && patch -p1 --silent --forward < "$abs" )
  fi

  # Direction control first: if the patched tree cannot even pass the lane it is
  # supposed to leave alone, the covering lane's red says nothing.
  if [ -n "$clane" ]; then
    rc=0; run_lane "$cwork" "$clane" "$scratch/$id-control.log" || rc=$?
    if [ "$rc" -eq 124 ]; then
      printf '   TIMEOUT  %s: control lane %s did not finish in %ss\n' "$id" "$clane" "$LANE_TIMEOUT"
      printf '            A lane that does not report is not a kill and not a survival.\n'
      broken=$((broken + 1))
      continue
    fi
    if [ "$rc" -eq 0 ]; then got=alive; else got=killed; fi
    if [ "$got" != "${cexpect:-alive}" ]; then
      printf '   BROKEN   %s: control lane %s was %s, expected %s\n' "$id" "$clane" "$got" "${cexpect:-alive}"
      printf '            The patch fails for a reason unrelated to what it blinds, so a\n'
      printf '            red on %s below would not be attributable to it.\n' "$lane"
      sed 's/^/     /' "$scratch/$id-control.log" | tail -20
      broken=$((broken + 1))
      continue
    fi
    controls=$((controls + 1))
    printf '   control  %s: %s still %s, as it must\n' "$id" "$clane" "$got"
  fi

  rc=0; run_lane "$work" "$lane" "$scratch/$id-cover.log" || rc=$?
  if [ "$rc" -eq 124 ]; then
    printf '   TIMEOUT  %s: %s did not finish in %ss with %s blinded\n' \
           "$id" "$lane" "$LANE_TIMEOUT" "$blinds"
    printf '            The mutation makes the lane HANG rather than fail. That is not a\n'
    printf '            kill: the lane never reported. Bound the loop the mutation\n'
    printf '            unbounds, or give the lane something to assert before it spins.\n'
    broken=$((broken + 1))
    continue
  fi
  if [ "$rc" -eq 0 ]; then got=alive; else got=killed; fi

  if [ "$got" != "$expect" ]; then
    if [ "$expect" = killed ]; then
      printf '   ALIVE    %s: %s stayed green with %s blinded\n' "$id" "$lane" "$blinds"
      printf '            Three possibilities, and they are not the same: %s cannot see\n' "$lane"
      printf '            this, the defence was never there, or the code is unreachable.\n'
      printf '            Only the third is fixed by deleting anything.\n'
      survived=$((survived + 1))
    else
      printf '   DIED     %s: %s reddened on a patch declared alive\n' "$id" "$lane"
      broken=$((broken + 1))
    fi
    continue
  fi

  if [ "$expect" = killed ]; then
    killed=$((killed + 1))
    printf '   killed   %s by %s (blinds %s)\n' "$id" "$lane" "$blinds"
  else
    printf '   canary   %s survived %s, as it must (%s)\n' "$id" "$lane" "$blinds"
  fi
done

printf '  ----------------------------------------------------------\n'
printf '   %d killed, %d survived, %d broken, %d direction controls held\n\n' \
       "$killed" "$survived" "$broken" "$controls"

if [ "$n" -eq 0 ]; then
  printf '  No patch ran. An empty mutation lane proves nothing.\n\n'; exit 2
fi
if [ "$controls" -eq 0 ]; then
  printf '  No direction control held. Without one, this lane cannot distinguish a\n'
  printf '  mutant that was caught from a patch that broke the build.\n\n'
  exit 2
fi
if [ "$survived" -ne 0 ] || [ "$broken" -ne 0 ]; then
  printf '  A patch did not behave as its header declares.\n'
  printf '  Fix the lane, not the patch: the patch is the specification of what\n'
  printf '  that lane is supposed to catch.\n\n'
  exit 1
fi
