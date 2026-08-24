# CARRY-FORWARD.md

Obligations a phase inherits from an earlier one, with the ruling that created them.

This file exists because the alternative failed. Commitments made in a design doc's §9 are read once,
by the person who wrote them, on the day they were written. A phase boundary is exactly where an
obligation goes quiet — and this project's most expensive defects have all been things that went
quiet while looking green.

Each entry names the phase that owes it, what is owed, and where the reasoning lives. An entry is
deleted only when the thing is done, and the commit that deletes it says so.

---

## Owed by A5

**~~The `At[Index, T]` proposal.~~ DISCHARGED at A5** — attempted, and the conclusion is recorded in
DESIGN-A5 §13. Typing the answer pays only where the caller does not already hold the position, and
A5's dimension has no such site because §7's discipline pushed the timestamp into the data instead.
Carrying the position in the data and typing it into the answer solve the same problem; the first is
available whenever you control the wire format. `raft.Configuration()` remains the case that would
pay and remains a frozen-interface change — see below.

**~~Every floored mutant class carries a seeds-to-detection ceiling.~~ DISCHARGED at A5** — every
class carries both, the lane fails on either, and the numbers were re-measured under A5's shape.

---

## Owed by the phase that next has reason to touch the frozen raft interface

**`raft.Configuration()` should take an index.** DESIGN-A5 §13: it is the one site where a caller
holds a position and asks a question that does not take one, which is what made BUG-015 possible. The
fix is a frozen-interface change (DESIGN-A0 D5), so it is a **report, never an assumed
ratification**, and it is not worth opening that interface for on its own.

---

## Owed by A6

**~~One reduced-seed unthrottled garbage-collection run.~~ DISCHARGED — and A5's figure does not
reproduce.** 40 seeds solo, 49m32s: 48.8× as many collections unthrottled, zero violations. And the
figure the obligation was actually about — *detection* — measured under A6's shape at 200 seeds:
**M53 is 0 of 200 throttled and 0 of 200 unthrottled.** If the class were still 1-in-60 unthrottled,
200 seeds would have found about three. **The throttle is not what puts M53 out of reach at A6 — the
schedule mix is**, which is A2's M34 lesson again. What the pair does *not* establish is that the
throttle costs nothing; it establishes that 200 seeds cannot tell. A6-HANDOFF §4.

*Ansh, on the A5 sign-off: "put one unthrottled run on the A6 checklist at a reduced seed count, so
the number gets re-measured under A6's shape rather than inherited from A5's."*

**~~Bound the race lane, with a measurement.~~ DISCHARGED, and the answer was a third one.**
DESIGN-A6 §33, §39. The measurement said neither the seed count nor the budget moves alone: at
`RAFT_SEEDS=50` the lane did not finish inside its own 5400s budget, timing out at 90 minutes with
one test at 36m20s — about **43 s/seed instrumented** against 8.4 uninstrumented — and reported zero
data races. So the lane is **split by what it is for**, with a budget per half taken from a
measurement:

| lane | question | measured | budget |
|---|---|---|---|
| `race` (per push) | does any cross-goroutine interaction reach node state off the mailbox (Amendment A1) — every package **except** `sim/hunt` | **191 s** | **900 s**, about five times the measurement |
| `race-soak` (nightly, sharded) | the seed search: `sim/hunt` under `-race`, 200 seeds across 8 shards | ~43 s/seed instrumented, ~9 h in one process | the nightly tier |

**What is given up is recorded rather than absorbed**: the per-push lane no longer instruments the
simulator driver, so a race introduced there is caught nightly instead of on push. The alternative was
a seed count in single digits, which A1's ruling — *a few hundred simulated seeds answer this lane's
question* — does not authorise, so the scope was kept and moved to a tier that can hold it.

**And the question Ansh actually asked could not be answered, which is on the record as its own
finding** (§21.4): *"are the two races it caught historically still caught at the lower counts"*
presupposes a record of those two findings, and there is none — no BUGS.md entry, no seed, no commit.
With zero races found at 50 seeds, the lane now rests on its **structural** argument alone, and the
"has found real races twice" claim is unsupported by anything in the repository.

*Ansh, on the A5 sign-off: "Before A6 closes, report what race seed count is actually needed... If
100 catches everything 200 catches, bound it there with the measurement recorded, which is the same
discipline as the window curve. Do not guess the number."*

