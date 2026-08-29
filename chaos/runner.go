package chaos

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/anshkanyadi/rift/sim"
)

// Run is one chaos run: a cluster, a fault schedule inflicted on it, and the
// history it produced.
//
// # WHAT A GREEN HERE MEANS, AND IT IS PRINTED WITH EVERY RESULT
//
// Every green this project has reported has been backed by replay: a seed, a
// bundle, a command anyone can run. This one is not.
//
//	A CHAOS GREEN IS A STATEMENT THAT NOTHING WAS OBSERVED UNDER THE SCHEDULES
//	THAT ACTUALLY OCCURRED. IT IS NOT A STATEMENT THAT NOTHING IS THERE.
//
// The usual recourse -- replay the seed -- does not exist. The schedule was the
// operating system's and the timing was the network's, and neither is
// recoverable, so a second run is a DIFFERENT EXPERIMENT rather than a
// repetition: it can neither confirm the first nor refute it.
//
//	THIS IS A WEAKER CLAIM THAN ANY GREEN TRACK A EVER REPORTED, and Report()
//	prints that sentence next to the numbers rather than leaving it to a limits
//	section further down. A caveat a reader has to go and find is a caveat that
//	travels separately from the number it bounds.
//
// The lane's value is in its REDS: it reaches schedules the simulator's fault
// model does not generate.
type Run struct {
	Counters Counters
	Ops      OpCounters
	Corr     Correlation

	// LeaderKills is how many of the kills removed a node that was leading at
	// the moment it was killed.
	//
	//	A KILL THAT MISSED THE LEADER IS A KILL THE CLUSTER BARELY NOTICED. On
	//	three nodes a round-robin schedule hits the leader a third of the time,
	//	and the resulting run reports the same shape as the experiment CLAUDE.md
	//	names while being a much gentler one. The number is separated from Kills
	//	so the difference cannot be rounded away.
	LeaderKills int

	// LedTicks is the total ticks any node spent leading, summed across nodes.
	// Zero means no node ever led, and a run in which no node ever led observed
	// NOTHING: every checker is green because nothing happened.
	LedTicks uint64
	Faults   []Fault
	Verdicts []Verdict
}

// OpCounters is the client side of the gate. A cluster that started, was
// killed, and served nothing has satisfied every process-level counter while
// producing no history for any checker to read.
type OpCounters struct {
	Issued    int
	Completed int
	Failed    int
	Keys      int
}

// Fault is one thing done to the cluster, recorded as it was inflicted.
//
// This is the closest thing to a seed that exists here. A violation has no
// reproduction, so WHAT WAS DONE TO THE CLUSTER is the whole of its context,
// and it is captured at the moment of infliction rather than reconstructed.
type Fault struct {
	At   time.Time
	Kind string // "kill", "restart"
	Node int
}

// Verdict is one checker's opinion. Verdicts are REPORTED, never gated on.
type Verdict struct {
	Checker string

	// Outcome is THREE-VALUED, and it was a bool until this file was read
	// against Amendment A4.
	//
	//	A BOOL HAS NOWHERE TO PUT AN INCONCLUSIVE, so an inconclusive would have
	//	arrived here as either a pass -- banking a check that never finished --
	//	or a violation, halting feature work over a timeout. Both are wrong in
	//	the specific way A4 was written to forbid, and the type would have made
	//	the wrongness invisible: every call site would have looked correct.
	//
	// sim.Verdict.CountsAsPass is the single site where the banking rule lives,
	// and this field exists so that this package asks it rather than deciding
	// for itself.
	Outcome sim.Verdict
	Detail  string

	// Consumed is how many operations this checker actually READ.
	//
	// It is not decoration and it is not the same number for every checker: a
	// per-key checker reads the operations on its key, a conservation checker
	// reads the transactions, and a checker with a filter reads whatever
	// survived it. The gap between the history's size and this number is where
	// a green hides.
	//
	//	A CHECKER THAT CONSUMED NOTHING AGREES WITH EVERY HYPOTHESIS, and it
	//	reports the same word as a checker that examined ten thousand
	//	operations and found them consistent.
	//
	// So the number travels WITH the verdict rather than being derivable from
	// the run, and a zero is a gate failure below rather than a green.
	Consumed int
}

// GateResult is what the lane exits on.
type GateResult struct {
	Failures []string
}

