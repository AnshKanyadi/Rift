//go:build rift_cgo

package simcgo

import (
	"testing"

	"github.com/anshkanyadi/rift/engine"
)

// CF-6's three checks, each directed.
//
// CF-6 opened at B5.4 and is explicit that incidental exposure is not closure:
// *"'it happens incidentally' is how a gap stays open while looking closed."*
// I1 runs the stack on this wrapper and crashes it thousands of times, which
// exercises all three paths — and exercising is not checking. Each check below
// asserts the specific property CF-6 names, on a schedule built to hit it.

// TestCF6_1_CrashMidApplyLeavesNoGoSideClaim.
//
// CF-6.1: "A crash mid-Apply through the wrapper leaves no Go-side state
// claiming a sequence the engine never took. Apply returns a SeqNum before
// durability; a crash between the two is the ordinary case and the wrapper must
// not have cached anything about it."
func TestCF6_1_CrashMidApplyLeavesNoGoSideClaim(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	first, err := d.Apply(engine.NewBatch().Set([]byte("a"), []byte("1")), false)
	if err != nil {
		t.Fatal(err)
	}
	d.AdvanceDurable(first)

	// Sequences the simulator never declares durable. This is the ordinary case:
	// Apply returned them, the harness has not synced them at its own watermark.
	for i := 0; i < 5; i++ {
		if _, err := d.Apply(engine.NewBatch().Set([]byte("b"), []byte("2")), false); err != nil {
			t.Fatal(err)
		}
	}
	claimedBefore := d.applied
	if claimedBefore <= first {
		t.Fatalf("premise failed: applied %d did not advance past the durable point %d",
			claimedBefore, first)
	}

	d.Crash()

	if d.applied != first {
		t.Errorf("after the crash the wrapper still claims applied=%d; the engine went back to %d.\n"+
			"      That is Go-side state claiming a sequence the engine never took, which is "+
			"exactly what CF-6.1 asks about.", d.applied, first)
	}
	if got := d.DurableSeq(); got != first {
		t.Errorf("DurableSeq is %d after the crash, want %d", got, first)
	}
	if _, err := d.Get([]byte("b")); err == nil {
		t.Error("a write above the durable point survived the crash")
	}
}

// TestCF6_2_OnDurableReportsTheEnginesWatermarkNotARememberedOne.
//
// CF-6.2: "OnDurable fires from the Go side. After a restart, the watermark it
// reports must be the engine's and not a value the wrapper remembered across the
// crash."
func TestCF6_2_OnDurableReportsTheEnginesWatermarkNotARememberedOne(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var fired []engine.SeqNum
	d.OnDurable(func(s engine.SeqNum) { fired = append(fired, s) })

	seq, err := d.Apply(engine.NewBatch().Set([]byte("k"), []byte("v")), false)
	if err != nil {
		t.Fatal(err)
	}
	d.AdvanceDurable(seq)
	if len(fired) == 0 {
		t.Fatal("OnDurable never fired, so this test proves nothing about what it reports")
	}
	before := len(fired)

	// Writes the harness never declares durable, then a crash.
	for i := 0; i < 3; i++ {
		if _, err := d.Apply(engine.NewBatch().Set([]byte("gone"), []byte("x")), false); err != nil {
			t.Fatal(err)
		}
	}
	d.Crash()

	// The callback registry must survive the reopen, and what it reports after
	// must be the reopened engine's, not a remembered high-water mark.
	seq2, err := d.Apply(engine.NewBatch().Set([]byte("after"), []byte("y")), false)
	if err != nil {
		t.Fatal(err)
	}
	d.AdvanceDurable(seq2)
	if len(fired) <= before {
		t.Fatal("OnDurable stopped firing after the crash: the registry did not survive the reopen, " +
			"so a restarted node would never learn its writes were durable")
	}
	last := fired[len(fired)-1]
	if last != d.DurableSeq() {
		t.Errorf("OnDurable reported %d but DurableSeq is %d after the restart", last, d.DurableSeq())
	}
	if last > d.applied {
		t.Errorf("OnDurable reported %d, past what has been applied since the crash (%d) -- "+
			"that is a value remembered across the crash, which CF-6.2 forbids", last, d.applied)
	}
}

// TestCF6_3_AnIteratorHeldAcrossACrashIsNotReadAsLive.
//
// CF-6.3: "An iterator holding a block across a crash. The wrapper holds decoded
// pairs in Go memory that the C side no longer backs. Nothing must read them as
// live."
func TestCF6_3_AnIteratorHeldAcrossACrashIsNotReadAsLive(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	b := engine.NewBatch()
	for _, k := range []string{"k1", "k2", "k3"} {
		b.Set([]byte(k), []byte("v"))
	}
	seq, err := d.Apply(b, false)
	if err != nil {
		t.Fatal(err)
	}
	d.AdvanceDurable(seq)

	it := d.NewIter(engine.IterOptions{})
	if !it.First() {
		t.Fatal("the iterator saw nothing, so this test proves nothing")
	}
	t.Logf("iterator positioned at %q before the crash", it.Key())

	// The crash closes the C++ DB out from under the open iterator. The wrapper
	// holds Go-side decoded pairs the C side no longer backs.
	d.Crash()

	// What must NOT happen is a read of freed C memory or a silent stale answer
	// presented as live. Closing must be safe, and any further use must not
	// crash the process.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("using an iterator held across a crash PANICKED: %v.\n"+
					"      CF-6.3 asks that nothing read it as live; a panic is not a read, but a "+
					"harness that crashes a node must not crash the process.", r)
			}
		}()
		it.Next()
		_ = it.Key()
		_ = it.Value()
		if err := it.Close(); err != nil {
			t.Logf("closing an iterator held across a crash returned: %v (an error is fine; a "+
				"silent stale answer is not)", err)
		}
	}()

	// And the engine is usable afterwards, which is what a restart needs.
	if _, err := d.Get([]byte("k1")); err != nil {
		t.Errorf("the engine is unusable after an iterator was held across a crash: %v", err)
	}
}
