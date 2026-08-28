#!/usr/bin/env sh
# A covering test must EXECUTE the line its mutant changes.
#
# # Why this exists, and why it is not another sentence in a design doc
#
# Four times in one day a covering test failed to kill its mutant because the
# test called the guarded function INLINE rather than through the path the patch
# modifies. The mutant deletes a call site; the test replicates that call; the
# test cannot fail. DESIGN-A6 §22.6 states that exact failure in its own words
# three hours before two of them happened.
#
# Every one was caught by the mutant surviving, so the suite works — but that
# makes the suite the only thing standing between the repository and dead
# covering tests, and it only notices after somebody runs the whole thing.
#
# # The check
#
# Go's own coverage answers it directly and without annotation: run the covering
# test on the UNMUTATED tree with coverage on, and require the FIRST LINE OF EACH
# CONTIGUOUS DELETED-OR-REPLACED RUN to be covered. A test that goes around the
# path leaves it at zero.
#
# It cannot be satisfied by claiming an entry point, because coverage is produced
# by execution rather than by assertion. That is what makes it mechanical rather
# than remembered.
#
# # SKIP is an ANSWER, and the person most likely to misread it is editing this file
#
# A patch that only ADDS lines has no deleted-or-replaced run, so there is no line
# whose coverage can be required and the question this lane asks -- *does the
# covering test execute the line this patch changes* -- has no subject. The lane
# reports SKIP, and that is the correct verdict rather than a failure to reach one.
#
# **A mutant that only adds is asking a different question from one that
# replaces.** An addition changes what happens NEXT TIME somebody edits nearby; a
# replacement changes what happens now, and only the second has a line to cover.
#
# SKIP reads like a gap. The temptation is to make the lane say something --
# require coverage of the line ABOVE the insertion, or of the inserted lines
# themselves on the mutated tree -- and both would turn a precise verdict into a
# false one, because neither is the question. **When SKIP appears, look at the
# patch, not at this file.** If the class deserves a coverage answer, rewrite the
# mutation as a replacement so that it poses one; A7's `M83` was rewritten exactly
# that way, from an inserted branch to a replacement of `return r.commitIndex`.
#
# # Why the FIRST line and not every line, which is what it used to be
#
# The first version required every deleted line to be covered, and its first
# complete run produced four DEAD verdicts of which zero were true findings
# (DESIGN-A6 §36). In all four the mutation's entry point was covered, every
# executable line of the mutated region was covered, and the only uncovered lines
# were ones no test can cover:
#
#   M15  sim/oracle.go:279     `}`  -- the block's closing brace
#   M55  kv/store.go:217       `}`  -- the block's closing brace
#   M29  raft/raft.go:2543-45  a panic's message, inside an assertion body
#   M60  kv/txn.go:204-205     `return err` from an engine read, and its brace
#
# A closing brace is not a statement: Go's profile records blocks as
# `file:startLine.col,endLine.col count` and the block ends at its last
# STATEMENT, so the `}` belongs to no span and reads uncovered forever. Every
# patch that deletes a block deletes one, so under the old rule every such patch
# was a candidate false positive. An assertion body only runs when the assertion
# FAILS -- M29 removes a panic that fires when state machine safety breaks, so
# the lane's own printed advice was asking for a covering test that violates
# state machine safety. An error branch only runs when the engine errors.
#
# Three more patches had already been narrowed BY HAND the same day to get past
# exactly this shape -- M72, and M65 and M66 after re-pointing, uncovered lines
# `return err` and its brace -- each narrowing a workaround for a defect nobody
# had named yet. Seven instances of one shape is a rule, not seven coincidences.
#
# So the rule is the faithful mechanisation of this lane's own sentence, *the
# line has to run at all*, where "the line" is the point at which the mutation
# takes effect. **A hunk whose first line never runs is a hunk the mutation
# cannot reach**, and that is precisely the failure this lane was built for: a
# test that calls the guarded function inline never executes the call site, so
# the hunk's first line is uncovered.
#
# This is a narrowing of what is DEMANDED and not of what is DETECTED, and the
# two checks that establish that were run before it landed:
#
#   1. The original induction -- a reconstruction of the seedClockAtLeast-inline
#      mistake against M70 -- still reports DEAD under it.
#   2. The full lane was run under both rules and every verdict that moved was
#      read one at a time. DESIGN-A6 §36 carries the table.
#
# # The lines come from APPLYING the patch, not from reading its header
#
# A patch's @@ numbers go stale as the file moves under it, and `patch` tolerates
# that with fuzz. The first version of this check read the header and reported a
# live path dead. Applying to a copy and diffing gives the lines `patch` actually
# touched, at their real positions.
#
# # What it does not check
#
# That the test would FAIL under the mutation. That is the mutant suite's job and
# it stays the mutant suite's job. This checks the necessary condition all four
# failures violated: the line has to run at all.
#
# # The timeout is EXPLICIT, and it matches the mutant lane's rather than being
# # a smaller number of its own
#
# The first full run never finished: every heavy `sim/hunt` covering test hit
# Go's 600-second default and was reported `ERROR ... did not run`, so the error
# count said nothing at all and a lane in `make ci` could not be relied on.
#
# The obvious fix is to bound the seed count, and it is the wrong one. This lane
# asks whether the covering test, AS THE MUTANT SUITE RUNS IT, executes the line
# its mutant changes. `scripts/mutants.sh` runs it at full seed ranges under
# `TEST_TIMEOUT=3600s` and no bound, so a bounded run here would answer a
# different question and could report a live path dead for a reason the mutant
# lane never sees. The two invocations are therefore kept the same shape, and
# what moves is the budget.
#
# Coverage instrumentation costs roughly 2x on top, which the budget absorbs. If
# one day it does not, the lane says which test ran out at a number somebody
# chose, instead of dying at a default nobody did.
#
# # And the LANE has a budget too, not just each test in it
#
# `TEST_TIMEOUT` bounds one covering test. Nothing bounded the lane: sixty
# mutants each entitled to an hour is a lane whose worst case is sixty hours, and
# a lane with no stated cost is a lane that quietly stops being run -- which is
# §37's third cost of having no remote, arriving from the inside.
#
# So `COVER_BUDGET` is the whole lane's wall clock, in seconds, taken from a
# measurement rather than guessed, on the same discipline as `race`'s 900s
# against a measured 191s. The lane checks it between batches, so an overrun is
# bounded by one batch rather than by one test.
#
# **It fails and says what it did not check.** A budget that silently truncated
# would turn "56 checked, 0 dead" into a sentence about a subset nobody named,
# which is the vacuous-green shape this whole lane exists to refuse.
#
# # And what the measurement actually said, which is worse than expected
#
# At `COVER_JOBS=6` under A6's shape the lane spent **89 minutes reaching 3 of
# its 11 batches**, and two of those three contained a covering test that ran out
# the whole 3600s per-test timeout. The cost driver is not the number of mutants:
# it is that several covering tests sweep **500 seeds** (`persist-before-reply`)
# or 1,500 (`leader-completeness`), and a seed costs ~5s at A6's shape and twice
# that under coverage instrumentation.
#
# So the budget below is not a target the lane comfortably meets. It bounds the
# pathological case -- every batch running out its per-test timeout is 11 hours --
# and it makes the cost a number somebody chose. **If the lane hits it, the fix is
# the per-test cost and not the budget**: re-point the 500-seed covering tests at
# cheap ones, which is exactly what was done for `M65` and `M66` when their
# covering test turned out to be the exit run (DESIGN-A6 section 25.3c).
set -eu

