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
	// DR-11's poller was reserved here under two candidate names, "whichever
	// of these two names A0.5 and B5 settle on, the other line goes."
	//
	// BOTH WENT. B1-Q11, ruled at B5: the poller is part of the HARNESS, not
	// the engine's contract -- a production embedder supplies its own. So
	// there is no engine/real and no engine/pump, and a reservation for a
	// package that will never exist is a hole in the boundary held open for
	// nothing. What actually arrived is the cgo adapter below, which is a
	// different thing: an Engine implementation, not a poller.
	//
	// BOTH ENTRIES BELOW ARE EXACT, never a prefix. They arrived from Track B
	// wildcarded; the wildcard is what blind-hunt-wildcard and
	// blind-differential-wildcard exist to kill, and these two sit directly
	// beneath engine/, the pattern that puts engine/model in scope, so a
	// wildcard here is the highest-consequence version of that mistake.
	//
	// THE CGO ENGINE. An exclusion rather than a hatch, by this table's own
	// test: it needs `sync` for its durability callbacks, and no hatch
	// sanctions sync in core scope. `unsafe` is the second reason and the more
	// fundamental one -- a cgo boundary is pointer identity and layout by
	// construction, which is exactly what core scope exists to keep out.
	//
	// AND IT IS THE CONSTITUTION'S OWN SCOPING, not a hole: "deterministic-
	// replay guarantees are scoped to sim runs on engine/model; C++ engine
	// correctness comes from the Env fault rig, differential tests, corpus
	// reruns in verification mode, and real chaos." Replay identity is defined
	// on the model. This package is checked by cpp-cgo and by the differential,
	// which are the instruments that claim apply to it.
	//
	// PROVISIONAL, and the mark is the point. Ansh at I1: "an argued property
	// and a measured one are different objects, and riftcgo is the first
	// package in either track whose scope was decided by reasoning alone." The
	// argument above stands and is better than the prediction it replaced; what
	// has not happened is the PASS running against this package with the C++
	// archive built, which is the only thing that can agree or disagree with
	// it. That runs at I1. If it agrees the note comes off and the argument is
	// vindicated by measurement; if it disagrees we have learned something
	// worth more than the exclusion.
	//
	// AND THE PREDICTION IS RECORDED BESIDE IT, so the answer can be checked
	// against what was guessed rather than quietly replacing it. Ansh, at the
	// classification: "My prior is that the cgo wrapper is core-scope code with
	// a hatch for the boundary rather than orchestration, since it implements
	// the frozen Engine interface and runs inside simulated runs at I1 -- but
	// that is a prediction and the pass should decide it." The argument above
	// disagrees with that prior and Ansh ratified the argument. Whichever the
	// pass says, one of the two was wrong about this package, and I1's report
	// says which.
	"github.com/anshkanyadi/rift/engine/riftcgo",

	// The B4 differential judge. Nothing in it executes during a simulated
	// run: it reads artifact FILES that a finished run left behind, and its
	// end-to-end test spawns the C++ writer with os/exec. That is
	// sim/checker's situation exactly, and it gets sim/checker's answer.
	//
	// THE JUDGE'S INDEPENDENCE IS WHY IT CANNOT BE IN CORE. It must reach the
	// filesystem, because reading bytes neither engine handed it is the whole
	// mechanism by which it is a second opinion rather than a mirror.
	//
	// And the exclusion is about this package's right to SHELL OUT AND READ
	// FILES. It is not a licence to iterate a map: a judge whose output order
	// depends on map iteration reports the same divergences in a different
	// order on two runs of the same artifact, and its output cannot be diffed.
	// That is a determinism leak whichever scope the package sits in, and
	// compare() routes through internal/sorted for that reason rather than
	// being excused here.
	"github.com/anshkanyadi/rift/engine/differential",

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
