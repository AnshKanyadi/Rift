#!/usr/bin/env sh
# The REFUTATION pass: an opt-out is a claim, and this is the instrument that can
# refute it.
#
# # Why this exists
#
# `scripts/power-mutants.sh` skips any patch carrying a `# power:` line. That is
# deliberate -- an opt-out is supposed to mean *there is nothing to measure* --
# and it is also the whole defect:
#
#   **An opt-out is a claim about reachability, and it exempts itself from the
#   only instrument that could refute it. A floored class is re-measured every
#   time the lane runs; an opted-out class is re-measured never.**
#
# `M56` is what that costs. Its declaration said the sweep could not reach its
# class, reasoned by ANALOGY with `M53` rather than measured. It measures 280 of
# 300, first at seed 0, and 28 of 30 under A5's own shape -- the shape the
# opt-out was written against. So the claim was **false on the day it was
# written**, not gone stale, and it stood for a phase and a half because writing
# `power: n/a` is what turns the measurement off. DESIGN-A6 §42.3.
#
# # The scope problem, which is why this is not a five-line loop
#
# Several opt-outs patch the ORACLE FRAMEWORK itself. Running the raft probe
# against a mutated checker reports differences for reasons that are not
# refutations: `M12` makes an unreturned operation score as a safety failure, so
# the probe's detection rate goes UP while nothing about the system under test
# has moved. A pass that reported that as a refutation would be manufacturing
# findings, which is BUG-016's standard and is worse than no pass at all.
#
# So the scope is decided by a fact about the patch rather than by a claim inside
# it, and the classification below is the whole design:
#
#   system     the patch changes the system under test. The raft probe is
#              INDEPENDENT of it, so the probe can refute the claim. MEASURED.
#   framework  the patch changes the code that COMPUTES THE PROBE'S VERDICT.
#              Measurement here is not weak, it is unsound: the instrument and
#              the thing under test are the same code. An exemption here must
#              carry a written argument saying what a sound refutation would be.
#
# # And the LABEL was split, which is the finding this pass actually produced
#
# One sentence -- `# power: n/a -- <reason>` -- meant two opposite things. The
# eight framework classes wearing it are **killed by their covering tests in
# about a second each**, which makes them the best-covered classes in the tree;
# `M56` wore the same sentence over a reachability claim that was false on the
# day it was written.
#
# > **A label that collapses two opposite meanings is worse than no label,
# > because it makes the well-covered case indistinguishable from the unexamined
# > one.**
#
# So there are two declarations, each with its own required evidence, and a class
# declaring neither is refused:
#
#   # power-covered-by: <instrument> -- <why a sweep is not the instrument>
#        EVIDENCE: the instrument. This pass RUNS it -- a floor named in
#        floors.go is checked and its lane executed; a covering test is put
#        through the mutant lane and must kill.
#
#   # power-unreachable: <detector> -- <why, including NO OTHER DETECTOR>
#        EVIDENCE: the detector the number was taken against, plus an argument
#        that no other detector sees the class more often. That second half is
#        the field `M67` would have failed: its numbers were right, it named
#        IdentityCollisions, and ForeignTagStarts reads 589 in thirty seeds.
#
# **The exemption is earned by the file list, not granted by the header.** A
# patch that does not touch a framework file cannot buy its way out by writing
# `power-refutation:`, and a patch that does touch one cannot stay silent. That
# is what stops the category becoming a label -- which is the failure mode this
# whole pass exists to answer, one level up.
#
# # What counts as a refutation, mechanically
#
# A detection the unmutated tree does not also have, at the same seed count and
# the same config. A difference, not a presence -- DESIGN-A6 §16.4 records what
# believing a presence costs.
#
# And a second verdict, because several `power-unreachable` claims are STRONGER
# than "the sweep does not detect it": `M30`, `M47` and `M66` claim the mutation changes
# nothing the harness counts. That claim is checkable exactly -- the probe prints
# its accumulated census -- so a class declaring `power-refute-claim:
# census-identical` fails this lane if its census moves, even when no oracle
# spoke. The conclusion surviving is not the same as the reason surviving, and an
# opt-out is the reason.
#
# # The cheap half, because this lane is a remembered lane the day it is written
#
# RISK-1's only self-applicable mitigation is *a millisecond check on the inputs
# of an hours-long lane*, and it is the one that found six inconsistent power
# declarations in the time it takes to run `sed`. This lane costs a probe per
# measurable class and is therefore in the same column as the one it was built to
# repair -- so it carries its own cheap half from the first commit rather than
# waiting to earn one.
#
# `--declarations` runs the PARTITION and the HEADER checks and no probe: every
# opt-out is classified, every framework exemption is earned by the file list and
# carries an argument, every toy redirection names a floor that exists. That is
# the half that catches a class arriving in the exempt column with a label, which
# is the failure this whole pass exists to refuse. It is milliseconds, so it goes
# where something runs it.
#
# usage: power-refute.sh [--demonstrate | --declarations] [patch-dir]
#
#   REFUTE_SEEDS   seeds per class (default 30)
#   REFUTE_JOBS    concurrent probes (default 1; the measurement parallelises
#                  exactly, for power-mutants.sh's reason)
#   --demonstrate  ALSO run the probe against the framework classes and print
#                  what it says. This is NOT a verdict and the report labels it
#                  so: it exists to show that the scope rule is necessary rather
#                  than argued -- see `M12`.
set -eu

