package kv

import (
	"encoding/binary"
	"fmt"

	"github.com/anshkanyadi/rift/hlc"
)

// The MVCC key encoding.
//
// # The shape, and why the timestamp is inverted
//
//	d <len(key) as 4 bytes> <key> <^wall as 8 bytes> <^logical as 4 bytes>
//
// Byte order within one user key is therefore REVERSE timestamp order, so the
// newest version of a key is the first record at or after its prefix. A read at
// timestamp T seeks to the encoding of (key, T) and the first record it lands on
// -- if it is still this key -- is the newest version at or before T. One seek,
// no stepping, on the hot path.
//
// The alternative was ascending timestamps and SeekLT. It costs the same asymptotically
// and it makes the common read -- "the latest value" -- the case that steps, which
// is the wrong way round for a store whose C++ engine will pay for every step at B3.
//
// # The length prefix, which is the classic MVCC encoding bug
//
// A user key may contain any byte, including whatever separator looks safe. With
// `d/<key>/<ts>` and a key containing the separator, one key's versions sort into
// another key's version chain -- and the failure is silent, because every record
// still decodes.
//
// Escaping would also work and is rejected: it makes the encoded length
// data-dependent and the comparison rules subtle, and subtle here means a key
// lands in the wrong chain. A fixed-width big-endian length is neither.
const dataPrefix byte = 'd'

const (
	lenBytes     = 4
	wallBytes    = 8
	logicalBytes = 4
	tsBytes      = wallBytes + logicalBytes
)

// EncodeKey renders (key, ts) as an engine key.
func EncodeKey(key []byte, ts hlc.Timestamp) []byte {
	b := make([]byte, 0, 1+lenBytes+len(key)+tsBytes)
	b = append(b, dataPrefix)
	b = binary.BigEndian.AppendUint32(b, uint32(len(key)))
	b = append(b, key...)
	return appendInvertedTS(b, ts)
}

// KeyPrefix is every version of key, and nothing else.
//
// It is the seek bound a scan of one key's chain stops at, and it is exact
// rather than a "starts with" test: the length prefix means no other key's
// encoding can share it.
func KeyPrefix(key []byte) []byte {
	b := make([]byte, 0, 1+lenBytes+len(key))
	b = append(b, dataPrefix)
	b = binary.BigEndian.AppendUint32(b, uint32(len(key)))
	return append(b, key...)
}

// appendInvertedTS writes the timestamp so that byte order is reverse timestamp
// order.
//
// The inversion is arithmetic on the ENCODING, never on the timestamp. A
// negated hlc.Timestamp is not a timestamp -- it does not order, it does not
// compare, and the moment one exists somebody will pass it to something that
// expects one.
func appendInvertedTS(b []byte, ts hlc.Timestamp) []byte {
	b = binary.BigEndian.AppendUint64(b, ^uint64(ts.Wall))
	return binary.BigEndian.AppendUint32(b, ^ts.Logical)
}

// DecodeKey reads an engine key back into its user key and timestamp.
func DecodeKey(b []byte) (key []byte, ts hlc.Timestamp, ok bool) {
	if len(b) < 1+lenBytes || b[0] != dataPrefix {
		return nil, hlc.Timestamp{}, false
	}
	n := int(binary.BigEndian.Uint32(b[1 : 1+lenBytes]))
	if n < 0 || len(b) != 1+lenBytes+n+tsBytes {
		return nil, hlc.Timestamp{}, false
	}
	key = b[1+lenBytes : 1+lenBytes+n]
	raw := b[1+lenBytes+n:]
	ts.Wall = hlcWall(^binary.BigEndian.Uint64(raw[:wallBytes]))
	ts.Logical = ^binary.BigEndian.Uint32(raw[wallBytes:])
	return key, ts, true
}

// MetaKey is where a key's non-versioned bookkeeping lives. A5 has none; A6's
// lock and write records land here, and the prefix is reserved now so that
// adding them is not a change to how data keys sort.
func MetaKey(key []byte) []byte {
	b := make([]byte, 0, 1+lenBytes+len(key))
	b = append(b, 'm')
	b = binary.BigEndian.AppendUint32(b, uint32(len(key)))
	return append(b, key...)
}

func (s *Store) String() string { return fmt.Sprintf("kv.Store(mark=%s)", s.gcMark) }
