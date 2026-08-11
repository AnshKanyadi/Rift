package sorted_test

import (
	"cmp"
	"slices"
	"testing"

	"github.com/anshkanyadi/rift/internal/sorted"
)

func TestKeysAreSorted(t *testing.T) {
	m := map[string]int{"delta": 4, "alpha": 1, "charlie": 3, "bravo": 2}
	got := sorted.Keys(m)
	want := []string{"alpha", "bravo", "charlie", "delta"}
	if !slices.Equal(got, want) {
		t.Errorf("Keys = %v, want %v", got, want)
	}
}

// TestKeysAreStable is the property the package exists for. One call proving
// the keys come back sorted says nothing about the failure mode, which is a
// loop that agrees with itself for a hundred runs and then does not.
func TestKeysAreStable(t *testing.T) {
	m := make(map[int]struct{}, 64)
	for i := range 64 {
		m[i*7%64] = struct{}{}
	}

	first := sorted.Keys(m)
	for i := range 500 {
		if got := sorted.Keys(m); !slices.Equal(got, first) {
			t.Fatalf("iteration %d returned a different order: %v != %v", i, got, first)
		}
	}
	if !slices.IsSorted(first) {
		t.Errorf("Keys returned unsorted output: %v", first)
	}
}

func TestKeysFunc(t *testing.T) {
	type rangeID struct{ store, index int }
	m := map[rangeID]string{
		{store: 2, index: 1}: "b",
		{store: 1, index: 9}: "a",
		{store: 2, index: 0}: "c",
	}

	got := sorted.KeysFunc(m, func(a, b rangeID) int {
		if c := cmp.Compare(a.store, b.store); c != 0 {
			return c
		}
		return cmp.Compare(a.index, b.index)
	})
	want := []rangeID{{1, 9}, {2, 0}, {2, 1}}
	if !slices.Equal(got, want) {
		t.Errorf("KeysFunc = %v, want %v", got, want)
	}
}

func TestEmptyAndNil(t *testing.T) {
	if got := sorted.Keys(map[string]int{}); len(got) != 0 {
		t.Errorf("Keys(empty) = %v, want empty", got)
	}
	if got := sorted.Keys(map[string]int(nil)); len(got) != 0 {
		t.Errorf("Keys(nil) = %v, want empty", got)
	}
}
