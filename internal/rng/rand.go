package rng

import "math/bits"

// Rand is the injected source of randomness. Nothing in Rift reads a
// package-level generator; a Rand is always passed in, always owned by whoever
// controls the seed, and in plan-execution mode is always poisoned.
//
// Two facilities beyond the usual draws matter:
//
//   - Derive returns an independent named sub-stream, so a change to how one
//     subsystem consumes randomness cannot shift another's schedule. Streams
//     are memoized per name and derived from the parent's key rather than its
//     position, so creation order is irrelevant.
//   - DeriveKey returns a key for the stateless PRF (see prf.go). Plan
//     generation pulls keys out this way and writes them into the plan; plan
//     execution then takes no sequential draws at all.
type Rand interface {
	// Uint64 returns a uniformly distributed 64-bit value.
	Uint64() uint64

	// Uint64N returns a uniformly distributed value in [0, n). It panics if
	// n == 0. Exactly uniform: biased candidates are rejected and redrawn.
	Uint64N(n uint64) uint64

	// IntN returns a uniformly distributed value in [0, n). It panics if n <= 0.
	IntN(n int) int

	// Float64 returns a uniformly distributed value in [0, 1) with 53 bits of
	// precision.
	//
	//rift:allow-nondeterminism generation-side only; the value is exact (see float.go) and is materialized as an integer in the plan before anything evaluates it
	Float64() float64

	// Bool reports true with probability p. p <= 0 is never, p >= 1 is always.
	//
	//rift:allow-nondeterminism generation-side only; p is plan-authored intent and the outcome is a bool, so no float crosses into evaluation
	Bool(p float64) bool

	// Shuffle permutes n elements using swap, by Fisher-Yates descending.
	Shuffle(n int, swap func(i, j int))

	// Perm returns a permutation of [0, n).
	Perm(n int) []int

	// Derive returns the named sub-stream, creating it on first use. Calling
	// Derive twice with the same name returns the same stream.
	Derive(name string) Rand

	// DeriveKey returns the named PRF key. Deterministic and stateless: it does
	// not consume from this stream, and repeated calls return the same key.
	DeriveKey(name string) Key
}

// compile-time assertion: *PCG is the production Rand.
var _ Rand = (*PCG)(nil)

// Uint64N returns a uniformly distributed value in [0, n).
//
// Lemire's nearly-divisionless method: take the high half of a 64x64 widening
// multiply, and reject only when the low half falls in the short interval that
// would otherwise make some outputs marginally more likely. The rejection loop
// is what makes this exactly uniform rather than approximately so; the PRF in
// prf.go deliberately skips it and documents the resulting bias, because a
// stateless function has no "draw again".
func (p *PCG) Uint64N(n uint64) uint64 {
	if n == 0 {
		panic("rng: Uint64N called with n == 0")
	}
	hi, lo := bits.Mul64(p.Uint64(), n)
	if lo < n {
		threshold := (-n) % n
		for lo < threshold {
			hi, lo = bits.Mul64(p.Uint64(), n)
		}
	}
	return hi
}

// IntN returns a uniformly distributed value in [0, n).
func (p *PCG) IntN(n int) int {
	if n <= 0 {
		panic("rng: IntN called with n <= 0")
	}
	return int(p.Uint64N(uint64(n)))
}

// Shuffle permutes n elements using swap, by Fisher-Yates descending.
func (p *PCG) Shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("rng: Shuffle called with n < 0")
	}
	for i := n - 1; i > 0; i-- {
		j := int(p.Uint64N(uint64(i + 1)))
		swap(i, j)
	}
}

// Perm returns a permutation of [0, n).
func (p *PCG) Perm(n int) []int {
	if n < 0 {
		panic("rng: Perm called with n < 0")
	}
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	p.Shuffle(n, func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// Derive returns the named sub-stream, creating it on first use.
func (p *PCG) Derive(name string) Rand {
	if c, ok := p.children[name]; ok {
		return c
	}
	c := NewFromKey(p.DeriveKey(name))
	if p.children == nil {
		p.children = make(map[string]*PCG)
	}
	p.children[name] = c
	return c
}

// DeriveKey returns the named PRF key.
func (p *PCG) DeriveKey(name string) Key {
	return deriveKey(p.key, name)
}

// Key returns this stream's derivation material.
func (p *PCG) Key() Key { return p.key }

// deriveKey mixes a parent key with a stream name. It depends only on the
// parent's key and the name -- never on the parent's position -- so the same
// name yields the same child no matter when it is first derived, and drawing
// from a parent does not change its children.
func deriveKey(parent Key, name string) Key {
	h := fnv1a64(name)
	return Key{
		Lo: mix(parent.Lo ^ h),
		Hi: mix(parent.Hi ^ mix(h+golden)),
	}
}
