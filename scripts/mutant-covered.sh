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
# test on the UNMUTATED tree with coverage on, and require the lines the patch
# touches to be covered. A test that goes around the path leaves them at zero.
#
# It cannot be satisfied by claiming an entry point, because coverage is produced
# by execution rather than by assertion. That is what makes it mechanical rather
# than remembered.
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
set -eu

GO=${GO:-go}
ONLY=${1:-}
ROOT=$(pwd)

printf '\n  mutant coverage: every covering test executes the line its mutant changes\n'
printf '  ----------------------------------------------------------------\n'

fail=0; checked=0; skipped=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

for patch in sim/mutants/*.patch; do
  name=$(basename "$patch" .patch)
  [ -z "$ONLY" ] || [ "$ONLY" = "$name" ] || continue

  test_name=$(sed -n 's/^# covering-test:[[:space:]]*//p' "$patch" | head -1)
  pkg=$(sed -n 's/^# package:[[:space:]]*//p' "$patch" | head -1)
  target=$(sed -n 's|^--- a/||p' "$patch" | head -1)
  expect=$(sed -n 's/^# expect:[[:space:]]*//p' "$patch" | head -1)
  if [ -z "$test_name" ] || [ -z "$pkg" ] || [ -z "$target" ]; then
    skipped=$((skipped + 1)); continue
  fi
  # Every file the patch names is copied, not only the first: a multi-file patch
  # applied against a partial copy fails, and the failure looks like a rotted
  # patch rather than a lane that did not lay the ground out.
  work="$tmp/$name"
  ok=1
  for f in $(sed -n 's|^--- a/||p' "$patch"); do
    mkdir -p "$(dirname "$work/$f")"
    cp "$ROOT/$f" "$work/$f" 2>/dev/null || ok=0
  done
  [ "$ok" = 1 ] || { skipped=$((skipped+1)); continue; }
  cp "$ROOT/$target" "$work/orig" 2>/dev/null || { skipped=$((skipped+1)); continue; }
  if ! (cd "$work" && patch -p1 --silent --forward -i "$ROOT/$patch") >/dev/null 2>&1; then
    printf '   ERROR    %-44s patch does not apply cleanly\n' "$name"
    fail=$((fail + 1)); continue
  fi

  lines=$(python3 -c '
import sys, difflib
a=open(sys.argv[1]).readlines(); b=open(sys.argv[2]).readlines()
out=[]
for tag,i1,i2,_,_ in difflib.SequenceMatcher(None,a,b,autojunk=False).get_opcodes():
    if tag in ("delete","replace"): out.extend(range(i1+1,i2+1))
print(" ".join(map(str,out)))' "$work/orig" "$work/$target")

  if [ -z "$lines" ]; then
    skipped=$((skipped + 1)); continue   # addition-only: no original line to cover
  fi

  checked=$((checked + 1))
  prof="$tmp/$name.cov"
  if ! $GO test -count=1 -run "^${test_name}\$" -coverpkg="./$(dirname "$target")/" \
        -coverprofile="$prof" "$pkg" >"$tmp/$name.log" 2>&1; then
    printf '   ERROR    %-44s %s did not run\n' "$name" "$test_name"
    tail -3 "$tmp/$name.log" | sed 's/^/              /'
    fail=$((fail + 1)); continue
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
    continue
  fi

  if [ -z "$miss" ]; then
    printf '   ok       %-44s %s runs it\n' "$name" "$test_name"
  else
    printf '   DEAD     %-44s %s never executes %s:%s\n' "$name" "$test_name" "$target" "$miss"
    printf '              The test goes AROUND the path this mutant changes, so the mutation\n'
    printf '              cannot affect it. Route the test through the real call site.\n'
    fail=$((fail + 1))
  fi
done

printf '  ----------------------------------------------------------------\n'
printf '   %d checked, %d skipped, %d dead\n\n' "$checked" "$skipped" "$fail"
[ "$fail" -eq 0 ] || exit 1
