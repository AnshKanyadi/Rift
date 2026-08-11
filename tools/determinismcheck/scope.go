package determinismcheck

import "strings"

// scope is the rule set a package is checked under.
type scope int

const (
	scopeOff scope = iota
	scopeCore
	scopeMailbox
)

// The governing principle, ruled by Ansh on 2026-08-11 and recorded in
// DESIGN-A0 D10:
//
//	Any code that executes during a simulated run is in scope, no exceptions.
//	Orchestration around runs -- parallel hunting, real-mode drivers, cmd/ --
//	is not.
//
// Everything below is that sentence applied to the tree. When a new package
// appears, the question is not what it contains but when it runs.
var defaultCore = []string{
	"github.com/anshkanyadi/rift/raft/...",
	"github.com/anshkanyadi/rift/store/...",
	"github.com/anshkanyadi/rift/kv/...",
	"github.com/anshkanyadi/rift/router/...",
	"github.com/anshkanyadi/rift/balancer/...",

	// engine and engine/model execute inside every sim run, and replay
	// identity is defined on the model. This is the package the pass exists to
	// protect; the real-mode adapters that cannot obey it are excluded below,
	// by name, rather than by leaving a hole in the pattern.
	"github.com/anshkanyadi/rift/engine/...",

	// The sim clock runs in-band. The real implementation takes a per-line
	// hatch on its single time.Now rather than an exclusion, so the repo's
	// wall-clock touchpoints stay enumerable from HATCHES.txt.
	"github.com/anshkanyadi/rift/clock/...",

	// The simulator itself: event loop, transport, injectors, oracles, and the
	// toy protocol the harness is calibrated against. It is seeded, so it needs
	// none of what the rules ban.
	//
	// The parts of sim/ that orchestrate rather than execute -- plan and bundle
	// file I/O, the parallel hunter -- must land in their own subpackages and
	// be added to defaultExclude when they do. Being in scope from today is the
	// forcing function for that split, while the moves are still cheap.
	"github.com/anshkanyadi/rift/sim/...",

	// The RNG needs nothing the pass bans, and the pass is what stops someone
	// importing math/rand inside it later -- the exact bug DESIGN-A0 D3 exists
	// to kill.
	"github.com/anshkanyadi/rift/internal/rng/...",

	// The only package in the repo that iterates a map, under a hatch. In
	// scope, deliberately: an excluded package would hide the exception, and
	// the point is that every determinism exception is a hatch.
	"github.com/anshkanyadi/rift/internal/sorted/...",
}

// defaultExclude wins over everything. It is for packages that sit under a core
// pattern but run outside a simulated run and need what the core rules forbid.
//
// The distinction that decides exclusion versus hatch: a hatch can excuse a
// symbol, and concurrency is never hatchable (see hardRule). A package that
// needs a goroutine is therefore an exclusion; a package that needs one
// time.Now is a hatch.
var defaultExclude = []string{
	// DR-11's poller, which adapts the C++ engine's blocking Sync() to the
	// async durability contract. Whichever of these two names A0.5 and B5
	// settle on, the other line goes.
	"github.com/anshkanyadi/rift/engine/real/...",
	"github.com/anshkanyadi/rift/engine/pump/...",
}

// defaultMailbox lists the real-mode driver packages, which exist to hold the
// concurrency core packages may not (DESIGN-A0 DR-2). They get the mailbox rule
// instead: core state is reached only through the mailbox.
//
// node/ does not exist yet -- it lands with the real-mode driver. The rule is
// written now so it is in force the day the package appears rather than
// retrofitted onto code that has grown around its absence. Until then it is
// proven by fixtures only, and DESIGN-A0 marks it provisional: A0 does not exit
// until node/ exists and the rule has end-to-end teeth.
var defaultMailbox = []string{
	"github.com/anshkanyadi/rift/node/...",
}

func scopeFor(path string) scope {
	path = normalize(path)
	switch {
	case matchAny(splitPatterns(flagExclude), path):
		return scopeOff
	case matchAny(splitPatterns(flagMailbox), path):
		return scopeMailbox
	case matchAny(splitPatterns(flagCore), path):
		return scopeCore
	default:
		return scopeOff
	}
}

// isCorePkg reports whether path holds core types, which is what the mailbox
// rule is defined against. It goes through scopeFor so exclusions apply: a type
// from engine/real is not core state.
func isCorePkg(path string) bool { return scopeFor(path) == scopeCore }

// normalize strips the _test suffix Go gives an external test package, so a
// package's tests are checked under the same rules as the package itself. A
// determinism leak in a test helper is still a leak; it just costs a flaky test
// instead of a flaky database.
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
