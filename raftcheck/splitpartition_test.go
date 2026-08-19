package raftcheck_test

import (
	"testing"

	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// noSplits and anyExtent are the two functions SplitPartition takes from the
// harness. This test builds a ledger with no split entries at all, so the
// decoders never have anything to decode: what is under test is the clause about
// a range EXISTING, not the clause about what a split said.
func noSplits([]byte, []raft.Entry) []raftcheck.SplitStep { return nil }

func anyExtent(b []byte) (start, end []byte, epoch uint64, ok bool) {
	return nil, nil, 1, len(b) > 0
}

// TestARangeNoLogEverCreatedIsAViolation induces split-partition's orphan
// clause, and it is induced here rather than by a mutant for a measured reason.
//
// # What the clause says
//
// Every range except the first was created by a split entry in some range's
// committed log. A range that exists without one is claimed by whoever created
// it and by whatever range still believes it owns those keys, and no oracle that
// judges a single range can see it: both of them are perfectly consistent with
// their own histories.
//
// # Why not a mutant
//
// M48 plants exactly this defect -- the leader creates the right-hand range out
// of band, which is candidate design (1) that DESIGN-A4 section 4 rejected. It is
// caught on 276 of 300 seeds and **never by this clause**: an out-of-band range
// derives its birth state at a different moment from every other replica's, so
// snapshot equivalence reports a divergence long before anything notices the
// range should not exist at all.
//
// That is worth stating plainly rather than hiding behind a mutant that kills
// for the wrong reason. Against the mutations this system admits, the clause is
// **redundant**: something else always gets there first. It is kept because the
// something-else is a coincidence of how ranges derive their state, and a clause
// that costs one pass over the ledger is cheap insurance against the coincidence
// ending. Its failure is induced here so that the clause is not a claim nobody
// has ever seen fire.
func TestARangeNoLogEverCreatedIsAViolation(t *testing.T) {
	l := raftcheck.NewLedger(3)

	// Range 1 is born with the machine and no split creates it. It must not be
	// reported, or the clause would fire on every run ever made.
	l.RecordRangeBase(1, provenance.Witness([]byte("first")), provenance.Witness([]byte("conf")))

	o := raftcheck.NewSplitPartition(l, noSplits, anyExtent)
	if v := o.OnStep(nil, sim.Event{}); v != nil {
		t.Fatalf("the first range was reported as an orphan: %s", v.Detail)
	}

	// Range 7 exists -- some replica wrote its birth state -- and no committed
	// log anywhere contains the split that created it.
	l.RecordRangeBase(7, provenance.Witness([]byte("orphan")), provenance.Witness([]byte("conf")))

	v := o.OnStep(nil, sim.Event{})
	if v == nil {
		t.Fatal("a range that no committed log ever created was not reported; the clause is a " +
			"claim nobody has ever seen fire")
	}
	if v.Checker != "split-partition" {
		t.Errorf("reported by %q, want split-partition", v.Checker)
	}
}
