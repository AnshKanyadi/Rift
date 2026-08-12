// Package monocore stands in for package clock in the fixtures: it defines the
// two reading types, so the Mono-leakage rule has something real to resolve.
package monocore

import "time"

type Mono int64

type Wall int64

// Sub and Add are the sanctioned spellings: a difference between two readings
// is a Duration, and advancing a reading by a Duration keeps its type.
func (t Mono) Sub(u Mono) time.Duration { return time.Duration(t - u) }
func (t Mono) Add(d time.Duration) Mono { return t + Mono(d) }
func (t Wall) Sub(u Wall) time.Duration { return time.Duration(t - u) }
func (t Wall) Add(d time.Duration) Wall { return t + Wall(d) }

func (t Mono) Before(u Mono) bool { return t < u }
func (t Wall) Before(u Wall) bool { return t < u }
