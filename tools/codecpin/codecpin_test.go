package codecpin_test

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/raft"
)

// Package codecpin_test pins the transport codec against raft.Message.
//
// # Why it lives here and not in store/
//
// It reads the source text, and store/ is in the determinism pass's core scope
// where importing os is a violation: core packages reach the outside world only
// through injected interfaces. Reading a file to check a contract is tooling,
// and tooling lives here.
//
// That is not a new argument. It is the one tools/gatepin's own header makes,
// word for word, about why the durability-gate pin is not in raft/ -- and this
// test was written into store/ anyway, which left the determinism lane red on
// the tree from the moment BUG-025's fix landed until it was noticed here. The
// SEMANTIC half of that test needs no source text and stays where it was
// (store/codec_readindex_test.go, TestMessageCodecCarriesEveryField); only the
// structural half moved.

// TestEveryMessageFieldIsCarried is the STRUCTURAL half of BUG-025's fix, and it
// exists because a round trip only covers the fields somebody thought to write
// into it.
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
	src, err := os.ReadFile("../../store/codec.go")
	if err != nil {
		t.Fatalf("read codec.go: %v", err)
	}
	text := string(src)
	i := strings.Index(text, "func encodeMessage")
	if i < 0 {
		t.Fatal("could not find encodeMessage in store/codec.go")
	}
	j := strings.Index(text[i:], "\n}\n")
	if j < 0 {
		t.Fatal("could not find the end of encodeMessage")
	}
	body := text[i : i+j]

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
