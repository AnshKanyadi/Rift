package core

import "monocore"

// Floats are banned on every path that can feed the trace hash, so they are
// banned in core packages outright: a multiply-add may be fused into one FMA on
// arm64 and not on amd64, and a last-bit difference in a lease expiry is a
// different history.
type loadStats struct {
	qps     float64 // want `float: float64`
	bytes   float32 // want `float: float32`
	samples int64
}

func (n *node) score() float64 { // want `float: float64`
	return 0.98 // want `float: floating-point literals`
}

// Mono leakage: a monotonic reading is meaningful only on the node and boot
// that produced it, so it may not sit in a field that leaves either.
type leaseRecord struct {
	Expiry   monocore.Mono   // want `monoleak: Expiry carries a clock.Mono`
	renewals []monocore.Mono `json:"renewals"` // want `monoleak: renewals carries a clock.Mono`
	acquired monocore.Wall
	local    monocore.Mono // node-local, unexported, untagged: this is what Mono is for
}

// Instant arithmetic: the operators that defined integer types keep, and the
// type lie they let through. The quantity in hand after subtracting two
// readings is a duration; calling it an instant lets it flow into
// instant-typed positions.
type lease struct {
	start monocore.Mono
	end   monocore.Mono
	until monocore.Wall
}

func (l lease) held() monocore.Mono {
	return l.end - l.start // want `instantmath: subtracting two monocore.Mono`
}

func (l lease) extended(by monocore.Mono) monocore.Mono {
	return l.end + by // want `instantmath: adding two monocore.Mono`
}

// An untyped constant takes the instant's type, so scaling a reading is caught
// by the same rule -- which is right: an instant times a scalar is not an
// instant, and `w + 1` should be w.Add(time.Nanosecond).
func (l lease) doubled() monocore.Wall {
	return l.until * 2 // want `instantmath: arithmetic on two monocore.Wall`
}

// Comparisons stay legal: ordering two readings from one node is what defined
// integer types bought, and there is no lie available in a bool.
func (l lease) expired(now monocore.Mono) bool {
	return now > l.end && l.start <= now
}
