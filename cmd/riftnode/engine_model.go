//go:build !rift_cgo

package main

import "github.com/anshkanyadi/rift/store"

// engineFor returns this node's storage.
//
// # Untagged: engine/model, and it is labelled at the point of use
//
// The C++ engine cannot link without its archive, so a build with no tag gets
// the reference engine. That is a real limitation and not a detail:
//
//	A CHAOS RUN ON engine/model IS A TEST OF RAFT AND THE TRANSPORT, NOT OF
//	STORAGE. It cannot lose a handle, cannot inherit a directory and cannot
//	orphan a callback across a crash -- GF-49's three, which is exactly the
//	class a real crash is supposed to reach.
//
// A run built this way must say so in its output. The alternative -- silently
// using the model and reporting a chaos result -- is the shape BUG-046 is
// about, one layer up.
func engineFor(dir string) func() store.Engine { return nil } // nil means engine/model

// EngineName is printed by the node so a reader of any result knows which
// engine produced it.
const EngineName = "engine/model (no rift_cgo tag: NOT a storage test)"

// EnginePersistent says whether this engine survives the process.
//
// # engine/model DOES NOT, and a restart is therefore not a crash (BUG-056)
//
// `engine/model` is a pure in-memory Go structure -- no files, no directory, no
// recovery path. `store/` persists HardState and log entries INTO the engine, so
// a SIGKILLed node restarts with no term, no vote and no log:
//
//	A RESTARTED NODE IS NOT THE SAME NODE RECOVERING. It is a FRESH node wearing
//	an existing identity, which is the single most dangerous thing that can
//	happen to a Raft cluster -- a node that has forgotten its vote can vote twice
//	in one term, and two leaders in one term overwrite committed entries.
//
// That is not a fault Rift claims to survive. CLAUDE.md's persistence rule --
// term, vote and log durable before replying to any RPC -- is an ASSUMPTION, and
// a harness that violates it is testing a different system.
//
// So the chaos gate refuses a restart schedule on a non-persistent engine, in
// the same shape as the untagged binary refusing to claim a storage result: the
// configuration is named, and the run that cannot support the claim does not
// make it.
const EnginePersistent = false
