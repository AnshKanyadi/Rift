package hunt

import (
	"testing"

	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
)

// TestNoLeaderIsInconclusiveNotPass induces the cluster-side half of the
// vacuous-green rule.
//
// The client-side half -- an unknown-dominated history -- fires on real seeds
// and is measured (34 inconclusive in 10k). This half does not: the A1 schedule
// mix elects somebody on every one of 10,000 seeds, which is good for the
// cluster and useless as evidence about the rule. So it is induced here instead
// of being assumed to work because it never had to.
func TestNoLeaderIsInconclusiveNotPass(t *testing.T) {
	pass := []sim.Report{{Checker: "linearizability", Verdict: sim.VerdictPass, Detail: "every key linearizable"}}

	// A run that elected somebody keeps its verdict.
	got := append([]sim.Report(nil), pass...)
	markVacuousIfNoLeader(got, raftcheck.Census{ElectionsWon: 1})
	if got[0].Verdict != sim.VerdictPass {
		t.Errorf("a run that elected a leader had its pass downgraded to %s", got[0].Verdict)
	}

	// A run that elected nobody does not.
	got = append([]sim.Report(nil), pass...)
	markVacuousIfNoLeader(got, raftcheck.Census{ElectionsWon: 0})
	if got[0].Verdict != sim.VerdictInconclusive {
		t.Fatalf("a run with no leader banked a %s; a safety verdict over a cluster that never did "+
			"the thing whose safety is asserted is vacuous, and this is exactly the shape the codec "+
			"off-by-one had", got[0].Verdict)
	}

	// A violation is never downgraded. A run that misbehaved without ever
	// electing anybody found something real, and turning it into "we cannot
	// tell" would lose the finding.
	viol := []sim.Report{{Checker: "linearizability", Verdict: sim.VerdictViolation, Detail: "k03 not linearizable"}}
	markVacuousIfNoLeader(viol, raftcheck.Census{ElectionsWon: 0})
	if viol[0].Verdict != sim.VerdictViolation {
		t.Errorf("a violation on a leaderless run was downgraded to %s, losing the finding", viol[0].Verdict)
	}
}
