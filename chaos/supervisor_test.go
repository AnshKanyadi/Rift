package chaos_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anshkanyadi/rift/chaos"
)

// buildSleeper builds a stand-in node: a process that starts, writes a file to
// prove it ran, and then blocks until killed.
//
// It is a FIXTURE and says so. What these tests check is the supervisor -- that
// a kill is a kill, that an unexpected exit is distinguished from an injected
// one, and that the counters the lane gates on are true. None of that needs a
// real node, and using one would make a supervisor bug and a node bug
// indistinguishable in the failure.
func buildSleeper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	prog := `package main

import ("flag";"os";"os/signal";"path/filepath";"syscall";"time")

func main() {
	id := flag.String("id", "", "")
	_ = flag.String("addr", "", "")
	d := flag.String("dir", "", "")
	exitNow := os.Getenv("SLEEPER_EXIT") != ""
	flag.Parse()
	if *d != "" {
		_ = os.WriteFile(filepath.Join(*d, "started-"+*id), []byte("x"), 0o644)
	}
	if exitNow {
		os.Exit(3)
	}
	// TRAPS SIGTERM AND WRITES A MARKER. This is what makes "kill -9 is the
	// point" checkable rather than merely stated: a graceful shutdown leaves
	// evidence behind, and a SIGKILL cannot.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		if *d != "" {
			_ = os.WriteFile(filepath.Join(*d, "graceful-"+*id), []byte("x"), 0o644)
		}
		os.Exit(0)
	}()
	for { time.Sleep(time.Hour) }
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "sleeper")
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the fixture: %v\n%s", err, out)
	}
	return bin
}

func TestAKillIsAKillAndTheCountersSaySo(t *testing.T) {
	bin := buildSleeper(t)
	root := t.TempDir()
	nodes := []*chaos.Node{
		{ID: 1, Addr: "127.0.0.1:0", Dir: filepath.Join(root, "n1")},
		{ID: 2, Addr: "127.0.0.1:0", Dir: filepath.Join(root, "n2")},
	}
	s := chaos.New(bin, nodes)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()

	// The processes must actually have run. A supervisor that reports two
	// starts having started nothing is the shape BUG-046 was about.
	waitFor(t, filepath.Join(root, "n1", "started-1"))
	waitFor(t, filepath.Join(root, "n2", "started-2"))

	if _, err := s.Kill(nodes[0]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	// SIGKILL, NOT SIGTERM. The fixture traps SIGTERM and leaves a marker; a
	// killed process cannot. A graceful shutdown flushes, and a flushed node is
	// not the fault being injected -- "the database survives a clean shutdown"
	// is not a claim worth a chaos lane.
	if _, err := os.Stat(filepath.Join(root, "n1", "graceful-1")); err == nil {
		t.Error("the node shut down GRACEFULLY: it was sent SIGTERM, not SIGKILL, so it got to run " +
			"its deferred closes and lost nothing. That is not the fault this lane injects")
	}

	c := s.Counters()
	if c.Started != 2 {
		t.Errorf("Started=%d, want 2", c.Started)
	}
	if c.Kills != 1 {
		t.Errorf("Kills=%d, want 1", c.Kills)
	}
	if c.ExitedOther != 0 {
		t.Errorf("ExitedOther=%d, want 0: a node we killed was counted as having died on its own, "+
			"which would turn every injected fault into a spurious finding", c.ExitedOther)
	}
}

// TestANodeThatDiesOnItsOwnIsDistinguishedFromOneWeKilled.
//
// This is the distinction the whole lane rests on. A process we killed is the
// fault we injected; a process that exited by itself is a FINDING. Collapsing
// them either hides real crashes or reports every injected kill as one.
func TestANodeThatDiesOnItsOwnIsDistinguishedFromOneWeKilled(t *testing.T) {
	bin := buildSleeper(t)
	t.Setenv("SLEEPER_EXIT", "1") // the fixture exits immediately, uninvited

	root := t.TempDir()
	nodes := []*chaos.Node{{ID: 1, Addr: "127.0.0.1:0", Dir: filepath.Join(root, "n1")}}
	s := chaos.New(bin, nodes)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.Counters().ExitedOther > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c := s.Counters()
	if c.ExitedOther != 1 {
		t.Fatalf("a node that exited on its own was not recorded: ExitedOther=%d, Kills=%d. "+
			"An uninvited exit is a finding, and a lane that cannot see one reports a dead "+
			"cluster as a quiet one", c.ExitedOther, c.Kills)
	}
	if c.Kills != 0 {
		t.Errorf("Kills=%d after no kill was issued", c.Kills)
	}
}

func TestARestartReusesTheDirectory(t *testing.T) {
	bin := buildSleeper(t)
	root := t.TempDir()
	n := &chaos.Node{ID: 7, Addr: "127.0.0.1:0", Dir: filepath.Join(root, "n7")}
	s := chaos.New(bin, []*chaos.Node{n})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()
	waitFor(t, filepath.Join(root, "n7", "started-7"))

	// A marker the restart must NOT erase: recovery runs against whatever the
	// kill left on disk, which is the state a real crash produces.
	marker := filepath.Join(root, "n7", "survives")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Kill(n); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := s.Restart(n); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the restart wiped the node's directory: %v.\n"+
			"      A crash that starts from an empty disk is not a crash, it is a new node, and "+
			"the engine's recovery path would never run", err)
	}
	c := s.Counters()
	if c.Restarts != 1 || c.Started != 2 {
		t.Errorf("Restarts=%d Started=%d, want 1 and 2", c.Restarts, c.Started)
	}
}

func waitFor(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared: the process did not run, so anything this test reports afterwards "+
		"is about a cluster that does not exist", path)
}

// BUG-053: a kill followed immediately by a restart must not report the killed
// process as having died on its own.
//
// The failure is a race between the dying process's reaper and the new
// process's start, so it is exercised the way it actually occurred: kill and
// restart back to back, several times, with no sleep between them.
func TestAKilledProcessIsNotReportedAsAnUninvitedExitAcrossARestart(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes")
	}
	// A LINGERER, not the plain sleeper, and the difference is what makes this
	// deterministic rather than lucky.
	//
	// cmd.Wait does not return when the process dies; it returns when the
	// process has died AND the stderr copier has seen EOF. The plain sleeper
	// leaves no other holder of the pipe, so Wait returns in microseconds and
	// the reaper always wins the race -- 60 kill/restart pairs against the
	// buggy code produced zero failures.
	//
	//	A RACE THAT ONE SIDE ALWAYS WINS IS NOT REPRODUCED BY REPETITION. It is
	//	reproduced by making the other side slow ON PURPOSE.
	//
	// The lingerer leaves a grandchild holding the inherited stderr, so Wait
	// blocks for a fixed second after the kill -- long past the restart, which
	// is the interleaving the real chaos run hit once in three kills.
	bin := buildLingerer(t)
	root := t.TempDir()
	n := &chaos.Node{ID: 1, Addr: "127.0.0.1:0", Dir: filepath.Join(root, "n1")}
	s := chaos.New(bin, []*chaos.Node{n})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.StopAll()

	for i := 0; i < 4; i++ {
		if _, err := s.Kill(n); err != nil {
			t.Fatal(err)
		}
		if err := s.Restart(n); err != nil {
			t.Fatal(err)
		}
	}
	// Let every reaper finish before reading. A count read while a reaper is
	// still blocked in Wait is a count of the races that happened to be over --
	// and with the lingerer every reaper is deliberately slow, so this wait is
	// load-bearing rather than defensive.
	time.Sleep(2 * time.Second)

	if c := s.Counters(); c.ExitedOther != 0 {
		t.Fatalf("%d process(es) reported as uninvited exits after %d ordered kills.\n"+
			"      A per-NODE expectation is read by the wrong process across a restart: the dying\n"+
			"      process's reaper sees the flag the new process just set. Counters: %+v",
			c.ExitedOther, 4, c)
	}
}

// buildLingerer is a fixture whose exit is SLOW TO OBSERVE.
//
// It spawns a grandchild that inherits stderr and outlives it, so the parent's
// cmd.Wait blocks on the pipe long after the parent is gone. That is the only
// way to make the reaper lose the restart race on purpose; see BUG-053.
func buildLingerer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	prog := `package main

import ("flag";"os";"os/exec";"path/filepath";"time")

func main() {
	id := flag.String("id", "", "")
	_ = flag.String("addr", "", "")
	d := flag.String("dir", "", "")
	flag.Parse()
	// The grandchild holds the inherited stderr open past this process's death.
	c := exec.Command("/bin/sleep", "1")
	c.Stderr = os.Stderr
	_ = c.Start()
	if *d != "" {
		_ = os.WriteFile(filepath.Join(*d, "started-"+*id), []byte("x"), 0o644)
	}
	for { time.Sleep(time.Hour) }
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "lingerer")
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the fixture: %v\n%s", err, out)
	}
	return bin
}
