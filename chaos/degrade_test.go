package chaos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/bench"
	"github.com/anshkanyadi/rift/chaos"
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
)

// A fault-free cluster must hold its throughput.
//
// # THIS TEST IS CURRENTLY RED, AND THAT IS THE CORRECT STATE
//
// It fails on BUG-055: `store.Config` requires a `raftcheck.Ledger`, so every
// real node runs the simulator's oracle inside it, and the oracle retains every
// message sent, every message received and every entry applied -- **875 bytes per
// operation, measured.** A node that has served 26,000 operations is carrying a
// complete audit log of its own history, and the cost shows up as latency.
//
//	1938 -> 996 -> 728 -> 606 -> 495 -> 478 ops/s across six 5-second windows,
//	no faults, nothing else running.
//
// The fix is to make the ledger optional in `store.Config`. `store/` is signed,
// so that is a ruling rather than an edit, and **the test stays red until it is
// made.** Weakening it -- widening the band, shortening the run, deleting it --
// would be weakening a checker to get green, and BUG-055 would then be a fact
// about this system that nothing in the repository states.
//
// The band is 2x, the same derivation `bench.Result.SteadyEnough` uses: at a
// factor of two the mean sits at 1.5x one end and 0.75x the other, so no single
// number describes the window.
func TestAFaultFreeClusterHoldsItsThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes and runs for 30 seconds")
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
		nodes = append(nodes, &chaos.Node{ID: i, Addr: fmt.Sprintf("127.0.0.1:%d", ports[i-1]),
			Dir: filepath.Join(root, fmt.Sprintf("n%d", i))})
	}
	s := chaos.NewWithArgs(bin, nodes, func(nd *chaos.Node) []string {
		var others []string
		for _, p := range peerParts {
			if !strings.HasPrefix(p, strconv.Itoa(nd.ID)+"=") {
				others = append(others, p)
			}
		}
		return []string{"--peers", strings.Join(others, ","),
			"--clients", fmt.Sprintf("%d=%s", clientID, clientAddr)}
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()
	addrs := map[sim.NodeID]string{}
	for i := 1; i <= n; i++ {
		addrs[sim.NodeID(i)] = fmt.Sprintf("127.0.0.1:%d", ports[i-1])
	}
	hist := &sim.History{}
	rec := chaos.NewClient(clientID, clock.NewReal(0), hist)
	wc, err := chaos.NewWireClient(clientID, clientAddr, addrs, rec, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer wc.Close()
	waitForLeader(t, nodes, 15*time.Second)

	var first, last float64
	for i := 0; i < 6; i++ {
		r := bench.Run(wc, bench.Mix{
			Name: fmt.Sprintf("w%d", i), ReadPct: 50, Keys: 512, Workers: 8,
			Window: 5 * time.Second, OpTimeout: 2 * time.Second,
		})
		if i == 0 {
			first = r.Throughput()
		}
		last = r.Throughput()
		heaps := make([]string, 0, len(nodes))
		for _, nd := range nodes {
			heaps = append(heaps, heapOf(nd))
		}
		t.Logf("window %d (t=%2ds): %8.0f ops/s  p50=%-12s p99=%-12s fail=%d  heap=[%s]",
			i, i*5, r.Throughput(), r.Hist.Quantile(0.5), r.Hist.Quantile(0.99), r.Fail,
			strings.Join(heaps, " "))
	}
	if first == 0 {
		t.Fatal("the cluster served nothing in its first window")
	}
	if ratio := last / first; ratio < 0.5 {
		t.Errorf("throughput fell to %.0f%% of its opening rate over 30 fault-free seconds "+
			"(%.0f -> %.0f ops/s).\n"+
			"      BUG-055: store.Config requires a raftcheck.Ledger, so every real node runs the\n"+
			"      simulator's oracle and retains 875 bytes per operation, forever. This test is RED\n"+
			"      until the ledger is optional in store/ -- which is signed, so it is a ruling.\n"+
			"      DO NOT widen this band or shorten this run to make it pass.",
			ratio*100, first, last)
	}
}

// heapOf reports one node's heap, from the node's own report.
func heapOf(nd *chaos.Node) string {
	b, err := os.ReadFile(filepath.Join(nd.Dir, "counters"))
	if err != nil {
		return "?"
	}
	for _, f := range strings.Fields(string(b)) {
		if strings.HasPrefix(f, "heap=") {
			return f
		}
	}
	return "?"
}
