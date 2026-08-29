package hunt_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// The exit run, split.
//
// # Why splitting is legitimate, stated rather than left implicit
//
// Ansh's ruling at A6: 25,000 seeds may be run as contiguous non-overlapping
// ranges in separate invocations, aggregated, **provided the union is provably
// the full set**. The argument it rests on is a property of the harness rather
// than a convenience:
//
//	Seeds are independent by construction. `MaterializeRaft(seed)` derives a
//	whole plan from the seed alone, and the PLAN is the reproduction unit -- a
//	bundle carries it, `simctl replay` re-executes it, and nothing about a run
//	depends on which seeds ran before it in the same process. So a seed's
//	verdict does not depend on which invocation ran it.
//
// What splitting is NOT allowed to do is lose seeds or double-count them, and
// "the union is the full set" is therefore asserted rather than assumed:
// `TestRaftExitAggregate` requires the shards to sort into a contiguous,
// non-overlapping cover of exactly [0, RAFT_TOTAL).
//
// # Why this also makes it finishable
//
// A6's exit run MEASURED 8.4 s/seed and about 58 CPU-hours (commit cb4937d);
// A7's shape measured 7.5 s/seed across eight shards. Split across N processes
// it is the same CPU-hours and roughly 1/N of the wall clock, which is the
// difference between a run that completes and a run that is always still going.
//
// # The number that used to be here was a PLANNING figure, and that is a hazard
//
// This comment read "25,000 seeds at A6's 3.75 s/seed". 3.75 was what A6's exit
// run was planned against; the run itself measured 8.4 and said so in its own
// commit message. The stale figure survived here anyway -- and then A7's shards
// printed 3.75 s/seed, because a doubled seed count halved a true 7.5.
//
// A wrong number that contradicts expectation gets questioned. One that CONFIRMS
// a stale expectation is invisible, so the most dangerous form of a wrong number
// is one that agrees with what you were already going to believe. A planning
// figure left in a comment becomes the expectation a real measurement is checked
// against, so it should have been deleted the day the first real rate was taken.
// Every number above is measured, and each says which run measured it.

// shardEnv reads a shard's range and output path.
func shardEnv(t *testing.T) (from, to uint64, out string) {
	t.Helper()
	get := func(k string) uint64 {
		v := os.Getenv(k)
		if v == "" {
			t.Skipf("%s unset: this is the sharded exit run, driven by scripts/exit-run.sh", k)
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("%s=%q: %v", k, v, err)
		}
		return n
	}
	from, to = get("RAFT_FROM"), get("RAFT_TO")
	out = os.Getenv("RAFT_SHARD_OUT")
	if out == "" {
		t.Fatal("RAFT_SHARD_OUT unset: a shard that writes no census cannot be aggregated, and an " +
			"aggregate over shards that did not report is the vacuous-green class with a seed " +
			"count attached")
	}
	if from >= to {
		t.Fatalf("shard range [%d,%d) is empty", from, to)
	}
	return from, to, out
}

// ShardCensus is one invocation's contribution, with the range it covered.
//
// The range is carried WITH the counts, not beside them, because the aggregate's
// whole claim is about coverage and a census that does not say what it covered
// cannot be checked for it.
type ShardCensus struct {
	From   uint64          `json:"from"`
	To     uint64          `json:"to"`
	Commit string          `json:"commit"`
	Census hunt.RaftCensus `json:"census"`
	Wall   string          `json:"wall"`
}

