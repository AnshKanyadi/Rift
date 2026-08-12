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
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
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

	numFlaws
)

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
	case numFlaws:
		return "invalid"
	}
	return "unknown"
}

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
	req      Request
	seq      engine.SeqNum
	acks     int
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

	// syncLatency is how long the modelled fsync takes. It is a plan constant
	// rather than a draw, so the unsynced window is a real, reproducible
	// stretch of virtual time rather than an accident.
	//
	// Fifty milliseconds, which is wide on purpose. At two it was narrower than
	// a replication round trip, so a crash aimed at the window kept landing
	// before the client had been acknowledged at all -- producing an in-flight
	// operation the checker correctly treats as "may or may not have happened",
	// and no violation. The window has to be wide enough that an acknowledged,
	// not-yet-durable write actually exists for a while, or the bug class it
	// exposes is unreachable.
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
}

// New builds a replica.
func New(id, primary sim.NodeID, peers []sim.NodeID, tr sim.Transport, h *sim.History, flaw Flaw) *Node {
	return &Node{
		ID: id, Primary: primary, Peers: peers, Flaw: flaw,
		db: model.New(), tr: tr, hist: h,
		applied:     make(map[int]uint64),
		inflight:    make(map[engine.SeqNum]*pending),
		syncLatency: clock.Instant(50_000_000), // 50ms
	}
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
		val, err := n.db.Get([]byte(req.Key))
		if err != nil {
			n.hist.End(req.HistIdx, ev.At, sim.RespOK, "")
			return
		}
		n.hist.End(req.HistIdx, ev.At, sim.RespOK, string(val))
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

	p := &pending{req: req, seq: seq}
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
		// durable there until it does.
		n.db.AdvanceDurable(n.db.VisibleSeq())
		n.tr.Send(sim.Envelope{From: n.ID, To: env.From, Kind: kindAck, Body: encodeAck(engine.SeqNum(seq))})

	case kindAck:
		seq, ok := decodeAck(env.Body)
		if !ok {
			return
		}
		if p := n.inflight[engine.SeqNum(seq)]; p != nil {
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
	if needAcks && p.acks < len(n.Peers) {
		return
	}
	n.answer(p, at)
}

func (n *Node) answer(p *pending, at clock.Instant) {
	if p.answered {
		return
	}
	p.answered = true
	n.hist.End(p.req.HistIdx, at, sim.RespOK, "")
	delete(n.inflight, p.seq)
}

// Crash discards everything the engine had not made durable, and forgets what
// was in flight -- a process death takes its memory with it.
func (n *Node) Crash() {
	n.db.Crash()
	n.inflight = make(map[engine.SeqNum]*pending)
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
