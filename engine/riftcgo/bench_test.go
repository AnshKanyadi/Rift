//go:build rift_cgo

package riftcgo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
)

// B5.5: THE TABLE. Three engines, one workload each, assembled by ONE piece of
// code in ONE process on ONE machine.
//
// That last part is the point. The boundary cost is the DIFFERENCE between the
// native column and the cgo column, and a difference between two numbers taken
// at different times on differently-loaded machines is not a cost -- it is two
// unrelated measurements subtracted. So this test shells out to rift_bench for
// the native column rather than reading a number someone recorded earlier, and
// runs all three back to back.
//
// IT IS A TEST AND NOT A go test -bench. testing.B chooses its own iteration
// count per benchmark, which is exactly what must NOT vary here: "the same
// workload" means the same n, and a framework that scales n per engine would
// hand back three numbers for three workloads.

const (
	benchKeyBytes   = 16
	benchValueBytes = 100
)

// bench64 is splitmix64, the same eight lines as engine-cpp/src/bench_keys.h.
// The pinned outputs below are asserted on both sides, so a divergence is a
// test failure rather than a number that is quietly wrong.
func bench64(seed, i uint64) uint64 {
	z := seed + (i+1)*0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func benchKey(seed, i uint64, width int) []byte {
	return []byte(fmt.Sprintf("%0*d", width, bench64(seed, i)%100000000000000))
}

func TestTheKeyStreamMatchesTheNativeHarness(t *testing.T) {
	// PINNED IN BOTH LANGUAGES. Without this the two columns could be running
	// different key orders and the difference between them would be partly a
	// difference in luck -- which is unfalsifiable and would look like a
	// boundary cost.
	want := []uint64{0x910A2DEC89025CC1, 0xBEEB8DA1658EEC67, 0xF893A2EEFB32555E}
	for i, w := range want {
		if got := bench64(1, uint64(i)); got != w {
			t.Fatalf("bench64(1, %d) = %#016x, want %#016x -- the Go and C++ key "+
				"streams have diverged and no column is comparable to another", i, got, w)
		}
	}
}

type benchResult struct {
	engine   string
	workload string
	n        uint64
	batch    int
	nanos    int64
}

func (r benchResult) nsPerOp() float64 { return float64(r.nanos) / float64(r.n) }

func nativeBench(t *testing.T, bin, workload string, n uint64, batch, block int, seed uint64) benchResult {
	t.Helper()
	out, err := exec.Command(bin, workload, strconv.FormatUint(n, 10),
		strconv.Itoa(batch), strconv.Itoa(block), strconv.FormatUint(seed, 10)).Output()
	if err != nil {
		t.Fatalf("rift_bench %s: %v", workload, err)
	}
	m := regexp.MustCompile(`ns=(\d+)`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("rift_bench printed no result: %q", out)
	}
	ns, _ := strconv.ParseInt(string(m[1]), 10, 64)
	return benchResult{"C++ native", workload, n, batch, ns}
}

// runGo drives either engine through the frozen interface. ONE function for
// both, so the model column and the cgo column cannot drift into being
// different loops.
func runGo(t *testing.T, name string, open func(dir string) engine.Engine,
	workload string, n uint64, batch, block int, seed uint64) benchResult {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name+workload)
	e := open(dir)
	defer e.Close()
	// THE MODEL'S DURABILITY MUST BE DRIVEN OR ITS MEMORY IS O(n^2).
	//
	// applyTo copies the whole entry slice per apply and `pending` retains every
	// version until durability advances -- which is correct for a reference
	// engine (a crash drops pending, which is the whole mechanism) and is not a
	// cost this table is comparing. In a simulated run something always advances
	// it; a bench that never did was holding 50,000 versions of up to 50,000
	// entries alive and the test process was OOM-killed at n=50,000.
	//
	// Driving it here is the harness doing its job, not the model being helped:
	// the C++ engine's equivalent -- flushing the memtable -- happens on its own.
	drain := func(seq engine.SeqNum) {}
	if m, ok := e.(*model.DB); ok {
		drain = func(seq engine.SeqNum) { m.AdvanceDurable(seq) }
	}

	value := make([]byte, benchValueBytes)
	for i := range value {
		value[i] = 'v'
	}

	// COUNTED IN APPLIES, NOT IN OPERATIONS. The first version keyed the drain
	// off the operation index AND the batch boundary, and the two coincide only
	// at batch=1: at batch=8 the condition i%1024==0 never lands on an apply, so
	// nothing drained and the process was killed again at a different row. A
	// periodic action inside a conditional block must count the thing that
	// actually happens, not the thing being iterated.
	applies := 0
	maybeDrain := func(seq engine.SeqNum) {
		applies++
		if applies%16 == 0 {
			drain(seq)
		}
	}

	fill := func(timed bool) int64 {
		start := time.Now()
		b := engine.NewBatch()
		in := 0
		for i := uint64(0); i < n; i++ {
			b.Set(benchKey(seed, i, benchKeyBytes), value)
			in++
			if in == batch {
				seq, err := e.Apply(b, false)
				if err != nil {
					t.Fatal(err)
				}
				maybeDrain(seq)
				b, in = engine.NewBatch(), 0
			}
		}
		if in != 0 {
			if _, err := e.Apply(b, false); err != nil {
				t.Fatal(err)
			}
		}
		return int64(time.Since(start))
	}

	if workload != "fillrandom" {
		// NO SYNC AFTER THE PRE-FILL, in any of the three columns.
		//
		// engine.Engine has no Sync: durability is DRIVEN in this project, and
		// the model has AdvanceDurable rather than a Sync of its own. A column
		// that synced where another could not would differ in work done outside
		// its own timed region, and the pre-fill's whole job is to leave all
		// three engines holding the same data by the same route.
		fill(false)
	}

	var nanos int64
	switch workload {
	case "fillrandom":
		nanos = fill(true)
	case "readrandom":
		start := time.Now()
		for i := uint64(0); i < n; i++ {
			k := benchKey(seed, bench64(seed^0x5eed, i)%n, benchKeyBytes)
			_, _ = e.Get(k)
		}
		nanos = int64(time.Since(start))
	case "mixed":
		start := time.Now()
		b := engine.NewBatch()
		in := 0
		for i := uint64(0); i < n; i++ {
			k := benchKey(seed, bench64(seed^0x5eed, i)%n, benchKeyBytes)
			if bench64(seed^0x111d, i)&1 == 0 {
				_, _ = e.Get(k)
			} else {
				b.Set(k, value)
				in++
				if in == batch {
					seq, err := e.Apply(b, false)
					if err != nil {
						t.Fatal(err)
					}
					maybeDrain(seq)
					b, in = engine.NewBatch(), 0
				}
			}
		}
		if in != 0 {
			if _, err := e.Apply(b, false); err != nil {
				t.Fatal(err)
			}
		}
		nanos = int64(time.Since(start))
	case "scan":
		original := DefaultBlock
		DefaultBlock = block
		defer func() { DefaultBlock = original }()
		start := time.Now()
		touched := 0
		it := e.NewIter(engine.IterOptions{})
		for ok := it.First(); ok; ok = it.Next() {
			touched += len(it.Key()) + len(it.Value())
		}
		_ = it.Close()
		nanos = int64(time.Since(start))
		if touched == 0 {
			t.Fatal("the scan read nothing, so its timing measures an empty loop")
		}
	}
	return benchResult{name, workload, n, batch, nanos}
}

