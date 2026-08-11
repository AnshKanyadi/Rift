// Package engine defines the storage interface that both the deterministic Go
// reference engine and the C++ LSM engine implement. This interface is frozen:
// Track B builds against it, so changes here are expensive on both tracks.
//
// The central contract is durability. Apply makes a batch visible to subsequent
// reads immediately and never blocks on I/O; durability is a separate, monotone
// watermark (DurableSeq) advanced asynchronously and observed via OnDurable. A
// crash reverts state to exactly DurableSeq. This is the write-ahead log's real
// behavior rather than an idealization of it, and the window between visibility
// and durability is where acknowledged-but-losable writes live.
//
// Batch operations are Set, Delete, and DeleteRange over [start, end).
//
// See DESIGN-A0 DR-10, DR-11, DR-13. Lands in A0.5.
package engine
