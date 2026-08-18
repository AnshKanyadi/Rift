// Package toy is the A0 acceptance protocol: a fixed-primary replicated
// register, deliberately simple and deliberately breakable.
//
// It exists to calibrate the harness rather than to be a system. "A toy state
// machine survives 1k seeds" proves the harness runs; the mutants prove it
// catches. So the toy is written to be *wrong in specific, named ways* on
// demand, and the interesting result is that a wrong toy is caught by a hunt
// rather than by a hand-built fixture.
//
// # The protocol
//
// One primary (node 0), the rest backups. A client sends a put or a get to the
// primary, which:
//
//   - dedupes by (client, seq), so a retried request applies at most once;
//   - for a put: writes to its engine, replicates to every backup, and
//     acknowledges only after the write is durable AND every backup has
//     acknowledged;
//   - for a get: answers from its own state.
//
// Under a partition it becomes unavailable, and that is correct behaviour. The
// oracle must not score it as a violation: a cluster that stops answering when
// it cannot safely answer is doing its job.
//
// # Why it is single-writer
//
// A fixed primary removes elections from the picture entirely. The harness is
// what is under test here; a toy with its own consensus would make a failure
// ambiguous between the two.
package toy

import (
	"errors"
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/internal/sorted"
	"github.com/anshkanyadi/rift/sim"
)

// Flaw names a deliberate defect. The zero value is a correct toy.
//
// These are the mutants of DESIGN-A0 §5, expressed as a field rather than as a
// patch, because the toy is the one place where being wrong on purpose is the
// feature. The mutant *suite* still applies them as patches to a scratch tree
// (DR-27); this field is what makes a flaw reachable from a hunt.
type Flaw uint8

const (
	// FlawNone is a correct toy.
	FlawNone Flaw = iota

	// FlawAckBeforeSync acknowledges a write before it is durable, so a crash
	// loses an acknowledged write. M1.
	FlawAckBeforeSync

	// FlawAckBeforeReplicate acknowledges before the backups have it, so a
	// primary crash loses an acknowledged write. M2.
	FlawAckBeforeReplicate

	// FlawDupApply applies a retried request twice instead of deduping. M3.
	FlawDupApply

	// FlawDirtyRead serves reads from the engine's visible state instead of
	// from committed state, so a client can observe a write that is still
	// speculative and that a crash then takes back.
	//
	// Added with the fix for the defect it names, per Amendment A2: the moment a
	// fix lands is the only moment we have a precise description of the blind
	// spot that let the bug through. The harness found this one on seed 103 with
	// no flaw planted.
	FlawDirtyRead

	// FlawAckCounting counts acknowledgements instead of tracking which peers
	// sent them, so one backup's duplicated ack satisfies the whole quorum.
	//
	// Also added with its fix. Found on seed 153, and it is the toy-sized
	// version of counting votes without checking who cast them.
	FlawAckCounting

	numFlaws
)

// ParseFlaw is the inverse of String, for command lines and bundle metadata.
//
// Unknown names are refused rather than defaulting to FlawNone: a typo that
// silently selected the correct toy would report a clean sweep for a flaw
// nobody ran.
func ParseFlaw(s string) (Flaw, error) {
	for f := FlawNone; f < numFlaws; f++ {
		if f.String() == s {
			return f, nil
		}
	}
	return FlawNone, fmt.Errorf("toy: unknown flaw %q", s)
}

func (f Flaw) String() string {
	switch f {
	case FlawNone:
		return "none"
	case FlawAckBeforeSync:
		return "ack-before-sync"
	case FlawAckBeforeReplicate:
		return "ack-before-replicate"
	case FlawDupApply:
		return "dup-apply"
	case FlawDirtyRead:
		return "dirty-read"
	case FlawAckCounting:
		return "ack-counting"
	case numFlaws:
		return "invalid"
	}
	return "unknown"
}

