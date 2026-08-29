//go:build rift_cgo

// Package simcgo is the harness's crashable adapter over the C++ engine.
//
// # Why it exists at all, and why it is not in riftcgo
//
// The frozen [engine.Engine] contract has no Crash() and no AdvanceDurable().
// That is correct and deliberate: a real engine crashes because the process
// dies, and durability is driven by whoever owns the poller (B1-Q11). Putting
// either on the interface would make a real engine implement a simulator
// concept -- DESIGN-I1 D1, refused.
//
// So the simulator's two primitives live HERE, in the harness, over a package
// that has neither. `engine/model` happens to implement both natively because
// it is a model; this package supplies them for a real engine, and the store
// depends on a store-side interface rather than on the frozen one.
//
// # What a crash is, and the one thing it must not become
//
// DESIGN-I1 D2(b): a crash is close, roll the directory back to the last state
// the harness considers durable, reopen. The engine's OWN recovery then runs,
// on a real directory, which is what closes CF-6.
//
// The rollback point is the harness's, not the engine's, and §12 is why. The
// simulator schedules an fsync completion carrying the sequence captured at
// APPLY time and fires it a latency later, so at fire time that sequence is
// below what the engine has applied. `rift_db_sync` covers everything submitted
// and takes no prefix argument, on a frozen boundary. If a crash restored the
// ENGINE's durable point, the unsynced tail would collapse to empty at every
// sync and a crash would lose strictly less than the same crash on the model.
//
//	THAT IS BUG-005's SHAPE ARRIVING THROUGH GRANULARITY: an injector softened
//	into something easier to satisfy. Conservative and correct are different
//	properties, and a weaker fault is a weaker claim however safe its direction.
//
// So a snapshot is taken per Apply, keyed by the sequence that Apply returned,
// and a crash restores the snapshot for the last sequence the harness declared
// durable. Cost is measured in simcgo_bench_test.go rather than assumed.
package simcgo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/riftcgo"
)

// DB is a riftcgo database plus the two primitives the simulator needs.
type DB struct {
	// EMBEDDED, so every method of the frozen contract this wrapper does not
	// override is forwarded unchanged and cannot silently diverge. Get, NewIter,
	// NewSnapshot, ApproximateDiskBytes, DurableSeq and OnDurable are the
	// engine's, verbatim; only Apply, Crash and AdvanceDurable are ours, and
	// each says why at the method.
	*riftcgo.DB

	dir   string // the live database directory
	snaps string // where per-sequence snapshots are kept

	applied engine.SeqNum // the highest sequence Apply has returned
	durable engine.SeqNum // the highest sequence the HARNESS considers durable
	kept    []engine.SeqNum

	// onDurable is held HERE, not only on the wrapped engine.
	//
	// CF-6.2 asks that after a restart the watermark OnDurable reports is the
	// engine's and not a value the wrapper remembered across the crash. The
	// first version failed it in the opposite direction and worse: Crash()
	// replaces the wrapped *riftcgo.DB with a freshly opened one, which carries
	// NO callbacks, so durability notifications simply stopped. A restarted node
	// would never learn its writes were durable.
	//
	//	A CRASH REPLACES THE ENGINE. ANYTHING REGISTERED ON THE OLD ONE IS GONE,
	//	AND WHAT IS GONE IS SILENT -- there is no error, no panic, and no missing
	//	value; there is a callback that stops being called.
	//
	// Found by CF-6.2's directed check, which is the entry's whole point: the
	// I1 sweep crashes this wrapper thousands of times and exercised this path
	// on every one of them without ever asking what it did.
	onDurable []func(engine.SeqNum)
}

