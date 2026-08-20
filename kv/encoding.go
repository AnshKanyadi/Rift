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

// EncodeKey renders (key, ts) as an engine key under a namespace.
//
// The namespace is the range's engine-key prefix. A4 gave every range a
// contiguous keyspace so a replica's state can be written, cleared and recovered
// without touching another's; MVCC versions live inside it for the same reason,
// and DeleteRange over the namespace still means "everything this range holds".
func EncodeKey(ns, key []byte, ts hlc.Timestamp) []byte {
	b := make([]byte, 0, len(ns)+1+lenBytes+len(key)+tsBytes)
	b = append(b, ns...)
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
func KeyPrefix(ns, key []byte) []byte {
	b := make([]byte, 0, len(ns)+1+lenBytes+len(key))
	b = append(b, ns...)
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

// DecodeKey reads an engine key back into its user key and timestamp, given the
// namespace it was written under.
func DecodeKey(ns, b []byte) (key []byte, ts hlc.Timestamp, ok bool) {
	if len(b) < len(ns)+1+lenBytes || string(b[:len(ns)]) != string(ns) || b[len(ns)] != dataPrefix {
		return nil, hlc.Timestamp{}, false
	}
	b = b[len(ns):]
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

// The transaction record kinds, in the namespace A5 reserved.
//
//	lock   l <len> <key>                 at most one per key, or the key is not locked
//	write  w <len> <key> <^commit_ts>    newest-commit-first within a key, like data
//	txn    t <len> <primary key>         the transaction record: committed or rolled back
//
// # Why write records sort newest-first and data does too
//
// A read at T wants the newest COMMIT at or before T, then the data version at
// that commit's start timestamp. Both lookups are one seek in the same
// direction, which is A5's D-A5-3 reasoning applied to the second index rather
// than restated for it.
const (
	lockPrefix  byte = 'l'
	writePrefix byte = 'w'
	txnPrefix   byte = 't'
)

// LockKey is where a key's lock lives. At most one exists at a time: a second
// prewrite on a locked key is a write conflict, not a second lock.
func LockKey(ns, key []byte) []byte { return metaKey(ns, lockPrefix, key) }

// TxnKey is where a transaction's record lives: its PRIMARY key and its START
// timestamp.
//
// Keyed by the key and not by a range identifier, deliberately. A split can move
// the primary after a secondary's lock was written, and a lock naming a range
// would then point at a range that no longer holds the record -- a position that
// ages, which is BUG-011's class. The key does not age; resolution re-routes it.
//
// # And keyed by the start timestamp, which the first draft left out
//
// A key is the primary of MANY transactions over its life. Without the start
// timestamp the second transaction to use a key as its primary finds the first
// one's record sitting there and is refused as already-decided -- and worse, a
// resolver holding a lock from the second reads the FIRST one's verdict and
// applies it. Two transactions would share one decision.
//
// The lock already carries the start timestamp, so a resolver can always build
// this key from what it holds. Found by the first test that used one key as the
// primary of two transactions, which is the ordinary case rather than an exotic
// one.
func TxnKey(ns, key []byte, startTS hlc.Timestamp) []byte {
	return appendInvertedTS(metaKey(ns, txnPrefix, key), startTS)
}

// WriteKey is the commit record for key at commitTS.
func WriteKey(ns, key []byte, commitTS hlc.Timestamp) []byte {
	return appendInvertedTS(metaKey(ns, writePrefix, key), commitTS)
}

// WritePrefix is every commit record for key, and nothing else.
func WritePrefix(ns, key []byte) []byte { return metaKey(ns, writePrefix, key) }

// DecodeWriteKey reads a commit record's key back.
func DecodeWriteKey(ns, b []byte) (key []byte, commitTS hlc.Timestamp, ok bool) {
	key, rest, ok := takeMetaKey(ns, writePrefix, b)
	if !ok || len(rest) != tsBytes {
		return nil, hlc.Timestamp{}, false
	}
	commitTS.Wall = hlcWall(^binary.BigEndian.Uint64(rest[:wallBytes]))
	commitTS.Logical = ^binary.BigEndian.Uint32(rest[wallBytes:])
	return key, commitTS, true
}

// MetaKey is retained as the generic form. A5 reserved the prefix; A6 uses it
// through the three named constructors above, which is what keeps the layout in
// one place.
func MetaKey(ns, key []byte) []byte { return metaKey(ns, 'm', key) }

func metaKey(ns []byte, kind byte, key []byte) []byte {
	b := make([]byte, 0, len(ns)+1+lenBytes+len(key)+tsBytes)
	b = append(b, ns...)
	b = append(b, kind)
	b = binary.BigEndian.AppendUint32(b, uint32(len(key)))
	return append(b, key...)
}

func takeMetaKey(ns []byte, kind byte, b []byte) (key, rest []byte, ok bool) {
	if len(b) < len(ns)+1+lenBytes || string(b[:len(ns)]) != string(ns) || b[len(ns)] != kind {
		return nil, nil, false
	}
	b = b[len(ns)+1:]
	n := int(binary.BigEndian.Uint32(b[:lenBytes]))
	b = b[lenBytes:]
	if n < 0 || len(b) < n {
		return nil, nil, false
	}
	return b[:n], b[n:], true
}

func (s *Store) String() string { return fmt.Sprintf("kv.Store(mark=%s)", s.gcMark) }
