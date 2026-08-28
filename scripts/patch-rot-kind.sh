#!/bin/sh
# patch-rot-kind.sh -- say WHY a mutant patch stopped applying.
#
# # Why this exists
#
# `power-refute` and `mutants.sh` both printed one sentence for every failure:
#
#     ROT  <id>: patch no longer applies; the code moved and the mutation did not
#
# At the A7/B5 merge that sentence was **false for one of the two patches it was
# printed for**. `M77`'s target line was byte-identical and still is; three
# COMMENT lines above it had been rewritten. A reader acting on that message goes
# looking for a behavioural change that never happened, and the two causes need
# different remedies:
#
#     ANCHOR DRIFT      the mutation site survives; the prose around it moved.
#                       Remedy: re-anchor the patch. Nothing about the code is
#                       wrong and nothing needs re-deciding.
#     STRUCTURAL DRIFT  the code the patch matched is genuinely gone.
#                       Remedy: regenerate the patch, and first ask whether the
#                       class still exists at all -- a mutant whose site vanished
#                       may be a mutant with nothing left to claim.
#
# # The discriminator, and it is cheap
#
# ANCHOR DRIFT iff every NON-COMMENT line the hunk matched on -- its removed
# lines and its code context -- is still present in the file, AND at least one
# COMMENT context line is not. That is exactly "the code is all still there and
# the prose is not".
#
# Anything else is STRUCTURAL. The asymmetry is deliberate: structural is the
# default, because it is the answer that makes someone look at the code.
#
# Usage:  scripts/patch-rot-kind.sh <patch> [repo-root]
#         scripts/patch-rot-kind.sh --self-test
set -eu

classify() {
  patch_file=$1; root=${2:-.}
  python3 - "$patch_file" "$root" <<'PY'
import sys, os, re
patch, root = sys.argv[1], sys.argv[2]

def is_comment(s, ext):
    s = s.strip()
    if not s: return False
    if ext in ('.go', '.cc', '.cpp', '.h', '.hh'):
        return s.startswith('//') or s.startswith('/*') or s.startswith('*')
    if ext in ('.sh', '.py', '.txt', '.yml', '.yaml', ''):
        return s.startswith('#')
    return False

target, ext, code, prose = None, '', [], []
for raw in open(patch):
    line = raw.rstrip('\n')
    if line.startswith('--- a/'):
        target = line[len('--- a/'):].strip()
    elif line.startswith('+++ '):
        f = line.split()
        ext = os.path.splitext(f[1])[1] if len(f) > 1 else ''
    elif line.startswith('@@') or line.startswith('#'):
        continue
    elif line[:1] in (' ', '-') and target:
        body = line[1:]
        if not body.strip():
            continue
        (prose if is_comment(body, ext) else code).append((target, body))

def present(rel, text):
    try:
        src = open(os.path.join(root, rel)).read()
    except OSError:
        return False
    return any(l.rstrip('\n') == text for l in src.split('\n'))

code_all_there  = all(present(r, t) for r, t in code) and bool(code)
prose_missing   = any(not present(r, t) for r, t in prose)
print('ANCHOR' if (code_all_there and prose_missing) else 'STRUCTURAL')
PY
}

if [ "${1:-}" = "--self-test" ]; then
  # INDUCTION. Two planted rots, one of each kind, and the classifier must tell
  # them apart. Without this the classifier is a claim; with it, it is checked.
  d=$(mktemp -d); trap 'rm -rf "$d"' EXIT
  mkdir -p "$d/pkg"
  cat > "$d/pkg/f.go" <<'GO'
package pkg

// REWRITTEN PROSE, entirely different words from what the patch matched on.
// Second line, also rewritten.
// Third line, also rewritten.
func target() bool { return true }
GO
  cat > "$d/anchor.patch" <<'P'
--- a/pkg/f.go
+++ b/pkg/f.go
@@ -3,4 +3,4 @@
 // original prose one
 // original prose two
 // original prose three
-func target() bool { return true }
+func target() bool { return false }
P
  cat > "$d/structural.patch" <<'P'
--- a/pkg/f.go
+++ b/pkg/f.go
@@ -3,4 +3,4 @@
 // REWRITTEN PROSE, entirely different words from what the patch matched on.
-func vanished() bool { return true }
+func vanished() bool { return false }
P
  fail=0
  a=$(classify "$d/anchor.patch" "$d")
  s=$(classify "$d/structural.patch" "$d")
  [ "$a" = "ANCHOR" ]     || { printf '  self-test: prose-only rot classified %s, want ANCHOR\n' "$a"; fail=1; }
  [ "$s" = "STRUCTURAL" ] || { printf '  self-test: vanished-code rot classified %s, want STRUCTURAL\n' "$s"; fail=1; }
  [ "$fail" -eq 0 ] && printf '  patch-rot-kind self-test: ANCHOR and STRUCTURAL told apart\n'
  exit "$fail"
fi

classify "$1" "${2:-.}"
