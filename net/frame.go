// Package net is I2's real-mode transport: length-prefixed frames over TCP.
//
// # Why this and not gRPC, which was pre-approved
//
// Ansh at I2, narrowing a pre-approval rather than refusing one:
// sim.Transport's contract is `Send(Envelope)` with NO ERROR RETURN, and that
// is deliberate since A0.7 --
//
//	"an error signal is a covert failure detector, and covert failure detectors
//	are how consensus implementations accidentally become unsafe"
//
// gRPC's natural shape is a unary call that returns something. Adapting a
// request-response transport to a fire-and-forget contract means discarding
// that error at the seam, and a seam like that is either correct-and-subtle or
// a hole.
//
//	CORRECT-AND-SUBTLE IS A THING YOU LATER DISCOVER WAS NEITHER.
//
// # The cost, stated rather than hidden
//
// Hand-rolled framing is code we own and must verify, against a library that is
// already verified and shaped wrong.
//
//	WE ARE CHOOSING THE CODE WE CAN CHECK OVER THE CODE THAT IS ALREADY CHECKED.
//
// # ITS PURITY IS DELIBERATE, NOT INCIDENTAL
//
// This package is IN CORE SCOPE and produces no findings: no os, no sockets, no
// goroutines, no time. It is the only piece of I2's new surface that survives
// core scope, and that is a property to defend rather than a coincidence to
// notice.
//
//	THE PURE HALF STAYS IN SCOPE AND GETS CHECKED. ONLY THE PART THAT TOUCHES
//	SOCKETS IS EXCLUDED.
//
// net/tcp holds the dialing, the listening, the goroutine per peer and the
// bounded queue, and is excluded by name. Keeping them in one package would
// have put this codec outside the determinism pass for no reason other than its
// neighbours -- and TestScopeTable pins both polarities so the split cannot
// erode: `net` in, `net/tcp` out, `net/tcpfoo` in.
//
// So this package inherits no confidence from anywhere. The frame ENCODING is
// sim/codec.go's and is already pinned by tools/codecpin; what is new here is
// the STREAM layer, and every way a stream can lie to a reader is a case with a
// test: a short read, a frame split across reads, a body length that exceeds
// the cap, a peer that closes mid-frame, and a length that would overflow.
package net

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/anshkanyadi/rift/sim"
)

// frameHeaderBytes mirrors sim/codec.go's header. It is duplicated ON PURPOSE
// and pinned by a test rather than exported from sim: sim is core scope and
// this package is orchestration, and a constant crossing that boundary would be
// a dependency in the wrong direction.
//
// TestTheHeaderSizeMatchesTheCodec is what keeps the two honest -- BUG-032's
// one-fact-two-places shape, answered with a check rather than with care.
const frameHeaderBytes = 1 + 4 + 4 + 8 + 2 + 4

// lengthOffset is where the body length sits inside the header.
const lengthOffset = 19

// maxBodyBytes bounds a body a peer may claim. It mirrors the codec's cap, and
// the reader enforces it BEFORE allocating.
//
//	A LENGTH READ OFF A SOCKET IS AN ATTACKER-CONTROLLED NUMBER EVEN WHEN THERE
//	IS NO ATTACKER: a corrupted or mis-framed stream produces the same effect as
//	a malicious one, and "allocate what the peer asked for" is how a framing bug
//	becomes an out-of-memory.
const maxBodyBytes = 1 << 22 // 4 MiB, the codec's own cap

// ErrFrameTooLarge is returned when a peer claims a body beyond the cap.
var ErrFrameTooLarge = errors.New("net: frame too large")

// ReadFrame reads exactly one frame from r and decodes it.
//
// # What it promises, and each clause is a test
//
//   - It reads the header FULLY before looking at the length, using
//     io.ReadFull, so a header split across TCP segments is not a short frame.
//   - It checks the length against the cap BEFORE allocating.
//   - It reads the body FULLY, so a body split across segments is not a short
//     frame either.
//   - A clean close at a frame boundary is io.EOF, and a close MID-frame is
//     io.ErrUnexpectedEOF. Those are different events -- one is a peer leaving,
//     one is a peer dying -- and collapsing them would lose the distinction a
//     chaos run exists to create.
func ReadFrame(r io.Reader) (sim.Envelope, error) {
	head := make([]byte, frameHeaderBytes)
	if _, err := io.ReadFull(r, head); err != nil {
		// io.ReadFull maps a zero-byte read to EOF and a partial one to
		// ErrUnexpectedEOF, which is exactly the distinction above.
		return sim.Envelope{}, err
	}

	n := binary.BigEndian.Uint32(head[lengthOffset:])
	if n > maxBodyBytes {
		return sim.Envelope{}, fmt.Errorf("%w: peer claims %d bytes, cap is %d", ErrFrameTooLarge, n, maxBodyBytes)
	}

	buf := make([]byte, frameHeaderBytes+int(n))
	copy(buf, head)
	if n > 0 {
		if _, err := io.ReadFull(r, buf[frameHeaderBytes:]); err != nil {
			return sim.Envelope{}, err
		}
	}
	return sim.Decode(buf)
}

// WriteFrame writes one encoded frame to w.
func WriteFrame(w io.Writer, e sim.Envelope) error {
	buf, err := sim.Encode(e)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}
