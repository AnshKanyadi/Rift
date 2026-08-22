package hunt

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim/plan"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/store"
)

// The bank workload and its transaction coordinator.
//
// # Why the coordinator lives here and not in store/
//
// A transaction spans ranges, and a range's state machine may not read
// another's. So the thing that sequences prewrite-primary-commit is a CLIENT: it
// issues each step as an ordinary request, waits for the step to apply, and
// moves on. That is what a router does at A4 and what `router/` will hold when
// real mode arrives; putting it in the store would be giving one range's state
// machine a view of the cluster that no state machine has.
//
// # Why a coordinator can die
//
// It is driven by the event loop, so a crash of the node it is talking to leaves
// it waiting, and the workload gives up on it after a deadline. That is the
// whole point: the interesting failures are all in the recovery path of a
// coordinator that died mid-commit, and a workload whose coordinators always
// finish would exercise none of them (DESIGN-A6 §1).

// account renders account i as a key.
//
// # The bank has its own key space, and that is a correctness boundary
//
// The plain workload writes with put and reads with ReadAt, which sees the
// newest DATA version and knows nothing about commit records. The bank writes
// with prewrite and commit, where a data version is invisible until its commit
// record exists. Point both at one key and a plain read returns a value no
// transaction has committed -- which is not a bug in either path, it is a client
// mixing two protocols on one key.
//
// Separating them says so structurally instead of hoping. Fixed width, so the
// key space is ordered the way the split logic expects.
func account(i int) string { return fmt.Sprintf("a%02d", i) }

// balance renders an account's value: the amount it holds, and which transfer
// last wrote it.
//
// # The writer's identity is part of the value, and it earns its place twice
//
// It makes a wrong answer say WHOSE write it is, which turned a conservation
// failure from "the total moved by 23" into a named transaction in one step.
// And it lets the snapshot-isolation oracle talk about visibility -- "this
// snapshot sees transaction 8 on one key and not on the other" -- which is the
// language the property is actually stated in.
func balance(v, txn int) string { return strconv.Itoa(v) + "@" + strconv.Itoa(txn) }

// parseBalance reads one back. The writer is returned separately because no
// arithmetic is ever done on it: it is provenance, not data.
func parseBalance(s string) (int, int, bool) {
	amt, who, ok := strings.Cut(s, "@")
	if !ok {
		return 0, 0, false
	}
	v, err := strconv.Atoi(amt)
	if err != nil {
		return 0, 0, false
	}
	w, err := strconv.Atoi(who)
	if err != nil {
		return 0, 0, false
	}
	return v, w, true
}

// txnPhase is where a transfer has got to.
type txnPhase uint8

const (
	// phaseRead is the transaction's SNAPSHOT: both accounts read at startTS.
	// A transfer that computed its new balances from numbers the harness was
	// carrying around would exercise the write half of the protocol and none of
	// the read half, and the read half is where locks, uncertainty and
	// resolution live.
	phaseRead txnPhase = iota
	phasePrewrite
	phasePrimaryRecord
	phaseCommitKeys
	phaseAbort
	phaseDone
	phaseAbandoned
)

// maxRestarts bounds how many times one transfer may restart above an uncertain
// commit, and maxResolves how many locks one read may clear before giving up.
//
// Bounded rather than unbounded because an unbounded retry in a simulated run
// is a hang, and because a transfer that gives up is not a failure: it leaves
// locks, which is the recovery path's raw material.
const (
	maxRestarts = 3
	maxResolves = 6

	// maxAuditResolves is the audit's larger budget.
	//
	// An audit cannot hurry a live lock owner: the resolver's correct verdict is
	// WAIT, and the only thing that clears the lock is the owner finishing or
	// its TTL running out. A transfer may take as long as its own deadline, so
	// an audit that gives up in six attempts gives up on locks that were about
	// to clear. Measured: 6 attempts completed 10 audits in 20 seeds, and the
	// budget below completes enough to check.
	//
	// This is patience, not tolerance. The audit still has to see every account
	// at one timestamp or be discarded; what changed is how long it is willing
	// to wait for a correct answer, not what it accepts as one.
	maxAuditResolves = 16

	// maxAuditRestarts is larger than a transaction's for an arithmetic reason.
	//
	// An audit reads every account at once, so several reads can come back
	// uncertain naming DIFFERENT commits, and the restart is taken above
	// whichever answer arrived first -- which need not be the highest. Each
	// round clears every commit below the one it restarted above, so it
	// converges, but it converges in rounds rather than in one step, and three
	// is not enough rounds for eight accounts.
	maxAuditRestarts = 8

	// auditPoll and auditPolls bound the re-asking.
	auditPoll  = clock.Instant(400 * time.Millisecond)
	auditPolls = 6

	// secondPassDelay is how long after a completed audit its accounts are
	// re-read at the same timestamp. Long enough that other transactions have
	// committed in between, which is the only way the second pass can differ
	// from the first.
	secondPassDelay = clock.Instant(600 * time.Millisecond)

	// auditLookback is how far into the past an audit takes its snapshot.
	//
	// # An auditor asks about a settled instant, and that is not a trick
	//
	// An audit at NOW must wait for every transaction currently in flight, and
	// it cannot hurry any of them: the resolver's correct verdict against a live
	// owner is WAIT. With transfers starting every few hundred milliseconds
	// there is almost always one, so an audit at now completed 14 times in 333
	// attempts -- which is a conservation check that mostly does not happen.
	//
	// An audit a second back asks about an instant whose transactions have
	// either finished or been abandoned, and an abandoned one's lock is past its
	// TTL, so the auditor can expire it and read through. That is exactly what
	// makes it a stronger check rather than a weaker one: it exercises the
	// expiry path, the roll-forward path and the roll-back path on the way to
	// its answer, where an audit at now mostly exercises waiting.
	//
	// Two TTLs, and comfortably inside the two-second collection retention: an
	// audit below the mark is REFUSED, which is a correct answer to a question
	// about a time the cluster no longer keeps, and is counted as such.
	auditLookback = 2 * kv.DefaultTTL

	// resolveBackoff is how long a reader waits before asking again after it
	// resolved a lock.
	//
	// # Why it is not zero
	//
	// The most common verdict is WAIT: the owner is alive by its own TTL, and
	// the resolver correctly leaves it alone. Asking again immediately gets the
	// same verdict, so a reader with an immediate retry burns its whole retry
	// budget inside a millisecond and gives up on a lock that would have
	// cleared. Measured: audits completing 9 times in 20 seeds with no backoff.
	//
	// Half the default TTL, so a reader that meets a dead owner outlives it
	// within two attempts and one that meets a live owner gives it room to
	// finish.
	resolveBackoff = clock.Instant(kv.DefaultTTL / 2)
)

