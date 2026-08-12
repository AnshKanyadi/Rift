package clock

import (
	"errors"
	"fmt"
	"math/bits"
	"time"
)

// ppb is the denominator for segment slopes: one part per billion. A crystal
// drifting 100 ppm is 100_000 ppb, so realistic drift lives in the millions and
// there is room above it for deliberately absurd schedules.
const ppb = 1_000_000_000

// Segment is one piece of a node's piecewise-linear oscillator offset. Within
// [Start, next.Start) the offset is Off + SlopePPB*(t-Start)/ppb.
//
// A flat segment (SlopePPB == 0) is a sustained hold, which is the case A8
// needs and the case a drift-rate-only model cannot express (DESIGN-A0 DR-15).
type Segment struct {
	Start    Instant
	Off      int64 // offset in nanoseconds at Start
	SlopePPB int64 // d(offset)/d(t), parts per billion; > -ppb
}

// Step is an instantaneous correction to the wall reading only: an NTP step.
// It does not touch the oscillator, so it does not perturb timers.
type Step struct {
	At    Instant
	Delta int64 // nanoseconds; may be negative
}

// Timeline is one node's clock, as a pure function of global virtual time.
// Nothing here reads a clock, allocates on the evaluation path, or consumes
// randomness: given a Timeline and a t, every reading is determined.
type Timeline struct {
	// Skew describes the oscillator: osc(t) = t + skew(t). Sorted by Start,
	// contiguous, and continuous -- a discontinuity in the oscillator is a
	// different thing from a step and is rejected by Validate.
	Skew []Segment

	// Steps are wall-only corrections, sorted by At.
	Steps []Step

	// Boots are the global times at which this node starts running. The first
	// boot is implicit at time 0; each entry after that is a restart, at which
	// Mono returns to zero and Wall does not.
	Boots []Instant

	// Epoch is where this node's wall clock reads at global time zero. It is a
	// plan constant and it must be nonzero, so that a zero Wall anywhere in the
	// system reads as unset rather than as the beginning of the run. Skew
	// between two nodes is a difference, so the epoch cancels and the checker
	// is unaffected by its value.
	Epoch Wall
}

// DefaultEpoch is the wall reading at global time zero when a plan does not say
// otherwise: 2020-09-13T12:26:40Z in Unix nanoseconds. Any nonzero constant
// would do; what matters is that it is not zero.
const DefaultEpoch = Wall(1_600_000_000_000_000_000)

var (
	errUnsorted      = errors.New("clock: segments or steps are out of order")
	errDiscontinuous = errors.New("clock: skew segments are discontinuous; a jump belongs in Steps, not in the oscillator")
	errBackwards     = errors.New("clock: segment slope <= -1e9 would run the oscillator backwards")
)

// Validate rejects a Timeline that cannot mean anything, at construction rather
// than at the first surprising reading. The three failures it catches are the
// three ways a schedule can be authored that has no physical interpretation.
func (tl *Timeline) Validate() error {
	if len(tl.Skew) == 0 {
		return errors.New("clock: timeline has no skew segments; use one flat segment at 0")
	}
	if tl.Skew[0].Start != 0 {
		return fmt.Errorf("clock: first skew segment starts at %d, want 0", tl.Skew[0].Start)
	}
	if tl.Epoch == 0 {
		return errors.New("clock: timeline has a zero wall epoch; zero means unset, so a run would be unable to tell an unfilled field from the start of time (use DefaultEpoch)")
	}

	for i, seg := range tl.Skew {
		if seg.SlopePPB <= -ppb {
			return fmt.Errorf("%w: segment %d has slope %d ppb", errBackwards, i, seg.SlopePPB)
		}
		if i == 0 {
			continue
		}
		prev := tl.Skew[i-1]
		if seg.Start <= prev.Start {
			return fmt.Errorf("%w: skew segment %d starts at %d, after %d", errUnsorted, i, seg.Start, prev.Start)
		}
		// The previous segment's offset, extended to this segment's start,
		// must be this segment's offset. Otherwise the oscillator teleports.
		if want := offsetIn(prev, seg.Start); want != seg.Off {
			return fmt.Errorf("%w: segment %d starts at offset %d, but segment %d reaches %d there",
				errDiscontinuous, i, seg.Off, i-1, want)
		}
	}

	for i := 1; i < len(tl.Steps); i++ {
		if tl.Steps[i].At < tl.Steps[i-1].At {
			return fmt.Errorf("%w: step %d at %d, after %d", errUnsorted, i, tl.Steps[i].At, tl.Steps[i-1].At)
		}
	}
	for i := 1; i < len(tl.Boots); i++ {
		if tl.Boots[i] <= tl.Boots[i-1] {
			return fmt.Errorf("%w: boot %d at %d, after %d", errUnsorted, i, tl.Boots[i], tl.Boots[i-1])
		}
	}
	return nil
}

