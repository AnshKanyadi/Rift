package sim

import "fmt"

// Epoch identifies one incarnation of a component.
//
// # The class: a completion from a dead incarnation reaching a live component
//
// This project has rediscovered the same bug three times, once per component
// that models a crash:
//
//	TOY-001's sibling, TOY-003  a durability completion scheduled before a
//	                            crash landed after the restart, advancing the
//	                            watermark past everything applied.
//	store restart bug (BUG-002) a second restart replaced the Raft while the
//	                            previous incarnation's pending marks survived,
//	                            so a fresh Raft was handed an acknowledgement
//	                            for a mark it never issued.
//	the durability panic        "durability advanced to 35, past the last
//	                            applied sequence 34" -- the same completion,
//	                            reaching the same engine, one component along.
//
// Each was found separately and patched separately, with a different ad-hoc
// test each time -- a sequence comparison here, a field reset there. **Nothing
// made it impossible, so every new component rediscovered it.**
//
// The fix is the Wall/Mono move: make the bug unrepresentable rather than
// catchable. Every incarnation carries a monotonically increasing epoch; every
// mark, acknowledgement and sync completion is stamped with the epoch that
// issued it; and delivery to a different epoch is dropped and counted rather
// than tolerated silently. A component cannot act on another incarnation's news
// because it cannot mistake it for its own.
type Epoch uint64

// Stamped carries a value together with the incarnation that issued it.
//
// Its zero value is deliberately useless: Epoch 0 is "no incarnation", so a
// forgotten stamp is refused by Accept rather than defaulting into being
// accepted -- the same derived-field discipline as the plan's nonzero wall epoch
// and clock.Hold's unset realization.
type Stamped[T any] struct {
	Epoch Epoch
	Value T
}

// Stamp pairs a value with the epoch issuing it.
func Stamp[T any](e Epoch, v T) Stamped[T] { return Stamped[T]{Epoch: e, Value: v} }

// EpochGuard drops cross-epoch deliveries and counts them.
//
// The count is the observable, and what it MEANS depends on whether the
// component could have prevented the delivery:
//
//   - A component that emits its own completions can decline to emit one from a
//     dead incarnation, so a nonzero count there is a defect in it. Check is for
//     those, and reads any drop as a failure.
//   - A component driven by the simulator's event queue cannot: a durability
//     event scheduled before a crash is delivered after the restart no matter
//     what, and the stamp is the only thing that can tell it apart. store.Node
//     is one of these. There the count is a fact about the SCHEDULE, and the
//     interesting failure is a count of zero, which means the race never
//     occurred and any test resting on the guard proved nothing.
//
// Getting this backwards costs either a lane that fails for doing its job or a
// mechanism nobody ever asks about. sim/hunt's
// TestStaleDurabilityCompletionIsRefused asks in the second sense.
type EpochGuard struct {
	current Epoch
	dropped int
}

// NewEpochGuard starts at epoch 1, since epoch 0 means "no incarnation".
func NewEpochGuard() *EpochGuard { return &EpochGuard{current: 1} }

// Current is the live incarnation.
func (g *EpochGuard) Current() Epoch { return g.current }

// Advance begins a new incarnation and returns it. Called on every crash and
// every restart: each is a distinct incarnation, and treating a restart as a
// continuation is how the store bug survived.
func (g *EpochGuard) Advance() Epoch {
	g.current++
	return g.current
}

// Accept reports whether a stamped delivery belongs to the live incarnation.
// Anything else is dropped and counted.
func (g *EpochGuard) Accept(e Epoch) bool {
	if e == g.current && e != 0 {
		return true
	}
	g.dropped++
	return false
}

// Dropped is how many cross-epoch deliveries were refused.
func (g *EpochGuard) Dropped() int { return g.dropped }

// Check refuses a run in which any cross-epoch delivery occurred.
func (g *EpochGuard) Check(who string) error {
	if g.dropped == 0 {
		return nil
	}
	return fmt.Errorf(
		"sim: %s received %d completion(s) from a dead incarnation; each was dropped rather than "+
			"acted on, but a driver that lets a dead incarnation's news reach a live component is "+
			"the class that produced TOY-003, the store restart bug and the durability panic",
		who, g.dropped)
}
