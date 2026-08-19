package kv_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/kv"
)

func ts(wall int64, logical uint32) hlc.Timestamp {
	return hlc.Timestamp{Wall: clock.NewWall(wall), Logical: logical}
}

func newStore(t *testing.T) (*kv.Store, *model.DB) {
	t.Helper()
	db := model.New()
	s, err := kv.NewStore(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s, db
}

func put(t *testing.T, s *kv.Store, db *model.DB, key string, at hlc.Timestamp, val string) {
	t.Helper()
	b := engine.NewBatch()
	if err := s.PutInto(b, []byte(key), at, []byte(val)); err != nil {
		t.Fatalf("put %s@%s: %v", key, at, err)
	}
	if _, err := db.Apply(b, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

func read(t *testing.T, s *kv.Store, key string, at hlc.Timestamp) (string, bool) {
	t.Helper()
	v, ok, err := s.ReadAt([]byte(key), at)
	if err != nil {
		t.Fatalf("read %s@%s: %v", key, at, err)
	}
	return string(v), ok
}

// TestReadAtReturnsTheVersionVisibleThere is the core MVCC semantic: a read at
// T sees the newest version at or before T, and nothing newer.
func TestReadAtReturnsTheVersionVisibleThere(t *testing.T) {
	s, db := newStore(t)
	put(t, s, db, "k", ts(100, 0), "v100")
	put(t, s, db, "k", ts(200, 0), "v200")
	put(t, s, db, "k", ts(200, 5), "v200.5")
	put(t, s, db, "k", ts(300, 0), "v300")

	for _, c := range []struct {
		at   hlc.Timestamp
		want string
		ok   bool
	}{
		{ts(99, 0), "", false},       // before every version
		{ts(100, 0), "v100", true},   // exactly at a version
		{ts(150, 0), "v100", true},   // between
		{ts(200, 0), "v200", true},   // logical tie: the one at .0
		{ts(200, 4), "v200", true},   // between logical versions
		{ts(200, 5), "v200.5", true}, // exactly at the logical version
		{ts(299, 9), "v200.5", true}, // still before v300
		{ts(300, 0), "v300", true},   // the newest
		{ts(9999, 0), "v300", true},  // far after
	} {
		got, ok := read(t, s, "k", c.at)
		if ok != c.ok || got != c.want {
			t.Errorf("read at %s = (%q, %v), want (%q, %v)", c.at, got, ok, c.want, c.ok)
		}
	}
}

// TestOneKeysVersionsNeverEnterAnothersChain is the covering test for
// M51-mvcc-key-encoding-without-a-length-prefix.
//
// A user key may contain any byte. With a separator-delimited encoding, a key
// containing the separator sorts its versions into a neighbouring key's chain --
// and the failure is silent, because every record still decodes and every read
// still returns a plausible value.
func TestOneKeysVersionsNeverEnterAnothersChain(t *testing.T) {
	s, db := newStore(t)

	// Keys chosen to collide under any fixed separator: one is a prefix of
	// another, and both contain the bytes an encoding might reserve.
	keys := []string{"a", "a/", "a/b", "a\x00", "a\x00b", "a\xff", "", "\x00"}
	for i, k := range keys {
		put(t, s, db, k, ts(int64(100+i), 0), fmt.Sprintf("v-%d", i))
	}
	for i, k := range keys {
		got, ok := read(t, s, k, ts(1000, 0))
		want := fmt.Sprintf("v-%d", i)
		if !ok || got != want {
			t.Errorf("key %q read %q (ok=%v), want %q; a version landed in another key's chain",
				k, got, ok, want)
		}
	}

	// And a read before a key's own version must not find a NEIGHBOUR's older
	// one, which is the direction the collision usually shows up in.
	for i, k := range keys {
		if _, ok := read(t, s, k, ts(int64(100+i)-1, 0)); ok {
			t.Errorf("key %q returned a value before its own only version existed", k)
		}
	}
}

// TestAReadBelowTheGCMarkIsRefused is exit criterion 2, and the covering test
// for M52-gc-answers-below-the-mark.
//
// After collection the versions that were visible below the mark are gone. An
// implementation that answers anyway returns an older state that never existed
// or a newer one, and either answer is a plausible value no checker downstream
// can question.
func TestAReadBelowTheGCMarkIsRefused(t *testing.T) {
	s, db := newStore(t)
	put(t, s, db, "k", ts(100, 0), "v100")
	put(t, s, db, "k", ts(200, 0), "v200")
	put(t, s, db, "k", ts(300, 0), "v300")

	// Before collection, history is intact.
	if got, ok := read(t, s, "k", ts(150, 0)); !ok || got != "v100" {
		t.Fatalf("pre-GC read at 150 = (%q,%v), want v100", got, ok)
	}

	b := engine.NewBatch()
	removed, err := s.AdvanceGCInto(b, ts(250, 0))
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := db.Apply(b, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if removed != 1 {
		t.Errorf("collected %d versions, want 1 (v100, with v200 kept as the newest below the mark)", removed)
	}

	// The read that used to be answerable must now be REFUSED, not answered.
	_, _, err = s.ReadAt([]byte("k"), ts(150, 0))
	if !errors.Is(err, kv.ErrBelowGCMark) {
		v, ok, _ := s.ReadAt([]byte("k"), ts(150, 0))
		t.Fatalf("a read below the mark returned (%q, %v, err=%v) instead of refusing. That answer "+
			"is a state the database never had, and it is indistinguishable from a correct one",
			v, ok, err)
	}
	if s.ReadsRefused() == 0 {
		t.Error("the refusal was not counted")
	}

	// At the mark is also refused: the mark is the first UNANSWERABLE point.
	if _, _, err := s.ReadAt([]byte("k"), ts(250, 0)); !errors.Is(err, kv.ErrBelowGCMark) {
		t.Errorf("a read exactly at the mark was answered: %v", err)
	}

	// Just above the mark is answerable, and it must find the version GC KEPT.
	// This is the off-by-one that makes collection silently lossy: collecting
	// the newest version at or below the mark leaves this read with nothing.
	if got, ok := read(t, s, "k", ts(250, 0).Next()); !ok || got != "v200" {
		t.Errorf("read just above the mark = (%q,%v), want v200. GC collected the version the "+
			"first answerable timestamp needs", got, ok)
	}
	if got, ok := read(t, s, "k", ts(400, 0)); !ok || got != "v300" {
		t.Errorf("read above every version = (%q,%v), want v300", got, ok)
	}
}

// TestGCIsIdempotentAndNeverGoesBackwards: a replica re-applies a GC command
// after recovery, because appliedIdx is deliberately not persisted (A1).
func TestGCIsIdempotentAndNeverGoesBackwards(t *testing.T) {
	s, db := newStore(t)
	put(t, s, db, "k", ts(100, 0), "v100")
	put(t, s, db, "k", ts(200, 0), "v200")

	apply := func(to hlc.Timestamp) int {
		b := engine.NewBatch()
		n, err := s.AdvanceGCInto(b, to)
		if err != nil {
			t.Fatalf("gc to %s: %v", to, err)
		}
		if _, err := db.Apply(b, true); err != nil {
			t.Fatalf("apply: %v", err)
		}
		return n
	}

	first := apply(ts(150, 0))
	again := apply(ts(150, 0))
	if again != 0 {
		t.Errorf("re-applying the same GC collected %d more versions; it is not idempotent", again)
	}
	if first < 0 {
		t.Fatal("negative")
	}
	if got := s.GCMark(); got != ts(150, 0) {
		t.Errorf("mark is %s, want 150.0", got)
	}

	// An older mark is a no-op, not a regression. A mark that went backwards
	// would make a refused read start answering from a history that is already
	// partly collected.
	if n := apply(ts(50, 0)); n != 0 {
		t.Errorf("a backwards GC collected %d versions", n)
	}
	if got := s.GCMark(); got != ts(150, 0) {
		t.Errorf("mark went backwards to %s", got)
	}
}

// TestWritingBelowTheMarkIsRefused: a version below the mark is one no read may
// ever see, and whether it survives depends on when GC next runs -- which is not
// a property a state machine may have.
func TestWritingBelowTheMarkIsRefused(t *testing.T) {
	s, db := newStore(t)
	put(t, s, db, "k", ts(100, 0), "v100")

	b := engine.NewBatch()
	if _, err := s.AdvanceGCInto(b, ts(200, 0)); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := db.Apply(b, true); err != nil {
		t.Fatalf("apply: %v", err)
	}

	b2 := engine.NewBatch()
	if err := s.PutInto(b2, []byte("k"), ts(150, 0), []byte("late")); !errors.Is(err, kv.ErrBelowGCMark) {
		t.Fatalf("a write below the mark was accepted (err=%v)", err)
	}
}

// TestUnsetTimestampsAreRefused: the zero Timestamp means unset, and a store
// that accepted it writes versions nothing can name.
func TestUnsetTimestampsAreRefused(t *testing.T) {
	s, _ := newStore(t)
	b := engine.NewBatch()
	if err := s.PutInto(b, []byte("k"), hlc.Timestamp{}, []byte("v")); !errors.Is(err, kv.ErrUnsetTimestamp) {
		t.Errorf("put at the zero timestamp: %v", err)
	}
	if _, _, err := s.ReadAt([]byte("k"), hlc.Timestamp{}); !errors.Is(err, kv.ErrUnsetTimestamp) {
		t.Errorf("read at the zero timestamp: %v", err)
	}
	if _, err := s.AdvanceGCInto(b, hlc.Timestamp{}); !errors.Is(err, kv.ErrUnsetTimestamp) {
		t.Errorf("gc to the zero timestamp: %v", err)
	}
}

// TestRandomizedHistoryAgreesWithAnIndependentModel is the property test: a
// randomized write history, then every read compared against a model built from
// the writes the test itself issued.
//
// The model is a plain sorted list. It shares nothing with the store but the
// question, which is the same reason the snapshot oracle takes a function.
func TestRandomizedHistoryAgreesWithAnIndependentModel(t *testing.T) {
	key, err := rng.ParseKey("1112131415161718191a1b1c1d1e1f20")
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	for seed := uint64(0); seed < 200; seed++ {
		s, db := newStore(t)
		type ver struct {
			at  hlc.Timestamp
			val string
		}
		model := map[string][]ver{}

		for i := uint64(0); i < 60; i++ {
			k := fmt.Sprintf("k%02d", key.Uint64N(1, seed, i, 0, 8))
			wall := int64(key.Uint64N(2, seed, i, 1, 500)) + 1
			log := uint32(key.Uint64N(3, seed, i, 2, 3))
			at := ts(wall, log)
			val := fmt.Sprintf("v%d-%d", seed, i)

			// Skip a duplicate timestamp for one key: two versions at one
			// timestamp is a caller bug the store does not have to define.
			dup := false
			for _, v := range model[k] {
				if v.at == at {
					dup = true
				}
			}
			if dup {
				continue
			}
			put(t, s, db, k, at, val)
			model[k] = append(model[k], ver{at, val})
		}

		for ki := 0; ki < 8; ki++ {
			k := fmt.Sprintf("k%02d", ki)
			for q := uint64(0); q < 40; q++ {
				at := ts(int64(key.Uint64N(4, seed, uint64(ki), q, 520))+1, uint32(key.Uint64N(5, seed, uint64(ki), q, 3)))

				var want string
				var wantOK bool
				var best hlc.Timestamp
				for _, v := range model[k] {
					if v.at.LessEq(at) && (!wantOK || best.Less(v.at)) {
						want, wantOK, best = v.val, true, v.at
					}
				}
				got, ok := read(t, s, k, at)
				if ok != wantOK || got != want {
					t.Fatalf("seed %d: read %s at %s = (%q,%v), model says (%q,%v)",
						seed, k, at, got, ok, want, wantOK)
				}
			}
		}
	}
}