// osc is the absolute oscillator position at global time t: t plus the node's
// accumulated skew. It is unexported because it is neither reading a node can
// take -- Mono is measured from a boot and Wall adds an epoch and steps -- and
// exporting it would offer a third notion of time with no owner.
//
// Osc is monotone non-decreasing, which is the property the inverse needs, and
// is NOT strictly increasing: a clock running slower than real time advances
// fewer than one nanosecond per nanosecond, so at integer granularity it sits
// still for stretches. That is the physical truth rather than a rounding
// artifact -- a crystal at 0.9x really does not tick on every nanosecond -- and
// it is why solveOsc is specified as "the earliest t at which the oscillator
// has reached target" rather than "the t at which it equals target".
//
// Validate's slope > -1e9 rule is what keeps it non-decreasing. A slope of
// exactly -1e9 would stop the clock forever; below that it would run backwards,
// and an oscillator that runs backwards is not a fault, it is a different kind
// of object.
func (tl *Timeline) osc(t Instant) Instant {
	return t + Instant(tl.skewAt(t))
}

// Mono is elapsed oscillator time since the boot in force at t. It restarts at
// zero on every restart, mirroring CLOCK_MONOTONIC, whose epoch is unspecified
// and in practice per-boot.
func (tl *Timeline) Mono(t Instant) Mono {
	return Mono(tl.osc(t) - tl.osc(tl.BootAt(t)))
}

// Wall is the node's estimate of physical time: the oscillator plus every step
// applied at or before t. Steps are applied at their instant, so Wall is
// right-continuous; WallLimit reads the value approached from the left.
func (tl *Timeline) Wall(t Instant) Wall {
	return tl.Epoch + Wall(tl.osc(t)) + Wall(tl.stepsThrough(t, true))
}

// WallLimit is Wall's left limit at t: the value an observer would have read
// arbitrarily shortly before t. At a step, Wall(t) and WallLimit(t) differ by
// exactly that step's delta, and the pair is why the skew checker can be exact
// at a discontinuity rather than reading one of two values and calling it the
// answer (DESIGN-A0.4 D5).
func (tl *Timeline) WallLimit(t Instant) Wall {
	return tl.Epoch + Wall(tl.osc(t)) + Wall(tl.stepsThrough(t, false))
}

// BootAt returns the start of the boot in force at t. Times before the first
// restart belong to boot zero, which starts at 0.
func (tl *Timeline) BootAt(t Instant) Instant {
	start := Instant(0)
	for _, b := range tl.Boots {
		if b > t {
			break
		}
		start = b
	}
	return start
}

// NextTick returns the global time of the first tick strictly after t, together
// with its per-boot ordinal (the first tick of a boot is 1).
//
// This is the closed-form inverse of the oscillator, not a rescheduled
// interval: tick k of a boot fires when Mono reaches k*interval, and that
// equation is solved exactly on whichever segment contains the answer. The two
// alternatives both fail quietly -- a fixed global cadence quantizes drift to
// whole ticks, and re-deriving the interval per segment is exactly right within
// a segment and wrong across every boundary (DESIGN-A0.4 D3).
//
// A restart cancels the current boot's pending tick: if the next boot begins
// before the tick would have fired, the answer is that boot's first tick.
func (tl *Timeline) NextTick(t Instant, interval time.Duration) (at Instant, ordinal int64) {
	iv := int64(interval)
	if iv <= 0 {
		panic("clock: NextTick called with a non-positive interval")
	}

	for {
		boot := tl.BootAt(t)
		mono := int64(tl.Mono(t))
		ordinal = mono/iv + 1
		target := tl.osc(boot) + Instant(ordinal*iv)

		at = tl.solveOsc(target)
		// Ties go forward: a tick landing exactly on t belongs to the past,
		// since the contract is "strictly after".
		if at <= t {
			ordinal++
			at = tl.solveOsc(tl.osc(boot) + Instant(ordinal*iv))
		}

		next, ok := tl.nextBootAfter(t)
		if !ok || at < next {
			return at, ordinal
		}
		// The node restarts before that tick would have fired. Start again
		// from the boot instant, where Mono is zero.
		t = next
	}
}

func (tl *Timeline) nextBootAfter(t Instant) (Instant, bool) {
	for _, b := range tl.Boots {
		if b > t {
			return b, true
		}
	}
	return 0, false
}

