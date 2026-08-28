# Track A: what was built, how it was verified, and what the verification cannot see

*Written for somebody who has never opened this repository and is not going to read the design
documents. Every number here comes from an exit run, a lane, or a measurement recorded at the time it
was taken. Where a number is a criterion rather than a measurement, it says so.*

---

## 1. What this is

A distributed, transactional key-value database, built from scratch in Go: multi-group Raft, ranges
that split under load, MVCC over hybrid logical clocks, snapshot-isolated distributed transactions,
and linearizable reads that do not cost a log entry.

The part worth your attention is not the database. It is that **the whole thing runs inside a
deterministic simulator**, so every schedule — every crash, partition, message reorder, clock jump,
lost unsynced write — is a seed, and every seed replays byte for byte. A bug found on Tuesday
reproduces on Friday from a single number.

The design goal behind that: **make it impossible to claim something is verified when it is not.**
Most of what follows is about the ways that turned out to be harder than it sounds.

---

## 2. Phase by phase, with its exit gate

Each phase is gated: nothing starts until the previous one's exit criteria are signed off, and the
criteria are written *before* the code. "Green" below means the exit run completed with **zero safety
violations** under the phase's fault mix.

| phase | what it added | its exit gate | state |
|---|---|---|---|
| **A0** | the simulator, fault injectors, `Clock`/`Rand`/`Transport`/`Engine` interfaces, the seed CLI | a toy state machine survives 1,000 seeds; identical seeds give identical traces; injector fire counts asserted; the mutant suite kills every mutant | **signed** |
| **A1** | single-group Raft: elections, replication, persistence with correct sync ordering, the figure-8 commit rule | 10,000 mixed-fault seeds, porcupine green, **and a non-empty BUGS.md** | **signed** |
| **A2** | snapshots, log compaction, pre-vote, leadership transfer | crash storms plus partitions with snapshot transfers in flight, 10,000 seeds green | **signed** |
| **A3** | single-node membership changes, learner catch-up, config-across-snapshot | continuous membership churn under faults, 15,000 seeds green | **signed** |
| **A4** | multi-raft: range descriptors with epochs, size-threshold splits, router retry, manual rebalance | workloads spanning many splits mid-traffic, per-key linearizability green, 10,000 seeds | **signed** |
| **A5** | MVCC, versioned key encoding, version GC, hybrid logical clocks with `maxOffset` | MVCC semantics suite deterministic and green; HLC causality green under skew schedules | **signed** |
| **A6** | Percolator-style distributed transactions, uncertainty intervals, reader-side lock resolution | 25,000 seeds: conservation, atomicity and SI green; single-key porcupine still green | **signed** — measured at commit `611d0b9`: **pass 24,903, violations 0, inconclusive 97** |
| **A7** | read index, follower reads, the term-start no-op | staleness checker green under partitions and leader churn; 25,000 seeds | **measured** at commit `6c43023`: **25,000 seeds, pass 24,900, violations 0, inconclusive 100, errors 0** |

**A7's exit run, in full**, because it is the most recent and the most completely recorded: eight
contiguous shards tiling `[0, 25000)` with no gap and no overlap, all at one commit, aggregated by a
test that *checks* the tiling rather than trusting it. 5h54m–6h02m per shard. Inconclusive rate **4.0
per mille** against a threshold of 30, and every sampled inconclusive is the same cause — a history in
which too few client operations were ever answered for the checker to conclude anything. **An
inconclusive is never counted as a pass.**

---

## 3. What was found: 35 defects, every one reproducing from a seed

`BUGS.md` carries, for each: the symptom, the seed or kill point, the root cause, the fix, **the
invariant that caught it**, which mutant class would have caught it, and what it would have caused in
production. If no existing mutant class would have caught it, a new one lands in the same change as
the fix.

**What found them is the distribution worth reading:**

