// Package coreclean is the false-positive guard: ordinary core-package code
// that must produce no diagnostics at all. A determinism pass that cries wolf
// gets an escape hatch pasted over it within a fortnight, so this fixture is
// the one that matters when a rule is tightened.
package coreclean

import (
	"fmt"
	"sort"
	"time"
)

type peer struct {
	id   uint64
	next uint64
}

type node struct {
	// The map is for lookup; order comes from the slice beside it, which is
	// the pattern core packages use instead of ranging the map.
	peers   map[uint64]*peer
	order   []uint64
	timeout time.Duration
	now     int64 // nanoseconds, from the injected Clock
}

func (n *node) addPeer(p *peer) {
	n.peers[p.id] = p
	n.order = append(n.order, p.id)
	sort.Slice(n.order, func(i, j int) bool { return n.order[i] < n.order[j] })
}

// broadcast iterates a sorted slice, so the message order is a property of the
// data rather than of the allocator.
func (n *node) broadcast(msg []byte) {
	for _, id := range n.order {
		if p, ok := n.peers[id]; ok {
			p.next += uint64(len(msg))
		}
	}
}

// expired does time arithmetic without reading a clock: durations and instants
// are values, and the instant arrives from outside.
func (n *node) expired(deadline int64) bool {
	return n.now >= deadline+int64(n.timeout)
}

// legalTime is the guard on the time allowlist. Banning time.Now is easy;
// banning it without making time.Duration unusable is the part that decides
// whether anyone can write core code at all. Constants, value types, their
// methods and the deterministic constructors all stay legal.
func (n *node) legalTime(lease time.Time) (time.Duration, string) {
	deadline := lease.Add(3 * time.Second).Add(500 * time.Millisecond)
	epoch := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	window := deadline.Sub(epoch)

	if d, err := time.ParseDuration("250ms"); err == nil && window > d {
		window -= d
	}
	if deadline.Before(epoch) || deadline.Equal(epoch) {
		window = 0
	}
	return window, deadline.Format(time.RFC3339Nano)
}

// utcOnly is the other side of that closure, and the reason the ban is
// affordable: with the Unix family off the allowlist, time.Local banned as a
// var and Local() banned as a method, every time.Time reachable in a core
// package is UTC or an explicit FixedZone. Formatted output therefore cannot
// depend on the host's TZ, which is a property of the rules rather than of
// anyone's discipline.
func (n *node) utcOnly() string {
	at := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	lease := at.Add(30 * time.Second).In(time.FixedZone("fixed", 0))
	return lease.Format(time.RFC3339Nano)
}

func (n *node) describe(p *peer) string {
	return fmt.Sprintf("peer %d next=%d timeout=%v", p.id, p.next, n.timeout)
}

// pct is the %p near-miss: a percentage sign followed by a letter, in an
// argument that is not the format string. Only the format argument is examined,
// so this stays legal.
func (n *node) pct() string {
	return fmt.Sprintf("%s", "100%pure")
}
