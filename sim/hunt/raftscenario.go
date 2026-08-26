package hunt

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/anshkanyadi/rift/clock"
	"github.com/anshkanyadi/rift/hlc"
	"github.com/anshkanyadi/rift/internal/provenance"
	"github.com/anshkanyadi/rift/internal/rng"
	"github.com/anshkanyadi/rift/kv"
	"github.com/anshkanyadi/rift/raft"
	"github.com/anshkanyadi/rift/raftcheck"
	"github.com/anshkanyadi/rift/sim"
	"github.com/anshkanyadi/rift/sim/checker"
	"github.com/anshkanyadi/rift/sim/plan"
	"github.com/anshkanyadi/rift/store"
)

// The A1 Raft scenario driver, alongside the A0 toy's.
//
// Sweeping and driving are orchestration and live here (Amendment A5); the
// materialization that decides *what run to perform* lives in the deterministic
// half, the same split TestScopeTable pins.

// RaftGenConfig is the plan shape A1 seeds are materialized against.
//
// # The schedule mix weights the single-cut geometry
//
// DESIGN-A0.7 blessed directed partitions with a forward binding: *A1's schedule
// mix weights the single-cut send-without-receive geometry.* Honoured here. A
// symmetric cut is two directed cuts and isolates a node cleanly; a SINGLE cut
// leaves a node that can send but not receive, so it campaigns, bumps terms, and
// never learns it lost. That is where the interesting consensus bugs live, and
// symmetric partitions never generate it.
// RaftOptions are the A2 build parameters: what the cluster is, as opposed to
// what happens to it.
//
// They are deliberately NOT plan entries. The pre-vote ablation runs the same
// schedules with the round on and off, so pre-vote must not perturb the schedule
// it is being measured against -- the same reason the toy carries its flaw in the
// scenario rather than in the plan.
type RaftOptions struct {
	PreVote           bool
	SnapshotThreshold raft.Index

	// FollowerReadPerMille is the share of plain reads addressed to ONE replica
	// rather than broadcast, so a follower can actually serve one. Ruling 2's
	// obligation: the exercise must be non-vacuous.
	FollowerReadPerMille int

	// ReadIndex serves PLAIN reads by read index instead of by a log entry (A7,
	// D-A7-5 ruled A). Snapshot reads keep their entry regardless.
	//
	// It is an OPTION rather than a default because D-A7-4 keeps both paths for
	// the phase: the differential oracle compares them, and a sweep that ran
	// only one has not tested the thing the oracle is for.
	ReadIndex bool

	// GCRetention is how far behind its clock a leader collects. Zero disables.
	GCRetention time.Duration

	// OverlapDrivers lets the churn driver and the rebalance driver run at the
	// same time, which DESIGN-A4 section 10 records as an unexercised
	// interleaving and the A5 sign-off put on A6's checklist.
	OverlapDrivers bool

	// EnvelopeExceeded holds a pair at a skew ABOVE maxOffset, deliberately
	// outside the assumption every A6 bound rests on. Never set in a safety
	// sweep: CLAUDE.md says skew is bounded by maxOffset in safety runs and
	// deliberately exceeds it in envelope experiments, and this is the second.
	EnvelopeExceeded bool

	// GCUnthrottled runs the collector without the mark-movement condition,
	// which is the shape A5's throttle replaced. A6 owes one reduced-seed sweep
	// in this shape (Ansh's A5 ruling); it is never on in a full-size run.
	GCUnthrottled bool

	// SnapshotReadPerMille is how many client reads name a REMEMBERED timestamp
	// rather than "now", in parts per thousand.
	//
	// Without these, every read names a timestamp above every version and the
	// MVCC read path is exercised in the one shape that cannot tell it from a
	// single-version store -- and the collection mark refuses nothing, ever.
	SnapshotReadPerMille uint64

	// Transfers is how many bank transactions the plan schedules. Zero disables
	// the bank workload, which is what every phase before A6 runs.
	Transfers2PC int

	// Accounts is how many bank accounts exist. Each is one key.
	Accounts int

	// Rebalances is how many manual replica movements the plan orders. Each is
	// ONE step of a move, so a move needs several: the mechanism is stateless by
	// design and finishes by being asked again (store.Replica.RequestMove).
	Rebalances int

	// Transfers is how many leadership transfers the plan schedules.
	Transfers int

	// Learners is how many of the cluster's nodes start as learners. They are
	// the highest-numbered ones, so the voters are a stable prefix.
	Learners int

	// ConfChanges is how many membership steps the plan schedules. Each moves one
	// node around the cycle absent -> learner -> voter -> absent.
	ConfChanges int

	// PromotionLag bounds how far behind a learner may be and still be promoted.
	PromotionLag raft.Index

	// SplitThreshold is how many keys a range may hold before its leader
	// proposes a split. Zero disables splitting.
	SplitThreshold int
}

// A2Options is what the sweep runs: snapshots on with a threshold small enough
// that a 40-operation workload compacts several times, pre-vote on, and a
// couple of leadership transfers.
func A2Options() RaftOptions {
	return RaftOptions{PreVote: true, SnapshotThreshold: 6, Transfers: 2}
}

// CurrentOptions is the configuration the sweep runs, and it is the SINGLE
// source of truth for it.
//
// # Why this is one function rather than a default in each place
//
// A2's options lingered as the default inside the oracle inductions after A3
// landed, so five mutants were induced against a configuration that schedules
// zero membership changes: tests that could not have killed them, reporting the
// oracles as at fault. A mutant is only as honest as the configuration its
// covering test runs under.
//
// The sweep, the oracle inductions and the power probe now all read this, so
// they cannot disagree. Advancing a phase is one edit here, and every instrument
// moves with it.
// CurrentOptions is whatever shape the sweep runs TODAY, and at A7 that means
// read index is on and a third of plain reads are addressed to one replica.
//
// Moving it is the phase boundary, and it moves every count underneath every
// floor -- which is why A7's exit criterion 4 asks for the power numbers again
// rather than citing A6's. DESIGN-A5 §11b is the standing warning about this
// name: it once meant "what the sweep runs" while pinned to a shape the sweep
// had left, and a label that stops describing its subject is the quiet drift
// this lane exists to catch in the system under test.
func CurrentOptions() RaftOptions { return A7Options() }

// CurrentShapeName is what CurrentOptions currently IS, derived from the options
// rather than written beside them.
//
// # Why this is a function and not a string somebody maintains
//
// Third instance of a label that stopped describing its subject: `power-config:
// a3` meant "what the sweep runs" while pinned to a shape the sweep had left;
// one `power: n/a` label carried two opposite claims; and `exit-run.sh` printed
// "A6 exit run" over a sweep of A7's shape. Each was cosmetic on the day it was
// written, which is the argument that was made about `a3`.
//
// Three instances is a pattern, so the name is COMPUTED. A banner that reads the
// options struct it swept cannot disagree with it.
func CurrentShapeName() string { return ShapeNameOf(CurrentOptions()) }

// ShapeNameOf names any options struct, so the naming rule is one place and a
// test can check it moves.
func ShapeNameOf(o RaftOptions) string {
	switch {
	case o.ReadIndex:
		return "A7 (read index, follower reads)"
	case o.Transfers2PC > 0:
		return "A6 (percolator transactions)"
	case o.GCRetention > 0:
		return "A5 (MVCC, HLC)"
	case o.SplitThreshold > 0:
		return "A4 (multi-raft)"
	case o.ConfChanges > 0:
		return "A3 (membership)"
	case o.SnapshotThreshold > 0:
		return "A2 (snapshots)"
	}
	return "A1 (single-group raft)"
}

// A3Options adds membership churn: a four-node cluster with one learner, and
// enough scheduled changes that the cycle runs several times per seed.
func A3Options() RaftOptions {
	o := A2Options()
	o.Learners = 1
	o.ConfChanges = 4
	o.PromotionLag = 8
	return o
}

// A4Options turns on splitting. The threshold is small relative to the key space
// so a seed's traffic crosses it several times and the cluster ends the run with
// several ranges rather than a hopeful one.
func A4Options() RaftOptions {
	o := A3Options()
	o.SplitThreshold = 4
	// Twelve orders, not twelve moves: they come in groups of four against one
	// fixed move, because a stateless move advances one step per order. Three
	// moves per seed, each with enough orders to finish.
	o.Rebalances = 12
	return o
}

// A5Options adds MVCC: collection with a retention window, and a share of reads
// at remembered timestamps.
// A6Options adds the bank: Percolator transactions over the accounts, on top of
// everything A5 runs.
// A7Options turns on read index for plain reads. It is A6's shape plus the one
// change the phase is about, so a comparison between them is a comparison of
// the read path and not of two workloads.
func A7Options() RaftOptions {
	o := A6Options()
	o.ReadIndex = true
	// A third of plain reads go to a designated replica. High enough that
	// followers serve reads on every seed rather than occasionally -- for a
	// mechanism whose measured exercise rate was ZERO, more is the right
	// direction to be wrong in, which is the same argument the cold-cache
	// routing makes one screen up.
	o.FollowerReadPerMille = 333
	return o
}

func A6Options() RaftOptions {
	o := A5Options()
	o.Accounts = 8
	// Enough that transfers overlap and contend -- a workload whose transactions
	// never meet exercises neither the write-conflict path nor resolution.
	o.Transfers2PC = 40
	return o
}

func A5Options() RaftOptions {
	o := A4Options()
	// Two seconds against a fourteen-second run: long enough that collection
	// happens several times per seed, short enough that a remembered timestamp
	// from early in the run falls behind the mark and the refusal path runs.
	o.GCRetention = 2 * time.Second
	o.SnapshotReadPerMille = 400
	return o
}

func RaftGenConfig() plan.GenConfig {
	cfg := plan.DefaultGenConfig()
	cfg.Nodes = 4 // three voters and one learner at A3
	cfg.Duration = 14 * time.Second
	cfg.ClientOps = 70
	cfg.Crashes = 8
	cfg.Partitions = 6 // weighted up, and genFaults alternates so most are single cuts
	// # Clock holds, which were ZERO through A5 and are the sixteenth instance
	//
	// The comment here read "A1 Raft has no clock-sensitive logic; holds land
	// with leases", and it was true when it was written. A6 made it false and
	// nothing noticed: uncertainty intervals, lock TTL expiry and the HLC
	// envelope are all clock-sensitive, and the sweep that exercises them was
	// injecting no skew at all -- only free drift of at most +-200 ppm, which
	// over a fourteen-second run is under three milliseconds against a
	// five-hundred-millisecond bound.
	//
	// So A6's headline mechanism was being exercised by HLC ORDERING alone: a
	// commit timestamp allocated after a read timestamp is above it on one
	// node's monotone clock, with or without any disagreement between clocks.
	// The 256 uncertainty restarts across 200 seeds were real restarts against
	// no real skew.
	//
	// Two holds, at 90% of maxOffset, which is inside the envelope and is what
	// CLAUDE.md means by "bounded by maxOffset in safety runs". floors.go's rule
	// applies: a schedule mix is a claim about reachability, so this number is a
	// claim about which defects A6 can find, and changing it silently would
	// change that claim silently.
	cfg.Holds = 2
	return cfg
}

