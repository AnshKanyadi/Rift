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

	// CONSECUTIVE WRITES SHARING A NON-ZERO SEQUENCE ARE ONE BATCH.
	//
	// That is what a batch IS in this engine — every op in one applies at one
	// sequence — so the artifact expresses batching without a field, and the
	// judge must group the same way or it applies as several batches what the
	// engine applied as one.
	//
	// THE DIFFERENCE IS NOT COSMETIC. Within a batch, a DeleteRange covers keys
	// written EARLIER in the same batch and a Set after it re-adds the key —
	// rules that only exist because the ops share a sequence. Replayed one per
	// batch, every one of them would be exercised wrongly and the model would
	// disagree with the engine for a reason that is the judge's fault.
	for i := 0; i < len(a.Submission); i++ {
		op := a.Submission[i]
		if isWrite(op.Kind) {
			b := engine.NewBatch()
			j := i
			for ; j < len(a.Submission); j++ {
				w := a.Submission[j]
				if !isWrite(w.Kind) {
					break
				}
				// Sequence 0 means the op was never issued — the run was cut
				// short — and such ops are grouped by position rather than by
				// sequence, because they have none.
				if j > i && w.Seq != op.Seq {
					break
				}
				switch w.Kind {
				case OpSet:
					b.Set(w.Key, w.Value)
				case OpDelete:
					b.Delete(w.Key)
				case OpDeleteRange:
					// nil means unbounded in engine.Batch, and the artifact's
					// flags are what distinguish that from an empty key.
					var start, end []byte
					if w.StartBounded {
						start = w.Key
					}
					if w.EndBounded {
						end = w.Value
					}
					b.DeleteRange(start, end)
				}
			}
			if _, err := db.Apply(b, false); err != nil {
				return RecoveredNeither, fmt.Sprintf(
					"model refused a batch the engine accepted: %v", err)
			}
			record()
			i = j - 1
			continue
		}
		switch op.Kind {
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

	if _, ok := stateAtSeq(history, engine.SeqNum(a.Watermark)); !ok {
		return RecoveredNeither, fmt.Sprintf(
			"the artifact's watermark %d is not a sequence this log ever applied", a.Watermark)
	}

	// THE RECOVERY SET IS A RANGE, NOT A VALUE, AND THE FIRST VERSION OF THIS
	// JUDGE COMPARED AGAINST A VALUE.
	//
	// B1's exactness oracle already knew this and stated it as a two-element
	// set: R ∈ {G_{k-1}, G_k} when a Sync was in flight at the kill. "A Sync
	// can complete on the device with the kill preempting its return: the bytes
	// are durable, the caller never learned it."
	//
	// The differential inherits that and it is WIDER here, because a Sync in
	// this engine can run a FLUSH — writing a table and a manifest edit, each
	// with its own fsync — so a kill inside one Sync can leave ANY prefix
	// between the last completed watermark and the in-flight target durable.
	//
	//	acceptable = the state at some applied sequence in [w, inFlight]
	//
	// where inFlight is the highest sequence the engine assigned before it
	// died. Ops after the kill carry sequence 0, so that maximum IS the
	// in-flight Sync's target and needs no new field in the frozen format.
	//
	// WHAT THIS DOES NOT PERMIT, so the widening is bounded rather than
	// generous: recovering BELOW w is still a violation — the promise was
	// broken; recovering ABOVE inFlight is still a violation — the engine
	// produced state from operations it never accepted; and recovering a state
	// at NO applied sequence is still a violation, torn or interleaved.
	// AND THE WIDENING APPLIES ONLY TO A RUN THAT WAS CUT SHORT, which the
	// first version of this rule missed and a hand-built test caught.
	//
	// On a CLEAN run the engine closed normally, and `Close` does not sync —
	// deliberately, so that close-then-reopen is indistinguishable from
	// kill-then-reopen. So unsynced writes MUST be lost, there is no Sync in
	// flight, and R must equal exactly G_w. Allowing the range there would
	// forgive precisely the defect the strict comparison exists to catch:
	// unsynced data surviving a clean shutdown.
	//
	// Whether the run was cut short is a fact about the LOG, not about the
	// engine's opinion: the driver stops issuing at the kill, so every op after
	// it carries sequence 0. A run in which every write carries a sequence ran
	// to completion.
	completed := true
	inFlight := engine.SeqNum(0)
	for _, op := range a.Submission {
		if engine.SeqNum(op.Seq) > inFlight {
			inFlight = engine.SeqNum(op.Seq)
		}
		switch op.Kind {
		case OpSet, OpDelete, OpDeleteRange:
			if op.Seq == 0 {
				completed = false
			}
		}
	}
	if completed {
		inFlight = engine.SeqNum(a.Watermark)
	} else if inFlight < engine.SeqNum(a.Watermark) {
		inFlight = engine.SeqNum(a.Watermark)
	}

	matches := matchingSeqs(history, a.Recovered)
	if len(matches) == 0 {
		want, _ := stateAtSeq(history, engine.SeqNum(a.Watermark))
		return RecoveredNeither, fmt.Sprintf(
			"recovered a state matching no applied sequence (watermark %d, in flight %d): %s",
			a.Watermark, inFlight, compare(want, a.Recovered))
	}
	for _, at := range matches {
		if at >= engine.SeqNum(a.Watermark) && at <= inFlight {
			return Agree, ""
		}
	}
	// WHICH DIRECTION, NAMED, AND FROM THE CLOSEST MATCH RATHER THAN THE FIRST.
	// The first version reported the FIRST matching sequence, so an empty
	// recovered state always matched sequence 0 and every such divergence was
	// reported as "recovered less" — including one that had recovered MORE and
	// happened to be empty because a clear-everything ran above the watermark.
	// A verdict that names the wrong direction sends the reader to the wrong
	// component, which is HARNESS-006's cost.
	best := matches[0]
	for _, at := range matches {
		if absDiff(at, engine.SeqNum(a.Watermark)) < absDiff(best, engine.SeqNum(a.Watermark)) {
			best = at
		}
	}
	want, _ := stateAtSeq(history, engine.SeqNum(a.Watermark))
	if best > inFlight {
		return RecoveredMore, fmt.Sprintf(
			"recovered the state at sequence %d, above what the engine could have made durable (%d): %s",
			best, inFlight, compare(want, a.Recovered))
	}
	return RecoveredLess, fmt.Sprintf(
		"recovered the state at sequence %d, below the promised watermark %d: %s",
		best, a.Watermark, compare(want, a.Recovered))
}

func absDiff(a, b engine.SeqNum) engine.SeqNum {
	if a > b {
		return a - b
	}
	return b - a
}

// snapshot is the model's full state after one applied sequence. It is a named
// type rather than an anonymous struct because two helpers take it, and an
// anonymous struct repeated at three sites is a shape nobody can change once.
type snapshot struct {
	seq   engine.SeqNum
	state map[string][]byte
}

func isWrite(k OpKind) bool {
	return k == OpSet || k == OpDelete || k == OpDeleteRange
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

// matchingSeqs returns EVERY applied sequence whose state equals the recovered
// one. All of them, not the first: an empty recovered state matches sequence 0
// and may also match a much later one, and reporting only the first names the
// wrong direction. That separates "recovered the wrong amount" from "recovered
// something that never existed" without guessing which amount.
func matchingSeqs(history []snapshot, got map[string][]byte) []engine.SeqNum {
	var out []engine.SeqNum
	for _, h := range history {
		if compare(h.state, got) == "" {
			out = append(out, h.seq)
		}
	}
	return out
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