**Re-enable the move-racing-churn interleaving.** DESIGN-A4 §10 records it as an unexercised
interleaving: the plan separates the two membership drivers in time because a move's add and an
unrelated removal are indistinguishable in a committed log. It is the interleaving a production
cluster produces constantly.

A6 reshapes the schedule mix anyway, which is the moment to try, and the A5 sign-off says to attempt
it there. What has to be solved first is
`rebalance-safety`'s **attribution** — it cannot tell whose removal it is looking at when both
drivers are live, which is what produced 252 false violations in 300 seeds (BUG-016). The
bidirectional assertion in `TestRaftExitCriteria` fails the day the interleaving becomes reachable,
so this cannot be forgotten silently; it can only be forgotten loudly.

*Ansh, on the A4 sign-off: "Record it in DESIGN-A4 as a named unexercised interleaving with the
reason, add it to the bidirectional-gap ledger so the day it becomes exercised the record is wrong
and says so, and put it on A6's checklist as a candidate once the schedule mix is being reshaped
anyway."*

**Not attempted at A6's close, and the reason is sequencing rather than difficulty.** Re-enabling it
changes the fault mix, which moves every trace, which invalidates the exit run that closes the phase —
and the exit run had already started when the question came up. `MovesRacingChurn` reads 0 across 200
seeds and the bidirectional assertion still holds, so the record is still correct. **It carries
forward to A7**, where the term-start no-op moves every trace anyway and is therefore the next moment
the reshape is free. DESIGN-A6 §30.3.

---

## Standing, from A6

**The symmetric-apply gap.** DESIGN-A6 §13.4: an apply path wrong identically on every replica is not
caught by replay equivalence, and is covered by a list of mutant classes rather than by a mechanism.
A list is a claim, not a proof. Any BUGS.md entry of that shape which no mutant caught invalidates the
record, and the response is a new class in §13.3, not a footnote.

*Ansh, on the A6 stop: "the day a defect of this shape reaches BUGS.md without a mutant having caught
it, the record is wrong and says so."*

**Status: tested once, already.** `M61` (a rollback that leaves the version) survived its first run —
symmetric, so replay equivalence left the same version; invisible to clients, because no commit record
pointed at the orphan. It was answered with `percolator-invariants` #5 rather than a tuned test. The
list is therefore a claim under active test rather than an assertion, which is the most that can
honestly be said for it.

**Should the corpus reproduction criterion tighten?** DESIGN-A6 §16.2.
`scripts/corpus-reproduces.sh` requires the mutated replay to *differ observably* — a violation, a
panic, an error, or a diverged trace. The stricter reading is that it must produce **the finding**.
Two bundles (`BUG-003`, `BUG-008`) satisfied the first and not the second, and were re-recorded
anyway. The criterion is one line in that script and it was ruled on at A5, so it is Ansh's to
change, not something to tighten quietly.

**BUG-015's bundle is blocked on a RULING now, not on a run.** The power measurement ran and **could
not measure `M46` at all**: the probe's timeout is 3600s and `M46` declares 3,000 seeds, which at A6's
8.4 s/seed is seven hours. So the instrument the entry was waiting on cannot execute. The options are
a raised probe timeout for one class, a sharded probe (the mechanism now exists — `POWER_FROM`), or
accepting that a 1-in-3,000 class cannot carry a bundle at this cost. DESIGN-A6 §34.1.

*(The original entry, for the reasoning:)* **BUG-015's bundle is red and blocked, not retired.** DESIGN-A6 §16.2. `M46` detects at 1 in 3,000
and its finding is a refusal rather than an oracle verdict; a 300-seed search is a quarter of one
expected detection and proves nothing. The seed comes from the mutant power measurement under A6's
shape. Until then `make corpus-reproduces` is red on exactly this entry, and that is the correct
colour for it.

**BUG-021 has no bundle, and the reason is structural rather than a search that has not finished.**
One was created at BUG-022's fix so that every A6 entry would have one, and it was **deleted in the
same pass**. The corpus's arrangement is *the bundle carries the schedule, the mutant carries the
defect* — and BUG-021's defect is a **pair**: the minting tag (`M67`) and the minted restart timestamp
(`M68`), each of which leaves a tree that still refuses the collision the other allows. No single
mutant reintroduces the bug, so no bundle can name one that reproduces it. A 300-seed search under
`M67` confirmed it, and `M67`'s own header already said so in a different form: its covering test is a
**unit test in `./hlc/`**, not a sweep.