// MaterializeRaft turns a seed into a prepared plan with A2's options.
func MaterializeRaft(seed uint64) (*plan.Plan, error) {
	return MaterializeRaftWith(seed, CurrentOptions())
}

// MaterializeRaftWith turns a seed into a prepared plan, adding the leadership
// transfers the options ask for.
//
// The transfer entries are derived from the seed's own key stream, so a plan is
// still a total repro and replay takes no live draw.
func MaterializeRaftWith(seed uint64, opt RaftOptions) (*plan.Plan, error) {
	gc := RaftGenConfig()
	gc.EnvelopeExceeded = opt.EnvelopeExceeded
	p, err := plan.Materialize(seed, gc)
	if err != nil {
		return nil, fmt.Errorf("hunt: materialize: %w", err)
	}
	if opt.ConfChanges > 0 {
		key, err := rng.ParseKey(p.Keys.Raft)
		if err != nil {
			return nil, fmt.Errorf("hunt: raft key: %w", err)
		}
		span := p.Config.DurationNS
		for i := range opt.ConfChanges {
			// # The two membership drivers do not overlap, and that is a
			// # limitation, recorded rather than hidden
			//
			// Churn runs in the first half, rebalance in the second. The reason
			// is attribution: a move's safety claim is about the ORDER of an add
			// and a remove, and an add and a remove look exactly like two
			// unrelated membership changes in the log. With both drivers live at
			// once, the rebalance oracle blamed the churn's removals on moves --
			// 252 seeds in 300, every one of them the oracle being right about
			// the log and wrong about who did it.
			//
			// The honest alternatives were to tag configuration entries with a
			// move identifier, which changes a frozen wire format for the
			// convenience of a checker, or to separate them in time. DESIGN-A4
			// §7 records what this costs: no seed exercises a move racing an
			// unrelated membership change.
			at := span/5 + int64(key.Uint64N(3, uint64(i), 0, 0, uint64(span*3/10)))
			if opt.OverlapDrivers {
				// # The interleaving DESIGN-A4 section 10 recorded as unexercised
				//
				// Churn spread over the WHOLE run instead of its first third, so
				// it overlaps the rebalance window. It became attemptable when
				// rebalance-safety stopped guessing whose removal it was looking
				// at: an ambiguous one is now counted as unattributable rather
				// than reported as a violation (Amendment A4's third outcome).
				at = span/5 + int64(key.Uint64N(3, uint64(i), 0, 0, uint64(span*7/10)))
			}
			target := int(key.Uint64N(4, uint64(i), 0, 0, uint64(p.Config.Nodes)))
			p.Faults.Entries = append(p.Faults.Entries, plan.Entry{
				AtNS: at, Action: "conf", Node: target,
			})
		}
	}
	if opt.Rebalances > 0 {
		key, err := rng.ParseKey(p.Keys.Raft)
		if err != nil {
			return nil, fmt.Errorf("hunt: raft key: %w", err)
		}
		span := p.Config.DurationNS
		for i := range opt.Rebalances {
			// The second half of the run: a move ordered before any range has
			// split is a move of the one range everybody already hosts.
			at := span*3/5 + int64(key.Uint64N(5, uint64(i), 0, 0, uint64(span*3/10)))
			from := int(key.Uint64N(6, uint64(i), 0, 0, uint64(p.Config.Nodes)))
			to := int(key.Uint64N(7, uint64(i), 0, 0, uint64(p.Config.Nodes)))
			p.Faults.Entries = append(p.Faults.Entries, plan.Entry{
				AtNS: at, Action: "rebalance", From: from, To: to,
			})
		}
	}
	if opt.Transfers > 0 {
		key, err := rng.ParseKey(p.Keys.Raft)
		if err != nil {
			return nil, fmt.Errorf("hunt: raft key: %w", err)
		}
		span := p.Config.DurationNS
		for i := range opt.Transfers {
			// Spread through the middle of the run: a transfer before anybody
			// has been elected is a transfer of nothing.
			at := span/4 + int64(key.Uint64N(1, uint64(i), 0, 0, uint64(span/2)))
			target := int(key.Uint64N(2, uint64(i), 0, 0, uint64(p.Config.Nodes)))
			p.Faults.Entries = append(p.Faults.Entries, plan.Entry{
				AtNS: at, Action: "promote", Node: target,
			})
		}
	}
	return p, nil
}

// replay is the harness's independent model of a range's state machine.
//
// It re-implements what a command DOES. What it borrows from store is the
// serialisation only, so a defect in APPLYING commands cannot cancel out on both
// sides of the comparison -- which is the whole reason the oracles take
// functions instead of importing one.
//
// onSplit is called for every split entry with the model's verdict on whether it
// takes effect, so the two oracles that need splits and the one that needs the
// final state all read the same replay rather than three drifting copies of it.
// modelLock is the harness's own notion of a prewritten intent.
type modelLock struct {
	primary  string
	startTS  hlc.Timestamp
	deadline hlc.Timestamp
}

// newestCommitOf is the latest commit timestamp among a key's records.
func newestCommitOf(cs []raftcheck.CommitFact) (hlc.Timestamp, bool) {
	var best hlc.Timestamp
	ok := false
	for _, c := range cs {
		if !ok || best.Less(c.CommitTS) {
			best, ok = c.CommitTS, true
		}
	}
	return best, ok
}

// namespaceOf is a range's engine-key prefix, which the model needs both to
// decode a birth payload and to render its own records.
func namespaceOf(id store.RangeID) []byte { return store.RangePrefix(id) }

func replayFull(base []byte, entries []raft.Entry,
	onSplit func(raft.Index, bool, store.SplitSpec),
	onRead func(raftcheck.ReadExpectation),
	commits map[string][]raftcheck.CommitFact,
	decided map[string]raftcheck.CommitFact,
) (store.RangeDescriptor, hlc.Timestamp, []kv.Version, map[string]*modelLock, bool) {
	if commits == nil {
		commits = map[string][]raftcheck.CommitFact{}
	}
	if decided == nil {
		decided = map[string]raftcheck.CommitFact{}
	}
	locks := map[string]*modelLock{}
	// The model's own read marks. BUG-022's third prewrite guard is restated
	// here rather than imported, for the reason every other step is: a defect in
	// the store's guard must not cancel out on both sides of the comparison.
	readMarks := map[string]hlc.Timestamp{}
	desc, mark, recs, ok := store.DecodeMachine(base)
	if !ok {
		return desc, mark, nil, nil, false
	}
	// A birth payload carries the CHILD's namespace, which is the namespace this
	// replay is about. Decoding with it rather than guessing is the difference
	// between reading a split-born range's inheritance and reading nothing.
	vs := seedFrom(namespaceOf(desc.ID), recs, locks, commits, decided, readMarks)
	for _, e := range entries {
		if len(e.Data) == 0 {
			continue
		}
		if c, ok := store.DecodeTxnCommand(e.Data); ok {
			// # The transaction steps, restated
			//
			// The model applies what a prewrite, a commit and a rollback DO --
			// it does not call the store's. A defect in applying them would then
			// cancel out on both sides of the comparison, which is the whole
			// reason every model function here is injected rather than imported.
			//
			// Extent and mark checks apply exactly as they do to a put: a step
			// for a key this range no longer owns is refused at the log
			// position, and so is one at or below the collection mark.
			if !desc.Contains([]byte(c.Key)) {
				continue
			}
			switch c.Op {
			case store.OpTxnGet:
				// A read marks the key at its own timestamp. Restated: the
				// store stages a record, the model keeps a number, and the two
				// have to agree about which prewrites are refused or the
				// commit facts diverge on every seed that reads a key it is
				// about to write (BUG-022).
				if rm, ok := readMarks[c.Key]; !ok || rm.Less(c.ReadTS) {
					readMarks[c.Key] = c.ReadTS
				}
			case store.OpPrewrite:
				if c.StartTS.LessEq(mark) || locks[c.Key] != nil {
					continue
				}
				if newest, ok := newestCommitOf(commits[c.Key]); ok && c.StartTS.Less(newest) {
					continue // write conflict
				}
				if rm, ok := readMarks[c.Key]; ok && c.StartTS.Less(rm) {
					continue // somebody was answered above this snapshot (BUG-022)
				}
				locks[c.Key] = &modelLock{primary: c.Primary, startTS: c.StartTS, deadline: c.Deadline}
				vs = insertVersion(vs, kv.Version{Key: []byte(c.Key), At: c.StartTS, Value: []byte(c.Value)})
			case store.OpCommitKey:
				commits[c.Key] = append(commits[c.Key], raftcheck.CommitFact{
					Key: c.Key, StartTS: c.StartTS, CommitTS: c.CommitTS,
				})
				delete(locks, c.Key)
			case store.OpRollbackKey:
				commits[c.Key] = append(commits[c.Key], raftcheck.CommitFact{
					Key: c.Key, StartTS: c.StartTS, CommitTS: c.StartTS, Rollback: true,
				})
				delete(locks, c.Key)
				vs = dropVersion(vs, c.Key, c.StartTS)
			case store.OpPutTxnRecord:
				dk := raftcheck.TxnDecisionKey(c.Key, c.StartTS)
				if _, seen := decided[dk]; seen {
					continue // a resolver may only make a record EXIST
				}
				decided[dk] = raftcheck.CommitFact{
					Key: c.Key, StartTS: c.StartTS, CommitTS: c.CommitTS,
					Rollback: c.Status != kv.TxnCommitted,
				}
			case store.OpResolveStatus:
				// The primary's range, deciding. A record that already exists
				// wins -- a resolver may only ever make one EXIST -- and an
				// undecided owner past its deadline is declared dead here.
				dk := raftcheck.TxnDecisionKey(c.Key, c.StartTS)
				if _, seen := decided[dk]; seen {
					continue
				}
				if c.ExpireAt.LessEq(c.Deadline) {
					continue // alive by its own TTL; waiting is the verdict
				}
				decided[dk] = raftcheck.CommitFact{
					Key: c.Key, StartTS: c.StartTS, CommitTS: c.StartTS, Rollback: true,
				}

			case store.OpApplyResolution:
				// The locked key's range, applying a verdict reached elsewhere.
				// It carries the verdict rather than looking it up, which is
				// what lets it work when the primary is on another range -- and
				// is why the lock's own start timestamp has to match: a verdict
				// about one transaction may not be applied to another's lock.
				l := locks[c.Key]
				if l == nil || l.startTS != c.StartTS {
					continue
				}
				if c.Status == kv.TxnCommitted {
					commits[c.Key] = append(commits[c.Key], raftcheck.CommitFact{
						Key: c.Key, StartTS: l.startTS, CommitTS: c.CommitTS,
					})
					delete(locks, c.Key)
					continue
				}
				commits[c.Key] = append(commits[c.Key], raftcheck.CommitFact{
					Key: c.Key, StartTS: l.startTS, CommitTS: l.startTS, Rollback: true,
				})
				delete(locks, c.Key)
				vs = dropVersion(vs, c.Key, l.startTS)
			}
			continue
		}
		if spec, ok := store.DecodeSplitCommand(e.Data); ok {
			// # The rule for whether a split applies, restated
			//
			// A split entry names the extent it was computed against and takes
			// effect only against exactly that extent, one epoch behind. Two
			// leaders can each propose a split from the same extent and both
			// entries can commit; the second names an extent the range has
			// already moved past, and every replica refuses it.
			//
			// A model that applied every split entry would compute a state no
			// replica ever held and report all of them in violation for
			// agreeing with each other (BUG-012).
			applies := spec.Left.Epoch == desc.Epoch+1 &&
				bytes.Equal(spec.Left.Start, desc.Start) &&
				bytes.Equal(spec.Right.End, desc.End)
			if onSplit != nil {
				onSplit(e.Index, applies, spec)
			}
			if !applies {
				continue
			}
			// What the left KEEPS is what its new extent covers -- not
			// everything below the cut point. The two are the same only when the
			// range holds nothing outside its own extent, so writing it the
			// short way made the model quietly repair a range that was holding a
			// key it did not own, and the oracle went green on a state no
			// replica ever had (BUG-014).
			kept := vs[:0]
			for _, v := range vs {
				if spec.Left.Contains(v.Key) {
					kept = append(kept, v)
				}
			}
			vs = kept
			// A split moves EVERY record kind, not just the versions. The first
			// version of this dropped only the data and left the locks and
			// commit records for keys the range had given away -- so the model
			// rendered records the store had moved, and snapshot equivalence
			// fired on every seed that split while a transaction was in flight.
			//
			// The same sentence as BUG-014, in the record dimension: what the
			// left KEEPS is what its new extent covers, for all four kinds.
			for k := range locks {
				if !spec.Left.Contains([]byte(k)) {
					delete(locks, k)
				}
			}
			for k := range commits {
				if !spec.Left.Contains([]byte(k)) {
					delete(commits, k)
				}
			}
			for dk, d := range decided {
				if !spec.Left.Contains([]byte(d.Key)) {
					delete(decided, dk)
				}
			}
			// Five kinds now, not four. A read mark left behind on the parent
			// would refuse prewrites for a key the parent no longer owns, and a
			// mark the child never received would accept one the store refuses.
			for k := range readMarks {
				if !spec.Left.Contains([]byte(k)) {
					delete(readMarks, k)
				}
			}
			desc = spec.Left
			continue
		}
		// A command for a key outside the extent at this position is refused,
		// exactly as the replicas refuse it. Restated, not shared.
		//
		// A write at or below the mark is refused too, for the reason the store
		// refuses it: the version would be one no read may ever see, and whether
		// it survived would depend on when garbage collection next ran, which is
		// not a property a state machine may have.
		op, k, v, at := store.DecodeCommand(e.Data)
		switch op {
		case store.OpGC:
			// Collection restated: everything strictly below the new mark goes,
			// EXCEPT the newest version at or below it for each key -- which is
			// the version a read at the mark's successor still needs. Getting
			// that one off by one is a silently lossy read, so the model says it
			// out loud rather than calling the store's collector.
			if !mark.Less(at) {
				continue
			}
			mark = at
			var kept []kv.Version
			var lastKey string
			seen := false
			for _, ver := range vs {
				if string(ver.Key) != lastKey {
					lastKey, seen = string(ver.Key), false
				}
				if !ver.At.Less(mark) {
					kept = append(kept, ver)
					continue
				}
				if !seen {
					seen = true
					kept = append(kept, ver)
				}
			}
			vs = kept

		case "get":
			if onRead != nil {
				val, found := readModel(vs, k, at)
				onRead(raftcheck.ReadExpectation{
					Index: e.Index, Key: k, At: at,
					Value: val, Found: found, Refused: at.LessEq(mark),
				})
			}

		case "put":
			// A command for a key outside the extent at this position is
			// refused, exactly as the replicas refuse it. Restated, not shared.
			//
			// A write at or below the mark is refused too, for the reason the
			// store refuses it: the version would be one no read may ever see,
			// and whether it survived would depend on when collection next ran,
			// which is not a property a state machine may have.
			if !desc.Contains([]byte(k)) || at.LessEq(mark) {
				continue
			}
			vs = insertVersion(vs, kv.Version{Key: []byte(k), At: at, Value: []byte(v)})
		}
	}
	return desc, mark, vs, locks, true
}

