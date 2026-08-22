package store

import (
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// nullTransport drops everything. This test never lets a message matter.
type nullTransport struct{}

func (nullTransport) Send(sim.Envelope) {}

// nullScheduler swallows the durability events applySplit schedules. The split
// itself is synchronous; only its persistence is not, and persistence is not
// what is under test here.
type nullScheduler struct{}

func (nullScheduler) At(clock.Instant, sim.Kind, sim.NodeID, any)    {}
func (nullScheduler) After(time.Duration, sim.Kind, sim.NodeID, any) {}
func (nullScheduler) Now() clock.Instant                             { return 0 }

// TestASupersededSplitIsRefused is the covering test for
// M47-superseded-split-applied-anyway, and it exists because the sweep cannot
// produce this schedule.
//
// # Why this is a direct induction and not a seed range
//
// Two leaders can each propose a split from the same extent, and both entries
// can commit at different indices. Applying the second would move the left
// range's End BACK past the first split, so two ranges would claim the same keys
// and their replicas would disagree about which entry is at index one of a range
// born twice. The epoch guard in applySplit exists for exactly that.
//
// Across 300 A4 seeds the guard fires **zero** times, and the reason is Raft's
// own figure-8 rule rather than luck: a new leader cannot commit a previous
// term's entry by counting, so it must append and commit an entry of its own
// first -- which commits the earlier split, which it then applies, which moves
// its extent before it can propose a second one. The race needs the first split
// committed but not yet applied, which is a narrow window the current schedule
// mix has never hit.
//
// A mechanism that is declared, wired and never invoked is the vacuous-green
// class this project has now recorded ten instances of, and "the sweep never
// reached it" is not a reason to leave the eleventh. So the schedule is built by
// hand: the guard is shown refusing a superseded split, and the mutant that
// removes it is killed here rather than by seed search. The sweep's count of
// zero is recorded in DESIGN-A4 §10 as an unexercised path, not as evidence.
func TestASupersededSplitIsRefused(t *testing.T) {
	m, err := New(Config{
		ID: 1, Peers: []raft.NodeID{1}, Ordinal: 0,
		Election: 10, Heartbeat: 3, SyncLatency: clock.Instant(1),
		Transport: nullTransport{}, Ledger: raftcheck.NewLedger(1),
		Clock: mustSimClock(t),
		Nodes: 1, SplitThreshold: 0,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	left := m.replicaOf(FirstRange)
	if left == nil {
		t.Fatal("the machine was born without its first range")
	}
	for i, k := range []string{"a", "b", "c", "d"} {
		b := engine.NewBatch()
		at := hlc.Timestamp{Wall: clock.NewWall(int64(100 + i))}
		if err := left.mvcc.PutInto(b, []byte(k), at, []byte(k)); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
		if _, err := m.db.Apply(b, false); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	holds := func(r *Replica, k string) bool {
		vs, err := r.mvcc.Versions()
		if err != nil {
			t.Fatalf("versions: %v", err)
		}
		for _, v := range vs {
			if string(v.Key) == k {
				return true
			}
		}
		return false
	}

	// The first split: cut at "c", against the extent the range actually has.
	first := SplitSpec{
		Key:   []byte("c"),
		Left:  RangeDescriptor{ID: FirstRange, Start: nil, End: []byte("c"), Epoch: 2},
		Right: RangeDescriptor{ID: 2, Start: []byte("c"), End: nil, Epoch: 1},
	}
	// Index 0: this replica's log is empty, and the only thing applySplit reads
	// the index for is the configuration in effect there. At 0 that is the
	// configuration the range was born with, which is what a hand-built schedule
	// can honestly say.
	m.applySplit(left, first, 0, 0, nullScheduler{})
	if got := len(m.replicas); got != 2 {
		t.Fatalf("the first split produced %d ranges, want 2", got)
	}
	if holds(left, "c") {
		t.Fatal("the first split left key \"c\" in the range that gave it away")
	}
	if left.desc.Epoch != 2 {
		t.Fatalf("the left range is at epoch %d after one split, want 2", left.desc.Epoch)
	}

	// The second split was computed against the SAME extent by a leader that
	// could not see the first. Applying it would move End from "c" back to "b"
	// and mint range 3 covering [b,∞) -- overlapping range 2 for everything at
	// or above "c".
	superseded := SplitSpec{
		Key:   []byte("b"),
		Left:  RangeDescriptor{ID: FirstRange, Start: nil, End: []byte("b"), Epoch: 2},
		Right: RangeDescriptor{ID: 3, Start: []byte("b"), End: nil, Epoch: 1},
	}
	before := left.StaleSplits()
	m.applySplit(left, superseded, 0, 0, nullScheduler{})

	if got := left.StaleSplits(); got != before+1 {
		t.Errorf("the superseded split was not counted as stale: %d refusals, want %d", got, before+1)
	}
	if got := len(m.replicas); got != 2 {
		t.Errorf("the superseded split created a range: %d ranges, want 2", got)
	}
	if m.replicaOf(3) != nil {
		t.Error("range 3 exists; a split every replica must refuse created a range that only " +
			"the replicas which applied it can see")
	}
	if string(left.desc.End) != "c" || left.desc.Epoch != 2 {
		t.Errorf("the superseded split moved the extent to %s; it must stay at [,c)@2", left.desc)
	}
	if !holds(left, "b") {
		t.Error("the superseded split moved key \"b\" out of a range that still owns it")
	}
}

// mustSimClock is a flat, drift-free timeline. This test is about a split's
// extent arithmetic; skew is the HLC's business and lives in hlc/.
func mustSimClock(t *testing.T) clock.Clock {
	t.Helper()
	c, err := clock.NewSim(clock.Flat(), time.Second)
	if err != nil {
		t.Fatalf("clock: %v", err)
	}
	return c
}

// counterSource is a timestamp source that is NOT an HLC: it hands out
// increasing timestamps from a counter and ignores every peer it is told about,
// which is roughly what a client of a central timestamp oracle sees.
type counterSource struct{ n int64 }

func (c *counterSource) Now() hlc.Timestamp {
	if c.n == 0 {
		// Start well above zero: the zero Timestamp means UNSET, and a source
		// whose first stamp is 1 leaves no timestamp that is genuinely "before
		// everything" for a test to ask about.
		c.n = 100
	}
	c.n++
	return hlc.Timestamp{Wall: clock.NewWall(c.n)}
}
func (c *counterSource) Update(t hlc.Timestamp) error {
	if int64(t.Wall) > c.n {
		c.n = int64(t.Wall)
	}
	return nil
}
func (c *counterSource) MaxOffset() time.Duration { return 0 }

// TestATimestampSourceCanBeSwapped is A5 exit criterion 3, and it is a test
// rather than an assertion for a reason.
//
// CLAUDE.md Amendment A6 pre-authorizes a TSO fallback if A6's uncertainty
// machinery is not green in time. An interface with one implementation is not a
// fallback -- it is a shape that happens to fit the only thing that has ever
// been put in it, and the first person to try a second one discovers the places
// that reached past it.
//
// So this drives the whole store on a source that is not an HLC: no wall clock,
// no logical counter, no envelope. Writes land, reads at a timestamp answer, and
// the extent arithmetic is untouched -- which is the claim "the fallback stays
// available" reduced to something that can fail.
func TestATimestampSourceCanBeSwapped(t *testing.T) {
	src := &counterSource{}
	m, err := New(Config{
		ID: 1, Peers: []raft.NodeID{1}, Ordinal: 0,
		Election: 10, Heartbeat: 3, SyncLatency: clock.Instant(1),
		Transport: nullTransport{}, Ledger: raftcheck.NewLedger(1),
		Nodes: 1, Clock: mustSimClock(t),
		NewTimestampSource: func(clock.Clock, uint32) (hlc.Source, error) { return src, nil },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	r := m.replicaOf(FirstRange)
	if r == nil {
		t.Fatal("no first range")
	}
	if _, ok := r.hlc.(*counterSource); !ok {
		t.Fatalf("the replica built an %T; the seam is not a seam", r.hlc)
	}

	// Three versions of one key, at three timestamps this source chose.
	var stamps []hlc.Timestamp
	for i, v := range []string{"a", "b", "c"} {
		at := r.hlc.Now()
		stamps = append(stamps, at)
		b := engine.NewBatch()
		if err := r.mvcc.PutInto(b, []byte("k"), at, []byte(v)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
		if _, err := m.db.Apply(b, false); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	for i, want := range []string{"a", "b", "c"} {
		got, ok, err := r.mvcc.ReadAt([]byte("k"), stamps[i])
		if err != nil || !ok || string(got) != want {
			t.Errorf("read at %s = (%q, %v, %v), want %q", stamps[i], got, ok, err, want)
		}
	}
	// And a read before the first version still finds nothing, which is the
	// answer that distinguishes "no version here" from "the newest one".
	if _, ok, err := r.mvcc.ReadAt([]byte("k"), hlc.Timestamp{Wall: clock.NewWall(1)}); err != nil || ok {
		t.Errorf("a read before every version returned one (ok=%v err=%v)", ok, err)
	}
}
