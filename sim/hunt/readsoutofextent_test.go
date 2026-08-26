package hunt_test

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestTheOutOfExtentReadCriterionSpeaks induces BUG-026's non-vacuity gate
// directly, in milliseconds, rather than waiting on a seed search.
//
// # Why a direct induction and not a mutant run
//
// The gate's failure condition is a COUNT being zero, and a count being zero is
// not something a sweep can be relied on to produce on demand -- the honest way
// to show the gate speaks is to hand it the census it exists to refuse.
// TestReadIndexAgreementSpeaks makes the same argument about the oracle: the
// sweep is the thing whose reach is uncertain, so establishing that an
// instrument CAN speak must not wait on one.
//
// The rate is measured separately and stated where the gate is: 9 firings across
// 240 clean seeds, about one seed in twenty-seven, so an aggregate of thousands
// reading zero is a broken path rather than a quiet one.
func TestTheOutOfExtentReadCriterionSpeaks(t *testing.T) {
	const want = "declined for naming a key its range had split away"

	fires := func(c hunt.RaftCensus) bool {
		for _, f := range exitCriteriaFailures(c) {
			if strings.Contains(f, want) {
				return true
			}
		}
		return false
	}

	for _, tc := range []struct {
		name   string
		census hunt.RaftCensus
		expect bool
	}{
		{"splits applied, read index on, and the reroute never fired",
			hunt.RaftCensus{Seeds: 100, ReadIndexRuns: 100, SplitsApplied: 500, ReadsOutOfExtent: 0}, true},
		{"the reroute fired, which is the path being exercised",
			hunt.RaftCensus{Seeds: 100, ReadIndexRuns: 100, SplitsApplied: 500, ReadsOutOfExtent: 1}, false},
		// Both guards matter, and each is a different reason the window cannot
		// open. A sweep with no splits has no key to move; a sweep with read
		// index off takes the replicated path, where the extent check is the
		// apply loop's and is counted as OutOfExtentRefusals instead.
		{"no splits, so the arrival-to-answer window cannot open",
			hunt.RaftCensus{Seeds: 100, ReadIndexRuns: 100, SplitsApplied: 0, ReadsOutOfExtent: 0}, false},
		{"read index off, so no read takes this path at all",
			hunt.RaftCensus{Seeds: 100, ReadIndexRuns: 0, SplitsApplied: 500, ReadsOutOfExtent: 0}, false},
	} {
		if got := fires(tc.census); got != tc.expect {
			t.Errorf("%s: criterion fired=%v, want %v", tc.name, got, tc.expect)
		}
	}
}
