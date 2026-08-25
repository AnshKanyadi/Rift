package store

import (
	"encoding/binary"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/raft"
)

// Wire and storage encoding. Fixed-width and explicit, for the same reason the
// envelope codec is: an encoding discovered at run time is an encoding that can
// differ between runs.

func putU64(b []byte, v uint64) []byte { return binary.BigEndian.AppendUint64(b, v) }

func takeU64(b []byte) (uint64, []byte, bool) {
	if len(b) < 8 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint64(b), b[8:], true
}

func putBytes(b, v []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(v)))
	return append(b, v...)
}

func takeBytes(b []byte) ([]byte, []byte, bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := int(binary.BigEndian.Uint32(b))
	b = b[4:]
	if len(b) < n {
		return nil, nil, false
	}
	out := make([]byte, n)
	copy(out, b[:n])
	return out, b[n:], true
}

func encodeHardState(hs raft.HardState) []byte {
	b := putU64(nil, uint64(hs.Term))
	return putU64(b, uint64(hs.Vote))
}

func decodeHardState(b []byte) raft.HardState {
	t, b, ok := takeU64(b)
	if !ok {
		return raft.HardState{}
	}
	v, _, ok := takeU64(b)
	if !ok {
		return raft.HardState{}
	}
	return raft.HardState{Term: raft.Term(t), Vote: raft.NodeID(v)}
}

func encodeEntry(e raft.Entry) []byte {
	b := []byte{byte(e.Type)}
	b = putU64(b, uint64(e.Term))
	b = putU64(b, uint64(e.Index))
	b = putU64(b, uint64(e.ID.Node))
	b = putU64(b, e.ID.Seq)
	return putBytes(b, e.Data)
}

func decodeEntry(b []byte) (raft.Entry, bool) {
	if len(b) < 1 {
		return raft.Entry{}, false
	}
	et := raft.EntryType(b[0])
	b = b[1:]
	t, b, ok := takeU64(b)
	if !ok {
		return raft.Entry{}, false
	}
	i, b, ok := takeU64(b)
	if !ok {
		return raft.Entry{}, false
	}
	pn, b, ok := takeU64(b)
	if !ok {
		return raft.Entry{}, false
	}
	ps, b, ok := takeU64(b)
	if !ok {
		return raft.Entry{}, false
	}
	d, _, ok := takeBytes(b)
	if !ok {
		return raft.Entry{}, false
	}
	return raft.Entry{
		Type: et, Term: raft.Term(t), Index: raft.Index(i),
		ID:   raft.ProposalID{Node: raft.NodeID(pn), Seq: ps},
		Data: d,
	}, true
}

// encodeSnapshot stores the snapshot metadata beside its bytes, because a
// snapshot without its index and term is a state machine nobody can place in a
// log.
// # The range descriptor travels with the snapshot, for A3's reason one layer out
//
// A2 learned that a snapshot must carry its configuration, because the
// configuration is a function of the log and a snapshot is the part of the log
// that no longer exists. A range's EXTENT is the same kind of fact: it is a
// function of the split entries in the log, and a snapshot compacts them away.
//
// Written separately, a descriptor could be stale relative to a snapshot that
// already covers the split which changed it -- and a node recovering that pair
// would serve a range whose extent it can no longer re-derive. Two replicas of
// one range would then disagree about which keys they own, which is what
// snapshot equivalence caught: two nodes recording different digests for the
// same index.
func encodeSnapshot(meta raft.SnapshotMeta, data []byte) []byte {
	b := putU64(nil, uint64(meta.Index))
	b = putU64(b, uint64(meta.Term))
	b = putBytes(b, raft.EncodeConfiguration(meta.Conf))
	return putBytes(b, data)
}

func decodeSnapshot(b []byte) (raft.SnapshotMeta, []byte, bool) {
	idx, b, ok := takeU64(b)
	if !ok {
		return raft.SnapshotMeta{}, nil, false
	}
	term, b, ok := takeU64(b)
	if !ok {
		return raft.SnapshotMeta{}, nil, false
	}
	confRaw, b, ok := takeBytes(b)
	if !ok {
		return raft.SnapshotMeta{}, nil, false
	}
	conf, ok := raft.DecodeConfiguration(confRaw)
	if !ok {
		return raft.SnapshotMeta{}, nil, false
	}
	data, _, ok := takeBytes(b)
	if !ok {
		return raft.SnapshotMeta{}, nil, false
	}
	return raft.SnapshotMeta{Index: raft.Index(idx), Term: raft.Term(term), Conf: conf}, data, true
}