| found by | count | representative |
|---|---|---|
| **persist-before-reply** — a message may not attest to state that is not on disk | 4 | **BUG-005** — a follower acknowledged index 15 with 5 on disk; **BUG-027** — an A1 oracle catching an A7 wire |
| other structural assertions inside `raft/` and the driver | 6 | **BUG-009** — a replica overwrote entries it had already reported committed |
| **snapshot equivalence** — a node's state must equal the log's state at its position | 4 | **BUG-011**, on 178 of the first 300 seeds of its phase |
| **conservation, atomicity, snapshot isolation** | 5 | **BUG-022** — a transaction committed underneath an answer already given |
| **per-key linearizability** (porcupine) | 3 | **BUG-004** — a client told its write succeeded, with no committed entry containing it; **BUG-026** |
| a **non-vacuity counter reading zero** | 2 | **BUG-025** — follower reads implemented, dispatched, and answered by nobody |
| the rebalance oracle | 1 | **BUG-016** |
| a plain sweep, the first after a feature landed | 1 | **BUG-010** |
| inspection while diagnosing a different defect | 1 | **BUG-012** |
| **the harness checking itself, or somebody reading its own record** | 6 | **BUG-029**, **BUG-031**, **BUG-033**, **BUG-035** |
| **an enumeration** — listing a property and asking whether it still held | 1 | **BUG-028** |
| **a false accusation**, chased down instead of tuned away | 1 | **BUG-032** |

### How that table was built, which is the same rule as everything else here

Three numbers were in play while writing this section:

| number | where it came from | |
|---|---|---|
| **36** | `grep -c '^### BUG-'` | **wrong** — `BUGS.md` carries a `BUG-NNN` template heading, and a count counted it |
| **33** | the categories, assigned from memory | **wrong** — it did not sum to any population at all |
| **35** | every entry's own *"Found by"* line, extracted | **right** |

The one that is right is the one **derived from the artifact** rather than from a count over it or a
recollection of it. That is the same rule three of this project's standing rules are (§6): *read the
thing, not the proxy for the thing* — and it is worth having arrive here, in the document a stranger
reads, rather than only in the design notes.

> A summary table in a document about vacuous verification that does not add up to its own population
> is the exact shape of thing this repository exists to refuse.

### Four a stranger should read

**BUG-022 — a well-formed database with money missing from it.** A transaction committed at a
timestamp *below* an answer already given to a reader. No error, no divergence, no structural
invariant violated, **on a schedule with no faults in it at all**. Percolator does not need the guard
that prevents this because Percolator has one timestamp oracle; per-node hybrid clocks do not. Caught
by the bank conservation check and by nothing else.

**BUG-025 — a feature that was implemented, dispatched, and answered by nobody.** A new message type
was added to `raft/` carrying two new fields. The transport codec serialises a fixed field list that
did not include them, so the message arrived with its type byte intact and its payload **zeroed**.
Follower reads were forwarded and never answered, silently, while every test passed — because the raft
tests call the state machine directly and never cross the wire.

> **A unit test that exercises a mechanism without its serialisation will pass over a wire that does
> not work.** This was the second such defect in this project; the first was six phases earlier in a
> different codec, and after it nothing was built that would catch the next one.

**BUG-026 — the absence of a range, delivered as the absence of a value.** A read was routed to the
range that owned its key, then answered after that range had split the key away. The answer was "not
found", to a client that had written the key a second earlier. 526 of 25,000 seeds.

The instructive part is what *didn't* catch it. The oracle built for exactly this path — comparing
each read against the range's own committed log — was **silent**, because the range's log genuinely no
longer held the key. The live answer and the model agreed, and both were wrong.

**BUG-028 — found by writing a table.** After BUG-026, the fix was ruled to be two parts: the one-line
check, and *an enumeration of every check a read gets **because it is a log entry**, each marked
preserved, replaced or dropped*. Row two of that table was "the read's timestamp is log-ordered".
Asking whether the new path preserved it answered **no** — and that was a second, independent defect,
live since the feature shipped, invisible to every sweep, and found by nobody reading the code.

> **A class is cheaper to enumerate than to meet one member at a time.** The first member cost a
> 25,000-seed run. The second cost a table row.

---

## 4. The register: 27 instances of verification that verified nothing

