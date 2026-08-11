// Package rng is Rift's own pseudorandom generator: PCG64 with pinned
// known-answer test vectors, plus named sub-streams derived deterministically
// from a parent seed.
//
// It exists instead of math/rand because the seed corpus is a permanent
// artifact. Go's compatibility promise for math/rand/v2's convenience mappings
// is not strong enough to guarantee that a seed recorded today reproduces the
// same schedule after a toolchain upgrade, and a silent change there would
// leave every corpus entry self-consistent but different -- the worst possible
// failure mode. Owning the generator makes stream stability our decision.
//
// Two facilities matter to callers:
//
//   - Derive(name) returns an independent sub-stream, so a change to how one
//     subsystem consumes randomness cannot perturb another's schedule.
//   - The keyed PRF is stateless: given a key and a canonical event identity it
//     returns the same value regardless of call order. Plan execution uses only
//     the PRF, never a sequential stream, which is what lets a serialized plan
//     reproduce a run with no live randomness.
//
// See DESIGN-A0 DR-4 and DR-6. Lands in A0.2.
package rng