// transfer is one bank transaction: read two accounts at a snapshot, move an
// amount between them, commit both.
//
// # The balances come from the READS, never from the harness
//
// This is DESIGN-A6 section 7 in one field. `read` is filled from what the
// cluster answered, `values` is computed from `read`, and conservation is then a
// property of what clients observed rather than of what the workload intended.
// A coordinator that knew the balances would be checking its own arithmetic.
type transfer struct {
	id int

	// origin is this transfer's request identity, unique for the run. Not the
	// start timestamp: see store.TxnCommand.Origin for what that cost.
	origin uint64

	startTS  hlc.Timestamp
	commitTS hlc.Timestamp
	primary  string
	keys     []string
	from, to string
	amount   int

	// ceiling is the top of this transaction's uncertainty interval, fixed at
	// its first snapshot and unchanged by every restart (kv.UncertaintyCeiling).
	ceiling hlc.Timestamp

	// ts is the node this transaction takes its timestamps from. Drawn from the
	// plan per transaction rather than fixed at node zero: one source for the
	// whole workload is a TSO wearing an HLC's clothes, and uncertainty
	// intervals exist precisely because there is more than one source.
	ts int

	// blocked maps a primary (and its start timestamp) to the key of THIS
	// transaction whose lock is waiting on it.
	//
	// A map and not a field. It was a field, and a transfer whose two keys were
	// both locked by the same transaction overwrote it: the first key's
	// resolution came back, looked up the field, and re-read the SECOND key.
	// The first key was then never read again -- and nothing noticed, because
	// its slot was counted by the counter below rather than by its own name.
	blocked map[string]string

	read   map[string]int
	values map[string]string

	phase txnPhase

	// ack is which keys have answered in the CURRENT phase. A set for the same
	// reason `read` is one: two answers for one key are one fact, and a counter
	// cannot tell the difference. Reset at every phase transition.
	ack map[string]bool

	restarts int
	resolves map[string]int
	node     sim.NodeID
}

// coordinator drives transfers over the store's client interface.
type coordinator struct {
	drivers []*store.Node
	nodes   int
	hist    *sim.History
	ledger  txnLedger

	live map[uint64]*transfer // keyed by request identity, never by timestamp

	// deadline is how long a transfer waits for a step before it is abandoned.
	// Abandoned is not failure: a coordinator that stops is exactly the case
	// resolution exists for, and the sweep needs them.
	deadline clock.Instant

	// nextOrigin hands out request identities. A counter and not a timestamp:
	// see store.TxnCommand.Origin.
	nextOrigin uint64

	// identities is every (primary, start timestamp) pair the run has issued.
	identities map[string]bool

	accounts int
	count    int

	audits map[uint64]*audit

	started   int
	committed int
	abandoned int
	resolves  int

	// The A6 evidence counters. Every one is asserted or deleted in the exit
	// run (DESIGN-A4 section 9.4b), and each names a path that is worthless if
	// it never ran: reads that found a lock, restarts above an uncertain
	// commit, audits that saw every account at one instant.
	reads              int
	readerResolves     int
	restarts           int
	refusedReads       int
	unparseable        int
	auditsStarted      int
	auditsComplete     int
	auditsLocked       int
	auditsUncertain    int
	auditsRetried      int
	identityCollisions int
	secondPass         int
	resolveWaited      int
	resolvedForward    int
	resolvedBack       int
	aborted            int
	lostToResolver     int
}

// audit is a client reading EVERY account at one timestamp.
//
// # Why conservation needs this and not a sum over transfers
//
// Summing what transactions intended is checking the workload's arithmetic. The
// bank invariant is a statement about a SNAPSHOT: at any instant, the accounts
// sum to what they summed to at the beginning. An audit is the only client
// operation that can witness that, and it is exactly the operation a real
// auditor would run.
//
// It completes only when every account answered AT THE SAME TIMESTAMP. A
// partial audit is discarded rather than checked over the accounts it got,
// because a sum over a subset conserves nothing.
type audit struct {
	id     int
	origin uint64
	readTS hlc.Timestamp
	seen   map[string]int
	need   int

	// ceiling is fixed at the audit's FIRST snapshot and survives its restarts,
	// for the same reason a transaction's does: an audit whose window moved up
	// with its read timestamp would meet a fresh set of uncertain commits every
	// time and never settle. Measured, before it was carried: 202 restarts and
	// 13 completions across 20 seeds.
	ceiling hlc.Timestamp

	resolves map[string]int
	restarts int
	done     bool

	// blocked maps a primary (and start timestamp) to the audit key whose lock
	// is waiting on it.
	blocked map[string]string
}

