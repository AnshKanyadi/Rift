// Package plan is the unit of reproduction.
//
// A seed generates a plan; the plan executes a run. The split is the whole
// point (DESIGN-A0 D4, DR-6):
//
//   - **Generation** (seed -> plan) may use sequential randomness freely. It
//     runs once and every one of its outputs is written down.
//   - **Execution** (plan -> run) takes no sequential draw at all. Every random
//     quantity is either materialized verbatim in the plan or is a pure keyed
//     function of a plan-carried key and a canonical event identity.
//
// That is stronger than "the plan plus the seed reproduces the run". A plan
// alone reproduces it, and — the property ddmin actually needs — deleting one
// fault entry leaves every other dice outcome bit-identical, so the minimizer
// is testing what it thinks it is testing.
//
// The keys the plan carries are **PRF keys, not stream positions**. That
// distinction is the one DR-6 turned on: embedding a sub-stream's sequential
// state would satisfy the letter of "the plan alone reproduces" and still
// desynchronize the moment a code change alters how many draws are taken.
// Counter-based dice have no draw order to desynchronize.
//
// **TestDeletingAFaultEntryPerturbsOnlyItself is the enforcement of that
// design, not an independent property.** Per-link keys are precisely why
// deleting one plan entry does not perturb its neighbours. Anyone who switches
// this package to embedded stream state will break that test, and the test is
// where they will find out why they should not.
//
// Execution is guarded rather than promised: **every** built run carries a
// poisoned Rand whose every method panics, so a stray sequential draw dies at
// the call site instead of quietly producing an unreproducible schedule.
//
// That the poison is on by default rather than only in a test is the whole
// guarantee. A poison installed for one exit test proves absence only along the
// paths that one plan happens to exercise; installed on every run, the
// completion of any seed is the proof for that seed, and ten thousand seeds are
// ten thousand proofs. The exit test remains, in the other role: it asserts the
// poison is *live*, because a dead poison silently converts the whole guarantee
// into decoration.
package plan

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/sim"
)

// SchemaVersion is the plan format. A plan carrying a different version is
// rejected rather than best-effort parsed: a corpus entry that half-loads is
// worse than one that does not load.
const SchemaVersion = 1

// Plan is a complete, executable description of one run.
type Plan struct {
	SchemaVersion int        `json:"schema_version"`
	Provenance    Provenance `json:"provenance"`
	Config        Config     `json:"config"`
	Keys          Keys       `json:"keys"`
	Network       Network    `json:"network"`
	Clock         ClockPlan  `json:"clock"`
	Faults        Faults     `json:"faults"`
	Workload      Workload   `json:"workload"`
	Assert        Assert     `json:"assert"`
}

// Provenance is for humans and for `git bisect`. It is **never read during
// execution**: a plan that behaved differently depending on which commit
// generated it would not be a reproduction.
type Provenance struct {
	Seed             uint64 `json:"seed"`
	GitSHA           string `json:"git_sha,omitempty"`
	GeneratorVersion int    `json:"generator_version"`
	Note             string `json:"note,omitempty"`
}

// Config is the run's shape.
type Config struct {
	Nodes          int    `json:"nodes"`
	DurationNS     int64  `json:"duration_ns"`
	TickIntervalNS int64  `json:"tick_interval_ns"`
	MaxOffsetNS    int64  `json:"max_offset_ns"`
	MaxSteps       uint64 `json:"max_steps"`
}

// Keys are the PRF keys, one per domain that needs independent dice. Written as
// hex so a plan stays readable and diffable.
//
// Named separately rather than derived at execution from a single root, so that
// changing how one domain derives its key cannot silently reshuffle another.
type Keys struct {
	Net        string `json:"net"`
	EngineSync string `json:"engine_sync"`
	ClockJit   string `json:"clock"`
	Raft       string `json:"raft"`
}

// Network is the per-directed-link dice.
type Network struct {
	Links []Link `json:"links"`
}

// Link is one directed link's parameters. Probabilities are per-mille integers:
// no float appears anywhere on an execution path (DESIGN-A0.4 §4).
type Link struct {
	From         int    `json:"from"`
	To           int    `json:"to"`
	DropPerMille uint64 `json:"drop_per_mille"`
	DupPerMille  uint64 `json:"dup_per_mille"`
	LatMinNS     int64  `json:"lat_min_ns"`
	LatMaxNS     int64  `json:"lat_max_ns"`
	TailPerMille uint64 `json:"tail_per_mille"`
	TailMaxNS    int64  `json:"tail_max_ns"`
}

// ClockPlan is the per-node timelines plus the authored holds.
type ClockPlan struct {
	EpochNS int64       `json:"epoch_ns"`
	Nodes   []NodeClock `json:"nodes"`
	Holds   []Hold      `json:"holds"`
}

// NodeClock is one node's oscillator schedule.
type NodeClock struct {
	Node  int       `json:"node"`
	Skew  []Segment `json:"skew,omitempty"`
	Steps []Step    `json:"steps,omitempty"`
	Boots []int64   `json:"boots,omitempty"`
}

