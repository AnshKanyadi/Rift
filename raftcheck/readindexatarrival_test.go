package raftcheck

import (
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/sim"
)

// TestReadIndexAtArrivalSpeaks induces ruling 3's oracle directly, in
// milliseconds, before any seed search.
//
// Same argument as TestReadIndexAgreementSpeaks: the SWEEP is the thing whose
// reach is uncertain, so establishing that an instrument can speak must not wait
// on one. DESIGN-A7 §5a.4.
func TestReadIndexAtArrivalSpeaks(t *testing.T) {
	read := func(rng uint64, key string, stamp raft.Index, issued clock.Instant) ReadRecord {
		return ReadRecord{Range: rng, Key: key, Index: stamp, AppliedAt: stamp,
			IssuedAt: issued, OffLog: true, Found: true}
	}
	write := func(rng uint64, key string, idx raft.Index, acked clock.Instant) WriteRecord {
		return WriteRecord{Range: rng, Key: key, Index: idx, AckedAt: acked}
	}

	for _, c := range []struct {
		name   string
		writes []WriteRecord
		reads  []ReadRecord
		expect bool
	}{
		{"a read stamped above an already-acknowledged write",
			[]WriteRecord{write(1, "a", 5, 100)}, []ReadRecord{read(1, "b", 7, 200)}, false},
		{"a read stamped exactly at the acknowledged write's index",
			[]WriteRecord{write(1, "a", 5, 100)}, []ReadRecord{read(1, "b", 5, 200)}, false},
		// THE DEFECT. `i - 1` is §5a.5's mutation and this is the shape it makes:
		// the write was promised to a client, and the read that follows is
		// answerable at a position below it.
		{"a read stamped BELOW a write already acknowledged when it was issued",
			[]WriteRecord{write(1, "a", 5, 100)}, []ReadRecord{read(1, "b", 4, 200)}, true},
		// A write acknowledged AFTER the read was issued is not this oracle's
		// business: the read is entitled to miss it, which is what "before that
		// read was issued" means and what a check without the instant would get
		// wrong in the direction of false accusation.
		{"a write acknowledged after the read was issued",
			[]WriteRecord{write(1, "a", 9, 300)}, []ReadRecord{read(1, "b", 4, 200)}, false},
		// Log indices are per range. Comparing across them manufactures
		// violations out of correct behaviour, and after a split the right-hand
		// range legitimately starts at a low index.
		{"a write on ANOTHER range at a higher index",
			[]WriteRecord{write(2, "a", 90, 100)}, []ReadRecord{read(1, "b", 4, 200)}, false},
		{"a REPLICATED read is not this oracle's business",
			[]WriteRecord{write(1, "a", 5, 100)},
			[]ReadRecord{{Range: 1, Key: "b", Index: 4, IssuedAt: 200, OffLog: false}}, false},
		{"a REFUSED off-log read is an outcome, not an answer",
			[]WriteRecord{write(1, "a", 5, 100)},
			[]ReadRecord{{Range: 1, Key: "b", Index: 4, IssuedAt: 200, OffLog: true, Refused: true}}, false},
	} {
		l := NewLedger(1)
		for _, w := range c.writes {
			l.RecordWrite(provenance.Witness(w))
		}
		for _, r := range c.reads {
			l.RecordRead(provenance.Witness(r))
		}
		o := NewReadIndexAtArrival(l)
		if got := o.Check(); (got != nil) != c.expect {
			t.Errorf("%s: violation=%v, want %v (%v)", c.name, got != nil, c.expect, got)
		}
	}
}

// TestReadIndexAtArrivalCountsWhatItCompared is the non-vacuity witness. A sweep
// in which no read was ever issued after an acknowledged write on its own range
// compares nothing, and a green over zero comparisons says nothing at all.
func TestReadIndexAtArrivalCountsWhatItCompared(t *testing.T) {
	l := NewLedger(1)
	o := NewReadIndexAtArrival(l)
	if o.Check() != nil || o.Compared() != 0 {
		t.Fatalf("an empty ledger compared %d pairs", o.Compared())
	}
	l.RecordWrite(provenance.Witness(WriteRecord{Range: 1, Key: "a", Index: 5, AckedAt: 100}))
	l.RecordRead(provenance.Witness(ReadRecord{Range: 1, Key: "b", Index: 7, AppliedAt: 7,
		IssuedAt: 200, OffLog: true, Found: true}))
	if o.Check() != nil {
		t.Fatal("a sound stamp reported a violation")
	}
	if o.Compared() != 1 {
		t.Errorf("compared %d pairs, want 1; without this an oracle that never reached a "+
			"comparison reads identically to one that made them all", o.Compared())
	}
}

var _ sim.Oracle = (*ReadIndexAtArrival)(nil)
