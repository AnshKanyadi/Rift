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
	//
	// # What happens if neither name is ever claimed
	//
	// These are the only entries here for packages that do not exist, and an
	// exclusion for a package that never arrives is a permanent hole nobody is
	// watching: the day somebody creates engine/real for an unrelated reason, it
	// arrives already exempt from every rule, silently, having never been
	// argued for. That is the opposite of the default this table is built on,
	// where an unclassified package defaults *in*.
	//
	// The enforcement story, so the hole cannot outlive its reason:
	//
	//  1. Both lines are a **B5 exit item**. B5 is where the cgo wrapper lands
	//     and settles the name; the unclaimed line is deleted in that same PR,
	//     not in a follow-up, on the same rule as A2's "the mutant lands with
	//     the fix".
	//  2. Until then they are inert by construction. scopeFor matches on package
	//     path, and a path that names no package matches nothing, so today these
	//     two lines change no diagnostic on any file in the tree. The
	//     TestScopeTable rows below pin that: they assert the exclusion is exact
	//     rather than a prefix, so neither line can adopt a sibling.
	//  3. If B5 arrives and claims neither name, both lines are deleted rather
	//     than kept "just in case". A speculative exemption is a decision nobody
	//     made.
	"github.com/anshkanyadi/rift/engine/real/...",
	"github.com/anshkanyadi/rift/engine/pump/...",

	// The hunt driver. Orchestration by Amendment A5's own text, which names
	// hunters alongside real-mode drivers and cmd/. Named exactly, never as a
	// prefix: a wildcard here would silently adopt every future subpackage,
	// which is how a boundary becomes a hole.
	"github.com/anshkanyadi/rift/sim/hunt",

	// The real-mode driver. It holds the concurrency core packages may not: one
	// goroutine per node, real timers, and a mailbox channel. Amendment A5 --
	// code that needs a goroutine is orchestration and lives outside the
	// boundary, or the design is wrong.
	//
	// The node LOGIC it drives stays in scope. That split is the whole point:
	// node/ is a driver, and the sim.Node it drives is the same type the
	// simulator's loop drives, with no build tag and no branch on mode.
	"github.com/anshkanyadi/rift/node/...",

	// The differential judge (Track B, landing at the merge). It judges one run
	// of the differential rig: it shells out to the rig, reads its artifacts
	// off disk, and reports. Amendment A5's orchestration definition without
	// stretching it -- nothing in this package executes during a simulated run.
	//
	// Named exactly, never as a prefix, for the reason sim/hunt gives above.
	// The exclusion is about this package's right to SHELL OUT AND READ FILES.
	// It is not a licence to iterate a map: a judge whose output order depends
	// on map iteration reports the same divergences in a different order on two
	// runs of the same artifact, and its output cannot be diffed. That is a
	// determinism leak whichever scope the package sits in, and it is fixed in
	// the package rather than excused here.
	"github.com/anshkanyadi/rift/engine/differential",

	// The end-of-run checkers. History *collection* runs in-sim and stays
	// dependency-free inside the boundary; porcupine runs after the run is
	// over, is an external dependency, and needs a timeout -- so it lives out
	// here by ruling. Nothing in this package executes during a simulated run.
	"github.com/anshkanyadi/rift/sim/checker/...",
}

// defaultMailbox lists packages that get the mailbox rule: core state is reached
// only through the mailbox (DESIGN-A0 DR-2).
//
// It is empty, and that is the correct state rather than an omission.
//
// node/ now exists, and it is in defaultExclude above rather than here. The
// mailbox rule constrains packages that hold BOTH node state and concurrency;
// node/ holds only the concurrency. It reaches node logic through the sim.Node
// interface and cannot touch node state at all -- the compiler enforces that,
// which is a stronger guarantee than the analyzer rule was going to provide.
//
// The rule stays implemented and its fixtures stay green, because the day a
// package does hold both -- a store/ that embeds its own driver, say -- it goes
// here and the rule is already in force.
var defaultMailbox = []string{}

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

// isSynthesizedTestMain reports whether this is the test binary's main package,
// which the go tool generates, which imports os and flag, and which is nobody's
// code to fix.
//
// Both halves of the test matter. The path suffix alone would also exclude a
// real source directory named x.test, which is somebody's code and belongs in
// scope; the package name alone would exclude every command in the repo. Only
// the generated main satisfies both.
func isSynthesizedTestMain(path, pkgName string) bool {
	return pkgName == "main" && strings.HasSuffix(path, ".test")
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
