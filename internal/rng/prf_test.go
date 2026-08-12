package rng

import (
	"testing"
)

func testKey() Key { return New(0x21f7).DeriveKey("test") }

// TestPRFIsStateless is the whole reason this function exists: plan execution
// evaluates dice in whatever order events happen to fire, and a plan replayed
// after a code change may evaluate a different set entirely. The answer for a
// given identity must not depend on any of that.
func TestPRFIsStateless(t *testing.T) {
	k := testKey()
	ids := [][3]uint64{{1, 2, 3}, {9, 9, 9}, {0, 0, 0}, {7, 0, 1}}

	forward := make([]uint64, len(ids))
	for i, id := range ids {
		forward[i] = k.PRF(DomainNetDrop, id[0], id[1], id[2])
	}
	// Same identities, evaluated backwards, with unrelated evaluations
	// interleaved to simulate a code change that added other work.
	for i := len(ids) - 1; i >= 0; i-- {
		k.PRF(DomainWorkload, 12345, 0, 0)
		if got := k.PRF(DomainNetDrop, ids[i][0], ids[i][1], ids[i][2]); got != forward[i] {
			t.Fatalf("identity %v: got %#x on the second pass, want %#x", ids[i], got, forward[i])
		}
	}
}

// TestPRFDomainSeparation: two subsystems reading the same identity under the
// same key must not see correlated values, or a link's drop decision would
// predict its latency.
func TestPRFDomainSeparation(t *testing.T) {
	k := testKey()
	domains := []Domain{
		DomainNetDrop, DomainNetLatency, DomainNetDuplicate,
		DomainEngineSync, DomainClockJitter, DomainRaftElection, DomainWorkload,
	}
	seen := make(map[uint64]Domain, len(domains))
	for _, d := range domains {
		v := k.PRF(d, 1, 2, 3)
		if prev, dup := seen[v]; dup {
			t.Fatalf("domains %d and %d collide on identity (1,2,3)", prev, d)
		}
		seen[v] = d
	}
}

func TestPRFKeySeparation(t *testing.T) {
	a := New(1).DeriveKey("net")
	b := New(2).DeriveKey("net")
	if a.PRF(DomainNetDrop, 1, 2, 3) == b.PRF(DomainNetDrop, 1, 2, 3) {
		t.Fatal("different keys produced the same value for the same identity")
	}
}

// TestPRFNoCollisions samples a realistic identity space -- every directed link
// in a 12-node cluster across many message ordinals -- and requires every
// output to be distinct. Collisions would correlate unrelated events.
func TestPRFNoCollisions(t *testing.T) {
	k := testKey()
	seen := make(map[uint64]struct{}, 1<<18)
	for from := uint64(1); from <= 12; from++ {
		for to := uint64(1); to <= 12; to++ {
			if from == to {
				continue
			}
			for ord := uint64(0); ord < 1500; ord++ {
				v := k.PRF(DomainNetLatency, from, to, ord)
				if _, dup := seen[v]; dup {
					t.Fatalf("collision at (%d,%d,%d)", from, to, ord)
				}
				seen[v] = struct{}{}
			}
		}
	}
	t.Logf("%d distinct identities, no collisions", len(seen))
}

func TestPRFUint64NInRange(t *testing.T) {
	k := testKey()
	for _, n := range []uint64{1, 2, 3, 10, 1 << 20, 1<<63 + 1, ^uint64(0)} {
		for i := uint64(0); i < 2000; i++ {
			if v := k.Uint64N(DomainEngineSync, i, 0, 0, n); v >= n {
				t.Fatalf("Uint64N(n=%d) returned %d, out of range", n, v)
			}
		}
	}
}

func TestPRFUint64NPanicsOnZero(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Key.Uint64N(n=0): want panic, got none")
		}
	}()
	testKey().Uint64N(DomainWorkload, 0, 0, 0, 0)
}

func TestPRFBetween(t *testing.T) {
	k := testKey()
	const lo, hi = 200_000, 3_000_000 // a plausible latency window in nanoseconds
	sawLow, sawHigh := false, false
	for i := uint64(0); i < 20000; i++ {
		v := k.Between(DomainNetLatency, 1, 2, i, lo, hi)
		if v < lo || v >= hi {
			t.Fatalf("Between returned %d, want [%d,%d)", v, lo, hi)
		}
		if v < lo+(hi-lo)/10 {
			sawLow = true
		}
		if v > hi-(hi-lo)/10 {
			sawHigh = true
		}
	}
	if !sawLow || !sawHigh {
		t.Errorf("Between did not span its range (sawLow=%v sawHigh=%v)", sawLow, sawHigh)
	}
}

func TestPRFBetweenPanicsOnEmptyRange(t *testing.T) {
	for _, r := range [][2]uint64{{5, 5}, {9, 4}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Between(lo=%d,hi=%d): want panic, got none", r[0], r[1])
				}
			}()
			testKey().Between(DomainWorkload, 0, 0, 0, r[0], r[1])
		}()
	}
}

func BenchmarkPRF(b *testing.B) {
	k := testKey()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = k.PRF(DomainNetLatency, 1, 2, uint64(i))
	}
}
