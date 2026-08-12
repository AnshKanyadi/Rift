package sim

import (
	"fmt"

	"github.com/anshkanyadi/rift/clock"
)

// NodeID names a node. It is an index into the run's node set, not an address:
// addressing is the transport's business (A0.7).
type NodeID uint32

// Kind is what an event does when it fires. It is an explicit enum rather than
// a Go type switch because the trace hash encodes it, and a hash that depends
// on Go's type identity would not survive a refactor.
type Kind uint8

const (
	KindTick Kind = iota + 1
	KindDeliver
	KindDurable
	KindCrash
	KindRestart
	KindClient

	// KindAction is the harness acting on the run itself: cutting a link,
	// healing one. It is an event like everything else so that a partition
	// takes effect at its instant, ordered against the messages in flight then,
	// rather than being applied out of band at setup.
	KindAction

	numKinds
)

func (k Kind) String() string {
	switch k {
	case KindTick:
		return "tick"
	case KindDeliver:
		return "deliver"
	case KindDurable:
		return "durable"
	case KindCrash:
		return "crash"
	case KindRestart:
		return "restart"
	case KindClient:
		return "client"
	case KindAction:
		return "action"
	case numKinds:
		return "invalid"
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// Event is one thing that happens at one instant of global virtual time.
//
// Everything a node experiences arrives as one of these: a timer firing, a
// message landing, an fsync completing, a process dying. That uniformity is
// what makes the loop a total order over the run rather than a scheduler with
// special cases, and a special case is where a determinism leak would live.
type Event struct {
	At   clock.Instant
	Kind Kind
	Node NodeID

	// Seq is the insertion counter, assigned by the loop. Two events at the
	// same instant are ordered by it, which makes the queue a total order
	// without depending on heap stability -- container/heap is not stable, and
	// assuming otherwise is the classic way a simulator becomes subtly
	// irreproducible (DESIGN-A0 D2).
	Seq uint64

	// Payload carries the event's data: a message, a sequence number, a client
	// operation. The loop never inspects it.
	Payload any
}

func (e Event) String() string {
	return fmt.Sprintf("%s@%d n%d #%d", e.Kind, int64(e.At), e.Node, e.Seq)
}

// before reports whether e sorts before f under the total order
// (at_nanos, insertion_seq).
func (e Event) before(f Event) bool {
	if e.At != f.At {
		return e.At < f.At
	}
	return e.Seq < f.Seq
}

// queue is a binary heap of pending events, ordered by (At, Seq).
//
// Hand-written rather than container/heap: the interface version needs a
// Less/Swap/Push/Pop shim whose panics are worse, and more importantly the
// comparison is the single most load-bearing line in the simulator. It is
// worth having it visible in the file that owns it.
type queue struct {
	events []Event
}

func (q *queue) len() int { return len(q.events) }

func (q *queue) push(e Event) {
	q.events = append(q.events, e)
	i := len(q.events) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !q.events[i].before(q.events[parent]) {
			break
		}
		q.events[i], q.events[parent] = q.events[parent], q.events[i]
		i = parent
	}
}

func (q *queue) pop() (Event, bool) {
	if len(q.events) == 0 {
		return Event{}, false
	}
	top := q.events[0]
	last := len(q.events) - 1
	q.events[0] = q.events[last]
	q.events = q.events[:last]

	i := 0
	for {
		l, r := 2*i+1, 2*i+2
		small := i
		if l < len(q.events) && q.events[l].before(q.events[small]) {
			small = l
		}
		if r < len(q.events) && q.events[r].before(q.events[small]) {
			small = r
		}
		if small == i {
			break
		}
		q.events[i], q.events[small] = q.events[small], q.events[i]
		i = small
	}
	return top, true
}

// peek returns the next event without removing it.
func (q *queue) peek() (Event, bool) {
	if len(q.events) == 0 {
		return Event{}, false
	}
	return q.events[0], true
}
