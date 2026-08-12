# DESIGN-A0.8: the plan as the unit of reproduction, as landed

**Status:** **RATIFIED** by Ansh, 2026-08-11 (checklist step 5), with three corrections applied.
**Phase:** A0.8 / checklist step 5. **Author:** Claude. **Decider:** Ansh.
**Decides nothing new.** The design is DESIGN-A0 D4 and DR-6, approved. This records what landed,
the house pattern the work named, and the two stated preconditions `minimize` will inherit.

---

## 1. House pattern: proving an absence

> **Absence is proven by poisoning the presence path and completing anyway.**

It is the induced-failure family applied to a negative. You cannot demonstrate that no code drew
sequentially by reading the code; you demonstrate it by making a sequential draw fatal and then
running to completion.

**The correction that makes it real.** A poison installed for one exit test proves absence only along
the paths that one plan happens to exercise — a different fault mix reaches different code, and the
test says nothing about it. So **every built run carries the poisoned Rand by default**, and the
completion of *any* seed is the proof for that seed. Ten thousand seeds are ten thousand proofs, each
covering exactly the paths it touched.

The exit test remains, in the other role: it asserts the poison is **live**. A dead poison — one
replaced by a working generator during a refactor, or one nothing is handed — silently converts the
entire guarantee into decoration, and nothing else would notice.

---

## 2. `minimize`'s two stated preconditions

Delta-debugging removes plan elements and asks whether the failure survives. That question is only
meaningful if removing element *X* changes *X* and nothing else. Two tests establish it, at two
levels, and **`minimize`'s design doc must cite both**:

1. **`TestDiceAreIdentityKeyedNotSequential`** (transport level, DESIGN-A0.7 §1) proves the
   *mechanism*: per-message dice are a keyed PRF over `(from, to, ordinal)`, so traffic on one link
   cannot perturb another's outcomes.
2. **`TestDeletingAFaultEntryPerturbsOnlyItself`** (plan level) proves the mechanism *survives the
   plan layer*: a crash entry removed from a plan leaves an unrelated link's delivery history
   byte-identical.

**Its scope, stated honestly.** The second test demonstrates independence for **one deletion of one
entry class** — a node-scoped fault, observed on a link whose endpoints that fault never touches. It
does **not** demonstrate independence for entries that are causally coupled by construction: a
partition and the retries it provokes, a crash and the reconnection traffic that follows, a hold and
the elections its skew shapes. Those couplings are real and this evidence does not cover them.

> When `minimize` is built, its design doc cites both tests **and states which couplings it does not
> yet have evidence for.** A minimizer that assumes independence it has not demonstrated will
> "reduce" one bug into a different one and report the reduction as the same bug.

---

## 3. Floats die at authoring

**The ruling, and the hatch that was refused.** A `float64 at_frac` in the serialized plan was
proposed with a hatch. It was rejected, and the reasoning is the sharpest statement of the float rule
yet: **the plan *is* replay identity.** A fraction carried in the plan is multiplied on the
*replaying* machine, and `off + slope*(t-start)` is exactly the multiply-add an arm64 fuses into one
FMA and an amd64 without FMA does not. Approving that hatch would have reconstructed the
cross-architecture last-bit divergence the float rule exists to kill — this time with a corpus
depending on it, and an exemption blessing it.

**What landed.** The float dies at authoring. `clock.Percent`, `PerMille` and `PPB` are the authoring
vocabulary and are exact integer constructors; a hold's target is parts per billion of `maxOffset`
end to end; the serialized plan carries integers only. `clock/frac.go` is deleted — there is no float
boundary left in the clock or plan path to concentrate.

**The evidence, structural rather than by inspection.** `TestPlanCarriesNoFloatingPoint` checks two
ways, because either alone has a hole:

- **Type graph.** It walks the whole reachable field graph of `plan.Plan` — through structs, slices,
  maps and pointers — and fails on any float kind. A grep of one struct would miss a float added
  three types down. It also fails on an `any` field, since a float could pass through one unseen.
- **Serialized values.** It decodes a real materialized plan with `json.Number` and requires every
  number to parse as an integer. The type walk cannot see a value smuggled through an interface;
  this can.

Induced, per standing policy: planting one `float64` field makes the type walk fail and name it.

**Registry effect:** three hatches removed (`clock/hold.go`, `clock/frac.go`, `sim/plan/plan.go`),
taking `HATCHES.txt` from **11 entries to 8**.

---

## 4. When the generator and the model collide, fix the generator's physics

The generator gave every node free drift and then layered a hold on the result, which the clock
package rejects: a hold *is* oscillator discipline, so its target would depend on when it began.

**The principle:** fix the generator's physics, never loosen the model. Free drift under a hold was
the generator lying about the crystal, and a model relaxed to accept the lie would have accepted it
from everyone forever.

**The correction:** the generator is not the only author of plans. Hand-written plans, corpus entries
edited during an investigation, and any future fuzzer bypass a generator-side fix entirely. So
legality is enforced at **plan validation** — a hold on a drifting node, two holds on one node, a
slew with no ramp, a ramp beginning before time zero, an empty window — and the illegal shape is
rejected no matter who wrote it. The generator fix stays; validation is what makes it a rule.

---

## 5. Harness actions are events

A link cut is scheduled, not applied at build time. **The class it avoids: build-time topology
contradicting the plan's own delivery claims.** A partition applied at setup silently cuts messages
the plan says should have crossed, so the run reproduces a different schedule than the one it claims
to — and nothing reports the difference, because both halves are internally consistent.

The general rule follows: **every harness action is an event, ordered against the traffic in flight
when it fires.** Any future injector tempted by build-time convenience semantics answers to this.

---

## 6. Derived-field discipline

`clock.Hold` rejects an unset realization rather than inferring it from `Ramp`. Sibling of the
nonzero wall epoch: a forgotten field must not read as a decision, and a field derived from another
field cannot be flipped independently — which `ddmin` must be able to do, converting a slew into a
step to ask whether the bug survives.
