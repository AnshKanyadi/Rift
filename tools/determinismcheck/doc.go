// Package determinismcheck is a go/analysis pass that turns Rift's determinism
// rules into build failures.
//
// The point is that "how do you know a time.Now() did not sneak in?" has a
// build failure as its answer rather than a promise. Rules that are enforced by
// review are violated roughly once per thousand lines, and the resulting bug
// does not present as a bug: it presents as a seed that replayed differently
// six weeks later, which is the exact failure mode this project exists to
// avoid.
//
// # Scope
//
// The governing principle, ruled 2026-08-11 (DESIGN-A0 D10, DR-23):
//
//	Any code that executes during a simulated run is in scope, no exceptions.
//	Orchestration around runs -- parallel hunting, real-mode drivers, cmd/ --
//	is not.
//
// Scope is therefore a question about when a package runs, not a judgement
// about how dangerous it looks. Packages fall into one of three, listed in
// scope.go:
//
//   - core: raft, store, kv, router, balancer, engine, clock, sim,
//     internal/rng, internal/sorted. Time, randomness, network and storage
//     arrive through injected interfaces and nothing else is touched.
//   - mailbox: node. The real-mode driver, which owns the goroutines core
//     packages may not have, under the rule that it reaches core state only
//     through the mailbox (DESIGN-A0 DR-2). Provisional until node/ exists.
//   - off: orchestration -- cmd, bench, chaos, soak, tools -- plus the
//     real-mode adapters named in the exclusion list, which wins over
//     everything.
//
// A subpackage nobody has classified is in scope, not out of it. A package's
// tests are checked under the same rules as the package.
//
// # Core rules
//
//   - time.Now, Since, Until, Sleep, After, AfterFunc, Tick, NewTimer,
//     NewTicker, Timer, Ticker. time.Duration and arithmetic on it are fine;
//     reading the wall clock is not.
//   - imports of os, net, path/filepath, io/ioutil, syscall, runtime, unsafe,
//     sync, sync/atomic, math/rand, math/rand/v2, maps, log.
//   - go statements, select statements, channel types, sends and receives.
//   - range over a map -- the classic Go determinism leak, and the only one
//     that is both silent and common -- and range over a channel.
//   - reflect.MapRange, MapKeys and MapIter, which reach map iteration through
//     method calls where neither the syntax rule nor an import ban can see it.
//     Together with the maps ban this closes the go1.23 iterator hole:
//     slices.Sorted(maps.Keys(m)) has no map-range syntax in it and is exactly
//     the same nondeterminism.
//   - pointer-keyed maps and %p in a format string, both of which put an
//     allocator address somewhere it can reach behaviour or the trace hash.
//
// Sorted iteration lives in internal/sorted and nowhere else: one helper, one
// hatch, one range statement in the repository.
//
// # Mailbox rule
//
// In a driver package, no method on a core type may be called except Handle and
// Status, and only post() may send on a channel. Constructors are permitted:
// building a node is not touching one. This is the second of three layers, the
// first being that core state is unexported in another package and the third
// being the -race lane.
//
// # Escape hatch
//
//	//rift:allow-nondeterminism <reason>
//
// Above the package clause it exempts the file; anywhere else it exempts its
// own line and the line below it. The reason is mandatory, a missing one is a
// diagnostic, and the near-miss form with a space after the slashes is reported
// rather than silently ignored.
//
// Three things keep the hatch from becoming the rule:
//
//   - A hatch that excused nothing is a diagnostic, not a warning. It has
//     either outlived its rule or drifted off the line it was written for, and
//     the second case means something is unguarded while its author believes
//     otherwise.
//   - Every declared hatch is announced and diffed against HATCHES.txt, so
//     adding one is a conscious edit to a checked-in list.
//   - No hatch sanctions go, select, chan or sync in core scope. Those are
//     refused outright: either the concurrency moves out of core, or the design
//     is wrong, and neither is something a comment can fix.
//
// # Running it
//
//	make determinism   # the lane CI fails on
//	make blind         # blind each rule in turn; its test must fail
//	go test ./tools/determinismcheck -update-hatches   # regenerate HATCHES.txt
//
// See DESIGN-A0 D10, DR-16, and DR-23 through DR-25. Landed in A0.3.
package determinismcheck
