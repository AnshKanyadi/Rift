//go:build rift_cgo

package riftcgo

import (
	"testing"

	"github.com/anshkanyadi/rift/engine"
	"github.com/anshkanyadi/rift/engine/model"
)

// TestANilValueIsTheSameAsAnEmptyOne is BUG-B008's covering test.
//
// It is directed rather than a sweep because the defect is a single mapping at
// a single call site, and because the instrument that SHOULD have found it --
// the differential -- cannot: its generator emits values of 1 to 40 bytes, so a
// zero-length value is outside its input space. A sweep through that generator
// would run forever without presenting this input once.
func TestANilValueIsTheSameAsAnEmptyOne(t *testing.T) {
	db, err := Open(t.TempDir(), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ref := model.New()
	for _, tc := range []struct {
		name  string
		value []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"one byte", []byte("v")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := []byte("k/" + tc.name)
			if _, err := ref.Apply(engine.NewBatch().Set(k, tc.value), false); err != nil {
				t.Fatalf("the model rejected it, so this test is about the wrong thing: %v", err)
			}
			if _, err := db.Apply(engine.NewBatch().Set(k, tc.value), false); err != nil {
				t.Fatalf("cgo rejected a value engine/model accepts: %v", err)
			}
			got, err := db.Get(k)
			if err != nil {
				t.Fatalf("reading it back: %v", err)
			}
			want, err := ref.Get(k)
			if err != nil {
				t.Fatalf("model read: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("cgo %q, model %q", got, want)
			}
		})
	}

	// And a nil BOUND still means unbounded, which is the half that must NOT
	// change: rift.cc carries the distinction in the pointer deliberately.
	if _, err := db.Apply(engine.NewBatch().DeleteRange(nil, nil), false); err != nil {
		t.Fatalf("an unbounded DeleteRange was refused, so the fix reached a case it must not: %v", err)
	}
}
