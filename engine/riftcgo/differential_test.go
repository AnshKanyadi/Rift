//go:build rift_cgo

package riftcgo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/differential"
	"github.com/anshkanyadi/rift/engine/model"
)

// B5.4: THE DIFFERENTIAL THROUGH THE CGO PATH. B4's question, one variable
// changed.
//
// The workloads are the differential's own -- 200 seeded operations per seed
// across all three regimes, mixing sets, deletes, bounded and unbounded range
// deletes, syncs and snapshots -- taken from the SUBMISSION section of real
// rift_diff artifacts. They are not reimplemented here and not regenerated:
// identical by construction, because they are read from the file the native run
// wrote.
//
// WHAT THIS IS NOT, AND B5-D6 SAID OTHERWISE.
//
// B5-D6 asked for byte-identical artifacts from a cgo-path run and a native run
// at the same seed. That is not reachable without new boundary surface:
// rift_db_open takes a PATH, so the C boundary cannot be handed a TestEnv, and
// every differential schedule -- including the clean control -- runs on one. A
// cgo run therefore cannot be given the same faults, cannot be killed at the
// same ordinal, and cannot produce the same artifact.
//
// Two ways to close that gap were available and both were declined:
//
//   - A test-only rift_db_open_on_env. Real, permanent boundary surface whose
//     only caller is a test, on the one interface B5 exists to keep narrow.
//   - A Go artifact ENCODER, making a third implementation of a format whose
//     two implementations Ansh weighed and paid for individually.
//
// So the path is isolated a different way, and the substance survives: the
// C++ engine reached THROUGH CGO is compared against engine/model, which is
// the reference every differential verdict is defined against. There is no
// kill, so the recovery RANGE the contract permits does not apply and the
// comparison is EXACT after every operation -- which is a stronger form of
// agreement than a recovered-state check, not a weaker one. What it does not
// cover is a fault schedule crossing the boundary, and nothing here should be
// read as covering it.

func diffBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "engine-cpp", "build", "test", "rift_diff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("rift_diff not built (%v) -- run `make cpp-build` first. "+
			"THIS IS A SKIP, NOT A PASS: the differential workloads did not run.", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// submissionFor produces one real differential workload. The kill ordinal is
// deliberately absent: a killed run's artifact would carry the same submission,
// and the kill is the one thing this path cannot reproduce.
func submissionFor(t *testing.T, bin, regime string, seed uint64) []differential.Op {
	t.Helper()
	cmd := exec.Command(bin, regime, strconv.FormatUint(seed, 10))
	cmd.Env = append(os.Environ(),
		"RIFT_ENGINE_COMMIT=b5.4-cgo-path", "RIFT_MODEL_COMMIT=b5.4-cgo-path")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rift_diff %s %d: %v", regime, seed, err)
	}
	a, err := differential.Parse(out)
	if err != nil {
		t.Fatalf("parsing the artifact for %s/%d: %v", regime, seed, err)
	}
	if len(a.Submission) == 0 {
		t.Fatalf("%s/%d produced no operations, so this seed asserts nothing", regime, seed)
	}
	return a.Submission
}

// bound turns an artifact op's key into what engine.Batch means by it. An empty
// key is a valid key, so boundedness is carried by the flag and never inferred
// from emptiness -- the same distinction the format carries and the same one
// bytePtr carries at the boundary.
func bound(key []byte, isBounded bool) []byte {
	if !isBounded {
		return nil
	}
	if key == nil {
		return []byte{}
	}
	return key
}

func TestTheDifferentialWorkloadsAgreeThroughTheBoundary(t *testing.T) {
	bin := diffBinary(t)
	pinsChecked, pinsWithDrift = 0, 0
	defer func() {
		if pinsChecked == 0 {
			t.Errorf("no snapshot was ever held to its pinned state; the workloads " +
				"take snapshots, so reaching zero means this test is not running the check")
		}
		if pinsWithDrift == 0 {
			t.Errorf("%d snapshots were checked and NOT ONE of them was held across a "+
				"write it should not see. The negative direction was never reached, so "+
				"this test would pass on an engine whose snapshots pin nothing.", pinsChecked)
		}
		t.Logf("snapshots checked: %d, of which %d spanned a later write", pinsChecked, pinsWithDrift)
	}()
	for _, regime := range []string{"default", "flush", "compact"} {
		for seed := uint64(1); seed <= 6; seed++ {
			t.Run(fmt.Sprintf("%s/%d", regime, seed), func(t *testing.T) {
				ops := submissionFor(t, bin, regime, seed)
				replayAgainstTheModel(t, regime, seed, ops)
			})
		}
	}
}

