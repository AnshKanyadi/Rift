// Package engine is the storage interface both engines implement: the
// deterministic Go reference in engine/model, and the C++ LSM behind cgo in
// Track B.
//
// The interface is frozen as of A0.5. Track B builds to it, so a change here is
// a change to two tracks at once, and interface churn after B has built to it
// is the most expensive kind there is.
//
// # The durability contract
//
// Exactly a WAL's, and the whole reason the interface has this shape:
//
//   - Apply makes writes visible immediately and never blocks on I/O.
//   - DurableSeq is a watermark that advances behind visibility.
//   - A crash reverts state to exactly DurableSeq.
//
// Buffered writes are readable and losable. That gap is not an implementation
// detail to minimize; it is the window in which acknowledged-but-unsynced data
// exists, and a soak that never lost an unsynced write has not tested
// durability regardless of how green it is (DESIGN-A0 DR-15).
//
// A blocking Apply was rejected: it cannot be called from a non-blocking event
// loop, and would force either a thread per node in simulation, which kills
// determinism, or two divergent engine interfaces (DR-10).
//
// # DeleteRange
//
// In the frozen interface by Amendment A3, over the original recommendation to
// leave it out. Snapshot application needs an atomic clear-then-ingest that a
// best-effort Go helper cannot provide at the right isolation, and replica
// removal at scale cannot be one unbounded point-delete batch. engine/model
// implements it natively from A0.5; the C++ engine implements it internally as
// iterate-and-point-delete through B2 so that B4's differential tests exercise
// the semantics early, and real range tombstones are a B3 deliverable that must
// land before any I2 benchmark number is taken.
//
// See DESIGN-A0 D7, D10, DR-10, DR-13. Landed in A0.5.
package engine
