package chaos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/chaos"
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
)

// TestChaosRunWithRealCheckers is I2's actual result.
//
// Everything before it in this file was the harness proving it could start,
// kill and restart processes. This is the first run where a CLIENT asks a real
// cluster to do something over a socket, records what came back on its own
// clock, and hands the result to the same checkers every seeded run uses.
//
// # What is exercised TOGETHER here, and what is still only exercised alone
//
//	TOGETHER   net/ frames, net/tcp sockets, the client wire format, the serving
//	           seam on the node loop, store/ + raft/ + engine/model, the process
//	           supervisor, the kill schedule, the client's correlation, the
//	           runner's gate, and sim/checker's linearizability model.
//	ALONE      engine/riftcgo (this binary is built without the rift_cgo tag, so
//	           the engine here is engine/model and this is NOT a storage result),
//	           range splits, transactions, and read index -- none of which this
//	           workload reaches.
//
// # The kill schedule is a NODE kill, not a leader kill
//
// CLAUDE.md's headline says "killing the leader every 10 seconds". This kills
// round-robin, because riftnode exposes no way to ask who the leader is.
//
//	A ROUND-ROBIN KILL HITS THE LEADER A THIRD OF THE TIME AND IS NOT THE SAME
//	EXPERIMENT. It is stated here rather than rounded up, and the headline stays
//	unclaimed until the schedule matches it.
func TestChaosRunWithRealCheckers(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes and kills them")
	}
	bin := buildNode(t)
	root := t.TempDir()

	const n = 3
	ports := freePorts(t, n+1)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", ports[n])
	const clientID = 100

	var peerParts []string
	var nodes []*chaos.Node
	for i := 1; i <= n; i++ {
		peerParts = append(peerParts, fmt.Sprintf("%d=127.0.0.1:%d", i, ports[i-1]))
		nodes = append(nodes, &chaos.Node{
			ID:   i,
			Addr: fmt.Sprintf("127.0.0.1:%d", ports[i-1]),
			Dir:  filepath.Join(root, fmt.Sprintf("n%d", i)),
		})
	}
	s := chaos.NewWithArgs(bin, nodes, func(nd *chaos.Node) []string {
		var others []string
		for _, p := range peerParts {
			if !strings.HasPrefix(p, strconv.Itoa(nd.ID)+"=") {
				others = append(others, p)
			}
		}
		return []string{
			"--peers", strings.Join(others, ","),
			"--clients", fmt.Sprintf("%d=%s", clientID, clientAddr),
		}
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()

	addrs := map[sim.NodeID]string{}
	for i := 1; i <= n; i++ {
		addrs[sim.NodeID(i)] = fmt.Sprintf("127.0.0.1:%d", ports[i-1])
	}

	run := chaos.Run{}
	hist := &sim.History{}
	rec := chaos.NewClient(clientID, clock.NewReal(0), hist)
	wc, err := chaos.NewWireClient(clientID, clientAddr, addrs, rec, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer wc.Close()

	// Let an election finish before the first operation. A run whose whole
	// history lands in the pre-leader window measures the startup gap and calls
	// it the system.
	waitForLeader(t, nodes, 8*time.Second)
	run.Persistent = clusterIsPersistent(nodes)

	const (
		keys     = 8
		duration = 12 * time.Second
		killEach = 3 * time.Second
	)
	led := newLedWatch()
	deadline := time.Now().Add(duration)
	nextKill := time.Now().Add(killEach)
	victim := 0
	i := 0
	for time.Now().Before(deadline) {
		if time.Now().After(nextKill) {
			led.sample(nodes)
			// AIM AT THE LEADER. CLAUDE.md's headline experiment kills the
			// leader; a round-robin schedule hits it a third of the time on
			// three nodes and is a gentler run reported under the same name.
			victim = victim%n + 1
			nd := nodes[victim-1]
			aimedAtLeader := false
			if l := leaderNode(nodes); l != nil {
				nd, victim, aimedAtLeader = l, l.ID, true
			}
			// COUNTED ON DELIVERY, never on intent. A kill aimed at a node that
			// is already down injects nothing, and counting the attempt reports
			// a fault that did not happen. BUG-058.
			delivered, err := s.Kill(nd)
			if err != nil {
				t.Logf("kill node %d: %v", victim, err)
			}
			if delivered && aimedAtLeader {
				run.LeaderKills++
			}
			if !delivered {
				continue
			}
			run.Faults = append(run.Faults, chaos.Fault{At: time.Now(), Kind: "kill", Node: victim})
			// Restarted immediately: a chaos run wants a cluster that keeps
			// losing and regaining a member, not one that shrinks to a minority
			// and stops being a cluster.
			if err := s.Restart(nd); err != nil {
				t.Logf("restart node %d: %v", victim, err)
			}
			run.Faults = append(run.Faults, chaos.Fault{At: time.Now(), Kind: "restart", Node: victim})
			nextKill = time.Now().Add(killEach)
			led.sample(nodes)
		}
		key := fmt.Sprintf("k%02d", i%keys)
		if i%3 == 0 {
			wc.Do("get", key, "", 800*time.Millisecond)
		} else {
			wc.Do("put", key, fmt.Sprintf("v%d", i), 800*time.Millisecond)
		}
		i++
	}

	led.sample(nodes)
	run.LedTicks = led.total()
	run.Counters = s.Counters()
	run.Ops = rec.Counters()
	run.Corr = rec.Correlation()

	// THE CHECKERS. The same objects the seeded runs use, over a history the
	// client built on its own clock.
	lin := checker.NewLinearizability()
	rep := lin.Check(hist)
	run.Verdicts = append(run.Verdicts, chaos.Verdict{
		Checker: lin.Name(), Outcome: rep.Verdict, Detail: rep.Detail, Consumed: rep.Consumed,
	})

	g := run.Gate(1, 20)
	var out strings.Builder
	run.Report(&out, g)
	out.WriteString("\n" + run.FaultLog())
	t.Log("\n" + out.String())

	if os.Getenv("RIFT_CHAOS_REPORT") != "" {
		_ = os.WriteFile(os.Getenv("RIFT_CHAOS_REPORT"), []byte(out.String()), 0o644)
	}

	for _, v := range run.Verdicts {
		if v.Outcome == sim.VerdictViolation {
			t.Errorf("SAFETY VIOLATION from %s: %s\n%s", v.Checker, v.Detail, run.FaultLog())
		}
	}
	if len(g.Failures) > 0 {
		// THE NODES' OWN WORDS, printed with the gate failure rather than left
		// to a second run. A gate arm about a PROCESS that does not show what
		// the process said sends the reader back to reproduce a schedule that
		// cannot be reproduced.
		t.Fatalf("gate failed:\n  %s\n\nnode stderr:\n%s",
			strings.Join(g.Failures, "\n  "), s.Stderr())
	}
}
