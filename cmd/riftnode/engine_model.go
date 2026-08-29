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
