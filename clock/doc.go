// Package clock provides Rift's time abstraction: two readings per node off one
// oscillator, in simulated and real implementations, with the hybrid logical
// clock built on top of it later (A5).
//
// No package outside this one may read wall-clock time. There is exactly one
// wall-clock call in the repository, in real.go, under a hatch registered in
// HATCHES.txt; a second would fail `make hatches`.
//
// # Two readings, one oscillator
//
//   - Mono is elapsed time on the node's oscillator since this boot. Drift
//     bends it; steps never touch it. Everything measuring a timeout reads it,
//     and the tick schedule derives from it, so a node whose crystal runs fast
//     campaigns and heartbeats fast.
//   - Wall is the node's estimate of physical time: the oscillator plus
//     accumulated steps. It moves backwards when a step does and survives a
//     restart. maxOffset bounds the disagreement between two nodes' Walls, and
//     leases, uncertainty intervals and the A5 HLC all read it.
//
// One clock would have to serve both, and would then rewind timers on a
// backward NTP step -- which no real system does, because timers read
// CLOCK_MONOTONIC. We would have spent seed budget on artifacts that cannot
// happen in production while hiding the ones that can (DESIGN-A0.4 D1).
//
// The monotonic epoch is per-boot. A Mono value is therefore meaningful only as
// a difference between two readings on the same node within one boot: never
// persist one, send one on the wire, or compare one across nodes or across a
// restart. A lease expiry stored as a Mono value survives a restart as a number
// from a timeline that no longer exists, and the node then serves reads under a
// lease it does not hold.
//
// # Schedules
//
// A Timeline is a pure function of global virtual time: a piecewise-linear
// oscillator offset, a list of wall-only steps, and the boot instants at which
// Mono restarts. Nothing here reads a clock, consumes randomness, or keeps a
// cursor, so a reading depends on nothing but the timeline and the instant --
// which is what evaluating in a shuffled order is tested to prove.
//
// Slopes are integers in parts per billion, and no float64 appears on the
// evaluation path. The Go spec permits fusing a multiply-add into one FMA,
// which arm64 does and amd64 without FMA does not, and an offset is exactly
// that shape: the same seed would produce readings differing in the last bit on
// two machines, and a one-nanosecond difference in a lease expiry is a
// different history.
//
// Holds are authored as pairwise intent -- pin these two nodes at 0.98 of
// maxOffset from here to there -- and compiled to segments or steps. The
// realization is recorded, because a step and a slew stress different
// consumers: a step is the HLC case, a slew is the lease-stasis case, and a
// bundle that says only "the clocks disagreed by 490ms" cannot tell an
// investigator which they are looking at. A correction wider than its ramp is
// rejected: slewing it would need the oscillator to run backwards, which is the
// same reason real implementations rate-limit slewing.
//
// # The skew checker
//
// MaxSkew is exact rather than sampled. Between consecutive breakpoints both
// walls are linear, so their difference is linear and its extremum sits at an
// endpoint; evaluating at the union of both timelines' breakpoints -- including
// ramp endpoints, window edges, and both one-sided limits at every step -- is
// the answer rather than a good approximation of it. A test induces the failure
// that claim exists to prevent: a schedule whose peak lies strictly inside a
// ramp, which a sampling checker passes and this one catches.
//
// See DESIGN-A0.4, and DESIGN-A0 DR-15. Landed in A0.4.
package clock
