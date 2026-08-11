// Package store is the multi-raft node: it hosts many Raft groups (one per
// range) over one shared transport, drives the persist and apply loops,
// executes splits, and reports per-range load statistics.
//
// It is event-driven and single-threaded by construction. Every input --
// message delivery, tick, durability completion, client request -- arrives as
// an event and is handled to completion without blocking. The same code runs
// under the simulator and in real mode; only the driver that feeds it differs.
//
// Lands in A2/A4.
package store