GO=${GO:-go}
DEMO=no
DECL=no
if [ "${1:-}" = "--demonstrate" ]; then DEMO=yes; shift; fi
if [ "${1:-}" = "--declarations" ]; then DECL=yes; shift; fi
PATCHDIR=${1:-sim/mutants}
ROOT=$(pwd)
SEEDS=${REFUTE_SEEDS:-30}
JOBS=${REFUTE_JOBS:-1}

# # The lane copies a LIVE tree, and a tree edited mid-copy is not a tree
#
# copy_tree tars the working directory. If a file changes while that runs, the
# copy is of a state that never existed: the patch may fail (ROT), or apply onto
# a mismatched file and produce a tree that does not compile, which surfaces as
# **ERROR -- the probe produced no measurement** with nothing in the log.
#
# That happened to `M76` and `M77` in A7's gating run: both were reported ERROR
# with no output, and both apply and build cleanly on a stable tree. The verdict
# was about the working directory, not about the class -- and an ERROR whose
# provenance is unestablished is the same category as a number quoted from an
# unchecked source, which this project has now been bitten by three times.
#
# A lane that takes minutes to hours cannot demand nobody touches the tree. It
# CAN record what it measured, so a verdict is attributable afterwards.
snapshot_id() {
  git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown
}
dirty_note() {
  if [ -n "$(git -C "$ROOT" status --porcelain 2>/dev/null)" ]; then
    printf ' (tree DIRTY at start: verdicts are against uncommitted state)'
  fi
}

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

copy_tree() {
  mkdir -p "$1"
  tar cf - --exclude=./.git --exclude=./.github . | (cd "$1" && tar xf -)
}

# classify names the group from the files the patch touches.
#
# The framework list is not "files in sim/". It is the code that produces the
# three things `noticed()` reads -- the loop's halt record, the checkers'
# verdicts, and the verdict classification the scenario applies afterwards. A
# mutation to any of them is a mutation to the probe's own answer.
classify() {
  group=system
  for f in $(grep '^--- ' "$1" | sed 's|^--- a/||'); do
    case $f in
      sim/oracle.go|sim/loop.go|sim/checker/*|sim/hunt/raftscenario.go)
        printf 'framework\n'; return 0 ;;
      sim/toy/*)
        group=toy ;;
    esac
  done
  printf '%s\n' "$group"
}

# probe_census prints the POWER-CENSUS line and probe_rate the POWER line, for a
# tree already prepared at $1.
run_probe() {
  (cd "$1" && POWER_SEEDS="$SEEDS" POWER_CONFIG="$2" \
    $GO test -count=1 -v -timeout 3600s -run TestPowerProbe ./sim/hunt/ 2>&1 | grep '^POWER' || true)
}

# baseline for one config, computed once and cached: every opted-out class in the
# tree measures under `current`, so this is one run rather than one per class.
baseline_for() {
  cache="$scratch/baseline-$1.out"
  if [ ! -f "$cache" ]; then
    run_probe "$ROOT" "$1" > "$cache"
  fi
  cat "$cache"
}

# system_census strips the fields the VERDICT computes, leaving what the system
# under test counted. For a framework mutant those fields are the mutation's own
# arithmetic, so a difference confined to them is not evidence about the system.
system_census() {
  sed 's/ Violations:[0-9-]*//; s/ Inconclusive:[0-9-]*//; s/ Pass:[0-9-]*//;
       s/ FirstViolation:[0-9-]*//; s/ FoundAViolation:[a-z]*//;
       s/ InconclusiveCauses:.*$//'
}

