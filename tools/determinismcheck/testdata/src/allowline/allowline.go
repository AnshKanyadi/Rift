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

// outOfRange is the boundary, and it costs two diagnostics rather than one: the
// call is unexcused, and the hatch that was meant to excuse it excused nothing.
// A hatch that has drifted off its line is the dangerous case -- something is
// now unguarded and someone believes it is not -- so it fails rather than warns.
func outOfRange(timeout time.Duration) time.Time {
	//rift:allow-nondeterminism fixture: too far from the call below // want `escape: this escape hatch excused nothing`

	return time.Now().Add(timeout) // want `time: time.Now`
}
