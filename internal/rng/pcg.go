package rng

import "math/bits"

// cheapMultiplier is the 64-bit "cheap multiplier" used both to advance the
// 128-bit LCG state and inside the DXSM output permutation.
const cheapMultiplier = 0xda942042e4dd58b5

// golden is 2^64 / phi, the usual odd-constant stirrer.
const golden = 0x9e3779b97f4a7c15

// PCG is a PCG64-DXSM generator: a 128-bit linear congruential state with a
// 64-bit "cheap" multiplier, and a double-xorshift-multiply output permutation
// applied to the pre-iterated state.
//
// The algorithm is fully specified by step and Uint64 below and pinned by the
// known-answer vectors in vectors_test.go. Those vectors are self-generated:
// they exist to detect any future change to this generator, which is their
// entire job. They are NOT evidence of bit-compatibility with O'Neill's
// reference implementation or with numpy's, and this package makes no such
// claim. Interoperability is not a goal; permanence of *our* stream is.
//
// Not safe for concurrent use. In simulation each node's logic is
// single-threaded off the event loop, and hunts parallelize across processes
// rather than goroutines, so a mutex here would cost throughput and buy
// nothing. Sharing one PCG across goroutines is a determinism bug regardless
// of whether it is also a data race.
type PCG struct {
	// 128-bit LCG state and increment. The increment is always odd.
	hi, lo       uint64
	incHi, incLo uint64

	// key is this stream's derivation material, immutable after construction.
	// It seeds children and is distinct from the mutable LCG state, so a
	// stream's position never affects what its children look like.
	key Key

	// children memoizes derived streams by name. Deriving the same name twice
	// must return the same stream, not two identical ones: two independent
	// streams with the same seed would silently hand out duplicate values to
	// callers that each believe they hold a private source.
	children map[string]*PCG
}

// New returns a root generator for a simulator seed.
func New(seed uint64) *PCG {
	return NewFromKey(Key{Hi: mix(seed ^ golden), Lo: mix(seed)})
}

// NewFromKey returns a generator seeded by a 128-bit key. Expansion runs the
// key through SplitMix64 to fill the LCG state and increment, then takes one
// step so that no output is a trivially invertible function of the seed.
func NewFromKey(k Key) *PCG {
	s0 := mix(k.Lo)
	s1 := mix(k.Hi ^ s0)
	s2 := mix(s0 ^ s1)
	s3 := mix(s1 ^ s2)

	p := &PCG{
		hi: s0, lo: s1,
		incHi: s2, incLo: s3 | 1, // the LCG increment must be odd
		key: k,
	}
	p.step()
	return p
}

// step advances the LCG: state = state*cheapMultiplier + inc, over 128 bits.
func (p *PCG) step() {
	carry, lo := bits.Mul64(p.lo, cheapMultiplier)
	hi := p.hi*cheapMultiplier + carry

	lo, c := bits.Add64(lo, p.incLo, 0)
	hi, _ = bits.Add64(hi, p.incHi, c)

	p.hi, p.lo = hi, lo
}

// Uint64 returns the next value in the stream.
func (p *PCG) Uint64() uint64 {
	// DXSM output permutation over the pre-iterated state.
	hi, lo := p.hi, p.lo|1
	hi ^= hi >> 32
	hi *= cheapMultiplier
	hi ^= hi >> 48
	hi *= lo

	p.step()
	return hi
}

// mix is the SplitMix64 finalizer: a strong 64-bit avalanche function used for
// key expansion and derivation. It is not a generator on its own here.
func mix(z uint64) uint64 {
	z ^= z >> 30
	z *= 0xbf58476d1ce4e5b9
	z ^= z >> 27
	z *= 0x94d049bb133111eb
	z ^= z >> 31
	return z
}

// fnv1a64 hashes a stream name. Its weak avalanche does not matter because
// every derived value passes through mix afterwards.
func fnv1a64(s string) uint64 {
	const (
		offset = 0xcbf29ce484222325
		prime  = 0x100000001b3
	)
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}
