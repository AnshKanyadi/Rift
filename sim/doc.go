// Package sim is the deterministic simulator: one event queue, one node at a
// time, virtual time that advances only at event boundaries.
//
// # The total order
//
// Every event is ordered by (at_nanos, insertion_seq). The instant comes from
// whoever scheduled it; the sequence is a counter this package owns. The second
// half is not a tie-break of convenience -- container/heap is not stable and
// neither is the heap here, so without an explicit counter two events at the
// same nanosecond would order by whatever the heap did last, and the simulator
// would be subtly irreproducible in a way no single run reveals (DESIGN-A0 D2).
//
// # What a node is
//
// A synchronous, non-blocking state machine with one entry point. The loop
// calls Handle; Handle runs to completion and may schedule future events. There
// are no goroutines inside a node, so a data race in node logic is not unlikely
// but unrepresentable, and the same Handle runs in real mode behind a mailbox
// so the two modes cannot drift apart in behaviour (DESIGN-A0 D1).
//
// # Ticks come from each node's own clock
//
// The loop asks a node's clock when its next tick falls, which is the
// closed-form inverse of that node's oscillator (DESIGN-A0.4 D3). A node whose
// crystal runs fast reaches its next tick earlier in global time, so it
// campaigns and heartbeats fast -- drift shapes the schedule and not only the
// reported time, which is what makes the injector worth having.
//
// A restart begins a new boot, so the monotonic curve restarts at zero and the
// tick schedule restarts with it.
//
// # Stopping
//
// Run returns a closed enum, not a bool, and the distinction is load-bearing: a
// run that went quiescent early did less than its configured duration suggests,
// and SOAK.md counts only completed-at-deadline runs toward cumulative hours.
// Ticks are therefore scheduled past the deadline rather than suppressed at it,
// so a tick-only run ends at the deadline with a non-empty queue and says so.
//
// Outcome.CountsTowardSoakHours is the one place that rule lives, and
// determinismcheck's exhaustive rule requires every switch over OutcomeKind to
// cover all variants with no default arm -- so adding a kind breaks every
// consumer that has not decided what to do about it.
//
// Landed in A0.6 (checklist step 1). Still to come: the transport and its
// injectors (step 3/4), plans as the repro unit (step 5), the oracle framework
// (step 6), and the trace hash with its fresh-process gate (step 2, riding with
// simctl at step 8).
package sim
