//rift:allow-nondeterminism the generation-side float surface, isolated here so the exception is one file rather than a habit: float64(u>>11) * 0x1p-53 is exact -- a 53-bit integer scaled by a power of two, no rounding and no addend for an FMA to fuse -- and every consumer materializes an integer into the plan before anything evaluates it, so no value here reaches the trace hash

// This file holds every float in internal/rng, and nothing else.
//
// The standing rule bans floating point in core scope because a fused
// multiply-add can differ between architectures, and a last-bit difference on a
// replay path is a different history. Two things here are exceptions on the
// rule's own terms:
//
//   - The conversions are exact. float64(u>>11) is a 53-bit integer, which a
//     float64 represents without rounding, and 0x1p-53 is a power of two, so
//     the multiply is exact and has no addend for an FMA to fuse with. Two
//     machines cannot disagree about the result.
//   - The values are generation-side. A probability decides a plan entry and
//     the plan records the outcome as data; plan execution takes no sequential
//     draw at all (DR-6), so nothing here is on the evaluation path or in the
//     trace hash.
//
// Keeping them in one file is what makes that argument reviewable in one place
// rather than restated at nine call sites.
package rng

// Float64 returns a uniformly distributed value in [0, 1).
func (p *PCG) Float64() float64 {
	return float64(p.Uint64()>>11) * 0x1p-53
}

// Bool reports true with probability p.
//
// It always consumes exactly one draw, including for p <= 0 and p >= 1, so
// changing a probability to zero in a config cannot shift the rest of the
// stream. That property costs nothing and removes a whole class of confusing
// diffs between two nearly-identical runs.
func (p *PCG) Bool(prob float64) bool {
	v := p.Float64()
	switch {
	case prob <= 0:
		return false
	case prob >= 1:
		return true
	default:
		return v < prob
	}
}

// Float64 returns a PRF-derived value in [0, 1) with 53 bits of precision.
func (k Key) Float64(d Domain, a, b, c uint64) float64 {
	return float64(k.PRF(d, a, b, c)>>11) * 0x1p-53
}

// Bool reports true with probability p for the given identity.
func (k Key) Bool(d Domain, a, b, c uint64, p float64) bool {
	switch {
	case p <= 0:
		return false
	case p >= 1:
		return true
	default:
		return k.Float64(d, a, b, c) < p
	}
}

func (p poisoned) Float64() float64  { p.die("Float64"); return 0 }
func (p poisoned) Bool(float64) bool { p.die("Bool"); return false }
