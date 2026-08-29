package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The corpus lane: every bundle in seeds/ is replayed, and a bundle that no
// longer reproduces its recorded verdict fails the build.
//
// # Why this lane and not a promise
//
// "Every bug ever found replays from a single seed" is a published claim --
// CLAUDE.md rests a resume line on it and seeds/README.md calls the corpus a
// regression suite rather than a museum. Nothing enforced it. A bundle stops
// reproducing the moment the harness moves under it, and it does so in complete
// silence: the directory is still there, the JSON still parses, and nobody runs
// `simctl replay` on a two-month-old entry unless a lane makes them.
//
// It was already true when this lane was written. Both A0 bundles had rotted --
// same finding, different trace -- and the only reason anybody looked is that
// somebody typed the command by hand.
//
// # What counts as reproducing
//
// Two claims, and they are not the same claim:
//
//	the VERDICT   the finding this bundle exists to carry is found again. This
//	              is what the corpus promises and what a stranger checks.
//	the TRACE     the run is bit-identical to the recorded one. This is a
//	              property of the harness at the recorded commit, so a
//	              deliberate harness change legitimately moves it.
//
// Both fail the lane, deliberately. A moved trace hash is not automatically a
// defect, but it is never a non-event: it means the corpus and the code have
// diverged, and the resolution is to regenerate the bundle IN THE SAME COMMIT
// that moved it, exactly as the fresh-process hash for seed 4242 was moved once
// and recorded. A lane that tolerated it would be back to silence, one step
// removed.

// bundleDir is one corpus entry.
type bundleDir struct {
	name string
	path string
	meta struct {
		Seed      uint64   `json:"seed"`
		Commit    string   `json:"commit"`
		Workload  string   `json:"workload"`
		TraceHash string   `json:"trace_hash"`
		Mutant    string   `json:"mutant"`
		Mutants   []string `json:"mutants"`
		Violation *struct {
			Checker string `json:"checker"`
			Detail  string `json:"detail"`
		} `json:"violation"`
	}
}

// mutantSet is every patch this bundle needs, from either field. A defect that
// no single patch reintroduces names a set (see Meta.Mutants); everything else
// names one, and both read the same here.
func (b bundleDir) mutantSet() []string {
	var out []string
	if b.meta.Mutant != "" {
		out = append(out, b.meta.Mutant)
	}
	return append(out, b.meta.Mutants...)
}

// nonBundleCorpora registers directories under seeds/ that are NOT Track A
// bundles: what kind of corpus each is, and who checks it.
//
// # The finding this exists for
//
// At the A7/B5 merge, `seeds/differential/` arrived with Track B's 22 format
// entries and this lane went red: it treated every directory under seeds/ as a
// Track A bundle, which was true when it was written and stopped being true the
// moment the two tracks became one tree. GF-39's debt, paid in the instant.
//
// **Three lanes walk seeds/ and they disagreed about what a non-bundle directory
// means**, which is the real finding:
//
//	corpus_test.go (here)     errors            <- the only right answer
//	corpus-reproduces.sh      skipped SILENTLY  <- fixed, see its counters
//	bundle-seeds.sh           never looks       <- it iterates seeds/BUG-*/ only
//
// The cheap fix -- skip the name -- is the wrong one. A bare `if name ==
// "differential" { continue }` is a hole with a comment on it. **A registered
// directory declares its kind and its owner; an unregistered one is an error;
// and the registry is asserted against the tree so a stale entry fails.** That
// last clause is here because of the sibling defect found the same day: a
// `notAnalysed` entry in tools/determinismcheck whose reason had gone false and
// which nothing re-asked. An exemption list with no rot check is how a
// permission nobody granted stays granted.
var nonBundleCorpora = map[string]string{
	"differential": "Track B's differential ARTIFACT FORMAT corpus. Not a replayable schedule: " +
		"22 hand-built .diff files that pin the wire format both engines' rigs agree on. " +
		"Checked by engine/differential/artifact_test.go and by " +
		"engine-cpp/test/differential_artifact_test.cc against the same bytes, and gated by " +
		"RequireJudged -- a file under seeds/differential/ must have been judged, or the rig " +
		"fails. It is somebody's corpus; it is not this lane's.",
}

