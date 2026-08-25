package store

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/anshkanyadi/rift/raft"
)

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

// TestEveryMessageFieldIsCarried is the STRUCTURAL half, and it exists because
// the round trip above only covers the fields somebody thought to write into it.
//
// `ReadCtx` and `ReadIndex` were missing from the codec for the length of their
// existence, and the reason no test caught it is that no test enumerated the
// fields -- each one asserted the fields its author had in mind. So this reads
// `raft.Message`'s declaration and requires every exported field to appear in
// `encodeMessage`'s body.
//
// It is crude: a source scan, not a semantic check. It is the honest crude thing
// for the same reason `TestOneApplyPath` counts call sites -- it fails the moment
// somebody adds a field and forgets the wire, which is exactly when it needs to,
// and it covers the types that existed before it did rather than only the ones
// added after.
func TestEveryMessageFieldIsCarried(t *testing.T) {
	src, err := os.ReadFile("codec.go")
	if err != nil {
		t.Fatalf("read codec.go: %v", err)
	}
	i := bytesIndex(string(src), "func encodeMessage")
	j := bytesIndex(string(src)[i:], "\n}\n")
	if i < 0 || j < 0 {
		t.Fatal("could not find encodeMessage")
	}
	body := string(src)[i : i+j]

	var missing []string
	mt := reflect.TypeOf(raft.Message{})
	for f := range mt.NumField() {
		name := mt.Field(f).Name
		if !regexp.MustCompile(`\bm\.` + name + `\b`).MatchString(body) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("raft.Message has %d fields and encodeMessage carries none of these: %v.\n"+
			"A field the codec does not write crosses the transport ZEROED -- the message is "+
			"delivered and the thing it exists to carry is gone, with no error anywhere. That is "+
			"how MsgReadIndex lost its ReadCtx and follower reads were forwarded and never "+
			"answered (BUGS.md, and DESIGN-A7 section 5c)", mt.NumField(), missing)
	}
}

func bytesIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
