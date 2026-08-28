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

### The standing rule: a pre-ruling on a result carries a PROVENANCE CONDITION

*Ansh, after the `M73` fabrication:* **"I may pre-rule what a result means, but never before its
provenance is established, so any pre-ruling on a null now carries an explicit provenance condition —
the measurement counts only if the instrument was demonstrated to be doing the thing being
measured."**

**The instance.** A7's no-op broke three corpus bundles, and three re-pin searches were launched. The
null case was pre-ruled to save a turn:

> *"For `M71` and `M73` over 600 seeds under current: if either comes back empty, that is a stronger
> signal, because those are not rare classes. An empty result there means the no-op moved the schedule
> out of the defect's reach, which is a measurement of the no-op and belongs in the report as one."*

`M73` came back `0 of 600`. **The patch had never applied** — `sh` does not expand a glob in a
redirection, so `patch` received zero bytes, exited 0, and the search swept 600 seeds of clean code.

> **A measurement of unmutated code would have landed as a measurement of the change under test,
> wearing a ruling that authorised the interpretation.**

**This is a distinct shape, not vacuous-green.** Vacuous-green is an instrument that reports nothing
while looking like it is working. Here the instrument was fine and **the interpretation was decided
before the result existed**, which removed the step that would have caught the result being wrong:
the turn in which somebody asks *where did this number come from*.

**And the pre-ruling was wrong on its facts as well as its form.** It rested on *those are not rare
classes*; `M73`'s own declaration is **per-seed 0 of 200, sweep-detected**, so an empty per-seed search
is the expected result rather than a signal. *Not found at this budget* is the right reading.

**The rule, binding on both of us:**

> **A pre-ruling may fix what a result MEANS. It may not fix that before the result's PROVENANCE is
> established. Every pre-ruling on a null therefore carries the condition: the measurement counts only
> if the instrument was demonstrated to be doing the thing being measured.**

For a mutation search, "demonstrated" is one comparison — does the file the patch names actually
differ. That check is now in the search scripts and in `power-refute`, `power-mutants` and
`mutant-covered` (DESIGN-A6 §43.14, §43.14b, §43.14c).

### The five instances, and what each one's number looked like

The rule generalises past nulls, and the register of instances is the argument for it. Every one is a
number, or a state, quoted from a source whose provenance had not been checked:

| # | what was believed | what was true | how it surfaced |
|---|---|---|---|
| 1 | `M73` measured 0 of 600 with the mutation applied | `sh` does not expand a glob in a redirection, so `patch` got **zero bytes** and the sweep measured CLEAN code | a sibling in the same loop printed a visible patch failure |
| 2 | `M76` caught 0 of **12** | the format string said 12; the run was **20** seeds | the seed bound in the file disagreed with the printed denominator |
| 3 | a "clean tree" baseline at 40 seeds | `cd` persisted, so the baseline ran **inside the mutated tree** | the two logs were byte-identical |
| 4 | `M76`/`M77` are unmeasurable — `ERROR`, no output | `copy_tree` tars the LIVE tree and it was being edited; both build fine on a stable one | they applied and built when retried |
| 5 | **the exit run had been running for two hours** | `exit-run.sh` had **refused at launch** — dirty tree — and nothing had run | the process list was checked, on a direct question |

**Instance 5 is the one that changes the rule**, for two reasons:

1. **The lane refused for exactly the reason that had just been written into the power lanes** — *an
   exit run at an uncommitted tree names a commit that does not contain what ran.* The rule already
   existed where it was needed and was already enforced. **The gap was not checking the refusal.**
2. **The failure mode was two hours of believing a measurement was in flight.** Nothing was wrong with
   the tree, the lane, or the rule. What was wrong was a report of *started* derived from having typed
   the launch.

> **"Started" is read from the process, never from the launch.** A backgrounded command that refuses
> immediately is indistinguishable, from the launcher's side, from one that ran all night.

Applied: any report of a long-running job's status quotes `pgrep`/the log, and `exit-run.sh` now prints
the shape it swept — derived via `cmd/shapename` from the options struct rather than written beside it,
because that banner said "A6 exit run" over A7's sweep. **Third instance of a label that stopped
describing its subject**, after `power-config: a3` and the single-label opt-out, and each was cosmetic
on the day it was written. `TestTheShapeNameTracksTheShape` asserts the derivation and is induced.

### The standing rule: the frozen interface opens ONCE, and this is the change it opens for

*Ansh, ruling D-A7-6:* **the frozen interface opens once, for `raft.Configuration()` taking an index,
which is the site that made BUG-015 possible. That is a change with a defect behind it. Anything else
that wants the interface opened waits and rides with it, and a request to open it for convenience is
refused.**

The test is not how small the change is. It is **whether a defect is behind it**: `Configuration()`
qualifies because BUG-015 happened at that exact site — a caller holding a position, asking a question
that does not take one. A new `EntryType` for the term-start no-op does not qualify, and it is the
better case for the rule precisely because it was *reasonable*: typed, idiomatic, and the survey still
found it bought a name rather than a behaviour.

