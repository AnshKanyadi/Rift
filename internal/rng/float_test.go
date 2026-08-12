//rift:allow-nondeterminism the tests of the generation-side float surface; they exercise exactly the functions float.go isolates, and no value here reaches an evaluation path or the trace hash

// This file holds every test in internal/rng that touches a float, for the
// reason float.go holds every float: one exemption with one argument, rather
// than nine of them scattered through the suite.
//
// These are distribution and rate tests. They assert that Float64 is uniform
// and that Bool honours its probability, which cannot be expressed without
// naming the probability.
package rng

import (
	"math/bits"
	"testing"
)

// TestBoolConsumesExactlyOneDraw pins a property that exists to keep diffs
// readable: setting a fault probability to 0 or 1 in a config must not shift
// the rest of the stream, so two runs differing only in that probability stay
// comparable.
func TestBoolConsumesExactlyOneDraw(t *testing.T) {
	for _, p := range []float64{-1, 0, 0.5, 1, 2} {
		a, b := New(77), New(77)
		a.Bool(p)
		b.Uint64()
		if got, want := a.Uint64(), b.Uint64(); got != want {
			t.Errorf("Bool(%v) consumed the wrong number of draws: next was %#x, want %#x", p, got, want)
		}
	}
}

func TestUint64NUniform(t *testing.T) {
	// Deterministic, so a tight bound cannot flake. 3% leaves room for honest
	// sampling noise (about 5 sigma here) while still failing loudly on a real
	// modulo-bias regression.
	const draws, buckets = 120000, 6
	var counts [buckets]int
	r := New(20250810)
	for i := 0; i < draws; i++ {
		counts[r.Uint64N(buckets)]++
	}
	expected := float64(draws) / buckets
	for i, c := range counts {
		dev := (float64(c) - expected) / expected
		if dev < -0.03 || dev > 0.03 {
			t.Errorf("bucket %d: %d draws, expected ~%.0f (%.2f%% off)", i, c, expected, dev*100)
		}
	}
}

// TestPRFAvalanche checks that a single-bit change to the identity changes
// about half the output bits. A weak mixer here would make neighbouring
// message ordinals produce correlated dice, so consecutive messages on a link
// would drop together -- which is exactly the correlated failure the injector
// is supposed to control explicitly rather than produce by accident.
func TestPRFAvalanche(t *testing.T) {
	k := testKey()
	const base = 0x0123456789abcdef
	ref := k.PRF(DomainNetLatency, base, 7, 11)

	total := 0
	for bit := 0; bit < 64; bit++ {
		flipped := k.PRF(DomainNetLatency, base^(1<<uint(bit)), 7, 11)
		d := bits.OnesCount64(ref ^ flipped)
		if d < 18 || d > 46 {
			t.Errorf("flipping bit %d changed %d output bits, want roughly 32", bit, d)
		}
		total += d
	}
	if avg := float64(total) / 64; avg < 30 || avg > 34 {
		t.Errorf("average avalanche %.2f bits, want close to 32", avg)
	}
}

func TestPRFBoolRate(t *testing.T) {
	k := testKey()
	const draws = 200000
	hits := 0
	for i := uint64(0); i < draws; i++ {
		if k.Bool(DomainNetDrop, 1, 2, i, 0.01) {
			hits++
		}
	}
	// Expect ~2000; deterministic, so a 10% band cannot flake.
	if hits < 1800 || hits > 2200 {
		t.Errorf("Bool(p=0.01) fired %d times in %d, want ~2000", hits, draws)
	}
}

// The PRF trades exact uniformity for statelessness and documents the bias as
// bounded by n/2^64. This pins that the practical distribution is still flat
// enough for fault dice; if it were not, the documented tradeoff would be
// wrong rather than merely tight.
func TestPRFUint64NUniformEnough(t *testing.T) {
	k := testKey()
	const draws, buckets = 120000, 6
	var counts [buckets]int
	for i := uint64(0); i < draws; i++ {
		counts[k.Uint64N(DomainWorkload, i, 0, 0, buckets)]++
	}
	expected := float64(draws) / buckets
	for i, c := range counts {
		if dev := (float64(c) - expected) / expected; dev < -0.03 || dev > 0.03 {
			t.Errorf("bucket %d: %d draws, expected ~%.0f (%.2f%% off)", i, c, expected, dev*100)
		}
	}
}
