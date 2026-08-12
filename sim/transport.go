package sim

import (
	"time"

	"github.com/anshkanyadi/rift/internal/rng"
)

// Transport is how a node sends. Fire-and-forget, no error return, and that is
// deliberate: an error signal is a covert failure detector, and covert failure
// detectors are how consensus implementations accidentally become unsafe
// (DESIGN-A0 D6). Loss is a normal outcome and the protocol above must tolerate
// it. Real mode gets the same semantics from a bounded per-peer queue that
// drops on overflow rather than blocking.
type Transport interface {
	Send(e Envelope)
}

// LinkParams are the per-directed-link dice. They live in the plan, so a run's
// network behaviour is data rather than code.
type LinkParams struct {
	DropPerMille uint64 // probability in parts per thousand
	DupPerMille  uint64
	LatMin       time.Duration
	LatMax       time.Duration

	// TailPerMille and TailMax model the heavy tail. Without one, every
	// latency sits in a narrow band and the reordering that finds real bugs
	// never happens.
	TailPerMille uint64
	TailMax      time.Duration
}

// DefaultLink is a plausible LAN link with enough tail to reorder.
func DefaultLink() LinkParams {
	return LinkParams{
		DropPerMille: 10,
		DupPerMille:  2,
		LatMin:       200 * time.Microsecond,
		LatMax:       3 * time.Millisecond,
		TailPerMille: 5,
		TailMax:      80 * time.Millisecond,
	}
}

// SimTransport delivers envelopes through the loop, applying per-message dice.
//
// Every dice roll is a keyed PRF over (from, to, ordinal on that directed
// link), never a sequential draw. That is what makes a plan a total repro: the
// outcome for one message on one link depends on nothing but its own identity,
// so deleting a fault entry, adding a log line, or sending one extra message on
// another link cannot perturb it. Sequential draws would desynchronize on any
// of those, and the minimizer would be testing something other than what it
// thinks (DR-6).
type SimTransport struct {
	loop  *Loop
	key   rng.Key
	links map[linkID]LinkParams
	def   LinkParams

	// ordinal counts messages per directed link, forming the third component
	// of each message's identity. It is state, but it is not randomness: the
	// same plan produces the same ordinals.
	ordinal map[linkID]uint64

	// partitions is the set of directed links currently cut. Asymmetric by
	// construction: a symmetric partition is two entries, which is what makes
	// "can send but not receive" expressible at all.
	cut map[linkID]bool

	counts *Counters
}

type linkID struct{ from, to NodeID }

// NewSimTransport builds a transport over a loop.
func NewSimTransport(l *Loop, key rng.Key, counts *Counters) *SimTransport {
	return &SimTransport{
		loop:    l,
		key:     key,
		links:   make(map[linkID]LinkParams),
		def:     DefaultLink(),
		ordinal: make(map[linkID]uint64),
		cut:     make(map[linkID]bool),
		counts:  counts,
	}
}

// SetLink overrides the parameters for one directed link.
func (t *SimTransport) SetLink(from, to NodeID, p LinkParams) { t.links[linkID{from, to}] = p }

// SetDefaultLink overrides the parameters for every unnamed link.
func (t *SimTransport) SetDefaultLink(p LinkParams) { t.def = p }

// Cut and Heal control partitions on one directed link. A symmetric partition
// is two cuts; an asymmetric one is a single cut, which is the case that
// produces a leader able to send but not receive -- and which symmetric
// partitions never generate.
func (t *SimTransport) Cut(from, to NodeID) {
	if !t.cut[linkID{from, to}] {
		t.cut[linkID{from, to}] = true
		t.counts.Fire(InjPartition)
	}
}

func (t *SimTransport) Heal(from, to NodeID) { delete(t.cut, linkID{from, to}) }

// CutBoth and HealBoth are the symmetric forms.
func (t *SimTransport) CutBoth(a, b NodeID)  { t.Cut(a, b); t.Cut(b, a) }
func (t *SimTransport) HealBoth(a, b NodeID) { t.Heal(a, b); t.Heal(b, a) }