// liveRef is the model the current replay is driving, so checkPinned can ask
// what exists NOW without threading it through every call. One replay runs at a
// time within a subtest.
var liveRef *model.DB

// GF-16, MECHANICALLY. A snapshot check that never sees a key written after the
// take asserts nothing about pinning, and would be green on an engine whose
// snapshots pin nothing at all. These count what was actually reached, and the
// test refuses to pass if the answer is "nothing".
var (
	pinsChecked   int
	pinsWithDrift int
)

func replayAgainstTheModel(t *testing.T, regime string, seed uint64, ops []differential.Op) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cgo")
	db, err := Open(dir, 0, 0, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ref := model.New()
	liveRef = ref

	// Ops sharing a sequence were ONE BATCH. Replaying them as separate batches
	// would be a different workload wearing the same name: the intra-batch
	// rules -- a range delete covering keys written earlier in its own batch --
	// would never be reached, which is exactly the gap BM114 found in the rig
	// itself.
	// Snapshots outstanding, in take order -- the driver releases the oldest.
	var pinned []pin
	defer func() {
		for _, p := range pinned {
			_ = p.snap.Close()
		}
	}()

	i := 0
	for i < len(ops) {
		op := ops[i]
		if op.Seq == 0 {
			// Consumes no sequence: sync, snapshot take, snapshot release.
			switch op.Kind {
			case differential.OpSync:
				w, err := db.Sync()
				if err != nil {
					t.Fatalf("op %d: cgo sync: %v", i, err)
				}
				// THE MODEL IS DRIVEN, NOT SELF-SYNCING -- it has
				// AdvanceDurable and no Sync, because in a simulated run the
				// harness decides when durability completes. So the comparison
				// is not "two engines synced the same"; it is that the
				// watermark the C++ engine reports covers EVERYTHING SUBMITTED,
				// which is what a completed Sync means.
				if w != ref.VisibleSeq() {
					t.Fatalf("op %d: Sync returned %d but %d had been submitted; "+
						"a completed Sync must cover everything before it", i, w, ref.VisibleSeq())
				}
				ref.AdvanceDurable(w)
				if got := db.DurableSeq(); got != w {
					t.Fatalf("op %d: DurableSeq %d disagrees with the watermark Sync returned %d", i, got, w)
				}
				if got := ref.DurableSeq(); got != w {
					t.Fatalf("op %d: model durable %d, cgo durable %d", i, got, w)
				}
			case differential.OpSnapshotTake:
				// THE ONE THING ONLY THIS TEST REACHES. A snapshot's whole
				// content is what it pins ACROSS SUBSEQUENT WRITES, and the
				// parity suite takes one and reads it immediately -- which
				// every broken snapshot in existence also passes.
				//
				// The workloads take and release snapshots tens of operations
				// apart, with sets, deletes, range deletes, flushes and
				// compactions in between. Recording the model's state at the
				// take and holding the C++ snapshot to it at the release is a
				// question no other lane in the repo asks through the boundary.
				pinned = append(pinned, pin{snap: db.NewSnapshot(), state: snapshotOf(t, ref)})
			case differential.OpSnapshotRelease:
				// LIFO, AND A RELEASE WITH NOTHING OUTSTANDING IS A NO-OP --
				// both matching the driver exactly. The workload emits releases
				// unconditionally and the driver closes snapshots->back(), so a
				// test that popped the oldest would be checking the wrong
				// snapshot's pin and a test that refused an empty release would
				// fail on a workload that is behaving correctly.
				if len(pinned) == 0 {
					break
				}
				p := pinned[len(pinned)-1]
				pinned = pinned[:len(pinned)-1]
				checkPinned(t, i, p)
			}
			i++
			continue
		}

		seq := op.Seq
		bc, bm := engine.NewBatch(), engine.NewBatch()
		n := 0
		for i < len(ops) && ops[i].Seq == seq {
			switch ops[i].Kind {
			case differential.OpSet:
				bc.Set(ops[i].Key, ops[i].Value)
				bm.Set(ops[i].Key, ops[i].Value)
			case differential.OpDelete:
				bc.Delete(ops[i].Key)
				bm.Delete(ops[i].Key)
			case differential.OpDeleteRange:
				s := bound(ops[i].Key, ops[i].StartBounded)
				e := bound(ops[i].Value, ops[i].EndBounded)
				bc.DeleteRange(s, e)
				bm.DeleteRange(s, e)
			default:
				t.Fatalf("op %d: kind %d carries a sequence and should not", i, ops[i].Kind)
			}
			i++
			n++
		}

		sc, err := db.Apply(bc, false)
		if err != nil {
			t.Fatalf("seq %d (%d ops): cgo apply: %v", seq, n, err)
		}
		sm, err := ref.Apply(bm, false)
		if err != nil {
			t.Fatalf("seq %d: model apply: %v", seq, err)
		}
		if sc != sm {
			t.Fatalf("seq %d: cgo returned %d, model returned %d", seq, sc, sm)
		}

		// AFTER EVERY BATCH, not once at the end. A divergence compared only at
		// the end is attributed to the last operation, and the whole value of a
		// 200-op seeded workload is that it names the one that did it.
		if err := statesAgree(t, db, ref); err != nil {
			t.Fatalf("%s/%d after seq %d (%d ops): %v", regime, seed, seq, n, err)
		}
	}
}

