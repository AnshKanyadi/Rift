#!/usr/bin/env sh
# citations.sh -- every commit this repository cites about itself must be ON main.
#
# # Why this exists
#
# The records cite SHAs as evidence: BUGS.md names the fix commit for each defect,
# the design docs name the commit a decision landed in, every bundle's meta.json
# names the commit its trace was recorded at, and every phase tag names the commit
# that closed the phase. Those citations are the mechanism behind "every defect
# replays" and "this number came from that run".
#
# **Nothing checked any of them.** A history rewrite changes every SHA, and the
# only thing standing between that and 87 dangling citations was somebody
# remembering to look. That is the property this lane removes -- see
# docs/CARRY-FORWARD.md, where building it is the stated precondition for ever
# rewriting history again.
#
# # What counts as a citation, and why the test is RESOLUTION plus ANCESTRY
#
# A 7-or-more-character hex token is a citation if `git cat-file -e <t>^{commit}`
# succeeds. A token that resolves to nothing is not a citation -- trace hashes,
# step hashes, seeds and vendored tree hashes all live in these files and none of
# them is a claim about this history.
#
# Then the actual question, which is NOT whether it resolves:
#
#     git merge-base --is-ancestor <t> HEAD
#
# **RESOLUTION IS NOT EXISTENCE** (GF-69). An object stays readable while anything
# references it, and a stale tag or a reflog entry is exactly such a thing, so a
# dangling citation is self-sustaining evidence for its own validity. It answers
# every question except the one that matters: is it on the branch we ship. Both
# tags survived a discarded rewrite looking perfectly healthy, and the check that
# had been named -- "confirm the tags still resolve" -- would have passed.
#
# # Three populations, because they dangle independently
#
#   PROSE   hex tokens in tracked .md/.txt/.patch/.sh/.yml files
#   BUNDLE  the `commit` field of every seeds/*/meta.json, read structurally
#   TAG     the commit every tag dereferences to
#
# # There is no exemption for prose that is ABOUT a dead commit
#
# A record saying "the rewrite produced X, which no longer exists" is a true
# sentence and a dangling citation, and this lane fails it. That is deliberate:
# an exemption for illustrative SHAs is an exemption the next dangling citation
# will claim. Describe the reference by its ROLE instead -- "a mutant patch
# header's commit reference", "the bundle's recorded commit" -- which is what the
# reader needs anyway, since the SHA is the part that stopped meaning anything.
#
# This is not hypothetical. GF-69, the entry about references that resolve into
# discarded history, was written citing a SHA the restore had discarded, and this
# lane caught it on its first run against a tree a hand audit had already passed.
#
# Bundle metas are read as JSON rather than scanned: each carries thousands of
# step hashes, and scanning them costs minutes to learn nothing. Tags are their
# own population because `filter-branch` rewrites them in place and backs up only
# the branch (GF-69), so they are the population most likely to be silently wrong
# after exactly the operation this lane exists for.
#
# usage:  scripts/citations.sh [--self-test]
set -eu

ROOT=$(pwd)