// TestRaftExitShard runs one contiguous slice of the exit run.
//
// It asserts only what is true of a slice: zero violations, and no harness
// errors. Everything that is a property of the WHOLE run -- the inconclusive
// rate, every mechanism having fired somewhere -- belongs to the aggregate,
// because a slice that happened not to take a snapshot has not failed.
func TestRaftExitShard(t *testing.T) {
	from, to, out := shardEnv(t)

	// # The progress line, written from the sweep's own loop
	//
	// A shard writes its census on completion and nothing before it, so a
	// running shard and a hung one looked identical for six and a half hours --
	// and a shard that DIED was indistinguishable from one still going, which is
	// the case that actually cost this project an afternoon.
	//
	// The write happens HERE rather than in hunt/ so the sweep package stays free
	// of I/O, and it is driven by a synchronous callback rather than by a ticker
	// or a goroutine: that loop is the deterministic simulator's own, and a
	// second thread of control near it trades an observability gap for a
	// determinism risk.
	progress := out + ".progress"
	_ = os.Remove(progress)
	start := time.Now()
	c, err := hunt.SweepRaftWithProgress(from, to, hunt.CurrentOptions(),
		func(seed uint64, done, total int, running hunt.RaftCensus) {
			el := time.Since(start)
			line := fmt.Sprintf("shard [%d,%d) %s: seed %d of %d done, %s elapsed, %.2f s/seed\n",
				from, to, hunt.CurrentShapeName(), done, total,
				el.Round(time.Second), el.Seconds()/float64(done))
			// Best effort on purpose: a progress line that cannot be written
			// must never take down the run it is reporting on.
			_ = os.WriteFile(progress, []byte(line), 0o644)
		})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("shard [%d,%d): %v", from, to, err)
	}
	t.Logf("shard [%d,%d): %d seeds in %s (%.2f s/seed), pass=%d violation=%d inconclusive=%d",
		from, to, c.Seeds, elapsed.Round(time.Second),
		elapsed.Seconds()/float64(c.Seeds), c.Pass, c.Violations, c.Inconclusive)

	sc := ShardCensus{From: from, To: to, Commit: os.Getenv("RAFT_COMMIT"),
		Census: c, Wall: elapsed.Round(time.Second).String()}
	b, err := json.MarshalIndent(sc, "", " ")
	if err != nil {
		t.Fatalf("encoding the shard census: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}

	if c.Violations != 0 {
		t.Errorf("SAFETY VIOLATION: %d in [%d,%d); first at seed %d",
			c.Violations, from, to, c.FirstViolation)
	}
	if c.Errors != 0 {
		t.Errorf("%d harness errors in [%d,%d)", c.Errors, from, to)
	}
}

// TestRaftExitAggregate unions the shards and applies the exit criteria to the
// whole.
//
// # The coverage assertion is the point
//
// Splitting is only sound if the pieces are a partition. This requires the
// shards to sort into a contiguous cover of exactly [0, total) with no gap and
// no overlap, so "25,000 seeds" is a checked claim rather than a sum of numbers
// somebody hoped were disjoint.
func TestRaftExitAggregate(t *testing.T) {
	dir := os.Getenv("RAFT_SHARD_DIR")
	if dir == "" {
		t.Skip("RAFT_SHARD_DIR unset: this aggregates a sharded exit run")
	}
	total := uint64(25000)
	if v := os.Getenv("RAFT_TOTAL"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("RAFT_TOTAL: %v", err)
		}
		total = n
	}

	// # A relative shard directory is resolved against the REPO ROOT
	//
	// `go test` runs with the package directory as its working directory, so a
	// relative RAFT_SHARD_DIR written by somebody standing at the repo root
	// silently resolves to sim/hunt/<dir> and finds nothing. "Found nothing" and
	// "the shards did not run" are the same message, which is the failure that
	// wastes an afternoon.
	paths, err := filepath.Glob(filepath.Join(dir, "shard-*.json"))
	if err != nil {
		t.Fatalf("globbing %s: %v", dir, err)
	}
	if len(paths) == 0 && !filepath.IsAbs(dir) {
		alt := filepath.Join("..", "..", dir)
		if p2, err2 := filepath.Glob(filepath.Join(alt, "shard-*.json")); err2 == nil && len(p2) > 0 {
			dir, paths = alt, p2
		}
	}
	if len(paths) == 0 {
		t.Fatalf("no shard census in %s: an aggregate over nothing is not an exit run", dir)
	}
	var shards []ShardCensus
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		var sc ShardCensus
		if err := json.Unmarshal(b, &sc); err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		shards = append(shards, sc)
	}
	sort.Slice(shards, func(i, j int) bool { return shards[i].From < shards[j].From })

	// # Coverage: contiguous, non-overlapping, and exactly the full range
	next := uint64(0)
	commit := shards[0].Commit
	for _, s := range shards {
		switch {
		case s.From != next:
			t.Fatalf("shard boundaries do not meet: expected the next shard to start at %d, and "+
				"[%d,%d) does not. A gap loses seeds and an overlap counts them twice; either way "+
				"the aggregate is not 25,000 distinct seeds", next, s.From, s.To)
		case s.To <= s.From:
			t.Fatalf("shard [%d,%d) is empty or inverted", s.From, s.To)
		case uint64(s.Census.Seeds) != s.To-s.From:
			t.Fatalf("shard [%d,%d) covers %d seeds and reported %d: a shard that did not finish "+
				"its range cannot be aggregated as if it had", s.From, s.To, s.To-s.From,
				s.Census.Seeds)
		case s.Commit != commit:
			t.Fatalf("shard [%d,%d) ran at commit %q and an earlier one at %q. An aggregate across "+
				"two builds is two experiments reported as one", s.From, s.To, s.Commit, commit)
		}
		next = s.To
	}
	if next != total {
		t.Fatalf("the shards cover [0,%d) and the exit run is [0,%d). Seeds %d..%d were never run, "+
			"and reporting the total as %d would be a claim about seeds nobody executed",
			next, total, next, total-1, total)
	}

	c := sumShards(shards)
	t.Logf("aggregate:    %d shards covering [0,%d) at commit %s", len(shards), total, commit)
	for _, s := range shards {
		t.Logf("  [%6d,%6d)  %s", s.From, s.To, s.Wall)
	}
	reportExitCensus(t, c)
	assertExitCriteria(t, c)
}

