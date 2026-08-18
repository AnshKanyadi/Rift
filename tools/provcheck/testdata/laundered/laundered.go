// This package compiles, and must not survive tools/provcheck: it launders a
// reported fact into an observed one in a single expression, which is the only
// way past the type boundary and therefore the thing the text lane exists to
// catch.
package laundered

import (
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
)

func readBack() provenance.Reported[raft.HardState] { return provenance.Claim(raft.HardState{}) }

func Wire(l *raftcheck.Ledger) {
	hs := readBack()
	l.RecordDurable(0, provenance.Witness(hs.Unverified()), provenance.Witness([]raft.Entry(nil)), clock.Instant(1))
}