func TestBenchmarkTable(t *testing.T) {
	if os.Getenv("RIFT_BENCH") == "" {
		t.Skip("set RIFT_BENCH=1 to take numbers; this is a measurement, not a check")
	}
	// THE LANE NAMES THE BINARY, because it is the lane that knows which build
	// directory holds a RELEASE one. Defaulting to the Debug tree would make
	// `go test` produce a table silently describing -O0.
	abs := os.Getenv("RIFT_BENCH_BIN")
	var err error
	if abs == "" {
		abs, err = filepath.Abs(filepath.Join("..", "..", "engine-cpp", "build", "bench", "rift_bench"))
	}
	if err != nil || !fileExists(abs) {
		t.Fatalf("rift_bench not built at %q -- run `make cpp-bench`. Without the native "+
			"column there is no boundary cost to report, only two numbers. (%v)", abs, err)
	}

	n := uint64(50000)
	if v := os.Getenv("RIFT_BENCH_N"); v != "" {
		n, _ = strconv.ParseUint(v, 10, 64)
	}
	const seed = 1
	reps := 3
	if v := os.Getenv("RIFT_BENCH_REPS"); v != "" {
		reps, _ = strconv.Atoi(v)
	}
	openCgo := func(dir string) engine.Engine {
		db, err := Open(dir, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	openModel := func(string) engine.Engine { return model.New() }

	fmt.Printf("\n%-12s %-7s %-6s %12s %12s %12s %10s\n",
		"workload", "batch", "block", "model ns/op", "native ns/op", "cgo ns/op", "boundary")
	for _, w := range []string{"fillrandom", "readrandom", "mixed", "scan"} {
		for _, batch := range []int{1, 8, 64, 512} {
			for _, block := range blocksFor(w) {
				// MEDIAN OF THREE, NOT ONE RUN.
				//
				// The first table taken here reported boundary costs of -23%
				// and -16% -- the cgo column beating native, which cannot be
				// true, since the cgo column does everything the native column
				// does and then crosses a boundary. Those were variance, and a
				// single run cannot tell variance from a finding. Three runs
				// and a median cannot either, but they make the impossible
				// numbers stop appearing, which is the minimum bar for
				// publishing any of them.
				nat := medianOf(t, reps, func() float64 {
					return nativeBench(t, abs, w, n, batch, block, seed).nsPerOp()
				})
				cg := medianOf(t, reps, func() float64 {
					return runGo(t, "cgo", openCgo, w, n, batch, block, seed).nsPerOp()
				})
				mod := medianOf(t, reps, func() float64 {
					return runGo(t, "model", openModel, w, n, batch, block, seed).nsPerOp()
				})
				fmt.Printf("%-12s %-7d %-6d %12.1f %12.1f %12.1f %9.1f%%\n",
					w, batch, block, mod, nat, cg, 100*(cg-nat)/nat)
			}
		}
	}
	fmt.Println()
}

// The block size only changes anything for iteration; sweeping it over point
// workloads would print identical rows and invite them to be read as a finding.
//
// AND THE REVERSE, WHICH THE TABLE MUST NOT HIDE: `batch` does not affect a
// scan either -- it changes only the pre-fill, which is outside the timed
// region. So scan's four batch rows at a given block size are REPLICATES of one
// measurement, not four measurements. They are kept deliberately: the spread
// across them is the only run-to-run variance estimate this table has, and a
// table that reports a boundary cost to one decimal without one is overstating
// what it knows.
func blocksFor(w string) []int {
	if w == "scan" {
		return []int{1, 8, 64, 512}
	}
	return []int{64}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func medianOf(t *testing.T, reps int, f func() float64) float64 {
	t.Helper()
	if reps < 1 {
		reps = 1
	}
	xs := make([]float64, reps)
	for i := range xs {
		xs[i] = f()
	}
	sort.Float64s(xs)
	return xs[len(xs)/2]
}
