//go:build rift_cgo

package hunt

import (
	"fmt"
	"path/filepath"

	"github.com/anshkanyadi/rift/engine/simcgo"
	"github.com/anshkanyadi/rift/store"
)

// EngineByName resolves the engine a run should use. This is the half that can
// link the C++ archive; see engine_model.go for why there are two.
func EngineByName(name, root string) (func(node int) store.Engine, error) {
	switch name {
	case "", "model":
		return nil, nil
	case "cgo":
		if root == "" {
			return nil, fmt.Errorf("hunt: engine %q needs a root directory to place per-node stores in", name)
		}
		return func(node int) store.Engine {
			// ONE DIRECTORY PER NODE, NAMED BY ORDINAL. One process, n
			// independent stores, exactly as n machines would have -- and named
			// by ordinal rather than by anything varying, so a rerun under a
			// different root is the same run.
			d, err := simcgo.Open(filepath.Join(root, fmt.Sprintf("n%02d", node)))
			if err != nil {
				// A harness that cannot open storage has not found a defect; it
				// has failed. Panicking is the honest report, because returning
				// a nil engine would surface later as a nil dereference in core
				// code and be read as a system defect.
				panic(fmt.Sprintf("hunt: opening the C++ engine for node %d under %s: %v", node, root, err))
			}
			return d
		}, nil
	default:
		return nil, fmt.Errorf("hunt: unknown engine %q (model, cgo)", name)
	}
}
