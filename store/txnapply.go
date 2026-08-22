package store

import (
	"errors"
	"fmt"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
)

// applyTxn applies one transaction step to this replica's state machine.
//
// # Refusals are outcomes, not failures, and they are counted rather than
// # panicked
//
// A prewrite can find a write conflict or a live lock; a transaction record can
// already exist. Every one of those is a legitimate thing for the log to
// contain, because the coordinator that proposed it was working from a view that
// has since moved -- and every replica reaches the SAME refusal, because the
// refusal is a function of the applied state at this position and of nothing
// else.
//
// What must never happen is a refusal that differs between replicas. That is why
// nothing here consults a clock, and why the resolve step carries the timestamp
// it judges an expiry against.
func (n *Replica) applyTxn(b *engine.Batch, c TxnCommand) TxnResult {
	// The counters are the driver's; the EFFECT is applyTxnTo's, which the
	// replay runs too. One apply implementation, two callers -- see
	// store.ReplayMachine for why that is the arrangement and what it costs.
	var res TxnResult
	switch c.Op {
	case OpPrewrite:
		err := n.mvcc.PrewriteInto(b, []byte(c.Key), kv.Lock{
			Primary: []byte(c.Primary), StartTS: c.StartTS, Deadline: c.Deadline,
		}, []byte(c.Value))
		switch {
		case err == nil:
		case errors.Is(err, kv.ErrWriteConflict):
			n.writeConflicts++
			res.Rejected = true
		case errors.Is(err, kv.ErrKeyIsLocked):
			n.prewriteBlocked++
			res.Rejected = true
		default:
			n.txnRefused++
			res.Rejected = true
		}

	case OpPutTxnRecord:
		err := n.mvcc.PutTxnInto(b, []byte(c.Key), kv.TxnRecord{
			Status: c.Status, StartTS: c.StartTS, CommitTS: c.CommitTS,
		})
		switch {
		case err == nil:
			if c.Status == kv.TxnCommitted {
				n.txnCommitted++
			} else {
				n.txnRolledBack++
			}
		case errors.Is(err, kv.ErrTxnAlreadyDecided):
			// Somebody decided first. This is the race the rule exists for, and
			// losing it is an outcome: the coordinator that proposed this will
			// read the record and abort.
			n.txnRaceLost++
			res.Rejected = true
		default:
			n.txnRefused++
			res.Rejected = true
		}

	case OpResolveStatus:
		// # Counted precisely, because the label is a claim
		//
		// This counter used to read "did a resolve change anything", which
		// lumped three unlike outcomes together: a live owner correctly left
		// alone, a lock that had already gone, and a primary this range could
		// not see. The exit run printed the total as "waited on a live owner",
		// which was true of some of it. A count whose label is wrong is worse
		// than no count, because it looks like evidence.
		rec, ok, err := n.mvcc.Txn([]byte(c.Key), c.StartTS)
		switch {
		case err != nil:
			n.txnRefused++
		case ok:
			// Already decided. The verdict is whatever the record says, and
			// nothing here may change it.
			n.resolveAlreadyDecided++
			res.Decided, res.Rollback = true, rec.Status != kv.TxnCommitted
			res.CommitTS = rec.CommitTS
		case c.ExpireAt.LessEq(c.Deadline):
			n.resolveWaits++
		default:
			// Past its deadline and undecided: MAKE it dead.
			n.declareDead(b, c.Key, c.StartTS)
			n.resolveDeclaredDead++
			res.Decided, res.Rollback = true, true
		}
		applyTxnTo(n.mvcc, b, c, n.desc)

	case OpApplyResolution:
		before := n.mvcc.RollForwards() + n.mvcc.RollBacks()
		applyTxnTo(n.mvcc, b, c, n.desc)
		if n.mvcc.RollForwards()+n.mvcc.RollBacks() == before {
			n.resolveNoLock++
		}

	default:
		applyTxnTo(n.mvcc, b, c, n.desc)
	}
	return res
}

// DeclareDeadInto stages the rollback record that makes a stalled transaction
// dead, on its primary's range.
//
// Separate from applyResolve because it lands on a different range, and because
// the two are different claims: this one DECIDES, and the other one applies a
// decision. Conflating them is how a resolver ends up deciding a transaction it
// cannot see the record of.
func (n *Replica) declareDead(b *engine.Batch, primary string, startTS hlc.Timestamp) {
	err := n.mvcc.PutTxnInto(b, []byte(primary), kv.TxnRecord{
		Status: kv.TxnRolledBack, StartTS: startTS,
	})
	switch {
	case err == nil:
		n.txnRolledBack++
	case errors.Is(err, kv.ErrTxnAlreadyDecided):
		n.txnRaceLost++
	default:
		n.txnRefused++
	}
}

// The transaction counters. Every one is asserted somewhere in the exit run: a
// count nobody asserts on is decoration that looks like evidence.
func (n *Replica) WriteConflicts() int        { return n.writeConflicts }
func (n *Replica) PrewriteBlocked() int       { return n.prewriteBlocked }
func (n *Replica) TxnCommitted() int          { return n.txnCommitted }
func (n *Replica) TxnRolledBack() int         { return n.txnRolledBack }
func (n *Replica) TxnRaceLost() int           { return n.txnRaceLost }
func (n *Replica) TxnRefused() int            { return n.txnRefused }
func (n *Replica) ResolveWaits() int          { return n.resolveWaits }
func (n *Replica) ResolveAlreadyDecided() int { return n.resolveAlreadyDecided }
func (n *Replica) ResolveDeclaredDead() int   { return n.resolveDeclaredDead }
func (n *Replica) ResolveNoLock() int         { return n.resolveNoLock }
func (n *Replica) RollForwards() int          { return n.mvcc.RollForwards() }
func (n *Replica) RollBacks() int             { return n.mvcc.RollBacks() }

var _ = fmt.Sprintf
