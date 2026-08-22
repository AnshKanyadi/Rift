#!/usr/bin/env sh
# No patch leftovers, and no second copy of a source file, in the tree.
#
# # Why this exists
#
# `patch` writes `<file>.orig` when it applies with fuzz and `<file>.rej` when a
# hunk fails, and this project applies patches constantly: every mutant in the
# suite, every corpus reproduction, every power measurement. A `git add -A` while
# one is applied commits the litter.
#
# That has happened. A5 recorded the process error -- a mutant patch committed by
# `git add -A` -- and the mutant was caught and reverted. What was not caught is
# that THREE `.orig` files had already been committed, at A1, A2 and A3, and had
# been sitting in the tree ever since: a hundred kilobytes of stale duplicate
# source, including a whole second copy of `raft/raft.go` frozen at A3.
#
# Nothing looked, because nothing was looking. Go ignores a non-`.go` extension,
# so it never failed a build; a reader opening `raft.go.orig` four phases from now
# would have found a plausible-looking Raft implementation that is silently a
# phase and a half out of date.
set -eu

bad=$(git ls-files | grep -E '\.(orig|rej)$' || true)
if [ -n "$bad" ]; then
  printf '\n  TRACKED PATCH LEFTOVERS:\n'
  printf '%s\n' "$bad" | sed 's/^/    /'
  printf '\n  These are what `patch` leaves behind, committed by a `git add -A` while a mutant\n'
  printf '  was applied. A .orig is a stale duplicate of a real source file; it never fails a\n'
  printf '  build and it rots in place. Remove them.\n\n'
  exit 1
fi
printf '  hygiene: no tracked patch leftovers\n'