GO=${GO:-go}
# ONLY: run this lane for a named subset, space separated.
#
# # Why a filter is a correctness feature and not a convenience
#
# It existed before this comment did -- as a single exact name, undocumented, and
# nobody found it. So the code-position axis of §5e.2b was recorded as
# "unresolvable, because asking one question costs a full suite run", which was
# true of the lane as anybody could discover how to use it.
#
# **With no CI, a lane's cost is a fact about whether it gets run at all.** A
# covering test nobody can afford to run is not a covering test, and a lane that
# turns one question into sixty is a lane that gets skipped and then trusted.
#
# Track B's `cpp-mutants` is the precedent and this now matches it: a
# space-separated list, matched by id, so `ONLY="M46-... M34-..."` asks about two
# classes and nothing else.
ONLY="${ONLY:-${1:-}}"
ROOT=$(pwd)
TEST_TIMEOUT=${TEST_TIMEOUT:-3600s}

# The lane's own budget, in seconds. Six hours: the worst case at COVER_JOBS=6 is
# 11 batches x 3600s = 11 hours, and the measured lower bound is 89 minutes for
# the first three batches. See the header for why this is a bound rather than a
# target.
COVER_BUDGET=${COVER_BUDGET:-21600}
started=$(date +%s)

# overbudget reports whether the lane has spent its wall clock.
overbudget() {
  [ $(( $(date +%s) - started )) -gt "$COVER_BUDGET" ]
}

