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
	"errors"
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// Keys in the engine. Raft's persistent state lives here and nowhere else, so a
// crash takes exactly what a crash should take and recovery is the real path.
// Keys are namespaced per range, so one group's state is a contiguous keyspace
// that can be written, cleared and recovered without touching another's. That is
// also what earns DeleteRange its place a second time (Amendment A3): a range
// that splits or moves clears exactly its own span.
func rangePrefix(id RangeID) []byte {
	k := make([]byte, 0, 10)
	k = append(k, 'r', '/')
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(id))
	return append(k, b[:]...)
}

func keyHardStateOf(id RangeID) []byte { return append(rangePrefix(id), []byte("/hs")...) }
func keySnapshotOf(id RangeID) []byte  { return append(rangePrefix(id), []byte("/snap")...) }
func keyDescOf(id RangeID) []byte      { return append(rangePrefix(id), []byte("/desc")...) }
func logPrefixOf(id RangeID) []byte    { return append(rangePrefix(id), []byte("/e/")...) }
func logUpperOf(id RangeID) []byte     { return append(rangePrefix(id), []byte("/e0")...) }

func logKeyOf(id RangeID, i raft.Index) []byte {
	k := logPrefixOf(id)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(i))
	return append(k, b[:]...)
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

	// Clock is this machine's physical clock. It is per MACHINE, not per range:
	// one node has one oscillator, and modelling each range with its own would
	// let the simulator produce skew between two replicas that share a process,
	// which is the harness lying in the system's favour.
	Clock clock.Clock

	// NewTimestampSource builds a range's timestamp source. Nil means the HLC.
	//
	// # This is the A6 escape hatch, and it is a seam rather than a promise
	//
	// CLAUDE.md Amendment A6 pre-authorizes a TSO fallback "if A6's uncertainty
	// machinery is not green by Dec 1". A fallback nobody has ever exercised is a
	// plan, not an option: the first person to try it finds the three places that
	// reached past the interface for something only an HLC has.
	//
	// So the constructor is injectable and TestATimestampSourceCanBeSwapped
	// drives the store on a source that is not an HLC. Same distinction this
	// project keeps making, between a mechanism declared and one invoked.
	NewTimestampSource func(clock.Clock) (hlc.Source, error)
	Ledger             *raftcheck.Ledger
	History            *sim.History

	// Learners are the peers that start as learners rather than voters.
	Learners []raft.NodeID

	// PromotionLag bounds how far behind a learner may be and still be promoted.
	PromotionLag raft.Index

	// Nodes is how many machines the cluster has, which a machine needs only to
	// mint range identifiers that cannot collide with another machine's.
	Nodes int

	// SplitThreshold is how many keys a range may hold before its leader
	// proposes a split. Zero disables splitting.
	SplitThreshold int

	// GCRetention is how far behind its own clock a leader proposes to collect.
	// Zero disables collection.
	//
	// It is a DURATION rather than a version count, because the thing a read
	// names is a timestamp: "keep the last N versions" cannot answer whether a
	// read at T is answerable, and answering that is the mark's whole job.
	GCRetention time.Duration

	// PreVote turns on the extra election round. It is a build parameter rather
	// than a plan entry: the ablation runs the same schedules with it on and off,
	// so it must not perturb the schedule itself.
	PreVote bool

	// SnapshotThreshold is how many applied entries past the last snapshot
	// this node tolerates before taking a new one. Zero disables snapshotting,
	// which is what A1's corpus bundles replay against.
	SnapshotThreshold raft.Index

	// ElectionJitter supplies the randomized election timeout for a term. It is
	// plan-derived rather than drawn live: a pure state machine cannot randomize
	// for itself, so the driver does it from a PRF keyed by (node, term).
	ElectionJitter func(term raft.Term) int
}

// RangeID names a range. Ranges are numbered as they are created, and the number
// never means a position: a range's identity outlives every split it takes part
// in, which is the same discipline as a proposal not being its log index
// (BUG-004).
type RangeID uint64

// Replica is one Raft group on this machine: its state machine, its durability
// record and its driving loop. It is what store.Node was through A3.
type Replica struct {
	// rng is the range this replica serves, and desc its descriptor. The
	// descriptor changes on a split -- its end moves and its epoch rises -- so it
	// is a value the replica owns rather than a constant it was built with.
	rng  RangeID
	desc RangeDescriptor

	// machine is the node hosting this replica. A replica reaches it for the
	// things that belong to the machine rather than to the range: the engine,
	// the crash boundary, and the sibling it creates when it splits.
	machine *Node

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

	// crossChecks counts how often the driver's durability record was compared
	// against the engine's own account of what it holds. Reported so a lane can
	// ask whether the check ran at all: a comparison that never happens is the
	// family this repository has now found seven of.
	crossChecks int

	// writtenLast is the highest log index the driver has written to the engine.
	// Recorded, because it is what says whether a Ready is a conflicting append
	// and the engine is still holding a discarded suffix.
	writtenLast raft.Index

	// mvcc is the replicated state machine: applied entries land here as
	// versions keyed by the timestamp their entry carried.
	mvcc *kv.Store

	// hlc is this replica's timestamp source. It is per RANGE rather than per
	// machine, which is worth stating: two ranges on one node share a physical
	// clock but not a logical counter, so a busy range cannot inflate a quiet
	// one's timestamps. What ties them together is that both read the same
	// clock.Wall, which is what maxOffset bounds.
	hlc hlc.Source

	// durSnap is the snapshot this node has durable, and snapPending is one
	// handed to the engine and not yet acknowledged. durLog holds only the
	// entries ABOVE durSnap.Index, exactly as the engine does.
	durSnap raft.SnapshotMeta

	// snapshots and installs count what this node did, so a lane can ask whether
	// the paths ran at all rather than trusting that they did.
	snapshotsTaken   int
	snapshotsApplied int
	transfersAsked   int
	confProposed     int
	confRefused      int
	lagRefused       int

	// confRecoveries counts restarts whose recovered log carried a configuration
	// change, and confCrossChecks counts those where the snapshot carried a
	// configuration to check the recovery against. Both are evidence that the
	// paths ran: a phase whose configuration never survived a crash proved
	// nothing about surviving one.
	confRecoveries  int
	confCrossChecks int

	// confErr latches the first time this node's cached configuration disagreed
	// with its own log.
	confErr error

	// splitPending stops a second split being proposed from a descriptor the
	// first has already moved. Two splits in flight would each be computed
	// against a range extent the other is changing.
	splitPending   bool
	splitsProposed int
	staleSplits    int

	// propSeq numbers this node's proposals. Combined with the node id it makes
	// a ProposalID unique across the cluster.
	propSeq uint64

	// answered tracks client ops already responded to, by history index.
	inflight []clientOp

	// outOfExtent counts committed commands refused at apply for naming a key
	// outside this range's extent at that log position (BUG-014).
	outOfExtent int

	// writesRefused and readsRefused count commands the MVCC store declined for
	// naming a timestamp at or below the garbage-collection mark. Both are
	// asserted somewhere: a count nobody asserts on is decoration that looks
	// like evidence (DESIGN-A4 section 9.4b).
	writesRefused int
	readsRefused  int

	// gcProposed, gcApplied and versionsCollected say whether collection ran at
	// all. A sweep that never collected is green about a mark that never moved.
	gcPending         bool
	gcProposed        int
	gcApplied         int
	versionsCollected int

	// envelopeRefusals counts peers whose timestamp was beyond maxOffset ahead.
	// Zero in a bounded run is the bound holding; nonzero in a skew run is the
	// check being reachable. Both directions are asserted.
	envelopeRefusals int

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

	// snap is the snapshot this batch installed, if any. A batch carries either
	// a snapshot or log entries, never both, so one mark still names exactly one
	// handover.
	// snapMark is this batch's point in the SNAPSHOT stream, acknowledged
	// through AckSnapshot rather than AckPersisted.
	snapMark raft.PersistMark

	snap         *raft.SnapshotMeta
	snapVersions []kv.Version
	snapMarkTS   hlc.Timestamp

	// clearAllLog is an install, which discards every entry; clearBelow is a
	// local compaction, which discards only the prefix the snapshot covers.
	clearAllLog bool
	clearBelow  raft.Index

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

	// readTS is the remembered timestamp a snapshot read named, kept so a
	// reroute reissues the same question rather than a fresher one.
	readTS hlc.Timestamp
}

