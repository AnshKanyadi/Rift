//go:build !rift_cgo

package hunt

import (
	"fmt"

	"github.com/anshkanyadi/rift/store"
)

// EngineByName resolves the engine a run should use.
//
// # Why this file has a twin
//
// engine/simcgo cannot link without the C++ archive, so a build without the
// rift_cgo tag must not import it -- `make test`, run from a clean clone with no
// CMake anywhere, is the lane that would break. The tag splits the resolver in
// two rather than the caller.
//
//	THE UNTAGGED HALF STILL KNOWS THE NAME "cgo" EXISTS AND REFUSES IT WITH A
//	REASON. A resolver that reported "unknown engine: cgo" would be true and
//	useless: the engine is not unknown, it is unbuilt, and those need different
//	actions from whoever hit the message.
func EngineByName(name, root string) (func(node int) store.Engine, error) {
	switch name {
	case "", "model":
		return nil, nil
	case "cgo":
		return nil, fmt.Errorf("hunt: engine %q needs the C++ archive: build with -tags rift_cgo "+
			"and CGO_LDFLAGS pointing at librift_capi.a (see `make cpp-cgo`)", name)
	default:
		return nil, fmt.Errorf("hunt: unknown engine %q (model, cgo)", name)
	}
}
