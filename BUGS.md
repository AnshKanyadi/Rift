# BUGS.md

Every bug caught by the simulator, the crash-consistency rig, or differential testing gets an entry
here. This file is the proof behind the verification claim: it is the difference between "we ran a
lot of tests" and "we can show you what the tests found and why they found it."

**Rules**

1. Every bug found by a checker gets an entry. No exceptions for embarrassing ones — especially not
   for embarrassing ones.
2. Every entry must be reproducible: a seed (at the commit that contained the bug) **and** a plan
   bundle in `seeds/` (reproducible at any commit).
3. Every entry must answer **"which mutant class would have caught this?"** If none would have, a new
   mutant is added to `sim/toy/mutants` **in the same PR as the fix** — not as a follow-up.
   *(CLAUDE.md Amendment A2.)*
4. A bug that a checker did *not* catch — found by inspection, by a real-mode run, or by luck — is
   the most valuable entry in the file. It must additionally record what checker was missing and
   whether one was added.

**Counts:** 0 entries (engine bugs; the fenced harness-defect section below is counted separately and does not satisfy this gate). *(A0 is in progress; the phase gate for A1 requires this file to be nonempty,
because a harness that finds nothing is a harness that is too weak.)*

---

## Template

Copy this block. Do not drop fields; write "n/a" with a reason instead.

```markdown
### BUG-NNN — <one-line symptom, in the voice of what an operator would see>

| field | value |
|---|---|
| **Found by** | sim / crash rig / differential / real-mode chaos / inspection |
| **Phase** | A1 |
| **Reproduce (seed)** | `simctl replay 8834127` at commit `<sha>` |
| **Reproduce (plan)** | `simctl run --plan seeds/BUG-NNN/plan.json` (any commit) |
| **Invariant that caught it** | e.g. Election Safety |
| **Mutant class** | e.g. `M2-ack-before-replicate`; or "none existed — added `M8-…` in this PR" |
| **Fix commit** | `<sha>` |
| **Minimized?** | yes — N fault entries, M ops, K nodes |

**Symptom.** What the checker reported, verbatim where possible.

**Root cause.** The actual mechanism, not the patch. Written so someone who has never seen this code
can follow it.

**Why the checkers caught it here and not earlier.** Which injector had to fire, in what order.

**What this would have caused in production.** Be concrete and be honest: silent data loss, a stalled
range, a lost acknowledged write, a duplicated transfer. If the answer is "nothing user-visible," say
that.

**Fix.** What changed and why that is the right fix rather than a narrower one that would also make
the seed pass.
```

---

## Entries

*(none yet)*

---

## Harness and lane defects

**This section is not the entry list above and does not substitute for it.** Entries here are defects
in *checkers* — lanes, rigs, mutation harnesses — found by other checkers. They are recorded because
rule 1 says every bug a checker finds gets an entry and makes no exception for embarrassing ones, and
they are fenced off because they are not engine bugs:

- they do **not** count toward the A1 phase gate's "BUGS.md is nonempty" requirement, which is about
  the simulator finding bugs in the *protocol*;
- they do **not** count toward the `[K] documented postmortems` figure in CLAUDE.md's resume lines,
  which is about faults injected into a running system;
- they **do** count as evidence that the induced-failure discipline works, which is the only reason
  either of these was visible at all.

Counts: 11 entries.

### The two general forms these entries taught

Recorded here rather than only inside the entries, because the instances are
cheap and the forms are not.

**GF-1 — a lane that verifies an ABSENCE must run in a state where the thing
could actually be present.** From HARNESS-002 and HARNESS-004. An absence
verified in a state that could not have contained the thing is not a
verification, it is a tautology wearing a lane's clothes. `cpp-ci` claims no
lane touches the network; it was resting on a warm FetchContent cache rather
than on the absence of a fetch, and the isolation it did have worked perfectly
and had nothing to block. Track A has hit the cousin of this twice. The cold
cache is now part of what `cpp-ci` MEANS and is asserted at both ends —
`scripts/cpp-cold-cache.sh`, induced by `COLD-fetch-despite-isolation`.

