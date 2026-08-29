//go:build rift_cgo

package main

import (
	"fmt"
	"path/filepath"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/riftcgo"
	"github.com/anshkanyadi/rift/store"
)

// engineFor returns this node's storage: the real C++ engine, on this node's
// own directory.
//
// Not engine/simcgo. That wrapper models a SIMULATED crash by rolling a
// directory back to a harness-chosen point, and here the crash is real -- the
// process dies and the engine's own recovery runs against whatever the kill
// left. Using simcgo would replace the thing under test with a model of it.
func engineFor(dir string) func() store.Engine {
	return func() store.Engine {
		db, err := riftcgo.Open(filepath.Join(dir, "engine"), 0, 0, 0)
		if err != nil {
			panic(fmt.Sprintf("riftnode: opening the C++ engine under %s: %v", dir, err))
		}
		return realEngine{db}
	}
}

const EngineName = "engine/riftcgo (the C++ engine)"

// realEngine adapts the C++ engine to store.Engine, which asks for two things a
// real engine has no answer to.
//
// # store.Engine is the SIMULATOR's requirement, and real mode inherits it
//
// The frozen engine.Engine contract has neither Crash() nor AdvanceDurable():
// DESIGN-I2 D2 kept them out on purpose, because a real engine crashes when the
// process dies and durability is driven by whoever owns the poller. store.Engine
// adds them because the STORE's code calls them, and the store is one body of
// code driven by two modes.
//
//	SO REAL MODE MUST ANSWER TWO QUESTIONS THAT ONLY MAKE SENSE IN SIMULATION,
//	and the honest answers are different from each other: one is a real
//	operation with a different name, and the other must never happen.
type realEngine struct{ *riftcgo.DB }

// AdvanceDurable is the simulator's fsync completion, and in real mode it is an
// actual fsync. The store schedules a durability event a sync-latency after the
// write; when it fires, this makes it true rather than modelling it.
//
// The sequence is IGNORED. That is DESIGN-A0 §7's I1 idealization arriving in
// real mode -- cited rather than restated, because the entry already carries the
// measurement and the bound.
//
//	AN IDEALIZATION THAT SHOWS UP EXACTLY WHERE IT WAS RECORDED TO SHOW UP IS
//	THE RECORD WORKING. It was written down at I1 as a property of the C++
//	engine's sync, predicted to bind wherever that sync is driven, and here it
//	binds without anyone rediscovering it.
func (e realEngine) AdvanceDurable(seq engine.SeqNum) {
	if _, err := e.DB.Sync(); err != nil {
		panic("riftnode: sync: " + err.Error())
	}
}

// Crash must never be called in real mode.
//
//	A REAL CRASH IS THE PROCESS DYING. If the store calls this, something is
//	driving a real node with a simulated fault, and the two must not be mixed --
//	a modelled crash on a real engine would discard nothing, report success, and
//	be exactly the weaker fault I1 refused.
//
// Panicking is the honest answer: this is a harness error, not a modelled fault,
// and returning quietly would let a chaos run report crash-survival having
// crashed nothing.
func (e realEngine) Crash() {
	panic("riftnode: store.Crash() was called on a real engine; a real crash is the process dying, " +
		"and a modelled one here would discard nothing while reporting success")
}
