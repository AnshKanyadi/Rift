package determinismcheck

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// checkCore enforces the rules that make a core package a pure state machine.
// Each rule below corresponds to a line in CLAUDE.md's determinism rules or in
// DESIGN-A0 D10, and each message says what to do instead -- a rule that only
// says no gets worked around rather than followed.
func checkCore(r *reporter, insp *inspector.Inspector) {
	checkImports(r)

	insp.Preorder([]ast.Node{
		(*ast.BasicLit)(nil),
		(*ast.BinaryExpr)(nil),
		(*ast.CallExpr)(nil),
		(*ast.ChanType)(nil),
		(*ast.GoStmt)(nil),
		(*ast.Ident)(nil),
		(*ast.MapType)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.StructType)(nil),
		(*ast.SelectStmt)(nil),
		(*ast.SendStmt)(nil),
		(*ast.UnaryExpr)(nil),
	}, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			checkInstantMath(r, n)
		case *ast.CallExpr:
			checkPointerFormat(r, n)
		case *ast.ChanType:
			r.reportHard(n.Pos(), ruleConcurrency,
				"channel types are not allowed in core packages; every input reaches a node through Handle(Event)")
		case *ast.GoStmt:
			r.reportHard(n.Pos(), ruleConcurrency,
				"go statements are not allowed in core packages; node logic runs single-threaded off the event loop, which is what makes a data race here unrepresentable rather than merely unlikely")
		case *ast.Ident:
			checkBannedSymbol(r, n)
			checkFloat(r, n)
		case *ast.BasicLit:
			if n.Kind == token.FLOAT || n.Kind == token.IMAG {
				r.report(n.Pos(), ruleFloat,
					"floating-point literals are not allowed in core packages; %s", floatWhy)
			}
		case *ast.MapType:
			checkMapKey(r, n)
		case *ast.StructType:
			checkMonoLeak(r, n)
		case *ast.RangeStmt:
			checkRange(r, n)
		case *ast.SelectStmt:
			r.reportHard(n.Pos(), ruleConcurrency,
				"select statements are not allowed in core packages; the runtime randomizes the choice among ready cases and there is no way to override it")
		case *ast.SendStmt:
			r.reportHard(n.Pos(), ruleConcurrency,
				"channel sends are not allowed in core packages; outputs leave a node through the Ready struct")
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				r.reportHard(n.Pos(), ruleConcurrency,
					"channel receives are not allowed in core packages; every input reaches a node through Handle(Event)")
			}
		}
	})
}

// bannedImport is the I/O and concurrency blocklist. The list is stricter than
// DESIGN-A0 D10's wording in two places, both noted below, because banning the
// import is mechanically simpler than chasing every symbol in it and leaves
// less room for a clever exception.
func bannedImport(path string) (why string, banned bool) {
	switch {
	case path == "os" || strings.HasPrefix(path, "os/"):
		return "core packages reach the outside world only through injected interfaces (Engine, Transport, Clock)", true
	case path == "net" || strings.HasPrefix(path, "net/"):
		return "the network is reached only through the injected Transport", true
	case path == "path/filepath" || path == "io/ioutil":
		return "core packages perform no filesystem access; storage is the injected Engine", true
	case path == "syscall" || strings.HasPrefix(path, "golang.org/x/sys"):
		return "core packages make no syscalls", true

	// Stricter than D10, which bans package-level math/rand functions: the
	// import itself is banned, because rand.New(rand.NewSource(seed)) in a core
	// package is the same bug with a local variable in front of it.
	case path == "math/rand" || path == "math/rand/v2":
		return "randomness is the injected rng.Rand, whose stream this project owns so the seed corpus survives Go upgrades (CLAUDE.md Amendment A1)", true

	case path == "sync" || path == "sync/atomic":
		return "core logic is single-threaded off the event loop, so there is nothing to synchronize; a mutex here means the loop has been bypassed", true

	// The go1.23 iterator hole. slices.Sorted(maps.Keys(m)),
	// slices.Collect(maps.Keys(m)) and range-over-maps.All contain no
	// map-range syntax and are exactly the same nondeterminism, so the rule
	// that catches `for k := range m` sees none of them. Banning the import is
	// the only place to stand: sorted iteration lives in internal/sorted and
	// nowhere else.
	case path == "maps":
		return "maps.Keys, Values and All iterate in randomized order behind an iterator, where the map-range rule cannot see them; use internal/sorted", true
	case path == "unsafe":
		return "unsafe leaks pointer identity and layout, neither of which is stable run to run", true
	case path == "runtime" || strings.HasPrefix(path, "runtime/"):
		return "runtime exposes goroutine counts, GC timing and scheduler state, none of which are reproducible", true

	// Also stricter than D10, and for the same reason time.Now is: log stamps
	// wall-clock time and writes to stderr. Core packages take the injected
	// Logger, whose fields are seed, step, node, term and range.
	case path == "log" || strings.HasPrefix(path, "log/"):
		return "logging goes through the injected Logger; log writes wall-clock timestamps to stderr", true
	}
	return "", false
}