**And A7 declined to open it for a smaller reason, which is the rule working.** DESIGN-A7 §3a
(D-A7-6): the term-start no-op could be a new `EntryType`, which is the typed answer this project
usually prefers — and `Entry` rides in `Ready`, so it is the same frozen interface. The recommendation
is the untyped one (`EntryNormal`, empty `Data`, the zero `ProposalID`) **partly in order to leave the
interface shut**, so that when it is opened it is opened for the change that has been waiting since
A5 rather than for a no-op. If Ansh rules the other way, the two changes should land together.

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

**And A7's ruling 6 constrains HOW it lands, which is worth recording before somebody takes the
obvious shortcut.** *"The no-op lands separately and first, with a full re-measurement — one reason
per moved number."* The reshape moves every trace too, so riding it on the no-op commit is exactly the
shape the ruling forbids: one commit, every count moved, two causes, and a power regression that
cannot be attributed to either. **The reshape is its own commit with its own re-measurement**, before
or after the no-op, and never inside it.

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

**RULED into A7's exit criteria** (DESIGN-A7 §8.2 criterion 4): every floored class is re-measured
under A7's shape rather than inherited from A6's, and the refutation pass is run and reported beside
it. The term-start no-op is what widens the shape, so the re-measurement is taken against the tree the
no-op produces, not against this one.

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

### RISK-1's demonstration: one lane, four silent breakages, none found by anything that looks

*Recorded here as the risk's demonstration rather than as four entries beside it, because the pattern
is the argument and the individual defects are not.* Every one of these is `make power-mutants` — the
lane whose entire purpose is to notice when detection power drops:

| # | what was broken | landed | found | how |
|---|---|---|---|---|
| 1 | `TestPowerProbe`'s `noticed()` consulted a hand-listed subset of the detectors, so **no class with an aggregate detector could be measured at all** — and zero was read as unreachable | `c1a1f6c`, 2026-08-18 | `d8589a9`, 2026-08-23 | chasing `M62`'s zero (§35.1) |
| 2 | `M67` and `M70` declared a **sweep** floor over a class killed by a **unit test**, a claim the probe can never satisfy — the lane was RED and shouting into an empty room | `f26435d`, 2026-08-22 | `ba9df9d`, 2026-08-23 | *while making the lane affordable, not while looking for it* (§31) |
| 3 | the sweep detector **could not fire in `POWER_JOBS = 1`** — the lane's default mode — because the sequential path never wrote the file the detector reads. `M68` and `M73` reported BLIND throughout | `d8589a9`, 2026-08-23 | 2026-08-24 | applying `M67`'s ruled disposition (§43.9d) |
| 4 | `status=$(cut -f1 file)` read **every** line, so any class whose probe emitted a sweep line came back as `ERROR -- the probe produced no measurement`. **`POWER_JOBS > 1` could not report a pass for essentially any class** | `d8589a9`, 2026-08-23 | 2026-08-24 | instrumenting, after three wrong guesses (§43.9e) |

> **Four distinct silent breakages in one lane. Not one was found by anything that looks — each was
> found by a person who happened to be doing something else nearby.** Two of them landed in the same
> commit and neither was noticed by the other's fix. And 3 and 4 together mean the lane was, for the
> whole post-A6 measurement cycle, **unable to return a verdict in either configuration**: sequential
> could not fire the detector, parallel could not report a pass.

**That is RISK-1 stated as a measurement rather than as a worry.** The claim was *a lane too expensive
for the hook has no executor at all, so its tier is a label rather than a schedule.* The demonstration
is that the label held for four consecutive defects in the one lane whose job was to notice things
going quiet. **A lane nobody runs cannot tell you it has stopped working**, and the corollary the
fourth entry adds is sharper: it cannot tell you it has stopped working *even while you are actively
reading its output*, because what you are reading is the output of a lane that cannot fail.

**What this does NOT change.** Nothing here is closable from inside the repository — §37's mitigation
stands and is the only one available: *a millisecond check on the inputs of an hours-long lane*, which
is why `power-decl` and `power-refute-decl` are in the hook and the measured halves are not. What the
four entries add is the honest size of what that mitigation does not cover: **it checks the lane's
inputs and nothing checks the lane.**

**And it has a sibling that costs nothing to run and is switched off anyway.** `M56` carried an
opt-out claiming its class was unreachable, reasoned by analogy with another mutant and never
measured; it measures **280 of 300, first at seed 0**, and **28 of 30 under A5's own shape** — so the
claim was **false on the day it was written**, not gone stale. It stood because `power-mutants.sh`
**skips** any patch with a `power:` line — *an opt-out exempts itself from the only instrument that
could refute it.* Different mechanism, same shape: **a claim nothing re-tests.** DESIGN-A6 §42.3,
where the refutation pass and its scope problem are written down.

**The sibling is now closed, and it closed differently from its parent.** `scripts/power-refute.sh`
is built, scoped and reported (DESIGN-A6 §43), and the label that made the claim possible is **split**:
`power-covered-by:` names an instrument the pass RUNS, `power-unreachable:` names the detector its
number was taken against and must argue NO OTHER DETECTOR, and the bare `power:` is retired. **Three
of the five reachability claims that can be measured turned out to be wrong** — `M30` at 1 of 300 with
a leader-completeness violation, `M67` at 589 foreign-tag starts in thirty seeds, and `M56` before
them. RISK-1 itself is *unclosable from inside the
repository* — nothing in the tree can make a lane execute. The opt-out sibling was closable from
inside, because what switched the instrument off was a sentence in an artefact rather than the
absence of a machine. **That is the distinction to carry: a lane nobody runs needs an executor; a
claim that exempts itself needs a rule that the exemption be earned by a fact rather than granted by
a sentence.** The second is buildable and has been built. The first is still RISK-1.

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