**GF-4 — AN UNSATISFIABLE GATE. A classification that decides whether evidence
counts must be tested in BOTH directions, because the safe-looking direction is
the one no assertion notices.** From HARNESS-006.

Every other entry in this file is a check that COULD NOT FAIL. This is the
opposite shape and it needs its own name: a check that could not PASS. A
classifier that marks too much as non-evidence breaks nothing — the engine still
behaves correctly, every assertion still holds, every lane stays green — and the
only consequence is that a column stays empty. It is invisible precisely BECAUSE
it is conservative.

**The consequence is the sharpest part.** The cost does not arrive where the
defect is. It arrives one or two steps downstream, as a gate nothing can
satisfy: §7.4 condition 3 requires both elements of the two-element recovery set
to have been observed across the sweep, and a sweep whose runs are structurally
uncountable as evidence can never satisfy it. Found there, **it presents as a
bug in the engine rather than in the classifier, so the debugging starts in the
wrong component** — which is the expensive part, not the fix.

The audit this forced is in HARNESS-006. Closed by §7.5's registry holding
exactly its two named members, asserted both ways, and by every other
evidence-deciding function in Track B now being asserted both ways too.

**GF-3 — when an end-to-end test cannot distinguish two designs because both
fail the same way, assert the discriminating property directly on the unit where
the two differ.** From BM10. Our WAL checksum covers `length ‖ type ‖ payload`;
LevelDB's covers `type ‖ data`. End to end the two are nearly
indistinguishable — a corrupted length fails the checksum under either coverage —
because **the difference is not in what happens, it is in what is KNOWABLE at
the moment of failure**: with the length covered, the failure is at a known
offset and resync has a sound starting point; without it, the bytes consumed
before the failure are a function of data recovery has already decided not to
trust. So the property is asserted on `FragmentCrc` itself: *same type, same
payload, different length ⇒ different checksum*, which is false under upstream's
coverage and true under ours, in one line, with no log image involved.

The corollary is about who the defence is aimed at. **A deliberate divergence
from a well-known upstream is not attacked, it is helpfully corrected.** BM10 is
the one mutation in this catalogue a reviewer would most likely *approve*: it
introduces no bug, it removes two bytes of work, and it makes us match LevelDB,
whose header is byte-identical to ours. A defence written against a defect would
be pointed the wrong way. This one is pointed at a competent, well-meaning
reader — which is why the reasoning lives on the helper as a comment and not
only in a design document nobody re-reads before a cleanup.

**GF-2 — a two-field assertion where both fields read the same value under the
defect is not an assertion.** From HARNESS-003. Track A has recorded this shape
twenty-four times; it appeared in Track B's first cycle of real code, in a
different language and a different subsystem, written by someone who had read
all twenty-four. That is not a coincidence and it is not about C++ or about
ledgers: **the class is about how verification code gets written.** The reflex
it demands is to ask, of every assertion, "what value would this read if the
thing I am checking were broken?" — and if the answer is "the same one", the
assertion is decoration no matter how specific it looks.

### HARNESS-001 — a mutation lane's scratch copy silently lost three files of the tree under test

| field | value |
|---|---|
| **Found by** | `make cpp-mutants`, its own baseline gate |
| **Phase** | B1.0 |
| **Reproduce** | n/a — not seed-driven. `tar cf - --exclude=./.github .` from the repo root, then count files under `third_party/googletest/.github` in the result: 0, not 3 |
| **Invariant that caught it** | vendored-tree integrity (DESIGN-B1 §9.2) — the hash check ran *inside* the scratch copy and disagreed with the recorded hash |
| **Mutant class** | none needed — the baseline gate is the mechanism, and it is the `blind`-lane pattern already required by Amendment A2 |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `INVALID — the unpatched tree does not pass lane "cpp-ci"`,
with the vendored-tree check inside the scratch copy computing a different tree hash and counting
247 files where the working tree has 250.

