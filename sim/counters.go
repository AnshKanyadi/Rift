package sim

import (
	"fmt"
	"strings"

	"github.com/anshkanyadi/rift/internal/sorted"
)

// Injector names every countable thing the harness does to a run.
//
// The counters exist for one reason: **a run in which an enabled injector never
// fired is a failure, not a pass** (DESIGN-A0 D9, DR-14). Without that
// assertion a chaos suite is chaos-shaped decoration — every seed green, no
// partition ever formed, and nobody the wiser. With it, a soak can say "10k
// seeds, of which partitions fired 41,207 times and sync-loss windows swallowed
// 3,918 acknowledged writes", which is a sentence that means something.
type Injector uint8

const (
	// Traffic, counted so the fault rates have a denominator.
	InjSent Injector = iota + 1
	InjDeliver

	// Per-message dice.
	InjDrop
	InjDuplicate
	InjTailLatency
	InjReorder

	// Link state.
	InjPartition
	InjPartitionDrop

	// Node state.
	InjCrash
	InjRestart
	InjPause

	// Storage.
	InjSyncLoss
	InjUnsyncedLost

	// Clocks.
	InjClockDrift
	InjClockStep
	InjClockHold

	numInjectors
)

func (i Injector) String() string {
	switch i {
	case InjSent:
		return "sent"
	case InjDeliver:
		return "deliver"
	case InjDrop:
		return "drop"
	case InjDuplicate:
		return "duplicate"
	case InjTailLatency:
		return "tail-latency"
	case InjReorder:
		return "reorder"
	case InjPartition:
		return "partition"
	case InjPartitionDrop:
		return "partition-drop"
	case InjCrash:
		return "crash"
	case InjRestart:
		return "restart"
	case InjPause:
		return "pause"
	case InjSyncLoss:
		return "sync-loss"
	case InjUnsyncedLost:
		return "unsynced-lost"
	case InjClockDrift:
		return "clock-drift"
	case InjClockStep:
		return "clock-step"
	case InjClockHold:
		return "clock-hold"
	case numInjectors:
		return "invalid"
	}
	return "unknown"
}

// InjectorByName resolves a plan's min_fires key to the injector it names. It
// lives here rather than in the plan package so that the name a plan uses and
// the name a report prints cannot drift apart.
func InjectorByName(s string) (Injector, bool) {
	for i := Injector(1); i < numInjectors; i++ {
		if i.String() == s {
			return i, true
		}
	}
	return 0, false
}

// Counters records fires and the minimum each enabled injector must reach.
//
// An array rather than a map: the injector set is closed and known at compile
// time, and a map here would put iteration order into the one structure whose
// job is to be reported identically on every run.
type Counters struct {
	fired    [numInjectors]uint64
	minFires map[Injector]uint64
}

// NewCounters returns an empty set with no minimums configured.
func NewCounters() *Counters {
	return &Counters{minFires: make(map[Injector]uint64)}
}

// Fire records one occurrence. Nil-safe, so a transport built without counters
// in a unit test does not need a branch at every call site.
func (c *Counters) Fire(i Injector) {
	if c == nil || i >= numInjectors {
		return
	}
	c.fired[i]++
}

// Add records n occurrences, for injectors that count quantities rather than
// events -- unsynced writes lost to a crash, for instance.
func (c *Counters) Add(i Injector, n uint64) {
	if c == nil || i >= numInjectors {
		return
	}
	c.fired[i] += n
}

// Count returns how many times an injector fired.
func (c *Counters) Count(i Injector) uint64 {
	if c == nil || i >= numInjectors {
		return 0
	}
	return c.fired[i]
}

// Require declares that an injector is enabled and must fire at least n times.
// The default for an enabled injector is one: zero fires is a failed run.
func (c *Counters) Require(i Injector, n uint64) {
	if n == 0 {
		n = 1
	}
	c.minFires[i] = n
}

// Shortfall is an injector that did not fire often enough.
type Shortfall struct {
	Injector Injector
	Want     uint64
	Got      uint64
}

func (s Shortfall) String() string {
	return fmt.Sprintf("%s fired %d times, wanted at least %d", s.Injector, s.Got, s.Want)
}

// Check returns every injector that fell short, in a stable order.
//
// The caller fails the run on a non-empty result. It is separated from the
// failing so that a hunt can report all shortfalls at once rather than the
// first, and so that the envelope mode -- where some injectors are deliberately
// off -- can decide for itself.
func (c *Counters) Check() []Shortfall {
	var out []Shortfall
	for _, i := range sorted.Keys(c.minFires) {
		if got := c.Count(i); got < c.minFires[i] {
			out = append(out, Shortfall{Injector: i, Want: c.minFires[i], Got: got})
		}
	}
	return out
}

// Report renders the counters for a run summary, in injector order so two runs
// are diffable line by line.
func (c *Counters) Report() string {
	var b strings.Builder
	for i := Injector(1); i < numInjectors; i++ {
		if n := c.Count(i); n > 0 {
			fmt.Fprintf(&b, "%-16s %d\n", i, n)
		}
	}
	return b.String()
}
