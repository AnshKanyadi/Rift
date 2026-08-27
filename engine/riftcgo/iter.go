//go:build rift_cgo

package riftcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../engine-cpp/capi
#include <stdlib.h>
#include "rift.h"
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/anshkanyadi/rift/engine"
)

// DefaultBlock is how many pairs one boundary crossing fetches.
//
// IT IS A VARIABLE AND NOT A CONSTANT, because BENCHMARKS.md is supposed to
// FIND it rather than assume it: the block interface exists to amortise
// per-call cgo overhead, and how much it amortises is the measurement.
var DefaultBlock = 64

// iter serves a CURSOR from the BLOCK it holds.
//
// The frozen engine.Iterator is a cursor; the boundary fetches blocks. This is
// where the two meet, and the alternative — buffering the whole iteration —
// would present the same cursor while defeating the amortisation the block
// interface exists to provide.
type iter struct {
	h     *C.rift_iter
	block int

	// The block currently held. keyBuf/valBuf are the caller memory the C side
	// packs into; keys/vals are SUB-SLICES of them, not copies.
	//
	// That is exactly what the frozen interface permits -- Key and Value are
	// "valid only until the next positioning call", and the only thing that
	// refills these buffers IS a positioning call. Copying per pair instead
	// would be two allocations per entry, which would put Go's allocator into
	// every boundary measurement BENCHMARKS.md is supposed to be taking.
	keyBuf, valBuf []byte
	keys, vals     [][]byte
	klen, vlen     []uint32
	at             int

	forward bool
	valid   bool
	err     error
	closed  bool
}

func (d *DB) NewIter(o engine.IterOptions) engine.Iterator {
	lp, ll := bytePtr(o.Lower)
	up, ul := bytePtr(o.Upper)
	var h *C.rift_iter
	if err := codeError(C.rift_db_iter(d.h, lp, ll, up, ul, &h)); err != nil {
		return &iter{err: err}
	}
	return &iter{h: h, block: DefaultBlock, forward: true}
}

func (i *iter) seek(mode C.rift_seek_mode, key []byte, forward bool) bool {
	if i.h == nil {
		return false
	}
	i.keys, i.vals, i.at = nil, nil, 0
	i.forward = forward
	kp, kl := bytePtr(key)
	var valid C.int
	if err := codeError(C.rift_iter_seek(i.h, mode, kp, kl, &valid)); err != nil {
		i.err = err
		i.valid = false
		return false
	}
	if valid == 0 {
		i.valid = false
		return false
	}
	// A SEEK POSITIONS; THE FETCH RETURNS THE ENTRY IT LANDED ON. Fetching here
	// rather than lazily keeps Valid() honest immediately after a seek.
	return i.fill()
}

// fill fetches one block, growing its buffers if a single pair needs more than
// they hold.
//
// CF-3: the outer loop's progress quantity is that a RIFT_BUFFER_TOO_SMALL
// carries the capacities the pair NEEDS, so the retry after a grow cannot ask
// again for the same pair. THE BOUND IS TWO PASSES AND IS ASSERTED, not hoped
// for: a boundary that reported "too small" without saying how small would give
// this loop no terminating quantity at all.
func (i *iter) fill() bool {
	n := i.block
	if n <= 0 {
		n = 1
	}
	if len(i.klen) < n {
		i.klen = make([]uint32, n)
		i.vlen = make([]uint32, n)
	}
	if i.keyBuf == nil {
		// Sized so an ordinary pair never round-trips twice. This is a
		// PERFORMANCE choice only because the grow path below makes it one.
		i.keyBuf = make([]byte, n*256+4096)
		i.valBuf = make([]byte, n*1024+4096)
	}
	for pass := 0; ; pass++ {
		var filled, ku, vu C.size_t
		fwd := C.int(0)
		if i.forward {
			fwd = 1
		}
		st := C.rift_iter_block(i.h, fwd, C.size_t(n),
			(*C.uint32_t)(unsafe.Pointer(&i.klen[0])),
			(*C.uint32_t)(unsafe.Pointer(&i.vlen[0])),
			(*C.char)(unsafe.Pointer(&i.keyBuf[0])), C.size_t(len(i.keyBuf)), &ku,
			(*C.char)(unsafe.Pointer(&i.valBuf[0])), C.size_t(len(i.valBuf)), &vu, &filled)
		if st == C.RIFT_BUFFER_TOO_SMALL {
			if pass > 0 {
				// The needed capacities were honoured and it still did not fit,
				// so the boundary is not reporting what it says it reports.
				i.err = errors.New("riftcgo: the boundary asked twice for the same pair")
				i.valid = false
				return false
			}
			if int(ku) > len(i.keyBuf) {
				i.keyBuf = make([]byte, int(ku))
			}
			if int(vu) > len(i.valBuf) {
				i.valBuf = make([]byte, int(vu))
			}
			continue
		}
		if err := codeError(st); err != nil {
			i.err = err
			i.valid = false
			return false
		}
		if filled == 0 {
			i.valid = false
			return false
		}
		i.keys = i.keys[:0]
		i.vals = i.vals[:0]
		ko, vo := 0, 0
		for j := 0; j < int(filled); j++ {
			i.keys = append(i.keys, i.keyBuf[ko:ko+int(i.klen[j])])
			i.vals = append(i.vals, i.valBuf[vo:vo+int(i.vlen[j])])
			ko += int(i.klen[j])
			vo += int(i.vlen[j])
		}
		i.at = 0
		i.valid = true
		return true
	}
}