**Root cause.** `copy_tree` excluded VCS metadata with `tar --exclude=./.git --exclude=./.github`.
bsdtar matches an exclude pattern against any suffix of a stored path, not only against the root, so
`./.github` also matched `./third_party/googletest/.github` — three files that upstream tracks.
The scratch tree was therefore not the tree under test.

**Why it was caught here.** Only because a vendored dependency with its own `.github` directory
arrived in the same step as the lane, and only because the vendored-tree hash check runs inside the
copy rather than only in the working tree. Without that check the copy would have been wrong and
every mutant result would have been about a slightly different tree, indefinitely and invisibly.

**What this would have caused.** Mutant verdicts computed against a tree that is not the repository.
For BM21 the three missing files are inert, so the verdict would have been right by luck; nothing
about the mechanism guarantees the next one would be.

**Fix.** Copy everything, then delete the root paths by name. A glob that "usually" anchors is not an
anchor. Recorded at the call site, because the next person to add an exclusion needs the reason.

### HARNESS-002 — `cpp-ci` passed under network isolation because its build directory was warm

| field | value |
|---|---|
| **Found by** | mutant `BM21-network-in-build` |
| **Phase** | B1.0 |
| **Reproduce** | apply `engine-cpp/mutants/BM21-network-in-build.patch`, run `make cpp-lane-set` (network available), then `make cpp-ci`. Before the fix: green |
| **Invariant that caught it** | no lane touches the network (DESIGN-B1 §9.2) |
| **Mutant class** | `BM21-network-in-build` — it existed, it fired, and the lane was wrong rather than the mutant |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `ALIVE  BM21-network-in-build: cpp-ci stayed green`.

**Root cause.** `cpp-ci` reused `engine-cpp/build`. CMake's `FetchContent` populates at configure time
and skips the download when `_deps/` is already populated, so any earlier networked build left behind
exactly the artifact that makes a network dependency invisible. The lane's premise — that a clean
clone with no network can build — was true only when the build directory happened to be cold.

**Why it was caught here.** Because BM21 is bidirectional by construction: the same patched tree must
go green with a network and red without. The control direction ran first, warmed the cache, and the
covering direction then measured the cache instead of the network. Two bugs met — one in the lane,
one in the mutation harness sharing a tree between directions — and the mutant surviving was the only
signal either produced.

**What this would have caused.** `cpp-ci` reporting that no lane touches the network while a lane
touched the network. The failure would surface for the first time in the hands of the stranger the
lane exists to protect: a clean clone, offline, one script, red.

**Fix.** Two, because there were two defects. `cpp-ci` now builds in its own directory and deletes it
first, so the lane is cold by construction rather than by habit. And `cpp-mutants` gives each
direction of a mutant its own tree, because a control run and a covering run are independent
experiments and one must not be able to feed the other.

### HARNESS-003 — the ledger's promotion flag was never under test, and a mutant proved it

| field | value |
|---|---|
| **Found by** | mutant `LEDGER-always-promoted`, which **survived** |
| **Phase** | B1.3 |
| **Reproduce** | apply `engine-cpp/mutants/LEDGER-always-promoted.patch` against the tree at `cf12938` and run `make cpp-test`: green |
| **Invariant that caught it** | none — that is the entry. The mutant survived, and the survival is the finding |
| **Mutant class** | `LEDGER-always-promoted`, added at B1.3 alongside the ledger it blinds |
| **Fix commit** | this one |

**Symptom.** `make cpp-mutants` reported `ALIVE  LEDGER-always-promoted: cpp-test stayed green`.

