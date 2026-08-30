package chaos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/bench"
	"github.com/anshkanyadi/rift/chaos"
	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
)

// I2's numbers, taken against DESIGN-I2 §3.2's ratified thresholds.
//
// # THE SAFETY GATE STANDS ABOVE EVERY NUMBER BELOW
//
// §3.2: *any safety violation under chaos is inadequate at any number; the
// benchmark section does not run.* That is enforced here rather than remembered:
// the chaos phase runs FIRST, its checkers read the history it produced, and a
// violation or a failed gate returns before a single throughput number is taken.
//
//	A NUMBER TAKEN BEFORE ITS PRECONDITION IS CHECKED IS A NUMBER THAT WILL BE
//	QUOTED REGARDLESS OF WHAT THE CHECK SAYS. Once it exists, deleting it takes a
//	decision, and the decision is made by whoever wants the number.
//
// # Parameters, and where each comes from
//
//	E  election timeout   500ms  = Election(10 ticks) x tickInterval(50ms), which
//	                             cmd/riftnode configures. NOT chosen here.
//	K  kill interval      10s    = CLAUDE.md's headline claim. NOT chosen here.
//	R  recovery                  measured; defined in bench.RecoveryAfter
//
// Every threshold below is computed from E and K at run time rather than
// written as a number, so a change to the tick rate moves the thresholds with it
// instead of leaving them stale.
const (
	electionTimeout = 500 * time.Millisecond // E
	killInterval    = 10 * time.Second       // K
)

