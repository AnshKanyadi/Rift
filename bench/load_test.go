package bench

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDoer is a cluster whose availability the test controls. A real cluster
// cannot be asked to go down and come back on cue, so the recovery detector is
// induced against this instead of against a run that happened to look right.
type fakeDoer struct {
	lat  time.Duration
	down atomic.Bool
}

func (f *fakeDoer) Do(op, key, value string, wait time.Duration) bool {
	if f.down.Load() {
		time.Sleep(wait)
		return false
	}
	time.Sleep(f.lat)
	return true
}

func TestWarmupOperationsAreISSUEDAndNotMEASURED(t *testing.T) {
	var issued atomic.Uint64
	d := doerFunc(func(string, string, string, time.Duration) bool {
		issued.Add(1)
		time.Sleep(time.Millisecond)
		return true
	})
	r := Run(d, Mix{
		Name: "warm", ReadPct: 50, Keys: 8, Workers: 2,
		Warmup: 200 * time.Millisecond, Window: 200 * time.Millisecond,
		OpTimeout: time.Second,
	})
	// The cluster was worked during warmup...
	if issued.Load() <= r.OK {
		t.Fatalf("issued=%d, measured=%d: warmup operations were not issued",
			issued.Load(), r.OK)
	}
	// ...and none of their latencies entered the histogram.
	if r.Hist.Count() >= issued.Load() {
		t.Fatalf("histogram holds %d of %d issued: warmup was measured",
			r.Hist.Count(), issued.Load())
	}
}

// A failed operation's latency is still a latency the client waited. Excluding
// it reports the tail of the operations that succeeded.
func TestAFailedOperationsLatencyStillEntersTheHistogram(t *testing.T) {
	d := &fakeDoer{lat: time.Millisecond}
	d.down.Store(true)
	r := Run(d, Mix{
		Name: "down", ReadPct: 0, Keys: 4, Workers: 2,
		Window: 300 * time.Millisecond, OpTimeout: 50 * time.Millisecond,
	})
	if r.OK != 0 {
		t.Fatalf("a down cluster completed %d operations", r.OK)
	}
	if r.Fail == 0 {
		t.Fatal("no failures recorded")
	}
	if r.Hist.Count() != r.Fail {
		t.Fatalf("histogram holds %d samples for %d failures", r.Hist.Count(), r.Fail)
	}
	if got := r.Hist.Quantile(0.5); got < 40*time.Millisecond {
		t.Fatalf("p50 = %s; the timeouts the client actually waited are missing", got)
	}
}

// A run that answers nothing has zero throughput, not throughput at the timeout
// rate.
func TestADeadClusterPostsZeroThroughput(t *testing.T) {
	d := &fakeDoer{lat: time.Millisecond}
	d.down.Store(true)
	r := Run(d, Mix{
		Name: "dead", Keys: 4, Workers: 2,
		Window: 200 * time.Millisecond, OpTimeout: 20 * time.Millisecond,
	})
	if r.Throughput() != 0 {
		t.Fatalf("throughput = %f over a cluster that completed nothing", r.Throughput())
	}
}

// The recovery detector, induced: the cluster goes down mid-window and comes
// back, and R must land near the outage's end rather than at its start.
func TestRecoveryIsMeasuredFromWhenServingRESUMES(t *testing.T) {
	d := &fakeDoer{lat: time.Millisecond}
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(700 * time.Millisecond)
		d.down.Store(true)
		time.Sleep(700 * time.Millisecond)
		d.down.Store(false)
	}()
	r := Run(d, Mix{
		Name: "recover", Keys: 4, Workers: 4,
		Window: 2500 * time.Millisecond, OpTimeout: 100 * time.Millisecond,
	})
	<-done

	// Steady state from the first half-second, before the outage.
	var early uint64
	for _, b := range r.Buckets {
		if b.Start < 500*time.Millisecond {
			early += b.OK
		}
	}
	steady := float64(early) / 0.5

	got, ok := RecoveryAfter(r, 700*time.Millisecond, steady)
	if !ok {
		t.Fatal("recovery never detected, though the cluster came back")
	}
	if got < 400*time.Millisecond || got > 1200*time.Millisecond {
		t.Fatalf("R = %s; the outage ran from 700ms to 1400ms, so R should be ~700ms", got)
	}
}

// Never recovering is NOT a large R, and must not be reported as one.
func TestNeverRecoveringIsNotALargeRecoveryTime(t *testing.T) {
	d := &fakeDoer{lat: time.Millisecond}
	go func() {
		time.Sleep(300 * time.Millisecond)
		d.down.Store(true)
	}()
	r := Run(d, Mix{
		Name: "gone", Keys: 4, Workers: 2,
		Window: 1200 * time.Millisecond, OpTimeout: 50 * time.Millisecond,
	})
	var early uint64
	for _, b := range r.Buckets {
		if b.Start < 250*time.Millisecond {
			early += b.OK
		}
	}
	if _, ok := RecoveryAfter(r, 300*time.Millisecond, float64(early)/0.25); ok {
		t.Fatal("a cluster that never came back reported a recovery time")
	}
}

type doerFunc func(string, string, string, time.Duration) bool

func (f doerFunc) Do(op, key, value string, wait time.Duration) bool {
	return f(op, key, value, wait)
}

// A run that gets slower throughout has no steady state, and the mean is the one
// number guaranteed to be wrong about both halves.
func TestARunThatDegradesIsNotASteadyState(t *testing.T) {
	d := &slowingDoer{start: time.Millisecond, growth: 6 * time.Millisecond}
	r := Run(d, Mix{
		Name: "slowing", Keys: 8, Workers: 4,
		Window: 3 * time.Second, OpTimeout: time.Second,
	})
	if r.SteadyEnough() {
		t.Fatalf("a run whose throughput fell throughout reported a steady state: drift=%.2f, "+
			"mean=%.0f ops/s", r.Drift(), r.Throughput())
	}
	if r.Drift() >= 1 {
		t.Fatalf("drift = %.2f over a run that only got slower", r.Drift())
	}
}

// And a flat run is not falsely rejected.
func TestAFlatRunIsSteadyEnough(t *testing.T) {
	d := &fakeDoer{lat: 2 * time.Millisecond}
	r := Run(d, Mix{
		Name: "flat", Keys: 8, Workers: 4,
		Window: 2 * time.Second, OpTimeout: time.Second,
	})
	if !r.SteadyEnough() {
		t.Fatalf("a flat run was rejected: drift=%.2f", r.Drift())
	}
}

// slowingDoer degrades linearly, the way the real cluster did.
type slowingDoer struct {
	start  time.Duration
	growth time.Duration
	t0     time.Time
	once   sync.Once
}

func (s *slowingDoer) Do(op, key, value string, wait time.Duration) bool {
	s.once.Do(func() { s.t0 = time.Now() })
	elapsed := time.Since(s.t0).Seconds()
	time.Sleep(s.start + time.Duration(elapsed*float64(s.growth)))
	return true
}
