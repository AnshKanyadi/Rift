#!/usr/bin/env sh
# The Env surface scan: the 1:1:1 correspondence, asserted rather than reviewed.
#
# DESIGN-B1 section 3.2. The NVI shape makes it impossible for an IMPLEMENTATION
# to expose an entry point that skips fault interception. It does not make it
# impossible for an edit to env.h itself -- adding a public virtual to a base
# class there would bypass, and that is exactly what mutant BM17 does. This is
# the check that catches it.
#
# THE CORRESPONDENCE: one public non-virtual wrapper, one private Do* pure
# virtual, one CallSite enumerator, one entry in AllCallSites(). Four artifacts,
# not three: the census iterates AllCallSites(), so a list that had drifted from
# the enum would make the census report on a different set than exists -- which
# is the failure the census is FOR, arriving inside the census itself. Not "the
# same number of each" -- the same NAMES. A count equality can be satisfied by two unrelated drifts cancelling;
# set equality cannot, and it costs nothing more to check.
#
# THE GRAMMAR IS STRICT AND THAT IS THE POINT. Every line inside the marked
# region must be classifiable. A line this scanner does not understand is a LANE
# FAILURE, never a line it skips. A parser that silently ignores what it cannot
# classify reports the health of its own grammar and calls it coverage -- which
# is the failure Track A spent five checklist steps inside this month.
#
# SCOPE, AND WHAT IS NOT HERE YET. At B1.2a this scan carries only the Env
# surface rules. B1.4 extends this same script with the rest of DESIGN-B1
# section 9.4: the A5 bans (<random>, <chrono>, float/double, getenv, raw
# open/write/fsync/rename outside env/posix/, `default:` over a closed enum),
# CPP-HATCHES.txt as the checked-in registry with the unused-entry rule, and the
# blind-patch set that makes this lane fail its own mutation test. Until then,
# this lane's green means the Env surface is intact and nothing more.
#
# usage: cpp-scan.sh [engine-cpp-dir]
set -eu

dir=${1:-engine-cpp}
env_h=$dir/src/env/env.h
call_site_h=$dir/src/env/call_site.h
call_site_cc=$dir/src/env/call_site.cc

for f in "$env_h" "$call_site_h" "$call_site_cc"; do
  if [ ! -f "$f" ]; then
    printf '\n  FAIL  missing %s\n\n' "$f"; exit 1
  fi
done

printf '\n  Env surface scan (1:1:1)\n'
printf '  ----------------------------------------------------------\n'

awk '
function bad(msg) { printf("   BAD   %s:%d  %s\n", FILENAME, FNR, msg); errs++ }

BEGIN { region = 0; access = ""; cls = ""; nbegin = 0; nend = 0; errs = 0
        ncall = 0; nimpl = 0; nenum = 0; inenum = 0
        nlist = 0; inlist = 0; nlbegin = 0; nlend = 0 }

# ------------------------------------------------------------ call_site.h
FILENAME ~ /call_site\.h$/ {
  if ($0 ~ /^enum class CallSite/) { inenum = 1; next }
  if (inenum && $0 ~ /^};/)        { inenum = 0; next }
  if (!inenum) next
  t = $0
  sub(/\/\/.*/, "", t)
  gsub(/[ \t]/, "", t)
  if (t == "") next
  if (t ~ /^k[A-Za-z0-9_]+,$/) {
    name = substr(t, 1, length(t) - 1)
    if (name in enums) bad("duplicate CallSite enumerator " name)
    enums[name] = 1; nenum++
    next
  }
  bad("unparsed line inside the CallSite enum: " $0)
  next
}

# ------------------------------------------------------------ call_site.cc
FILENAME ~ /call_site\.cc$/ {
  if ($0 ~ /RIFT-CALL-SITE-LIST-BEGIN/) { inlist = 1; nlbegin++; next }
  if ($0 ~ /RIFT-CALL-SITE-LIST-END/)   { inlist = 0; nlend++;   next }
  if (!inlist) next
  t = $0
  sub(/\/\/.*/, "", t)
  gsub(/[ \t]/, "", t)
  if (t == "") next
  if (t ~ /^CallSite::k[A-Za-z0-9_]+,$/) {
    name = t; sub(/^CallSite::/, "", name); sub(/,$/, "", name)
    if (name in listed) bad("duplicate entry in AllCallSites(): " name)
    listed[name] = 1; nlist++
    next
  }
  bad("unparsed line inside the AllCallSites() list: " $0)
  next
}

