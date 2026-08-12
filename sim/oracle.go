package sim

import (
	"fmt"
	"strings"

	"github.com/anshkanyadi/rift/clock"
)

// Verdict is what a checker concluded. Three outcomes, never two.
//
// **Inconclusive is not a shade of pass.** A linearizability check that hit its
// timeout, or a checker handed a history too short to mean anything, has
// established nothing — and "zero violations across N seeds" is quietly false
// if a fraction of those seeds never finished checking (Amendment A4). SOAK.md
// carries an explicit inconclusive column and the public claim quotes the rate
// beside the violation count.
//
// When the inconclusive rate rises the response is to make the *problem*
// smaller — shorter history windows, harder per-key partitioning — never to
// make the oracle weaker. Not the timeout, not the model, not the operation
// set.
type Verdict uint8

const (
	// VerdictUnset is the zero value and is rejected. A checker that forgot to
	// set a verdict must not read as a pass.
	VerdictUnset Verdict = iota
	VerdictPass
	VerdictViolation
	VerdictInconclusive
	numVerdicts
)

func (v Verdict) String() string {
	switch v {
	case VerdictUnset:
		return "unset"
	case VerdictPass:
		return "pass"
	case VerdictViolation:
		return "violation"
	case VerdictInconclusive:
		return "inconclusive"
	case numVerdicts:
		return "invalid"
	}
	return "unknown"
}

// CountsAsPass is the single site where the banking rule lives, in the shape
// blessed for OutcomeKind: policy on one method of the type it belongs to,
// never at call sites. Adding a verdict forces a decision here rather than
// defaulting to "sure, that's fine" wherever it was forgotten.
func (v Verdict) CountsAsPass() bool {
	switch v {
	case VerdictPass:
		return true
	case VerdictUnset, VerdictViolation, VerdictInconclusive:
		return false
	case numVerdicts:
		return false
	}
	return false
}

// Report is a checker's conclusion about one run.
type Report struct {
	Checker string
	Verdict Verdict

	// Detail says what happened, in a sentence an investigator can act on.
	Detail string

	// At and Step locate the first violation. A dump that says a run failed
	// without saying where costs an afternoon before it says anything at all.
	At   clock.Instant
	Step uint64

	// Consumed is how many operations the checker actually examined, and Min is
	// the floor it required. Reported even on a pass, because a checker that
	// silently consumed nothing is the failure mode this field exists to
	// expose.
	Consumed int
	Min      int
}

func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s (%d ops, floor %d)", r.Checker, r.Verdict, r.Consumed, r.Min)
	if r.Detail != "" {
		fmt.Fprintf(&b, " -- %s", r.Detail)
	}
	if r.Verdict == VerdictViolation {
		fmt.Fprintf(&b, " [first at instant %d, step %d]", int64(r.At), r.Step)
	}
	return b.String()
}

// Oracle is a checker that watches a run as it happens.
//
// OnStep returning a non-nil violation halts the run: the loop stops and
// reports OutcomeHalted, so a violation is a *result* rather than a duration
// and never counts toward soak hours.
//
// Two rules bind every safety oracle here:
//
//   - It judges from independently observed history — the harness's op log,
//     the client-observed responses, plan-derived truth — never from the
//     system's own account of itself.
//   - It never counts a timeout or an unavailability as a violation. A
//     partitioned cluster that stops answering is behaving correctly, and an
//     oracle that scores that as a safety failure trains everyone to ignore it.
//     Liveness is measured separately and never gates a safety claim.
type Oracle interface {
	Name() string

	// OnStep is called after each event fires. A non-nil return halts the run.
	// Expensive checkers declare the kinds they care about via Interested and
	// are not called for the rest.
	OnStep(view View, ev Event) *Violation

	// Interested reports whether this oracle wants to see a kind of event.
	Interested(k Kind) bool
}

// View is the read-only façade an oracle sees. It deliberately offers no way to
// reach node state: the interface *is* the independence rule.
type View interface {
	Now() clock.Instant
	Steps() uint64
	Down(NodeID) bool
}

// Violation is a safety failure found mid-run.
type Violation struct {
	Checker string
	Detail  string
	At      clock.Instant
	Step    uint64
}

func (v *Violation) Error() string {
	return fmt.Sprintf("%s at instant %d, step %d: %s", v.Checker, int64(v.At), v.Step, v.Detail)
}

// EndChecker runs once, after the run, over the collected history.
//
// Separate from Oracle because the expensive checks belong here: porcupine is
// NP-hard in general and runs once at end of run with a history cap and a
// timeout, not incrementally per step (DR-18).
type EndChecker interface {
	Name() string

	// MinOps is the history floor below which this checker cannot conclude
	// anything and must report inconclusive.
	MinOps() int

	Check(h *History) Report
}

// CheckAll runs every end-of-run checker and returns the reports in the order
// the checkers were given.
//
// The floor is enforced here rather than trusted to each checker: a checker
// handed zero operations returns green by construction, which is the
// silent-success failure the count-not-presence rule exists to catch — and it
// would poison the soak ledger from the first banked hour, because nothing
// distinguishes "checked and found nothing wrong" from "checked nothing".
func CheckAll(h *History, checkers ...EndChecker) []Report {
	reports := make([]Report, 0, len(checkers))
	for _, c := range checkers {
		min := c.MinOps()
		if min < 1 {
			min = 1
		}
		if h == nil || h.Len() < min {
			got := 0
			if h != nil {
				got = h.Len()
			}
			reports = append(reports, Report{
				Checker:  c.Name(),
				Verdict:  VerdictInconclusive,
				Detail:   fmt.Sprintf("history of %d operations is below this checker's floor of %d; it concluded nothing and must not be banked as a pass", got, min),
				Consumed: got,
				Min:      min,
			})
			continue
		}

		r := c.Check(h)
		r.Checker = c.Name()
		r.Min = min
		if r.Verdict == VerdictUnset {
			r.Verdict = VerdictInconclusive
			r.Detail = "checker returned no verdict; treated as inconclusive rather than as a pass"
		}
		reports = append(reports, r)
	}
	return reports
}

// Summary folds reports into the counts a soak ledger records.
type Summary struct {
	Pass         int
	Violation    int
	Inconclusive int
}

func Summarize(reports []Report) Summary {
	var s Summary
	for _, r := range reports {
		switch r.Verdict {
		case VerdictPass:
			s.Pass++
		case VerdictViolation:
			s.Violation++
		case VerdictInconclusive, VerdictUnset:
			s.Inconclusive++
		case numVerdicts:
		}
	}
	return s
}

// Clean reports whether every checker concluded pass. An inconclusive run is
// not clean, which is the entire point of the third verdict.
func (s Summary) Clean() bool { return s.Violation == 0 && s.Inconclusive == 0 && s.Pass > 0 }

func (s Summary) String() string {
	return fmt.Sprintf("%d pass, %d violation, %d inconclusive", s.Pass, s.Violation, s.Inconclusive)
}
