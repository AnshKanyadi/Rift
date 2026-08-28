package differential

import (
	"testing"
)

// THE JUDGE'S THREE DIRECTIONS, INDUCED FROM HAND-BUILT ARTIFACTS BEFORE ANY
// REAL RUN IS JUDGED.
//
// A judge whose verdicts have never been seen to fire is a judge that reports
// agreement for the reason that it reports nothing else. Each direction below
// is produced deliberately, so "the rig found no divergence" is a statement
// about the engine rather than about the judge.

func artifact(ops []Op, watermark uint64, recovered map[string][]byte) *Artifact {
	keys := make([]string, 0, len(recovered))
	for k := range recovered {
		keys = append(keys, k)
	}
	return &Artifact{
		Provenance:    Provenance{EngineCommit: "e", ModelCommit: "m", Regime: "flush"},
		Submission:    ops,
		Watermark:     watermark,
		Recovered:     recovered,
		RecoveredKeys: keys,
	}
}

func set(seq uint64, k, v string) Op {
	return Op{Kind: OpSet, Seq: seq, Key: []byte(k), Value: []byte(v)}
}

func sync() Op { return Op{Kind: OpSync} }

// AGREEMENT. Without this the other three could all be produced by a judge that
// never agrees with anything.
func TestAgreeWhenTheEngineRecoveredExactlyTheWatermark(t *testing.T) {
	a := artifact(
		[]Op{set(1, "a", "1"), sync(), set(2, "b", "2")},
		1,
		map[string][]byte{"a": []byte("1")},
	)
	got, why := Judge(a)
	if got != Agree {
		t.Fatalf("outcome = %v (%s), want agree", got, why)
	}
}

// RECOVERED LESS: acknowledged durable data lost. The obvious direction, and
// every crash rig catches it.
func TestRecoveredLessThanPromised(t *testing.T) {
	a := artifact(
		[]Op{set(1, "a", "1"), set(2, "b", "2"), sync()},
		2,
		map[string][]byte{"a": []byte("1")}, // "b" was durable and is gone
	)
	got, why := Judge(a)
	if got != RecoveredLess {
		t.Fatalf("outcome = %v (%s), want recovered-less", got, why)
	}
}

// RECOVERED MORE: THE DANGEROUS DIRECTION. Harmless in isolation — nobody lost
// anything — and it means the watermark is not the durability boundary, so a
// LATER crash at a DIFFERENT point loses data the caller was told was safe.
//
// This is the direction that requires engine/model to retain every version
// between durable and visible. A model that rounded its watermark up would be
// compared against an engine that rounded up and agree with it.
//
// NOTE THE RUN IS COMPLETE — every write carries a sequence. On a run cut short
// by a kill, recovering above the watermark is LEGITIMATE (a Sync in flight),
// and this test is what caught the widening being applied unconditionally.
func TestRecoveredMoreThanPromised(t *testing.T) {
	a := artifact(
		[]Op{set(1, "a", "1"), sync(), set(2, "b", "2")},
		1,
		map[string][]byte{"a": []byte("1"), "b": []byte("2")}, // kept "b", never promised
	)
	got, why := Judge(a)
	if got != RecoveredMore {
		t.Fatalf("outcome = %v (%s), want recovered-more", got, why)
	}
}

// RECOVERED NEITHER: a state at no applied sequence at all — torn, or
// interleaved. The case a two-element recovery set cannot express, and the one
// only a model retaining every intermediate sequence can name.
func TestRecoveredAStateAtNoSequence(t *testing.T) {
	a := artifact(
		[]Op{set(1, "a", "1"), sync(), set(2, "b", "2")},
		1,
		map[string][]byte{"a": []byte("WRONG")},
	)
	got, why := Judge(a)
	if got != RecoveredNeither {
		t.Fatalf("outcome = %v (%s), want recovered-neither", got, why)
	}
}