// sumShards adds the shard censuses. Maxima are taken as maxima and totals as
// totals, which is the only part of this that can be got wrong silently.
func sumShards(shards []ShardCensus) hunt.RaftCensus {
	var c hunt.RaftCensus
	for _, s := range shards {
		c = hunt.AddCensus(c, s.Census)
	}
	return c
}

// TestAddCensusCoversEveryField is the guard the hand-written summation needs.
//
// # Why a reflection test over a hand-written function
//
// AddCensus is written out field by field on purpose: Terms and Ranges are
// maxima, FirstViolation is the earliest, and everything else is a total, and a
// reflective sum would erase that distinction. The cost of writing it out is
// that a counter added later is silently left at zero -- a number that reads LOW,
// which is the exact shape of every count this project has learned not to trust.
//
// So reflection does the part it is good at: every numeric field is set to a
// distinct nonzero value in both operands, and the sum must move for all of
// them. It cannot tell a max from a total, and does not try; it tells you the
// field was not forgotten.
func TestAddCensusCoversEveryField(t *testing.T) {
	var a, b hunt.RaftCensus
	va, vb := reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem()
	typ := va.Type()

	n := 0
	for i := 0; i < typ.NumField(); i++ {
		switch va.Field(i).Kind() {
		case reflect.Int:
			n++
			va.Field(i).SetInt(int64(n))
			vb.Field(i).SetInt(int64(100 + n))
		case reflect.Uint64:
			n++
			va.Field(i).SetUint(uint64(n))
			vb.Field(i).SetUint(uint64(100 + n))
		}
	}

	// # Exemptions are by NAME and carry a reason
	//
	// A field that is deliberately not a total has to say so here, so that
	// "AddCensus does not add this" is a decision somebody made rather than one
	// nobody noticed. FirstViolation is the earliest seed any shard violated at,
	// and it moves only when FoundAViolation is set -- which it is not in this
	// synthetic pair, so it correctly does not move.
	notTotals := map[string]string{
		"FirstViolation": "the earliest violating seed, gated on FoundAViolation, not a sum",
	}

	sum := reflect.ValueOf(hunt.AddCensus(a, b))
	var missed []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if _, exempt := notTotals[f.Name]; exempt {
			continue
		}
		switch va.Field(i).Kind() {
		case reflect.Int:
			if sum.Field(i).Int() == va.Field(i).Int() {
				missed = append(missed, f.Name)
			}
		case reflect.Uint64:
			if sum.Field(i).Uint() == va.Field(i).Uint() && vb.Field(i).Uint() > va.Field(i).Uint() {
				missed = append(missed, f.Name)
			}
		}
	}
	if len(missed) > 0 {
		t.Errorf("AddCensus leaves %d field(s) at the first operand's value: %v.\n"+
			"  A shard census that is not folded in reads LOW in the aggregate, and a count that "+
			"reads low is indistinguishable from a mechanism that did not fire -- which is the "+
			"one thing every assertion in the exit run is trying to tell apart.", len(missed), missed)
	}
	if n < 40 {
		t.Errorf("only %d numeric fields found on RaftCensus; this test is not looking at the "+
			"struct it thinks it is", n)
	}
}