// seedFrom loads a birth payload into the model's logical state.
//
// # All five kinds, because a split-born range INHERITS all five
//
// The first version pulled out the data versions and dropped the rest. A range
// born from a split then started with the values and none of the locks, commit
// records or decisions its parent had -- so the model refused prewrites the
// store accepted, kept versions the store had rolled back, and reported a
// divergence on every seed that split while a transaction was in flight.
//
// That is the same mistake the STORE made one commit earlier in its snapshot
// payload, and the two were made independently. "The state machine is its data"
// is the intuition, and at A6 it is wrong on both sides of the comparison.
func seedFrom(ns []byte, recs []kv.Record, locks map[string]*modelLock,
	commits map[string][]raftcheck.CommitFact, decided map[string]raftcheck.CommitFact,
	readMarks map[string]hlc.Timestamp) []kv.Version {
	var out []kv.Version
	for _, r := range recs {
		kind, ok := kv.KindOf(ns, r.Key)
		if !ok {
			continue
		}
		key, ok := kv.DecodeAnyKey(ns, r.Key)
		if !ok {
			continue
		}
		switch kind {
		case kv.KindData:
			_, at, ok := kv.DecodeKey(ns, r.Key)
			if !ok {
				continue
			}
			out = append(out, kv.Version{Key: key, At: at, Value: r.Value})
		case kv.KindLock:
			l, ok := kv.DecodeLockValue(r.Value)
			if !ok {
				continue
			}
			locks[string(key)] = &modelLock{
				primary: string(l.Primary), startTS: l.StartTS, deadline: l.Deadline,
			}
		case kv.KindWrite:
			_, commitTS, ok := kv.DecodeWriteKey(ns, r.Key)
			if !ok {
				continue
			}
			startTS, rollback, ok := kv.DecodeWriteValue(r.Value)
			if !ok {
				continue
			}
			commits[string(key)] = append(commits[string(key)], raftcheck.CommitFact{
				Key: string(key), StartTS: startTS, CommitTS: commitTS, Rollback: rollback,
			})
		case kv.KindTxn:
			rec, ok := kv.DecodeTxnValue(r.Value)
			if !ok {
				continue
			}
			decided[raftcheck.TxnDecisionKey(string(key), rec.StartTS)] = raftcheck.CommitFact{
				Key: string(key), StartTS: rec.StartTS, CommitTS: rec.CommitTS,
				Rollback: rec.Status != kv.TxnCommitted,
			}
		case kv.KindRead:
			// The fifth kind, inherited like the other four. A split-born range
			// that started with no marks would accept a prewrite its parent had
			// already refused, which is the same mistake seedFrom's own comment
			// records for locks and commit records, one kind later.
			_, at, ok := kv.DecodeReadMarkKey(ns, r.Key)
			if !ok {
				continue
			}
			if rm, seen := readMarks[string(key)]; !seen || rm.Less(at) {
				readMarks[string(key)] = at
			}
		}
	}
	return out
}

// readModel answers a read from the model's own version list: the newest version
// of key at or before at.
//
// Versions are newest-first within a key, so the first match is the answer --
// the same order the encoding produces, restated rather than shared.
func readModel(vs []kv.Version, key string, at hlc.Timestamp) (string, bool) {
	for _, v := range vs {
		if string(v.Key) != key {
			continue
		}
		if v.At.LessEq(at) {
			return string(v.Value), true
		}
	}
	return "", false
}

// dropVersion removes the uncommitted version a rollback discards.
//
// The store's RollbackInto deletes it, because a version no commit record will
// ever point at is unreachable and would sit in the range forever. The model has
// to do the same, or its digest carries a record the store does not.
func dropVersion(vs []kv.Version, key string, at hlc.Timestamp) []kv.Version {
	out := vs[:0]
	for _, v := range vs {
		if string(v.Key) == key && v.At == at {
			continue
		}
		out = append(out, v)
	}
	return out
}

// insertVersion places a version in engine order: by key ascending, and by
// timestamp DESCENDING within a key.
//
// The order is the encoding's, restated. It is not a tidiness choice: the
// snapshot digest is over these bytes, so a model that ordered versions
// differently from the store would report a divergence on every seed that
// wrote twice to one key.
func insertVersion(vs []kv.Version, v kv.Version) []kv.Version {
	i := sort.Search(len(vs), func(i int) bool {
		if c := bytes.Compare(vs[i].Key, v.Key); c != 0 {
			return c >= 0
		}
		return !v.At.Less(vs[i].At)
	})
	if i < len(vs) && bytes.Equal(vs[i].Key, v.Key) && vs[i].At == v.At {
		vs[i].Value = v.Value // a second write at one timestamp overwrites, as Set does
		return vs
	}
	vs = append(vs, kv.Version{})
	copy(vs[i+1:], vs[i:])
	vs[i] = v
	return vs
}

// stateDigest is raftcheck.StateAt.
func stateDigest(base []byte) raftcheck.StateCursor {
	r, ok := store.NewReplay(base)
	if !ok {
		panic("hunt: a range was replayed with no birth state recorded")
	}
	return &replayCursor{base: base, r: r}
}

// replayCursor adapts store.Replay to the oracle's cursor.
//
// The digest is over the WHOLE state machine, which at A6 is four record kinds
// and not one. A digest over the data versions alone would go green on a
// snapshot that dropped every lock.
type replayCursor struct {
	base []byte
	r    *store.Replay
}

// DigestThrough advances the cursor, or restarts it when asked to go BACKWARDS.
//
// # Why backwards happens, and why the first version was wrong to assume it did not
//
// Snapshots are fed in index order, but a snapshot is skipped while the ledger
// has not yet witnessed every committed entry beneath it -- and it becomes
// checkable later, after the cursor has moved past. Asking a forward-only cursor
// for a shorter prefix then returns the state at the LATER index, and the
// comparison fails against a snapshot that was perfectly correct.
//
// Measured: 10 of 100 seeds, immediately. A cursor is an optimisation, and an
// optimisation that changes an answer is a defect -- so when the request goes
// backwards it is answered from a fresh replay, which is what the shape before
// the optimisation always did.
func (c *replayCursor) DigestThrough(prefix []raft.Entry) uint64 {
	r := c.r
	if len(prefix) < r.Applied() {
		fresh, ok := store.NewReplay(c.base)
		if !ok {
			panic("hunt: a range was replayed with no birth state recorded")
		}
		r = fresh
	} else {
		c.r = r
	}
	r.Apply(prefix)
	desc, mark, recs, ok := r.State()
	if !ok {
		panic("hunt: a replay could not read its own state back")
	}
	if r == c.r {
		c.r = r
	}
	return store.StateDigest(desc, mark, recs)
}

