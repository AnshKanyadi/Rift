// Package model is the deterministic Go reference engine.
//
// Track A runs on it exclusively, so consensus and transaction bugs never alias
// with storage bugs, and Track B differentially tests against it: identical
// operation sequences must produce byte-identical iterator output.
//
// It is backed by a copy-on-write sorted structure so snapshots are O(1) and
// iteration order is total and obvious. It models durability exactly as the
// interface specifies: visible state and durable state are tracked separately,
// and a crash discards everything above DurableSeq.
//
// See DESIGN-A0 DR-12. Lands in A0.5.
package model
