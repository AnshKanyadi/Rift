// Package determinismcheck is a go/analysis pass that turns Rift's determinism
// rules into build failures.
//
// Over raft/, store/, kv/, router/, balancer/, and engine/model it rejects:
// time.Now and friends, package-level math/rand, imports of net/os/filepath,
// go statements, select statements, channel types, sync primitives, unsafe,
// pointer-keyed maps -- and range over a map, which is the classic Go
// determinism leak and the only one that is both silent and common.
//
// Over node/ it enforces the mailbox rule: no goroutine may touch core state
// except through Handle and Status.
//
// The escape hatch (//rift:allow-nondeterminism <reason>) requires a written
// reason and every use is printed in the CI build log, so exceptions stay
// visible instead of accumulating.
//
// The point is that "how do you know a time.Now() did not sneak in?" has a
// build failure as its answer rather than a promise.
//
// See DESIGN-A0 DR-16. Lands in A0.3.
package determinismcheck
