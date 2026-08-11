// Package mailboxcore stands in for a core package: a node whose entire public
// vocabulary is Handle, Status and a constructor, with its state unexported so
// the compiler is the first thing stopping a driver from reaching it.
package mailboxcore

type Node struct {
	term    uint64
	applied uint64
}

func New() *Node { return &Node{} }

func (n *Node) Handle(ev int) { n.applied += uint64(ev) }

func (n *Node) Status() uint64 { return n.term }

// Propose is legitimate on the node loop and illegitimate anywhere else, which
// is exactly what makes it the interesting case for the mailbox rule.
func (n *Node) Propose(data []byte) { n.applied += uint64(len(data)) }