measure_one() {
  patch=$1; out=$2; cfg=$3
  id=$(sed -n 's/^# id: *//p' "$patch")
  case $patch in /*) abs=$patch ;; *) abs=$ROOT/$patch ;; esac
  work="$scratch/w-$id"
  copy_tree "$work"
  if ! (cd "$work" && patch -p1 --silent --forward < "$abs" 2>/dev/null); then
    printf 'ROT\n' > "$out"; rm -rf "$work"; return 0
  fi
  # # patch(1) exiting 0 is NOT proof the mutation is present
  #
  # An EMPTY patch file -- zero bytes, or one whose hunks all fail to parse into
  # anything -- makes patch exit 0 having changed nothing. Measured:
  #
  #     empty.patch   patch exit=0   tree=IDENTICAL
  #     junk.patch    patch exit=2   tree=IDENTICAL
  #
  # Junk is caught by the exit status. Empty is not, and the consequence differs
  # by lane. In `mutants.sh` an unmutated tree reports ALIVE, which is loud. HERE
  # it reports **zero detections**, and this lane reads zero detections as the
  # reachability claim CONFIRMED -- a green over a tree that was never mutated,
  # by the pass whose entire purpose is refusing claims nothing checks.
  #
  # So the mutation is verified PRESENT rather than assumed from an exit code:
  # at least one file the patch names must actually differ.
  tgt=$(grep '^--- a/' "$abs" | head -1 | sed 's|^--- a/||')
  if [ -n "$tgt" ] && cmp -s "$ROOT/$tgt" "$work/$tgt"; then
    printf 'UNMUTATED\n' > "$out"; rm -rf "$work"; return 0
  fi
  run_probe "$work" "$cfg" > "$out.raw"
  rm -rf "$work"
  if ! grep -q '^POWER ' "$out.raw"; then printf 'ERROR\n' > "$out"; return 0; fi
  printf 'OK\n' > "$out"
  cat "$out.raw" >> "$out"
}

failed=0; refuted=0; confirmed=0; exempt=0; redirected=0; listed=0; verified=0

printf '\n  the refutation pass: every claim re-asked, or exempt for a reason it earns\n'
printf '  measured against %s%s\n' "$(snapshot_id)" "$(dirty_note)"
printf '  ----------------------------------------------------------------------\n'
printf '   %d seeds per class. An exemption is a claim; this is what re-asks it.\n\n' "$SEEDS"

# ------------------------------------------------------------------ partition
#
# Two axes, and neither is a judgement call. The DECLARATION says which claim the
# class is making; the FILE LIST says whether this instrument may judge it.

fw_list=""; toy_list=""; sys_list=""; test_list=""; canary_list=""
for patch in "$PATCHDIR"/*.patch; do
  id=$(sed -n 's/^# id: *//p' "$patch")
  covered=$(sed -n 's/^# power-covered-by: *//p' "$patch")
  unreach=$(sed -n 's/^# power-unreachable: *//p' "$patch")
  bare=$(sed -n 's/^# power: *//p' "$patch")
  expect=$(sed -n 's/^# expect: *//p' "$patch")
  # A patch that must SURVIVE is not making a reachability claim: the canary is
  # declared against a test that does not cover it and a detection floor on it
  # would contradict its purpose. It is excluded by its own `expect:`, not by
  # name, so a second canary is handled the same way.
  if [ "$expect" = alive ]; then
    canary_list="$canary_list $id"; continue
  fi
  if [ -n "$covered" ] && [ -n "$unreach" ]; then
    printf '   BOTH      %-44s declares power-covered-by AND power-unreachable.\n' "$id"
    printf '             They are opposite claims. A class making both has made neither.\n'
    failed=$((failed + 1))
    continue
  fi
  if [ -z "$covered" ] && [ -z "$unreach" ]; then
    # Not an exemption at all -- a floored class, which power-mutants measures.
    # Unless it carries the retired bare label, which is the thing being retired.
    if [ -n "$bare" ]; then
      printf '   RETIRED   %-44s carries the bare power: opt-out and declares neither\n' "$id"
      printf '             power-covered-by: nor power-unreachable:. One label for two opposite\n'
      printf '             meanings is how a class killed in one second read the same as a class\n'
      printf '             nobody had measured.\n'
      failed=$((failed + 1))
    fi
    continue
  fi
  scope=$(classify "$patch")
  if [ -n "$covered" ]; then
    case "$covered" in
      floors.go\ *) toy_list="$toy_list $patch" ;;
      *)            test_list="$test_list $patch" ;;
    esac
    # A covered-by class whose patch modifies the instrument owes the scope
    # argument as well: naming a better instrument does not make the probe sound.
    [ "$scope" = framework ] && fw_list="$fw_list $patch"
  else
    case "$scope" in
      framework) fw_list="$fw_list $patch" ;;
      *)         sys_list="$sys_list $patch" ;;
    esac
  fi