**Root cause.** The lying-Sync test asserted on `LastPromisedBytes`, a helper that scans the ledger for
entries with `promoted == true` and returns the last `durable_bytes_after`. Under a suppressed
promotion `durable_bytes_after` is 0 whether or not the entry claims to have promoted, so the two
fields agreed at zero and the flag itself was never read by any assertion. The ledger's whole job is to
record what *happened* rather than what was *reported*, and the field carrying that distinction was
unchecked.

**Which of the three things a surviving mutant means.** A checker that cannot see it. Not a defence
that was never there — the flag was set correctly — and not unreachable code, which is the only one of
the three whose correct response is deletion. The response here is to strengthen the checker.

**What this would have caused.** Nothing in production; the engine does not read the ledger. It would
have cost the *oracle*: B1.9a's exactness assertions are required to derive their verdict from the
ledger and from nothing else, and a ledger whose promotion column had silently become a copy of the
Sync's return value is the engine's account of itself wearing harness clothing. It would have been
discovered, at best, as an unexplained pass at B1.9b.

**Fix.** The test now asserts on `promoted` directly, in both directions: a lying Sync's entry must
read `promoted == false` with `injection == kSyncLoss`, and a clean Sync's must read `promoted == true`
with the right byte count. A flag asserted in only one direction degenerates into a constant.

**The class, not the instance.** See GF-2 above. Both fields reading zero under the defect is the same
shape Track A has recorded twenty-four times, and it arrived in Track B's first cycle of code that does
anything. The lesson is not about ledgers, or about C++: it is about how verification code gets
written, and it will arrive again in B1.6's byte digest and B1.9a's oracle unless the question "what
would this read if the subject were broken?" is asked of every assertion.

### HARNESS-004 — the cold-cache check asserted the absence of something it had just deleted

| field | value |
|---|---|
| **Found by** | inducing the gate, by hand, in the same cycle that wrote it |
| **Phase** | B1.4 |
| **Reproduce** | at the first draft of `scripts/cpp-cold-cache.sh`: `mkdir -p engine-cpp/build-ci && make cpp-ci` — green |
| **Invariant that caught it** | none. The induced-failure rule caught it: the gate was written, run, and did not fire |
| **Mutant class** | `COLD-fetch-despite-isolation` covers the *after* half; the *before* half is induced by hand, `mkdir engine-cpp/build-ci && make cpp-ci` |
| **Fix commit** | this one |

**Symptom.** The gate written to prevent HARNESS-002 recurring was created, wired, and induced — and
the induction printed `*** GATE DID NOT FIRE ***`.

**Root cause.** `cpp-ci`'s recipe ran `rm -rf $(CPP_BUILD_CI)` and *then* called the check. The check
asserted that the build root did not exist, immediately after the lane had deleted it. It was green
unconditionally, including in the exact state HARNESS-002 occurred in.

**Why this one is worth an entry despite never being committed.**

**It is the first time in either track that a general form recurred inside its own remedy.** GF-1 was
being written down, in the same working session, by someone with the sentence in front of them — and
the gate written to enforce it violated it one line later. The draft and the fix were
**indistinguishable by reading**. They were distinguished **in four seconds by running**.

That is the entire lesson, and it is larger than this gate. **The induced-failure rule is not a
formality applied to gates once they are written; it is the only thing that distinguishes a fix from a
fix-shaped edit.** Every other check in this repository — review, the general form itself, the author's
attention — passed this draft. One `mkdir` and one `make` did not. A rule you can state and still
violate one line later is a rule that needs a mechanism, and this is the mechanism.

**Fix.** The check no longer removes what it checks for — *a check that removes the thing it is
checking for is a check that cannot fail*. `cpp-ci` refuses when the build root exists and says how to
clear it; a successful run removes its own tree at the end, so the next run is cold; a failed run
leaves its tree for whoever has to debug it.

### HARNESS-005 — a pointer-keyed container in `TestEnv`, and the split labels refusing to absorb it

