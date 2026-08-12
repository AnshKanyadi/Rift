package sim

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/internal/rng"
)

// TestCodecRoundTrip is the property the fidelity ruling rests on: what goes in
// comes out, byte for byte, and the decoded body shares nothing with the frame.
func TestCodecRoundTrip(t *testing.T) {
	cases := []Envelope{
		{From: 0, To: 1, RangeID: 0, Kind: 0, Body: nil},
		{From: 1, To: 2, RangeID: 7, Kind: 3, Body: []byte("hello")},
		{From: 4294967295, To: 0, RangeID: 1 << 63, Kind: 65535, Body: bytes.Repeat([]byte{0xAB}, 4096)},
	}
	for i, want := range cases {
		frame, err := Encode(want)
		if err != nil {
			t.Fatalf("case %d: encode: %v", i, err)
		}
		if len(frame) != want.Size() {
			t.Errorf("case %d: frame is %d bytes, Size says %d", i, len(frame), want.Size())
		}

		got, err := Decode(frame)
		if err != nil {
			t.Fatalf("case %d: decode: %v", i, err)
		}
		if got.From != want.From || got.To != want.To || got.RangeID != want.RangeID || got.Kind != want.Kind {
			t.Errorf("case %d: header round-trip: got %+v, want %+v", i, got, want)
		}
		if !bytes.Equal(got.Body, want.Body) {
			t.Errorf("case %d: body round-trip: got %q, want %q", i, got.Body, want.Body)
		}

		// The decoded body must not alias the frame: a receiver that shares
		// memory with the wire buffer is the aliasing bug encoding exists to
		// remove.
		if len(got.Body) > 0 {
			frame[frameHeaderBytes] ^= 0xFF
			if bytes.Equal(got.Body, frame[frameHeaderBytes:]) {
				t.Errorf("case %d: decoded body aliases the frame", i)
			}
		}
	}
}

// TestCodecRejectsMalformedFrames: every truncation and corruption gets a named
// error rather than a partial read. Silently ignoring trailing bytes or a short
// body is how a framing bug survives until it corrupts something that matters.
func TestCodecRejectsMalformedFrames(t *testing.T) {
	good, err := Encode(Envelope{From: 1, To: 2, Body: []byte("payload")})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Every truncation of a valid frame must fail.
	for n := range len(good) {
		if _, err := Decode(good[:n]); err == nil {
			t.Errorf("decoding a %d-byte truncation of a %d-byte frame succeeded", n, len(good))
		}
	}
	// Trailing bytes.
	if _, err := Decode(append(append([]byte{}, good...), 0x00)); err == nil {
		t.Error("decoding a frame with trailing bytes succeeded")
	}
	// Wrong version.
	bad := append([]byte{}, good...)
	bad[0] = 99
	if _, err := Decode(bad); err == nil {
		t.Error("decoding an unknown wire version succeeded")
	}
	// A length field larger than the maximum must be rejected without
	// allocating: an unbounded length is how a corrupt frame becomes an
	// out-of-memory.
	huge := append([]byte{}, good...)
	huge[19], huge[20], huge[21], huge[22] = 0xFF, 0xFF, 0xFF, 0xFF
	if _, err := Decode(huge); err == nil {
		t.Error("decoding an oversized body length succeeded")
	}
}

// sink is a node that decodes what it receives and records the order.
type sink struct {
	id  NodeID
	got []string
}

func (s *sink) Handle(ev Event, _ Scheduler) {
	if ev.Kind != KindDeliver {
		return
	}
	frame, ok := ev.Payload.([]byte)
	if !ok {
		return
	}
	e, err := Decode(frame)
	if err != nil {
		s.got = append(s.got, "decode-error:"+err.Error())
		return
	}
	s.got = append(s.got, string(e.Body))
}