// registeredCorpusFindings is the classification, pulled out of corpus() so it
// can be induced. Returns one message per finding, empty when the tree agrees
// with the registry.
func registeredCorpusFindings(dirs []string, entryCount func(string) int) []string {
	var out []string
	present := map[string]bool{}
	for _, d := range dirs {
		present[d] = true
	}
	for name := range nonBundleCorpora {
		if !present[name] {
			out = append(out, name+" is registered in nonBundleCorpora and is not in seeds/. "+
				"A registry entry for a directory that does not exist is an exemption nobody "+
				"can revoke, because nothing ever reaches it again. Delete the entry or "+
				"restore the directory.")
		}
	}
	for _, d := range dirs {
		if _, ok := nonBundleCorpora[d]; !ok {
			continue
		}
		if entryCount(d) == 0 {
			out = append(out, d+" is registered as a non-bundle corpus and is EMPTY. "+
				"A registry entry is permission to be a different shape, not permission to "+
				"be nothing: an empty registered directory is exactly what a corpus looks "+
				"like after somebody deletes it.")
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func corpus(t *testing.T) []bundleDir {
	t.Helper()
	root := filepath.Join("..", "..", "seeds")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}

	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	for _, f := range registeredCorpusFindings(dirs, func(name string) int {
		sub, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			return 0
		}
		return len(sub)
	}) {
		t.Error(f)
	}
	var out []bundleDir
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if why, registered := nonBundleCorpora[e.Name()]; registered {
			t.Logf("  %s: a registered non-bundle corpus -- %s", e.Name(), why)
			continue
		}
		b := bundleDir{name: e.Name(), path: filepath.Join(root, e.Name())}
		mb, err := os.ReadFile(filepath.Join(b.path, "meta.json"))
		if err != nil {
			t.Errorf("%s is a directory in seeds/ with no meta.json and no entry in "+
				"nonBundleCorpora; a corpus entry that is neither a bundle nor a registered "+
				"corpus of another kind is either an unfinished bundle or litter, and both "+
				"need resolving", b.name)
			continue
		}
		if err := json.Unmarshal(mb, &b.meta); err != nil {
			t.Errorf("%s/meta.json does not parse: %v", b.name, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(b.path, "plan.json")); err != nil {
			t.Errorf("%s has no plan.json, so it reproduces at no commit but its own", b.name)
			continue
		}
		out = append(out, b)
	}
	return out
}

// TestEveryStoredBundleReplays is the lane.
func TestEveryStoredBundleReplays(t *testing.T) {
	bundles := corpus(t)
	if len(bundles) == 0 {
		t.Fatal("seeds/ holds no bundles, so this lane asserts nothing. An empty corpus is a " +
			"legitimate state only before the first bug is found; after that it means entries were " +
			"removed without the lane noticing, which is the failure this lane exists for")
	}

	bin := build(t)
	for _, b := range bundles {
		t.Run(b.name, func(t *testing.T) {
			out, err := exec.Command(bin, "replay", "--bundle", b.path).CombinedOutput()
			text := string(out)
			if err != nil {
				t.Errorf("bundle %s no longer reproduces (recorded at %s):\n%s",
					b.name, commitOrUnknown(b.meta.Commit), indent(text))
				return
			}
			if !strings.Contains(text, "MATCH") {
				t.Errorf("bundle %s replayed without reporting a match:\n%s", b.name, indent(text))
			}
			if b.meta.Violation != nil && !strings.Contains(text, "violation reproduced") {
				t.Errorf("bundle %s records a %s violation that the replay did not reproduce:\n%s",
					b.name, b.meta.Violation.Checker, indent(text))
			}
			t.Logf("seed %d, %s workload, recorded at %s: %s", b.meta.Seed, b.meta.Workload,
				commitOrUnknown(b.meta.Commit), verdictOf(b))
		})
	}
	t.Logf("%d bundle(s) replayed", len(bundles))
}

// TestCorpusLaneDetectsRot induces the lane in both of the directions it checks.
//
// A lane over a corpus that currently reproduces cannot distinguish "every
// bundle replays" from "replay always says yes", and this repository has shipped
// five mechanisms that were never invoked. So a real bundle is copied, damaged
// one way at a time, and the replay is required to refuse it.
func TestCorpusLaneDetectsRot(t *testing.T) {
	bundles := corpus(t)
	if len(bundles) == 0 {
		// Not a skip. This test exists because a lane over a corpus that
		// currently reproduces cannot tell "every bundle replays" from "replay
		// always says yes" -- and with no bundle to damage it cannot tell that
		// either, while reporting success. A rot detector that skips when there
		// is nothing to damage is the thing it was built to catch.
		t.Fatal("seeds/ holds no bundle to damage, so this lane cannot show that replay is " +
			"capable of refusing anything, and a green here would say only that it ran")
	}
	src := bundles[0]
	bin := build(t)

	for _, tc := range []struct {
		name   string
		damage func(m map[string]any)
		expect string
	}{
		{
			name:   "trace hash moved",
			damage: func(m map[string]any) { m["trace_hash"] = strings.Repeat("0", 64) },
			expect: "DIVERGED",
		},
		{
			name: "recorded finding replaced",
			damage: func(m map[string]any) {
				m["violation"] = map[string]any{
					"checker": "a-checker-that-does-not-exist",
					"detail":  "a finding this run never produced",
					"at_ns":   1,
				}
			},
			expect: "VIOLATION NOT REPRODUCED",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			copyFile(t, filepath.Join(src.path, "plan.json"), filepath.Join(dir, "plan.json"))

			raw, err := os.ReadFile(filepath.Join(src.path, "meta.json"))
			if err != nil {
				t.Fatalf("reading meta: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("parsing meta: %v", err)
			}
			tc.damage(m)
			mb, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				t.Fatalf("re-encoding meta: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "meta.json"), mb, 0o644); err != nil {
				t.Fatalf("writing meta: %v", err)
			}

			out, err := exec.Command(bin, "replay", "--bundle", dir).CombinedOutput()
			if err == nil {
				t.Fatalf("a bundle with %s replayed clean; the lane would report green over a rotted "+
					"corpus:\n%s", tc.name, indent(string(out)))
			}
			if !strings.Contains(string(out), tc.expect) {
				t.Errorf("expected the replay to report %q, got:\n%s", tc.expect, indent(string(out)))
			}
		})
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("reading %s: %v", from, err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatalf("writing %s: %v", to, err)
	}
}

func commitOrUnknown(c string) string {
	if c == "" {
		return "a commit the bundle does not record"
	}
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func verdictOf(b bundleDir) string {
	if b.meta.Violation != nil {
		return b.meta.Violation.Checker + " -- " + b.meta.Violation.Detail
	}
	if set := b.mutantSet(); len(set) > 0 {
		// The schedule is preserved here and the defect is preserved in the
		// mutant; neither half reproduces the bug alone.
		return "schedule only; the defect it exposed is fixed and preserved as " + strings.Join(set, " + ")
	}
	return "no violation recorded; a determinism artifact rather than a finding"
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n    ")
}

// TestTheCorpusRegistryIsInduced fires every arm of the classification, because
// a registry that has only ever been seen agreeing is not a registry that has
// been checked.
//
// Three arms, and each is a failure this project has actually paid for in some
// form: an unregistered directory (the merge), a registered directory gone empty
// (nothing yet, and that is the point of pinning it before it happens), and a
// registry entry whose subject no longer exists (the notAnalysed entry in
// tools/determinismcheck, found the same day, whose reason had gone false and
// which nothing re-asked).
func TestTheCorpusRegistryIsInduced(t *testing.T) {
	full := func(string) int { return 22 }
	empty := func(string) int { return 0 }

	// The tree as it stands: differential present and non-empty, nothing else
	// unregistered. Silence here is what makes the other three arms mean
	// something.
	if got := registeredCorpusFindings([]string{"BUG-001", "differential"}, full); len(got) != 0 {
		t.Fatalf("a tree that agrees with the registry produced findings: %v", got)
	}

	// A registered directory that is empty.
	got := registeredCorpusFindings([]string{"BUG-001", "differential"}, empty)
	if len(got) != 1 || !strings.Contains(got[0], "EMPTY") {
		t.Fatalf("an empty registered corpus was not reported: %v", got)
	}

	// A registry entry with nothing behind it.
	got = registeredCorpusFindings([]string{"BUG-001"}, full)
	if len(got) != 1 || !strings.Contains(got[0], "not in seeds/") {
		t.Fatalf("a stale registry entry was not reported: %v", got)
	}

	// And the arm the merge itself fired: an unregistered directory is not the
	// registry's business, it is corpus()'s, so this function must stay silent
	// about it rather than growing a second opinion.
	if got := registeredCorpusFindings([]string{"BUG-001", "differential", "somebodys-scratch"}, full); len(got) != 0 {
		t.Fatalf("the registry check reported on an unregistered directory, which is corpus()'s "+
			"job: %v", got)
	}
}

// TestAnUnregisteredDirectoryIsAnError is the merge's own failure, pinned.
//
// It runs corpus() against a synthetic seeds/ rather than the real one, because
// the real one is now correct and a lane cannot be induced by the defect it just
// eliminated.
func TestAnUnregisteredDirectoryIsAnError(t *testing.T) {
	if _, registered := nonBundleCorpora["differential"]; !registered {
		t.Fatal("seeds/differential is no longer registered; if it was deliberately removed, " +
			"this test and the registry entry go with it")
	}
	if _, registered := nonBundleCorpora["BUG-001"]; registered {
		t.Fatal("a Track A bundle is registered as a non-bundle corpus, which would exempt it " +
			"from replaying -- the one thing the corpus lane exists to require")
	}
	root := filepath.Join("..", "..", "seeds", "differential")
	sub, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("the registered corpus is unreadable: %v", err)
	}
	if len(sub) == 0 {
		t.Fatal("seeds/differential is empty")
	}
	t.Logf("seeds/differential holds %d entr(ies), checked by its own owners", len(sub))
}

// TestTheReproducesLaneAccountsForEveryDirectory refuses the removal of
// BUG-042's totals reconciliation.
//
// # Why a source pin, and why it is being written down as the weaker option
//
// The reconciliation was induced by hand: the counter was removed and the lane
// printed *"1 of 25 directories are unaccounted for."* That is a real induction
// and its output is in BUG-042. What it is not is a STANDING one -- nothing
// would notice the counter going away again, and every other fix landed this day
// carries something that would.
//
// `scripts/corpus-reproduces.sh` copies the whole tree once per bundle, so a
// self-test that exercises the loop is not cheap enough to sit in a push lane.
// The pin is what is affordable: it does not re-run the arithmetic, it refuses
// the removal of the arithmetic. **Stated plainly because a weaker instrument
// described as a strong one is this repository's own worst failure mode**, and
// the honest version is that this pin catches deletion and would not catch a
// wrong sum.
func TestTheReproducesLaneAccountsForEveryDirectory(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "corpus-reproduces.sh"))
	if err != nil {
		t.Fatalf("reading the lane: %v", err)
	}
	src := string(b)
	for _, want := range []struct{ frag, why string }{
		{"notbundle=$((notbundle + 1))",
			"the fourth drop path must COUNT what it drops; three of four did and the fourth " +
				"was how seeds/differential went past unseen"},
		{"accounted=$((",
			"the totals must reconcile against the population, or the counters describe what " +
				"the loop felt like doing rather than what it saw"},
		{"directories in seeds/",
			"the summary must report the POPULATION, not just the checked count: a count taken " +
				"when it happened to equal the population reads as a population forever after"},
	} {
		if !strings.Contains(src, want.frag) {
			t.Errorf("scripts/corpus-reproduces.sh no longer contains %q.\n      %s", want.frag, want.why)
		}
	}
}

