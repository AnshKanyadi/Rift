package bench

import (
	"fmt"
	"sort"
	"time"
)

// Hist is a log-linear latency histogram.
//
// # It is NOT an HDR histogram library, and the difference is stated rather than
// # implied
//
// CLAUDE.md asks for HDR histograms. There is no approved dependency for one and
// new dependencies need approval, so this is a project-owned histogram of the
// same FAMILY -- bucketed by exponent with a fixed number of linear sub-buckets,
// which is what gives HDR its bounded relative error -- and the bound is measured
// here rather than assumed.
//
//	A HISTOGRAM'S PRECISION IS PART OF EVERY NUMBER IT REPORTS. A p99 quoted
//	without it is quoted to unknown accuracy, and at the tail that is exactly
//	where the number is doing work.
//
// With subBuckets = 128 the worst-case relative error is 1/128 < 0.79%, and
// TestTheStatedPrecisionHolds measures it across the whole range instead of
// trusting the arithmetic.
//
// # Why not just keep every sample and sort
//
// For these run lengths that would work and would be exact. It is rejected for
// one reason: a benchmark that allocates per operation measures its own
// allocator at the tail, and the tail is the number I2 is about.
type Hist struct {
	counts []uint64
	n      uint64
	min    time.Duration
	max    time.Duration
	sum    time.Duration
}

const (
	// subBuckets is the linear resolution inside one power-of-two band.
	subBuckets = 128

	// exponents covers 1ns .. ~1.2e18ns (about 38 years), so nothing a real run
	// produces falls off the end. A histogram that silently clamps its largest
	// samples reports a tail that is smaller than the truth, which is the one
	// direction a latency number must never be wrong in.
	exponents = 62
)

// NewHist returns an empty histogram.
func NewHist() *Hist {
	return &Hist{counts: make([]uint64, exponents*subBuckets)}
}

// bucket maps a duration to an index. Sub-microsecond values land in band 0.
func bucket(d time.Duration) int {
	v := uint64(d)
	if v == 0 {
		return 0
	}
	// exp is the position of the highest set bit.
	exp := 0
	for x := v; x > 1; x >>= 1 {
		exp++
	}
	if exp >= exponents {
		exp = exponents - 1
	}
	// Linear position within the band [2^exp, 2^(exp+1)).
	base := uint64(1) << uint(exp)
	off := int(((v - base) * subBuckets) / base)
	if off >= subBuckets {
		off = subBuckets - 1
	}
	return exp*subBuckets + off
}

// value returns the representative duration for a bucket: the bucket's UPPER
// edge, so every reported quantile is an over-estimate rather than an under-.
// A latency number that errs low is the one that misleads.
func value(i int) time.Duration {
	exp := i / subBuckets
	off := i % subBuckets
	base := uint64(1) << uint(exp)
	return time.Duration(base + ((uint64(off)+1)*base)/subBuckets)
}

// Add records one sample.
func (h *Hist) Add(d time.Duration) {
	if d < 0 {
		d = 0
	}
	h.counts[bucket(d)]++
	h.n++
	h.sum += d
	if h.n == 1 || d < h.min {
		h.min = d
	}
	if d > h.max {
		h.max = d
	}
}

// Count is how many samples went in. Quoted with every quantile, because a p999
// over fewer than 1000 samples is the largest sample wearing a percentile's name.
func (h *Hist) Count() uint64 { return h.n }

// Quantile returns the q-th quantile, 0 < q < 1.
func (h *Hist) Quantile(q float64) time.Duration {
	if h.n == 0 {
		return 0
	}
	// The rank is computed with integer arithmetic on a scaled q so the result
	// does not depend on float rounding -- determinismcheck bans floats in core
	// scope for this reason, and the reason does not stop applying here.
	want := (uint64(q*1e6)*h.n + 999_999) / 1_000_000
	if want == 0 {
		want = 1
	}
	var seen uint64
	for i, c := range h.counts {
		seen += c
		if seen >= want {
			return value(i)
		}
	}
	return h.max
}

// Max is the largest sample. Reported beside p999 because the gap between them
// says whether the tail is a shoulder or a cliff.
func (h *Hist) Max() time.Duration { return h.max }

// Mean is the arithmetic mean of the raw samples, not of the buckets.
func (h *Hist) Mean() time.Duration {
	if h.n == 0 {
		return 0
	}
	return h.sum / time.Duration(h.n)
}

// String renders the quantiles with the sample count attached.
func (h *Hist) String() string {
	return fmt.Sprintf("n=%d p50=%s p99=%s p999=%s max=%s",
		h.n, round(h.Quantile(0.50)), round(h.Quantile(0.99)),
		round(h.Quantile(0.999)), round(h.Max()))
}

func round(d time.Duration) time.Duration {
	switch {
	case d > time.Second:
		return d.Round(time.Millisecond)
	case d > time.Millisecond:
		return d.Round(10 * time.Microsecond)
	default:
		return d.Round(time.Microsecond)
	}
}

// exact is a reference implementation used only by the precision test: it keeps
// every sample and sorts. The histogram is checked AGAINST it rather than
// against arithmetic, so the precision claim is measured.
type exact []time.Duration

func (e exact) quantile(q float64) time.Duration {
	if len(e) == 0 {
		return 0
	}
	s := append(exact(nil), e...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := int(q*float64(len(s))+0.999999) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}
