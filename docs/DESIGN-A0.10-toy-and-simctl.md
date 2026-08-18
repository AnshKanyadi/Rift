# DESIGN-A0.10: the toy, the ablation, and simctl

**Status:** landed for review (checklist steps 7 and 8). **Author:** Claude. **Decider:** Ansh.

Step 8's fresh-process gate was ratified by Ansh on 2026-08-17; its bundle chain (§3) and step 7's
two conditions (§1, §2) landed after that ruling and are unruled.

---

## 1. The placement ablation, answered

The remedy that first caught `ack-before-sync` bundled two changes: it made the crash reactive on the
unsynced window, **and** it widened the modelled fsync from 2ms to 50ms. Two claims, and each had to
earn its place separately.

### Placement: reactive materially wins

The null hypothesis is the same crash, aimed at the same node, for the same downtime, at a uniformly
drawn instant instead of at the window. Only the placement differs — letting the uniform cell also
pick a random node would have confounded placement with target and answered neither question.

Measured at commit `f9dedcd`. **Every ablation in this project carries the commit it was measured
at**, for the reason §5 gives: an ablation is a measurement of the *harness*, so it expires the moment
the machinery under it changes.

| placement | fsync | caught / eligible | seeds-to-detection |
|---|---|---|---|
| reactive | 50ms | **504 / 1000** | **1** |
| uniform | 50ms | 44 / 1000 | 12 |

**Reactive targeting is ratified on evidence.** It wins on both axes: 11x the detection rate, and it
finds its first violation on the first seed rather than the twelfth.

The honest caveat: this pair was measured *after* the two harness fixes below, and before them the
same comparison read 82 vs 43 with uniform winning on seeds-to-detection. The conclusion reversed
because the harness got stronger, which is the argument for re-measuring an ablation whenever the
machinery under it changes rather than quoting a number taken once.

### The window: two constraints, and the gate was checking the wrong one

**Wrong framing:** the harness fails to detect a present bug at a narrow fsync window.

**Right framing:** at a narrow window *there is no incorrect behaviour in existence to detect.* That
much was already established. What was not established is *which* narrowness matters, and the answer
turned out not to be the one the gate was defending.

The class needs two independent things to be true:

1. **Equivalence.** fsync must be slower than a replication round trip, or a primary awaiting backup
   acknowledgements is already durable when it answers and the flawed toy is behaviourally identical
   to the correct one.
2. **Reachability.** The window must outlast the reactive crash's delay. The crash fires `crashDelay`
   after the window opens; a window that has already closed leaves an in-flight operation the checker
   correctly refuses to score.

`MinWindowMargin` was 3, defended by a curve showing detection "still marginal" at 10ms. **That curve
was measured under the `Trigger` budget defect**, on a harness running at a sixth of its power, so it
could not support any margin. Re-measured at `MinWindowMargin = 1` on the fixed harness, per eligible
seed, with `crashDelay` at 10ms:

| modelled fsync | eligible / 1000 | detections per mille | seeds to first |
|---|---|---|---|
| 2 ms | 0 | — | no seed's network is fast enough |
| 5 ms | 1 | (one-seed sample, not a rate) | 338 |
| **10 ms** | 344 | **11** | 338 |
| **11 ms** | 344 | **534** | 1 |
| 12 ms | 1000 | 499 | 1 |
| 20 ms | 1000 | 500 | 1 |
| 50 ms | 1000 | 504 | 1 |

**The step is between 10ms and 11ms — `crashDelay` — not at any multiple of the round trip.** Detection
then saturates completely: 11ms is worth as much as 50ms.

Three conclusions, all of which change something:

- **The margin is 1, and it is now derived rather than stated.** Parity is the true equivalence
  threshold. A margin above it refuses regimes where the flaw genuinely manifests, which is a gate
  refusing correct configurations for a reason that is not true.
- **The binding constraint is checked explicitly, against the quantity that governs it.**
  `ValidateWindow` now refuses `syncLatency <= crashDelay`, strictly and with no extra margin, because
  the curve says none is needed.
- **The 50ms default is no longer justified by the curve.** It is ~5x wider than reachability
  requires. It is *not* narrowed here: `DefaultSyncLatency` is on the execution path, so changing it
  changes every trace hash including the ratified seed 4242 cross-invocation hash. That is a ruling,
  not an audit side effect.

