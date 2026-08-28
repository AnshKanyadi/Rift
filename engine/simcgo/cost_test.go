//go:build rift_cgo

package simcgo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/engine"
)

// TestPerApplyCopyCost is the measurement DESIGN-I1 §12 rests on.
//
// Ansh: "whether it is affordable is arithmetic rather than argument." So this
// reports the arithmetic: wall time per Apply with and without the snapshot,
// and the directory bytes copied, at sim-shaped batch sizes.
//
// It is a MEASUREMENT and not a gate. It asserts only that the mechanism works
// -- a crash rolls back to the harness's durable point and the engine recovers
// from it -- and prints numbers for the ruling. A threshold here would be a
// number nobody chose.
func TestPerApplyCopyCost(t *testing.T) {
	const (
		batches   = 400 // enough to grow past the first flush
		perBatch  = 4   // keys per batch, sim-shaped: hardstate + a few entries
		valueSize = 256
	)

	type row struct {
		label    string
		snapshot bool
		elapsed  time.Duration
		bytes    int64
	}
	var rows []row

	for _, snap := range []bool{false, true} {
		root := t.TempDir()
		d, err := Open(root)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		val := make([]byte, valueSize)
		for i := range val {
			val[i] = byte(i)
		}

		start := time.Now()
		for i := 0; i < batches; i++ {
			b := engine.NewBatch()
			for k := 0; k < perBatch; k++ {
				b.Set([]byte(fmt.Sprintf("r1/e/%010d/%d", i, k)), val)
			}
			var seq engine.SeqNum
			if snap {
				seq, err = d.Apply(b, false)
			} else {
				seq, err = d.DB.Apply(b, false)
				d.applied = seq
			}
			if err != nil {
				t.Fatalf("apply %d: %v", i, err)
			}
			// A durability event every 16 batches, lagging by 8 -- the shape the
			// simulator produces: the sequence was captured at apply time and
			// fires later, so it is below what has since been applied.
			if i%16 == 15 {
				// SeqNum is UNSIGNED: subtracting past zero wraps rather than
				// going negative, and the first version of this line did
				// exactly that -- "durability advanced to 18446744073709551600"
				// -- caught by AdvanceDurable's own precondition rather than by
				// producing a quietly wrong measurement.
				var lag engine.SeqNum
				if seq > 8 {
					lag = seq - 8
				}
				if snap {
					d.AdvanceDurable(lag)
				} else if _, err := d.DB.Sync(); err != nil {
					t.Fatalf("sync %d: %v", i, err)
				}
			}
		}
		elapsed := time.Since(start)

		var bytes int64
		_ = filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				bytes += fi.Size()
			}
			return nil
		})
		label := "Apply only"
		if snap {
			label = "Apply + per-Apply snapshot"
		}
		rows = append(rows, row{label, snap, elapsed, bytes})
		if err := d.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	t.Logf("%d batches x %d keys x %d bytes", batches, perBatch, valueSize)
	for _, r := range rows {
		t.Logf("  %-28s total %-10v per-Apply %-10v on-disk %d KB",
			r.label, r.elapsed.Round(time.Millisecond),
			(r.elapsed / batches).Round(time.Microsecond), r.bytes/1024)
	}
	base, with := rows[0].elapsed, rows[1].elapsed
	t.Logf("  SNAPSHOT COST: %v per Apply, %.1fx the un-snapshotted path",
		((with - base) / batches).Round(time.Microsecond),
		float64(with)/float64(base))
}

// TestACrashRollsBackToTheHarnessDurablePoint is the correctness half, and it
// is the reason B was chosen over C.
//
// It asserts the thing §12 says C cannot do: after a crash, a key written after
// the harness's durable point is GONE, even though the engine had synced it.
func TestACrashRollsBackToTheHarnessDurablePoint(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	b1 := engine.NewBatch().Set([]byte("kept"), []byte("v1"))
	durable, err := d.Apply(b1, false)
	if err != nil {
		t.Fatal(err)
	}

	// More writes land AFTER the sequence the simulator will call durable.
	b2 := engine.NewBatch().Set([]byte("lost"), []byte("v2"))
	if _, err := d.Apply(b2, false); err != nil {
		t.Fatal(err)
	}

	// The fsync completion carries the EARLIER sequence -- the normal case,
	// because the simulator captured it at apply time. Sync() really runs and
	// really covers everything, which is exactly the divergence being handled.
	d.AdvanceDurable(durable)
	if got := d.DB.DurableSeq(); got <= durable {
		t.Fatalf("premise failed: the engine's own watermark is %d, not past the harness's %d. "+
			"This test exists because Sync covers everything submitted; if it no longer does, "+
			"the gap DESIGN-I1 section 12 describes has closed and B may be unnecessary", got, durable)
	}
	t.Logf("engine watermark %d is past the harness's durable point %d, as expected",
		d.DB.DurableSeq(), durable)

	d.Crash()

	if v, err := d.DB.Get([]byte("kept")); err != nil || string(v) != "v1" {
		t.Errorf("a write below the harness's durable point did not survive the crash: %q %v", v, err)
	}
	if _, err := d.DB.Get([]byte("lost")); err == nil {
		t.Error("A WRITE ABOVE THE HARNESS'S DURABLE POINT SURVIVED THE CRASH.\n" +
			"      That is the whole of what B buys: the engine had synced it, and the\n" +
			"      simulator had not declared it durable. A crash that keeps it loses\n" +
			"      strictly less than the same crash on engine/model, which is BUG-005's\n" +
			"      shape arriving through granularity.")
	}
}
