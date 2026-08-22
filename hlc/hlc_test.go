package hlc_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/rng"
)

// fixed is a physical clock a test drives by hand. It is not a clock.Sim
// because these tests want the physical reading to move exactly when the test
// says, including backwards -- which a Sim timeline will not do and should not.
type fixed struct {
	wall clock.Wall
	max  time.Duration
}

func (f *fixed) Wall() clock.Wall         { return f.wall }
func (f *fixed) Mono() clock.Mono         { return 0 }
func (f *fixed) MaxOffset() time.Duration { return f.max }

func (f *fixed) advance(d clock.Wall) { f.wall += d }

func newAt(t *testing.T, ns int64, max time.Duration) (*hlc.Clock, *fixed) {
	t.Helper()
	f := &fixed{wall: clock.NewWall(ns), max: max}
	c, err := hlc.New(f, 0)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c, f
}

// TestNowIsStrictlyMonotonic: the first thing an HLC owes anybody.
//
// Including across a physical clock that does not move, which is the case the
// logical counter exists for -- and across one that moves BACKWARDS, which is
// what a wall clock does when NTP steps it.
func TestNowIsStrictlyMonotonic(t *testing.T) {
	c, f := newAt(t, 1_000_000, time.Second)
	prev := c.Now()

	for i := range 200 {
		switch i % 4 {
		case 0: // clock stands still: the logical counter must do the work
		case 1:
			f.wall = f.wall.Add(time.Nanosecond)
		case 2:
			f.wall = f.wall.Add(time.Millisecond)
		case 3: // clock steps BACKWARDS
			f.wall = f.wall.Add(-time.Microsecond)
		}
		got := c.Now()
		if !prev.Less(got) {
			t.Fatalf("step %d: Now returned %s after %s; an HLC that repeats a timestamp has "+
				"given two events the same position in the order", i, got, prev)
		}
		prev = got
	}
	if c.PhysicalRegressions() == 0 {
		t.Error("the physical clock never read behind the last timestamp, so the logical " +
			"tie-breaking half of the algorithm was never exercised and this test proved half of " +
			"what it claims")
	}
}

// TestUpdateOrdersAfterTheMessage is the causality property in its smallest
// form: a receive is after the send that caused it.
func TestUpdateOrdersAfterTheMessage(t *testing.T) {
	send, _ := newAt(t, 5_000_000, time.Second)
	recv, _ := newAt(t, 1_000_000, time.Second) // behind the sender

	for range 100 {
		m := send.Now()
		if err := recv.Update(m); err != nil {
			t.Fatalf("update: %v", err)
		}
		got := recv.Now()
		if !m.Less(got) {
			t.Fatalf("a receiver stamped %s for an event caused by a send at %s; the receive does "+
				"not order after the send, which is the one thing an HLC is for", got, m)
		}
	}
}