// txnLedger is what the coordinator tells the harness's record. It is an
// interface so the oracle's ledger and a test's stub are interchangeable, and so
// that nothing here can reach into the ledger for anything but recording.
type txnLedger interface {
	RecordTxnBegin(id int, startTS hlc.Timestamp, primary string, keys []string, at clock.Instant)
	RecordTxnCommit(id int, commitTS hlc.Timestamp, at clock.Instant)
	RecordAudit(readTS hlc.Timestamp, total, accounts int, at clock.Instant)
}

// begin takes the transaction's snapshot: both accounts, read at startTS.
func (c *coordinator) begin(t *transfer, at clock.Instant, s sim.Scheduler) {
	// # The identity Percolator assumes, asserted rather than assumed
	//
	// A transaction record is addressed by (primary key, start timestamp), and
	// two transactions sharing that pair would share a record: the second's
	// decision would be refused as already made, and it would silently adopt
	// the first's fate. Percolator is safe here because a single TSO issues
	// start timestamps; with a per-node HLC nothing guarantees it.
	//
	// The exit run asserts this at zero. The day it fires, the identity is the
	// thing to fix -- a transaction id in the record key, or the TSO fallback
	// Amendment A6 pre-authorises -- and not this assertion.
	if key := t.primary + "@" + t.startTS.String(); c.identities[key] {
		c.identityCollisions++
	} else {
		c.identities[key] = true
	}
	c.live[t.origin] = t
	c.started++
	c.ledger.RecordTxnBegin(t.id, t.startTS, t.primary, t.keys, at)
	t.phase = phaseRead
	t.read = map[string]int{}
	for _, k := range t.keys {
		c.readAt(t, k, at, s)
	}
}

// readAt issues one snapshot read for a transfer.
func (c *coordinator) readAt(t *transfer, key string, at clock.Instant, s sim.Scheduler) {
	c.reads++
	c.send(t, store.TxnCommand{
		Op: store.OpTxnGet, Key: key, StartTS: t.startTS, ReadTS: t.startTS,
		MaxTS: t.ceiling,
	}, at, s)
}

// prewrite moves a transfer from its snapshot to its writes.
func (c *coordinator) prewrite(t *transfer, at clock.Instant, s sim.Scheduler) {
	// The new balances are computed HERE, from what was read, and this is the
	// only place in the workload that does arithmetic on a balance.
	// # The assertion that would have caught it in one line
	//
	// A transfer writes what it read. Writing a key it never read is inventing
	// money, and the resulting conservation failure looks exactly like a
	// database losing a write -- so it costs a full investigation to find out
	// the workload did it. The check is one line and it belongs where the
	// arithmetic is.
	for _, k := range t.keys {
		if _, ok := t.read[k]; !ok {
			panic(fmt.Sprintf(
				"hunt: transfer %d is about to prewrite %q from a balance it never read. Its "+
					"writes would not be derived from its snapshot, and every conservation "+
					"failure that produced would be the workload's, not the cluster's "+
					"(keys=%v read=%v from=%q to=%q)", t.id, k, t.keys, t.read, t.from, t.to))
		}
	}
	t.values[t.from] = balance(t.read[t.from]-t.amount, t.id)
	t.values[t.to] = balance(t.read[t.to]+t.amount, t.id)
	t.phase = phasePrewrite
	t.ack = map[string]bool{}
	for _, k := range t.keys {
		c.send(t, store.TxnCommand{
			Op: store.OpPrewrite, Key: k, Value: t.values[k],
			Primary: t.primary, StartTS: t.startTS,
			Deadline: hlc.Timestamp{Wall: t.startTS.Wall.Add(kv.DefaultTTL)},
		}, at, s)
	}
}

// restartAbove re-takes the transaction's snapshot strictly above an uncertain
// commit.
//
// # The new timestamp comes from the ERROR, not from the clock
//
// CLAUDE.md's sharp-edge list: "Uncertainty restarts must bump past the observed
// value's timestamp." Restarting at now would be a different bug on a slow node
// -- now can be BELOW the value that caused the restart -- and restarting at
// readTS+maxOffset would be a guess that is both too large and, on a node whose
// clock is behind, still too small.
func (c *coordinator) restartAbove(t *transfer, ts hlc.Timestamp, at clock.Instant, s sim.Scheduler) {
	t.restarts++
	c.restarts++
	t.startTS = ts
	t.resolves = map[string]int{}
	t.blocked = map[string]string{}
	t.phase = phaseRead
	t.read = map[string]int{}
	for _, k := range t.keys {
		c.readAt(t, k, at, s)
	}
}

// send issues one step to whichever node will take it.
//
// Every node is offered the request and only the leader of the key's range
// answers, which is the same routing every other client operation uses -- and
// the same reason: the leader's identity is not known until the run produces it.
func (c *coordinator) send(t *transfer, cmd store.TxnCommand, at clock.Instant, s sim.Scheduler) {
	// # Every step a client issues carries the client's own snapshot
	//
	// Not because the state machine needs it -- a prewrite does not -- but
	// because it is how the answer finds its way home. A resolve-status step is
	// addressed to somebody ELSE'S primary key and carries somebody else's start
	// timestamp, so routing the answer by the transaction it names would deliver
	// it to the transaction being resolved rather than to the one doing the
	// resolving.
	cmd.ReadTS = t.startTS
	cmd.Origin = t.origin
	req := store.Request{
		Op: "txn", Key: cmd.Key, HistIdx: -1,
		Range: store.FirstRange, Epoch: 1,
		Txn: &cmd,
	}
	for i := range c.nodes {
		s.At(at, sim.KindClient, sim.NodeID(i), req)
	}
}

