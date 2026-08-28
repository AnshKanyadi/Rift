#!/usr/bin/env sh
# Vendored-tree integrity, entirely offline.
#
# Recomputes the git tree hash of the vendored GoogleTest directory and requires
# it to equal the hash recorded in that directory's VERSION file. A local edit
# to a vendored dependency then reddens a lane with a name on it, instead of
# surfacing weeks later as a build that behaves differently here than anywhere
# else.
#
# This check makes NO network call and must never acquire one. The provenance
# question it does not answer -- are these bytes the bytes upstream published --
# is answered once, on purpose, by scripts/verify-vendored-gtest.sh, which does
# reach the network and is deliberately not a lane. DESIGN-B1 section 9.2.
#
# The recomputation is isolated from the developer's git configuration
# (GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are neutralized) because a global
# core.autocrlf or a global excludes file would otherwise change the answer, and
# a check whose result depends on who is running it is not a check.
#
# usage: cpp-vendor-check.sh [vendored-dir]
#   The directory argument exists so the induced failure can run this exact code
#   against a scratch copy. Default: third_party/googletest.
set -eu

dir=${1:-third_party/googletest}
meta=$dir/VERSION

if [ ! -d "$dir" ]; then
  printf '\n  FAIL  vendored directory missing: %s\n\n' "$dir"
  exit 1
fi
if [ ! -f "$meta" ]; then
  printf '\n  FAIL  provenance record missing: %s\n\n' "$meta"
  exit 1
fi

recorded=$(sed -n 's/^tree: *//p' "$meta" | head -1)
commit=$(sed -n 's/^commit: *//p' "$meta" | head -1)
tag=$(sed -n 's/^tag: *//p' "$meta" | head -1)
excluded=$(sed -n 's/^exclude-from-hash: *//p' "$meta" | head -1)

if [ -z "$recorded" ]; then
  printf '\n  FAIL  %s records no "tree:" hash to check against.\n\n' "$meta"
  exit 1
fi
if [ "$excluded" != "VERSION" ]; then
  printf '\n  FAIL  %s: exclude-from-hash must be exactly "VERSION", got "%s".\n' "$meta" "$excluded"
  printf '        The exclusion set is one hard-coded name on purpose. Widening it\n'
  printf '        is how a vendored tree stops being the upstream tree quietly.\n\n'
  exit 1
fi

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

GIT_CONFIG_GLOBAL=/dev/null
GIT_CONFIG_SYSTEM=/dev/null
export GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM

git init -q "$scratch/probe"
tar cf - -C "$dir" . | (cd "$scratch/probe" && tar xf -)
rm -f "$scratch/probe/VERSION"

# -f because the vendored tree carries its own .gitignore, and an ignored file
# that upstream tracks must still be hashed or the equality is against a subset.
( cd "$scratch/probe" && git add -A -f . >/dev/null )
computed=$( cd "$scratch/probe" && git write-tree )
files=$( cd "$scratch/probe" && git ls-files | wc -l | tr -d ' ' )

printf '\n  vendored-tree integrity (offline)\n'
printf '  ----------------------------------------------------------\n'
printf '   dir      : %s\n' "$dir"
printf '   tag      : %s\n' "$tag"
printf '   commit   : %s\n' "$commit"
printf '   files    : %s (excluding VERSION)\n' "$files"
printf '   recorded : %s\n' "$recorded"
printf '   computed : %s\n' "$computed"

if [ "$computed" != "$recorded" ]; then
  printf '  ----------------------------------------------------------\n'
  printf '   FAIL  the vendored tree is not the tree that was recorded.\n\n'
  printf '  Something under %s changed since vendoring.\n' "$dir"
  printf '  Do not update the recorded hash to match the tree. The hash is the\n'
  printf '  claim; the tree is what is being checked against it. Restore the tree,\n'
  printf '  or re-vendor deliberately and record the new commit as a new pin.\n\n'
  exit 1
fi

printf '  ----------------------------------------------------------\n'
printf '   ok  tree matches its recorded hash\n\n'