| field | value |
|---|---|
| **Found by** | implementing the scan rule that catches it (`A5-ADDRESS`), at B1.4 |
| **Phase** | B1.3, found at B1.4 |
| **Reproduce** | at `3239469`: `std::map<const void*, std::string> handles_` in `engine-cpp/src/env/test/test_env.cc` |
| **Invariant that caught it** | §6.1 — "nothing may depend on an address — no pointer-keyed containers, no address-ordered anything", which §9.4 says the scan checks |
| **Mutant class** | `A5-ADDRESS`'s fixture and blind patch; `A5-ADDRINT` was added at B1.5 for the arithmetic half the rule was missing |
| **Fix commit** | `187a3eb` |

**Symptom.** The first run of the new rule over `engine-cpp/src` reported a violation in code ratified
the previous cycle.

**Root cause.** `TestEnv` mapped an Env handle's address to its path, in order to know which file a
fault was being injected against. §6.1 bans that outright. **No behaviour was wrong**: the map is
looked up and never iterated, so no address ordering was ever observable.

**The disposition is the finding, not the defect.** The obvious move was a registry entry, and the
registry would not take it. `covered-by` requires naming an instrument that catches the class instead,
and nothing caught it. `unreachable` requires naming a detector that would have seen it if it could
occur, and it *did* occur — the logic was right there. **A taxonomy that refuses to absorb a defect is
doing its job.** With a free-text reason field the entry would have written itself: "looked up, never
iterated, harmless" — true today, unexaminable tomorrow, and indistinguishable from the seventeen
single-labelled opt-outs Track A spent a full cycle re-deriving and found three of them wrong. **One
label absorbing two meanings is how a real gap comes to look accounted for.**

So it was fixed rather than exempted, which is the only remaining option once both labels decline.

**A determinism win falling out of a hygiene fix.** `HandleId` replaced the pointer through the whole
Env surface: an integer assigned sequentially by the creating Env, so the same workload assigns the
same ids on every run and every machine. That makes a kill point reportable as
`Sync(handle 3, 000001.log)` rather than `Sync(0x7f9c4a005e10)` — a bug report instead of a number that
means nothing on the second run. §9.5 asks for exactly that and would have had to build it separately;
here it arrived as a consequence of obeying §6.1.

**What this would have caused.** Nothing, until someone iterated the map — at which point the fault
schedule would have depended on the allocator, and a kill-point sweep would have injected against
different files on different runs while reporting the same ordinals.

### HARNESS-006 — a prefix-granular torn `Sync` was classified as exactness-suspending

| field | value |
|---|---|
| **Found by** | writing B1.7b's discard test, which needed a torn `Sync` and found it marked the run non-evidence |
| **Phase** | B1.3, found at B1.7b |
| **Reproduce** | at `dfba754`: `SuspendsExactness(Injection::kTornSync)` returns true |
| **Invariant that caught it** | none — no lane could catch it. The classification decides what a run may be *banked* as, and every run it mislabelled still passed every assertion |
| **Mutant class** | `REGISTRY-lying-sync-not-suspending`, which existed and could not see this: it checks that members ARE members, not that non-members are not |
| **Fix commit** | this one |

**Root cause.** B1-D5 rules two things and they were collapsed into one. Prefix granularity — a kill
inside `Sync` promoting `content[0:k)` — is **the contract model**, the thing §7.4's two-element set
`R ∈ {G_{k−1}, G_k}` describes, and the engine is held to exactness under it. Only the *sector-subset*
mode, where an arbitrary set of 4 KiB sectors is promoted, suspends: that is a device violating fsync's
own ordering guarantee, and holding the engine to exactness there would report the engine for the
disk's crime. B1.3 implemented one injector and mapped it onto the suspending registry member.