## Owed by A7

Created by the thirteen rulings on DESIGN-A7. Each names the ruling that made it.

**The three-guard totality argument, restated under read index.** Ruling 13. A6 §28.3's argument names
a log position in every clause — *before the prewrite*, *after the prewrite*, *blocks on the lock* —
and read index answers reads that occupy none, so **the proof expired with its premises**. The
restatement is an exit criterion rather than a paragraph, and `M71`/`M72` are re-induced **against the
restated form** rather than re-run against the old one and observed to still pass.

*Ansh: "a mutant that passes because the property it attacks moved has stopped meaning anything."*

**The oracle for ruling 3's condition, and the defect that induces it.** *A read arriving at index `i`
and confirmed later must be provably not answerable at any index below `i`.* Arrival capture is the
weaker of D-A7-3's two options by construction, so the safety of the cheaper choice **is** that claim,
and it is not allowed to live in prose. A ledger-side invariant, plus a planted defect that serves a
read one index low.

**`M71` re-pointed, with the boundary planted as the defect.** Ruling 11. The mutant is *a snapshot
read is served by read index* — ruling 8's decision applied as a patch — killed by a **conservation
failure** rather than a structural check, because that is what BUG-022 looked like. If the boundary
between the two read paths can be erased by simplifying a conditional, it is a comment; if erasing it
fails a run, it is a mechanism.

**A counted plain-read census field, before any number reaches BENCHMARKS.md.** Ruling 9. §4.2's
one-in-ten is **derived** from a configured ratio (`SnapshotReads / 0.4`), not counted. And
BENCHMARKS.md states A7 in the ruled terms — *the correctness mechanism CLAIM 4 names, with a
throughput win on one read in ten* — carrying both qualifications, the derivation and the fact that
the mix is this workload's, whose audits are a checker reading rather than a client.

**The fate of the replicated read path, decided at exit and recorded.** Ruling 4. A fallback nobody
decided to keep is a second code path with no owner.

**Non-vacuity for the two things this phase adds.** Rulings 2 and 5: a sweep in which every read was
served by a leader has not tested follower reads, and a differential that compared nothing is this
register's commonest entry. Both carry a count.

**Standing from A7, not owed by it: the assumption audit.** Ruling 10. Both audits are reported at
every phase close from A7 onward — the fact table asks *where is this fact taken from*, the assumption
audit asks *what does this mechanism's correctness argument assume, and does this system provide it*.
**They fail differently, which is why there are two.** A6's fact table came out clean and the phase's
most expensive defect was in the column the table has no room for.

---

## Standing, from A7's refutation pass

**~~`M30` and `M67` are RED and their dispositions are unruled.~~ DISCHARGED — ruled and taken.**
DESIGN-A6 §43.12. `M73`'s precedent for both, and they diverge on how each measured: `M67` →
`power-detector: sweep` at 30 seeds naming **`ForeignTagStarts`** (no per-seed rate, and 589 against a
criterion asserted at exactly zero across the signed 25,000-seed run — the margin is in the magnitude,
not the seed count); `M30` → **out of the exempt column entirely** and into a floored class,
`power-seeds: 300 / power-floor: 1 / power-ceiling: 300`, measured from the 300-seed number.

*Ansh, ruling: "a class with a confirmed first-tier safety detection has no business in either exempt
column."*

**Verified green, and the lane returned a verdict for the first time in its history.**
`ok M30 — 1 of 300 (floor 1), first=178 (ceiling 300)`; `sweep M67 — 589 foreign-tag starts against a
baseline that passes`; `0 failures`. Reaching that required fixing two defects in `power-mutants`
(DESIGN-A6 §43.9d, §43.9e) — see RISK-1's demonstration above.

**But only two of sixty-one classes were run.** The full lane has NEVER completed under a working
reader: every class whose probe emits a sweep line was unmeasurable through the gating path for the
whole post-A6 cycle, so those verdicts are **unknown rather than green**. ~15 CPU-hours, in the column
with no executor. The first complete run of `power-mutants` is now worth more than it has ever been
and there is still nothing that will run it.

**What is carried rather than closed: `M30` has no kill-time signal separate from its rate.** At 1
detection in 300, floor and ceiling say the same thing — *detected at all* — which is `floors.go`'s
documented answer for a class too weak to carry a rate. The day the rate rises is the day this class
can carry a real ceiling, and Amendment A2 wants one. Declaring a wider sweep now to manufacture the
separation would be declaring a seed count nobody measured.

**Three classes could leave the unmeasurable-here column for one census field.** DESIGN-A6 §43.6. `M8`, `M9` and
`M15` are exempt because the only thing their mutations move is the `Inconclusive` count, which the
mutated lines write. One field — decided-and-total operations per run, recorded where the history is
ASSEMBLED rather than where it is scored — is independent of all three and would convert them from
*exempt with an argument* into *measured*. It adds a census field and an exit criterion, so it is not
a change to make quietly.

