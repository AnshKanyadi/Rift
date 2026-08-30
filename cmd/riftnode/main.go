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
	"runtime"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
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
	clients := flag.String("clients", "", "comma-separated id=host:port for client endpoints")
	unobserved := flag.Bool("unobserved", false,
		"run with NO oracle ledger: faster, and produces no checker evidence (BUG-055)")
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

	// # THE TRANSPORT IS ADDRESSED BY ORDINAL, NOT BY NODE ID
	//
	// sim.NodeID's own doc says it: "an index into the run's node set, not an
	// address". store/ obeys that -- every envelope it sends carries
	// sim.NodeID(n.cfg.Ordinal) and sim.NodeID(n.ordinalOf(m.To)) -- so a
	// transport keyed by the 1-based --id is keyed in the wrong space.
	//
	// The first version of this file was, and BUGS.md BUG-050 is what that
	// looked like from outside: a cluster that started, listened, connected,
	// exchanged bytes, and elected nobody. Node 2 reported sent=0 dropped=36,
	// because both of its destinations resolved to ordinals its map did not
	// hold; nodes 1 and 3 each reported sent=18 dropped=18, one destination
	// landing and one missing. The arithmetic identified the bug before the code
	// was read.
	//
	// CLIENT IDS STAY IN THEIR OWN RANGE. They are not ordinals of anything --
	// a client is not in the node set -- so they are chosen above it and never
	// collide.
	byOrdinal := make(map[sim.NodeID]string, len(addrs))
	for pid, a := range addrs {
		if pid == 0 {
			fmt.Fprintln(os.Stderr, "riftnode: --peers ids are 1-based")
			os.Exit(2)
		}
		byOrdinal[sim.NodeID(pid-1)] = a
	}
	self := sim.NodeID(*id - 1)

	// CLIENT ENDPOINTS ARE ADDRESSABLE, AND THEY ARE NOT PEERS. They join the
	// transport's address map, so a response can be routed back the same way a
	// Raft message is; they stay out of peerIDs, so no client is ever counted in
	// a quorum. Folding them together would put a process that holds no log into
	// the majority arithmetic, which is not a bug that fails loudly.
	caddrs, err := parsePeers(*clients)
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: --clients: %v\n", err)
		os.Exit(2)
	}
	all := map[sim.NodeID]string{}
	for k, v := range byOrdinal {
		all[k] = v
	}
	for k, v := range caddrs {
		if k < sim.NodeID(len(addrs)+1) {
			fmt.Fprintf(os.Stderr, "riftnode: client id %d collides with the node ordinal space (0..%d)\n",
				k, len(addrs))
			os.Exit(2)
		}
		if _, dup := all[k]; dup {
			fmt.Fprintf(os.Stderr, "riftnode: id %d is both a peer and a client\n", k)
			os.Exit(2)
		}
		all[k] = v
	}

	var startMode string
	observedFlag := 1
	if *unobserved {
		observedFlag = 0
	}

	tr := tcp.New(self, all)
	defer tr.Close()

	// THE STORE, THE ENGINE AND THE DRIVER, wired by TYPE rather than by
	// arrangement, and this is A0's claim being exercised for the first time.
	// See BUGS.md GF-55.
	//
	// THREE SIMULATOR CONCEPTS HAD TO BE ANSWERED FOR HERE, and the answers are
	// different from each other. They are the phase's honest content:
	//
	//	AdvanceDurable  a real fsync, whole tail. DESIGN-A0 section 7's I1
	//	                idealization, arriving exactly where it was recorded to
	//	                arrive -- see engine_cgo.go.
	//	Crash           panics. A modelled crash on a real engine discards
	//	                nothing while reporting success, which is the weaker
	//	                fault I1 refused, refused again in a new context.
	//	SyncLatency     below: non-zero, or the unsynced window disappears.
	peerIDs := make([]raft.NodeID, 0, len(addrs)+1)
	for pid := range addrs {
		peerIDs = append(peerIDs, raft.NodeID(pid))
	}
	peerIDs = append(peerIDs, raft.NodeID(*id))
	sort.Slice(peerIDs, func(i, j int) bool { return peerIDs[i] < peerIDs[j] })

	clk := clock.NewReal(maxOffset)
	hist := &sim.History{}
	scfg := store.Config{
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
		// THE ORACLE, AND THE DEFAULT IS ON.
		//
		// BUG-055: the ledger retains every message and every applied entry
		// forever -- the mechanism a bounded run needs, and a cost an unbounded
		// one cannot carry. Real mode may decline it, and declining is a CHOICE
		// the flag has to make explicitly; store.New refuses a config that
		// simply omits it.
		Ledger:     ledgerFor(*unobserved, len(peerIDs)),
		Unobserved: *unobserved,
		// The node's OWN history, which is discarded. The authoritative history
		// is the CLIENT's, in the client's process, on the client's clock --
		// one monotonic source, which is why the two cannot be the same object.
		History:   hist,
		NewEngine: engineFor(*dir),
		PreVote:   true,
	}

	// FRESH OR RECOVER, and the choice is made from the engine rather than
	// assumed. BUG-059: this called store.New unconditionally, so a restarted
	// node built empty bookkeeping over an engine that already held a recovered
	// log, and store/'s durability cross-check objected on the first real crash.
	//
	// store.New now refuses a non-empty engine and store.Open refuses an empty
	// one, so the wrong call cannot be made silently -- and this switch says
	// which one it made, because a node that RECOVERED and a node that started
	// FRESH are different runs.
	var st *store.Node
	if hasState, herr := storeHasState(*dir); herr != nil {
		fmt.Fprintf(os.Stderr, "riftnode: inspecting %s: %v\n", *dir, herr)
		os.Exit(1)
	} else if hasState {
		st, err = store.Open(scfg)
		startMode = "recovered"
	} else {
		st, err = store.New(scfg)
		startMode = "fresh"
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: store: %v\n", err)
		os.Exit(1)
	}

	// THE CLIENT PROTOCOL WRAPS THE STORE, so it runs on the node loop. See
	// clientserve.go for why that placement is the whole design.
	srv := newServing(st, self, hist, tr)

	drv, err := node.New(self, srv, clk, mailboxDepth)
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
		// THE PAYLOAD IS A FRAME, NOT AN ENVELOPE, and the difference cost a
		// whole debugging session. store.Node's deliver arm reads
		// `ev.Payload.([]byte)` and calls sim.Decode on it; handed a
		// sim.Envelope, the assertion fails and the arm RETURNS SILENTLY.
		//
		//	EVERY RAFT MESSAGE THIS CLUSTER EVER EXCHANGED WAS DISCARDED AT THAT
		//	LINE. Three nodes campaigned every election timeout for three minutes,
		//	each received the others' votes, and none of them ever saw one.
		//	BUGS.md BUG-051.
		//
		// The transport parses a frame in order to route it, and the store wants
		// the frame; re-encoding is the honest cost of one wire format being
		// read twice, and it is a memcpy.
		frame, err := sim.Encode(e)
		if err != nil {
			return
		}
		// INTO THE MAILBOX, never into node state. Amendment A1: every
		// cross-goroutine interaction enters through the mailbox, and this
		// accept goroutine is as cross as they come.
		//
		// Counting the bytes BEFORE the post is deliberate: a node that
		// receives and drops is still a node that received, and the reality
		// counter must not depend on what the protocol did next.
		//
		// AND THERE IS NO TIME TO FORGET. Post takes what happened, never an
		// Event: the driver owns its clock and stamps at the moment the event
		// enters the mailbox. This file is why -- it omitted At on every
		// delivery and every tick, and the leader panicked on its first answered
		// operation with a return three seconds before its call (BUG-052). The
		// asymmetry it exposed was amended in node/ rather than patched here.
		drvRef.Post(sim.KindDeliver, self, frame)
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
	//
	// AND SO IS THE ORACLE, in the same place and for the same reason. A green
	// from a cluster that was not producing checker evidence is not a verified
	// result, and the only thing standing between those two readings is this
	// line.
	fmt.Fprintf(os.Stderr, "riftnode %d listening on %s, dir=%s, peers=%d, engine=%s, ledger=%s, start=%s\n",
		*id, *addr, *dir, len(addrs), EngineName, ledgerName(*unobserved), startMode)
	// A readiness marker the supervisor can wait on. Waiting on a sleep instead
	// is how a test becomes flaky on a loaded machine, and how a cluster that
	// never came up reports as one that came up slowly.
	if err := os.WriteFile(filepath.Join(*dir, "ready"), []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "riftnode: %v\n", err)
		os.Exit(1)
	}

	// THE TICKER. Raft's only source of time.
	//
	// # This replaced a synthetic heartbeat, and the swap is the finding
	//
	// There was no ticker here until now. `node.Driver` does not tick itself --
	// it is a mailbox and a loop, and in sim the LOOP owns the tick schedule --
	// so every real cluster this project has started ran with Raft FROZEN: no
	// election timeout ever fired, no leader was ever elected, and no entry was
	// ever replicated.
	//
	// What hid it was the thing that used to occupy these lines: a synthetic
	// heartbeat, sending a hand-made envelope to every peer every 50ms purely so
	// the reality counters would see wire bytes on an idle cluster.
	//
	//	IT WORKED. The counters went green. They were measuring the HARNESS'S
	//	OWN TRAFFIC, and they would have reported exactly the same numbers if
	//	the store had been deleted.
	//
	// That is BUG-046's shape, one level up: not a test that measured nothing,
	// but a test that measured something real and irrelevant. A reality counter
	// fed by a source the system under test does not control is not a reality
	// counter. So the synthetic traffic is GONE, and the bytes the counters see
	// are now Raft's heartbeats -- which means a frozen cluster reports zero and
	// the counter finally says what it claims to say. BUGS.md BUG-049.
	//
	// The interval: Election is 10 ticks and Heartbeat is 3, so 50ms gives a
	// 500ms-1s election timeout and a 150ms heartbeat. Fast enough that a kill
	// is recovered from inside a chaos run, slow enough that a loaded CI machine
	// does not manufacture elections out of scheduling delay.
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(tickInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				// INTO THE MAILBOX. A tick is an event like any other, and
				// Amendment A1 does not make an exception for the one that
				// arrives on a schedule.
				drv.Post(sim.KindTick, self, nil)
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
				writeCounters(*dir, *id, tr, &received, srv, observedFlag)
			}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	<-sigs
	close(stop)
	writeCounters(*dir, *id, tr, &received, srv, observedFlag)

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
func writeCounters(dir string, id int, tr *tcp.Transport, received *uint64, srv *serving, observedFlag int) {
	sent, dropped, wire := tr.Counters()
	// served and refused are read WITHOUT a lock, from the node loop's own
	// fields, and that is a deliberate limitation stated rather than hidden:
	// they are advisory reality counters, not gated quantities, and a torn read
	// of a uint64 on the platforms this runs on cannot produce a number that
	// changes a verdict. Anything the GATE reads comes from the client's own
	// accounting, in the client's process, under the client's mutex.
	admitted, served, refused := srv.Counters()
	// HEAP AND GOROUTINES, so a run that slows down can say WHY rather than
	// leaving the slowdown to be attributed. A benchmark whose throughput falls
	// and whose memory is unreported has one number and no explanation.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	led, ticks, now := srv.Leadership()
	role, term, vote, last, commit := srv.Status()
	cur := 0
	if now {
		cur = 1
	}
	line := fmt.Sprintf("id=%d sent=%d dropped=%d wire=%d recv=%d admitted=%d served=%d refused=%d led=%d ticks=%d leader=%d heap=%d goroutines=%d observed=%d persistent=%d role=%s term=%d vote=%d last=%d commit=%d\n",
		id, sent, dropped, wire, atomicLoad(received), admitted, served, refused, led, ticks, cur,
		ms.HeapAlloc, runtime.NumGoroutine(), observedFlag, persistentFlag(),
		role, term, vote, last, commit)
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

	// tickInterval is the wall period between Raft ticks. Election is 10 ticks
	// and Heartbeat is 3, so this sets a 500ms-1s election timeout and a 150ms
	// heartbeat. It is the ONLY source of time Raft has in real mode: without
	// it the state machine is frozen and reports nothing wrong.
	tickInterval = 50 * time.Millisecond

	// mailboxDepth bounds the driver's queue. Deep enough that a burst does not
	// drop, shallow enough that a wedged node does not accumulate unboundedly.
	mailboxDepth = 1024
)