// applied advances a transfer, or an audit, when one of its steps lands.
//
// # Dispatch is on whether the step names a transaction
//
// A transfer's reads carry its start timestamp; an audit's do not, because an
// audit is not a transaction -- it is a client asking what the world looked like
// at one instant, which is the only shape that can check conservation without
// being a participant in the thing it is checking.
func (c *coordinator) applied(cmd store.TxnCommand, r store.TxnResult, at clock.Instant, s sim.Scheduler) {
	t := c.live[cmd.Origin]
	if t == nil {
		c.auditApplied(cmd, r, at, s)
		return
	}
	if t.phase == phaseDone || t.phase == phaseAbandoned {
		return
	}
	switch {
	case cmd.Op == store.OpTxnGet && t.phase == phaseRead:
		if !t.ceiling.IsSet() && r.Ceiling.IsSet() {
			t.ceiling = r.Ceiling
		}
		switch {
		case r.Locked:
			// # Reader-side lock discovery: the door Percolator actually uses
			//
			// The competing transaction is whichever one touches the key next,
			// and this is it. The resolver's read timestamp is the READER'S
			// OWN, so a resolver decides an expiry against the timestamp it is
			// reading at rather than against a clock -- which is what makes two
			// replicas applying the same resolve entry reach the same verdict
			// (kv.ResolveLock, DESIGN-A6 section 8 row one).
			if t.resolves[cmd.Key] >= maxResolves {
				c.abandon(t)
				return
			}
			t.resolves[cmd.Key]++
			c.resolves++
			c.readerResolves++
			// # The expiry timestamp is FRESH, and is not the read's snapshot
			//
			// The command carries the timestamp so that every replica compares
			// the same two values -- that is the determinism requirement and it
			// is unchanged. What it does not require is that the timestamp be
			// the READER'S, and using the reader's makes expiry unreachable
			// from an old snapshot: the lock's deadline is fixed, the snapshot
			// is fixed, so a resolver that waited once waits forever however
			// long the owner has been dead. Measured: 8977 waits and 9
			// completed audits in 20 seeds.
			//
			// A timestamp taken now, at propose time, and carried, is
			// deterministic for exactly the same reason and answers the
			// question a TTL is actually asking.
			ts, ok := c.now(t.ts)
			if !ok {
				c.abandon(t)
				return
			}
			// Step one goes to the PRIMARY's key, which routes it to whichever
			// range holds the primary now -- which is why a split cannot orphan
			// a lock: the lock names the key, not the range.
			t.blocked[r.LockPrimary+r.LockStartTS.String()] = cmd.Key
			c.send(t, store.TxnCommand{
				Op: store.OpResolveStatus, Key: r.LockPrimary,
				StartTS: r.LockStartTS, Deadline: r.LockDeadline,
				ReadTS: t.startTS, ExpireAt: ts,
			}, at, s)

		case r.Uncertain:
			if t.restarts >= maxRestarts {
				c.abandon(t)
				return
			}
			c.restartAbove(t, r.RestartAt, at, s)

		case r.Refused:
			// The read named a timestamp at or below the collection mark. A5's
			// refusal, reaching a transaction: the start timestamp was chosen
			// when the transaction began and the mark moved past it while it
			// was in flight, which DESIGN-A5 section 11 names as the case the
			// refusal exists for.
			c.refusedReads++
			c.abandon(t)

		default:
			// Absent is zero. An account nobody has written holds nothing, and
			// there is no genesis transaction to make that explicit -- see
			// the conservation note on newCoordinator.
			v := 0
			if r.Found {
				n, _, ok := parseBalance(r.Value)
				if !ok {
					// A balance that will not parse is a value this workload
					// never wrote, which means a read answered with somebody
					// else's key. Abandoning would hide it; the oracle over the
					// recorded history is what judges it.
					c.unparseable++
					c.abandon(t)
					return
				}
				v = n
			}
			// # Completion is a SET, not a counter
			//
			// It was a counter of answers, and two answers for one key -- which
			// happens whenever two resolutions of the same lock both come back
			// -- counted as two keys read. The transaction then prewrote a
			// balance for a key it had never read, using the zero value, and
			// the money it invented was caught by bank-conservation as a
			// twenty-three unit hole in seed 9.
			//
			// The audit had this right from the start with `seen`. The two
			// answer the same question and only one of them was asked as a
			// question about DISTINCT FACTS.
			t.read[cmd.Key] = v
			if len(t.read) < len(t.keys) {
				return
			}
			c.prewrite(t, at, s)
		}

	case cmd.Op == store.OpResolveStatus && t.phase == phaseRead:
		blocked, ok := t.blocked[cmd.Key+cmd.StartTS.String()]
		if !ok {
			return
		}
		if !r.Decided {
			// The owner is alive by its own TTL. Waiting is the correct verdict
			// and the reader asks again later; it is not a failure and not a
			// retry of a failed thing.
			c.resolveWaited++
			c.readAt(t, blocked, at+resolveBackoff, s)
			return
		}
		st := kv.TxnCommitted
		if r.Rollback {
			st = kv.TxnRolledBack
			c.resolvedBack++
		} else {
			c.resolvedForward++
		}
		c.send(t, store.TxnCommand{
			Op: store.OpApplyResolution, Key: blocked,
			StartTS: cmd.StartTS, CommitTS: r.CommitTS, Status: st,
		}, at, s)

	case cmd.Op == store.OpApplyResolution && t.phase == phaseRead:
		c.readAt(t, cmd.Key, at, s)

	case cmd.Op == store.OpPrewrite && t.phase == phasePrewrite:
		if r.Rejected {
			// # First-committer-wins, and the loser aborts EXPLICITLY
			//
			// The prewrite found a lock or a commit newer than this
			// transaction's snapshot, so this transaction can never commit.
			// Abandoning it here would be correct but lazy -- the locks it did
			// place would sit until a reader expired them -- and it would leave
			// the rollback path exercised only by dead coordinators. A live
			// coordinator that loses the race says so, which is both what a
			// real one does and the only way rollback gets exercised on a
			// schedule that is not a crash.
			c.abort(t, at, s)
			return
		}
		t.ack[cmd.Key] = true
		if len(t.ack) < len(t.keys) {
			return
		}
		// # Every prewrite is durable before the primary's record is proposed
		//
		// The step reported here applied, which means its entry committed, which
		// means a quorum holds it. Proposing the primary's record before that
		// would let a committed transaction lose a value it promised
		// (DESIGN-A6 D-A6-3, ordering 1).
		// # The commit timestamp is allocated HERE, not derived from the start
		//
		// It was startTS.Next(), which is a transaction committing into its own
		// past: every reader whose snapshot sits between the two sees a write
		// appear below a timestamp it has already read at. Percolator takes the
		// commit timestamp after prewrite for exactly this reason, and taking it
		// here is also what makes an uncertain commit possible at all -- a
		// commit derived from a start timestamp is never ahead of anybody.
		ts, ok := c.now(t.ts)
		if !ok {
			c.abandon(t)
			return
		}
		t.commitTS = ts
		t.phase = phasePrimaryRecord
		c.send(t, store.TxnCommand{
			Op: store.OpPutTxnRecord, Key: t.primary, Status: kv.TxnCommitted,
			StartTS: t.startTS, CommitTS: t.commitTS,
		}, at, s)

	case cmd.Op == store.OpPutTxnRecord && t.phase == phaseAbort:
		// The rollback record is down: the transaction is dead, whatever else
		// happens. Its keys are cleaned up next, and a resolver that gets there
		// first reaches the same verdict from the same record.
		t.ack = map[string]bool{}
		for _, k := range t.keys {
			c.send(t, store.TxnCommand{
				Op: store.OpRollbackKey, Key: k, StartTS: t.startTS,
			}, at, s)
		}

	case cmd.Op == store.OpRollbackKey && t.phase == phaseAbort:
		t.ack[cmd.Key] = true
		if len(t.ack) == len(t.keys) {
			t.phase = phaseDone
			delete(c.live, t.origin)
		}

	case cmd.Op == store.OpPutTxnRecord && r.Rejected && t.phase == phasePrimaryRecord:
		// Somebody wrote this transaction's record first, and by the
		// make-it-exist rule that record can only be a ROLLBACK: a resolver
		// found the lock expired and declared the owner dead. The coordinator
		// is the one that has to accept the verdict (DESIGN-A6 section 5).
		c.lostToResolver++
		t.phase = phaseAbort
		t.ack = map[string]bool{}
		for _, k := range t.keys {
			c.send(t, store.TxnCommand{
				Op: store.OpRollbackKey, Key: k, StartTS: t.startTS,
			}, at, s)
		}

	case cmd.Op == store.OpPutTxnRecord && t.phase == phasePrimaryRecord:
		// The transaction is now COMMITTED, whatever happens next. Everything
		// after this is bookkeeping that a resolver can finish.
		c.committed++
		c.ledger.RecordTxnCommit(t.id, t.commitTS, at)
		t.phase = phaseCommitKeys
		t.ack = map[string]bool{}
		for _, k := range t.keys {
			c.send(t, store.TxnCommand{
				Op: store.OpCommitKey, Key: k, StartTS: t.startTS, CommitTS: t.commitTS,
			}, at, s)
		}

	case cmd.Op == store.OpCommitKey && t.phase == phaseCommitKeys:
		t.ack[cmd.Key] = true
		if len(t.ack) == len(t.keys) {
			t.phase = phaseDone
			delete(c.live, t.origin)
		}
	}
}

