# DESIGN-A0.7: transport, codec and injectors, as landed

**Status:** **RATIFIED** by Ansh, 2026-08-11 (checklist steps 3 and 4).
**Phase:** A0.7 / checklist steps 3–4. **Author:** Claude. **Decider:** Ansh.
**Decides nothing new.** The design is DESIGN-A0 D6 (fire-and-forget transport, real wire codec) and
D9/DR-14 (injector set, fire-count assertions), both approved. This records what landed and the two
properties Ansh blessed by name, each with the forward binding that now depends on it.

---

## 1. Blessed property: link independence, and why ddmin depends on it

**The property.** Per-message dice are a keyed PRF over `(from, to, ordinal on that directed link)`.
The outcome for one message depends on nothing but its own identity, so traffic on one link cannot
perturb another link's dice.

**The test that proves it.** `TestDiceAreIdentityKeyedNotSequential`: 200 messages on link `0→1`
deliver identically whether or not 200 unrelated messages are interleaved on link `2→0`. Under
sequential draws that noise would shift every subsequent outcome on every link.

**What it is a precondition for.** Delta-debugging works by removing plan elements and asking whether
the failure survives. That question is only meaningful if removing element *X* changes *X* and
nothing else. Under sequential randomness, deleting a fault entry shifts every draw after it, so the
minimizer would attribute a behaviour change to the entry it removed when the real cause was a moved
draw — and it would happily "minimize" one bug into a different one.

> **Binding: when `minimize` lands, its design doc cites this test as a precondition.** The minimizer
> is not sound without it, and the citation is the paper trail for that claim.

`TestDeletingAFaultEntryPerturbsOnlyItself` checks the same property one level up, at the plan: a
crash entry removed from a plan leaves an unrelated link's delivery history byte-identical.

---

## 2. Blessed property: partition cuts are directed

**The property.** A cut is one directed link. A symmetric partition is two cuts; a **single** cut is
the asymmetric case — a node that can send but not receive, or receive but not send.

**Why asymmetry is not an edge case.** It is the geometry behind a whole family of consensus
pathologies. A leader that can send heartbeats but not receive responses believes it is healthy while
the cluster elects around it; a node that can receive but not send accumulates a view it cannot act
on; and a node that can only *send* raises terms nobody can answer. Symmetric partitions never
produce any of it, so a harness with only symmetric cuts has a permanent blind spot in exactly the
region where the interesting bugs live.

The generator makes half its partitions one-way for that reason, rather than leaving asymmetry to
chance.

> **Bindings, recorded now:** A1's schedule mix weights the asymmetric case explicitly. Pre-vote's
> eventual justification in DESIGN-A2 cites this geometry — pre-vote exists because a partitioned
> node returning with an inflated term disrupts a healthy leader, and the term inflation comes from
> exactly this shape.

---

## 3. The wire codec, and why fidelity beat throughput

Simulated messages cross the production encoder by default (DR-9). Two reasons, in order of how much
they matter:

1. **Aliasing.** Nodes share an address space. A message passed by reference lets a sender mutate
   what a receiver reads: a bug class that cannot exist in production and a determinism leak that
   can. Encoding removes it, and every delivery copies the frame again so two copies of one message
   cannot share a buffer either.
2. **Free fuzzing.** Every message in every soak run exercises the encoder, so truncation, a field
   dropped after a schema change, and unbounded sizes are caught by the corpus rather than at I2. It
   also makes message *size* observable, which the delay model reads and a future bandwidth model
   will need.

The encoding is fixed-width big-endian in a fixed order with an explicit length — no reflection, no
map iteration, no varint — so it is a property of the struct definition rather than of anything
discovered at run time. Every truncation of a valid frame is tested to fail, along with trailing
bytes, an unknown version, and an oversized length field. That last one is how a corrupt frame
becomes an out-of-memory if the length is trusted before it is checked.

A reduced-fidelity fast path remains permitted **only** on measured hunt-throughput evidence, and it
must still deep-copy.

---

## 4. Reordering is emergent, and the count is not a rate

Reordering falls out of independent per-message latency with a heavy tail; there is no reorder knob
to look for. The test asserts that reordering *occurs* — a model without the tail would deliver in
order forever — and deliberately does **not** assert a rate.

The reason is recorded because the restraint is the point: every send in that test happens at one
instant, so delivery order is decided purely by latency and roughly half of everything arrives out of
order. That fraction is an artifact of the test, not a property of the network.

> **The refusal pattern, named:** an unasserted count with a comment explaining why it cannot be a
> rate is the correct residue of the signal-below-quantization rule (DESIGN-A0). Deleting the count
> entirely would have hidden the decision; asserting it as a rate would have banked an artifact as
> evidence. Keep the number, refuse the inference, say why.

---

## 5. Fire counts

Every injector counts. `Require` declares a floor, and an enabled injector that never fired is a
**failed run**, not a pass. Without it a chaos suite is chaos-shaped decoration: every seed green, no
partition ever formed, nobody the wiser.

`Check` returns every shortfall in a stable order rather than the first, so a hunt reports all of them
in one pass, and the envelope mode — where some injectors are deliberately off — decides for itself.

A representative run of 3000 sends: 2973 delivered, 29 dropped, 2 duplicated, 14 tail excursions.
Those are the numbers that make "10k seeds, zero violations" mean something.
