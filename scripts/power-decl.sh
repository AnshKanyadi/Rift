#!/usr/bin/env sh
# Every mutant's POWER DECLARATION is internally consistent, checked without
# running anything.
#
# # Why this exists
#
# `make power-mutants` went red the day `M67` and `M70` landed and stayed red for
# the back half of A6. Both declared `power-seeds: 1 / floor: 1 / ceiling: 1` with
# a note saying they are killed deterministically by their covering test -- and
# both covering tests are UNIT tests, while the probe measures SWEEP detection. A
# one-seed sweep floor is a claim the instrument can never satisfy.
#
# Nobody noticed because the lane costs fifteen CPU-hours, so nothing runs it: it
# is in `make ci`, the workflow has never executed, and the pre-push hook -- the
# only thing on this machine that runs automatically -- does not include it.
#
# **The declaration is checkable in milliseconds and the measurement is not.** So
# the cheap half goes where something runs it, and the expensive half keeps its
# tier. This catches the shape that actually failed without waiting for the sweep
# that would have caught it eventually.
set -eu
PATCHDIR=${1:-sim/mutants}

printf '\n  power declarations: consistent without running anything\n'
printf '  ----------------------------------------------------------------\n'
bad=0; checked=0
for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  na=$(sed -n 's/^# power: *//p' "$patch")
  covered=$(sed -n 's/^# power-covered-by: *//p' "$patch")
  unreach=$(sed -n 's/^# power-unreachable: *//p' "$patch")
  expect=$(sed -n 's/^# expect: *//p' "$patch")
  seeds=$(sed -n 's/^# power-seeds: *//p' "$patch")
  floor=$(sed -n 's/^# power-floor: *//p' "$patch")
  ceiling=$(sed -n 's/^# power-ceiling: *//p' "$patch")
  detector=$(sed -n 's/^# power-detector: *//p' "$patch")
  measured=$(sed -n 's/^# power-measured: *//p' "$patch")
  [ -n "$detector" ] || detector=rate
  checked=$((checked + 1))

  say() { printf '   BAD      %-44s %s\n' "$id" "$1"; bad=$((bad + 1)); }

  # # The opt-out label was SPLIT, because one label held two opposite meanings
  #
  # `# power: n/a -- <reason>` covered both *this class is out of the sweep's
  # reach* and *this class is covered by a better instrument than a sweep*. Those
  # are opposites, and a reader could not tell them apart. Measured: the eight
  # framework classes wearing that label are killed by their covering tests in
  # about a SECOND each -- the best-covered classes in the tree -- and they wore
  # the same sentence as `M56`, whose claim was false on the day it was written.
  #
  # > **A label that collapses two opposite meanings is worse than no label,
  # > because it makes the well-covered case indistinguishable from the
  # > unexamined one.**
  #
  # So there are two declarations now, each with its own required evidence:
  #
  #   # power-covered-by: <instrument> -- <argument>
  #        the class is covered by a NAMED instrument that is not a sweep rate.
  #        The evidence is the instrument, and the refutation pass RUNS it.
  #
  #   # power-unreachable: <detector> -- <argument>
  #        the class is claimed out of the sweep's reach. The evidence is the
  #        DETECTOR the number was taken against, plus an argument that no other
  #        detector sees the class more often -- which is the field `M67` would
  #        have failed: its number was right and it named IdentityCollisions,
  #        while ForeignTagStarts reads 589 in thirty seeds.
  #
  # A class that declares NEITHER is refused. The bare `# power:` survives only
  # for a patch that must SURVIVE, where it is earned by `expect: alive` rather
  # than granted by the sentence.
  if [ -n "$na" ]; then
    if [ "$expect" = alive ]; then
      continue
    fi
    say "carries the retired bare 'power:' opt-out. Say which claim you are making: power-covered-by: <instrument> -- <why>, or power-unreachable: <detector> -- <why>. One label for two opposite meanings is how a class killed in one second read the same as a class nobody had measured"
    continue
  fi

  if [ -n "$covered" ] && [ -n "$unreach" ]; then
    say "declares BOTH power-covered-by and power-unreachable; they are opposite claims and a class making both has made neither"
    continue
  fi

  if [ -n "$covered" ]; then
    inst=${covered%% -- *}
    case "$covered" in
      *" -- "*) : ;;
      *) say "declares power-covered-by with no ' -- <argument>'; naming an instrument without saying why it is the right one is the old label with a new name" ;;
    esac
    [ -n "$inst" ] || say "declares power-covered-by with no instrument"
    if [ "$(printf '%s' "$covered" | wc -c)" -lt 60 ]; then
      say "declares power-covered-by with a $(printf '%s' "$covered" | wc -c)-character argument; the evidence for this label is which instrument covers the class and why a sweep is not it"
    fi
    continue
  fi

  if [ -n "$unreach" ]; then
    det=${unreach%% -- *}
    case "$unreach" in
      *" -- "*) : ;;
      *) say "declares power-unreachable with no ' -- <argument>'" ;;
    esac
    [ -n "$det" ] || say "declares power-unreachable with no detector named"
    # # The named-detector rule, which is M67's finding as a check
    #
    # A reachability number is a property of the DETECTOR that produced it. An
    # opt-out citing a number has bounded itself to that one detector and is
    # silent about every other criterion in the list -- including, four times in
    # this project now, the one that catches the class.
    case "$unreach" in
      *"NO OTHER DETECTOR"*) : ;;
      *) say "declares power-unreachable and does not argue NO OTHER DETECTOR. A reachability number is a property of the detector that produced it, so naming the detector is half the claim and arguing that no other detector sees the class more often is the other half. M62, M73, M47 and M67 are four occurrences of exactly that gap" ;;
    esac
    continue
  fi

  [ -n "$seeds" ] || { say "declares neither power-seeds nor one of the two exemptions (power-covered-by / power-unreachable). Saying nothing is how thirty-one classes ended up sharing four floors"; continue; }

  # # A sweep expectation on a handful of seeds is not a measurement
  #
  # This is the rule that would have caught M67, M68 and M70 on the day they
  # landed. A class whose covering test is a unit test has no sweep rate; saying
  # "1 of 1" does not give it one, it just makes the lane red.
  if [ "$seeds" -lt 30 ]; then
    say "declares a sweep expectation over $seeds seeds; a sweep that short cannot measure a rate, and a class killed deterministically by a unit test wants an opt-out with that reason"
  fi

  case "$measured" in
    "")        say "carries no power-measured line, so its floor is a guess nobody has checked" ;;
    PENDING*)  say "carries power-measured: PENDING; a floor asserted against a measurement that was never taken is a number nobody chose" ;;
  esac

  if [ "$detector" = sweep ]; then
    [ -z "$floor" ] && [ -z "$ceiling" ] || \
      say "declares a sweep detector AND a rate floor; a sweep verdict has no rate"
    continue
  fi

  [ -n "$floor" ] || say "declares power-seeds and no power-floor"
  [ -n "$ceiling" ] || say "declares power-seeds and no power-ceiling"
  if [ -n "$ceiling" ] && [ "$ceiling" -gt "$seeds" ]; then
    say "declares a ceiling of $ceiling above its own sweep of $seeds seeds, which nothing can breach"
  fi
  if [ -n "$floor" ] && [ "$floor" -gt "$seeds" ]; then
    say "declares a floor of $floor above its own sweep of $seeds seeds, which nothing can meet"
  fi
done
printf '  ----------------------------------------------------------------\n'
printf '   %d declarations checked, %d inconsistent\n\n' "$checked" "$bad"
[ "$checked" -gt 0 ] || { printf '  No declarations. This lane proved nothing.\n\n'; exit 2; }
[ "$bad" -eq 0 ] || exit 1
