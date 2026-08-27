//go:build rift_cgo

package riftcgo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
)

// PARITY: the same operations against both engines, compared after each one.
//
// This is NOT the differential — that compares a RECOVERED state against the
// model at a watermark, through an artifact, across two processes. This is the
// cheaper question one layer down: does the wrapper present the same SEMANTICS
// as the model when nothing goes wrong?
//
//	A BOUNDARY THAT LOSES A SEMANTIC LOSES IT ON EVERY RUN, so it is worth
//	asking before any crash schedule is involved. The differential would find
//	it too, and would report it as a recovery divergence — which sends the
//	reader to the wrong component.
func openBoth(t *testing.T, name string) (*DB, *model.DB, func()) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	db, err := Open(dir, 0, 0, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db, model.New(), func() {
		_ = db.Close()
		_ = os.RemoveAll(dir)
	}
}

func stateOfEngine(t *testing.T, e engine.Engine) map[string]string {
	t.Helper()
	out := map[string]string{}
	it := e.NewIter(engine.IterOptions{})
	for ok := it.First(); ok; ok = it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	_ = it.Close()
	return out
}

func compareStates(t *testing.T, step int, cgo, mod map[string]string) {
	t.Helper()
	if len(cgo) != len(mod) {
		t.Fatalf("step %d: cgo has %d keys, model has %d", step, len(cgo), len(mod))
	}
	for k, v := range mod {
		got, ok := cgo[k]
		if !ok {
			t.Fatalf("step %d: model has %q, cgo does not", step, k)
		}
		if got != v {
			t.Fatalf("step %d: key %q: model %q, cgo %q", step, k, v, got)
		}
	}
}

func applyBoth(t *testing.T, step int, a *DB, b *model.DB, build func() *engine.Batch) {
	t.Helper()
	sa, err := a.Apply(build(), false)
	if err != nil {
		t.Fatalf("step %d: cgo apply: %v", step, err)
	}
	sb, err := b.Apply(build(), false)
	if err != nil {
		t.Fatalf("step %d: model apply: %v", step, err)
	}
	// THE SEQUENCES MUST MATCH TOO, not only the states. A boundary that
	// numbered batches differently would produce identical states and a
	// watermark the differential could not compare against.
	if sa != sb {
		t.Fatalf("step %d: cgo seq %d, model seq %d", step, sa, sb)
	}
	compareStates(t, step, stateOfEngine(t, a), stateOfEngine(t, b))
}

func TestParityAcrossTheOperationKinds(t *testing.T) {
	a, b, done := openBoth(t, "kinds")
	defer done()

	steps := []func() *engine.Batch{
		func() *engine.Batch { return engine.NewBatch().Set([]byte("a"), []byte("1")) },
		func() *engine.Batch { return engine.NewBatch().Set([]byte("b"), []byte("2")) },
		func() *engine.Batch { return engine.NewBatch().Set([]byte("c"), []byte("3")) },
		func() *engine.Batch { return engine.NewBatch().Delete([]byte("b")) },
		// An overwrite, then a range that covers part of the space.
		func() *engine.Batch { return engine.NewBatch().Set([]byte("a"), []byte("1b")) },
		func() *engine.Batch { return engine.NewBatch().DeleteRange([]byte("a"), []byte("c")) },
		// AN EMPTY KEY IS A VALID KEY, and it must survive the boundary as one.
		func() *engine.Batch { return engine.NewBatch().Set([]byte(""), []byte("empty-key")) },
		// A MULTI-OP BATCH, including the intra-batch rule: a DeleteRange
		// covers keys written EARLIER in the same batch, and a Set after it
		// re-adds the key.
		func() *engine.Batch {
			return engine.NewBatch().
				Set([]byte("x"), []byte("before")).
				DeleteRange([]byte("w"), []byte("z")).
				Set([]byte("y"), []byte("after"))
		},
		// UNBOUNDED ON ONE SIDE.
		func() *engine.Batch { return engine.NewBatch().Set([]byte("z9"), []byte("keep")) },
		func() *engine.Batch { return engine.NewBatch().DeleteRange(nil, []byte("z")) },
	}
	for i, s := range steps {
		applyBoth(t, i, a, b, s)
	}
}

// AN EMPTY BOUNDED RANGE DELETES NOTHING AND AN UNBOUNDED ONE CLEARS
// EVERYTHING, and the distinction has to survive Go → C → C++ intact. It is
// carried by the POINTER at the boundary, since an empty key is a valid key.
func TestParityOnBoundedVersusUnboundedRanges(t *testing.T) {
	a, b, done := openBoth(t, "bounds")
	defer done()

	seed := func() *engine.Batch {
		return engine.NewBatch().
			Set([]byte("k1"), []byte("1")).
			Set([]byte("k2"), []byte("2"))
	}
	applyBoth(t, 0, a, b, seed)

	// [ "", "" ) — bounded and empty. Deletes nothing.
	applyBoth(t, 1, a, b, func() *engine.Batch {
		return engine.NewBatch().DeleteRange([]byte{}, []byte{})
	})
	if len(stateOfEngine(t, a)) != 2 {
		t.Fatal("an empty bounded range deleted something across the boundary")
	}

	// [ nil, nil ) — unbounded. Clears everything.
	applyBoth(t, 2, a, b, func() *engine.Batch {
		return engine.NewBatch().DeleteRange(nil, nil)
	})
	if n := len(stateOfEngine(t, a)); n != 0 {
		t.Fatalf("clear-everything left %d keys", n)
	}
}