// DefaultSyncLatency is the modelled fsync duration: 12ms, the smallest window
// that satisfies both of the class's constraints for every seed the generator
// can produce. Derived, not chosen.
//
// # Correction: the original rationale was wrong, including where the number worked
//
// This constant was 50ms, and it was defended by an equivalence argument: the
// ack-before-durable class exists only when fsync is slower than a replication
// round trip, so the window had to clear the round trip by a margin of three.
// **That argument was checking the wrong quantity.** Re-measuring the curve on a
// harness with the Trigger budget defect fixed shows the step is not at any
// multiple of the round trip:
//
//	fsync   2ms:     0 eligible of 1000   -- no seed's network is fast enough
//	fsync  10ms:   344 eligible,   11 per mille, first at seed 338
//	fsync  11ms:   344 eligible,  534 per mille, first at seed 1
//	fsync  12ms:  1000 eligible,  499 per mille, first at seed 1
//	fsync  20ms:  1000 eligible,  500 per mille, first at seed 1
//	fsync  50ms:  1000 eligible,  504 per mille, first at seed 1
//
// The step is between 10ms and 11ms, which is crashDelay. The reactive crash
// fires that long after the window opens, so a window narrower than it has
// already closed -- the write is durable -- before the crash lands, and every
// attempt yields an in-flight operation the checker correctly refuses to score.
//
// So the class needs *two* independent things, and only the weaker one was ever
// stated:
//
//  1. **Equivalence.** fsync slower than a replication round trip, or the flawed
//     toy and the correct one are behaviourally identical and there is nothing
//     in existence to detect. True, still checked, and never the binding
//     constraint at any window this project has used.
//  2. **Reachability.** fsync longer than the crash delay, or the flaw exists
//     and cannot be reached. This is what actually bound, and it was unchecked.
//
// The old rationale happened to produce a working number, which is why it
// survived three cycles. A reason that is wrong but yields the right answer is
// worse than one that is visibly wrong, because nothing ever contradicts it.
//
// # Why 12, and why not 11
//
// The detection *rate* saturates at 11ms -- 534 per mille against 504 at 50ms --
// and 11ms is the answer if the rate column is read alone. The eligibility column
// says otherwise:
//
//	fsync  11ms:   344 eligible,  184 of 1000 seeds caught
//	fsync  12ms:  1000 eligible,  499 of 1000 seeds caught
//	fsync  50ms:  1000 eligible,  504 of 1000 seeds caught
//
// At 11ms the *equivalence* constraint refuses the slowest two thirds of seeds:
// the generator draws each link's upper latency from [1ms, 6ms], so a round trip
// runs to 12ms, and any seed whose network is slower than the window is a regime
// where the flaw cannot exist. Narrowing to 11ms would have raised the rate while
// **cutting absolute detections from 504 to 184** and making two thirds of the
// seed space untestable for this class -- including for the correct toy, since
// the gate refuses a configuration regardless of what is planted in it.
//
// So the two constraints together give the number, and it needs no measurement at
// all:
//
//	window > crashDelay          = 10ms   (reachability)
//	window >= worst-case RTT     = 12ms   (equivalence, 2 x the 6ms slowest link)
//
// Twelve. The measured curve then confirms it: full eligibility and 499 of 1000,
// against 504 at a window four times wider.
//
// The old 50ms was therefore roughly four times wider than the flaw requires,
// which made the toy easier than the thing it calibrates. The number is now the
// tightest regime in which the planted flaws exist on every seed.
//
// Lowering it below crashDelay returns the whole class to unreachable while every
// seed passes and every lane stays green -- structurally the crash injector that
// marked a node down without telling it, a clean sweep over an empty search
// space. ValidateWindow refuses that rather than trusting this comment.
const DefaultSyncLatency = clock.Instant(12_000_000)

// MinWindowMargin is how far the modelled fsync must exceed the replication
// round trip. **One**, and it is now a derived number rather than a stated one.
//
// It was three, defended by a curve showing detection "still marginal" at 10ms.
// That curve was measured under the Trigger budget defect, on a harness running
// at a sixth of its power, so it could not support any margin -- and re-measuring
// on the fixed harness shows the round trip was never the binding constraint.
// Parity is the real equivalence threshold: at ratio 1 the flaw exists, and the
// only reason 10ms detects poorly is crashDelay (see DefaultSyncLatency).
//
// A margin above 1 would refuse regimes where the flaw genuinely manifests,
// which is a gate refusing correct configurations for a reason that is not true.
// The reachability constraint that *does* bind is checked separately and
// explicitly, against the quantity that actually governs it.
const MinWindowMargin = 1

