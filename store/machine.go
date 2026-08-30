package store

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// Node is one machine. It owns the engine, the crash boundary and the mailbox,
// and it hosts a replica of every range that lives here.
//
// # Why the machine is the node and the range is not
//
// Through A3 a Node was a Raft group. A4 makes it a container, and the reason is
// the crash boundary: one engine, one process, one death. Modelling each range as
// its own node would let the simulator crash one range and not another on the
// same machine -- a schedule the real system cannot produce, which is the harness
// lying in the system's favour (DESIGN-A4 §2).
type Node struct {
	cfg   Config
	db    *tracked
	epoch *sim.EpochGuard

	// replicas is a SORTED SLICE keyed by RangeID, never a map. This package is
	// in core determinism scope and a map range here would leak into message
	// ordering, which is the one place it would be hardest to notice.
	replicas []*Replica

	// nextRange is this machine's source of new range identifiers. It is seeded
	// from the machine's ordinal and stepped by the number of machines, so two
	// machines splitting concurrently cannot mint the same id -- and a split's
	// identifiers travel in the log entry anyway, so every replica uses the one
	// the proposer chose rather than minting its own.
	nextRange RangeID

	down        bool
	staleEpochs int
	splits      int
}

// New builds a machine hosting one replica of the initial range, which covers
// the whole key space.
//
// # It REFUSES a non-empty engine (BUG-059)
//
// Opening a store over durable state without recovering is exactly what fired
// on the first real crash: `cmd/riftnode` called this on every start, so a
// restarted node built empty bookkeeping over an engine that already held a
// recovered log, and the durability cross-check objected -- correctly.
//
//	A CONSTRUCTOR THAT CAN EXPRESS THAT STATE WILL EXPRESS IT AGAIN. So it
//	cannot: New is the FRESH-CLUSTER path and says so by failing, and Open is the
//	recover path and fails on an empty engine for the mirror reason. "I meant
//	fresh" and "I meant recover" must not be the same call.
func New(cfg Config) (*Node, error) {
	m, err := newMachine(cfg)
	if err != nil {
		return nil, err
	}
	if m.engineHasState() {
		return nil, fmt.Errorf("store: node %d called New over an engine that already holds "+
			"durable state; New is the fresh-cluster path and Open is the recover path", cfg.ID)
	}
	first := FirstRangeDescriptor()
	r, err := m.newReplicaFor(first)
	if err != nil {
		return nil, err
	}
	m.replicas = []*Replica{r}

	// The first range's birth state is RECORDED, not left for the ledger to
	// assume from a missing entry. Every machine records the same constant, which
	// is the point: the model reads a fact rather than a default.
	cfg.Ledger.RecordRangeBase(uint64(FirstRange),
		provenance.Witness(encodeMachine(first, hlc.Timestamp{}, nil)),
		provenance.Witness(raft.EncodeConfiguration(initialConf(cfg))))
	return m, nil
}

// Open builds a machine over an engine that already holds durable state, and
// recovers it.
//
// This is the process-start half of what `restart` used to do alone. It runs the
// SAME Recover, because there is one definition of "read what is durable and
// rebuild bookkeeping from it" and both callers go through it.
//
// It does NOT record the first range's birth state: that constant describes a
// range being born, and this range was born in an earlier incarnation. A ledger
// told otherwise would hold a fact that is false.
func Open(cfg Config) (*Node, error) {
	m, err := newMachine(cfg)
	if err != nil {
		return nil, err
	}
	if !m.engineHasState() {
		return nil, fmt.Errorf("store: node %d called Open over an EMPTY engine; Open is the "+
			"recover path and New is the fresh-cluster path", cfg.ID)
	}
	m.Recover()
	return m, nil
}

// newMachine is the shared construction and validation, with no policy about
// whether the engine is expected to be empty.
func newMachine(cfg Config) (*Node, error) {
	if cfg.Transport == nil {
		return nil, fmt.Errorf("store: node %d needs a transport", cfg.ID)
	}
	if err := cfg.checkLedger(); err != nil {
		return nil, err
	}
	if cfg.SyncLatency <= 0 {
		return nil, fmt.Errorf("store: node %d has no modelled fsync duration", cfg.ID)
	}
	return &Node{
		cfg: cfg, db: cfg.Engine(), epoch: sim.NewEpochGuard(),
		nextRange: RangeID(2 + cfg.Ordinal),
	}, nil
}

// engineHasState reports whether this engine holds anything at all.
//
// Any key is enough: the store owns the whole keyspace it writes, so one key
// means a previous incarnation got as far as writing something down.
func (m *Node) engineHasState() bool {
	it := m.db.NewIter(engine.IterOptions{})
	defer func() { _ = it.Close() }()
	return it.First()
}

// initialConf is the membership every machine starts the first range with. It is
// the same on every machine by construction -- it comes from the configuration
// the cluster was built with, not from anything a node decided.
func initialConf(cfg Config) raft.Configuration {
	var c raft.Configuration
	for _, p := range cfg.Peers {
		if containsNode(cfg.Learners, p) {
			c.Learners = append(c.Learners, p)
			continue
		}
		c.Voters = append(c.Voters, p)
	}
	return c
}

