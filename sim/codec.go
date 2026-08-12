package sim

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The wire codec.
//
// Simulated messages cross it by default, and the reason is fidelity rather
// than purity (DESIGN-A0 D6, DR-9). Nodes in a simulation share an address
// space, so a message passed by reference lets the sender mutate what the
// receiver reads -- a bug class that cannot exist in production and a
// determinism leak that can. Encoding removes it.
//
// The second reason is the one that pays off later: every message in every soak
// run goes through the production encoder, so truncation, a field dropped after
// a schema change, and unbounded sizes are caught by the corpus rather than
// discovered at I2. It also makes message size observable, which the delay
// model wants and a future bandwidth model will need.
//
// If profiling ever shows this dominating hunt throughput, the fast path is an
// explicitly-labelled reduced-fidelity mode -- and it must still deep-copy.
// Nothing ever shares message memory across nodes.

// wireVersion prefixes every frame. A version byte costs one byte per message
// and buys the ability to tell "this is an old frame" from "this is corrupt",
// which is the difference between a clear error and an afternoon.
const wireVersion uint8 = 1

// maxBodyBytes bounds a decoded body. An unbounded length field is how a codec
// turns a corrupt frame into an allocation the size of the length field.
const maxBodyBytes = 1 << 24 // 16 MiB

var (
	ErrShortFrame   = errors.New("sim: frame is shorter than its header claims")
	ErrWireVersion  = errors.New("sim: unknown wire version")
	ErrBodyTooLarge = errors.New("sim: body length exceeds the maximum")
	ErrTrailingData = errors.New("sim: frame has trailing bytes")
)

// Envelope is one message in flight.
//
// Body is opaque to the transport: the protocol above it encodes its own
// message and hands over bytes. That keeps the transport free of any dependency
// on raft or kv, which is what lets A0.7 land before either exists.
type Envelope struct {
	From    NodeID
	To      NodeID
	RangeID uint64
	Kind    uint16
	Body    []byte
}

// Size is the encoded size in bytes, which the delay model reads.
func (e Envelope) Size() int { return frameHeaderBytes + len(e.Body) }

const frameHeaderBytes = 1 + 4 + 4 + 8 + 2 + 4 // version, from, to, range, kind, body length

// Encode writes e as a frame.
//
// Fixed-width big-endian fields, in a fixed order, with an explicit length. No
// reflection, no map iteration, no varint: the encoding is a property of the
// struct definition rather than of anything discovered at run time, so the same
// envelope encodes to the same bytes on every machine and in every Go release.
func Encode(e Envelope) ([]byte, error) {
	if len(e.Body) > maxBodyBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, len(e.Body))
	}
	buf := make([]byte, frameHeaderBytes+len(e.Body))
	buf[0] = wireVersion
	binary.BigEndian.PutUint32(buf[1:], uint32(e.From))
	binary.BigEndian.PutUint32(buf[5:], uint32(e.To))
	binary.BigEndian.PutUint64(buf[9:], e.RangeID)
	binary.BigEndian.PutUint16(buf[17:], e.Kind)
	binary.BigEndian.PutUint32(buf[19:], uint32(len(e.Body)))
	copy(buf[frameHeaderBytes:], e.Body)
	return buf, nil
}

// Decode reads a frame into a fresh Envelope. The returned Body is a copy, so
// the decoded message shares nothing with the buffer it came from.
func Decode(buf []byte) (Envelope, error) {
	if len(buf) < frameHeaderBytes {
		return Envelope{}, fmt.Errorf("%w: %d bytes, need at least %d", ErrShortFrame, len(buf), frameHeaderBytes)
	}
	if buf[0] != wireVersion {
		return Envelope{}, fmt.Errorf("%w: %d", ErrWireVersion, buf[0])
	}

	n := binary.BigEndian.Uint32(buf[19:])
	if n > maxBodyBytes {
		return Envelope{}, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, n)
	}
	if uint64(len(buf)) < uint64(frameHeaderBytes)+uint64(n) {
		return Envelope{}, fmt.Errorf("%w: body claims %d bytes, frame has %d", ErrShortFrame, n, len(buf)-frameHeaderBytes)
	}
	if uint64(len(buf)) > uint64(frameHeaderBytes)+uint64(n) {
		// Trailing bytes mean the framing is wrong somewhere upstream. Silently
		// ignoring them is how a length bug survives until it corrupts
		// something that matters.
		return Envelope{}, fmt.Errorf("%w: %d extra", ErrTrailingData, uint64(len(buf))-uint64(frameHeaderBytes)-uint64(n))
	}

	e := Envelope{
		From:    NodeID(binary.BigEndian.Uint32(buf[1:])),
		To:      NodeID(binary.BigEndian.Uint32(buf[5:])),
		RangeID: binary.BigEndian.Uint64(buf[9:]),
		Kind:    binary.BigEndian.Uint16(buf[17:]),
	}
	if n > 0 {
		e.Body = make([]byte, n)
		copy(e.Body, buf[frameHeaderBytes:])
	}
	return e, nil
}