func TestI2Numbers(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes and runs for about two minutes")
	}
	bin := buildNode(t)
	root := t.TempDir()

	const n = 3
	ports := freePorts(t, n+1)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", ports[n])
	const clientID = 100

	var peerParts []string
	var nodes []*chaos.Node
	for i := 1; i <= n; i++ {
		peerParts = append(peerParts, fmt.Sprintf("%d=127.0.0.1:%d", i, ports[i-1]))
		nodes = append(nodes, &chaos.Node{
			ID: i, Addr: fmt.Sprintf("127.0.0.1:%d", ports[i-1]),
			Dir: filepath.Join(root, fmt.Sprintf("n%d", i)),
		})
	}
	// The node arguments, parameterised by whether the run carries an oracle.
	//
	// BUG-055: the ledger retains 875 bytes per operation, forever, so a cluster
	// carrying it slows down for as long as it runs. The SAFETY phase must have
	// it -- a verdict from an unobserved cluster is an opinion about a run nobody
	// watched, and the gate refuses that combination. The MEASUREMENT phase must
	// not, or it measures the oracle.
	//
	//	THE TWO PHASES ARE DIFFERENT CONFIGURATIONS AND THE REPORT SAYS SO BESIDE
	//	EVERY NUMBER. Running one and quoting it as the other is the whole hazard.
	argsFor := func(observed bool) func(*chaos.Node) []string {
		return func(nd *chaos.Node) []string {
			var others []string
			for _, p := range peerParts {
				if !strings.HasPrefix(p, strconv.Itoa(nd.ID)+"=") {
					others = append(others, p)
				}
			}
			a := []string{
				"--peers", strings.Join(others, ","),
				"--clients", fmt.Sprintf("%d=%s", clientID, clientAddr),
			}
			if !observed {
				a = append(a, "--unobserved")
			}
			return a
		}
	}

	s := chaos.NewWithArgs(bin, nodes, argsFor(true))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()

	addrs := map[sim.NodeID]string{}
	for i := 1; i <= n; i++ {
		addrs[sim.NodeID(i)] = fmt.Sprintf("127.0.0.1:%d", ports[i-1])
	}
	run := chaos.Run{}
	hist := &sim.History{}
	rec := chaos.NewClient(clientID, clock.NewReal(0), hist)
	wc, err := chaos.NewWireClient(clientID, clientAddr, addrs, rec, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer wc.Close()
	waitForLeader(t, nodes, 15*time.Second)
	run.Persistent = clusterIsPersistent(nodes)

	// The key space is wide DELIBERATELY. A4's remedy for a rising inconclusive
	// rate is a smaller problem, not a longer timeout: 512 keys over a run this
	// long keeps each per-key history short enough for porcupine to finish.
	mix := bench.Mix{
		Name: "ycsb-a", ReadPct: 50, Keys: 512, Workers: 8,
		Warmup: 2 * time.Second, OpTimeout: 2 * time.Second,
	}

	// ---- chaos FIRST, because the gate stands above every number ------------
	//
	// The first version of this ran steady state first and printed it before the
	// gate. The gate then failed and refused the THRESHOLDS -- with a throughput
	// number already on the screen.
	//
	//	A GATE THAT LETS ONE NUMBER PAST IS NOT ABOVE THE NUMBERS. §3.2 says the
	//	benchmark section does not run, and "except the first line" is not a
	//	reading of that sentence. Once a number exists, deleting it takes a
	//	decision, and the decision is made by whoever wants the number.
	//
	// So the chaos phase runs first, the gate and the checkers decide, and no
	// measurement is taken or printed until both have passed.
	led := newLedWatch()
	chaosMix := mix
	chaosMix.Name = "ycsb-a/chaos"
	chaosMix.Window = 3 * killInterval
	// The killer goroutine writes these while the load driver runs, so they are
	// guarded. A benchmark harness with a data race reports numbers from a run
	// whose own bookkeeping was undefined.
	var killMu sync.Mutex
	var killAt []time.Duration

	stop := make(chan struct{})
	go func() {
		// The kill schedule starts when the MEASURED window does. bench.Run's
		// buckets are relative to the end of warmup, and a kill timestamped
		// against a different origin cannot be matched to the throughput slice
		// it caused -- which would make every recovery number wrong by exactly
		// the warmup.
		select {
		case <-stop:
			return
		case <-time.After(chaosMix.Warmup):
		}
		t0 := time.Now()
		tk := time.NewTicker(killInterval)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				led.sample(nodes)
				victim := nodes[0]
				aimed := false
				if l := leaderNode(nodes); l != nil {
					victim, aimed = l, true
				}
				delivered, _ := s.Kill(victim)
				if !delivered {
					continue
				}
				killMu.Lock()
				if aimed {
					run.LeaderKills++
				}
				killMu.Unlock()
				killMu.Lock()
				killAt = append(killAt, time.Since(t0))
				run.Faults = append(run.Faults,
					chaos.Fault{At: time.Now(), Kind: "kill", Node: victim.ID})
				killMu.Unlock()
				_ = s.Restart(victim)
				killMu.Lock()
				run.Faults = append(run.Faults,
					chaos.Fault{At: time.Now(), Kind: "restart", Node: victim.ID})
				killMu.Unlock()
				led.sample(nodes)
			}
		}
	}()
	safety := bench.Run(wc, chaosMix)
	close(stop)
	led.sample(nodes)
	killMu.Lock()
	killAt = append([]time.Duration(nil), killAt...)
	killMu.Unlock()

	run.LedTicks = led.total()
	run.Counters = s.Counters()
	run.Ops = rec.Counters()
	run.Corr = rec.Correlation()

	lin := checker.NewLinearizability()
	rep := lin.Check(hist)
	run.Verdicts = append(run.Verdicts, chaos.Verdict{
		Checker: lin.Name(), Outcome: rep.Verdict, Detail: rep.Detail, Consumed: rep.Consumed,
	})

	g := run.Gate(1, 100)
	var out strings.Builder
	run.Report(&out, g)
	out.WriteString("\n" + run.FaultLog())
	t.Log("\n" + out.String())

	// ---- THE SAFETY GATE, above the numbers --------------------------------
	for _, v := range run.Verdicts {
		if v.Outcome == sim.VerdictViolation {
			t.Fatalf("SAFETY VIOLATION (%s: %s). §3.2: any safety violation under chaos is "+
				"inadequate at any number, and the benchmark section does not run.\n%s",
				v.Checker, v.Detail, run.FaultLog())
		}
	}
	if len(g.Failures) > 0 {
		t.Fatalf("the chaos gate failed, so no benchmark number may be taken:\n  %s\n\nnode stderr:\n%s",
			strings.Join(g.Failures, "\n  "), s.Stderr())
	}

	// ---- ONLY NOW: the steady-state baseline, ON A FRESH CLUSTER ------------
	//
	// Two constraints that look opposed and are not.
	//
	//	THE GATE STANDS ABOVE EVERY NUMBER, so the baseline cannot be measured
	//	before the chaos phase has been checked.
	//	A BASELINE MUST DESCRIBE THE CONDITION THE CHAOS RUN STARTED IN, so it
	//	cannot be measured on a cluster that has just absorbed 30 seconds of it.
	//
	// Both are satisfied by restarting the cluster here: after the gate, and from
	// the same state the chaos phase began from. The first version measured the
	// baseline on the post-chaos cluster and reported a steady state SLOWER than
	// the chaos run it was the denominator for -- 68 ops/s against 498 -- which
	// produced a "728% of steady state" result that read as a pass.
	for _, nd := range nodes {
		// KILLED FIRST. Restart waits for the previous launch to be reaped
		// (BUG-054), so calling it on a LIVE node waits out the full five-second
		// timeout and then refuses -- correctly, and the first version of this
		// block did exactly that.
		if _, err := s.Kill(nd); err != nil {
			t.Fatal(err)
		}
	}
	s.StopAll()
	for _, nd := range nodes {
		if err := os.RemoveAll(nd.Dir); err != nil {
			t.Fatal(err)
		}
	}

	// A SECOND CLUSTER, UNOBSERVED. Fresh state, no oracle, and it claims no
	// verdicts -- which is what entitles it to run without one.
	m := chaos.NewWithArgs(bin, nodes, argsFor(false))
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.StopAll()
	waitForLeader(t, nodes, 15*time.Second)

	steadyMix := mix
	steadyMix.Window = 15 * time.Second
	steady := bench.Run(wc, steadyMix)

	// ---- and the chaos numbers, on the same unobserved cluster --------------
	measured := chaos.Run{Unobserved: true, Persistent: clusterIsPersistent(nodes)}
	mLed := newLedWatch()
	mMix := mix
	mMix.Name = "ycsb-a/chaos"
	mMix.Window = 3 * killInterval
	var mKillAt []time.Duration
	var mMu sync.Mutex

	mStop := make(chan struct{})
	go func() {
		select {
		case <-mStop:
			return
		case <-time.After(mMix.Warmup):
		}
		t0 := time.Now()
		tk := time.NewTicker(killInterval)
		defer tk.Stop()
		for {
			select {
			case <-mStop:
				return
			case <-tk.C:
				// A KILL THE WINDOW CANNOT OBSERVE RECOVERING IS NOT A KILL THIS
				// RUN CAN MEASURE. The ticker fires at K, 2K, 3K and the window
				// IS 3K, so the last one landed at the boundary: RecoveryAfter
				// found no slices past it and reported "never recovered", which
				// then read as a defect. It is an artifact of the schedule, so
				// the schedule stops rather than the measurement being excused.
				if time.Since(t0)+5*electionTimeout/2 > mMix.Window {
					return
				}
				mLed.sample(nodes)
				victim := nodes[0]
				aimed := false
				if l := leaderNode(nodes); l != nil {
					victim, aimed = l, true
				}
				delivered, _ := m.Kill(victim)
				if !delivered {
					continue
				}
				mMu.Lock()
				if aimed {
					measured.LeaderKills++
				}
				mMu.Unlock()
				mMu.Lock()
				mKillAt = append(mKillAt, time.Since(t0))
				mMu.Unlock()
				_ = m.Restart(victim)
				mLed.sample(nodes)
			}
		}
	}()
	under := bench.Run(wc, mMix)
	close(mStop)
	mLed.sample(nodes)
	measured.LedTicks = mLed.total()
	measured.Counters = m.Counters()
	// FROM THE LOAD DRIVER, not from the recorder. `rec` accumulates across both
	// phases, so its counters describe the safety run plus this one; this gate is
	// about THIS window. The first version left Ops zero entirely and the gate
	// said so -- "0 operations completed, wanted at least 100" -- which is the
	// arm working on the harness that fed it.
	measured.Ops = chaos.OpCounters{
		Issued:    int(under.OK + under.Fail),
		Completed: int(under.OK),
		Failed:    int(under.Fail),
		Keys:      mMix.Keys,
	}
	mMu.Lock()
	killAt = mKillAt
	mMu.Unlock()

	mg := measured.Gate(1, 100)
	if len(mg.Failures) > 0 {
		t.Fatalf("the MEASUREMENT run's gate failed, so its numbers may not be taken:\n  %s\n\n"+
			"node stderr:\n%s", strings.Join(mg.Failures, "\n  "), m.Stderr())
	}

	// ---- the four thresholds ------------------------------------------------
	var b strings.Builder
	fmt.Fprintf(&b, "\nI2 NUMBERS -- threshold, result, conclusion\n\n")
	fmt.Fprintf(&b, "  parameters   E=%s (Election 10 ticks x %s), K=%s\n",
		electionTimeout, 50*time.Millisecond, killInterval)
	fmt.Fprintf(&b, "  engine       %s\n", engineNameOf(t, s))
	// THE CONFIGURATION, BESIDE THE NUMBERS. Two phases, two clusters, two
	// different things they are entitled to claim.
	fmt.Fprintf(&b, "  safety       ledger=on   -- produced the verdicts and the gate above\n")
	fmt.Fprintf(&b, "  measurement  ledger=OFF  -- claims NO checker evidence (BUG-055)\n")
	fmt.Fprintf(&b, "  workload     %s, %d keys, %d workers, closed loop\n\n",
		mix.Name, mix.Keys, mix.Workers)
	fmt.Fprintf(&b, "  STEADY  [ledger=OFF]  %s   drift=%.2f\n", steady, steady.Drift())
	fmt.Fprintf(&b, "  CHAOS   [ledger=OFF]  %s   drift=%.2f\n", under, under.Drift())
	fmt.Fprintf(&b, "  SAFETY  [ledger=on ]  %s   drift=%.2f  -- the run the verdicts came from,\n"+
		"                        reported so the two configurations can be compared rather than\n"+
		"                        one being read as the other\n\n", safety, safety.Drift())

	// IS THERE A BASELINE AT ALL? Every ratio below divides by steady state, and
	// a mean over a window whose ends differ by more than 2x represents neither
	// end. Checked before any threshold reads it, so a broken baseline produces
	// NOT MEASURED rather than a confident ratio.
	baselineOK := steady.SteadyEnough() && steady.Throughput() > 0
	if !baselineOK {
		fmt.Fprintf(&b, "  BASELINE INVALID: steady-state throughput drifted by %.2fx across its own\n"+
			"  window, so no single number describes it. Every ratio below is reported as\n"+
			"  NOT MEASURED rather than computed against it.\n\n", steady.Drift())
	}
	// And a chaos run that OUTPERFORMS its baseline is not a good result; it is
	// evidence the baseline is wrong, and saying "MET" would bank exactly that.
	if baselineOK && under.Throughput() > steady.Throughput() {
		baselineOK = false
		fmt.Fprintf(&b, "  BASELINE INVALID: throughput under chaos (%.0f) EXCEEDS steady state (%.0f).\n"+
			"  A cluster losing its leader every %s cannot outperform one that never does, so this\n"+
			"  is a fact about the measurement and not about the system.\n\n",
			under.Throughput(), steady.Throughput(), killInterval)
	}

	fail := 0
	report := func(name, threshold, result, conclusion string, ok bool) {
		word := "MET"
		if !ok {
			word = "NOT MET"
			fail++
		}
		// THE CONCLUSION FOLLOWS THE VERDICT. The first version of this printed
		// a fixed conclusion beside a computed verdict, and produced
		// "T1 NOT MET ... conclusion: the cluster recovers inside its own timing
		// parameters" -- a line that contradicts the word above it, which is
		// worse than printing no conclusion at all.
		fmt.Fprintf(&b, "  %-24s %s\n      threshold  %s\n      result     %s\n      conclusion %s\n\n",
			name, word, threshold, result, conclusion)
	}

	// THRESHOLD 1 -- recovery, R <= 2.5E; inadequate if R >= K.
	var worst time.Duration
	nMeasured, missed := 0, 0
	for _, at := range killAt {
		r, ok := bench.RecoveryAfter(under, at, steady.Throughput())
		if !ok {
			missed++
			continue
		}
		nMeasured++
		if r > worst {
			worst = r
		}
	}
	switch {
	case nMeasured == 0:
		report("T1 recovery", fmt.Sprintf("R <= 2.5E = %s", 5*electionTimeout/2),
			fmt.Sprintf("NOT MEASURED: throughput never returned to half of steady state after "+
				"any of %d kills", len(killAt)),
			"Not a large R. §3.2 distinguishes them, and this is the never-recovered case.", false)
	case worst >= killInterval:
		report("T1 recovery", fmt.Sprintf("R <= 2.5E = %s", 5*electionTimeout/2),
			fmt.Sprintf("worst R = %s over %d kills (%d never recovered)", worst, nMeasured, missed),
			"INADEQUATE: R >= K, so the cluster never reaches steady state between kills. "+
				"Read as permanent recovery, not as low throughput.", false)
	case !baselineOK:
		report("T1 recovery", fmt.Sprintf("R <= 2.5E = %s", 5*electionTimeout/2),
			"NOT MEASURED: recovery is defined against the steady-state rate, and there is no valid one",
			"The definition's own input is missing, so any R computed here would be a number "+
				"about an arbitrary target.", false)
	default:
		ok := worst <= 5*electionTimeout/2 && missed == 0
		concl := "The cluster recovers inside its own timing parameters."
		if !ok {
			concl = fmt.Sprintf("The cluster takes longer than its own timing parameters predict "+
				"(2.5E = %s), and the excess is unexplained.", 5*electionTimeout/2)
			if missed > 0 {
				concl = fmt.Sprintf("%d of %d kills never recovered at all, which is not a large R "+
					"-- Section 3.2 distinguishes them.", missed, len(killAt))
			}
		}
		report("T1 recovery", fmt.Sprintf("R <= 2.5E = %s", 5*electionTimeout/2),
			fmt.Sprintf("worst R = %s over %d kills (%d never recovered)", worst, nMeasured, missed),
			concl, ok)
	}

	// THRESHOLD 2 -- chaos throughput, >= (K - 2.5E)/K of steady state.
	wantRatio := float64(killInterval-5*electionTimeout/2) / float64(killInterval)
	gotRatio := 0.0
	if steady.Throughput() > 0 {
		gotRatio = under.Throughput() / steady.Throughput()
	}
	if !baselineOK {
		report("T2 chaos throughput",
			fmt.Sprintf(">= (K - 2.5E)/K = %.1f%% of steady state", wantRatio*100),
			fmt.Sprintf("NOT MEASURED: chaos %.0f ops/s, but the steady-state denominator is invalid",
				under.Throughput()),
			"A ratio against an invalid baseline is not a number about anything.", false)
	} else {
		concl := "Availability under chaos matches what E and K predict."
		if gotRatio < 0.10 {
			concl = "INADEQUATE: below 10%. The system is technically live and practically down."
		} else if gotRatio < wantRatio {
			concl = "Availability is below what E and K predict, and the shortfall is unexplained."
		}
		report("T2 chaos throughput",
			fmt.Sprintf(">= (K - 2.5E)/K = %.1f%% of steady state", wantRatio*100),
			fmt.Sprintf("%.1f%% (%.0f of %.0f ops/s)", gotRatio*100, under.Throughput(), steady.Throughput()),
			concl, gotRatio >= wantRatio)
	}

	// THRESHOLD 3 -- chaos latency, p99 <= 3E and p999 <= 5E.
	p99, p999 := under.Hist.Quantile(0.99), under.Hist.Quantile(0.999)
	t3 := p99 <= 3*electionTimeout && p999 <= 5*electionTimeout
	t3concl := "Chaos p99 is dominated by R, not by steady-state latency -- Section 3.2's amended form."
	if !t3 {
		t3concl = "The tail exceeds what the election timeout predicts, so something other than " +
			"leader election is in it."
	}
	// The op timeout is quoted because it CENSORS the tail: no sample can exceed
	// it, so a p999 sitting at the timeout means the true p999 is unknown and at
	// least this.
	report("T3 chaos latency",
		fmt.Sprintf("p99 <= 3E = %s, p999 <= 5E = %s", 3*electionTimeout, 5*electionTimeout),
		fmt.Sprintf("p99 = %s, p999 = %s (steady p99 = %s; op timeout %s CENSORS the tail at that value)",
			p99, p999, steady.Hist.Quantile(0.99), mix.OpTimeout),
		t3concl, t3)

	// THRESHOLD 4 -- cgo boundary cost. Regression bound against B5, at the same
	// block size. NOT MEASURABLE from this binary and reported as such.
	// T4 IS NOT MEASURED HERE, AND THAT IS THE CORRECT DISPOSITION.
	//
	// It needs a rift_cgo build, which is I1's configuration and not this one.
	// Carried as a named obligation with the configuration it requires, and
	// reported at every run:
	//
	//	A THRESHOLD REPORTED NOT MEASURED IS HONEST. THE SAME THRESHOLD QUIETLY
	//	OMITTED IS NOT -- the reader cannot tell an absent number from a passing
	//	one, and absence is the reading that flatters.
	report("T4 boundary cost",
		"no regression beyond +5pp vs B5 at the same block size",
		// THE REASON IS THE MISSING COMPARISON, NOT THE MISSING TAG.
		//
		// This string said "built without the rift_cgo tag" while the run above it
		// printed engine/riftcgo -- a stale reason surviving the disappearance of
		// its cause, which is GF-61 exactly, inside the line that exists to be
		// honest about an unmeasured threshold.
		fmt.Sprintf("NOT MEASURED: this run crossed the cgo boundary (engine %s) but never "+
			"measured its COST -- there is no native C++ harness result for the same workload "+
			"at the same block size to compare against", engineNameOf(t, s)),
		"UNMEASURED UNDER CHAOS, not passed. The tag is present now; what is missing is the "+
			"native-versus-cgo comparison B5 defines. Carried as an obligation, never as a "+
			"result.", false)

	fmt.Fprintf(&b, "  %d of 4 thresholds not met.\n", fail)
	t.Log(b.String())

	if p := os.Getenv("RIFT_BENCH_REPORT"); p != "" {
		_ = os.WriteFile(p, []byte(out.String()+b.String()), 0o644)
	}
}

