// Package sim is the deterministic simulator: one event queue, one event at a
// time, virtual nanosecond timestamps, total order by (at, insertion_seq).
//
// It owns the fault injectors (drop, delay, duplicate, asymmetric and symmetric
// partitions, crash, restart, pause, sync-loss windows, clock drift and jumps),
// the checkers, the trace hash that gates determinism, and the fault plan --
// the serializable artifact that makes a failing run reproducible at any commit
// rather than only at the one that produced it.
//
// Plan execution takes no sequential randomness at all: every random quantity
// is either materialized in the plan or derived by a stateless keyed PRF over a
// canonical event identity. A poisoned Rand panics if any sequential draw is
// attempted, so "a plan is a complete reproduction" is enforced rather than
// promised.
//
// See DESIGN-A0 DR-3, DR-5, DR-6, DR-14, DR-17, DR-18. Lands in A0.6-A0.9.
package sim