// hardImport reports whether a banned import is one no hatch may excuse. sync
// keeps company with go, select and chan for the same reason: a core package
// that needs to synchronize has already left the event loop, and that is a
// design problem rather than an annotation problem.
func hardImport(path string) bool {
	return path == "sync" || path == "sync/atomic"
}

func checkImports(r *reporter) {
	for _, f := range r.pass.Files {
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			why, banned := bannedImport(path)
			if !banned {
				continue
			}
			report := r.report
			if hardImport(path) {
				report = r.reportHard
			}
			report(spec.Pos(), ruleImport, "importing %q is not allowed in core packages: %s", path, why)
		}
	}
}

// timeAllowed is an allowlist, not a blocklist, and that polarity is the point:
// a blocklist has to be extended every time the time package grows a new way to
// read the clock, and nobody will notice that it needs extending. Anything here
// is pure value machinery, deterministic given its arguments. Everything else
// in time is banned by default, including whatever gets added next.
//
// Constants are allowed wherever they appear (Nanosecond through Hour, the
// layout strings, the month and weekday names) -- a constant cannot read a
// clock. So are methods on time's value types and their struct fields: the
// types are legal, so Add, Sub, Before and the rest are legal with them.
var timeAllowed = map[string]bool{
	// Value types.
	"Duration": true, "Time": true, "Month": true, "Weekday": true,
	"Location": true, "ParseError": true,

	// Deterministic constructors and parsers: same arguments, same result, on
	// any machine at any moment. Each either takes an explicit *Location or
	// returns UTC.
	//
	// Audited 2026-08-11 for constructors that return a location-bearing Time.
	// Unix, UnixMilli and UnixMicro are exactly that -- they return a Time in
	// the host's local zone, so anything formatted from one reads differently
	// in two datacentres -- and are therefore NOT on this list. Date and
	// ParseInLocation take a *Location explicitly, and passing time.Local to
	// either is caught by the ban on Local itself. Parse returns UTC when the
	// value carries no zone.
	"Date": true, "Parse": true, "ParseInLocation": true, "ParseDuration": true,
	"FixedZone": true,

	// A fixed zone. Unlike Local, which is whatever TZ says today.
	"UTC": true,
}

// bannedTimeMethod is the closure on host-TZ dependence. Methods on time's
// value types are otherwise legal -- Add, Sub, Before, Format all are -- but
// Local() is time.Local written with method syntax, and it puts the host's
// timezone into anything formatted downstream, including trace output.
//
// With these banned and the Unix family off the allowlist, every time.Time
// reachable inside a core package is UTC or an explicit FixedZone.
var bannedTimeMethod = map[string]string{
	"Local": "keep instants in UTC; Local() is time.Local with method syntax, and it makes formatted output depend on the host's TZ",
}

