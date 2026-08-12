# DESIGN-A0.6: the event loop, as landed

**Status:** **RATIFIED** by Ansh, 2026-08-11 (checklist step 1), with the outcome enum hardened one
notch on the same ruling.
**Phase:** A0.6 / checklist step 1. **Author:** Claude. **Decider:** Ansh.
**Decides nothing new.** The design is DESIGN-A0 D1 (single-threaded event loop) and D2 (discrete
event simulation, total order `(at_nanos, insertion_seq)`), both approved. This records what landed
and names the two findings the work produced.

---

## 1. What landed

One queue, one node at a time, virtual time advancing only at event boundaries. `Node.Handle(ev,
scheduler)` is the sole entry point: synchronous, non-blocking, no goroutines inside a node — so a
data race in node logic is unrepresentable rather than unlikely, and the same `Handle` runs in real
mode behind a mailbox so the two modes cannot drift apart in behaviour.

Ticks come from each node's own clock, which is where A0.4's closed-form inversion meets the loop.
This is the mechanism behind the rule that **drift shapes tick rate, not only `Now()`**: a node whose
oscillator runs fast reaches its next tick earlier in global time, so it campaigns and heartbeats
fast. Measured end to end: over ten simulated seconds at a 10ms interval, a +5000 ppm node ticks
1005 times, nominal 1000, a −5000 ppm node 995.

A restart begins a new boot, so the monotonic curve restarts at zero and the tick schedule restarts
with it. A crashed node receives nothing, and events scheduled for it while it is down are **dropped
rather than deferred** — a message to a dead process does not wait for it to come back.

Setup asserts every node advertises the same `maxOffset`, at the place the Q2 ruling put it: before
the run, because two nodes with divergent bounds are each individually self-consistent and nothing
downstream can detect it afterwards.

---

## 2. Named finding: signal below quantization

**What happened.** The first version of the drift test ran over one simulated second at ±2000 ppm.
One second at a 10ms tick interval is 100 ticks; 2000 ppm of 100 ticks is **0.2 ticks**. The
difference the test was asserting on could not exist, because ticks are integers. It passed for the
nominal node and failed loudly only because the fast node happened to land on exactly the same
count — had the boundary fallen differently it would have *passed*, while measuring nothing.

**The standing rule, ruled 2026-08-11 and recorded in DESIGN-A0:**

> Any assertion over an accumulated discrete quantity states its expected signal and demonstrates
> that the signal exceeds quantization noise. A delta the rounding can erase is not an assertion.

**Applied here:** ten seconds at ±5000 ppm is 1000 nominal ticks and a five-tick signal either way,
stated in the test's own comment so the next person to shorten the window sees why they should not.

This is the dangerous kind of weak test, because it is green. A test that fails when it should pass
gets fixed within the hour; a test that passes while measuring rounding is banked as evidence.

---

## 3. Named finding: deadline/quiescent conflation

**What happened.** `scheduleTick` suppressed ticks falling past the run's `Until`. The consequence
was that a tick-only run ran out of scheduled work exactly at the deadline, emptied its queue, and
reported itself **quiescent** — erasing the distinction between *"the clock ran out with work still
pending"* and *"nothing was left to do"*.

**Why that distinction is not cosmetic.** A run that goes quiet early did less than its configured
duration suggests. Every soak number is a sum over runs; banking a quiet run as a full one inflates
the single number the entire verification claim rests on.

**The fix.** Ticks are scheduled past the deadline; `Run` stops on the first event beyond it. A
tick-only run now ends at the deadline with a non-empty queue and says so.

**The hardening, ruled on the same day.** The outcome became a **closed enum** rather than a
boolean:

| kind | meaning | banked as soak hours? |
|---|---|---|
| `OutcomeDeadline` | reached the end of time with work still scheduled | **yes — only this one** |
| `OutcomeQuiescent` | the queue emptied early | no; logged with its quiet point and investigated |
| `OutcomeHalted` | an oracle fired | no; a result, not a duration |
| `OutcomeStepLimit` | the event budget ran out before the clock did | no |

`OutcomeStepLimit` is a fourth kind beyond the three the ruling named, added under the ruling's own
reasoning: a runaway workload that exhausted its budget is not completed-at-deadline, is not
quiescent, and no oracle fired. Folding it into any of the three would put a lie in the ledger, which
is the exact failure the closed enum exists to prevent.

**Forward binding, recorded now:** SOAK.md accounting counts only `OutcomeDeadline` runs toward
cumulative hours. Quiescent runs are logged with their **quiet point** and investigated, never
silently banked. Step-limit runs are investigated the same way with a different first clue: their
**step census** — the per-kind event counts the run accumulated — because a run that fired a million
ticks and delivered nothing is a liveness smell, while one that delivered a million messages is a
workload that needs bounding. The census costs one array and is tomorrow's first question answered
today. `Outcome.CountsTowardSoakHours` is the single place that rule lives, so adding a kind forces
a decision there rather than defaulting to "sure, count it".

**And the type system holds the line.** `determinismcheck` gained an `exhaustive` rule: a switch over
a closed enum must cover every variant and may carry **no default arm**. A default is the specific
danger — it makes an omission invisible forever, which is precisely what adding a variant has to
break. The rule deliberately runs in **every** package rather than only in core scope, because the
consumer that matters most is the soak runner deciding what to bank as an hour, and that is
orchestration, exempt from every determinism rule.

Both findings stay out of BUGS.md under the harness-bug rule. This is their record.