func newTransportRun(t *testing.T, nodes int, seed uint64) (*Loop, *SimTransport, *Counters, []*sink) {
	t.Helper()

	sinks := make([]*sink, nodes)
	ns := make([]Node, nodes)
	cs := make([]*clock.Sim, nodes)
	for i := range nodes {
		sinks[i] = &sink{id: NodeID(i)}
		ns[i] = sinks[i]
		c, err := clock.NewSim(clock.Flat(), maxOffset)
		if err != nil {
			t.Fatalf("clock: %v", err)
		}
		cs[i] = c
	}

	l, err := NewLoop(Config{
		Nodes: ns, Clocks: cs,
		TickInterval: time.Second, // out of the way; this suite is about messages
		Until:        clock.Instant(10 * time.Second),
		MaxSteps:     1_000_000,
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}

	counts := NewCounters()
	return l, NewSimTransport(l, rng.New(seed).DeriveKey("net"), counts), counts, sinks
}

// TestDiceAreIdentityKeyedNotSequential is the property that makes a plan a
// total repro. The outcome for a message depends on its own identity and
// nothing else, so traffic on one link cannot perturb another link's dice --
// which a sequential draw would, and which would make the minimizer unsound.
func TestDiceAreIdentityKeyedNotSequential(t *testing.T) {
	run := func(extraTraffic bool) []string {
		l, tr, _, sinks := newTransportRun(t, 3, 42)
		for i := range 200 {
			if extraTraffic {
				// Unrelated traffic on a different link, interleaved. Under
				// sequential draws this shifts every subsequent outcome on
				// every link.
				tr.Send(Envelope{From: 2, To: 0, Body: fmt.Appendf(nil, "noise%d", i)})
			}
			tr.Send(Envelope{From: 0, To: 1, Body: fmt.Appendf(nil, "m%d", i)})
		}
		if _, err := l.Run(); err != nil {
			t.Fatalf("run: %v", err)
		}
		return sinks[1].got
	}

	quiet, noisy := run(false), run(true)
	if len(quiet) == 0 {
		t.Fatal("no messages were delivered, so this proves nothing")
	}
	if len(quiet) != len(noisy) {
		t.Fatalf("unrelated traffic changed link 0->1: %d delivered alone, %d alongside noise", len(quiet), len(noisy))
	}
	for i := range quiet {
		if quiet[i] != noisy[i] {
			t.Fatalf("unrelated traffic changed link 0->1 at %d: %q then %q", i, quiet[i], noisy[i])
		}
	}
}

// TestFaultsActuallyFire is the assertion that separates a chaos suite from a
// chaos-shaped decoration: over a large sample, drops, duplicates and tail
// latencies all occur, and reordering emerges from independent latency without
// being a knob.
func TestFaultsActuallyFire(t *testing.T) {
	l, tr, counts, sinks := newTransportRun(t, 2, 7)
	counts.Require(InjDrop, 1)
	counts.Require(InjDuplicate, 1)
	counts.Require(InjTailLatency, 1)

	const n = 3000
	for i := range n {
		tr.Send(Envelope{From: 0, To: 1, Body: fmt.Appendf(nil, "%d", i)})
	}
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	if short := counts.Check(); len(short) != 0 {
		for _, s := range short {
			t.Errorf("injector shortfall: %s", s)
		}
	}

	// Reordering is emergent, not dialled. Assert it happened rather than
	// assuming it: independent per-message latency with a heavy tail is what
	// produces it, and a model without the tail would deliver in order forever.
	var reordered int
	var last int = -1
	for _, s := range sinks[1].got {
		var v int
		if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
			continue
		}
		if v < last {
			reordered++
		}
		last = v
	}
	counts.Add(InjReorder, uint64(reordered))
	if reordered == 0 {
		t.Error("no message arrived out of order in 3000 sends; the latency model is not producing reordering")
	}

	// The reorder count is deliberately not asserted as a *rate*: every send in
	// this test happens at one instant, so delivery order is decided purely by
	// latency and roughly half of everything arrives "out of order". The
	// assertion is that reordering occurs at all, which is what the heavy tail
	// buys; quoting the fraction as a network property would be wrong.
	t.Logf("of %d sends:\n%s", n, counts.Report())
}

// TestPartitionsAreDirected: a symmetric partition is two cuts, and an
// asymmetric one is a single cut -- which is the case that produces a node able
// to send but not receive, and which symmetric partitions never generate.
func TestPartitionsAreDirected(t *testing.T) {
	l, tr, counts, sinks := newTransportRun(t, 2, 3)
	counts.Require(InjPartition, 1)

	tr.Cut(0, 1) // one direction only
	for i := range 50 {
		tr.Send(Envelope{From: 0, To: 1, Body: fmt.Appendf(nil, "forward%d", i)})
		tr.Send(Envelope{From: 1, To: 0, Body: fmt.Appendf(nil, "back%d", i)})
	}
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(sinks[1].got) != 0 {
		t.Errorf("%d messages crossed a cut link", len(sinks[1].got))
	}
	if len(sinks[0].got) == 0 {
		t.Error("the reverse direction was cut too; the partition is not directed")
	}
	if counts.Count(InjPartitionDrop) == 0 {
		t.Error("partition drops were not counted")
	}
	if short := counts.Check(); len(short) != 0 {
		t.Errorf("injector shortfall: %v", short)
	}

	// Healing restores it.
	tr.Heal(0, 1)
	if tr.cut[linkID{0, 1}] {
		t.Error("Heal did not clear the cut")
	}
}

// TestShortfallIsAFailure pins the rule itself: an enabled injector that never
// fired is a failed run, and Check is what says so.
func TestShortfallIsAFailure(t *testing.T) {
	counts := NewCounters()
	counts.Require(InjCrash, 1)
	counts.Require(InjSyncLoss, 3)
	counts.Fire(InjSyncLoss)

	short := counts.Check()
	if len(short) != 2 {
		t.Fatalf("got %d shortfalls, want 2: %v", len(short), short)
	}
	// Stable order, so two runs are diffable.
	if short[0].Injector != InjCrash || short[1].Injector != InjSyncLoss {
		t.Errorf("shortfalls out of order: %v", short)
	}
	if short[1].Got != 1 || short[1].Want != 3 {
		t.Errorf("sync-loss shortfall = %+v, want got 1 want 3", short[1])
	}

	counts.Fire(InjCrash)
	counts.Add(InjSyncLoss, 2)
	if short := counts.Check(); len(short) != 0 {
		t.Errorf("still short after meeting both minimums: %v", short)
	}
}

// TestSendIsFireAndForget: Send returns nothing, so no caller can branch on
// delivery. An error signal would be a covert failure detector, and covert
// failure detectors are how consensus implementations accidentally become
// unsafe.
func TestSendIsFireAndForget(t *testing.T) {
	l, tr, _, _ := newTransportRun(t, 2, 1)
	tr.CutBoth(0, 1)
	tr.Send(Envelope{From: 0, To: 1, Body: []byte("into the void")})
	if _, err := l.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Nothing to assert about the return value: there isn't one. That is the
	// property, and this test exists so that adding one breaks it.
	var _ func(Envelope) = tr.Send
}