**A pinned class is measured every run, under a shape no run uses.** DESIGN-A6 §43.11. `M18` (`a1`),
`M34` (`a2`) and `M14` (`a3`) pin themselves with `power-config:`. This is a *lesser* sibling of the
opt-out: the pin is honest — the report prints `(a1)` on the line — and the class is re-measured. But
*is this class still reachable by the sweep this project actually executes* is a question nothing
asks. Whether a pin also owes a current-shape number, even one that reads zero, is a ruling.

**The pass cannot tell a hard-zero exit criterion from a non-vacuity one.** DESIGN-A6 §43.12 item 4.
`ForeignTagStarts != 0` is asserted at exactly zero across the signed 25,000-seed run; *no move ever
completed* is marginal at thirty seeds on the clean tree as well. Both reach the pass as "a criterion
the baseline passes", so it prints the weaker caution for both and understates the strong case.
Distinguishing them means the criteria declaring their own kind, which is a change to the
exit-criteria list.

**The refutation pass cannot refute its own unmeasurable-here column, at any seed count.** That is its ceiling,
stated so it is never quoted past it: it converts *nothing re-tests this claim* into *this claim is
re-tested, or it is exempt for a written reason the artefact earns*. It does not convert the second
into the first.

---

## Owed by I2

**The undecodable-snapshot panic.** DESIGN-A4 §11 classifies it as correct today and wrong later: the
sim's transport reorders, duplicates and drops but never corrupts bytes, so an undecodable snapshot
can only be a harness or codec defect. A real transport can deliver a corrupt frame. Changing it
before a real transport exists would be guessing at the right behaviour.

---

## M34's disposition and BUG-009's re-pin are ONE piece of work

**Owed out of A7, 2026-08-26. Track A does not exit while `make corpus-reproduces` is red — Ansh,
holding this open explicitly.**

Two items have been tracked separately and they are the same search:

- **`M34-append-from-zero-over-a-snapshot` has no measured disposition.** Its floor is unmet by two
  independent instruments — 0 of 3,000 in the gating lane, 0 of 6,000 in BUG-009's re-pin search —
  and its measurement under A7's shape was launched during the exit run and deliberately killed for
  competing with it. The standing rule binds the result: *an exclusion from a measurement pass may
  cite a measurement or an argument about reachability, **never** the excluded class's own
  declaration.*
- **`seeds/BUG-009` is STALE**: it now replays **identically** with `M34` applied, so the bundle no
  longer carries its finding. It was WEAK before this session and the re-record at `6c43023` moved it
  the last step.

**They are one job because the class BUG-009 tests with is the class whose floor is unmet.** A seed at
which `M34` makes a difference is simultaneously (a) BUG-009's new pin and (b) the first evidence
`M34` has produced in two instruments' worth of searching. Finding one settles both; finding none
settles neither, because — Ansh's standing instruction — **a search that finds nothing is not a
verdict that there is nothing.**

`seeds/BUG-015` needs the same treatment under `M46-split-inherits-the-appended-configuration`, and it
has no second question riding on it.

**The precedent for how this is done is BUG-024, this phase**: a sharded search found a reproducing
seed at 1 in 8,400, the bundle was re-pinned 10303 → 5042 **in the commit that found it**, and
`corpus-reproduces` confirmed the new pin exercises its defect rather than being assumed to.

**And §5e.2b now adds a step before any opt-out.** A zero from a mutant can mean the claim is aimed at
the wrong point rather than at the wrong line — M79 and M80 both read zero on their first attempt for
that reason. So before `M34`'s two zeros are read as reach, its **aim** is varied: it is an
`append-from-zero-over-a-snapshot` class, and the questions are which role appends, and at which point
relative to the install.

---

## Owed out of A7's BUG-009 re-pin

**M34's declaration is stale in two independent ways**, both found while re-pinning BUG-009 and
neither a reason to hold the bundle, which now reproduces at seed 155.

1. **Its floor names a shape in which the class cannot occur.** `power-config: a2`, and under `a2`
   the precondition — an append from index 1 arriving at a node whose prefix is *already* in a
   snapshot — occurred **0 times in 200 seeds**, against 5,051 appends-from-index-1 that did not meet
   the ordering. Under A7's shape it occurs **once in 200 seeds**. The declaration should name A7's
   shape with that rate, and the measurement it currently cites (`2 of 3000 … at commit A5-close`)
   predates the term-start no-op, which moved every trace.
2. **Its covering test does not kill it.** `TestSnapshotPrefixIsNotOverwritten` **passes with `M34`
   applied at HEAD**, so the class is `expect: killed` and survives. That is a covering-test failure,
   not a floor failure, and it is the axis `make mutant-covered` is blind to here — the test executes
   the line, and the line no longer produces an observable difference on the seeds it sweeps.

**`M46` is owed the same two checks** and its declaration carries the same staleness marker
(`1 of 3000, first at seed 215, under current at commit A5-close`). Its covering test,
`TestSplitInheritsTheConfigurationAtItsIndex`, is a 1,000-seed serial sweep that takes over an hour,
which is why it has not been re-run inside a phase.

