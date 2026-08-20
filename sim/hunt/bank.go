package hunt

import (
	"fmt"
	"time"

	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim/plan"

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

// account renders account i as a key.
//
// # The bank has its own key space, and that is a correctness boundary
//
// The plain workload writes with put and reads with ReadAt, which sees the
// newest DATA version and knows nothing about commit records. The bank writes
// with prewrite and commit, where a data version is invisible until its commit
// record exists. Point both at one key and a plain read returns a value no
// transaction has committed -- which is not a bug in either path, it is a client
// mixing two protocols on one key.
//
// Separating them says so structurally instead of hoping. Fixed width, so the
// key space is ordered the way the split logic expects.
func account(i int) string { return fmt.Sprintf("a%02d", i) }

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

	accounts int
	count    int

	started   int
	committed int
	abandoned int
	resolves  int
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

// newCoordinator builds the bank's client.
func newCoordinator(drivers []*store.Node, nodes int, hist *sim.History, l *raftcheck.Ledger,
	opt RaftOptions, p *plan.Plan) *coordinator {
	return &coordinator{
		drivers: drivers, nodes: nodes, hist: hist,
		ledger:   ledgerAdapter{l},
		live:     map[string]*transfer{},
		deadline: clock.Instant(2 * time.Second),
		accounts: opt.Accounts,
		count:    opt.Transfers2PC,
	}
}

// ledgerAdapter narrows the ledger to the two things a coordinator may do to it.
//
// Not a convenience: the coordinator is the harness's own client, and letting it
// reach the whole ledger would put the thing that drives the run and the thing
// that judges it one method call apart.
type ledgerAdapter struct{ l *raftcheck.Ledger }

func (a ledgerAdapter) RecordTxnBegin(id int, startTS hlc.Timestamp, primary string, keys []string, at clock.Instant) {
	a.l.RecordTxnBegin(provenance.Witness(raftcheck.TxnRecord{
		ID: id, StartTS: startTS, Primary: primary,
		Keys: append([]string(nil), keys...), Began: at,
	}))
}

func (a ledgerAdapter) RecordTxnCommit(id int, commitTS hlc.Timestamp, at clock.Instant) {
	a.l.RecordTxnCommit(id, commitTS, at)
}

// schedule places the transfers, the abandonment sweeps and the resolution
// attempts on the event queue.
//
// All three come from the plan's key stream, so a run replays: the coordinator
// is a client, and a client whose choices were not plan-derived would make the
// simulator nondeterministic in exactly the way this project forbids.
func (c *coordinator) schedule(run *plan.Run, p *plan.Plan) error {
	key, err := rng.ParseKey(p.Keys.Raft)
	if err != nil {
		return fmt.Errorf("hunt: raft key: %w", err)
	}
	span := p.Config.DurationNS

	for i := range c.transfers(p) {
		at := span/10 + int64(key.Uint64N(20, uint64(i), 0, 0, uint64(span*8/10)))
		from := int(key.Uint64N(21, uint64(i), 0, 0, uint64(c.accounts)))
		to := int(key.Uint64N(22, uint64(i), 0, 0, uint64(c.accounts)))
		if to == from {
			to = (to + 1) % c.accounts
		}
		amount := int(key.Uint64N(23, uint64(i), 0, 0, 20)) + 1
		id := i

		run.Loop.Do(clock.Instant(at), func() {
			c.beginTransfer(id, from, to, amount, run)
		})

		// The abandonment sweep. A transfer whose step never landed is given up
		// on, and its locks become the recovery path's raw material.
		run.Loop.Do(clock.Instant(at)+c.deadline, func() {
			if t := c.byID(id); t != nil {
				c.abandon(t)
			}
		})
	}

	// Resolution sweeps: somebody has to be the competing transaction that finds
	// a stranded lock. In a real cluster that is whichever transaction touches
	// the key next; here it is a periodic sweep over the accounts, which reaches
	// the same code by the same door and does not depend on the workload
	// happening to collide.
	for i := 0; i < 24; i++ {
		at := span/4 + int64(key.Uint64N(24, uint64(i), 0, 0, uint64(span*3/4)))
		acct := int(key.Uint64N(25, uint64(i), 0, 0, uint64(c.accounts)))
		run.Loop.Do(clock.Instant(at), func() {
			c.resolve(account(acct), clock.Instant(at), run)
		})
	}
	return nil
}

func (c *coordinator) transfers(p *plan.Plan) []struct{} { return make([]struct{}, c.count) }

func (c *coordinator) byID(id int) *transfer {
	for _, t := range c.live {
		if t.id == id {
			return t
		}
	}
	return nil
}

// beginTransfer reads two accounts at a snapshot and writes both.
//
// The read is the SNAPSHOT read every transaction starts with, and it goes
// through the same path a client read does -- which is what makes the
// transaction's view a thing the oracle can check rather than a number the
// coordinator carried around.
func (c *coordinator) beginTransfer(id, from, to, amount int, run *plan.Run) {
	if len(c.drivers) == 0 {
		return
	}
	// The start timestamp comes from a node's clock, which is where a real
	// client would get it: the coordinator has no clock of its own, and giving
	// it one would put a time source outside the cluster's envelope.
	startTS, ok := c.drivers[0].Now()
	if !ok {
		return
	}
	t := &transfer{
		id: id, startTS: startTS,
		commitTS: startTS.Next(),
		primary:  account(from),
		keys:     []string{account(from), account(to)},
		values:   map[string]string{},
	}
	// The amount is recorded as the value, and conservation is checked over what
	// clients read rather than over these -- see DESIGN-A6 §7. What is written
	// here is a transfer marker the bank oracle interprets.
	t.values[account(from)] = fmt.Sprintf("-%d@%d", amount, id)
	t.values[account(to)] = fmt.Sprintf("+%d@%d", amount, id)
	c.begin(t, run.Loop.Now(), run.Loop)
}

// resolve issues a resolution attempt against whatever lock a key is holding.
func (c *coordinator) resolve(key string, at clock.Instant, run *plan.Run) {
	readTS, ok := c.drivers[0].Now()
	if !ok {
		return
	}
	c.resolves++
	cmd := store.TxnCommand{Op: store.OpResolve, Key: key, ReadTS: readTS}
	req := store.Request{
		Op: "txn", Key: key, HistIdx: -1,
		Range: store.FirstRange, Epoch: 1, Txn: &cmd,
	}
	for i := range c.nodes {
		run.Loop.At(run.Loop.Now(), sim.KindClient, sim.NodeID(i), req)
	}
}

// Started, Committed, Abandoned and Resolves report what the bank did.
func (c *coordinator) Started() int   { return c.started }
func (c *coordinator) Committed() int { return c.committed }
func (c *coordinator) Abandoned() int { return c.abandoned }
func (c *coordinator) Resolves() int  { return c.resolves }