func statesAgree(t *testing.T, a, b engine.Engine) error {
	t.Helper()
	ka, va := drain(t, a)
	kb, vb := drain(t, b)
	if len(ka) != len(kb) {
		return fmt.Errorf("cgo holds %d keys, model holds %d", len(ka), len(kb))
	}
	for i := range ka {
		if ka[i] != kb[i] {
			return fmt.Errorf("key %d: cgo %q, model %q", i, ka[i], kb[i])
		}
		if !bytes.Equal(va[i], vb[i]) {
			return fmt.Errorf("key %q: cgo %q, model %q", ka[i], va[i], vb[i])
		}
	}
	return nil
}

func drain(t *testing.T, e engine.Engine) ([]string, [][]byte) {
	t.Helper()
	var keys []string
	var vals [][]byte
	it := e.NewIter(engine.IterOptions{})
	for ok := it.First(); ok; ok = it.Next() {
		keys = append(keys, string(it.Key()))
		vals = append(vals, append([]byte(nil), it.Value()...))
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	_ = it.Close()
	return keys, vals
}

// pin is a snapshot taken through the boundary and the state the model held at
// the moment it was taken.
type pin struct {
	snap  engine.Snapshot
	state map[string][]byte
}

func snapshotOf(t *testing.T, e engine.Engine) map[string][]byte {
	t.Helper()
	keys, vals := drain(t, e)
	out := make(map[string][]byte, len(keys))
	for i := range keys {
		out[keys[i]] = vals[i]
	}
	return out
}

// checkPinned holds the C++ snapshot to the state the model had when it was
// taken -- BOTH DIRECTIONS. Every key that was there must still read, with its
// value at the time; and a key written after the take must NOT be visible, or
// the "snapshot" is just a second name for the live database and the positive
// direction alone would never say so.
func checkPinned(t *testing.T, opIndex int, p pin) {
	t.Helper()
	defer p.snap.Close()
	for k, want := range p.state {
		got, err := p.snap.Get([]byte(k))
		if err != nil {
			t.Fatalf("op %d: key %q was present when the snapshot was taken and reads %v now",
				opIndex, k, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("op %d: key %q pinned as %q, snapshot returns %q",
				opIndex, k, want, got)
		}
	}
	// The negative direction needs a key the snapshot cannot have seen. The
	// model's CURRENT state minus its state at the take is exactly that set.
	pinsChecked++
	drifted := false
	for k := range snapshotOf(t, liveRef) {
		if _, was := p.state[k]; was {
			continue
		}
		drifted = true
		if _, err := p.snap.Get([]byte(k)); err == nil {
			t.Fatalf("op %d: key %q was written AFTER the snapshot was taken and "+
				"the snapshot can see it -- it is pinning nothing", opIndex, k)
		}
	}
	if drifted {
		pinsWithDrift++
	}
}