**And `RaftMeta` needs the treatment `send()` got.** BUG-034 is the fourth time an option was added
to the shape and not to the thing that pins the shape, and the third time the struct's own comment
predicted it. The fix that worked for BUG-027 was to **invert the default so the compiler asks the
question**; the equivalent here is a check that every field of `hunt.RaftOptions` is either carried by
`RaftMeta` or named in an exemption list with a reason — the same shape as
`TestAddCensusCoversEveryField`, which exists because a counter added to one place and not the other
reads low. **A bundle records the shape its writer knows how to write down, and nothing today asks
whether the writer knows the whole shape.**

---

## BUG-015 is OPEN, and the next step is not another search

**2026-08-27.** `make corpus-reproduces` reports **20 checked, 4 skipped, 1 failure**, and the failure
is `seeds/BUG-015`. BUG-009 is resolved at seed 155; this one is not, and it is **not retired on a
null** — the search is recorded in its BUGS.md entry with all four axes.

**What was measured:** the precondition occurs (8 in 200 seeds under `current`, 25 in 200 under `a4`),
detection does not (0 of 400 under `current`, 0 of 200 under `a4`, 0 at the two seeds where the
precondition demonstrably fired). The gap is between precondition and consequence: a divergent birth
configuration only becomes a defect when the **new** range subsequently receives a membership entry
the behind replica reads as illegal.

**So the next step is a workload change, not more seeds.** No shape in the tree aims a configuration
change at a range that was split out moments earlier; every one of them targets the cluster and lets
ranges appear underneath. Three candidates, cheapest first:

1. **A directed test** that arranges both halves — a split applied with a conf entry appended above
   it, then a membership change on the range that split produced. This is what `M46` should be
   floored against, and it makes the class a *test* rather than a *search*.
2. **A schedule-generator option** that targets a recently-created range with a conf change, declared
   the way `floors.go` requires — a schedule mix is a claim about reachability, so adding one changes
   which defects the sweep can find and must say so.
3. **A wider search**, which is the option this record exists to argue against: 600 seeds across two
   shapes returned nothing while the precondition fired sixteen times, so more seeds buy more of the
   precondition and none of the consequence.

**BOTH BLOCKERS BELOW ARE NOW GONE, and the paragraph stood for a day after they went.** Corrected
2026-08-27 at the A7/B5 merge, when the lane was run and the entry was read beside it.

> ~~*And the covering test cannot be used to check any of this as it stands.*
> `TestSplitInheritsTheConfigurationAtItsIndex` *is a 1,000-seed serial sweep, over an hour, and*
> `scripts/mutant-covered.sh` *has no per-mutant filter — so the code-position axis costs a full
> suite run to ask.*~~

- **M46's covering test is directed now.** `TestASplitDoesNotInheritAnUnappliedConfiguration`,
  `store/splitconf_test.go:39` — which is *this entry's own option 1*, "a directed test that arranges
  both halves… this is what `M46` should be floored against." It was written during A7's eight-test
  conversion and this entry was not told. The old sweep still exists at `sim/hunt/oracles_test.go:525`
  and is no longer `M46`'s declared covering test.
- **`scripts/mutant-covered.sh` has `ONLY`.** Space-separated, documented in the Makefile help.

**What is still true is the part that matters:** `seeds/BUG-015` is STALE — it replays identically
with `M46` applied — and the next step is a workload change rather than more seeds. The measurement
above stands. **What changed is that it is now cheap to ask**, which is precisely what the struck
paragraph said it was not.

**And this entry is the third instance in one day of a class worth naming.** A `notAnalysed` entry
whose reason had gone false; a `corpus-reproduces` count that no longer equalled its population; this
blocker paragraph. See `BUGS.md` **GF-42**, and the `BLOCKER` declaration below, which is the cheap
mechanism that would have caught this one.

**Track A does not exit while `corpus-reproduces` is red.**

---

## The lane filter, and what it cost to not have one

**2026-08-27, on Ansh's instruction.** `scripts/mutant-covered.sh` **already had** an `ONLY` filter —
positional `$1`, single exact name, undocumented, mentioned in no Makefile help. So it existed and
nobody found it, and the code-position axis of §5e.2b was recorded as *"unresolvable, because asking
one question costs a full suite run"*. That was true of the lane **as anybody could discover how to
use it**, which is the only sense that matters.

It now matches Track B's `cpp-mutants` convention exactly — a space-separated list matched by id,
settable as `ONLY=` or as a positional argument, with the invocation in the Makefile's help:

```
make mutant-covered ONLY="M46-split-inherits-the-appended-configuration"
```

> **With no CI, a lane's cost is a fact about whether it gets run at all.** A covering test nobody can
> afford to run is not a covering test, and a lane that turns one question into sixty is a lane that
> gets skipped and then trusted.

**Measured, on the question that was blocked:** `M46`'s code-position axis went from *over an hour and
never taken* to **2 seconds, ok**. Two changes were needed and neither alone was enough — the filter,
and a covering test that is not itself a 1,000-seed serial sweep.

