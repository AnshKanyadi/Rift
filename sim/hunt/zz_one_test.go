package hunt_test

import (
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

func TestZZOpts(t *testing.T) {
	opt := hunt.A5Options()
	n, first := 0, -1
	var detail string
	for seed := uint64(0); seed < 200; seed++ {
		p, err := hunt.MaterializeRaftWith(seed, opt)
		if err != nil {
			t.Fatal(err)
		}
		r, err := hunt.RunRaftWith(p, opt, nil)
		if err != nil || r.Violated != nil {
			n++
			if first < 0 {
				first = int(seed)
				if r.Violated != nil {
					detail = r.Violated.Checker + ": " + r.Violated.Detail
				} else {
					detail = err.Error()
				}
			}
		}
	}
	t.Logf("A5 bad=%d/200 first=%d %s", n, first, detail)
}
