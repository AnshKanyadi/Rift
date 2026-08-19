// Package raft is a from-scratch Raft state machine.
//
// It is pure: no goroutines, no channels, no clocks, no I/O. Input is
// Step(Message) and Tick(); output is a Ready. That purity is what makes
// deterministic simulation possible, and it is not negotiable (CLAUDE.md).
//
// The package is in the determinism pass's core scope from its first commit, so
// math/rand, time.Now, map iteration and concurrency primitives are build
// failures here rather than review comments.
//
// # What this package does not know
//
// It does not know what a network is, what a disk is, or what time it is. It
// counts ticks.
//
// # The driver has no ordering obligation
//
// raft never hands out a message whose meaning depends on persistent state that
// is not yet durable. Gated messages are withheld here and released in a later
// Ready once AckPersisted arrives, so "persist before reply" is a property of
// this package rather than a rule every caller must remember (DESIGN-A0 DR-7,
// DR-8).
package raft

import (
	"errors"
	"fmt"
)

// NodeID identifies a peer. Zero is "none", so a zero Vote is an unset vote
// rather than a vote for node 0 — the same derived-field discipline as the
// plan's nonzero wall epoch.
type NodeID uint64

// Term is a Raft term. Index is a 1-based log position; zero means "before the
// log begins", which is the only sentinel the log arithmetic needs.
type (
	Term  uint64
	Index uint64
)

// Role is a closed enum: every switch over it must be exhaustive with no default
// arm, which determinismcheck enforces.
type Role uint8

const (
	RoleFollower Role = iota + 1
	RoleCandidate
	RoleLeader
	numRoles
)

func (r Role) String() string {
	switch r {
	case RoleFollower:
		return "follower"
	case RoleCandidate:
		return "candidate"
	case RoleLeader:
		return "leader"
	case numRoles:
		return "invalid"
	}
	return "unknown"
}

// ProposalID identifies a client proposal independently of where it lands in the
// log.
//
// # Why a log index is not a proposal identity
//
// This is the frozen shape from DESIGN-A0 D5, and dropping it produced BUG-004.
// A driver that matched a client's outstanding proposal against an applied entry
// by log index alone told the client its write had succeeded when a later leader
// had overwritten that index with somebody else's command. The index was reused;
// the proposal was not.
//
// Zero is "no proposal", so a forgotten identifier is refused by Propose rather
// than defaulting into a value that happens to match -- the same derived-field
// discipline as the plan's nonzero wall epoch, clock.Hold's unset realization
// and sim.Epoch 0.
type ProposalID struct {
	Node NodeID
	Seq  uint64
}

// Zero reports whether the identifier is unset.
func (p ProposalID) Zero() bool { return p.Node == 0 && p.Seq == 0 }

// EntryType is a closed enum: an entry is a state machine command or a
// configuration change, and nothing decides which by looking at the bytes.
type EntryType uint8

const (
	EntryNormal EntryType = iota + 1
	EntryConfChange
	numEntryTypes
)

func (e EntryType) String() string {
	switch e {
	case EntryNormal:
		return "normal"
	case EntryConfChange:
		return "conf-change"
	case numEntryTypes:
		return "invalid"
	}
	return "unknown"
}

// Entry is one log entry.
type Entry struct {
	Type  EntryType
	Term  Term
	Index Index
	ID    ProposalID
	Data  []byte
}

// ConfChangeType is a closed enum of the changes a single step may make.
type ConfChangeType uint8

const (
	ConfChangeAddVoter ConfChangeType = iota + 1
	ConfChangeAddLearner
	ConfChangeRemoveNode
	numConfChangeTypes
)

func (c ConfChangeType) String() string {
	switch c {
	case ConfChangeAddVoter:
		return "add-voter"
	case ConfChangeAddLearner:
		return "add-learner"
	case ConfChangeRemoveNode:
		return "remove-node"
	case numConfChangeTypes:
		return "invalid"
	}
	return "unknown"
}

// ConfChangeSingle is one server's change.
type ConfChangeSingle struct {
	Type ConfChangeType
	Node NodeID
}

// ConfChangeTransition is how a change gets from the old configuration to the
// new one.
type ConfChangeTransition uint8

const (
	// ConfChangeSimple is the only transition A3 implements: one server at a
	// time, no joint configuration, safe by the overlapping-quorum argument in
	// DESIGN-A3 §4.
	ConfChangeSimple ConfChangeTransition = iota + 1

	// The joint transitions are named because the frozen type is called V2 and
	// V2 is the shape that supports them. They are refused at A3 by Amendment
	// A6, and refusing them by name is how the cut stays visible at the call
	// site instead of being implied by an absence.
	ConfChangeJointImplicit
	ConfChangeJointExplicit
	numConfChangeTransitions
)

func (c ConfChangeTransition) String() string {
	switch c {
	case ConfChangeSimple:
		return "simple"
	case ConfChangeJointImplicit:
		return "joint-implicit"
	case ConfChangeJointExplicit:
		return "joint-explicit"
	case numConfChangeTransitions:
		return "invalid"
	}
	return "unknown"
}

// ConfChangeV2 is the frozen shape from DESIGN-A0 D5.
//
// # The name says joint and the phase says not
//
// D5 froze `ProposeConfChange(id ProposalID, cc ConfChangeV2) error` and nothing
// about the type's contents. ConfChangeV2 is etcd's name for the change type
// that SUPPORTS joint consensus, which Amendment A6 cut. The type therefore has
// its general shape -- a list of changes and a transition -- and A3 refuses
// anything but one simple change, citing A6.
//
// Conforming to the frozen signature and refusing what a later amendment cut
// contradicts neither: D5 constrains the shape, A6 constrains the semantics, and
// both hold at once. Enabling the STRETCH item later deletes a refusal rather
// than changing a frozen signature (DESIGN-A3 §2).
type ConfChangeV2 struct {
	Changes    []ConfChangeSingle
	Transition ConfChangeTransition
}

// EncodeConfChange and DecodeConfChange are the configuration change's wire and
// storage form.
//
// The encoding lives in raft/ rather than in the driver because a configuration
// change is not a state machine command: raft itself has to read one back out of
// its own log to recompute the active configuration after a truncation or a
// restore. A driver-owned encoding would make the pure state machine depend on
// the driver to understand its own log.
//
// Fixed-width and explicit, for the reason every codec here is: an encoding
// discovered at run time is an encoding that can differ between runs.
func EncodeConfChange(cc ConfChangeV2) []byte {
	b := []byte{byte(cc.Transition), byte(len(cc.Changes))}
	for _, ch := range cc.Changes {
		b = append(b, byte(ch.Type))
		var id [8]byte
		for i := 7; i >= 0; i-- {
			id[i] = byte(ch.Node)
			ch.Node >>= 8
		}
		b = append(b, id[:]...)
	}
	return b
}

// DecodeConfChange reads one back.
func DecodeConfChange(b []byte) (ConfChangeV2, bool) {
	if len(b) < 2 {
		return ConfChangeV2{}, false
	}
	cc := ConfChangeV2{Transition: ConfChangeTransition(b[0])}
	n := int(b[1])
	b = b[2:]
	if len(b) != n*9 {
		return ConfChangeV2{}, false
	}
	for range n {
		ch := ConfChangeSingle{Type: ConfChangeType(b[0])}
		var id NodeID
		for i := 1; i <= 8; i++ {
			id = id<<8 | NodeID(b[i])
		}
		ch.Node = id
		cc.Changes = append(cc.Changes, ch)
		b = b[9:]
	}
	return cc, true
}

// EncodeConfiguration and DecodeConfiguration put a membership on the wire and
// in a snapshot.
func EncodeConfiguration(c Configuration) []byte {
	b := []byte{byte(len(c.Voters)), byte(len(c.Learners))}
	for _, n := range append(append([]NodeID(nil), c.Voters...), c.Learners...) {
		for i := 56; i >= 0; i -= 8 {
			b = append(b, byte(n>>uint(i)))
		}
	}
	return b
}

// DecodeConfiguration reads one back.
func DecodeConfiguration(b []byte) (Configuration, bool) {
	if len(b) < 2 {
		return Configuration{}, false
	}
	nv, nl := int(b[0]), int(b[1])
	b = b[2:]
	if len(b) != (nv+nl)*8 {
		return Configuration{}, false
	}
	read := func() NodeID {
		var n NodeID
		for i := range 8 {
			n = n<<8 | NodeID(b[i])
		}
		b = b[8:]
		return n
	}
	var c Configuration
	for range nv {
		c.Voters = append(c.Voters, read())
	}
	for range nl {
		c.Learners = append(c.Learners, read())
	}
	return c, true
}

// Configuration is who is in the cluster.
//
// Voters vote and are counted in quorums. Learners receive entries and apply
// them and are counted in nothing -- which is the entire point: adding a slow
// server directly as a voter raises the quorum while that server can contribute
// nothing, so a cluster that tolerated one failure tolerates none until it
// catches up.
//
// Both slices are sorted and deduplicated. Sorted because this package is in
// core determinism scope and any order that leaks into behaviour must not depend
// on insertion; deduplicated because a node counted twice is a quorum of one
// wearing a hat.
type Configuration struct {
	Voters   []NodeID
	Learners []NodeID
}

// IsVoter reports whether n votes and counts toward quorums.
func (c Configuration) IsVoter(n NodeID) bool { return contains(c.Voters, n) }

// IsLearner reports whether n receives entries without counting.
func (c Configuration) IsLearner(n NodeID) bool { return contains(c.Learners, n) }

// Members is every node in the configuration, voters then learners, each sorted.
func (c Configuration) Members() []NodeID {
	out := make([]NodeID, 0, len(c.Voters)+len(c.Learners))
	out = append(out, c.Voters...)
	out = append(out, c.Learners...)
	sortIDs(out)
	return out
}

// Clone returns a copy, because a configuration is stored in a snapshot and
// replayed from a log and must never alias either.
func (c Configuration) Clone() Configuration {
	return Configuration{
		Voters:   append([]NodeID(nil), c.Voters...),
		Learners: append([]NodeID(nil), c.Learners...),
	}
}

// Equal compares two configurations.
func (c Configuration) Equal(o Configuration) bool {
	if len(c.Voters) != len(o.Voters) || len(c.Learners) != len(o.Learners) {
		return false
	}
	for i := range c.Voters {
		if c.Voters[i] != o.Voters[i] {
			return false
		}
	}
	for i := range c.Learners {
		if c.Learners[i] != o.Learners[i] {
			return false
		}
	}
	return true
}

func (c Configuration) String() string {
	return fmt.Sprintf("voters=%v learners=%v", c.Voters, c.Learners)
}

// ApplyConfChange returns the configuration after one single change, or an
// error. Exported so a checker can derive a configuration independently from the
// same bytes the node read.
func ApplyConfChange(c Configuration, ch ConfChangeSingle) (Configuration, error) {
	return c.apply(ch)
}

// apply returns the configuration after one single change, or an error.
func (c Configuration) apply(ch ConfChangeSingle) (Configuration, error) {
	out := c.Clone()
	switch ch.Type {
	case ConfChangeAddVoter:
		out.Learners = removeID(out.Learners, ch.Node)
		out.Voters = insertID(out.Voters, ch.Node)
	case ConfChangeAddLearner:
		if contains(out.Voters, ch.Node) {
			return c, fmt.Errorf("raft: node %d is a voter and cannot be demoted to a learner in one step", ch.Node)
		}
		out.Learners = insertID(out.Learners, ch.Node)
	case ConfChangeRemoveNode:
		out.Voters = removeID(out.Voters, ch.Node)
		out.Learners = removeID(out.Learners, ch.Node)
	case numConfChangeTypes:
		return c, fmt.Errorf("raft: unknown configuration change type %d", ch.Type)
	}
	if len(out.Voters) == 0 {
		return c, fmt.Errorf("raft: %s would leave the cluster with no voters, which is a cluster "+
			"that can never elect anybody again", ch.Type)
	}
	return out, nil
}

