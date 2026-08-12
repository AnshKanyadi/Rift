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
// # Messages cross the real wire codec
//
// Nodes share an address space, so a message passed by reference lets a sender
// mutate what a receiver reads: a bug class that cannot exist in production and
// a determinism leak that can. Encoding removes it, and pays a second time --
// every message in every soak run goes through the production encoder, so
// truncation and dropped fields are caught by the corpus rather than at I2. It
// also makes message size observable, which the delay model wants.
//
// Per-message dice come from a keyed PRF over (from, to, ordinal on that
// directed link), never a sequential draw. Traffic on one link therefore cannot
// perturb another link's outcomes, which is what makes a plan a total repro and
// the minimizer sound (DR-6). Reordering is emergent from independent latency
// rather than a knob; there is no reorder flag to look for.
//
// # Fire counts
//
// Every injector counts, and an enabled injector that never fired fails the
// run. Without that, a chaos suite is chaos-shaped decoration: every seed green
// and no partition ever formed. With it, a soak can say how many times each
// fault actually happened, which is what makes the safety claim mean something.
//
// Landed: the loop (step 1), transport and codec (step 3), injectors and fire
// counts (step 4). Still to come: plans as the repro unit (step 5), the oracle
// framework (step 6), the toy protocol (step 7), and the trace hash with its
// fresh-process gate (step 2, riding with simctl at step 8).
package sim
