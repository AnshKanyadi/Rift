package bench

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Op is one issued operation's outcome, as the load driver saw it.
type Op struct {
	Latency time.Duration
	OK      bool
}

// Doer is whatever puts an operation on the wire. chaos.WireClient satisfies it.
//
// The load driver takes an interface rather than the concrete client so it can
// be exercised against a fake with known latencies -- which is how the recovery
// detector below is induced, since a real cluster cannot be asked to recover on
// cue.
type Doer interface {
	Do(op, key, value string, wait time.Duration) bool
}

// Mix is a YCSB-style workload shape.
type Mix struct {
	Name      string
	ReadPct   int // 0..100
	Keys      int
	Workers   int
	Warmup    time.Duration
	Window    time.Duration
	OpTimeout time.Duration
}

// Result is one measured window.
type Result struct {
	Mix       Mix
	Hist      *Hist
	OK, Fail  uint64
	Elapsed   time.Duration
	Buckets   []Bucket // per-slice throughput, for the recovery measurement
	SliceSize time.Duration
}

// Bucket is throughput over one slice of the window. Recovery is measured from
// these rather than from a latency sample, because recovery is a statement about
// THE CLUSTER SERVING AGAIN, not about one operation finishing.
type Bucket struct {
	Start time.Duration
	OK    uint64
}

// Throughput is completed operations per second over the measured window.
//
// It counts ONLY completed operations. A run that answers nothing has zero
// throughput, not "throughput at the timeout rate" -- an operation that timed out
// did not do work, and counting it would let a dead cluster post a number.
func (r Result) Throughput() float64 {
	if r.Elapsed <= 0 {
		return 0
	}
	return float64(r.OK) / r.Elapsed.Seconds()
}

func (r Result) String() string {
	return fmt.Sprintf("%-8s %8.0f ops/s  ok=%d fail=%d  %s",
		r.Mix.Name, r.Throughput(), r.OK, r.Fail, r.Hist)
}

