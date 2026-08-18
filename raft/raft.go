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
	msgs     []Message
	unstable []Entry

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
	r.sendGated(Message{
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
	return r.hardStateDirty || len(r.unstable) > 0 || len(r.msgs) > 0 || r.appliedIdx < r.commitIndex
}

// Ready is a drain: it returns pending outputs and clears them.
//
// There is no Advance. The driver acknowledges progress per resource, with
// AckPersisted(rd.Mark) once the hard state and entries are durable and
// AckApplied(index) once entries are applied, and the two are independent.
func (r *Raft) Ready() Ready {
	rd := Ready{Entries: r.unstable, Messages: r.msgs, Mark: r.dirtyMark}
	if r.hardStateDirty {
		hs := HardState{Term: r.term, Vote: r.vote}
		rd.HardState = &hs
	}
	for i := r.appliedIdx + 1; i <= r.commitIndex && i <= r.lastIndex(); i++ {
		rd.Committed = append(rd.Committed, r.log[i-1])
	}

	if rd.HardState != nil || len(rd.Entries) > 0 {
		r.markHandedOff = true
	}

	r.unstable = nil
	r.msgs = nil
	r.hardStateDirty = false

	// A mark that covers nothing is satisfied by definition. If the driver has
	// never been handed anything to persist under it -- because a conflicting
	// append truncated the entries that opened it -- then waiting for a
	// durability acknowledgement waits forever, so it is closed here and its
	// gated messages are released.
	//
	// AssertQuiescent is what turned this from a silent stall into a failure.
	if rd.Mark != 0 && !r.markHandedOff {
		mark := rd.Mark
		rd.Mark = 0
		r.releaseThrough(mark)
	}

	// Otherwise the mark stays open until the driver acknowledges it: it is the
	// token the gated queue is keyed on, and clearing it here would release
	// every withheld message on the next mutation.
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

// persistedIndex is the highest log index the driver has made durable. Entries
// still in the unstable tail are not counted.
func (r *Raft) persistedIndex() Index {
	last := r.lastIndex()
	if len(r.unstable) == 0 {
		return last
	}
	return r.unstable[0].Index - 1
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
func (r *Raft) sendGated(m Message) {
	if r.dirtyMark == 0 {
		r.msgs = append(r.msgs, m)
		return
	}
	r.gated = append(r.gated, gatedMessage{msg: m, mark: r.dirtyMark})
}

// dirty opens (or extends) the pending durability point. Called by every
// mutation of persistent state, so a mark exists to gate against.
func (r *Raft) dirty() {
	if r.dirtyMark == 0 {
		r.nextMark++
		r.dirtyMark = r.nextMark
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
	r.unstable = append(r.unstable, es...)
	r.dirty()
}

func (r *Raft) truncateFrom(i Index) {
	if i < 1 || i > r.lastIndex() {
		return
	}
	r.log = r.log[:i-1]
	// Anything unstable at or past the truncation point is no longer real.
	kept := r.unstable[:0]
	for _, e := range r.unstable {
		if e.Index < i {
			kept = append(kept, e)
		}
	}
	r.unstable = kept
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