// engineNameOf reports which engine produced these numbers, READ FROM THE NODE'S
// OWN STARTUP LINE.
//
// The first version of this returned a hard-coded string. That is the claim
// riftnode already prints, copied to a second place where it cannot be wrong
// today and cannot be right forever:
//
//	A CONSTANT ASSERTING WHAT A BINARY DID IS A CLAIM THAT SURVIVES THE BINARY
//	CHANGING. Building with -tags=rift_cgo would have produced real-engine
//	numbers under a label saying they were not.
func engineNameOf(t *testing.T, s *chaos.Supervisor) string {
	t.Helper()
	for _, line := range strings.Split(s.Stderr(), "\n") {
		i := strings.Index(line, "engine=")
		if i < 0 {
			continue
		}
		// STOP AT THE FIRST FIELD. The startup line carries engine=, ledger= and
		// start=, and taking the rest of the line reported the engine as
		// "engine/riftcgo (the C++ engine), ledger=on, start=fresh" -- three facts
		// under one label, and the other two change between phases.
		rest := line[i+len("engine="):]
		if j := strings.Index(rest, ", ledger="); j >= 0 {
			rest = rest[:j]
		}
		return strings.TrimSpace(rest)
	}
	t.Fatal("no node reported which engine it opened; a number whose engine is unknown is unquotable")
	return ""
}
