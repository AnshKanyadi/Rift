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

// TestTheClusterSurvivesKillsIsACOMPOSITIONTEST, not a chaos run that passed.
//
//	IT CHECKS NOTHING ABOUT WHAT THE CLUSTER COMPUTED. riftnode does not yet
//	wire the store, so there is no history and no checker consumed anything. It
//	is the harness proving it can start, kill and restart processes that talk to
//	each other -- real, necessary, and not the claim I2 exists to make.
//
// It is labelled that way here so nobody later reads a passing chaos test in
// this file as a safety result. The real chaos lane is the one whose verdict
// list is non-empty.
//
// # It runs BEFORE the benchmarks, and the order is the gate
//
// The safety gate says the benchmark section does not run if a violation
// appears. Running benchmarks first inverts it:
//
//	A NUMBER TAKEN BEFORE ITS PRECONDITION IS CHECKED IS A NUMBER THAT WILL BE
//	QUOTED REGARDLESS OF WHAT THE CHECK SAYS. Once it exists, deleting it takes
//	a decision, and the decision is made by whoever wants the number.
func TestTheClusterSurvivesKillsIsACompositionTest(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes and kills them")
	}
	bin := buildNode(t)
	root := t.TempDir()

	const n = 3
	ports := freePorts(t, n)
	var nodes []*chaos.Node
	var peerParts []string
	for i := 1; i <= n; i++ {
		peerParts = append(peerParts, fmt.Sprintf("%d=127.0.0.1:%d", i, ports[i-1]))
		nodes = append(nodes, &chaos.Node{
			ID: i, Addr: fmt.Sprintf("127.0.0.1:%d", ports[i-1]),
			Dir: filepath.Join(root, fmt.Sprintf("n%d", i)),
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
	for _, nd := range nodes {
		waitFor(t, filepath.Join(nd.Dir, "ready"))
	}

	// The fault schedule, recorded AS IT IS INFLICTED. A violation here has no
	// reproduction, so what was done to the cluster is the whole of its context.
	var faults []chaos.Fault
	const rounds = 4
	for r := 0; r < rounds; r++ {
		victim := nodes[r%n]
		if err := s.Kill(victim); err != nil {
			t.Fatal(err)
		}
		faults = append(faults, chaos.Fault{At: time.Now(), Kind: "kill", Node: victim.ID})
		time.Sleep(150 * time.Millisecond)
		if err := s.Restart(victim); err != nil {
			t.Fatal(err)
		}
		faults = append(faults, chaos.Fault{At: time.Now(), Kind: "restart", Node: victim.ID})
		waitFor(t, filepath.Join(victim.Dir, "ready"))
		time.Sleep(150 * time.Millisecond)
	}
	s.StopAll()
	time.Sleep(300 * time.Millisecond)

	sent, wire, recv := parseExitCounters(t, nodes)
	run := chaos.Run{
		Counters: s.Counters(),
		Ops:      chaos.OpCounters{Issued: int(sent), Completed: int(sent), Keys: n},
		Faults:   faults,
		// No client verdicts yet: riftnode does not wire the store, so there is
		// no history for a checker to read. Reported as absent rather than as
		// clean -- an empty verdict list is not a passing one.
		Verdicts: nil,
	}
	g := run.Gate(rounds, 1)

	var b strings.Builder
	run.Report(&b, g)
	t.Log("\n" + b.String())
	// THE ASYMMETRY BOUND, DERIVED -- and the equality claim it replaces was
	// wrong.
	//
	// The first end-to-end run reported out=1794 in=1794 and I called equal
	// counts a round trip. Two later runs gave out=1850/in=1794 and
	// out=1005/in=1018 -- asymmetric in BOTH directions. The equality was luck.
	//
	// Counters are sampled every 100ms and written by each node independently,
	// so whichever side's last sample landed later reads higher. The bound
	// follows from the sampling rate rather than from a hope:
	//
	//	heartbeat  every 50ms, to (n-1) peers
	//	sampling   every 100ms
	//	slack      n nodes x (n-1) peers x 2 frames x ~29 bytes  ~= 350 bytes at n=3
	//
	// What is meaningful is that BOTH are non-zero -- bytes moved in both
	// directions -- and that the gap is inside the sampling slack. A gap far
	// outside it, especially out >> in, would be the finding: bytes claimed
	// written that nobody read.
	slack := uint64(n * (n - 1) * 2 * 29 * 2)
	gap := wire - recv
	if recv > wire {
		gap = recv - wire
	}
	t.Logf("wire: out=%d in=%d gap=%d (sampling slack %d)", wire, recv, gap, slack)
	if wire == 0 || recv == 0 {
		t.Errorf("one direction carried nothing: out=%d in=%d", wire, recv)
	}
	if gap > slack {
		t.Errorf("out=%d in=%d differ by %d, beyond the %d-byte sampling slack.\n"+
			"      Inside the slack this is two independent 100ms samples disagreeing about when "+
			"they were taken. Outside it, something wrote bytes nobody read", wire, recv, gap, slack)
	}

	if len(g.Failures) > 0 {
		t.Fatalf("the chaos gate failed, so no benchmark number may be taken: %v", g.Failures)
	}
	if len(run.Verdicts) == 0 {
		t.Log("NOTE: zero checker verdicts. The cluster survived the fault schedule, and NOTHING " +
			"was checked about what it computed -- riftnode does not yet wire the store. This is " +
			"a liveness result, not a safety one, and it must not be quoted as the latter.")
	}
}
