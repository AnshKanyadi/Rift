//go:build rift_cgo

package hunt_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim/hunt"
	"github.com/anshkanyadi/rift/store"
)

// TestSweepOnTheCppEngine is I1's sweep: the raft workload on engine/simcgo.
//
// # THE SEED COUNT, DERIVED RATHER THAN INHERITED
//
// What this sweep is looking for is not "bugs in Raft" -- 25,000 seeds of that
// were run at A7's close on engine/model, and I1 does not re-earn them. It is
// looking for STORAGE-PATH DIVERGENCES under schedules the corpus does not
// contain: the corpus is 24 fixed schedules, and a sweep is the only instrument
// that reaches the ones nobody recorded.
//
// So the count comes from the recorded seeds-to-first-detection ceilings of the
// classes an engine swap could plausibly move -- durability, recovery, restart,
// snapshot install, iteration:
//
//	M30-leader-counts-its-own-unsynced-append   ceiling 300   <- the maximum
//	M70-ingest-does-not-seed-the-clock          ceiling 150
//	M25-restart-recovers-unsynced-writes        ceiling  48
//
//	300 SEEDS IS THE POINT AT WHICH EVERY STORAGE-FACING CLASS WITH A RECORDED
//	CEILING WOULD HAVE BEEN DETECTED AT LEAST ONCE IF IT WERE PRESENT.
//
// M19-vote-for-a-shorter-log's ceiling of 1,350 is deliberately NOT used. It is
// an election class; an engine swap cannot move it, and stretching the sweep
// 4.5x to cover a class this phase cannot affect would be inheriting a number
// rather than justifying one -- which is the thing the criterion asks against.
//
// # It reports the counters, not just the verdict
//
// BUG-046: a run reported a byte-identical trace hash having never opened the
// engine. A sweep is a thousand such runs, and "no violations" across a sweep
// that exercised nothing is the same claim about nothing, multiplied. So this
// prints every non-vacuity counter the census carries, and fails if the ones
// this workload must exercise are zero.
//
// # Chunked, because the runtime kills a long job
//
// ~20s per seed on the C++ engine (the per-Apply fsync and snapshot D2(b) costs,
// measured in engine/simcgo/cost_test.go), so 300 seeds is ~100 minutes and the
// observed per-job ceiling on this machine is 59-96 minutes. RIFT_SWEEP_FROM and
// RIFT_SWEEP_TO run a range; the default is small so `go test` stays a test.
func TestSweepOnTheCppEngine(t *testing.T) {
	from := envSeed(t, "RIFT_SWEEP_FROM", 0)
	to := envSeed(t, "RIFT_SWEEP_TO", 3)
	if testing.Short() {
		t.Skip("the C++ engine sweep is not a -short test")
	}

	// ONE ROOT PER SEED, and the first version got this wrong in a way only a
	// real engine could show.
	//
	// A single root for the whole sweep means every seed's node 0 opens the same
	// directory and inherits the previous seed's data. Seed 1 panicked on it:
	//
	//	store: node 2 has made durable something its own record disagrees with:
	//	snapshot recorded {Index:0 ...}, engine returned {Index:232 Term:3 ...}
	//
	// The record was right and the engine was right; the directory belonged to
	// seed 0. On engine/model this cannot happen -- a model engine is a fresh
	// struct with no past -- so the sweep harness carried an assumption that a
	// new node gets a blank slate, which is true of a model and false of a disk.
	//
	//	A SWEEP IS N INDEPENDENT RUNS ONLY IF EVERYTHING THEY TOUCH IS
	//	INDEPENDENT, AND A DIRECTORY IS NOT INDEPENDENT BY DEFAULT.
	// RIFT_SWEEP_ENGINE lets the SAME code path run on the model, which is the
	// only honest way to ask whether a result is engine-specific: same options,
	// same workload, same checkers, one variable changed.
	root := t.TempDir()
	var opened int
	useModel := os.Getenv("RIFT_SWEEP_ENGINE") == "model"
	newEngine := func(node int) store.Engine {
		// Unique per CALL, which is per (seed, node): store.New is the only
		// creator now that newReplica takes the machine's engine, and it is
		// called once per node per seed.
		opened++
		f, err := hunt.EngineByName("cgo", filepath.Join(root, fmt.Sprintf("r%05d", opened)))
		if err != nil {
			t.Fatalf("resolving the engine: %v", err)
		}
		if f == nil {
			t.Fatal("EngineByName returned no factory for \"cgo\", so this sweep would silently " +
				"run on engine/model -- which is BUG-046 at sweep scale")
		}
		return f(node)
	}

	opt := hunt.CurrentOptions()
	if !useModel {
		opt.NewEngine = newEngine
	} else {
		t.Log("RIFT_SWEEP_ENGINE=model: running the CONTROL, not the C++ engine")
	}

	start := time.Now()
	// A KILLED CHUNK LOSES EVERYTHING IT FOUND, and that is recorded here
	// because it cost thirty minutes and cannot be fixed from this side.
	//
	// SweepRaftWithProgress's hook exists because "a shard writes its census on
	// completion and nothing before it, so a running shard and a hung one look
	// identical from outside". That fixed LIVENESS. It did not fix RESULT
	// PRESERVATION: chunk [150,225) was killed by the runtime at 60 of 75 seeds
	// having printed sixty progress lines and no verdicts, and every one of
	// those sixty results was unrecoverable.
	//
	//	A PROGRESS INDICATOR THAT REPORTS ONLY HOW FAR A RUN GOT TURNS A KILLED
	//	RUN INTO A TOTAL LOSS. ONE THAT REPORTS WHAT IT HAS FOUND SO FAR TURNS
	//	THE SAME KILL INTO A PARTIAL RESULT.
	//
	// The hook's signature is `func(seed uint64, done, total int)` and carries
	// no verdict, so this cannot be fixed from the caller. Widening it to pass
	// the running census is a change to A7's signed work and is PROPOSED rather
	// than made. The interim remedy is smaller chunks: the runtime's per-job
	// ceiling is variable -- kills observed at 30m, 59m and 96m -- so a chunk
	// sized against the largest observation is one that will eventually be cut.
	c, err := hunt.SweepRaftWithProgress(from, to, opt, func(seed uint64, done, total int) {
		if done == 1 || done%10 == 0 || done == total {
			t.Logf("  seed %d: %d/%d done in %v", seed, done, total, time.Since(start).Round(time.Second))
		}
	})
	if err != nil {
		t.Fatalf("sweep [%d,%d): %v", from, to, err)
	}

	which := "the C++ engine"
	if useModel {
		which = "engine/model (CONTROL)"
	}
	t.Logf("SWEEP [%d,%d) on %s in %v, %d engines opened",
		from, to, which, time.Since(start).Round(time.Second), opened)
	t.Logf("  verdicts   seeds=%d violations=%d inconclusive=%d pass=%d errors=%d",
		c.Seeds, c.Violations, c.Inconclusive, c.Pass, c.Errors)
	t.Logf("  raft       terms=%d elections=%d/%d split-votes=%d no-leader=%d contention=%d",
		c.Terms, c.ElectionsWon, c.ElectionsStart, c.SplitVotes, c.SeedsWithNoLeader, c.SeedsWithContention)
	t.Logf("  snapshots  taken=%d applied=%d transfers=%d", c.SnapshotsTaken, c.SnapshotsApplied, c.TransfersAsked)
	t.Logf("  conf       proposed=%d refused=%d lag-refused=%d recoveries=%d cross-checks=%d",
		c.ConfProposed, c.ConfRefused, c.LagRefused, c.ConfRecoveries, c.ConfCrossChecks)
	t.Logf("  ranges     ranges=%d splits=%d/%d stale-epoch=%d out-of-extent=%d",
		c.Ranges, c.SplitsApplied, c.SplitsProposed, c.StaleEpochRefusals, c.OutOfExtentRefusals)

	if c.Violations > 0 {
		t.Errorf("%d violation(s) on the C++ engine", c.Violations)
	}
	if c.Errors > 0 {
		t.Errorf("%d harness error(s) -- not findings, and they must be explained before the "+
			"sweep's verdict means anything", c.Errors)
	}

	// NON-VACUITY. A sweep with no violations over a workload that did nothing
	// is BUG-046 multiplied by the seed count.
	for _, m := range []struct {
		name string
		got  int
	}{
		{"seeds", c.Seeds},
		{"elections won", c.ElectionsWon},
		{"snapshots taken", int(c.SnapshotsTaken)},
		{"conf changes proposed", c.ConfProposed},
		{"splits applied", c.SplitsApplied},
	} {
		if m.got == 0 {
			t.Errorf("non-vacuity: %s is ZERO across the sweep. The workload is declared to "+
				"exercise it, so a zero here means the sweep's green is about a run that did "+
				"not happen", m.name)
		}
	}
	// A4: an inconclusive is never counted as a pass, AND the cause is quoted
	// with the number. The first version of this test printed the count alone,
	// which is the half A4 explicitly refuses -- a bare "1 inconclusive" is a
	// number nobody can act on and is indistinguishable from a rounding error.
	if c.Inconclusive > 0 {
		t.Logf("  INCONCLUSIVE: %d of %d seeds. Amendment A4 -- never counted as a pass.",
			c.Inconclusive, c.Seeds)
		if len(c.InconclusiveCauses) == 0 {
			t.Errorf("%d inconclusive verdict(s) and NO recorded cause. A4 requires the cause "+
				"beside the number; a count without one cannot be acted on and cannot be "+
				"distinguished from a checker that gave up quietly", c.Inconclusive)
		}
		for i, why := range c.InconclusiveCauses {
			t.Logf("    cause %d: %s", i+1, why)
		}
		if c.Inconclusive > len(c.InconclusiveCauses) {
			t.Logf("    (%d further cause(s) not retained: the census caps the list at 10)",
				c.Inconclusive-len(c.InconclusiveCauses))
		}
	}
}

func envSeed(t *testing.T, key string, def uint64) uint64 {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		t.Fatalf("%s=%q: %v", key, v, err)
	}
	return n
}
