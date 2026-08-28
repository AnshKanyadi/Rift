#!/usr/bin/env sh
# Run a command with networking disabled, and PROVE the networking was disabled.
#
# DESIGN-B1 section 9.2: the claim is that a stranger reproduces every number
# from a clean clone with one script, so the test of that claim is doing it
# under the conditions a stranger might have. That is what this wrapper is for.
#
# THE CONTROL IS THE WHOLE POINT. A wrapper that silently fails to isolate
# produces a green lane that proves nothing -- the exact shape of failure this
# project rejects everywhere else, and the reason `make blind` has a baseline
# gate. So before running anything, this probes the network twice:
#
#   probe WITHOUT isolation   probe WITH isolation   verdict
#   -----------------------   --------------------   -------------------------
#   reachable                 blocked                VERIFIED -- proceed
#   reachable                 reachable              INVALID  -- refuse to run
#   unreachable               (not run)              UNTESTED -- proceed, loudly
#
# The UNTESTED row is the honest handling of a machine that is already offline:
# the claim under test ("the lane set passes with no network") is satisfied
# directly, but the isolation MECHANISM was not exercised this run, and saying
# so is cheaper than pretending otherwise.
#
# usage: cpp-no-network.sh <command> [args...]
set -eu

if [ $# -eq 0 ]; then
  echo "usage: cpp-no-network.sh <command> [args...]" >&2
  exit 2
fi

PROBE_URL=${RIFT_NET_PROBE_URL:-https://github.com/}
probe() { curl -sS --max-time 8 -o /dev/null "$PROBE_URL" >/dev/null 2>&1; }

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

# ------------------------------------------------------- pick the mechanism
mech=
if command -v unshare >/dev/null 2>&1 && unshare -rn true >/dev/null 2>&1; then
  mech=unshare
elif command -v sandbox-exec >/dev/null 2>&1; then
  cat > "$scratch/nonet.sb" <<'SB'
(version 1)
(allow default)
(deny network*)
SB
  mech=sandbox-exec
fi

if [ -z "$mech" ]; then
  printf '\n  ==========================================================\n'
  printf '   cpp-ci: NO WORKING NETWORK ISOLATION ON THIS MACHINE\n'
  printf '  ----------------------------------------------------------\n'
  printf '   Tried: unshare -rn (Linux), sandbox-exec (macOS).\n'
  printf '   Refusing to run the lane set WITHOUT isolation and call it\n'
  printf '   cpp-ci. A lane that cannot enforce its premise must not report\n'
  printf '   a result about it.\n'
  printf '  ==========================================================\n\n'
  exit 2
fi

isolate() {
  case $mech in
    unshare)      unshare -rn -- "$@" ;;
    sandbox-exec) sandbox-exec -f "$scratch/nonet.sb" "$@" ;;
  esac
}

# ------------------------------------------------------------- the control
printf '\n  network isolation (%s)\n' "$mech"
printf '  ----------------------------------------------------------\n'

if probe; then
  outside=reachable
else
  outside=unreachable
fi
printf '   probe without isolation : %s\n' "$outside"

verdict=
if [ "$outside" = unreachable ]; then
  verdict=UNTESTED
else
  if isolate /usr/bin/env sh -c 'curl -sS --max-time 8 -o /dev/null "$0" >/dev/null 2>&1' "$PROBE_URL"; then
    printf '   probe with    isolation : reachable\n'
    printf '  ----------------------------------------------------------\n'
    printf '   INVALID  the isolation mechanism did not isolate.\n\n'
    printf '  Every lane run under it would be green for reasons unrelated to\n'
    printf '  the property being claimed. Refusing to run. Fix the mechanism.\n\n'
    exit 2
  fi
  printf '   probe with    isolation : blocked\n'
  verdict=VERIFIED
fi

if [ "$verdict" = UNTESTED ]; then
  printf '  ----------------------------------------------------------\n'
  printf '   ISOLATION UNTESTED -- this machine has no network to block.\n'
  printf '   The lane set below still runs with no network available, so the\n'
  printf '   claim holds for this run. What was NOT exercised is the isolation\n'
  printf '   mechanism itself, so this run is not evidence that %s works.\n' "$mech"
else
  printf '  ----------------------------------------------------------\n'
  printf '   ISOLATION VERIFIED -- reachable outside, blocked inside.\n'
fi
printf '   running: %s\n\n' "$*"

isolate "$@"