The most productive artifact in this repository is a numbered list of times a checking mechanism
reported success while checking nothing. Most of the standing rules came out of it.

**Four entries carry the argument:**

**#8 — the oracle asked the accused.** The checker whose job is catching a node that claims durability
it does not have was comparing that node's claims against *the engine's view of what it held* — one
indirection away from asking the accused. The design document had already written *"an oracle which
interrogates the engine believes the lie"*, and the implementation did it anyway, in the mechanism
that sentence was written to protect.

**#23 — an opt-out is a claim that switches off its own instrument.** A mutant class carried a written
exemption saying the sweep could not reach it. The lane skips any class with an exemption, so the
claim was never measured. When it finally was: **280 detections in 300 seeds.** The claim had been
false the day it was written — not gone stale — and it stood for a phase and a half, because writing
the exemption is what turns the measurement off.

**#26 — two tests written for this exact failure mode, both vacuous.** Written by someone who had
spent that day documenting twenty-five prior instances, to assert two properties that had just been
ruled on. Both passed under the exact mutation each existed to catch. **One command found both.**

**#27 — a checker wired into something that never calls it.** A7's differential oracle was appended to
a list of step-oracles. That list is driven by a function that calls one method, and this oracle's
work is in another. It compared **zero** answers on every seed of every run, and was found by its own
non-vacuity counter rather than by anyone reading the code.

> The register's thesis is that this is not a competence problem. **#26 is the experiment**: maximum
> attention, maximum recent exposure, an explicit rule requiring exactly this check — and the tests
> were still vacuous, and no amount of reading them would have shown it.

---

## 5. The general forms

Each came from a specific defect, and each is enforced by something that fails.

**A fact maintained by the apply path is a function of the log.** The moment an operation is answered
off the log, every fact it used to maintain becomes a fact somebody must maintain elsewhere — *and the
place it used to live still compiles.*

**A detection floor is a property of the class AND the shape, jointly.** The same mutant measured `0 of
200` under one phase's workload — reported as unreachable — and `22 of 600` under the next, while
being killable by hand throughout. A floor recorded without its shape is not a measurement.

**A planted defect tests the checker as much as the code.** A kill is evidence about both, and about
the code only if the verdict *describes the defect that was planted*.

**Copying the shape of a guarantee is how you lose it quietly.** One code path answers a read "at its
own timestamp, not at the newest version" — correct, because that timestamp is log-ordered. A second
path copied *answered-at-a-timestamp* and inherited none of the reason. Both sites read correctly in
isolation; the defect was the relation between them, and **a relation is not visible from either end**.

**A mutant is a claim about where a defect lives.** A zero can mean the claim is aimed at the wrong
point rather than at the wrong line. Four axes, and no single lane asks more than one: the **line**,
the **code position**, the **role** the defect is reachable on, and the **moment** in the run.

**A green with no baseline is not a result.** Regenerating a corpus after a change made a lane green in
102 seconds with three bundles silently no longer carrying their findings.

**Absence as evidence needs the experiment verified independently of the result.** A lane that reads
*nothing happened* as confirmation cannot tell an absent effect from an absent experiment.

**A label that collapses two opposite meanings is worse than no label.** One exemption keyword meant
both *nothing can reach this* and *something better than a sweep covers this*. The classes wearing it
in the second sense were the best-covered in the tree.

**A distinction made uncompilable in one layer is not thereby known in another.** Wall time and
monotonic time are separate types here, with a poison serialiser and a vet rule. The same confusion
then arrived in how the harness read its own processes — where nothing enforces it — and cost an
unexplained observation and two wrong hypotheses.

**A rule written about one instance does not generalise itself to its siblings.** A handoff carried a
section headed *"state it, do not assume it"* about one lane, while the two lanes beside it went
unrun.

---

## 6. The standing rules, and the defect that produced each