Eligibility remains per seed, since per-seed link latencies vary and a window productive on a fast
seed's network is blind on a slow one's. A refused seed is counted in neither numerator nor
denominator.

`TestWindowCurveIsRecorded` keeps the curve as a live artifact rather than a comment that ages. The
previous curve went stale invisibly and was used to justify a margin for three cycles; one that is
re-derived by running cannot fail that way. It asserts the *shape* — refused at the crash delay,
saturated one millisecond above it — because the shape is the claim.

**The carry into A1:** real Raft has its own window between acknowledgement and durability. This
assumption is re-tested against it rather than inherited.

---

## 2. Promotion, and the gap it closed

`ack-before-replicate` was a recorded gap: reads were served only by the primary and there was no
failover, so a write missing from the backups was invisible to every client.

Promotion closes it. The toy now takes a `promote` plan entry — operator-driven, not elected, because
a toy with its own consensus would make every failure ambiguous between the harness and the protocol
— applied to every node at one instant so two simultaneous primaries are unrepresentable.

| flaw | failover | caught / 1000 | seeds-to-detection |
|---|---|---|---|
| `ack-before-sync` | no | 504 | 1 |
| `ack-before-replicate` | no | 0 | — (gap, preserved) |
| `ack-before-replicate` | **yes** | **35** | **7** |
| `dirty-read` | no | 1 | 104 |
| `ack-counting` | yes | 1 | 154 |

The no-failover row is kept rather than deleted. It is still true that the flaw is invisible without
promotion, and keeping both rows is what makes that statement falsifiable instead of folklore — the
bidirectional assertion fails if an unobservable flaw is suddenly caught.

**Two flaws are new**, and they are the two defects the harness found in the *correct* toy this cycle
(BUG-001, BUG-002). Amendment A2 requires a mutant class per root cause, and the moment a fix lands
is the only moment we have a precise description of the blind spot that let the bug through. Their
detection rates — 1 in 1000 each — are the honest number and are worth watching: both sit close to
the edge of detectability, which is exactly where a silent regression would hide.

### The crash rides on different triggers for different flaws

`triggerFor` picks `unsynced_window_open` without failover and `write_acked` with it, and the reason
is not tuning:

- `ack-before-sync` needs the primary to die **before** its fsync.
- `ack-before-replicate` needs it to die **after** it has answered a client.

Those windows do not overlap. Aiming at the wrong edge does not weaken the signal, it removes it: a
crash before the acknowledgement leaves an in-flight operation the checker correctly refuses to score.

### Failover must not manufacture violations

`TestFailoverDoesNotManufactureViolations` is the other half, and the easy half to skip. Promotion
adds a way for the *harness* to produce a non-linearizable history, and at three seeds in a thousand
that would be indistinguishable from a real find. The correct toy stays clean across exactly the
schedule that catches the broken one: 0 violations, 0 inconclusive, 1000 seeds.

---

## 3. simctl: the bundle chain

### The fresh-process gate (ratified 2026-08-17)

`replay` is by definition a fresh-process re-execution, which is why the gate rides here rather than
needing a spawner built and then deleted. An in-process rerun shares its address space, its map seeds,
and everything initialized once per process; it cannot catch map iteration order seeded from process
state, address-dependent behaviour, or a value captured at package init. The gate runs four
invocations, two with `GOGC` and `GOMAXPROCS` perturbed so allocation timing and scheduler shape
differ while the run does not.

**Cross-invocation hash, seed 4242, darwin/arm64. It has moved exactly once, on purpose:**

```
was  a679fba6bc13468491e9cb06745609810d97c9e145925f658f8bd5d15574e6de
now  046a9ce5f129c15948279ba8e2e8ed59a9621a9a7a65ff8184ed5c4954ab055a
```

**Why it moved.** The fire-count fixes changed the schedule the generator produces: restarts are now
drawn from what is left of the run instead of a flat two seconds, so they land inside it. The old
value pinned a schedule in which ~19% of runs never executed the restart their plan asserted — a
recorded hash for a run that did not do what its plan said. Preserving it would have been preserving
the defect.

