#!/usr/bin/env sh
# The scope scan: the half of Amendment A5 the Env seam cannot see.
#
# B1-D11, ruled. Env catches syscalls by construction. It is structurally blind
# to a `double`, a `rand()`, a `steady_clock::now()`, a pointer-keyed container,
# or a raw ::open that never went through it -- and the answer to "how do you
# know a steady_clock::now() didn't sneak in?" must be a BUILD FAILURE, not a
# promise.
#
# FOUR PARTS, each able to fail on its own:
#
#   1. the Env surface correspondence            scripts/cpp-scan-surface.sh
#   2. the A5 rules, the PosixEnv thinness rules, and (2b) the ARTIFACT/BELIEF
#      boundary: what an oracle may parse and what it may never consult
#   3. CPP-HATCHES.txt reconciled against what part 2 actually found
#   4. CLAIMS.txt -- the sentences this lane is what makes true
#   5. DECIDERS.txt -- every function that decides evidentiary status, asserted
#      in BOTH directions
#   6. FLOORS.txt -- every mutant class has a standing measurement (CF-2)
#
# EVERY OPT-OUT CARRIES A SPLIT LABEL, FROM THE START. An entry in
# CPP-HATCHES.txt is either
#
#     covered-by: <instrument>     the class this rule catches is caught there
#                                  instead, by something that actually exists
#
#     unreachable: <detector> | <argument>
#                                  the thing cannot occur here, <detector> would
#                                  have seen it, and no other detector sees the
#                                  class more often
#
# and never a free-text reason. Track A spent a full cycle refuting seventeen
# single-labelled opt-outs and found three of them wrong, one a first-tier
# safety defect. A single label lets "this is fine" and "nothing checks this"
# wear the same clothes; the split makes the second one say so.
#
# usage: cpp-scan.sh [--fixtures] [engine-cpp-dir]
set -eu

FIXTURES=no
ONE_FIXTURE=""
if [ "${1:-}" = "--fixtures" ]; then
  FIXTURES=yes; shift
  # A single fixture may be named, which is what lets the blind lane declare a
  # patch against a fixture that does NOT cover it and require it to survive.
  # Without per-fixture granularity every blinding kills every canary.
  case "${1:-}" in
    *.fixture) ONE_FIXTURE=$1; shift ;;
  esac
fi
dir=${1:-engine-cpp}
here=$(dirname "$0")
SCAN_RULES_AWK=$here/cpp-scan-rules.awk
HATCHES=$dir/CPP-HATCHES.txt
CLAIMS=$dir/CLAIMS.txt
DECIDERS=$dir/DECIDERS.txt

errs=0
note() { printf '   BAD   %s\n' "$1"; errs=$((errs + 1)); }

RULE_IDS=""

# rule <id> <scope: any|nonposix> <regex> <why>
rule() {
  RULE_IDS="$RULE_IDS $1"
  k=$(echo "$1" | tr - _)
  eval "RULE_SCOPE_$k=\$2"
  eval "RULE_PAT_$k=\$3"
  eval "RULE_WHY_$k=\$4"
}