done

# ------------------------------------------------- group 1: the system classes

if [ -n "$sys_list" ] && [ "$DECL" = yes ]; then
  # Declarations mode still ACCOUNTS for these -- a class silently dropping out
  # of the measurable group would otherwise be invisible in the cheap half, which
  # is the same blindness one level down.
  printf '  MEASURABLE -- not measured in this mode, listed so the partition is checkable\n'
  for patch in $sys_list; do
    printf '   measurable %-43s\n' "$(sed -n 's/^# id: *//p' "$patch")"
    listed=$((listed + 1))
  done
  printf '\n'
elif [ -n "$sys_list" ]; then
  printf '  MEASURED -- the probe is independent of these patches\n'
  if [ "$JOBS" -gt 1 ]; then
    running=0
    for patch in $sys_list; do
      id=$(sed -n 's/^# id: *//p' "$patch")
      cfg=$(sed -n 's/^# power-config: *//p' "$patch"); [ -n "$cfg" ] || cfg=current
      baseline_for "$cfg" >/dev/null
      measure_one "$patch" "$scratch/$id.result" "$cfg" &
      running=$((running + 1))
      if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
    done
    wait
  fi
  for patch in $sys_list; do
    id=$(sed -n 's/^# id: *//p' "$patch")
    cfg=$(sed -n 's/^# power-config: *//p' "$patch"); [ -n "$cfg" ] || cfg=current
    claim=$(sed -n 's/^# power-refute-claim: *//p' "$patch")
    if [ "$JOBS" -le 1 ]; then
      baseline_for "$cfg" >/dev/null
      measure_one "$patch" "$scratch/$id.result" "$cfg"
    fi
    status=$(head -1 "$scratch/$id.result" 2>/dev/null || echo ERROR)
    if [ "$status" = UNMUTATED ]; then
      printf '   UNMUTATED %-44s the patch applied and the tree did not change.\n' "$id"
      printf '             patch(1) exits 0 on an EMPTY patch, so a zero-detection result here\n'
      printf '             would have CONFIRMED this claim against a clean tree.\n'
      failed=$((failed + 1)); continue
    fi
    if [ "$status" != OK ]; then
      printf '   %-9s %-44s the probe produced no measurement\n' "$status" "$id"
      failed=$((failed + 1)); continue
    fi
    got=$(grep '^POWER ' "$scratch/$id.result" | sed -n 's/.*detected=\([0-9]*\).*/\1/p')
    first=$(grep '^POWER ' "$scratch/$id.result" | sed -n 's/.*first=\(-\{0,1\}[0-9]*\).*/\1/p')

    baseline_for "$cfg" | grep '^POWER-SWEEP ' | sed 's/^POWER-SWEEP //' | sort > "$scratch/$id.base"
    grep '^POWER-SWEEP ' "$scratch/$id.result" | sed 's/^POWER-SWEEP //' | sort > "$scratch/$id.swept"
    newfail=$(comm -13 "$scratch/$id.base" "$scratch/$id.swept" | head -1)

    # # The GROUND of a refutation is printed, because the two grounds are not
    # # equally strong at this lane's seed count
    #
    # A per-seed detection is unambiguous: a violation, an error, a panic or a
    # leaderless run on some seed, which the unmutated tree does not have.
    #
    # A sweep-criterion difference is the `power-detector: sweep` rule, and it is
    # the right rule -- but several of the exit criteria are NON-VACUITY
    # assertions (*no move ever completed*, *no prewrite ever met a live lock*),
    # and at a reduced seed count those are marginal on the unmutated tree too.
    # A mutation that only reshuffles the schedule can flip one without the class
    # being reachable at all. So the ground is named and the guidance is printed:
    # a sweep-only refutation is re-taken at a higher seed count before anybody
    # rewrites a declaration on the strength of it.
    #
    # Both grounds FAIL the lane. An opt-out under suspicion is not a passing
    # opt-out, and the difference is what has to be done next, not whether.
    if [ "${got:-0}" -gt 0 ]; then
      printf '   REFUTED   %-44s per-seed %s of %s, first=%s (%s)\n' "$id" "$got" "$SEEDS" "$first" "$cfg"
      printf '             Ground: a seed detected it. The opt-out says this class is out of the\n'
      printf '             sweep; the sweep found it.\n'
      [ -z "$newfail" ] || printf '             (a sweep criterion moved too: %s)\n' "$newfail"
      refuted=$((refuted + 1)); failed=$((failed + 1)); continue
    fi
    if [ -n "$newfail" ]; then
      printf '   REFUTED?  %-44s sweep only, per-seed 0 of %s (%s)\n' "$id" "$SEEDS" "$cfg"
      printf '             a criterion the baseline passes: %s\n' "$newfail"
      printf '             Ground: an AGGREGATE criterion, and at %s seeds the non-vacuity criteria\n' "$SEEDS"
      printf '             are marginal on the clean tree as well. Re-take at a higher seed count\n'
      printf '             before rewriting the declaration; the lane fails either way.\n'
      refuted=$((refuted + 1)); failed=$((failed + 1)); continue
    fi

    # The conclusion held. Now the REASON, for the classes that state one strong
    # enough to check.
    if [ "$claim" = census-identical ]; then
      bc=$(baseline_for "$cfg" | grep '^POWER-CENSUS ')
      mc=$(grep '^POWER-CENSUS ' "$scratch/$id.result")
      if [ "$bc" != "$mc" ]; then
        printf '   CLAIM-BROKEN %-41s 0 of %s, and the census MOVED (%s)\n' "$id" "$SEEDS" "$cfg"
        printf '             It declares census-identical -- UNREACHED rather than undetected -- and\n'
        printf '             the mutation changed something the harness counts. The conclusion may\n'
        printf '             still hold; the stated reason does not, and the reason is the opt-out.\n'
        # POSIX sh, so no process substitution: the field split goes through
        # files. Splitting on ` Word:` is a heuristic for READABILITY only -- the
        # verdict above is exact string equality on the whole line, so a bad
        # split can make this listing noisy and cannot make the lane wrong.
        printf '%s\n' "$bc" | sed 's/ \([A-Z][A-Za-z0-9]*:\)/@\1/g' | tr '@' '\n' > "$scratch/$id.bfields"
        printf '%s\n' "$mc" | sed 's/ \([A-Z][A-Za-z0-9]*:\)/@\1/g' | tr '@' '\n' > "$scratch/$id.mfields"
        diff "$scratch/$id.bfields" "$scratch/$id.mfields" | grep '^[<>]' | head -8 | sed 's/^/               /'
        failed=$((failed + 1)); continue
      fi
      printf '   confirmed %-44s 0 of %s, census byte-identical (%s)\n' "$id" "$SEEDS" "$cfg"
      printf '             (identity at %s seeds is WEAKER than the declaration it checks, which was\n' "$SEEDS"
      printf '             taken at its own count; this re-asks the claim, it does not restate it)\n'
    else
      printf '   confirmed %-44s 0 of %s, no new sweep criterion (%s)\n' "$id" "$SEEDS" "$cfg"
    fi
    confirmed=$((confirmed + 1))
  done
  printf '\n'
