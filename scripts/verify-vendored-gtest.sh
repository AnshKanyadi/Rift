#!/usr/bin/env sh
# Provenance check for the vendored GoogleTest tree. REACHES THE NETWORK.
#
# This answers the question the offline lane cannot: are the bytes in
# third_party/googletest the bytes upstream published at the pinned commit?
# It fetches that commit from GitHub and compares upstream's tree hash against
# the hash recorded in third_party/googletest/VERSION -- which the offline lane
# (scripts/cpp-vendor-check.sh) has separately proven the working tree matches.
#
# IT IS DELIBERATELY NOT A LANE. Ruled in DESIGN-B1 section 9.2: putting a
# network call in a lane reintroduces exactly the dependency vendoring exists to
# remove, and it would fail in the situation the vendoring is for -- a stranger
# reproducing our numbers from a clean clone, offline. So this is a one-time
# check, run on purpose, by whoever wants to check our work. It is written to be
# runnable by someone who did not do the vendoring and does not trust it.
#
# usage: verify-vendored-gtest.sh
set -eu

meta=third_party/googletest/VERSION
upstream=$(sed -n 's/^upstream: *//p' "$meta" | head -1)
tag=$(sed -n 's/^tag: *//p' "$meta" | head -1)
commit=$(sed -n 's/^commit: *//p' "$meta" | head -1)
recorded=$(sed -n 's/^tree: *//p' "$meta" | head -1)

printf '\n  vendored-tree provenance (NETWORK)\n'
printf '  ----------------------------------------------------------\n'
printf '   upstream : %s\n' "$upstream"
printf '   tag      : %s\n' "$tag"
printf '   commit   : %s\n' "$commit"
printf '   recorded : %s\n' "$recorded"
printf '   fetching the pinned commit...\n'

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

GIT_CONFIG_GLOBAL=/dev/null
GIT_CONFIG_SYSTEM=/dev/null
export GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM

git init -q "$scratch/up"
git -C "$scratch/up" remote add origin "$upstream"
if ! git -C "$scratch/up" fetch -q --depth 1 origin "$commit"; then
  printf '   FAIL  could not fetch %s from %s\n\n' "$commit" "$upstream"
  exit 1
fi

up_tree=$(git -C "$scratch/up" rev-parse "$commit^{tree}")
printf '   upstream tree : %s\n' "$up_tree"

# The tag must also point at the pinned commit, or "v1.17.0" in the record is a
# label nobody checked. A lightweight tag resolves directly; an annotated one
# resolves through ^{commit}.
tag_line=$(git -C "$scratch/up" ls-remote --tags origin "$tag" | head -1 || true)
tag_sha=$(printf '%s' "$tag_line" | cut -f1)
if [ -n "$tag_sha" ]; then
  printf '   remote tag %s -> %s\n' "$tag" "$tag_sha"
else
  printf '   WARN  remote does not publish tag %s\n' "$tag"
fi

rc=0
if [ "$up_tree" != "$recorded" ]; then
  printf '  ----------------------------------------------------------\n'
  printf '   FAIL  upstream tree hash != the hash recorded in VERSION.\n'
  printf '         The pin is wrong, or the record is.\n'
  rc=1
fi
if [ -n "$tag_sha" ] && [ "$tag_sha" != "$commit" ]; then
  printf '   FAIL  tag %s points at %s, not the pinned commit.\n' "$tag" "$tag_sha"
  rc=1
fi

if [ $rc -ne 0 ]; then printf '\n'; exit 1; fi

printf '  ----------------------------------------------------------\n'
printf '   ok  upstream tree hash matches the recorded hash\n'
printf '\n  Run scripts/cpp-vendor-check.sh (offline) for the other half: that the\n'
printf '  working tree still matches this record. Both together are the claim.\n\n'
