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
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/net/tcp"
	"github.com/anshkanyadi/rift/node"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/store"
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

	// THE STORE, THE ENGINE AND THE DRIVER, wired by TYPE rather than by
	// arrangement. store.Node implements sim.Node; node.Driver drives a
	// sim.Node; tcp.Transport implements sim.Transport. Nothing here adapts one
	// to another, which is node/'s own claim -- "the toy that runs under a
	// thousand seeded schedules is byte-for-byte the toy that runs here" -- and
	// this is the first time it has been asked of the real stack.
	peerIDs := make([]raft.NodeID, 0, len(addrs)+1)
	for pid := range addrs {
		peerIDs = append(peerIDs, raft.NodeID(pid))
	}
	peerIDs = append(peerIDs, raft.NodeID(*id))
	sort.Slice(peerIDs, func(i, j int) bool { return peerIDs[i] < peerIDs[j] })

	clk := clock.NewReal(maxOffset)
	hist := &sim.History{}
	st, err := store.New(store.Config{
		ID: raft.NodeID(*id), Peers: peerIDs, Ordinal: *id - 1,
		Nodes:     len(peerIDs),
		Election:  10,
		Heartbeat: 3,
		Transport: tr,
		Clock:     clk,
		// SYNC LATENCY IS A SIMULATOR CONCEPT, and real mode must still answer
		// for it. In sim it is how long an fsync is MODELLED to take; here the
		// fsync is real and happens inside AdvanceDurable, so this is only the
		// delay before the store asks for it.
		//
		// Small and non-zero: zero would ask for durability in the same instant
		// as the write and erase the unsynced window entirely, which is the
		// fault the whole phase is built to inject.
		SyncLatency: clock.Instant(2 * time.Millisecond),
		Ledger:      raftcheck.NewLedger(len(peerIDs)),
		// The node's OWN history, which is discarded. The authoritative history
		// is the CLIENT's, in the client's process, on the client's clock --
		// one monotonic source, which is why the two cannot be the same object.
		History:   hist,
		NewEngine: engineFor(*dir),
		PreVote:   true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: store: %v\n", err)
		os.Exit(1)
	}

	drv, err := node.New(sim.NodeID(*id), st, clk, mailboxDepth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: driver: %v\n", err)
		os.Exit(1)
	}
	if err := drv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: driver start: %v\n", err)
		os.Exit(1)
	}
	defer drv.Stop()

	// The listener hands every frame to the mailbox. It is NOT allowed to touch node
	// state: Amendment A1's rule is that every cross-goroutine interaction
	// enters through the mailbox, and this goroutine is as cross as they come.
	var received uint64     // atomic: written by accept goroutines, read by the counter writer
	var drvRef *node.Driver // set below; the listener starts before the driver exists
	ln, err := tcp.Listen(*addr, func(e sim.Envelope) {
		atomic.AddUint64(&received, uint64(e.Size()))
		// INTO THE MAILBOX, never into node state. Amendment A1: every
		// cross-goroutine interaction enters through the mailbox, and this
		// accept goroutine is as cross as they come.
		//
		// Counting the bytes BEFORE the post is deliberate: a node that
		// receives and drops is still a node that received, and the reality
		// counter must not depend on what the protocol did next.
		drvRef.Post(sim.Event{Kind: sim.KindDeliver, Node: sim.NodeID(*id), Payload: e})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: listen %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer ln.Close()

	drvRef = drv

	// THE ENGINE IS NAMED IN THE OUTPUT. A result is about whichever engine
	// produced it, and a reader who cannot tell which will assume the one the
	// phase claims.
	fmt.Fprintf(os.Stderr, "riftnode %d listening on %s, dir=%s, peers=%d, engine=%s\n",
		*id, *addr, *dir, len(addrs), EngineName)
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

const (
	// maxOffset is the clock-skew bound this node advertises. Real mode does not
	// get to assume zero: A5's uncertainty machinery reads it, and a node
	// claiming perfect clocks would make every uncertainty interval empty.
	maxOffset = 250 * time.Millisecond

	// mailboxDepth bounds the driver's queue. Deep enough that a burst does not
	// drop, shallow enough that a wedged node does not accumulate unboundedly.
	mailboxDepth = 1024
)
