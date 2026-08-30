package chaos_test

import (
	"fmt"
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

// The chaos harness, calibrated against a cluster that breaks its guarantees on
// purpose (GF-62 item 2).
//
// `sim/hunt`'s mutants work because `sim/toy` exists. These are `chaos/`'s
// equivalent: a real three-process cluster told to misbehave, so every mechanism
// in the real-mode path has a positive control instead of only an induced one.
//
//	INDUCING A GATE BY EDITING THE RUNNER PROVES THE CODE PATH. It does not prove
//	the mechanism catches a CLUSTER actually doing the thing, over a real socket,
//	through the real client.
func calibrationCluster(t *testing.T, fault string) (*chaos.Supervisor, []*chaos.Node, *chaos.Client, *sim.History, *chaos.WireClient) {
	t.Helper()
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
			"--clients", fmt.Sprintf("%d=%s", clientID, clientAddr), "--fault", fault}
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	addrs := map[sim.NodeID]string{}
	for i := 1; i <= n; i++ {
		addrs[sim.NodeID(i)] = fmt.Sprintf("127.0.0.1:%d", ports[i-1])
	}
	hist := &sim.History{}
	rec := chaos.NewClient(clientID, clock.NewReal(0), hist)
	wc, err := chaos.NewWireClient(clientID, clientAddr, addrs, rec, 2*time.Second)
	if err != nil {
		s.StopAll()
		t.Fatal(err)
	}
	waitForLeader(t, nodes, 15*time.Second)
	return s, nodes, rec, hist, wc
}

// A cluster serving stale reads must be caught by the linearizability checker.
func TestAStaleReadingClusterIsCaught(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes")
	}
	s, _, _, hist, wc := calibrationCluster(t, "stale-read")
	defer s.StopAll()
	defer wc.Close()

	for i := 0; i < 200; i++ {
		wc.Do("put", "k0", fmt.Sprintf("v%d", i), time.Second)
		wc.Do("get", "k0", "", time.Second)
	}
	rep := checker.NewLinearizability().Check(hist)
	if rep.Verdict != sim.VerdictViolation {
		t.Fatalf("a cluster answering reads from a value it wrote first and never updated was "+
			"reported as %s over %d operations.\n"+
			"      The checker did not catch a real cluster doing a real stale read, which is a "+
			"statement about the CHECKER and not about this fixture.", rep.Verdict, rep.Consumed)
	}
	t.Logf("caught: %s", rep.Detail)
}

// A cluster answering twice, differently, must be caught by the client's
// correlation -- and NOT by porcupine, which never sees the second answer.
func TestADoubleAnsweringClusterIsCaught(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes")
	}
	s, _, rec, _, wc := calibrationCluster(t, "double-answer")
	defer s.StopAll()
	defer wc.Close()

	for i := 0; i < 100; i++ {
		wc.Do("put", fmt.Sprintf("k%d", i%8), fmt.Sprintf("v%d", i), time.Second)
	}
	// Late duplicates need a moment to land.
	time.Sleep(500 * time.Millisecond)

	c := rec.Correlation()
	if c.Conflicting == 0 {
		t.Fatalf("a cluster answering every operation twice with DIFFERENT values produced "+
			"%d conflicting responses.\n"+
			"      This is the case porcupine structurally cannot see -- the history holds one "+
			"response per operation -- so if the correlation misses it, nothing catches it.",
			c.Conflicting)
	}
	if !c.Loud() {
		t.Fatal("conflicting responses were counted but not LOUD, so the gate would not fire")
	}
	t.Logf("caught: %+v", c)
}
