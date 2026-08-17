package hunt

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/toy"
)

// Sweep runs a seed range against a scenario and returns one result per seed, in
// seed order.
//
// # Why worker count cannot affect the answer
//
// This is the property the whole command rests on, and it is structural rather
// than tested-into-existence:
//
//   - Each seed is a complete run. Its plan comes from its own seed, its dice
//     come from plan-carried PRF keys, and nothing it touches outlives it.
//   - Results are written to a preallocated slice at the seed's own index. There
//     is no append, no shared accumulator and no ordering dependency, so the
//     output is identical whether one worker or thirty-two produced it.
//   - Nothing is read back across workers. A worker never sees another's result,
//     so it cannot branch on how the work was divided.
//
// A hunt whose findings depended on `--workers` would be a hunt that could not
// hand anyone a reproduction, which is the one thing it exists to do.
// TestWorkerCountDoesNotAffectResults asserts it at one worker and at several.
//
// Concurrency lives here because this package is orchestration by Amendment A5
// and is excluded from the determinism pass by name. Nothing inside a simulated
// run is concurrent; the goroutines are around runs, never inside one.
func Sweep(from, to uint64, sc toy.Scenario, workers int) ([]Result, error) {
	if to < from {
		return nil, fmt.Errorf("hunt: seed range [%d,%d) runs backwards", from, to)
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	n := int(to - from)
	if n == 0 {
		return nil, nil
	}
	if workers > n {
		workers = n
	}

	out := make([]Result, n)
	var next atomicCounter
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.take()
				if i >= n {
					return
				}
				out[i] = runOne(from+uint64(i), sc)
			}
		}()
	}
	wg.Wait()
	return out, nil
}

// runOne materializes, prepares and runs a single seed, classifying the two
// outcomes that are not failures: a refused seed and a clean one.
func runOne(seed uint64, sc toy.Scenario) Result {
	r := Result{Seed: seed}
	p, err := toy.MaterializeToy(seed, sc)
	if err == nil {
		var res Result
		res, err = RunToy(p, sc, nil)
		if err == nil {
			res.Seed = seed
			return res
		}
	}
	// A refused seed is not a failure and not a pass: on this seed's network the
	// flaw cannot exist, so there was nothing here to find. It belongs in
	// neither numerator nor denominator.
	if errors.Is(err, toy.ErrWindowTooNarrow) {
		r.Refused = true
		return r
	}
	r.Err = err
	return r
}

// atomicCounter hands out indices. A counter rather than a channel of work so
// that the division of labour is invisible to the results: whichever worker
// takes index i produces the same Result for it.
type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) take() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.n
	c.n++
	return i
}

// Census summarises a sweep in the shape a report and a soak ledger both want.
type Census struct {
	Seeds        int
	Eligible     int
	Refused      int
	Violations   int
	Inconclusive int
	Errors       int

	// FirstViolation is the lowest seed that produced one, and it is the lowest
	// rather than the first observed: with workers in play "first observed" is a
	// race, and a hunt must name the same seed every time it is run.
	FirstViolation   uint64
	FoundAViolation  bool
	FirstViolationAt int // index into the results slice
}

// Summarize folds a sweep, in seed order.
func Summarize(rs []Result) Census {
	var c Census
	for i, r := range rs {
		c.Seeds++
		switch {
		case r.Err != nil:
			c.Errors++
			continue
		case r.Refused:
			c.Refused++
			continue
		}
		c.Eligible++
		for _, rep := range r.Reports {
			switch rep.Verdict {
			case sim.VerdictViolation:
				c.Violations++
				if !c.FoundAViolation {
					c.FoundAViolation = true
					c.FirstViolation = r.Seed
					c.FirstViolationAt = i
				}
			case sim.VerdictInconclusive:
				c.Inconclusive++
			case sim.VerdictPass, sim.VerdictUnset:
			}
		}
	}
	return c
}
