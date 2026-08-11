package rng

import (
	"encoding/hex"
	"fmt"
	"math/bits"
)

// Key is a 128-bit key for the stateless PRF, and the derivation material for
// a generator. It round-trips through plan files as 32 hex digits.
type Key struct{ Hi, Lo uint64 }

// Domain separates PRF uses so that two subsystems keyed identically cannot
// produce correlated values. New domains are appended; existing values are
// never renumbered, because renumbering silently rewrites every plan that
// referenced them.
type Domain uint32

const (
	DomainNetDrop Domain = iota + 1
	DomainNetLatency
	DomainNetDuplicate
	DomainEngineSync
	DomainClockJitter
	DomainRaftElection
	DomainWorkload
)

// PRF returns a pseudorandom value for the identity (domain, a, b, c) under
// this key.
//
// It is a pure function: the same inputs give the same output regardless of
// call order, of what else has been evaluated, or of how many messages a code
// change added elsewhere. That is the property that makes a serialized plan a
// complete reproduction with no live randomness, and the property that makes
// delta-debugging sound -- deleting one fault entry cannot shift any other
// event's dice, because no shared position exists to shift.
//
// Identities are canonical tuples, for example:
//
//	network dice     (from, to, ordinal on that directed link)
//	engine sync      (node, seqnum, 0)
//	clock jitter     (node, tick ordinal, 0)
//	election timeout (node, term, election ordinal)
//
// Unused components are zero. Three components cover every identity the
// simulator needs, and fixing the arity keeps this allocation-free on a path
// that runs once per message in a soak measured in billions of messages.
func (k Key) PRF(d Domain, a, b, c uint64) uint64 {
	h := mix(k.Lo ^ (uint64(d)+1)*golden)
	h = mix(h ^ k.Hi ^ a)
	h = mix(h ^ b)
	h = mix(h ^ c)
	return h
}

// Float64 returns a PRF-derived value in [0, 1) with 53 bits of precision.
func (k Key) Float64(d Domain, a, b, c uint64) float64 {
	return float64(k.PRF(d, a, b, c)>>11) * 0x1p-53
}

// Bool reports true with probability p for the given identity.
func (k Key) Bool(d Domain, a, b, c uint64, p float64) bool {
	switch {
	case p <= 0:
		return false
	case p >= 1:
		return true
	default:
		return k.Float64(d, a, b, c) < p
	}
}

// Uint64N returns a PRF-derived value in [0, n). It panics if n == 0.
//
// Unlike (*PCG).Uint64N this does NOT reject-and-redraw, because a stateless
// function has nothing to redraw from: the identity determines the answer. It
// uses the multiply-shift reduction directly, whose bias is bounded by n/2^64
// -- around one part in 10^15 for the largest ranges the simulator uses, and
// exactly zero for powers of two. That is irrelevant for fault dice and
// latency sampling, which is all this is for. Anywhere exact uniformity
// matters, draw from a sequential stream during plan generation instead, where
// rejection sampling is available.
func (k Key) Uint64N(d Domain, a, b, c uint64, n uint64) uint64 {
	if n == 0 {
		panic("rng: Key.Uint64N called with n == 0")
	}
	hi, _ := bits.Mul64(k.PRF(d, a, b, c), n)
	return hi
}

// Between returns a PRF-derived value in [lo, hi). It panics if hi <= lo.
func (k Key) Between(d Domain, a, b, c uint64, lo, hi uint64) uint64 {
	if hi <= lo {
		panic("rng: Key.Between called with hi <= lo")
	}
	return lo + k.Uint64N(d, a, b, c, hi-lo)
}

// String renders the key as 32 lowercase hex digits.
func (k Key) String() string {
	var b [16]byte
	putUint64BE(b[0:8], k.Hi)
	putUint64BE(b[8:16], k.Lo)
	return hex.EncodeToString(b[:])
}

// MarshalText implements encoding.TextMarshaler so keys round-trip through
// plan files as plain hex rather than as a pair of numbers.
func (k Key) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (k *Key) UnmarshalText(text []byte) error {
	b, err := hex.DecodeString(string(text))
	if err != nil {
		return fmt.Errorf("rng: bad key %q: %w", text, err)
	}
	if len(b) != 16 {
		return fmt.Errorf("rng: bad key %q: got %d bytes, want 16", text, len(b))
	}
	k.Hi = uint64BE(b[0:8])
	k.Lo = uint64BE(b[8:16])
	return nil
}

// ParseKey parses 32 hex digits into a Key.
func ParseKey(s string) (Key, error) {
	var k Key
	err := k.UnmarshalText([]byte(s))
	return k, err
}

func putUint64BE(b []byte, v uint64) {
	_ = b[7]
	b[0], b[1], b[2], b[3] = byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32)
	b[4], b[5], b[6], b[7] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

func uint64BE(b []byte) uint64 {
	_ = b[7]
	return uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |
		uint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56
}
