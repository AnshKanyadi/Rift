// Package hunt drives runs. It is orchestration, not simulation.
//
// Amendment A5 draws the boundary by when code runs: anything executing
// *during* a simulated run is deterministic by construction, and orchestration
// *around* runs -- hunters, real-mode drivers, cmd/ -- is not required to be.
// This package is named in `determinismcheck`'s exclusion list for exactly that
// reason, by name and never as a prefix.
//
// # Why the exclusion is safe, not merely permitted
//
// The one thing this package does that a core package may not is read the wall
// clock, to report seeds per second. Wall time provably does not reach run
// identity, and the determinism lane already proves it without new machinery:
// the same seed produces the same trace across two runs whose *measured wall
// times differ*, because nothing inside a run can observe how long the harness
// took. If wall time could reach run identity, the determinism gate would
// already be failing.
//
// # Two conditions that keep the classification from rotting
//
//  1. The exclusion names this package exactly. A prefix or a wildcard would
//     silently adopt every future subpackage, which is how a boundary becomes a
//     hole.
//  2. **No package in core scope may import this one.** A boundary is only real
//     if it cannot be crossed inward, and that direction is asserted
//     mechanically by TestNoCorePackageImportsOrchestration rather than left to
//     review.
package hunt
