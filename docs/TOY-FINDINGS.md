# TOY-FINDINGS.md

Defects the verification machinery found in **`sim/toy`**, the fixture protocol the harness is
calibrated against.

**What this file holds, and why it is not BUGS.md.** These are **fixture defects**, and an entry here
is a claim about the *machinery*, not about Rift. The toy is a deliberately simple replicated register
built to exercise the harness; it is not the system under test, and Rift's system under test — `raft/`,
`store/`, `kv/`, `router/`, `balancer/`, the C++ engine — does not exist yet.

BUGS.md holds defects in that real system, found by the machinery, and nothing else. Its whole value
is that a stranger can read any entry and know it is a real defect in the real system. Two toy bugs
sitting in it would mean that when A1 lands its first genuine Raft bug, nobody could tell which
entries are Rift and which are scaffolding — so the files are separate, permanently, and BUGS.md is
correctly empty until A1. *(Ansh, 2026-08-17.)*

**Why they are kept at all.** They are the best evidence in the project that the machinery works end
to end: two real defects, in code nobody had marked as suspect, found by sweeping seeds against the
*correct* build, triaged through the stripped-fault gate, reproduced from a bundle, and each closed
with a mutant class that keeps the bug class catchable. That chain is what A1 will rely on, and this
file is the demonstration that it ran.

Format matches BUGS.md exactly, so an entry can be read the same way and so the two files never drift
into different shapes. Harness defects — bugs in the simulator, generator, or injectors themselves —
are recorded in their fix commit and analysed in the relevant design doc, per DR-29; the `Trigger`
budget defect that surfaced both entries below is in `DESIGN-A0.10` §5.

**Counts:** 2 entries.

---

## Standing limitation of these two classes

Both mutant classes below detect at **1 in 1000 seeds**, with seeds-to-detection 104 and 154. Stated
plainly, and stated here rather than buried in a table:

- **At that rate a 100-seed run finds neither.** As regression detectors these two currently prove
  almost nothing; a change that silently removed either would very likely pass a smoke lane.
- **Reproducing at exactly their discovery seed is reproducibility, not power.** It shows the bundle
  and the replay path work. It does not show the class is reliably reachable.
- Their harness-power floors are therefore set at **detected-at-all**, not at a rate — see
  `sim/hunt/floors.go`. A floor derived from a 1-in-1000 measurement would be noise.

**When the soak farm switches on at A1, these are the first two classes to re-measure at higher seed
counts**, to establish whether 1 in 1000 is the real rate or an artifact of the current schedule mix
(30 client ops, 8 keys, one crash, one partition, five seconds). If it is the schedule, the mix is
what changes — never the checker.

---

## Entries

### TOY-001 — a value that was read back successfully disappeared after the primary restarted

| field | value |
|---|---|
| **Found by** | sim — 1000-seed sweep of the *correct* toy, with no flaw planted |
| **Phase** | A0 (checklist step 7) |
| **Reproduce (seed)** | `simctl run --seed 103 --workload toy --flaw none` at the commit that contained the bug |
| **Reproduce (plan)** | `simctl replay --bundle seeds/TOY-001` (any commit; the bundle plants `--flaw dirty-read`, which is the bug preserved as a mutant) |
| **Triage verdict** | `simctl replay --bundle seeds/TOY-001 --strip-faults` → **VIOLATION DID NOT SURVIVE**; the finding is fault-dependent, so it is the toy rather than the harness or the workload |
| **Invariant that caught it** | Linearizability of single-key reads and writes (porcupine, per key) |
| **Mutant class** | none existed — added `toy.FlawDirtyRead` in the same change, per Amendment A2 |
| **Fix commit** | *(recorded on landing; see the commit titled "BUG fixes: the dirty read and the counted acknowledgement")* |
| **Minimized?** | no — `simctl minimize` is cut to STRETCH.md until a corpus entry earns it, and this is the entry that starts earning it |

**Symptom.** `linearizability: violation — key "k03" has a non-linearizable history over 5
operations`. The client-observed history:

```
put k03=v57  called 9,945,179    never returned
get k03      at    14,736,175    -> "v57"
get k03      at 2,971,440,735    -> ""
get k03      at 3,396,737,513    -> ""
```

A read observed `v57`, and later reads of the same key observed the pre-write value. Once a read has
seen a write, no later read may unsee it, whatever happened to the operation that wrote it.

**Root cause.** The toy served reads straight from `model.DB.Get`, which returns the engine's
*visible* state — and visible state includes a write that has been applied but is neither durable,
nor replicated, nor acknowledged to any client. The put at 9.9ms was applied to the primary's
memtable immediately, so the read at 14.7ms saw it. The primary then crashed before its own fsync
completed, `db.Crash()` discarded everything past the durability watermark, and the write was gone.
The client had never been told the put succeeded, so the operation itself is legitimately "may or may
not have happened" — but the *read* that observed it is not, and it is what makes the history
unlinearizable.