// ValidateWindow refuses a configuration in which the planted flaws cannot
// manifest.
//
// This is a gate rather than a warning. A harness that runs happily in a regime
// where its own mutants are invisible reports green sweeps that mean nothing,
// and the report reads identically to a real one.
//
// It is called by New, not merely exported for tests to call. A gate no
// production path passes through is a green thing that cannot fail -- the same
// class as the crash injector that marked a node down without telling it -- and
// TestNewRefusesAWindowTheFlawCannotManifestIn proves the wiring rather than the
// rule.
func ValidateWindow(syncLatency clock.Instant, replicationRTT clock.Instant) error {
	if syncLatency <= 0 || replicationRTT <= 0 {
		return fmt.Errorf("toy: window validation needs positive durations, got fsync %d and rtt %d", syncLatency, replicationRTT)
	}

	// Constraint 1: equivalence. Below parity the flawed toy and the correct toy
	// behave identically, so there is nothing in existence to detect.
	if int64(syncLatency) < int64(replicationRTT)*MinWindowMargin {
		return fmt.Errorf(
			"toy: modelled fsync of %dus does not exceed the replication round trip of %dus; "+
				"in this regime a primary awaiting backup acks is already durable when it answers, so ack-before-durable "+
				"cannot manifest at all and a clean sweep would be a sweep over an empty search space: %w",
			int64(syncLatency)/1000, int64(replicationRTT)/1000, ErrWindowTooNarrow)
	}

	// Constraint 2: reachability, and the one that actually binds. The reactive
	// crash fires crashDelay after the window opens; a window that has already
	// closed by then leaves an in-flight operation the checker correctly refuses
	// to score, which is not a violation and not a detection.
	//
	// Strictly greater, with no extra margin, because the curve says none is
	// needed: at exactly crashDelay detection is 11 per mille, one millisecond
	// above it is 534, and it is flat from there to 50ms. Demanding a multiple
	// would refuse configurations that are measurably at full power.
	if syncLatency <= crashDelay {
		return fmt.Errorf(
			"toy: modelled fsync of %dus does not outlast the reactive crash delay of %dus, so the write is "+
				"already durable when the crash lands and every attempt produces an in-flight operation the "+
				"checker cannot score; detection at parity is 11 per mille against 534 one millisecond above it: %w",
			int64(syncLatency)/1000, int64(crashDelay)/1000, ErrWindowTooNarrow)
	}
	return nil
}

// ErrWindowTooNarrow identifies a refusal by the window gate specifically, so a
// caller can tell "this seed's network is too slow for the flaw to exist" from
// "the harness is broken".
//
// It is a sentinel rather than a string match because of what the distinction is
// worth to the ablation: a refused seed is not a failure and not a pass, it is a
// seed that was **never eligible**, and counting it in the denominator of a
// detection rate would quietly understate the harness's power over the seeds
// where the flaw could actually occur. Per-seed link latencies vary, so
// eligibility varies seed to seed rather than globally -- which is exactly the
// nuance a single global curve could not express.
var ErrWindowTooNarrow = errors.New("modelled fsync window leaves the flaw class unreachable")

// Request is a client operation carried as an event payload.
type Request struct {
	Client  int
	Seq     uint64
	Op      string // "put" | "get"
	Key     string
	Value   string
	HistIdx int // index into the harness history, for the response
}

// wire message kinds.
const (
	kindReplicate uint16 = 1
	kindAck       uint16 = 2
)

