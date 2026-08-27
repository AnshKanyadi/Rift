package store

import (
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// Directed covering tests for A7's read-index defects.
//
// # Why directed and not a sweep
//
// Every one of these classes named `TestRaftExitCriteria` when it was declared —
// a TEN THOUSAND seed sweep, 19 hours per invocation at the exit run's measured
// 6.9 s/seed. Seven mutants named it, so `make mutants` was asking for six days
// of machine time for that covering test alone, and `make mutants` is in the `ci`
// target.
//
// That is the disease this phase diagnosed twice in other people's mutants — a
// covering test built on a sweep stops covering when the workload moves, and it
// does it silently — and then reproduced in four mutants declared while writing
// the general form. These are the replacements: each arranges its precondition
// directly, asserts that it arranged it, and answers in milliseconds.

// serveAt is a nonzero instant. Zero is the in-flight sentinel in a history, so
// serving there makes "answered" and "never answered" the same observation.
const serveAt = clock.Instant(1_000_000)

// loopback is the smallest scheduler that lets a single-voter replica make
// progress: it feeds durability completions straight back to the machine.
//
// Without it nothing ever commits. A leader's own append counts toward its
// quorum only once the driver reports it DURABLE, and a scheduler that drops the
// completion leaves the term-start no-op uncommitted forever -- so a read waits
// on an index its own log will never reach. That is the durability contract
// working, and a directed test has to satisfy it rather than route around it.
type loopback struct {
	m     *Node
	depth int
}

func (l *loopback) At(at clock.Instant, k sim.Kind, n sim.NodeID, p any) {
	if k != sim.KindDurable || l.depth > 8 {
		return
	}
	l.depth++
	l.m.Handle(sim.Event{At: at, Kind: k, Node: n, Payload: p}, l)
	l.depth--
}
func (l *loopback) After(time.Duration, sim.Kind, sim.NodeID, any) {}
func (l *loopback) Now() clock.Instant                             { return 0 }

// recordingScheduler captures what a reroute reissues.
type recordingScheduler struct{ sent []any }

func (r *recordingScheduler) At(clock.Instant, sim.Kind, sim.NodeID, any)    {}
func (r *recordingScheduler) After(time.Duration, sim.Kind, sim.NodeID, any) {}
func (r *recordingScheduler) Now() clock.Instant                             { return 0 }

type capturingScheduler struct{ reqs []Request }

func (c *capturingScheduler) At(_ clock.Instant, _ sim.Kind, _ sim.NodeID, p any) {
	if req, ok := p.(Request); ok {
		c.reqs = append(c.reqs, req)
	}
}
func (c *capturingScheduler) After(time.Duration, sim.Kind, sim.NodeID, any) {}
func (c *capturingScheduler) Now() clock.Instant                             { return 0 }

func directedReplica(t *testing.T) (*Node, *Replica, *sim.History) {
	t.Helper()
	hist := sim.NewHistory()
	m, err := New(Config{
		ID: 1, Peers: []raft.NodeID{1}, Ordinal: 0,
		Election: 10, Heartbeat: 3, SyncLatency: clock.Instant(1),
		Transport: nullTransport{}, Ledger: raftcheck.NewLedger(1),
		Clock: mustSimClock(t), History: hist,
		Nodes: 1, SplitThreshold: 0, ReadIndex: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	r := m.replicaOf(FirstRange)
	if r == nil {
		t.Fatal("the machine was born without its first range")
	}
	return m, r, hist
}

func put(t *testing.T, m *Node, r *Replica, key string, wall int64, val string) {
	t.Helper()
	b := engine.NewBatch()
	if err := r.mvcc.PutInto(b, []byte(key), hlc.Timestamp{Wall: clock.NewWall(wall)}, []byte(val)); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
	if _, err := m.db.Apply(b, false); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

// TestAReadIsRerouted WhenItsRangeNoLongerHoldsTheKey is BUG-026, directed.
//
// The window is arrival-to-answer: a read routed to the range that owned its key,
// then answered after that range split the key away. It is a WINDOW, not a state,
// which is why a sweep reaches it on about one seed in twenty-seven and this
// test reaches it every time.
func TestAReadIsReroutedWhenItsRangeNoLongerHoldsTheKey(t *testing.T) {
	m, r, hist := directedReplica(t)
	put(t, m, r, "k", 100, "v")

	idx := hist.Begin(0, 0, 1, "get", "k", "")
	r.pendingReads = append(r.pendingReads, pendingRead{
		ctx: []byte("c1"), histIdx: idx, key: "k", index: 1,
	})
	r.applied = 1

	// The split happens between arrival and answer: the extent stops covering
	// the key. Asserted, so the test cannot pass over a range that still holds it.
	r.desc.End = []byte("j")
	if r.desc.Contains([]byte("k")) {
		t.Fatal("the extent still covers the key, so this test arranged no window")
	}

	// # Served at a NONZERO instant, and it is not cosmetic
	//
	// `HistoryEvent.InFlight()` is `Return == 0`, so an operation ended AT
	// instant zero is indistinguishable from one never answered — and the
	// assertion below is that the operation was NOT ended. At instant zero it
	// would hold whatever happened, which is this test passing over nothing.
	sched := &capturingScheduler{}
	before := r.ReadsOutOfExtent()
	r.serveReadyReads(serveAt, sched)

	if got := r.ReadsOutOfExtent(); got != before+1 {
		t.Errorf("the read was not counted as out of extent: %d, want %d", got, before+1)
	}
	for _, e := range hist.Events() {
		if e.Key == "k" && !e.InFlight() {
			t.Errorf("the operation was ENDED in the history as %v with %q. Nothing told the "+
				"client anything: the range does not hold this key, so the answer would describe "+
				"the absence of a RANGE and be delivered as the absence of a VALUE (BUG-026)",
				e.Outcome, e.Value)
		}
	}
	if len(sched.reqs) != 1 {
		t.Fatalf("the read was not reissued: %d requests scheduled, want 1", len(sched.reqs))
	}
	if q := sched.reqs[0]; q.Key != "k" || q.Op != "get" || q.Range != 0 || q.Epoch != 0 {
		t.Errorf("the reissued request is %+v; it must be the same question, unrouted, so the "+
			"machine routes it by extent the way a refreshed client would", q)
	}
}

// TestAPlainReadIsAnsweredAtTheLatestVersion is BUG-028, directed.
//
// A plain read has no timestamp to protect — that sentence is D-A7-5's, and it is
// the argument that let this read leave the log. Answering it at a clock reading
// puts the clock back into the one mechanism chosen for not needing one.
func TestAPlainReadIsAnsweredAtTheLatestVersion(t *testing.T) {
	m, r, hist := directedReplica(t)

	// # Driven through onClient and drain, not by appending a pendingRead
	//
	// The first version of this test built the pendingRead by hand and
	// `mutant-covered` called it DEAD: the mutation also changes the QUEUE site
	// in onClient, and a test that constructs the queue entry itself goes around
	// the line it is meant to cover. Killing a mutant and executing the line it
	// changes are different questions, and this lane asks the second.
	lb := &loopback{m: m}
	if err := r.raft.Campaign(); err != nil {
		t.Fatalf("campaign: %v", err)
	}
	r.drain(serveAt, lb)
	if r.raft.Role() != raft.RoleLeader {
		t.Fatalf("the single-voter replica did not become leader: %v", r.raft.Role())
	}

	// Two versions. The newer is stamped ABOVE this replica's own clock, which is
	// what skew inside maxOffset produces between the leader that stamped a write
	// and the follower answering a read. Derived FROM the clock rather than
	// picked: the first attempt picked a constant that sat below it, and the
	// guard below refused to run.
	base := int64(r.hlc.Now().Wall)
	put(t, m, r, "k", base-1_000_000_000, "old")
	ahead := base + 1_000_000_000
	put(t, m, r, "k", ahead, "new")

	if now := r.hlc.Now(); !now.Less(hlc.Timestamp{Wall: clock.NewWall(ahead)}) {
		t.Fatalf("this replica's clock (%s) is not below the newer version's timestamp, so the "+
			"test arranged no skew and would pass whatever the read is answered at", now)
	}

	idx := hist.Begin(0, 0, 1, "get", "k", "")
	r.onClient(Request{Op: "get", Key: "k", HistIdx: idx})
	if len(r.pendingReads) != 1 {
		t.Fatalf("onClient queued %d reads, want 1; the read-index path was not taken and this "+
			"test would assert nothing", len(r.pendingReads))
	}

	// # The confirmed index is supplied, and that is the ONLY hand-set value
	//
	// In a cluster it arrives as a ReadState once a quorum answers the
	// leadership round; a single voter needs a heartbeat cycle to produce one,
	// and this test is not about how the index is confirmed -- `M76` is, and it
	// has its own. Everything the mutation touches is reached through the real
	// path: the queue site in onClient above, and the answer site below.
	r.pendingReads[0].index = 1
	r.applied = 1
	r.serveReadyReads(serveAt+1, lb)

	var got string
	var ended bool
	for _, e := range hist.Events() {
		if e.Key == "k" && !e.InFlight() {
			got, ended = e.Value, true
		}
	}
	if !ended {
		t.Fatal("the read was never answered")
	}
	if got != "new" {
		t.Errorf("the read returned %q; the latest version is %q.\n"+
			"  ReadAt returns the newest version AT OR BEFORE a timestamp, so answering a plain "+
			"read at this replica's own clock makes every write stamped above that clock "+
			"invisible -- however far past the confirmed index this replica has applied "+
			"(BUG-028).", got, "new")
	}
}

// TestASnapshotInstallAdvancesTheAppliedIndex is BUG-032, directed.
//
// `n.applied` is what says where this replica's state machine is. It was written
// in exactly one place — the committed-entry branch — so a snapshot install moved
// the state machine forward and left the number behind.
func TestASnapshotInstallAdvancesTheAppliedIndex(t *testing.T) {
	m, r, _ := directedReplica(t)
	put(t, m, r, "k", 100, "v")

	const snapIndex = raft.Index(9)
	data := encodeMachine(r.desc, r.mvcc.GCMark(), r.versions())
	if err := r.raft.Step(raft.Message{
		Type: raft.MsgSnap, From: 1, To: 1, Term: 1,
		SnapIndex: snapIndex, SnapTerm: 1, SnapData: data,
		SnapConf: raft.EncodeConfiguration(r.raft.Configuration()),
	}); err != nil {
		t.Fatalf("step snapshot: %v", err)
	}
	if r.applied >= snapIndex {
		t.Fatalf("this replica already reports applied=%d before the install, so the test would "+
			"pass without the install doing anything", r.applied)
	}

	r.drain(0, &recordingScheduler{})

	if r.applied < snapIndex {
		t.Errorf("after installing a snapshot at index %d this replica reports applied=%d.\n"+
			"  The state machine is AT the snapshot's index and nothing else will say so: no "+
			"committed batch covers it. Reads wait on this number, so a read the snapshot "+
			"already answers waits for an entry that may never arrive; and the ledger records "+
			"it as AppliedAt, so the differential oracle replays the log to a position below "+
			"the node's real state and accuses a correct answer (BUG-032).",
			snapIndex, r.applied)
	}
}