// encodeMachine serialises the WHOLE state machine: the extent, the
// garbage-collection mark, and every version.
//
// # Why the extent is in here and not beside it
//
// It rode beside it for most of A4, first as a separately written descriptor key
// and then as a field of the snapshot record. Both are the same mistake in
// different clothing: they make the extent something the storage layer keeps
// ABOUT the state machine rather than something the state machine IS.
//
// The extent is applied state. A split entry moves it, exactly as a put moves a
// key, and a split applies only against the extent it names -- so a replica that
// adopts a state machine without its extent has adopted half of one. That is not
// hypothetical: a follower that installed a snapshot kept its own stale extent,
// refused the next split entry for naming an extent it could not see, and
// diverged from every replica that applied it (BUG-013).
//
// # A5 adds the GC mark for exactly the same reason
//
// The mark decides whether a read is answerable, it advances by an applied
// command, and it is therefore applied state too. A snapshot that carried
// versions without the mark would hand a follower a state machine whose history
// has been collected and whose record of that collection has not -- so it would
// answer reads below the mark from a history that is no longer there, which is
// the silently-wrong read the mark exists to prevent.
func encodeMachine(desc RangeDescriptor, mark hlc.Timestamp, rs []kv.Record) []byte {
	b := putBytes(nil, encodeDesc(desc))
	b = putU64(b, uint64(mark.Wall))
	b = putU64(b, uint64(mark.Logical))
	b = putU64(b, uint64(len(rs)))
	for _, r := range rs {
		b = putBytes(b, r.Key)
		b = putBytes(b, r.Value)
	}
	return b
}

func decodeMachine(b []byte) (RangeDescriptor, hlc.Timestamp, []kv.Record, bool) {
	var zero RangeDescriptor
	descRaw, b, ok := takeBytes(b)
	if !ok {
		return zero, hlc.Timestamp{}, nil, false
	}
	desc, ok := decodeDesc(descRaw)
	if !ok {
		return zero, hlc.Timestamp{}, nil, false
	}
	mw, b, ok := takeU64(b)
	if !ok {
		return zero, hlc.Timestamp{}, nil, false
	}
	ml, b, ok := takeU64(b)
	if !ok {
		return zero, hlc.Timestamp{}, nil, false
	}
	mark := hlc.Timestamp{Wall: clock.NewWall(int64(mw)), Logical: uint32(ml)}
	n, b, ok := takeU64(b)
	if !ok {
		return zero, hlc.Timestamp{}, nil, false
	}
	rs := make([]kv.Record, 0, n)
	for range n {
		var k, v []byte
		k, b, ok = takeBytes(b)
		if !ok {
			return zero, hlc.Timestamp{}, nil, false
		}
		v, b, ok = takeBytes(b)
		if !ok {
			return zero, hlc.Timestamp{}, nil, false
		}
		rs = append(rs, kv.Record{Key: k, Value: v})
	}
	return desc, mark, rs, true
}

// The map state machine's codec is GONE, and the note is worth keeping.
//
// `encodeKV`/`decodeKV` serialised the state machine when the state machine was
// a Go map. Both halves lost their callers at `e8b258c` -- *"A5: MVCC is the
// replicated state machine"* -- and neither has been called since, by anything,
// including tests. DESIGN-A6 §25.1's third meaning: the response to code that
// cannot be reached is to delete it.
//
// Its key ordering was load-bearing rather than tidy -- a snapshot's bytes are
// compared against an independently computed expectation, so ranging a map here
// would make one state produce different bytes on different runs -- and the
// requirement survives it without needing the code: this package is in core
// determinism scope and the vet pass forbids ranging a map anywhere in it, and
// `encodeMachine` above walks an ordered SLICE of records, which is the same
// property obtained by not having a map in the first place.
//
// It was `store/`'s only use of `internal/sorted`, and that import goes with it.

// DecodeMachine decodes a state machine for the harness's model: the extent, the
// garbage-collection mark, and every version.
//
// It reports false rather than returning an empty state on undecodable input.
// The model cannot judge a range whose starting extent it does not know, and
// quietly substituting a zero one is how a checker comes out green on a state
// nobody ever had.
func DecodeMachine(b []byte) (RangeDescriptor, hlc.Timestamp, []kv.Record, bool) {
	return decodeMachine(b)
}

// DecodeCommand exposes the command wire format to the harness, which builds an
// independent model of the state machine to check snapshots against.
//
// Sharing the FORMAT is deliberate and sharing the SEMANTICS is not: the harness
// re-implements what a put and a get do rather than calling into this package,
// so a defect in applying commands cannot be cancelled out by the same defect on
// both sides of the comparison.
func DecodeCommand(data []byte) (op, key, value string, at hlc.Timestamp) { return decodeCmd(data) }

func encodeMessage(m raft.Message) []byte {
	b := []byte{byte(m.Type)}
	b = putU64(b, uint64(m.From))
	b = putU64(b, uint64(m.To))
	b = putU64(b, uint64(m.Term))
	b = putU64(b, uint64(m.LastLogIndex))
	b = putU64(b, uint64(m.LastLogTerm))
	b = putU64(b, uint64(m.PrevLogIndex))
	b = putU64(b, uint64(m.PrevLogTerm))
	b = putU64(b, uint64(m.LeaderCommit))
	b = putU64(b, uint64(m.MatchIndex))
	b = putU64(b, uint64(m.Hint))
	b = putU64(b, uint64(m.SnapIndex))
	b = putU64(b, uint64(m.SnapTerm))
	b = putBytes(b, m.SnapData)
	b = putBytes(b, m.SnapConf)
	flags := byte(0)
	if m.Granted {
		flags |= 1
	}
	if m.Success {
		flags |= 2
	}
	b = append(b, flags)
	b = binary.BigEndian.AppendUint32(b, uint32(len(m.Entries)))
	for _, e := range m.Entries {
		b = putBytes(b, encodeEntry(e))
	}
	return b
}