// pending is a put waiting for durability and backup acknowledgements.
type pending struct {
	req Request
	seq engine.SeqNum

	// acked is the set of peers that have acknowledged, and it is a set rather
	// than a counter for a reason the harness found on seed 153.
	//
	// Counting acknowledgements makes a *duplicated* ack from one backup satisfy
	// the quorum while the other backup never received the write at all. The
	// transport duplicates messages on purpose (DupPerMille), so this is not a
	// hypothetical: the primary answered a client, was promoted away from, and
	// the surviving replica did not have the value. Counting responses instead
	// of distinct responders is the same mistake as counting votes without
	// checking who cast them, which is how a consensus implementation
	// accidentally commits without a majority.
	acked map[sim.NodeID]bool

	// acks is the same information counted rather than deduplicated, kept only
	// so FlawAckCounting can be the bug this field used to be.
	acks int

	durable  bool
	answered bool
}

// Node is one toy replica.
type Node struct {
	ID      sim.NodeID
	Primary sim.NodeID
	Peers   []sim.NodeID
	Flaw    Flaw

	db   *model.DB
	tr   sim.Transport
	hist *sim.History

	// applied dedupes by (client, seq): the highest sequence each client has
	// had applied. Client dedupe is one of the invariants the harness checks.
	applied map[int]uint64

	// inflight is the put awaiting completion, keyed by engine sequence.
	inflight map[engine.SeqNum]*pending

	// committed is the state a client is allowed to observe.
	//
	// # Why reads do not come straight from the engine
	//
	// They did, and it was a dirty read: `db.Get` returns visible state, which
	// includes a write that is neither durable nor replicated nor acknowledged.
	// A client could therefore read a value the primary was still holding
	// speculatively, and a crash a moment later would take it back -- so a read
	// observed a write that then un-happened, which is not linearizable and is
	// not something the flaw fields asked for. The harness found it on seed 103
	// with no flaw planted at all.
	//
	// A key becomes readable here at the moment the write to it is real from
	// this replica's point of view: acknowledged, on the primary, and applied
	// durably, on a backup. Crash rebuilds it from what the engine actually
	// kept, which is what makes the ack-before-sync flaw still observable --
	// there the acknowledgement precedes durability, so the rebuild takes the
	// value back and a later read sees the old one.
	committed map[string]string

	// syncLatency is how long the modelled fsync takes. It is a plan constant
	// rather than a draw, so the unsynced window is a real, reproducible
	// stretch of virtual time rather than an accident.
	//
	// Unexported, and settable only through Config, because that is the only
	// path ValidateWindow guards. A public field would let a caller widen or
	// narrow the window after construction and walk straight past the gate,
	// which would leave the gate technically wired and practically optional.
	syncLatency clock.Instant

	// OnUnsyncedWindow fires when this node has acknowledged-or-applied data
	// that is not yet durable, which is DR-15's `unsynced_window_open`
	// condition.
	//
	// It exists because leaving the crash to chance does not work, and the
	// arithmetic says why: the window is two milliseconds inside a five-second
	// run, so a uniformly-placed crash lands in one perhaps half a percent of
	// the time, and it then also has to be followed by a read of the same key.
	// A thousand seeds of that is a thousand near misses. The reactive rule
	// makes the crash land in the window by construction, which is the
	// difference between testing durability and hoping to.
	OnUnsyncedWindow func()

	// OnWriteAcked fires when this node has told a client a write succeeded.
	//
	// It is the ack-before-replicate window's opening edge, and it is a
	// different instant from OnUnsyncedWindow's on purpose: the two flaws need
	// the crash on opposite sides of the acknowledgement. ack-before-sync needs
	// the primary to die *before* its fsync; ack-before-replicate needs it to
	// die *after* it has answered, while a backup is still missing the write. A
	// single trigger aimed at one of those cannot reach the other, and a crash
	// that lands on the wrong side produces an in-flight operation the checker
	// correctly calls "may or may not have happened" -- no violation, and a
	// green sweep that means nothing.
	//
	// It fires on every acknowledged write rather than only on under-replicated
	// ones, so the correct toy exercises failover too. A promotion path that
	// only ever ran against a broken build would be untested in the one
	// configuration that is supposed to stay linearizable across it.
	OnWriteAcked func()
}

// SetPrimary changes who serves clients.
//
// Promotion here is operator-driven, not elected: the toy has no consensus by
// design (a toy with its own elections would make a failure ambiguous between
// the harness and the protocol), so failover arrives as a scheduled plan entry
// applied to every node at one instant. That every node is told at the same
// instant is what makes two simultaneous primaries unrepresentable rather than
// merely unlikely.
func (n *Node) SetPrimary(id sim.NodeID) { n.Primary = id }