# ----------------------------------------------------------------- env.h
FILENAME ~ /env\.h$/ {
  if ($0 ~ /RIFT-ENV-SURFACE-BEGIN/) { region = 1; nbegin++; next }
  if ($0 ~ /RIFT-ENV-SURFACE-END/)   { region = 0; nend++;   next }

  if (!region) {
    if ($0 ~ /^class (Env|WritableFile|SequentialFile|RandomAccessFile|Directory)[ \t]*\{/)
      bad("surface class declared OUTSIDE the scanned region; moving a class out of the region moves it out of this check")
    next
  }

  t = $0
  sub(/[ \t]+$/, "", t)
  sub(/^[ \t]+/, "", t)
  if (t == "") next
  if (t ~ /^\/\//) next

  if (t ~ /^class [A-Za-z_][A-Za-z0-9_]*[ ]*\{$/) {
    cls = t; sub(/^class /, "", cls); sub(/[ ]*\{$/, "", cls)
    seen_class[cls] = 1
    access = "private"          # a class defaults to private, as C++ does
    next
  }
  if (t == "};") { cls = ""; access = ""; next }
  if (t == "public:")    { access = "public";    next }
  if (t == "protected:") { access = "protected"; next }
  if (t == "private:")   { access = "private";   next }

  if (cls == "") { bad("declaration outside any class: " t); next }

  # The bypass rule, checked before anything else so its message is the one
  # that prints. This is the mutant BM17 exists to plant.
  if (access == "public" && t ~ /virtual/ && t !~ /^virtual ~[A-Za-z_][A-Za-z0-9_]*\(\);$/) {
    bad("PUBLIC VIRTUAL in the Env surface -- an implementation could override it and bypass fault interception entirely: " t)
    next
  }

  if (t ~ /^virtual ~[A-Za-z_][A-Za-z0-9_]*\(\);$/) {
    if (access != "public") bad("the destructor must be public: " t)
    next
  }

  if (t ~ /\/\/ RIFT_ENV_CALL /) {
    name = t; sub(/^.*\/\/ RIFT_ENV_CALL /, "", name); sub(/[ \t]*$/, "", name)
    if (access != "public") bad("RIFT_ENV_CALL wrapper is not public: " t)
    if (name in calls) bad("two wrappers claim CallSite " name)
    calls[name] = 1; call_class[name] = cls; ncall++
    next
  }
  if (t ~ /\/\/ RIFT_ENV_IMPL /) {
    name = t; sub(/^.*\/\/ RIFT_ENV_IMPL /, "", name); sub(/[ \t]*$/, "", name)
    if (access != "private") bad("RIFT_ENV_IMPL virtual is not private -- private is what stops an implementation from being called directly: " t)
    if (t !~ /virtual /)     bad("RIFT_ENV_IMPL is not virtual: " t)
    if (t !~ /= 0;/)         bad("RIFT_ENV_IMPL is not pure -- a default implementation is an implementation that skipped review: " t)
    if (name in impls) bad("two implementations claim CallSite " name)
    impls[name] = 1; impl_class[name] = cls; nimpl++
    next
  }
  if (t ~ /\/\/ RIFT_ENV_CTOR/)  { if (access == "public") bad("constructor must not be public: " t); next }
  if (t ~ /\/\/ RIFT_ENV_STATE/) { if (access != "private") bad("state must be private: " t); next }
  if (t ~ /= delete;$/) next

  bad("unparsed line -- this scanner refuses to skip what it cannot classify: " t)
  next
}

END {
  if (nbegin != 1 || nend != 1) {
    printf("   BAD   env.h must contain exactly one RIFT-ENV-SURFACE-BEGIN and one -END (found %d and %d)\n", nbegin, nend)
    errs++
  }

  split("Env WritableFile SequentialFile RandomAccessFile Directory", req, " ")
  for (i in req) if (!(req[i] in seen_class)) {
    printf("   BAD   surface class %s was not found inside the region\n", req[i]); errs++
  }

  for (n in calls) {
    if (!(n in impls)) { printf("   BAD   %s has a public wrapper but no private Do* implementation\n", n); errs++ }
    if (!(n in enums)) { printf("   BAD   %s has a public wrapper but no CallSite enumerator -- an entry point nobody can inject at or kill at\n", n); errs++ }
    if ((n in impls) && call_class[n] != impl_class[n]) {
      printf("   BAD   %s: wrapper is in %s but implementation is in %s\n", n, call_class[n], impl_class[n]); errs++
    }
  }
  for (n in impls) {
    if (!(n in calls)) { printf("   BAD   %s has a private Do* but no public wrapper -- unreachable through the choke point\n", n); errs++ }
    if (!(n in enums)) { printf("   BAD   %s has a private Do* but no CallSite enumerator\n", n); errs++ }
  }
  for (n in enums) {
    if (!(n in calls)) { printf("   BAD   CallSite %s has no public wrapper\n", n); errs++ }
    if (!(n in impls)) { printf("   BAD   CallSite %s has no private Do* implementation\n", n); errs++ }
    if (!(n in listed)) { printf("   BAD   CallSite %s is missing from AllCallSites(), so the census would never look for it\n", n); errs++ }
  }
  for (n in listed) {
    if (!(n in enums)) { printf("   BAD   AllCallSites() lists %s, which is not a CallSite enumerator\n", n); errs++ }
  }
  if (nlbegin != 1 || nlend != 1) {
    printf("   BAD   call_site.cc must contain exactly one RIFT-CALL-SITE-LIST-BEGIN and one -END (found %d and %d)\n", nlbegin, nlend)
    errs++
  }

  printf("   public non-virtual wrappers : %d\n", ncall)
  printf("   private Do* pure virtuals   : %d\n", nimpl)
  printf("   CallSite enumerators        : %d\n", nenum)
  printf("   AllCallSites() entries      : %d\n", nlist)
  if (ncall != nimpl || ncall != nenum || ncall != nlist) {
    printf("   BAD   the four counts must be equal\n"); errs++
  }

  printf("  ----------------------------------------------------------\n")
  if (errs > 0) {
    printf("   FAIL  %d problem(s) in the Env surface.\n\n", errs)
    printf("  The fault surface and the kill-point set are defined by this\n")
    printf("  correspondence. A drift in it is not a style problem: it is an Env\n")
    printf("  call that no injector can reach, or an injector that fires at\n")
    printf("  nothing, and neither reports itself anywhere else.\n\n")
    exit 1
  }
  printf("   ok  one wrapper, one Do*, one CallSite, one list entry -- names, not counts\n\n")
}
' "$env_h" "$call_site_h" "$call_site_cc"