func containsNode(ns []raft.NodeID, n raft.NodeID) bool {
	for _, x := range ns {
		if x == n {
			return true
		}
	}
	return false
}

// seedClockAtLeast raises this range's clock to at least ts.
//
// # BUG-023, and the invariant this is the enforcement of
//
// **No range's clock may sit below a timestamp present in the versions it
// holds.** A range that stamps a read below data it already has makes a
// completed write invisible, and MVCC is right to hide it: the store answered
// the question it was asked, and the question was wrong.
//
// A split-born range broke this by construction. It inherited the parent's
// versions and none of its clock, so its first Now() returned the local physical
// wall — which under skew sits well below a parent HLC that has absorbed a fast
// peer. Nothing closed the gap afterwards, because a range's clock only advances
// on messages FOR THAT RANGE and the child's first messages come from the same
// fresh clock. The window expired rather than closing.
//
// Two callers, deliberately separable, because they fail independently and each
// has its own mutant: the split path seeds from the value the ENTRY carries, and
// every path that ingests records seeds from the records themselves.
func (r *Replica) seedClockAtLeast(ts hlc.Timestamp) {
	if !ts.IsSet() {
		return
	}
	if err := r.hlc.Update(ts); err != nil {
		// Beyond the envelope. The refusal is the envelope working and is counted
		// where every other one is; the clock keeps its own value, which is still
		// the honest thing for it to report.
		r.envelopeRefusals++
	}
}

// maxVersionTimestamp is the highest timestamp any of these records carries.
//
// Derived from the records rather than carried beside them, so it is identical
// on every replica that ingests the same payload without anything having to keep
// a second field in step (BUG-023's fix must not be a thing that can drift).
func maxVersionTimestamp(ns []byte, rs []kv.Record) hlc.Timestamp {
	var hi hlc.Timestamp
	for _, rec := range rs {
		if ts, ok := kv.TimestampOf(ns, rec.Key); ok && hi.Less(ts) {
			hi = ts
		}
	}
	return hi
}

func (m *Node) newReplicaFor(d RangeDescriptor) (*Replica, error) {
	r, err := newReplica(m.cfg, m.db)
	if err != nil {
		return nil, err
	}
	r.rng, r.desc = d.ID, d.Clone()
	r.db, r.epoch, r.machine = m.db, m.epoch, m

	// The MVCC store lives inside this range's engine-key namespace, so a
	// range's versions are part of the contiguous keyspace A4 gave it: written,
	// cleared and recovered without touching another range's.
	store, err := kv.NewStore(m.db, rangePrefix(d.ID))
	if err != nil {
		return nil, err
	}
	r.mvcc = store

	// One HLC per range over the machine's ONE physical clock. Two ranges on a
	// node share a clock and not a logical counter, so a busy range cannot
	// inflate a quiet one's timestamps -- and both are bounded by the same
	// maxOffset, which is what makes the cluster-wide argument work.
	// # The node ordinal reaches the clock, and that is load-bearing
	//
	// Every HLC on this machine carries the same tag, and no other machine's
	// does. That is what makes a start timestamp unique cluster-wide, which is
	// what BUG-021 needed and did not have.
	//
	// Two ranges on one node share the tag, and that is correct: they mint into
	// separate namespaces, and the timestamps a CLIENT sees all come from
	// replicas[0] (Node.Now), so the identity a transaction is addressed by has
	// exactly one source per node.
	newSource := m.cfg.NewTimestampSource
	if newSource == nil {
		newSource = func(c clock.Clock, node uint32) (hlc.Source, error) { return hlc.New(c, node) }
	}
	h, err := newSource(m.cfg.Clock, uint32(m.cfg.Ordinal))
	if err != nil {
		return nil, err
	}
	r.hlc = h
	return r, nil
}

// replicaFor returns the replica serving key, or nil.
func (m *Node) replicaFor(key []byte) *Replica {
	for _, r := range m.replicas {
		if r.desc.Contains(key) {
			return r
		}
	}
	return nil
}

func (m *Node) replicaOf(id RangeID) *Replica {
	for _, r := range m.replicas {
		if r.rng == id {
			return r
		}
	}
	return nil
}

// addReplica inserts a replica keeping the slice sorted by RangeID.
func (m *Node) addReplica(r *Replica) {
	m.replicas = append(m.replicas, r)
	for i := len(m.replicas) - 1; i > 0 && m.replicas[i-1].rng > m.replicas[i].rng; i-- {
		m.replicas[i-1], m.replicas[i] = m.replicas[i], m.replicas[i-1]
	}
}

