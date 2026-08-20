package store

import (
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/raft"
)

// ReplayMachine rebuilds a range's state machine from its birth payload and its
// committed log, in a fresh engine that has never been anything else.
//
// # What this is for, and the independence it keeps and the independence it gives up
//
// Snapshot equivalence asks: *is a snapshot exactly the state the committed log
// produces at its index?* Through A5 the harness answered by REIMPLEMENTING the
// state machine, so that a defect in applying commands could not cancel out on
// both sides of the comparison. That was the right trade while a command was a
// put: the model was ten lines.
//
// A6 makes a command a step of a two-phase commit with locks, write records,
// transaction records, expiry, and resolution that depends on which range holds
// a primary. A hand-written second implementation of that is a second
// implementation of Percolator, and this project has the measurement: in one
// sitting, five divergences, **every one of them a defect in the model** --
// inherited records dropped at a split, versions a rollback removes, locks the
// split leaves behind, resolution of a primary the range cannot see. A checker
// wrong that often is worse than no checker; those are BUG-016's words and they
// were right.
//
// So the trade is made deliberately and it is stated where it happens:
//
//	KEPT.      The replay is an independent EXECUTION -- a fresh engine, from the
//	           birth payload, through the committed log, with no access to any
//	           running node. It catches a snapshot taken at the wrong index, a
//	           snapshot that drops a record kind, an install that loses state,
//	           and any divergence between two replicas of one range.
//	GIVEN UP.  It no longer catches an apply path that is wrong in the SAME way
//	           on both sides.
//
// That last property has not gone unguarded. It moves to the three A6 oracles --
// transaction atomicity, snapshot isolation and bank conservation -- which are
// written against harness-observed CLIENT facts and share no code with the apply
// path at all. They are the independent judgement now, and they judge the thing
// that matters: what a client can see.
func ReplayMachine(base []byte, entries []raft.Entry) (RangeDescriptor, hlc.Timestamp, []kv.Record, bool) {
	desc, mark, recs, ok := decodeMachine(base)
	if !ok {
		return desc, mark, nil, false
	}
	db := model.New()
	s, err := kv.NewStore(db, rangePrefix(desc.ID))
	if err != nil {
		return desc, mark, nil, false
	}
	seed := engine.NewBatch()
	s.IngestRecordsInto(seed, recs, mark)
	if _, err := db.Apply(seed, false); err != nil {
		return desc, mark, nil, false
	}

	flush := func(b *engine.Batch) {
		if b.Empty() {
			return
		}
		if _, err := db.Apply(b, false); err != nil {
			panic("store: replay cannot apply into its own engine: " + err.Error())
		}
		b.Reset()
	}

	b := engine.NewBatch()
	for _, e := range entries {
		if len(e.Data) == 0 {
			continue
		}
		switch {
		case isTxnCommand(e.Data):
			c, ok := decodeTxnCommand(e.Data)
			if !ok || !desc.Contains([]byte(c.Key)) {
				continue
			}
			flush(b)
			applyTxnTo(s, b, c, desc)
			flush(b)

		case isSplitCommand(e.Data):
			spec, ok := decodeSplitCommand(e.Data)
			if !ok {
				continue
			}
			if spec.Left.Epoch != desc.Epoch+1 ||
				string(spec.Left.Start) != string(desc.Start) ||
				string(spec.Right.End) != string(desc.End) {
				continue
			}
			flush(b)
			all, err := s.Records()
			if err != nil {
				return desc, mark, nil, false
			}
			var kept []kv.Record
			for _, rec := range all {
				userKey, ok := s.UserKeyOf(rec.Key)
				if ok && spec.Left.Contains(userKey) {
					kept = append(kept, rec)
				}
			}
			s.IngestRecordsInto(b, kept, s.GCMark())
			flush(b)
			desc = spec.Left

		default:
			op, k, v, at := decodeCmd(e.Data)
			switch op {
			case opGC:
				flush(b)
				if _, err := s.AdvanceGCInto(b, at); err == nil {
					flush(b)
				} else {
					b.Reset()
				}
			case "put":
				if desc.Contains([]byte(k)) {
					_ = s.PutInto(b, []byte(k), at, []byte(v))
				}
			}
		}
	}
	flush(b)

	out, err := s.Records()
	if err != nil {
		return desc, mark, nil, false
	}
	return desc, s.GCMark(), out, true
}

// applyTxnTo is the apply path for a transaction step, with no Replica around
// it.
//
// Factored out so the driver and the replay run the SAME code. Two copies of
// this would be the thing ReplayMachine's comment says not to build, one level
// down.
func applyTxnTo(s *kv.Store, b *engine.Batch, c TxnCommand, desc RangeDescriptor) {
	switch c.Op {
	case OpPrewrite:
		_ = s.PrewriteInto(b, []byte(c.Key), kv.Lock{
			Primary: []byte(c.Primary), StartTS: c.StartTS, Deadline: c.Deadline,
		}, []byte(c.Value))
	case OpCommitKey:
		_ = s.CommitInto(b, []byte(c.Key), c.StartTS, c.CommitTS)
	case OpRollbackKey:
		_ = s.RollbackInto(b, []byte(c.Key), c.StartTS)
	case OpPutTxnRecord:
		_ = s.PutTxnInto(b, []byte(c.Key), kv.TxnRecord{
			Status: c.Status, StartTS: c.StartTS, CommitTS: c.CommitTS,
		})
	case OpResolve:
		l, ok, err := s.Lock([]byte(c.Key))
		if err != nil || !ok {
			return
		}
		r, commitTS, err := s.ResolveLock([]byte(c.Key), l, c.ReadTS, desc.Contains(l.Primary))
		if err != nil || r == kv.ResolveWait {
			return
		}
		_ = s.ApplyResolutionInto(b, []byte(c.Key), l, r, commitTS)
	}
}
