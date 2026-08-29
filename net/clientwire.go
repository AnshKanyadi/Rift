package net

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The client wire format: a request and a response, carried in an Envelope's
// Body.
//
// # It gets the same treatment every frozen format in this repository got
//
// This is a wire this project has never sent operations over, so the shape is
// decided here and its refusals are induced from HAND-BUILT BYTES before any
// writer exists. That discipline paid four times in Track B, most recently on
// BM67's single character.
//
//	A DECODER TESTED ONLY AGAINST ITS OWN ENCODER IS TESTED AGAINST ONE INPUT:
//	the one the encoder happens to produce. Every byte sequence it will actually
//	meet in production is one the encoder did not make.
//
// So every refusal below is exercised from a literal byte slice, not from a
// round trip.
//
// # The shape
//
//	request   op(1) seq(8) klen(4) key vlen(4) value
//	response  seq(8) status(1) vlen(4) value
//
// Fixed-width big-endian, explicit lengths, no reflection and no varint -- the
// same rules sim/codec.go states, for the same reason: the encoding is a
// property of the definition rather than of anything discovered at run time.
// KindRequest and KindResponse are the Envelope kinds that carry client
// traffic. They live HERE rather than in either process, because they are part
// of the agreement between the two: one transport carries Raft messages and
// client operations on the same socket, and a request that arrives looking like
// a heartbeat is a bug that presents as silence. Two copies of a wire constant
// is one copy away from a protocol split.
const (
	KindRequest  uint16 = 40
	KindResponse uint16 = 41
)

const (
	// OpGet and OpPut are the request opcodes. They start at 1 so a zero byte
	// -- the most common corruption and the most common uninitialised value --
	// is never a valid operation.
	OpGet byte = 1
	OpPut byte = 2

	// StatusOK and the rest are response statuses, also starting at 1.
	StatusOK       byte = 1
	StatusNotFound byte = 2
	StatusError    byte = 3
)

var (
	ErrShortRequest  = errors.New("net: request truncated")
	ErrShortResponse = errors.New("net: response truncated")
	ErrBadOp         = errors.New("net: unknown opcode")
	ErrBadStatus     = errors.New("net: unknown status")
	ErrTrailing      = errors.New("net: trailing bytes")
	ErrFieldTooLarge = errors.New("net: field exceeds the wire limit")
)

// maxFieldBytes bounds a key or value on the wire.
//
// This is a POLICY limit, and saying so precisely matters, because the obvious
// thing to call it -- an allocation guard, the way ReadFrame's maxBodyBytes is
// one -- would be false here and the falseness would be invisible.
//
//	ReadFrame reads from an io.Reader: it must bound the claimed length before
//	allocating, because nothing has been read yet and the number is
//	attacker-controlled. These decoders take a []byte that is ALREADY IN MEMORY.
//	A length claimed here can never cause an allocation larger than the frame the
//	length arrived in; the bounds check against len(b) does that work by itself.
//
// The first version of this file called it an allocation guard and had a test
// named for it. Deleting the guard broke NOTHING -- the test passed on the
// bounds check underneath, with the same error, for a different reason. BUGS.md
// GF-56. What the limit actually does is refuse a field that IS present and IS
// within a legal frame: maxBodyBytes is 4MiB, so a 2MiB key arrives intact and
// is rejected here on size. That is the case the induction has to build.
const maxFieldBytes = 1 << 20

// Request is a client operation.
type Request struct {
	Op    byte
	Seq   uint64
	Key   string
	Value string
}

// Response is what a node answered.
type Response struct {
	Seq    uint64
	Status byte
	Value  string
}

// EncodeRequest renders r.
func EncodeRequest(r Request) ([]byte, error) {
	if r.Op != OpGet && r.Op != OpPut {
		return nil, fmt.Errorf("%w: %d", ErrBadOp, r.Op)
	}
	if len(r.Key) > maxFieldBytes || len(r.Value) > maxFieldBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrFieldTooLarge, maxFieldBytes)
	}
	b := make([]byte, 0, 17+len(r.Key)+len(r.Value))
	b = append(b, r.Op)
	b = binary.BigEndian.AppendUint64(b, r.Seq)
	b = binary.BigEndian.AppendUint32(b, uint32(len(r.Key)))
	b = append(b, r.Key...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(r.Value)))
	b = append(b, r.Value...)
	return b, nil
}