// Config is one replica's construction parameters.
//
// A struct rather than eight positional arguments, and specifically so that
// ReplicationRTT cannot be added later as an optional extra that most callers
// forget: it is required, and New refuses without it.
type Config struct {
	ID      sim.NodeID
	Primary sim.NodeID
	Peers   []sim.NodeID

	Transport sim.Transport
	History   *sim.History
	Flaw      Flaw

	// SyncLatency is the modelled fsync duration. Zero takes
	// DefaultSyncLatency, whose argument lives at its definition.
	SyncLatency clock.Instant

	// Counters censuses that the window gate actually ran. Nil is tolerated for
	// callers outside a plan-built run; the lane asserts the run path supplies
	// it, which is the path that matters.
	Counters *sim.Counters

	// ReplicationRTT is the round trip the window is validated against, and it
	// has **no default on purpose**.
	//
	// A default here would be a number invented to satisfy the gate rather than
	// measured from the network the run actually has, and the gate would then
	// pass by construction on every plan — which is precisely the failure it
	// exists to prevent. Callers take it from the plan they are about to run
	// (plan.Plan.ReplicationRTT). Zero is refused, in the same discipline as
	// clock.Hold's unset realization and the plan's zero wall epoch.
	ReplicationRTT clock.Instant
}

// New builds a replica, or refuses.
//
// The refusal is the point. Before this, ValidateWindow existed and was induced
// in its own test while no production path called it, so a future cycle could
// have lowered the modelled fsync for speed and returned the entire
// ack-before-durable class to unreachable with every seed passing and every lane
// green. A gate nothing passes through is decoration.
func New(cfg Config) (*Node, error) {
	if cfg.Transport == nil {
		return nil, fmt.Errorf("toy: node %d has no transport", cfg.ID)
	}
	if cfg.History == nil {
		return nil, fmt.Errorf("toy: node %d has no history to record into", cfg.ID)
	}
	if cfg.Flaw >= numFlaws {
		return nil, fmt.Errorf("toy: node %d names unknown flaw %d", cfg.ID, cfg.Flaw)
	}
	if cfg.ReplicationRTT <= 0 {
		return nil, fmt.Errorf(
			"toy: node %d was built without a replication round trip; the window gate has nothing to "+
				"validate against, and a gate that passes by default is the failure it exists to prevent",
			cfg.ID)
	}

	sync := cfg.SyncLatency
	if sync == 0 {
		sync = DefaultSyncLatency
	}
	if err := ValidateWindow(sync, cfg.ReplicationRTT); err != nil {
		return nil, err
	}
	cfg.Counters.Asserted("toy.ValidateWindow")

	return &Node{
		ID: cfg.ID, Primary: cfg.Primary, Peers: cfg.Peers, Flaw: cfg.Flaw,
		db: model.New(), tr: cfg.Transport, hist: cfg.History,
		applied:     make(map[int]uint64),
		inflight:    make(map[engine.SeqNum]*pending),
		committed:   make(map[string]string),
		syncLatency: sync,
	}, nil
}

// IsPrimary reports whether this node serves clients.
func (n *Node) IsPrimary() bool { return n.ID == n.Primary }

// Handle is the loop's single entry point.
func (n *Node) Handle(ev sim.Event, s sim.Scheduler) {
	switch ev.Kind {
	case sim.KindClient:
		n.onClient(ev, s)
	case sim.KindDeliver:
		n.onDeliver(ev, s)
	case sim.KindDurable:
		n.onDurable(ev, s)
	case sim.KindCrash:
		// A process death takes its memory and its unsynced writes with it.
		n.Crash()
	case sim.KindTick, sim.KindRestart, sim.KindAction:
		// A tick drives nothing here: the toy has no timers, which is part of
		// being a calibration target rather than a system.
	}
}

