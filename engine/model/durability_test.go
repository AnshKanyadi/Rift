package model_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/internal/sorted"
)

// oracle recomputes what the engine must contain, from the harness's own record
// of what was applied.
//
// This is the point of the whole file. DESIGN-A0 D12 requires the durability
// checker to compute expected post-crash state from the operation log rather
// than by asking the engine what it thinks it had -- an engine that lies about
// durability must be caught, and an oracle that interrogates it believes the
// lie. So this replays batches into a plain map with no shared code path.
type oracle struct {
	batches []*engine.Batch // in apply order, index+1 == SeqNum
}

func (o *oracle) record(b *engine.Batch) engine.SeqNum {
	o.batches = append(o.batches, b)
	return engine.SeqNum(len(o.batches))
}

// stateAt replays every batch up to and including seq.
func (o *oracle) stateAt(seq engine.SeqNum) map[string]string {
	state := make(map[string]string)
	for i, b := range o.batches {
		if engine.SeqNum(i+1) > seq {
			break
		}
		if b == nil {
			continue
		}
		for _, op := range b.Ops() {
			switch op.Kind {
			case engine.OpSet:
				state[string(op.Key)] = string(op.Value)
			case engine.OpDelete:
				delete(state, string(op.Key))
			case engine.OpDeleteRange:
				for _, k := range sorted.Keys(state) {
					if engine.InRange([]byte(k), op.Key, op.End) {
						delete(state, k)
					}
				}
			}
		}
	}
	return state
}

// assertMatches compares the engine's full iteration against the oracle.
func assertMatches(t *testing.T, db *model.DB, want map[string]string, ctx string) {
	t.Helper()

	it := db.NewIter(engine.IterOptions{})
	defer it.Close()

	var got []string
	for ok := it.First(); ok; ok = it.Next() {
		got = append(got, string(it.Key())+"="+string(it.Value()))
	}
	if err := it.Error(); err != nil {
		t.Fatalf("%s: iterator error: %v", ctx, err)
	}

	var expect []string
	for _, k := range sorted.Keys(want) {
		expect = append(expect, k+"="+want[k])
	}

	if len(got) != len(expect) {
		t.Fatalf("%s: engine has %d keys, oracle has %d\n  engine: %v\n  oracle: %v", ctx, len(got), len(expect), got, expect)
	}
	for i := range got {
		if got[i] != expect[i] {
			t.Fatalf("%s: at %d engine has %q, oracle has %q", ctx, i, got[i], expect[i])
		}
	}

	// Point reads must agree with the scan; an engine whose Get and iterator
	// disagree would satisfy either check alone.
	for _, k := range sorted.Keys(want) {
		got, err := db.Get([]byte(k))
		if err != nil {
			t.Fatalf("%s: Get(%q) = %v, want %q", ctx, k, err, want[k])
		}
		if string(got) != want[k] {
			t.Fatalf("%s: Get(%q) = %q, want %q", ctx, k, got, want[k])
		}
	}
}

// TestCrashRecoversExactlyTheDurablePrefix is the A0.5 exit criterion. For a
// randomized workload with the durability watermark advancing behind
// visibility, a crash must recover the state at DurableSeq: not the last
// applied batch, and not an older one either.
func TestCrashRecoversExactlyTheDurablePrefix(t *testing.T) {
	for seed := uint64(0); seed < 200; seed++ {
		r := rng.New(seed)
		db := model.New()
		orc := &oracle{}

		var lastDurable engine.SeqNum
		var applied engine.SeqNum

		for step := 0; step < 40; step++ {
			b := randomBatch(r)
			seq, err := db.Apply(b, r.IntN(2) == 0)
			if err != nil {
				t.Fatalf("seed %d: apply: %v", seed, err)
			}
			applied = seq
			if got := orc.record(b); got != seq {
				t.Fatalf("seed %d: engine sequence %d, oracle sequence %d", seed, seq, got)
			}

			// The watermark advances behind visibility, sometimes not at all,
			// which is what leaves acknowledged-but-unsynced data in flight.
			if r.IntN(5) < 2 { // two runs in five leave the watermark behind
				target := lastDurable + engine.SeqNum(r.Uint64N(uint64(applied-lastDurable)+1))
				db.AdvanceDurable(target)
				if db.DurableSeq() < lastDurable {
					t.Fatalf("seed %d: durable watermark went backwards", seed)
				}
				lastDurable = db.DurableSeq()
			}

			assertMatches(t, db, orc.stateAt(applied), fmt.Sprintf("seed %d step %d, before crash", seed, step))
		}

		// The unsynced window is real: if this never happens the test is not
		// testing what it claims.
		unsynced := applied - db.DurableSeq()

		durable := db.DurableSeq()
		db.Crash()

		if got := db.DurableSeq(); got != durable {
			t.Fatalf("seed %d: durable sequence moved across the crash: %d then %d", seed, durable, got)
		}
		assertMatches(t, db, orc.stateAt(durable), fmt.Sprintf("seed %d, after crash at durable=%d (lost %d unsynced batches)", seed, durable, unsynced))
	}
}

