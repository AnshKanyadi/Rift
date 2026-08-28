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
	r.onClient(Request{Op: "get", Key: "k", HistIdx: idx}, serveAt)
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

// leaderReplica returns a single-voter replica that has actually become leader,
// with its term-start no-op appended, committed and applied.
//
// The loopback scheduler is what makes that true rather than nearly true: a
// leader's own append counts toward its quorum only once the driver reports it
// durable, so without feeding the completion back the no-op never commits and
// every read waits on an index the log will never reach.
func leaderReplica(t *testing.T) (*Node, *Replica, *sim.History, *loopback) {
	t.Helper()
	m, r, hist := directedReplica(t)
	lb := &loopback{m: m}
	if err := r.raft.Campaign(); err != nil {
		t.Fatalf("campaign: %v", err)
	}
	r.drain(serveAt, lb)
	if r.raft.Role() != raft.RoleLeader {
		t.Fatalf("the single-voter replica did not become leader: %v", r.raft.Role())
	}
	return m, r, hist, lb
}

// TestATermStartNoOpMatchesNoStateMachineArm is D-A7-6's two propositions, and it
// replaces a 10,000-seed sweep with the entry itself.
//
// The no-op is `EntryNormal`, nil `Data`, the zero `ProposalID`. The apply
// switch's arms are `isTxnCommand`, `isSplitCommand` and `len(e.Data) > 0`, with
// NO default — so a dataless entry matches nothing by construction rather than by
// luck. This counts the construction being true, on an entry raft actually
// produced.
func TestATermStartNoOpMatchesNoStateMachineArm(t *testing.T) {
	_, r, hist, lb := leaderReplica(t)

	// # An ordinary command is applied too, and that is not decoration
	//
	// The mutation replaces the `len(e.Data) > 0` arm with a `default`, so a test
	// that applies only DATALESS entries never executes the line it changes --
	// `mutant-covered` called the first version of this test DEAD for exactly
	// that. The contrast is also what makes the assertion mean something: the
	// switch has to route a real command to that arm and the no-op to none.
	idx := hist.Begin(0, 0, 1, "put", "k", "v")
	r.onClient(Request{Op: "put", Key: "k", Value: "v", HistIdx: idx}, serveAt)
	r.drain(serveAt+1, lb)
	var applied bool
	for _, e := range hist.Events() {
		if e.Key == "k" && !e.InFlight() {
			applied = true
		}
	}
	if !applied {
		t.Fatal("the ordinary command was never applied, so the data arm was not executed and " +
			"this test covers only half the switch")
	}

	// Non-vacuity: becoming leader must have produced a no-op.
	if r.NoOpsApplied() == 0 {
		t.Fatal("no term-start no-op was applied, so the two zeros below are statements about " +
			"an entry that does not exist")
	}
	if got := r.NoOpReachedArm(); got != 0 {
		t.Errorf("a dataless entry matched %d state-machine arm(s). The apply switch has no "+
			"default precisely so this cannot happen; an arm that accepts the no-op applies a "+
			"COMMAND the cluster never issued (DESIGN-A7 section 3a.2)", got)
	}
	if got := r.NoOpAnswered(); got != 0 {
		t.Errorf("the no-op answered %d client operation(s). Its identity is the ZERO ProposalID, "+
			"which Propose refuses, so it can match no client's proposal -- and answering one "+
			"would tell a client its write succeeded when nothing of its was applied", got)
	}
}

// TestAReadIsNotAnsweredBeforeItsOwnApply is section 1.1's SECOND condition, and
// it is the half that gets forgotten because the hard-looking half — talking to a
// quorum — is already done.
func TestAReadIsNotAnsweredBeforeItsOwnApply(t *testing.T) {
	m, r, hist := directedReplica(t)
	put(t, m, r, "k", 100, "v")

	idx := hist.Begin(0, 0, 1, "get", "k", "")
	r.pendingReads = append(r.pendingReads, pendingRead{
		ctx: []byte("c1"), histIdx: idx, key: "k", index: 9,
	})
	r.applied = 3
	if r.applied >= 9 {
		t.Fatal("this replica has already applied past the confirmed index, so waiting is " +
			"vacuous here")
	}

	before := r.ReadsServed()
	r.serveReadyReads(serveAt, &recordingScheduler{})

	if got := r.ReadsServed(); got != before {
		t.Errorf("the read was answered with applied=%d against a confirmed index of 9.\n"+
			"  The quorum establishes a POSITION -- that this leader was still leader at or "+
			"after the read arrived -- and says nothing about whether THIS node has got there. "+
			"Answering before it has is reading your own past.", r.applied)
	}
	for _, e := range hist.Events() {
		if e.Key == "k" && !e.InFlight() {
			t.Errorf("the operation was ended as %v with %q before this replica applied to the "+
				"confirmed index", e.Outcome, e.Value)
		}
	}
	if len(r.pendingReads) != 1 {
		t.Errorf("the read was dropped rather than kept waiting: %d pending", len(r.pendingReads))
	}
}

