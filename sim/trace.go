package sim

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// Trace is the rolling hash of a run.
//
// Comparing O(1) hashes rather than storing full traces is what makes the
// determinism gate affordable at ten thousand seeds (DESIGN-A0 D11). Every step
// feeds a canonical, fixed-width encoding into an incremental SHA-256, and the
// digest after each step is retained so that a mismatch can name the *first*
// divergent step rather than only reporting that two runs differ.
//
// # What is hashed, and what is deliberately not
//
// Hashed: the step ordinal, the instant, the event kind, the node, and the
// payload's length. All fixed-width big-endian, in a fixed order.
//
// Not hashed, on purpose: pointers, wall-clock time, goroutine identity, map
// iteration order, and error *strings*. Every one of those varies between two
// runs that are otherwise identical, so including any of them would make the
// gate fail for reasons that are not divergence -- which trains everyone to
// ignore it, and a gate people ignore is worse than no gate.
//
// Payload *length* rather than payload bytes is a deliberate narrowing for
// A0.10: the transport already round-trips every message through the production
// codec, so message content is covered there, and hashing decoded content would
// mean this file knowing every payload type in the system.
type Trace struct {
	h     [32]byte
	steps []string
	limit int
}

// NewTrace returns a trace that retains at most limit per-step digests. Zero
// means retain everything, which is right for a single replay and wrong for a
// ten-thousand-seed hunt.
func NewTrace(limit int) *Trace {
	return &Trace{limit: limit}
}

// Step folds one event into the rolling hash.
func (t *Trace) Step(step uint64, ev Event) {
	var buf [32]byte
	binary.BigEndian.PutUint64(buf[0:], step)
	binary.BigEndian.PutUint64(buf[8:], uint64(ev.At))
	binary.BigEndian.PutUint32(buf[16:], uint32(ev.Kind))
	binary.BigEndian.PutUint32(buf[20:], uint32(ev.Node))
	binary.BigEndian.PutUint64(buf[24:], uint64(payloadLen(ev.Payload)))

	sum := sha256.New()
	sum.Write(t.h[:])
	sum.Write(buf[:])
	copy(t.h[:], sum.Sum(nil))

	if t.limit == 0 || len(t.steps) < t.limit {
		t.steps = append(t.steps, hex.EncodeToString(t.h[:8]))
	}
}

// Sum is the digest of the whole run, short enough to paste into a report and
// long enough that a collision is not the explanation for a match.
func (t *Trace) Sum() string { return hex.EncodeToString(t.h[:]) }

// Short is the first eight bytes, for logs and summaries.
func (t *Trace) Short() string { return hex.EncodeToString(t.h[:8]) }

// Steps returns the retained per-step digests.
func (t *Trace) Steps() []string { return t.steps }

// FirstDivergence compares two step-digest sequences and returns the index of
// the first that differs, or -1 if one is a prefix of the other and they agree
// as far as they go.
//
// The index is nearly always immediately diagnostic, which is the reason to
// retain per-step digests at all: "these two runs differ" sends an investigator
// to the whole run, and "they differ at step 4,117" sends them to one event.
func FirstDivergence(a, b []string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

// DivergenceReport renders a mismatch for a human.
func DivergenceReport(want, got []string) string {
	i := FirstDivergence(want, got)
	if i < 0 {
		return "traces agree"
	}
	if i >= len(want) || i >= len(got) {
		return fmt.Sprintf("traces agree for %d steps, then one ends: recorded %d steps, replayed %d",
			i, len(want), len(got))
	}
	return fmt.Sprintf("first divergence at step %d: recorded %s, replayed %s (agreed for the preceding %d steps)",
		i, want[i], got[i], i)
}

func payloadLen(p any) int {
	switch v := p.(type) {
	case nil:
		return 0
	case []byte:
		return len(v)
	default:
		// A payload the trace cannot measure contributes a constant, so its
		// presence is still hashed while its representation is not. Hashing a
		// Go value's formatting would put the fmt package's behaviour into the
		// run's identity.
		return -1
	}
}
