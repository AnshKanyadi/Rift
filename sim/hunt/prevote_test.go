package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestPreVoteAblation is pre-vote's exit criterion: a measured difference, not a
// claim.
//
// # What is being measured and why the geometry matters
//
// A SINGLE directed cut leaves a node that can send but not receive. It never
// hears a heartbeat, so it campaigns; its vote request arrives carrying a term
// above everyone else's, so the whole cluster steps down and re-elects; it still
// hears nothing, so it campaigns again, one term higher. Each cycle costs a real
// election and a real leader.
//
// A symmetric cut cannot produce this: a cleanly isolated node's requests never
// arrive, so its term climbs privately and rejoins harmlessly. DESIGN-A0.7
// blessed directed partitions with a forward binding for exactly this, A1 spent
// half of it weighting the mix, and this is the other half.
//
// Pre-vote's whole value is that the disrupted node asks first and is refused by
// peers that have heard from a leader recently, so the term does not move.
//
// # The two arms are the same schedules
//
// PreVote is a BUILD parameter, not a plan entry, so both arms materialize
// byte-identical plans and see the same cuts at the same instants. A difference
// in the numbers is therefore a difference in the protocol and not in the
// weather.
func TestPreVoteAblation(t *testing.T) {
	const seeds = 200

	type arm struct {
		terms     uint64
		started   int
		won       int
		maxTerm   raft.Term
		noLeader  int
		contended int
	}
	run := func(preVote bool) arm {
		var a arm
		for seed := uint64(0); seed < seeds; seed++ {
			opt := hunt.A2Options()
			opt.PreVote = preVote
			p, err := hunt.MaterializeRaftWith(seed, opt)
			if err != nil {
				t.Fatalf("seed %d: materialize: %v", seed, err)
			}
			r, err := hunt.RunRaftWith(p, opt, nil)
			if err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
			a.terms += uint64(r.Census.Terms)
			a.started += r.Census.ElectionsStart
			a.won += r.Census.ElectionsWon
			if r.Census.Terms > a.maxTerm {
				a.maxTerm = r.Census.Terms
			}
			if r.Census.ElectionsWon == 0 {
				a.noLeader++
			}
			if r.Census.ElectionsWon > 1 || r.Census.SplitVotes > 0 {
				a.contended++
			}
		}
		return a
	}

	off, on := run(false), run(true)
	t.Logf("pre-vote OFF: terms summed %d, highest %d, elections started %d, won %d",
		off.terms, off.maxTerm, off.started, off.won)
	t.Logf("pre-vote ON:  terms summed %d, highest %d, elections started %d, won %d",
		on.terms, on.maxTerm, on.started, on.won)
	t.Logf("liveness:     off %d seeds contended / %d with no leader; on %d / %d",
		off.contended, off.noLeader, on.contended, on.noLeader)

	// The sharpest number in the table: how many elections a cluster has to
	// start before one of them sticks. Pre-vote's claim is that a disrupted node
	// stops making the other two hold elections it will not win.
	t.Logf("started per win: off %.2f, on %.2f",
		float64(off.started)/float64(max(off.won, 1)), float64(on.started)/float64(max(on.won, 1)))

	// The inflation must be REAL in the off arm, or the schedules are not
	// producing the geometry and the comparison is between two quiet runs.
	if off.terms <= uint64(seeds) {
		t.Fatalf("with pre-vote off, terms summed to %d across %d seeds -- barely more than one term "+
			"each. The single-cut geometry is not being generated, so this test is comparing two "+
			"quiet clusters and proves nothing about pre-vote", off.terms, seeds)
	}

	// And it must be visibly smaller with pre-vote on. Stated as a ratio rather
	// than a threshold pulled from the air: the claim is that pre-vote removes
	// most of the inflation, so most of it has to go.
	if on.terms*2 >= off.terms {
		t.Errorf("pre-vote did not reduce term inflation: %d terms summed with it on against %d "+
			"with it off. Its entire value is this difference, so a version that does not produce "+
			"one is a version that is not doing anything", on.terms, off.terms)
	}

	// Pre-vote must not cost liveness. A round that stops elections from
	// happening at all would trade one failure for a worse one.
	if on.noLeader > off.noLeader+seeds/20 {
		t.Errorf("pre-vote cost liveness: %d seeds elected nobody with it on against %d with it off",
			on.noLeader, off.noLeader)
	}
}