// splitSteps is raftcheck.SplitsAt.
func splitSteps(base []byte, entries []raft.Entry) []raftcheck.SplitStep {
	var out []raftcheck.SplitStep
	_, _, _, _, ok := replayFull(base, entries, func(idx raft.Index, applies bool, spec store.SplitSpec) {
		out = append(out, raftcheck.SplitStep{
			Index: idx, Applied: applies,
			Child:      uint64(spec.Right.ID),
			ChildStart: spec.Right.Start,
			ChildEnd:   spec.Right.End,
			ChildEpoch: spec.Right.Epoch,
		})
	}, nil, nil, nil)
	if !ok {
		panic("hunt: a range was replayed with no birth state recorded")
	}
	return out
}

// readExpectations is raftcheck.ReadsAt.
func readExpectations(base []byte, entries []raft.Entry) []raftcheck.ReadExpectation {
	var out []raftcheck.ReadExpectation
	_, _, _, _, ok := replayFull(base, entries, nil, func(e raftcheck.ReadExpectation) {
		out = append(out, e)
	}, nil, nil)
	if !ok {
		panic("hunt: a range was replayed with no birth state recorded")
	}
	return out
}

// txnFacts is raftcheck.TxnFactsAt.
func txnFacts(base []byte, entries []raft.Entry) ([]raftcheck.CommitFact, map[string]raftcheck.CommitFact) {
	commits := map[string][]raftcheck.CommitFact{}
	decided := map[string]raftcheck.CommitFact{}
	if _, _, _, _, ok := replayFull(base, entries, nil, nil, commits, decided); !ok {
		panic("hunt: a range was replayed with no birth state recorded")
	}
	var flat []raftcheck.CommitFact
	for _, cs := range commits {
		flat = append(flat, cs...)
	}
	sort.Slice(flat, func(i, j int) bool {
		if flat[i].Key != flat[j].Key {
			return flat[i].Key < flat[j].Key
		}
		return flat[i].CommitTS.Less(flat[j].CommitTS)
	})
	return flat, decided
}

// resolutions is raftcheck.ResolutionsAt: the two command shapes the resolution
// oracle reads, decoded out of one range's committed log.
//
// It DECODES and stops. It does not say which resolve declared an owner dead --
// deciding that here would re-run the rule the oracle exists to check, and the
// verdict would cancel out against a defect in it, which is exactly how the
// removed model failed.
func resolutions(entries []raft.Entry) ([]raftcheck.ResolveFact, []raftcheck.ProposedRollback) {
	var rs []raftcheck.ResolveFact
	var ps []raftcheck.ProposedRollback
	for _, e := range entries {
		if len(e.Data) == 0 {
			continue
		}
		c, ok := store.DecodeTxnCommand(e.Data)
		if !ok {
			continue
		}
		switch {
		case c.Op == store.OpResolveStatus:
			rs = append(rs, raftcheck.ResolveFact{
				Primary: c.Key, StartTS: c.StartTS,
				Deadline: c.Deadline, ExpireAt: c.ExpireAt,
			})
		case c.Op == store.OpPutTxnRecord && c.Status != kv.TxnCommitted:
			ps = append(ps, raftcheck.ProposedRollback{Primary: c.Key, StartTS: c.StartTS})
		}
	}
	return rs, ps
}

// recoveredStates is raftcheck.RecoveredAt: every range's final state machine,
// decoded into the four record kinds.
//
// The state comes from store.ReplayMachine -- the real apply path over a fresh
// engine -- and this only decodes the result. The invariants oracle then asserts
// properties of that result and never of how it was reached, which is the
// distinction that makes it legitimate where the removed model was not.
func recoveredStates(l *raftcheck.Ledger, clocks map[uint64]hlc.Timestamp) []raftcheck.RecoveredState {
	var out []raftcheck.RecoveredState
	for _, rl := range l.Ranges() {
		if rl.Base() == nil {
			continue
		}
		desc, mark, recs, ok := store.ReplayMachine(rl.Base(), rl.Committed())
		if !ok {
			continue
		}
		st := raftcheck.RecoveredState{
			Range: rl.ID(), Start: desc.Start, End: desc.End, GCMark: mark,
			Clock: clocks[rl.ID()],
		}
		ns := namespaceOf(desc.ID)
		for _, r := range recs {
			kind, ok := kv.KindOf(ns, r.Key)
			if !ok {
				continue
			}
			key, ok := kv.DecodeAnyKey(ns, r.Key)
			if !ok {
				continue
			}
			switch kind {
			case kv.KindData:
				_, at, ok := kv.DecodeKey(ns, r.Key)
				if ok {
					st.Versions = append(st.Versions, raftcheck.RecoveredVersion{Key: string(key), At: at})
				}
			case kv.KindLock:
				lk, ok := kv.DecodeLockValue(r.Value)
				if ok {
					st.Locks = append(st.Locks, raftcheck.RecoveredLock{
						Key: string(key), Primary: string(lk.Primary),
						StartTS: lk.StartTS, Deadline: lk.Deadline,
					})
				}
			case kv.KindWrite:
				_, commitTS, ok := kv.DecodeWriteKey(ns, r.Key)
				if !ok {
					continue
				}
				startTS, rollback, ok := kv.DecodeWriteValue(r.Value)
				if ok {
					st.Writes = append(st.Writes, raftcheck.CommitFact{
						Key: string(key), StartTS: startTS, CommitTS: commitTS, Rollback: rollback,
					})
				}
			case kv.KindTxn:
				rec, ok := kv.DecodeTxnValue(r.Value)
				if ok {
					st.Decided = append(st.Decided, raftcheck.CommitFact{
						Key: string(key), StartTS: rec.StartTS, CommitTS: rec.CommitTS,
						Rollback: rec.Status != kv.TxnCommitted,
					})
				}
			}
		}
		out = append(out, st)
	}
	return out
}

// extentOf is raftcheck.ExtentOf.
func extentOf(base []byte) (start, end []byte, epoch uint64, ok bool) {
	desc, _, _, ok := store.DecodeMachine(base)
	if !ok {
		return nil, nil, 0, false
	}
	return desc.Start, desc.End, desc.Epoch, true
}

// RaftResult is one Raft run.
type RaftResult struct {
	Ledger  *raftcheck.Ledger
	History *sim.History

	// StaleEpochDrops is how many completions from a dead incarnation this run's
	// nodes refused. It is EVIDENCE, not a verdict, and
	// TestStaleDurabilityCompletionIsRefused is what asks for it: a nonzero
	// count means the schedules are producing the crash-with-a-sync-in-flight
	// race the guard exists for, and a zero count across a range means the test
	// is proving nothing.
	//
	// sim.EpochGuard.Check is deliberately NOT called here. It reads any drop as
	// a driver defect, which is right for a component that can decline to emit
	// the completion and wrong for this one: the simulator owns the event queue,
	// a durability event scheduled before a crash is delivered after the restart
	// whatever the driver wants, and the stamp is the only thing that can tell it
	// apart. Calling it would have failed 3 seeds in 200 for doing exactly what
	// the guard is for. Collecting a verdict nobody consults is worse than not
	// collecting it, so the field it was stored in is gone.
	StaleEpochDrops int

	// DurabilityCrossChecks is how often a node's durability record was compared
	// against the engine's own account. Evidence, like StaleEpochDrops: a count
	// of zero means the comparison never ran and any test resting on it proved
	// nothing.
	DurabilityCrossChecks int

	// SnapshotsTaken, SnapshotsApplied and TransfersAsked are A2's evidence that
	// its three features ran at all. A sweep in which no snapshot was ever taken
	// proves nothing about snapshots, however green it is.
	SnapshotsTaken   int
	SnapshotsApplied int
	TransfersAsked   int

	// A3's evidence. ConfRefused is not a failure count: a change refused
	// because one is already in flight, or because a learner lags, is the rule
	// working. A sweep where every change was refused is the one that proves
	// nothing, which is why both are reported.
	ConfProposed    int
	ConfRefused     int
	LagRefused      int
	ConfRecoveries  int
	ConfCrossChecks int

	// A4's evidence. Ranges is how many the cluster ended with, Splits how many
	// were applied, and StaleEpochRefusals how many requests arrived under a
	// descriptor the range had moved past -- which is the epoch invariant doing
	// its job rather than a failure count.
	Ranges              int
	SplitsProposed      int
	SplitsApplied       int
	StaleEpochRefusals  int
	StaleSplits         int
	OutOfExtentRefusals int

	// D-A7-6's two propositions plus their non-vacuity. NoOpsApplied must be
	// NON-zero once A7's term-start no-op lands (one per election); the other two
	// must be zero. DESIGN-A7 §3a.2.
	NoOpsApplied   int
	NoOpReachedArm int
	NoOpAnswered   int

	// ReadsServed counts reads answered by read index rather than by the log.
	ReadsServed int

	// FollowerReads is how many reads a NON-LEADER answered. Ruling 2's
	// non-vacuity witness.
	FollowerReads int

	// ReadAgreeCompared is how many off-log answers the differential oracle
	// actually compared. Its NON-VACUITY witness: the oracle is silent when it
	// has no model and when there are no off-log answers, and those silences
	// look identical to a clean run from outside.
	ReadAgreeCompared int

	// ReadIndexRuns is 1 when this run had the read-index path ON. It exists
	// so the non-vacuity assertion can tell "served none" from "never asked to",
	// which under D-A7-4's two-paths decision are different runs rather than a
	// pass and a failure.
	ReadIndexRuns int

	// MovesOrdered and MovesCompleted are the manual rebalance's non-vacuity
	// evidence. A sweep in which every move stalled is green about a mechanism
	// that never finished, and a stalled move is SAFE -- which is exactly why the
	// rebalance oracle alone cannot tell the two apart.
	MovesOrdered   int
	MovesCompleted int

	// MovesRacingChurn is the bidirectional half of a recorded gap: it must be
	// zero, and the exit run fails if it is not. DESIGN-A4 section 10.
	MovesRacingChurn int

	// MovesUnattributable is how many moves rebalance-safety could not judge
	// because the churn driver had also ordered a change to the same node in
	// the same window. Neither a pass nor a violation (Amendment A4).
	MovesUnattributable int

	// A5's evidence. Every one of these is asserted in the exit run: a count
	// nobody asserts on is decoration that looks like evidence.
	GCProposed        int
	GCApplied         int
	VersionsCollected int
	MVCCReadsRefused  int
	MVCCWritesRefused int
	EnvelopeRefusals  int
	SnapshotReads     int

	// A6's evidence, and the same rule again: every count below is asserted in
	// the exit run or deleted. They divide into three groups -- what the bank
	// did, what its READS ran into, and what the recovery path did about it --
	// because a green sweep in which no read ever found a lock says nothing
	// about the half of Percolator that matters.
	TxnStarted            int
	TxnCommitted          int
	TxnAbandoned          int
	TxnAborted            int
	TxnLostToResolver     int
	TxnReads              int
	ReaderResolves        int
	UncertaintyRestarts   int
	LedgerRestarts        int
	TxnReadsRefused       int
	AuditsStarted         int
	AuditsComplete        int
	AuditsLocked          int
	AuditsUncertain       int
	AuditsRetried         int
	IdentityCollisions    int
	ForeignLocksKept      int
	SnapshotsCompared     int
	SecondPassReads       int
	ForeignTagStarts      int
	StaleRestarts         int
	StaleIncarnation      int
	ResolveWaited         int
	ResolvedForward       int
	ResolvedBack          int
	UnparseableReads      int
	WriteConflicts        int
	PrewriteBlocked       int
	ReadMarks             int
	ReadConflicts         int
	TxnRaceLost           int
	ResolveWaits          int
	ResolveAlreadyDecided int
	ResolveDeclaredDead   int
	ResolveNoLock         int
	RollForwards          int
	RollBacks             int
	ReadsBlocked          int
	ResolverDeclarations  int

	Seed     uint64
	Outcome  sim.Outcome
	Reports  []sim.Report
	Census   raftcheck.Census
	Violated *sim.Violation
	Err      error
}