**A NEW SHAPE: an unsatisfiable gate.** This does not belong beside the vacuous-green entries and is
not filed with them. Every one of those is a check that *could not fail*. This is a check that *could
not pass*. It survived four ratified steps because it was **conservative**, and conservative is the
direction no assertion notices: the engine behaved correctly, every test held, every lane was green,
and the only symptom was that a column would have stayed empty. `REGISTRY-lying-sync-not-suspending`
existed and could not see it — it is pointed at a member that stops suspending, and nothing was
pointed at a non-member that starts.

**What it would have cost, and where.** Not here. At B1.9a: §7.4 condition 3 requires that *both*
elements of the two-element recovery set were observed across the sweep, and runs that are
structurally uncountable as evidence can never satisfy it. **Found there, it presents as a bug in the
engine rather than in the classifier, so the debugging starts in the wrong component.** That is the
expensive part. The fix is four lines.

**The general form is GF-4**, and §7.5 of DESIGN-B1 cross-references it: the registry holding exactly
its two named members, asserted in both directions, is what closes this.

**THE AUDIT IT FORCED, and its result.** The same question was asked of every place in Track B that
decides whether a run counts as evidence. Six exist:

| function | decides | both directions asserted before the audit? |
|---|---|---|
| `SuspendsExactness` | whether a run is characterization-only | **no** — this entry |
| `OutcomeFloor` | the same, one layer up | **no** — only ever called with `true` |
| `OutcomeForCapVerdict` | whether a cap verdict is a pass, a void or a violation | **no** — `kNormal` never asserted |
| `IsDivergence` | whether a cap verdict fails the run | **no** — only the two true cases |
| `CountsAsRecoveryEvidence` | what may be banked | yes, all five kinds |
| `AggregateRuns` | whether runs may be banked *together* | yes, both regimes |

**Three more instances, all the same shape**, and two of them reachable: `OutcomeFloor` returning
`kCharacterizationOnly` unconditionally, and `OutcomeForCapVerdict` filing a normal run as `kVoid`.
Both make the evidence column permanently empty with nothing going red. Mutants `FLOOR-always-suspends`
and `VERDICT-normal-is-void` now exist for exactly those, and all six functions are asserted in both
directions.

**Fix.** The injector is split: `kTornSync` (prefix, the contract model, does not suspend) and
`kSectorSubsetTornSync` (an arbitrary sector left unpromoted, suspends). The registry now has exactly
the two members §7.5 names. The test asserts **both directions** — that the two members suspend and
that the prefix mode does not — because a classification asserted in one direction is the shape GF-2
already names.

### HARNESS-008 and HARNESS-009 — the other two evidentiary deciders, side by side

Found by the audit HARNESS-006 forced. Recorded together because **the shape is identical and the
consequences differ in a way worth seeing beside each other** — that pair is the argument for the
both-directions rule in two lines.

| | HARNESS-008 | HARNESS-009 |
|---|---|---|
| **where** | `rig::OutcomeFloor` | `rig::OutcomeForCapVerdict` |
| **the untested direction** | `OutcomeFloor(false)` — it had only ever been called with `true` | `kNormal` — the two divergences and `kVoid` were asserted, a normal run never |
| **the defect it admits** | suspend unconditionally | file a normal run as `kVoid` |
| **what that costs** | **every run becomes unbankable** | **the evidence column empties permanently** |
| **what turns red** | nothing | nothing |
| **mutant** | `FLOOR-always-suspends` | `VERDICT-normal-is-void` |

Both are GF-4. Both are conservative, which is why neither is visible: the engine is correct, the
arithmetic is correct, every assertion holds, and the only symptom is a number that never appears.
One kills the evidence at the source and one kills it at the sink, and **the pair is the reason the
rule is "both directions" rather than "test the interesting case"** — there is no interesting case
here, only two boring ones whose absence is invisible.

Closed structurally: `engine-cpp/DECIDERS.txt` enumerates every function that decides evidentiary
status, `scripts/cpp-scan.sh` requires each to name the tests asserting **both** of its directions, and
a decider that lands without them fails the lane. Six is a small enough population to enumerate, and
enumerating it is what stops the seventh arriving in B2 and the audit being re-run by hand.

