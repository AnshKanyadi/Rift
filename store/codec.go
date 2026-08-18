package store

import (
	"encoding/binary"

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
	b := putU64(nil, uint64(e.Term))
	b = putU64(b, uint64(e.Index))
	return putBytes(b, e.Data)
}

func decodeEntry(b []byte) (raft.Entry, bool) {
	t, b, ok := takeU64(b)
	if !ok {
		return raft.Entry{}, false
	}
	i, b, ok := takeU64(b)
	if !ok {
		return raft.Entry{}, false
	}
	d, _, ok := takeBytes(b)
	if !ok {
		return raft.Entry{}, false
	}
	return raft.Entry{Term: raft.Term(t), Index: raft.Index(i), Data: d}, true
}

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
	var vals [10]uint64
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

func encodeCmd(op, key, value string) []byte {
	b := putBytes(nil, []byte(op))
	b = putBytes(b, []byte(key))
	return putBytes(b, []byte(value))
}

func decodeCmd(b []byte) (string, string, string) {
	o, b, ok := takeBytes(b)
	if !ok {
		return "", "", ""
	}
	k, b, ok := takeBytes(b)
	if !ok {
		return "", "", ""
	}
	v, _, ok := takeBytes(b)
	if !ok {
		return "", "", ""
	}
	return string(o), string(k), string(v)
}