Three resolutions, none taken without a ruling: a two-half patch that is a reproduction recipe rather
than a mutant class (which muddies Amendment A2's per-class count); a bundle carrying the
`transaction-atomicity` violation with the pair applied by hand; or the entry staying bundle-less with
this reason recorded. **What is not on the list is loosening the corpus matcher** — §16.4 is the record
of what that costs.

**~~BUG-021 has no bundle.~~ DISCHARGED** — recorded at seed 69 against the `M67`+`M68` pair, found by
a sharded search over `[0,3200)`. And the premise it was blocked on was wrong: `M68` alone reproduces
it on all eight first-detecting seeds and `M67` alone on none, so the mutant-set mechanism landed for
this entry is not required by it. DESIGN-A6 §38.1.

**Two entries still have no bundle, which BUGS.md rule 2 requires.** `BUG-017` (A5) and `BUG-020` (the
harness defect). A bundle is only worth having if it reproduces, and finding a seed that does is the
expensive half.

**Corpus regeneration is a search, not a re-record.** DESIGN-A6 §16.3. Whenever the workload moves
traces, `TestEveryStoredBundleReplays` fails and the fix is to regenerate — but a schedule that no
longer reaches its defect regenerates exactly as happily as one that does, so the reproduction lane
has to be re-run afterwards and its verdict read, not assumed.

**The mutant lane's budget under A6's shape.** 36 of the 39 mutants that declare a power expectation
measure under `current`, which is now A6's shape: **14,700 seed-runs at ~3.75 s/seed, about 15
CPU-hours**, on a lane CI runs on every push. It was affordable when a seed cost 0.36 s. Either the
per-mutant seed counts are re-derived under the new cost, or the lane moves to the nightly tier — and
Amendment A2 says the choice has to keep kill-time monitored either way. Re-measure before A7 widens
the shape again.

**The transaction identity gap.** DESIGN-A6 §15.6. A transaction record is addressed by `(primary
key, start timestamp)`, which Percolator can rely on because a single TSO issues start timestamps.
Per-node HLCs do not guarantee it: two nodes can mint the identical `(wall, logical)` pair. Asserted
at zero in the exit run as `IdentityCollisions`; the day it fires the fix is the identity — a
transaction id in the record key, or the TSO fallback Amendment A6 pre-authorises — and never the
assertion.

**~~The race lane no longer fits its own budget.~~ DISCHARGED by the split** — see the entry under
*Owed by A6* above for the two lanes and the two budgets.

### RISK-1: **there is no executor.** *Named project risk, standing until a remote exists.*

Not a passing note and not a lane's problem. It is the largest standing threat to every verification
claim this repository makes, and it is recorded here in the form the A6 audit produced (DESIGN-A6
§20, §37):

> **Zero lanes run automatically in the sense the workflow means. Eight run because a hook exists.
> Fifteen are configured for a workflow that has never executed. And every mutation lane sits in the
> remembered column, because between them they cost roughly twenty CPU-hours — so a lane too
> expensive for the hook has no executor at all, and its tier is a label rather than a schedule.**

**What it has already cost, in findings rather than in worry.** Three lanes have now been found red
after running unattended — `provcheck` red across a commit, `make test` unrunnable since A1, and
`power-mutants` red from the day `M67` and `M70` landed and through the back half of a phase — and
**the mechanism that would have caught all three has never run once.** That is the risk stated as a
measurement: the detector for "a lane stopped" is itself a lane in the column that does not run.

**And it has a sibling that costs nothing to run and is switched off anyway.** `M56` carried an
opt-out claiming its class was unreachable, reasoned by analogy with another mutant and never
measured; it measures **280 of 300, first at seed 0**, and **28 of 30 under A5's own shape** — so the
claim was **false on the day it was written**, not gone stale. It stood because `power-mutants.sh`
**skips** any patch with a `power:` line — *an opt-out exempts itself from the only instrument that
could refute it.* Different mechanism, same shape: **a claim nothing re-tests.** DESIGN-A6 §42.3,
where the refutation pass and its scope problem are written down.

**Why the usual answers do not close it.** `make lane-coverage` keeps the *list* honest and cannot
make the list *run*. A pre-push hook can hold eight lanes and cannot hold fifteen CPU-hours. Moving a
lane to the nightly tier renames the problem unless something executes the nightly tier. **Nothing
inside the repository can fix this**, which is what makes it a project risk rather than an item of
work.

**The mitigations actually available, and the one that has been taken.** A lane too expensive to run
is a lane whose claims are unchecked, and the *cheap invariant over its inputs* is worth more than
the expensive measurement nobody schedules: `make power-decl` checks every mutant's power
DECLARATION for internal consistency in milliseconds and is in the hook, and on its first run it
found six inconsistent declarations including the three that had been red for half a phase (§37.2).
That pattern — **a millisecond check on the inputs of an hours-long lane** — is the only mitigation
this repository can apply to itself, and it should be applied to every remembered lane, not just this
one.

**What discharges it.** A remote, or any executor that runs the nightly and remembered tiers on a
schedule nobody has to remember. Until then: every phase that ships without one should expect to find
another lane that stopped, and the phase report should say which ones were actually run.


**~~`M62` is reachable and undetected.~~ DISCHARGED — the detector is built.** DESIGN-A6 §35.4, §40.
`resolution-only-breaks-expired-locks`: a rolled-back transaction record that nobody proposed must
have a resolve behind it carrying `Deadline < ExpireAt`. Both values already ride in the command for
D-A6-10's reason, so the oracle reads the permission out of the committed log and the decision out of
the recovered state, and shares no code with `kv.ResolveLock`. Induced directly in `raftcheck/` on
seven built cases including the exact-boundary one, then measured against `M62`.

*Ansh, on the post-A6 list: "unlike invariant 7 this one is not a remedy in search of a class, the
class is established and measured."*

**What it leaves standing.** The symmetric-apply gap itself is not closed — it is one class smaller.
`M64` (a secondary committing at its own timestamp) is now the symmetric-apply class covered by a
mutant and nothing else.

**~~`make mutant-covered` is believed to be wrong.~~ ACCEPTED and LANDED.** DESIGN-A6 §36, §36.4. The
rule is now *the FIRST line of each contiguous deleted-or-replaced run must be covered*, and both
checks were run before it landed: the original `seedClockAtLeast`-inline induction still reports
`DEAD`, and the diff of every verdict that can move was taken. The static half of that diff is a
proof rather than a sample — **48 of 61 patches have identical old and new required sets**, and the
new set is by construction a subset of the old, so nothing can move from `ok` to `DEAD`. The four
original failures the lane exists for are on two single-line patches whose sets are unchanged, so
their verdicts are unchanged by construction.

*Ansh, re-taking the ruling: "Closing braces attributed to no span, a panic message reachable only
when safety breaks, and an error return no unit test can force are not covering-test defects, and a
rule that asks M29 for a test violating state machine safety is a rule that cannot be satisfied on
that shape."*

**The lane also has a wall-clock budget now** (`COVER_BUDGET`): sixty mutants each entitled to a
`TEST_TIMEOUT` is a lane whose worst case is sixty hours. It stops between batches, names every patch
it did not reach as `UNCHECK`, and **fails** — a budget that truncated quietly would report a subset
nobody named as though it were the list.

**`read-answers-match-the-history` is designed and not built.** DESIGN-A6 §28.5b.

**The read mark is a function of the log only until A7.** DESIGN-A6 §28.6, DESIGN-A7 D-A7-5.
BUG-022's guard consults a record staged by the apply path for every `OpTxnGet`, and that is a
function of the log **because every read is a log entry**. Read index answers reads off the log. A
read served that way stages no mark, a later prewrite passes a guard with nothing to consult, and
BUG-022 returns with no error anywhere. The general form, which will recur at every optimisation that
takes work off the log:

> **A fact maintained by the apply path is a function of the log. The moment an operation is answered
> off the log, every fact that operation used to maintain becomes a fact somebody has to maintain
> somewhere else — and the place it used to live will still compile.**

*Ansh has not ruled on D-A7-5. The recommendation on the table is that read index serves the
linearizable read path and A6's snapshot reads keep their log entry.*

**~~`sim/hunt`'s `modelRecords` has no caller.~~ DISCHARGED — deleted, and then swept for.**
DESIGN-A6 §30.3, §41. `modelRecords` was found **by accident**, while adding a record kind, which
says nothing about how many more there are. So the question was asked mechanically: for every
identifier in the system packages, is there a caller anywhere in the tree including tests?

**The sweep found six more.** Three deleted:

- `kv.EncodeLockValue`, `kv.EncodeWriteValue`, `kv.EncodeTxnValue` — **the same leftover**, and their
  own doc comment said so: *"exported for the harness's model"*. They existed so `modelRecords` could
  render the model's state into engine records, and they outlived both the model and `modelRecords`.
  The **decoders** stay, and not for symmetry: a split-born range inherits records, so `recoveredStates`
  has to read what the harness did not write.
- `coordinator.resolves`/`Resolves()` — a duplicate counter incremented on the same two lines as
  `readerResolves`, and only `ReaderResolves()` is ever read.
- `raftcheck.Ledger.Rev()` — an exported accessor with no caller in any commit.

Two more deleted, on the same rule applied where it is less comfortable: `store/codec.go`'s
`encodeKV`/`decodeKV`, the serialiser from when the state machine was a Go map, whose callers went at
A5's `e8b258c` (it was `store/`'s only use of `internal/sorted`, and that import went too); and
`raftcheck.rangeLedger.holds`, **written at A2 and never called by any commit** — `git log -G` finds
no commit that added or removed a call. **Both kept their reasoning as comments where the code was**,
because a deletion that takes the reasoning with it is how the same thing gets rediscovered.