// Run drives a closed-loop workload and returns the measured window.
//
// # Closed loop, and the consequence is stated
//
// Each worker issues one operation and waits for it. That means offered load
// FALLS when the cluster slows, so this measures a system under a fixed
// concurrency rather than under a fixed arrival rate.
//
//	A CLOSED LOOP CANNOT PRODUCE A QUEUE, so it cannot show the latency collapse
//	an open loop finds. What it can show honestly is throughput and latency at a
//	stated concurrency, and that is what BENCHMARKS.md will say it is.
//
// Warmup is DISCARDED rather than merely waited out: its operations are issued,
// so the cluster is warm, and their latencies never enter the histogram.
func Run(d Doer, m Mix) Result {
	h := NewHist()
	var ok, fail uint64
	const sliceSize = 250 * time.Millisecond

	// buckets are written by workers and read at the end; one atomic counter per
	// slice, indexed by elapsed time, so there is no lock on the hot path.
	nSlices := int(m.Window/sliceSize) + 2
	slices := make([]atomic.Uint64, nSlices)

	var mu sync.Mutex // guards h only; the histogram is not concurrency-safe
	var wg sync.WaitGroup

	start := time.Now()
	warmEnd := start.Add(m.Warmup)
	end := warmEnd.Add(m.Window)

	for w := 0; w < m.Workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			i := 0
			for {
				now := time.Now()
				if !now.Before(end) {
					return
				}
				// Each worker walks its own stride through the key space, so
				// workers do not all hammer one key and the per-key histories
				// stay small enough for a per-key checker to finish.
				key := fmt.Sprintf("k%05d", (w*7919+i)%m.Keys)
				isRead := (i*100/max1(m.Workers))%100 < m.ReadPct
				op, val := "put", fmt.Sprintf("v%d-%d", w, i)
				if isRead {
					op, val = "get", ""
				}
				i++

				t0 := time.Now()
				good := d.Do(op, key, val, m.OpTimeout)
				lat := time.Since(t0)

				if t0.Before(warmEnd) {
					continue // WARM, NOT MEASURED
				}
				if good {
					atomic.AddUint64(&ok, 1)
					mu.Lock()
					h.Add(lat)
					mu.Unlock()
					if s := int(t0.Sub(warmEnd) / sliceSize); s >= 0 && s < nSlices {
						slices[s].Add(1)
					}
				} else {
					atomic.AddUint64(&fail, 1)
					// A FAILED OPERATION'S LATENCY IS STILL A LATENCY THE CLIENT
					// WAITED. Excluding it would report the tail of the
					// operations that succeeded, which is a different and much
					// prettier number.
					mu.Lock()
					h.Add(lat)
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	// ONLY THE SLICES THE WINDOW COVERS. `slices` is over-allocated by two so a
	// straggler cannot index past the end; returning those two would append two
	// permanently-empty slices to every run.
	//
	// The first version returned all of them, and a FLAT run reported drift=0.33
	// -- the padding sat entirely in the last third. It would have declared every
	// healthy baseline degraded, including the ones this measure exists to
	// distinguish from the sick ones.
	inWindow := int(m.Window / sliceSize)
	if inWindow > nSlices {
		inWindow = nSlices
	}
	buckets := make([]Bucket, 0, inWindow)
	for i := 0; i < inWindow; i++ {
		buckets = append(buckets, Bucket{Start: time.Duration(i) * sliceSize, OK: slices[i].Load()})
	}
	return Result{
		Mix: m, Hist: h, OK: ok, Fail: fail,
		Elapsed: time.Since(warmEnd), Buckets: buckets, SliceSize: sliceSize,
	}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// RecoveryAfter measures how long the cluster took to serve again after an event
// at `at`, given a steady-state rate.
//
// # The definition, stated because "recovery time" has several
//
//	R = the time from the event to the START of the first slice whose throughput
//	    reaches half the steady-state rate.
//
// Half rather than full: a cluster returning to exactly its prior rate is a
// stricter question than "is it serving again", and the threshold I2 declared is
// about availability returning. Half is stated here so the number is
// interpretable rather than defensible.
//
// Returns ok=false when throughput never recovers within the window, which is
// NOT the same as a large R and must not be reported as one.
func RecoveryAfter(r Result, at time.Duration, steady float64) (time.Duration, bool) {
	if steady <= 0 {
		return 0, false
	}
	target := uint64(steady * r.SliceSize.Seconds() / 2)
	if target == 0 {
		target = 1
	}
	for _, b := range r.Buckets {
		if b.Start < at {
			continue
		}
		if b.OK >= target {
			return b.Start - at, true
		}
	}
	return 0, false
}

// Drift is last-third throughput divided by first-third throughput over the
// measured window.
//
// # A baseline is a claim that one number represents the window
//
// `Throughput()` averages. An average over a window whose ends differ by a
// factor of two represents neither end, and quoting it as "steady state" asserts
// a stability the run did not have.
//
//	A RUN THAT GETS SLOWER THROUGHOUT DOES NOT HAVE A STEADY STATE TO MEASURE,
//	and the mean is the one number guaranteed to be wrong about both halves.
//
// Reported with every baseline so a reader can see the claim's foundation rather
// than take it.
func (r Result) Drift() float64 {
	if len(r.Buckets) < 3 {
		return 1
	}
	third := len(r.Buckets) / 3
	var first, last uint64
	for i := 0; i < third; i++ {
		first += r.Buckets[i].OK
	}
	for i := len(r.Buckets) - third; i < len(r.Buckets); i++ {
		last += r.Buckets[i].OK
	}
	if first == 0 {
		return 0
	}
	return float64(last) / float64(first)
}

// SteadyEnough reports whether Drift is inside the band in which a single mean
// represents the window.
//
// The bound is 2x, DERIVED rather than picked: at a factor of two the mean sits
// at 1.5x one end and 0.75x the other, so it is outside a +/-25% band around both
// and there is no reading under which it describes the run. Anything tighter
// would be a judgement about acceptable noise, which is a different question.
func (r Result) SteadyEnough() bool {
	d := r.Drift()
	return d >= 0.5 && d <= 2.0
}
