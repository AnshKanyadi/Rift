// Package provenance types a fact by where it came from.
//
// # The defect this exists to make impossible
//
// The persist-before-reply oracle judged every acknowledgement against a record
// of what each node had made durable. That record was built by reading the
// engine back — and an engine read returns the VISIBLE state, which by
// construction includes batches applied and not yet synced (DR-15). So the
// oracle was comparing the system's claims against the system's own account of
// itself, one layer of indirection removed through the ledger.
//
// It did not report false violations. It reported nothing: an inflated
// durability watermark makes every acknowledgement look covered. Measured across
// 10,000 seeds, the read-back was ahead of true durability 44,911 times. The
// moment the record told the truth, a 300-seed sweep went from 2 violations to
// 257.
//
// That is oracle independence failing inside the mechanism oracle independence
// exists to protect, and nothing in the type system objected.
//
// # The rule, and why it is about PASSING rather than about reading
//
// A system-reported fact is not forbidden. It is forbidden as an input to a
// verdict that can come out green.
//
//	raft.AssertQuiescent reads r.gated, which is node state, deliberately. It
//	can only make a run FAIL. A node that lied about its withheld queue would
//	buy itself nothing, so the direction of the error is safe.
//
//	The ledger's durable record can make a run PASS. A node — or an engine —
//	that overstates what it holds buys itself a green, and that is the whole
//	failure.
//
// So the two kinds of fact get two types, and the ledger accepts only one of
// them. Handing a Reported where an Observed is wanted does not fail a run; it
// fails the build.
//
// # What this does and does not guarantee
//
// It guarantees that the specific wiring mistake that happened cannot be made
// silently: readDurable's result no longer fits where the ledger's input goes.
//
// It does not make laundering impossible. Witness(x.Unverified()) compiles, and
// so does Wall(mono) in clock — the same limit applies to every type of this
// shape. What the type buys is that laundering must be WRITTEN, in one
// expression, with both halves named; tools/provcheck fails the build when
// anyone writes it. A rule you have to type out is a rule somebody reviews.
//
// Fifth instance of the house move, after Wall/Mono, the epoch stamp, the D5
// conformance check and markFor's refusal: fix the class by making it
// unrepresentable rather than catchable.
package provenance

// Observed is a fact the harness witnessed at a component's boundary: a message
// released, a batch handed to the engine, a durability completion received, an
// entry handed over for application.
//
// The test for "observed" is not who called Witness. It is whether the value
// crossed the interface under its own power, so that a wrapper around that
// interface could have recorded it without asking anybody anything.
type Observed[T any] struct{ v T }

// Witness records a boundary observation.
func Witness[T any](v T) Observed[T] { return Observed[T]{v: v} }

// Fact returns the observed value.
func (o Observed[T]) Fact() T { return o.v }

// Reported is the system under test's own account of its state: an engine
// read-back, a role field, a commit index, anything the system answered a
// question with.
//
// It is a legitimate input to a recovery path and to an assertion that can only
// fail. It is not an input to a verdict.
type Reported[T any] struct{ v T }

// Claim records something the system said about itself.
func Claim[T any](v T) Reported[T] { return Reported[T]{v: v} }

// Unverified returns the reported value, and is named to be uncomfortable at
// the call site. Every use should be either a recovery path acting on the
// system's own state, or a cross-check against something independently
// observed.
func (r Reported[T]) Unverified() T { return r.v }