// Open creates a database under root/live with its snapshot store beside it.
func Open(root string) (*DB, error) {
	live := filepath.Join(root, "live")
	snaps := filepath.Join(root, "snaps")
	for _, d := range []string{live, snaps} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	inner, err := riftcgo.Open(live, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	d := &DB{DB: inner, dir: live, snaps: snaps}
	// Sequence 0 is a real crash point: a node can die before applying
	// anything, and the empty directory is what it recovers from.
	if err := d.snapshot(0); err != nil {
		return nil, err
	}
	return d, nil
}

// Apply applies the batch, forces it to disk, and snapshots the directory at
// the sequence it returned, because that sequence is a possible crash point.
//
// # THE SYNC HERE IS NOT OPTIONAL, and the first version of this omitted it
//
// Apply does not block on I/O -- the frozen contract says so, and the bytes
// reach the file only when something syncs. A snapshot taken straight after
// Apply therefore captures a directory THE DATA HAS NOT REACHED YET, and
// restoring it loses a write that was below the harness's durable point.
// Measured, because it was written the wrong way first: the test asserting a
// kept write survives a crash failed with `key not found`, on the write it was
// supposed to keep.
//
//	A SNAPSHOT IS A CLAIM ABOUT WHAT IS ON DISK. TAKING IT AFTER AN OPERATION
//	THAT DELIBERATELY DOES NOT TOUCH THE DISK IS A CLAIM ABOUT NOTHING.
//
// So each Apply syncs before it snapshots, and snapshot(seq) then means what it
// has to mean: everything through seq is durable and nothing after it exists.
// The unsynced window is modelled entirely by the HARNESS -- which sequence it
// declares durable -- rather than by the engine's own buffering.
//
// The cost is an fsync AND a tree copy per Apply, and both are in the number
// cost_test.go reports. The fidelity it gives up is stated with the other I1
// idealizations: the engine's own partially-written-WAL crash paths are not
// exercised by this route, and they are Track B's Env rig's subject, where the
// injection is at the syscall.
func (d *DB) Apply(b *engine.Batch, sync bool) (engine.SeqNum, error) {
	seq, err := d.DB.Apply(b, sync)
	if err != nil {
		return seq, err
	}
	if _, err := d.DB.Sync(); err != nil {
		return seq, err
	}
	d.applied = seq
	return seq, d.snapshot(seq)
}

// AdvanceDurable is the simulator's fsync completion. It syncs for real -- so
// the engine's WAL and its own durability machinery are exercised -- and
// separately records the sequence the HARNESS considers durable, which is the
// one a crash rolls back to. The two differ, and §12 of DESIGN-I1 is why.
// It PANICS rather than returning an error, matching engine/model's signature
// and store.Engine's. That is deliberate: an I/O failure here is the HARNESS
// failing, not a fault being injected. Returning it would send a harness defect
// up a path built to carry modelled faults, where it would be reported as a
// finding about the system.
func (d *DB) AdvanceDurable(seq engine.SeqNum) {
	if seq > d.applied {
		panic(fmt.Sprintf("simcgo: durability advanced to %d, past the last applied sequence %d", seq, d.applied))
	}
	if seq < d.durable {
		panic(fmt.Sprintf("simcgo: durability moved backwards, %d then %d", d.durable, seq))
	}
	if _, err := d.DB.Sync(); err != nil {
		panic("simcgo: sync: " + err.Error())
	}
	d.durable = seq
	if err := d.discardBelow(seq); err != nil {
		panic("simcgo: discarding snapshots: " + err.Error())
	}
}

// DurableSeq reports the HARNESS's durable point, not the engine's.
//
// # This override is the whole reason the wrapper can be honest
//
// Apply syncs, so the engine's own watermark runs ahead of the sequence the
// simulator has declared durable -- rift_db_sync covers everything submitted
// and takes no prefix argument. Reporting the engine's number leaks that
// over-coverage into the store, and store/node.go's continuous cross-check
// fires immediately:
//
//	panic: store: node 3 has made durable something its own record disagrees
//	with: recorded 5 durable entries above the snapshot, engine returned 6
//
// That check compares the driver's record of what it made durable against a
// read-back of the engine, and it is gated on `visibleSeq == DurableSeq()` --
// "nothing in flight, so a read-back IS the durable state". With the engine's
// raw watermark that gate is open almost always and the two derivations are of
// different things.
//
//	THE DRIVER'S RECORD AND THE ENGINE'S ACCOUNT ARE TWO INDEPENDENT DERIVATIONS
//	OF ONE FACT. Making them agree by reporting the engine's number would not
//	fix the disagreement, it would delete one of the derivations.
//
// So the wrapper reports what the harness declared, and a crash rolls back to
// exactly that point. From the store's side the C++ engine is then
// indistinguishable from the model, which is what makes every Track A checker
// apply to it unchanged.
func (d *DB) DurableSeq() engine.SeqNum { return d.durable }

// Crash closes the engine, rolls the directory back to the last sequence the
// harness declared durable, and reopens. Recovery is the engine's own.
// It PANICS on failure, for the reason AdvanceDurable does.
func (d *DB) Crash() {
	if err := d.DB.Close(); err != nil {
		panic("simcgo: close: " + err.Error())
	}
	if err := os.RemoveAll(d.dir); err != nil {
		panic("simcgo: clearing the live directory: " + err.Error())
	}
	if err := copyTree(d.snapAt(d.durable), d.dir); err != nil {
		panic("simcgo: restoring the durable snapshot: " + err.Error())
	}
	inner, err := riftcgo.Open(d.dir, 0, 0, 0)
	if err != nil {
		panic("simcgo: reopening after the crash: " + err.Error())
	}
	d.DB = inner
	// RE-REGISTER, because the callbacks belonged to the engine that just died.
	for _, f := range d.onDurable {
		d.DB.OnDurable(f)
	}
	d.applied = d.durable
}

// OnDurable registers a callback and REMEMBERS it, so a crash can re-register
// it on the reopened engine. See the field's comment for what CF-6.2 caught.
func (d *DB) OnDurable(f func(engine.SeqNum)) {
	d.onDurable = append(d.onDurable, f)
	d.DB.OnDurable(f)
}

func (d *DB) Close() error { return d.DB.Close() }

func (d *DB) snapAt(seq engine.SeqNum) string {
	return filepath.Join(d.snaps, fmt.Sprintf("%020d", uint64(seq)))
}

func (d *DB) snapshot(seq engine.SeqNum) error {
	if err := copyTree(d.dir, d.snapAt(seq)); err != nil {
		return err
	}
	d.kept = append(d.kept, seq)
	return nil
}

// discardBelow drops snapshots that can no longer be a crash point: once the
// harness calls sequence N durable, nothing below N is ever rolled back to.
func (d *DB) discardBelow(seq engine.SeqNum) error {
	keep := d.kept[:0]
	for _, s := range d.kept {
		if s < seq {
			if err := os.RemoveAll(d.snapAt(s)); err != nil {
				return err
			}
			continue
		}
		keep = append(keep, s)
	}
	d.kept = keep
	return nil
}

func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		s := filepath.Join(src, e.Name())
		t := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyTree(s, t); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, t); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
