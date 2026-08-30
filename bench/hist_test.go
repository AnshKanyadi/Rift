package bench

import (
	"testing"
	"time"

	"github.com/anshkanyadi/rift/internal/rng"
)

// The precision claim is MEASURED against a keep-everything-and-sort reference,
// not derived from the bucket arithmetic. A histogram whose error bound is an
// argument rather than an observation is a number quoted to unknown accuracy.
func TestTheStatedPrecisionHolds(t *testing.T) {
	const bound = 0.0079 // 1/128, the sub-bucket resolution

	// Spread across nine orders of magnitude, because the bound is a RELATIVE
	// one and a test that only samples milliseconds proves it only there.
	r := rng.New(1)
	h := NewHist()
	var ref exact
	for i := 0; i < 200_000; i++ {
		// Log-uniform: an exponent, then a value inside its band.
		exp := int(r.Uint64() % 30)
		base := uint64(1) << uint(exp)
		d := time.Duration(base + r.Uint64()%base)
		h.Add(d)
		ref = append(ref, d)
	}

	for _, q := range []float64{0.5, 0.9, 0.99, 0.999} {
		got, want := h.Quantile(q), ref.quantile(q)
		if want == 0 {
			t.Fatalf("reference q%.3f is zero", q)
		}
		rel := float64(got-want) / float64(want)
		if rel < 0 {
			rel = -rel
		}
		if rel > bound {
			t.Errorf("q%.3f: histogram %s vs exact %s, relative error %.4f > %.4f",
				q, got, want, rel, bound)
		}
		// AND IT MUST ERR HIGH. A latency number that reads low is the one that
		// misleads, so the bucket's upper edge is reported and this pins it.
		if got < want {
			t.Errorf("q%.3f: histogram %s UNDER-reports exact %s", q, got, want)
		}
	}
}

// The largest samples must not be clamped: a histogram that silently caps its
// tail reports a smaller tail than the truth.
func TestTheTailIsNotClamped(t *testing.T) {
	h := NewHist()
	for i := 0; i < 998; i++ {
		h.Add(time.Millisecond)
	}
	// TWO large samples, not one. With one, rank ceil(0.999 x 1000) = 999 lands
	// on the last 1ms sample and p999 is CORRECTLY 1ms -- the first version of
	// this test asserted otherwise and was wrong about the quantile, not about
	// the histogram.
	h.Add(90 * time.Second)
	h.Add(90 * time.Second)
	if got := h.Max(); got != 90*time.Second {
		t.Fatalf("max = %s, want 90s", got)
	}
	if got := h.Quantile(0.999); got < 80*time.Second {
		t.Fatalf("p999 = %s; the single 90s sample was lost", got)
	}
}

// A quantile over too few samples is the largest sample wearing a percentile's
// name, so the count travels with every rendering.
func TestTheCountIsQuotedWithTheQuantiles(t *testing.T) {
	h := NewHist()
	h.Add(time.Millisecond)
	if got := h.String(); got[:4] != "n=1 " {
		t.Fatalf("rendering does not lead with the sample count: %q", got)
	}
}