It stays comparable against CI's runner the day the remote lands; if that runner is amd64 it is the
FMA defence's first cross-architecture datapoint. **The clock golden vectors (including tick 5) and
the `internal/rng` KAT vectors did not move**, verified by both packages having no diff at all in the
commit that moved this one — neither depends on injector scheduling, and both moving would have meant
the fix reached further than intended.

**Induced, both ways.** Two different seeds must produce different hashes, or the hash is not covering
the run. And a deliberately perturbed plan — one fault entry moved — produces a divergence report
naming the step:

```
DIVERGED
first divergence at step 0: recorded 61311716da1aa79c, replayed 0631e76f5ef262c2
  (agreed for the preceding 0 steps)
```

### The toy is now reachable, and that is what makes a violation an artifact

The gate above hashed a run of do-nothing nodes: it covered the loop, the transport, the plan and the
clock, and the toy was reachable only through `go test`. **So no toy-level violation could produce a
replayable bundle, and `seeds/` held only a README.** That is the entire repro chain — step 9's hunt
would have had nothing to hand a human, and A1's first corpus entry no mechanism behind it.

`simctl run --workload toy` closes it. The workload is selected explicitly rather than defaulted,
because a silent default would make empty runs and toy runs indistinguishable in a bundle while
producing different hashes.

A bundle carries:

```
plan.json      the plan that ran, faults and workload fully materialized
meta.json      seed, workload, scenario, trace hash, per-step digests, outcome, violation
history.json   what the client observed, which is what the checker judged
```

The violation record locates the finding in both coordinate systems an investigator needs: the
instant, and the trace step ordinal resolved from it. When the trace was capped before that instant
the step is omitted rather than guessed, because a wrong ordinal sends an investigator to the wrong
event with full confidence.

**Proved end to end**, in `TestToyViolationBundlesAndReplays`: hunt seeds through the command line
until one trips the broken toy, bundle it, replay from a fresh process, and require the same
*verdict* and not merely the same hash. The hash says the run reproduced; the verdict says the
finding did, and only the second is the claim a corpus entry makes.

```
hunted: seed 29 trips ack-before-sync
MATCH
violation reproduced: linearizability: key "k01" has a non-linearizable history over 5 operations
```

### One driver, not two

`RunToy`, `ToyGenConfig` and `MaterializeToy` are exported from `sim/hunt` and used by both the sweep
and `simctl`. This is not decoration: the first version had `simctl` materializing against
`plan.DefaultGenConfig` while the sweep used its own, so **seed 29 meant two different plans** and the
violation the sweep found did not exist in the bundle. Both halves ran cleanly and printed a hash. A
repro chain with two implementations of its middle is not a repro chain.

`sim/hunt` is already excluded from the determinism pass by name as orchestration, and holding the
driver there costs no new scope decision. It is **not** step 9: there is no seed sweeping, no worker
pool and no `hunt` subcommand.

### Stripped-fault replay now answers the question it was built to ask

`--strip-faults` re-executes with every fault entry and rule removed. Previously it reported only that
the hashes differed, which is expected and uninformative. It now reports the triage verdict:

```
VIOLATION DID NOT SURVIVE: consistent with a defect in the system under test,
         since removing the faults removed the finding
```

or, for a harness-manufactured violation, that it survived with zero injected faults and must not
enter the corpus. Both BUG-001 and BUG-002 were triaged through this gate before being written up,
and it is what established they were the toy rather than the harness.

---

## 4. Standing practices this cycle produced or confirmed

### Harness-manufactured violation, and the triage gate

**The named class.** A violation produced by the harness or the workload rather than by the system
under test. The generator's out-of-order client sequence numbers were one: 913 of 1000 seeds reported
a non-linearizable history, and every one was the generator's fault.

At 913 of 1000 it was unmissable. **At 3 of 1000 it would have been indistinguishable from a real
find.** This cycle produced exactly that case — 1 violation in 1000 in a toy with no flaw planted —
and the gate answered it in one command.

### The two harness bugs failed in opposite directions; the third failed in a new one

- **Silent — false negatives.** The loop marked a crashed node down without telling it, so its engine
  kept the unsynced writes a real process death would have taken.
- **Loud — false positives.** The generator's out-of-order sequences, 913 of 1000.
- **New this cycle, and silent again.** The `Trigger` budget defect, analysed in full in §5.

