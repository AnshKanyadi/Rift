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
	OpResolve // read a lock, decide from the primary's record, apply the verdict
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
	case OpResolve:
		return "resolve"
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

	// ReadTS is the timestamp a resolve command judges an expiry against. It is
	// chosen by the resolver and carried, so every replica compares the same two
	// timestamps rather than each consulting its own clock.
	ReadTS hlc.Timestamp
}

func isTxnCommand(data []byte) bool { return len(data) > 0 && data[0] == txnMarker }

func encodeTxnCommand(c TxnCommand) []byte {
	b := []byte{txnMarker, byte(c.Op), byte(c.Status)}
	b = putBytes(b, []byte(c.Key))
	b = putBytes(b, []byte(c.Value))
	b = putBytes(b, []byte(c.Primary))
	for _, t := range []hlc.Timestamp{c.StartTS, c.CommitTS, c.Deadline, c.ReadTS} {
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
	for _, dst := range []*hlc.Timestamp{&c.StartTS, &c.CommitTS, &c.Deadline, &c.ReadTS} {
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
