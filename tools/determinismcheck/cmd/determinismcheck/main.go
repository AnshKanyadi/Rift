// Command determinismcheck runs Rift's determinism pass over the packages
// named on the command line.
//
//	go run ./tools/determinismcheck/cmd/determinismcheck ./...
//
// It lives here rather than under the repo's cmd/ directory on purpose: cmd/
// holds shipping binaries, and golang.org/x/tools is approved as a tooling-only
// dependency that never enters a shipping binary's build graph (DESIGN-A0 Q4).
// `make tooling-only` asserts exactly that, and the assertion would be
// meaningless if the checker itself lived under cmd/.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/anshkanyadi/rift/tools/determinismcheck"
)

func main() { singlechecker.Main(determinismcheck.Analyzer) }