func contains(xs []NodeID, n NodeID) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func insertID(xs []NodeID, n NodeID) []NodeID {
	if contains(xs, n) {
		return xs
	}
	xs = append(xs, n)
	sortIDs(xs)
	return xs
}

func removeID(xs []NodeID, n NodeID) []NodeID {
	out := xs[:0]
	for _, x := range xs {
		if x != n {
			out = append(out, x)
		}
	}
	return out
}

// sortIDs is an insertion sort. Small sets, and no import for something a
// cluster membership list can do in six lines.
func sortIDs(xs []NodeID) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

// MessageType is a closed enum.
type MessageType uint8

const (
	MsgVote MessageType = iota + 1
	MsgVoteResp
	MsgApp
	MsgAppResp

	// MsgPreVote and MsgPreVoteResp are their own members rather than a flag on
	// MsgVote (D-A2-4). A message whose meaning depends on a boolean is the
	// implicit variant the codec rules refused once already, and it would make
	// the UNGATED case a special case of a gated one -- the exact adjacency
	// where a future change silently starts gating, or stops.
	MsgPreVote
	MsgPreVoteResp

	// MsgSnap carries a state machine snapshot to a follower too far behind for
	// the leader to catch up from its log.
	MsgSnap

	// MsgTimeoutNow tells a target it may campaign immediately, skipping the
	// election timeout. It is leadership transfer's only new message.
	MsgTimeoutNow

	numMessageTypes
)

func (m MessageType) String() string {
	switch m {
	case MsgVote:
		return "vote"
	case MsgVoteResp:
		return "vote-resp"
	case MsgApp:
		return "app"
	case MsgAppResp:
		return "app-resp"
	case MsgPreVote:
		return "pre-vote"
	case MsgPreVoteResp:
		return "pre-vote-resp"
	case MsgSnap:
		return "snap"
	case MsgTimeoutNow:
		return "timeout-now"
	case numMessageTypes:
		return "invalid"
	}
	return "unknown"
}

// Message is the wire form. One struct for every type, fixed fields, because a
// per-type union would need a type switch on the wire and the codec rules
// already refused that once.
type Message struct {
	Type MessageType
	From NodeID
	To   NodeID
	Term Term

	// MsgVote
	LastLogIndex Index
	LastLogTerm  Term

	// MsgVoteResp, MsgAppResp
	Granted bool
	Success bool

	// MsgApp
	PrevLogIndex Index
	PrevLogTerm  Term
	Entries      []Entry
	LeaderCommit Index

	// MsgAppResp
	MatchIndex Index

	// Hint is the follower's last index on a rejected append, so a leader can
	// back up in one round rather than one index per round. It is an
	// optimization and carries no safety weight.
	Hint Index

	// MsgSnap
	SnapIndex Index
	SnapTerm  Term
	SnapData  []byte

	// SnapConf is the snapshot's configuration, encoded. Carried on the wire
	// because a follower installing a snapshot has to learn the membership from
	// the same message: it is discarding the log that would otherwise tell it.
	SnapConf []byte
}

// HardState is the state Raft must have durable before it may act on it.
//
// Term and Vote, and nothing else. Commit is deliberately absent: a commit index
// can be recomputed on restart from the log and the leader's next append, and
// persisting it would mean a node could recover claiming an entry was committed
// on the word of its own memory.
type HardState struct {
	Term Term
	Vote NodeID
}

// PersistMark is an opaque monotone token naming a durability point. Zero means
// "nothing to persist".
//
// Marks are what make persist-before-reply structural rather than conventional.
// A message whose meaning depends on state covered by mark m is not released
// until AckPersisted(m) arrives, so the driver cannot send it early because it
// never has it to send.
type PersistMark uint64

// Snapshot is a state machine snapshot crossing the interface.
//
// raft treats Data as opaque and never stores it: the bytes belong to the state
// machine and to the engine, and a pure state machine that held an unbounded
// payload would be a state machine nobody could compare or assert over
// (D-A2-3). What raft keeps is the metadata.
type Snapshot struct {
	Index Index
	Term  Term
	Data  []byte

	// Conf is the configuration as of Index.
	//
	// A snapshot without it is a state machine that does not know who it is
	// talking to. The active configuration is a function of the log, and a
	// snapshot is precisely the part of the log that no longer exists -- so
	// after compaction there is nowhere else for it to come from. CLAUDE.md's
	// invariant list says it directly: snapshots carry the active config.
	Conf Configuration
}

// Ready is a drain: calling it returns pending outputs and clears them.
//
// **There is no Advance().** Progress is acknowledged per resource --
// AckPersisted(Mark) and AckApplied(index) -- independently, so appends pipeline
// against replication instead of serializing behind one outstanding Ready
// (DESIGN-A0 DR-7).
type Ready struct {
	// --- must be made durable before AckPersisted(Mark) ---

	// HardState is non-nil iff (term, vote) changed.
	HardState *HardState

	// Entries are appended at Entries[0].Index, truncating any conflict.
	Entries []Entry

	// SnapshotTo names followers whose next index is behind this leader's
	// compaction point: they cannot be caught up from the log, so the driver
	// must send each of them the snapshot it holds.
	SnapshotTo []NodeID

	// Snapshot is a state machine snapshot to install, or nil. When it is
	// non-nil the driver must replace its state machine wholesale, and Mark
	// covers that install rather than any log write: a Ready never carries both,
	// so one mark still names exactly one handover.
	Snapshot *Snapshot

	// Mark names this Ready's LOG durability point; zero if nothing to persist.
	Mark PersistMark

	// SnapMark names this Ready's SNAPSHOT durability point, acknowledged
	// through AckSnapshot. It is a separate stream with no ordering against
	// Mark, so the driver must write the snapshot in its own batch and
	// acknowledge it on its own completion.
	SnapMark PersistMark

	// --- safe to act on immediately ---

	// Messages is every message whose preconditions are already satisfied.
	//
	// # Durability gating -- normative
	//
	// This is the interface's central safety claim. **The general rule, from
	// which the cases follow:**
	//
	//	An outbound message is released in Ready.Messages only after every
	//	element of persistent state that the message *attests to* is durable.
	//	If a Step mutates HardState or the log, no message generated by that
	//	Step whose meaning depends on the mutation is released until the
	//	corresponding PersistMark is acknowledged.
	//
	// Enumerated, with the failure each gate prevents:
	//
	//	MsgAppResp (accept)
	//	  gated on: the appended entries AND HardState.Term durable
	//	  without it: Follower acks index i, leader counts it toward a quorum
	//	  and commits; follower crashes, loses i, comes back and is elected
	//	  with a shorter log => committed entry lost. Violates Leader
	//	  Completeness and "committed is forever".
	//
	//	MsgVoteResp (grant)
	//	  gated on: (Term, Vote) durable
	//	  without it: Node grants to A, crashes, forgets, restarts, grants to B
	//	  in the same term => two leaders in one term. Violates Election
	//	  Safety. This is the canonical case.
	//
	//	MsgVoteResp (reject) and any response emitted after a term bump
	//	  gated on: HardState.Term durable
	//	  without it: A response reveals the responder's new term. Forgetting a
	//	  term bump after advertising it lets the node re-participate in a term
	//	  it has already acted in. Cheap to gate; gated.
	//
	//	MsgAppResp following InstallSnapshot
	//	  gated on: snapshot durably installed, including its config and
	//	  applied index
	//	  without it: Node acks the snapshot, crashes before it is durable,
	//	  restarts with an empty/older log while the leader has already
	//	  advanced Next past the snapshot index => silent hole in the log.
	//	  (A2 lands snapshots; the gate is declared here so it cannot be
	//	  forgotten when they do.)
	//
	//	MsgHeartbeatResp
	//	  gated on: HardState.Term durable (only when the heartbeat bumped the
	//	  term)
	//	  without it: Same term-amnesia case as above. (Heartbeats are MsgApp
	//	  with no entries in this implementation; the gate applies identically.)
	//
	//	MsgReadIndexResp (A7)
	//	  gated on: leadership-confirming quorum, *not* durability
	//	  without it: nothing. Read index attests to a commit index, which is
	//	  already durable by the time it is committed. Documented so nobody
	//	  adds a spurious gate and pays latency for nothing.
	//
	// **MsgPreVoteResp is deliberately NOT gated, and that is a correctness
	// argument, not an oversight.** Pre-vote by construction mutates no
	// persistent state: the responder does not advance its term and does not
	// record a vote. A pre-vote grant attests only to "my log is not more up to
	// date than yours and I have not heard from a leader recently" -- both facts
	// about volatile state that are permitted to be forgotten across a crash.
	// Gating it would cost an fsync on the hot path of every election attempt
	// and buy nothing. If a future change makes pre-vote touch HardState, this
	// gate must be reinstated; a test asserts pre-vote handling leaves HardState
	// unchanged so that change cannot pass silently. (Pre-vote landed in A2 and
	// that test is now real rather than hypothetical.)
	//
	// **MsgTimeoutNow is deliberately NOT gated**, on the same footing and with
	// its own argument. It attests to the sender's leadership, which is
	// volatile, and to a term the recipient re-checks for itself before acting.
	// A transfer order a crash forgets is a transfer that does not happen, and a
	// transfer that does not happen is the safe direction: the old leader keeps
	// leading until its own election timeout, which is the state the cluster was
	// already in. Nothing about the order is a promise about persistent state.
	//
	// The two non-gates are listed apart from the enumeration on purpose.
	// tools/gatepin pins the gate SET, and folding a non-gate into it as
	// "gated on: nothing" would grow that set by something that is not a gate --
	// making the next real addition look like more of the same.
	//
	// The consequence worth stating: **the driver has no ordering obligation at
	// all.** It may send Ready.Messages in any order, at any time, before or
	// after it starts the persist, and safety holds. The persist-before-reply
	// sharp edge is discharged inside raft/, where it is unit-testable in
	// isolation, rather than distributed across every caller.
	Messages []Message

	// Committed are entries to apply in order, then AckApplied(lastIndex).
	Committed []Entry
}

// Empty reports whether there is nothing to do.
func (r Ready) Empty() bool {
	return r.HardState == nil && len(r.Entries) == 0 && len(r.Committed) == 0 && len(r.Messages) == 0
}

// Config is one node's construction parameters.
type Config struct {
	ID    NodeID
	Peers []NodeID // including ID, in ascending order

	// ElectionTimeout is the base number of ticks before a follower campaigns.
	// The driver randomizes around it via SetElectionTimeout; a fixed timeout on
	// every node produces perfectly synchronized split votes forever.
	ElectionTimeout int

	// HeartbeatTimeout is ticks between leader heartbeats. It must be well under
	// ElectionTimeout or a healthy leader is deposed by its own silence.
	HeartbeatTimeout int

	// Learners are the peers that start as learners rather than voters. Peers
	// not listed here are voters.
	Learners []NodeID

	// PromotionLag is how many entries behind the leader a learner may be and
	// still be promoted. Zero takes a default; promotion is REFUSED past it
	// rather than queued, because a queued promotion is one whose preconditions
	// were true at some point in the past (DESIGN-A3 §5).
	PromotionLag Index

	// PreVote adds the round that stops a node which can send but not receive
	// from inflating the cluster's term every time it campaigns. Off by default
	// so the ablation can measure the difference rather than assert it.
	PreVote bool
}