// Gate applies Ansh's third ruling: gate on counters, report the verdict.
//
// Everything checked here is a deterministic property of the run HAVING
// HAPPENED, so a failure is unambiguous and bisectable in a way a violation is
// not. A non-reproducible red is a bad gate; an ungated lane rots; this is the
// third option.
func (r Run) Gate(minKills, minOps int) GateResult {
	var g GateResult
	add := func(f string, a ...any) { g.Failures = append(g.Failures, fmt.Sprintf(f, a...)) }

	if r.Counters.Started == 0 {
		add("no node was ever started: every number below is about a cluster that does not exist")
	}
	if r.Counters.Kills < minKills {
		add("%d kills, wanted at least %d: THE FAULTS DID NOT LAND. A run that killed nothing and "+
			"observed no violation has observed nothing", r.Counters.Kills, minKills)
	}
	if r.Counters.ExitedOther > 0 {
		add("%d node(s) exited WITHOUT being killed. That is a finding, not a fault we injected, "+
			"and it must be explained before anything else here is read", r.Counters.ExitedOther)
	}
	if r.Ops.Completed < minOps {
		add("%d operations completed, wanted at least %d: the history is too thin for a checker "+
			"to say anything about", r.Ops.Completed, minOps)
	}
	if r.Ops.Completed > 0 && r.Ops.Keys == 0 {
		add("operations completed but no key was touched, so the history is non-vacuous by count " +
			"and vacuous by content")
	}
	// The counters must also be CONSISTENT, not merely large. A run reporting
	// more completions than issues is a broken harness, and it would otherwise
	// sail through every threshold above.
	if r.Ops.Completed+r.Ops.Failed > r.Ops.Issued {
		add("%d completed + %d failed exceeds %d issued: the client's own accounting disagrees "+
			"with itself", r.Ops.Completed, r.Ops.Failed, r.Ops.Issued)
	}
	// A response for an operation nobody issued means the correlation the
	// history rests on is broken. It is gated rather than reported because it is
	// a statement about the RECORD, not about the cluster: if responses are
	// landing on operations that do not exist, no verdict below is a verdict
	// about the run that happened.
	// A cluster that never elected a leader is a cluster that never did
	// anything, and its history is green by construction.
	if r.LedTicks == 0 {
		add("no node ever led: an unled cluster serves nothing, so a green history " +
			"here is a statement about an experiment that did not run")
	}
	if r.Counters.Kills > 0 && r.LeaderKills == 0 {
		add("%d kill(s) landed and NONE of them removed a leader: this is a gentler experiment "+
			"than the one being reported, and the difference is the whole of what a leader kill "+
			"tests", r.Counters.Kills)
	}
	if r.Corr.Unissued > 0 {
		add("%d response(s) arrived for operations the client never issued: the Begin/response/End "+
			"correlation the history rests on is broken, so nothing below describes this run",
			r.Corr.Unissued)
	}
	// And a checker that read nothing is not a checker that agreed.
	for _, v := range r.Verdicts {
		if v.Outcome.CountsAsPass() && v.Consumed == 0 {
			add("checker %q reported green having consumed 0 operations: a checker that examined "+
				"nothing agrees with every hypothesis", v.Checker)
		}
	}
	return g
}