# THE RULE TABLE IS THE POINT OF THIS SHAPE. One rule per line between the
# markers, so a blind patch that deletes one line blinds exactly one rule and
# nothing else -- which is what lets this lane fail its own mutation test
# (scripts/cpp-scan-blind.sh, DR-27). A lane that has quietly stopped checking
# something looks exactly like a lane with nothing to report.
#
# RIFT-SCAN-RULES-BEGIN
rule A5-RANDOM  any      '<random>|[^A-Za-z_](rand|srand|random|arc4random|mt19937)[[:space:]]*\(' 'randomness in engine scope; the simulator owns the only stream'
rule A5-CLOCK   any      '<chrono>|[^A-Za-z_](time|clock|gettimeofday|clock_gettime)[[:space:]]*\(|steady_clock|system_clock' 'a wall-clock read; the C++ analogue of clock/real.go has ZERO hatched calls'
rule A5-FLOAT   any      '(^|[^A-Za-z_0-9])(float|double)([^A-Za-z_0-9]|$)' 'a float on a path that can reach on-disk bytes'
rule A5-GETENV  any      '[^A-Za-z_]getenv[[:space:]]*\(' 'ambient environment; a run must be a function of its inputs'
rule A5-STREAM  any      '<fstream>|<iostream>|[^A-Za-z_]fopen[[:space:]]*\(' 'the engine does not open files to talk about itself'
rule A5-DEFAULT any      '(^|[[:space:]])default[[:space:]]*:' 'a default: arm buys back the exhaustiveness -Werror=switch gives for free'
rule A5-ADDRESS any      '(map|set|unordered_map|unordered_set)[[:space:]]*<[[:space:]]*[A-Za-z_][A-Za-z0-9_:[:space:]]*\*' 'a pointer-keyed container; nothing may depend on an address (section 6.1)'
rule A5-ADDRINT any      'reinterpret_cast[[:space:]]*<[[:space:]]*(std::)?(uintptr_t|intptr_t|uint64_t|int64_t|size_t|unsigned long)' 'address arithmetic; section 6.1 bans address-ORDERED behaviour as well as pointer-keyed containers, and a value derived from an allocation is both'
rule A5-SYSCALL nonposix '::[[:space:]]*(open|write|read|pread|pwrite|fsync|fdatasync|rename|unlink|mkdir|rmdir|stat|lstat|fcntl|close|_exit|opendir|readdir|closedir|ftruncate|lseek)[[:space:]]*\(' 'a syscall outside env/posix/; every syscall goes through Env for the reason every clock read goes through Clock'
# RIFT-SCAN-RULES-END

# THE STATEMENT CAP FOR engine-cpp/src/env/posix, AND ITS DERIVATION.
#
# Recorded here, at the constant, and not in prose elsewhere -- the same rule
# section 8.4 applies to kMaxRecordBytes and kWalBufferBytes, for the same
# reason: there has to be exactly one place to correct.
#
# Measured, not guessed. After the readdir loop moved to the raw seam, every
# function in posix_env.cc is at or under 15 non-blank code lines:
#
#     DoLockFile        15      DoRead (seq/random)   9
#     DoUnlockFile      14      DoSync / DoClose      8
#     DoFileExists      10      everything else      <=5
#
# 16 sits one line above the largest conforming function. Tight enough that a
# method cannot absorb a second responsibility without saying so, loose enough
# that adding an errno case to DoLockFile does not redden the lane -- and a lane
# that reddens on a benign change is a lane that gets its cap raised rather than
# its code fixed.
#
# EXACTLY ONE FUNCTION IN THE TREE EXCEEDS IT: WriteFully, at 19. That is the
# mechanism working rather than an accident. It is the single place in PosixEnv
# with real logic and the single place with dedicated tests, so it is also the
# one place where `covered-by` is true, and the registry entry naming those five
# tests is what the cap exists to force somebody to write.
POSIX_LINE_CAP=16

scan_file() {  # scan_file <file> <posix: yes|no>
  f=$1
  posix=$2
  # Comments are stripped before matching. A rule that fired on the prose
  # explaining why the rule exists is a rule nobody can document.
  stripped=$(sed 's://.*::' "$f")
  for id in $RULE_IDS; do
    k=$(echo "$id" | tr - _)
    eval "sc=\$RULE_SCOPE_$k"
    eval "pat=\$RULE_PAT_$k"
    if [ "$sc" = nonposix ] && [ "$posix" = yes ]; then continue; fi
    printf '%s\n' "$stripped" | grep -E "$pat" | sed 's/^[ \t]*//; s/[ \t]*$//' |
      while IFS= read -r text; do
        [ -z "$text" ] || printf '%s|%s|%s\n' "$id" "$f" "$text"
      done
  done
  if [ "$posix" = yes ]; then
    awk -v CAP="$POSIX_LINE_CAP" -f "$SCAN_RULES_AWK" "$f"
  fi
}