// Send applies the dice and schedules delivery. It never blocks and never
// reports failure.
func (t *SimTransport) Send(e Envelope) {
	id := linkID{e.From, e.To}
	t.ordinal[id]++
	ord := t.ordinal[id]
	t.counts.Fire(InjSent)

	// The codec runs on every message, so encoding bugs are caught by the
	// corpus rather than at I2, and the receiver shares no memory with the
	// sender. A frame that will not encode is a programming error here, not a
	// network condition, so it is not silently dropped.
	frame, err := Encode(e)
	if err != nil {
		panic("sim: unencodable envelope: " + err.Error())
	}

	if t.cut[id] {
		t.counts.Fire(InjPartitionDrop)
		return
	}

	p := t.def
	if lp, ok := t.links[id]; ok {
		p = lp
	}

	if t.dice(perMilleDomain, uint64(e.From), uint64(e.To), ord, p.DropPerMille) {
		t.counts.Fire(InjDrop)
		return
	}

	t.deliver(frame, e, p, ord, 0)

	if t.dice(dupDomain, uint64(e.From), uint64(e.To), ord, p.DupPerMille) {
		t.counts.Fire(InjDuplicate)
		// The copy is delayed independently, so a duplicate that arrives
		// *first* falls out naturally -- which is the interesting case, and one
		// a "deliver twice back to back" model never produces.
		t.deliver(frame, e, p, ord, 1)
	}
}

// deliver schedules one copy of a frame.
func (t *SimTransport) deliver(frame []byte, e Envelope, p LinkParams, ord uint64, copyIdx uint64) {
	lat := t.latency(e.From, e.To, ord, copyIdx, p)
	t.counts.Fire(InjDeliver)

	// The frame is copied per delivery, so two copies of one message cannot
	// share a buffer either.
	dup := make([]byte, len(frame))
	copy(dup, frame)
	t.loop.At(t.loop.Now().Add(lat), KindDeliver, e.To, dup)
}

// latency draws this message's delay: a base uniform in [LatMin, LatMax], plus
// a tail excursion on a small fraction of messages. Reordering is emergent from
// independent per-message latency rather than a separate knob -- stated
// explicitly so nobody looks for a reorder flag -- and the emergent rate is
// something a run can assert on rather than dial.
func (t *SimTransport) latency(from, to NodeID, ord, copyIdx uint64, p LinkParams) time.Duration {
	span := int64(p.LatMax - p.LatMin)
	if span < 0 {
		span = 0
	}
	base := int64(p.LatMin)
	if span > 0 {
		base += int64(t.key.Uint64N(latDomain, uint64(from), uint64(to), ord*2+copyIdx, uint64(span)))
	}

	if t.dice(tailDomain, uint64(from), uint64(to), ord*2+copyIdx, p.TailPerMille) {
		t.counts.Fire(InjTailLatency)
		extra := int64(p.TailMax)
		if extra > 0 {
			base += int64(t.key.Uint64N(tailLenDomain, uint64(from), uint64(to), ord*2+copyIdx, uint64(extra)))
		}
	}
	return time.Duration(base)
}

// dice is a per-mille probability from the keyed PRF. Integer arithmetic, no
// float anywhere: the standing rule bans floating point on any path feeding
// replay identity, and a per-mille integer is exactly reproducible.
func (t *SimTransport) dice(d rng.Domain, a, b, c, perMille uint64) bool {
	if perMille == 0 {
		return false
	}
	if perMille >= 1000 {
		return true
	}
	return t.key.Uint64N(d, a, b, c, 1000) < perMille
}

// PRF domains. Distinct constants so that two quantities drawn for the same
// message identity cannot collide.
const (
	perMilleDomain rng.Domain = iota + 1
	dupDomain
	latDomain
	tailDomain
	tailLenDomain
)

var _ Transport = (*SimTransport)(nil)
