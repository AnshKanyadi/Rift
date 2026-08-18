// This package must NOT compile. It is the induction for the provenance types:
// it feeds an engine read-back — the system's own account of what it holds —
// into the ledger the safety oracles read, which is exactly the wiring that made
// the persist-before-reply oracle silent.
//
// tools/provcheck builds it and requires the build to fail.
package reported

import (
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
)

// readBack stands in for store.readDurable: what the engine says it holds.
func readBack() (provenance.Reported[raft.HardState], provenance.Reported[[]raft.Entry]) {
	return provenance.Claim(raft.HardState{}), provenance.Claim([]raft.Entry(nil))
}

func Wire(l *raftcheck.Ledger) {
	hs, entries := readBack()
	l.RecordDurable(0, hs, entries, clock.Instant(1))
}
