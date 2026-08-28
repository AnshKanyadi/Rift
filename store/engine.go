package store

import (
	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
)

// Engine is what a replica needs from storage: the frozen contract, plus the
// two primitives a SIMULATED node needs and a real engine has no business
// having.
//
// # Why these two are here and not on engine.Engine
//
// DESIGN-I1 D2, ruled: a real engine has no Crash() -- it crashes when the
// process dies -- and no AdvanceDurable() -- durability is driven by whoever
// owns the poller, which B1-Q11 put outside the engine deliberately. Putting
// either on the frozen contract would make a production embedder implement a
// simulator concept, on the one interface both tracks agreed to keep narrow.
//
//	A CHANGE INSIDE THE STORE IS A CHANGE TO CODE. A CHANGE TO THE INTERFACE IS
//	A CHANGE TO THE CONTRACT BOTH TRACKS BUILD AGAINST. Prefer the former.
//
// So the simulator's requirement lives here, in the harness's own package, and
// two things satisfy it: engine/model natively, because it is a model, and
// engine/simcgo for the C++ engine, where a crash is a real close, rollback and
// reopen and durability is driven by the event loop.
//
// # VisibleSeq is deliberately NOT here
//
// It was on *model.DB and the store called it four times. It is not on this
// interface and not on the frozen one, because Apply ALREADY RETURNS the
// sequence a batch became visible at: the store was handed the number and then
// asked the engine to remember it for later.
//
//	THAT IS BUG-032's ONE-FACT-TWO-PLACES SHAPE, and it cost Track A three
//	cycles. The store tracks its own visible sequence from what Apply returned.
type Engine interface {
	engine.Engine

	// Crash discards everything not durable and returns the engine to the state
	// a restart would recover. On the model this is a method; on a real engine
	// the harness closes it, rolls the directory back to the last state it
	// declared durable, and reopens, so the engine's OWN recovery runs.
	Crash()

	// AdvanceDurable is the simulator's fsync completion, carrying the sequence
	// captured when the write was applied.
	//
	// On engine/model the watermark moves to exactly that sequence. On the C++
	// engine it cannot: rift_db_sync covers everything submitted and takes no
	// prefix argument on a frozen boundary, so a completion there makes
	// everything applied so far durable. DESIGN-A0 section 7 carries that as a
	// stated idealization with the gap measured -- mean 2.5 sequences, max 39
	// across five raft seeds -- rather than left open.
	AdvanceDurable(seq engine.SeqNum)
}

// Engine builds this config's storage, defaulting to the reference engine.
//
// The default is engine/model and stays so: at I1 the model becomes the CONTROL
// rather than a stepping stone. A divergence between the two engines is only a
// finding because one of them is the engine every Track A number was measured
// on, and if the model is retired the comparison stops meaning anything.
func (c Config) Engine() *tracked {
	if c.NewEngine != nil {
		return &tracked{Engine: c.NewEngine()}
	}
	return &tracked{Engine: model.New()}
}

// tracked pairs an Engine with the visible sequence its Applies returned.
//
// # Why a wrapper and not a field on Replica
//
// The first attempt put visibleSeq on Replica. That is wrong, and the reason is
// the same rule that moved the tracking off the interface in the first place:
// newReplicaFor does `r.db = m.db`, so MANY REPLICAS SHARE ONE ENGINE. A field
// per replica is one fact in N places, each drifting independently -- BUG-032
// exactly, avoided at the interface and reintroduced ten lines later.
//
// Caught by store/node.go's own guard on the first run: "node 1 read the engine
// back with sequence 107 visible and only 106 durable". The guard exists for a
// different defect and found this one, which is what a real invariant does.
//
// So the fact lives with the engine it is a fact about. Every replica sharing an
// engine shares its tracking by construction rather than by discipline.
type tracked struct {
	Engine
	visible engine.SeqNum
}

func (t *tracked) Apply(b *engine.Batch, sync bool) (engine.SeqNum, error) {
	seq, err := t.Engine.Apply(b, sync)
	if err == nil {
		t.visible = seq
	}
	return seq, err
}

// Crash returns the visible sequence to what survived, because a crash moves it
// as surely as an Apply does. Tracking a value means tracking EVERY transition
// of it, and the one that is easy to miss is the one that moves it backwards.
func (t *tracked) Crash() {
	t.Engine.Crash()
	t.visible = t.Engine.DurableSeq()
}

// visibleSeq is what Apply last returned. It is not on any interface: the store
// was handed the number and keeps it, which is the whole of the ruling.
func (t *tracked) visibleSeq() engine.SeqNum { return t.visible }

// Nothing in this package may call the wrapped engine's Apply directly: the
// tracking is the only reason the wrapper exists. Enforced by
// tools/enginepin, which lives there rather than here because reading source
// text needs os and this package is core scope.
