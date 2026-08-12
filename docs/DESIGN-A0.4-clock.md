# DESIGN-A0.4: Clocks, drift, and the skew envelope

**Status:** **APPROVED** by Ansh, 2026-08-11. Architecture (two-clock model, closed-form tick
inversion, intent-form holds, inverting envelope checker) plus all five §4 questions, all amendments,
and the A0.4b implementation ruling. Amendments are marked inline **[Amended — Ansh, 2026-08-11]**.
Nothing here is provisional.
**Phase:** A0.4 (Track A). **Author:** Claude. **Decider:** Ansh.
**Blocks:** A0.6 (the event loop schedules ticks through this), A5 (HLC wraps `PhysicalNow`),
A8 (lease disjointness and the envelope experiment consume the schedules defined here).
**Consumes:** DESIGN-A0 D2 (virtual time), D9 and DR-15 (clock injectors), the A0.3 rulings
(`clock/` is core scope; the real implementation takes a per-line hatch).

---

## 0. What A0.4 must deliver

From CLAUDE.md and DESIGN-A0's landing plan: `Clock` interfaces, a sim clock with drift and jump
schedules, and a skew checker, gated on skew property tests.

Two requirements carry over from the A0 rulings and are treated as fixed inputs rather than open
questions:

1. **Drift shapes tick rate, not only `Now()`.** A node whose oscillator runs fast campaigns and
   heartbeats fast. Drift that moved only `PhysicalNow()` would be cosmetic (D9).
2. **The plan format exposes a hold-at-boundary primitive** that pins a node at ±`maxOffset` for a
   window, *and* a schedule that deliberately exceeds it. A8's envelope experiments consume both
   directly, and A0.4's exit criteria include a demonstration schedule of each.

A0.4 does **not** build the HLC. That is A5. It must not preclude it.

---

## 1. Problem

Three consumers want different things from "the time", and conflating them is how clock bugs become
safety bugs:

- **Safety decisions** (A8 leases, A5 uncertainty intervals) need each node's *estimate of wall
  time*, which drifts from other nodes' estimates and may be stepped by NTP. `maxOffset` bounds the
  disagreement, and every lease argument rests on that bound holding.
- **Timeouts** (elections, heartbeats, lease renewal) need *elapsed* time, which must never run
  backwards. A real system takes these from a monotonic clock precisely so an NTP step cannot make a
  timer fire early or hang for an hour.
- **The simulator** needs both to be pure functions of virtual time, computable without an event
  loop, so that A0.4 can ship and be tested before A0.6 exists.

The tension is between fidelity and simplicity. One clock per node is simpler and wrong in a way
that matters: it makes a backward jump rewind the election timer, which no real system does, and
would let us "discover" bugs that cannot happen while hiding the ones that can.

---

## 2. Decisions

### D1 — One clock per node, or two?

**Candidates**

- **(a) One clock.** `Now()` returns the node's wall estimate: virtual time plus a per-node offset
  schedule covering drift and jumps. Ticks are scheduled off it.
- **(b) Two clocks per node, one oscillator.** A **monotonic** clock, affected by drift only, never
  jumping, strictly increasing; and a **wall** clock, the monotonic reading plus a step function of
  jumps. Ticks come from monotonic; leases and HLC come from wall.
- **(c) One wall clock plus a global tick rate.** Ticks fire on a fixed global cadence, unaffected by
  the node's clock.

**Tradeoffs**

(c) is the simplest and violates requirement 1 outright: drift stops shaping tick rate, so a slow
node no longer campaigns slowly, and the injector becomes cosmetic. Rejected on the ruling.

(a) is one field and one function, and it mismodels the case we care most about. Under a backward
jump of 500ms, a node's election timer would stall for 500ms of extra real time, and under a forward
jump it would fire instantly — neither of which happens in production, because `time.After` and
`time.Since` read `CLOCK_MONOTONIC`. We would spend seed budget on artifacts.