scan() {
  bad=0
  checked=0

  # ---- PROSE
  # third_party/ is excluded by name: it is vendored, its hashes are upstream's,
  # and a claim about another repository's history is not a claim about ours.
  files=$(git ls-files '*.md' '*.txt' '*.patch' '*.sh' '*.yml' '*.yaml' | grep -v '^third_party/' || true)
  for f in $files; do
    [ -f "$f" ] || continue
    for t in $(grep -ohE '\b[0-9a-f]{7,40}\b' "$f" 2>/dev/null | sort -u); do
      case $(classify "$t") in
        notacommit) continue ;;
        ambiguous)
          printf '   AMBIGUOUS prose   %-40s %s resolves to more than one object\n' "$f" "$t"
          bad=$((bad + 1)); continue ;;
      esac
      checked=$((checked + 1))
      if ! git merge-base --is-ancestor "$t" HEAD 2>/dev/null; then
        printf '   DANGLING  prose   %-40s %s\n' "$f" "$t"
        bad=$((bad + 1))
      fi
    done
  done

  # ---- BUNDLE
  for m in seeds/*/meta.json; do
    [ -f "$m" ] || continue
    c=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('commit',''))" "$m")
    [ -n "$c" ] || continue
    checked=$((checked + 1))
    if ! git merge-base --is-ancestor "$c" HEAD 2>/dev/null; then
      printf '   DANGLING  bundle  %-40s %s\n' "$m" "$c"
      bad=$((bad + 1))
    fi
  done

  # ---- TAG
  for t in $(git for-each-ref --format='%(refname:short)' refs/tags/); do
    c=$(git rev-list -n1 "$t")
    checked=$((checked + 1))
    if ! git merge-base --is-ancestor "$c" HEAD 2>/dev/null; then
      printf '   DANGLING  tag     %-40s %s\n' "$t" "$(echo "$c" | cut -c1-12)"
      bad=$((bad + 1))
    fi
  done

  echo "$checked $bad" > "$scratch/counts"
  return 0
}

# # AMBIGUOUS is not the same answer as NOT A COMMIT, and one exit code says both
#
# `git cat-file -e <t>^{commit}` fails identically for "no such object" and for
# "that prefix matches several objects". A lane that treats both as *not a
# citation* SILENTLY SKIPS the ambiguous one -- and an ambiguous citation is
# strictly worse than a dangling one, because it reads as fine and resolves to
# whichever object the reader's repository happens to disambiguate to.
#
# So the two are told apart on stderr and ambiguity FAILS the lane. A citation
# that does not name one commit is not a citation.
#
# Reachability, stated rather than implied: at 821 commit objects here, no two
# share a 7-character prefix, so this branch does not fire on this repository
# today. It is a latent hole rather than a live one, and it is closed now because
# the population only grows and because the skip would have been silent.
classify() {
  if git rev-parse --verify --quiet "${1}^{commit}" >/dev/null 2>&1; then
    echo commit; return
  fi
  # NO --quiet on this one. --quiet suppresses the very message being matched, so
  # the ambiguous case comes back with empty stderr and reads as not-a-commit --
  # which is exactly the silent skip this function exists to remove. The lane's
  # own self-test caught it on its first run.
  err=$(git rev-parse --verify "${1}^{commit}" 2>&1 >/dev/null) || true
  case "$err" in
    *ambiguous*) echo ambiguous ;;
    *)           echo notacommit ;;
  esac
}

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

# # The self-test, both directions
#
# A lane that has only ever been seen green is a lane nobody has watched fail.
# The plant is a DANGLING COMMIT: `git commit-tree` writes a real commit object
# with no ref pointing at it, so it resolves and is not an ancestor of anything --
# which is precisely the state a rewritten-away citation is left in, produced
# without touching the repository's refs.
if [ "${1:-}" = "--self-test" ]; then
  printf '\n  citations: self-test\n'
  printf '  ----------------------------------------------------------------\n'

  orphan=$(git commit-tree "$(git rev-parse HEAD^{tree})" -m 'citations.sh self-test: a commit on no branch' < /dev/null)
  short=$(echo "$orphan" | cut -c1-7)

  git cat-file -e "${orphan}^{commit}" || { printf '   the plant does not resolve; the fixture is wrong\n'; exit 2; }
  if git merge-base --is-ancestor "$orphan" HEAD 2>/dev/null; then
    printf '   the plant IS an ancestor; the fixture is wrong\n'; exit 2
  fi
  printf '   plant %s resolves and is not an ancestor -- the state a rewrite leaves\n' "$short"

  plant="CITATIONS-SELFTEST.md"
  [ -e "$plant" ] && { printf '   %s already exists; refusing to overwrite\n' "$plant"; exit 2; }
  printf 'self-test fixture, deleted by the lane. Cites %s\n' "$short" > "$plant"
  # Untracked files are invisible to `git ls-files`, so the plant must be staged
  # for the scan to see it -- which is itself the shape of a vacuous test.
  git add -N "$plant" >/dev/null 2>&1 || true
  scan > "$scratch/planted.out" 2>&1 || true
  git rm -q --cached "$plant" >/dev/null 2>&1 || true
  rm -f "$plant"
  set -- $(cat "$scratch/counts")
  if [ "${2:-0}" -lt 1 ]; then
    printf '   FIRES ON ITS PLANT: NO. The lane passed a tree citing a non-ancestor.\n'
    cat "$scratch/planted.out"
    exit 1
  fi
  printf '   fires on its plant: yes, %d dangling\n' "$2"

  scan > "$scratch/clean.out" 2>&1 || true
  set -- $(cat "$scratch/counts")
  if [ "${2:-0}" -ne 0 ]; then
    printf '   SILENT ON A CLEAN TREE: NO. %d dangling with the plant removed:\n' "$2"
    cat "$scratch/clean.out"
    exit 1
  fi
  printf '   silent on a clean tree: yes, %d citations checked\n' "$1"

  # # The third branch, and what this induction does NOT prove
  #
  # A 4-character prefix is ambiguous among 821 commit objects where a
  # 7-character one is not, so `classify` can be driven onto its ambiguous arm
  # directly. That proves THE CODE PATH. It does not prove the MECHANISM -- only
  # a real 7-character collision arriving through the scan would, and this
  # repository cannot produce one at its size. The distinction is I2's, about its
  # chaos gates, and it is written here rather than left for a reader to assume
  # the stronger claim.
  amb=$(git rev-parse HEAD | cut -c1-4)
  got=$(classify "$amb")
  if [ "$got" != "ambiguous" ]; then
    printf '   TELLS AMBIGUOUS FROM NOT-A-COMMIT: NO. classify(%s) said %s\n' "$amb" "$got"
    exit 1
  fi
  printf '   tells ambiguous from not-a-commit: yes, on the code path (see the note)\n'
  if [ "$(classify zzzzzzz)" != "notacommit" ] || [ "$(classify "$(git rev-parse HEAD | cut -c1-12)")" != "commit" ]; then
    printf '   ...but misclassifies a plain non-commit or a plain commit\n'
    exit 1
  fi
  printf '   and still calls a non-commit a non-commit, and a commit a commit\n'
  printf '  ----------------------------------------------------------------\n'
  printf '   citations self-test: three branches\n\n'
  exit 0
fi

printf '\n  citations: every commit this repository cites is on main\n'
printf '  ----------------------------------------------------------------\n'
scan
set -- $(cat "$scratch/counts")
printf '  ----------------------------------------------------------------\n'
printf '   %d citations checked (prose + bundle metas + tags), %d dangling\n\n' "$1" "$2"
[ "$2" -eq 0 ] || exit 1