// ledgerFor builds the oracle unless the run declined it.
//
// A nil return is legal ONLY beside Config.Unobserved, which store.New enforces:
// a forgotten ledger and a declined one must not produce the same program.
func ledgerFor(unobserved bool, n int) *raftcheck.Ledger {
	if unobserved {
		return nil
	}
	return raftcheck.NewLedger(n)
}

// ledgerName is what the startup line says, in the engine name's own shape: a
// configuration that weakens what a run can claim says so where the claim is
// read, not in a document beside it.
func ledgerName(unobserved bool) string {
	if unobserved {
		return "OFF (--unobserved: NOT producing checker evidence)"
	}
	return "on"
}

// persistentFlag reports whether this build's engine survives the process, so a
// chaos runner can refuse a restart schedule it cannot support. See BUG-056.
func persistentFlag() int {
	if EnginePersistent {
		return 1
	}
	return 0
}

// storeHasState reports whether this node's directory already holds engine
// state, which decides New versus Open.
//
// It asks the ENGINE rather than looking for files, because "what counts as
// state" is the engine's answer and not this file's: engine/model keeps none on
// disk at all, and the C++ engine's layout is its own business.
func storeHasState(dir string) (bool, error) {
	mk := engineFor(dir)
	if mk == nil {
		// engine/model: purely in memory, so a restart never has state. That is
		// BUG-056's whole problem, and the gate refuses restart schedules on it
		// rather than this line pretending otherwise.
		return false, nil
	}
	db := mk()
	defer db.Close()
	it := db.NewIter(engine.IterOptions{})
	defer func() { _ = it.Close() }()
	return it.First(), nil
}