(b) mirrors POSIX exactly, at the cost of one extra field and one extra method. Drift belongs to the
oscillator and therefore affects *both* readings — which is what requirement 1 asks for — while jumps
are corrections applied to the wall estimate only. The safety story lines up: leases and uncertainty
intervals are the things bounded by `maxOffset` and the things a jump perturbs; timers are the things
that must not rewind.

**Recommendation: (b).**

```go
package clock

// Instant is nanoseconds on a single node's timeline. It is deliberately not
// time.Time: no location, no embedded monotonic reading, no formatting, and no
// way to accidentally compare two nodes' instants as if they were the same
// timeline.
type Instant int64

type Clock interface {
    // Mono is elapsed nanoseconds on this node's oscillator. Strictly
    // increasing; unaffected by jumps. Everything that measures a timeout
    // reads this.
    Mono() Instant

    // Wall is this node's estimate of physical time: Mono plus accumulated
    // steps. It moves backwards when a jump does. Everything bounded by
    // maxOffset -- leases, uncertainty intervals, the HLC in A5 -- reads this.
    Wall() Instant

    // MaxOffset is the assumed bound on |Wall_i - Wall_j| across the cluster.
    // Safety arguments quote it; the envelope experiment violates it.
    MaxOffset() int64
}
```

**Rejected:** (a) — a backward jump would rewind timers, which no real system does, manufacturing
bugs we cannot ship and hiding ones we can. (c) — drift stops shaping ticks, contradicting the
ruling and making the injector decorative.

#### [Amended — Ansh, 2026-08-11] The monotonic epoch is per-boot

`Mono` is elapsed time **since this boot**, not since the simulation began. A `restart` event starts
a fresh monotonic curve at zero; `Wall` continues from the oscillator plus accumulated steps, because
a restarting machine does not forget what time it is. Restart times are materialized in the plan, so
the closed-form inversion of D3 holds unchanged — it is applied per boot segment, and the segment
boundaries are known before the run starts rather than discovered during it.

This mirrors `CLOCK_MONOTONIC`, whose epoch is unspecified and in practice per-boot, and it makes a
whole bug class expressible rather than accidentally impossible:

> **Named bug class — monotonic leakage.** A monotonic-derived value must never be persisted, sent on
> the wire, or compared across nodes or across a restart. It is meaningful only as a difference
> between two readings on the same node within one boot. A lease expiry stored as a `Mono` value
> survives a restart as a number from a timeline that no longer exists, and the node then serves
> reads under a lease it does not hold.

That is a design-doc invariant today and a review-checklist item until there is a mechanical check
for it. A mechanical check is possible — `Instant` could split into distinct `Mono` and `Wall` types
so the compiler rejects the mix, and serialization could refuse the monotonic type — and if the
review item is ever hit in practice, that is the fix rather than more vigilance.

---

#### [Found while implementing — Ansh to rule] A slew cannot be wider than its ramp

`Compile` rejects a hold whose correction exceeds its ramp duration, because applying it would need
the oscillator to run backwards on the way out. This is not an implementation limit: it is the same
constraint that makes real implementations rate-limit slewing rather than letting `adjtime` take
arbitrary corrections. The caller's options are a longer ramp or `Ramp: 0` for a step, and those are
genuinely different experiments rather than two spellings of one.

Consequence for A8's schedules: holding a pair at 0.98 of a 500ms `maxOffset` needs a ramp longer
than 490ms. The demonstration schedules use 2s and 3s ramps. If A8 wants a *fast* approach to the
boundary, that is a step, and the doc now says so where somebody will read it before writing the
plan.

---

### D2 — How is a node's timeline expressed, and by what arithmetic?

Per D9 the plan carries a **piecewise-linear** offset schedule. A0.4 has to say precisely what it is
linear *in*, because the answer decides whether ticks can be computed in closed form.

**Recommendation.** Two separate curves per node, both authored in the plan:

- **`skew`** — a piecewise-linear function of global virtual time `t`, giving the node's oscillator
  offset: `osc_i(t) = t + skew_i(t)`. A sloped segment is a drift *rate* (slope 1000 ppb is 1 ppm;
  see the §4 refinement on why the slope is an integer);
  a flat segment is a **sustained hold**, which is what A8 needs. Slope is constrained to
  `(-1, +∞)`, i.e. `mono_i` is strictly increasing — an oscillator that runs backwards is not a
  fault, it is a different kind of object.
- **`steps`** — a list of `(at_ns, delta_ns)` jumps, applied to the wall reading only:
  `wall_i(t) = mono_i(t) + Σ{delta : at ≤ t}`. Deltas may be negative.

Both are pure functions of `t`, evaluable in constant time per segment with a cursor, and both are
invertible where it matters (see D3). The skew checker asserts on `wall`, since `maxOffset` bounds
disagreement about physical time, not about oscillators.

**Why not express drift as a rate with a start time** (`drift node=n rate=ppm from=t`): a rate is a
special case of a segment slope, and a rate-only model cannot express a hold, which DR-15 already
established is the case A8 actually needs. Keeping one representation with two readings of it is
cheaper than two representations.

---

### D3 — Where do ticks come from?

Requirement 1 in mechanical form: node *i*'s tick *k* fires at the global time `t` satisfying
`mono_i(t) = k · interval`. Since `mono_i` is strictly increasing and piecewise linear, that
inversion is exact and cheap — walk to the segment containing the target, solve one linear equation.

**Candidates**

- **(a) Closed-form inversion**, as above. `NextTick(node, afterGlobal) Instant` is a pure function;
  A0.6's event loop calls it to enqueue the next tick and nothing else.
- **(b) Fixed global cadence, node counts ticks it "should" have had.** Approximates drift by
  dropping or doubling ticks.
- **(c) Re-evaluate drift at each tick and schedule `interval / (1 + slope)` ahead.** Correct within
  a segment, wrong across a segment boundary, and the error accumulates silently.

**Recommendation: (a).** It is the only one that is exact, and exactness here is worth having because
the tick schedule is the thing that decides election timing, which is the thing seeds are spent on.
(b) quantizes drift to whole ticks, so a 200 ppm drift is invisible until it accumulates a whole
interval. (c) is a bug generator: it looks right in every test with a single segment.

**Consequence, stated because it is load-bearing:** ticks are driven by `mono`, so a backward wall
jump does **not** rewind the tick schedule. That is the behaviour of every real system and the reason
D1 recommends two clocks.

**Tick interval** is one global constant in plan config (`tick_interval_ns`), not per-node. Per-node
tick *rates* emerge from drift, which is the point; randomized election timeouts are A1's business
and come from the PRF, not from the tick source.

---

### D4 — The hold-at-boundary primitive

A8 needs a cluster that *sits* just short of the skew cliff across many lease acquisitions,
transfers and expirations. Authoring that as absolute per-node offsets is possible and miserable: it
is pairwise reasoning ("2 is 490ms ahead of 1") written in absolute coordinates, and every edit to
one node's schedule silently changes every pair it participates in.

**Candidates for what the plan file carries**

- **(a) Compiled per-node schedules only.** Execution is trivial. The intent is unreadable, the
  minimizer can only delete whole schedules, and a bundle stops being a bug report a human can read.
- **(b) The authored primitive only, compiled at load.** The plan says what was meant; a pure,
  RNG-free compiler turns it into per-node curves at load time.
- **(c) Both, with a checker asserting they agree.** Belt and braces, and two sources of truth that
  will diverge the first time someone hand-edits a bundle.

**Recommendation: (b).**

```jsonc
"clock": {
  "max_offset_ns": 5e8,
  "tick_interval_ns": 1e7,
  "nodes": [ { "node": 2, "skew": [[0, 0], [4e9, 4.9e8]], "steps": [{"at_ns": 9e9, "delta_ns": -3e8}] } ],
  "holds": [
    // Pin the pair at 98% of maxOffset from 10s to 40s: a flat segment, not a sweep.
    { "a": 1, "b": 2, "at": "0.98*max_offset", "from_ns": 1e10, "to_ns": 4e10, "ramp_ns": 2e8 },
    // The A8 envelope: deliberately outside the assumption.
    { "a": 1, "b": 3, "at": "1.20*max_offset", "from_ns": 2e10, "to_ns": 3e10, "ramp_ns": 2e8,
      "envelope": true }
  ]
}
```

