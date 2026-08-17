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

### The window: still a limitation, now an enforced one

**Wrong framing:** the harness fails to detect a present bug at a 2ms fsync window.

**Right framing:** at 2ms *there is no incorrect behaviour in existence to detect.* fsync completes
before a replication round trip does, so a primary awaiting backup acknowledgements is already
durable when it answers — the flawed toy and the correct toy are behaviourally identical. No oracle
could find anything, and no amount of crash targeting would help.

**The window does not tune the detector. It selects whether the flaw manifests at all.** That is a
limitation, not a gap.

That fact is now enforced rather than remembered. `toy.New` validates the modelled fsync against the
plan's own `ReplicationRTT` and refuses to construct a node in a regime below the margin, so the
2ms and 10ms cells report **0 of 0 eligible, 1000 refused as blind**. Eligibility is per seed, since
per-seed link latencies vary — which is a sharper statement than the old global curve could make: a
window that is productive on a fast seed's network is blind on a slow one's, and a refused seed
belongs in neither numerator nor denominator.

The historical curve, measured before the gate existed, is retained because it is what justified the
gate:

| modelled fsync | detections / 1000 | seeds to first |
|---|---|---|
| 2 ms | 0 | not detected |
| 10 ms | 2 | 663 |
| 50 ms | 82 | 29 |

**An open question for Ansh.** `MinWindowMargin` is 3, and at that margin the gate refuses the entire
10ms row — a regime that historically produced 2 detections in 1000. So the gate is *conservative*
relative to its own stated rationale: it refuses some regimes where the flaw can manifest, rarely,
rather than only those where it cannot. Safe direction, but it is stricter than the sentence
defending it, and the margin should either be lowered to ~1 or the rationale reworded to say
"reliably reachable" rather than "reachable". This is flagged, not decided.

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

**Cross-invocation hash, seed 4242, darwin/arm64:**

```
a679fba6bc13468491e9cb06745609810d97c9e145925f658f8bd5d15574e6de
```

Unchanged by this cycle's work, which is the point of recording it: `--workload none` still generates
its plan from `plan.DefaultGenConfig`, so the number stays comparable against CI's runner the day the
remote lands. If that runner is amd64 it is the FMA defence's first cross-architecture datapoint.

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
