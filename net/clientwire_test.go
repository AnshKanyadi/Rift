package net

import (
	"errors"
	"strings"
	"testing"
)

// Every case here is a HAND-BUILT byte slice.
//
// Not one of them comes from EncodeRequest or EncodeResponse. That is the whole
// discipline: a decoder tested against its own encoder is tested against the one
// input the encoder happens to produce, and every byte sequence it will meet on a
// real socket is a sequence the encoder did not make. The refusals are induced
// here, before anything writes these bytes to a wire.

func TestRequestRefusesHandBuiltBytes(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", []byte{}, ErrShortRequest},
		{"header one byte short", make([]byte, 16), ErrShortRequest},
		// A ZERO OPCODE is the most common corruption and the most common
		// uninitialised value. It must never mean an operation.
		{"zero opcode", append([]byte{0}, make([]byte, 16)...), ErrBadOp},
		{"unknown opcode", append([]byte{9}, make([]byte, 16)...), ErrBadOp},
		{
			// klen claims 4, body carries 0.
			name: "key length past the end",
			in: []byte{
				OpPut,
				0, 0, 0, 0, 0, 0, 0, 1, // seq
				0, 0, 0, 4, // klen = 4
				0, 0, 0, 0, // ... but these four bytes are the vlen field
			},
			want: ErrShortRequest,
		},
		{
			// klen honest, vlen claims 8 with nothing behind it.
			name: "value length past the end",
			in: []byte{
				OpGet,
				0, 0, 0, 0, 0, 0, 0, 1,
				0, 0, 0, 1, 'k',
				0, 0, 0, 8,
			},
			want: ErrShortRequest,
		},
		{
			// STATED LIMIT (GF-56). This row names a POLICY limit and
			// DEMONSTRATES A BOUNDS CHECK, and the two are separable only by the
			// sentinel. With the size limit deleted, the bounds check underneath
			// refuses these same bytes -- differently enough that ErrFieldTooLarge
			// vs ErrShortRequest still fails the row, but the row is not
			// evidence that a present oversized field is refused.
			//
			//	IT WILL KEEP PASSING WHEN THE THING IT NAMES IS GONE, if the
			//	sentinel ever converges. TestAnOversizedFieldThatIsActuallyThere-
			//	IsRefused is the row that carries the real claim; it exists
			//	because these cheap rows do not, and it costs a 2MiB slice, which
			//	is why the cheap rows got written first.
			name: "absurd key length",
			in: []byte{
				OpPut,
				0, 0, 0, 0, 0, 0, 0, 1,
				0xFF, 0xFF, 0xFF, 0xFF,
				0, 0, 0, 0,
			},
			want: ErrFieldTooLarge,
		},
		{
			name: "absurd value length",
			in: []byte{
				OpPut,
				0, 0, 0, 0, 0, 0, 0, 1,
				0, 0, 0, 0,
				0xFF, 0xFF, 0xFF, 0xFF,
			},
			want: ErrFieldTooLarge,
		},
		{
			name: "trailing bytes",
			in: []byte{
				OpGet,
				0, 0, 0, 0, 0, 0, 0, 1,
				0, 0, 0, 1, 'k',
				0, 0, 0, 0,
				0xAA,
			},
			want: ErrTrailing,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeRequest(c.in)
			if !errors.Is(err, c.want) {
				t.Fatalf("DecodeRequest(% x) = %v, want %v", c.in, err, c.want)
			}
		})
	}
}

func TestResponseRefusesHandBuiltBytes(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", []byte{}, ErrShortResponse},
		{"header one byte short", make([]byte, 12), ErrShortResponse},
		{"zero status", make([]byte, 13), ErrBadStatus},
		{
			name: "unknown status",
			in: []byte{
				0, 0, 0, 0, 0, 0, 0, 1,
				9,
				0, 0, 0, 0,
			},
			want: ErrBadStatus,
		},
		{
			name: "value length past the end",
			in: []byte{
				0, 0, 0, 0, 0, 0, 0, 1,
				StatusOK,
				0, 0, 0, 4,
				'a', 'b',
			},
			want: ErrShortResponse,
		},
		{
			name: "absurd value length",
			in: []byte{
				0, 0, 0, 0, 0, 0, 0, 1,
				StatusOK,
				0xFF, 0xFF, 0xFF, 0xFF,
			},
			want: ErrFieldTooLarge,
		},
		{
			name: "trailing bytes",
			in: []byte{
				0, 0, 0, 0, 0, 0, 0, 1,
				StatusNotFound,
				0, 0, 0, 0,
				0xAA,
			},
			want: ErrTrailing,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeResponse(c.in)
			if !errors.Is(err, c.want) {
				t.Fatalf("DecodeResponse(% x) = %v, want %v", c.in, err, c.want)
			}
		})
	}
}