// Raft is one node's state machine.
type Raft struct {
	id NodeID

	// peers is every node this replica tracks -- voters and learners -- sorted,
	// and it is what the parallel slices below are indexed by. It is DERIVED
	// from conf and rebuilt whenever conf changes, preserving each node's
	// progress by identity rather than by position (DESIGN-A3 §3).
	peers []NodeID

	// conf is the active configuration: the latest one in this node's log,
	// committed or not. baseConf is the configuration the snapshot carried,
	// which is where a recompute starts from after compaction.
	//
	// Effect-on-append is what makes the overlapping-quorum argument work, and
	// its consequence is that every path which changes the log -- append,
	// truncate, install, restore -- must recompute conf. Not one of them may
	// forget, which is why recomputeConf exists and why nothing sets conf
	// directly.
	conf     Configuration
	baseConf Configuration

	// promotionLag bounds how far behind a learner may be and still be promoted.
	promotionLag Index

	// Persistent, and durable before it is acted upon.
	term Term
	vote NodeID

	// log holds the entries this node still has. It is NOT indexed by log
	// position: log[0] is entry snapIndex+1, and every access goes through pos,
	// at or suffixFrom (D-A2-1).
	log []Entry

	// snapIndex and snapTerm describe the snapshot the log sits on top of.
	// Entries at or below snapIndex are compacted away; snapTerm is the term of
	// entry snapIndex, which the consistency check for the first entry after a
	// snapshot asks for and which the log can no longer answer.
	//
	// # Why an explicit offset and not a dummy entry at position 0
	//
	// etcd/raft keeps a placeholder entry so the slice is never empty and the
	// arithmetic survives unchanged. That is the smallest diff and the shape
	// this repository keeps paying for: a value whose meaning is positional and
	// whose presence is load-bearing. A log index is not a proposal identity
	// (BUG-004); an empty slice is not a durability fact (BUG-005); a
	// placeholder entry is not an entry. Two named fields cannot be misread.
	snapIndex Index
	snapTerm  Term

	// snapMark is the durability point of a snapshot install that has been handed
	// to the driver and not yet acknowledged, and nextSnapMark/persistedSnap are
	// its own monotone counter and watermark.
	//
	// # Why a separate counter and not a separate field on the same one
	//
	// The first attempt gave the snapshot its own FIELD and drew its value from
	// the log's counter. That is not two streams, it is one stream with two
	// names: marks stay monotone, so acknowledging a later log mark implies the
	// snapshot's, and the "second answer" collapses back into the first. Removing
	// the snapshot arm from the gate then changed nothing across 300 seeds, which
	// is how the collapse was found.
	//
	// Two streams means two counters and two watermarks, with NO ordering assumed
	// between them. That is also the honest model of the write path: the snapshot
	// is a separate object with a separate durability point, and inferring its
	// persistence from the log's is the same inference BUG-005 was about.
	snapMark      PersistMark
	nextSnapMark  PersistMark
	persistedSnap PersistMark

	// pendingSnap is the snapshot to hand the driver in the next Ready, and
	// pendingSnapAck is the acknowledgement withheld until the install is
	// durable.
	pendingSnap *Snapshot

	// Volatile.
	role        Role
	commitIndex Index
	appliedIdx  Index
	leader      NodeID

	// Leader bookkeeping, parallel to peers. Slices rather than maps: a map here
	// would be iterated, and map iteration order is the classic determinism leak.
	nextIndex  []Index
	matchIndex []Index

	// votesGranted is parallel to peers.
	votesGranted []bool
	votesDenied  []bool

	// preVote turns on the extra round; preVoting says one is in progress.
	// Pre-vote mutates no persistent state, which is why its responses are the
	// one message class DR-8 leaves ungated.
	preVote    bool
	preVoting  bool
	preGranted []bool

	// leadTransferee is the target of a leadership transfer in progress, and
	// transferElapsed bounds it. A transfer that never completes must not
	// disable the leader forever, so it expires after one election timeout.
	leadTransferee  NodeID
	transferElapsed int

	// snapTo names the followers that need a snapshot rather than an append.
	// raft does not hold the bytes, so it names the targets and the driver, which
	// owns the snapshot in the engine, does the sending (D-A2-3).
	snapTo []NodeID

	electionElapsed  int
	heartbeatElapsed int

	electionTimeout           int
	randomizedElectionTimeout int
	heartbeatTimeout          int

	// pending output, drained by Ready.
	msgs []Message

	// tail records what has been handed over and what is durable. Never
	// inferred from the shape of anything.
	tail logTail

	// markLastIdx is the highest index handed over under the most recently
	// handed mark, and lastHandedMark is that mark.
	//
	// Two scalars, not a table of spans keyed by mark. At most one mark is OPEN
	// -- issued and not yet handed over -- at a time, so there is only ever one
	// coverage window being built; once handed, a mark's coverage is frozen and
	// only the newest one can still be growing. tail.persisted therefore
	// advances on an acknowledgement that reaches lastHandedMark, and lags
	// conservatively otherwise, which costs a message a little extra time in the
	// gated queue and costs safety nothing. A span table is a second bookkeeping
	// structure that has to stay consistent with the log under truncation, which
	// is what broke the first attempt at this.
	markLastIdx    Index
	lastHandedMark PersistMark

	// hardStateDirty is set when (term, vote) changed and has not yet been
	// handed to the driver in a Ready.
	hardStateDirty bool

	// nextMark is the monotone token generator. persisted is the highest mark
	// the driver has acknowledged durable.
	nextMark  PersistMark
	persisted PersistMark

	// gated holds messages withheld until their mark is durable, in generation
	// order so release is deterministic. This queue is where a whole bug class
	// now lives: a message gated on a mark that is never acked is
	// indistinguishable from a message never generated, so AssertQuiescent
	// exists to make that surface as a failure rather than as silence.
	gated []gatedMessage

	// dirtyMark is the mark covering state mutated since the last Ready. Zero
	// when nothing is pending.
	dirtyMark PersistMark

	// markHandedOff records whether the driver has actually been given
	// something to persist under the current mark.
	//
	// It exists because a mark can end up covering nothing. A conflicting append
	// truncates the unstable tail, so entries that opened a mark can be removed
	// before any Ready hands them over; if the hard state did not also change,
	// the driver is handed a Ready with a mark and nothing to write, never
	// persists, never acknowledges, and every message gated on that mark is
	// withheld forever. The cluster stalls and every checker stays green.
	markHandedOff bool
}

// logTail names the states a log suffix can be in, so that "handed over" and
// "durable" cannot be spelled the same way.
//
// # Durability is recorded, never inferred
//
// This replaced an `unstable []Entry` slice whose emptiness was read as "all
// durable". Ready() clears that slice on HANDOVER, so between Ready and
// AckPersisted the state machine believed everything it had handed the driver
// was already on disk, and released an append response acking entries the driver
// had not yet written. BUG-005.
//
// The error is not the arithmetic, it is the shape: **a fact inferred from an
// incidental property is a fact that silently becomes wrong the moment the
// property changes for an unrelated reason.** The slice emptied for a reason
// that had nothing to do with durability. Identifying a proposal by its log
// index was the same error one subsystem over (BUG-004).
//
// So both facts are recorded, in fields with different names, and neither is
// derived from the shape of anything else.
type logTail struct {
	// persisted is the highest index the driver has ACKNOWLEDGED durable. It
	// moves only in AckPersisted.
	persisted Index

	// handed is the highest index given to the driver. It moves in Ready. A
	// handed entry is not a durable entry and the two are never compared as if
	// they were.
	handed Index
}

// gatedMessage is an outbound message and the durability points it attests to.
//
// Two, not one, and not "the later of the two": with two independent streams
// there is no ordering between the marks, so a message that attests to state in
// both waits for BOTH. "The later of the two marks" was the right rule while
// there was a single ordered stream, and it is the special case of this one.
type gatedMessage struct {
	msg      Message
	mark     PersistMark // log stream; zero means no constraint
	snapMark PersistMark // snapshot stream; zero means no constraint
}

// ErrNotLeader is returned by Propose on a node that is not the leader.
var ErrNotLeader = errors.New("raft: not leader")

// ErrTransferInProgress is returned by Propose while leadership is being handed
// over. It is a refusal, not a failure: the client retries and reaches the new
// leader.
var ErrTransferInProgress = errors.New("raft: leadership transfer in progress")

