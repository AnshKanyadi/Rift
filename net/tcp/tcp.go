// Package tcp is the socket half of I2's transport.
//
// # Why the split, and it is node/'s split one layer down
//
// net/ holds the FRAME layer and is pure: encoding, decoding, and reading a
// frame off an io.Reader. It imports no sockets, spawns no goroutines, and the
// determinism pass CHECKS it -- it is in core scope and produces no findings.
//
// This package holds everything that touches a socket: dialing, listening, one
// goroutine per peer, and a bounded queue. It is excluded by name, for the
// reason node/ is: Amendment A5, code that needs a goroutine is orchestration.
//
//	THE PURE HALF STAYS IN SCOPE AND GETS CHECKED. ONLY THE PART THAT TOUCHES
//	SOCKETS IS EXCLUDED. Splitting the package is what buys that, and a single
//	package holding both would have put the frame codec outside the pass for no
//	reason other than its neighbours.
//
// # Send does not return an error, and that is the whole design
//
// sim.Transport is `Send(Envelope)` with no error, since A0.7:
//
//	"an error signal is a covert failure detector, and covert failure detectors
//	are how consensus implementations accidentally become unsafe"
//
// So a send that cannot be delivered is DROPPED, exactly as the simulator drops
// a message under a partition, and the protocol above tolerates loss because it
// has always had to. A queue that blocked would be a backpressure signal the
// protocol can observe; a queue that returned an error would be a failure
// detector. Both are refused, and the bound is what keeps a dead peer from
// consuming memory without either.
package tcp

import (
	"net"
	"sync"
	"time"

	riftnet "github.com/anshkanyadi/rift/net"
	"github.com/anshkanyadi/rift/sim"
)

// Transport is one node's view of the cluster. It implements sim.Transport.
type Transport struct {
	self  sim.NodeID
	peers map[sim.NodeID]*peer

	mu      sync.Mutex
	dropped uint64 // sends discarded because a peer's queue was full or absent
	sent    uint64 // envelopes handed to a peer's queue
	wire    uint64 // BYTES ACTUALLY WRITTEN TO A SOCKET
	recv    uint64 // bytes read off a socket
}

// wireBytes and recvBytes are the counters that prove the cluster is REAL.
//
// A queued send proves a client called Send. Only BYTES ON A SOCKET prove a
// process talked to another process -- and I2's whole claim is that these are
// separate processes over a network. BUG-046 is the measured instance of what
// the alternative looks like: a run reporting a byte-identical trace hash from
// an engine it never opened, indistinguishable from success by every other
// signal.
//
//	A CLUSTER THAT RUNS IN ONE PROCESS WITH A LOOPBACK THAT NEVER LEAVES
//	USERSPACE REPORTS A CLEAN HISTORY AND LOOKS EXACTLY LIKE SUCCESS. The
//	counters go in before the first green, not after it.

type peer struct {
	addr string
	out  chan sim.Envelope
	done chan struct{}
}

// queueDepth bounds a per-peer send queue.
//
// The number is a policy and is stated rather than tuned: deep enough that a
// brief stall does not drop, shallow enough that a peer down for a second does
// not accumulate a second's traffic. A dead peer's queue fills and every
// subsequent send is dropped, which is the behaviour a partition has in sim.
const queueDepth = 256

// New builds a transport for self, with peer addresses.
func New(self sim.NodeID, addrs map[sim.NodeID]string) *Transport {
	t := &Transport{self: self, peers: make(map[sim.NodeID]*peer, len(addrs))}
	for id, a := range addrs {
		if id == self {
			continue
		}
		p := &peer{addr: a, out: make(chan sim.Envelope, queueDepth), done: make(chan struct{})}
		t.peers[id] = p
		go t.pump(p)
	}
	return t
}

// Send is fire-and-forget. A full queue or an unknown peer drops the message.
func (t *Transport) Send(e sim.Envelope) {
	p, ok := t.peers[e.To]
	if !ok {
		t.count(&t.dropped)
		return
	}
	select {
	case p.out <- e:
		t.count(&t.sent)
	default:
		// DROP RATHER THAN BLOCK. Blocking here would stall the node loop on a
		// peer that is down, which turns one dead node into a cluster-wide
		// stall -- and it would do so through the transport, which the protocol
		// above is entitled to treat as unreliable and nothing more.
		t.count(&t.dropped)
	}
}

// Counters reports what this transport did. A chaos run gates on these: a run
// that sent nothing is indistinguishable from a clean one by every other means.
func (t *Transport) Counters() (sent, dropped, wireBytes uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sent, t.dropped, t.wire
}

// AddRecv records bytes read off a socket. Listen's caller reports them,
// because the listener is not owned by any one Transport.
func (t *Transport) AddRecv(n uint64) {
	t.mu.Lock()
	t.recv += n
	t.mu.Unlock()
}

// RecvBytes is the receiving half of the reality check. Sent bytes prove
// somebody wrote; received bytes prove somebody else read.
func (t *Transport) RecvBytes() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.recv
}

func (t *Transport) count(p *uint64) {
	t.mu.Lock()
	*p++
	t.mu.Unlock()
}

// pump owns one peer's connection: dial, write frames, redial on failure.
//
// It is the only goroutine that touches this peer's socket, so the connection
// needs no lock. Reconnection is unconditional and unlogged-as-an-error: a peer
// being unreachable is a normal state in this system, not an exception.
func (t *Transport) pump(p *peer) {
	var conn net.Conn
	for {
		select {
		case <-p.done:
			if conn != nil {
				_ = conn.Close()
			}
			return
		case e := <-p.out:
			if conn == nil {
				c, err := net.DialTimeout("tcp", p.addr, time.Second)
				if err != nil {
					// The message is dropped. See the package comment: this is
					// the partition case and the protocol tolerates it.
					continue
				}
				conn = c
			}
			if err := riftnet.WriteFrame(conn, e); err != nil {
				_ = conn.Close()
				conn = nil
			} else {
				t.mu.Lock()
				t.wire += uint64(e.Size())
				t.mu.Unlock()
			}
		}
	}
}

// Close stops every pump.
func (t *Transport) Close() {
	for _, p := range t.peers {
		close(p.done)
	}
}

// Listen accepts connections and hands each decoded envelope to deliver.
//
// deliver is called from the ACCEPT goroutine, not the node loop. The caller is
// responsible for posting into a mailbox -- Amendment A1's rule, and the reason
// this package cannot simply call Handle.
func Listen(addr string, deliver func(sim.Envelope)) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return // the listener was closed
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					e, err := riftnet.ReadFrame(c)
					if err != nil {
						return // EOF or a peer that died mid-frame; both end this conn
					}
					deliver(e)
				}
			}(c)
		}
	}()
	return ln, nil
}
