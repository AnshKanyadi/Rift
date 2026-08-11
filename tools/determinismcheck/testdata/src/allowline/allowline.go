// Package allowline exercises the line-level escape hatch in both its forms,
// and the boundary of the two-line window that keeps a hatch attached to what
// it excuses.
package allowline

import "time"

func aboveTheLine(timeout time.Duration) time.Time {
	//rift:allow-nondeterminism fixture: hatch on the line above
	return time.Now().Add(timeout)
}

func trailing(timeout time.Duration) time.Time {
	return time.Now().Add(timeout) //rift:allow-nondeterminism fixture: trailing hatch
}

// outOfRange is the boundary: a hatch does not reach past the line below it, so
// a hatch cannot drift away from the thing it was written for and quietly go on
// excusing whatever lands there later.
func outOfRange(timeout time.Duration) time.Time {
	//rift:allow-nondeterminism fixture: too far from the call below

	return time.Now().Add(timeout) // want `time: time.Now`
}