In one line: the toy was performing a dirty read of its own uncommitted state.

**Why the checkers caught it here and not earlier.** It needed the primary to crash *and come back*,
because the disappearance is only observable on a later read. The reactive restart rule had never
fired: `Trigger` counted `Times` per condition rather than per rule, so the crash rule consumed the
budget and the restart rule beside it never ran. Fixing that made the restart real, and this defect
surfaced on the next sweep. It had been latent and unreachable for the whole of steps 5 through 8.

**What this would have caused in production.** A read-your-writes violation with no crash required to
*explain* it afterwards: a client reads a value, a node restarts, and the value is gone with the
cluster reporting perfect health. In the real system this is the same class as serving reads from an
uncommitted Raft log entry — the entry can still be overwritten by a new leader, so the read was of
something that never happened. It is silent, and it surfaces to a user as "the system lost my write"
long after the log has rolled.

**Fix.** Reads are served from a `committed` view rather than from engine-visible state. A key becomes
readable at the moment the write to it is real *from that replica's point of view*: acknowledged, on
the primary; applied and durable, on a backup. `Crash()` rebuilds the view from what the engine
actually kept, which is what keeps `FlawAckBeforeSync` observable — there the acknowledgement runs
ahead of durability, so the rebuild takes the value back and a later read sees the old one.

The narrower fix that would also have made seed 103 pass is to serve reads only when the node has no
in-flight writes. That is wrong: it makes the bug depend on concurrency rather than on commit state,
so a slower workload would hide it, and the same dirty read would return the moment two puts
overlapped.

---

### TOY-002 — a promoted replica was missing a write the client had been told succeeded

| field | value |
|---|---|
| **Found by** | sim — 1000-seed sweep of the *correct* toy under failover, with no flaw planted |
| **Phase** | A0 (checklist step 7) |
| **Reproduce (seed)** | `simctl run --seed 153 --workload toy --flaw none --failover` at the commit that contained the bug |
| **Reproduce (plan)** | `simctl replay --bundle seeds/TOY-002` (any commit; the bundle plants `--flaw ack-counting`) |
| **Triage verdict** | `simctl replay --bundle seeds/TOY-002 --strip-faults` → **VIOLATION DID NOT SURVIVE**; fault-dependent, so the toy rather than the harness |
| **Invariant that caught it** | Linearizability of single-key reads and writes (porcupine, per key) |
| **Mutant class** | none existed — added `toy.FlawAckCounting` in the same change, per Amendment A2 |
| **Fix commit** | *(recorded on landing; same commit as TOY-001)* |
| **Minimized?** | no — same reason as TOY-001 |

**Symptom.** `linearizability: violation — key "k06" has a non-linearizable history over 2
operations`. The history is as short as a violation gets:

```
put k06=v71  called 33,833,209  returned OK at 83,833,209
get k06      at  4,439,571,040  -> ""
```

The write was acknowledged. Four seconds later the value was not there.

**Root cause.** `pending.acks` was a counter, incremented once per acknowledgement received, and the
primary answered the client when `acks >= len(peers)`. The simulated transport duplicates messages
(`DupPerMille`), so **one backup's acknowledgement, delivered twice, satisfied the quorum on its
own.** On seed 153 node 2 acked twice, node 1 never received the replicate at all, and the primary
told the client the write had succeeded with only one of its two backups holding it. The primary was
then crashed and node 1 promoted — and node 1 was the replica that did not have it.

Counting responses instead of distinct responders. It is the toy-sized version of counting votes
without checking who cast them, which is how a consensus implementation commits without a majority.

**Why the checkers caught it here and not earlier.** Three things had to line up, and the last did not
exist until this cycle: a duplicated ack (a few per mille), a crash of the primary *after* the
acknowledgement rather than before it, and **a promotion**, so a client could read from the replica
missing the write. Without failover, reads were served only by the primary, which did have the value
— the defect was real the whole time and completely invisible. This is precisely the gap
`ack-before-replicate` had been recorded against, closed for a bug nobody had planted.

**What this would have caused in production.** A lost acknowledged write on failover, with no error
anywhere: the client's write returns success, the leader dies, the new leader never had the data. This
is the single worst failure a transactional store can have, and duplicate delivery is not exotic — it
is the normal consequence of a retransmit whose original was merely slow.

**Fix.** `pending.acked` is a set of `NodeID` rather than a count, and the quorum test is
`len(acked) >= len(peers)`. Deduplication happens at the point the guarantee is defined, which is the
only place it is safe: an idempotence check further out — dropping duplicate frames at the transport —
would fix this seed and leave the real defect in place for the next path that double-delivers: a
retried RPC, a re-sent batch after a reconnect, a peer that acks twice because it applied twice.
