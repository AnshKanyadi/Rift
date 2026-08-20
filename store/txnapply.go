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
func (n *Replica) applyTxn(b *engine.Batch, c TxnCommand) {
	switch c.Op {
	case OpPrewrite:
		err := n.mvcc.PrewriteInto(b, []byte(c.Key), kv.Lock{
			Primary: []byte(c.Primary), StartTS: c.StartTS, Deadline: c.Deadline,
		}, []byte(c.Value))
		switch {
		case err == nil:
		case errors.Is(err, kv.ErrWriteConflict):
			n.writeConflicts++
		case errors.Is(err, kv.ErrKeyIsLocked):
			n.prewriteBlocked++
		default:
			n.txnRefused++
		}

	case OpCommitKey:
		if err := n.mvcc.CommitInto(b, []byte(c.Key), c.StartTS, c.CommitTS); err != nil {
			n.txnRefused++
		}

	case OpRollbackKey:
		if err := n.mvcc.RollbackInto(b, []byte(c.Key), c.StartTS); err != nil {
			n.txnRefused++
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
		default:
			n.txnRefused++
		}

	case OpResolve:
		n.applyResolve(b, c)

	default:
		n.txnRefused++
	}
}

// applyResolve reads the lock on this key, decides from the primary's record,
// and applies the verdict.
//
// # Why the whole decision happens here rather than at the coordinator
//
// The verdict must be identical on every replica, so it has to be a function of
// applied state at this log position. A resolver that decided on its own machine
// and shipped the answer would be shipping a fact derived somewhere else -- and
// two resolvers racing would ship two answers for one lock.
//
// So the command carries the INPUTS (which key, at what read timestamp) and the
// state machine computes the verdict. The one thing it cannot do is read another
// range: when the primary lives elsewhere the command is a no-op here and the
// coordinator routes the primary half separately, which is D-A6-4's split
// between deciding and applying.
func (n *Replica) applyResolve(b *engine.Batch, c TxnCommand) {
	l, ok, err := n.mvcc.Lock([]byte(c.Key))
	if err != nil || !ok {
		// No lock: somebody else resolved it, or it was never there. Both are
		// ordinary, and neither is a failure.
		return
	}
	primaryHere := n.desc.Contains(l.Primary)
	r, commitTS, err := n.mvcc.ResolveLock([]byte(c.Key), l, c.ReadTS, primaryHere)
	if err != nil {
		n.txnRefused++
		return
	}
	if r == kv.ResolveWait {
		n.resolveWaits++
		return
	}
	if err := n.mvcc.ApplyResolutionInto(b, []byte(c.Key), l, r, commitTS); err != nil {
		n.txnRefused++
	}
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
func (n *Replica) WriteConflicts() int  { return n.writeConflicts }
func (n *Replica) PrewriteBlocked() int { return n.prewriteBlocked }
func (n *Replica) TxnCommitted() int    { return n.txnCommitted }
func (n *Replica) TxnRolledBack() int   { return n.txnRolledBack }
func (n *Replica) TxnRaceLost() int     { return n.txnRaceLost }
func (n *Replica) TxnRefused() int      { return n.txnRefused }
func (n *Replica) ResolveWaits() int    { return n.resolveWaits }
func (n *Replica) RollForwards() int    { return n.mvcc.RollForwards() }
func (n *Replica) RollBacks() int       { return n.mvcc.RollBacks() }

var _ = fmt.Sprintf