type Segment struct {
	StartNS  int64 `json:"start_ns"`
	OffNS    int64 `json:"off_ns"`
	SlopePPB int64 `json:"slope_ppb"`
}

type Step struct {
	AtNS    int64 `json:"at_ns"`
	DeltaNS int64 `json:"delta_ns"`
}

// Hold pins a pair at a fraction of maxOffset for a window.
//
// Realization is authored rather than inferred from Ramp, so that ddmin can
// convert a slew into a step and ask whether the bug survives. The two stress
// different consumers -- a step is the HLC case, a slew is the lease-stasis
// case -- and a bundle that could not tell them apart would leave an
// investigator guessing.
type Hold struct {
	A int `json:"a"`
	B int `json:"b"`

	// AtPPB is the target as parts per billion of maxOffset: 980_000_000 is
	// 0.98 of the bound.
	//
	// An integer, and the reason is the whole point of the plan format. The
	// plan is replay identity. A fraction carried here would be multiplied on
	// the replaying machine, and that multiply-add is exactly what an arm64
	// fuses into one FMA and an amd64 without FMA does not -- reconstructing
	// the cross-architecture divergence the float rule exists to kill, this
	// time behind an exemption. No float crosses this boundary in either
	// direction, and TestPlanCarriesNoFloatingPoint checks it structurally.
	AtPPB int64 `json:"at_ppb"`

	FromNS   int64  `json:"from_ns"`
	ToNS     int64  `json:"to_ns"`
	RampNS   int64  `json:"ramp_ns"`
	Realize  string `json:"realize"` // "step" or "slew"
	Envelope bool   `json:"envelope,omitempty"`
}

// Faults are timed entries and reactive rules.
type Faults struct {
	Entries []Entry `json:"entries,omitempty"`
	Rules   []Rule  `json:"rules,omitempty"`
}

// Entry is a fault at an absolute instant. Individually deletable, which is
// what makes ddmin possible at all.
type Entry struct {
	AtNS   int64  `json:"at_ns"`
	Action string `json:"action"` // crash | restart | cut | heal | cut_both | heal_both | promote | conf
	Node   int    `json:"node,omitempty"`
	From   int    `json:"from,omitempty"`
	To     int    `json:"to,omitempty"`
}

// Rule is a reactive fault: one that fires on a named condition rather than at
// a known instant, because "partition the leader 200ms after it is elected"
// cannot be materialized by time.
//
// It is still declarative and still individually deletable, so the nemesis is a
// rule generator rather than an escape from the plan.
type Rule struct {
	On      string `json:"on"`
	AfterNS int64  `json:"after_ns"`
	Action  string `json:"action"`
	Node    int    `json:"node,omitempty"`
	From    int    `json:"from,omitempty"`
	To      int    `json:"to,omitempty"`
	Times   int    `json:"times"`
}

// Workload is fully materialized, for the same reason the faults are: the
// minimizer must be able to delete one client operation without perturbing
// anything else.
type Workload struct {
	Ops []Op `json:"ops,omitempty"`
}

type Op struct {
	AtNS   int64  `json:"at_ns"`
	Client int    `json:"client"`
	Seq    uint64 `json:"seq"`
	Kind   string `json:"op"`
	Key    string `json:"k,omitempty"`
	Value  string `json:"v,omitempty"`
}

// Assert is the fire-count floor. An enabled injector that never fired is a
// failed run, and this is where the run says which injectors it enabled.
type Assert struct {
	MinFires map[string]uint64 `json:"min_fires,omitempty"`
}

// Marshal renders a plan as indented JSON.
//
// Readability is a requirement rather than a nicety: "partition {1} from {2,3}
// at 4.2s for 800ms; crash node 2 at 4.9s" is a bug report a human can argue
// with, and "seed 8834127" is not.
func Marshal(p *Plan) ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// Unmarshal parses a plan and validates it.
func Unmarshal(b []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate rejects a plan that cannot mean anything.
func (p *Plan) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("plan: schema version %d, this build speaks %d; a corpus entry that half-loads is worse than one that does not load",
			p.SchemaVersion, SchemaVersion)
	}
	if p.Config.Nodes <= 0 {
		return fmt.Errorf("plan: %d nodes", p.Config.Nodes)
	}
	if p.Config.TickIntervalNS <= 0 {
		return fmt.Errorf("plan: tick interval is %dns", p.Config.TickIntervalNS)
	}
	if p.Clock.EpochNS == 0 {
		return fmt.Errorf("plan: wall epoch is zero, so an unset Wall would be indistinguishable from the start of the run")
	}
	for _, k := range []struct{ name, hex string }{
		{"net", p.Keys.Net}, {"engine_sync", p.Keys.EngineSync},
		{"clock", p.Keys.ClockJit}, {"raft", p.Keys.Raft},
	} {
		if k.hex == "" {
			return fmt.Errorf("plan: key %q is empty; a plan without its keys cannot reproduce its run", k.name)
		}
		if _, err := rng.ParseKey(k.hex); err != nil {
			return fmt.Errorf("plan: key %q: %w", k.name, err)
		}
	}
	if err := p.validateHolds(); err != nil {
		return err
	}
	for i, e := range p.Faults.Entries {
		if !knownAction(e.Action) {
			return fmt.Errorf("plan: fault entry %d has unknown action %q", i, e.Action)
		}
	}
	for i, r := range p.Faults.Rules {
		if !knownAction(r.Action) {
			return fmt.Errorf("plan: rule %d has unknown action %q", i, r.Action)
		}
		if r.On == "" {
			return fmt.Errorf("plan: rule %d has no trigger", i)
		}
	}
	return nil
}