// now asks node i for a timestamp, falling back across the cluster.
//
// # Why a client asks a NODE and not the harness
//
// A client has no clock. Every timestamp a transaction uses -- its snapshot,
// its commit, an audit's instant -- is minted by a node's hlc.Source, which is
// the only place in the system where time is admitted. A harness that handed
// out timestamps would be a second clock, outside the envelope the whole of A5
// exists to bound.
//
// The fallback is a scan and not a retry, because a crashed node has no answer
// and a client that insisted on one would stall for the length of the crash.
func (c *coordinator) now(i int) (hlc.Timestamp, bool) {
	if len(c.drivers) == 0 {
		return hlc.Timestamp{}, false
	}
	for k := 0; k < len(c.drivers); k++ {
		if ts, ok := c.drivers[(i+k)%len(c.drivers)].Now(); ok {
			return ts, true
		}
	}
	return hlc.Timestamp{}, false
}

// beginAudit reads every account at one timestamp.
func (c *coordinator) beginAudit(id int, at clock.Instant, run *plan.Run) {
	if len(c.drivers) == 0 {
		return
	}
	now, ok := c.now(id)
	if !ok {
		return
	}
	readTS := hlc.Timestamp{Wall: now.Wall.Add(-auditLookback)}
	c.nextOrigin++
	origin := c.nextOrigin
	// # An audit has an EMPTY uncertainty window, and that is not a shortcut
	//
	// The uncertainty interval exists because a transaction reading at "now"
	// cannot tell whether a commit slightly above its snapshot happened before
	// the read began in real time. An audit is not asking that question. It
	// names an instant explicitly -- the same shape as a read AS OF SYSTEM TIME,
	// and the same shape as the plain workload's snapshot reads, which have
	// never restarted either -- so the only correct answer is the state at that
	// instant, and there is nothing for a restart to improve.
	//
	// Measured with a window: a snapshot a second old sees every commit of the
	// following half second as uncertain, so nearly every audit restarted, and
	// each restart is a fresh round of N reads. The interval was being applied
	// to a question it is not the answer to.
	//
	// The uncertainty machinery is exercised by TRANSFERS, whose snapshots are
	// taken at now and are exactly the case it is for.
	a := &audit{id: id, origin: origin, readTS: readTS, ceiling: readTS,
		seen: map[string]int{}, need: c.accounts, resolves: map[string]int{},
		blocked: map[string]string{}}
	c.audits[origin] = a
	c.auditsStarted++
	for i := 0; i < c.accounts; i++ {
		c.auditRead(a, account(i), at, run.Loop)
	}
	c.pollAudit(a, at, run)
}