// timeAdvice says what to do instead, for the symbols anyone will actually
// reach for. Everything else falls back to the general rule.
var timeAdvice = map[string]string{
	"Now":                    "read the current time from the injected Clock",
	"Since":                  "subtract two Clock readings",
	"Until":                  "subtract two Clock readings",
	"Sleep":                  "schedule an event; nothing in a core package may block",
	"After":                  "schedule an event; nothing in a core package may block",
	"AfterFunc":              "schedule an event; nothing in a core package may block",
	"Tick":                   "drive time through Tick(), which the event loop calls",
	"NewTimer":               "drive time through Tick(), which the event loop calls",
	"NewTicker":              "drive time through Tick(), which the event loop calls",
	"Timer":                  "drive time through Tick(), which the event loop calls",
	"Ticker":                 "drive time through Tick(), which the event loop calls",
	"Local":                  "use UTC; Local is whatever the host's TZ says, which is not a property of the run",
	"LoadLocation":           "use UTC or FixedZone; loading a zone reads the host's tzdata",
	"LoadLocationFromTZData": "use UTC or FixedZone",
	"Unix":                   "carry instants as nanoseconds (clock.Instant); time.Unix returns a Time in the host's local zone",
	"UnixMilli":              "carry instants as nanoseconds (clock.Instant); time.UnixMilli returns a Time in the host's local zone",
	"UnixMicro":              "carry instants as nanoseconds (clock.Instant); time.UnixMicro returns a Time in the host's local zone",
}

// bannedReflect is the other half of the iterator hole. reflect reaches map
// iteration through methods rather than syntax or an import that can be banned
// outright -- core packages have no business in reflect at all, but these are
// the ones that are silently nondeterministic rather than merely unwise. Seq
// and Seq2 are the same hole in iterator clothing: no range syntax, no maps
// import, and a map underneath.
var bannedReflect = map[string]bool{
	"MapRange": true,
	"MapKeys":  true,
	"MapIter":  true,
	"Seq":      true,
	"Seq2":     true,
}

func checkBannedSymbol(r *reporter, id *ast.Ident) {
	obj := r.pass.TypesInfo.Uses[id]
	if obj == nil || obj.Pkg() == nil {
		return
	}
	switch obj.Pkg().Path() {
	case "time":
		checkTimeSymbol(r, id, obj)
	case "reflect":
		if bannedReflect[obj.Name()] {
			r.report(id.Pos(), ruleMapRange,
				"reflect.%s iterates a map in randomized order, where the map-range rule cannot see it; use internal/sorted", obj.Name())
		}
	}
}

func checkTimeSymbol(r *reporter, id *ast.Ident, obj types.Object) {
	if _, isConst := obj.(*types.Const); isConst {
		return // Nanosecond through Hour, the layouts, the month names
	}
	if v, isVar := obj.(*types.Var); isVar && v.IsField() {
		return // a field of a legal value type
	}

	// A method is legal with its type -- Add, Sub, Before, Format -- unless it
	// is one of the few that reintroduce the host's timezone.
	if sig := methodSig(obj); sig != nil {
		instead, banned := bannedTimeMethod[obj.Name()]
		if !banned {
			return
		}
		recv := strings.TrimPrefix(types.TypeString(sig.Recv().Type(), nil), "*")
		r.report(id.Pos(), ruleTime,
			"%s.%s is not allowed in core packages; %s (CLAUDE.md determinism rules)", recv, obj.Name(), instead)
		return
	}

	if timeAllowed[obj.Name()] {
		return
	}
	instead, known := timeAdvice[obj.Name()]
	if !known {
		instead = "core packages take time from the injected Clock; only time's value types, constants and deterministic constructors are allowed here"
	}
	r.report(id.Pos(), ruleTime,
		"time.%s is not allowed in core packages; %s (CLAUDE.md determinism rules)", obj.Name(), instead)
}

// methodSig returns obj's signature if it is a method, and nil otherwise.
func methodSig(obj types.Object) *types.Signature {
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	return sig
}

// checkRange is the important one. Go randomizes map iteration order on
// purpose, so a loop over a map that decides anything -- which replica to send
// to first, which lock to resolve -- produces a different history on every run
// from the same seed. It is silent, it is common, and it is the leak CLAUDE.md
// singles out.
func checkRange(r *reporter, rs *ast.RangeStmt) {
	t := r.pass.TypesInfo.TypeOf(rs.X)
	if t == nil {
		return
	}
	switch t.Underlying().(type) {
	case *types.Map:
		// The advice is deliberately not "collect the keys and sort them":
		// collecting them means ranging the map, which is this rule. Keep the
		// order beside the map, or take it from a helper outside the core
		// packages, which is where the one blessed implementation belongs.
		r.report(rs.Pos(), ruleMapRange,
			"range over a map is not allowed in core packages; Go randomizes map iteration order, so anything derived from it differs run to run -- range a sorted slice of keys instead")
	case *types.Chan:
		r.report(rs.Pos(), ruleConcurrency,
			"range over a channel is not allowed in core packages; every input reaches a node through Handle(Event)")
	}
}

