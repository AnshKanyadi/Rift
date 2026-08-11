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
// Packages fall into one of three scopes, listed in scope.go:
//
//   - core: raft, store, kv, router, balancer, engine, engine/model, sim/toy.
//     Pure state machines. Time, randomness, network and storage arrive through
//     injected interfaces and nothing else is touched.
//   - mailbox: node. The real-mode driver, which owns the goroutines core
//     packages may not have, under the rule that they reach core state only
//     through the mailbox (DESIGN-A0 DR-2).
//   - off: everything else. sim injects nondeterminism on purpose, clock is
//     where the real clock lives by name, and a pass that shouted at either
//     would be switched off within a week.
//
// A package's tests are checked under the same rules as the package, so the
// sorted-keys helper that map iteration must go through belongs outside the
// core packages.
//
// # Core rules
//
//   - time.Now, Since, Until, Sleep, After, AfterFunc, Tick, NewTimer,
//     NewTicker, Timer, Ticker. time.Duration and arithmetic on it are fine;
//     reading the wall clock is not.
//   - imports of os, net, path/filepath, io/ioutil, syscall, runtime, unsafe,
//     sync, sync/atomic, math/rand, math/rand/v2, log.
//   - go statements, select statements, channel types, sends and receives.
//   - range over a map -- the classic Go determinism leak, and the only one
//     that is both silent and common -- and range over a channel.
//   - pointer-keyed maps and %p in a format string, both of which put an
//     allocator address somewhere it can reach behaviour or the trace hash.
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
// rather than silently ignored. Every use is printed to stderr on every run, as
// is every hatch that excused nothing, so exemptions stay visible instead of
// accumulating.
//
// # Running it
//
//	make determinism
//	go run ./tools/determinismcheck/cmd/determinismcheck ./...
//
// See DESIGN-A0 D10 and DR-16. Landed in A0.3.
package determinismcheck