**One reported, not deleted, and it is the sharp one.** `store.Replica.TxnRefused()` is a **live**
counter with no reader, three lines below a comment that says *"Every one is asserted somewhere in the
exit run: a count nobody asserts on is decoration that looks like evidence."* Deletion is the wrong
response — a refusal count in the apply path is evidence worth having. The right one is to carry it
and assert it, as §37.3 did for `ForeignTagStarts`, and **an exit criterion is added against a
measurement, not by argument**, so it is on this list rather than in the exit run.

**A surviving mutant has three meanings.** DESIGN-A6 §25.1: no checker can see it (add the
assertion), the test goes around the path (route it through), or **the code cannot be reached**
(delete the code and the mutant). Only the third's response is deletion, and it extends Amendment
A2's rule rather than restating it.

**Three clock-dependent mechanisms are not established as exercised.** DESIGN-A6 §27.1: a
snapshot-built range whose records outrank its clock; two replicas deriving different GC marks under
skew; a snapshot read routed to a split-born range. Each needs a targeted lane, on the model of the
150% envelope lane. None is a claim that a defect exists — each is a claim that the absence of one
has not been shown.

**Does a phase's fault mix cover the phase's own mechanisms?** DESIGN-A6 §18. `cfg.Holds = 0` was
correct at A1 and wrong from A6, and nothing connected "this phase is clock-sensitive" to "this
phase's plan generates clock faults". A7 adds read index, which is not clock-sensitive; STRETCH's
leases are. Ask the question explicitly at every phase gate, because a comment that was true when it
was written is not a check.

