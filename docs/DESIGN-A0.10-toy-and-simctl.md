# DESIGN-A0.10: the toy, the ablation, and simctl

**Status:** landed for review (checklist steps 7 and 8). **Author:** Claude. **Decider:** Ansh.

Step 7 does **not** close with this document. Its two remaining conditions are the uniform-crash
ablation cell and promotion making `ack-before-replicate` observable, neither of which is landed.

---

## 1. The ablation, in the corrected framing

The two available framings are not equally true, and only one of them is.

**Wrong:** the harness fails to detect a present bug at a 2ms fsync window.

**Right:** at 2ms *there is no incorrect behaviour in existence to detect.* fsync completes before a
replication round trip does, so a primary awaiting backup acknowledgements is already durable when
it answers -- the flawed toy and the correct toy are behaviourally identical. No oracle could find
anything, and no amount of crash targeting would help.

**The window does not tune the detector. It selects whether the flaw manifests at all.** That
distinction is the whole difference between a limitation and a gap, and this is a limitation.

### The sensitivity curve, kept as an artifact

| modelled fsync | detections / 1000 | seeds to first |
|---|---|---|
| 2 ms | 0 | not detected |
| 10 ms | 2 | 663 |
| 50 ms | 82 | 29 |

The steepness is the interesting part. **Seeds-to-detection collapsing from 663 to 29 across a 5x
window change** is the quantitative statement of how sharply harness power depends on the regime.
Re-run this curve when A1's real timings land.

**The carry into A1, honestly:** real Raft has its own window between acknowledgement and durability.
This assumption is re-tested against it rather than inherited.

### The constant is defended where it lives

`toy.DefaultSyncLatency` carries the argument at its definition rather than in this document, and
`toy.ValidateWindow` refuses to run when the window does not exceed the replication round trip by a
stated margin. Lowering the constant for speed would otherwise return the whole ack-before-durable
class to unreachable while every seed passed and every lane stayed green -- structurally the same
failure as the crash injector that marked a node down without telling it: **a clean sweep over an
empty search space.** The refusal is itself a gate and its failure is induced in
`TestWindowValidationIsAGate`.

---

## 2. simctl, and the fresh-process gate

`replay` is by definition a fresh-process re-execution, which is why the gate rides here rather than
needing a spawner built and then deleted.

**Why separate invocations and not two runs in one process:** an in-process rerun shares its address
space, its map seeds, and everything initialized once per process. It cannot catch map iteration
order seeded from process state, address-dependent behaviour, or a value captured at package init.
Only separate invocations can. The gate runs four invocations, two of them with `GOGC` and
`GOMAXPROCS` perturbed so that allocation timing and scheduler shape differ while the run does not.

**Cross-invocation hash, seed 4242, darwin/arm64:**

```
a679fba6bc13468491e9cb06745609810d97c9e145925f658f8bd5d15574e6de
```

Recorded for comparison against CI's runner the day the remote lands. If that runner is amd64 it is
the FMA defence's first cross-architecture datapoint.

**Induced, both ways.** Two different seeds must produce different hashes, or the hash is not
covering the run and every agreement it has reported is worthless. And a deliberately perturbed
plan -- one fault entry moved -- produces a divergence report naming the step:

```
DIVERGED
first divergence at step 0: recorded 61311716da1aa79c, replayed 0631e76f5ef262c2
  (agreed for the preceding 0 steps)
```

*Step 0 because the perturbation moved that entry to the front of the schedule; the report names
whichever step first differs, which for a mid-run perturbation is a mid-run step.*

**Stripped-fault replay** is the triage gate as an affordance rather than a procedure:
`simctl replay --bundle DIR --strip-faults` re-executes with every fault entry and rule removed, and
says plainly that a differing hash is expected because the run is different by construction. What is
*not* expected is a violation surviving it.

---

## 3. Standing practices this cycle produced

### Harness-manufactured violation, and the triage gate

**The named class.** A violation produced by the harness or the workload rather than by the system
under test. The generator's out-of-order client sequence numbers were one: 913 of 1000 seeds
reported a non-linearizable history, and every one of them was the generator's fault.

At 913 of 1000 it was unmissable. **At 3 of 1000 it would have been indistinguishable from a real
find** — it would have entered the corpus, replayed faithfully forever, and spent the credibility of
every genuine entry beside it. A poisoned corpus entry is worse than a missing one.

**The gate, standing from here through A6.** Before any violation becomes a corpus candidate, replay
its plan with the fault entries stripped. **A violation that survives with zero injected faults is
the harness or the workload, not the system under test.** It uses the entry-independence property
`TestDeletingAFaultEntryPerturbsOnlyItself` already proved — which is a good demonstration that the
property earns its keep — and lands as a `simctl` affordance rather than a manual procedure.

### The two harness bugs failed in opposite directions

Worth separating, because the report that first described them called both of them "green":

- **The silent one — false negatives.** The loop marked a crashed node down without telling it, so
  its engine kept the unsynced writes a real process death would have taken. The crash injector was
  injecting nothing and the whole ack-before-durable class was unreachable, while every lane
  reported clean.
- **The loud one — false positives.** The generator's out-of-order sequences, 913 of 1000.

A harness can be wrong in both directions and only one of them announces itself.

### seeds-to-first-detection

The harness's power expressed as a number, and the number that says whether a green sweep means
anything. **Every planted flaw class carries its own, recorded per class and tracked across cycles.
A rising number means the harness got weaker and nobody noticed.**

| flaw | detection | seeds-to-first | notes |
|---|---|---|---|
| `ack-before-sync` | 82 / 1000 | 30 | at a 50ms modelled fsync; see the ablation |
| `ack-before-replicate` | 0 / 1000 | — | recorded gap: not observable without promotion |

### The bidirectional gap assertion

**Most recorded gaps rot into folklore; this one cannot.** Each planted flaw declares whether it is
currently observable *and the reason*, and the test asserts both directions: an observable flaw that
is missed fails, and an unobservable flaw that is suddenly caught fails too — because the record has
become wrong and must say so. The gap maintains itself.