Three harness defects now, and the pattern is stable: the dangerous ones are not wrong answers, they
are unasked questions, and they all report green.

### seeds-to-first-detection

The harness's power as a number, tracked per flaw class across cycles. **A rising number means the
harness got weaker and nobody noticed.** The current table is §2 above. Two of its five rows sit at 1
in 1000, and those are the rows to watch.

### The bidirectional gap assertion

Each planted flaw declares whether it is currently observable *and the reason*, and the test asserts
both directions: an observable flaw that is missed fails, and an unobservable flaw that is suddenly
caught fails too. It fired for real this cycle — the `ack-before-replicate` no-failover row briefly
started catching a seed after the `Trigger` fix, which is how BUG-001 surfaced.

### A gate no production path calls is decoration

`ValidateWindow` existed, had its failure induced in its own test, and was called by nothing. That is
the same class as the crash injector that injected nothing: a green thing that cannot fail. It is now
called by `toy.New`, which refuses construction, and the wiring is proved through the constructor
rather than through the rule — the distinction between a planted violation in live code and a
testdata fixture.

---

## 5. The `Trigger` budget defect, and the rule it produces

The most important thing in this cycle, and it is not the ablation.

### What it was

`Run.Trigger` fires the reactive rules registered for a named condition. `Rule.Times` bounds how many
times a rule may fire. The counter was keyed by **condition**:

```go
if rule.Times > 0 && r.fired[condition] >= rule.Times { continue }
r.fired[condition]++
```

Two rules on one condition therefore shared one budget. The toy's reactive schedule is exactly that
shape — *crash the primary 10ms after the unsynced window opens, restart it 200ms after* — so the
crash rule's single fire exhausted the budget and **the restart rule never ran at all.** Not once,
on any seed, since checklist step 5.

### The number is the argument

Measured across the fix, same seeds, same schedule mix, nothing else changed:

| | before | after |
|---|---|---|
| `ack-before-sync` detections | 82 / 1000 | **504 / 1000** |
| seeds-to-detection | 30 | **1** |

**Detection against the harness's own headline flaw class ran at roughly a sixth of its true power
for five checklist steps, with every lane green.** The mechanism is direct: a primary that never
restarts can never serve the read that observes what it lost, so the majority of seeds that had
produced a genuine data-loss window scored nothing.

It also suppressed two real defects in the toy into invisibility. TOY-001 surfaced on the very next
sweep after the fix.

### Why fire counts could not catch it

Fire counts are this project's standing defence against an injector that does not fire, and they were
in force the whole time. They could not see this, and the reason generalises:

**The crash *did* fire.** It incremented `InjCrash`, which satisfied `min_fires`. The rule that never
ran shared its *condition*, not its counter — and no counter anywhere was keyed to it.

> **A per-entity counter proves that entity acted. It proves nothing about entities sharing its
> budget.**

That is the reusable sentence. The failure mode it names is not "a counter was wrong"; it is "a
counter was keyed more coarsely than the thing it was trusted to cover", and every consumer reading
that counter inherited a guarantee it never made.

### The general shape, third instance

| # | defect | direction | what noticed |
|---|---|---|---|
| 1 | crashed node marked down without being told | silent false negatives | nothing — found by inspection |
| 2 | generator's out-of-order client sequences | loud false positives | 913 of 1000 seeds |
| 3 | `Times` counted per condition | silent false negatives | nothing — found by a bug it was suppressing |

Two of three were silent, and neither was caught by a lane. This is what forced the harness-power
floors lane: three defects have now silently reduced detection power while every lane stayed green,
and nothing in the repo would have noticed a fourth.

### Standing rule: an ablation expires when the machinery under it changes

An ablation measures the *harness*, not the system, so it is only valid against the harness that
produced it. This cycle demonstrated the cost of not knowing that: the placement ablation measured
82 vs 43 with uniform winning on seeds-to-detection, and after the `Trigger` fix the same comparison
measured 504 vs 44 with reactive winning on both axes. **The conclusion reversed through no fault of
reasoning.**

So, mechanically rather than as a habit:

1. Every recorded ablation carries the commit it was measured at.
2. Any change that moves a detection number invalidates every ablation, and they are re-run.
3. The harness-power lane is what detects (2) having happened, since a moved detection number is
   exactly what it watches.

