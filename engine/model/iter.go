package model

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/anshkanyadi/rift/engine"
)

// iter walks one version's entries within [Lower, Upper).
//
// It holds the version rather than the DB, so writes after it was created are
// invisible to it -- which is the same guarantee a snapshot gives, and is what
// makes an iterator safe to hold across an Apply. The C++ engine gets the same
// property from pinning a version set; here it falls out of copy-on-write.
type iter struct {
	v     *version
	lo    []byte
	hi    []byte
	pos   int // index into v.entries; out of [first,last] means invalid
	valid bool
	err   error
	close func()
}

var _ engine.Iterator = (*iter)(nil)

func newIter(v *version, o engine.IterOptions, onClose func()) *iter {
	return &iter{v: v, lo: o.Lower, hi: o.Upper, pos: -1, close: onClose}
}

// bounds returns the half-open index range this iterator may visit.
func (it *iter) bounds() (int, int) {
	lo := 0
	if it.lo != nil {
		lo = sort.Search(len(it.v.entries), func(i int) bool {
			return bytes.Compare(it.v.entries[i].key, it.lo) >= 0
		})
	}
	hi := len(it.v.entries)
	if it.hi != nil {
		hi = sort.Search(len(it.v.entries), func(i int) bool {
			return bytes.Compare(it.v.entries[i].key, it.hi) >= 0
		})
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

func (it *iter) settle(i int) bool {
	lo, hi := it.bounds()
	it.valid = i >= lo && i < hi
	it.pos = i
	return it.valid
}

// SeekGE positions at the first key at or after key, within bounds.
func (it *iter) SeekGE(key []byte) bool {
	lo, _ := it.bounds()
	target := key
	if it.lo != nil && bytes.Compare(target, it.lo) < 0 {
		target = it.lo
	}
	i := sort.Search(len(it.v.entries), func(i int) bool {
		return bytes.Compare(it.v.entries[i].key, target) >= 0
	})
	if i < lo {
		i = lo
	}
	return it.settle(i)
}

// SeekLT positions at the last key strictly before key, within bounds.
func (it *iter) SeekLT(key []byte) bool {
	i := sort.Search(len(it.v.entries), func(i int) bool {
		return bytes.Compare(it.v.entries[i].key, key) >= 0
	})
	return it.settle(i - 1)
}

func (it *iter) First() bool {
	lo, _ := it.bounds()
	return it.settle(lo)
}

func (it *iter) Last() bool {
	_, hi := it.bounds()
	return it.settle(hi - 1)
}

// Next advances. From an invalid position it stays invalid: a caller that
// ignores a false return and keeps calling gets false, not a wrapped-around
// scan that silently produces the wrong answer.
func (it *iter) Next() bool {
	if !it.valid {
		return false
	}
	return it.settle(it.pos + 1)
}

func (it *iter) Prev() bool {
	if !it.valid {
		return false
	}
	return it.settle(it.pos - 1)
}

func (it *iter) Valid() bool { return it.valid }

// Key is valid only until the next positioning call, matching the C++ engine,
// where the bytes point into a block that the next Seek may release. The model
// could safely return a stable slice and that would be the more comfortable
// contract -- and precisely for that reason it does not, because code written
// against the comfortable contract breaks at I1 rather than here.
func (it *iter) Key() []byte {
	if !it.valid {
		return nil
	}
	return it.v.entries[it.pos].key
}

func (it *iter) Value() []byte {
	if !it.valid {
		return nil
	}
	return it.v.entries[it.pos].value
}

func (it *iter) Error() error { return it.err }

func (it *iter) Close() error {
	if it.close != nil {
		it.close()
		it.close = nil
	}
	it.valid = false
	return nil
}

// snapshot is a pinned version. O(1) to take, because copy-on-write means the
// version it holds is already immutable.
type snapshot struct {
	db     *DB
	v      *version
	closed bool
}

var _ engine.Snapshot = (*snapshot)(nil)

func (s *snapshot) Get(key []byte) ([]byte, error) {
	if s.closed {
		return nil, fmt.Errorf("model: Get on a closed snapshot")
	}
	return s.v.get(key)
}

func (s *snapshot) NewIter(o engine.IterOptions) engine.Iterator {
	s.db.openIters++
	return newIter(s.v, o, func() { s.db.openIters-- })
}

func (s *snapshot) Close() error {
	if s.closed {
		return fmt.Errorf("model: Close on an already closed snapshot")
	}
	s.closed = true
	s.db.openSnapshots--
	return nil
}