func (n *Node) onClient(ev sim.Event, s sim.Scheduler) {
	req, ok := ev.Payload.(Request)
	if !ok || !n.IsPrimary() {
		return
	}

	if req.Op == "get" {
		// From the committed view, never from the engine's visible state: the
		// difference is a write this replica is still holding speculatively,
		// and letting a client see one is a read of something that can still
		// un-happen. FlawDirtyRead is that bug, kept reachable.
		if n.Flaw == FlawDirtyRead {
			val, err := n.db.Get([]byte(req.Key))
			if err != nil {
				n.hist.End(req.HistIdx, ev.At, sim.RespOK, "")
				return
			}
			n.hist.End(req.HistIdx, ev.At, sim.RespOK, string(val))
			return
		}
		n.hist.End(req.HistIdx, ev.At, sim.RespOK, n.committed[req.Key])
		return
	}

	// Dedupe by (client, seq). A retried request must apply at most once, and
	// the flaw that skips this is M3.
	if n.Flaw != FlawDupApply && n.applied[req.Client] >= req.Seq {
		n.hist.End(req.HistIdx, ev.At, sim.RespOK, "")
		return
	}
	n.applied[req.Client] = req.Seq

	b := engine.NewBatch().Set([]byte(req.Key), []byte(req.Value))
	seq, err := n.db.Apply(b, true)
	if err != nil {
		n.hist.End(req.HistIdx, ev.At, sim.RespError, "")
		return
	}

	p := &pending{req: req, seq: seq, acked: make(map[sim.NodeID]bool)}
	n.inflight[seq] = p

	// The modelled fsync completes later, which is what creates the window in
	// which acknowledged-but-unsynced data can exist.
	s.At(ev.At+n.syncLatency, sim.KindDurable, n.ID, seq)
	if n.OnUnsyncedWindow != nil {
		n.OnUnsyncedWindow()
	}

	for _, peer := range n.Peers {
		n.tr.Send(sim.Envelope{
			From: n.ID, To: peer, Kind: kindReplicate,
			Body: encodeReplicate(seq, req.Key, req.Value),
		})
	}

	n.maybeAnswer(p, ev.At)
}

func (n *Node) onDeliver(ev sim.Event, s sim.Scheduler) {
	frame, ok := ev.Payload.([]byte)
	if !ok {
		return
	}
	env, err := sim.Decode(frame)
	if err != nil {
		return
	}

	switch env.Kind {
	case kindReplicate:
		seq, key, val, ok := decodeReplicate(env.Body)
		if !ok {
			return
		}
		if _, err := n.db.Apply(engine.NewBatch().Set([]byte(key), []byte(val)), true); err != nil {
			return
		}
		// A backup syncs too; nothing waits on it, but the write is not
		// durable there until it does. Once it has, the value is real here and
		// a promotion may serve it.
		n.db.AdvanceDurable(n.db.VisibleSeq())
		n.committed[key] = val
		n.tr.Send(sim.Envelope{From: n.ID, To: env.From, Kind: kindAck, Body: encodeAck(engine.SeqNum(seq))})

	case kindAck:
		seq, ok := decodeAck(env.Body)
		if !ok {
			return
		}
		if p := n.inflight[engine.SeqNum(seq)]; p != nil {
			p.acked[env.From] = true
			p.acks++
			n.maybeAnswer(p, ev.At)
		}
	}
}

func (n *Node) onDurable(ev sim.Event, s sim.Scheduler) {
	seq, ok := ev.Payload.(engine.SeqNum)
	if !ok {
		return
	}

	// A durability completion belonging to a process that has since died is not
	// this process's news.
	//
	// The fsync was scheduled before a crash and lands after the restart; the
	// write it was completing was discarded with the rest of the unsynced state,
	// so the sequence it names no longer exists here. Acting on it advances the
	// durability watermark past everything applied, which the engine asserts
	// against -- and rightly, since a watermark ahead of the data is the exact
	// corruption that assertion exists to catch.
	//
	// This was almost unreachable until the generator stopped scheduling
	// restarts past the end of the run: the node had to come back before its own
	// fsync completed, and 19% of restarts never fired at all.
	if seq > n.db.VisibleSeq() {
		return
	}
	n.db.AdvanceDurable(seq)
	if p := n.inflight[seq]; p != nil {
		p.durable = true
		n.maybeAnswer(p, ev.At)
	}
}

