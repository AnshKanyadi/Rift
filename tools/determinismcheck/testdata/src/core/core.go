// Package core is a fixture: every construct the core rules reject, written in
// roughly the shape it would take in real node logic rather than as a list of
// isolated statements, so the diagnostics are exercised where they will fire.
package core

import (
	"fmt"
	"log"       // want `import: importing "log"`
	"maps"      // want `import: importing "maps"`
	"math/rand" // want `import: importing "math/rand"`
	"os"        // want `import: importing "os"`
	"reflect"
	"slices"
	"sync" // want `import: importing "sync"`
	"time"
	"unsafe" // want `import: importing "unsafe"`
)

type peer struct {
	id   uint64
	next uint64
}

type node struct {
	peers    map[uint64]*peer // fine: the key is an id, not an address
	inflight map[*peer]uint64 // want `mapkey: pointer-keyed maps`
	inbox    chan int         // want `concurrency: channel types`
	mu       sync.Mutex
	deadline time.Time
	timeout  time.Duration
}

func (n *node) tick() {
	n.deadline = time.Now().Add(n.timeout) // want `time: time.Now`
	if time.Since(n.deadline) > 0 {        // want `time: time.Since`
		n.campaign()
	}
	time.Sleep(time.Millisecond) // want `time: time.Sleep`
}

// broadcast is the canonical leak: replication order comes out of a map, so the
// same seed produces a different message order on every run.
func (n *node) broadcast(msg []byte) {
	for id := range n.peers { // want `maprange: range over a map`
		n.send(id, msg)
	}
}

func (n *node) drain() {
	for ev := range n.inbox { // want `concurrency: range over a channel`
		_ = ev
	}
}

func (n *node) spawn() {
	go n.tick() // want `concurrency: go statements`

	select { // want `concurrency: select statements`
	case ev := <-n.inbox: // want `concurrency: channel receives`
		_ = ev
	default:
	}
}

func (n *node) post(ev int) {
	n.inbox <- ev // want `concurrency: channel sends`
}

func (n *node) describe(p *peer) string {
	return fmt.Sprintf("peer %p next=%d", p, p.next) // want `pointerfmt: %p formats a pointer`
}

func (n *node) campaign() {
	log.Printf("campaigning after %v", n.timeout)
	fmt.Fprintln(os.Stderr, "campaigning")
}

func (n *node) send(id uint64, msg []byte) {
	n.inflight[n.peers[id]] = uint64(len(msg)) + uint64(rand.Intn(2)) + uint64(unsafe.Sizeof(id))
}

// sortedPeers is the go1.23 iterator hole: not one map-range statement in
// sight, and exactly the same nondeterminism. Banning the import is the only
// place to stand, so the diagnostic is on the import line rather than here.
func (n *node) sortedPeers() []uint64 {
	return slices.Sorted(maps.Keys(n.peers))
}

// reflective is the other half of the hole: map iteration reached through
// method calls, where neither the syntax rule nor an import ban can see it.
// Seq and Seq2 are the same hole in iterator clothing.
func (n *node) reflective(v reflect.Value) int {
	iter := v.MapRange() // want `maprange: reflect.MapRange`
	_ = iter
	for range v.Seq() { // want `maprange: reflect.Seq`
	}
	for range v.Seq2() { // want `maprange: reflect.Seq2`
	}
	return len(v.MapKeys()) // want `maprange: reflect.MapKeys`
}

// stopwatch reaches past time.Now for the rest of the package's real-time
// surface. The allowlist is what makes this exhaustive rather than a list
// somebody remembered to extend.
func (n *node) stopwatch() {
	_ = time.After(n.timeout)                             // want `time: time.After`
	_ = time.Tick(n.timeout)                              // want `time: time.Tick`
	_ = time.NewTimer(n.timeout)                          // want `time: time.NewTimer`
	_ = time.AfterFunc(n.timeout, func() {})              // want `time: time.AfterFunc`
	_ = time.Until(n.deadline)                            // want `time: time.Until`
	_ = n.deadline.In(time.Local)                         // want `time: time.Local`
	if loc, err := time.LoadLocation("UTC"); err == nil { // want `time: time.LoadLocation`
		_ = loc
	}
}
