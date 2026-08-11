package rng

import (
	"math/bits"
	"testing"
)

func TestUint64NInRange(t *testing.T) {
	r := New(99)
	for _, n := range []uint64{1, 2, 3, 6, 7, 255, 256, 1 << 32, 1<<63 + 1, ^uint64(0)} {
		for i := 0; i < 1000; i++ {
			if v := r.Uint64N(n); v >= n {
				t.Fatalf("Uint64N(%d) returned %d, out of range", n, v)
			}
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

// TestUint64NRejectionIsLive proves the rejection loop is reached rather than
// being dead code that happens to look correct. Without rejection the
// distribution is subtly biased for n that does not divide 2^64, and a purely
// statistical test would need enormous samples to notice.
//
// White-box: replay the same stream by hand, applying Lemire's method, and
// count how many raw draws the k results actually consumed.
func TestUint64NRejectionIsLive(t *testing.T) {
	// ~2/3 of 2^64, so Lemire rejects roughly a third of the time.
	var n uint64 = 0xaaaaaaaaaaaaaaab
	const k = 200

	got := make([]uint64, k)
	r := New(31337)
	for i := range got {
		got[i] = r.Uint64N(n)
	}

	raw := New(31337)
	consumed := 0
	next := func() uint64 { consumed++; return raw.Uint64() }
	threshold := (-n) % n
	if threshold == 0 {
		t.Fatalf("test constant is wrong: n=%#x cannot exercise rejection", n)
	}
	for i := 0; i < k; i++ {
		hi, lo := bits.Mul64(next(), n)
		if lo < n {
			for lo < threshold {
				hi, lo = bits.Mul64(next(), n)
			}
		}
		if hi != got[i] {
			t.Fatalf("value %d: hand-replay got %d, Uint64N got %d", i, hi, got[i])
		}
	}
	if consumed <= k {
		t.Fatalf("consumed %d draws for %d values: rejection loop never ran", consumed, k)
	}
	t.Logf("rejection exercised: %d raw draws for %d values", consumed, k)
}

func TestUint64NPanicsOnZero(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Uint64N(0): want panic, got none")
		}
	}()
	New(1).Uint64N(0)
}

func TestIntNPanicsOnNonPositive(t *testing.T) {
	for _, n := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("IntN(%d): want panic, got none", n)
				}
			}()
			New(1).IntN(n)
		}()
	}
}

func TestFloat64InRange(t *testing.T) {
	r := New(5)
	for i := 0; i < 100000; i++ {
		v := r.Float64()
		if v < 0 || v >= 1 {
			t.Fatalf("Float64 returned %v, want [0,1)", v)
		}
	}
}

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

func TestBoolExtremes(t *testing.T) {
	r := New(3)
	for i := 0; i < 1000; i++ {
		if r.Bool(0) {
			t.Fatal("Bool(0) returned true")
		}
		if !r.Bool(1) {
			t.Fatal("Bool(1) returned false")
		}
	}
}

func TestShuffleIsPermutation(t *testing.T) {
	r := New(11)
	for n := 0; n <= 64; n++ {
		xs := make([]int, n)
		for i := range xs {
			xs[i] = i
		}
		r.Shuffle(n, func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })

		seen := make([]bool, n)
		for _, v := range xs {
			if v < 0 || v >= n || seen[v] {
				t.Fatalf("Shuffle(%d) produced %v, not a permutation", n, xs)
			}
			seen[v] = true
		}
	}
}

func TestPermEdgeCases(t *testing.T) {
	r := New(13)
	if got := r.Perm(0); len(got) != 0 {
		t.Errorf("Perm(0) = %v, want empty", got)
	}
	if got := r.Perm(1); len(got) != 1 || got[0] != 0 {
		t.Errorf("Perm(1) = %v, want [0]", got)
	}
}

// TestDeriveIsMemoized guards against the worst plausible mistake in this
// package: handing two callers separate streams under the same name, each
// believing it holds a private source, and silently serving them identical
// values.
func TestDeriveIsMemoized(t *testing.T) {
	root := New(8)
	a := root.Derive("net")
	b := root.Derive("net")
	if a != b {
		t.Fatal("Derive returned distinct streams for the same name")
	}
	first := a.Uint64()
	if second := b.Uint64(); second == first {
		t.Fatalf("two handles to the same stream returned the same value %#x twice", first)
	}
}

func TestDeriveIndependence(t *testing.T) {
	root := New(8)
	names := []string{"net.latency", "net.drop", "fault.crash", "clock", "workload"}
	seen := make(map[uint64]string, len(names)*32)
	for _, name := range names {
		s := root.Derive(name)
		for i := 0; i < 32; i++ {
			v := s.Uint64()
			if prev, dup := seen[v]; dup {
				t.Fatalf("streams %q and %q both produced %#x", prev, name, v)
			}
			seen[v] = name
		}
	}
}

// TestDeriveOrderIndependence is the property that makes named streams worth
// having: a change to how one subsystem consumes randomness must not perturb
// another's schedule, so deriving in a different order must be invisible.
func TestDeriveOrderIndependence(t *testing.T) {
	forward := New(4242)
	a1 := drawN(forward.Derive("a"), 8)
	b1 := drawN(forward.Derive("b"), 8)

	reverse := New(4242)
	b2 := drawN(reverse.Derive("b"), 8)
	a2 := drawN(reverse.Derive("a"), 8)

	if !equalU64(a1, a2) {
		t.Errorf("stream \"a\" depends on derivation order: %v vs %v", a1, a2)
	}
	if !equalU64(b1, b2) {
		t.Errorf("stream \"b\" depends on derivation order: %v vs %v", b1, b2)
	}
}

// TestDerivationIgnoresParentPosition: children derive from the parent's key,
// not its state, so drawing from a parent before deriving must not change what
// the child looks like. Without this, adding one draw anywhere upstream would
// reshuffle every sub-stream.
func TestDerivationIgnoresParentPosition(t *testing.T) {
	early := New(555)
	fromEarly := drawN(early.Derive("net"), 8)

	late := New(555)
	for i := 0; i < 100; i++ {
		late.Uint64()
	}
	fromLate := drawN(late.Derive("net"), 8)

	if !equalU64(fromEarly, fromLate) {
		t.Errorf("child stream depends on parent position: %v vs %v", fromEarly, fromLate)
	}
}

func TestNestedDerive(t *testing.T) {
	root := New(2)
	deep := root.Derive("node.3").Derive("election")
	other := root.Derive("node.4").Derive("election")
	if equalU64(drawN(deep, 8), drawN(other, 8)) {
		t.Fatal("streams under different parents collided")
	}
}

// TestNoHiddenGlobalState: two generators built from the same seed must be
// identical no matter what else exists or has been drawn. This is the property
// CLAUDE.md's ban on package-level randomness is protecting.
func TestNoHiddenGlobalState(t *testing.T) {
	noise := New(1)
	for i := 0; i < 50; i++ {
		noise.Uint64()
	}

	a, b := New(42), New(42)
	for i := 0; i < 100; i++ {
		noise.Uint64() // interleaved traffic on an unrelated generator
		x, y := a.Uint64(), b.Uint64()
		if x != y {
			t.Fatalf("draw %d diverged: %#x vs %#x", i, x, y)
		}
	}
}

func drawN(r Rand, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = r.Uint64()
	}
	return out
}

func equalU64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func BenchmarkUint64(b *testing.B) {
	r := New(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Uint64()
	}
}

func BenchmarkUint64N(b *testing.B) {
	r := New(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Uint64N(1000)
	}
}