// syncLatency is the modelled fsync for a Raft node's engine.
//
// It must exceed a replication round trip for the persist-before-reply window to
// exist at all -- the same regime argument the toy's window gate makes, and for
// the same reason. Twelve milliseconds: max(crash targeting delay, worst-case
// RTT from the generator's 6ms slowest link).
const syncLatency = clock.Instant(12_000_000)

// RunRaft builds a three-node Raft group on a plan, drives client traffic
// against it, and checks the result.
func RunRaft(p *plan.Plan, tr *sim.Trace) (RaftResult, error) {
	return RunRaftWith(p, CurrentOptions(), tr)
}

// RunRaftWith drives the group with explicit build options.
func RunRaftWith(p *plan.Plan, opt RaftOptions, tr *sim.Trace) (RaftResult, error) {
	res := RaftResult{Seed: p.Provenance.Seed}
	if opt.ReadIndex {
		// Recorded per run so the non-vacuity assertion can tell "this sweep
		// served no reads off the log" from "this sweep never turned the path
		// on", which under D-A7-4 are different runs rather than a failure.
		res.ReadIndexRuns = 1
	}
	n := p.Config.Nodes

	hist := sim.NewHistory()
	ledger := raftcheck.NewLedger(n)

	nodes := make([]sim.Node, n)
	for i := range nodes {
		nodes[i] = &lateBinder{}
	}
	run, err := plan.Build(p, nodes)
	if err != nil {
		return res, fmt.Errorf("hunt: build: %w", err)
	}
	if tr != nil {
		run.Loop.SetTrace(tr)
	}

	// The oracles watch the run and halt it at the first violation. They read
	// the ledger and nothing else (DESIGN-A1 §0).
	oracles, reb, _ := raftcheck.AllWithRebalance(
		ledger, stateDigest, splitSteps, extentOf, readExpectations, txnFacts, valueAtIndex(ledger))
	run.Loop.SetOracles(oracles)

	peers := make([]raft.NodeID, n)
	for i := range peers {
		peers[i] = raft.NodeID(i + 1)
	}
	// The highest-numbered nodes start as learners, so the voter set is a stable
	// prefix and a seed's initial configuration is readable from its node count.
	var learners []raft.NodeID
	for i := n - opt.Learners; i < n; i++ {
		if i >= 0 {
			learners = append(learners, raft.NodeID(i+1))
		}
	}

	// Election jitter is plan-derived: a pure state machine cannot randomize for
	// itself, and a live draw would break replay.
	jitKey, err := rng.ParseKey(p.Keys.Raft)
	if err != nil {
		return res, fmt.Errorf("hunt: raft key: %w", err)
	}

	// coordRef is set once the coordinator exists, and read from inside the
	// store's callback. The indirection is because the drivers must exist before
	// the coordinator can address them, and the coordinator must exist before a
	// driver can call back into it -- the same late-binding the transport uses.
	var coordRef *coordinator
	drivers := make([]*store.Node, n)
	for i := range nodes {
		ord := i
		d, err := store.New(store.Config{
			ID: raft.NodeID(i + 1), Peers: peers, Ordinal: ord,
			Election: 10, Heartbeat: 3,
			Nodes:             n,
			Learners:          learners,
			PromotionLag:      opt.PromotionLag,
			SplitThreshold:    opt.SplitThreshold,
			GCRetention:       opt.GCRetention,
			GCUnthrottled:     opt.GCUnthrottled,
			PreVote:           opt.PreVote,
			SnapshotThreshold: opt.SnapshotThreshold,
			ReadIndex:         opt.ReadIndex,
			SyncLatency:       syncLatency,
			Transport:         run.Transport, Ledger: ledger, History: hist,
			OnTxnApplied: func(c store.TxnCommand, r store.TxnResult, at clock.Instant, s sim.Scheduler) {
				if coordRef != nil {
					coordRef.applied(c, r, at, s)
				}
			},
			// The node's own clock, with its own timeline: skew between nodes
			// is what the HLC exists to reconcile, so handing every node the
			// same clock would make A5's whole property vacuous.
			Clock: run.Clocks[ord],
			ElectionJitter: func(term raft.Term) int {
				return 10 + int(jitKey.Uint64N(0, uint64(ord), uint64(term), 0, 10))
			},
		})
		if err != nil {
			return res, err
		}
		drivers[i] = d
		nodes[i].(*lateBinder).inner = d
	}

	// A scheduled promote is A2's leadership transfer: whoever is leading hands
	// off to the named node. The plan carries it as an action, so it replays.
	run.OnConfChange = func(target sim.NodeID) {
		// Recorded whether or not anybody acts on it. An order the cluster
		// ignored still makes a later removal of that node ambiguous, because
		// the oracle cannot see which orders were acted on either.
		ledger.RecordConfOrder(provenance.Witness(raftcheck.ConfOrder{
			Node: int(target) + 1, At: run.Loop.Now(),
		}))
		for _, d := range drivers {
			if d.IsLeader() {
				d.RequestConfChange(raft.NodeID(int(target) + 1))
				break
			}
		}
	}

	// A rebalance moves one replica of one range. The RANGE is chosen here, from
	// the ledger -- which is built entirely from what the harness observed --
	// rather than asked of a store. The oracle has to know which range the order
	// named, and taking that from the system it judges is the shape this project
	// spent A1 learning to refuse: a store that started no move at all could
	// name a quiet range, and the check would come out green over nothing.
	//
	// Round-robin over the ranges in sorted order, stepped by a counter, so
	// successive orders spread across whatever has split so far and the choice
	// stays a function of the plan.
	// # A move takes several orders, and they all have to be the SAME move
	//
	// The mechanism is stateless: each order advances one step. So a move needs
	// three or four orders -- add, promote, sometimes a leadership handoff, then
	// remove -- and every one of them has to name the range and the two nodes
	// the first one named. Rotating the target between orders is how a driver
	// issues six orders and completes zero moves, which is exactly what the
	// first version did: 444 moves ordered across 300 seeds, none finished, a
	// mechanism declared and never invoked.
	//
	// So orders come in groups of movesPerOrder against one fixed move, and the
	// range rotates between groups rather than within them.
	const movesPerOrder = 4
	order, group, live := 0, -1, false
	var cur raftcheck.MoveRecord
	run.OnRebalance = func(from, to sim.NodeID) {
		g := order / movesPerOrder
		order++

		if g != group || !live {
			group, live = g, false
			ranges := ledger.Ranges()
			if len(ranges) == 0 {
				return
			}
			rl := ranges[g%len(ranges)]
			conf, ok := rl.CommittedConfig()
			if !ok {
				return
			}

			// # A move to a node that is already there is not a move
			//
			// The plan names two nodes; the cluster decides whether that pair
			// is a move. If the destination is already a member, the mechanism
			// has nothing to add and goes straight to removing the source --
			// a quorum reduction wearing a move's name, and the oracle said so
			// on 252 of 300 seeds. Endpoints are validated here against the
			// COMMITTED configuration the ledger derives from what it observed,
			// and an order that is not a move is never issued and never
			// recorded.
			//
			// The plan's two numbers are the starting points of a deterministic
			// scan, so the choice stays a function of the seed.
			src, dst := raft.NodeID(0), raft.NodeID(0)
			for i := range n {
				c := raft.NodeID((int(from)+i)%n + 1)
				if conf.IsVoter(c) && len(conf.Voters) > 1 {
					src = c
					break
				}
			}
			for i := range n {
				c := raft.NodeID((int(to)+i)%n + 1)
				if !conf.IsVoter(c) && !conf.IsLearner(c) {
					dst = c
					break
				}
			}
			if src == 0 || dst == 0 {
				return
			}
			// The move is recorded only once a leader ACCEPTS it. An order
			// nobody could act on is not a move the cluster was asked to make,
			// and recording it would leave the oracle judging a range against
			// an intent that never reached it.
			started := false
			for _, d := range drivers {
				if d.RequestMove(store.RangeID(rl.ID()), src, dst, true) {
					started = true
					break
				}
			}
			if !started {
				return
			}
			cur = raftcheck.MoveRecord{Range: rl.ID(), From: src, To: dst, At: run.Loop.Now()}
			live = true
			ledger.RecordMove(provenance.Witness(cur))
			return
		}

		for _, d := range drivers {
			if d.RequestMove(store.RangeID(cur.Range), cur.From, cur.To, false) {
				break
			}
		}
	}

	run.OnPromote = func(target sim.NodeID) {
		for _, d := range drivers {
			if d.RequestTransfer(raft.NodeID(int(target) + 1)) {
				break
			}
		}
	}

	// # The bank
	//
	// Transfers are scheduled through the middle of the run, so they overlap
	// crashes, splits and each other. Each is driven by the coordinator in
	// bank.go: an ordinary client issuing one step at a time.
	var coord *coordinator
	if opt.Transfers2PC > 0 && opt.Accounts > 0 {
		coord = newCoordinator(drivers, n, hist, ledger, opt, p)
		coordRef = coord
		if err := coord.schedule(run, p); err != nil {
			return res, err
		}
	}

	// Client operations go to every node; only the leader answers. That is how a
	// request reaches a leader whose identity is not known until the run
	// produces it -- the same routing the toy uses under failover.
	snapshotReads := 0
	readKey, err := rng.ParseKey(p.Keys.Raft)
	if err != nil {
		return res, fmt.Errorf("hunt: raft key: %w", err)
	}
	for _, op := range p.Workload.Ops {
		// A share of reads name a REMEMBERED timestamp: see the comment below.
		// Decided first, because such a read never enters the linearizability
		// history at all -- it is not an operation on the current value, and
		// Begin-then-cancel would leave a hole in a record that is supposed to
		// be append-only.
		snapshotRead := op.Kind == "get" && opt.SnapshotReadPerMille > 0 &&
			readKey.Uint64N(8, uint64(op.Seq), uint64(op.Client), 0, 1000) < opt.SnapshotReadPerMille
		if snapshotRead {
			snapshotReads++
		}

		idx := -1
		if !snapshotRead {
			idx = hist.Begin(clock.Instant(op.AtNS), op.Client, op.Seq, op.Kind, op.Key, op.Value)
		}
		// # Every request routes from a descriptor cache, and the cache is stale
		//
		// This carried no routing at all -- Range and Epoch zero, "unrouted" --
		// so the epoch check at arrival was skipped on every request the sweep
		// ever made. Across 10,000 seeds it refused **zero**. A mechanism
		// declared, wired and never invoked is the eleventh instance of the
		// class this project has been counting, and it was guarding an invariant
		// CLAUDE.md names: *no request served under a stale descriptor epoch.*
		//
		// So every request now routes the way a client with a cold cache does:
		// believing the whole key space is the first range at epoch one, which is
		// exactly what a client that has never refreshed believes. Once anything
		// splits, requests for moved keys arrive naming a range that no longer
		// owns them and requests for kept keys arrive naming an epoch that has
		// moved on -- and both refusal paths run, are counted, and retry with the
		// routing the replica corrected.
		//
		// Modelling the cache as cold on EVERY request rather than persistent is
		// deliberate and it over-exercises the path. A persistent cache would
		// refuse once per split; this refuses once per request after a split,
		// which is more traffic through the check than a real client would
		// generate. For a mechanism whose measured exercise rate was zero, more
		// is the right direction to be wrong in.
		req := store.Request{
			Client: op.Client, Seq: op.Seq, Op: op.Kind,
			Key: op.Key, Value: op.Value, HistIdx: idx,
			Range: store.FirstRange, Epoch: 1,
		}

		// # A share of reads name a REMEMBERED timestamp
		//
		// The client asks for the value as of a point in the past, which is what
		// a snapshot read is and what A6's transactions will do for real. The
		// timestamp is a wall reading from earlier in this run, taken off a
		// node's own timeline, so it is a time the cluster actually passed
		// through rather than a number invented by the harness.
		//
		// The lookback deliberately straddles the retention window: some land
		// above the collection mark and are answered from history, some land
		// below it and must be REFUSED. Both are checked, because a store that
		// refused everything would pass a checker that only inspected wrong
		// answers.
		//
		// These reads are excluded from the linearizability history, and that is
		// not a dodge: a read at a past timestamp is not a linearizable
		// operation on the current value, and feeding it to porcupine as one
		// would manufacture violations out of correct behaviour. Their
		// correctness is judged by mvcc-read-correctness instead, which is the
		// oracle that knows what timestamp they named.
		if snapshotRead {
			back := time.Duration(readKey.Uint64N(9, uint64(op.Seq), uint64(op.Client), 0, uint64(4*time.Second)))
			when := clock.Instant(op.AtNS) - clock.Instant(back)
			if when < 0 {
				when = 0
			}
			req.ReadTS = hlc.Timestamp{Wall: run.Clocks[0].Timeline().Wall(when)}
		}
		// # A share of plain reads is addressed to ONE replica (D-A7-2)
		//
		// Everything else is delivered to every node and acted on only by the
		// leader, which is a broadcast with a filter. Under that dispatch a
		// follower can never serve anything, so follower reads were implemented
		// and exercised by nothing.
		//
		// These are addressed to a single replica chosen from the seed, and that
		// replica answers whether or not it is the leader -- forwarding to the
		// leader for an index and waiting for its own apply. If it happens to
		// BE the leader, the read takes the ordinary path, which is why the
		// count that matters is FollowerReads rather than the number dispatched.
		if opt.FollowerReadPerMille > 0 && op.Kind == "get" && !req.ReadTS.IsSet() &&
			readKey.Uint64N(11, uint64(op.Seq), uint64(op.Client), 0, 1000) <
				uint64(opt.FollowerReadPerMille) {
			req.AnyReplica = true
			target := int(readKey.Uint64N(12, uint64(op.Seq), uint64(op.Client), 0, uint64(len(nodes))))
			run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, sim.NodeID(target), req)
			continue
		}
		for i := range nodes {
			run.Loop.At(clock.Instant(op.AtNS), sim.KindClient, sim.NodeID(i), req)
		}
	}

	out, err := run.Loop.Run()
	if err != nil {
		return res, fmt.Errorf("hunt: run: %w", err)
	}
	res.Outcome = out
	res.Violated = run.Loop.Violation()

	// The Percolator invariants are properties of the FINAL state, so they are
	// evaluated once, here, rather than on every step. That is not an
	// optimisation dressed as a principle: evaluating them mid-run would assert
	// an eventual property against a run caught mid-cleanup, and it cost ten
	// times the runtime for the privilege (raftcheck.PercolatorInvariants.Interested).
	// The A6 oracles are all whole-history or final-state properties, so they
	// are evaluated once, here. Ordered cheapest-evidence-first: a conservation
	// failure names one audit and one number, and is the easiest of the four to
	// read on the way to a seed.
	if res.Violated == nil && coord != nil {
		res.Violated = raftcheck.NewBankConservation(ledger, opt.Accounts).Check()
	}
	if coord != nil {
		si := raftcheck.NewSnapshotIsolation(ledger)
		if v := si.Check(); v != nil && res.Violated == nil {
			res.Violated = v
		}
		res.SnapshotsCompared = si.Compared()
		res.SecondPassReads = coord.SecondPass()
		res.ForeignTagStarts = coord.ForeignTagStarts()
		res.StaleRestarts = coord.StaleRestarts()
		res.StaleIncarnation = coord.StaleIncarnation()
	}
	if res.Violated == nil && coord != nil {
		res.Violated = raftcheck.NewUncertaintyEnvelope(
			ledger, time.Duration(p.Config.MaxOffsetNS)).Check()
	}
	if res.Violated == nil {
		// Every range's clock, from whichever node hosts it. Merged rather than
		// taken from one node: a range lives on several, and the invariant is
		// about the clock that will stamp the next read there.
		clocks := map[uint64]hlc.Timestamp{}
		for _, d := range drivers {
			for id, ts := range d.RangeClocks() {
				if cur, seen := clocks[id]; !seen || ts.Less(cur) {
					clocks[id] = ts // the LOWEST, which is the one that can go stale
				}
			}
		}
		// Recovered ONCE and shared. Each call replays every range's whole
		// committed log through the real apply path, which is the cost that
		// kept these oracles out of OnStep in the first place; two oracles
		// wanting the same state is not a reason to pay it twice.
		var states []raftcheck.RecoveredState
		var recoveredOnce bool
		recovered := func() []raftcheck.RecoveredState {
			if !recoveredOnce {
				states, recoveredOnce = recoveredStates(ledger, clocks), true
			}
			return states
		}
		inv := raftcheck.NewPercolatorInvariants(ledger, recovered)
		res.Violated = inv.Check()

		// # Why this one is here and not beside the invariants
		//
		// It needs BOTH halves: the rolled-back records out of the recovered
		// state, and the permission out of the committed log. The invariants
		// oracle takes only the first and is documented as taking only the
		// first -- a property of the final state, checkable by inspection -- so
		// handing it a log would make it a different kind of check under the
		// same name.
		//
		// It is the first built answer to the symmetric-apply gap that is a
		// DETECTOR rather than a mutant class (DESIGN-A6 sections 13.4, 35.4).
		ra := raftcheck.NewResolutionAuthority(ledger, resolutions, recovered)
		if v := ra.Check(); v != nil && res.Violated == nil {
			res.Violated = v
		}
		res.ResolverDeclarations = ra.Declarations()
	}

	// # A7's differential, called HERE rather than left in the oracle list
	//
	// `sim.Oracle` is OnStep and Interested. It has no Check, so a final-state
	// oracle put in the list built by AllWithRebalance is **never invoked** --
	// SetOracles drives OnStep and nothing else. This one was in that list and
	// compared ZERO answers across every seed, which is register entry 4 in a
	// new place: a checker wired into something that does not call it.
	//
	// It was caught by the oracle's own non-vacuity counter rather than by
	// reading the code, which is the argument for having built the counter.
	if opt.ReadIndex {
		agree := raftcheck.NewReadIndexAgreement(ledger, valueAtIndex(ledger))
		if v := agree.Check(); v != nil && res.Violated == nil {
			res.Violated = v
		}
		res.ReadAgreeCompared = agree.Compared()
	}
	if coord != nil {
		res.TxnStarted = coord.Started()
		res.TxnCommitted = coord.Committed()
		res.TxnAbandoned = coord.Abandoned()
		res.TxnAborted = coord.Aborted()
		res.TxnLostToResolver = coord.LostToResolver()
		res.TxnReads = coord.Reads()
		res.ReaderResolves = coord.ReaderResolves()
		res.UncertaintyRestarts = coord.Restarts()
		// # Two counts of one fact, because a recording path can stop being
		// # called and nothing notices
		//
		// `TxnRecord.Restarts` existed as a field that nothing wrote, and
		// `StartTS` kept the abandoned timestamp -- so the ledger described a
		// transaction that never existed and read zero when asked how often it
		// had moved. The comparison below is what makes that state impossible to
		// reach silently.
		res.LedgerRestarts = ledger.TxnRestarts()
		res.TxnReadsRefused = coord.RefusedReads()
		res.AuditsStarted = coord.AuditsStarted()
		res.AuditsComplete = coord.AuditsComplete()
		res.AuditsLocked = coord.AuditsLocked()
		res.AuditsUncertain = coord.AuditsUncertain()
		res.AuditsRetried = coord.AuditsRetried()
		res.IdentityCollisions = coord.IdentityCollisions()
		res.ForeignLocksKept = coord.ForeignLocksKept()
		res.ResolveWaited = coord.ResolveWaited()
		res.ResolvedForward = coord.ResolvedForward()
		res.ResolvedBack = coord.ResolvedBack()
		res.UnparseableReads = coord.Unparseable()
	}
	res.Census = ledger.Census()

	// A node that stopped while still withholding a message is a stall, not a
	// clean run. A permanently withheld message is indistinguishable from one
	// never generated, so it is surfaced rather than left as silence.
	for i, d := range drivers {
		if err := d.AssertQuiescent(); err != nil {
			return res, fmt.Errorf("hunt: node %d: %w", i, err)
		}
		// How often the schedule produced a completion that outlived the
		// incarnation that asked for it. Collected as evidence, judged by
		// TestStaleDurabilityCompletionIsRefused, and never by a verdict here --
		// see the field's own comment for why a drop is the guard working.
		res.StaleEpochDrops += d.StaleEpochDrops()
		res.DurabilityCrossChecks += d.DurabilityCrossChecks()
		res.SnapshotsTaken += d.SnapshotsTaken()
		res.SnapshotsApplied += d.SnapshotsApplied()
		res.TransfersAsked += d.TransfersAsked()
		res.ConfProposed += d.ConfProposed()
		res.ConfRefused += d.ConfRefused()
		res.LagRefused += d.LagRefused()
		res.ConfRecoveries += d.ConfRecoveries()
		res.ConfCrossChecks += d.ConfCrossChecks()
		res.SplitsProposed += d.SplitsProposed()
		res.SplitsApplied += d.Splits()
		res.StaleEpochRefusals += d.StaleEpochRefusals()
		res.StaleSplits += d.StaleSplits()
		res.OutOfExtentRefusals += d.OutOfExtentRefusals()
		res.NoOpsApplied += d.NoOpsApplied()
		res.NoOpReachedArm += d.NoOpReachedArm()
		res.NoOpAnswered += d.NoOpAnswered()
		res.ReadsServed += d.ReadsServed()
		res.FollowerReads += d.FollowerReads()
		res.GCProposed += d.GCProposed()
		res.GCApplied += d.GCApplied()
		res.VersionsCollected += d.VersionsCollected()
		res.MVCCReadsRefused += d.MVCCReadsRefused()
		res.MVCCWritesRefused += d.MVCCWritesRefused()
		res.EnvelopeRefusals += d.EnvelopeRefusals()
		res.WriteConflicts += d.WriteConflicts()
		res.PrewriteBlocked += d.PrewriteBlocked()
		res.ReadMarks += d.ReadMarks()
		res.ReadConflicts += d.ReadConflicts()
		res.TxnRaceLost += d.TxnRaceLost()
		res.ResolveWaits += d.ResolveWaits()
		res.ResolveAlreadyDecided += d.ResolveAlreadyDecided()
		res.ResolveDeclaredDead += d.ResolveDeclaredDead()
		res.ResolveNoLock += d.ResolveNoLock()
		res.RollForwards += d.RollForwards()
		res.RollBacks += d.RollBacks()
		res.ReadsBlocked += d.ReadsBlocked()
		if c := d.RangeCount(); c > res.Ranges {
			res.Ranges = c
		}
	}

	// The fire-count assertion only means anything on a run that reached its
	// deadline. A run an oracle halted stopped early by construction, so its
	// schedule is legitimately incomplete -- and reporting a shortfall there
	// would replace the violation with a harness error, hiding the finding
	// behind a complaint about the finding's own consequence.
	if res.Outcome.Kind == sim.OutcomeDeadline {
		if short := run.Counters.Check(); len(short) > 0 {
			return res, fmt.Errorf("hunt: the run injected less than its plan asserts: %v", short)
		}
	}
	res.History = hist
	res.Ledger = ledger
	res.MovesOrdered = len(ledger.Moves())
	res.MovesCompleted = ledger.MovesCompleted()
	res.MovesRacingChurn = ledger.MovesRacingUnrelatedChanges()
	res.MovesUnattributable = reb.Unattributable()

	res.SnapshotReads = snapshotReads
	res.Reports = sim.CheckAll(run.Counters, hist, checker.NewLinearizability())

	// # A run with no leader concluded nothing, whatever the checkers say
	//
	// sim.CheckAll enforces the vacuous-green rule from the client's side: a
	// history that is mostly unknowns cannot bank a pass. This is the same rule
	// from the cluster's side, and it is a separate fact rather than a
	// restatement -- the checkers are told about operations, and "no node ever
	// became leader" is not an operation.
	//
	// It is worth asserting on its own because it is the shape the codec
	// off-by-one had: no leader, no answers, and a clean linearizability verdict
	// over forty unknowns (BUGS.md BUG-001). A safety claim over a cluster that
	// never did the thing whose safety is asserted is vacuous, so it is reported
	// as inconclusive.
	markVacuousIfNoLeader(res.Reports, res.Census)
	return res, nil
}

