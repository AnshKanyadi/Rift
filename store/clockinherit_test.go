package store

import (
	"testing"

	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
)

// pinnedWall is a physical clock that does not move, so these tests are about
// the logical counter and the inherited floor rather than about time passing.
type pinnedWall struct{ w clock.Wall }

func (p *pinnedWall) Wall() clock.Wall         { return p.w }
func (p *pinnedWall) Mono() clock.Mono         { return 0 }
func (p *pinnedWall) MaxOffset() time.Duration { return time.Second }

// The two halves of BUG-023's fix, isolated.
//
// # Why they need separating, and how I found out
//
// `TestBUG023` replays the seed the bug was found on, and the mutant survived
// it: on that schedule the child inherits records, so the floor binds through
// the payload and the seed cannot isolate the path. This one goes through the
// real `ingest`, which is what the mutant removes.
//
// The entry-carried floor and its mutant M69 are GONE: M69 could not be killed
// because the half it removed was unreachable, which made it a mutant reporting
// a dead path rather than a gap. One floor remains and this covers it.

// TestARangeIngestingRecordsRaisesItsClock kills M70.
//
// A range acquired from a SNAPSHOT never applies the split entry, so the entry's
// value never reaches it. The records are all it has, and the floor has to come
// from them.
func TestARangeIngestingRecordsRaisesItsClock(t *testing.T) {
	m, err := New(Config{
		ID: 1, Peers: []raft.NodeID{1}, Ordinal: 0,
		Election: 10, Heartbeat: 3, SyncLatency: clock.Instant(1),
		Transport: nullTransport{}, Ledger: raftcheck.NewLedger(1),
		Nodes: 1, Clock: mustSimClock(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	r := m.replicaOf(FirstRange)
	if r == nil {
		t.Fatal("no first range")
	}
	c := r.hlc

	ns := r.mvcc.Namespace()
	// ABOVE the physical clock, and INSIDE the envelope.
	//
	// Two corrections, both of which the first version got wrong. A wall of
	// 800000 against a simulated clock near 1.6e18 meant `Now()` was already far
	// above the records, so the floor never bound and the mutant survived a test
	// that could not fail. Then five seconds ahead meant `Update` REFUSED it as
	// beyond maxOffset, so the floor could not bind either — and that refusal is
	// correct: a parent more than maxOffset ahead of this node would itself have
	// been refused, so the case cannot arise in a run. A hundred milliseconds is
	// the realistic gap, and it is the one that has to work.
	high := hlc.Timestamp{Wall: c.Now().Wall.Add(100 * time.Millisecond), Logical: 5<<hlc.IDBits | 1}
	recs := []kv.Record{
		{Key: kv.EncodeKey(ns, []byte("k"), hlc.Timestamp{Wall: high.Wall.Add(-time.Millisecond), Logical: 1}), Value: []byte("a")},
		{Key: kv.EncodeKey(ns, []byte("k"), high), Value: []byte("b")},
	}

	// # Through the REAL ingest, not through the helper it calls
	//
	// The first version of this test called `seedClockAtLeast` inline, which is
	// where the mechanism lives rather than where the fact is — so removing the
	// real call site changed nothing and the mutant survived. That is the same
	// mistake M68 caught three hours earlier, made again. A test must go through
	// the path the defect removes.
	r.ingest(recs, hlc.Timestamp{})

	got := c.Now()
	if !high.Less(got) {
		t.Errorf("a range holding a version at %s stamped %s, at or below it. Its next read can "+
			"land under data it already has, and the write will be invisible (BUG-023, M70)",
			high, got)
	}
}

// TestARangeIngestingAReadMarkRaisesItsClock is the fifth record kind reaching
// the same invariant.
//
// It is not a restatement of the test above. A data version's timestamp is
// carried by a version the range can also answer with; a read mark's is carried
// by nothing else at all, so a range that ingests one and does not raise its
// clock will mint a start timestamp below a read it has already answered — and
// then every prewrite for that key is refused by BUG-022's own guard until
// physical time catches up. The window is the same 92ms window BUG-023 had.
func TestARangeIngestingAReadMarkRaisesItsClock(t *testing.T) {
	m, err := New(Config{
		ID: 1, Peers: []raft.NodeID{1}, Ordinal: 0,
		Election: 10, Heartbeat: 3, SyncLatency: clock.Instant(1),
		Transport: nullTransport{}, Ledger: raftcheck.NewLedger(1),
		Nodes: 1, Clock: mustSimClock(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	r := m.replicaOf(FirstRange)
	if r == nil {
		t.Fatal("no first range")
	}
	c := r.hlc
	ns := r.mvcc.Namespace()
	high := hlc.Timestamp{Wall: c.Now().Wall.Add(100 * time.Millisecond), Logical: 5<<hlc.IDBits | 1}

	// A read mark and NOTHING else: no version, no lock, no commit record. This
	// is the state a range reaches whenever a read arrives above every write the
	// key has ever had, which is the ordinary case for an account nobody has
	// touched yet.
	r.ingest([]kv.Record{{Key: kv.ReadMarkKey(ns, []byte("k"), high)}}, hlc.Timestamp{})

	if got := c.Now(); !high.Less(got) {
		t.Errorf("a range holding a read mark at %s stamped %s, at or below it. Every transaction "+
			"minted here would be refused by its own key's mark until physical time caught up "+
			"(BUG-022 meeting BUG-023)", high, got)
	}
}
