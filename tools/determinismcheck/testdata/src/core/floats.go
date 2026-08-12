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
