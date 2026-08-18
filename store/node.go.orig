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

	// pending is every write handed to the engine that the engine has not yet
	// reported durable, in issue order.
	pending []pendingWrite

	// durHS and durLog are what this node has ACTUALLY made durable: folded
	// forward from pending as each write completes, dropped wholesale on a
	// crash. This, and not a read-back, is what the ledger is told.
	//
	// # Why the ledger cannot be fed by reading the engine
	//
	// engine.Engine reads return the VISIBLE state, which by construction
	// includes batches that have been applied and not yet synced -- that window
	// is the whole point of the model (DR-15). Feeding the ledger a read-back
	// therefore reports writes a crash would take as persisted, and the
	// persist-before-reply oracle compares its acks against exactly that record.
	// An inflated durability watermark does not make that oracle noisy, it makes
	// it SILENT: every ack looks covered. Measured across 10k seeds, the
	// read-back was ahead of durability 44,911 times.
	//
	// DESIGN-A1 §0 names the ledger's second stream as "every write that reached
	// the Engine, and when it became durable". That is what the driver knows,
	// and it is the driver's to report.
	durHS  raft.HardState
	durLog []raft.Entry

	// writtenLast is the highest log index the driver has written to the engine.
	// Recorded, because it is what says whether a Ready is a conflicting append
	// and the engine is still holding a discarded suffix.
	writtenLast raft.Index

	// kv is the replicated state machine: applied entries land here.
	kv map[string]string

	// propSeq numbers this node's proposals. Combined with the node id it makes
	// a ProposalID unique across the cluster.
	propSeq uint64

	// answered tracks client ops already responded to, by history index.
	inflight []clientOp

	down bool
}

