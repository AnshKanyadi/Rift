// Package clock provides Rift's time abstraction: a physical clock with a
// configured maxOffset bound, in simulated and real implementations, and later
// the hybrid logical clock built on top of it (A5).
//
// No package outside this one may read wall-clock time. In simulation a node's
// clock is a piecewise-linear offset schedule over virtual time, so a drift
// rate is a sloped segment and a sustained hold at a chosen skew is a flat one.
// Node ticks derive from the node-local clock, so a drifted node also campaigns
// and heartbeats on a drifted schedule.
//
// See DESIGN-A0 DR-15. Lands in A0.4.
package clock
