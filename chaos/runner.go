package chaos

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
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
	OK      bool
	Detail  string
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
	return g
}

// Report writes the run, with the caveat attached to the numbers.
func (r Run) Report(w io.Writer, g GateResult) {
	fmt.Fprintf(w, "chaos run\n")
	fmt.Fprintf(w, "  processes  started=%d kills=%d restarts=%d exited-uninvited=%d\n",
		r.Counters.Started, r.Counters.Kills, r.Counters.Restarts, r.Counters.ExitedOther)
	fmt.Fprintf(w, "  client     issued=%d completed=%d failed=%d keys=%d\n",
		r.Ops.Issued, r.Ops.Completed, r.Ops.Failed, r.Ops.Keys)
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

	bad := 0
	for _, v := range r.Verdicts {
		if !v.OK {
			bad++
		}
	}
	fmt.Fprintf(w, "\n  verdicts   %d checker(s), %d violation(s)\n", len(r.Verdicts), bad)
	for _, v := range r.Verdicts {
		if !v.OK {
			fmt.Fprintf(w, "    VIOLATION  %s: %s\n", v.Checker, v.Detail)
		}
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