# ---------------------------------------------------------------- fixtures
#
# Each fixture contains exactly ONE violation and declares which rule must fire
# on it. This is what makes a blinded rule detectable: blind the rule, the
# fixture stops being rejected, the lane says so.
if [ "$FIXTURES" = yes ]; then
  fixdir=$dir/scan-fixtures
  printf '\n  scope scan -- fixture check (each rule must still fire)\n'
  printf '  ----------------------------------------------------------\n'
  n=0
  set -- "$fixdir"/*
  [ -n "$ONE_FIXTURE" ] && set -- "$ONE_FIXTURE"
  for f in "$@"; do
    [ -f "$f" ] || continue
    want=$(sed -n 's|^// RIFT_SCAN_FIXTURE  *||p' "$f" | head -1)
    scope=$(sed -n 's|^// RIFT_SCAN_SCOPE  *||p' "$f" | head -1)
    [ -n "$scope" ] || scope=no
    if [ -z "$want" ]; then
      note "$f declares no // RIFT_SCAN_FIXTURE rule"
      continue
    fi
    n=$((n + 1))
    got=$(scan_file "$f" "$scope" | cut -d'|' -f1 | sort -u | tr '\n' ' ')
    case " $got " in
      *" $want "*) printf '   fires   %-16s %s\n' "$want" "$(basename "$f")" ;;
      *) note "fixture $(basename "$f") should trip $want; rules that fired: [$got]" ;;
    esac
  done
  printf '  ----------------------------------------------------------\n'
  if [ "$n" -eq 0 ]; then
    printf '   FAIL  no fixtures found. An empty fixture check proves nothing.\n\n'
    exit 2
  fi
  if [ "$errs" -ne 0 ]; then
    printf '   FAIL  %d rule(s) no longer fire on their own fixture.\n\n' "$errs"
    exit 1
  fi
  printf '   ok  %d rules still fire on their fixtures\n\n' "$n"
  exit 0
fi

# ------------------------------------------------------------------ part 1
"$here/cpp-scan-surface.sh" "$dir" || errs=$((errs + 1))

# ------------------------------------------------------------------ part 2
printf '  [2/4] A5 scope rules and PosixEnv thinness\n'
printf '  ----------------------------------------------------------\n'

found=$(mktemp)
trap 'rm -f "$found" "$found.hatched" "$found.used"' EXIT INT TERM

files=$(find "$dir/src" -type f \( -name '*.h' -o -name '*.cc' \) | sort)
nfiles=$(printf '%s\n' "$files" | grep -c . || true)
if [ "$nfiles" -eq 0 ]; then
  note "no sources found under $dir/src -- a scan over nothing is not a scan"
fi
for f in $files; do
  posix=no
  case $f in */src/env/posix/*) posix=yes ;; esac
  scan_file "$f" "$posix" >> "$found"
done

nviol=$(grep -c . "$found" || true)
printf '   files scanned  : %s\n' "$nfiles"
printf '   rules applied  : %s\n' "$(printf '%s' "$RULE_IDS" | wc -w | tr -d ' ')"
printf '   violations     : %s (each must be in the registry below)\n' "$nviol"

# ------------------------------------------------------------- part 2b
#
# ORACLE INDEPENDENCE, MADE CHECKABLE -- AND CORRECTED AT B3.
#
# The rule used to be "rig/exactness_oracle.{h,cc} includes nothing from src/".
# That was too coarse in a way B3 could not work around: the drop checker has to
# read the BYTES an SSTable holds, because reading through Get would filter by
# snapshot and hide the one failure it exists to find.
#
# RULED 2026-08-25, and corrected rather than excepted:
#
#   AN ORACLE MAY PARSE THE ENGINE'S ARTIFACTS, AND MAY NOT CONSULT THE ENGINE'S
#   BELIEFS. Parsing shares a format; consulting shares a judgement.
#
# Bytes on disk are not a claim. What the engine SAYS is a claim, and ruling 4's
# sentence -- AN ORACLE THAT INTERROGATES THE ENGINE BELIEVES THE LIE -- is about
# claims. Made mechanical here by one mark on the artifact side:
#
#   AN ARTIFACT HEADER DECLARES NOTHING TAKING AN `Env*` AND NOTHING TAKING A
#   SNAPSHOT.
#
# Three checks, each able to fail on its own.
printf '   oracle         : '
oracle_bad=0
ARTIFACTS=$dir/ARTIFACTS.txt
ORACLES=$dir/ORACLES.txt

if [ ! -f "$ARTIFACTS" ]; then note "missing $ARTIFACTS"; oracle_bad=$((oracle_bad + 1)); fi
if [ ! -f "$ORACLES" ]; then note "missing $ORACLES"; oracle_bad=$((oracle_bad + 1)); fi

if [ -f "$ARTIFACTS" ] && [ -f "$ORACLES" ]; then
  allowed=$(grep -vE '^\s*#|^\s*$' "$ARTIFACTS" | awk -F'|' '{print $1}' | sed 's/ *$//' | tr '\n' ' ')
  oracle_files=$(grep -vE '^\s*#|^\s*$' "$ORACLES" | awk -F'|' '{print $1}' | sed 's/ *$//' | tr '\n' ' ')

  # (1) EVERY ARTIFACT EXISTS, IS JUSTIFIED, AND CARRIES NEITHER MARK.
  while IFS= read -r line; do
    case $line in ''|\#*) continue ;; esac
    hdr=$(printf '%s' "$line" | awk -F'|' '{print $1}' | sed 's/ *$//')
    why=$(printf '%s' "$line" | awk -F'|' '{print $2}' | sed 's/^ *//; s/ *$//')
    [ -n "$hdr" ] || continue
    if [ -z "$why" ]; then
      note "ARTIFACTS.txt: $hdr is listed with no justification"; oracle_bad=$((oracle_bad + 1)); continue
    fi
    if [ ! -f "$dir/src/$hdr" ]; then
      note "ARTIFACTS.txt names $hdr, which does not exist"; oracle_bad=$((oracle_bad + 1)); continue
    fi
    # Comments stripped first: the mark is about DECLARATIONS, and every one of
    # these files explains in prose why it has neither.
    decls=$(sed 's://.*::' "$dir/src/$hdr")
    if printf '%s\n' "$decls" | grep -qE 'Env[[:space:]]*\*'; then
      note "$hdr is an ARTIFACT and declares something taking an Env* -- it went and looked, which is an opinion"
      oracle_bad=$((oracle_bad + 1))
    fi
    if printf '%s\n' "$decls" | grep -qi 'snapshot'; then
      note "$hdr is an ARTIFACT and mentions a snapshot -- deciding what a caller may see is the judgement an oracle may not share"
      oracle_bad=$((oracle_bad + 1))
    fi
  done < "$ARTIFACTS"

  # (2) EVERY ORACLE'S src/ INCLUDES ARE ALLOW-LISTED.
  for f in $oracle_files; do
    if [ ! -f "$dir/$f" ]; then
      note "ORACLES.txt names $f, which does not exist"; oracle_bad=$((oracle_bad + 1)); continue
    fi
    # THE MARKER IS A DECLARATION, NOT AN OCCURRENCE: it is the file's FIRST
    # LINE. A loose substring match read `rig/image_fixture.h` -- whose header
    # says it carries no RIFT_ORACLE marker -- as carrying one. A file has to be
    # able to say what it is not.
    if ! head -1 "$dir/$f" | grep -q '^// RIFT_ORACLE'; then
      note "$f is registered in ORACLES.txt and does not declare RIFT_ORACLE on its first line"
      oracle_bad=$((oracle_bad + 1))
    fi
    for inc in $(sed 's://.*::' "$dir/$f" | sed -n 's/^#include "\([^"]*\)".*/\1/p'); do
      # NOT `found`: that name is part 3's temp file, and clobbering it made two
      # unrelated registry entries report as stale. Caught by this lane on its
      # first run, which is the lane doing to itself what it does to the tree.
      art_hit=$(cd "$dir/src" && find . -name "$inc" | sed 's|^\./||' | head -1)
      [ -n "$art_hit" ] || continue      # not from src/ at all
      case " $allowed " in
        *" $art_hit "*) ;;
        *) note "$f includes $art_hit, which is not in ARTIFACTS.txt -- an oracle may parse artifacts and may not consult beliefs"
           oracle_bad=$((oracle_bad + 1)) ;;
      esac
    done
  done

  # (3) AND THE OTHER WAY: a marked file that is not registered.
  for f in $(find "$dir/rig" -name '*.h' -o -name '*.cc'); do
    head -1 "$f" | grep -q '^// RIFT_ORACLE' || continue
    rel=${f#"$dir/"}
    case " $oracle_files " in
      *" $rel "*) ;;
      *) note "$rel carries RIFT_ORACLE and is not in ORACLES.txt"; oracle_bad=$((oracle_bad + 1)) ;;
    esac
  done