// TestASnapshotReadKeepsItsLogEntry is D-A7-5's boundary, ruled A.
//
// A PLAIN read has no timestamp to protect. A SNAPSHOT read at T is a promise
// about T that a later commit can break, and BUG-022's third first-committer-wins
// guard rests on the read mark it leaves — a mark that is a function of the log
// ONLY because every such read is a log entry. A snapshot read answered off the
// log stages nothing and the guard consults a record that does not exist.
func TestASnapshotReadKeepsItsLogEntry(t *testing.T) {
	m, r, hist, _ := leaderReplica(t)
	put(t, m, r, "k", int64(r.hlc.Now().Wall)-1_000_000_000, "v")

	// A plain read takes the read-index path: the premise of the contrast.
	plain := hist.Begin(0, 0, 1, "get", "k", "")
	r.onClient(Request{Op: "get", Key: "k", HistIdx: plain}, serveAt)
	if len(r.pendingReads) != 1 {
		t.Fatalf("a PLAIN read did not take the read-index path (%d pending), so the contrast "+
			"below is not a contrast", len(r.pendingReads))
	}
	r.pendingReads = nil

	// A read naming a remembered timestamp must NOT.
	snap := hist.Begin(0, 0, 2, "get", "k", "")
	r.onClient(Request{Op: "get", Key: "k", HistIdx: snap,
		ReadTS: hlc.Timestamp{Wall: clock.NewWall(int64(r.hlc.Now().Wall) - 500_000_000)}}, serveAt)
	if len(r.pendingReads) != 0 {
		t.Errorf("a read naming a timestamp was queued on the READ-INDEX path (%d pending).\n"+
			"  It stages no read mark there, so BUG-022's guard consults a record that does not "+
			"exist and a prewrite that should be refused is accepted (D-A7-5).", len(r.pendingReads))
	}
}

// TestAReadStampIsNotBelowAnAcknowledgedWrite is ruling 3's gate, driven through
// the real `readFloor` rather than over a synthetic ledger.
//
// `TestReadIndexAtArrivalSpeaks` induces the ORACLE — it proves the instrument
// can speak. This proves the SYSTEM feeds it a sound stamp, which is a different
// question and the one `M83` attacks: `readFloor()` returning one index low is
// §5a.5's mutation, and it is invisible to a test that constructs its own ledger.
func TestAReadStampIsNotBelowAnAcknowledgedWrite(t *testing.T) {
	m, r, hist, lb := leaderReplica(t)

	// A write, acknowledged to a client. Its index is what any read issued
	// afterwards must reflect.
	w := hist.Begin(0, 0, 1, "put", "k", "v")
	r.onClient(Request{Op: "put", Key: "k", Value: "v", HistIdx: w}, serveAt)
	r.drain(serveAt+1, lb)

	acked := m.cfg.Ledger.Writes()
	if len(acked) == 0 {
		t.Fatal("no write was acknowledged, so there is nothing for a read stamp to be below and " +
			"this test would pass over an empty comparison")
	}

	// A read issued after it.
	g := hist.Begin(serveAt+2, 0, 2, "get", "k", "")
	r.onClient(Request{Op: "get", Key: "k", HistIdx: g}, serveAt+2)
	if len(r.pendingReads) != 1 {
		t.Fatalf("the read did not take the read-index path (%d pending)", len(r.pendingReads))
	}
	stamp := r.pendingReads[0].index
	if stamp == 0 {
		// Confirmation needs a round this single-voter harness does not run; the
		// stamp raft chose is still the thing under test, so read it directly.
		stamp = r.raft.ReadFloorForTest()
	}
	r.pendingReads[0].index = stamp
	r.applied = stamp
	r.serveReadyReads(serveAt+3, lb)

	for _, aw := range acked {
		if aw.Index > stamp {
			t.Errorf("a read issued at %d was stamped at index %d, but the write of %q at index "+
				"%d had already been acknowledged at %d.\n"+
				"  Arrival capture is the WEAKER of D-A7-3's two options, so the whole safety "+
				"case for the cheaper choice is that the stamp is a sound floor (DESIGN-A7 §5a).",
				int64(serveAt+2), stamp, aw.Key, aw.Index, int64(aw.AckedAt))
		}
	}
}