**Still owed:** `scripts/mutants.sh` and `scripts/power-mutants.sh` have no filter at all. The same
reasoning applies to both and neither was in scope for this instruction; recorded rather than done.

---

## OBLIGATION: eight sweep-based covering tests remain, due at the phase that next touches their classes

**Raised A7, 2026-08-27. Ansh: record it as a named obligation with the count and the reason, due at
whichever phase next touches those classes, rather than as a general to-do.**

Nine covering tests were converted from seed sweeps to directed tests this phase, which took
`make mutants` from **six days to about 2.7 hours** and from `INVALID` to runnable. **Eight sweeping
covering tests remain**, and they are the whole of the residual cost:

| covering test | seeds | classes it covers |
|---|---|---|
| `TestFailoverDoesNotManufactureViolations` | 1,000 | A1 failover |
| `TestDurableRecordAgreesWithTheEngine` | 300 | A1 durability |
| `TestConfigurationSurvivesRecovery` | 200 | A3 membership recovery |
| `TestStaleDurabilityCompletionIsRefused` | 200 | A1 epoch guard |
| `TestClientHistoryIsLinearizable` | 100 | A1 linearizability |
| `TestRestartsMintTheirOwnStartTimestamp` | 64 | A6 identity |
| `TestToySurvivesOneThousandSeeds` | 64 | A0 toy |
| `TestSnapshotEquivalenceOracleReportsNothing` | (sweep) | A2, six classes |
| **total** | **~1,928 seeds ≈ 2.7 h per baseline pass** | |


### CORRECTION, 2026-08-28: the table above is wrong in three ways, and the total is understated by about 3×

**Found while chunking `mutant-covered` around the runner's job ceiling**, which forced the question
*which classes are actually expensive?* — and the answer had to be derived from the source rather than
read off this table.

**One: a number contradicted by the test's own name.**

| | table | source |
|---|---|---|
| `TestToySurvivesOneThousandSeeds` | 64 | **1,000** (`sim/hunt/hunt_test.go:71`, `const seeds int64 = 1000`) |

Sixteen times larger, in a row whose test is *named* for the count. Five of the other six numbered rows
are exactly right (1000, 300, 200, 200, 100), and `TestRestartsMintTheirOwnStartTimestamp` is **60**
rather than 64 (`boundSeeds(60)`), which is a rounding rather than an error.

**Two: `(sweep)` hid the cheapest entry, not an expensive one.** `TestSnapshotEquivalenceOracleReportsNothing`
is **50 seeds**. It is listed without a number and credited with *"A2, six classes"*, which reads as the
heaviest row in the table. It is the lightest.

**Three, and this is the one that matters: an entire family of sweeps is missing.** The table counts
only tests that sweep with a local `const seeds` loop. It does not count the `assertOracleSilent`
family at all — **thirteen** covering tests over **24** classes, verified by extracting every
`assertOracleSilent(t, "...", N)` call with its enclosing function:

