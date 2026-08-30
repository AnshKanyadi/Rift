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
	"strings"
	"sync"
	"syscall"
	"time"
)

// launch is ONE process. A node has many over a run, and the distinction is
// load-bearing rather than tidy.
//
// # BUG-053: an expectation stored per NODE is read by the wrong process
//
// The first version kept a single `up` flag on Node, cleared by Kill before
// signalling so the reaper would not count the death as unexpected. Then a
// restart set it back to true for the NEW process -- and the OLD process's
// reaper, still blocked in Wait, woke up, read the flag, and reported a node
// that had been deliberately killed as one that died on its own.
//
//	THE FLAG ANSWERED "IS THIS NODE UP", AND THE QUESTION THE REAPER HAS IS "WAS
//	MY PROCESS SUPPOSED TO DIE". Those are the same question only while there is
//	one process, which is exactly the case a chaos run leaves.
//
// So the expectation lives with the process it is about. Each reaper closes over
// its own launch and can never read another's.
type launch struct {
	cmd    *exec.Cmd
	pid    int
	killed bool // set by Kill BEFORE signalling: this death was ordered
}

// Node is one cluster member, running as its own process.
type Node struct {
	ID   int
	Addr string
	Dir  string

	mu     sync.Mutex
	cur    *launch // the process that is meant to be running, or nil
	kills  int
	starts int
}

// PID is this node's operating-system process id, or 0 when it is down.
//
// It is exposed because I2's claim is SEPARATE PROCESSES, and the only proof of
// that is distinct pids that are not this process's. A cluster of goroutines
// would satisfy every other counter here.
func (n *Node) PID() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cur == nil {
		return 0
	}
	return n.cur.pid
}

// Supervisor owns the cluster's processes.
type Supervisor struct {
	bin   string
	nodes []*Node
	extra func(*Node) []string // per-node arguments beyond the common three

	stderrMu sync.Mutex
	stderr   strings.Builder

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

// NewWithArgs adds per-node arguments -- peer lists, chiefly.
func NewWithArgs(bin string, nodes []*Node, extra func(*Node) []string) *Supervisor {
	return &Supervisor{bin: bin, nodes: nodes, extra: extra}
}

// Stderr is everything the nodes wrote. A node's own exit counters arrive this
// way, and they are the only evidence available after it is gone.
func (s *Supervisor) Stderr() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	return s.stderr.String()
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
	args := []string{"--id", fmt.Sprint(n.ID), "--addr", n.Addr, "--dir", n.Dir}
	if s.extra != nil {
		args = append(args, s.extra(n)...)
	}
	cmd := exec.Command(s.bin, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = &teeStderr{s: s}
	// SETPGID so a kill reaches the child and nothing else. Without it a
	// signal aimed at the group would reach the harness, and a chaos runner
	// that kills itself is a failure mode with a very confusing report.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	l := &launch{cmd: cmd, pid: cmd.Process.Pid}
	n.mu.Lock()
	n.cur = l
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
		// ASK ABOUT THIS LAUNCH, never about the node. See BUG-053: a restart
		// makes "is the node up" true again while this process is still dying.
		n.mu.Lock()
		ordered := l.killed
		if n.cur == l {
			n.cur = nil
		}
		n.mu.Unlock()
		if !ordered {
			s.mu.Lock()
			s.counts.ExitedOther++
			s.mu.Unlock()
			fmt.Fprintf(os.Stderr, "chaos: node %d (pid %d) exited WITHOUT being killed: %v\n",
				n.ID, l.pid, err)
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
	l := n.cur
	if l != nil {
		// Claimed BEFORE signalling, on the launch itself, so the reaper for
		// THIS process sees the order however fast the death arrives.
		l.killed = true
		n.cur = nil
		n.kills++
	}
	n.mu.Unlock()
	if l == nil || l.cmd == nil || l.cmd.Process == nil {
		return nil
	}
	if err := l.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		return err
	}
	// LOGGED WITH THE PID. An uninvited exit names its pid; a kill names the pid
	// it signalled. One occurrence with both lines closes OPEN-I2-1 or opens a
	// second hypothesis, and neither is possible without the pair.
	s.stderrMu.Lock()
	s.stderr.WriteString(fmt.Sprintf("chaos: killed node %d (pid %d)\n", n.ID, l.pid))
	s.stderrMu.Unlock()
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

// Reality is the evidence that this cluster is what the phase claims: separate
// operating-system processes, not goroutines wearing the same interface.
//
// I1 supplied the measured instance of the alternative. BUG-046: a run reported
// a byte-identical trace hash having never opened the engine, and nothing in the
// verdict distinguished it from success. The equivalent here is a "cluster" in
// one process with an in-memory transport, which would produce a clean history
// and satisfy every checker.
//
//	THE COUNTERS GO IN BEFORE THE FIRST GREEN, NOT AFTER IT.
type Reality struct {
	PIDs     []int // one per live node
	Distinct int   // how many are distinct and not this process's
	Self     int
}

// Reality reports the process evidence.
func (s *Supervisor) Reality() Reality {
	r := Reality{Self: os.Getpid()}
	seen := map[int]bool{}
	for _, n := range s.nodes {
		p := n.PID()
		r.PIDs = append(r.PIDs, p)
		if p != 0 && p != r.Self && !seen[p] {
			seen[p] = true
		}
	}
	r.Distinct = len(seen)
	return r
}

// teeStderr copies a node's stderr to this process's and retains it, so exit
// counters survive the node that printed them.
type teeStderr struct{ s *Supervisor }

func (t *teeStderr) Write(p []byte) (int, error) {
	t.s.stderrMu.Lock()
	t.s.stderr.Write(p)
	t.s.stderrMu.Unlock()
	return os.Stderr.Write(p)
}