A `hold` compiles to segments on the named nodes' `skew` curves: ramp in over `ramp_ns`, hold flat,
ramp out. `at` is a fraction of `maxOffset` rather than an absolute nanosecond count, so a plan that
holds *at the boundary* keeps holding at the boundary when `maxOffset` is changed — which is exactly
what an envelope sweep varies. Holds are individually deletable, so ddmin can ask "does the bug still
happen without the hold?", which is the first question anyone will ask.

`envelope: true` marks a hold as deliberately outside the assumption. Its effect is on the checker
(D5), not on the arithmetic.

#### [Amended — Ansh, 2026-08-11] Compiled holds record step or slew

A hold reaches its target one of two ways, and the compiled output records which:

- **step** — `ramp_ns == 0`, realized as a `steps` entry: a discontinuous correction, an NTP step.
- **slew** — `ramp_ns > 0`, realized as a sloped `skew` segment: the oscillator is disciplined
  gradually, which is what `adjtime` and every well-behaved daemon actually do.

They stress different consumers and must not be conflated in a corpus. A step is the A5 case: the HLC
must not go backwards, and an uncertainty interval must widen across the discontinuity. A slew is the
A8 case: a lease's stasis margin is consumed continuously while the node believes its clock is fine,
so the failure is gradual and the detection story is different. A bundle that says only "the clocks
disagreed by 490ms" cannot tell an investigator which of those they are looking at.

The realization is therefore a recorded field on the compiled segment rather than something inferred
from `ramp_ns` at read time, `simctl` prints it in the run summary, and **ddmin can distinguish
them** — "does the bug still happen if this hold slews instead of steps?" is a question the minimizer
should be able to ask, and a minimized bundle should say which answer it found.

**Rejected:** (a) — unreadable bundles and a minimizer that can only delete whole timelines. (c) —
two representations of one fact, guaranteed to disagree eventually.

---

### D5 — The skew checker, and what "envelope mode" means

The checker asserts, at every step where a clock is read: `max_{i,j} |wall_i(t) − wall_j(t)| ≤
maxOffset`. Two modes:

- **Safety runs (default).** A violation is a **harness failure**, not a protocol failure, and says
  so in its message. The generator constrains schedules to satisfy the bound by construction, so if
  the checker ever fires, the generator is wrong — and a generator bug that quietly exceeded our own
  assumption would present as a protocol violation and cost days (DR-15).
- **Envelope runs (`--skew-envelope`, or any plan containing a hold marked `envelope`).** The
  checker **inverts**: it records the realized excess rather than failing, and the safety checkers
  are *expected* to fire. Characterizing what breaks, and how we detect it, is the experiment.

Two properties I want asserted in A0.4 rather than assumed later:

1. **Realized, not intended, skew is what gets recorded.** The checker reads the compiled curves,
   not the authored holds, so a compiler bug shows up as a discrepancy rather than as agreement
   between two things computed the same way.
2. **The bound is checked continuously, not at hold boundaries.** A ramp that overshoots between two
   sample points is exactly the bug this checker exists to catch. Because both curves are piecewise
   linear, pairwise `|wall_i - wall_j|` is piecewise linear too, so its extrema occur at segment
   endpoints — the checker can therefore be *exact* by evaluating at the union of both nodes'
   breakpoints, rather than sampling and hoping.

#### [Amended — Ansh, 2026-08-11] Exactness, spelled out so it can be tested

"Evaluate at the breakpoints" is not yet a specification. The evaluation set is the **union of both
nodes' breakpoints**, and it explicitly includes:

- every `skew` segment endpoint on either node;
- **hold ramp endpoints** — ramp-in start, ramp-in end, ramp-out start, ramp-out end — which is where
  a compiled hold's extremum sits when the two nodes' ramps are not aligned;
- **hold window edges**, which are breakpoints of intent even when the compiler emits no segment
  boundary there;
- every `steps` discontinuity, at which **both one-sided limits are evaluated**. A jump is a
  discontinuity in `wall`, so the supremum of `|wall_i - wall_j|` may be attained on either side and
  is attained by neither if only the post-jump value is sampled. This is the case a naive
  implementation gets wrong, because evaluating "at the jump" reads as one point and is two.

The exit criterion (§3.1) is correspondingly sharpened: a fixture in which the skew maximum exists
**only strictly inside a ramp**, between sample points a reasonable implementation would have chosen
— midpoints, segment starts, a fixed grid — so that a sampling checker demonstrably passes a schedule
that violates the bound. Under the standing policy that a gate is not landed until its failure has
been induced, that fixture is what induces this one.

---

### D6 — The real clock, and the one hatch

Per the A0.3 ruling, `clock/` is in core scope and the real implementation takes a per-line hatch
rather than living in an excluded package, so that every wall-clock touchpoint in the repo is
enumerable from `HATCHES.txt`.

`clock/real.go` holds **exactly one** `time.Now()` call, hatched, from which both readings derive:
Go's `time.Time` carries a monotonic reading, so `Mono` is the difference between two samples and
`Wall` is the wall component. One hatch, one line, one entry in the registry — and `make hatches`
fails if a second appears.

The sim clock has no hatch at all: it is arithmetic on plan data.

---

## 3. Exit criteria

Signed off by Ansh, not by me. Each is a test, and each names what it would catch.

**Status: all seven met.** `go test ./clock/` is green, and the two demonstration schedules print
their numbers (schedule A: max skew 490,000,000ns against a 500,000,000ns bound, in both
realizations; schedule B: 600,000,000ns, excess 100,000,000ns, 1.20x, recorded not failed).

1. **Skew property tests, and the induced failure of the exact checker.** Over randomized schedules,
   realized pairwise skew never exceeds `maxOffset`; the exact-extrema argument in D5 is tested
   against a dense-sampling oracle on the same schedules, so "exact" is verified rather than
   asserted. Plus the fixture required by the D5 amendment: a schedule whose skew maximum lies
   strictly inside a ramp, on which a sampling checker passes and the exact checker fails. Without
   that fixture the exactness claim is untested, and an untested gate is a decoration.
2. **Demonstration schedule A — the hold.** A plan pins nodes 1 and 2 at 0.98 `maxOffset` for 30
   simulated seconds, **once as a step and once as a slew**. Recorded: realized skew at every
   breakpoint, its min and max over the window, the realization (step or slew), and the checker's
   verdict. Passing means A8 has its substrate before A8 needs it, in both shapes.
3. **Demonstration schedule B — the envelope.** A plan drives the pair to 1.20 `maxOffset`. The
   checker inverts, records an excess of exactly 0.20 `maxOffset`, and the run reports rather than
   fails. Passing means the A8 experiment is expressible today, not a thing we hope to add later.
4. **Drift shapes ticks.** A node at +200 ppm accumulates ticks faster than a node at nominal, by the
   expected ratio to within one tick over a long window. This is the test that would have caught
   D1(c) or D3(b) shipping by mistake.
5. **Jumps do not rewind timers, and restarts reset the right clock.** Under schedules with negative
   steps, `Mono` is strictly increasing within a boot and the tick sequence is unperturbed, while
   `Wall` moves backwards as authored. Across a `restart`, `Mono` returns to zero and `Wall` does
   not — the per-boot epoch of the D1 amendment, tested rather than assumed, since a `Mono` that
   survived a restart is the monotonic-leakage bug class in its most direct form.