// The shape is pinned by hand too, so a change to the layout is a test edit and
// not a silent renegotiation of a format two processes agree on.
func TestTheShapeIsWhatTheDocSays(t *testing.T) {
	got, err := EncodeRequest(Request{Op: OpPut, Seq: 0x0102030405060708, Key: "k", Value: "vv"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		OpPut,
		1, 2, 3, 4, 5, 6, 7, 8,
		0, 0, 0, 1, 'k',
		0, 0, 0, 2, 'v', 'v',
	}
	if string(got) != string(want) {
		t.Fatalf("request bytes:\n got % x\nwant % x", got, want)
	}

	gotR, err := EncodeResponse(Response{Seq: 0x0102030405060708, Status: StatusOK, Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	wantR := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		StatusOK,
		0, 0, 0, 1, 'v',
	}
	if string(gotR) != string(wantR) {
		t.Fatalf("response bytes:\n got % x\nwant % x", gotR, wantR)
	}
}

// An empty value is a value. The differential generator's zero-length hole on
// Track B was exactly this shape of gap, so it is pinned on both sides here.
func TestEmptyValueIsNotAbsentValue(t *testing.T) {
	b, err := EncodeRequest(Request{Op: OpPut, Seq: 1, Key: "k", Value: ""})
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != "" || r.Key != "k" || r.Op != OpPut {
		t.Fatalf("round trip lost an empty value: %+v", r)
	}
	rb, err := EncodeResponse(Response{Seq: 1, Status: StatusOK, Value: ""})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DecodeResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != StatusOK || resp.Value != "" {
		t.Fatalf("round trip lost an empty value: %+v", resp)
	}
}

// The encoder refuses what the decoder refuses. A format with a writer that can
// emit something its own reader rejects has two specifications.
func TestTheEncoderRefusesWhatTheDecoderWouldReject(t *testing.T) {
	if _, err := EncodeRequest(Request{Op: 0, Seq: 1}); !errors.Is(err, ErrBadOp) {
		t.Fatalf("encoder accepted opcode 0: %v", err)
	}
	if _, err := EncodeResponse(Response{Seq: 1, Status: 0}); !errors.Is(err, ErrBadStatus) {
		t.Fatalf("encoder accepted status 0: %v", err)
	}
	big := strings.Repeat("x", maxFieldBytes+1)
	if _, err := EncodeRequest(Request{Op: OpPut, Seq: 1, Key: big}); err == nil {
		t.Fatal("encoder accepted an oversized key")
	}
	if _, err := EncodeResponse(Response{Seq: 1, Status: StatusOK, Value: big}); err == nil {
		t.Fatal("encoder accepted an oversized value")
	}
}

// GF-56: the limit's ONE unambiguous case, built by hand at full size.
//
// The absurd-length rows above cannot prove the limit exists on their own: with
// the limit deleted, the bounds check underneath refuses the same bytes. Only a
// field that is genuinely PRESENT, inside a frame ReadFrame would carry, and
// over the limit, separates the two. maxBodyBytes is 4MiB, so this request is a
// legal frame and an illegal request.
func TestAnOversizedFieldThatIsActuallyThereIsRefused(t *testing.T) {
	key := strings.Repeat("k", maxFieldBytes+1)
	b := make([]byte, 0, 17+len(key))
	b = append(b, OpPut)
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 1)
	b = append(b, byte(len(key)>>24), byte(len(key)>>16), byte(len(key)>>8), byte(len(key)))
	b = append(b, key...)
	b = append(b, 0, 0, 0, 0)
	if len(b) > maxBodyBytes {
		t.Fatalf("the induction is not a legal frame: %d > %d", len(b), maxBodyBytes)
	}
	if _, err := DecodeRequest(b); !errors.Is(err, ErrFieldTooLarge) {
		t.Fatalf("a present %d-byte key was not refused on size: %v", len(key), err)
	}

	val := strings.Repeat("v", maxFieldBytes+1)
	rb := make([]byte, 0, 13+len(val))
	rb = append(rb, 0, 0, 0, 0, 0, 0, 0, 1)
	rb = append(rb, StatusOK)
	rb = append(rb, byte(len(val)>>24), byte(len(val)>>16), byte(len(val)>>8), byte(len(val)))
	rb = append(rb, val...)
	if _, err := DecodeResponse(rb); !errors.Is(err, ErrFieldTooLarge) {
		t.Fatalf("a present %d-byte value was not refused on size: %v", len(val), err)
	}
}
