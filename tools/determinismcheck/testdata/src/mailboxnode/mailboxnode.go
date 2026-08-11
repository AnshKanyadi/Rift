// Package mailboxnode stands in for the real-mode driver: goroutines are its
// job, and every one of them has to enter the node the same way.
package mailboxnode

import "mailboxcore"

type driver struct {
	core *mailboxcore.Node
	mbox chan int
}

func newDriver() *driver {
	// Constructors are permitted: building a node is not touching one.
	return &driver{core: mailboxcore.New(), mbox: make(chan int, 8)}
}

// post is the single writer of the mailbox.
func (d *driver) post(ev int) { d.mbox <- ev }

// loop is the node loop. Handle and Status are the whole vocabulary.
func (d *driver) loop() {
	for ev := range d.mbox {
		d.core.Handle(ev)
		_ = d.core.Status()
	}
}

// receive is a transport goroutine doing it correctly.
func (d *driver) receive(in <-chan int) {
	for ev := range in {
		d.post(ev)
	}
}

// receiveDirect is the same goroutine writing the mailbox itself, which is a
// second writer today and a second entry point tomorrow.
func (d *driver) receiveDirect(in <-chan int) {
	for ev := range in {
		d.mbox <- ev // want `mailbox: channel sends`
	}
}

// applyOffLoop is the bug the rule exists for: core state mutated from whatever
// goroutine happened to be holding the durability completion.
func (d *driver) applyOffLoop(data []byte) {
	go func() {
		d.core.Propose(data) // want `mailbox: .*Propose`
	}()
}

// deferred takes the method as a value, which reaches the same state one step
// later and must be caught the same way.
func (d *driver) deferred() func([]byte) {
	return d.core.Propose // want `mailbox: .*Propose`
}
