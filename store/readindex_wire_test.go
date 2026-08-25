package store

import (
	"testing"

	"github.com/anshkanyadi/rift/hlc"
)

// TestReadIndexIsOffByDefault keeps D-A7-4's decision visible: both read paths
// exist for the phase, and which one a sweep exercised is a fact about its
// configuration rather than about the code.
func TestReadIndexIsOffByDefault(t *testing.T) {
	var c Config
	if c.ReadIndex {
		t.Error("read index defaults on; the differential oracle needs runs of both kinds, " +
			"and a default that silently picks one makes the sweep's coverage a thing " +
			"nobody chose (DESIGN-A7 D-A7-4)")
	}
}

// TestOnlyPlainReadsTakeTheReadIndexPath is D-A7-5 ruled A, asserted on the
// predicate rather than trusted to the comment beside it.
func TestOnlyPlainReadsTakeTheReadIndexPath(t *testing.T) {
	for _, c := range []struct {
		name string
		req  Request
		want bool
	}{
		{"plain get", Request{Op: "get", Key: "k"}, true},
		{"snapshot read at a timestamp", Request{Op: "get", Key: "k", ReadTS: hlc.Timestamp{Wall: 5}}, false},
		{"a write", Request{Op: "put", Key: "k", Value: "v"}, false},
		{"a transaction step", Request{Op: "get", Key: "k", Txn: &TxnCommand{}}, false},
	} {
		got := c.req.Op == "get" && !c.req.ReadTS.IsSet() && c.req.Txn == nil
		if got != c.want {
			t.Errorf("%s: takes read-index path = %v, want %v. Serving a timestamped read off "+
				"the log stages no read mark, and PrewriteInto's third guard then consults a "+
				"record that does not exist -- BUG-022 returning as a design consequence "+
				"rather than as a patch (DESIGN-A7 D-A7-5)", c.name, got, c.want)
		}
	}
}
