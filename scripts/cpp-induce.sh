#!/bin/sh
# INDUCE ONE MUTANT AND READ THE FAILING ASSERTION. The label in FLOORS.txt is
# DETERMINED this way and never inferred from what a patch says it blinds.
#
# ---------------------------------------------------------------------------
# WHY THE REVERT IS `git apply -R` AND NEVER `git checkout -- <dir>`.
#
# The first version of this script undid a patch with `git checkout --
# engine-cpp/src`, which reverts EVERYTHING UNCOMMITTED under that path. It ate
# an unrelated comment once (recorded as HARNESS-016's second instance), and
# then -- after that entry was written -- IT ATE AN ENTIRE STEP'S UNCOMMITTED
# SOURCE, because the entry fixed the record and not the tool.
#
#   `git apply -R` is the exact inverse of the apply. Its side effect is no
#   wider than its purpose, which is the rule HARNESS-016 names.
#
# It also refuses to run on a dirty tree at all, because reading an induction
# on top of uncommitted work tells you which assertion fails IN A TREE NOBODY
# WILL EVER BUILD AGAIN.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

if [ -n "$(git status --porcelain -- engine-cpp/src engine-cpp/rig)" ]; then
  echo "REFUSING: engine-cpp/src or engine-cpp/rig has uncommitted changes."
  echo "An induction reads which assertion fails. On a dirty tree that answer"
  echo "is about a tree that will never exist again -- and a failed patch would"
  echo "leave the work half-reverted. Commit first."
  exit 2
fi

for id in "$@"; do
  patch="engine-cpp/mutants/$id.patch"
  printf '\n===== %s =====\n' "$id"
  if [ ! -f "$patch" ]; then echo "NO SUCH PATCH"; continue; fi
  if ! git apply "$patch"; then echo "APPLY FAILED (patch has rotted)"; continue; fi
  if make cpp-build >/dev/null 2>&1; then
    out=$(./engine-cpp/build/test/rift_engine_test 2>&1 || true)
    failed=$(printf '%s\n' "$out" | grep -E '^\[  FAILED  \] [A-Za-z]' | sort -u || true)
    if [ -n "$failed" ]; then
      printf '%s\n' "$failed"
    elif printf '%s\n' "$out" | grep -q 'RIFT PARTIAL RUN'; then
      # AN ABORT IS A KILL AND THE SUITE PRINTS NO FAILING TEST -- FLOORS.txt's
      # header warns of exactly this, and the first induction of BM78 read it as
      # a survival until the marker was looked for.
      printf 'KILLED BY A GUARD (abort, no failing test):\n'
      printf '%s\n' "$out" | grep -E 'CHECK failed|RIFT PARTIAL RUN' | head -3
    else
      echo "NO TEST FAILED AND NO ABORT -- SURVIVED"
    fi
  else
    echo "BUILD FAILED (the control lane would be killed)"
  fi
  git apply -R "$patch"
done
make cpp-build >/dev/null 2>&1
echo
echo "tree restored"
