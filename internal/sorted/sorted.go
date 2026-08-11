// Package sorted is the only place in Rift that iterates a map.
//
// Go randomizes map iteration order deliberately, so a loop over a map that
// decides anything -- which replica to send to first, which lock to resolve --
// produces a different history on every run from the same seed. The
// determinism pass bans the loop outright in every package that executes during
// a simulated run, which includes this one: collecting keys means ranging the
// map, so the one range statement left in the repo lives here under a hatch,
// and everything else calls Keys.
//
// The hatch is registered in HATCHES.txt and checked there, so this exception
// is a line in a reviewed list rather than a habit.
package sorted

import (
	"cmp"
	"slices"
)

// Keys returns m's keys in ascending order.
func Keys[K cmp.Ordered, V any](m map[K]V) []K {
	ks := keys(m)
	slices.Sort(ks)
	return ks
}

// KeysFunc returns m's keys ordered by compare, for keys that are comparable
// but not ordered -- a range descriptor, a (node, range) pair. It exists so
// that meeting such a key is not a reason to write a map range somewhere else.
func KeysFunc[K comparable, V any](m map[K]V, compare func(a, b K) int) []K {
	ks := keys(m)
	slices.SortFunc(ks, compare)
	return ks
}

// keys collects m's keys in whatever order the runtime feels like. Every caller
// sorts before returning, so the order is never observable outside this file.
func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	//rift:allow-nondeterminism the one map range in the repo; every caller sorts before returning, so this order is never observable
	for k := range m {
		out = append(out, k)
	}
	return out
}
