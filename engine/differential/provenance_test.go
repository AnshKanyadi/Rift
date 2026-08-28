package differential

import (
	"errors"
	"testing"
)

// BOTH DIRECTIONS. A provenance check that only ever refuses is a check nobody
// can run, and one that only ever accepts is not a check.

func TestADirtyCommitInTheArtifactIsRefused(t *testing.T) {
	a := &Artifact{Provenance: Provenance{ModelCommit: "abc123-dirty"}}
	if err := CheckModelProvenance(a); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err = %v, want ErrDirtyWorktree", err)
	}
}

func TestAnUnresolvableCommitFailsRatherThanFallingBackToHead(t *testing.T) {
	// A syntactically valid object name that this repository does not contain.
	a := &Artifact{Provenance: Provenance{ModelCommit: "0000000000000000000000000000000000000000"}}
	err := CheckModelProvenance(a)
	if err == nil {
		t.Fatal("an unresolvable commit was accepted; a silent fallback to HEAD " +
			"would compare against a different reference and report the " +
			"difference as a divergence in the engine")
	}
	// A dirty worktree is also a legitimate refusal here and the test says so
	// rather than pinning whichever fires first.
	if !errors.Is(err, ErrUnresolvableCommit) && !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err = %v, want unresolvable or dirty", err)
	}
}