// THE CURSOR OVER BLOCKS BEHAVES LIKE A CURSOR, at every block size. A block
// interface whose CONTENTS depend on the block size cannot be measured, because
// every measurement would be of a different thing.
func TestParityOfIterationAtEveryBlockSize(t *testing.T) {
	a, b, done := openBoth(t, "blocks")
	defer done()

	build := func() *engine.Batch {
		bat := engine.NewBatch()
		for i := 0; i < 40; i++ {
			bat.Set([]byte(fmt.Sprintf("k%03d", i)), []byte(fmt.Sprintf("v%d", i)))
		}
		return bat
	}
	applyBoth(t, 0, a, b, build)

	want := stateOfEngine(t, b)
	original := DefaultBlock
	defer func() { DefaultBlock = original }()
	for _, n := range []int{1, 2, 7, 40, 500} {
		DefaultBlock = n
		compareStates(t, n, stateOfEngine(t, a), want)
	}
}

// FORWARD THEN BACKWARD OVER ONE CURSOR, which is where a block interface is
// most likely to be wrong: the block held was fetched walking one way and says
// nothing about the other.
func TestParityOfDirectionSwitches(t *testing.T) {
	a, b, done := openBoth(t, "directions")
	defer done()
	applyBoth(t, 0, a, b, func() *engine.Batch {
		bat := engine.NewBatch()
		for i := 0; i < 20; i++ {
			bat.Set([]byte(fmt.Sprintf("k%02d", i)), []byte("v"))
		}
		return bat
	})

	original := DefaultBlock
	defer func() { DefaultBlock = original }()
	DefaultBlock = 3 // small enough that a switch lands mid-block

	walk := func(e engine.Engine) []string {
		var seen []string
		it := e.NewIter(engine.IterOptions{})
		ok := it.First()
		for i := 0; ok && i < 5; i++ {
			seen = append(seen, string(it.Key()))
			ok = it.Next()
		}
		// Now back up three.
		for i := 0; i < 3 && ok; i++ {
			ok = it.Prev()
			if ok {
				seen = append(seen, "<"+string(it.Key()))
			}
		}
		// And forward again.
		for i := 0; i < 3 && ok; i++ {
			ok = it.Next()
			if ok {
				seen = append(seen, ">"+string(it.Key()))
			}
		}
		_ = it.Close()
		return seen
	}
	got, want := walk(a), walk(b)
	if len(got) != len(want) {
		t.Fatalf("cgo walked %d steps, model walked %d:\n cgo=%v\n mod=%v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: cgo %q, model %q\n cgo=%v\n mod=%v",
				i, got[i], want[i], got, want)
		}
	}
}

// BOUNDED ITERATION, which is the option the boundary carries as a nil-or-not
// pointer rather than as a flag.
func TestParityOfBoundedIteration(t *testing.T) {
	a, b, done := openBoth(t, "iterbounds")
	defer done()
	applyBoth(t, 0, a, b, func() *engine.Batch {
		bat := engine.NewBatch()
		for i := 0; i < 20; i++ {
			bat.Set([]byte(fmt.Sprintf("k%02d", i)), []byte("v"))
		}
		return bat
	})

	collect := func(e engine.Engine, o engine.IterOptions) []string {
		var out []string
		it := e.NewIter(o)
		for ok := it.First(); ok; ok = it.Next() {
			out = append(out, string(it.Key()))
		}
		_ = it.Close()
		return out
	}
	cases := []engine.IterOptions{
		{},
		{Lower: []byte("k05")},
		{Upper: []byte("k05")},
		{Lower: []byte("k05"), Upper: []byte("k10")},
		{Lower: []byte("k99")},
	}
	for i, o := range cases {
		got, want := collect(a, o), collect(b, o)
		if !equalStrings(got, want) {
			t.Fatalf("case %d (%v): cgo=%v model=%v", i, o, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DURABILITY: OnDurable fires from THIS side, and the watermark it reports is
// the one the engine promised.
func TestOnDurableFiresFromTheGoSideAndIsMonotone(t *testing.T) {
	a, _, done := openBoth(t, "durable")
	defer done()

	var seen []engine.SeqNum
	a.OnDurable(func(s engine.SeqNum) { seen = append(seen, s) })

	for i := 0; i < 3; i++ {
		if _, err := a.Apply(engine.NewBatch().Set([]byte("k"), []byte("v")), false); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("OnDurable never fired")
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("watermarks not increasing: %v", seen)
		}
	}
	if got := a.DurableSeq(); got != seen[len(seen)-1] {
		t.Fatalf("DurableSeq %d, last callback %d", got, seen[len(seen)-1])
	}
}

// A LARGE VALUE CROSSES INTACT, which is the buffer-growth path: the first Get
// under-sizes, is told the length, and asks again.
func TestALargeValueCrossesIntact(t *testing.T) {
	a, b, done := openBoth(t, "large")
	defer done()
	big := bytes.Repeat([]byte("x"), 100_000)
	applyBoth(t, 0, a, b, func() *engine.Batch {
		return engine.NewBatch().Set([]byte("big"), big)
	})
	got, err := a.Get([]byte("big"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("value crossed as %d bytes, want %d", len(got), len(big))
	}
}
