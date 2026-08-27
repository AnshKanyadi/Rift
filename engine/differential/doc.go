// Package differential judges one run of Track B's differential rig.
//
// # What it is
//
// The C++ engine and [engine/model] are two implementations of the recovery
// contract A0.5 froze. This package is a third thing: it reads an artifact the
// C++ rig produced, replays the same operations into the model, and reports
// whether the two agree.
//
// It is NOT part of the model. The model is the reference; the judge consults
// it. Keeping them separate is the whole of the topology decision — see
// docs/DESIGN-B4-verification.md §2.
//
// # Why the decoder here is a second implementation and not a binding
//
// The C++ side has a decoder for the same format. This one is written from
// docs/FORMAT-differential.md and never from that code.
//
//	A shared decoder makes writer and reader agree by construction, and the
//	single bug a frozen format exists to catch is a disagreement about what the
//	bytes mean. Byte-for-byte agreement is the thing under test, so it cannot
//	be the thing assumed.
//
// The cost is a second implementation, maintained. What makes it affordable is
// that the FIXTURES are the shared artifact rather than the code: both decoders
// are checked against the same committed bytes in seeds/differential/format/,
// hand-built from the document and produced by neither encoder.
//
// # Ruling 4, restated for a world with two engines
//
// The op log is the shared input. Neither engine is a witness about the other.
// The watermark is captured from the live C++ process before the kill and is an
// INPUT to both sides — a rig that asked the survivor what had been promised
// could not catch a broken promise.
//
// This package therefore never links the C++ engine and cannot ask it anything.
// It reads bytes.
package differential
