package hunt_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/sim/plan"
)

func runSafe(p *plan.Plan) (r hunt.RaftResult, err error) {
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("PANIC %v", x)
		}
	}()
	return hunt.RunRaft(p, nil)
}

func TestZZScan(t *testing.T) {
	by := map[string]int{}
	first := map[string]string{}
	for seed := uint64(0); seed < 2000; seed++ {
		p, _ := hunt.MaterializeRaft(seed)
		r, err := runSafe(p)
		if err != nil {
			by["ERR"]++
			if first["ERR"] == "" {
				first["ERR"] = fmt.Sprintf("seed %d: %v", seed, err)
			}
			continue
		}
		if r.Violated != nil {
			by[r.Violated.Checker]++
			if first[r.Violated.Checker] == "" {
				first[r.Violated.Checker] = fmt.Sprintf("seed %d: %s", seed, r.Violated.Detail)
			}
		}
	}
	ks := []string{}
	for k := range by {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		t.Logf("%-24s %3d | %s", k, by[k], first[k])
	}
	if len(ks) == 0 {
		t.Log("NO VIOLATIONS in 50 seeds")
	}
}