func decodeMessage(b []byte) (raft.Message, bool) {
	if len(b) < 1 {
		return raft.Message{}, false
	}
	var m raft.Message
	m.Type = raft.MessageType(b[0])
	b = b[1:]
	var vals [12]uint64
	for i := range vals {
		v, rest, ok := takeU64(b)
		if !ok {
			return raft.Message{}, false
		}
		vals[i], b = v, rest
	}
	m.From = raft.NodeID(vals[0])
	m.To = raft.NodeID(vals[1])
	m.Term = raft.Term(vals[2])
	m.LastLogIndex = raft.Index(vals[3])
	m.LastLogTerm = raft.Term(vals[4])
	m.PrevLogIndex = raft.Index(vals[5])
	m.PrevLogTerm = raft.Term(vals[6])
	m.LeaderCommit = raft.Index(vals[7])
	m.MatchIndex = raft.Index(vals[8])
	m.Hint = raft.Index(vals[9])
	m.SnapIndex = raft.Index(vals[10])
	m.SnapTerm = raft.Term(vals[11])
	sd, rest, ok := takeBytes(b)
	if !ok {
		return raft.Message{}, false
	}
	if len(sd) > 0 {
		m.SnapData = sd
	}
	b = rest
	sc, rest, ok := takeBytes(b)
	if !ok {
		return raft.Message{}, false
	}
	if len(sc) > 0 {
		m.SnapConf = sc
	}
	b = rest
	if len(b) < 1 {
		return raft.Message{}, false
	}
	m.Granted = b[0]&1 != 0
	m.Success = b[0]&2 != 0
	b = b[1:]
	if len(b) < 4 {
		return raft.Message{}, false
	}
	n := int(binary.BigEndian.Uint32(b))
	b = b[4:]
	for range n {
		raw, rest, ok := takeBytes(b)
		if !ok {
			return raft.Message{}, false
		}
		e, ok := decodeEntry(raw)
		if !ok {
			return raft.Message{}, false
		}
		m.Entries = append(m.Entries, e)
		b = rest
	}
	return m, true
}

// # A command carries its TIMESTAMP, and the leader stamps it at propose time
//
// The alternative is each replica stamping when it applies, and that is not a
// near miss -- it is a guaranteed divergence. Two replicas apply the same entry
// at different wall times, so they would write the same value at two different
// timestamps and every subsequent read would see different history depending on
// which replica served it.
//
// So the leader reads its HLC once, at propose, and the timestamp travels in the
// entry. Every replica then applies a fact derived at a position, which is A4's
// class in A5's dimension (DESIGN-A5 section 7) and the same reasoning that put
// the split key in the split entry rather than re-deriving it per replica.
// opGC is the collection command's opcode. It is a command like put and get so
// that it travels the log and applies at a position, which is the only way every
// replica can agree on what is still answerable.
const opGC = "gc"

// OpGC exposes the collection opcode to the harness, which restates what the
// command does rather than calling into this package.
const OpGC = opGC

func encodeCmd(op, key, value string, at hlc.Timestamp) []byte {
	b := putBytes(nil, []byte(op))
	b = putBytes(b, []byte(key))
	b = putBytes(b, []byte(value))
	b = putU64(b, uint64(at.Wall))
	return putU64(b, uint64(at.Logical))
}

func decodeCmd(b []byte) (string, string, string, hlc.Timestamp) {
	o, b, ok := takeBytes(b)
	if !ok {
		return "", "", "", hlc.Timestamp{}
	}
	k, b, ok := takeBytes(b)
	if !ok {
		return "", "", "", hlc.Timestamp{}
	}
	v, b, ok := takeBytes(b)
	if !ok {
		return "", "", "", hlc.Timestamp{}
	}
	w, b, ok := takeU64(b)
	if !ok {
		return "", "", "", hlc.Timestamp{}
	}
	l, _, ok := takeU64(b)
	if !ok {
		return "", "", "", hlc.Timestamp{}
	}
	return string(o), string(k), string(v), hlc.Timestamp{Wall: clock.NewWall(int64(w)), Logical: uint32(l)}
}

// encodeReadCtx builds the identifier a read-index request is matched by.
//
// Raft carries it opaquely and hands it back on the ReadState; the driver uses
// it to find the request that asked. It is (node, sequence) for the same reason
// a ProposalID is: matching an answer to a request on anything positional --
// arrival order, an index -- is BUG-004's mistake, and a read index is a
// position, so the temptation is real.
func encodeReadCtx(node raft.NodeID, seq uint64) []byte {
	b := make([]byte, 0, 16)
	b = putU64(b, uint64(node))
	return putU64(b, seq)
}
