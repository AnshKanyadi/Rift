package differential

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PROVENANCE CHECKS, B4-Q1's ruling made mechanical.
//
//	The judge compares against engine/model at a NAMED COMMIT, recorded in the
//	artifact. A replay that cannot resolve that commit FAILS LOUDLY rather than
//	falling back to HEAD, and the judge REFUSES to run against a dirty
//	engine/model worktree.
//
// Track A's reason, one layer out: an exit run at an uncommitted tree names a
// commit that does not contain what ran — and a differential artifact naming no
// commit is the same defect.

var (
	// ErrUnresolvableCommit is what a replay gets instead of a silent fallback.
	ErrUnresolvableCommit = errors.New("differential: the artifact's model commit cannot be resolved")
	// ErrDirtyWorktree refuses a judgment nobody could reproduce.
	ErrDirtyWorktree = errors.New("differential: engine/model has uncommitted changes")
)

// CheckModelProvenance verifies that the model this process links can honestly
// be described by the artifact's recorded commit.
//
// It runs git rather than trusting a build-time constant, for the reason
// rift_diff takes its commits from the environment: a constant compiled in goes
// stale on the next commit and is worse than an absent one.
func CheckModelProvenance(a *Artifact) error {
	if strings.HasSuffix(a.Provenance.ModelCommit, "-dirty") {
		return fmt.Errorf("%w: artifact names %q", ErrDirtyWorktree, a.Provenance.ModelCommit)
	}
	// A DIRTY engine/model IS REFUSED WHETHER OR NOT THE ARTIFACT ADMITS IT.
	// The artifact records what the PRODUCER believed; this checks what is
	// actually here, and the two disagreeing is exactly the case worth catching.
	out, err := exec.Command("git", "status", "--porcelain", "--", "engine/model").Output()
	if err == nil && len(strings.TrimSpace(string(out))) != 0 {
		return fmt.Errorf("%w: %s", ErrDirtyWorktree, strings.TrimSpace(string(out)))
	}
	// THE COMMIT MUST RESOLVE. `git cat-file -e` answers "does this object
	// exist here" without checking anything out, which is the question — the
	// judge is not going to run a different version of the model, it is going
	// to REFUSE if the one it has is not the one named.
	if err := exec.Command("git", "cat-file", "-e", a.Provenance.ModelCommit+"^{commit}").Run(); err != nil {
		return fmt.Errorf("%w: %q", ErrUnresolvableCommit, a.Provenance.ModelCommit)
	}
	return nil
}