// pollAudit re-issues the reads an audit never got an answer to.
//
// # An unanswered request is the common case, not an error
//
// A client request reaches every node and only the leader answers, so a request
// issued while nobody is leader -- during an election, inside a partition, at a
// crashed node -- is simply never answered. Nothing retries it: the harness's
// other clients live in the linearizability history, where an unanswered
// operation is correctly recorded as may-or-may-not-have-happened.
//
// An audit cannot use that. It needs all N accounts AT ONE TIMESTAMP, so one
// unanswered read discards the whole thing, and an audit issued during any
// disturbance is lost. That is why 435 audits produced 16 answers: they were not
// failing, they were waiting.
//
// Re-asking is safe in a way it would not be for a write: a snapshot read at a
// fixed timestamp is idempotent by construction, so a duplicate answer is the
// same answer. It is exactly the retry a router does on NotLeader.
func (c *coordinator) pollAudit(a *audit, at clock.Instant, run *plan.Run) {
	for round := 1; round <= auditPolls; round++ {
		when := at + clock.Instant(round)*auditPoll
		run.Loop.Do(when, func() {
			if a.done {
				return
			}
			for i := 0; i < c.accounts; i++ {
				k := account(i)
				if _, ok := a.seen[k]; ok {
					continue
				}
				c.auditsRetried++
				c.auditRead(a, k, when, run.Loop)
			}
		})
	}
}

func (c *coordinator) auditRead(a *audit, key string, at clock.Instant, s sim.Scheduler) {
	c.reads++
	c.broadcast(store.TxnCommand{Op: store.OpTxnGet, Key: key,
		ReadTS: a.readTS, MaxTS: a.ceiling, Origin: a.origin}, key, at, s)
}

// broadcast sends one command to every node; only the key's leader answers.
func (c *coordinator) broadcast(cmd store.TxnCommand, key string, at clock.Instant, s sim.Scheduler) {
	req := store.Request{Op: "txn", Key: key, HistIdx: -1,
		Range: store.FirstRange, Epoch: 1, Txn: &cmd}
	for i := range c.nodes {
		s.At(at, sim.KindClient, sim.NodeID(i), req)
	}
}

// auditApplied advances an audit, and is where an audit meets the same three
// answers a transaction's read does.
func (c *coordinator) auditApplied(cmd store.TxnCommand, r store.TxnResult, at clock.Instant, s sim.Scheduler) {
	a := c.audits[cmd.Origin]
	if a == nil || a.done {
		return
	}
	switch cmd.Op {
	case store.OpResolveStatus:
		if !r.Decided {
			c.resolveWaited++
			c.auditRead(a, a.blocked[cmd.Key+cmd.StartTS.String()], at+resolveBackoff, s)
			return
		}
		st := kv.TxnCommitted
		if r.Rollback {
			st = kv.TxnRolledBack
			c.resolvedBack++
		} else {
			c.resolvedForward++
		}
		key := a.blocked[cmd.Key+cmd.StartTS.String()]
		cmd3 := store.TxnCommand{Op: store.OpApplyResolution, Key: key,
			StartTS: cmd.StartTS, CommitTS: r.CommitTS, Status: st,
			ReadTS: a.readTS, Origin: a.origin}
		c.broadcast(cmd3, key, at, s)
		return
	case store.OpApplyResolution:
		c.auditRead(a, cmd.Key, at, s)
		return
	case store.OpTxnGet:
		// # Learned from ANY answer, not only a successful one
		//
		// It was learned only from a read that returned a value, so an audit
		// whose first answer was UNCERTAIN restarted without one -- and the
		// node, seeing no ceiling, computed a fresh window from the new read
		// timestamp. The fix for a moving window was in place and the one path
		// that needed it most walked around it. Measured: 174 restarts and 15
		// completions with the ceiling learned late, against the numbers below
		// with it learned here.
		_ = r.Ceiling // an audit's ceiling is its own timestamp; see beginAudit.
	default:
		return
	}
	switch {
	case r.Locked:
		c.auditsLocked++
		if a.resolves[cmd.Key] >= maxAuditResolves {
			a.done = true
			return
		}
		a.resolves[cmd.Key]++
		c.resolves++
		c.readerResolves++
		ts, ok := c.now(a.id)
		if !ok {
			a.done = true
			return
		}
		// The audit remembers which of ITS keys this primary is being resolved
		// for, because the answer comes back addressed to the primary and the
		// effect has to be applied to the locked key.
		a.blocked[r.LockPrimary+r.LockStartTS.String()] = cmd.Key
		c.broadcast(store.TxnCommand{Op: store.OpResolveStatus, Key: r.LockPrimary,
			StartTS: r.LockStartTS, Deadline: r.LockDeadline,
			ReadTS: a.readTS, ExpireAt: ts, Origin: a.origin}, r.LockPrimary, at, s)

	case r.Uncertain:
		// An audit restarts like a transaction does, and for the same reason:
		// its snapshot is not safely below a commit it cannot order against.
		// A new timestamp means a new audit, because the old one's partial
		// results belong to a snapshot it has abandoned.
		c.auditsUncertain++
		a.done = true
		if a.restarts >= maxAuditRestarts {
			return
		}
		c.nextOrigin++
		b := &audit{id: a.id, origin: c.nextOrigin, readTS: r.RestartAt,
			seen: map[string]int{}, need: c.accounts, resolves: map[string]int{},
			restarts: a.restarts + 1, ceiling: a.ceiling, blocked: map[string]string{}}
		c.audits[b.origin] = b
		c.auditsStarted++
		for i := 0; i < c.accounts; i++ {
			c.auditRead(b, account(i), at, s)
		}

	case r.Refused:
		c.refusedReads++
		a.done = true

	default:
		v := 0
		if r.Found {
			n, _, ok := parseBalance(r.Value)
			if !ok {
				c.unparseable++
				a.done = true
				return
			}
			v = n
		}
		if _, dup := a.seen[cmd.Key]; dup {
			return
		}
		a.seen[cmd.Key] = v
		if len(a.seen) < a.need {
			return
		}
		a.done = true
		c.auditsComplete++
		total := 0
		for _, v := range a.seen {
			total += v
		}
		c.ledger.RecordAudit(a.readTS, total, len(a.seen), at)

		// # The second pass, which is the stability probe
		//
		// Every account is asked again, at the SAME timestamp, a beat later. A
		// snapshot is a question about a fixed instant, so both passes must
		// answer the same thing -- and the failure that makes them differ is a
		// transaction committing at or below a timestamp somebody has already
		// read at, which is the one snapshot-isolation failure no amount of care
		// in the write path prevents.
		//
		// Without it the property is unfalsifiable in practice: the same (key,
		// timestamp) pair is almost never asked twice by accident, so
		// snapshot-isolation's stability check would run over an empty set and
		// report green. That is the vacuous-green class, and the answer to it is
		// to make the workload ask the question rather than to hope it does.
		for i := 0; i < c.accounts; i++ {
			c.secondPass++
			c.broadcast(store.TxnCommand{Op: store.OpTxnGet, Key: account(i),
				ReadTS: a.readTS, MaxTS: a.ceiling, Origin: 0}, account(i),
				at+secondPassDelay, s)
		}
	}
}

