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

// Entry is one log entry.
type Entry struct {
	Term  Term
	Index Index
	ID    ProposalID
	Data  []byte
}

// MessageType is a closed enum.
type MessageType uint8

const (
	MsgVote MessageType = iota + 1
	MsgVoteResp
	MsgApp
	MsgAppResp
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

	// Mark names this Ready's durability point; zero if nothing to persist.
	Mark PersistMark

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
	// unchanged so that change cannot pass silently. (Pre-vote lands in A2.)
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
}

// Raft is one node's state machine.
type Raft struct {
	id    NodeID
	peers []NodeID

	// Persistent, and durable before it is acted upon.
	term Term
	vote NodeID
	log  []Entry // index 0 holds log position 1

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

// gatedMessage is an outbound message and the durability point it attests to.
type gatedMessage struct {
	msg  Message
	mark PersistMark
}

// ErrNotLeader is returned by Propose on a node that is not the leader.
var ErrNotLeader = errors.New("raft: not leader")

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

	peers := make([]NodeID, len(cfg.Peers))
	copy(peers, cfg.Peers)
	r := &Raft{
		id: cfg.ID, peers: peers,
		role:                      RoleFollower,
		electionTimeout:           cfg.ElectionTimeout,
		randomizedElectionTimeout: cfg.ElectionTimeout,
		heartbeatTimeout:          cfg.HeartbeatTimeout,
		nextIndex:                 make([]Index, len(peers)),
		matchIndex:                make([]Index, len(peers)),
		votesGranted:              make([]bool, len(peers)),
		votesDenied:               make([]bool, len(peers)),
	}
	return r, nil
}

// Restore rebuilds a node from what an engine gave back after a crash. It is the
// real recovery path: every restart in the simulator goes through it, so the
// path is exercised by every crash the harness injects rather than by a test.
func Restore(cfg Config, hs HardState, entries []Entry) (*Raft, error) {
	r, err := New(cfg)
	if err != nil {
		return nil, err
	}
	r.term, r.vote = hs.Term, hs.Vote
	r.log = append(r.log, entries...)
	for i, e := range r.log {
		if e.Index != Index(i+1) {
			return nil, fmt.Errorf("raft: recovered log entry %d claims index %d; the log is not a gapless prefix", i, e.Index)
		}
	}
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

// ID, Role and Term exist for the driver and for logging. **No oracle may call
// them**: oracle independence means the checkers read the Ready stream and what
// was persisted, never the node (DESIGN-A1 §0). A driver needs Role to decide
// whether to accept a client proposal, which is not a safety judgement.
func (r *Raft) ID() NodeID { return r.id }

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
		if r.electionElapsed >= r.randomizedElectionTimeout {
			r.campaign()
		}
	case RoleLeader:
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
	e := Entry{Term: r.term, Index: r.lastIndex() + 1, ID: id, Data: append([]byte(nil), data...)}
	r.appendEntries(e)
	r.matchIndex[r.peerIdx(r.id)] = e.Index
	r.broadcastAppend()
	return nil
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
	if m.Term > r.term {
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
	case numMessageTypes:
	}
	return nil
}

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
	case count(r.votesGranted) >= r.quorum():
		r.becomeLeader()
	case count(r.votesDenied) >= r.quorum():
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
	gate := r.markFor(last)
	if r.dirtyMark > gate {
		gate = r.dirtyMark
	}
	r.sendGatedOn(gate, Message{
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
		for i := range r.peers {
			if r.matchIndex[i] >= n {
				cnt++
			}
		}
		if cnt >= r.quorum() {
			r.commitIndex = n
			return
		}
	}
}

func (r *Raft) campaign() {
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
}