func checkMapKey(r *reporter, mt *ast.MapType) {
	t := r.pass.TypesInfo.TypeOf(mt.Key)
	if t == nil {
		return
	}
	addressKeyed := false
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Chan:
		addressKeyed = true
	case *types.Basic:
		addressKeyed = u.Kind() == types.UnsafePointer
	}
	if addressKeyed {
		r.report(mt.Key.Pos(), ruleMapKey,
			"pointer-keyed maps are not allowed in core packages; the key is an address, so both iteration order and equality depend on where the allocator happened to put things")
	}
}

// checkPointerFormat catches %p, which prints an address. An address in a log
// line is noise; an address in anything the trace hash sees is a determinism
// failure that reproduces as "same seed, different hash" and costs an afternoon
// before anyone thinks to look at a format string.
func checkPointerFormat(r *reporter, call *ast.CallExpr) {
	i := formatArg(r, call)
	if i < 0 || i >= len(call.Args) {
		return
	}
	lit, ok := call.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return
	}
	if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, "%p") {
		r.report(lit.Pos(), rulePointerFmt,
			"%%p formats a pointer address, which varies run to run; format a stable identity (node id, range id, index) instead")
	}
}

// formatArg returns the index of a call's format string, or -1 if the call is
// not Printf-style. It decides by signature rather than by name -- a variadic
// tail preceded by a string parameter -- which covers fmt, the injected Logger,
// and anything either grows later. Only that one argument is examined, so an
// ordinary string that happens to contain %p stays legal wherever it appears.
func formatArg(r *reporter, call *ast.CallExpr) int {
	obj := typeutil.Callee(r.pass.TypesInfo, call)
	if obj == nil {
		return -1
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || !sig.Variadic() {
		return -1
	}
	params := sig.Params()
	if params.Len() < 2 {
		return -1
	}
	format, ok := params.At(params.Len() - 2).Type().Underlying().(*types.Basic)
	if !ok || format.Kind() != types.String {
		return -1
	}
	return params.Len() - 2
}

// floatWhy is one sentence, repeated by every float diagnostic, because the
// reason is the whole rule.
const floatWhy = "the Go spec permits fusing a multiply-add into one FMA, which arm64 does and amd64 without FMA does not, so the same seed can produce last-bit differences on two machines -- and a one-nanosecond difference in a lease expiry is a different history; keep integers or fixed-point on every path feeding the trace hash, and materialize plan-authored floats to integers at the compile boundary"

// checkFloat rejects float32 and float64 in core scope, by resolved kind rather
// than by name, so a defined type over a float is caught with the same rule.
//
// Ruled 2026-08-11 and promoted to a standing rule: no floating point on any
// path feeding the trace hash or replay identity. Hatchable only with a
// registry reason stating why the value cannot affect replay -- clock/frac.go
// is the model, where a plan-authored fraction becomes nanoseconds and is never
// read as a float again.
func checkFloat(r *reporter, id *ast.Ident) {
	obj := r.pass.TypesInfo.Uses[id]
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return
	}
	basic, ok := tn.Type().Underlying().(*types.Basic)
	if !ok {
		return
	}
	switch basic.Kind() {
	case types.Float32, types.Float64, types.Complex64, types.Complex128, types.UntypedFloat, types.UntypedComplex:
		r.report(id.Pos(), ruleFloat,
			"%s is not allowed in core packages; %s", tn.Name(), floatWhy)
	}
}

