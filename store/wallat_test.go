package store_test

import (
	"time"

	"github.com/anshkanyadi/rift/clock"
)

// wallAt is a physical clock pinned to one reading, for tests that care about
// the logical counter rather than about time passing.
type wallAt struct{ w clock.Wall }

func (f *wallAt) Wall() clock.Wall         { return f.w }
func (f *wallAt) Mono() clock.Mono         { return 0 }
func (f *wallAt) MaxOffset() time.Duration { return time.Second }
