//go:build rift_cgo

// THE BUILD TAG IS NOT A CONVENIENCE. This package CANNOT LINK without the C++
// archive, which is a fact about what it is and not a configuration: `make test`
// -- Track A's Go unit lane, run from a clean clone with no CMake anywhere --
// would fail to link it, and every scheme that makes that lane green without
// the archive either skips this package or lies about it. The tag skips it, and
// says so.
//
// THE HOLE A TAG OPENS, AND WHERE IT IS CLOSED. A tagged package vanishes from
// ./... , so it could sit unanalyzed by the determinism pass forever with every
// lane green -- which is precisely the silent-exclusion failure that pass's
// load gate exists to prevent. Two things close it: `make determinism` and the
// hatch-registry test both load with -tags rift_cgo (they only TYPECHECK, and
// typechecking needs no archive, which is what the ${SRCDIR} CFLAGS bought),
// and TestHatchRegistry asserts BY NAME that this package was among those
// loaded. The tag can hide it from a lane; it cannot hide it from the check
// that it was looked at.
// Package riftcgo implements [engine.Engine] over the C++ LSM engine through
// the frozen C boundary in engine-cpp/capi.
//
// # What crosses, and what does not
//
// No C++ type crosses. No Go pointer is stored by C beyond a call. No C-to-Go
// callback exists — [A1] prohibits them, so OnDurable is implemented on this
// side over the blocking Sync, which db.h's divergence 1 records as the more
// primitive of the two: a callback can be built from a poller and a poller
// cannot be built from a callback.
//
// # The poller is NOT here
//
// B1-Q11, ruled at B5: the poller is part of the harness, not the engine's
// contract. The frozen contract is Apply returning a SeqNum without blocking, a
// monotone DurableSeq, and OnDurable — nothing in that requires the engine to
// own a thread that drives syncs, and a poller inside it would be a thread of
// control the engine schedules, which is the concurrency [A5] refused at the
// memtable for the same reason.
//
//	A PRODUCTION EMBEDDER SUPPLIES ITS OWN POLLER, and nothing in BENCHMARKS.md
//	is a claim about a poller we ship.
//
// This package therefore exposes Sync and DrainDurable; whoever owns the
// goroutine calls them.
//
// # Iteration
//
// The C boundary fetches BLOCKS of pairs and this package serves a CURSOR from
// the block it holds. Positioning and fetching are separate calls, so a seek's
// own entry is returned rather than skipped — and buffering the whole iteration
// on this side, which would also present a cursor, would defeat the
// amortisation the block interface exists to provide.
package riftcgo