fi

if [ "$oracle_bad" -eq 0 ]; then
  printf 'parses %s artifact(s), consults no beliefs\n' \
         "$(grep -vcE '^\s*#|^\s*$' "$ARTIFACTS" 2>/dev/null || echo 0)"
else
  printf '%d problem(s)\n' "$oracle_bad"
fi

# ------------------------------------------------------------------ part 3
printf '  [3/4] CPP-HATCHES.txt, reconciled against what actually fired\n'
printf '  ----------------------------------------------------------\n'

if [ ! -f "$HATCHES" ]; then
  note "missing $HATCHES"
else
  : > "$found.used"
  nreg=0
  while IFS= read -r line; do
    case $line in ''|\#*) continue ;; esac
    nreg=$((nreg + 1))
    rid=$(printf '%s' "$line" | awk -F'|' '{print $1}' | sed 's/^ *//; s/ *$//')
    rfile=$(printf '%s' "$line" | awk -F'|' '{print $2}' | sed 's/^ *//; s/ *$//')
    ranchor=$(printf '%s' "$line" | awk -F'|' '{print $3}' | sed 's/^ *//; s/ *$//')
    rlabel=$(printf '%s' "$line" | awk -F'|' '{print $4}' | sed 's/^ *//; s/ *$//')

    # The split label, enforced. Exactly one of two forms, both with their
    # fields filled in.
    case $rlabel in
      covered-by:*)
        inst=$(printf '%s' "$rlabel" | sed 's/^covered-by: *//')
        if [ -z "$inst" ]; then
          note "$rid $rfile: covered-by names no instrument"
        else
          instpath=$(printf '%s' "$inst" | awk '{print $1}')
          [ -e "$instpath" ] || note "$rid $rfile: covered-by names '$instpath', which does not exist"
        fi
        ;;
      unreachable:*)
        rest=$(printf '%s' "$rlabel" | sed 's/^unreachable: *//')
        det=$(printf '%s' "$rest" | awk -F'~' '{print $1}' | sed 's/ *$//')
        arg=$(printf '%s' "$rest" | awk -F'~' '{print $2}' | sed 's/^ *//')
        [ -n "$det" ] || note "$rid $rfile: unreachable names no detector"
        [ -n "$arg" ] || note "$rid $rfile: unreachable gives no argument that no other detector sees the class more often"
        ;;
      *)
        note "$rid $rfile: label must be 'covered-by: <instrument>' or 'unreachable: <detector> ~ <argument>', got '$rlabel'"
        ;;
    esac

    if grep -Fq "$rid|$rfile|$ranchor" "$found"; then
      printf '%s|%s|%s\n' "$rid" "$rfile" "$ranchor" >> "$found.used"
    else
      # AN UNUSED ENTRY FAILS. A drifted hatch means something is unguarded
      # while its author believes otherwise, which is worse than no hatch at
      # all -- the registry is read as a list of known gaps, so a stale line
      # makes a real gap look accounted for.
      note "$rid $rfile: registry entry matches nothing the scan found -- either the code changed and the exemption is stale, or the rule stopped firing"
    fi
  done < "$HATCHES"
  printf '   registry lines : %s\n' "$nreg"

  # Every violation must be registered.
  while IFS= read -r v; do
    [ -z "$v" ] && continue
    if ! grep -Fqx "$v" "$found.used"; then
      vid=$(printf '%s' "$v" | awk -F'|' '{print $1}')
      vf=$(printf '%s' "$v" | awk -F'|' '{print $2}')
      vt=$(printf '%s' "$v" | awk -F'|' '{print $3}')
      k=$(echo "$vid" | tr - _)
      eval "why=\${RULE_WHY_$k:-}"
      note "$vid  $vf"
      printf '           %s\n' "$vt"
      [ -n "$why" ] && printf '           why the rule exists: %s\n' "$why"
      printf '           To exempt it, add a line to %s with a SPLIT LABEL.\n' "$HATCHES"
    fi
  done < "$found"