func (r *Raft) becomeLeader() {
	r.role = RoleLeader
	r.leader = r.id
	r.heartbeatElapsed = 0
	last := r.lastIndex()
	for i := range r.peers {
		r.nextIndex[i] = last + 1
		r.matchIndex[i] = 0
	}
	r.matchIndex[r.peerIdx(r.id)] = last
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
	prev := next - 1
	var ents []Entry
	if next <= r.lastIndex() {
		ents = append(ents, r.log[next-1:]...)
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
	return r.hardStateDirty || r.tail.handed < r.lastIndex() || len(r.msgs) > 0 || r.appliedIdx < r.commitIndex
}

// Ready is a drain: it returns pending outputs and clears them.
//
// There is no Advance. The driver acknowledges progress per resource, with
// AckPersisted(rd.Mark) once the hard state and entries are durable and
// AckApplied(index) once entries are applied, and the two are independent.
func (r *Raft) Ready() Ready {
	rd := Ready{Messages: r.msgs, Mark: r.dirtyMark}
	if r.tail.handed < r.lastIndex() {
		rd.Entries = append(rd.Entries, r.log[r.tail.handed:]...)
	}
	if r.hardStateDirty {
		hs := HardState{Term: r.term, Vote: r.vote}
		rd.HardState = &hs
	}
	for i := r.appliedIdx + 1; i <= r.commitIndex && i <= r.lastIndex(); i++ {
		rd.Committed = append(rd.Committed, r.log[i-1])
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

	// A leader counts its own replication only once it is durable. Pipelining
	// depends on this being persistedIndex rather than lastIndex: without it a
	// leader could commit on the strength of an append it has not yet written.
	if r.role == RoleLeader {
		if i := r.peerIdx(r.id); i >= 0 && r.persistedIndex() > r.matchIndex[i] {
			r.matchIndex[i] = r.persistedIndex()
			r.maybeCommit()
		}
	}
}

// releaseThrough closes any mark at or below m and releases what waited on it.
func (r *Raft) releaseThrough(m PersistMark) {
	if m > r.persisted {
		r.persisted = m
	}
	if r.dirtyMark != 0 && r.dirtyMark <= m {
		r.dirtyMark = 0
		r.markHandedOff = false
	}

	kept := r.gated[:0]
	for _, g := range r.gated {
		if g.mark <= r.persisted {
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
func (r *Raft) markFor(idx Index) PersistMark {
	if idx <= r.tail.persisted {
		return 0
	}
	if r.dirtyMark == 0 {
		// The invariant above has broken. An index that is neither durable nor
		// covered by an open mark has no gate to wait on, so any message
		// attesting to it would be released immediately -- silently, and only
		// on the schedules where the gap is reachable.
		//
		// This is the house move rather than a checker: the state is refused
		// where it is constructed instead of being caught downstream by an
		// oracle, some seeds and one instant later. Restore forgetting to
		// record what the engine gave back lands exactly here, and so does an
		// acknowledgement that never moves the persisted watermark.
		panic(fmt.Sprintf(
			"raft: node %d was asked for the mark covering index %d with %d durable and no mark "+
				"open; the index is neither durable nor pending, so nothing would gate a message "+
				"that attests to it", r.id, idx, r.tail.persisted))
	}
	return r.dirtyMark
}

// PendingGated is how many messages are withheld awaiting durability.
//
// Exported for the driver's quiescence assertion, not for oracles: it is a
// harness-liveness question, not a safety judgement, and no safety oracle reads
// it (DESIGN-A1 §0).
func (r *Raft) PendingGated() int { return len(r.gated) }

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
			"gated on mark %d while only %d is durable. A message gated on a mark that is never acked is "+
			"indistinguishable from a message never generated: the cluster stalls and every checker stays green",
		r.id, len(r.gated), g.msg.Type, g.msg.To, g.mark, r.persisted)
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
func (r *Raft) sendGated(m Message) { r.sendGatedOn(r.dirtyMark, m) }

// sendGatedOn withholds a message until the named mark is durable.
func (r *Raft) sendGatedOn(mark PersistMark, m Message) {
	if mark == 0 {
		r.msgs = append(r.msgs, m)
		return
	}
	r.gated = append(r.gated, gatedMessage{msg: m, mark: mark})
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
	r.log = append(r.log, es...)
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

	r.log = r.log[:i-1]

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
}

func (r *Raft) lastIndex() Index {
	if len(r.log) == 0 {
		return 0
	}
	return r.log[len(r.log)-1].Index
}

func (r *Raft) lastTerm() Term { return r.termAt(r.lastIndex()) }

func (r *Raft) termAt(i Index) Term {
	if i == 0 || i > r.lastIndex() {
		return 0
	}
	return r.log[i-1].Term
}

func (r *Raft) matches(idx Index, term Term) bool {
	if idx == 0 {
		return true
	}
	if idx > r.lastIndex() {
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

func (r *Raft) quorum() int { return len(r.peers)/2 + 1 }

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