// maybeAnswer acknowledges a put once it is durable AND every backup has it.
//
// The two ack-early flaws drop one conjunct each, deliberately kept separate:
// they are different bugs with different observable consequences, and a toy
// where they shared a code path would report one detection profile for two
// mutants and tell us nothing about either.
func (n *Node) maybeAnswer(p *pending, at clock.Instant) {
	if p.answered {
		return
	}
	needDurable := n.Flaw != FlawAckBeforeSync
	needAcks := n.Flaw != FlawAckBeforeReplicate

	if needDurable && !p.durable {
		return
	}
	if needAcks && n.ackedPeers(p) < len(n.Peers) {
		return
	}
	n.answer(p, at)
}

// ackedPeers is how many distinct backups have the write, or -- under
// FlawAckCounting -- how many acknowledgements arrived, which is not the same
// number the moment the transport duplicates one.
func (n *Node) ackedPeers(p *pending) int {
	if n.Flaw == FlawAckCounting {
		return p.acks
	}
	return len(p.acked)
}

func (n *Node) answer(p *pending, at clock.Instant) {
	if p.answered {
		return
	}
	p.answered = true
	n.committed[p.req.Key] = p.req.Value
	n.hist.End(p.req.HistIdx, at, sim.RespOK, "")
	delete(n.inflight, p.seq)
	if n.OnWriteAcked != nil {
		n.OnWriteAcked()
	}
}

// Crash discards everything the engine had not made durable, and forgets what
// was in flight -- a process death takes its memory with it.
func (n *Node) Crash() {
	n.db.Crash()
	n.inflight = make(map[engine.SeqNum]*pending)

	// The committed view is memory, so it is rebuilt from what the engine kept.
	// This is where an acknowledgement that ran ahead of durability is paid for:
	// the value was readable a moment ago and is not now.
	//
	// Sorted keys rather than a map range -- the determinism rule -- so a crash
	// rebuilds identically on every run.
	for _, k := range sorted.Keys(n.committed) {
		v, err := n.db.Get([]byte(k))
		if err != nil {
			delete(n.committed, k)
			continue
		}
		n.committed[k] = string(v)
	}
}

// Wire encoding. Fixed-width and explicit, for the same reason the envelope
// codec is: an encoding discovered at run time is an encoding that can differ
// between runs.

func encodeReplicate(seq engine.SeqNum, key, val string) []byte {
	b := make([]byte, 0, 8+2+len(key)+2+len(val))
	b = appendU64(b, uint64(seq))
	b = appendStr(b, key)
	b = appendStr(b, val)
	return b
}

func decodeReplicate(b []byte) (uint64, string, string, bool) {
	seq, b, ok := takeU64(b)
	if !ok {
		return 0, "", "", false
	}
	key, b, ok := takeStr(b)
	if !ok {
		return 0, "", "", false
	}
	val, _, ok := takeStr(b)
	if !ok {
		return 0, "", "", false
	}
	return seq, key, val, true
}

func encodeAck(seq engine.SeqNum) []byte { return appendU64(nil, uint64(seq)) }

func decodeAck(b []byte) (uint64, bool) {
	seq, _, ok := takeU64(b)
	return seq, ok
}

func appendU64(b []byte, v uint64) []byte {
	return append(b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func takeU64(b []byte) (uint64, []byte, bool) {
	if len(b) < 8 {
		return 0, nil, false
	}
	var v uint64
	for i := range 8 {
		v = v<<8 | uint64(b[i])
	}
	return v, b[8:], true
}

func appendStr(b []byte, s string) []byte {
	b = append(b, byte(len(s)>>8), byte(len(s)))
	return append(b, s...)
}

func takeStr(b []byte) (string, []byte, bool) {
	if len(b) < 2 {
		return "", nil, false
	}
	n := int(b[0])<<8 | int(b[1])
	if len(b) < 2+n {
		return "", nil, false
	}
	return string(b[2 : 2+n]), b[2+n:], true
}
