package determinismcheck

import (
	"fmt"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the determinism pass. See doc.go for what it enforces and why.
var Analyzer = &analysis.Analyzer{
	Name:     "determinismcheck",
	Doc:      analyzerDoc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

const analyzerDoc = `enforce Rift's determinism rules as build failures

Core packages (raft, store, kv, router, balancer, engine, engine/model,
sim/toy) are pure state machines: they take time, randomness, network and
storage from injected interfaces and touch nothing else. This pass rejects the
constructs that would quietly break that -- wall-clock reads, package-level
randomness, I/O imports, goroutines and channels, range over a map, pointer
identity -- so "a time.Now() cannot sneak in" is a build failure rather than a
promise.

Driver packages (node) get the mailbox rule instead: core state is reached only
through Handle and Status, and only the mailbox post function may send.

Escape hatch: //rift:allow-nondeterminism <reason>, per file (above the package
clause) or per line (on or above the offending line). A reason is mandatory and
every use is printed to stderr, so exceptions stay visible instead of
accumulating.`

// Rule names prefix every diagnostic so a build log can be grepped by rule and
// so the mutant suite can assert which rule fired, not merely that one did.
const (
	ruleImport      = "import"
	ruleTime        = "time"
	ruleConcurrency = "concurrency"
	ruleMapRange    = "maprange"
	ruleMapKey      = "mapkey"
	rulePointerFmt  = "pointerfmt"
	ruleMailbox     = "mailbox"
	ruleEscape      = "escape"
)

var (
	flagCore           = strings.Join(defaultCore, ",")
	flagMailbox        = strings.Join(defaultMailbox, ",")
	flagMailboxAllow   = "Handle,Status"
	flagPostFunc       = "post"
	flagListAllowances = true
)

// The scope tables live in the code rather than in a Makefile line, so adding a
// core package is a reviewed diff. The flags exist for tests and for one-off
// runs over an unmerged package.
func init() {
	Analyzer.Flags.StringVar(&flagCore, "core", flagCore,
		"comma-separated package patterns under the core rules (`p` or `p/...`)")
	Analyzer.Flags.StringVar(&flagMailbox, "mailbox", flagMailbox,
		"comma-separated package patterns under the mailbox rule")
	Analyzer.Flags.StringVar(&flagMailboxAllow, "mailbox-allow", flagMailboxAllow,
		"comma-separated method names a mailbox package may call on a core type")
	Analyzer.Flags.StringVar(&flagPostFunc, "mailbox-post", flagPostFunc,
		"name of the only function in a mailbox package permitted to send on a channel")
	Analyzer.Flags.BoolVar(&flagListAllowances, "list-allowances", flagListAllowances,
		"print every use of the //rift:allow-nondeterminism escape hatch to stderr")
}

func run(pass *analysis.Pass) (any, error) {
	sc := scopeFor(pass.Pkg.Path())
	if sc == scopeOff {
		return nil, nil
	}

	r := &reporter{pass: pass, allow: newAllowIndex(pass)}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	switch sc {
	case scopeCore:
		checkCore(r, insp)
	case scopeMailbox:
		checkMailbox(r, insp)
	}

	r.allow.finish()
	return nil, nil
}

// reporter routes every finding through the escape hatch, so no rule can
// forget to honour it and none can suppress a finding without leaving a record.
type reporter struct {
	pass  *analysis.Pass
	allow *allowIndex
}

func (r *reporter) report(pos token.Pos, rule, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if r.allow.suppress(pos, rule, msg) {
		return
	}
	r.pass.Reportf(pos, "%s: %s", rule, msg)
}
