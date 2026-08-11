#!/usr/bin/env sh
# Assert that golang.org/x/tools never reaches a shipping binary.
#
# DESIGN-A0 Q4 approved the dependency for the determinism vet pass on the
# condition that it is tooling only, "enforced by a CI check that the dependency
# does not appear in any cmd/ binary's build graph". This is that check.
#
# It is a build-graph check rather than a grep, so an import three packages deep
# fails it exactly as loudly as a direct one.
set -eu

GO=${GO:-go}
BANNED=${BANNED:-golang.org/x/tools}

fail=0

# Every main package under cmd/ -- the shipping binaries.
for pkg in $($GO list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./cmd/... 2>/dev/null); do
  if $GO list -deps "$pkg" 2>/dev/null | grep -q "^$BANNED"; then
    printf '  FAIL %s links %s\n' "$pkg" "$BANNED"
    printf '       path: '
    $GO list -deps "$pkg" | grep "^$BANNED" | head -3
    fail=1
  else
    printf '  ok   %s\n' "$pkg"
  fi
done

# Belt and braces: nothing outside tools/ may depend on it either, so the
# check keeps meaning something before cmd/ has grown its second binary.
for pkg in $($GO list ./... 2>/dev/null | grep -v '/tools/'); do
  if $GO list -deps "$pkg" 2>/dev/null | grep -q "^$BANNED"; then
    printf '  FAIL %s depends on %s outside tools/\n' "$pkg" "$BANNED"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  printf '\n%s is approved as a tooling-only dependency (DESIGN-A0 Q4).\n' "$BANNED"
  printf 'Move the code that needs it under tools/, or ask for the ruling to be revisited.\n'
  exit 1
fi

printf '  ok   no shipping binary links %s\n' "$BANNED"
