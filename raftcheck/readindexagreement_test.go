package raftcheck

import (
	"testing"

	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/sim"
)

// TestReadIndexAgreementSpeaks induces the oracle directly, in milliseconds,
// before any seed search -- for the reason §40.3 gives about M62's detector: the
// SWEEP is the thing whose reach is uncertain, so establishing that the oracle
// can speak must not wait on one.
func TestReadIndexAgreementSpeaks(t *testing.T) {
	// The model: at index 10, key "k" held "v2".
	model := func(_ uint64, upto raft.Index, key string, _ hlc.Timestamp) (string, bool, bool) {
		if key != "k" {
			return "", false, true
		}
		if upto >= 10 {
			return "v2", true, true
		}
		return "v1", true, true
	}

	for _, c := range []struct {
		name   string
		rec    ReadRecord
		expect bool // a violation?
	}{
		{"an answer matching the log at the position the node reached",
			ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 10, Value: "v2", Found: true, OffLog: true}, false},
		{"an answer that does not match the log at that position",
			ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 10, Value: "v1", Found: true, OffLog: true}, true},
		{"an answer claiming absence where the log has a value",
			ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 10, Value: "", Found: false, OffLog: true}, true},
		// THE HALF M76 REMOVES: answered before its own apply reached the
		// confirmed index. The quorum established a position; this node had not
		// got there.
		{"answered on the quorum alone, before applying to the confirmed index",
			ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 4, Value: "v1", Found: true, OffLog: true}, true},
		// TOO FRESH IS NOT STALE. A node that applied past the confirmed index
		// may legitimately answer with a newer version, and calling that a
		// violation would accuse correct behaviour -- BUG-016's standard.
		{"applied PAST the confirmed index and answered with the newer version",
			ReadRecord{Range: 1, Key: "k", Index: 4, AppliedAt: 10, Value: "v2", Found: true, OffLog: true}, false},
		{"a REPLICATED read is not this oracle's business",
			ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 10, Value: "v1", Found: true, OffLog: false}, false},
		{"a REFUSED off-log read is an outcome, not an answer",
			ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 10, Refused: true, OffLog: true}, false},
	} {
		l := NewLedger(1)
		l.RecordRead(provenance.Witness(c.rec))
		o := NewReadIndexAgreement(l, model)
		got := o.Check()
		if (got != nil) != c.expect {
			t.Errorf("%s: violation=%v, want %v (%v)", c.name, got != nil, c.expect, got)
		}
	}
}

// TestReadIndexAgreementIsSilentWithoutAModel is BUG-016's standard: an oracle
// with no expectation must conclude nothing rather than accuse a correct run.
func TestReadIndexAgreementIsSilentWithoutAModel(t *testing.T) {
	l := NewLedger(1)
	l.RecordRead(provenance.Witness(ReadRecord{Range: 1, Key: "k", Index: 10, AppliedAt: 10, Value: "x",
		Found: true, OffLog: true}))
	if v := NewReadIndexAgreement(l, nil).Check(); v != nil {
		t.Errorf("an oracle with no model reported a violation: %v", v)
	}
}

// TestReadIndexAgreementCountsWhatItCompared is the non-vacuity witness. A green
// over zero comparisons is this register's commonest entry.
func TestReadIndexAgreementCountsWhatItCompared(t *testing.T) {
	model := func(uint64, raft.Index, string, hlc.Timestamp) (string, bool, bool) {
		return "v", true, true
	}
	l := NewLedger(1)
	o := NewReadIndexAgreement(l, model)
	if o.Check() != nil || o.Compared() != 0 {
		t.Fatalf("an empty ledger compared %d answers", o.Compared())
	}
	l.RecordRead(provenance.Witness(ReadRecord{Range: 1, Key: "k", Index: 1, AppliedAt: 1, Value: "v",
		Found: true, OffLog: true}))
	if o.Check() != nil {
		t.Fatal("a matching answer reported a violation")
	}
	if o.Compared() != 1 {
		t.Errorf("compared %d answers, want 1; a sweep that served no reads off the log "+
			"exercises none of this and its silence says nothing", o.Compared())
	}
}

var _ sim.Oracle = (*ReadIndexAgreement)(nil)
