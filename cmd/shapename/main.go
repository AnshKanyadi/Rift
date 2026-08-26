// Command shapename prints the shape the sweep currently runs.
//
// It exists so scripts name the shape by READING it rather than by carrying a
// string beside it. `exit-run.sh` printed "A6 exit run" over a sweep of A7's
// shape; the two before it were `power-config: a3` and the single-label opt-out.
// Three instances is a pattern, and the answer is derivation.
package main

import (
	"fmt"

	"github.com/anshkanyadi/rift/sim/hunt"
)

func main() { fmt.Print(hunt.CurrentShapeName()) }
