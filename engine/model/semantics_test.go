package model_test

import (
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
)

func load(t *testing.T, db *model.DB, pairs ...string) {
	t.Helper()
	b := engine.NewBatch()
	for i := 0; i < len(pairs); i += 2 {
		b.Set([]byte(pairs[i]), []byte(pairs[i+1]))
	}
	seq, err := db.Apply(b, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	db.AdvanceDurable(seq)
}

func scan(t *testing.T, r interface {
	NewIter(engine.IterOptions) engine.Iterator
}, o engine.IterOptions) []string {
	t.Helper()
	it := r.NewIter(o)
	defer it.Close()

	var out []string
	for ok := it.First(); ok; ok = it.Next() {
		out = append(out, string(it.Key()))
	}
	return out
}

func eq(t *testing.T, got, want []string, ctx string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", ctx, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", ctx, got, want)
		}
	}
}

// TestDeleteRangeIsHalfOpen pins the convention every range in this system
// uses. Getting it wrong by one key is the kind of bug that survives a long
// time, because most workloads never notice the boundary.
func TestDeleteRangeIsHalfOpen(t *testing.T) {
	db := model.New()
	defer db.Close()
	load(t, db, "a", "1", "b", "2", "c", "3", "d", "4")

	if _, err := db.Apply(engine.NewBatch().DeleteRange([]byte("b"), []byte("d")), true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eq(t, scan(t, db, engine.IterOptions{}), []string{"a", "d"}, "after DeleteRange[b,d)")
}

// TestDeleteRangeUnbounded is the clear half of snapshot application's
// clear-then-ingest, which is the reason DeleteRange is in the frozen interface
// at all (Amendment A3).
func TestDeleteRangeUnbounded(t *testing.T) {
	db := model.New()
	defer db.Close()
	load(t, db, "a", "1", "b", "2", "c", "3")

	b := engine.NewBatch().DeleteRange(nil, nil).Set([]byte("x"), []byte("9"))
	if _, err := db.Apply(b, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eq(t, scan(t, db, engine.IterOptions{}), []string{"x"}, "clear-then-ingest")
}

// TestBatchOrderingWithinOneApply: a batch is applied in order, so a
// DeleteRange removes keys written earlier in the same batch and not those
// written after it. Anything else would make batch construction
// order-dependent in a way callers cannot reason about.
func TestBatchOrderingWithinOneApply(t *testing.T) {
	db := model.New()
	defer db.Close()

	b := engine.NewBatch().
		Set([]byte("k1"), []byte("early")).
		DeleteRange([]byte("k0"), []byte("k9")).
		Set([]byte("k2"), []byte("late"))
	if _, err := db.Apply(b, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eq(t, scan(t, db, engine.IterOptions{}), []string{"k2"}, "batch ordering")

	// Last write wins within a batch.
	b2 := engine.NewBatch().Set([]byte("k2"), []byte("one")).Set([]byte("k2"), []byte("two"))
	if _, err := db.Apply(b2, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if v, _ := db.Get([]byte("k2")); string(v) != "two" {
		t.Errorf("last write did not win: %q", v)
	}
}

// TestIterBounds covers the half-open [Lower, Upper) window and the seek
// operations that have to respect it.
func TestIterBounds(t *testing.T) {
	db := model.New()
	defer db.Close()
	load(t, db, "a", "1", "b", "2", "c", "3", "d", "4", "e", "5")

	eq(t, scan(t, db, engine.IterOptions{Lower: []byte("b"), Upper: []byte("e")}),
		[]string{"b", "c", "d"}, "bounded scan")

	it := db.NewIter(engine.IterOptions{Lower: []byte("b"), Upper: []byte("e")})
	defer it.Close()

	// A seek before the lower bound lands on the bound, not before it.
	if !it.SeekGE([]byte("a")) || string(it.Key()) != "b" {
		t.Errorf("SeekGE below the bound = %q, want b", it.Key())
	}
	// A seek past the upper bound is invalid rather than clamped: there is no
	// key there, and pretending otherwise would silently return the wrong row.
	if it.SeekGE([]byte("z")) {
		t.Errorf("SeekGE past the bound was valid at %q", it.Key())
	}
	if !it.Last() || string(it.Key()) != "d" {
		t.Errorf("Last = %q, want d", it.Key())
	}
	if !it.SeekLT([]byte("d")) || string(it.Key()) != "c" {
		t.Errorf("SeekLT(d) = %q, want c", it.Key())
	}
	// Stepping off the end leaves it invalid and it stays invalid.
	for it.Next() {
	}
	if it.Next() || it.Valid() {
		t.Error("an exhausted iterator became valid again")
	}
}

// TestSnapshotIsPinned is the isolation guarantee: a snapshot sees the state as
// of the moment it was taken, no matter what is applied afterwards. Copy-on-
// write makes it free here; the C++ engine pays for it by pinning a version
// against compaction, which is why Close is not optional.
func TestSnapshotIsPinned(t *testing.T) {
	db := model.New()
	defer db.Close()
	load(t, db, "a", "1", "b", "2")

	snap := db.NewSnapshot()

	if _, err := db.Apply(engine.NewBatch().Set([]byte("c"), []byte("3")).Delete([]byte("a")), true); err != nil {
		t.Fatalf("apply: %v", err)
	}

	eq(t, scan(t, snap, engine.IterOptions{}), []string{"a", "b"}, "snapshot view")
	eq(t, scan(t, db, engine.IterOptions{}), []string{"b", "c"}, "live view")

	if v, err := snap.Get([]byte("a")); err != nil || string(v) != "1" {
		t.Errorf("snapshot lost a key deleted after it was taken: %q %v", v, err)
	}
	if err := snap.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if err := snap.Close(); err == nil {
		t.Error("double close was accepted")
	}
}

// TestIteratorSurvivesWrites: an iterator holds its version, so a write during
// a scan cannot make it skip or repeat a key. In the C++ engine this is the
// same guarantee for a different reason, and code written against either has to
// behave the same at I1.
func TestIteratorSurvivesWrites(t *testing.T) {
	db := model.New()
	defer db.Close()
	load(t, db, "a", "1", "b", "2", "c", "3")

	it := db.NewIter(engine.IterOptions{})
	defer it.Close()

	var got []string
	for ok := it.First(); ok; ok = it.Next() {
		got = append(got, string(it.Key()))
		if _, err := db.Apply(engine.NewBatch().Set([]byte("zz"), []byte("new")), false); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	eq(t, got, []string{"a", "b", "c"}, "scan across concurrent writes")
}

// TestCloseReportsLeaks: an open iterator or snapshot at Close is a caller bug.
// It is invisible here and a slow disk leak in production, so the model refuses
// to be the more forgiving of the two engines.
func TestCloseReportsLeaks(t *testing.T) {
	db := model.New()
	load(t, db, "a", "1")

	snap := db.NewSnapshot()
	_ = snap

	if err := db.Close(); err == nil {
		t.Error("Close accepted an open snapshot")
	}
}

// TestBatchCopiesItsArguments: an engine whose behaviour depends on whether a
// caller reused a buffer is an engine that reproduces on one machine and not
// another.
func TestBatchCopiesItsArguments(t *testing.T) {
	db := model.New()
	defer db.Close()

	key := []byte("k")
	val := []byte("v1")
	b := engine.NewBatch().Set(key, val)

	key[0] = 'X'
	val[1] = '9'

	if _, err := db.Apply(b, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if v, err := db.Get([]byte("k")); err != nil || string(v) != "v1" {
		t.Errorf("batch retained caller buffers: Get(k) = %q %v", v, err)
	}
}

// TestApproximateDiskBytes is used for split decisions, so it has to respond to
// the range it is asked about rather than to the whole keyspace.
func TestApproximateDiskBytes(t *testing.T) {
	db := model.New()
	defer db.Close()
	load(t, db, "a", "11", "b", "22", "c", "33")

	all, err := db.ApproximateDiskBytes(nil, nil)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	part, err := db.ApproximateDiskBytes([]byte("a"), []byte("c"))
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if all != 9 {
		t.Errorf("whole keyspace = %d bytes, want 9", all)
	}
	if part != 6 {
		t.Errorf("[a,c) = %d bytes, want 6", part)
	}
}