fi

# ------------------------------------------------------------------ part 4
printf '  [4/4] CLAIMS.txt -- the sentences this lane is what makes true\n'
printf '  ----------------------------------------------------------\n'
if [ ! -f "$CLAIMS" ]; then
  note "missing $CLAIMS"
else
  nclaim=0
  while IFS= read -r line; do
    case $line in ''|\#*) continue ;; esac
    nclaim=$((nclaim + 1))
    cfile=$(printf '%s' "$line" | awk -F'|' '{print $1}' | sed 's/^ *//; s/ *$//')
    ctext=$(printf '%s' "$line" | awk -F'|' '{print $2}' | sed 's/^ *//; s/ *$//')
    if [ ! -f "$cfile" ]; then
      note "claim names $cfile, which does not exist"
    elif ! grep -Fq "$ctext" "$cfile"; then
      note "claim not found verbatim in $cfile: \"$ctext\""
    fi
  done < "$CLAIMS"
  printf '   claims checked : %s\n' "$nclaim"
  if [ "$nclaim" -eq 0 ]; then
    note "CLAIMS.txt lists nothing; a step in the epistemic-chokepoint class must name what it carries"
  fi
fi

# ------------------------------------------------------------------ part 5
#
# A DECIDER OF EVIDENTIARY STATUS IS A NAMED CATEGORY, and every member is
# asserted in BOTH directions. HARNESS-006, -008 and -009 were all the same
# defect in three different functions: a conservative misclassification, which
# is the direction nothing notices, whose cost arrives downstream as a gate
# nothing can satisfy. Six is a small enough population to enumerate, and
# enumerating it is what stops the seventh arriving in B2.
printf '  [5/5] DECIDERS.txt -- both directions, for every evidentiary decider\n'
printf '  ----------------------------------------------------------\n'
if [ ! -f "$DECIDERS" ]; then
  note "missing $DECIDERS"