// pendingWrite is one batch in flight to the engine, kept until it is durable
// so that durability can be RECORDED when it arrives rather than read back from
// an engine that cannot distinguish durable from merely visible.
type pendingWrite struct {
	seq  engine.SeqNum
	mark raft.PersistMark

	hs      *raft.HardState
	entries []raft.Entry

	// clearAbove is nonzero when this batch also cleared the log above that
	// index, which is how a conflicting append is expressed on disk.
	clearAbove raft.Index
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
			w := pendingWrite{mark: rd.Mark}
			if rd.HardState != nil {
				hs := *rd.HardState
				b.Set(keyHardState, encodeHardState(hs))
				w.hs = &hs
			}
			if k := len(rd.Entries); k > 0 {
				last := rd.Entries[k-1].Index

				// # A conflicting append must clear the discarded suffix
				//
				// A Ready whose first entry lands at or below an index already
				// written means raft truncated: the entries it is handing over
				// replace ones the engine still holds, and the engine also still
				// holds everything ABOVE the new last index, because a Set only
				// overwrites the keys it names.
				//
				// Without the clear, recovery reads back a new prefix spliced
				// onto a dead branch's tail and calls the result a log -- gapless
				// by index, so Restore accepts it, and wrong in every entry above
				// the cut. This is what DeleteRange is in the frozen interface
				// for (Amendment A3), and the batch is atomic, so the clear and
				// the rewrite land together or not at all.
				if rd.Entries[0].Index <= n.writtenLast {
					b.DeleteRange(logKey(last+1), logUpper)
					w.clearAbove = last
				}
				for _, e := range rd.Entries {
					b.Set(logKey(e.Index), encodeEntry(e))
				}
				w.entries = append(w.entries, rd.Entries...)
				n.writtenLast = last
			}
			seq, err := n.db.Apply(b, true)
			if err == nil {
				w.seq = seq
				n.pending = append(n.pending, w)
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
	kept := n.pending[:0]
	for _, w := range n.pending {
		if w.seq <= seq {
			n.fold(w)
			if w.mark > mark {
				mark = w.mark
			}
			continue
		}
		kept = append(kept, w)
	}
	n.pending = kept

	// A mark may be acknowledged only when EVERY write issued under it is
	// durable. raft freezes a mark's coverage at handover, so today that is one
	// batch per mark and this never fires -- but the acknowledgement is the
	// driver's statement, not raft's, and the driver is the only side that knows
	// what it has actually written. If a mark ever spans two batches again, this
	// keeps the statement true instead of quietly making it a guess.
	if len(n.pending) > 0 && n.pending[0].mark != 0 && n.pending[0].mark <= mark {
		mark = n.pending[0].mark - 1
	}

	n.cfg.Ledger.RecordDurable(n.cfg.Ordinal, n.durHS, n.durLog, at)
	if mark != 0 {
		n.raft.AckPersisted(mark)
	}
}

// fold applies one completed write to the durable record. It is the only place
// that record moves forward, and it moves on the engine's completion rather than
// on the driver's intention.
func (n *Node) fold(w pendingWrite) {
	if w.hs != nil {
		n.durHS = *w.hs
	}
	for _, e := range w.entries {
		if int(e.Index) <= len(n.durLog) {
			n.durLog[e.Index-1] = e
			continue
		}
		n.durLog = append(n.durLog, e)
	}
	if w.clearAbove > 0 && int(w.clearAbove) < len(n.durLog) {
		n.durLog = n.durLog[:w.clearAbove]
	}
}

// readDurable reads the engine back. It is the recovery path and only the
// recovery path: after a crash the engine has discarded everything unsynced, so
// what it returns is durable by construction.
//
// That precondition is asserted rather than assumed. An engine read is of the
// VISIBLE state; calling this while a write is in flight returns writes a crash
// would take, and the one consumer that would be misled -- the ledger the
// persist-before-reply oracle reads -- would be misled silently.
func (n *Node) readDurable() (raft.HardState, []raft.Entry) {
	if v, d := n.db.VisibleSeq(), n.db.DurableSeq(); v != d {
		panic(fmt.Sprintf(
			"store: node %d read the engine back with sequence %d visible and only %d durable; "+
				"the result would report as persisted writes that a crash would take",
			n.cfg.ID, v, d))
	}
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

	// These writes never became durable, so they never enter the durable record
	// -- and the engine has just reverted to it, so what the driver has written
	// comes back to the durable log's own end.
	n.pending = nil
	n.writtenLast = lastLogIndex(n.durLog)
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
	// # A restart is a death followed by a recovery, and the death is not optional
	//
	// A restart delivered to a node that is NOT down still ends the running
	// incarnation -- that is what the word means -- so the unsynced writes go
	// exactly as they would in a crash. The plan can schedule a restart without
	// a preceding crash, and a duplicated restart produces one too.
	//
	// Without this the node was rebuilt from whatever the engine had VISIBLE,
	// which includes batches that were applied and never synced: a process that
	// "recovered" writes no crash would have kept. It then answered for them.
	// That is the precise inverse of the fault being injected, and it is how a
	// follower acked index 15 with 5 durable on seed 92 (BUG-005).
	if !n.down {
		n.crash()
	}

	// A restart is a new incarnation too, not a continuation of the crashed
	// one. Treating it as a continuation is how a second restart handed a fresh
	// Raft an acknowledgement for a mark it never issued (BUG-002).
	n.epoch.Advance()
	n.pending = nil
	n.inflight = nil

	hs, entries := n.readDurable()

	// The driver's record of what it made durable and the engine's read-back are
	// two independent derivations of one fact, and recovery is the moment they
	// can be compared. A disagreement means one of them is wrong, and which one
	// is a question worth stopping for: everything the ledger asserts about
	// persistence rests on the first, and everything the cluster does after a
	// crash rests on the second.
	if err := sameDurableState(n.durHS, n.durLog, hs, entries); err != nil {
		panic(fmt.Sprintf("store: node %d recovered a state its own durability record disagrees with: %v", n.cfg.ID, err))
	}

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
	n.writtenLast = lastLogIndex(n.durLog)
	n.jitter()
}

// sameDurableState compares the driver's durability record with what the engine
// gave back.
func sameDurableState(recHS raft.HardState, recLog []raft.Entry, gotHS raft.HardState, gotLog []raft.Entry) error {
	if recHS != gotHS {
		return fmt.Errorf("hard state recorded %+v, engine returned %+v", recHS, gotHS)
	}
	if len(recLog) != len(gotLog) {
		return fmt.Errorf("recorded %d durable entries, engine returned %d", len(recLog), len(gotLog))
	}
	for i := range recLog {
		a, b := recLog[i], gotLog[i]
		if a.Index != b.Index || a.Term != b.Term || a.ID != b.ID || string(a.Data) != string(b.Data) {
			return fmt.Errorf("entry %d recorded as index %d term %d, engine returned index %d term %d",
				i+1, a.Index, a.Term, b.Index, b.Term)
		}
	}
	return nil
}

func lastLogIndex(es []raft.Entry) raft.Index {
	if len(es) == 0 {
		return 0
	}
	return es[len(es)-1].Index
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
	if n.down || len(n.pending) > 0 {
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
