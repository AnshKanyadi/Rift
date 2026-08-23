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
// `TestBUG023` replays the seed the bug was found on, and **both mutants survive
// it**: on that schedule the child inherits records, so the record-derived floor
// alone is enough, and it also carries the entry's value, so that alone is enough
// too. The seed cannot tell the halves apart because on it they are redundant.
//
// That is BUG-021's lesson arriving again — a decision in two halves needs a
// mutant per half, and a mutant per half needs a test per half. Each of these
// exercises a path the other cannot reach:
//
//	no records inherited  -> only the entry's value can seed  -> kills M69
//	records but no entry  -> only the record floor can seed   -> kills M70

// TestASplitChildWithNoRecordsStillInheritsTheClock kills M69.
//
// A child that inherits an EMPTY half has no records to derive a floor from, so
// the value the split entry carries is the only thing that can seed it. Without
// it the child starts at the local physical wall, which under skew is below the
// parent — and the first key written into that half is stamped in the past.
func TestASplitChildWithNoRecordsStillInheritsTheClock(t *testing.T) {
	phys := &pinnedWall{w: 1000}
	parent := hlc.Timestamp{Wall: 900000, Logical: 3<<hlc.IDBits | 1}
	if clock.Wall(1000) >= parent.Wall {
		t.Fatal("the parent must be above the physical clock or the floor cannot bind")
	}

	c, err := hlc.New(phys, 2)
	if err != nil {
		t.Fatal(err)
	}
	r := &Replica{hlc: c}

	// What applySplit does for a child with nothing in it: the entry's value,
	// then a record floor over an empty set.
	r.seedClockAtLeast(parent)
	r.seedClockAtLeast(maxVersionTimestamp([]byte("ns"), nil))

	got := c.Now()
	if !parent.Less(got) {
		t.Errorf("an empty child stamped %s, at or below the parent's %s. The entry's value is "+
			"the only floor an empty child has, and without it the first write into that half "+
			"lands in the past (BUG-023, M69)", got, parent)
	}
}

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
