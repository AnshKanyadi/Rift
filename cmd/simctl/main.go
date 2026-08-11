// Command simctl drives the deterministic simulator.
//
//	simctl run       --seed N | --plan p.json    execute one schedule
//	simctl replay    <bundle|seed>               re-execute, assert trace identity
//	simctl hunt      [--workers N]               search seeds in parallel
//	simctl minimize  <bundle>                    ddmin a failing plan
//
// Exit codes: 0 pass, 1 invariant violation (bundle written), 2 harness error.
//
// See DESIGN-A0 section 3. Lands in A0.10/A0.11.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "simctl: not implemented yet (lands in A0.10)")
	fmt.Fprintln(os.Stderr, "usage: simctl run|replay|hunt|minimize ...")
	os.Exit(2)
}
