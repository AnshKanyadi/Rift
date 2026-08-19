package store

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/internal/sorted"
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
	db    *model.DB
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
func New(cfg Config) (*Node, error) {
	if cfg.Transport == nil || cfg.Ledger == nil {
		return nil, fmt.Errorf("store: node %d needs a transport and a ledger", cfg.ID)
	}
	if cfg.SyncLatency <= 0 {
		return nil, fmt.Errorf("store: node %d has no modelled fsync duration", cfg.ID)
	}
	m := &Node{
		cfg: cfg, db: model.New(), epoch: sim.NewEpochGuard(),
		nextRange: RangeID(2 + cfg.Ordinal),
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
	cfg.Ledger.RecordRangeBase(uint64(FirstRange), provenance.Witness(encodeMachine(first, map[string]string{})))
	return m, nil
}

func (m *Node) newReplicaFor(d RangeDescriptor) (*Replica, error) {
	r, err := newReplica(m.cfg)
	if err != nil {
		return nil, err
	}
	r.rng, r.desc = d.ID, d.Clone()
	r.db, r.epoch, r.machine = m.db, m.epoch, m
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
		id, body, ok := takeRange(env.Body)
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
	r.onClient(req)
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
	for _, r := range m.replicas {
		r.restart()
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
func putRange(id RangeID, body []byte) []byte {
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint64(b, uint64(id))
	return append(b, body...)
}

func takeRange(b []byte) (RangeID, []byte, bool) {
	if len(b) < 8 {
		return 0, nil, false
	}
	return RangeID(binary.BigEndian.Uint64(b)), b[8:], true
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
	rightKV := map[string]string{}
	for _, k := range sorted.Keys(left.kv) {
		switch {
		case spec.Right.Contains([]byte(k)):
			rightKV[k] = left.kv[k]
			delete(left.kv, k)
		case spec.Left.Contains([]byte(k)):
		default:
			panic(fmt.Sprintf(
				"store: node %d splitting range %d holds key %q, which neither %s nor %s covers",
				m.cfg.ID, left.rng, k, spec.Left, spec.Right))
		}
	}
	left.desc = spec.Left.Clone()

	if existing := m.replicaOf(spec.Right.ID); existing != nil {
		return
	}

	r, err := m.newReplicaFor(spec.Right)
	if err != nil {
		panic(fmt.Sprintf("store: node %d cannot create range %d: %v", m.cfg.ID, spec.Right.ID, err))
	}
	r.kv = rightKV

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
	data := encodeMachine(spec.Right, rightKV)

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

	m.cfg.Ledger.RecordRangeBase(uint64(spec.Right.ID), provenance.Witness(data))
	r.adoptSnapshot(snapMeta, rightKV)
	m.addReplica(r)
	m.splits++
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
