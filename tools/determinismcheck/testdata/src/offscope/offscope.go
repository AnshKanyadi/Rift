// Package offscope is checked with the default scope table, which it does not
// match. Everything below is a core-rule violation and none of it is reported:
// the simulator, the real clock and the load generators all need exactly these
// constructs, and a pass that shouted at them would be switched off.
package offscope

import (
	"os"
	"sync"
	"time"
)

type sim struct {
	mu     sync.Mutex
	events chan int
	nodes  map[string]int
}

func (s *sim) run() {
	go func() {
		s.events <- 1
	}()

	for name := range s.nodes {
		_ = name
	}

	select {
	case <-s.events:
	case <-time.After(time.Millisecond):
	}

	_ = os.Getenv("RIFT_SEED")
	_ = time.Now()
}