// AND ON A RUN CUT SHORT, RECOVERING ABOVE THE WATERMARK IS LEGITIMATE. This is
// B1's two-element recovery set, widened: a Sync in this engine can run a
// FLUSH, each with its own fsync, so a kill inside one can leave any prefix
// between the last completed watermark and the in-flight target durable.
//
// GF-14's other half for the rule above: without it, "recovered more is a
// violation" would be asserted by a judge that has never seen the legitimate
// case, and every killed schedule with an in-flight Sync would be reported as
// an engine defect.
func TestRecoveringAboveTheWatermarkIsLegitimateOnARunCutShort(t *testing.T) {
	a := artifact(
		[]Op{
			set(1, "a", "1"), sync(), set(2, "b", "2"),
			{Kind: OpSet, Seq: 0, Key: []byte("c"), Value: []byte("3")}, // never issued
		},
		1,
		map[string][]byte{"a": []byte("1"), "b": []byte("2")}, // the in-flight target
	)
	got, why := Judge(a)
	if got != Agree {
		t.Fatalf("outcome = %v (%s), want agree", got, why)
	}
}

// AND THE WIDENING IS BOUNDED: above what the engine ever assigned is still a
// violation, cut short or not.
func TestRecoveringAboveWhatWasEverAssignedIsAViolation(t *testing.T) {
	a := artifact(
		[]Op{
			set(1, "a", "1"), sync(),
			{Kind: OpSet, Seq: 0, Key: []byte("b"), Value: []byte("2")}, // never issued
		},
		1,
		map[string][]byte{"a": []byte("1"), "b": []byte("2")}, // state from an op never accepted
	)
	got, why := Judge(a)
	if got == Agree {
		t.Fatalf("outcome = agree, want a violation: the engine produced state "+
			"from an operation it never accepted (%s)", why)
	}
}

// A WATERMARK THE LOG NEVER APPLIED is not a divergence in the engine's STATE —
// it is an engine claiming durability for a sequence that does not exist, and
// the judge must say so rather than compare against the nearest thing.
func TestAWatermarkTheLogNeverAppliedIsRefused(t *testing.T) {
	a := artifact([]Op{set(1, "a", "1")}, 99, map[string][]byte{"a": []byte("1")})
	got, why := Judge(a)
	if got != RecoveredNeither {
		t.Fatalf("outcome = %v (%s), want recovered-neither", got, why)
	}
}

// DELETE_RANGE IS THE STRONGEST EVIDENCE THE RIG PRODUCES, because the two
// engines implement it by entirely different mechanisms. The judge must apply
// the model's native version with the artifact's explicit bounds.
func TestDeleteRangeIsReplayedWithItsBounds(t *testing.T) {
	a := artifact(
		[]Op{
			set(1, "a", "1"), set(2, "m", "2"), set(3, "z", "3"),
			{Kind: OpDeleteRange, Seq: 4, Key: []byte("b"), Value: []byte("n"),
				StartBounded: true, EndBounded: true},
			sync(),
		},
		4,
		map[string][]byte{"a": []byte("1"), "z": []byte("3")}, // "m" is in [b,n)
	)
	got, why := Judge(a)
	if got != Agree {
		t.Fatalf("outcome = %v (%s), want agree", got, why)
	}
}

// AN UNBOUNDED RANGE IS NOT AN EMPTY ONE. Section 8.2's clear-everything case,
// and the distinction the artifact's flags exist to carry: with nil bounds the
// model must clear everything, and with empty bounded ones it must clear
// nothing.
func TestAnUnboundedRangeClearsEverythingAndAnEmptyOneClearsNothing(t *testing.T) {
	clear := artifact(
		[]Op{set(1, "a", "1"), set(2, "b", "2"),
			{Kind: OpDeleteRange, Seq: 3, StartBounded: false, EndBounded: false},
			sync()},
		3,
		map[string][]byte{},
	)
	if got, why := Judge(clear); got != Agree {
		t.Fatalf("unbounded: outcome = %v (%s), want agree", got, why)
	}

	empty := artifact(
		[]Op{set(1, "a", "1"), set(2, "b", "2"),
			{Kind: OpDeleteRange, Seq: 3, Key: []byte{}, Value: []byte{},
				StartBounded: true, EndBounded: true},
			sync()},
		3,
		map[string][]byte{"a": []byte("1"), "b": []byte("2")},
	)
	if got, why := Judge(empty); got != Agree {
		t.Fatalf("empty bounded: outcome = %v (%s), want agree", got, why)
	}
}