---

## 6. The shared-budget audit

§5's rule — *a per-entity counter proves that entity acted and proves nothing about entities sharing
its budget* — was turned on the rest of the harness. Every place where two things share a resource
keyed by something coarser than the thing itself. The answer is **not** "nothing else shares a
budget": two more were found, both in the fire-count machinery, which is the very mechanism that was
supposed to be the defence.

### What was audited

| site | keyed by | verdict |
|---|---|---|
| `plan.Run.fired` (rule budgets) | rule index | **fixed** in `be8a626`; was the defect |
| `sim.Counters.minFires` | injector *kind* | **finding 1** — coarser than the entities it covers |
| `Counters.Fire` call sites | — | **finding 2** — counts intent, not occurrence |
| `SimTransport.ordinal` | directed link | correct — per-link, which is what the dice are keyed on |
| `SimTransport.cut` / `links` | directed link | correct — a cut is a property of one direction of one link |
| `toy.applied` (client dedupe) | client id | correct — dedupe is per client by definition |
| `toy.inflight` | engine sequence | correct — one pending per write |
| `pending.acked` | node id | **fixed** in `f9dedcd`; was TOY-002, the same class |
| `clock` holds | node, one per node | correct — validation rejects two holds on one node |
| `rng.PCG.children` | derived-stream name | correct — memoization, not a budget |
| `Trace.limit` | whole run | correct — a retention cap, not a per-entity quota |

### Finding 1 — `min_fires` is keyed by injector kind, not by planned entry

`genFaults` sets `p.Assert.MinFires["crash"] = 1` **inside the loop** over `cfg.Crashes`. With two
crashes planned, the plan still asserts that one crash fired. The floor proves *some* crash happened
and says nothing about the second, which is the audited class exactly: a per-kind counter covering
multiple entries.

The consequence is bounded but real — an injector that silently stopped scheduling half its entries
would satisfy every assertion in the plan.

### Finding 2 — the counters count *scheduling*, not *firing*

`Run.schedule` calls `r.Counters.Fire(sim.InjCrash)` at the moment the event is **enqueued**, not when
it executes. Same for `InjRestart`, and `SimTransport.deliver` fires `InjDeliver` when a delivery is
scheduled. An event scheduled past the run's deadline is counted as having fired.

Measured over 300 seeds of the toy scenario, comparing each counter against the loop's own census of
events that actually executed:

| injector | scheduled | fired | seeds where they differ |
|---|---|---|---|
| crash | 600 | 600 | 0 |
| **restart** | **600** | **544** | **56 of 300 (19%)** |
| deliver | 14,216 | 14,208 | 5 of 300 |

**In roughly a fifth of seeds, `min_fires["restart"]` was satisfied by a restart that never
happened.** The generator draws the downtime as `IntN(2s)` from a crash instant anywhere in a 5s run,
so a restart is regularly scheduled past the deadline; the counter records the intent and the run
never performs it.

This is the sharper of the two, because the mechanism is named `Fire` and the assertion is called
`min_fires`. Both read as claims about occurrence.

### Not fixed here, and why that is a ruling rather than a decision

Both fixes ripple further than a fix should ripple without a ruling:

- **Finding 1** changes `Assert.MinFires` in every generated plan, so every `plan.json` changes and
  the two corpus bundles need regenerating. The trace hash is unaffected — the assert block does not
  execute — so this one is cheap and I would take it.
- **Finding 2** is the expensive half. Counting at fire time is a two-line change, but it would then
  correctly *fail* the ~19% of seeds whose restart cannot fire inside the deadline. The right fix for
  that is the generator's physics, not a looser assertion — clamp the restart inside the run — and
  **that changes execution, so every trace hash changes**, including the seed 4242 cross-invocation
  hash recorded for the CI comparison when the remote lands.

Deliberately not taken unilaterally: that hash was ratified as a comparison point, and invalidating it
is Ansh's call, not a side effect of an audit.

**Recommendation.** Take both, in one commit, and re-record the seed 4242 hash — the counter currently
asserts something it does not check, and a fire-count mechanism that can be satisfied by an event that
never occurred is the third instance of the same class in one cycle. The recorded hash's purpose is
cross-architecture comparison, which a re-record preserves; a fire count that lies does not become
true by being left alone.