**Three A6 decisions still have one mutant where they need two.** DESIGN-A6 §22.6b. The commit point
(*primary record exists* + *secondaries follow*), resolution (*decide on the primary's range* +
*apply on the key's*), and the uncertainty ceiling (*fixed at the first snapshot* + *learned from any
answer*). Each is one decision whose halves fail independently. Each gets its second mutant in the
same commit as the next change that touches it.

**Where an assertion goes, and what it is keyed on.** DESIGN-A6 §22.7 and §22.6, one class on two
axes. Key it on what the DATA STRUCTURE is addressed by, not on what the concept feels owned by
(`(primary, startTS)` read zero on the seed that had the collision, because a version key has no
primary in it). And put it where the FACT is observable, not inside the mechanism that produces it
(a counter inside `nowAbove` is deleted by the mutant that deletes the call to `nowAbove`). Both were
found by the same mutant surviving twice.

**The batch-boundary technique.** DESIGN-A6 §14.4. When a replica disagrees with a replay of its own
log, digest per `Ready` on the node and per entry in the replay and look for the divergence across a
**skip** in the node's indices. A7 (read index, where a lease or an index read can be answered
mid-batch) and B4 (kill points, where a kill lands inside a batch) will both want it.

---

## Owed by I2

**The undecodable-snapshot panic.** DESIGN-A4 §11 classifies it as correct today and wrong later: the
sim's transport reorders, duplicates and drops but never corrupts bytes, so an undecodable snapshot
can only be a harness or codec defect. A real transport can deliver a corrupt frame. Changing it
before a real transport exists would be guessing at the right behaviour.
