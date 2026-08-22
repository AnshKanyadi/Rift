package store

import (
	"encoding/binary"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
)

// txnMarker distinguishes a transaction command from a client put/get and from a
// split, in the same way splitMarker does: raft carries bytes, and the state
// machine is the only thing that knows what they mean.
const txnMarker byte = 0xFE

// TxnOp is what a transaction command asks the state machine to do.
type TxnOp uint8

const (
	OpPrewrite TxnOp = iota + 1
	OpCommitKey
	OpRollbackKey
	OpPutTxnRecord

	// OpTxnGet is a transaction's SNAPSHOT READ: the value of a key visible at
	// a timestamp, under the three questions kv.GetTxn answers.
	//
	// # Why a read is a log command here
	//
	// For the same reason a plain get is (store/node.go onClient): serving it
	// from the leader's applied state is a stale read, and read index is A7.
	// It costs a round of replication per read and BENCHMARKS.md will say so.
	//
	// # It stages nothing, and that is what makes it safe to answer locally
	//
	// The entry commits, every replica applies it as a no-op, and the PROPOSER
	// alone evaluates the read against the state that entry's position
	// produces. So the answer is a function of the log -- which is the property
	// BUG-018 broke in the other direction.
	OpTxnGet

	// OpResolveStatus is the PRIMARY half of resolution: read the transaction's
	// record on the range that holds its primary key and, if the owner is past
	// its deadline and nobody has decided, declare it dead.
	//
	// # Why resolution is two commands and not one
	//
	// A lock names a primary key, and that key may be on another range -- after
	// a split it very often is. One command cannot do both halves: the decision
	// has to be read (or made) on the primary's range, and the effect has to be
	// applied on the locked key's range, and no state machine may read another
	// range's state. DESIGN-A6 section 5 says so; the single-command form simply
	// gave up whenever the primary was elsewhere, which meant a cross-range lock
	// could never be cleared by anybody.
	OpResolveStatus

	// OpApplyResolution is the SECONDARY half: apply a decision somebody else
	// reached, to one key.
	//
	// It carries the verdict rather than re-deriving it, for the reason every
	// A6 command carries its timestamps: a replica applying this must reach the
	// same state as every other replica applying it, and re-deriving would let
	// two replicas read two different primary records.
	OpApplyResolution
)

func (o TxnOp) String() string {
	switch o {
	case OpPrewrite:
		return "prewrite"
	case OpCommitKey:
		return "commit-key"
	case OpRollbackKey:
		return "rollback-key"
	case OpPutTxnRecord:
		return "put-txn"
	case OpTxnGet:
		return "txn-get"
	case OpResolveStatus:
		return "resolve-status"
	case OpApplyResolution:
		return "apply-resolution"
	}
	return "?"
}

// TxnCommand is one step of a transaction, as it travels the log.
//
// # Every field it needs, carried rather than re-derived
//
// A replica applying this must reach the same conclusion as every other replica
// applying it, so nothing here may be recomputed from local state: not the
// timestamps, not the deadline, not the verdict's inputs. That is DESIGN-A6 §8's
// rule and A4's class in the dimension A5 opened, and it is why a command this
// wide is the right shape.
type TxnCommand struct {
	Op       TxnOp
	Key      string
	Value    string
	Primary  string
	StartTS  hlc.Timestamp
	CommitTS hlc.Timestamp
	Deadline hlc.Timestamp

	// Status is the decision a put-txn command records.
	Status kv.TxnStatus

	// ReadTS is the client's SNAPSHOT: the instant a read is asking about, and
	// on a resolve, the snapshot of the reader that ran into the lock.
	ReadTS hlc.Timestamp

	// Origin identifies the CLIENT REQUEST this step belongs to, so an answer
	// can find its way back to the client that asked.
	//
	// # Why a start timestamp cannot do this job
	//
	// Percolator identifies a transaction by its start timestamp, and that works
	// there because a single TSO issues them. Here every node has its own HLC,
	// and two nodes can mint the identical (wall, logical) pair -- so two
	// concurrent transactions can share a start timestamp. Routing answers by it
	// delivered one transaction's read to another, which then prewrote a balance
	// it had never read; bank-conservation caught the money that invented.
	//
	// It rides in the command rather than beside it for the same reason
	// raft.ProposalID does: the answer comes back from the apply path, and
	// anything not in the entry is not there when the answer is formed.
	Origin uint64

	// ExpireAt is the timestamp a resolve judges a lock's TTL against, chosen by
	// the resolver at propose time and carried so that every replica compares
	// the same two values rather than each consulting its own clock.
	//
	// # Why this is not ReadTS, which is where it started
	//
	// A lock's deadline is fixed and a reader's snapshot is fixed, so a resolver
	// judging expiry against the reader's snapshot reaches the same verdict
	// forever: a reader whose snapshot predates the deadline can never expire
	// the lock, however long its owner has been dead. Measured, before they were
	// separated: 8977 waits and 9 completed audits across 20 seeds.
	//
	// The determinism requirement was never that the two be the same value --
	// it is that the value be CARRIED rather than read from a clock at apply
	// time, and both of these are (DESIGN-A6 section 8).
	ExpireAt hlc.Timestamp

	// MaxTS is the top of this transaction's uncertainty interval, fixed at its
	// FIRST snapshot and carried unchanged through every restart. Unset on a
	// transaction's first read, where the node computes it from the advertised
	// bound and reports it back. See kv.UncertaintyCeiling for why it may not
	// move.
	MaxTS hlc.Timestamp
}