// markVacuousIfNoLeader downgrades a pass to inconclusive on a run that never
// elected anybody. Separated out so it can be induced directly: the condition is
// rare enough in the schedule mix -- 0 of 10,000 seeds -- that a sweep is no
// evidence at all that the rule works.
//
// A VIOLATION is never downgraded. A run that misbehaved without ever electing
// anybody found something real, and turning that into "we cannot tell" would
// lose it.
func markVacuousIfNoLeader(reports []sim.Report, census raftcheck.Census) {
	if census.ElectionsWon != 0 {
		return
	}
	for i := range reports {
		if reports[i].Verdict != sim.VerdictPass {
			continue
		}
		reports[i].Verdict = sim.VerdictInconclusive
		reports[i].Detail = "no node ever became leader in this run, so the cluster never did " +
			"the thing whose safety this verdict asserts; a pass here is a statement about nothing"
	}
}

// RaftCensus aggregates a sweep.
type RaftCensus struct {
	Seeds        int
	Violations   int
	Inconclusive int
	Pass         int
	Errors       int

	Terms          uint64
	ElectionsStart int
	ElectionsWon   int
	SplitVotes     int

	// SeedsWithNoLeader is the number that never elected anybody. A run whose
	// leader is never challenged proves nothing; a run with NO leader proves
	// less than that, because every client operation goes unanswered and the
	// linearizability checker reports green over a history of unknowns.
	SeedsWithNoLeader   int
	SeedsWithContention int

	// A2's evidence that its three features ran. A sweep that never took a
	// snapshot, never installed one and never transferred leadership is green
	// about A1, whatever else it says.
	SnapshotsTaken   int
	SnapshotsApplied int
	TransfersAsked   int

	// A3's. ConfRefused is not a failure count -- a change refused because one is
	// already in flight, or because a learner lags, is the rule working -- but a
	// sweep where every change was refused proves nothing, so both are reported.
	ConfProposed    int
	ConfRefused     int
	LagRefused      int
	ConfRecoveries  int
	ConfCrossChecks int

	Ranges              int
	SplitsProposed      int
	SplitsApplied       int
	StaleEpochRefusals  int
	OutOfExtentRefusals int
	NoOpsApplied        int
	NoOpReachedArm      int
	NoOpAnswered        int
	ReadsServed         int
	FollowerReads       int
	ReadAgreeCompared   int
	ReadIndexRuns       int
	MovesOrdered        int
	MovesCompleted      int
	MovesRacingChurn    int
	MovesUnattributable int

	GCProposed        int
	GCApplied         int
	VersionsCollected int
	MVCCReadsRefused  int
	MVCCWritesRefused int
	EnvelopeRefusals  int
	SnapshotReads     int

	// A6's evidence.
	TxnStarted            int
	TxnCommitted          int
	TxnAbandoned          int
	TxnAborted            int
	TxnLostToResolver     int
	TxnReads              int
	ReaderResolves        int
	UncertaintyRestarts   int
	LedgerRestarts        int
	TxnReadsRefused       int
	AuditsStarted         int
	AuditsComplete        int
	AuditsLocked          int
	AuditsUncertain       int
	AuditsRetried         int
	IdentityCollisions    int
	ForeignLocksKept      int
	SnapshotsCompared     int
	SecondPassReads       int
	ForeignTagStarts      int
	StaleRestarts         int
	StaleIncarnation      int
	ResolveWaited         int
	ResolvedForward       int
	ResolvedBack          int
	UnparseableReads      int
	WriteConflicts        int
	PrewriteBlocked       int
	ReadMarks             int
	ReadConflicts         int
	TxnRaceLost           int
	ResolveWaits          int
	ResolveAlreadyDecided int
	ResolveDeclaredDead   int
	ResolveNoLock         int
	RollForwards          int
	RollBacks             int
	ReadsBlocked          int
	ResolverDeclarations  int

	FirstViolation     uint64
	FoundAViolation    bool
	InconclusiveCauses []string
}

