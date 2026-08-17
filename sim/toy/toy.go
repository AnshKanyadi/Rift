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

// DefaultSyncLatency is the modelled fsync duration, and it is load-bearing.
//
// # Why 50ms, and why lowering it silently breaks the harness
//
// The ack-before-durable flaw class only *exists* when fsync is slower than a
// replication round trip. Below that, a primary that waits for backup
// acknowledgements is already durable by the time it answers, so the flawed
// implementation and the correct one are behaviourally identical -- there is no
// incorrect behaviour in existence for any oracle to find, and no amount of
// crash targeting helps. The measured curve:
//
//	fsync window   2ms:   0 of 1000 seeds detect ack-before-sync
//	fsync window  10ms:   2 of 1000, first at seed 663
//	fsync window  50ms:  82 of 1000, first at seed 29
//
// A future cycle lowering this for speed would return the entire class to
// unreachable while all thousand seeds pass and every lane stays green -- which
// is structurally the same failure as the crash injector that marked a node
// down without telling it: a clean sweep over an empty search space.
//
// So the number is named, the argument lives here rather than in a design doc,
// and ValidateWindow refuses to run in a regime where the harness's own planted
// flaws cannot exist.
const DefaultSyncLatency = clock.Instant(50_000_000)

// MinWindowMargin is how far the fsync window must exceed the replication round
// trip before the ack-before-durable class is reliably reachable. Three times,
// which is not a derived constant but a stated one: at parity the flaw is
// unreachable, and the measured curve shows detection still marginal at 5x the
// 2ms floor.
const MinWindowMargin = 3

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
	if int64(syncLatency) < int64(replicationRTT)*MinWindowMargin {
		return fmt.Errorf(
			"toy: modelled fsync of %dus does not exceed the replication round trip of %dus by the required %dx; "+
				"in this regime a primary awaiting backup acks is already durable when it answers, so ack-before-durable "+
				"cannot manifest at all and a clean sweep would be a sweep over an empty search space: %w",
			int64(syncLatency)/1000, int64(replicationRTT)/1000, MinWindowMargin, ErrWindowTooNarrow)
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