else
  ndec=0
  registered=""
  while IFS= read -r line; do
    case $line in ''|\#*) continue ;; esac
    ndec=$((ndec + 1))
    fn=$(printf '%s' "$line" | awk -F'|' '{print $1}' | sed 's/^ *//; s/ *$//')
    t1=$(printf '%s' "$line" | awk -F'|' '{print $2}' | sed 's/^ *//; s/ *$//')
    t2=$(printf '%s' "$line" | awk -F'|' '{print $3}' | sed 's/^ *//; s/ *$//')
    registered="$registered $fn"
    if [ -z "$fn" ] || [ -z "$t1" ] || [ -z "$t2" ]; then
      note "DECIDERS.txt line needs <function> | <true-direction test> | <false-direction test>: $line"
      continue
    fi
    if ! grep -rq "RIFT_EVIDENCE_DECIDER" "$dir/src" "$dir/rig" --include='*.h' 2>/dev/null; then
      note "no RIFT_EVIDENCE_DECIDER markers found at all"
      break
    fi
    if ! grep -rq "^[^/]*[^A-Za-z_]$fn(.*RIFT_EVIDENCE_DECIDER" "$dir/src" "$dir/rig" --include='*.h'; then
      note "$fn is registered but its declaration carries no RIFT_EVIDENCE_DECIDER marker"
    fi
    for t in "$t1" "$t2"; do
      suite=$(printf '%s' "$t" | cut -d. -f1)
      name=$(printf '%s' "$t" | cut -d. -f2)
      if ! grep -rq "TEST(.*$suite, *$name)\|TEST_F(.*$suite, *$name)" "$dir/test"; then
        note "$fn names test $t, which does not exist -- a direction asserted by a test that is not there is a direction not asserted"
      fi
    done
  done < "$DECIDERS"
  printf '   deciders       : %s\n' "$ndec"

  # And the other way: every MARKED declaration must be registered. A decider
  # that lands with a marker and no entry is the seventh arriving unasserted.
  for f in $(find "$dir/src" "$dir/rig" -name '*.h'); do
    grep -n 'RIFT_EVIDENCE_DECIDER' "$f" | while IFS= read -r hit; do
      decl=$(printf '%s' "$hit" | sed 's/^[0-9]*://')
      name=$(printf '%s' "$decl" | sed 's/(.*//' | awk '{print $NF}' | sed 's/^[*&]*//')
      case " $registered " in
        *" $name "*) ;;
        *) printf '   BAD   %s is marked RIFT_EVIDENCE_DECIDER and is not in %s\n' "$name" "$DECIDERS" ;;
      esac
    done
  done
  # The subshell above cannot raise errs, so re-check in this shell.
  for f in $(find "$dir/src" "$dir/rig" -name '*.h'); do
    for name in $(grep 'RIFT_EVIDENCE_DECIDER' "$f" | sed 's/^[0-9]*://' | sed 's/(.*//' | awk '{print $NF}' | sed 's/^[*&]*//'); do
      case " $registered " in
        *" $name "*) ;;
        *) errs=$((errs + 1)) ;;
      esac
    done
  done
