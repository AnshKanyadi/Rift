package chaos_test

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/chaos"
)

// TestARealClusterIsRealInTheWayThePhaseClaims is I2's first end-to-end run.
//
// # Four pieces, four claims, and this is the first evidence of one
//
// net/ (frames), net/tcp/ (sockets), chaos/ (supervision, history) and the
// riftnode binary were each induced ALONE. Four pieces each induced alone is
// four claims, not one, and a composition can fail in seams that no piece owns.
//
// # It asserts reality with COUNTERS, never by construction
//
// The vacuous version of this test is a cluster running entirely in one process
// over a loopback that never leaves userspace, reporting a clean history and
// looking exactly like success. BUG-046 is the measured instance of that shape,
// so the counters are here before the first green rather than after it:
//
//	SEPARATE PIDS      the processes are processes, not goroutines
//	WIRE BYTES         somebody wrote to a socket
//	RECEIVED BYTES     somebody else read from one
//	DISK FOOTPRINT     each node touched its OWN directory
func TestARealClusterIsRealInTheWayThePhaseClaims(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes")
	}
	bin := buildNode(t)
	root := t.TempDir()

	const n = 3
	ports := freePorts(t, n)
	var nodes []*chaos.Node
	var peerParts []string
	for i := 1; i <= n; i++ {
		peerParts = append(peerParts, fmt.Sprintf("%d=127.0.0.1:%d", i, ports[i-1]))
	}
	for i := 1; i <= n; i++ {
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
		return []string{"--peers", strings.Join(others, ",")}
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()

	// Wait on a marker the node writes, not on a sleep. A sleep is how a test
	// becomes flaky on a loaded machine, and how a cluster that never came up
	// reports as one that came up slowly.
	for _, nd := range nodes {
		waitFor(t, filepath.Join(nd.Dir, "ready"))
	}

	// SEPARATE PROCESSES.
	r := s.Reality()
	if r.Distinct != n {
		t.Fatalf("%d distinct pids, want %d (self=%d, pids=%v).\n"+
			"      A cluster of goroutines would satisfy every other counter in this test",
			r.Distinct, n, r.Self, r.PIDs)
	}
	for _, nd := range nodes {
		b, err := os.ReadFile(filepath.Join(nd.Dir, "ready"))
		if err != nil {
			t.Fatal(err)
		}
		pid, _ := strconv.Atoi(string(b))
		if pid == r.Self {
			t.Errorf("node %d reports this test's own pid: it is not a separate process", nd.ID)
		}
	}

	// BYTES ON SOCKETS. Let the heartbeats run.
	time.Sleep(600 * time.Millisecond)
	s.StopAll()
	time.Sleep(300 * time.Millisecond)

	// EACH NODE TOUCHED ITS OWN ROOT. Summing counters proves bytes moved; it
	// does not prove three nodes moved them. Three nodes sharing one directory
	// would have the last writer win, this test would read that one file three
	// times, and the total would look healthy.
	//
	//	A SUM OVER N DIRECTORIES THAT ARE SECRETLY ONE DIRECTORY IS N TIMES ONE
	//	NODE, and it is indistinguishable from N nodes by every total.
	for _, nd := range nodes {
		b, err := os.ReadFile(filepath.Join(nd.Dir, "counters"))
		if err != nil {
			t.Fatalf("node %d wrote no counters: %v", nd.ID, err)
		}
		var id int
		if _, err := fmt.Sscanf(string(b), "id=%d", &id); err != nil || id != nd.ID {
			t.Errorf("the counters under node %d's directory report id=%d.\n"+
				"      Either the nodes share a directory or one wrote into another's, and in "+
				"both cases per-node totals are one node counted repeatedly", nd.ID, id)
		}
	}

	sent, wire, recv := parseExitCounters(t, nodes)
	if wire == 0 {
		t.Errorf("no bytes were written to any socket. The cluster may be running entirely in "+
			"userspace, which is BUG-046's shape one layer up: a clean run over a network that "+
			"was never used (sent=%d)", sent)
	}
	if recv == 0 {
		t.Errorf("no bytes were read from any socket. Something wrote and nothing listened, so " +
			"the transport is a monologue")
	}
	t.Logf("REALITY: %d distinct pids, %d wire bytes out, %d bytes in, self=%d",
		r.Distinct, wire, recv, r.Self)
}

func buildNode(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "riftnode")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/riftnode")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building riftnode: %v\n%s", err, out)
	}
	return bin
}

// freePorts asks the kernel for ports rather than guessing.
//
// A hard-coded port is how a test passes alone and fails beside anything else,
// and a chaos suite that cannot run twice at once is a suite nobody runs in CI.
func freePorts(t *testing.T, n int) []int {
	t.Helper()
	var out []int
	var hold []net.Listener
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		hold = append(hold, l)
		out = append(out, l.Addr().(*net.TCPAddr).Port)
	}
	// Held until every port is chosen, so the kernel cannot hand the same one
	// out twice inside this loop.
	for _, l := range hold {
		_ = l.Close()
	}
	return out
}

// parseExitCounters reads what the nodes reported as they exited.
//
// The counters come from the NODES, not from this test's view of them: a test
// that counted its own sends would be measuring the harness. A node reporting
// its own wire bytes is the only party that knows whether a write reached a
// socket.
func parseExitCounters(t *testing.T, nodes []*chaos.Node) (sent, wire, recv uint64) {
	t.Helper()
	for _, nd := range nodes {
		b, err := os.ReadFile(filepath.Join(nd.Dir, "counters"))
		if err != nil {
			t.Errorf("node %d wrote no counters file: %v.\n"+
				"      A node that cannot report what it did leaves a run with no evidence, and a "+
				"chaos lane kills its nodes, so anything reported only at exit is never reported",
				nd.ID, err)
			continue
		}
		var id int
		var a, d, w, r uint64
		if n, _ := fmt.Sscanf(string(b), "id=%d sent=%d dropped=%d wire=%d recv=%d",
			&id, &a, &d, &w, &r); n == 5 {
			sent += a
			wire += w
			recv += r
		}
	}
	return sent, wire, recv
}