func (i *iter) step() bool {
	if !i.valid {
		return false
	}
	i.at++
	if i.at < len(i.keys) {
		return true
	}
	return i.fill()
}

func (i *iter) First() bool { return i.seek(C.RIFT_SEEK_FIRST, nil, true) }
func (i *iter) Last() bool  { return i.seek(C.RIFT_SEEK_LAST, nil, false) }
func (i *iter) SeekGE(key []byte) bool {
	return i.seek(C.RIFT_SEEK_GE, key, true)
}
func (i *iter) SeekLT(key []byte) bool {
	return i.seek(C.RIFT_SEEK_LT, key, false)
}

func (i *iter) Next() bool {
	// A DIRECTION SWITCH RE-SEEKS, because the block held was fetched walking
	// the other way and says nothing about this one. It is MergedIter's rule
	// one layer up, and for the same reason: stepping back without re-seeking
	// reads whichever entries happen to be under the cursor, which is wrong in
	// a way that looks like a missing key.
	if !i.forward {
		if !i.valid {
			return false
		}
		k := append([]byte(nil), i.keys[i.at]...)
		if !i.seek(C.RIFT_SEEK_GE, k, true) {
			return false
		}
		return i.step()
	}
	return i.step()
}

func (i *iter) Prev() bool {
	if i.forward {
		if !i.valid {
			return false
		}
		k := append([]byte(nil), i.keys[i.at]...)
		if !i.seek(C.RIFT_SEEK_LT, k, false) {
			return false
		}
		return true
	}
	return i.step()
}

func (i *iter) Valid() bool { return i.valid && i.at < len(i.keys) }

func (i *iter) Key() []byte {
	if !i.Valid() {
		return nil
	}
	return i.keys[i.at]
}

func (i *iter) Value() []byte {
	if !i.Valid() {
		return nil
	}
	return i.vals[i.at]
}

func (i *iter) Error() error { return i.err }

func (i *iter) Close() error {
	if i.closed {
		return nil
	}
	i.closed = true
	if i.h != nil {
		C.rift_iter_free(i.h)
		i.h = nil
	}
	i.valid = false
	return i.err
}

// ------------------------------------------------------------------ snapshot

type snapshot struct {
	db  *DB
	h   *C.rift_snapshot
	err error
}

func (s *snapshot) Get(key []byte) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	kp, kl := bytePtr(key)
	buf := make([]byte, 256)
	var needed C.size_t
	st := C.rift_snapshot_get(s.h, kp, kl, (*C.char)(unsafe.Pointer(&buf[0])),
		C.size_t(len(buf)), &needed)
	if st == C.RIFT_BUFFER_TOO_SMALL {
		buf = make([]byte, int(needed)+1)
		st = C.rift_snapshot_get(s.h, kp, kl, (*C.char)(unsafe.Pointer(&buf[0])),
			C.size_t(len(buf)), &needed)
	}
	if err := codeError(st); err != nil {
		return nil, err
	}
	return buf[:needed], nil
}

// NewIter on a snapshot is NOT IMPLEMENTED AT B5, and the boundary does not
// pretend otherwise. Adding it means a snapshot-scoped iterator handle in C,
// which is real surface; nothing in B5's criteria needs it, and an iterator
// that silently read the LIVE state would be a wrong answer wearing a
// snapshot's name.
func (s *snapshot) NewIter(o engine.IterOptions) engine.Iterator {
	return &iter{err: errors.New("riftcgo: snapshot iterators are not implemented at B5")}
}

func (s *snapshot) Close() error {
	if s.err != nil {
		return s.err
	}
	if s.h == nil {
		return nil
	}
	err := codeError(C.rift_snapshot_close(s.h))
	s.h = nil
	return err
}
