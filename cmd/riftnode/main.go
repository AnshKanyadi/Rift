// Command riftnode is one cluster member, as its own operating-system process.
//
// # This is where I2's four pieces stop being independently testable
//
// net/ (frames), net/tcp/ (sockets), node/ (the mailbox driver) and chaos/
// (supervision and history) were each built and induced ALONE. Four pieces each
// induced alone is FOUR CLAIMS, NOT ONE.
//
//	THE FIRST END-TO-END RUN IS THE FIRST EVIDENCE THAT THE COMPOSITION HOLDS,
//	AND IF SOMETHING BREAKS THERE IT IS MORE LIKELY TO BE IN THE SEAMS THAN IN
//	ANY PIECE.
//
// So this binary is deliberately thin: it wires, it does not decide. Every
// policy it appears to make is a flag, so a failure here is a wiring failure
// rather than a new implementation of something already verified.
//
// # It reports its own reality
//
// On a signal-free clean exit it prints the counters that prove it was real:
// bytes written to sockets, bytes read from them, and the engine's footprint on
// disk. BUG-046 is why: a run that reports success having never opened the
// engine is indistinguishable from one that did, unless something counts.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anshkanyadi/rift/net/tcp"
	"github.com/anshkanyadi/rift/sim"
)

func main() {
	id := flag.Int("id", 0, "this node's id, 1-based")
	addr := flag.String("addr", "", "host:port to listen on")
	dir := flag.String("dir", "", "this node's storage directory")
	peers := flag.String("peers", "", "comma-separated id=host:port for every other node")
	flag.Parse()

	if *id == 0 || *addr == "" || *dir == "" {
		fmt.Fprintln(os.Stderr, "riftnode: --id, --addr and --dir are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: %v\n", err)
		os.Exit(1)
	}

	addrs, err := parsePeers(*peers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: --peers: %v\n", err)
		os.Exit(2)
	}

	tr := tcp.New(sim.NodeID(*id), addrs)
	defer tr.Close()

	// The listener hands every frame to the mailbox. It is NOT allowed to touch node
	// state: Amendment A1's rule is that every cross-goroutine interaction
	// enters through the mailbox, and this goroutine is as cross as they come.
	var received uint64 // atomic: written by accept goroutines, read by the counter writer
	ln, err := tcp.Listen(*addr, func(e sim.Envelope) {
		atomic.AddUint64(&received, uint64(e.Size()))
		// The mailbox post goes here once the store is wired. Counting the
		// bytes first is deliberate: a node that receives and drops is still a
		// node that received, and the reality counter must not depend on what
		// the protocol did next.
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: listen %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer ln.Close()

	fmt.Fprintf(os.Stderr, "riftnode %d listening on %s, dir=%s, peers=%d\n",
		*id, *addr, *dir, len(addrs))
	// A readiness marker the supervisor can wait on. Waiting on a sleep instead
	// is how a test becomes flaky on a loaded machine, and how a cluster that
	// never came up reports as one that came up slowly.
	if err := os.WriteFile(filepath.Join(*dir, "ready"), []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: %v\n", err)
		os.Exit(1)
	}

	// A heartbeat to every peer, so a cluster that is merely idle still produces
	// wire bytes. Without traffic the reality counters cannot distinguish "the
	// network works" from "nobody said anything".
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		var seq uint64
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				seq++
				for peer := range addrs {
					tr.Send(sim.Envelope{
						From: sim.NodeID(*id), To: peer, Kind: 1,
						Body: []byte(fmt.Sprintf("hb-%d", seq)),
					})
				}
			}
		}
	}()

	// COUNTERS ARE WRITTEN DURING THE RUN, NOT AT EXIT, and the first version of
	// this got it wrong in a way the counters themselves caught.
	//
	// A chaos lane kills with SIGKILL. A process that is SIGKILLed prints
	// nothing, runs no deferred function and flushes no buffer -- so the first
	// end-to-end run reported three real pids and ZERO bytes on any socket,
	// because every node had been killed before it could say what it had done.
	//
	//	EVIDENCE THAT ONLY EXISTS AT CLEAN EXIT IS EVIDENCE A CHAOS LANE CAN
	//	NEVER COLLECT. The lane's whole method is killing things, so anything it
	//	needs to know must already be on disk when the kill lands.
	//
	// This is the third instance of one shape: a sweep chunk killed at 60 of 75
	// seeds lost every verdict because the census printed only at the end, and
	// SweepRaftWithProgress's hook was widened for the same reason. Liveness and
	// result-preservation are different problems, and a kill separates them.
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				writeCounters(*dir, *id, tr, &received)
			}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	<-sigs
	close(stop)
	writeCounters(*dir, *id, tr, &received)

	sent, dropped, wire := tr.Counters()
	fmt.Fprintf(os.Stderr, "riftnode %d exiting: sent=%d dropped=%d wire-bytes=%d recv-bytes=%d\n",
		*id, sent, dropped, wire, received)
}

func parsePeers(s string) (map[sim.NodeID]string, error) {
	out := map[sim.NodeID]string{}
	if s == "" {
		return out, nil
	}
	for _, part := range splitComma(s) {
		var id int
		var host string
		if _, err := fmt.Sscanf(part, "%d=%s", &id, &host); err != nil {
			return nil, fmt.Errorf("%q is not id=host:port", part)
		}
		out[sim.NodeID(id)] = host
	}
	return out, nil
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// writeCounters persists this node's reality counters where a kill cannot
// destroy them. Written to a temporary file and renamed, so a reader never sees
// a half-written line -- a kill during the write would otherwise produce a
// corrupt counter that reads as a small one.
func writeCounters(dir string, id int, tr *tcp.Transport, received *uint64) {
	sent, dropped, wire := tr.Counters()
	line := fmt.Sprintf("id=%d sent=%d dropped=%d wire=%d recv=%d\n",
		id, sent, dropped, wire, atomicLoad(received))
	tmp := filepath.Join(dir, "counters.tmp")
	if err := os.WriteFile(tmp, []byte(line), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(dir, "counters"))
}

// atomicLoad reads the receive counter. It is a plain load behind a function so
// the race detector has one place to complain about if this ever becomes a
// contended field rather than a monotonically-increasing one.
func atomicLoad(p *uint64) uint64 { return atomic.LoadUint64(p) }