| rule | the defect behind it |
|---|---|
| **No gate counts until its failure has been induced.** | Checkers that had never been watched to go red, several of which could not. |
| **An oracle must fire on its plant *and* be silent on a clean tree**, both at a stated seed count. | A7's differential oracle passed the first test while being wrong in a way that would have failed clean runs. |
| **Every mutant carries a detection floor AND a seeds-to-detection ceiling.** | A class kept its rate while its first detection moved from seed 145 to 553 — out of reach of every short run, with the rate unchanged. |
| **An exclusion may cite a measurement or an argument about reachability — never the excluded class's own declaration.** | Register #23: the exemption was what stopped the measurement. |
| **Started is read from the process, never from the launch.** | An exit run believed to be running for two hours had refused at launch. |
| **The tree state is read from the tree, never from the revert that was supposed to have happened.** | A killed measurement left a mutant applied; three later floors were measured against two mutations and read plausible. |
| **Liveness is read from the CPU clock, never from the wall clock.** | A two-hour timeout that had not fired at 2h56m — because the machine had slept for 62 minutes and the timeout counts monotonic time. |
| **An inconclusive is never a pass.** | A linearizability checker that could not conclude was being banked as green. |
| **A bundle is not retired on a null.** | A search that found nothing was about to be read as a verdict that there was nothing. |
| **A field a script parses holds one value, not a paragraph.** | A reproduce-seed field accumulated three seeds and a narrative; a pre-push hook refused the push over the inconsistency. |
| **A directed test that arranges a precondition must assert that it arranged it.** | Four of this project's own directed tests silently arranged nothing until a vacuity guard caught them. |

**The three-rule chain is one rule seen three times** — *read the thing, not the proxy for the thing* —
and the part that makes it actionable is that **the discriminator was available every time**. `ps`
before the first, `git diff` before the second, `ps -o time=` before the third.

> **The failure is not missing instrumentation. It is reading the number that was easier to reach.**

---

## 7. The numbers

| | |
|---|---|
| phases signed | **A0 – A7** |
| defects found, all reproducing from a seed | **35** |
| mutant classes, each with a covering test and a measured floor or a named instrument | **71** |
| corpus bundles | **24** |
| vacuous-green register | **27 instances** |
| A6 exit run | 25,000 seeds — pass 24,903, violations **0**, inconclusive 97 |
| A7 exit run | 25,000 seeds — pass 24,900, violations **0**, inconclusive 100, errors 0 |

---

## 8. What this verification cannot see

*This section is in the same document as the claims, on purpose. A document that states what it cannot
claim is the one worth trusting on what it does.*

### 8.1 A signed phase is signed against a fault mix, not against the world

**This is the most important sentence here, and it has two measured demonstrations.**

- **A mutant planting a real defect was detected once in 3,000 seeds under one phase's fault mix, and
  ZERO times under the previous phase's.** The defect did not change. The schedule did. Widening the
  mix did not make numbers look better — it made a real class findable. *(The same class was measured
  again at A7's close: the precondition it needs occurs **once in 200 seeds** under A7's shape and
  **zero times in 200** under the shape its floor had been declared against. The floor had been
  pointed at a workload that cannot produce the defect.)*
- **A phase whose headline feature is hybrid logical clocks ran its entire verification with clock
  skew injection switched off.** The configuration said `Holds = 0` — correct when written two phases
  earlier, false from the moment clocks mattered. Nothing connected *this phase is clock-sensitive* to
  *this phase's plan generates clock faults*. Turning it on immediately produced a defect.

> Every fault count and duration in the workload configuration is a claim about which defects this
> project can find. **"Zero violations" always means zero violations under the schedules that were
> generated**, and the schedules are a choice.

### 8.2 Idealizations built into the simulator

The model is not the world, and these are the specific places it is kinder:

- **The network is fair-lossy with bounded delay.** Partitions heal; messages are dropped, delayed,
  duplicated and reordered, but not corrupted and not delivered arbitrarily late.
- **The disk fails in modelled ways.** Unsynced writes are lost on crash — the important one — but
  bit rot, partial sector writes and silent corruption are the C++ engine's territory, not this
  simulator's.
