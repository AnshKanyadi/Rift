// Package monocore stands in for package clock in the fixtures: it defines the
// two reading types, so the Mono-leakage rule has something real to resolve.
package monocore

type Mono int64

type Wall int64
