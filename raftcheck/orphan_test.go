package raftcheck_test

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/raftcheck"
)

func at(w int64) hlc.Timestamp { return hlc.Timestamp{Wall: clock.NewWall(w)} }

// state builds one recovered range covering the whole keyspace, with a clock
// above everything so invariant 6 stays quiet and the case under test is the
// only thing that can speak.
func state(v []raftcheck.RecoveredVersion, l []raftcheck.RecoveredLock,
	w []raftcheck.CommitFact, d []raftcheck.CommitFact) []raftcheck.RecoveredState {
	return []raftcheck.RecoveredState{{
		Range: 1, Start: []byte(""), End: nil, Clock: at(1 << 40),
		Versions: v, Locks: l, Writes: w, Decided: d,
	}}
}

func check(t *testing.T, sts []raftcheck.RecoveredState) string {
	t.Helper()
	o := raftcheck.NewPercolatorInvariants(raftcheck.NewLedger(1),
		func() []raftcheck.RecoveredState { return sts })
	if v := o.Check(); v != nil {
		return v.Detail
	}
	return ""
}

// TestAnOrphanedVersionIsAViolation induces invariant 7.
//
// # Why induced here and not on a seed
//
// The invariant exists for a class the sweep does not reliably reach — a lock
// stolen at commit time leaves a version nobody can resolve and nobody can read,
// and whether any schedule produces that is a separate question being settled by
// measurement. An invariant whose only evidence is "no seed contradicted it" is
// an invariant nobody has seen fail, which is the shape this project distrusts.
// So the state is built directly.
func TestAnOrphanedVersionIsAViolation(t *testing.T) {
	commit := raftcheck.CommitFact{Key: "k", StartTS: at(100), CommitTS: at(200)}

	// The key is inside the domain because it carries a commit record, and it
	// holds a second version at 150 that nothing names. That is the orphan.
	got := check(t, state(
		[]raftcheck.RecoveredVersion{{Key: "k", At: at(100)}, {Key: "k", At: at(150)}},
		nil,
		[]raftcheck.CommitFact{commit},
		[]raftcheck.CommitFact{commit},
	))
	if !strings.Contains(got, "orphan") {
		t.Fatalf("an orphaned version was accepted; the oracle said %q", got)
	}
}

// TestTheAccountedShapesAreSilent is the other half: every legitimate way a
// version can exist has to pass, or the invariant is a source of false
// violations rather than an instrument.
func TestTheAccountedShapesAreSilent(t *testing.T) {
	commit := raftcheck.CommitFact{Key: "k", StartTS: at(100), CommitTS: at(200)}

	t.Run("a committed version", func(t *testing.T) {
		if got := check(t, state(
			[]raftcheck.RecoveredVersion{{Key: "k", At: at(100)}},
			nil,
			[]raftcheck.CommitFact{commit},
			[]raftcheck.CommitFact{commit},
		)); got != "" {
			t.Errorf("a committed version was reported: %s", got)
		}
	})

	t.Run("a prewritten version still holding its lock", func(t *testing.T) {
		if got := check(t, state(
			[]raftcheck.RecoveredVersion{{Key: "k", At: at(300)}},
			[]raftcheck.RecoveredLock{{Key: "k", Primary: "k", StartTS: at(300), Deadline: at(400)}},
			nil, nil,
		)); got != "" {
			t.Errorf("a live prewrite was reported: %s", got)
		}
	})

	// # The domain, induced
	//
	// A plain key carries a version and nothing else, because PutInto writes no
	// lock and no write record. It is outside the invariant rather than an
	// exception to it, and the unscoped first draft fired on exactly this shape
	// on seed 0.
	t.Run("a plain non-transactional version", func(t *testing.T) {
		if got := check(t, state(
			[]raftcheck.RecoveredVersion{{Key: "k", At: at(100)}, {Key: "k", At: at(150)}},
			nil, nil, nil,
		)); got != "" {
			t.Errorf("a plain version was reported: %s", got)
		}
	})
}
