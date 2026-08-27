package differential

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
)

// Judge replays an artifact's submission log into engine/model and compares the
// model's state at the artifact's watermark against what the C++ engine
// recovered.
//
// # The three directions, and only one is obvious
//
//	recovers LESS than w      acknowledged durable data lost. Every crash rig
//	                          catches this one.
//	recovers MORE than w      THE DANGEROUS ONE. Harmless in isolation, and it
//	                          means the watermark is not the durability
//	                          boundary — so a LATER crash at a DIFFERENT point
//	                          loses data the caller was told was safe.
//	recovers NEITHER          a state at no applied sequence at all: torn, or
//	                          interleaved. The case a two-element recovery set
//	                          cannot express.
//
// The second is why engine/model retains every version between durable and
// visible. An engine that rounded its watermark up would be compared against a
// model that rounded it up too, and every disagreement would be blamed on the
// C++ side.
//
// # What this function may consult
//
// The artifact and the model. It never links the C++ engine, and the watermark
// it compares at came from the live C++ process BEFORE the kill — it is an
// input to both sides, not an answer from the survivor.
func Judge(a *Artifact) (Outcome, string) {
	// The model applies batches in submission order. A DeleteRange's bounds
	// come from the artifact's explicit flags rather than from emptiness,
	// because an empty key is a valid key.
	db := model.New()

	// stateAt records the model's full state after each applied sequence, so
	// "recovered a state at no watermark" can be answered rather than guessed.
	// It is the universal quantifier in A0.5's recovery contract made
	// checkable: for ANY applied w, not only the most recent.
	var history []snapshot
	record := func() {
		history = append(history, snapshot{seq: db.VisibleSeq(), state: stateOf(db)})
	}
	record() // the empty state, at sequence 0

	for _, op := range a.Submission {
		switch op.Kind {
		case OpSet:
			b := engine.NewBatch().Set(op.Key, op.Value)
			if _, err := db.Apply(b, false); err != nil {
				return RecoveredNeither, fmt.Sprintf("model refused a Set the engine accepted: %v", err)
			}
		case OpDelete:
			b := engine.NewBatch().Delete(op.Key)
			if _, err := db.Apply(b, false); err != nil {
				return RecoveredNeither, fmt.Sprintf("model refused a Delete the engine accepted: %v", err)
			}
		case OpDeleteRange:
			// nil means unbounded in engine.Batch, and the artifact's flags are
			// what distinguish that from an empty key.
			var start, end []byte
			if op.StartBounded {
				start = op.Key
			}
			if op.EndBounded {
				end = op.Value
			}
			b := engine.NewBatch().DeleteRange(start, end)
			if _, err := db.Apply(b, false); err != nil {
				return RecoveredNeither, fmt.Sprintf("model refused a DeleteRange the engine accepted: %v", err)
			}
		case OpSync:
			// THE MODEL'S DURABILITY IS DRIVEN, NOT AUTOMATIC. A Sync in the
			// C++ engine makes everything applied so far durable, so the model
			// is advanced to its own visible sequence — which is the same
			// promise, expressed in the model's vocabulary.
			db.AdvanceDurable(db.VisibleSeq())
		case OpSnapshotTake, OpSnapshotRelease:
			// Snapshots change what the C++ engine may DROP, never what it
			// holds at a watermark. The model has no compaction, so there is
			// nothing here for it to do — and saying so is better than a
			// silent default case.
		}
		record()
	}

	want, ok := stateAtSeq(history, engine.SeqNum(a.Watermark))
	if !ok {
		return RecoveredNeither, fmt.Sprintf(
			"the artifact's watermark %d is not a sequence this log ever applied", a.Watermark)
	}
	if diff := compare(want, a.Recovered); diff != "" {
		// WHICH DIRECTION, NAMED. A verdict that cannot say whether the engine
		// kept too little or too much is a verdict nobody can act on.
		if other, at := findMatchingSeq(history, a.Recovered); other {
			if at > engine.SeqNum(a.Watermark) {
				return RecoveredMore, fmt.Sprintf(
					"recovered the state at sequence %d, above the promised watermark %d: %s",
					at, a.Watermark, diff)
			}
			return RecoveredLess, fmt.Sprintf(
				"recovered the state at sequence %d, below the promised watermark %d: %s",
				at, a.Watermark, diff)
		}
		return RecoveredNeither, fmt.Sprintf(
			"recovered a state matching no applied sequence (watermark %d): %s", a.Watermark, diff)
	}
	return Agree, ""
}

// snapshot is the model's full state after one applied sequence. It is a named
// type rather than an anonymous struct because two helpers take it, and an
// anonymous struct repeated at three sites is a shape nobody can change once.
type snapshot struct {
	seq   engine.SeqNum
	state map[string][]byte
}

func stateOf(db *model.DB) map[string][]byte {
	out := map[string][]byte{}
	it := db.NewIter(engine.IterOptions{})
	for ok := it.First(); ok; ok = it.Next() {
		out[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	_ = it.Close()
	return out
}

// stateAtSeq returns the state at EXACTLY seq, and reports false if the log
// never applied it.
//
// AN EXACT MATCH RATHER THAN THE NEAREST BELOW, and the difference is a real
// verdict. The first version returned the state at the last sequence at or
// below seq, so a watermark the log never applied — 99 in a run that applied
// two batches — silently compared against the newest state and AGREED.
//
// A watermark is not an approximation. The engine reports one it applied, or it
// is claiming durability for a sequence that does not exist, and the judge must
// say so rather than compare against the nearest thing. Every applied sequence
// IS in this history, because the model retains every version between durable
// and visible — which is the property that makes an exact match answerable.
func stateAtSeq(history []snapshot, seq engine.SeqNum) (map[string][]byte, bool) {
	for _, h := range history {
		if h.seq == seq {
			return h.state, true
		}
	}
	return nil, false
}

// findMatchingSeq reports whether the recovered state equals the model's state
// at SOME applied sequence, and which. That is what separates "recovered the
// wrong amount" from "recovered something that never existed".
func findMatchingSeq(history []snapshot, got map[string][]byte) (bool, engine.SeqNum) {
	for _, h := range history {
		if compare(h.state, got) == "" {
			return true, h.seq
		}
	}
	return false, 0
}

// compare returns "" when the two states are equal, and otherwise the first
// difference in a stable order — so a divergence names a key rather than a size.
func compare(want, got map[string][]byte) string {
	keys := map[string]struct{}{}
	for k := range want {
		keys[k] = struct{}{}
	}
	for k := range got {
		keys[k] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		w, inWant := want[k]
		g, inGot := got[k]
		switch {
		case inWant && !inGot:
			return fmt.Sprintf("key %q: model has it, engine does not", k)
		case !inWant && inGot:
			return fmt.Sprintf("key %q: engine has it, model does not", k)
		case !bytes.Equal(w, g):
			return fmt.Sprintf("key %q: model %q, engine %q", k, w, g)
		}
	}
	return ""
}