### A shape three of this cycle's defects shared

**HARNESS-006, `HEADER-conditional`, and the near half of BM2's survival are all the same thing: a
check placed somewhere that something else decides whether it runs.** The exactness classification ran
off an injector enum that had collapsed two ruled cases into one; the FILE_HEADER validation ran inside
a loop bounded by whether a `GROUP_END` existed; the discard assertion ran on a workload where the
records it was checking never reached a file. In each, the check itself was correct and its *reachability*
was decided elsewhere — so it passed, and reported on a situation that had not occurred.

Worth one sentence rather than three entries, because the remedy is one habit: when writing a check,
ask what has to be true for it to run at all, and assert that too.

### The shape behind every mutant that has survived its first induction

Four have: `LEDGER-always-promoted`, `BM2-accept-torn-tail`, `BM7-drop-close-error`,
`BM1-ack-before-sync`. All four are meaning #1 — a checker that cannot see it — and all four have
**one shape**:

> **The test never created the situation it was checking.**

`BM7` is the cleanest exemplar: a Close test that only ever ran a *successful* Close cannot distinguish
a propagated error from a swallowed one, whatever it asserts about the return value.

`BM1` is the subtlest, and it sharpened the rig. The oracle learned the engine's watermark **only from
a `Sync`'s return value** — and a killed `Sync` returns nothing, so an engine that advanced the
watermark before writing a byte was invisible: the premature value died with the process. The fix is
not a bigger test but a wider definition of the promise. **`DurableSeq()` is a durability claim like
any other**, so the rig now records every value it is ever told and holds the engine to the highest,
and the induction runs the failure through an fsync that *errors* rather than one that kills — so the
process survives to be asked.

This is **§22.6c's discriminator rule arriving in C++ independently** — a check must be run in a state
where the thing it discriminates could actually differ — and it is cited rather than restated. It is
also the same family as GF-1, one level in: GF-1 is about a *lane* verifying an absence, this is about
an *assertion* verifying a distinction. Filed once here; individual survivals are not entries.

### HARNESS-007 — `Slice` bound silently to temporary strings, and a test dangled

| field | value |
|---|---|
| **Found by** | AddressSanitizer, inside `make cpp-mutants`'s **baseline gate** |
| **Phase** | B1.7b |
| **Reproduce** | at `dfba754`, with `Slice(std::string&&)` still permitted: `Op op; op.key = Slice("k");` |
| **Invariant that caught it** | none by design. The baseline gate ran `cpp-asan` on the unpatched tree before reporting any kill, and refused to report |
| **Mutant class** | none, and none is added: the fix is a compile error, so there is no runtime behaviour left for a mutant to blind |
| **Fix commit** | this one |

**Root cause.** `Slice` had `Slice(const std::string&)` and no `const char*` overload, so `Slice("k")`
constructed a **temporary `std::string`** and pointed into it. The Slice outlived the full expression;
the buffer did not.

**Why the baseline gate is the entry.** The mutant lane refuses to report kills until the unpatched
tree passes every lane a patch names — a rule borrowed from `make blind` after a lane there reported
seven kills while one of the tests doing the killing was failing for its own reasons. Here it turned an
unattributable run into a named ASan stack trace.

**Defects found by the baseline gate while doing its actual job: 2** (HARNESS-001, HARNESS-007). The
count is kept because it is the argument for the gate. Its stated purpose is to make kills
attributable; what it has actually done twice is find defects nothing else was looking for, in a tree
that every other lane called green. A mechanism that keeps paying outside its stated purpose is worth
more than the purpose.