| seeds | classes | covering test |
|---|---|---|
| **2,000** | 1 | `TestLeaderCompletenessOracleReportsNothing` |
| 500 | 3 | `TestPersistBeforeReplyOracleReportsNothing` |
| 500 | 1 | `TestLogMatchingOracleReportsNothing` |
| 100 | 2 | `TestStateMachineSafetyOracleReportsNothing` |
| 100 | 1 | `TestSingleServerChangeOracleReportsNothing` |
| 60 | 2 | `TestPercolatorInvariantsReportNothing` |
| 60 | 2 | `TestMVCCReadCorrectnessOracleReportsNothing` |
| 60 | 1 | `TestTransactionAtomicityOracleReportsNothing` |
| 60 | 1 | `TestSplitPartitionOracleReportsNothing` |
| 60 | 1 | `TestRebalanceSafetyOracleReportsNothing` |
| 50 | 6 | `TestSnapshotEquivalenceOracleReportsNothing` *(the table's one entry from this family)* |
| 50 | 2 | `TestElectionSafetyOracleReportsNothing` |
| 50 | 1 | `TestApplyContinuityOracleReportsNothing` |

**`TestLeaderCompletenessOracleReportsNothing` runs 2,000 seeds — twice the largest row in the table —
and does not appear in it.**

**The corrected totals:**

| | recorded | derived |
|---|---|---|
| sweep-backed covering tests | **8** | **~20** |
| seeds, once per test (`mutants` baseline) | **~1,928** | **~6,450** |

**AND THE ENTRY ALREADY CONTAINED THE EVIDENCE THAT ITS OWN TABLE WAS WRONG.** Three paragraphs below
it: *"`make mutants`' baseline — the 52 covering tests in one `go test` binary — still exceeded two
hours of monotonic time and died on its own timeout."* That was recorded as *firmer than the estimate
above*, and it is — it is also **inconsistent with** the estimate above, and nobody reconciled them. At
1,928 seeds a two-hour death is a puzzle. At ~6,450 it is arithmetic.

> **A MEASUREMENT THAT CONTRADICTS THE TABLE IT SITS BESIDE DOES NOT CORRECT THE TABLE BY BEING TRUE.
> SOMEBODY HAS TO NOTICE THEY DISAGREE.**

That is `GF-42` again — a written reason outliving its truth — but sharper, because here the refuting
evidence was **already in the same entry**, added later by the same author, and the table above it was
left standing.

**What this changes about the obligation, which is the point of correcting it.** *"Eight sweeping
covering tests remain, and they are the whole of the residual cost"* is false. Converting all eight
would leave the **2,000-seed** `TestLeaderCompletenessOracleReportsNothing` and twelve more sweeps
untouched — the majority of the cost. **Whoever discharges this should start from the derived table,
not from the eight**, and the cheapest single win is the 2,000-seed leader-completeness sweep, which
covers exactly one class.

**The limit of this derivation, stated so it is not over-trusted.** It finds two mechanisms: a local
`const seeds` loop and the `assertOracleSilent` family. A third sweeping idiom, if one exists, would be
missed the same way the second was — which is exactly how the original table came to be wrong. The
scan is `assertOracleSilent(?:With)?\(\s*t\s*,\s*"[^"]*"\s*,\s*(\d+)` plus `const seeds`, run line by
line with the enclosing function tracked, and it was sanity-checked against three known values before
being trusted. **A first version of this derivation was wrong** — a misplaced `?` in the regex made it
report one sweep instead of thirteen — and it was caught only because two scripts written minutes apart
disagreed. The corrected numbers are the ones that survived that disagreement.

**The framing is the Makefile header's own sentence:** *the cost is driven by the number of sweeping
tests, not by any one bound.* No value of `RAFT_SEEDS` fixes this; only converting the tests does.

**MEASURED, 2026-08-27, and it is firmer than the estimate above.** With all nine A7 conversions in
place, `make mutants`' baseline — the 52 covering tests in one `go test` binary — **still exceeded two
hours of monotonic time and died on its own timeout**: `panic: test timed out after 2h0m0s`. That is
the baseline alone, before any of the 70 per-mutant runs.

**TWO INDEPENDENT MEASURED NUMBERS, from two lanes, by different routes.** This is not an estimate that
grew:

| lane | what the eight cost it |
|---|---|
| `make mutants` | the baseline — 52 covering tests in one binary — exceeded **two hours of monotonic time** and died on its own timeout, before any of the 70 per-mutant runs |
| `make mutant-covered` | **10 of 71 classes in 52 minutes**, projecting **6.1 h against a 6.0 h budget** |

**MEASURED 2026-08-28, and it changes the SHAPE of the obligation rather than its size.** The entry
above frames the cost as a total — 6.1 h against a 6.0 h budget — as though the question were whether
anyone can afford an afternoon. It is not. `make mutant-covered` was run twice as a single background
job on the merged tree and **terminated by the runner both times**: once at `M19`, once after
**1h35m53s** having completed **14 of 71** classes, every one of them `ok`. `make cpp-ci`, at roughly
25 minutes, completes.

> **A LANE WHOSE COST EXCEEDS THE RUNNER'S PER-JOB LIFETIME IS NOT AN EXPENSIVE LANE. IT IS AN
> UNRUNNABLE ONE, AND THE TWO LOOK IDENTICAL IN A BUDGET.**

**The remedy is the filter, and this is what it was added for.** `ONLY=` takes a space-separated list,
so the 71 classes run as six chunks of ten, each finishing well inside the observed ceiling. That makes
the lane *runnable* without making it *cheap*: it still costs about six hours of wall clock, and it now
costs six deliberate invocations rather than one. **Whoever discharges the eight-test obligation should
know that converting them shrinks a cost that is currently paid in a currency the budget does not
name.**

**`mutant-covered` pays more than `mutants` for the same eight**, and the reason tells whoever
discharges this which lane recovers most: it runs every covering test **under coverage
instrumentation**, so a sweep costs more there than it does anywhere else. Converting the eight
recovers more from `mutant-covered` than from `mutants`, and both recover.

> **So this obligation is not a tidiness item.** The lane is in the `ci` target and cannot pass at any
> timeout anybody would put in CI, and the eight remaining sweeps are the whole of the reason. The
> script's own sanctioned remedy is *more time, never shorter sweeps* — which is correct as far as it
> goes and does not scale: the honest fix is the conversion, and it is due at whichever phase next
> touches each class.

**Why it is NOT being done now, and this is the reason rather than the excuse.** These belong to A1
through A6 classes whose defects would have to be understood properly to write an honest directed
replacement — a directed test asserts a specific mechanism, and one written from a patch header
without understanding the defect will assert the wrong thing and pass. **Improvising that at a phase's
end is how a covering test comes to name the wrong assertion**, which is precisely the failure mode
this phase spent itself finding in `M34` and `M46`.

**Due:** at whichever phase next touches each class, converted alongside work that already requires
understanding the defect. **Not** as a batch, and **not** by anybody reading only the patch header.

**The standard each replacement must meet**, from the nine written this phase:

1. it arranges the precondition directly rather than hoping a schedule produces it;
2. it **asserts that it arranged it** — every one of the nine caught a construction error of its
   author's before the real assertion could pass over nothing;
3. `make mutant-covered ONLY="<id>"` reports `ok`, which is a different question from *does it kill*
   and caught four defects in this phase's own tests.

---

## A per-seed rate does not compose into a suite time when the suite shares a process

**A7, 2026-08-27, and it is the same author making the same class of estimate error a second time.**

`make mutants`' baseline was estimated at **2.7 hours** from ~1,928 seeds at ~5 s/seed — a per-seed
rate measured on a quiet machine, multiplied by a seed count. The baseline is **52 covering tests
sharing one `go test` binary**, running their sweeps at `NumCPU` workers, at load average 20.9.

> **A per-seed rate is a property of one seed run alone. It does not compose into a suite time when
> the suite shares a process**, because the thing that made the rate what it was — a free machine — is
> exactly what the suite removes.

**This is the shard-count-is-not-parallelism finding one level over.** There, the shard count was
taken to be the machine's parallelism, and eight shards on eleven cores produced a load average of
21 — *every wall-clock estimate derived from the shard count was wrong, not by arithmetic but by
multiplying the wrong number.* Here the same shape: a rate multiplied by a count, where the count
changes the conditions the rate was measured under.

**Recorded rather than hidden because it is the second instance**, and two instances is what turns an
error into a class. The remedy is the one this project already uses everywhere else: **measure the
thing you are going to claim, under the conditions you are going to claim it in.** A suite time is
measured by running the suite, once, and quoting that.


---

## `make power-mutants` is UNRUN at A7's close, with its cost and its blocker named

**Not run.** Its cost is **~15 CPU-hours**, and it is queued behind `make mutant-covered`, which is
itself blocked by the eight sweep-based covering tests above.

**What is not affected by its absence:** every mutant's power DECLARATION is checked by
`make power-decl`, which runs in milliseconds and is green at **71 of 71**, and `make power-refute-decl`
is green. Those check that each declaration is internally consistent and that no class carries the
retired bare opt-out. What `power-mutants` adds is the *re-measurement* of each floor and ceiling
against today's shape.

**What that means for the numbers in the record:** every floor quoted in this phase was measured when
it was taken, and each says so and names its shape. What has not been done at A7's close is a sweep
re-measuring all seventy-one **together**. A floor that has drifted since its own measurement would
not be caught by anything that ran here — and §5d is the reason that matters, since a floor is a
property of the class and the shape jointly.

**Due:** with the eight-test conversion, since that is what makes the lane affordable enough to
schedule at all.

<!-- BLOCKER
     what: make power-mutants is queued behind the ~20 sweep-based covering tests (NOT the eight the table named; see the 2026-08-28 correction), the largest of which is the 2,000-seed leader-completeness sweep this tripwire watches
     stale-when: ! grep -rqs "func TestLeaderCompletenessOracleReportsNothing(" sim/hunt/
-->

**The blocker above is declared machine-checkably**, and `tools/blockercheck` re-asks it on every
push. Its tripwire was re-pointed on 2026-08-28 by the correction below: it named
`TestFailoverDoesNotManufactureViolations` as *"the 1,000-seed one and the most expensive"*, which was
true of the eight and false of the repository — `TestLeaderCompletenessOracleReportsNothing` is
2,000 seeds. **A tripwire on the second-largest sweep would have gone green while the largest stood**,
which is the same mistake as the table it was derived from. It now names the real maximum: when that test stops being a sweep in `sim/hunt/`, the lane says
so and this entry gets rewritten rather than quietly continuing to claim a cost it no longer has.
That is `GF-42`'s mechanism, and this is its first live subject.

---

## OBLIGATION: `engine/riftcgo`'s determinism scope, decided at I1 by the pass

**Opened at the A7/B5 merge, 2026-08-27. Due at I1, where the cgo lane exists and the C++ archive is
a build dependency rather than an obstacle.**

`engine/riftcgo` is named in `notAnalysed` with its reason: it cannot type-check without the C++
static archive and `rift.h`, so no amount of tag forwarding reaches it from a Go-only lane. Its
determinism scope is therefore **open**, and it is named as not-analysed rather than assumed clean.

**Ansh's prediction, recorded so the answer can be checked against it:**

> *"My prior is that the cgo wrapper is core-scope code with a hatch for the boundary rather than
> orchestration, since it implements the frozen `Engine` interface and runs inside simulated runs at
> I1."*

**What settles it, and it is not the prediction.** Build the archive, forward the tag, run the pass,
and read what it says. The two mechanisms it needs are already in place:
`TestTagForwardingActuallyReachesTheLoader` proves `BuildFlags` reaches the loader, and
`TestEveryBuildTaggedPackageIsAnalysedOrNamed` will stop accepting the `notAnalysed` entry the moment
the package becomes loadable — because a named exemption is only honest while the reason holds.

**Why the answer is not obvious either way.** If the prediction is right, the wrapper is core scope
and its cgo boundary needs a per-line hatch in `HATCHES.txt`, which is a reviewed list rather than a
package exclusion, per Amendment A5. If it is wrong — if the wrapper turns out to be adaptation that
runs *around* simulated runs rather than inside them — it joins the by-name exclusion list beside
`engine/differential`, exactly and never as a prefix. **The two outcomes require different work, which
is the reason not to guess.**

**Also owed at the merge, found while classifying:** `engine/differential`'s tests read a fixture
corpus from `seeds/differential/format` (22 entries). The package and the corpus have to move
together or its tests fail on a missing directory, which is a merge-completeness item rather than a
defect.