// TestCausalityUnderSkew is the property test: a randomized message schedule
// across nodes whose physical clocks disagree, asserting that every receive
// orders after its send and every node's own events order among themselves.
//
// # Why randomized rather than a fixed sequence
//
// The interesting failures need two nodes disagreeing about physical time in a
// pattern nobody thought to write down. The schedule comes from internal/rng
// with a pinned key, so a failure replays.
func TestCausalityUnderSkew(t *testing.T) {
	const nodes = 5
	const maxOffset = 250 * time.Millisecond

	key, err := rng.ParseKey("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	for seed := uint64(0); seed < 200; seed++ {
		// # The skew model, and the first version of it was wrong
		//
		// Physical clocks are spread across the envelope -- a test where every
		// node agrees is a test of nothing -- and each node's offset is redrawn
		// every step, so the skew CHANGES within the bound rather than being a
		// fixed per-node constant.
		//
		// The first version drifted each node independently by a random amount
		// per step. That diverges without bound: by step 285 of seed 4 two
		// nodes were 250.85ms apart under a 250ms envelope, and the test failed
		// on its own premise. Independent unbounded drift is an ENVELOPE
		// EXPERIMENT -- deliberately exceeding maxOffset to see what breaks,
		// which is STRETCH -- and using it here would have been asserting the
		// bounded property against an unbounded schedule.
		//
		// A shared base with per-node offsets inside the bound keeps every pair
		// within maxOffset by construction, which is what "bounded skew" means.
		cs := make([]*hlc.Clock, nodes)
		fs := make([]*fixed, nodes)
		base := 10 * time.Second
		offset := func(step uint64, i int) time.Duration {
			return time.Duration(key.Uint64N(1, seed, step, uint64(i), uint64(maxOffset))) - maxOffset/2
		}
		for i := range cs {
			cs[i], fs[i] = newAt(t, int64(base+offset(0, i)), maxOffset)
		}

		// last[i] is the most recent timestamp node i issued, which is what
		// program order on that node has to advance past.
		last := make([]hlc.Timestamp, nodes)

		for step := uint64(0); step < 300; step++ {
			from := int(key.Uint64N(2, seed, step, 0, nodes))
			to := int(key.Uint64N(3, seed, step, 1, nodes))

			// Time advances for everybody, and each node's offset inside the
			// envelope is redrawn, so the skew moves without the spread ever
			// exceeding the bound.
			base += time.Duration(key.Uint64N(4, seed, step, 2, uint64(2*time.Millisecond)))
			for i := range fs {
				fs[i].wall = clock.NewWall(int64(base + offset(step+1, i)))
			}

			m := cs[from].Now()
			if last[from].IsSet() && !last[from].Less(m) {
				t.Fatalf("seed %d step %d: node %d issued %s after %s; program order broken on one node",
					seed, step, from, m, last[from])
			}
			last[from] = m

			if to == from {
				continue
			}
			if err := cs[to].Update(m); err != nil {
				if errors.Is(err, hlc.ErrBeyondEnvelope) {
					// Within a bounded run this must not happen: two nodes
					// inside maxOffset of a common physical time are within
					// maxOffset of each other by construction.
					t.Fatalf("seed %d step %d: %d refused %d's timestamp inside the envelope: %v",
						seed, step, to, from, err)
				}
				t.Fatalf("seed %d step %d: update: %v", seed, step, err)
			}
			r := cs[to].Now()
			if !m.Less(r) {
				t.Fatalf("seed %d step %d: node %d stamped %s for a receive of %s from node %d",
					seed, step, to, r, m, from)
			}
			last[to] = r
		}
	}
}

// TestATimestampBeyondTheEnvelopeIsRefused is the covering test for
// M49-hlc-accepts-any-timestamp.
//
// A node whose clock has jumped forward past maxOffset sends a timestamp the
// receiver cannot reconcile. Classic HLC takes the max regardless, which drags
// the whole cluster's physical time forward on one broken node's say-so and
// silently voids every bound that rests on maxOffset.
func TestATimestampBeyondTheEnvelopeIsRefused(t *testing.T) {
	c, f := newAt(t, 1_000_000_000, 100*time.Millisecond)
	before := c.Now()

	far := hlc.Timestamp{Wall: f.wall.Add(101 * time.Millisecond)}
	err := c.Update(far)
	if !errors.Is(err, hlc.ErrBeyondEnvelope) {
		t.Fatalf("a timestamp %s past the bound was accepted (err=%v); one node's broken clock now "+
			"defines physical time for the cluster", 101*time.Millisecond, err)
	}
	if c.UpdatesRefused() != 1 {
		t.Errorf("the refusal was not counted: %d", c.UpdatesRefused())
	}

	// And the refusal must leave the clock ALONE. A refusal that had already
	// moved the clock would be an acceptance with an error attached.
	if got := c.Now(); far.LessEq(got) {
		t.Errorf("after refusing %s the clock issued %s, which is at or past it; the refusal "+
			"absorbed the timestamp it declined", far, got)
	}
	if !before.Less(c.Last()) && c.Last() != before {
		t.Errorf("clock went backwards across a refusal: %s then %s", before, c.Last())
	}

	// Exactly at the bound is INSIDE it: maxOffset is the largest disagreement
	// the cluster promises, not the smallest it refuses.
	if err := c.Update(hlc.Timestamp{Wall: f.wall.Add(100 * time.Millisecond)}); err != nil {
		t.Errorf("a timestamp exactly at the bound was refused: %v", err)
	}
}