// SweepRaft runs a seed range and aggregates it.
func SweepRaft(from, to uint64) (RaftCensus, error) {
	return SweepRaftWith(from, to, CurrentOptions())
}

// SweepRaftWith sweeps a seed range under explicit options, for the lanes that
// vary the build: the unthrottled collector and the overlapped drivers.
func SweepRaftWith(from, to uint64, opt RaftOptions) (RaftCensus, error) {
	var c RaftCensus
	for seed := from; seed < to; seed++ {
		p, err := MaterializeRaftWith(seed, opt)
		if err != nil {
			return c, err
		}
		r, err := RunRaftWith(p, opt, nil)
		c.Seeds++
		if err != nil {
			c.Errors++
			return c, fmt.Errorf("seed %d: %w", seed, err)
		}

		c = AddCensus(c, CensusOf(seed, r))
	}
	return c, nil
}

// CensusOf is one seed's contribution to a census.
//
// # Why it is a function and not a block inside the sweep
//
// Because two things need it and one of them was silently making do without.
// `SweepRaftWith` folds a seed into a running total; `TestPowerProbe` needs the
// same total so it can ask the exit criteria whether the SWEEP was noticed, and
// the version that did not have this asked a hand-listed subset of the criteria
// instead. A counter added to one place and not the other is a number that reads
// low, which is the shape this file already refuses to trust (see AddCensus).
func CensusOf(seed uint64, r RaftResult) RaftCensus {
	var c RaftCensus
	c.Seeds = 1
	if uint64(r.Census.Terms) > c.Terms {
		c.Terms = uint64(r.Census.Terms)
	}
	c.SnapshotsTaken += r.SnapshotsTaken
	c.SnapshotsApplied += r.SnapshotsApplied
	c.TransfersAsked += r.TransfersAsked
	c.ConfProposed += r.ConfProposed
	c.ConfRefused += r.ConfRefused
	c.LagRefused += r.LagRefused
	c.ConfRecoveries += r.ConfRecoveries
	c.ConfCrossChecks += r.ConfCrossChecks
	c.SplitsProposed += r.SplitsProposed
	c.SplitsApplied += r.SplitsApplied
	c.StaleEpochRefusals += r.StaleEpochRefusals
	c.OutOfExtentRefusals += r.OutOfExtentRefusals
	c.NoOpsApplied += r.NoOpsApplied
	c.NoOpReachedArm += r.NoOpReachedArm
	c.NoOpAnswered += r.NoOpAnswered
	c.ReadsServed += r.ReadsServed
	c.FollowerReads += r.FollowerReads
	c.ReadAgreeCompared += r.ReadAgreeCompared
	c.ReadIndexRuns += r.ReadIndexRuns
	c.MovesOrdered += r.MovesOrdered
	c.MovesCompleted += r.MovesCompleted
	c.MovesRacingChurn += r.MovesRacingChurn
	c.MovesUnattributable += r.MovesUnattributable
	c.GCProposed += r.GCProposed
	c.GCApplied += r.GCApplied
	c.VersionsCollected += r.VersionsCollected
	c.MVCCReadsRefused += r.MVCCReadsRefused
	c.MVCCWritesRefused += r.MVCCWritesRefused
	c.EnvelopeRefusals += r.EnvelopeRefusals
	c.TxnStarted += r.TxnStarted
	c.TxnCommitted += r.TxnCommitted
	c.TxnAbandoned += r.TxnAbandoned
	c.TxnAborted += r.TxnAborted
	c.TxnLostToResolver += r.TxnLostToResolver
	c.TxnReads += r.TxnReads
	c.ReaderResolves += r.ReaderResolves
	c.UncertaintyRestarts += r.UncertaintyRestarts
	c.LedgerRestarts += r.LedgerRestarts
	c.TxnReadsRefused += r.TxnReadsRefused
	c.AuditsStarted += r.AuditsStarted
	c.AuditsComplete += r.AuditsComplete
	c.AuditsLocked += r.AuditsLocked
	c.AuditsUncertain += r.AuditsUncertain
	c.UnparseableReads += r.UnparseableReads
	c.WriteConflicts += r.WriteConflicts
	c.PrewriteBlocked += r.PrewriteBlocked
	c.ReadMarks += r.ReadMarks
	c.ReadConflicts += r.ReadConflicts
	c.TxnRaceLost += r.TxnRaceLost
	c.ResolveWaits += r.ResolveWaits
	c.ResolveAlreadyDecided += r.ResolveAlreadyDecided
	c.ResolveDeclaredDead += r.ResolveDeclaredDead
	c.ResolveNoLock += r.ResolveNoLock
	c.AuditsRetried += r.AuditsRetried
	c.IdentityCollisions += r.IdentityCollisions
	c.ForeignLocksKept += r.ForeignLocksKept
	c.SnapshotsCompared += r.SnapshotsCompared
	c.SecondPassReads += r.SecondPassReads
	c.ForeignTagStarts += r.ForeignTagStarts
	c.StaleRestarts += r.StaleRestarts
	c.StaleIncarnation += r.StaleIncarnation
	c.ResolveWaited += r.ResolveWaited
	c.ResolvedForward += r.ResolvedForward
	c.ResolvedBack += r.ResolvedBack
	c.RollForwards += r.RollForwards
	c.RollBacks += r.RollBacks
	c.ReadsBlocked += r.ReadsBlocked
	c.ResolverDeclarations += r.ResolverDeclarations
	c.SnapshotReads += r.SnapshotReads
	if r.Ranges > c.Ranges {
		c.Ranges = r.Ranges
	}
	c.ElectionsStart += r.Census.ElectionsStart
	c.ElectionsWon += r.Census.ElectionsWon
	c.SplitVotes += r.Census.SplitVotes
	if r.Census.ElectionsWon == 0 {
		c.SeedsWithNoLeader++
	}
	if r.Census.ElectionsWon > 1 || r.Census.SplitVotes > 0 {
		c.SeedsWithContention++
	}

	if r.Violated != nil {
		c.Violations++
		if !c.FoundAViolation {
			c.FoundAViolation, c.FirstViolation = true, seed
		}
	}
	for _, rep := range r.Reports {
		switch rep.Verdict {
		case sim.VerdictPass:
			c.Pass++
		case sim.VerdictViolation:
			c.Violations++
			if !c.FoundAViolation {
				c.FoundAViolation, c.FirstViolation = true, seed
			}
		case sim.VerdictInconclusive:
			c.Inconclusive++
			if len(c.InconclusiveCauses) < 10 {
				c.InconclusiveCauses = append(c.InconclusiveCauses,
					fmt.Sprintf("seed %d: %s", seed, rep.Detail))
			}
		case sim.VerdictUnset:
		}
	}
	return c
}