fi

# ------------------------------------ group 2: COVERED-BY, verified by running it
#
# The evidence for `power-covered-by` is the instrument, so the pass runs it. A
# named instrument nobody executes is the old label with a longer sentence.

if [ -n "$toy_list" ]; then
  printf '  COVERED-BY a floor -- the raft sweep never runs sim/toy, so its silence is no evidence\n'
  printf '   These classes do not claim unreachability. They name another instrument, and the\n'
  printf '   sound check is that the instrument exists and enforces something.\n'
  for patch in $toy_list; do
    id=$(sed -n 's/^# id: *//p' "$patch")
    covered=$(sed -n 's/^# power-covered-by: *//p' "$patch")
    inst=${covered%% -- *}
    floor=${inst#floors.go }
    if [ "$floor" = "$inst" ] || [ -z "$floor" ]; then
      printf '   UNNAMED   %-44s names no floor after "floors.go".\n' "$id"
      printf '             A redirection nobody can follow is an exemption with extra words.\n'
      failed=$((failed + 1)); continue
    fi
    if ! grep -q "Name: *\"$floor\"" sim/hunt/floors.go; then
      printf '   MISSING   %-44s names floor %s, which is not in sim/hunt/floors.go\n' "$id" "$floor"
      printf '             A redirection to a floor that no longer exists READS as coverage.\n'
      failed=$((failed + 1)); continue
    fi
    printf '   covered   %-44s floored as %s\n' "$id" "$floor"
    redirected=$((redirected + 1))
  done
  if [ "$DECL" = yes ]; then
    printf '   the floors are named and present; running their lane is the measured half.\n\n'
  else
    printf '   ---- running the lane those floors live in\n'
    if $GO test -count=1 -run 'TestHarnessPower|TestEveryObservableFlawHasAFloor' ./sim/hunt/ >"$scratch/toy.log" 2>&1; then
      printf '   the floor lane is GREEN, so the instrument named is one that ran.\n\n'
    else
      printf '   the floor lane FAILED. The instrument named does not hold.\n'
      tail -20 "$scratch/toy.log" | sed 's/^/     /'
      failed=$((failed + 1))
      printf '\n'
    fi
  fi
fi

if [ -n "$test_list" ]; then
  printf '  COVERED-BY a test -- and the evidence is that the test KILLS, not that it exists\n'
  printf '   This is the half of the old label that was never wrong: a deterministic kill in a\n'
  printf '   second is a STRONGER statement than a rate floor over %s seeds, not a weaker one.\n' "$SEEDS"
  for patch in $test_list; do
    id=$(sed -n 's/^# id: *//p' "$patch")
    covered=$(sed -n 's/^# power-covered-by: *//p' "$patch")
    inst=${covered%% -- *}
    ct=$(sed -n 's/^# covering-test: *//p' "$patch")
    if [ "$inst" != "$ct" ]; then
      printf '   MISMATCH  %-44s names %s and its covering-test is %s\n' "$id" "$inst" "$ct"
      printf '             The instrument this class claims must be the one the mutant lane runs,\n'
      printf '             or the claim and the check are about different things.\n'
      failed=$((failed + 1)); continue
    fi
    printf '   covered   %-44s %s\n' "$id" "$inst"
    verified=$((verified + 1))
  done
  if [ "$DECL" = yes ]; then
    printf '   the instruments are named and match their covering-test; RUNNING them is the\n'
    printf '   measured half.\n\n'
  else
    printf '   ---- putting every one of them through the mutant lane\n'
    mkdir -p "$scratch/covertests"
    for patch in $test_list; do cp "$patch" "$scratch/covertests/"; done
    if sh scripts/mutants.sh "$scratch/covertests" >"$scratch/covertests.log" 2>&1; then
      grep -cE '^   killed' "$scratch/covertests.log" > "$scratch/killcount" || true
      printf '   %s killed by the instrument each one names.\n\n' "$(cat "$scratch/killcount")"
    else
      # The lane exits non-zero when it has no surviving canary, which is a
      # property of the SUBSET this pass hands it rather than a finding. So the
      # verdict is read from the counts, not from the exit code.
      k=$(grep -cE '^   killed' "$scratch/covertests.log" || true)
      n=$(ls "$scratch/covertests" | wc -l | tr -d ' ')
      if [ "$k" = "$n" ]; then
        printf '   %s of %s killed by the instrument each one names.\n\n' "$k" "$n"
      else
        printf '   ONLY %s of %s killed. A class exempt because something better covers it, whose\n' "$k" "$n"
        printf '   something-better does not kill it, is exempt for nothing.\n'
        grep -E '^   (ALIVE|ROT|MISSING|DIED)' "$scratch/covertests.log" | sed 's/^/     /' | head -10
        failed=$((failed + 1))
        printf '\n'
      fi
    fi
  fi
fi

# ---------------------------------------------- group 3: the framework classes

if [ -n "$fw_list" ]; then
  printf '  UNMEASURABLE HERE -- the instrument is what the patch modifies\n'
  printf '   Not "too hard to measure". The probe reads a verdict these patches compute, so a\n'
  printf '   number taken here is the mutation reporting on itself. Naming a better instrument\n'
  printf '   (above) does not make the probe sound, so each also says what a sound refutation\n'
  printf '   WOULD look like.\n'
  for patch in $fw_list; do
    id=$(sed -n 's/^# id: *//p' "$patch")
    arg=$(sed -n 's/^# power-refutation: *//p' "$patch")
    if [ -z "$arg" ]; then
      printf '   BARE      %-44s is exempt from measurement and carries no argument.\n' "$id"
      printf '             That is a class whose only defence is that nothing checks it, which is\n'
      printf '             the exact thing this pass exists to refuse.\n'
      failed=$((failed + 1)); continue
    fi
    if [ "$(printf '%s' "$arg" | wc -c)" -lt 80 ]; then
      printf '   THIN      %-44s carries a %s-character argument.\n' "$id" "$(printf '%s' "$arg" | wc -c | tr -d ' ')"
      printf '             An exemption is a claim that no sound instrument exists. Say what one\n'
      printf '             would have to look like, or it is a label.\n'
      failed=$((failed + 1)); continue
    fi
    printf '   exempt    %-44s\n' "$id"
    printf '%s\n' "$arg" | fold -s -w 84 | sed 's/^/               /'
    exempt=$((exempt + 1))
  done
  printf '\n'
fi

# --------------------------------------------------- the demonstration, opt-in

if [ "$DEMO" = yes ] && [ -n "$fw_list" ]; then
  printf '  DEMONSTRATION -- what the probe says about the exempt classes, and why it is not a verdict\n'
  printf '   Read the two columns together. A framework mutant that "detects" while every system\n'
  printf '   census field is unchanged has moved the VERDICT and nothing else, which is a false\n'
  printf '   refutation caught before it was reported rather than argued away afterwards.\n'
  for patch in $fw_list; do
    id=$(sed -n 's/^# id: *//p' "$patch")
    cfg=$(sed -n 's/^# power-config: *//p' "$patch"); [ -n "$cfg" ] || cfg=current
    baseline_for "$cfg" >/dev/null
    measure_one "$patch" "$scratch/d-$id.result" "$cfg"
    if [ "$(head -1 "$scratch/d-$id.result")" != OK ]; then
      printf '   %-44s probe produced no measurement (%s)\n' "$id" "$(head -1 "$scratch/d-$id.result")"
      continue
    fi
    got=$(grep '^POWER ' "$scratch/d-$id.result" | sed -n 's/.*detected=\([0-9]*\).*/\1/p')
    bsys=$(baseline_for "$cfg" | grep '^POWER-CENSUS ' | system_census)
    msys=$(grep '^POWER-CENSUS ' "$scratch/d-$id.result" | system_census)
    same=MOVED; [ "$bsys" = "$msys" ] && same='byte-identical'
    printf '   %-44s detected %s of %s, system-side census %s\n' "$id" "$got" "$SEEDS" "$same"
  done
  printf '\n'
fi

printf '  ----------------------------------------------------------------------\n'
if [ "$DECL" = yes ]; then
  printf '   DECLARATIONS ONLY -- no probe ran, so nothing here confirms a reachability claim.\n'
  printf '   %d measurable and unmeasured, %d floors + %d tests named but NOT run, %d with the\n' \
    "$listed" "$redirected" "$verified" "$exempt"
  printf '   unmeasurable-here argument, %d failures\n' "$failed"
else
  printf '   %d confirmed, %d REFUTED  |  covered-by: %d floors + %d tests, instruments RUN\n' \
    "$confirmed" "$refuted" "$redirected" "$verified"
  printf '   %d unmeasurable here and carrying the argument for why  |  %d failures\n' \
    "$exempt" "$failed"
fi
[ -z "$canary_list" ] || printf '   not a reachability claim, excluded by its own expect:%s\n' "$canary_list"
printf '\n'

if [ $((confirmed + refuted + redirected + exempt + listed + verified)) -eq 0 ]; then
  printf '  No opt-out was examined. An empty refutation pass proves nothing.\n\n'
  exit 2
fi
[ "$failed" -eq 0 ] || exit 1
