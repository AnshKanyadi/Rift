// Package balancer computes replica movements from store heartbeats carrying
// per-range QPS and write-bytes, against a mean-plus-threshold heuristic.
//
// A move is add replica, wait for catch-up, transfer lease and leadership,
// remove replica -- add-then-remove, so quorum availability is never
// voluntarily reduced -- throttled to one in-flight move per range.
//
// Lands in A10.
package balancer
