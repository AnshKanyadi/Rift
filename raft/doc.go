// Package raft is a pure consensus state machine: Step(Message) and Tick() in,
// a Ready struct out. No goroutines, no channels, no clocks, no I/O. That
// purity is what makes deterministic simulation possible and is non-negotiable.
//
// Ready is a drain, and there is no Advance(). Progress is acknowledged per
// resource -- AckPersisted(mark) and AckApplied(index) -- so appends can
// pipeline against replication instead of serializing behind one outstanding
// Ready.
//
// The interface's central safety property: raft never places a message in
// Ready.Messages whose meaning depends on persistent state that is not yet
// durable. Append responses, vote grants, post-term-bump responses, and
// snapshot acknowledgements are all withheld until their PersistMark is
// acknowledged. The driver therefore has no ordering obligation at all, and
// "persist before reply" is discharged here, where it is unit-testable, rather
// than distributed across every caller.
//
// See DESIGN-A0 DR-7 and DR-8. Lands in A1.
package raft