- **Clock skew is bounded by `maxOffset` in safety runs.** Runs that exceed it are a separate,
  deliberately-designed experiment, not part of any "zero violations" claim.
- **One process per node.** Real deployments lose a whole machine hosting many ranges; here a crash is
  modelled that way, but resource contention between co-hosted ranges is not.
- **Determinism is guaranteed on the Go reference engine only.** The C++ storage engine's correctness
  rests on different machinery — a fault-injecting `Env`, kill-point sweeps, differential tests — and
  that scoping is stated wherever the claim appears rather than blurred into it.

### 8.3 Recorded gaps, named rather than discovered later

- **`make mutants` is INVALID and A7 was signed with it red.** Not because a mutant survived — none
  did — but because the lane's *baseline* cannot finish. It runs 52 covering tests in one process, and
  eight of them are seed sweeps inherited from A1–A6 totalling ~1,928 seeds, which puts the baseline
  over **two hours of monotonic time** before any of the 70 per-mutant runs begin. A7 converted nine
  covering tests from sweeps to directed tests, taking the lane from **six days to a two-hour
  baseline**; the remaining eight are a named, dated obligation, due at whichever phase next touches
  each class. **Raising the timeout was refused: it would make the number pass without making the
  claim true.** Every A7 class was induced and confirmed individually.
- **One corpus bundle no longer reproduces.** Its defect class is real and is killed by a directed test
  in 0.3 seconds; what is missing is a *schedule* that exhibits it. A search costing 5h34m found none,
  and the reason is now understood: the class needs two independent things to coincide, and **a bundle
  pins one schedule, so a class needing two coincidences has no single schedule to pin.** The entry
  says so in the field a stranger reads, and it is not retired.
- **Interleavings the sweep does not deliberately produce.** No shape aims a membership change at a
  range that was split out moments earlier; no shape drives a snapshot install concurrently with a
  configuration change on the same range. These are not claimed to be safe — they are claimed to be
  unexercised, which is a different and weaker statement.
- **Range merges are out of scope**, and joint consensus, parallel commits, leader leases and automatic
  load-based balancing are deliberately outside this version, with the reasoning preserved. **None of
  them is claimed.**

### 8.4 The instrument problem, which does not go away

Three of this project's defects were in the instruments rather than in the system: an oracle that was
**wrong** and would have failed clean runs; an oracle that was **right about its property and silent
about the one violated**; and a **guarantee copied without its reason**, where the model and the code
were wrong identically and therefore agreed.

> **An oracle's failure is not visible from inside the oracle.** Each was found by something outside
> it — a mutant whose kill was too good, a client that happened to observe the write, a table, and a
> contradiction. The practical consequence is that an instrument needs an instrument, and that
> regress is why the two-number standard and the enumeration exist.

---

## 8.5 One thing that did not go wrong

**The `Engine` interface was frozen at A0.5, before a single line of Raft existed, and seven phases
were built against it without reopening it.** Two changes were argued for along the way and both were
refused; one was ruled to ride with a later change that needed the interface opened anyway, and that
change never became necessary either.

That is not a claim about foresight. It is evidence about where the line was drawn: **a freeze that
survives the phases it was written for is a freeze drawn in the right place**, and a freeze that has
to be reopened three times was a guess. The same holds for the two deferrals this track ran under: the
Dec-1 timestamp-oracle fallback was pre-authorised and never invoked, and the leader-lease deferral
held shut for the whole of A7 including the phase where leases would have been the convenient answer.

---

## 9. How to check any of this yourself

```
make test          # every package, every covering test capped to a handful of seeds
make smoke         # 500 seeds, all checkers on
make corpus        # every historical bug replays from its bundle
make corpus-reproduces   # every bundle still EXERCISES its defect, which is a different question
make power-decl    # every mutant's power declaration is internally consistent, in milliseconds
make exit-run      # 25,000 seeds across contiguous shards, aggregated with the tiling checked
simctl replay --bundle seeds/BUG-022    # one historical defect, byte for byte
```

Nothing above is on trust. Every claim in this document is a lane that fails, a seed that replays, or
a number written down when it was taken.