// abort ends a transaction the cluster refused, by declaring it rolled back.
//
// The record goes on the PRIMARY, which is the only place a decision about this
// transaction may live, and it goes through the log like every other decision.
// A resolver racing it reaches the same verdict because PutTxnInto refuses to
// overwrite and both of them are asking the same range's log the same question.
func (c *coordinator) abort(t *transfer, at clock.Instant, s sim.Scheduler) {
	if t.phase == phaseDone || t.phase == phaseAbandoned || t.phase == phaseAbort {
		return
	}
	c.aborted++
	t.phase = phaseAbort
	c.send(t, store.TxnCommand{
		Op: store.OpPutTxnRecord, Key: t.primary, Status: kv.TxnRolledBack,
		StartTS: t.startTS,
	}, at, s)
}

// abandon gives up on a transfer whose step never landed.
//
// The locks it leaves behind are the raw material of the recovery path: another
// transaction will find them, read the primary, and either roll forward or roll
// back. A sweep whose coordinators all finish tests none of that.
func (c *coordinator) abandon(t *transfer) {
	if t.phase == phaseDone || t.phase == phaseAbandoned {
		return
	}
	t.phase = phaseAbandoned
	c.abandoned++
	delete(c.live, t.origin)
}

// newCoordinator builds the bank's client.
func newCoordinator(drivers []*store.Node, nodes int, hist *sim.History, l *raftcheck.Ledger,
	opt RaftOptions, p *plan.Plan) *coordinator {
	return &coordinator{
		drivers: drivers, nodes: nodes, hist: hist,
		ledger:     ledgerAdapter{l},
		live:       map[uint64]*transfer{},
		audits:     map[uint64]*audit{},
		identities: map[string]bool{},
		// # Four seconds, not two
		//
		// A transfer is now seven replicated round trips -- two reads, two
		// prewrites, the primary's record, two commits -- plus a resolution
		// backoff for every lock it meets. Two seconds abandoned three
		// transfers in five, which is not a workload exercising recovery so
		// much as a workload that is mostly recovery, and the locks it strands
		// are what stop audits from ever completing.
		deadline: clock.Instant(4 * time.Second),
		accounts: opt.Accounts,
		count:    opt.Transfers2PC,
	}
}

// ledgerAdapter narrows the ledger to the two things a coordinator may do to it.
//
// Not a convenience: the coordinator is the harness's own client, and letting it
// reach the whole ledger would put the thing that drives the run and the thing
// that judges it one method call apart.
type ledgerAdapter struct{ l *raftcheck.Ledger }

func (a ledgerAdapter) RecordTxnBegin(id int, startTS hlc.Timestamp, primary string, keys []string, at clock.Instant) {
	a.l.RecordTxnBegin(provenance.Witness(raftcheck.TxnRecord{
		ID: id, StartTS: startTS, Primary: primary,
		Keys: append([]string(nil), keys...), Began: at,
	}))
}

func (a ledgerAdapter) RecordTxnCommit(id int, commitTS hlc.Timestamp, at clock.Instant) {
	a.l.RecordTxnCommit(id, commitTS, at)
}

func (a ledgerAdapter) RecordAudit(readTS hlc.Timestamp, total, accounts int, at clock.Instant) {
	a.l.RecordAudit(provenance.Witness(raftcheck.AuditRecord{
		ReadTS: readTS, Total: total, Accounts: accounts, When: at,
	}))
}