// AddCensus folds one census into another, for the sharded exit run.
//
// # Why it lives here and not in the test
//
// Because it has to be right, and "right" is field-by-field: Terms and Ranges
// are MAXIMA, FirstViolation is the lowest seed of any shard that saw one, and
// every other number is a total. A summation written in the aggregating test
// would be a second place that has to be updated whenever a counter is added --
// and the failure mode of forgetting is a number that silently reads low, which
// is the shape of every count this project has learned not to trust.
//
// Reflection would remove the maintenance and remove the distinction with it, so
// it is written out.
func AddCensus(a, b RaftCensus) RaftCensus {
	out := a

	if b.Terms > out.Terms {
		out.Terms = b.Terms
	}
	if b.Ranges > out.Ranges {
		out.Ranges = b.Ranges
	}
	// The lowest seed anybody violated at. A shard covering a later range that
	// found the first violation IN ITS RANGE must not overwrite an earlier one.
	if b.FoundAViolation && (!out.FoundAViolation || b.FirstViolation < out.FirstViolation) {
		out.FoundAViolation, out.FirstViolation = true, b.FirstViolation
	}
	out.InconclusiveCauses = append(out.InconclusiveCauses, b.InconclusiveCauses...)
	if len(out.InconclusiveCauses) > 10 {
		out.InconclusiveCauses = out.InconclusiveCauses[:10]
	}

	for _, p := range []struct {
		dst *int
		src int
	}{
		{&out.Seeds, b.Seeds}, {&out.Violations, b.Violations},
		{&out.Inconclusive, b.Inconclusive}, {&out.Pass, b.Pass}, {&out.Errors, b.Errors},
		{&out.ElectionsStart, b.ElectionsStart}, {&out.ElectionsWon, b.ElectionsWon},
		{&out.SplitVotes, b.SplitVotes},
		{&out.SeedsWithNoLeader, b.SeedsWithNoLeader},
		{&out.SeedsWithContention, b.SeedsWithContention},
		{&out.SnapshotsTaken, b.SnapshotsTaken}, {&out.SnapshotsApplied, b.SnapshotsApplied},
		{&out.TransfersAsked, b.TransfersAsked},
		{&out.ConfProposed, b.ConfProposed}, {&out.ConfRefused, b.ConfRefused},
		{&out.LagRefused, b.LagRefused}, {&out.ConfRecoveries, b.ConfRecoveries},
		{&out.ConfCrossChecks, b.ConfCrossChecks},
		{&out.SplitsProposed, b.SplitsProposed}, {&out.SplitsApplied, b.SplitsApplied},
		{&out.StaleEpochRefusals, b.StaleEpochRefusals},
		{&out.OutOfExtentRefusals, b.OutOfExtentRefusals},
		{&out.NoOpsApplied, b.NoOpsApplied},
		{&out.NoOpReachedArm, b.NoOpReachedArm},
		{&out.NoOpAnswered, b.NoOpAnswered},
		{&out.ReadsServed, b.ReadsServed},
		{&out.FollowerReads, b.FollowerReads},
		{&out.ReadAgreeCompared, b.ReadAgreeCompared},
		{&out.ReadIndexRuns, b.ReadIndexRuns},
		{&out.MovesOrdered, b.MovesOrdered}, {&out.MovesCompleted, b.MovesCompleted},
		{&out.MovesRacingChurn, b.MovesRacingChurn},
		{&out.MovesUnattributable, b.MovesUnattributable},
		{&out.GCProposed, b.GCProposed}, {&out.GCApplied, b.GCApplied},
		{&out.VersionsCollected, b.VersionsCollected},
		{&out.MVCCReadsRefused, b.MVCCReadsRefused},
		{&out.MVCCWritesRefused, b.MVCCWritesRefused},
		{&out.EnvelopeRefusals, b.EnvelopeRefusals}, {&out.SnapshotReads, b.SnapshotReads},
		{&out.TxnStarted, b.TxnStarted}, {&out.TxnCommitted, b.TxnCommitted},
		{&out.TxnAbandoned, b.TxnAbandoned}, {&out.TxnAborted, b.TxnAborted},
		{&out.TxnLostToResolver, b.TxnLostToResolver},
		{&out.TxnReads, b.TxnReads}, {&out.ReaderResolves, b.ReaderResolves},
		{&out.UncertaintyRestarts, b.UncertaintyRestarts},
		{&out.LedgerRestarts, b.LedgerRestarts},
		{&out.TxnReadsRefused, b.TxnReadsRefused},
		{&out.AuditsStarted, b.AuditsStarted}, {&out.AuditsComplete, b.AuditsComplete},
		{&out.AuditsLocked, b.AuditsLocked}, {&out.AuditsUncertain, b.AuditsUncertain},
		{&out.AuditsRetried, b.AuditsRetried},
		{&out.IdentityCollisions, b.IdentityCollisions},
		{&out.ForeignLocksKept, b.ForeignLocksKept},
		{&out.SnapshotsCompared, b.SnapshotsCompared},
		{&out.SecondPassReads, b.SecondPassReads},
		{&out.ForeignTagStarts, b.ForeignTagStarts}, {&out.StaleRestarts, b.StaleRestarts},
		{&out.StaleIncarnation, b.StaleIncarnation},
		{&out.ResolveWaited, b.ResolveWaited},
		{&out.ResolvedForward, b.ResolvedForward}, {&out.ResolvedBack, b.ResolvedBack},
		{&out.UnparseableReads, b.UnparseableReads},
		{&out.WriteConflicts, b.WriteConflicts}, {&out.PrewriteBlocked, b.PrewriteBlocked},
		{&out.ReadMarks, b.ReadMarks}, {&out.ReadConflicts, b.ReadConflicts},
		{&out.TxnRaceLost, b.TxnRaceLost},
		{&out.ResolveWaits, b.ResolveWaits},
		{&out.ResolveAlreadyDecided, b.ResolveAlreadyDecided},
		{&out.ResolveDeclaredDead, b.ResolveDeclaredDead},
		{&out.ResolveNoLock, b.ResolveNoLock},
		{&out.RollForwards, b.RollForwards}, {&out.RollBacks, b.RollBacks},
		{&out.ReadsBlocked, b.ReadsBlocked},
		{&out.ResolverDeclarations, b.ResolverDeclarations},
	} {
		*p.dst += p.src
	}
	return out
}

// valueAtIndex is raftcheck.ValueAtIndex: replay a range's committed prefix up
// to `upto` and report what `key` held at `at`.
//
// The prefix is truncated at `upto` deliberately. A read confirmed at index i is
// required to reflect the log AT i, not the log as it eventually ended up --
// comparing against the final state would pass a read that missed a write and
// fail one that correctly did not see a later one.
func valueAtIndex(l *raftcheck.Ledger) raftcheck.ValueAtIndex {
	return func(rangeID uint64, upto raft.Index, key string, at hlc.Timestamp) (string, bool, bool) {
		for _, rl := range l.Ranges() {
			if rl.ID() != rangeID || rl.Base() == nil {
				continue
			}
			committed := rl.Committed()
			prefix := make([]raft.Entry, 0, len(committed))
			for _, e := range committed {
				if e.Index > upto {
					break
				}
				prefix = append(prefix, e)
			}
			desc, _, recs, ok := store.ReplayMachine(rl.Base(), prefix)
			if !ok {
				return "", false, false
			}
			// The visible version at `at`: the highest committed data version at
			// or below it. Derived from the replayed records rather than asked
			// of any store, which is the independence the oracle rests on.
			ns := namespaceOf(desc.ID)
			var bestAt hlc.Timestamp
			var bestVal string
			var found bool
			for _, r := range recs {
				kind, ok := kv.KindOf(ns, r.Key)
				if !ok || kind != kv.KindData {
					continue
				}
				k, vAt, ok := kv.DecodeKey(ns, r.Key)
				if !ok || string(k) != key {
					continue
				}
				if at.Less(vAt) {
					continue
				}
				if !found || bestAt.Less(vAt) {
					bestAt, bestVal, found = vAt, string(r.Value), true
				}
			}
			return bestVal, found, true
		}
		return "", false, false
	}
}