printf '\n  mutant coverage: every covering test executes the line its mutant changes\n'
printf '  ----------------------------------------------------------------\n'

fail=0; checked=0; skipped=0; unchecked=0; budgetblown=
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# # COVER_JOBS, for the same reason POWER_JOBS exists
#
# This lane runs each covering test at FULL seed ranges with coverage
# instrumentation, which is `make covering`'s workload once per mutant. Measured
# under A6's cost: 16 of 59 mutants in 105 minutes. Sequentially it is a lane
# nobody finishes, and a lane nobody finishes is a lane that reports nothing.
#
# It parallelises for a reason worth stating rather than assuming: **whether a
# test executes a line is a function of the test and the tree**, and nothing else
# on the machine changes it. Coverage is produced by execution and the execution
# is deterministic. A parallel run and a sequential run therefore produce
# identical verdicts, and the report is printed in patch order afterwards so they
# produce identical TEXT.
#
# What parallelism does cost is per-mutant wall time, which this lane does not
# claim and does not print. `make mutants` is where kill-time lives.
JOBS=${COVER_JOBS:-1}

# cover_one writes "MISSING\t<lines>" or "SKIP" or "ERROR\t<tail>" for one patch.
cover_one() {
  patch=$1; name=$2; out=$3
  test_name=$(sed -n 's/^# covering-test:[[:space:]]*//p' "$patch" | head -1)
  pkg=$(sed -n 's/^# package:[[:space:]]*//p' "$patch" | head -1)
  target=$(sed -n 's|^--- a/||p' "$patch" | head -1)
  work="$tmp/$name"
  ok=1
  for f in $(sed -n 's|^--- a/||p' "$patch"); do
    mkdir -p "$(dirname "$work/$f")"
    cp "$ROOT/$f" "$work/$f" 2>/dev/null || ok=0
  done
  [ "$ok" = 1 ] || { printf 'SKIP\n' > "$out"; return 0; }
  cp "$ROOT/$target" "$work/orig" 2>/dev/null || { printf 'SKIP\n' > "$out"; return 0; }
  if ! (cd "$work" && patch -p1 --silent --forward -i "$ROOT/$patch") >/dev/null 2>&1; then
    printf 'ROT\n' > "$out"; return 0
  fi
  # # An UNMUTATED tree yields no changed lines, and so does a legitimate
  # # addition-only patch. They are not the same thing.
  #
  # `patch` exits 0 on an EMPTY patch file having changed nothing (DESIGN-A6
  # section 43.14b), and this lane then diffs `orig` against a target identical
  # to it, finds no deleted-or-replaced run, and reports **SKIP** -- the same
  # verdict a real addition-only mutant gets. Absence is absorbed into a benign
  # category and the class reads as legitimately unaskable.
  #
  # The two are distinguishable in one comparison: an addition-only patch CHANGES
  # the target (it adds lines) while leaving the deleted set empty; an unapplied
  # one leaves the target byte-identical. Ask that before ascribing the skip.
  if cmp -s "$work/orig" "$work/$target"; then
    printf 'UNMUTATED\n' > "$out"; return 0
  fi
  lines=$(python3 -c '
import sys, difflib
a=open(sys.argv[1]).readlines(); b=open(sys.argv[2]).readlines()
gone=set()
for tag,i1,i2,_,_ in difflib.SequenceMatcher(None,a,b,autojunk=False).get_opcodes():
    if tag in ("delete","replace"): gone.update(range(i1+1,i2+1))
# The FIRST line of each contiguous deleted-or-replaced run: the point at which
# the mutation takes effect. See the header for why the interior lines are not
# askable.
print(" ".join(str(n) for n in sorted(gone) if n-1 not in gone))' "$work/orig" "$work/$target")
  if [ -z "$lines" ]; then printf 'SKIP\n' > "$out"; return 0; fi
  prof="$tmp/$name.cov"
  if ! $GO test -count=1 -timeout "$TEST_TIMEOUT" -run "^${test_name}\$" \
        -coverpkg="./$(dirname "$target")/" \
        -coverprofile="$prof" "$pkg" >"$tmp/$name.log" 2>&1; then
    printf 'ERROR\n' > "$out"; return 0
  fi
  miss=$(python3 -c '
import sys
prof, target = sys.argv[1], sys.argv[2]
want=set(int(x) for x in sys.argv[3:]); cov=set()
for line in open(prof):
    if line.startswith("mode:") or ":" not in line: continue
    fname, rest = line.rsplit(":",1)
    if not (fname.endswith("/"+target) or fname.endswith(target)): continue
    span,_st,cnt = rest.split()
    lo=int(span.split(",")[0].split(".")[0]); hi=int(span.split(",")[1].split(".")[0])
    if int(cnt)>0: cov.update(range(lo,hi+1))
print(",".join(map(str,sorted(want-cov))))' "$prof" "$target" $lines)
  printf 'DONE\t%s\n' "$miss" > "$out"
}

if [ "$JOBS" -gt 1 ]; then
  running=0
  for patch in sim/mutants/*.patch; do
    name=$(basename "$patch" .patch)
    if [ -n "$ONLY" ]; then
      case " $ONLY " in *" $name "*) ;; *) continue ;; esac
    fi
    : > "$tmp/$name.launched"
    cover_one "$patch" "$name" "$tmp/$name.result" &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then
      wait; running=0
      if overbudget; then
        printf '   BUDGET   the lane has spent %ss of its %ss and stopped.\n' \
          "$(( $(date +%s) - started ))" "$COVER_BUDGET"
        printf '            Everything after %s is UNCHECKED. This is a failure, not a\n' "$name"
        printf '            partial pass: a lane that truncates quietly reports a subset\n'
        printf '            nobody named as though it were the whole list.\n'
        budgetblown=$name
        break
      fi
    fi
  done
  wait
fi

# report_one prints one mutant's verdict from $name/$test_name/$expect/$miss.
# Shared by both paths so a parallel run and a sequential run print the same text.
report_one() {
  # # The canary is SUPPOSED to be mispointed
  #
  # `canary-mispointed` declares `expect: alive`: it names a covering test that
  # does not cover it, so that the mutant lane can prove it notices a mispointed
  # mutant. This lane rediscovered it independently on its first full run, which
  # is the strongest evidence available that the check works — and then it has to
  # be told, or the canary fails the lane for doing its job.
  #
  # So an expect:alive patch is REQUIRED to be uncovered. The exemption is
  # bidirectional: if the canary ever becomes covered, this fails and says the
  # canary has stopped being one.
  if [ "$expect" = "alive" ]; then
    if [ -z "$miss" ]; then
      printf '   BROKEN   %-44s the canary is COVERED by %s, so it no longer stands for a\n' \
        "$name" "$test_name"
      printf '              mispointed mutant and the lane it guards proves less than it claims\n'
      fail=$((fail + 1))
    else
      printf '   canary   %-44s correctly uncovered by %s\n' "$name" "$test_name"
    fi
    return 0
  fi

  if [ -z "$miss" ]; then
    printf '   ok       %-44s %s runs it\n' "$name" "$test_name"
  else
    printf '   DEAD     %-44s %s never executes %s:%s\n' "$name" "$test_name" "$target" "$miss"
    printf '              The test goes AROUND the path this mutant changes, so the mutation\n'
    printf '              cannot affect it. Route the test through the real call site.\n'
    fail=$((fail + 1))
  fi
}

for patch in sim/mutants/*.patch; do
  name=$(basename "$patch" .patch)
  if [ -n "$ONLY" ]; then
    case " $ONLY " in *" $name "*) ;; *) continue ;; esac
  fi
  if [ "$JOBS" -le 1 ] && overbudget; then
    printf '   BUDGET   the lane has spent %ss of its %ss and stopped at %s.\n' \
      "$(( $(date +%s) - started ))" "$COVER_BUDGET" "$name"
    printf '            Everything from here on is UNCHECKED, which is a failure.\n'
    budgetblown=$name
    break
  fi

  test_name=$(sed -n 's/^# covering-test:[[:space:]]*//p' "$patch" | head -1)
  pkg=$(sed -n 's/^# package:[[:space:]]*//p' "$patch" | head -1)
  target=$(sed -n 's|^--- a/||p' "$patch" | head -1)
  expect=$(sed -n 's/^# expect:[[:space:]]*//p' "$patch" | head -1)
  if [ -z "$test_name" ] || [ -z "$pkg" ] || [ -z "$target" ]; then
    skipped=$((skipped + 1)); continue
  fi
  if [ "$JOBS" -gt 1 ]; then
    # A patch the budget stopped us reaching is UNCHECKED, and saying so is the
    # whole point: reporting it as an ERROR would blame the patch for a decision
    # the budget made.
    if [ ! -f "$tmp/$name.launched" ]; then
      printf '   UNCHECK  %-44s not reached: the lane ran out of budget\n' "$name"
      unchecked=$((unchecked + 1)); continue
    fi
    # The work was done above; this loop only reports it, in patch order.
    status=$(cut -f1 "$tmp/$name.result" 2>/dev/null || echo ERROR)
    case "$status" in
      UNMUTATED)
        printf '   UNMUTATED %-44s the patch applied and the target is byte-identical.\n' "$name"
        printf '             patch(1) exits 0 on an empty patch, and this lane would otherwise\n'
        printf '             have recorded it as an addition-only SKIP -- absence absorbed into a\n'
        printf '             benign category (DESIGN-A6 section 43.14c).\n'
        fail=$((fail + 1)); continue ;;
      SKIP) skipped=$((skipped + 1)); continue ;;
      ROT)  printf '   ERROR    %-44s patch does not apply cleanly\n' "$name"
            fail=$((fail + 1)); continue ;;
      ERROR) printf '   ERROR    %-44s %s did not run\n' "$name" "$test_name"
            tail -3 "$tmp/$name.log" 2>/dev/null | sed 's/^/              /'
            fail=$((fail + 1)); continue ;;
    esac
    checked=$((checked + 1))
    miss=$(cut -f2- "$tmp/$name.result" 2>/dev/null || true)
    report_one; continue
  fi

  # # ONE apply-and-diff path, because the duplicate swallowed a guard
  #
  # This loop used to carry its own inline copy of `cover_one`'s logic for the
  # sequential case. That is the shape DESIGN-A6 §43.9d found in
  # `power-mutants.sh`, where the sweep detector was taught to the shared helper
  # and the inline copy silently did not have it, leaving the lane unable to fire
  # the detector in its DEFAULT mode for a full cycle.
  #
  # It then happened again HERE, an hour after that was written up: the
  # UNMUTATED guard was added to `cover_one`, the induction reported `1 skipped`
  # instead of `UNMUTATED`, and the reason was that sequential mode was running
  # the other copy. **A shape that has produced two silent failures will produce
  # a third**, so the copy is gone rather than guarded: `cover_one` measures,
  # both modes read its output, and anything added later lands in both by
  # construction.
  #
  # `TestOneApplyPath` in ./cmd/simctl/ asserts there is exactly one call site.
  cover_one "$patch" "$name" "$tmp/$name.result"
  status=$(cut -f1 "$tmp/$name.result" 2>/dev/null || echo ERROR)
  case "$status" in
    UNMUTATED)
      printf '   UNMUTATED %-44s the patch applied and the target is byte-identical.\n' "$name"
      printf '             patch(1) exits 0 on a hunk that changes nothing, and this lane would\n'
      printf '             otherwise record it as an addition-only SKIP (DESIGN-A6 §43.14c).\n'
      fail=$((fail + 1)); continue ;;
    SKIP) skipped=$((skipped + 1)); continue ;;
    ROT)  printf '   ERROR    %-44s patch does not apply cleanly\n' "$name"
          fail=$((fail + 1)); continue ;;
    ERROR) printf '   ERROR    %-44s %s did not run\n' "$name" "$test_name"
          tail -3 "$tmp/$name.log" 2>/dev/null | sed 's/^/              /'
          fail=$((fail + 1)); continue ;;
  esac
  checked=$((checked + 1))
  miss=$(cut -f2- "$tmp/$name.result" 2>/dev/null || true)
  report_one
done

printf '  ----------------------------------------------------------------\n'
printf '   %d checked, %d skipped, %d unchecked, %d dead, %ss of %ss budget\n' \
  "$checked" "$skipped" "$unchecked" "$fail" "$(( $(date +%s) - started ))" "$COVER_BUDGET"
if [ -n "$budgetblown" ]; then
  printf '   OVER BUDGET: stopped at %s. The verdicts above are a SUBSET.\n\n' "$budgetblown"
  exit 1
fi
printf '\n'
[ "$fail" -eq 0 ] || exit 1