// New builds a node.
func New(cfg Config) (*Raft, error) {
	if cfg.ID == 0 {
		return nil, fmt.Errorf("raft: node id 0 is reserved for \"no node\"")
	}
	if len(cfg.Peers) == 0 {
		return nil, fmt.Errorf("raft: node %d has no peer set", cfg.ID)
	}
	found := false
	for i, p := range cfg.Peers {
		if p == 0 {
			return nil, fmt.Errorf("raft: peer set contains node id 0")
		}
		if i > 0 && cfg.Peers[i-1] >= p {
			return nil, fmt.Errorf("raft: peer set is not sorted and deduplicated: %v", cfg.Peers)
		}
		if p == cfg.ID {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("raft: node %d is not in its own peer set %v", cfg.ID, cfg.Peers)
	}
	if cfg.ElectionTimeout <= 0 || cfg.HeartbeatTimeout <= 0 {
		return nil, fmt.Errorf("raft: election and heartbeat timeouts must be positive, got %d and %d",
			cfg.ElectionTimeout, cfg.HeartbeatTimeout)
	}
	if cfg.HeartbeatTimeout >= cfg.ElectionTimeout {
		return nil, fmt.Errorf("raft: heartbeat timeout %d is not under election timeout %d, so a healthy leader is deposed by its own silence",
			cfg.HeartbeatTimeout, cfg.ElectionTimeout)
	}

	conf := Configuration{}
	for _, p := range cfg.Peers {
		if contains(cfg.Learners, p) {
			conf.Learners = insertID(conf.Learners, p)
		} else {
			conf.Voters = insertID(conf.Voters, p)
		}
	}
	if len(conf.Voters) == 0 {
		return nil, fmt.Errorf("raft: node %d was configured with no voters", cfg.ID)
	}
	lag := cfg.PromotionLag
	if lag == 0 {
		lag = defaultPromotionLag
	}
	peers := conf.Members()
	r := &Raft{
		id: cfg.ID, peers: peers, conf: conf, baseConf: conf.Clone(), promotionLag: lag,
		role:                      RoleFollower,
		electionTimeout:           cfg.ElectionTimeout,
		randomizedElectionTimeout: cfg.ElectionTimeout,
		heartbeatTimeout:          cfg.HeartbeatTimeout,
		nextIndex:                 make([]Index, len(peers)),
		matchIndex:                make([]Index, len(peers)),
		votesGranted:              make([]bool, len(peers)),
		votesDenied:               make([]bool, len(peers)),
		preGranted:                make([]bool, len(peers)),
		preVote:                   cfg.PreVote,
	}
	return r, nil
}

// Restore rebuilds a node from what an engine gave back after a crash. It is the
// real recovery path: every restart in the simulator goes through it, so the
// path is exercised by every crash the harness injects rather than by a test.
func Restore(cfg Config, hs HardState, snap SnapshotMeta, entries []Entry) (*Raft, error) {
	r, err := New(cfg)
	if err != nil {
		return nil, err
	}
	r.term, r.vote = hs.Term, hs.Vote
	r.snapIndex, r.snapTerm = snap.Index, snap.Term
	if len(snap.Conf.Voters) > 0 {
		// The snapshot's configuration is the base a recovering node rebuilds
		// from. Without it the node would recover into whatever its Config said
		// at construction, which is the membership it had when it was first
		// started -- possibly several changes ago.
		r.baseConf = snap.Conf.Clone()
	}
	r.log = append(r.log, entries...)
	for i, e := range r.log {
		if e.Index != snap.Index+Index(i)+1 {
			return nil, fmt.Errorf("raft: recovered log entry %d claims index %d behind a snapshot at %d; the log is not a gapless prefix", i, e.Index, snap.Index)
		}
	}

	// A snapshot is taken from an APPLIED prefix, so recovering one is
	// recovering the knowledge that everything through its index was committed
	// and applied. Leaving commit and applied at zero would have the node
	// believe nothing is committed while its state machine sits at snap.Index,
	// and it would then try to apply entries it has already applied.
	r.commitIndex = snap.Index
	r.appliedIdx = snap.Index
	r.recomputeConf()
	// Everything the engine gave back is durable by definition, and it is
	// RECORDED as such here. Leaving the watermark at zero would leave a
	// recovered node believing nothing it holds is durable, which is not a
	// conservative error: markFor would then hand back the open mark for an
	// index that needs no gate, and the recovered log would be handed to the
	// driver a second time under a mark nobody ever acknowledges.
	last := r.lastIndex()
	r.tail.persisted = last
	r.tail.handed = last
	r.markLastIdx = last
	return r, nil
}

// SnapshotMeta is the snapshot a recovering node found on disk. Its zero value
// means there was none, which is the ordinary case for a young node.
type SnapshotMeta struct {
	Index Index
	Term  Term

	// Conf is the configuration as of Index. A recovering node reads its
	// membership from here before it can do anything at all.
	Conf Configuration
}

// ID, Role and Term exist for the driver and for logging. **No oracle may call
// them**: oracle independence means the checkers read the Ready stream and what
// was persisted, never the node (DESIGN-A1 §0). A driver needs Role to decide
// whether to accept a client proposal, which is not a safety judgement.
func (r *Raft) ID() NodeID { return r.id }

// Term reports the current term, for the driver's routing and for logging. No
// oracle may call it (DESIGN-A1 §0); the driver needs it to stamp a snapshot
// message it sends on raft's behalf, which is not a safety judgement.
func (r *Raft) Term() Term { return r.term }

// SnapshotIndex is the compaction point, for the driver's snapshot bookkeeping.
func (r *Raft) SnapshotIndex() Index { return r.snapIndex }

// AppliedIndex is how far the state machine has consumed, for the driver's
// snapshot trigger.
func (r *Raft) AppliedIndex() Index { return r.appliedIdx }

// Role reports the current role, for the driver's routing only.
func (r *Raft) Role() Role { return r.role }

// SetElectionTimeout installs this node's randomized timeout.
//
// Randomness cannot come from inside a pure state machine, so it comes from the
// driver, which derives it from a plan-carried PRF. No live draw, and the value
// is reproducible from the plan alone.
func (r *Raft) SetElectionTimeout(ticks int) {
	if ticks > 0 {
		r.randomizedElectionTimeout = ticks
	}
}

// Tick advances the logical clock by one.
func (r *Raft) Tick() {
	switch r.role {
	case RoleFollower, RoleCandidate:
		r.electionElapsed++
		if r.electionElapsed >= r.randomizedElectionTimeout && r.conf.IsVoter(r.id) {
			// A learner never campaigns. It is counted in no quorum, so an
			// election it started could not finish, and the term it burned would
			// depose a healthy leader for nothing.
			r.campaign()
		}
	case RoleLeader:
		if r.leadTransferee != 0 {
			r.transferElapsed++
			if r.transferElapsed >= r.electionTimeout {
				// A transfer that never completes must not disable the leader
				// forever. Proposals are refused while one is pending, so an
				// abandoned transfer would be an outage with no error anywhere.
				r.leadTransferee = 0
			}
		}
		r.heartbeatElapsed++
		if r.heartbeatElapsed >= r.heartbeatTimeout {
			r.heartbeatElapsed = 0
			r.broadcastAppend()
		}
	case numRoles:
	}
}

// Propose appends a client command on the leader.
//
// The signature is DESIGN-A0 D5's, and the identifier is the point: a caller
// tracks its proposal by the id it supplied, never by the index the entry
// happened to receive, because a later leader may put a different command at
// that index. Returning the index was the shape that produced BUG-004.
func (r *Raft) Propose(id ProposalID, data []byte) error {
	if id.Zero() {
		return fmt.Errorf("raft: a proposal needs an identifier; the zero value is refused so a " +
			"caller cannot fall back to matching on log index, which is not a proposal identity")
	}
	if r.role != RoleLeader {
		return ErrNotLeader
	}
	if r.leadTransferee != 0 {
		// Accepting a proposal now would append an entry the transferee does not
		// have, which is the one thing that can make a transfer fail to complete.
		return ErrTransferInProgress
	}
	e := Entry{Type: EntryNormal, Term: r.term, Index: r.lastIndex() + 1, ID: id, Data: append([]byte(nil), data...)}
	r.appendEntries(e)
	// The leader's own match index is NOT advanced here. A leader counts its own
	// replication only once the entry is durable, and AckPersisted is where that
	// happens; setting it on append would count a copy the leader has not
	// written, which is the same ack-before-sync this package gates every
	// outbound message against.
	r.broadcastAppend()
	return nil
}

// ErrConfChangeInFlight is returned while an earlier configuration change has
// not committed. Three simultaneously live configurations have no overlap
// guarantee, which is DESIGN-A3 §4's reasoning one level up.
var ErrConfChangeInFlight = errors.New("raft: a configuration change is already in flight")

// ErrLearnerLagging is returned when a learner is too far behind to promote.
var ErrLearnerLagging = errors.New("raft: learner is too far behind to promote")

// ProposeConfChange appends a configuration change. The signature is DESIGN-A0
// D5's, frozen.
//
// A3 accepts exactly one simple change and refuses the joint transitions BY
// NAME, citing Amendment A6, so the cut is visible where somebody tries to use
// it rather than implied by an absence (DESIGN-A3 §2).
func (r *Raft) ProposeConfChange(id ProposalID, cc ConfChangeV2) error {
	if id.Zero() {
		return fmt.Errorf("raft: a configuration change needs an identifier; the zero value is refused")
	}
	if r.role != RoleLeader {
		return ErrNotLeader
	}
	if r.leadTransferee != 0 {
		return ErrTransferInProgress
	}
	if cc.Transition != ConfChangeSimple {
		return fmt.Errorf("raft: %s is a joint-consensus transition, which Amendment A6 moved to "+
			"STRETCH.md. v1 changes one server at a time, which is safe without joint consensus by "+
			"the overlapping-quorum argument in DESIGN-A3 §4", cc.Transition)
	}
	if len(cc.Changes) != 1 {
		return fmt.Errorf("raft: a v1 configuration change carries exactly one server, got %d. Two "+
			"at once is the case the overlap argument does not cover, which is what joint consensus "+
			"exists for", len(cc.Changes))
	}
	if last := r.lastConfIndex(); last > r.commitIndex {
		return ErrConfChangeInFlight
	}

	ch := cc.Changes[0]
	if ch.Type == ConfChangeAddVoter && r.conf.IsLearner(ch.Node) {
		if i := r.peerIdx(ch.Node); i >= 0 {
			if gap := r.lastIndex() - r.matchIndex[i]; gap > r.promotionLag {
				return fmt.Errorf("%w: node %d is %d entries behind, bound is %d. Promoting it now "+
					"raises the quorum while it can contribute nothing, so a cluster that tolerated "+
					"one failure would tolerate none until it caught up",
					ErrLearnerLagging, ch.Node, gap, r.promotionLag)
			}
		}
	}
	if _, err := r.conf.apply(ch); err != nil {
		return err
	}

	e := Entry{
		Type: EntryConfChange, Term: r.term, Index: r.lastIndex() + 1,
		ID: id, Data: EncodeConfChange(cc),
	}
	r.appendEntries(e)
	r.broadcastAppend()
	return nil
}

// lastConfIndex is the index of the newest configuration entry still in the log,
// or zero. Anything the snapshot covers is committed by construction.
func (r *Raft) lastConfIndex() Index {
	for i := len(r.log) - 1; i >= 0; i-- {
		if r.log[i].Type == EntryConfChange {
			return r.log[i].Index
		}
	}
	return 0
}

// Step feeds one message in.
func (r *Raft) Step(m Message) error {
	if m.To != r.id {
		return fmt.Errorf("raft: node %d received a message addressed to %d", r.id, m.To)
	}
	if m.Type >= numMessageTypes || m.Type == 0 {
		return fmt.Errorf("raft: unknown message type %d", m.Type)
	}

	// A higher term always wins and always demotes. This is the rule that makes
	// stale leaders harmless, and it runs before any type-specific handling.
	//
	// **Pre-vote is exempt, and that exemption is the whole feature.** A
	// MsgPreVote carries the term its sender WOULD use, not one it has adopted;
	// treating it as a real term is exactly the disruption pre-vote exists to
	// prevent, and would make the round worse than useless. A MsgPreVoteResp
	// carries the responder's own term and is handled where it can be read as
	// evidence rather than as an order.
	if m.Term > r.term && m.Type != MsgPreVote && m.Type != MsgPreVoteResp {
		vote := NodeID(0)
		r.becomeFollower(m.Term, vote)
	}

	switch m.Type {
	case MsgVote:
		r.stepVote(m)
	case MsgVoteResp:
		r.stepVoteResp(m)
	case MsgApp:
		r.stepApp(m)
	case MsgAppResp:
		r.stepAppResp(m)
	case MsgPreVote:
		r.stepPreVote(m)
	case MsgPreVoteResp:
		r.stepPreVoteResp(m)
	case MsgSnap:
		r.stepSnap(m)
	case MsgTimeoutNow:
		r.stepTimeoutNow(m)
	case numMessageTypes:
	}
	return nil
}

// stepPreVote answers a pre-vote request. It mutates nothing persistent, which
// is why the reply goes out through send rather than sendGated: the facts it
// attests to -- "your log is not behind mine" and "I have not heard from a
// leader recently" -- are volatile, and a crash is permitted to forget them.
func (r *Raft) stepPreVote(m Message) {
	grant := false
	switch {
	case m.Term <= r.term:
		// The would-be term is not ahead of ours, so there is nothing to gain.
	case r.leader != 0 && r.electionElapsed < r.randomizedElectionTimeout:
		// We have heard from a leader inside one election timeout. This is the
		// refusal that stops a node which can send but not receive from
		// disrupting a healthy cluster, and it is the entire point of the round.
	case !r.logIsUpToDate(m.LastLogIndex, m.LastLogTerm):
		// It could not win the real election either.
	default:
		grant = true
	}
	// # The response carries the PROPOSED term, never the responder's own
	//
	// This is the one place A2 changed a message rather than a checker, and the
	// persist-before-reply oracle is what forced it. Replying with r.term made
	// the response a claim about the responder's persistent state -- "I am in
	// term T" -- while the round is supposed to be ungated, so a node whose term
	// bump was still in flight advertised a term it had not written. 95 of 200
	// seeds caught it.
	//
	// Echoing the proposed term instead makes the response say only "yes, if you
	// campaigned for that term, I would vote for you", which is a statement
	// about volatile facts and nothing else. DR-8's non-gate argument then holds
	// exactly as written rather than nearly.
	//
	// The cost is real and small: a candidate that is behind no longer learns the
	// true term from a pre-vote rejection. It learns it from the next MsgApp or
	// MsgVoteResp a real leader sends, one round later. That is liveness, and
	// buying it with a durability claim on the hot path of every election attempt
	// is the trade DR-8 already refused.
	r.send(Message{Type: MsgPreVoteResp, From: r.id, To: m.From, Term: m.Term, Granted: grant})
}

// stepPreVoteResp counts pre-votes and, on a quorum, campaigns for real.
func (r *Raft) stepPreVoteResp(m Message) {
	if !r.preVoting {
		return
	}
	if m.Term != r.term+1 {
		// Not an answer to the round we are running. Pre-vote responses echo the
		// proposed term, so this is stale rather than informative -- and it is
		// deliberately NOT read as "somebody is in a higher term", because the
		// response no longer carries the responder's term at all.
		return
	}
	i := r.peerIdx(m.From)
	if i < 0 || !m.Granted {
		return
	}
	r.preGranted[i] = true
	if r.countVoters(r.preGranted) >= r.quorum() {
		r.preVoting = false
		r.forceCampaign()
	}
}

// stepTimeoutNow is leadership transfer's receiving half: the current leader has
// said this node may campaign now, so it does, skipping both the election
// timeout and the pre-vote round.
//
// Skipping pre-vote is not a hole in pre-vote. The round exists to gather
// evidence that a campaign will not disrupt a healthy cluster, and being told to
// campaign BY the current leader is stronger evidence than any quorum of peers
// could give.
func (r *Raft) stepTimeoutNow(m Message) {
	if m.Term < r.term || r.role == RoleLeader {
		return
	}
	r.forceCampaign()
}

// stepSnap installs a snapshot from a leader that has compacted past what this
// node needs.
func (r *Raft) stepSnap(m Message) {
	if m.Term < r.term {
		// GATE: any response emitted after a term bump, on HardState.Term.
		r.sendGated(Message{Type: MsgAppResp, From: r.id, To: m.From, Term: r.term, Success: false})
		return
	}
	r.becomeFollower(m.Term, r.vote)
	r.leader = m.From
	r.electionElapsed = 0

	if m.SnapIndex <= r.commitIndex {
		// Everything this snapshot covers is already committed here, so
		// installing it would replace a longer state with a shorter one.
		// GATE: MsgAppResp (accept), on whatever covers the index being acked.
		last := r.lastIndex()
		lm, sm := r.marksFor(last)
		if r.dirtyMark > lm {
			lm = r.dirtyMark
		}
		r.sendGatedOn(lm, sm, Message{
			Type: MsgAppResp, From: r.id, To: m.From, Term: r.term,
			Success: true, MatchIndex: last,
		})
		return
	}

	// Install. The log is replaced wholesale, so every withheld append response
	// attesting to it has become a lie -- the same reasoning as truncateFrom,
	// and for the same reason: releasing one later would tell a leader this node
	// holds entries it has just discarded.
	kept := r.gated[:0]
	for _, g := range r.gated {
		if g.msg.Type == MsgAppResp && g.msg.Success {
			continue
		}
		kept = append(kept, g)
	}
	r.gated = kept

	conf, ok := DecodeConfiguration(m.SnapConf)
	if !ok {
		// A snapshot whose configuration does not decode is a snapshot that
		// cannot say who the cluster is. Refusing it leaves this node behind,
		// which is recoverable; installing it leaves this node guessing, which
		// is not.
		r.sendGated(Message{Type: MsgAppResp, From: r.id, To: m.From, Term: r.term, Success: false})
		return
	}

	r.log = nil
	r.baseConf = conf
	r.recomputeConf()
	r.snapIndex, r.snapTerm = m.SnapIndex, m.SnapTerm
	r.commitIndex = m.SnapIndex
	r.appliedIdx = m.SnapIndex
	r.tail.persisted = m.SnapIndex
	r.tail.handed = m.SnapIndex
	r.markLastIdx = m.SnapIndex

	r.dirty()
	r.nextSnapMark++
	r.snapMark = r.nextSnapMark
	r.pendingSnap = &Snapshot{Index: m.SnapIndex, Term: m.SnapTerm, Data: m.SnapData, Conf: conf}

	// GATE: MsgAppResp following InstallSnapshot, on the snapshot durably
	// installed. Without it the node acks, crashes before the install is
	// durable, and comes back with an older log while the leader has advanced
	// Next past the snapshot index -- a silent hole. DR-8, enumerated before
	// raft/ existed.
	// Both streams: the entries it acks are in the snapshot, and the term it
	// carries is in the hard state this Step also dirtied.
	r.sendGatedOn(r.dirtyMark, r.snapMark, Message{
		Type: MsgAppResp, From: r.id, To: m.From, Term: r.term,
		Success: true, MatchIndex: m.SnapIndex,
	})
}

// sendSnapshot names a follower the driver must send the snapshot to. raft holds
// no bytes to send, so it names the target instead (D-A2-3).
func (r *Raft) sendSnapshot(to NodeID) {
	for _, p := range r.snapTo {
		if p == to {
			return
		}
	}
	r.snapTo = append(r.snapTo, to)

	// Assume it lands, so the leader does not re-send on every heartbeat. A
	// reject backs this up again, which is the same recovery an ordinary append
	// gets.
	if i := r.peerIdx(to); i >= 0 {
		r.nextIndex[i] = r.snapIndex + 1
	}
}

// Compact discards the log prefix through index, which the driver calls once its
// own snapshot at that index is durable. It returns the term of the compacted
// index, which is the metadata the driver must store beside the snapshot.
//
// It REFUSES to compact past what has been applied, and past what is durable.
// Discarding a prefix the state machine has not consumed, or one the engine has
// not written, is unrecoverable in a way no later check can undo, so it is a
// refusal rather than an invariant.
func (r *Raft) Compact(index Index) (Term, Configuration, error) {
	if index <= r.snapIndex {
		return r.snapTerm, r.baseConf.Clone(), nil
	}
	if index > r.appliedIdx {
		return 0, Configuration{}, fmt.Errorf("raft: node %d cannot compact through %d with only %d applied; that "+
			"prefix has not reached the state machine yet", r.id, index, r.appliedIdx)
	}
	if index > r.tail.persisted {
		return 0, Configuration{}, fmt.Errorf("raft: node %d cannot compact through %d with only %d durable; the "+
			"prefix would be gone from memory and never have been on disk", r.id, index, r.tail.persisted)
	}
	e, ok := r.at(index)
	if !ok {
		return 0, Configuration{}, fmt.Errorf("raft: node %d cannot compact through %d, which its log does not hold", r.id, index)
	}

	// The configuration AS OF index, which is not the active one: entries above
	// the compaction point may have changed it again, and a snapshot that
	// carried the newer configuration would describe a cluster its own state
	// machine has not caught up to.
	conf := r.baseConf.Clone()
	for _, x := range r.log {
		if x.Index > index {
			break
		}
		if x.Type != EntryConfChange {
			continue
		}
		if cc, ok := DecodeConfChange(x.Data); ok {
			for _, ch := range cc.Changes {
				if next, err := conf.apply(ch); err == nil {
					conf = next
				}
			}
		}
	}

	p, _ := r.pos(index)
	r.log = append([]Entry(nil), r.log[p+1:]...)
	r.snapIndex, r.snapTerm = index, e.Term
	r.baseConf = conf.Clone()
	r.recomputeConf()
	return e.Term, conf, nil
}

// TransferLeadership hands leadership to target without waiting out an election
// timeout. A2 ships the mechanism; choosing when to use it is A4's.
//
// # Why this returns nothing
//
// DESIGN-A0 D5 froze the signature without an error, and conforming to a frozen
// interface is not negotiable -- twice in A1 the divergence WAS the defect. The
// consequence has to be lived with honestly, so the two failure kinds are split:
//
//	not the leader, a transfer already pending, or the target no longer in the
//	configuration -- runtime conditions the caller cannot predict. A no-op.
//	target is self -- a caller BUG, which a silent no-op would hide until
//	somebody wondered why leadership never moved. It panics.
//
// # "not a peer" moved sides at A3, and that is a real correction
//
// A2 classified an unknown target as a caller bug, because membership was fixed
// and a caller naming a non-member had made a mistake. A3 makes membership
// change under the caller's feet: a node scheduled for a transfer can be removed
// from the configuration before the order is issued, and nothing the caller can
// check would have told it. It is now a runtime condition, and it fired on the
// first sweep after membership churn landed.
func (r *Raft) TransferLeadership(target NodeID) {
	if target == r.id {
		panic(fmt.Sprintf("raft: node %d was asked to transfer leadership to itself", r.id))
	}
	i := r.peerIdx(target)
	if i < 0 || !r.conf.IsVoter(target) {
		// Not in the configuration, or a learner: a learner cannot win an
		// election, so ordering it to campaign would burn a term for nothing.
		return
	}
	if r.role != RoleLeader || r.leadTransferee != 0 {
		return
	}
	r.leadTransferee = target
	r.transferElapsed = 0
	if r.matchIndex[i] >= r.lastIndex() {
		r.send(Message{Type: MsgTimeoutNow, From: r.id, To: target, Term: r.term})
		return
	}
	// Behind: catch it up first, and send the order when it reports caught up.
	r.sendAppend(target)
}

// Campaign starts an election immediately, without waiting for the timeout.
//
// It is the frozen D5 entry point for a driver that has decided this node should
// try now -- and it goes through the ordinary campaign path, so pre-vote still
// applies. Only MsgTimeoutNow skips the round, because only that carries the
// current leader's say-so.
func (r *Raft) Campaign() error {
	if r.role == RoleLeader {
		return fmt.Errorf("raft: node %d is already the leader", r.id)
	}
	r.campaign()
	return nil
}

// Status is a snapshot of this node's position, for the driver and for logging.
//
// **No oracle may call it** (DESIGN-A1 §0). It is node-reported state, and the
// provenance rule is not that such state is forbidden -- it is that it must not
// be an input to a verdict that can come out green. Nothing here feeds a ledger.
type Status struct {
	ID      NodeID
	Term    Term
	Vote    NodeID
	Role    Role
	Leader  NodeID
	Commit  Index
	Applied Index
	Snap    Index
	Last    Index

	// Transferee is the target of a leadership transfer in progress, or zero.
	Transferee NodeID
}

// Status reports this node's position.
func (r *Raft) Status() Status {
	return Status{
		ID: r.id, Term: r.term, Vote: r.vote, Role: r.role, Leader: r.leader,
		Commit: r.commitIndex, Applied: r.appliedIdx, Snap: r.snapIndex,
		Last: r.lastIndex(), Transferee: r.leadTransferee,
	}
}

// LeadTransferee is the target of a transfer in progress, or zero. For the
// driver's routing, not for any oracle.
func (r *Raft) LeadTransferee() NodeID { return r.leadTransferee }

func (r *Raft) stepVote(m Message) {
	grant := false
	switch {
	case m.Term < r.term:
		// Stale candidate. Reply with our term so it steps down.
	case r.vote != 0 && r.vote != m.From:
		// Already voted this term, for somebody else. One vote per term is the
		// property that makes election safety hold at all.
	case !r.logIsUpToDate(m.LastLogIndex, m.LastLogTerm):
		// A candidate whose log is behind must not win, or a committed entry
		// could be overwritten -- leader completeness depends on this test.
	default:
		grant = true
		r.setVote(m.From)
		// Granting resets the election timer: a node that just endorsed somebody
		// should not immediately campaign against them.
		r.electionElapsed = 0
	}
	// GATE: MsgVoteResp (grant) on (Term, Vote) durable -- the canonical case,
	// two leaders in one term. MsgVoteResp (reject) and any response after a
	// term bump on HardState.Term durable. Both are discharged by sendGated,
	// which withholds against whatever this Step made dirty.
	r.sendGated(Message{Type: MsgVoteResp, From: r.id, To: m.From, Term: r.term, Granted: grant})
}

func (r *Raft) stepVoteResp(m Message) {
	if r.role != RoleCandidate || m.Term != r.term {
		return
	}
	i := r.peerIdx(m.From)
	if i < 0 {
		return
	}
	if m.Granted {
		r.votesGranted[i] = true
	} else {
		r.votesDenied[i] = true
	}
	switch {
	case r.countVoters(r.votesGranted) >= r.quorum():
		r.becomeLeader()
	case r.countVoters(r.votesDenied) >= r.quorum():
		// The election is lost and cannot be won; wait for the timeout rather
		// than campaigning again immediately, which would spin the term.
		r.becomeFollower(r.term, r.vote)
	}
}

func (r *Raft) stepApp(m Message) {
	if m.Term < r.term {
		// GATE: any response emitted after a term bump, on HardState.Term.
		r.sendGated(Message{Type: MsgAppResp, From: r.id, To: m.From, Term: r.term, Success: false})
		return
	}
	// A valid append from the current term means an election is settled.
	r.becomeFollower(m.Term, r.vote)
	r.leader = m.From
	r.electionElapsed = 0

	if !r.matches(m.PrevLogIndex, m.PrevLogTerm) {
		// GATE: as above -- a reject still reveals this node's term.
		r.sendGated(Message{
			Type: MsgAppResp, From: r.id, To: m.From, Term: r.term,
			Success: false, Hint: r.lastIndex(),
		})
		return
	}

	// Append, truncating any conflicting suffix. An entry already present with
	// the same term is left alone, so a duplicated append does not rewrite the
	// log and does not un-persist anything.
	for _, e := range m.Entries {
		if e.Index <= r.lastIndex() {
			if r.termAt(e.Index) == e.Term {
				continue
			}
			r.truncateFrom(e.Index)
		}
		r.appendEntries(e)
	}

	last := m.PrevLogIndex + Index(len(m.Entries))
	if m.LeaderCommit > r.commitIndex {
		r.commitIndex = min(m.LeaderCommit, last)
	}
	// GATE: MsgAppResp (accept) on the appended entries AND HardState.Term
	// durable. Without it a follower acks index i, the leader commits on that
	// ack, the follower crashes and loses i, and a committed entry is lost.
	//
	// **The gate is the LATER of two marks, not either one alone.** An accept
	// attests to two different pieces of persistent state and DR-8 enumerates
	// them as separate gates:
	//
	//	the entries through `last`  -> markFor(last)
	//	this responder's term       -> whatever this Step dirtied
	//
	// Gating on the term alone releases an ack for entries still in flight: a
	// duplicate append adds no entries and so dirties nothing, while the
	// entries it acks may not be durable. That was BUG-005, index 15 acked with
	// 5 durable. Gating on the index alone releases a response that reveals a
	// term bump that is not durable yet -- the same amnesia MsgVoteResp is
	// gated against, and the first attempt at this fix regressed to 24
	// violations by making exactly that trade.
	//
	// Taking the later of the two is the only choice that discharges both.
	lm, sm := r.marksFor(last)
	if r.dirtyMark > lm {
		lm = r.dirtyMark
	}
	r.sendGatedOn(lm, sm, Message{
		Type: MsgAppResp, From: r.id, To: m.From, Term: r.term,
		Success: true, MatchIndex: last,
	})
}

func (r *Raft) stepAppResp(m Message) {
	if r.role != RoleLeader || m.Term != r.term {
		return
	}
	i := r.peerIdx(m.From)
	if i < 0 {
		return
	}
	if !m.Success {
		// Back up. The hint collapses the common case to one round trip.
		next := r.nextIndex[i]
		if m.Hint+1 < next {
			next = m.Hint + 1
		} else if next > 1 {
			next--
		}
		if next < 1 {
			next = 1
		}
		r.nextIndex[i] = next
		r.sendAppend(m.From)
		return
	}
	if m.MatchIndex > r.matchIndex[i] {
		r.matchIndex[i] = m.MatchIndex
	}
	if r.matchIndex[i]+1 > r.nextIndex[i] {
		r.nextIndex[i] = r.matchIndex[i] + 1
	}
	if r.leadTransferee == m.From && r.matchIndex[i] >= r.lastIndex() {
		// The transferee has everything this leader has, so it can win without
		// losing anything. Now it may campaign.
		r.send(Message{Type: MsgTimeoutNow, From: r.id, To: m.From, Term: r.term})
	}
	r.maybeCommit()
}

// maybeCommit advances the commit index by counting matches.
//
// # The figure-8 rule
//
// Only entries from the CURRENT term are committed by counting. An entry from an
// earlier term that happens to be replicated on a majority is not committed on
// that basis, because a later leader with a shorter log could still overwrite
// it. It commits indirectly, when an entry from the current term above it does.
//
// This is the single subtlest rule in Raft and the one figure 8 of the paper
// exists to illustrate. Removing the term check makes almost every test pass.
func (r *Raft) maybeCommit() {
	for n := r.lastIndex(); n > r.commitIndex; n-- {
		if r.termAt(n) != r.term {
			continue
		}
		cnt := 0
		for i, p := range r.peers {
			if r.conf.IsVoter(p) && r.matchIndex[i] >= n {
				cnt++
			}
		}
		if cnt >= r.quorum() {
			r.commitIndex = n
			r.stepDownIfRemoved()
			return
		}
	}
}

// campaign is what an election timeout produces. With pre-vote on it asks first
// and only spends a term if the answer is yes.
// stepDownIfRemoved makes a leader stand down once its own removal is committed.
//
// It keeps leading until then, deliberately. Effect-on-append means it stopped
// being a voter the moment it appended the change, so it no longer counts itself
// toward the quorum -- but somebody has to replicate the entry that removes it,
// and the only node that can is the one being removed.
func (r *Raft) stepDownIfRemoved() {
	if r.role != RoleLeader || r.conf.IsVoter(r.id) {
		return
	}
	if last := r.lastConfIndex(); last != 0 && last > r.commitIndex {
		return
	}
	r.becomeFollower(r.term, r.vote)
	r.leader = 0
}

func (r *Raft) campaign() {
	if r.preVote {
		r.preCampaign()
		return
	}
	r.forceCampaign()
}

// preCampaign asks the cluster whether a real campaign would succeed, WITHOUT
// advancing this node's term or recording a vote.
//
// # The single cut, which is why this exists
//
// A single directed cut leaves a node that can send but not receive. It never
// hears a heartbeat, so it campaigns; its vote request carries a term above
// everyone else's, so the whole cluster steps down and re-elects; it still hears
// nothing, so it campaigns again one term higher. A symmetric cut cannot produce
// this, because a cleanly isolated node's requests never arrive. DESIGN-A0.7
// blessed directed partitions with a forward binding for exactly this, and A2
// spends it.
//
// The request carries the term this node WOULD use. Nobody adopts it, which is
// what makes the round free.
func (r *Raft) preCampaign() {
	r.preVoting = true
	r.electionElapsed = 0
	for i := range r.preGranted {
		r.preGranted[i] = false
	}
	r.preGranted[r.peerIdx(r.id)] = true

	if r.quorum() == 1 {
		r.preVoting = false
		r.forceCampaign()
		return
	}
	for _, p := range r.peers {
		if p == r.id {
			continue
		}
		// Ungated on purpose, and DR-8 carries the argument: a pre-vote mutates
		// no persistent state, so there is nothing for a crash to forget and
		// nothing for the message to outrun.
		r.send(Message{
			Type: MsgPreVote, From: r.id, To: p, Term: r.term + 1,
			LastLogIndex: r.lastIndex(), LastLogTerm: r.lastTerm(),
		})
	}
}

func (r *Raft) forceCampaign() {
	r.preVoting = false
	r.role = RoleCandidate
	r.setTermAndVote(r.term+1, r.id)
	r.leader = 0
	r.electionElapsed = 0
	for i := range r.votesGranted {
		r.votesGranted[i] = false
		r.votesDenied[i] = false
	}
	r.votesGranted[r.peerIdx(r.id)] = true

	if r.quorum() == 1 {
		r.becomeLeader()
		return
	}
	for _, p := range r.peers {
		if p == r.id {
			continue
		}
		// GATE: a vote request advertises this node's new term and its vote for
		// itself, so it is a response-after-a-term-bump in all but name.
		r.sendGated(Message{
			Type: MsgVote, From: r.id, To: p, Term: r.term,
			LastLogIndex: r.lastIndex(), LastLogTerm: r.lastTerm(),
		})
	}
}

func (r *Raft) becomeFollower(term Term, vote NodeID) {
	if term > r.term {
		r.setTermAndVote(term, vote)
	}
	r.role = RoleFollower
	r.heartbeatElapsed = 0
	r.leadTransferee = 0
}

func (r *Raft) becomeLeader() {
	r.role = RoleLeader
	r.leader = r.id
	r.heartbeatElapsed = 0
	r.leadTransferee = 0
	r.preVoting = false
	last := r.lastIndex()
	for i := range r.peers {
		r.nextIndex[i] = last + 1
		r.matchIndex[i] = 0
	}
	r.matchIndex[r.peerIdx(r.id)] = r.tail.persisted
	r.broadcastAppend()
}

func (r *Raft) broadcastAppend() {
	for _, p := range r.peers {
		if p == r.id {
			continue
		}
		r.sendAppend(p)
	}
}

func (r *Raft) sendAppend(to NodeID) {
	i := r.peerIdx(to)
	if i < 0 {
		return
	}
	next := r.nextIndex[i]
	if next < 1 {
		next = 1
	}

	// A follower whose next index is behind our compaction point cannot be
	// caught up from the log, because the entries it needs are gone. It gets a
	// snapshot instead.
	if next <= r.snapIndex {
		r.sendSnapshot(to)
		return
	}

	prev := next - 1
	var ents []Entry
	if next <= r.lastIndex() {
		ents = append(ents, r.suffixFrom(next)...)
	}
	// GATE: MsgApp carries this leader's term and its own entries. Gating it
	// keeps a leader from advertising a log prefix it has not written.
	r.sendGated(Message{
		Type: MsgApp, From: r.id, To: to, Term: r.term,
		PrevLogIndex: prev, PrevLogTerm: r.termAt(prev),
		Entries: ents, LeaderCommit: r.commitIndex,
	})
}

// HasReady reports whether there is anything to drain.
func (r *Raft) HasReady() bool {
	return r.hardStateDirty || r.tail.handed < r.lastIndex() || len(r.msgs) > 0 ||
		r.appliedIdx < r.commitIndex || r.pendingSnap != nil || len(r.snapTo) > 0
}

// Ready is a drain: it returns pending outputs and clears them.
//
// There is no Advance. The driver acknowledges progress per resource, with
// AckPersisted(rd.Mark) once the hard state and entries are durable and
// AckApplied(index) once entries are applied, and the two are independent.
func (r *Raft) Ready() Ready {
	rd := Ready{Messages: r.msgs, Mark: r.dirtyMark}
	if r.pendingSnap != nil {
		rd.Snapshot = r.pendingSnap
		rd.SnapMark = r.snapMark
		r.pendingSnap = nil
	}
	if len(r.snapTo) > 0 {
		rd.SnapshotTo = r.snapTo
		r.snapTo = nil
	}
	if r.tail.handed < r.lastIndex() {
		rd.Entries = append(rd.Entries, r.suffixFrom(r.tail.handed+1)...)
	}
	if r.hardStateDirty {
		hs := HardState{Term: r.term, Vote: r.vote}
		rd.HardState = &hs
	}
	for i := r.appliedIdx + 1; i <= r.commitIndex && i <= r.lastIndex(); i++ {
		e, ok := r.at(i)
		if !ok {
			// Applied indices never run behind the snapshot: Compact refuses to
			// discard anything the state machine has not consumed, and an
			// install sets appliedIdx to the snapshot's index. Reaching here
			// means one of those two stopped holding.
			panic(fmt.Sprintf(
				"raft: node %d was told to apply index %d, which is behind its snapshot at %d",
				r.id, i, r.snapIndex))
		}
		rd.Committed = append(rd.Committed, e)
	}

	if rd.HardState != nil || len(rd.Entries) > 0 {
		// The mark names THIS handover, and from here its coverage is frozen:
		// anything mutated after this point gets a mark of its own (see dirty).
		r.markHandedOff = true
		r.lastHandedMark = rd.Mark
		if n := len(rd.Entries); n > 0 {
			// Handed over, not durable. The two are recorded separately, in
			// fields with different names, and never compared as one fact.
			r.tail.handed = rd.Entries[n-1].Index
			r.markLastIdx = r.tail.handed
		}
	} else {
		// A mark that covers nothing is satisfied by definition. If the driver
		// has never been handed anything to persist under it -- because a
		// conflicting append truncated the entries that opened it -- then
		// waiting for an acknowledgement waits forever, so it is closed here
		// and its gated messages are released.
		//
		// AssertQuiescent is what turned this from a silent stall into a
		// failure.
		if rd.Mark != 0 && !r.markHandedOff {
			r.releaseThrough(rd.Mark)
		}
		// A Ready that hands nothing over names no durability point, even while
		// an earlier mark is still in flight. Reporting one the driver cannot
		// act on invites it to acknowledge a write it never made.
		rd.Mark = 0
	}

	r.msgs = nil
	r.hardStateDirty = false
	return rd
}

// AckPersisted reports that everything up to and including mark m is durable,
// and releases every message that was waiting on it.
//
// Marks are monotone, so an ack for m implies every earlier mark. A stale or
// duplicate ack is ignored rather than rejected: the driver may legitimately
// batch several marks into one sync.
func (r *Raft) AckPersisted(m PersistMark) {
	if m <= r.persisted {
		return
	}
	r.persisted = m

	// The persisted INDEX watermark moves here and nowhere else, and it moves
	// only on an acknowledgement that reaches the most recent handover. An
	// earlier mark going durable says nothing about where the log's durable
	// prefix now ends, because a later handover may have carried it further; so
	// this lags rather than guesses, and a lagging watermark only over-gates.
	if r.lastHandedMark != 0 && m >= r.lastHandedMark && r.markLastIdx > r.tail.persisted {
		r.tail.persisted = r.markLastIdx
	}

	r.releaseThrough(m)

	// # A leader counts its own replication only once it is durable
	//
	// This is the only place the leader's own match index advances, and that is
	// the point. It used to be set optimistically in Propose and becomeLeader as
	// well, so the durability-driven update here could never lower it and the
	// comment above it was false: the leader counted a copy it had not written.
	//
	// The defect was LATENT rather than live, and the reason is worth recording
	// because it is a property of the configuration and not of the code. A
	// follower cannot acknowledge before its own fsync, and the leader's fsync
	// starts no later than the append it is replicating, so on this schedule mix
	// the leader's own copy is always durable before any follower's ack arrives.
	// Removing the optimistic assignment is byte-identical: seed 92 produces the
	// same trace hash, and 300 seeds produce the same census to the digit.
	//
	// It is fixed anyway. The equality holds because of a latency relationship
	// nothing enforces -- a slower local disk, or a follower whose sync outruns
	// the leader's, breaks it -- and a safety property resting on a timing
	// coincidence is the shape this repository keeps finding at the bottom of
	// its bugs.
	if r.role == RoleLeader {
		if i := r.peerIdx(r.id); i >= 0 && r.persistedIndex() > r.matchIndex[i] {
			r.matchIndex[i] = r.persistedIndex()
			r.maybeCommit()
		}
	}
}

// AckSnapshot reports that the snapshot install named by mark is durable.
//
// A separate entry point because it is a separate stream: no ordering may be
// assumed between a snapshot write and a log write, so acknowledging one says
// nothing about the other. Conflating them is how one token came to stand for
// more than the acknowledgement meant (BUG-006).
func (r *Raft) AckSnapshot(m PersistMark) {
	if m <= r.persistedSnap {
		return
	}
	r.persistedSnap = m
	if r.snapMark != 0 && r.snapMark <= m {
		r.snapMark = 0
	}
	r.release()
}

// releaseThrough closes any log mark at or below m and releases what no longer
// waits on anything.
func (r *Raft) releaseThrough(m PersistMark) {
	if m > r.persisted {
		r.persisted = m
	}
	if r.dirtyMark != 0 && r.dirtyMark <= m {
		r.dirtyMark = 0
		r.markHandedOff = false
	}
	r.release()
}

// release moves out every withheld message whose durability points have all
// landed. A message with a constraint in both streams waits for both, because
// with two streams there is no ordering to make one imply the other.
func (r *Raft) release() {
	kept := r.gated[:0]
	for _, g := range r.gated {
		if g.mark <= r.persisted && g.snapMark <= r.persistedSnap {
			r.msgs = append(r.msgs, g.msg)
			continue
		}
		kept = append(kept, g)
	}
	r.gated = kept
}

// AckApplied reports that entries up to index have been applied.
func (r *Raft) AckApplied(index Index) {
	if index > r.appliedIdx {
		r.appliedIdx = index
	}
}

// persistedIndex is the highest log index the driver has ACKNOWLEDGED durable.
//
// Acknowledged, not handed over. The two differ for exactly as long as a write
// is in flight, which is the entire window persist-before-reply covers, and
// conflating them was BUG-005.
func (r *Raft) persistedIndex() Index { return r.tail.persisted }

// markFor returns the mark that must be durable before index idx may be
// attested to, or zero if it already is.
//
// It rests on one invariant: **every log index above tail.persisted is covered
// by the currently open mark.** Entries are appended only through
// appendEntries, which opens a mark; the mark stays open until it is
// acknowledged, and the acknowledgement is what moves tail.persisted. So an
// index that is not yet durable is either already handed over under the open
// mark or was appended under it moments ago, and in both cases the open mark is
// the point that must become durable first.
func (r *Raft) marksFor(idx Index) (logMark, snapMark PersistMark) {
	if idx == 0 {
		return 0, 0
	}
	if idx <= r.snapIndex {
		// Covered by the snapshot stream, and by nothing in the log stream.
		return 0, r.snapMark
	}
	if idx <= r.tail.persisted {
		return 0, 0
	}
	if r.dirtyMark == 0 {
		panic(fmt.Sprintf(
			"raft: node %d was asked for the mark covering index %d with %d durable and no mark "+
				"open; the index is neither durable nor pending, so nothing would gate a message "+
				"that attests to it", r.id, idx, r.tail.persisted))
	}
	return r.dirtyMark, 0
}

// PendingGated is how many messages are withheld awaiting durability.
//
// Exported for the driver's quiescence assertion, not for oracles: it is a
// harness-liveness question, not a safety judgement, and no safety oracle reads
// it (DESIGN-A1 §0).
func (r *Raft) PendingGated() int { return len(r.gated) }

// AssertConfConsistent refuses a node whose cached configuration disagrees with
// its own log.
//
// The active configuration is a FUNCTION of the log (DESIGN-A3 §3), so it can
// always be recomputed and compared. Every path that changes the log has to
// recompute it, and DESIGN-A3 names that as where the bugs live -- a truncation
// that forgets leaves a node using a membership its own log no longer describes,
// and nothing outside the node can see the difference, because the log it
// publishes is correct and only its idea of who is in the cluster is not.
//
// This is a check that can only fail, which is the side of the provenance rule
// where a node's own state is a permitted input.
func (r *Raft) AssertConfConsistent() error {
	want := r.baseConf.Clone()
	for _, e := range r.log {
		if e.Type != EntryConfChange {
			continue
		}
		cc, ok := DecodeConfChange(e.Data)
		if !ok {
			continue
		}
		for _, ch := range cc.Changes {
			if next, err := want.apply(ch); err == nil {
				want = next
			}
		}
	}
	if r.conf.Equal(want) {
		return nil
	}
	return fmt.Errorf(
		"raft: node %d is using configuration %s while its own log says %s; a path that changes the "+
			"log forgot to recompute, and from outside the node this is invisible -- the log it "+
			"publishes is correct and only its idea of who is in the cluster is not",
		r.id, r.conf, want)
}

// AssertQuiescent refuses a node that has gone quiet while still withholding a
// message.
//
// The gated queue is where a whole bug class now lives. A message gated on a
// mark that is never acknowledged is **indistinguishable from a message that was
// never generated**: the cluster simply stalls, every checker stays green, and
// the run reports quiescent. This repository has already shipped five mechanisms
// that were silently never invoked; a queue that quietly swallows would be the
// sixth.
//
// So the driver calls this at every quiescent point and at run end, and a
// non-empty queue is a failure rather than silence.
func (r *Raft) AssertQuiescent() error {
	if len(r.gated) == 0 {
		return nil
	}
	g := r.gated[0]
	marks := ""
	for i, x := range r.gated {
		if i > 6 {
			break
		}
		marks += fmt.Sprintf(" %d", x.mark)
	}
	_ = marks
	return fmt.Errorf(
		"raft: node %d went quiescent still withholding %d message(s); the oldest is a %s to node %d "+
			"gated on log mark %d / snapshot mark %d while only %d / %d are durable. A message gated "+
			"on a mark that is never acked is indistinguishable from a message never generated: the "+
			"cluster stalls and every checker stays green",
		r.id, len(r.gated), g.msg.Type, g.msg.To, g.mark, g.snapMark, r.persisted, r.persistedSnap)
}

// --- helpers -----------------------------------------------------------------

// send releases a message that attests to no persistent state.
func (r *Raft) send(m Message) { r.msgs = append(r.msgs, m) }

// sendGated withholds a message until the currently-pending persistent state is
// durable. If nothing is pending it is released immediately, which is not a
// weakening: with nothing dirty there is no mutation for the message to outrun.
//
// Every call site names the gate it is discharging, so the enumeration on
// Ready.Messages and the code cannot drift apart silently.
func (r *Raft) sendGated(m Message) { r.sendGatedOn(r.dirtyMark, 0, m) }

// sendGatedOn withholds a message until BOTH named marks are durable. A zero
// mark is no constraint from that stream.
func (r *Raft) sendGatedOn(mark, snapMark PersistMark, m Message) {
	if mark == 0 && snapMark == 0 {
		r.msgs = append(r.msgs, m)
		return
	}
	r.gated = append(r.gated, gatedMessage{msg: m, mark: mark, snapMark: snapMark})
}

// dirty opens (or extends) the pending durability point. Called by every
// mutation of persistent state, so a mark exists to gate against.
func (r *Raft) dirty() {
	// # A mark's coverage is frozen at handover
	//
	// Reusing a mark that has already been handed over lets its coverage grow
	// after the driver started writing it, so the acknowledgement means strictly
	// less than the messages gated on that mark require: the driver reports the
	// first batch durable, raft releases an append response attesting to the
	// second, and a follower acks index N with N-1 on disk. That was the whole
	// residue of BUG-005 once the oracle stopped reading the engine's visible
	// state and started reading what was actually durable.
	//
	// It is also a liveness hazard on its own. Under a steady stream of appends
	// a reused mark never stops growing, so it never becomes fully durable, and
	// every message gated on it waits forever behind a convoy.
	if r.dirtyMark == 0 || r.markHandedOff {
		r.nextMark++
		r.dirtyMark = r.nextMark
		r.markHandedOff = false
	}
}

func (r *Raft) setVote(v NodeID) {
	if r.vote != v {
		r.vote = v
		r.hardStateDirty = true
		r.dirty()
	}
}

func (r *Raft) setTermAndVote(t Term, v NodeID) {
	if r.term != t || r.vote != v {
		r.term, r.vote = t, v
		r.hardStateDirty = true
		r.dirty()
	}
}

func (r *Raft) appendEntries(es ...Entry) {
	conf := false
	// Positions are derived from indices now, so a gap would not be a wrong
	// answer later, it would be a wrong answer everywhere. Refuse it here.
	for _, e := range es {
		if e.Index != r.lastIndex()+1 {
			panic(fmt.Sprintf(
				"raft: node %d appended index %d onto a log ending at %d; the slice's positions are "+
					"derived from indices, so a gap makes every subsequent lookup wrong",
				r.id, e.Index, r.lastIndex()))
		}
		r.log = append(r.log, e)
		if e.Type == EntryConfChange {
			conf = true
		}
	}
	if conf {
		// Effect on APPEND, not on commit (DESIGN-A3 §3): commitment is counted
		// under the OLD configuration, so waiting for it lets a node's own vote
		// commit its own removal.
		r.recomputeConf()
	}
	r.dirty()
}

func (r *Raft) truncateFrom(i Index) {
	if i < 1 || i > r.lastIndex() {
		return
	}
	// # What may never be truncated is COMMITTED, not durable
	//
	// The assertion that stood here refused any truncation at or below the
	// durable watermark, on the reasoning that the driver would then have
	// acknowledged an entry that later vanished. That is a stronger claim than
	// Raft makes, and it is false: §5.3 has a follower delete a conflicting
	// entry and everything after it, and those entries are routinely already on
	// disk. A follower's persisted suffix being overwritten by a new leader is
	// the protocol working. What Raft guarantees is that a COMMITTED entry is
	// never overwritten, because the up-to-date check keeps a candidate missing
	// one from ever winning.
	//
	// The false assertion sat here unreachable for as long as tail.persisted
	// never moved -- which is the same defect twice: a claim nothing exercised,
	// guarding a watermark nothing advanced.
	if r.commitIndex >= i {
		panic(fmt.Sprintf(
			"raft: node %d truncated to %d with commit index %d; an entry this node was told "+
				"was committed is being overwritten, which is state machine safety failing",
			r.id, i, r.commitIndex))
	}

	if i <= r.snapIndex {
		panic(fmt.Sprintf(
			"raft: node %d truncated to %d with a snapshot at %d; the entries being discarded are "+
				"already in the snapshot, which means an applied prefix is being rewritten",
			r.id, i, r.snapIndex))
	}
	p, ok := r.pos(i)
	if !ok {
		panic(fmt.Sprintf("raft: node %d truncated to %d, which its log does not hold", r.id, i))
	}
	r.log = r.log[:p]

	// Durability is a fact about a POSITION, and these positions no longer hold
	// what was written to them. All three watermarks come back to the cut --
	// recorded, like every other durability fact here, rather than left to be
	// inferred from a log that no longer contains the entries.
	if r.tail.persisted >= i {
		r.tail.persisted = i - 1
	}
	if r.tail.handed >= i {
		r.tail.handed = i - 1
	}
	if r.markLastIdx >= i {
		r.markLastIdx = i - 1
	}

	// A withheld append response that attests to a truncated index has become a
	// lie, and releasing it later would tell a leader this node holds an entry
	// it has just deleted -- a stale ack that can be counted toward a quorum.
	// Dropping it is safe in a way that sending it is not: Raft tolerates lost
	// messages and the leader retries, so the cost is a round trip and the
	// alternative is a phantom acknowledgement.
	kept := r.gated[:0]
	for _, g := range r.gated {
		if g.msg.Type == MsgAppResp && g.msg.Success && g.msg.MatchIndex >= i {
			continue
		}
		kept = append(kept, g)
	}
	r.gated = kept

	// A truncation can remove a configuration entry, so the active configuration
	// has to be rebuilt from what the log now says. Recomputed from scratch
	// rather than undone: an undo that is one case short is a cluster that
	// disagrees with itself about who is in it.
	r.recomputeConf()
}

// pos converts a log index to a position in the slice, reporting whether the
// slice actually holds it.
//
// It answers false for an index BEHIND the snapshot and for one PAST the end,
// and callers must distinguish those: a compacted index is a known entry this
// node no longer holds, and an index past the end is an entry nobody holds.
// Collapsing them is how a compacted log starts answering questions about
// entries it does not have.
func (r *Raft) pos(i Index) (int, bool) {
	if i <= r.snapIndex || i > r.lastIndex() {
		return 0, false
	}
	return int(i - r.snapIndex - 1), true
}

// at returns the entry at index i, or false if the log does not hold it.
func (r *Raft) at(i Index) (Entry, bool) {
	p, ok := r.pos(i)
	if !ok {
		return Entry{}, false
	}
	return r.log[p], true
}

// suffixFrom returns the entries from index i to the end.
//
// Asking for a suffix that starts behind the snapshot is a caller error, not a
// short answer: those entries are gone, the right response is a snapshot, and
// returning what happens to be left would replicate a hole.
func (r *Raft) suffixFrom(i Index) []Entry {
	if i <= r.snapIndex {
		panic(fmt.Sprintf(
			"raft: node %d was asked for the log from index %d with a snapshot at %d; those entries "+
				"are compacted away and the caller owes the follower a snapshot instead",
			r.id, i, r.snapIndex))
	}
	p, ok := r.pos(i)
	if !ok {
		return nil
	}
	return r.log[p:]
}

func (r *Raft) lastIndex() Index {
	if len(r.log) == 0 {
		return r.snapIndex
	}
	return r.log[len(r.log)-1].Index
}

func (r *Raft) lastTerm() Term { return r.termAt(r.lastIndex()) }

// termAt returns the term of entry i, or zero if this node cannot answer.
//
// Zero is returned both for "before the log begins" and for "compacted away",
// and every caller that could be misled by that conflation checks snapIndex
// itself rather than reading zero as an answer. matches is the one that matters.
func (r *Raft) termAt(i Index) Term {
	if i == 0 {
		return 0
	}
	if i == r.snapIndex {
		return r.snapTerm
	}
	if e, ok := r.at(i); ok {
		return e.Term
	}
	return 0
}

func (r *Raft) matches(idx Index, term Term) bool {
	if idx == 0 {
		// "Append from the very beginning" is only agreeable to a node that HAS
		// a beginning. Once a prefix is in a snapshot, accepting an append at
		// index 1 means overwriting entries this node has already applied --
		// and it is not a hypothetical: a leader whose view of this follower has
		// been reset sends exactly this, and the follower's own commit index is
		// already past it.
		//
		// Found at 10k by the assertion BUG-007 corrected: "node 3 truncated to
		// 1 with commit index 6". The old form of that assertion, which fired on
		// the durable watermark instead of the commit index, would have caught a
		// legal truncation here and missed this one.
		//
		// Rejecting is safe and self-correcting: the reject carries lastIndex as
		// its hint, so the leader jumps forward, finds the follower is past its
		// own log, and sends a snapshot.
		return r.snapIndex == 0
	}
	if idx > r.lastIndex() {
		return false
	}
	if idx < r.snapIndex {
		// The entry is behind our snapshot: part of a prefix already applied,
		// which this node cannot re-verify and must not pretend to. Refusing is
		// safe -- the reject carries lastIndex as its hint, so a leader that is
		// this far behind our state jumps forward in one round rather than
		// walking backwards into a log that no longer exists.
		return false
	}
	return r.termAt(idx) == term
}

// logIsUpToDate implements the candidate-log check: a later term wins, and at
// equal terms a longer log wins.
func (r *Raft) logIsUpToDate(idx Index, term Term) bool {
	mine := r.lastTerm()
	if term != mine {
		return term > mine
	}
	return idx >= r.lastIndex()
}

// quorum is a majority of VOTERS. Learners are counted in nothing, which is the
// whole reason they exist.
func (r *Raft) quorum() int { return len(r.conf.Voters)/2 + 1 }

// countVoters counts set flags belonging to voters only. The flag slices are
// indexed by position in peers, which includes learners, so counting the slice
// would let a learner vote by being in it.
func (r *Raft) countVoters(flags []bool) int {
	n := 0
	for i, p := range r.peers {
		if i < len(flags) && flags[i] && r.conf.IsVoter(p) {
			n++
		}
	}
	return n
}

// defaultPromotionLag is the catch-up bound when none is configured.
const defaultPromotionLag Index = 8

// recomputeConf rebuilds the active configuration from the snapshot's
// configuration plus every configuration entry still in the log, in order.
//
// # Why every log change goes through here
//
// A configuration entry takes effect when it is APPENDED (DESIGN-A3 §3), so a
// configuration is a function of the log's contents -- and the log can be
// truncated, replaced by a snapshot, or restored from disk. Recomputing from
// scratch is the only version that cannot drift: incremental application has to
// be undone on truncation, and an undo that is one case short is a cluster that
// disagrees with itself about who is in it.
func (r *Raft) recomputeConf() {
	conf := r.baseConf.Clone()
	for _, e := range r.log {
		if e.Type != EntryConfChange {
			continue
		}
		cc, ok := DecodeConfChange(e.Data)
		if !ok {
			continue
		}
		for _, ch := range cc.Changes {
			if next, err := conf.apply(ch); err == nil {
				conf = next
			}
		}
	}
	r.setConf(conf)
}

// setConf installs a configuration and rebuilds the peer-indexed state,
// preserving each node's progress BY IDENTITY.
//
// Preserving by position would be the same bug as identifying a proposal by its
// log index (BUG-004): the position means something only relative to a peer set
// that has just changed.
func (r *Raft) setConf(conf Configuration) {
	oldPeers := r.peers
	oldNext, oldMatch := r.nextIndex, r.matchIndex
	oldGranted, oldDenied, oldPre := r.votesGranted, r.votesDenied, r.preGranted

	r.conf = conf
	r.peers = conf.Members()
	n := len(r.peers)
	r.nextIndex = make([]Index, n)
	r.matchIndex = make([]Index, n)
	r.votesGranted = make([]bool, n)
	r.votesDenied = make([]bool, n)
	r.preGranted = make([]bool, n)

	for i, p := range r.peers {
		for j, q := range oldPeers {
			if p != q {
				continue
			}
			if j < len(oldNext) {
				r.nextIndex[i] = oldNext[j]
			}
			if j < len(oldMatch) {
				r.matchIndex[i] = oldMatch[j]
			}
			if j < len(oldGranted) {
				r.votesGranted[i] = oldGranted[j]
			}
			if j < len(oldDenied) {
				r.votesDenied[i] = oldDenied[j]
			}
			if j < len(oldPre) {
				r.preGranted[i] = oldPre[j]
			}
			break
		}
		// A node that is new to the configuration starts from the leader's own
		// end of the log, as becomeLeader would have set it.
		if r.nextIndex[i] == 0 {
			r.nextIndex[i] = r.lastIndex() + 1
		}
	}
}

// Configuration reports the active membership. For the driver's routing and for
// logging; no oracle may call it (DESIGN-A1 §0).
func (r *Raft) Configuration() Configuration { return r.conf.Clone() }

func (r *Raft) peerIdx(id NodeID) int {
	for i, p := range r.peers {
		if p == id {
			return i
		}
	}
	return -1
}

func count(bs []bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}