// Handle is the loop's single entry point for the machine.
func (m *Node) Handle(ev sim.Event, s sim.Scheduler) {
	switch ev.Kind {
	case sim.KindTick:
		if m.down {
			return
		}
		for _, r := range m.replicas {
			r.raft.Tick()
		}
	case sim.KindDeliver:
		if m.down {
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
		id, senderTS, body, ok := takeRange(env.Body)
		if !ok {
			return
		}
		msg, ok := decodeMessage(body)
		if !ok {
			return
		}
		r := m.replicaOf(id)
		if r == nil {
			// A message for a range this machine does not host. That happens
			// legitimately -- a replica removed by a rebalance, or one that has
			// not been created here yet -- and dropping it is what a real node
			// would do.
			return
		}
		// Fold the sender's timestamp in BEFORE stepping, so anything this
		// replica stamps as a consequence of the message orders after it. After
		// stepping would be a receive that can be stamped before its own send.
		if err := r.hlc.Update(senderTS); err != nil {
			// Beyond the envelope. The message is still processed: refusing to
			// step it would turn one node's bad clock into a partition, which is
			// a bigger failure than the timestamp it carried.
			r.envelopeRefusals++
		}
		m.cfg.Ledger.RecordReceived(uint64(id), m.cfg.Ordinal, provenance.Witness(msg), ev.At)
		_ = r.raft.Step(msg)
	case sim.KindDurable:
		if m.down {
			return
		}
		tok, ok := ev.Payload.(sim.Stamped[engine.SeqNum])
		if !ok {
			return
		}
		if !m.epoch.Accept(tok.Epoch) {
			return
		}
		// One engine, one sequence, many replicas: the completion is offered to
		// every replica and each folds the writes it owns at or below it.
		m.db.AdvanceDurable(tok.Value)
		for _, r := range m.replicas {
			r.onDurable(tok.Value, ev.At)
		}
	case sim.KindClient:
		if m.down {
			return
		}
		m.onClient(ev, s)
	case sim.KindCrash:
		m.crash()
		return
	case sim.KindRestart:
		m.restart()
		return
	case sim.KindAction:
	}
	m.drainAll(ev.At, s)
}

func (m *Node) drainAll(at clock.Instant, s sim.Scheduler) {
	if m.down {
		return
	}
	// Indexed rather than ranged: applying a split appends a replica, and a
	// range loop over a slice that grows underneath it is a different bug in
	// every language.
	for i := 0; i < len(m.replicas); i++ {
		m.replicas[i].drain(at, s)
	}
}

// onClient routes a request to the replica that owns its key, and refuses one
// routed under a stale descriptor.
func (m *Node) onClient(ev sim.Event, s sim.Scheduler) {
	req, ok := ev.Payload.(Request)
	if !ok {
		return
	}
	r := m.replicaFor([]byte(req.Key))
	if r == nil {
		return
	}
	// # Range epoch monotonicity, enforced where the request arrives
	//
	// The client routed from a cached descriptor. If the range has split since,
	// the cache points at a range that no longer owns this key, or at an epoch
	// behind the replica's own. Serving it would be serving under a stale
	// descriptor, which CLAUDE.md's invariant list forbids by name.
	//
	// The refusal is typed and it retries: a silent drop is indistinguishable
	// from a partition, and the client would go on asking the same wrong replica
	// forever. Re-scheduling the request with the corrected routing is the
	// router's refresh-and-retry, living where the sim can see it.
	if req.Epoch != 0 && (req.Range != r.rng || req.Epoch < r.desc.Epoch) {
		m.staleEpochs++
		next := req
		next.Range, next.Epoch = r.rng, r.desc.Epoch
		s.At(ev.At+staleRetryDelay, sim.KindClient, sim.NodeID(m.cfg.Ordinal), next)
		return
	}
	r.onClient(req, ev.At)
}

// staleRetryDelay is how long a client waits before retrying with a refreshed
// descriptor. One tick: long enough that a retry is a separate event, short
// enough that a split does not stall the workload.
const staleRetryDelay = clock.Instant(10_000_000)

func (m *Node) crash() {
	// A process death ends an incarnation, and it takes every replica on the
	// machine with it. That is the whole reason the machine is the node.
	m.epoch.Advance()
	m.db.Crash()
	m.down = true
	for _, r := range m.replicas {
		r.onCrash()
	}
}

func (m *Node) restart() {
	if !m.down {
		m.crash()
	}
	m.epoch.Advance()
	m.Recover()
}

// Recover reads what is durable and rebuilds this machine's bookkeeping from it.
//
// # It is a PROCEDURE, and `restart` is a state transition (BUG-059)
//
// These were one function. `restart` meant both "crash, then come back" and
// "read what is durable and rebuild from it", and real mode needs only the
// second: a process that died for real does not need to be told to crash, and
// telling it to would run `Crash()` on an engine where a modelled crash discards
// nothing while reporting success.
//
//	THE FIX IS NOT A skipCrash FLAG. That keeps two meanings in one function and
//	signs the seam a second time. The transition calls the procedure; the
//	procedure has one definition; both callers go through it.
//
// Legal after a modelled crash (via `restart`) and at process start (via
// `Open`), and identical in both.
func (m *Node) Recover() {
	// Recovery rebuilds the machine's ranges from what the engine holds, not
	// from what it had in memory: a range created by a split before the crash is
	// on disk with its descriptor, and a machine that recovered only the ranges
	// it started with would lose it.
	descs := m.readDescriptors()
	kept := m.replicas[:0]
	for _, d := range descs {
		r := m.replicaOf(d.ID)
		if r == nil {
			var err error
			if r, err = m.newReplicaFor(d); err != nil {
				panic(fmt.Sprintf("store: node %d cannot rebuild range %d: %v", m.cfg.ID, d.ID, err))
			}
		}
		r.desc = d
		kept = append(kept, r)
	}
	m.replicas = nil
	for _, r := range kept {
		m.addReplica(r)
	}
	m.down = false

	// Read every replica's durable state BEFORE rebuilding any of them. One
	// engine serves every replica on this machine, so a rebuild writes to the
	// engine another replica has not read yet -- and the read-back assertion
	// (BUG-005's fix) fires on those writes, correctly. See Replica.readRecovered.
	states := make([]provenance.Reported[recovered], len(m.replicas))
	for i, r := range m.replicas {
		states[i] = r.readRecovered()
	}
	for i, r := range m.replicas {
		r.restartFrom(states[i])
	}
}

// readDescriptors reads every range descriptor this machine has on disk.
func (m *Node) readDescriptors() []RangeDescriptor {
	var out []RangeDescriptor
	it := m.db.NewIter(engine.IterOptions{Lower: []byte("r/"), Upper: []byte("r0")})
	for ok := it.First(); ok; ok = it.Next() {
		k := it.Key()
		if len(k) < 15 || string(k[10:]) != "/desc" {
			continue
		}
		if d, ok := decodeDesc(it.Value()); ok {
			out = append(out, d)
		}
	}
	_ = it.Close()
	// The first range is never written down: it exists from the moment the
	// machine does, and its extent is a constant.
	if !hasRange(out, FirstRange) {
		out = append([]RangeDescriptor{FirstRangeDescriptor()}, out...)
	}
	return out
}

// SplitsProposed is how many splits this machine's leaders proposed.
func (m *Node) SplitsProposed() int { return m.sum(func(r *Replica) int { return r.SplitsProposed() }) }

// StaleSplits counts split entries skipped for naming a stale extent.
func (m *Node) StaleSplits() int { return m.sum(func(r *Replica) int { return r.StaleSplits() }) }

// RangeCount is how many ranges this machine hosts.
func (m *Node) RangeCount() int { return len(m.replicas) }

// OutOfExtentRefusals is how many committed commands this machine's replicas
// refused to apply for naming a key outside their extent.
func (m *Node) OutOfExtentRefusals() int {
	return m.sum(func(r *Replica) int { return r.OutOfExtentRefusals() })
}

// ReadsServed is how many reads this node answered off the log by read index.
// A7's NON-VACUITY count: a sweep reading zero has not exercised the read path
// at all, whatever the staleness oracles say about it.
func (m *Node) ReadsServed() int {
	return m.sum(func(r *Replica) int { return r.ReadsServed() })
}

// FollowerReads is how many reads a non-leader answered on this node.
func (m *Node) FollowerReads() int {
	return m.sum(func(r *Replica) int { return r.FollowerReads() })
}

// ReadsOutOfExtent is how many read-index reads this node's replicas declined to
// answer because their extent no longer covered the key, and rerouted instead.
// BUG-026's fix, counted separately from OutOfExtentRefusals so each mechanism
// can read zero on its own.
func (m *Node) ReadsOutOfExtent() int {
	return m.sum(func(r *Replica) int { return r.ReadsOutOfExtent() })
}

// NoOpsApplied, NoOpReachedArm and NoOpAnswered aggregate D-A7-6's two
// propositions across this node's replicas. The first is the NON-VACUITY count
// -- one term-start no-op per election, so a sweep reading zero has exercised
// none of it -- and the other two must be zero on every run. DESIGN-A7 §3a.2.
func (m *Node) NoOpsApplied() int {
	return m.sum(func(r *Replica) int { return r.NoOpsApplied() })
}

// NoOpReachedArm must read zero: a dataless entry that matched a state-machine
// arm is the first proposition failing.
func (m *Node) NoOpReachedArm() int {
	return m.sum(func(r *Replica) int { return r.NoOpReachedArm() })
}

// NoOpAnswered must read zero: a dataless, zero-identity entry that completed a
// client operation is the second proposition failing.
func (m *Node) NoOpAnswered() int {
	return m.sum(func(r *Replica) int { return r.NoOpAnswered() })
}

// A5's counters, summed over this machine's replicas.
func (m *Node) GCProposed() int { return m.sum(func(r *Replica) int { return r.GCProposed() }) }
func (m *Node) GCApplied() int  { return m.sum(func(r *Replica) int { return r.GCApplied() }) }
func (m *Node) VersionsCollected() int {
	return m.sum(func(r *Replica) int { return r.VersionsCollected() })
}
func (m *Node) MVCCReadsRefused() int { return m.sum(func(r *Replica) int { return r.ReadsRefused() }) }
func (m *Node) MVCCWritesRefused() int {
	return m.sum(func(r *Replica) int { return r.WritesRefused() })
}
func (m *Node) EnvelopeRefusals() int {
	return m.sum(func(r *Replica) int { return r.EnvelopeRefusals() })
}

// A6's per-node totals. Each is a sum over this node's replicas of a counter
// the apply path keeps, and each is asserted in the exit run or deleted.
func (m *Node) WriteConflicts() int {
	return m.sum(func(r *Replica) int { return r.WriteConflicts() })
}

func (m *Node) PrewriteBlocked() int {
	return m.sum(func(r *Replica) int { return r.PrewriteBlocked() })
}

func (m *Node) TxnRaceLost() int {
	return m.sum(func(r *Replica) int { return r.TxnRaceLost() })
}

func (m *Node) ReadMarks() int {
	return m.sum(func(r *Replica) int { return r.ReadMarks() })
}

func (m *Node) ReadConflicts() int {
	return m.sum(func(r *Replica) int { return r.ReadConflicts() })
}

func (m *Node) ResolveWaits() int {
	return m.sum(func(r *Replica) int { return r.ResolveWaits() })
}

func (m *Node) ResolveAlreadyDecided() int {
	return m.sum(func(r *Replica) int { return r.ResolveAlreadyDecided() })
}

func (m *Node) ResolveDeclaredDead() int {
	return m.sum(func(r *Replica) int { return r.ResolveDeclaredDead() })
}

func (m *Node) ForeignLocksKept() int {
	return m.sum(func(r *Replica) int { return r.mvcc.ForeignLocksKept() })
}

func (m *Node) ResolveNoLock() int {
	return m.sum(func(r *Replica) int { return r.ResolveNoLock() })
}

func (m *Node) RollForwards() int {
	return m.sum(func(r *Replica) int { return r.RollForwards() })
}

func (m *Node) RollBacks() int {
	return m.sum(func(r *Replica) int { return r.RollBacks() })
}

func (m *Node) ReadsBlocked() int {
	return m.sum(func(r *Replica) int { return r.mvcc.ReadsBlocked() })
}

// StaleEpochRefusals is how many requests this machine refused for arriving
// under a descriptor the range has moved past.
func (m *Node) StaleEpochRefusals() int { return m.staleEpochs }

// Splits is how many splits this machine applied.
func (m *Node) Splits() int { return m.splits }

// Replicas returns the hosted replicas, for the harness's counters.
func (m *Node) Replicas() []*Replica { return m.replicas }

// StaleEpochDrops is how many completions from a dead incarnation this machine
// refused.
func (m *Node) StaleEpochDrops() int { return m.epoch.Dropped() }

// AssertQuiescent surfaces any replica that stopped with something outstanding.
func (m *Node) AssertQuiescent() error {
	for _, r := range m.replicas {
		if err := r.AssertQuiescent(); err != nil {
			return fmt.Errorf("range %d: %w", r.rng, err)
		}
	}
	return nil
}

// putRange and takeRange prefix a message frame with the range it belongs to.
//
// One transport carries every group's traffic, so the frame has to say which
// group -- and it says so explicitly rather than by any property of the message,
// because a routing decision inferred from content is a routing decision that
// changes when the content does.
// # Every message carries the sender's timestamp, and that is what makes
// # causality hold across the cluster rather than within one node
//
// An HLC that only stamped local events would give each node a private order
// that never reconciles with anyone else's. The property that matters -- if a
// happens before b then ts(a) < ts(b) -- has a cross-node half, and a send/receive
// pair is the only place that half is observable. So the timestamp rides on the
// envelope and the receiver folds it in before stepping the message.
//
// It is on the STORE's framing rather than inside a raft message: raft has no
// business knowing what a timestamp is, exactly as it has no business knowing
// what a range is (D-A4-3). The two facts the store adds to an envelope -- which
// range, and when -- sit together here.
func putRange(id RangeID, at hlc.Timestamp, body []byte) []byte {
	b := make([]byte, 20, 20+len(body))
	binary.BigEndian.PutUint64(b, uint64(id))
	binary.BigEndian.PutUint64(b[8:], uint64(at.Wall))
	binary.BigEndian.PutUint32(b[16:], at.Logical)
	return append(b, body...)
}

func takeRange(b []byte) (RangeID, hlc.Timestamp, []byte, bool) {
	if len(b) < 20 {
		return 0, hlc.Timestamp{}, nil, false
	}
	at := hlc.Timestamp{
		Wall:    clock.NewWall(int64(binary.BigEndian.Uint64(b[8:]))),
		Logical: binary.BigEndian.Uint32(b[16:]),
	}
	return RangeID(binary.BigEndian.Uint64(b)), at, b[20:], true
}

var _ = raft.EntryNormal
var _ = raftcheck.SnapshotRecord{}

// --- the machine's aggregate counters -----------------------------------------
//
// Each is a sum over the replicas hosted here. They are evidence that a path
// ran, and a machine that hosts five ranges runs each path five times over.

func (m *Node) sum(f func(*Replica) int) int {
	n := 0
	for _, r := range m.replicas {
		n += f(r)
	}
	return n
}

// DurabilityCrossChecksDeclined counts completions whose precondition did not
// hold. Quoted beside DurabilityCrossChecks; either alone is not a coverage
// statement. See OPEN-I2-2.
func (m *Node) DurabilityCrossChecksDeclined() int {
	return m.sum(func(r *Replica) int { return r.DurabilityCrossChecksDeclined() })
}

// DurabilityCrossChecks counts comparisons of a durability record against the engine.
func (m *Node) DurabilityCrossChecks() int {
	return m.sum(func(r *Replica) int { return r.DurabilityCrossChecks() })
}

// SnapshotsTaken counts snapshots created here.
func (m *Node) SnapshotsTaken() int { return m.sum(func(r *Replica) int { return r.SnapshotsTaken() }) }

// SnapshotsApplied counts snapshots installed here.
func (m *Node) SnapshotsApplied() int {
	return m.sum(func(r *Replica) int { return r.SnapshotsApplied() })
}

// TransfersAsked counts leadership transfers initiated here.
func (m *Node) TransfersAsked() int { return m.sum(func(r *Replica) int { return r.TransfersAsked() }) }

// ConfProposed, ConfRefused and LagRefused count membership changes.
func (m *Node) ConfProposed() int { return m.sum(func(r *Replica) int { return r.ConfProposed() }) }

// ConfRefused counts changes the state machine declined for any reason.
func (m *Node) ConfRefused() int { return m.sum(func(r *Replica) int { return r.ConfRefused() }) }

// LagRefused counts promotions declined for a lagging learner.
func (m *Node) LagRefused() int { return m.sum(func(r *Replica) int { return r.LagRefused() }) }

// ConfRecoveries and ConfCrossChecks count configuration recovery evidence.
func (m *Node) ConfRecoveries() int { return m.sum(func(r *Replica) int { return r.ConfRecoveries() }) }

// ConfCrossChecks counts recoveries checked against a snapshot configuration.
func (m *Node) ConfCrossChecks() int {
	return m.sum(func(r *Replica) int { return r.ConfCrossChecks() })
}

// IsLeader reports whether this machine leads any range, which is the routing
// question the harness asks before issuing a range-level command.
func (m *Node) IsLeader() bool {
	for _, r := range m.replicas {
		if r.IsLeader() {
			return true
		}
	}
	return false
}

// leaderReplica returns a replica this machine leads, preferring the lowest
// range id so the choice is a function of state rather than of iteration luck.
func (m *Node) leaderReplica() *Replica {
	for _, r := range m.replicas {
		if r.IsLeader() {
			return r
		}
	}
	return nil
}

// RequestConfChange moves target one step around the membership cycle on a range
// this machine leads.
func (m *Node) RequestConfChange(target raft.NodeID) {
	if r := m.leaderReplica(); r != nil {
		r.RequestConfChange(target)
	}
}

// RequestMove advances a manual rebalance of one named range by one step.
//
// The RANGE is named by the caller rather than chosen here. A move is an
// intent, and letting the machine pick which range to move would leave the
// harness unable to say what it ordered -- so the oracle would have to take the
// answer from the system it is judging.
func (m *Node) RequestMove(rng RangeID, from, to raft.NodeID, begin bool) bool {
	r := m.replicaOf(rng)
	return r != nil && r.RequestMove(from, to, begin)
}

// Now hands out a timestamp from this machine's clock, for a client that needs
// one.
//
// # Why a client asks a node rather than reading a clock
//
// A transaction's start timestamp has to be inside the cluster's uncertainty
// envelope, and the envelope is a property of the nodes' clocks. A coordinator
// with a clock of its own would be a time source outside the bound every read
// rests on -- and every uncertainty interval computed from maxOffset would be
// describing a different cluster than the one that issued the timestamp.
//
// It reports false when this machine hosts no range, which is not a failure: the
// caller asks somebody else, exactly as it does for any other request.
func (m *Node) Now() (hlc.Timestamp, bool) {
	if m.down || len(m.replicas) == 0 {
		return hlc.Timestamp{}, false
	}
	return m.replicas[0].hlc.Now(), true
}

// NowAbove mints a timestamp strictly greater than lb, from this node.
//
// # Why a restart may not simply use the value it was told to restart above
//
// `RestartAt` is `observedCommit.Next()` -- Logical plus one -- so it carries
// the tag of whichever node minted the commit that caused the restart. Using it
// as the restarted transaction's START timestamp hands that transaction another
// node's identity, and BUG-021 returns one level out: two transactions with one
// start timestamp, sharing a key's lock and its version.
//
// So it is MINTED. Update folds the lower bound into this node's clock, which is
// what Update is for, and Now then issues the next value above it carrying this
// node's own tag. The refusal path is honoured: a lower bound beyond the
// envelope is not absorbed, and the caller gets this node's clock instead --
// which is still above the bound it needs, because a bound that far ahead was
// never this cluster's to accept.
func (m *Node) NowAbove(lb hlc.Timestamp) (hlc.Timestamp, bool) {
	if m.down || len(m.replicas) == 0 {
		return hlc.Timestamp{}, false
	}
	h := m.replicas[0].hlc
	if lb.IsSet() {
		if err := h.Update(lb); err != nil {
			m.replicas[0].envelopeRefusals++
		}
	}
	return h.Now(), true
}

// RangeClocks reports each range this node hosts and the clock it is at.
//
// For the invariant that no range's clock sits below a version it holds
// (BUG-023). A Reported value, and safe as one: it is compared against versions
// recovered independently from the log, so a wrong value can only make the check
// FAIL.
func (m *Node) RangeClocks() map[uint64]hlc.Timestamp {
	out := make(map[uint64]hlc.Timestamp, len(m.replicas))
	for _, r := range m.replicas {
		out[uint64(r.rng)] = r.hlc.Now()
	}
	return out
}

// RequestTransfer hands leadership of a led range to target.
func (m *Node) RequestTransfer(target raft.NodeID) bool {
	r := m.leaderReplica()
	return r != nil && r.RequestTransfer(target)
}

// applySplit performs a split that a replica has just applied from its log.
//
// # Why this is safe across a crash, and what it rests on
//
// The split entry is in the left range's log and every replica applies it at the
// same index, so there is no "between": either a replica has applied it or it
// has not (DESIGN-A4 §4). The effects -- the left's new descriptor, the right's
// descriptor and initial state -- are written in ONE atomic batch.
//
// The ordering that makes it safe is the engine's: the log entry was written
// before the apply, so it has a lower sequence, and durability advances in
// sequence order. The effect therefore cannot be durable while the entry that
// justifies it is not, which is the case that would leave two ranges both
// claiming the same keys. That is the same FIFO property the persist marks
// already rest on, named here because this is the second thing standing on it.
//
// Idempotent by necessity, not by taste. appliedIdx is deliberately not
// persisted -- A1's rule that a node must not recover claiming an entry was
// committed on the word of its own memory -- so recovery WILL re-apply this.
func (m *Node) applySplit(left *Replica, spec SplitSpec, index raft.Index, at clock.Instant, s sim.Scheduler) {
	// # A split entry names the extent it was computed against, and it has to match
	//
	// Two leaders can each propose a split from the same descriptor: the first
	// proposes, loses leadership before its entry commits, and the second
	// proposes from the extent it can still see. Both entries can end up
	// committed, at different indices -- and applying the second would move the
	// left range's end BACK past the first split, so two ranges would claim the
	// same keys and their replicas would disagree about which entry is at index
	// one.
	//
	// That is what the epoch is for. A split is applied only if it was computed
	// against exactly this descriptor: same start, same end, and an epoch one
	// step ahead. Anything else is a spec the range has moved past, and it is
	// skipped rather than applied -- the range simply does not split at that
	// entry, which the next threshold crossing will propose again.
	//
	// Found by state machine safety on the first A4 sweep: two nodes applying
	// different commands at index 1 of a range born from a split.
	if spec.Left.Epoch != left.desc.Epoch+1 ||
		!bytes.Equal(spec.Left.Start, left.desc.Start) ||
		!bytes.Equal(spec.Right.End, left.desc.End) {
		left.staleSplits++
		return
	}
	// # The left half happens every time; only the right half is idempotent
	//
	// The first version skipped BOTH when the right range already existed. That
	// is right for creating the range -- re-deriving its state would destroy
	// everything it has done since -- and wrong for the left, whose keys are
	// still sitting where they were.
	//
	// A node that recovers from a snapshot taken before the split and re-applies
	// the split from its log finds the right range already on disk, takes the
	// early return, and keeps serving keys it does not own. Its state machine
	// then diverges from every replica that applied the split once. Snapshot
	// equivalence caught it: two nodes recording different digests for the same
	// index of the same range, on 178 of 300 seeds.
	//
	// So the left's keys move and its descriptor advances unconditionally, and
	// only the creation of the right-hand replica is skipped.
	// The two halves PARTITION the extent, so every key the left holds lands in
	// exactly one of them. A key in neither means the range was holding a key it
	// did not own, which is BUG-014's shape: it is asserted rather than tolerated,
	// because tolerating it is precisely how that bug survived a whole phase.
	// A split moves VERSIONS, not values: the right range inherits the whole
	// history of every key it takes, because a read at an old timestamp against
	// the new range must answer what it would have answered against the old one.
	// Moving only the newest version would make a split silently truncate
	// history, and the read that noticed would be one at a timestamp before the
	// split -- which is exactly the read a snapshot-isolated transaction makes.
	//
	// A6: it moves every RECORD, not only the data versions. A lock, a commit
	// record and a transaction record all belong to the key they name, and a
	// split that carried the values without them would hand the right range
	// values nobody had committed and strand the locks on the left -- where the
	// key they lock no longer lives, so nothing would ever resolve them.
	var leftKept, rightVs []kv.Record
	for _, rec := range left.versions() {
		userKey, ok := left.mvcc.UserKeyOf(rec.Key)
		if !ok {
			continue
		}
		switch {
		case spec.Right.Contains(userKey):
			rightVs = append(rightVs, reNamespace(rec, left.mvcc.Namespace(), rangePrefix(spec.Right.ID)))
		case spec.Left.Contains(userKey):
			leftKept = append(leftKept, rec)
		default:
			panic(fmt.Sprintf(
				"store: node %d splitting range %d holds key %q, which neither %s nor %s covers",
				m.cfg.ID, left.rng, userKey, spec.Left, spec.Right))
		}
	}
	// The mark travels with the versions. A right range born with a zero mark
	// would answer reads its parent had already collected the history for.
	mark := left.mvcc.GCMark()
	left.ingest(leftKept, mark)
	left.desc = spec.Left.Clone()

	if existing := m.replicaOf(spec.Right.ID); existing != nil {
		return
	}

	r, err := m.newReplicaFor(spec.Right)
	if err != nil {
		panic(fmt.Sprintf("store: node %d cannot create range %d: %v", m.cfg.ID, spec.Right.ID, err))
	}
	r.ingest(rightVs, mark)

	// The right range starts from a snapshot at index zero carrying its derived
	// state and the configuration it inherits. A range born with no log and no
	// snapshot would recover into an empty state machine and have nothing to
	// rebuild from.
	// # The inherited configuration is taken AT the split's index
	//
	// `Configuration()` is the ACTIVE configuration, effective on append. It is
	// not a function of the applied prefix, so two replicas applying this same
	// entry with different appended tails would give the new range two different
	// birth configurations -- and the one that started behind refuses the next
	// membership entry as an illegal transition (BUG-015). Everything a split
	// derives has to be derived at the split's position, which is the sentence
	// this whole phase keeps rediscovering.
	conf, confErr := left.raft.ConfigurationAt(index)
	if confErr != nil {
		panic(fmt.Sprintf("store: node %d cannot state range %d's configuration at the split it is applying: %v",
			m.cfg.ID, left.rng, confErr))
	}
	snapMeta := raft.SnapshotMeta{Index: 0, Term: 0, Conf: conf}
	data := encodeMachine(spec.Right, mark, rightVs)

	// Only the RIGHT range's descriptor is written, and it is a discovery
	// record: it tells a restarting machine that this range exists here. The
	// left's is deliberately not written, because a descriptor on disk is not
	// aligned with any index and the left's extent has to be (see the switch in
	// Replica.restart, and BUG-011).
	b := engine.NewBatch()
	b.Set(keyDescOf(spec.Right.ID), encodeDesc(spec.Right))
	b.Set(keySnapshotOf(spec.Right.ID), encodeSnapshot(snapMeta, data))
	seq, err := m.db.Apply(b, true)
	if err == nil {
		r.durSnap = snapMeta
		s.At(at+m.cfg.SyncLatency, sim.KindDurable, sim.NodeID(m.cfg.Ordinal),
			sim.Stamp(m.epoch.Current(), seq))
	}

	m.cfg.Ledger.RecordRangeBase(uint64(spec.Right.ID),
		provenance.Witness(data), provenance.Witness(raft.EncodeConfiguration(conf)))
	r.adoptSnapshot(snapMeta, rightVs, mark)

	// # BUG-023: the child inherits the parent's CLOCK, not only its versions
	//
	// One floor, not two. This started as two — the value the split entry
	// carried, and the maximum among the records — and `M69` proved the first
	// redundant: the invariant is *no range's clock below a version it HOLDS*, so
	// a child with no records has nothing to hide and a child with records gets
	// its floor from them. `spec.ClockAt` was deleted with its mutant (§25).
	//
	// The floor lives in `ingest` rather than here, because that is the path
	// every arriving record takes — a split's birth state, an installed snapshot,
	// a restart's recovery — so a path added later cannot bypass it.

	m.addReplica(r)
	m.splits++
}

// reNamespace rewrites a record's engine key from one range's namespace into
// another's.
//
// A record's engine key embeds the namespace, so a record moving between ranges
// has to be re-addressed. Doing it here rather than storing namespace-relative
// keys is deliberate: the engine key is what the store actually writes, and a
// record that carried a relative key would be one decode away from landing in
// the wrong range every time somebody forgot.
func reNamespace(r kv.Record, from, to []byte) kv.Record {
	if len(r.Key) < len(from) {
		return r
	}
	out := make([]byte, 0, len(to)+len(r.Key)-len(from))
	out = append(out, to...)
	out = append(out, r.Key[len(from):]...)
	return kv.Record{Key: out, Value: r.Value}
}

// nextRangeID mints an identifier for a new range.
//
// Stepped by the number of machines from a per-machine seed, so two machines
// proposing splits at the same moment cannot mint the same number. It only
// matters for the PROPOSER: the identifier travels in the split entry, and every
// other replica uses the one the proposer chose rather than minting its own.
func (m *Node) nextRangeID() RangeID {
	id := m.nextRange
	m.nextRange += RangeID(m.cfg.Nodes)
	return id
}