// validateHolds enforces the legality a hold's physics requires.
//
// The generator was taught not to layer a hold on a drifting node, because a
// hold *is* oscillator discipline and free drift underneath it was the
// generator lying about the crystal. But the generator is not the only author
// of plans: a hand-written plan, a corpus entry edited during an
// investigation, and any future fuzzer all bypass a generator-side fix
// entirely. Legality belongs here, where the illegal shape is rejected no
// matter who wrote it.
func (p *Plan) validateHolds() error {
	drift := make([]bool, p.Config.Nodes)
	for _, nc := range p.Clock.Nodes {
		if nc.Node < 0 || nc.Node >= p.Config.Nodes {
			return fmt.Errorf("plan: clock schedule names unknown node %d", nc.Node)
		}
		for _, seg := range nc.Skew {
			if seg.SlopePPB != 0 {
				drift[nc.Node] = true
			}
		}
	}

	held := make([]bool, p.Config.Nodes)
	for i, h := range p.Clock.Holds {
		if h.Realize != "step" && h.Realize != "slew" {
			return fmt.Errorf("plan: hold %d realizes as %q; it must say \"step\" or \"slew\" rather than leave it to be inferred", i, h.Realize)
		}
		if h.B < 0 || h.B >= p.Config.Nodes {
			return fmt.Errorf("plan: hold %d names unknown node %d", i, h.B)
		}
		if h.FromNS >= h.ToNS {
			return fmt.Errorf("plan: hold %d has an empty window [%d,%d)", i, h.FromNS, h.ToNS)
		}
		// A hold is a configuration rather than an event, so no fire count can
		// tell you it took effect; the equivalent guarantee is static, and this
		// is it. A window entirely past the deadline is a hold the run never
		// experiences, which is a plan claiming a fault it does not inject --
		// the same class as a restart scheduled past the end of time.
		if h.FromNS >= p.Config.DurationNS {
			return fmt.Errorf(
				"plan: hold %d opens at %dns, at or after the run ends at %dns, so the run never experiences it",
				i, h.FromNS, p.Config.DurationNS)
		}
		if drift[h.B] {
			return fmt.Errorf(
				"plan: hold %d disciplines node %d, which the plan also gives free drift; a hold is oscillator discipline, so its target would depend on when it began",
				i, h.B)
		}
		if held[h.B] {
			return fmt.Errorf("plan: node %d carries two holds; they would fight over one timeline and the plan would mean neither", h.B)
		}
		held[h.B] = true

		if h.Realize == "slew" {
			if h.RampNS <= 0 {
				return fmt.Errorf("plan: hold %d slews with a zero ramp; a slew is a rate and needs a window, use \"step\" for an instant correction", i)
			}
			if h.FromNS < h.RampNS {
				return fmt.Errorf("plan: hold %d ramps in from %d, which is before time zero", i, h.FromNS-h.RampNS)
			}
		}
	}
	return nil
}

// ReplicationRTT is the round trip this plan's network implies: twice the
// slowest ordinary link latency in it.
//
// It exists so that a consumer validating a timing regime against the network
// -- toy.ValidateWindow is the first -- takes the number from the plan it is
// about to run rather than from a constant somebody chose. A constant would make
// the validation pass by construction on every plan, which is not validation.
//
// # Why the heavy tail is deliberately excluded
//
// A link's tail excursions are rare by construction (TailPerMille), and folding
// TailMax in here would report a round trip two orders of magnitude above the
// one almost every message actually experiences. The regime question -- is fsync
// slower than replication in the common case -- is about the common case. A tail
// excursion that does make one replication slower than one fsync costs a
// detection opportunity for that message; it cannot make the flaw class
// unreachable, which is the thing the validation is guarding against.
func (p *Plan) ReplicationRTT() clock.Instant {
	var worst int64
	for _, l := range p.Network.Links {
		if l.LatMaxNS > worst {
			worst = l.LatMaxNS
		}
	}
	return clock.Instant(2 * worst)
}

func knownAction(a string) bool {
	switch a {
	case "crash", "restart", "cut", "heal", "cut_both", "heal_both", "promote", "conf":
		return true
	}
	return false
}

// realization converts the authored string to the clock package's enum.
func realization(s string) clock.Realization {
	switch s {
	case "step":
		return clock.StepHold
	case "slew":
		return clock.SlewHold
	}
	return clock.RealizeUnset
}

func ns(d int64) time.Duration { return time.Duration(d) }
func at(d int64) clock.Instant { return clock.Instant(d) }
func nodeID(i int) sim.NodeID  { return sim.NodeID(i) }
