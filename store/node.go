// Package store drives a Raft group: it owns the engine, the transport and the
// persist/apply loop, and it is the only thing in the system that touches a
// *raft.Raft.
//
// # Why the driver has no ordering obligation
//
// DR-7. raft never hands out a message whose meaning depends on state that is
// not yet durable, so this driver may send Ready.Messages the instant it has
// them. It does not have to remember an ordering rule, and it could not violate
// one if it forgot: the messages that would be unsafe are simply not in the
// Ready.
//
// What the driver does owe is the acknowledgement: it must persist, and when the
// write is durable, call AckPersisted. A driver that never acknowledges stalls
// the group, which is why every quiescent point asserts the gated queue is
// empty.
package store

import (
	"encoding/binary"
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// Keys in the engine. Raft's persistent state lives here and nowhere else, so a
// crash takes exactly what a crash should take and recovery is the real path.
var (
	keyHardState = []byte("raft/hs")
	logPrefix    = []byte("raft/e/")
	logUpper     = []byte("raft/e0") // one past '/' in byte order
)

func logKey(i raft.Index) []byte {
	k := make([]byte, len(logPrefix)+8)
	copy(k, logPrefix)
	binary.BigEndian.PutUint64(k[len(logPrefix):], uint64(i))
	return k
}

// Config is one driver's parameters.
type Config struct {
	ID        raft.NodeID
	Peers     []raft.NodeID
	Ordinal   int // index into the ledger's per-node slices
	Election  int
	Heartbeat int

	// SyncLatency is the modelled fsync duration for this node's engine.
	SyncLatency clock.Instant

	Transport sim.Transport
	Ledger    *raftcheck.Ledger
	History   *sim.History

	// ElectionJitter supplies the randomized election timeout for a term. It is
	// plan-derived rather than drawn live: a pure state machine cannot randomize
	// for itself, so the driver does it from a PRF keyed by (node, term).
	ElectionJitter func(term raft.Term) int
}

// Node is one Raft replica plus its storage and driving loop.
type Node struct {
	cfg  Config
	raft *raft.Raft
	db   *model.DB

	// epoch guards against a completion from a dead incarnation reaching this
	// live one. See sim.Epoch for the class and its three instances.
	epoch *sim.EpochGuard

	// pending maps an engine sequence to the persist mark it carries, so a
	// durability completion can be turned into the right AckPersisted.
	pendingSeq  []engine.SeqNum
	pendingMark []raft.PersistMark

	// kv is the replicated state machine: applied entries land here.
	kv map[string]string

	// propSeq numbers this node's proposals. Combined with the node id it makes
	// a ProposalID unique across the cluster.
	propSeq uint64

	// answered tracks client ops already responded to, by history index.
	inflight []clientOp

	down bool
}

type clientOp struct {
	id      raft.ProposalID
	histIdx int
	value   string
	op      string
	key     string
}

// New builds a driver with an empty engine.
func New(cfg Config) (*Node, error) {
	if cfg.Transport == nil || cfg.Ledger == nil {
		return nil, fmt.Errorf("store: node %d needs a transport and a ledger", cfg.ID)
	}
	if cfg.SyncLatency <= 0 {
		return nil, fmt.Errorf("store: node %d has no modelled fsync duration", cfg.ID)
	}
	r, err := raft.New(raft.Config{
		ID: cfg.ID, Peers: cfg.Peers,
		ElectionTimeout: cfg.Election, HeartbeatTimeout: cfg.Heartbeat,
	})
	if err != nil {
		return nil, err
	}
	n := &Node{cfg: cfg, raft: r, db: model.New(), kv: map[string]string{}, epoch: sim.NewEpochGuard()}
	n.jitter()
	return n, nil
}

func (n *Node) jitter() {
	if n.cfg.ElectionJitter != nil {
		n.raft.SetElectionTimeout(n.cfg.ElectionJitter(0))
	}
}

// Handle is the loop's single entry point.
func (n *Node) Handle(ev sim.Event, s sim.Scheduler) {
	switch ev.Kind {
	case sim.KindTick:
		if n.down {
			return
		}
		n.raft.Tick()
	case sim.KindDeliver:
		if n.down {
			return
		}
		frame, ok := ev.Payload.([]byte)
		if !ok {
			return
		}
		env, err := sim.Decode(frame)
		if err != nil {
			return
		}
		m, ok := decodeMessage(env.Body)
		if !ok {
			return
		}
		_ = n.raft.Step(m)
	case sim.KindDurable:
		if n.down {
			return
		}
		tok, ok := ev.Payload.(sim.Stamped[engine.SeqNum])
		if !ok {
			return
		}
		// A completion is stamped with the incarnation that requested it. One
		// from a dead incarnation is dropped and counted, never acted on: acting
		// on it is what advanced the durability watermark past everything
		// applied and panicked the engine.
		if !n.epoch.Accept(tok.Epoch) {
			return
		}
		n.onDurable(tok.Value, ev.At)
	case sim.KindClient:
		if n.down {
			return
		}
		n.onClient(ev)
	case sim.KindCrash:
		n.crash()
		return
	case sim.KindRestart:
		n.restart()
		return
	case sim.KindAction:
	}
	n.drain(ev.At, s)
}

// onClient proposes a command. A node that is not the leader simply does not
// answer, which the checker treats as "may or may not have happened" -- correct,
// since an unavailable replica is behaving properly.
func (n *Node) onClient(ev sim.Event) {
	req, ok := ev.Payload.(Request)
	if !ok || n.raft.Role() != raft.RoleLeader {
		return
	}
	// Reads go through the log, exactly like writes.
	//
	// Serving a read from the leader's own applied state is a **stale read**: a
	// leader that has been deposed and does not yet know it will answer happily
	// from state a newer leader has already moved past. Porcupine found this on
	// seed 4 the moment the four safety oracles were green, which is the
	// division of labour working -- the safety oracles watch the log, and the
	// linearizability checker watches what a client saw.
	//
	// The cheap fix is read index (A7): confirm leadership with a quorum, then
	// read locally. That is not A1's scope, so A1 pays the honest price and
	// replicates reads. BENCHMARKS.md will state that cost when A7 removes it.
	n.propSeq++
	id := raft.ProposalID{Node: n.cfg.ID, Seq: n.propSeq}
	if err := n.raft.Propose(id, encodeCmd(req.Op, req.Key, req.Value)); err != nil {
		return
	}
	n.inflight = append(n.inflight, clientOp{
		id: id, histIdx: req.HistIdx, key: req.Key, value: req.Value, op: req.Op,
	})
}

// drain does the driver's whole job: persist, send, apply, acknowledge.
func (n *Node) drain(at clock.Instant, s sim.Scheduler) {
	for n.raft.HasReady() {
		rd := n.raft.Ready()

		// 1. Persist. Everything goes through the engine interface, so a crash
		//    takes exactly what a crash should take.
		if rd.HardState != nil || len(rd.Entries) > 0 {
			b := engine.NewBatch()
			if rd.HardState != nil {
				b.Set(keyHardState, encodeHardState(*rd.HardState))
			}
			for _, e := range rd.Entries {
				b.Set(logKey(e.Index), encodeEntry(e))
			}
			seq, err := n.db.Apply(b, true)
			if err == nil {
				n.pendingSeq = append(n.pendingSeq, seq)
				n.pendingMark = append(n.pendingMark, rd.Mark)
				s.At(at+n.cfg.SyncLatency, sim.KindDurable, sim.NodeID(n.cfg.Ordinal),
					sim.Stamp(n.epoch.Current(), seq))
			}
		}

		// 2. Send, in any order, at any time. Safety does not depend on this
		//    happening after step 1, which is DR-7's entire point.
		for _, m := range rd.Messages {
			n.cfg.Ledger.RecordSent(n.cfg.Ordinal, m, at)
			n.cfg.Transport.Send(sim.Envelope{
				From: sim.NodeID(n.cfg.Ordinal),
				To:   sim.NodeID(n.ordinalOf(m.To)),
				Kind: 1, Body: encodeMessage(m),
			})
		}

		// 3. Apply, answering each client operation at the instant its own entry
		//    is applied so a read observes exactly the state at its log
		//    position.
		if len(rd.Committed) > 0 {
			for _, e := range rd.Committed {
				op, k, v := "", "", ""
				if len(e.Data) > 0 {
					op, k, v = decodeCmd(e.Data)
					if op == "put" {
						n.kv[k] = v
					}
				}
				n.answerAt(e, op, k, at)
			}
			n.cfg.Ledger.RecordApplied(n.cfg.Ordinal, rd.Committed, at)
			n.raft.AckApplied(rd.Committed[len(rd.Committed)-1].Index)
		}
	}
}

// answerAt completes the client operation whose entry has just been applied,
// reading the state machine at exactly that log position.
//
// Matching is on the proposal's own identifier, never on the log index. A log
// index is not a proposal identity: a later leader may place a different command
// at the same index, and answering on an index match tells the client its write
// succeeded when the slot was taken by somebody else (BUG-004).
//
// A proposal whose entry was overwritten is simply never answered, and that is
// the correct outcome rather than a gap. The client genuinely does not know
// whether its write happened, so the history leaves it in flight and the checker
// treats it as may-or-may-not-have-happened -- the honest answer.
func (n *Node) answerAt(e raft.Entry, op, key string, at clock.Instant) {
	if e.ID.Zero() {
		return
	}
	kept := n.inflight[:0]
	for _, c := range n.inflight {
		if c.id != e.ID {
			kept = append(kept, c)
			continue
		}
		val := ""
		if op == "get" {
			val = n.kv[key]
		}
		n.cfg.History.End(c.histIdx, at, sim.RespOK, val)
	}
	n.inflight = kept
}

// onDurable turns an engine sync completion into AckPersisted, and records what
// is now durable into the ledger.
func (n *Node) onDurable(seq engine.SeqNum, at clock.Instant) {
	n.db.AdvanceDurable(seq)

	var mark raft.PersistMark
	kept := n.pendingSeq[:0]
	keptMarks := n.pendingMark[:0]
	for i, s := range n.pendingSeq {
		if s <= seq {
			if n.pendingMark[i] > mark {
				mark = n.pendingMark[i]
			}
			continue
		}
		kept = append(kept, s)
		keptMarks = append(keptMarks, n.pendingMark[i])
	}
	n.pendingSeq, n.pendingMark = kept, keptMarks

	hs, entries := n.readDurable()
	n.cfg.Ledger.RecordDurable(n.cfg.Ordinal, hs, entries, at)
	if mark != 0 {
		n.raft.AckPersisted(mark)
	}
}

// readDurable reads back what the engine holds. This is the same code recovery
// uses, so the read-back path is exercised on every sync rather than only after
// a crash.
func (n *Node) readDurable() (raft.HardState, []raft.Entry) {
	var hs raft.HardState
	if v, err := n.db.Get(keyHardState); err == nil {
		hs = decodeHardState(v)
	}
	var out []raft.Entry
	it := n.db.NewIter(engine.IterOptions{Lower: logPrefix, Upper: logUpper})
	for ok := it.First(); ok; ok = it.Next() {
		if e, ok := decodeEntry(it.Value()); ok {
			out = append(out, e)
		}
	}
	_ = it.Close()
	return hs, out
}

// crash takes the process down: volatile state goes, unsynced writes go.
func (n *Node) crash() {
	// A process death ends an incarnation. Every completion already in flight
	// belongs to it and must never reach whatever comes next.
	n.epoch.Advance()
	n.db.Crash()
	n.down = true
	n.inflight = nil
	n.pendingSeq, n.pendingMark = nil, nil
}

// restart rebuilds the node from the engine. This is the real recovery path:
// every crash the harness injects exercises it.
//
// It discards the previous incarnation's durability bookkeeping, and that is
// load-bearing rather than tidiness. A sync completion names a mark issued by
// the Raft that requested it; handing it to the Raft that replaced it
// acknowledges a mark that instance never issued, which closes nothing and
// leaves every message gated on its real mark withheld forever. BUG-001.
func (n *Node) restart() {
	// A restart is a new incarnation too, not a continuation of the crashed
	// one. Treating it as a continuation is how a second restart handed a fresh
	// Raft an acknowledgement for a mark it never issued (BUG-002).
	n.epoch.Advance()
	n.pendingSeq, n.pendingMark = nil, nil
	n.inflight = nil

	hs, entries := n.readDurable()
	r, err := raft.Restore(raft.Config{
		ID: n.cfg.ID, Peers: n.cfg.Peers,
		ElectionTimeout: n.cfg.Election, HeartbeatTimeout: n.cfg.Heartbeat,
	}, hs, entries)
	if err != nil {
		// A log the engine cannot produce a gapless prefix from is a storage
		// bug, not a recoverable condition, and swallowing it would hide it.
		panic(fmt.Sprintf("store: node %d cannot recover: %v", n.cfg.ID, err))
	}
	n.raft = r
	n.kv = map[string]string{}
	n.down = false
	n.jitter()
}

// StaleEpochDrops is how many completions from a dead incarnation this node
// refused.
func (n *Node) StaleEpochDrops() int { return n.epoch.Dropped() }

// CheckEpochs refuses a run in which any cross-epoch delivery occurred.
func (n *Node) CheckEpochs() error {
	return n.epoch.Check(fmt.Sprintf("node %d", n.cfg.ID))
}

// AssertQuiescent surfaces a node that stopped with a message withheld and
// nothing outstanding that could ever release it.
//
// The distinction is between *waiting* and *waiting forever*. A run that reaches
// its deadline with a sync in flight legitimately leaves messages gated: the
// durability event was scheduled past the end of time and the clock simply ran
// out, which is not a stall. What is a stall is a queue with no pending persist
// behind it, because then no acknowledgement is ever coming.
//
// A crashed node is exempt for the same reason: its in-flight writes died with
// it, and the gated messages died with the Raft that made them.
func (n *Node) AssertQuiescent() error {
	if n.down || len(n.pendingSeq) > 0 {
		return nil
	}
	return n.raft.AssertQuiescent()
}

// Request is a client operation carried as an event payload.
type Request struct {
	Client  int
	Seq     uint64
	Op      string
	Key     string
	Value   string
	HistIdx int
}

func (n *Node) ordinalOf(id raft.NodeID) int {
	for i, p := range n.cfg.Peers {
		if p == id {
			return i
		}
	}
	return 0
}