// TestTheEnvelopeIsMeasuredAgainstThePhysicalClock is the covering test for
// M50-envelope-measured-against-the-hlc.
//
// Checking a peer's timestamp against c.last instead of the physical reading
// compounds: once one over-eager timestamp is accepted, the next is measured
// against the inflated value and the bound ratchets outward forever. The
// physical clock is the only value in Update that is not downstream of a peer's
// claim.
func TestTheEnvelopeIsMeasuredAgainstThePhysicalClock(t *testing.T) {
	const bound = 100 * time.Millisecond
	c, f := newAt(t, 1_000_000_000, bound)

	// Walk a peer forward in steps that are each inside the bound relative to
	// the HLC's own last value, but which together leave the physical clock far
	// behind. Measured against c.last every one of these is legal; measured
	// against the physical clock, all but the first must be refused.
	accepted := 0
	for i := 1; i <= 10; i++ {
		peer := hlc.Timestamp{Wall: f.wall.Add(time.Duration(i) * 90 * time.Millisecond)}
		if err := c.Update(peer); err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("%d of 10 ratcheting timestamps were accepted, want 1. The bound is being "+
			"measured against a value the peers themselves moved, so it is not a bound", accepted)
	}
	if drift := c.Last().Wall.Sub(f.wall); drift > bound {
		t.Errorf("the clock ended %s ahead of its own physical reading, past the %s bound", drift, bound)
	}
}

// TestTwoNodesNeverMintTheSameTimestamp is BUG-021's first half, induced.
//
// # What it would look like without the tag
//
// Two clocks over ONE physical clock is the worst case and the realistic one:
// nodes whose clocks a hold has pulled together, HLCs sitting at the same
// logical counter because they have seen the same traffic. Before the tag, that
// configuration produced identical timestamps freely -- and a transaction's
// start timestamp is what its lock and its data version are addressed by, so two
// transactions then shared a key.
//
// The union of everything two differently-tagged clocks mint must contain no
// value twice, however their calls interleave.
func TestTwoNodesNeverMintTheSameTimestamp(t *testing.T) {
	phys := &fixed{}
	a, err := hlc.New(phys, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hlc.New(phys, 2)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[hlc.Timestamp]string{}
	note := func(who string, ts hlc.Timestamp) {
		t.Helper()
		if prev, dup := seen[ts]; dup {
			t.Fatalf("%s and %s both minted %s. Two transactions started at one timestamp share "+
				"the lock and the data version of every key they both touch, which is BUG-021",
				prev, who, ts)
		}
		seen[ts] = who
	}

	// Interleaved minting, and interleaved absorbing: Update is the path the
	// four-case version lost the tag on, so it gets the same exercise as Now.
	for i := 0; i < 200; i++ {
		note("a", a.Now())
		note("b", b.Now())
		if i%3 == 0 {
			if err := a.Update(b.Last()); err != nil {
				t.Fatalf("a absorbing b: %v", err)
			}
			note("a", a.Now())
		}
		if i%5 == 0 {
			if err := b.Update(a.Last()); err != nil {
				t.Fatalf("b absorbing a: %v", err)
			}
			note("b", b.Now())
		}
		if i%7 == 0 {
			phys.advance(1)
		}
	}
	if len(seen) < 400 {
		t.Errorf("only %d distinct timestamps; the loop is not exercising what it thinks", len(seen))
	}

	// And the tag says who, which is what makes a collision diagnosable rather
	// than merely impossible.
	if got := hlc.NodeOf(a.Last()); got != 1 {
		t.Errorf("a's last timestamp is tagged %d, want 1", got)
	}
	if got := hlc.NodeOf(b.Last()); got != 2 {
		t.Errorf("b's last timestamp is tagged %d, want 2", got)
	}
}

// TestAbsorbingAPeerKeepsThisNodesTag is the path the tag is easiest to lose on.
//
// Update folds in a peer's timestamp, and the version this replaced set c.last
// to the peer's value outright on three of its four branches. One absorbed
// message was then enough for every subsequent timestamp to carry a foreign tag
// -- so the property would hold in a quiet test and fail in a cluster.
func TestAbsorbingAPeerKeepsThisNodesTag(t *testing.T) {
	phys := &fixed{}
	me, err := hlc.New(phys, 3)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := hlc.New(phys, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		peer.Now()
		if err := me.Update(peer.Last()); err != nil {
			t.Fatalf("update: %v", err)
		}
		ts := me.Now()
		if got := hlc.NodeOf(ts); got != 3 {
			t.Fatalf("after absorbing node 4, node 3 minted %s tagged %d. Every timestamp it "+
				"issues from here collides with node 4's", ts, got)
		}
		if !peer.Last().Less(ts) {
			t.Fatalf("minted %s, which is not above the peer's %s", ts, peer.Last())
		}
	}
}