**Fix, and why it is structural rather than local.** `Slice(std::string&&) = delete;` makes binding to
a temporary a **build failure**, and a `const char*` overload makes the literal case point at static
storage. Twenty call sites had to hoist their strings into named locals; every one of them was a latent
instance of the same bug, safe only by accident of lifetime. A class of dangling-pointer bug became a
class of compile error.

### HARNESS-010 — the sweep could not see BM2, because the snapshot was hiding the damage

| field | value |
|---|---|
| **Found by** | measuring the sweep's power against every class, per GF-4's sibling discipline in §10.3 |
| **Phase** | B1.9b |
| **Reproduce** | apply `BM2-accept-torn-tail`, run `make cpp-sweep` before the post-reopen continuation existed: 175 points, 175 passes |
| **Invariant that caught it** | none. The **measurement** caught it: a class that should be sweep-detectable measured **0 of 175** |
| **Mutant class** | `FLOOR-continuation-removed`, which makes the regression repeatable |
| **Fix commit** | this one |

**Root cause.** With BM2 applied, recovery applies BATCH records past the last `GROUP_END` — records that were
never committed. They land in the memtable at sequences **above** the recovered watermark, and every read goes
through a snapshot pinned at that watermark, so **they are present and unreadable**. The oracle compares the
visible state, which is correct, and passes.

That is not a defence. It is an accident of the read path, and it expires: at **B2 the flush writes the memtable
out**, and uncommitted records become durable, visible and permanent. The engine would have shipped a recovery
path that quietly retains data it never promised, with a 175-point sweep reporting 175 passes.

**Why the measurement found it and no assertion could.** Every lane was green and correct. The sweep visited every
kill point, observed both elements of the recovery set, and reported no violation — all true. What was wrong was
its **power**, and power is not a property any single run can assert about itself. §10.3 exists for exactly this,
and it is the first thing it found.

**Fix.** The sweep now **continues after reopening**: one write, then a comparison. A reopened database keeps
serving, and the new write takes the sequence the hidden records already occupy, so they become visible at exactly
the moment a real database would have resumed service. BM2 went from 0 to **194 per mille**, first detected at
kill point 14.

**The floor that keeps it.** `BM2`'s rate floor is 90 per mille — roughly half the measurement, and set against
**the suppressed number rather than under today's**: the value that matters is the 0 this class measured before the
continuation existed. `FLOOR-continuation-removed` induces exactly that regression, and its control is
`cpp-sweep` **staying green** — the lane whose job is finding defects is perfectly healthy while its power has
collapsed.

### HARNESS-011 — TestEnv's ledger under-reported what a torn `Sync` promoted

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, on its first run with torn modes enabled, against the **unpatched** tree |
| **Phase** | B1.3, found at B1.9b |
| **Reproduce** | before the fix: a torn `Sync` whose prefix covers the whole newly covered extent records `promoted=false` |
| **Invariant that caught it** | the exactness oracle — it reported a violation at kill point 35 |
| **Mutant class** | none added: the ledger field is now written from the durable image itself, so there is no separate flag to blind |
| **Fix commit** | this one |

**Symptom.** `VIOLATION at ordinal 35 (kWritableFileSync, before effect): recovery landed on sequence 6, a batch
boundary strictly inside a group.`

**Root cause.** `promoted` was set by `RecordPromotion`, which only runs when `DoSync` runs. A torn `Sync` kills
*instead of* running `DoSync` — so however much of the extent it actually promoted, the ledger said it promoted
nothing. When the prefix happened to cover the entire group, durability really had advanced, and the oracle,
reading `promoted=false`, refused to offer the in-flight element of the recovery set and **reported the engine for
landing exactly where the ledger's own bytes said it should**.

**The lesson, and it is not the one it looks like.** Ruling 4 says an oracle that interrogates the engine believes
the lie. This is one level in: **a harness record that under-reports is as damaging as an engine that
over-reports**, and it is worse in one way — it blames the engine. The ledger is now written from the durable image
before and after the injection, so it records what happened rather than which code path ran.
