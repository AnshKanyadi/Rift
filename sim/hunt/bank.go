package hunt

import (
	"fmt"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/store"
)

// The bank workload and its transaction coordinator.
//
// # Why the coordinator lives here and not in store/
//
// A transaction spans ranges, and a range's state machine may not read
// another's. So the thing that sequences prewrite-primary-commit is a CLIENT: it
// issues each step as an ordinary request, waits for the step to apply, and
// moves on. That is what a router does at A4 and what `router/` will hold when
// real mode arrives; putting it in the store would be giving one range's state
// machine a view of the cluster that no state machine has.
//
// # Why a coordinator can die
//
// It is driven by the event loop, so a crash of the node it is talking to leaves
// it waiting, and the workload gives up on it after a deadline. That is the
// whole point: the interesting failures are all in the recovery path of a
// coordinator that died mid-commit, and a workload whose coordinators always
// finish would exercise none of them (DESIGN-A6 §1).

// account renders account i as a key. Fixed width, so the key space is ordered
// the way the split logic expects.
func account(i int) string { return fmt.Sprintf("k%02d", i) }

// txnPhase is where a transfer has got to.
type txnPhase uint8

const (
	phasePrewrite txnPhase = iota
	phasePrimaryRecord
	phaseCommitKeys
	phaseDone
	phaseAbandoned
)

// transfer is one bank transaction: read two accounts at a snapshot, move an
// amount between them, commit both.
type transfer struct {
	id       int
	startTS  hlc.Timestamp
	commitTS hlc.Timestamp
	primary  string
	keys     []string
	values   map[string]string

	phase   txnPhase
	pending int
	node    sim.NodeID
}

// coordinator drives transfers over the store's client interface.
type coordinator struct {
	drivers []*store.Node
	nodes   int
	hist    *sim.History
	ledger  txnLedger

	live map[string]*transfer // keyed by start timestamp

	// deadline is how long a transfer waits for a step before it is abandoned.
	// Abandoned is not failure: a coordinator that stops is exactly the case
	// resolution exists for, and the sweep needs them.
	deadline clock.Instant

	started   int
	committed int
	abandoned int
}

// txnLedger is what the coordinator tells the harness's record. It is an
// interface so the oracle's ledger and a test's stub are interchangeable, and so
// that nothing here can reach into the ledger for anything but recording.
type txnLedger interface {
	RecordTxnBegin(id int, startTS hlc.Timestamp, primary string, keys []string, at clock.Instant)
	RecordTxnCommit(id int, commitTS hlc.Timestamp, at clock.Instant)
}

func (c *coordinator) begin(t *transfer, at clock.Instant, s sim.Scheduler) {
	c.live[t.startTS.String()] = t
	c.started++
	c.ledger.RecordTxnBegin(t.id, t.startTS, t.primary, t.keys, at)
	t.phase = phasePrewrite
	t.pending = len(t.keys)
	for _, k := range t.keys {
		c.send(t, store.TxnCommand{
			Op: store.OpPrewrite, Key: k, Value: t.values[k],
			Primary: t.primary, StartTS: t.startTS,
			Deadline: hlc.Timestamp{Wall: t.startTS.Wall.Add(kv.DefaultTTL)},
		}, at, s)
	}
}

// send issues one step to whichever node will take it.
//
// Every node is offered the request and only the leader of the key's range
// answers, which is the same routing every other client operation uses -- and
// the same reason: the leader's identity is not known until the run produces it.
func (c *coordinator) send(t *transfer, cmd store.TxnCommand, at clock.Instant, s sim.Scheduler) {
	req := store.Request{
		Op: "txn", Key: cmd.Key, HistIdx: -1,
		Range: store.FirstRange, Epoch: 1,
		Txn: &cmd,
	}
	for i := range c.nodes {
		s.At(at, sim.KindClient, sim.NodeID(i), req)
	}
}

// applied advances a transfer when one of its steps lands.
func (c *coordinator) applied(cmd store.TxnCommand, at clock.Instant, s sim.Scheduler) {
	t := c.live[cmd.StartTS.String()]
	if t == nil || t.phase == phaseDone || t.phase == phaseAbandoned {
		return
	}
	switch {
	case cmd.Op == store.OpPrewrite && t.phase == phasePrewrite:
		t.pending--
		if t.pending > 0 {
			return
		}
		// # Every prewrite is durable before the primary's record is proposed
		//
		// The step reported here applied, which means its entry committed, which
		// means a quorum holds it. Proposing the primary's record before that
		// would let a committed transaction lose a value it promised
		// (DESIGN-A6 D-A6-3, ordering 1).
		t.phase = phasePrimaryRecord
		c.send(t, store.TxnCommand{
			Op: store.OpPutTxnRecord, Key: t.primary, Status: kv.TxnCommitted,
			StartTS: t.startTS, CommitTS: t.commitTS,
		}, at, s)

	case cmd.Op == store.OpPutTxnRecord && t.phase == phasePrimaryRecord:
		// The transaction is now COMMITTED, whatever happens next. Everything
		// after this is bookkeeping that a resolver can finish.
		c.committed++
		c.ledger.RecordTxnCommit(t.id, t.commitTS, at)
		t.phase = phaseCommitKeys
		t.pending = len(t.keys)
		for _, k := range t.keys {
			c.send(t, store.TxnCommand{
				Op: store.OpCommitKey, Key: k, StartTS: t.startTS, CommitTS: t.commitTS,
			}, at, s)
		}

	case cmd.Op == store.OpCommitKey && t.phase == phaseCommitKeys:
		t.pending--
		if t.pending == 0 {
			t.phase = phaseDone
			delete(c.live, t.startTS.String())
		}
	}
}

// abandon gives up on a transfer whose step never landed.
//
// The locks it leaves behind are the raw material of the recovery path: another
// transaction will find them, read the primary, and either roll forward or roll
// back. A sweep whose coordinators all finish tests none of that.
func (c *coordinator) abandon(t *transfer) {
	if t.phase == phaseDone || t.phase == phaseAbandoned {
		return
	}
	t.phase = phaseAbandoned
	c.abandoned++
	delete(c.live, t.startTS.String())
}