// TestEveryCounterInTheReproducesLaneIsAccountedFor derives the buckets instead
// of pinning the expression.
//
// # Why derived and not literal
//
// The first version pinned `accounted=$((checked + skipped + notbundle))`
// verbatim. That is a literal, and a literal has to be edited every time a
// bucket is added -- so it cannot catch the thing it exists for. It did not:
// BUG-042 fixed one uncounted drop path and left the two ROT paths
// incrementing `failed` and nothing accounted, and the miss surfaced only when
// I1's strict-criterion run printed "1 of 25 directories are unaccounted for".
//
//	THE FIX FOR AN UNCOUNTED DROP PATH HAD AN UNCOUNTED DROP PATH, and a pin on
//	the fix's exact text could not see it.
//
// So this reads every `name=$((name + 1))` in the script and requires each to
// appear in the reconciliation, minus the two that are deliberately not
// buckets: `failed` counts outcomes rather than directories, and `dirs` IS the
// population being reconciled against.
func TestEveryCounterInTheReproducesLaneIsAccountedFor(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "corpus-reproduces.sh"))
	if err != nil {
		t.Fatalf("reading the lane: %v", err)
	}
	src := string(b)

	notBuckets := map[string]bool{"failed": true, "dirs": true}
	inc := regexp.MustCompile(`(\w+)=\$\(\((\w+) \+ 1\)\)`)
	var buckets []string
	for _, m := range inc.FindAllStringSubmatch(src, -1) {
		if m[1] == m[2] && !notBuckets[m[1]] {
			buckets = append(buckets, m[1])
		}
	}
	if len(buckets) == 0 {
		t.Fatal("no counters found, so this test is checking nothing")
	}

	acc := regexp.MustCompile(`accounted=\$\(\(([^)]*)\)\)`).FindStringSubmatch(src)
	if acc == nil {
		t.Fatal("no reconciliation found: the lane must add its buckets up and compare to the population")
	}
	seen := map[string]bool{}
	for _, name := range buckets {
		if !strings.Contains(acc[1], name) {
			t.Errorf("counter %q is incremented but is NOT in the reconciliation %q.\n"+
				"      A drop path that counts itself into a bucket nothing sums is invisible "+
				"in exactly the way an uncounted one is -- see BUG-042 and its own recurrence.",
				name, strings.TrimSpace(acc[1]))
		}
		seen[name] = true
	}
	t.Logf("buckets reconciled: %v", buckets)
}
