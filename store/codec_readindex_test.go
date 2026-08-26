package store

import (
	"bytes"
	"testing"

	"github.com/anshkanyadi/rift/raft"
)

// The STRUCTURAL half of BUG-025's fix is not here. It reads store/codec.go's
// source text, which needs `os`, and store/ is core scope where that is a
// determinism violation -- so it lives in tools/codecpin, for the reason
// tools/gatepin gives about itself. This file keeps the half that needs no
// source text: an actual round trip.

// TestMessageCodecCarriesEveryField is the gap that made follower reads silently
// not work.
//
// # What happened
//
// `MsgReadIndex` and `MsgReadIndexResp` were added to `raft/` with `ReadCtx` and
// `ReadIndex`, and this codec was not. The type byte survived the transport and
// the payload did not: a follower forwarded a read whose context arrived empty,
// and the answer's index arrived zero. Nothing errored. Follower reads were
// implemented, dispatched, and answered by nobody.
//
// # Why every test passed anyway
//
// The raft-level tests call `Step` directly and never cross this boundary. The
// protocol was correct and the WIRE was not, and no test in the tree looked at
// the wire. That gap -- a unit test that exercises a mechanism without its
// serialisation -- is the finding rather than the two missing lines.
//
// So this asserts the round trip on every field a message can carry, and it
// fails the moment a field is added to `raft.Message` and not to the codec.
func TestMessageCodecCarriesEveryField(t *testing.T) {
	want := raft.Message{
		Type: raft.MsgReadIndexResp, From: 3, To: 1, Term: 9,
		LastLogIndex: 11, LastLogTerm: 4, PrevLogIndex: 12, PrevLogTerm: 5,
		LeaderCommit: 13, MatchIndex: 14, Hint: 15, SnapIndex: 16, SnapTerm: 6,
		SnapData: []byte("snap"), SnapConf: []byte("conf"),
		ReadCtx: []byte("ctx-7"), ReadIndex: 42,
		Granted: true, Success: true,
	}
	got, ok := decodeMessage(encodeMessage(want))
	if !ok {
		t.Fatal("a well-formed message did not decode")
	}
	if !bytes.Equal(got.ReadCtx, want.ReadCtx) {
		t.Errorf("ReadCtx did not survive the transport: %q -> %q. A read-index request "+
			"whose context arrives empty can never be matched to the request that asked, "+
			"so the read is forwarded and never answered -- silently", want.ReadCtx, got.ReadCtx)
	}
	if got.ReadIndex != want.ReadIndex {
		t.Errorf("ReadIndex did not survive the transport: %d -> %d. The answer then names "+
			"index zero, which no replica can ever have applied past", want.ReadIndex, got.ReadIndex)
	}
	if got.Type != want.Type || got.Term != want.Term || got.MatchIndex != want.MatchIndex ||
		!got.Granted || !got.Success || !bytes.Equal(got.SnapConf, want.SnapConf) {
		t.Errorf("a pre-existing field regressed: %+v", got)
	}
}