6. **Determinism.** Clock-heavy plans produce identical `(node, tick_ordinal, mono, wall)` sequences
   across an in-process rerun and a fresh process. (The rolling trace hash is A0.6; until it exists
   this sequence is the thing compared.)

   *Landed as:* the in-process half is asserted more strongly than "rerun and compare" — every
   reading is a pure function of `(timeline, t)`, so the test evaluates a schedule forwards and then
   backwards and requires identical results, which a cursor or cache would fail. **The fresh-process
   half is deferred to A0.6 and here is why:** spawning a process needs `os/exec`, which is
   orchestration and does not belong in a core package. Hatching it in `clock`'s tests would put the
   wrong thing in the registry. It lands in the runner that owns process spawning, alongside the
   trace-hash gate it was always going to share.
7. **One hatch.** `HATCHES.txt` gains exactly one entry, in `clock/real.go`, and `make hatches` is
   green.

---

## 4. Questions for Ansh

**All five ruled. Q4 by Ansh directly; Q1, Q2, Q3 and Q5 delegated to Claude, 2026-08-11
("your rulings on those four, then A0.4 code"), each adopting the recommendation as written.** A
delegated ruling is still a ruling and is recorded as one; each is a one-line change to overturn, and
the code says which line.

#### [Amended — Ansh, 2026-08-11] Q1's implementation: two types, A0.4b

`Instant` alone was the right direction and the wrong implementation. The per-boot amendment created
a bug class -- a monotonic value persisted or sent on the wire -- and one type leaves it reviewable.
**`Wall` and `Mono` are separate defined types**, both `int64` nanoseconds, and the class becomes
uncompilable: the same move as D5 making persist-before-reply structural rather than a rule every
driver has to remember.

- **Encoding.** The wire codec implements `Wall` only. `Mono` has no encoder anywhere; its
  `MarshalJSON` exists solely to fail loudly if a reflection-based path reaches it. No serializable
  struct carries a `Mono`.
- **The vet rule.** `determinismcheck` rejects a `clock.Mono` in an exported or tagged struct field
  outside `clock`, in core *and* driver scope, with a fixture and a blind patch. Exported-or-tagged
  is the test because those are the fields that leave the node; an unexported, untagged field is
  node-local by construction, which is what a `Mono` is for.
- **Arithmetic.** Same-type instants subtract to a `Duration`; an instant plus a `Duration` is the
  same instant type; cross-type arithmetic does not compile.
- **Zero value.** The simulated wall epoch is a nonzero plan constant, so a zero `Wall` reads as
  unset rather than as the beginning of the run. `Timeline.Validate` rejects a zero epoch.
- A5's HLC wraps `Wall` only.

*The residual gap, and how it closed.* Because these are defined integer types, `a - b` on two
`Mono`s compiles and yields a `Mono` rather than a `Duration`. Making them structs would close it and
cost the comparison operators and every constant expression, which was rejected.

**[Amended — Ansh, 2026-08-11] Closed by analyzer rule instead.** `determinismcheck` bans binary
arithmetic between two values of the same instant type, in core and driver scope, outside `clock`
itself. `Sub` and `Add` are the sanctioned spellings; comparisons stay legal, which is what defined
integer types bought in the first place. The rationale is that `a - b` on two `Mono`s yields an
instant when the quantity in hand is a *duration*, and that type lie then flows into instant-typed
positions -- the same confusion the two types exist to prevent, one level down. An untyped constant
takes the instant's type, so `w * 2` and `w + 1` are caught by the same rule, correctly: an instant
scaled by a scalar is not an instant.

With that landed the uncompilable claim is whole, one analyzer rule wide.

*The predicted class fired.* Within a minute of the nonzero-epoch rule existing, `Compile` was found
building its output `Timeline` field by field and dropping `Epoch` -- a forgotten field that would
have read as a valid instant at the beginning of the run. That is the class the constant was ruled in
to catch, caught on its first day. It stays out of BUGS.md under the harness-bug rule; this is its
record.

*Tick 5's one-nanosecond overshoot is pinned deliberately.* It is the ceiling rounding, and any
future change that makes the reading exact must fail the golden vector and force a conversation
rather than silently reshaping every recorded run.