fi

# ------------------------------------------------------------------ part 6
#
# CF-2, MADE A LANE RATHER THAN AN OBLIGATION. FLOORS.txt's own header says a
# class missing from it is a class with NO STANDING MEASUREMENT, "which is how a
# bug class drifts back into being uncatchable one flaw at a time" -- and 47 of
# 98 were missing at B2's close.
#
# The distinction that makes this worth a lane: every one of those 47 carried a
# covering LANE and died in the mutant catalogue. What none carried was a
# covering ASSERTION. A class killed by a lane but named by nothing is a class
# whose covering assertion can be DELETED WITH NO LANE GOING RED -- the mutant
# still dies, on some other assertion in the same lane, and the class quietly
# stops testing what it was written to test.
# ---------------------------------------------------------------- part 7
#
# THE C BOUNDARY IS BUILT WITHOUT EXCEPTIONS, AND THAT IS A CLAIM ABOUT THE
# BUILD RATHER THAN ABOUT ANY LINE OF CODE.
#
# B5-D2: no exception crosses into Go, and the enforcement is `-fno-exceptions`
# on the archive -- so `throw` does not compile, `try` does not compile, and
# operator new aborts. A `catch (...)` would have been weaker: it converts an
# exception into a code and loses what it was.
#
# A FLAG IS ENFORCED WHERE FLAGS LIVE. Nothing in the source can assert its own
# compile options, so the lane reads them.
printf '  [7/7] the C boundary is built without exceptions\n'
printf '  ----------------------------------------------------------\n'
if ! grep -q 'fno-exceptions' "$dir/CMakeLists.txt"; then
  note "RIFT_CXX_FLAGS does not carry -fno-exceptions"
