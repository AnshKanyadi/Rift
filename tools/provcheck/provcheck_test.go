package provcheck_test

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Package provcheck_test enforces the provenance boundary around the ledger the
// safety oracles read.
//
// It lives under tools/ for gatepin's reason: it reads source text and builds
// fixture packages, both of which need os and os/exec, and raftcheck/ and
// store/ are places where that is a build failure.
//
// # What is being enforced, and why it is not just style
//
// The persist-before-reply oracle judged acknowledgements against a record of
// what each node had made durable, and that record was an ENGINE READ-BACK —
// the system's own account of what it held, including writes a crash would
// take. The oracle did not report false violations; it reported nothing, because
// an inflated durability watermark makes every acknowledgement look covered.
// Across 10,000 seeds the read-back was ahead of true durability 44,911 times,
// and correcting it turned 2 violations into 257 on a 300-seed sweep.
//
// Oracle independence failed inside the mechanism oracle independence exists to
// protect. These tests are the part of the fix that a future change has to get
// past.

// TestReportedFactCannotReachTheLedger is the induction for the type boundary.
//
// A type-level guarantee cannot be induced by a test that runs; it has to be
// induced by a compilation that fails. testdata/reported wires an engine
// read-back into Ledger.RecordDurable — the exact mistake — and this requires
// the build to refuse it.
func TestReportedFactCannotReachTheLedger(t *testing.T) {
	out, err := exec.Command("go", "build", "./testdata/reported").CombinedOutput()
	if err == nil {
		t.Fatal("testdata/reported COMPILED. A reported fact reached the ledger the safety " +
			"oracles read, which is the wiring that made persist-before-reply silent for the whole " +
			"of A1's first sweep. The provenance types are no longer load-bearing")
	}
	text := string(out)
	for _, want := range []string{"provenance.Reported", "provenance.Observed", "RecordDurable"} {
		if !strings.Contains(text, want) {
			t.Errorf("the build failed, but not for the reason this test is about: %q is absent from\n%s", want, text)
		}
	}
	t.Logf("refused at compile time:\n%s", strings.TrimSpace(text))
}

// launder matches the one expression that gets a reported fact past the type
// boundary. Witness(x.Unverified()) compiles, exactly as Wall(mono) does in
// clock; the type's job is to make the conversion something you have to write
// out, and this lane's job is to fail the build when anybody does.
var launder = regexp.MustCompile(`Witness\([^;\n]*\.Unverified\(\)`)

// code renders a file's declarations with every comment discarded.
//
// The lane scans code, not text. Its first version scanned raw bytes and fired
// on the two places that DESCRIBE the pattern in prose -- the provenance package
// explaining its own limit, and this file explaining what it looks for. The
// options were a file-exception list or scanning the thing that actually
// matters, and an exception list is how a rule starts collecting reasons not to
// apply.
func code(t *testing.T, path string, src []byte) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0) // mode 0: comments discarded
	if err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, f); err != nil {
		t.Fatalf("%s does not print: %v", path, err)
	}
	return buf.String()
}

// TestNoSourceLaundersReportedIntoObserved scans the tree, and is induced
// against a fixture that does exactly what it forbids.
func TestNoSourceLaundersReportedIntoObserved(t *testing.T) {
	// Induction first: a lane that has never seen its own pattern is a lane
	// nobody has checked. The fixture goes through the same normalization, so
	// this also proves the pattern survives it.
	fixture := filepath.Join("testdata", "laundered", "laundered.go")
	bad, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("reading the laundering fixture: %v", err)
	}
	if !launder.MatchString(code(t, fixture, bad)) {
		t.Fatal("the laundering fixture is not matched by the pattern this lane scans for, so the " +
			"lane could pass over a real one and nobody would know")
	}

	scanned := 0
	err = filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		if hit := launder.FindString(code(t, path, src)); hit != "" {
			t.Errorf("%s launders a reported fact into an observed one: %q.\n"+
				"  A value the system said about itself is being handed to a checker as though the "+
				"harness had witnessed it. If the conversion is genuinely justified, it needs a "+
				"ruling and an exception here, not a one-liner.", path, strings.TrimSpace(hit))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned no Go files, so this lane asserts nothing")
	}
	t.Logf("%d Go files scanned as code with comments discarded, no laundering", scanned)
}

// TestLedgerIngestionIsTypedByProvenance pins the boundary itself: every way
// into the ledger takes an observed fact, and raftcheck cannot see the engine.
func TestLedgerIngestionIsTypedByProvenance(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "raftcheck", "ledger.go"))
	if err != nil {
		t.Fatalf("reading ledger.go: %v", err)
	}
	text := string(src)

	sig := regexp.MustCompile(`(?m)^func \(l \*Ledger\) (Record\w+)\(([^)]*)\)`)
	found := sig.FindAllStringSubmatch(text, -1)
	if len(found) == 0 {
		t.Fatal("no Ledger.Record* method found; either the ingestion surface was renamed or this " +
			"lane stopped covering it, and both need resolving rather than passing")
	}
	for _, m := range found {
		if !strings.Contains(m[2], "provenance.Observed[") {
			t.Errorf("Ledger.%s takes a fact that is not provenance.Observed: (%s)\n"+
				"  Every input to a verdict that can come out GREEN must be something the harness "+
				"witnessed. A system-reported input here buys the system a pass, which is the "+
				"failure this boundary exists for.", m[1], m[2])
		}
	}
	t.Logf("%d ledger ingestion points, all typed by provenance", len(found))

	// The oracles must not be able to reach the engine at all, by any route.
	for _, forbidden := range []string{
		`"github.com/anshkanyadi/rift/engine"`,
		`"github.com/anshkanyadi/rift/engine/model"`,
		`"github.com/anshkanyadi/rift/store"`,
	} {
		entries, err := filepath.Glob(filepath.Join("..", "..", "raftcheck", "*.go"))
		if err != nil {
			t.Fatalf("globbing raftcheck: %v", err)
		}
		for _, f := range entries {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading %s: %v", f, err)
			}
			if strings.Contains(string(b), forbidden) {
				t.Errorf("%s imports %s. An oracle that can reach the engine can ask it what it "+
					"holds, and the engine answers with its VISIBLE state.", f, forbidden)
			}
		}
	}
}
