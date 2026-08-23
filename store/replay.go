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
	r, ok := NewReplay(base)
	if !ok {
		return RangeDescriptor{}, hlc.Timestamp{}, nil, false
	}
	r.Apply(entries)
	return r.State()
}

// Replay is a resumable ReplayMachine.
//
// # Why resumable, and the number that forced it
//
// Snapshot equivalence checks EVERY snapshot a range takes, and a range takes
// one every few applied entries. Rebuilding from the birth payload for each of
// them is quadratic in the log, and with transactions the log is long: measured,
// it took an A6 seed from 0.35 seconds to 5.2, and a 2,000-seed sweep did not
// finish inside two hours.
//
// A cursor makes it linear. Nothing about the CHECK changes -- each snapshot is
// still compared against the state its own committed prefix produces, through
// the real apply path, in an engine that has never been anything else.
type Replay struct {
	desc RangeDescriptor
	s    *kv.Store
	db   *model.DB
	b    *engine.Batch
	next int
}

// NewReplay starts a replay from a range's birth payload.
func NewReplay(base []byte) (*Replay, bool) {
	desc, mark, recs, ok := decodeMachine(base)
	if !ok {
		return nil, false
	}
	db := model.New()
	s, err := kv.NewStore(db, rangePrefix(desc.ID))
	if err != nil {
		return nil, false
	}
	seed := engine.NewBatch()
	s.IngestRecordsInto(seed, recs, mark)
	if _, err := db.Apply(seed, false); err != nil {
		return nil, false
	}
	return &Replay{desc: desc, s: s, db: db, b: engine.NewBatch()}, true
}

// Apply advances the replay to cover entries, which must be the range's
// committed prefix in index order. Entries already applied are skipped, so a
// caller may hand it a growing prefix.
func (r *Replay) Apply(entries []raft.Entry) {
	for i := r.next; i < len(entries); i++ {
		r.applyOne(entries[i])
	}
	if len(entries) > r.next {
		r.next = len(entries)
	}
	r.flush()
}

// Applied is how many entries this replay has consumed.
func (r *Replay) Applied() int { return r.next }

// State is the replayed state machine.
func (r *Replay) State() (RangeDescriptor, hlc.Timestamp, []kv.Record, bool) {
	r.flush()
	out, err := r.s.Records()
	if err != nil {
		return r.desc, r.s.GCMark(), nil, false
	}
	return r.desc, r.s.GCMark(), out, true
}

func (r *Replay) flush() {
	if r.b.Empty() {
		return
	}
	if _, err := r.db.Apply(r.b, false); err != nil {
		panic("store: replay cannot apply into its own engine: " + err.Error())
	}
	r.b.Reset()
}

func (r *Replay) applyOne(e raft.Entry) {
	if len(e.Data) == 0 {
		return
	}
	switch {
	case isTxnCommand(e.Data):
		c, ok := decodeTxnCommand(e.Data)
		if !ok || !r.desc.Contains([]byte(c.Key)) {
			return
		}
		r.flush()
		applyTxnTo(r.s, r.b, c, r.desc)
		r.flush()

	case isSplitCommand(e.Data):
		spec, ok := decodeSplitCommand(e.Data)
		if !ok {
			return
		}
		if spec.Left.Epoch != r.desc.Epoch+1 ||
			string(spec.Left.Start) != string(r.desc.Start) ||
			string(spec.Right.End) != string(r.desc.End) {
			return
		}
		r.flush()
		all, err := r.s.Records()
		if err != nil {
			return
		}
		var kept []kv.Record
		for _, rec := range all {
			userKey, ok := r.s.UserKeyOf(rec.Key)
			if ok && spec.Left.Contains(userKey) {
				kept = append(kept, rec)
			}
		}
		r.s.IngestRecordsInto(r.b, kept, r.s.GCMark())
		r.flush()
		r.desc = spec.Left

	default:
		op, k, v, at := decodeCmd(e.Data)
		switch op {
		case opGC:
			r.flush()
			if _, err := r.s.AdvanceGCInto(r.b, at); err == nil {
				r.flush()
			} else {
				r.b.Reset()
			}
		case "put":
			if r.desc.Contains([]byte(k)) {
				_ = r.s.PutInto(r.b, []byte(k), at, []byte(v))
			}
		}
	}
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
	case OpTxnGet:
		// # A snapshot read stages its MARK, and nothing else
		//
		// It changed nothing until BUG-022. What it now stages is not a version
		// and not an answer: it is the record that this range has been asked
		// about this key at this timestamp, which is what PrewriteInto's third
		// guard consults. The read's ANSWER is still the proposer's alone and
		// still stages nothing, so the asymmetry Replica.readTxn rests on is
		// unchanged.
		//
		// It is here, in the shared apply path, for the reason everything else
		// is: the driver and the replay run the same code, so the mark is a
		// function of the log on both sides rather than a fact one of them
		// remembers.
		_ = s.NoteReadInto(b, []byte(c.Key), c.ReadTS)

	case OpResolveStatus:
		// The primary's range decides. If the owner is undecided and past its
		// deadline, the decision is MADE here, by writing a rollback record --
		// which is what "the TTL is expiry, not permission" means: nobody
		// concludes the owner is dead, somebody makes it dead, through the log
		// (DESIGN-A6 section 5).
		l := kv.Lock{Primary: []byte(c.Key), StartTS: c.StartTS, Deadline: c.Deadline}
		r, _, err := s.ResolveLock([]byte(c.Key), l, c.ExpireAt, true)
		if err != nil || r != kv.ResolveBack {
			return
		}
		if _, ok, err := s.Txn([]byte(c.Key), c.StartTS); err == nil && !ok {
			_ = s.PutTxnInto(b, []byte(c.Key), kv.TxnRecord{
				Status: kv.TxnRolledBack, StartTS: c.StartTS,
			})
		}

	case OpApplyResolution:
		// The locked key's range applies a verdict reached elsewhere. It still
		// reads its own lock: the verdict says what to do with THIS
		// transaction's version, and a lock that has since gone means somebody
		// already did it.
		l, ok, err := s.Lock([]byte(c.Key))
		if err != nil || !ok || l.StartTS != c.StartTS {
			return
		}
		r := kv.ResolveForward
		if c.Status == kv.TxnRolledBack {
			r = kv.ResolveBack
		}
		_ = s.ApplyResolutionInto(b, []byte(c.Key), l, r, c.CommitTS)
	}
}