// solveOsc returns the earliest global time at which the oscillator has reached
// target. Rounding up is what makes "has reached" true rather than
// "approached": on a slow clock several global nanoseconds can share one
// oscillator reading, and the earliest of them is the answer.
func (tl *Timeline) solveOsc(target Instant) Instant {
	for i, seg := range tl.Skew {
		// Osc within this segment: t + Off + SlopePPB*(t-Start)/ppb, which
		// rearranges to Start + (target-Start-Off)*ppb/(ppb+SlopePPB).
		rel := int64(target) - int64(seg.Start) - seg.Off
		if rel < 0 {
			// target is behind this segment's start; the oscillator is
			// strictly increasing, so it was reached earlier.
			return seg.Start
		}
		dt := mulDivCeil(rel, ppb, ppb+seg.SlopePPB)
		at := seg.Start + Instant(dt)

		last := i == len(tl.Skew)-1
		if last || at < tl.Skew[i+1].Start {
			return at
		}
	}
	// Unreachable: the final segment extends to infinity and its slope is
	// > -1e9, so the oscillator reaches every target eventually.
	panic("clock: oscillator never reaches target; timeline was not validated")
}

// skewAt evaluates the piecewise-linear offset. Linear search rather than a
// binary one: schedules have a handful of segments, and a cursor would be state
// on what is otherwise a pure function.
func (tl *Timeline) skewAt(t Instant) int64 {
	seg := tl.Skew[0]
	for _, s := range tl.Skew {
		if s.Start > t {
			break
		}
		seg = s
	}
	return offsetIn(seg, t)
}

// offsetIn extends seg to time t, which may lie beyond the segment: Validate
// uses exactly that to check continuity at the next segment's start.
func offsetIn(seg Segment, t Instant) int64 {
	return seg.Off + mulDiv(seg.SlopePPB, int64(t-seg.Start), ppb)
}

// stepsThrough sums the corrections at or before t. inclusive=false takes the
// left limit, excluding a step landing exactly on t.
func (tl *Timeline) stepsThrough(t Instant, inclusive bool) int64 {
	var sum int64
	for _, s := range tl.Steps {
		if s.At > t || (!inclusive && s.At == t) {
			break
		}
		sum += s.Delta
	}
	return sum
}

// Breakpoints returns every global time at which this timeline's Wall changes
// slope or jumps, within [from, to]. It is what makes the skew checker exact:
// between two consecutive breakpoints Wall is linear, so an extremum of the
// difference of two Walls can only sit at one of them.
func (tl *Timeline) Breakpoints(from, to Instant) []Instant {
	out := []Instant{from}
	for _, seg := range tl.Skew {
		if seg.Start > from && seg.Start < to {
			out = append(out, seg.Start)
		}
	}
	for _, s := range tl.Steps {
		if s.At > from && s.At < to {
			out = append(out, s.At)
		}
	}
	out = append(out, to)
	return out
}

// mulDiv computes a*b/d exactly, truncating toward zero, via a 128-bit
// intermediate. No float64 appears anywhere on this path on purpose: the Go
// spec permits fusing a multiply-add into one FMA, which arm64 does and amd64
// without FMA does not, and off + slope*(t-start) is precisely that shape. A
// last-bit difference between a laptop and a CI runner is a different lease
// expiry, which is a different history (DESIGN-A0.4 §4).
func mulDiv(a, b, d int64) int64 {
	if d == 0 {
		panic("clock: division by zero")
	}
	neg := false
	ua, neg := absU64(a, neg)
	ub, neg := absU64(b, neg)
	ud, neg := absU64(d, neg)

	hi, lo := bits.Mul64(ua, ub)
	if hi >= ud {
		panic(fmt.Sprintf("clock: overflow in %d*%d/%d; the schedule is outside the modelled range", a, b, d))
	}
	q, _ := bits.Div64(hi, lo, ud)
	if neg {
		return -int64(q)
	}
	return int64(q)
}

// mulDivCeil is mulDiv rounded up, for non-negative inputs. Ticks round up so
// that a tick fires at the first instant its target has actually been reached.
func mulDivCeil(a, b, d int64) int64 {
	if a < 0 || b < 0 || d <= 0 {
		panic("clock: mulDivCeil requires non-negative inputs")
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(d) {
		panic(fmt.Sprintf("clock: overflow in ceil(%d*%d/%d)", a, b, d))
	}
	q, r := bits.Div64(hi, lo, uint64(d))
	if r != 0 {
		q++
	}
	return int64(q)
}

func absU64(v int64, neg bool) (uint64, bool) {
	if v < 0 {
		return uint64(-v), !neg
	}
	return uint64(v), neg
}
