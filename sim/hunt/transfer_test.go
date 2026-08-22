package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestLeadershipTransferSkipsTheTimeout is transfer's exit criterion: the
// transferred-to node wins WITHOUT waiting out an election timeout.
//
// # What is observed, and from where
//
// Nothing is asked of a node. A transfer is three messages in sequence on the
// wire -- the order to campaign, the campaign, the first act of leadership -- and
// the ledger reconstructs it from exactly those, the same way it reconstructs
// "who led term T" from an MsgApp.
//
// Two things make the shortcut visible, and they are different claims:
//
//	the timeout is skipped   the target campaigns within a fraction of one
//	                         election timeout of being told to. Waiting one out
//	                         would be indistinguishable from an ordinary failover,
//	                         which is what transfer exists to avoid.
//	the pre-vote is skipped  the target sends MsgVote with no MsgPreVote before
//	                         it. With pre-vote ON for every other campaign in the
//	                         run, that absence is a signature nothing else
//	                         produces -- so it is evidence rather than an
//	                         assertion about internals.
//
// "No committed entry lost across the transfer" is not re-asserted here. Leader
// completeness already checks it, in-run and cluster-wide, and a second weaker
// version of an existing oracle is a second thing to keep in step.
func TestLeadershipTransferSkipsTheTimeout(t *testing.T) {
	skipIfShort(t, "a transfer order must be seen delivered")
	const seeds = 300

	// One election timeout: 10 ticks at the plan's 10ms tick interval. A target
	// that campaigns inside a fifth of that plainly did not wait one out.
	const electionTimeout = clock.Instant(100 * 1_000_000)
	const promptly = electionTimeout / 5

	delivered, took, won, ignored, preVotedInWindow := 0, 0, 0, 0, 0
	for seed := uint64(0); seed < seeds; seed++ {
		p, err := hunt.MaterializeRaft(seed)
		if err != nil {
			t.Fatalf("seed %d: materialize: %v", seed, err)
		}
		r, err := hunt.RunRaft(p, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		for _, tr := range r.Ledger.Transfers() {
			delivered++

			// Inside the window, anything the target does is attributable to the
			// order. Outside it, the target's own election timeout could have
			// produced the same campaign, and counting that either way would be
			// measuring the schedule rather than the feature.
			campaignedInWindow := tr.Campaigned != 0 && tr.Campaigned-tr.At <= promptly
			preVoteInWindow := tr.PreVoted && tr.PreVotedAt-tr.At <= promptly

			switch {
			case preVoteInWindow:
				// The order was accepted and the target ran the round anyway.
				preVotedInWindow++
			case campaignedInWindow:
				took++
				if tr.Won != 0 {
					won++
				}
			default:
				// Down, deposed, already leading, or crashed before it could
				// act. The order did not take, which is not the same as the
				// feature failing.
				ignored++
			}
		}
	}

	t.Logf("transfers: %d orders delivered, %d took effect within %v, %d of those went on to lead, "+
		"%d did not take", delivered, took, promptly, won, ignored)
	t.Logf("pre-vote:  %d targets ran a pre-vote round inside the window despite the order", preVotedInWindow)

	if delivered == 0 {
		t.Fatal("no leadership transfer order was ever delivered across the whole range, so this " +
			"test asserts nothing: the schedule stopped generating them, the driver stopped " +
			"requesting them, or none survived the network")
	}
	if took == 0 {
		t.Fatal("every delivered transfer order failed to produce a prompt campaign; the receiving " +
			"half of the feature is not connected")
	}

	// The shortcut has to be the ordinary outcome, not an occasional one. A
	// feature that works on a minority of the orders it is given is a feature
	// whose green is being carried by the cases where nothing happened.
	if took*2 < delivered {
		t.Errorf("only %d of %d delivered orders produced a prompt campaign; the rest did nothing, "+
			"which means this test's evidence is mostly absence", took, delivered)
	}

	// With pre-vote on for every other campaign in the run, a transfer target
	// that runs the round anyway has not taken the shortcut. Being told to
	// campaign BY the current leader is stronger evidence than a quorum of peers
	// could give, so the round there is pure delay.
	if preVotedInWindow != 0 {
		t.Errorf("%d targets ran a pre-vote round within %v of accepting a transfer order",
			preVotedInWindow, promptly)
	}
	if won == 0 {
		t.Error("no transfer target ever went on to lead; the orders are being obeyed but the " +
			"handover never completes")
	}
}
