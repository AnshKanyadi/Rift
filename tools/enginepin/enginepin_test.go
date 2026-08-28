package enginepin_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheEngineWrapperIsTheOnlyPathToApply refuses a direct call past the
// tracking.
//
// The wrapper exists so that the store's visible sequence cannot drift from what
// the engine last returned. A `.Engine.Apply(` anywhere but in the wrapper's own
// method reintroduces exactly the drift the wrapper removes, and it would be
// caught -- if at all -- by a panic in readDurable on some later seed rather
// than here.
//
// This is a source pin and says so: it catches the syntactic bypass and would
// not catch a semantic one.
//
// IT LIVES UNDER tools/ FOR THE REASON gatepin's OWN COMMENT GIVES, which is a
// comment I read today and then ignored by writing this in store/ first: reading
// source text needs os, store/ is core scope, and core packages reach the
// outside world only through injected interfaces. The determinism pass refused
// it in one command. A rule written down in the repository is not a rule you
// have applied.
func TestTheEngineWrapperIsTheOnlyPathToApply(t *testing.T) {
	const dir = "../../store"
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, ".Engine.Apply(") {
				continue
			}
			found++
			if f.Name() != "engine.go" {
				t.Errorf("%s:%d calls the wrapped engine's Apply directly, past the "+
					"visible-sequence tracking:\n      %s", f.Name(), i+1, strings.TrimSpace(line))
			}
		}
	}
	if found == 0 {
		t.Fatal("no call to the wrapped engine's Apply exists at all, so this test is checking " +
			"nothing. If the wrapper was removed, remove this with it")
	}
}