// checkMonoLeak rejects a clock.Mono in an exported or tagged struct field
// outside package clock.
//
// A monotonic reading's epoch is this boot of this node, so the value means
// nothing anywhere else: persisted, it survives a restart as a number from a
// timeline that no longer exists; sent on the wire, it is read against a
// different boot entirely. A lease expiry stored that way makes a node serve
// reads under a lease it does not hold.
//
// Exported or tagged is the test because those are the fields that leave: an
// unexported, untagged field is node-local by construction, which is exactly
// what a Mono is for.
func checkMonoLeak(r *reporter, st *ast.StructType) {
	if r.pass.Pkg != nil && r.pass.Pkg.Path() == flagMonoPkg {
		return // the package that defines it may hold it
	}
	for _, field := range st.Fields.List {
		if !mentionsMono(r, field.Type) {
			continue
		}
		tagged := field.Tag != nil
		for _, name := range field.Names {
			if name.IsExported() || tagged {
				r.report(name.Pos(), ruleMonoLeak,
					"%s carries a clock.Mono in an %s struct field; a monotonic reading is meaningful only on the node and boot that produced it, so it must never be persisted or sent on the wire -- store a clock.Wall, or store the duration",
					name.Name, exportedOrTagged(name.IsExported(), tagged))
			}
		}
		if len(field.Names) == 0 && tagged {
			r.report(field.Type.Pos(), ruleMonoLeak,
				"an embedded clock.Mono in a tagged struct field is still on the wire; store a clock.Wall, or store the duration")
		}
	}
}

func exportedOrTagged(exported, tagged bool) string {
	switch {
	case exported && tagged:
		return "exported, tagged"
	case exported:
		return "exported"
	default:
		return "tagged"
	}
}

// mentionsMono reports whether a field type is clock.Mono or contains one:
// a slice of them, a pointer to one, a map of them are all the same leak.
func mentionsMono(r *reporter, expr ast.Expr) bool {
	t := r.pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	return containsMono(t, make(map[types.Type]bool))
}

func containsMono(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true

	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj != nil && obj.Name() == "Mono" && obj.Pkg() != nil && obj.Pkg().Path() == flagMonoPkg {
			return true
		}
		return containsMono(named.Underlying(), seen)
	}
	switch u := t.(type) {
	case *types.Slice:
		return containsMono(u.Elem(), seen)
	case *types.Array:
		return containsMono(u.Elem(), seen)
	case *types.Pointer:
		return containsMono(u.Elem(), seen)
	case *types.Map:
		return containsMono(u.Key(), seen) || containsMono(u.Elem(), seen)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if containsMono(u.Field(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

// checkInstantMath closes the last gap in the monotonic-leakage claim.
//
// Defined integer types make cross-type arithmetic uncompilable, which is most
// of the win, but they keep their operators within a type: `a - b` on two Monos
// compiles and yields a Mono. That is a type lie. The quantity in hand is a
// duration -- how long between two readings -- and calling it an instant lets
// it flow into instant-typed positions, which is the same confusion the two
// types were introduced to prevent, one level down.
//
// So arithmetic between two values of the same instant type is banned outside
// the package that defines them. Sub and Add are the sanctioned spellings, and
// comparisons stay legal: ordering two readings from one node is exactly what
// defined integer types bought, and no lie is available there.
func checkInstantMath(r *reporter, be *ast.BinaryExpr) {
	switch be.Op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
		token.AND, token.OR, token.XOR, token.SHL, token.SHR, token.AND_NOT:
	default:
		return // comparisons and logical operators are fine
	}
	if r.pass.Pkg != nil && r.pass.Pkg.Path() == flagMonoPkg {
		return
	}

	x := instantTypeName(r.pass.TypesInfo.TypeOf(be.X))
	y := instantTypeName(r.pass.TypesInfo.TypeOf(be.Y))
	if x == "" || x != y {
		return
	}
	verb := "arithmetic on"
	if be.Op == token.SUB {
		verb = "subtracting"
	} else if be.Op == token.ADD {
		verb = "adding"
	}
	r.report(be.OpPos, ruleInstantMath,
		"%s two %s values is not allowed outside %s; the result is typed as an instant but the quantity is a duration, and that lie flows into instant-typed positions -- use Sub for the difference between two readings, or Add to advance one by a Duration",
		verb, x, flagMonoPkg)
}

// instantTypeName returns the bare name of t if it is one of the instant types,
// and "" otherwise.
func instantTypeName(t types.Type) string {
	named, ok := t.(*types.Named)
	if !ok {
		return ""
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != flagMonoPkg {
		return ""
	}
	if slices.Contains(splitPatterns(flagInstantTypes), obj.Name()) {
		return obj.Pkg().Name() + "." + obj.Name()
	}
	return ""
}