// TestUnsyncedDataIsReadableAndLosable is the window itself: data that Apply
// has made visible, that a read returns, and that a crash takes. An engine that
// synced eagerly would pass every correctness test and would silently stop
// exercising the case this project cares most about.
func TestUnsyncedDataIsReadableAndLosable(t *testing.T) {
	db := model.New()

	durableSeq, _ := db.Apply(engine.NewBatch().Set([]byte("a"), []byte("1")), true)
	db.AdvanceDurable(durableSeq)

	if _, err := db.Apply(engine.NewBatch().Set([]byte("b"), []byte("2")), false); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Readable before the crash.
	if v, err := db.Get([]byte("b")); err != nil || string(v) != "2" {
		t.Fatalf("unsynced write not readable: %q %v", v, err)
	}
	if db.DurableSeq() != durableSeq {
		t.Fatalf("durable watermark advanced without being told: %d", db.DurableSeq())
	}

	db.Crash()

	// Gone after it.
	if _, err := db.Get([]byte("b")); err != engine.ErrNotFound {
		t.Errorf("unsynced write survived a crash: err = %v", err)
	}
	if v, err := db.Get([]byte("a")); err != nil || string(v) != "1" {
		t.Errorf("synced write did not survive a crash: %q %v", v, err)
	}
}

// TestOnDurableFiresOncePerAdvance covers the callback contract the sim relies
// on: it fires when the watermark moves and not when it does not, and it
// reports the watermark rather than the requested sequence.
func TestOnDurableFiresOncePerAdvance(t *testing.T) {
	db := model.New()

	var fired []engine.SeqNum
	db.OnDurable(func(s engine.SeqNum) { fired = append(fired, s) })

	s1, _ := db.Apply(engine.NewBatch().Set([]byte("a"), []byte("1")), true)
	s2, _ := db.Apply(engine.NewBatch().Set([]byte("b"), []byte("2")), true)

	db.AdvanceDurable(s1)
	db.AdvanceDurable(s1) // already there: no callback
	db.AdvanceDurable(s2)

	want := []engine.SeqNum{s1, s2}
	if len(fired) != len(want) {
		t.Fatalf("callback fired %v, want %v", fired, want)
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Errorf("callback %d reported %d, want %d", i, fired[i], want[i])
		}
	}
}

// TestAdvancePastAppliedPanics: the watermark cannot outrun visibility. A
// caller that advances past what it applied has lost track of its own writes,
// and continuing would silently mark data durable that was never written.
func TestAdvancePastAppliedPanics(t *testing.T) {
	db := model.New()
	seq, _ := db.Apply(engine.NewBatch().Set([]byte("a"), []byte("1")), true)

	defer func() {
		if recover() == nil {
			t.Error("advancing the watermark past the last applied sequence did not panic")
		}
	}()
	db.AdvanceDurable(seq + 1)
}

// randomBatch builds a batch from the injected generator: sets, deletes, and
// the occasional range delete over a small key space, so collisions and
// overlaps are common rather than rare.
func randomBatch(r rng.Rand) *engine.Batch {
	b := engine.NewBatch()
	for n := r.IntN(4) + 1; n > 0; n-- {
		switch {
		case r.IntN(20) < 3:
			lo, hi := keyOf(r.IntN(8)), keyOf(r.IntN(8))
			if bytes.Compare(lo, hi) > 0 {
				lo, hi = hi, lo
			}
			b.DeleteRange(lo, hi)
		case r.IntN(4) == 0:
			b.Delete(keyOf(r.IntN(8)))
		default:
			b.Set(keyOf(r.IntN(8)), []byte(fmt.Sprintf("v%d", r.IntN(100))))
		}
	}
	return b
}

func keyOf(i int) []byte { return []byte(fmt.Sprintf("k%02d", i)) }