---

## 7. The tally, and what it means

Six harness defects have been found in this project so far. **Every one of them was in the observer
rather than the observed, and every one made the machinery appear stronger than it was.**

| # | defect | direction | what noticed it |
|---|---|---|---|
| 1 | crashed node marked down without being told — the crash injector injected nothing | silent | inspection |
| 2 | generator's out-of-order client sequences manufactured violations | **loud**, 913 of 1000 | itself |
| 3 | `Trigger` counted `Times` per condition, so a rule sharing a trigger never fired | silent | a bug it was suppressing |
| 4 | `min_fires` keyed by injector kind while the generator plans N entries | silent | the audit |
| 5 | counters counted scheduling, not firing — 600 restarts scheduled, 544 fired | silent | the audit |
| 6 | `Counters.Check()` never called on any run path; `InjClockHold` never fired anywhere | silent | the audit |

Not one was a wrong answer. Every one was an **unasked question**, and each reported green.

### The sentence that carries it

**Defect 6 would have caught defects 4 and 5 the day either was written, and it could not, because
it was the thing that was broken.**

Six defects total, four of them inside the mechanism designated as the defence against the class the
other two belong to. **The guard rail was never installed.**

### The part that matters

Defects 4, 5 and 6 are all in the fire-count machinery — **the mechanism designated as the defence
against exactly the class defects 1 and 3 belong to.** The guard rail was never installed: `Build`
populated the requirements and nothing ever consulted them, so `min_fires` was decorative from
checklist step 4 onward. Defect 6 would have caught 4 and 5 the day either was written; it could not,
because it was the thing that was broken. And `InjClockHold` was required on every plan with a hold
and fired by nothing anywhere, so that requirement was unsatisfiable from the day it was written and
invisible for the same reason.

**In this project, every defect found to date has been in the observer rather than the observed.**
That is the sentence to keep. The system under test does not exist yet; what exists is the machinery
built to judge it, and the machinery has been wrong six times, always in the direction of claiming
more coverage than it had.

The standing consequence is the harness-power lane (`sim/hunt/floors.go`), which is the first
mechanism in the repository that fails when detection power drops rather than reporting it into a log
nobody diffs.

---

## 8. Re-measurement after the fire-count fixes

Standing rule from §5: an ablation expires when the machinery under it changes. This was the largest
such change yet, so everything was re-run. Two numbers fell, and per the ruling a fall is a finding
rather than noise — so both were isolated before the floors were reset.

### Matched-window comparison isolates the fix from the new default

The fire-count fixes and the `DefaultSyncLatency` change from 50ms to 12ms landed together, so a
naive before/after conflates them. Re-measured at the **old** 50ms window on the **fixed** harness:

| class | before the fixes | after, at matched 50ms |
|---|---|---|
| ack-before-sync | 504 / 1000, s2d 1 | **504 / 1000, s2d 1** |
| ack-before-replicate, failover | 35 / 1000, s2d 7 | **35 / 1000, s2d 7** |
| dirty-read | 1 / 1000, s2d 104 | **1 / 1000, s2d 104** |
| ack-counting | 1 / 1000, s2d 154 | **1 / 1000, s2d 154** |
| uniform placement | 44 / 1000, s2d 12 | **45 / 1000, s2d 12** |

**No class fell because of the fire-count fixes.** One rose by one seed. The apparent falls at the new
default are entirely the window narrowing, which is a deliberate tightening.

**Why the fixes moved almost nothing, which is worth understanding rather than waving at:** the
restart clamp applies to the *generator's* timed crash/restart pair, which targets a randomly chosen
node. The scenarios' detection paths ride on the *reactive* rules, whose restart fires 200ms after the
trigger and was already comfortably inside a 5s run. So the clamp corrected a real overstatement in
what the plans asserted without much changing what the detecting seeds did. That is the honest reading:
defects 4–6 were about what the harness *claimed*, not, in these classes, about what it *did*.

### Numbers at the new default (12ms)

