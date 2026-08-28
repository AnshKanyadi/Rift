# The thinness rules for engine-cpp/src/env/posix (B1-Q12 measure 1).
#
# PosixEnv is unverified by every lane in B1 and it is the component that talks
# to the actual disk. Its correctness rests on exactly two things: (a) the
# thinness of the implementation, and (b) the seam tests. (b) was already a
# checked property and (a) was a belief about a person. These rules remove the
# asymmetry.
#
# Emits one line per violation: RULE|FILE|ANCHOR
BEGIN { depth = 0; inm = 0 }

{
  raw = $0
  code = $0
  sub(/[ \t]*\/\/.*$/, "", code)

  if (!inm && code ~ /^(  )?([A-Za-z_][A-Za-z0-9_:<>,*& ]*)[ \t]+[A-Za-z_][A-Za-z0-9_]*\(.*\)[^;]*\{[ \t]*$/) {
    name = raw; sub(/^[ \t]*/, "", name); sub(/[ \t]*\{[ \t]*$/, "", name)
    inm = 1; depth = 1; lines = 0
    next
  }
  if (!inm) next

  # POSIX-THIN-LOOP: a loop that is not a documented retry loop.
  #
  # "No branching beyond a documented retry loop" is the ruled wording, and
  # `documented` is made mechanical by requiring the marker on the loop's own
  # line. An enumeration loop or a state machine hiding in PosixEnv is exactly
  # the logic the thinness argument claims is not there.
  if (code ~ /(^|[^a-zA-Z_])(while|for)[ \t]*\(/ && raw !~ /RIFT_POSIX_RETRY/) {
    a = raw; sub(/^[ \t]*/, "", a); sub(/[ \t]*$/, "", a)
    printf "POSIX-THIN-LOOP|%s|%s\n", FILENAME, a
  }

  o = gsub(/\{/, "{", code); c = gsub(/\}/, "}", code); depth += o - c
  if (depth <= 0) {
    if (lines > CAP) {
      printf "POSIX-THIN-LINES|%s|%s\n", FILENAME, name
    }
    inm = 0
    next
  }
  t = code; gsub(/[ \t]/, "", t)
  if (t != "") lines++
}
