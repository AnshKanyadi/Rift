package hunt_test

import (
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/sim/hunt"
)

// TestTheShapeNameTracksTheShape is the fix for a label that stopped describing
// its subject, asserted rather than intended.
//
// Three instances by A7: `power-config: a3` meant "what the sweep runs" while
// pinned to a shape the sweep had left; one `power: n/a` label carried two
// opposite claims and made a class killed in a second read the same as a class
// nobody had measured; and `exit-run.sh` printed "A6 exit run" over a sweep of
// A7's shape. Each was cosmetic the day it was written.
//
// So the name is computed from the options, and this asserts the two agree. It
// fails when a phase turns something on and forgets the name -- which is the
// only moment it could be wrong.
func TestTheShapeNameTracksTheShape(t *testing.T) {
	if got := hunt.CurrentShapeName(); !strings.HasPrefix(got, "A7") {
		t.Errorf("CurrentOptions has read index on and the shape name is %q. A banner that "+
			"names the wrong phase is the third instance of a label that stopped describing "+
			"its subject, and the first two were also cosmetic on the day", got)
	}
	if !hunt.CurrentOptions().ReadIndex {
		t.Error("CurrentOptions no longer runs read index; A7's exit run does not sweep A7")
	}
	// And the name must MOVE when the shape does, or it is a constant wearing a
	// function's clothes.
	for _, c := range []struct {
		name string
		opt  hunt.RaftOptions
		want string
	}{
		{"A6", hunt.A6Options(), "A6"},
		{"A5", hunt.A5Options(), "A5"},
		{"A2", hunt.A2Options(), "A2"},
	} {
		if hunt.ShapeNameOf(c.opt) == hunt.CurrentShapeName() {
			t.Errorf("%s's shape names the same thing as current; the name is not derived", c.name)
		}
		if !strings.HasPrefix(hunt.ShapeNameOf(c.opt), c.want) {
			t.Errorf("%s's shape is named %q", c.name, hunt.ShapeNameOf(c.opt))
		}
	}
}
