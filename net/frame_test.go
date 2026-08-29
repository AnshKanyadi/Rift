package net_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	riftnet "github.com/anshkanyadi/rift/net"
	"github.com/anshkanyadi/rift/sim"
)

// The stream layer's tests. This package inherits no confidence from a
// dependency, so every way a stream can lie to a reader is a case here.

func TestTheHeaderSizeMatchesTheCodec(t *testing.T) {
	// The constant is duplicated on purpose -- sim is core scope, this is
	// orchestration, and importing a constant across that boundary would point
	// the dependency the wrong way. BUG-032's one-fact-two-places shape,
	// answered with a check rather than with care.
	e := sim.Envelope{From: 1, To: 2, Kind: 7}
	buf, err := sim.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(buf); got != 23 {
		t.Fatalf("an empty-bodied frame encodes to %d bytes; this package's frameHeaderBytes says 23. "+
			"One of the two moved and the other did not", got)
	}
}

func TestAWholeFrameRoundTrips(t *testing.T) {
	want := sim.Envelope{From: 3, To: 9, RangeID: 42, Kind: 5, Body: []byte("hello")}
	var buf bytes.Buffer
	if err := riftnet.WriteFrame(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := riftnet.ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.From != want.From || got.To != want.To || got.RangeID != want.RangeID ||
		got.Kind != want.Kind || string(got.Body) != string(want.Body) {
		t.Errorf("round trip lost something:\n  want %+v\n  got  %+v", want, got)
	}
}

// TestAFrameSplitAcrossReadsIsNotAShortFrame is the case that separates a
// stream reader from a buffer reader.
//
// TCP does not deliver messages, it delivers bytes. A reader that calls Read
// once and decodes what it got will work on a loopback and fail under load,
// which is the worst possible failure schedule: it passes every test on the
// developer's machine and breaks when the network is busy.
func TestAFrameSplitAcrossReadsIsNotAShortFrame(t *testing.T) {
	want := sim.Envelope{From: 1, To: 2, Kind: 3, Body: bytes.Repeat([]byte("x"), 300)}
	var whole bytes.Buffer
	if err := riftnet.WriteFrame(&whole, want); err != nil {
		t.Fatal(err)
	}

	// One byte at a time: the most hostile legal split.
	got, err := riftnet.ReadFrame(&iotest{data: whole.Bytes(), chunk: 1})
	if err != nil {
		t.Fatalf("a frame delivered one byte per Read failed: %v", err)
	}
	if string(got.Body) != string(want.Body) {
		t.Errorf("body corrupted across reads")
	}

	// And a split landing INSIDE the header, which is the subtler half: a
	// reader that handles a split body but not a split header works until a
	// segment boundary happens to land in the first 23 bytes.
	got, err = riftnet.ReadFrame(&iotest{data: whole.Bytes(), chunk: 7})
	if err != nil {
		t.Fatalf("a frame split inside the header failed: %v", err)
	}
	if string(got.Body) != string(want.Body) {
		t.Errorf("body corrupted with a header-straddling split")
	}
}

// TestAnOversizedLengthIsRefusedBeforeAllocating.
//
// A length read off a socket is an attacker-controlled number even when there
// is no attacker: a mis-framed stream produces the same effect as a malicious
// one. "Allocate what the peer asked for" is how a framing bug becomes an
// out-of-memory.
func TestAnOversizedLengthIsRefusedBeforeAllocating(t *testing.T) {
	head := make([]byte, 23)
	head[0] = 1 // wire version
	binary.BigEndian.PutUint32(head[19:], 1<<30)

	_, err := riftnet.ReadFrame(bytes.NewReader(head))
	if !errors.Is(err, riftnet.ErrFrameTooLarge) {
		t.Fatalf("a 1 GiB body claim was not refused as too large: %v", err)
	}
}

// TestACloseMidFrameIsDistinguishedFromACloseAtABoundary.
//
// One is a peer leaving, the other is a peer dying. In a chaos run that
// distinction is the whole signal: kill -9 during a send produces the second,
// and a graceful shutdown produces the first. Collapsing them would erase the
// difference the lane exists to create.
func TestACloseMidFrameIsDistinguishedFromACloseAtABoundary(t *testing.T) {
	var whole bytes.Buffer
	if err := riftnet.WriteFrame(&whole, sim.Envelope{From: 1, To: 2, Body: []byte("payload")}); err != nil {
		t.Fatal(err)
	}
	full := whole.Bytes()

	if _, err := riftnet.ReadFrame(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Errorf("a close at a frame boundary gave %v, want io.EOF", err)
	}
	if _, err := riftnet.ReadFrame(bytes.NewReader(full[:10])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a close inside the HEADER gave %v, want io.ErrUnexpectedEOF", err)
	}
	if _, err := riftnet.ReadFrame(bytes.NewReader(full[:len(full)-2])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a close inside the BODY gave %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestBackToBackFramesOnOneStream(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 5; i++ {
		if err := riftnet.WriteFrame(&buf, sim.Envelope{From: sim.NodeID(i), To: 9, Body: []byte{byte(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		got, err := riftnet.ReadFrame(&buf)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if got.From != sim.NodeID(i) || len(got.Body) != 1 || got.Body[0] != byte(i) {
			t.Errorf("frame %d came back as %+v", i, got)
		}
	}
	if _, err := riftnet.ReadFrame(&buf); !errors.Is(err, io.EOF) {
		t.Errorf("after five frames the stream should be at EOF, got %v", err)
	}
}

// iotest delivers data in fixed-size chunks, so a test can force the splits TCP
// would produce under load rather than hoping for them.
type iotest struct {
	data  []byte
	chunk int
	off   int
}

func (r *iotest) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if r.off+n > len(r.data) {
		n = len(r.data) - r.off
	}
	copy(p, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}