elif ! grep -q 'target_compile_options(rift_capi PRIVATE ${RIFT_CXX_FLAGS})' "$dir/CMakeLists.txt"; then
  note "rift_capi does not compile with RIFT_CXX_FLAGS, so -fno-exceptions may not reach it"
else
  printf '   boundary       : -fno-exceptions reaches rift_capi\n'
fi

printf '  [6/6] FLOORS.txt -- every mutant class has a standing measurement\n'
printf '  ----------------------------------------------------------\n'
if [ ! -f "$dir/FLOORS.txt" ]; then
  note "missing $dir/FLOORS.txt"
elif [ ! -d "$dir/mutants" ]; then
  note "missing $dir/mutants"
else
  # TEMP FILES, NOT PROCESS SUBSTITUTION. `<(...)` is a bashism and this script
  # runs under `sh`; the first version of this check died with a syntax error
  # AFTER printing its own heading, which is the shape of a lane that looks
  # like it ran.
  listed_f=$(mktemp); present_f=$(mktemp)
  grep -vE '^[[:space:]]*#|^[[:space:]]*$' "$dir/FLOORS.txt" \
    | awk -F'|' '{print $1}' | sed 's/ *$//' | sort -u > "$listed_f"
  ls "$dir"/mutants/*.patch 2>/dev/null | xargs -n1 basename | sed 's/\.patch$//' \
    | sort -u > "$present_f"
  nclass=$(grep -c . "$present_f" || true)
  missing=$(comm -23 "$present_f" "$listed_f")
  nmissing=$(printf '%s\n' "$missing" | grep -c . || true)
  rm -f "$listed_f" "$present_f"
  printf '   classes        : %s\n' "$nclass"
  if [ "$nmissing" -ne 0 ]; then
    for m in $missing; do
      note "$m has no entry in FLOORS.txt -- a covering lane is not a covering assertion"
    done
  fi

  # EVERY ROW IS SEVEN FIELDS, AND THIS IS A DELIMITER RULE RATHER THAN A TIDY
  # ONE. The file is pipe-delimited with NO ESCAPE, and the campaign parses it
  # POSITIONALLY. A row that names a C++ guard verbatim can contain the `or`
  # operator, which is this delimiter DOUBLED: it splits one field into three,
  # shifts every column right, and the row still looks like a row.
  #
  # B3.5b wrote such a row. The campaign caught it -- but only because the
  # displaced value landed in `regime`, which is validated against a known set.
  # Two columns further along it would have parsed, and `covered-by:` would have
  # silently named the wrong assertion in the one file a reader trusts before
  # deleting one.
  #
  # So the shape is checked rather than the luck relied on.
  #
  # THE BOUND IS AN UPPER ONE, AND THE FILE'S OWN HEADER IS WHY. Trailing
  # columns may be omitted -- "an absent column means default" -- so 5, 6 and 7
  # fields are all legal rows and the check would be WRONG to demand exactly
  # seven. Nothing legitimate can produce MORE than seven, and more is exactly
  # what a doubled delimiter produces. Induced against a row carrying
  # `RIFT_CHECK(a == 0 || b == 1)`, which is nine.
  badfields=$(grep -vE '^[[:space:]]*#|^[[:space:]]*$' "$dir/FLOORS.txt" \
    | awk -F'|' 'NF > 7 {print $1 " (" NF " fields, at most 7)"}')
  if [ -n "$badfields" ]; then
    printf '%s\n' "$badfields" | while IFS= read -r row; do
      printf '   %-14s :    BAD   %s\n' "floors row" "$row"
    done
    errs=$((errs + 1))
  fi
fi

printf '  ----------------------------------------------------------\n'
if [ "$errs" -ne 0 ]; then
  printf '   FAIL  %d problem(s).\n\n' "$errs"
  exit 1
fi
printf '   ok  scope clean, registry exact, claims present, deciders asserted both ways\n\n'
