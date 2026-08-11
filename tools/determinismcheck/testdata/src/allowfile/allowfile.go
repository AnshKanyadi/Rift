//rift:allow-nondeterminism fixture: exercises the file-level escape hatch

// Package allowfile carries a whole-file hatch, the form reserved for the rare
// file whose entire job is the thing the rules forbid. Nothing here is
// reported; every suppression is printed instead, with the reason attached.
package allowfile

import "time"

func armed(timeout time.Duration) time.Time {
	return time.Now().Add(timeout)
}

func expired(deadline time.Time) bool {
	return time.Since(deadline) > 0
}
