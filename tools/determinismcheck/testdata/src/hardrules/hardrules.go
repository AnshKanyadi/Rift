// Package hardrules is the fixture for the one line the escape hatch does not
// cross. Ruled 2026-08-11: a hatch never sanctions go, select, chan or sync in
// core scope. Either the concurrency moves out of core, or the design is wrong,
// and neither of those is something a comment can fix.
//
// Every hatch below is well-formed and carries a reason. Every one of them is
// refused, and none of them is then reported as unused -- the author gets the
// diagnostic that matters and not a second one about their annotation.
package hardrules

import (
	//rift:allow-nondeterminism fixture: a hatch may not sanction sync
	"sync" // want `import: importing "sync".*does not apply`

	//rift:allow-nondeterminism fixture: a hatch may not sanction atomics either
	"sync/atomic" // want `import: importing "sync/atomic".*does not apply`
)

// An atomic in a single-threaded core is a concurrency smell with no
// legitimate use there: whoever reached for it believes two things are running
// at once, and if they are right the event loop has been bypassed.
type driver struct {
	seq atomic.Uint64
	mu  sync.Mutex

	//rift:allow-nondeterminism fixture: a hatch may not sanction a channel
	inbox chan int // want `concurrency: channel types.*does not apply`
}

func (d *driver) start() {
	//rift:allow-nondeterminism fixture: a hatch may not sanction a goroutine
	go d.loop() // want `concurrency: go statements.*does not apply`
}

func (d *driver) loop() {
	//rift:allow-nondeterminism fixture: a hatch may not sanction a select
	select { // want `concurrency: select statements.*does not apply`
	default:
	}
}