// DecodeRequest parses one request, refusing everything malformed.
func DecodeRequest(b []byte) (Request, error) {
	var r Request
	if len(b) < 17 {
		return r, fmt.Errorf("%w: %d bytes, need at least 17", ErrShortRequest, len(b))
	}
	r.Op = b[0]
	if r.Op != OpGet && r.Op != OpPut {
		return r, fmt.Errorf("%w: %d", ErrBadOp, r.Op)
	}
	r.Seq = binary.BigEndian.Uint64(b[1:])
	kl := binary.BigEndian.Uint32(b[9:])
	if kl > maxFieldBytes {
		return r, fmt.Errorf("%w: key claims %d bytes", ErrFieldTooLarge, kl)
	}
	if uint64(len(b)) < 13+uint64(kl)+4 {
		return r, fmt.Errorf("%w: key claims %d bytes, frame has %d", ErrShortRequest, kl, len(b)-13)
	}
	r.Key = string(b[13 : 13+kl])
	off := 13 + uint64(kl)
	vl := binary.BigEndian.Uint32(b[off:])
	if vl > maxFieldBytes {
		return r, fmt.Errorf("%w: value claims %d bytes", ErrFieldTooLarge, vl)
	}
	off += 4
	if uint64(len(b)) < off+uint64(vl) {
		return r, fmt.Errorf("%w: value claims %d bytes, frame has %d", ErrShortRequest, vl, uint64(len(b))-off)
	}
	r.Value = string(b[off : off+uint64(vl)])
	if uint64(len(b)) > off+uint64(vl) {
		// Trailing bytes mean the framing is wrong upstream. Ignoring them is
		// how a length bug survives until it corrupts something that matters --
		// sim/codec.go's words, and the same judgement.
		return r, fmt.Errorf("%w: %d extra", ErrTrailing, uint64(len(b))-off-uint64(vl))
	}
	return r, nil
}

// EncodeResponse renders resp.
func EncodeResponse(resp Response) ([]byte, error) {
	if resp.Status != StatusOK && resp.Status != StatusNotFound && resp.Status != StatusError {
		return nil, fmt.Errorf("%w: %d", ErrBadStatus, resp.Status)
	}
	if len(resp.Value) > maxFieldBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrFieldTooLarge, maxFieldBytes)
	}
	b := make([]byte, 0, 13+len(resp.Value))
	b = binary.BigEndian.AppendUint64(b, resp.Seq)
	b = append(b, resp.Status)
	b = binary.BigEndian.AppendUint32(b, uint32(len(resp.Value)))
	b = append(b, resp.Value...)
	return b, nil
}

// DecodeResponse parses one response.
func DecodeResponse(b []byte) (Response, error) {
	var r Response
	if len(b) < 13 {
		return r, fmt.Errorf("%w: %d bytes, need at least 13", ErrShortResponse, len(b))
	}
	r.Seq = binary.BigEndian.Uint64(b)
	r.Status = b[8]
	if r.Status != StatusOK && r.Status != StatusNotFound && r.Status != StatusError {
		return r, fmt.Errorf("%w: %d", ErrBadStatus, r.Status)
	}
	vl := binary.BigEndian.Uint32(b[9:])
	if vl > maxFieldBytes {
		return r, fmt.Errorf("%w: value claims %d bytes", ErrFieldTooLarge, vl)
	}
	if uint64(len(b)) < 13+uint64(vl) {
		return r, fmt.Errorf("%w: value claims %d bytes, frame has %d", ErrShortResponse, vl, len(b)-13)
	}
	r.Value = string(b[13 : 13+uint64(vl)])
	if uint64(len(b)) > 13+uint64(vl) {
		return r, fmt.Errorf("%w: %d extra", ErrTrailing, uint64(len(b))-13-uint64(vl))
	}
	return r, nil
}