| # | Question | Ruling |
|---|---|---|
| 1 | `Instant` as a project-owned `int64`, or reuse `time.Time`/`time.Duration` in core signatures? | **Project-owned `Instant`.** `time.Time` carries a location, a monotonic reading and formatting we do not want, and it makes two nodes' timelines look mutually comparable. `time.Duration` stays legal and idiomatic for *durations*. Strengthened since it was written: with the Unix family and `Time.Local` now banned, a project-owned `Instant` is also what stops core code reaching for them. |
| 2 | Does `Clock` expose `MaxOffset()`, or does it come from config alongside the clock? | **On the interface**, with four conditions, all landed in A0.4b: the bound is fixed at construction and immutable for the process lifetime (unexported field, no setter — the compiler, not a test); `AssertUniformMaxOffset` requires every node to advertise the same value and sim setup halts otherwise; the bug class is named in its doc comment — two nodes with different bounds are each individually self-consistent, so a divergence presents as a lease-disjointness violation that is a harness artifact, or as a real violation that a too-generous bound hides; and an envelope experiment exceeds the bound by shaping true offsets in the plan while the advertised bound never moves, which is asserted by `TestEnvelopeShapesOffsetsNotTheBound` — if the bound moved with the experiment, nothing would ever be outside it. |
| 3 | Should the sim clock also model **frequency error in the estimate** (a node that knows it is uncertain), or only true offset? | **Only true offset in A0.4.** Uncertainty intervals are A5/A6 and derive from `maxOffset`, not from a per-node error estimate. Modelling something no consumer reads would be modelling it wrong for free. |
| 4 | `holds` expressed as a fraction of `maxOffset` | **Plain float field** (`"at_frac": 0.98`) — Ansh, 2026-08-11. A string expression is a parser and a parser is a place for bugs. |
| 5 | Does A0.4 land the tick *scheduling* (pure `NextTick`) only, with A0.6 wiring it to the queue? | **Yes**, plus pinned golden vectors in the `internal/rng` style, since tick times feed the trace hash and get locked the way the generator did. Three schedules, one per behaviour the design turns on: a drift ramp, a compiled hold's ramp/flat/ramp, and a per-boot reset. They are self-generated and say so; changing one is only ever correct alongside a deliberate clock change that invalidates the corpus. |

### [Refinement found while implementing — flagged for overrule] Slopes are integers, not `float64`

D2's sketch gives a segment a `float64` slope. **The implementation uses `SlopePPB int64`** — parts
per billion — with 128-bit intermediate arithmetic, and no floating point anywhere on the evaluation
path. The reason is a determinism hazard, not taste:

> The Go spec permits an implementation to fuse floating-point operations: *"an implementation may
> combine multiple floating-point operations into a single fused operation, possibly across
> statements, and produce a result that differs from the value obtained by executing and rounding the
> instructions individually."*

`skew = off + slope*(t-start)` is exactly the multiply-add that arm64 fuses into `FMADD` and amd64
without FMA support does not. The same seed on a laptop and on a CI runner could therefore produce
clock readings differing in the last bit, and a one-nanosecond difference in a lease expiry is a
different history. That would not have shown up in any test on one machine — it would have shown up
as an unreproducible soak failure months later, which is the failure mode this project exists to
avoid.

`at_frac` stays a `float64` in the plan (Q4), because it is used once at *compile* time in a bare
multiply with no addition to fuse, and the result is rounded to an integer immediately. The boundary
is: floats may appear in authored intent, never on the evaluation path.

---

## 5. What this does not decide

- The HLC: its causality rules, its `maxOffset` interaction and uncertainty-interval restarts are A5.
  A0.4 only guarantees `Wall()` is the reading it will wrap.
- Lease validity arithmetic and the stasis rule (never serve past expiration minus `maxOffset`): A8.
- How the event loop interleaves ticks with message delivery at equal virtual times: A0.6, under
  D2's `(at_nanos, insertion_seq)` total order.