// schedule places the transfers, the abandonment sweeps and the resolution
// attempts on the event queue.
//
// All three come from the plan's key stream, so a run replays: the coordinator
// is a client, and a client whose choices were not plan-derived would make the
// simulator nondeterministic in exactly the way this project forbids.
func (c *coordinator) schedule(run *plan.Run, p *plan.Plan) error {
	key, err := rng.ParseKey(p.Keys.Raft)
	if err != nil {
		return fmt.Errorf("hunt: raft key: %w", err)
	}
	span := p.Config.DurationNS

	for i := range c.transfers(p) {
		at := span/10 + int64(key.Uint64N(20, uint64(i), 0, 0, uint64(span*8/10)))
		from := int(key.Uint64N(21, uint64(i), 0, 0, uint64(c.accounts)))
		to := int(key.Uint64N(22, uint64(i), 0, 0, uint64(c.accounts)))
		if to == from {
			to = (to + 1) % c.accounts
		}
		amount := int(key.Uint64N(23, uint64(i), 0, 0, 20)) + 1
		id := i

		run.Loop.Do(clock.Instant(at), func() {
			c.beginTransfer(id, from, to, amount, run)
		})

		// The abandonment sweep. A transfer whose step never landed is given up
		// on, and its locks become the recovery path's raw material.
		run.Loop.Do(clock.Instant(at)+c.deadline, func() {
			if t := c.byID(id); t != nil {
				c.abandon(t)
			}
		})
	}

	// # Audits, which are also the resolution door
	//
	// Resolution used to be a periodic sweep issuing bare resolve commands at
	// keys chosen by the plan. That reached the resolve code, but by a door no
	// client has: a real cluster resolves a lock because a READER ran into it,
	// and the reader's own timestamp is what the expiry is judged against.
	// Driving it from a sweep meant the two halves -- discovery and decision --
	// were never connected, and a reader that discovered a lock and did nothing
	// with it would have passed.
	//
	// So the sweep is gone and audits take its place: an audit reads every
	// account, runs into whatever locks are outstanding, resolves them and reads
	// again. It exercises the same code by the door the protocol actually uses,
	// and it produces the conservation evidence at the same time.
	for i := 0; i < 16; i++ {
		// Placed in the first three quarters of the run: an audit is several
		// round trips per account and one scheduled at the deadline has no time
		// to finish, which would make it a discarded audit rather than a
		// checked one.
		at := span/5 + int64(key.Uint64N(24, uint64(i), 0, 0, uint64(span*11/20)))
		id := i
		run.Loop.Do(clock.Instant(at), func() {
			c.beginAudit(id, clock.Instant(at), run)
		})
	}
	return nil
}

func (c *coordinator) transfers(p *plan.Plan) []struct{} { return make([]struct{}, c.count) }

func (c *coordinator) byID(id int) *transfer {
	for _, t := range c.live {
		if t.id == id {
			return t
		}
	}
	return nil
}

// beginTransfer reads two accounts at a snapshot and writes both.
//
// The read is the SNAPSHOT read every transaction starts with, and it goes
// through the same path a client read does -- which is what makes the
// transaction's view a thing the oracle can check rather than a number the
// coordinator carried around.
func (c *coordinator) beginTransfer(id, from, to, amount int, run *plan.Run) {
	if len(c.drivers) == 0 {
		return
	}
	// The start timestamp comes from a node's clock, which is where a real
	// client would get it: the coordinator has no clock of its own, and giving
	// it one would put a time source outside the cluster's envelope.
	startTS, ok := c.now(id)
	if !ok {
		return
	}
	c.nextOrigin++
	t := &transfer{
		id: id, origin: c.nextOrigin, startTS: startTS,
		ts:       id,
		primary:  account(from),
		from:     account(from),
		to:       account(to),
		amount:   amount,
		keys:     []string{account(from), account(to)},
		values:   map[string]string{},
		resolves: map[string]int{},
		blocked:  map[string]string{},
	}
	c.begin(t, run.Loop.Now(), run.Loop)
}

// What the bank did. Every one is asserted or deleted in the exit run.
func (c *coordinator) Started() int            { return c.started }
func (c *coordinator) Committed() int          { return c.committed }
func (c *coordinator) Abandoned() int          { return c.abandoned }
func (c *coordinator) Resolves() int           { return c.resolves }
func (c *coordinator) Reads() int              { return c.reads }
func (c *coordinator) ReaderResolves() int     { return c.readerResolves }
func (c *coordinator) Restarts() int           { return c.restarts }
func (c *coordinator) RefusedReads() int       { return c.refusedReads }
func (c *coordinator) Unparseable() int        { return c.unparseable }
func (c *coordinator) AuditsStarted() int      { return c.auditsStarted }
func (c *coordinator) AuditsComplete() int     { return c.auditsComplete }
func (c *coordinator) AuditsLocked() int       { return c.auditsLocked }
func (c *coordinator) AuditsUncertain() int    { return c.auditsUncertain }
func (c *coordinator) AuditsRetried() int      { return c.auditsRetried }
func (c *coordinator) SecondPass() int         { return c.secondPass }
func (c *coordinator) IdentityCollisions() int { return c.identityCollisions }

// ForeignLocksKept is BUG-019's evidence, summed over the cluster: how often a
// commit or a rollback found somebody else's lock and left it alone.
func (c *coordinator) ForeignLocksKept() int {
	n := 0
	for _, d := range c.drivers {
		n += d.ForeignLocksKept()
	}
	return n
}
func (c *coordinator) ResolveWaited() int   { return c.resolveWaited }
func (c *coordinator) ResolvedForward() int { return c.resolvedForward }
func (c *coordinator) ResolvedBack() int    { return c.resolvedBack }
func (c *coordinator) Aborted() int         { return c.aborted }
func (c *coordinator) LostToResolver() int  { return c.lostToResolver }
