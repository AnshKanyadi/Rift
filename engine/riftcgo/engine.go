//go:build rift_cgo

package riftcgo

/*
// THE HEADER'S LOCATION IS A FACT ABOUT THE REPO LAYOUT, so it is stated here,
// where ${SRCDIR} makes it package-relative and therefore true in a copied tree
// (which is how the mutant runner builds) and from any working directory.
//
// THE ARCHIVE'S LOCATION IS NOT. It is a build artifact whose directory is a
// choice the build makes, so -L and -l stay in the lane (CGO_LDFLAGS in
// `make cpp-cgo`) rather than being frozen into the source.
//
// The split has a consequence worth naming: this package TYPECHECKS with no C++
// build present, which is what lets the determinism pass load the whole tree
// without Track A needing a C++ toolchain to run lint.
#cgo CFLAGS: -I${SRCDIR}/../../engine-cpp/capi
#cgo LDFLAGS: -lstdc++
#include <stdlib.h>
#include "rift.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/anshkanyadi/rift/engine"
)

// codeError maps a C status onto the error the frozen interface promises.
//
// ONE ENUM, ONE MEANING: the C codes are Status::Code's values, held together
// by a static_assert on the C++ side. This function is the only place a code
// becomes a Go error, so a new code cannot arrive with two meanings.
// THE FROZEN INTERFACE, ASSERTED AT COMPILE TIME AND NOT BY A TEST.
//
// Nothing in this package required these until now: the parity and benchmark
// suites happen to hold a *DB in an engine.Engine, which is an implicit
// assertion in a _test.go file behind a build tag -- so a signature drift would
// have been caught by whichever test ran first, reported as that test failing,
// and NOT caught at all by anything that did not run.
//
// A0.5 froze this interface. A package whose whole purpose is to implement it
// should say so where the compiler reads it.
var (
	_ engine.Engine   = (*DB)(nil)
	_ engine.Iterator = (*iter)(nil)
	_ engine.Snapshot = (*snapshot)(nil)
)

// ErrBusy is backpressure: the write was NOT applied, and the caller should
// drain before retrying.
//
// A SENTINEL AND NOT A STRING, because this is the one code a caller is
// expected to ACT on rather than report. errors.Is(err, ErrBusy) is the
// difference between a retry loop and a crash; an anonymous errors.New here
// would leave every caller matching on message text.
var ErrBusy = errors.New("riftcgo: busy -- unsynced backlog above the threshold")

// errForCode is codeError reached from a plain integer.
//
// IT EXISTS BECAUSE A _test.go FILE MAY NOT IMPORT "C". The exhaustiveness test
// derives its code set by PARSING rift.h -- which is the only way it can fail
// on a code nobody added a case for -- and it therefore holds those codes as
// integers rather than as cgo constants. Without this, the test would have to
// name the codes itself, and a test that names the set it is checking checks
// nothing.
func errForCode(code int) error { return codeError(C.rift_status(code)) }

func codeError(st C.rift_status) error {
	switch st {
	case C.RIFT_OK:
		return nil
	case C.RIFT_NOT_FOUND:
		return engine.ErrNotFound
	case C.RIFT_RECORD_TOO_LARGE:
		return errors.New("riftcgo: record too large")
	case C.RIFT_WAL_BUFFER_FULL:
		return errors.New("riftcgo: WAL buffer full")
	case C.RIFT_IO_ERROR:
		return errors.New("riftcgo: I/O error")
	case C.RIFT_DISK_FULL:
		return errors.New("riftcgo: disk full")
	case C.RIFT_CORRUPTION:
		return errors.New("riftcgo: corruption")
	case C.RIFT_KILLED:
		return errors.New("riftcgo: killed")
	case C.RIFT_INVALID_ARGUMENT:
		return errors.New("riftcgo: invalid argument")
	case C.RIFT_BUSY:
		return ErrBusy
	case C.RIFT_INTERNAL:
		return errors.New("riftcgo: internal boundary failure")
	case C.RIFT_BUFFER_TOO_SMALL:
		return errors.New("riftcgo: buffer too small")
	}
	// A CODE THIS BUILD CANNOT NAME IS REPORTED, NEVER MAPPED TO A NEIGHBOUR.
	// Guessing would make a new engine code arrive as an old meaning.
	return fmt.Errorf("riftcgo: unknown status %d", int(st))
}

// bytePtr returns a pointer C may read for the duration of one call, and nil
// for a nil slice — which is how UNBOUNDED crosses, since an empty non-nil
// slice is the EMPTY KEY and the two must not become the same pointer.
func bytePtr(b []byte) (*C.char, C.size_t) {
	if b == nil {
		return nil, 0
	}
	if len(b) == 0 {
		// A non-nil empty slice needs a non-nil pointer, or the C side reads it
		// as unbounded. `&empty[0]` panics on a zero-length slice, so a
		// one-byte backing array is used and the length says zero.
		return (*C.char)(unsafe.Pointer(&emptySentinel[0])), 0
	}
	return (*C.char)(unsafe.Pointer(&b[0])), C.size_t(len(b))
}

var emptySentinel = [1]byte{}

// DB implements engine.Engine.
type DB struct {
	h *C.rift_db

	mu        sync.Mutex
	onDurable []func(engine.SeqNum)
	lastSeen  engine.SeqNum
}

// Open opens or creates a database. Zero caps mean the shipped defaults, so a
// caller that does not care cannot accidentally configure a regime.
func Open(dir string, flushBytes, walBufferBytes, maxRecordBytes uint64) (*DB, error) {
	cdir := C.CString(dir)
	defer C.free(unsafe.Pointer(cdir))
	var h *C.rift_db
	st := C.rift_db_open(cdir, C.size_t(len(dir)), C.uint64_t(flushBytes),
		C.uint64_t(walBufferBytes), C.uint64_t(maxRecordBytes), &h)
	if err := codeError(st); err != nil {
		return nil, err
	}
	return &DB{h: h}, nil
}

func (d *DB) Apply(b *engine.Batch, sync bool) (engine.SeqNum, error) {
	cb := C.rift_batch_new()
	if cb == nil {
		return 0, errors.New("riftcgo: batch allocation failed")
	}
	defer C.rift_batch_free(cb)

	// ONE CALL COMMITS THE WHOLE BATCH. Building it costs one cgo call per op,
	// which is what BENCHMARKS.md measures against a native harness — the
	// alternative, one Write per op, is the cost the batch interface exists to
	// remove and is measured as the comparison rather than assumed away.
	for _, op := range b.Ops() {
		var st C.rift_status
		switch op.Kind {
		case engine.OpSet:
			kp, kl := bytePtr(op.Key)
			vp, vl := bytePtr(op.Value)
			st = C.rift_batch_set(cb, kp, kl, vp, vl)
		case engine.OpDelete:
			kp, kl := bytePtr(op.Key)
			st = C.rift_batch_delete(cb, kp, kl)
		case engine.OpDeleteRange:
			sp, sl := bytePtr(op.Key)
			ep, el := bytePtr(op.End)
			st = C.rift_batch_delete_range(cb, sp, sl, ep, el)
		default:
			return 0, fmt.Errorf("riftcgo: unknown op kind %v", op.Kind)
		}
		if err := codeError(st); err != nil {
			return 0, err
		}
	}
	var seq C.uint64_t
	if err := codeError(C.rift_db_write(d.h, cb, &seq)); err != nil {
		return engine.SeqNum(seq), err
	}
	if sync {
		// THE FLAG IS HONOURED BY CALLING Sync, not by passing it across. The
		// C boundary has no sync flag, deliberately: db.h's divergence 2 says
		// the flag's POLICY is a decision about the pair rather than about the
		// engine, and Write never blocks on I/O whatever the caller wanted.
		if _, err := d.Sync(); err != nil {
			return engine.SeqNum(seq), err
		}
	}
	return engine.SeqNum(seq), nil
}

// Sync blocks until everything applied so far is durable, and returns the
// watermark. WHOEVER OWNS THE GOROUTINE CALLS THIS — see the package doc.
func (d *DB) Sync() (engine.SeqNum, error) {
	var w C.uint64_t
	if err := codeError(C.rift_db_sync(d.h, &w)); err != nil {
		return engine.SeqNum(w), err
	}
	d.fire(engine.SeqNum(w))
	return engine.SeqNum(w), nil
}

func (d *DB) DurableSeq() engine.SeqNum {
	return engine.SeqNum(C.rift_db_durable_seq(d.h))
}

// OnDurable registers a callback. IT IS INVOKED FROM THIS SIDE, by whichever
// goroutine called Sync — no C frame ever calls into Go, which is [A1]'s rule
// and the reason the C boundary has no callback registration at all.
func (d *DB) OnDurable(f func(engine.SeqNum)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onDurable = append(d.onDurable, f)
}

func (d *DB) fire(w engine.SeqNum) {
	d.mu.Lock()
	if w <= d.lastSeen {
		d.mu.Unlock()
		return
	}
	d.lastSeen = w
	fns := make([]func(engine.SeqNum), len(d.onDurable))
	copy(fns, d.onDurable)
	d.mu.Unlock()
	// CALLED OUTSIDE THE LOCK: a callback posts to a node mailbox, and holding
	// a lock across a send is how a mailbox becomes a deadlock.
	for _, f := range fns {
		f(w)
	}
}

func (d *DB) Get(key []byte) ([]byte, error) {
	kp, kl := bytePtr(key)
	// A CALLER-SUPPLIED BUFFER, GROWN ONCE IF SHORT. The first size is a guess
	// and the second is the answer the engine gave, so at most two calls.
	buf := make([]byte, 256)
	var needed C.size_t
	st := C.rift_db_get(d.h, kp, kl, (*C.char)(unsafe.Pointer(&buf[0])),
		C.size_t(len(buf)), &needed)
	if st == C.RIFT_BUFFER_TOO_SMALL {
		buf = make([]byte, int(needed)+1)
		st = C.rift_db_get(d.h, kp, kl, (*C.char)(unsafe.Pointer(&buf[0])),
			C.size_t(len(buf)), &needed)
	}
	if err := codeError(st); err != nil {
		return nil, err
	}
	return buf[:needed], nil
}

func (d *DB) NewSnapshot() engine.Snapshot {
	var h *C.rift_snapshot
	if err := codeError(C.rift_db_snapshot(d.h, &h)); err != nil {
		return &snapshot{err: err}
	}
	return &snapshot{db: d, h: h}
}

func (d *DB) ApproximateDiskBytes(start, end []byte) (uint64, error) {
	// NOT ACROSS THE BOUNDARY YET, and saying so is better than returning a
	// number nobody computed. It is a split decision's input and Track A does
	// not call it through this path before I1.
	return 0, errors.New("riftcgo: ApproximateDiskBytes is not implemented at B5")
}

func (d *DB) Close() error {
	if d.h == nil {
		return nil
	}
	err := codeError(C.rift_db_close(d.h))
	d.h = nil
	return err
}