| measurement | old (50ms default) | new (12ms default) |
|---|---|---|
| correct toy, 1000 seeds | 1000 pass, 0 violation, 0 inconclusive | unchanged |
| correct toy under failover | 0 violations | unchanged |
| ack-before-sync | 504 / 1000, s2d 1 | 499 / 1000, s2d 1 |
| ack-before-replicate, no failover | 0 / 1000 (gap) | 0 / 1000 (gap) |
| ack-before-replicate, failover | 35 / 1000, s2d 7 | 32 / 1000, s2d 7 |
| dirty-read | 1 / 1000, s2d 104 | 1 / 1000, s2d 104 |
| ack-counting | 1 / 1000, s2d 154 | 1 / 1000, s2d 154 |

### The placement ablation got sharper, and that is the interesting result

| placement | window | caught / eligible | seeds-to-detection |
|---|---|---|---|
| reactive | 50 ms (old default) | 504 / 1000 | 1 |
| uniform | 50 ms (old default) | 45 / 1000 | 12 |
| **reactive** | **12 ms (new default)** | **499 / 1000** | **1** |
| **uniform** | **12 ms (new default)** | **12 / 1000** | **130** |

Reactive targeting's advantage goes from **11x to 42x**, and its seeds-to-detection advantage from
12x to 130x. Narrowing the window made aiming matter far more, which is the expected direction and
the strongest evidence yet that reactive placement is not unproven complexity. Both pairs are kept so
the change of default is visible rather than inferred.

**Floors are reset from the new numbers**, not carried over: `ack-before-sync` from 499, and
`ack-before-replicate under failover` from 32. The floor *values* are unchanged because the reasoning
that set them is unchanged — half the measured rate for the strong class, detected-at-all for the two
weak ones — and both remain comfortably above their floors.

---

## 9. The standing rule A0 owes: assertion mechanisms must be proven to run

**A check that is never invoked is indistinguishable from a check that always passes.**

This repository has shipped five of them. Three were fixed in the last two cycles; the audit the
ruling asked for found two more that were still live:

| mechanism | what it refuses | state when found |
|---|---|---|
| `toy.ValidateWindow` | a regime where the planted flaws cannot manifest | declared, tested, called by nothing |
| `sim.Counters.Check` | a run that injected less than its plan asserts | required by every plan, called by nothing |
| `sim.InjClockHold` | — | required by every plan, **fired by nothing anywhere** |
| **`sim.History.Validate`** | a history that cannot be checked being handed to a checker that returns green | **written for the oracle path, called by nothing** |
| **`clock.Check`** | (computes the exact skew envelope) | **called by no run path** |

Five instances is a pattern. Every one looked identical from the outside: a function that exists, with
its own passing test, that no production path reaches. Reading the source and satisfying oneself the
call is there is exactly how this was missed five times over.

### The mechanism

`sim/assertions.go` is a checked-in registry, in the shape `HATCHES.txt` already proved works: an
exemption is a conscious edit to a reviewed list rather than an omission nobody sees. Every mechanism
is classified, and the unset kind is **rejected** rather than defaulted — a mechanism nobody
classified must not default into being exempt, which is precisely how the five got there.

- **every-run** (7): asserted by census. Each records into the run's own `Counters` when it executes,
  and the lane runs an ordinary seed and requires each to appear. Per-run, not a package-level
  counter, which core scope could not hold anyway.
- **diagnostic** (1): `clock.Check`, invoked by the skew tests and by the envelope experiment when
  A8's successor is built. It is an analysis over a pair of timelines, not a per-run gate — maxOffset
  uniformity is the per-run property and is asserted separately. Being uninvoked-by-default is now a
  written decision with a reason.

`sim.History.Validate` is fixed rather than merely registered: `CheckAll` now calls it before any
checker sees the history, and a malformed history reports **inconclusive** — a harness failure, not a
verdict.

### Induced

Removing the `Counters.Check` call site fails the lane, naming the mechanism, what it refuses, and who
was supposed to invoke it:

```
ASSERTION NEVER INVOKED: sim.Counters.Check ran zero times on an ordinary run.
  refuses:    a run that injected less than its plan asserts
  invoked by: hunt.RunToy, after the run
```

### Two instruments, deliberately not one

This lane answers **was it called**. Whether a mechanism that was called still *catches* anything is
the mutant suite's question. Conflating them is how a repository ends up with checks that run and
prove nothing, which is the failure one level along from the one this lane closes.
