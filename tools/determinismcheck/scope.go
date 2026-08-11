package determinismcheck

import "strings"

// scope is the rule set a package is checked under. Most of the repo is
// scopeOff: the simulator owns clocks, goroutines and files on purpose, and a
// pass that shouted at it would be turned off within a week.
type scope int

const (
	scopeOff scope = iota
	scopeCore
	scopeMailbox
)

// defaultCore lists the packages that must be deterministic by construction:
// pure state machines that take their time, randomness, network and storage
// from injected interfaces and touch nothing else.
//
// DESIGN-A0 D10 names raft, store, kv, router, balancer and engine/model. Three
// additions, each stricter than the letter of that list and none of them
// arbitrary:
//
//   - engine: interface definitions only, and they should stay that way. Note
//     this is the exact path, not engine/..., because the future cgo wrapper
//     needs the poller goroutine that DR-11 puts there.
//   - sim/toy: node logic, just without elections. It has to be here or the
//     M5-wall-clock mutant is not a compile-time catch, which DESIGN-A0 §5's
//     mutant table says it is.
//   - node: covered by defaultMailbox below rather than by the core rules,
//     since a driver's whole job is the goroutines core packages may not have.
//
// Deliberately absent: clock (CLAUDE.md exempts the real clock implementation
// by name), sim (owns the nondeterminism it injects), internal/rng, cmd, tools.
var defaultCore = []string{
	"github.com/anshkanyadi/rift/raft/...",
	"github.com/anshkanyadi/rift/store/...",
	"github.com/anshkanyadi/rift/kv/...",
	"github.com/anshkanyadi/rift/router/...",
	"github.com/anshkanyadi/rift/balancer/...",
	"github.com/anshkanyadi/rift/engine",
	"github.com/anshkanyadi/rift/engine/model/...",
	"github.com/anshkanyadi/rift/sim/toy/...",
}

// defaultMailbox lists the real-mode driver packages, which exist precisely to
// hold the concurrency core packages may not (DESIGN-A0 DR-2). They get the
// mailbox rule instead: core state is reached only through the mailbox.
//
// node/ does not exist yet -- it lands with the real-mode driver. The rule is
// written now so that it is in force the day the package appears, rather than
// being retrofitted onto code that has already grown around its absence.
var defaultMailbox = []string{
	"github.com/anshkanyadi/rift/node/...",
}

func scopeFor(path string) scope {
	path = normalize(path)
	// Mailbox is tested first: if a pattern ever covers a package both ways,
	// the driver rules are the ones that can actually be satisfied.
	switch {
	case matchAny(splitPatterns(flagMailbox), path):
		return scopeMailbox
	case matchAny(splitPatterns(flagCore), path):
		return scopeCore
	default:
		return scopeOff
	}
}

// isCorePkg reports whether path holds core types, which is what the mailbox
// rule is defined against.
func isCorePkg(path string) bool {
	return matchAny(splitPatterns(flagCore), normalize(path))
}

// normalize strips the _test suffix Go gives an external test package, so a
// package's tests are checked under the same rules as the package itself. A
// determinism leak in a test helper is still a leak; it just costs a flaky test
// instead of a flaky database. The cost is that the sorted-keys helper every
// map iteration must go through has to live outside the core packages -- which
// is where a single blessed implementation belongs anyway.
func normalize(path string) string {
	return strings.TrimSuffix(path, "_test")
}

func splitPatterns(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func matchAny(pats []string, path string) bool {
	for _, p := range pats {
		if match(p, path) {
			return true
		}
	}
	return false
}

// match understands the two forms go itself uses: an exact package path, and a
// "p/..." pattern covering p and everything beneath it. Nothing more: a pattern
// language in a rule table is a place for rules to hide.
func match(pat, path string) bool {
	if base, ok := strings.CutSuffix(pat, "/..."); ok {
		return path == base || strings.HasPrefix(path, base+"/")
	}
	return pat == path
}
