// Package chaos runs a real cluster as separate OS processes and inflicts
// faults on it.
//
// # One process per node, and why the cheaper option is refused
//
// Ansh at I2: two configurations is the right price for `kill -9` meaning what
// it means. Goroutines-as-nodes cannot express a process dying with unsynced
// writes in flight, and:
//
//	A SUBSTITUTE THAT CANNOT EXPRESS A CLASS MAKES THAT CLASS INVISIBLE RATHER
//	THAN ABSENT.
//
// That is GF-49, measured at I1: three of five defects there could not have
// existed on engine/model, because a model engine has no files, no handles and
// no past. A goroutine "node" has the same problem one layer up — it has no
// process, so it cannot lose one.
//
// If a single-process configuration is kept for speed it is A FIXTURE WITH ITS
// LIMIT STATED, never evidence about crash behaviour.
//
// # What this package gates on, and what it merely reports
//
// Ansh's third ruling: gate on counters, report the verdict. A chaos run has no
// seed, so a red cannot be bisected and a green cannot be re-examined —
//
//	A CHAOS GREEN IS A STATEMENT THAT NOTHING WAS OBSERVED. IT IS NOT A
//	STATEMENT THAT NOTHING IS THERE.
//
// So the lane FAILS on the deterministic properties of the run having happened
// — nodes started, faults landed, operations completed, the history non-vacuous
// — and REPORTS checker verdicts as findings needing judgement. A run that
// killed a leader every ten seconds and recorded no elections is a broken
// harness, and it is indistinguishable from a clean run by every other signal.
package chaos

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Node is one cluster member, running as its own process.
type Node struct {
	ID   int
	Addr string
	Dir  string

	mu     sync.Mutex
	cmd    *exec.Cmd
	up     bool
	kills  int
	starts int
}

// Supervisor owns the cluster's processes.
type Supervisor struct {
	bin   string
	nodes []*Node

	mu     sync.Mutex
	counts Counters
}

// Counters is what the lane GATES on. Every field is a deterministic property
// of the run having happened, not a verdict about what it found.
type Counters struct {
	Started     int // process starts, including restarts
	Kills       int // SIGKILLs delivered
	Restarts    int // starts that followed a kill
	ExitedOther int // processes that died without being killed -- always a finding
}

// New builds a supervisor over n nodes.
func New(bin string, nodes []*Node) *Supervisor {
	return &Supervisor{bin: bin, nodes: nodes}
}

// Start launches every node.
func (s *Supervisor) Start() error {
	for _, n := range s.nodes {
		if err := s.startOne(n); err != nil {
			return fmt.Errorf("chaos: starting node %d: %w", n.ID, err)
		}
	}
	return nil
}

func (s *Supervisor) startOne(n *Node) error {
	if err := os.MkdirAll(n.Dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command(s.bin,
		"--id", fmt.Sprint(n.ID),
		"--addr", n.Addr,
		"--dir", n.Dir,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// SETPGID so a kill reaches the child and nothing else. Without it a
	// signal aimed at the group would reach the harness, and a chaos runner
	// that kills itself is a failure mode with a very confusing report.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	n.mu.Lock()
	n.cmd, n.up = cmd, true
	n.starts++
	n.mu.Unlock()

	s.mu.Lock()
	s.counts.Started++
	s.mu.Unlock()

	// Reap, so a killed process does not become a zombie and so an UNEXPECTED
	// exit is recorded. A node that dies on its own is not a fault we injected
	// and is always a finding -- distinguishing it from one we killed is the
	// whole reason this goroutine exists.
	go func() {
		err := cmd.Wait()
		n.mu.Lock()
		wasUp := n.up
		n.up = false
		n.mu.Unlock()
		if wasUp {
			s.mu.Lock()
			s.counts.ExitedOther++
			s.mu.Unlock()
			fmt.Fprintf(os.Stderr, "chaos: node %d exited WITHOUT being killed: %v\n", n.ID, err)
		}
	}()
	return nil
}

// Kill delivers SIGKILL. Not SIGTERM: a graceful shutdown flushes, and a
// flushed node is not the fault being injected.
//
//	kill -9 IS THE POINT. A process that gets to run a deferred Close has lost
//	nothing, and "the database survives a clean shutdown" is not a claim worth
//	a chaos lane.
func (s *Supervisor) Kill(n *Node) error {
	n.mu.Lock()
	cmd, up := n.cmd, n.up
	if up {
		n.up = false // claim it before signalling, so the reaper does not count this as unexpected
		n.kills++
	}
	n.mu.Unlock()
	if !up || cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		return err
	}
	s.mu.Lock()
	s.counts.Kills++
	s.mu.Unlock()
	return nil
}

// Restart brings a killed node back. Its directory is untouched, so recovery
// runs against whatever the kill left on disk -- which is the state a real
// crash produces and the one the engine's own recovery path exists for.
func (s *Supervisor) Restart(n *Node) error {
	if err := s.startOne(n); err != nil {
		return err
	}
	s.mu.Lock()
	s.counts.Restarts++
	s.mu.Unlock()
	return nil
}

// StopAll kills everything, for teardown rather than for chaos.
func (s *Supervisor) StopAll() {
	for _, n := range s.nodes {
		_ = s.Kill(n)
	}
	// Give the reapers a moment to run so the counters settle before a caller
	// reads them.
	time.Sleep(50 * time.Millisecond)
}

// Counters returns what happened. The lane gates on these.
func (s *Supervisor) Counters() Counters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts
}