// Report writes the run, with the caveat attached to the numbers.
func (r Run) Report(w io.Writer, g GateResult) {
	fmt.Fprintf(w, "chaos run\n")
	fmt.Fprintf(w, "  processes  started=%d kills=%d restarts=%d exited-uninvited=%d\n",
		r.Counters.Started, r.Counters.Kills, r.Counters.Restarts, r.Counters.ExitedOther)
	fmt.Fprintf(w, "  client     issued=%d completed=%d failed=%d keys=%d\n",
		r.Ops.Issued, r.Ops.Completed, r.Ops.Failed, r.Ops.Keys)
	fmt.Fprintf(w, "  responses  unissued=%d conflicting=%d duplicate=%d late-after-timeout=%d\n",
		r.Corr.Unissued, r.Corr.Conflicting, r.Corr.Duplicate, r.Corr.LateAfterTimeout)
	fmt.Fprintf(w, "  cluster    leader-kills=%d of %d kills, led-ticks=%d\n",
		r.LeaderKills, r.Counters.Kills, r.LedTicks)
	fmt.Fprintf(w, "  faults     %d recorded\n", len(r.Faults))

	// A FAILED GATE SUPPRESSES THE VERDICTS AS REPORTABLE, and this is a
	// decision rather than an implementation detail.
	//
	//	A CHECKER'S OPINION ABOUT A RUN THAT DID NOT OCCUR AS DESCRIBED IS NOT A
	//	RESULT, and printing it beside a gate failure invites reading the verdict
	//	and skipping the gate.
	//
	// The verdicts are still PRINTED -- suppressing them entirely would hide
	// information from whoever is debugging the gate failure -- but they are
	// printed under a line that says they are not reportable, so the two can
	// never be quoted apart.
	if len(g.Failures) > 0 {
		fmt.Fprintf(w, "\n  GATE FAILED -- the run did not happen as described:\n")
		for _, f := range g.Failures {
			fmt.Fprintf(w, "    - %s\n", f)
		}
		fmt.Fprintf(w, "\n  The verdicts below are NOT reportable: a checker's opinion about a run\n")
		fmt.Fprintf(w, "  that did not occur as described is an opinion about nothing.\n")
	}

	bad, unsure := 0, 0
	for _, v := range r.Verdicts {
		switch v.Outcome {
		case sim.VerdictViolation:
			bad++
		case sim.VerdictInconclusive:
			unsure++
		}
	}
	// EVERY verdict prints, with the number of operations it consumed, and the
	// greens print too. A report that lists only violations cannot distinguish
	// "four checkers examined this history and found nothing" from "four
	// checkers were configured and three of them never ran".
	// THE INCONCLUSIVE COUNT IS QUOTED BESIDE THE VIOLATION COUNT, never folded
	// into it and never omitted when it is zero. A4: the public claim quotes the
	// inconclusive rate alongside the violation count, and a field that is
	// printed only when nonzero is a field a reader learns to stop looking for.
	fmt.Fprintf(w, "\n  verdicts   %d checker(s), %d violation(s), %d inconclusive\n",
		len(r.Verdicts), bad, unsure)
	for _, v := range r.Verdicts {
		word := "         "
		switch v.Outcome {
		case sim.VerdictPass:
			word = "pass     "
		case sim.VerdictViolation:
			word = "VIOLATION"
		case sim.VerdictInconclusive:
			word = "INCONCL. "
		case sim.VerdictUnset:
			word = "UNSET    "
		}
		fmt.Fprintf(w, "    %s  %-24s consumed=%-7d %s\n", word, v.Checker, v.Consumed, v.Detail)
	}

	if bad > 0 {
		// THE DOCUMENTED WORKFLOW, printed rather than remembered. Ansh at I2:
		// a chaos violation's disposition is a workflow, not a decision made in
		// the moment.
		fmt.Fprintf(w, `
  A CHAOS VIOLATION HAS NO REPRODUCTION. Its disposition is a workflow:

    1. CAPTURE the history and the fault log. They are the artifact; there is
       nothing else, and nothing regenerates them.
    2. ASK WHETHER IT REPRODUCES IN SIM. A schedule that produces the same
       violation makes it a seed and returns it to the world where every other
       instrument in this repository works.
    3. IF IT DOES NOT REPRODUCE, that is a finding about the SIMULATOR'S FAULT
       MODEL -- which was supposed to cover this -- and it goes in the record as
       one rather than being dropped.

  A CHAOS RED IS NEVER CLOSED BY RE-RUNNING UNTIL IT GOES AWAY.

  AND THE SAFETY GATE STANDS ABOVE THE THRESHOLDS: any violation and the
  benchmark section does not run. There is no throughput that redeems a number
  taken from a run that broke an invariant.
`)
		return
	}

	if unsure > 0 {
		// AN INCONCLUSIVE IS NOT A GREEN, so the green block below does not
		// print. A4 names the remedy too, and it is not a longer timeout:
		// shrink the per-run history window, then partition harder per key.
		fmt.Fprintf(w, `
  NOT GREEN: %d checker(s) did not finish. An inconclusive is not a pass, and
  this run banks nothing. The remedy is a SMALLER PROBLEM -- shorter history
  windows, harder per-key partitioning -- and never a longer timeout, a weaker
  model, or a smaller operation set.
`, unsure)
		return
	}

	if len(g.Failures) == 0 {
		// THE CAVEAT IS CARRIED IN THIS OUTPUT, not in a limits section further
		// down, and TestAGreenCarriesItsCaveatInTheSameOutput enforces that.
		//
		//	A CAVEAT A READER HAS TO GO AND FIND IS A CAVEAT THAT TRAVELS
		//	SEPARATELY FROM THE NUMBER IT BOUNDS.
		//
		// Every honest-limits section in this repository can be read past. This
		// one cannot: the limit is in the same bytes as the claim, so quoting
		// the result without it takes deliberate effort rather than mere haste.
		// That is the strongest form of the discipline available -- it makes the
		// limit INSEPARABLE from the claim rather than adjacent to it.
		fmt.Fprintf(w, `
  GREEN, AND HERE IS WHAT THAT MEANS. Nothing was observed under the schedules
  that ACTUALLY OCCURRED. It is not a statement that nothing is there.

  There is no seed. The schedule was the operating system's and the timing was
  the network's, and neither is recoverable, so a second run is a DIFFERENT
  EXPERIMENT rather than a repetition -- it can neither confirm this result nor
  refute it.

  THIS IS A WEAKER CLAIM THAN ANY GREEN TRACK A EVER REPORTED, where every green
  was backed by a seed anyone could replay. Quote it with this sentence attached.
`)
	}
}

// FaultLog renders the schedule that was inflicted, which is a violation's only
// context. Sorted by time, because the order faults landed in is the thing a
// reader is trying to reconstruct.
func (r Run) FaultLog() string {
	f := append([]Fault(nil), r.Faults...)
	sort.Slice(f, func(i, j int) bool { return f[i].At.Before(f[j].At) })
	var b strings.Builder
	if len(f) > 0 {
		t0 := f[0].At
		for _, x := range f {
			fmt.Fprintf(&b, "%8.3fs  %-8s node %d\n", x.At.Sub(t0).Seconds(), x.Kind, x.Node)
		}
	}
	return b.String()
}