func isTxnCommand(data []byte) bool { return len(data) > 0 && data[0] == txnMarker }

func encodeTxnCommand(c TxnCommand) []byte {
	b := []byte{txnMarker, byte(c.Op), byte(c.Status)}
	b = putU64(b, c.Origin)
	b = putBytes(b, []byte(c.Key))
	b = putBytes(b, []byte(c.Value))
	b = putBytes(b, []byte(c.Primary))
	for _, t := range []hlc.Timestamp{c.StartTS, c.CommitTS, c.Deadline, c.ReadTS, c.MaxTS, c.ExpireAt} {
		b = putU64(b, uint64(t.Wall))
		b = putU64(b, uint64(t.Logical))
	}
	return b
}

func decodeTxnCommand(data []byte) (TxnCommand, bool) {
	if !isTxnCommand(data) || len(data) < 3 {
		return TxnCommand{}, false
	}
	c := TxnCommand{Op: TxnOp(data[1]), Status: kv.TxnStatus(data[2])}
	b := data[3:]
	var raw []byte
	var ok bool
	if c.Origin, b, ok = takeU64(b); !ok {
		return TxnCommand{}, false
	}
	if raw, b, ok = takeBytes(b); !ok {
		return TxnCommand{}, false
	}
	c.Key = string(raw)
	if raw, b, ok = takeBytes(b); !ok {
		return TxnCommand{}, false
	}
	c.Value = string(raw)
	if raw, b, ok = takeBytes(b); !ok {
		return TxnCommand{}, false
	}
	c.Primary = string(raw)
	for _, dst := range []*hlc.Timestamp{&c.StartTS, &c.CommitTS, &c.Deadline, &c.ReadTS, &c.MaxTS, &c.ExpireAt} {
		var w, l uint64
		if w, b, ok = takeU64(b); !ok {
			return TxnCommand{}, false
		}
		if l, b, ok = takeU64(b); !ok {
			return TxnCommand{}, false
		}
		*dst = hlc.Timestamp{Wall: clock.NewWall(int64(w)), Logical: uint32(l)}
	}
	return c, true
}

// DecodeTxnCommand exposes the wire format to the harness, which models the
// state machine independently and has to apply transaction steps to stay
// faithful to it.
//
// The FORMAT crosses this boundary and the SEMANTICS do not: the harness
// restates what a prewrite and a commit do, so a defect in applying them cannot
// cancel out on both sides of the comparison.
func DecodeTxnCommand(data []byte) (TxnCommand, bool) { return decodeTxnCommand(data) }

var _ = binary.BigEndian

// TxnResult is what a transaction step produced for the client that issued it.
//
// # Why a step has a result at all, when four of the five change state
//
// Because the fifth is a read, and a read whose answer never reaches the client
// is not a read. The four writing steps report an empty result and the
// coordinator advances on the fact that the step landed, exactly as before.
//
// # Locked and Uncertain are ANSWERS, not errors
//
// A read that finds a lock has not failed: it has discovered that the value at
// its timestamp depends on a transaction nobody has decided, and the protocol's
// response is to decide it (§5). A read inside the uncertainty interval has not
// failed either: it has discovered that its snapshot is not safely below a
// commit, and the response is to restart above it. Reporting either as an error
// would collapse two decidable outcomes into "something went wrong", and the
// bank would then have no way to exercise the two paths that make Percolator
// work.
type TxnResult struct {
	Value string
	Found bool

	// Locked, with the lock's owner, when the read found a lock at or below its
	// timestamp. The primary is carried because resolution is routed by the
	// PRIMARY KEY, not by the range the lock was written on (D-A6-1).
	Locked      bool
	LockPrimary string
	LockStartTS hlc.Timestamp

	// LockDeadline is the lock's TTL, reported so the reader can carry it to the
	// primary's range: the range that decides the expiry is not the range that
	// holds the lock, so the deadline has to travel with the question.
	LockDeadline hlc.Timestamp

	// Decided, with the verdict, is what a resolve-status step answers. Waiting
	// is reported as not-decided rather than as an error: the owner being alive
	// is the correct outcome and the reader tries again later.
	Decided  bool
	Rollback bool

	// Uncertain, with the commit that caused it and the timestamp to restart
	// at. RestartAt comes from the error rather than being recomputed here: it
	// must be strictly above the observed commit, which is not derivable from
	// the read timestamp and the bound (kv.UncertaintyError.RestartAt).
	Uncertain bool
	CommitTS  hlc.Timestamp
	RestartAt hlc.Timestamp

	// Refused, when the read named a timestamp at or below the collection mark.
	// An outcome, and the one A5 built the mark's refusal for.
	Refused bool

	// Ceiling is the top of the uncertainty interval this read actually used. A
	// transaction carries it forward so its restarts keep the interval its first
	// snapshot had, which is what makes them terminate.
	Ceiling hlc.Timestamp

	// Rejected says a WRITING step did not take effect: a prewrite that found a
	// lock or a newer commit, or a transaction record somebody else had already
	// written.
	//
	// # Why this has to reach the client
	//
	// Because the alternative is a coordinator that commits a transaction one of
	// whose prewrites never landed -- and the commit record then names a version
	// that does not exist, which kv.GetTxn reports as "a commit record outlived
	// the value it makes visible". First-committer-wins is only a rule if the
	// loser is told it lost.
	Rejected bool
}