// newReplica builds one group's driver.
func newReplica(cfg Config) (*Replica, error) {
	if cfg.Transport == nil || cfg.Ledger == nil {
		return nil, fmt.Errorf("store: node %d needs a transport and a ledger", cfg.ID)
	}
	if cfg.SyncLatency <= 0 {
		return nil, fmt.Errorf("store: node %d has no modelled fsync duration", cfg.ID)
	}
	r, err := raft.New(raft.Config{
		ID: cfg.ID, Peers: cfg.Peers,
		ElectionTimeout: cfg.Election, HeartbeatTimeout: cfg.Heartbeat,
		Learners: cfg.Learners, PromotionLag: cfg.PromotionLag,
		PreVote: cfg.PreVote,
	})
	if err != nil {
		return nil, err
	}
	n := &Replica{cfg: cfg, raft: r, db: model.New(), epoch: sim.NewEpochGuard()}
	n.jitter()
	return n, nil
}

func (n *Replica) keyHardState() []byte { return keyHardStateOf(n.rng) }
func (n *Replica) keySnapshot() []byte  { return keySnapshotOf(n.rng) }
func (n *Replica) logPrefix() []byte    { return logPrefixOf(n.rng) }
func (n *Replica) logUpper() []byte     { return logUpperOf(n.rng) }

func (n *Replica) logKey(i raft.Index) []byte { return logKeyOf(n.rng, i) }

func (n *Replica) jitter() {
	if n.cfg.ElectionJitter != nil {
		n.raft.SetElectionTimeout(n.cfg.ElectionJitter(0))
	}
}

// onClient proposes a command. A node that is not the leader simply does not
// answer, which the checker treats as "may or may not have happened" -- correct,
// since an unavailable replica is behaving properly.
func (n *Replica) onClient(req Request) {
	if n.raft.Role() != raft.RoleLeader {
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
	// The leader stamps HERE, once, and the timestamp travels in the entry.
	// Every replica then applies a fact derived at a position rather than
	// re-deriving it from its own clock, which would give the same value two
	// different timestamps on two replicas (store/codec.go).
	// A read at a remembered timestamp keeps that timestamp; everything else is
	// stamped now. The HLC is still advanced either way, because the request is
	// an event on this node and the clock's order has to include it.
	at := n.hlc.Now()
	if req.ReadTS.IsSet() && req.Op == "get" {
		at = req.ReadTS
	}
	n.propSeq++
	id := raft.ProposalID{Node: n.cfg.ID, Seq: n.propSeq}
	if err := n.raft.Propose(id, encodeCmd(req.Op, req.Key, req.Value, at)); err != nil {
		return
	}
	n.inflight = append(n.inflight, clientOp{
		id: id, histIdx: req.HistIdx, key: req.Key, value: req.Value, op: req.Op,
		readTS: req.ReadTS,
	})
}

// drain does the driver's whole job: persist, send, apply, acknowledge.
func (n *Replica) drain(at clock.Instant, s sim.Scheduler) {
	defer func() {
		// # Checked here rather than only at the end of the run
		//
		// A node's cached configuration must agree with its own log at every
		// quiet moment. Checking only at the end misses every divergence that
		// something later repaired -- a subsequent configuration entry, a
		// snapshot install, a restart -- and a defect that is repaired before
		// anybody looks is a defect nobody finds. Measured: a truncation that
		// forgets to recompute went from 0 detections in 300 seeds to a real
		// number the moment the check moved here.
		//
		// Latched rather than returned, because drain has no error path and the
		// first inconsistency is the one worth reporting.
		if n.confErr == nil {
			n.confErr = n.raft.AssertConfConsistent()
		}
	}()
	for n.raft.HasReady() {
		rd := n.raft.Ready()
		wroteMark := false

		// 1. Persist. Everything goes through the engine interface, so a crash
		//    takes exactly what a crash should take.
		// 0. A snapshot install is its OWN batch on its OWN durability stream.
		//    No ordering may be assumed between it and the log write, so putting
		//    them in one batch -- or on one mark -- would let an acknowledgement
		//    of either stand for both.
		if rd.Snapshot != nil {
			desc, mark, vs, ok := decodeMachine(rd.Snapshot.Data)
			if !ok {
				panic(fmt.Sprintf("store: node %d was handed a snapshot it cannot decode", n.cfg.ID))
			}
			meta := raft.SnapshotMeta{
				Index: rd.Snapshot.Index, Term: rd.Snapshot.Term, Conf: rd.Snapshot.Conf,
			}
			sb := engine.NewBatch()
			sb.Set(n.keySnapshot(), encodeSnapshot(meta, rd.Snapshot.Data))
			sb.DeleteRange(n.logPrefix(), n.logUpper())
			if seq, err := n.db.Apply(sb, true); err == nil {
				n.pending = append(n.pending, pendingWrite{
					seq: seq, snapMark: rd.SnapMark, snap: &meta, snapVersions: vs, snapMarkTS: mark,
					clearAllLog: true,
				})
				s.At(at+n.cfg.SyncLatency, sim.KindDurable, sim.NodeID(n.cfg.Ordinal),
					sim.Stamp(n.epoch.Current(), seq))
			}
			n.writtenLast = meta.Index
			// The extent arrives WITH the state it describes. Keeping the old
			// one here is BUG-013: the node adopts a state machine that has
			// split and an extent that has not, then refuses the split entry
			// that would reconcile them.
			n.ingest(vs, mark)
			n.desc = desc
			n.snapshotsApplied++
			n.cfg.Ledger.RecordSnapshot(uint64(n.rng), n.cfg.Ordinal, provenance.Witness(raftcheck.SnapshotRecord{
				Index: meta.Index, Term: meta.Term, Digest: digest(rd.Snapshot.Data),
			}), at)
		}

		if rd.HardState != nil || len(rd.Entries) > 0 {
			wroteMark = true
			b := engine.NewBatch()
			w := pendingWrite{mark: rd.Mark}

			// A snapshot install replaces the state machine wholesale, so the
			// whole log goes with it in the same atomic batch. Leaving any of it
			// behind would give recovery a prefix from one history and a tail
			// from another, which is BUG-008 with a snapshot in place of a
			// truncation.
			if rd.HardState != nil {
				hs := *rd.HardState
				b.Set(n.keyHardState(), encodeHardState(hs))
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
					b.DeleteRange(n.logKey(last+1), n.logUpper())
					w.clearAbove = last
				}
				for _, e := range rd.Entries {
					b.Set(n.logKey(e.Index), encodeEntry(e))
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

		// 2a. Followers too far behind for the log get the snapshot instead.
		//     raft named them; the bytes are the driver's, so the driver sends.
		for _, to := range rd.SnapshotTo {
			meta, data, ok := n.storedSnapshot()
			if !ok {
				continue
			}
			m := raft.Message{
				Type: raft.MsgSnap, From: n.cfg.ID, To: to, Term: n.raft.Term(),
				SnapIndex: meta.Index, SnapTerm: meta.Term, SnapData: data,
				SnapConf: raft.EncodeConfiguration(meta.Conf),
			}
			n.cfg.Ledger.RecordSent(uint64(n.rng), n.cfg.Ordinal, provenance.Witness(m), at)
			n.cfg.Transport.Send(sim.Envelope{
				From: sim.NodeID(n.cfg.Ordinal), To: sim.NodeID(n.ordinalOf(to)),
				Kind: 1, Body: putRange(n.rng, n.hlc.Now(), encodeMessage(m)),
			})
		}

		// 2. Send, in any order, at any time. Safety does not depend on this
		//    happening after step 1, which is DR-7's entire point.
		for _, m := range rd.Messages {
			n.cfg.Ledger.RecordSent(uint64(n.rng), n.cfg.Ordinal, provenance.Witness(m), at)
			n.cfg.Transport.Send(sim.Envelope{
				From: sim.NodeID(n.cfg.Ordinal),
				To:   sim.NodeID(n.ordinalOf(m.To)),
				Kind: 1, Body: putRange(n.rng, n.hlc.Now(), encodeMessage(m)),
			})
		}

		// # A mark the driver was handed and did not write is a mark nobody can
		// # acknowledge
		//
		// raft reports Ready.Mark as the durability point this handover has to
		// clear. If it names a mark and the driver writes nothing under it, the
		// mark is never acknowledged and every message gated on it waits
		// forever -- a stall that looks exactly like a partition and keeps every
		// checker green.
		//
		// raft is supposed to make this unreachable: a Ready that hands nothing
		// over reports Mark zero. The assertion is here anyway, on the driver
		// side, because that guarantee is a property of ONE branch in Ready and
		// this is the only place that can see whether it held.
		if rd.Mark != 0 && !wroteMark {
			panic(fmt.Sprintf(
				"store: node %d range %d was handed mark %d with nothing to persist under it; "+
					"nothing will ever acknowledge that mark and every message gated on it is lost",
				n.cfg.ID, n.rng, rd.Mark))
		}

		// 3. Apply, answering each client operation at the instant its own entry
		//    is applied so a read observes exactly the state at its log
		//    position.
		if len(rd.Committed) > 0 {
			// # Every command in a Ready applies in ONE batch
			//
			// A state machine that applied each entry as its own engine write
			// would let a crash between two entries of one Ready leave the
			// machine at a position no log index names. The batch makes the
			// whole apply atomic against a crash, which is the same argument
			// A4's split rests on -- and the batch is written UNSYNCED, because
			// the state machine is derived state: it is rebuilt by replay, and
			// what must be durable is the log, not the derivation.
			mb := engine.NewBatch()
			for _, e := range rd.Committed {
				op, k, v, owned := "", "", "", true
				var cmdTS hlc.Timestamp
				switch {
				case isSplitCommand(e.Data):
					// A split is a command to the state machine like any other,
					// and raft has no business knowing what a range is.
					//
					// # The staged writes are flushed FIRST, and the order is
					// # not a preference
					//
					// A split reads the range's whole version set and rewrites
					// it. Writes staged earlier in this same Ready are still in
					// the batch, so a split that ran before the flush would
					// partition a state that is missing them -- and then the
					// flush would land them back afterwards, including keys the
					// split had just given away.
					//
					// It fired immediately: a range whose extent was [,k02)
					// holding k02, so the next split cut at k02 and produced an
					// EMPTY right range. Found by applySplit's own partition
					// assertion, which A4 added for exactly this shape.
					n.flushApply(mb)
					if spec, ok := decodeSplitCommand(e.Data); ok {
						n.machine.applySplit(n, spec, e.Index, at, s)
						n.splitPending = false
					}
				case len(e.Data) > 0:
					op, k, v, cmdTS = decodeCmd(e.Data)
					// # The extent is checked HERE, at the log position, and
					// # the check at arrival cannot stand in for it
					//
					// A leader accepts a request against the extent it has
					// APPLIED. A split entry it has already appended is not
					// applied yet, so between those two moments the leader
					// accepts writes for keys the log has already given away,
					// and the entry commits behind the split.
					//
					// Applying it anyway puts a key into a range that does not
					// own it while the range that does owns it too -- two
					// ranges claiming one key, which is "no request served
					// under a stale descriptor epoch" broken from inside. It
					// survived because the key then sat in the wrong range
					// forever: the next split moves out only what its own new
					// right half contains, and a key outside both halves is
					// moved by nothing (BUG-014).
					//
					// This check is at the log position, so every replica
					// reaches the same verdict from the same state. That is
					// what the arrival check cannot do and never could.
					if op == opGC {
						// Collection is not owned by any key, so the extent
						// check does not apply to it: it is a statement about
						// this range's history, and the range applies it whole.
						n.flushApply(mb)
						b := engine.NewBatch()
						n.gcPending = false
						removed, err := n.mvcc.AdvanceGCInto(b, cmdTS)
						if err == nil {
							n.flushApply(b)
							n.gcApplied++
							n.versionsCollected += removed
						}
						n.answerAt(e, "", "", cmdTS, at)
						continue
					}
					owned = n.desc.Contains([]byte(k))
					if owned && op == "put" {
						if err := n.mvcc.PutInto(mb, []byte(k), cmdTS, []byte(v)); err != nil {
							// A write the state machine refuses is not a crash:
							// below the GC mark it is a command the cluster has
							// collectively decided is unanswerable, and every
							// replica refuses it identically because the mark is
							// applied state.
							n.writesRefused++
							owned = false
						}
					}
				}
				if owned {
					// A read must see every write at a lower index, including
					// ones staged into this same batch. Flushing before the
					// answer costs the batching on read-heavy traffic and buys
					// the only thing that matters: a read at a log position sees
					// the state that position produces. A read answered from a
					// batch that had not landed would return the value from
					// before its own predecessor -- a stale read manufactured by
					// the driver rather than by the protocol.
					if op == "get" {
						n.flushApply(mb)
					}
					n.answerAt(e, op, k, cmdTS, at)
				} else {
					n.rerouteAt(e, op, k, v, at, s)
				}
			}
			n.flushApply(mb)
			n.maybeGC()
			n.cfg.Ledger.RecordApplied(uint64(n.rng), n.cfg.Ordinal, provenance.Witness(rd.Committed), at)
			n.raft.AckApplied(rd.Committed[len(rd.Committed)-1].Index)
			n.maybeSnapshot(at, s)
			n.maybeSplit(at)
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
func (n *Replica) answerAt(e raft.Entry, op, key string, readTS hlc.Timestamp, at clock.Instant) {
	if e.ID.Zero() {
		return
	}
	kept := n.inflight[:0]
	for _, c := range n.inflight {
		if c.id != e.ID {
			kept = append(kept, c)
			continue
		}
		// # The read is answered AT ITS OWN TIMESTAMP
		//
		// Not at the newest version, and not at the leader's clock now. The
		// entry carries the timestamp the read was stamped with at propose, and
		// the answer is the version visible there -- which is what makes the
		// answer a function of the log rather than of when this replica got
		// round to applying it (DESIGN-A5 section 7).
		val := ""
		if op == "get" {
			v, ok, err := n.mvcc.ReadAt([]byte(key), readTS)
			n.cfg.Ledger.RecordRead(provenance.Witness(raftcheck.ReadRecord{
				Range: uint64(n.rng), Node: n.cfg.Ordinal, Index: e.Index, Key: key, At: readTS,
				Value: string(v), Found: ok && err == nil, Refused: err != nil, When: at,
			}))
			switch {
			case err != nil:
				// A refused read is an outcome, not an answer. Ending it as OK
				// with an empty value would tell the linearizability checker
				// the key was absent, which is a claim about history rather
				// than about answerability.
				n.readsRefused++
				if c.histIdx >= 0 {
					n.cfg.History.End(c.histIdx, at, sim.RespError, "")
				}
				continue
			case ok:
				val = string(v)
			}
		}
		// A snapshot read carries no history index: a read at a past timestamp
		// is not a linearizable operation on the current value, and handing one
		// to porcupine as if it were would manufacture violations out of
		// correct behaviour. It is judged by mvcc-read-correctness, which is the
		// oracle that knows which timestamp it named.
		if c.histIdx >= 0 {
			n.cfg.History.End(c.histIdx, at, sim.RespOK, val)
		}
	}
	n.inflight = kept
}

// flushApply writes a staged apply batch and empties it.
//
// Unsynced, because the state machine is DERIVED state: it is rebuilt from the
// snapshot plus the log on every restart, so what must be durable is the log.
// Syncing here would pay an fsync per apply for a guarantee nothing needs.
func (n *Replica) flushApply(b *engine.Batch) {
	if b.Empty() {
		return
	}
	if _, err := n.db.Apply(b, false); err != nil {
		panic(fmt.Sprintf("store: node %d cannot apply range %d's committed batch: %v",
			n.cfg.ID, n.rng, err))
	}
	b.Reset()
}

// rerouteAt answers a command applied outside this range's extent by sending the
// client back to whichever range owns the key now.
//
// The operation is NOT ended in the history. It never took effect, and the
// client is still waiting -- which is exactly what a router doing
// refresh-and-retry looks like from outside. Ending it as an error would be a
// second claim, that the client was told, and nothing here told anyone.
func (n *Replica) rerouteAt(e raft.Entry, op, key, value string, at clock.Instant, s sim.Scheduler) {
	n.outOfExtent++
	if e.ID.Zero() {
		return
	}
	kept := n.inflight[:0]
	for _, c := range n.inflight {
		if c.id != e.ID {
			kept = append(kept, c)
			continue
		}
		s.At(at+staleRetryDelay, sim.KindClient, sim.NodeID(n.cfg.Ordinal),
			// The remembered timestamp survives the reroute. Dropping it would
			// silently turn a snapshot read into a read at now, and the oracle
			// would then be comparing an answer against a timestamp the client
			// never asked for.
			Request{Op: op, Key: key, Value: value, HistIdx: c.histIdx, ReadTS: c.readTS})
	}
	n.inflight = kept
}

// OutOfExtentRefusals is how many committed commands this replica refused to
// apply for naming a key its extent no longer covers.
func (n *Replica) OutOfExtentRefusals() int { return n.outOfExtent }

// onDurable turns an engine sync completion into AckPersisted, and records what
// is now durable into the ledger.
func (n *Replica) onDurable(seq engine.SeqNum, at clock.Instant) {
	var mark, snapMark raft.PersistMark
	kept := n.pending[:0]
	for _, w := range n.pending {
		if w.seq <= seq {
			n.fold(w)
			if w.mark > mark {
				mark = w.mark
			}
			if w.snapMark > snapMark {
				snapMark = w.snapMark
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

	// # The driver's record and the engine's own account, compared continuously
	//
	// These are two independent derivations of one fact: what a crash would
	// recover. The ledger is fed the first, because the second is an answer the
	// system gives about itself and answers with its VISIBLE state.
	//
	// But the second is a legitimate input to a check that can only FAIL, and
	// here is where it can be taken honestly: the engine has nothing in flight,
	// so a read-back IS the durable state. Comparing them only at recovery left
	// the check firing twice per run; comparing them here fires it on every
	// completion, which took a planted defect's detection from seed 905 to the
	// first seeds of the range.
	if n.db.VisibleSeq() == n.db.DurableSeq() {
		n.crossChecks++
		st := n.readDurable().Unverified()
		if err := sameDurableState(n.durHS, n.durSnap, n.durLog, st); err != nil {
			panic(fmt.Sprintf("store: node %d has made durable something its own record disagrees with: %v",
				n.cfg.ID, err))
		}
	}

	n.cfg.Ledger.RecordDurable(uint64(n.rng), n.cfg.Ordinal, provenance.Witness(raftcheck.DurableState{
		HardState: n.durHS, Snapshot: n.durSnap, Log: n.durLog,
	}), at)
	if mark != 0 {
		n.raft.AckPersisted(mark)
	}
	if snapMark != 0 {
		n.raft.AckSnapshot(snapMark)
	}
}

// fold applies one completed write to the durable record. It is the only place
// that record moves forward, and it moves on the engine's completion rather than
// on the driver's intention.
func (n *Replica) fold(w pendingWrite) {
	if w.hs != nil {
		n.durHS = *w.hs
	}
	if w.snap != nil {
		n.durSnap = *w.snap
	}
	if w.clearAllLog {
		// An install replaces the state machine and every entry with it.
		n.durLog = nil
	}
	if w.clearBelow > 0 {
		// A local compaction drops only the prefix the snapshot now covers.
		kept := n.durLog[:0]
		for _, e := range n.durLog {
			if e.Index > w.clearBelow {
				kept = append(kept, e)
			}
		}
		n.durLog = kept
	}
	for _, e := range w.entries {
		if p := int(e.Index - n.durSnap.Index - 1); p < len(n.durLog) {
			n.durLog[p] = e
			continue
		}
		n.durLog = append(n.durLog, e)
	}
	if w.clearAbove > 0 {
		keep := int(w.clearAbove - n.durSnap.Index)
		if keep >= 0 && keep < len(n.durLog) {
			n.durLog = n.durLog[:keep]
		}
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
func (n *Replica) readDurable() provenance.Reported[recovered] {
	if v, d := n.db.VisibleSeq(), n.db.DurableSeq(); v != d {
		panic(fmt.Sprintf(
			"store: node %d read the engine back with sequence %d visible and only %d durable; "+
				"the result would report as persisted writes that a crash would take",
			n.cfg.ID, v, d))
	}
	var st recovered

	if v, err := n.db.Get(n.keyHardState()); err == nil {
		st.hs = decodeHardState(v)
	}
	if v, err := n.db.Get(n.keySnapshot()); err == nil {
		if meta, data, ok := decodeSnapshot(v); ok {
			st.snap = meta
			if desc, mark, vs, ok := decodeMachine(data); ok {
				st.desc, st.haveDesc, st.versions, st.mark = desc, true, vs, mark
			}
		}
	}
	it := n.db.NewIter(engine.IterOptions{Lower: n.logPrefix(), Upper: n.logUpper()})
	for ok := it.First(); ok; ok = it.Next() {
		if e, ok := decodeEntry(it.Value()); ok {
			st.entries = append(st.entries, e)
		}
	}
	_ = it.Close()
	return provenance.Claim(st)
}

// recovered is everything a restart reads back: the hard state, the snapshot the
// state machine restarts from, the log tail above it, and the state machine that
// snapshot decodes to.
type recovered struct {
	hs       raft.HardState
	snap     raft.SnapshotMeta
	entries  []raft.Entry
	versions []kv.Version
	mark     hlc.Timestamp

	// desc is the range extent the snapshot carried. haveDesc distinguishes "the
	// snapshot said the range is empty" from "there was no snapshot", which a
	// zero descriptor cannot.
	desc     RangeDescriptor
	haveDesc bool
}

// storedSnapshot is the snapshot this node can hand a follower.
//
// It reads the engine, so it is Reported and it is used for one thing only:
// deciding what to SEND. Sending a snapshot that is visible but not yet durable
// is safe -- its contents are derived from entries a quorum already holds -- and
// no verdict rests on it.
func (n *Replica) storedSnapshot() (raft.SnapshotMeta, []byte, bool) {
	v, err := n.db.Get(n.keySnapshot())
	if err != nil {
		return raft.SnapshotMeta{}, nil, false
	}
	meta, data, ok := decodeSnapshot(v)
	return meta, data, ok
}

// maybeSnapshot compacts once the log has grown past the threshold.
//
// The snapshot and the prefix delete are ONE batch, so a crash between them is
// not a state a reader can observe: either the snapshot is there and the prefix
// is gone, or neither happened and recovery reads the full log. The write is not
// gated on anything, because no outbound message depends on it -- raft has
// already compacted in memory, and if the write is lost the node simply recovers
// the longer log it had before.
func (n *Replica) maybeSnapshot(at clock.Instant, s sim.Scheduler) {
	if n.cfg.SnapshotThreshold == 0 {
		return
	}
	applied := n.raft.AppliedIndex()
	if applied < n.raft.SnapshotIndex()+n.cfg.SnapshotThreshold {
		return
	}
	term, conf, err := n.raft.Compact(applied)
	if err != nil {
		// Not applied or not durable yet. A refusal, not a failure: the
		// threshold will be met again on the next apply.
		return
	}
	meta := raft.SnapshotMeta{Index: applied, Term: term, Conf: conf}
	data := encodeMachine(n.desc, n.mvcc.GCMark(), n.versions())

	b := engine.NewBatch()
	b.Set(n.keySnapshot(), encodeSnapshot(meta, data))
	b.DeleteRange(n.logPrefix(), n.logKey(applied+1))
	seq, err := n.db.Apply(b, true)
	if err != nil {
		return
	}
	n.pending = append(n.pending, pendingWrite{seq: seq, snap: &meta, clearBelow: applied})
	s.At(at+n.cfg.SyncLatency, sim.KindDurable, sim.NodeID(n.cfg.Ordinal),
		sim.Stamp(n.epoch.Current(), seq))
	n.snapshotsTaken++
	n.cfg.Ledger.RecordSnapshot(uint64(n.rng), n.cfg.Ordinal, provenance.Witness(raftcheck.SnapshotRecord{
		Index: meta.Index, Term: meta.Term, Digest: digest(data), Taken: true,
	}), at)
}

// maybeGC proposes a collection mark once the leader's clock has moved past the
// retention window.
//
// # Why the mark is proposed rather than applied
//
// Collection is a state machine transition like any other: it changes what reads
// are answerable, so every replica must do it at the same log position or two
// replicas disagree about whether a read can be served -- which surfaces as an
// error on one node and an answer on another, the hardest kind of divergence to
// attribute.
//
// So the leader picks the mark from ITS clock, once, and the mark travels in the
// entry. Every replica applies the same mark at the same index. That is the
// same sentence as the split key and the command timestamp, for the third time
// (DESIGN-A5 section 7).
// # Throttled to one collection in flight, and to a mark that has moved
//
// The first version proposed whenever the retention window had passed, which
// after the first collection is true on essentially every apply -- so it wrote a
// collection entry per applied entry. That is not a performance detail: it is a
// log made mostly of bookkeeping, and it slowed a 200-seed sweep to the point
// where a 10,000-seed exit run would have taken most of a day. A workload the
// harness cannot afford to run is a workload the harness does not check.
//
// So: one collection in flight per range, and the mark must move by a
// meaningful fraction of the window before another is worth an entry. Both are
// what a real collector does, for the same reason.
func (n *Replica) maybeGC() {
	if n.cfg.GCRetention == 0 || n.gcPending || !n.IsLeader() {
		return
	}
	now := n.hlc.Now()
	to := hlc.Timestamp{Wall: now.Wall.Add(-n.cfg.GCRetention)}
	if !to.IsSet() {
		return
	}
	if !n.mvcc.GCMark().Wall.Add(n.cfg.GCRetention / 4).Before(to.Wall) {
		return
	}
	n.propSeq++
	id := raft.ProposalID{Node: n.cfg.ID, Seq: n.propSeq}
	if err := n.raft.Propose(id, encodeCmd(opGC, "", "", to)); err == nil {
		n.gcProposed++
		n.gcPending = true
	}
}

// maybeSplit proposes a split once this range holds more keys than its
// threshold.
//
// The split key is the MEDIAN of the keys the range holds, which is a function
// of state every replica already agrees on. The entry carries it anyway, so
// followers never re-derive it -- but choosing it from something a follower
// could not have computed (a clock, a counter) would make the leader's choice
// unverifiable, and this project has spent three phases learning what an
// unverifiable value costs.
func (n *Replica) maybeSplit(at clock.Instant) {
	if n.cfg.SplitThreshold == 0 || n.splitPending || !n.IsLeader() {
		return
	}
	keys, err := n.mvcc.Keys()
	if err != nil || len(keys) < n.cfg.SplitThreshold {
		return
	}
	// The cut is the median USER KEY, not the median version. Cutting at the
	// median version would put the split wherever write traffic was heaviest
	// rather than in the middle of the key space, so a hot key would split a
	// range into one holding it and one holding everything else.
	key, ok := splitKeyFor(strs(keys))
	if !ok {
		return
	}
	left := n.desc.Clone()
	left.End = append([]byte(nil), key...)
	left.Epoch++
	right := RangeDescriptor{
		ID:    n.machine.nextRangeID(),
		Start: append([]byte(nil), key...),
		End:   append([]byte(nil), n.desc.End...),
		Epoch: 1,
	}
	n.propSeq++
	id := raft.ProposalID{Node: n.cfg.ID, Seq: n.propSeq}
	if err := n.raft.Propose(id, encodeSplitCommand(SplitSpec{Key: key, Left: left, Right: right})); err != nil {
		return
	}
	n.splitPending = true
	n.splitsProposed++
	_ = at
}

// StaleSplits is how many split entries this replica skipped for naming an
// extent the range had already moved past.
func (n *Replica) StaleSplits() int { return n.staleSplits }

// SplitsProposed is how many splits this replica's leader proposed.
func (n *Replica) SplitsProposed() int { return n.splitsProposed }

// Descriptor reports this replica's range extent, for the harness's counters.
func (n *Replica) Descriptor() RangeDescriptor { return n.desc.Clone() }

// digest is FNV-1a over the snapshot bytes. It is a comparison key, not a
// security property: the snapshot's contents are checked against an
// independently computed state, and a digest keeps the ledger from carrying a
// copy of every state machine in the run.
func digest(b []byte) uint64 {
	const offset64, prime64 = uint64(14695981039346656037), uint64(1099511628211)
	h := offset64
	for _, c := range b {
		h ^= uint64(c)
		h *= prime64
	}
	return h
}

// SnapshotsTaken and SnapshotsApplied report what this node did with snapshots,
// so a lane can ask whether the paths ran at all.
func (n *Replica) SnapshotsTaken() int   { return n.snapshotsTaken }
func (n *Replica) SnapshotsApplied() int { return n.snapshotsApplied }

// adoptSnapshot installs a derived initial state on a newly created replica.
func (n *Replica) adoptSnapshot(meta raft.SnapshotMeta, vs []kv.Version, mark hlc.Timestamp) {
	r, err := raft.Restore(raft.Config{
		ID: n.cfg.ID, Peers: n.cfg.Peers,
		ElectionTimeout: n.cfg.Election, HeartbeatTimeout: n.cfg.Heartbeat,
		Learners: n.cfg.Learners, PromotionLag: n.cfg.PromotionLag,
		PreVote: n.cfg.PreVote,
	}, raft.HardState{}, meta, nil)
	if err != nil {
		panic(fmt.Sprintf("store: node %d cannot start range %d: %v", n.cfg.ID, n.rng, err))
	}
	n.raft = r
	n.ingest(vs, mark)
	n.durSnap = meta
	n.jitter()
}

// ingest replaces this replica's whole state machine in one batch.
//
// Clear-then-ingest is atomic because the engine interface has DeleteRange
// (Amendment A3): a best-effort clear followed by a separate write would leave a
// window holding a mixture of two states, and a crash in that window recovers
// into it.
func (n *Replica) ingest(vs []kv.Version, mark hlc.Timestamp) {
	b := engine.NewBatch()
	n.mvcc.IngestInto(b, vs, mark)
	if _, err := n.db.Apply(b, false); err != nil {
		panic(fmt.Sprintf("store: node %d cannot ingest range %d's state: %v", n.cfg.ID, n.rng, err))
	}
}

// versions is everything this replica's state machine holds.
func (n *Replica) versions() []kv.Version {
	vs, err := n.mvcc.Versions()
	if err != nil {
		panic(fmt.Sprintf("store: node %d cannot read range %d's own state back: %v", n.cfg.ID, n.rng, err))
	}
	return vs
}

// IsLeader is the driver's own routing question, not a safety judgement.
func (n *Replica) IsLeader() bool { return !n.down && n.raft.Role() == raft.RoleLeader }

// RequestTransfer hands leadership to target if this node is the leader.
//
// The resulting MsgTimeoutNow sits in raft's outbound queue until the next event
// on this node drains it, which is at most one tick away. The alternative is
// threading a scheduler into an action callback that has none, for a message
// whose whole point is that it does not have to be fast.
func (n *Replica) RequestTransfer(target raft.NodeID) bool {
	if !n.IsLeader() || target == n.cfg.ID {
		return false
	}
	n.raft.TransferLeadership(target)
	n.transfersAsked++
	return true
}

// TransfersAsked is how many transfers this node initiated.
func (n *Replica) TransfersAsked() int { return n.transfersAsked }

// RequestConfChange moves target one step around the membership cycle:
// absent -> learner -> voter -> absent.
//
// A cycle rather than one kind of change, because A3 has to exercise all three
// and a cluster that only ever adds runs out of room. The step is chosen from
// the CURRENT configuration, so a refusal -- a lagging learner, a change already
// in flight -- leaves the node where it is and the next action tries again.
// Refusals are counted, because a phase whose membership changes were all
// refused is a phase that tested nothing.
func (n *Replica) RequestConfChange(target raft.NodeID) {
	if !n.IsLeader() || target == n.cfg.ID {
		return
	}
	conf := n.raft.Configuration()
	var ch raft.ConfChangeSingle
	switch {
	case conf.IsLearner(target):
		ch = raft.ConfChangeSingle{Type: raft.ConfChangeAddVoter, Node: target}
	case conf.IsVoter(target):
		ch = raft.ConfChangeSingle{Type: raft.ConfChangeRemoveNode, Node: target}
	default:
		ch = raft.ConfChangeSingle{Type: raft.ConfChangeAddLearner, Node: target}
	}
	n.proposeConf(ch)
}

// RequestMove advances a manual rebalance of this range by ONE step: move the
// replica on `from` to `to`.
//
// # Stateless on purpose, and that is the whole design
//
// A move is four things -- add a learner, promote it, hand leadership away if
// the source is holding it, remove the source -- and the obvious shape is a
// little state machine on the leader. That shape has a hole with a name: the
// leader can lose leadership between the add and the remove, and the next
// leader has no idea a move was under way. The move stalls with an extra
// replica that nobody will ever remove.
//
// So there is no state. Each call reads the configuration and does whichever
// step is next, which makes the operation idempotent, replayable, and
// completable by whatever node happens to be leading when it is next asked.
// Ordering the same move repeatedly is how it finishes.
//
// # Safety is a property of the ORDER, and the order is not negotiable
//
// The removal is proposed only once `to` is a voter. Since membership changes
// one server at a time (D-A3-1), the committed voter count goes N -> N+1 -> N
// and never dips below where it started -- which is "quorum availability is
// never voluntarily reduced" discharged by construction rather than by care.
// The rebalance-safety oracle checks it from the committed log anyway, because
// a property discharged by construction is a property one refactor from being
// discharged by nothing.
func (n *Replica) RequestMove(from, to raft.NodeID, begin bool) bool {
	if !n.IsLeader() || from == to {
		return false
	}
	conf := n.raft.Configuration()

	// # The precondition belongs to the FIRST order, and it has to be explicit
	//
	// Statelessness costs one thing: the mechanism cannot tell "the destination
	// is already a voter because I just added it" from "the destination was
	// already there before anybody asked". The first is the middle of a move.
	// The second is not a move at all, and treating it as one goes straight to
	// removing the source -- a quorum reduction wearing a move's name.
	//
	// The caller knows which order is the first one, so the caller says so.
	// That is not state carried across leadership changes; it is a parameter,
	// and it turns an implicit assumption into a stated precondition.
	//
	// Found by the rebalance oracle on seed 103, in the one shape a checker
	// reading committed entries cannot be argued out of: a removal committed
	// with no addition anywhere in the log.
	if begin {
		if conf.IsVoter(to) || conf.IsLearner(to) || !conf.IsVoter(from) || len(conf.Voters) < 2 {
			return false
		}
		return n.proposeConf(raft.ConfChangeSingle{Type: raft.ConfChangeAddLearner, Node: to})
	}

	if !conf.IsVoter(from) && !conf.IsLearner(from) {
		// Already gone. The move is done, or somebody else did it.
		return false
	}
	switch {
	case !conf.IsVoter(to) && !conf.IsLearner(to):
		return n.proposeConf(raft.ConfChangeSingle{Type: raft.ConfChangeAddLearner, Node: to})
	case conf.IsLearner(to):
		// raft enforces the catch-up bound here, and a refusal is the bound
		// doing its job: the next order will try again.
		return n.proposeConf(raft.ConfChangeSingle{Type: raft.ConfChangeAddVoter, Node: to})
	case from == n.cfg.ID:
		// The source is holding leadership. Hand it away first: removing the
		// leader is legal but it costs an election, and the point of a move is
		// that the range keeps serving through it.
		for _, v := range conf.Voters {
			if v != from {
				n.RequestTransfer(v)
				return true
			}
		}
		return false
	default:
		return n.proposeConf(raft.ConfChangeSingle{Type: raft.ConfChangeRemoveNode, Node: from})
	}
}

// proposeConf is the one place a single-server change is proposed, so the
// refusal accounting cannot drift between the churn path and the move path.
func (n *Replica) proposeConf(ch raft.ConfChangeSingle) bool {
	n.propSeq++
	id := raft.ProposalID{Node: n.cfg.ID, Seq: n.propSeq}
	if err := n.raft.ProposeConfChange(id, raft.ConfChangeV2{
		Changes: []raft.ConfChangeSingle{ch}, Transition: raft.ConfChangeSimple,
	}); err != nil {
		n.confRefused++
		if errors.Is(err, raft.ErrLearnerLagging) {
			n.lagRefused++
		}
		return false
	}
	n.confProposed++
	return true
}

// ConfProposed, ConfRefused and LagRefused report what the membership churn
// actually did. A sweep in which every change was refused is green about nothing.
func (n *Replica) ConfProposed() int { return n.confProposed }

// ConfRefused counts changes the state machine declined for any reason.
func (n *Replica) ConfRefused() int { return n.confRefused }

// LagRefused counts promotions declined because the learner was too far behind,
// which is the catch-up bound doing its job.
func (n *Replica) LagRefused() int { return n.lagRefused }

// ConfRecoveries is how many restarts recovered a log containing a configuration
// change, and ConfCrossChecks how many had a snapshot configuration to be
// checked against.
func (n *Replica) ConfRecoveries() int  { return n.confRecoveries }
func (n *Replica) ConfCrossChecks() int { return n.confCrossChecks }

// StateDigest is the harness's hook for computing the digest of a state machine
// it built itself: the extent AND the keys, because both are applied state.
//
// The harness re-implements what a command DOES and shares only the
// serialisation, so a defect in applying commands cannot cancel out on both
// sides of the snapshot comparison.
func StateDigest(desc RangeDescriptor, mark hlc.Timestamp, vs []kv.Version) uint64 {
	return digest(encodeMachine(desc, mark, vs))
}

// crash takes the process down: volatile state goes, unsynced writes go.
func (n *Replica) onCrash() {
	// A process death ends an incarnation. Every completion already in flight
	// belongs to it and must never reach whatever comes next.
	n.down = true
	n.inflight = nil

	// These writes never became durable, so they never enter the durable record
	// -- and the engine has just reverted to it, so what the driver has written
	// comes back to the durable log's own end.
	n.pending = nil
	n.writtenLast = n.durSnap.Index
	if last := lastLogIndex(n.durLog); last > n.writtenLast {
		n.writtenLast = last
	}
}

// restart rebuilds the node from the engine. This is the real recovery path:
// every crash the harness injects exercises it.
//
// It discards the previous incarnation's durability bookkeeping, and that is
// load-bearing rather than tidiness. A sync completion names a mark issued by
// the Raft that requested it; handing it to the Raft that replaced it
// acknowledges a mark that instance never issued, which closes nothing and
// leaves every message gated on its real mark withheld forever. BUG-001.
func (n *Replica) restart() {
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
	// A restart is a new incarnation too, not a continuation of the crashed
	// one. Treating it as a continuation is how a second restart handed a fresh
	// Raft an acknowledgement for a mark it never issued (BUG-002).
	n.pending = nil
	n.inflight = nil

	n.restartFrom(n.readDurable())
}

// readRecovered is the pure half of a restart: it reads the engine and returns
// what recovery will be built from, without writing anything.
//
// # Why the read is separated from the rebuild
//
// A machine hosts many replicas over ONE engine (A4), and rebuilding a replica's
// state machine writes to that engine. So a rebuild-as-you-go loop has replica 1
// writing before replica 2 reads -- and replica 2's read-back assertion, the one
// that says the engine must not report as persisted what a crash would take,
// fires on writes replica 1 made a microsecond earlier.
//
// The assertion is right and the loop was wrong. Every replica reads first, then
// every replica rebuilds. A5 found this the moment the state machine stopped
// living in a Go map and started living in the engine.
func (n *Replica) readRecovered() provenance.Reported[recovered] {
	n.pending = nil
	n.inflight = nil
	return n.readDurable()
}

func (n *Replica) restartFrom(stR provenance.Reported[recovered]) {
	st := stR.Unverified()

	// The driver's record of what it made durable and the engine's read-back are
	// two independent derivations of one fact, and recovery is the moment they
	// can be compared. A disagreement means one of them is wrong, and which one
	// is a question worth stopping for: everything the ledger asserts about
	// persistence rests on the first, and everything the cluster does after a
	// crash rests on the second.
	n.crossChecks++
	if err := sameDurableState(n.durHS, n.durSnap, n.durLog, st); err != nil {
		panic(fmt.Sprintf("store: node %d recovered a state its own durability record disagrees with: %v", n.cfg.ID, err))
	}

	r, err := raft.Restore(raft.Config{
		ID: n.cfg.ID, Peers: n.cfg.Peers,
		ElectionTimeout: n.cfg.Election, HeartbeatTimeout: n.cfg.Heartbeat,
		Learners: n.cfg.Learners, PromotionLag: n.cfg.PromotionLag,
		PreVote: n.cfg.PreVote,
	}, st.hs, st.snap, st.entries)
	if err != nil {
		// A log the engine cannot produce a gapless prefix from is a storage
		// bug, not a recoverable condition, and swallowing it would hide it.
		panic(fmt.Sprintf("store: node %d cannot recover: %v", n.cfg.ID, err))
	}
	n.raft = r

	// # The recovered configuration, checked against the same bytes
	//
	// recomputeConf is where DESIGN-A3 §3 says the bugs live: a configuration is
	// a function of the log, and the log can be truncated, replaced by a
	// snapshot, or read back off disk. Recovery is the moment the node's answer
	// can be compared against one derived independently from what it recovered
	// FROM, and the comparison can only fail, which is the side of the provenance
	// rule where the system's own answer is allowed.
	for _, e := range st.entries {
		if e.Type == raft.EntryConfChange {
			n.confRecoveries++
			break
		}
	}
	want, ok := derivedConf(st)
	if ok {
		n.confCrossChecks++
	}
	if ok && !r.Configuration().Equal(want) {
		panic(fmt.Sprintf(
			"store: node %d recovered configuration %s from a snapshot and log that say %s",
			n.cfg.ID, r.Configuration(), want))
	}

	// The state machine restarts from the snapshot, not from empty. Anything
	// above it sits in the log tail and comes back through Ready.Committed once a
	// leader confirms it -- which is the "snapshot plus tail" half of the
	// equivalence this phase has to prove.
	n.ingest(st.versions, st.mark)

	// # The extent is applied state, and it is recovered only from an index
	//
	// This line read `if st.desc.Epoch >= n.desc.Epoch`, taking whichever
	// descriptor was newer -- the snapshot's, or the one `readDescriptors` found
	// under the range's own key. Taking the newer of two facts is right when both
	// describe the same instant. These do not.
	//
	// The snapshot's descriptor is aligned with the state machine the snapshot
	// decodes to: it is the extent AT that index. The descriptor key is written
	// whenever a split applies and is aligned with nothing, so after a crash it
	// can be ahead of the state being rebuilt. A node that applied a split at
	// index 10, crashed, and recovers from a snapshot at index 6 then rebuilds a
	// state machine that has not split while holding a descriptor that has.
	// Re-applying the split entry finds its own effect already recorded in the
	// extent, judges the entry stale, and skips it -- so the keys never move, and
	// the node's state machine diverges from every replica that applied it once
	// (BUG-011).
	//
	// So the snapshot wins outright. Where there is no snapshot the extent is the
	// range's birth extent, and replaying the log carries it forward from there.
	// Both are positions in the log; the descriptor key is not one, and it is
	// demoted to what it can honestly be: a record that the range exists.
	switch {
	case st.haveDesc:
		n.desc = st.desc
	case n.rng == FirstRange:
		n.desc = FirstRangeDescriptor()
	default:
		// A split-born range's descriptor and its birth snapshot are written in
		// one batch, so discovering the range without its snapshot is not a
		// state this code can recover from -- and guessing an extent here is how
		// two ranges come to claim the same keys.
		panic(fmt.Sprintf(
			"store: node %d recovered range %d with no snapshot to take an extent from",
			n.cfg.ID, n.rng))
	}
	n.down = false
	n.writtenLast = n.durSnap.Index
	if last := lastLogIndex(n.durLog); last > n.writtenLast {
		n.writtenLast = last
	}
	n.jitter()
}

// derivedConf rebuilds the configuration from a recovered state: the snapshot's
// configuration, then every configuration entry in the log tail, in order.
//
// It reports false when the snapshot carries no configuration, which is the
// ordinary case for a node young enough never to have compacted -- there is
// nothing to derive from and nothing to compare against.
func derivedConf(st recovered) (raft.Configuration, bool) {
	if len(st.snap.Conf.Voters) == 0 {
		return raft.Configuration{}, false
	}
	conf := st.snap.Conf.Clone()
	for _, e := range st.entries {
		if e.Type != raft.EntryConfChange {
			continue
		}
		if next, err := raft.ApplyConfEntry(conf, e.Data); err == nil {
			conf = next
		}
	}
	return conf, true
}

// sameDurableState compares the driver's durability record with what the engine
// gave back.
func sameDurableState(recHS raft.HardState, recSnap raft.SnapshotMeta, recLog []raft.Entry, got recovered) error {
	gotHS, gotLog := got.hs, got.entries
	if recHS != gotHS {
		return fmt.Errorf("hard state recorded %+v, engine returned %+v", recHS, gotHS)
	}
	if recSnap.Index != got.snap.Index || recSnap.Term != got.snap.Term ||
		!recSnap.Conf.Equal(got.snap.Conf) {
		return fmt.Errorf("snapshot recorded %+v, engine returned %+v", recSnap, got.snap)
	}
	if len(recLog) != len(gotLog) {
		return fmt.Errorf("recorded %d durable entries above the snapshot, engine returned %d", len(recLog), len(gotLog))
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

// DurabilityCrossChecks is how often this node's durability record was compared
// against the engine's own account of what it holds.
func (n *Replica) DurabilityCrossChecks() int { return n.crossChecks }

// StaleEpochDrops is how many completions from a dead incarnation this node
// refused.
func (n *Replica) StaleEpochDrops() int { return n.epoch.Dropped() }

// CheckEpochs refuses a run in which any cross-epoch delivery occurred.
func (n *Replica) CheckEpochs() error {
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
func (n *Replica) AssertQuiescent() error {
	if n.down {
		return nil
	}
	// The configuration check does not depend on anything being in flight: a
	// node's cached membership must agree with its own log at every quiet
	// moment, in flight or not.
	if n.confErr != nil {
		return n.confErr
	}
	if len(n.pending) > 0 {
		return nil
	}
	if err := n.raft.AssertQuiescent(); err != nil {
		return fmt.Errorf("%w [%s]", err, n.raft.QuiesceDebug())
	}
	return nil
}

// Request is a client operation carried as an event payload.
type Request struct {
	Client  int
	Seq     uint64
	Op      string
	Key     string
	Value   string
	HistIdx int

	// ReadTS, when set, is the timestamp this read names: a SNAPSHOT READ at a
	// point the client remembers rather than at whatever "now" is.
	//
	// # Why the sim needs these at all
	//
	// Without them every read is stamped at propose, so every read names a
	// timestamp above every version, and "the version visible at that timestamp"
	// is only ever tested at one timestamp: the newest. The MVCC read path would
	// be exercised in exactly the shape that cannot distinguish it from a
	// single-version store, and the garbage-collection mark would never refuse
	// anything -- a mechanism declared and never invoked, which is the class
	// this project has now counted eleven instances of.
	//
	// A6's snapshot-isolated transactions read this way for real. A5 issues them
	// from the workload so the path is exercised before the phase that depends
	// on it.
	ReadTS hlc.Timestamp

	// Range and Epoch are what the client BELIEVED when it routed this request.
	// They are the client's claim, not the cluster's fact, and the replica
	// checks them against its own descriptor -- which is the entire point of a
	// range epoch. Zero means "unrouted", which the sim uses for the first
	// attempt before any descriptor is cached.
	Range RangeID
	Epoch uint64
}

func (n *Replica) ordinalOf(id raft.NodeID) int {
	for i, p := range n.cfg.Peers {
		if p == id {
			return i
		}
	}
	return 0
}

// strs renders byte keys as strings for splitKeyFor, which works in the same
// space the client does.
func strs(bs [][]byte) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = string(b)
	}
	return out
}

// WritesRefused and ReadsRefused report commands the MVCC store declined for
// naming a timestamp at or below the garbage-collection mark.
func (n *Replica) WritesRefused() int     { return n.writesRefused }
func (n *Replica) GCProposed() int        { return n.gcProposed }
func (n *Replica) GCApplied() int         { return n.gcApplied }
func (n *Replica) VersionsCollected() int { return n.versionsCollected }
func (n *Replica) EnvelopeRefusals() int  { return n.envelopeRefusals }
func (n *Replica) ReadsRefused() int      { return n.readsRefused }
