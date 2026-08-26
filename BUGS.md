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

**Counts:** 2 entries (engine bugs; the fenced harness-defect section below is counted separately and does not satisfy this gate). *(Both are Track B's, found by the kill-point sweep at B2. Track A's A1 gate is a separate obligation and is unaffected by them.)*

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

Counts: 2 entries. Both were found by `make cpp-sweep` — the kill-point sweep — in the cycle that
wrote the code, before either reached a commit anyone else would have built. Neither is a bug in
signed work; both are recorded because rule 1 makes no exception for a defect a checker caught early,
and because **what they have in common is worth more than either**: an ordering argument that was
right about the state it named and wrong about the state it did not.

### BUG-001 — a database that had crashed once refused to open ever again

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, first run in the default regime: **41 violations of 300 kill points** |
| **Phase** | B2.4, found at B2.6 — before the code was ever run outside a lane |
| **Reproduce** | `rift_sweep default` at the commit before the fix; every kill from ordinal 26 onward |
| **Invariant that caught it** | the exactness oracle, via `reopen failed: db/000002.log: named by the manifest and absent` |
| **Mutant class** | **BM59-wal-named-before-it-exists**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** After any crash between naming a WAL in the manifest and creating the file, every later
`Open` fails with *"named by the manifest and absent"* — permanently. The database is intact; it
simply cannot be opened again.

**Root cause.** B2-D5 replaces B1's gapless numbering check with *"every file number the manifest
names exists"*. Naming a WAL **before** creating it looks like the safer order — it guarantees no
file exists that the manifest has not heard of — and a crash in that window leaves **a name with no
file**. That name is durable. It persists into every subsequent manifest, so `named and absent`
stops meaning `lost directory entry` from that moment on, and the only available repair is an
exception ("the highest named number may be absent") that then has to be justified at every Open
forever after — and which the sweep also refused, because a *second* crash in the same window makes
the previously-absent name no longer the highest.

**The fix inverts the order.** A WAL is **created, then named, then written to**. A crash between
creation and naming leaves an *empty unnamed file*, which carries nothing and is deleted. Both halves
of the rule then hold with **no exception in either direction**: every named WAL exists, and nothing
in an unnamed one is above what the tables cover.

**What it would have caused in production.** Total unavailability after an ordinary power cut, on a
database with no data loss and no corruption. The worst kind: nothing to recover, nothing to
diagnose, and the engine insisting it was right.

**The general form.** *An ordering that looks safer because it prevents the state you can name may
create the state you cannot.* Both orders leave a crash window; the question is not which window is
smaller but **which one closes itself**. Create-then-name leaves a file that provably carries
nothing; name-then-create leaves a durable claim that nothing can ever discharge.

---

### BUG-002 — recovery would have discarded committed records in a WAL a flush had retired

| field | value |
|---|---|
| **Found by** | `make cpp-sweep`, flush regime: **64 violations of 985 kill points** |
| **Phase** | B2.4, found at B2.6 |
| **Reproduce** | `rift_sweep flush` at the commit before the fix; every kill from ordinal 149 onward |
| **Invariant that caught it** | the exactness oracle, via `db/000002.log: present, not named by the manifest, and holding 46 committed batches` |
| **Mutant class** | **BM62-unnamed-wal-unchecked**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** After a crash between the manifest edit that retires a WAL and the deletion of the file,
`Open` refuses — reporting a WAL that is *supposed* to be there.

**Root cause.** BUG-001's fix rested on an argument: *a present unnamed WAL is one caught between
creation and naming, so it is empty*. That is true of one of the two ways a WAL can be unnamed. The
other is a flush: the manifest drops a WAL's name in the same group that adds the table covering it,
so between that group and the file's deletion there is an unnamed WAL **full of records**. The
emptiness check was an assumption about how the state arose rather than a statement about the state.

**The fix states the property instead of the provenance.** *Nothing in an unnamed WAL may be above
what the SSTables cover.* An empty one satisfies it trivially; a retired one satisfies it because the
table covers it; and a WAL whose records nobody covers — the case worth refusing — still fails it.
This is B2-Q1's **"nothing covered twice"** seen from the file side, and it is a strictly stronger
check than the one it replaced.

**What it would have caused in production.** The same total unavailability as BUG-001, in a narrower
window — and the check that produced it was the one added to *prevent* silent loss, which is the
shape worth noticing: **a safety rule stated in terms of how a state arises rather than what it
contains will refuse legitimate states the moment a second path reaches the same state.**


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

Counts: 13 entries.

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

**GF-5 — AN ACCIDENTAL DEFENCE IS WORSE THAN A MISSING ONE.** From HARNESS-010,
and new to both tracks.

A missing defence measures as missing. An **accidental** one — a property that
holds for a reason nobody chose, in a component that was not trying to provide
it — makes a real gap **measure as covered**, and then removes itself on a
schedule nobody is tracking.

The instance: recovery applied records it never committed, and they were
invisible only because every read went through a snapshot pinned at the
recovered watermark. Correct state, correct lanes, correct counts — and the
whole sweep reported 175 passes on an engine with a live recovery defect. The
read path was not defending anything; it simply had no way to show the damage.
**And it expires at B2**, where the flush writes the memtable out and those
records become durable, visible and permanent.

Two obligations follow, and the second is the one that is easy to skip:

1. When a defence is found to be accidental, say so where it is measured — a
   floor derived from an accident is a floor measuring the accident.
2. **Put its expiry in `CARRY-FORWARD.md` as a dated obligation, not a note.**
   An accidental defence has a date, and the date is the phase that removes it.
   If nobody is holding that date, the gap reopens silently and the measurement
   that would have caught it is the one the accident was inflating.

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

**THIS IS A STANDING PATTERN, NOT A LIST OF INCIDENTS — AND B2'S LAST RUN BROKE THE RUN OF ONE
MEANING, WHICH IS BETTER THAN AN UNBROKEN ONE.** Six have survived a first induction:

| class | meaning | shape |
|---|---|---|
| `LEDGER-always-promoted` | #1, a checker that cannot see it | the test never created the situation it was checking |
| `BM2-accept-torn-tail` | #1 | " |
| `BM7-drop-close-error` | #1 | " |
| `BM1-ack-before-sync` | #1 | " |
| `BM52-current-parsed-leniently` | #1 | " |
| **`BM55-tables-oldest-first`** | **#2, a defence that was never there** | **the patch was aimed at a line a comment claimed was load-bearing and was not** |

**Five of the six share one sentence.** The sixth is the first of a different kind in this track, and
it matters that the tally now has two entries rather than one: a classification that only ever
returns one answer is a classification nobody has tested. `BM55` is what shows the three meanings are
doing work.

`BM55`'s own story is short and is recorded at the code. It reversed the order sources are handed to
`MergedIter`, and every test stayed green **correctly** — the merge orders by KEY, sequences are
unique, so there are no ties for source order to break and the order it is given is irrelevant. The
comment beside that line said otherwise, in words that are true of the *point-read* path a hundred
lines below. **A comment asserting a load-bearing property for a line where it is not load-bearing is
worse than no comment**: it is where the next reader looks for the invariant, and it sends them to a
line nothing depends on. The patch is re-pointed at the walk that carries the property, and the
comment is corrected, in the same diff.

**All six are meaning #1 or #2. None has been meaning #3** — code that cannot be reached — which is
the only one whose correct response is deletion, and the one a tired reader is most tempted to reach
for.

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

`BM52` is the fifth and the plainest, which is why it is worth recording that the shape did not have
to be rediscovered. Eight malformed `CURRENT` bodies were each asserted to be refused, and each was —
**because the manifest it named did not exist**, so a lenient parse failed too, for a reason with
nothing to do with parsing. One line fixed it: put a real `MANIFEST-000001` in the directory, and
`MANIFEST-1\n` becomes a body a lenient parse *resolves*.

This is **§22.6c's discriminator rule arriving in C++ independently** — a check must be run in a state
where the thing it discriminates could actually differ — and it is cited rather than restated. It is
also the same family as GF-1, one level in: GF-1 is about a *lane* verifying an absence, this is about
an *assertion* verifying a distinction. Filed once here; individual survivals are not entries.

**THE STANDING QUESTIONS, now that the tally has two meanings in it.** A survival is a fork, not a
verdict, and the two questions are different:

1. *What must be true for this assertion to run at all?* — assert that too. In all five meaning-#1
   survivals that question was the whole of the fix.
2. *Is the line this patch is aimed at actually the line that carries the property?* — `BM55`'s
   question, and the one nobody asks while a comment is answering it for them.

Reach for meaning #3 last. Nothing in this catalogue has been unreachable code, and it is the only
meaning whose correct response is to delete something.

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

---

### HARNESS-012 — the oracle's durability fact rested on there being exactly one file

| field | value |
|---|---|
| **Found by** | `make cpp-sweep` in the flush regime, against the **unpatched** engine, after BUG-001 and BUG-002 were fixed |
| **Phase** | B1.9a, found at B2.6 |
| **Reproduce** | before the fix: `rift_sweep flush` reports violations at kill points 149, 159, 167, 178, and `rift_sweep default` at 59 |
| **Invariant that caught it** | the exactness oracle reporting the ENGINE for landing on a group the ledger's own bytes said was durable |
| **Mutant class** | **ORACLE-facts-last-sync**, added in the same PR as the fix |
| **Fix commit** | this one |

**Symptom.** `VIOLATION at ordinal 149 (kWritableFileSync, before effect): recovery landed on sequence
46, a batch boundary strictly inside a group.` The engine was right; the harness was wrong three
different ways in one line of code.

**Root cause.** `FactsFrom` computed `in_flight_durability_applied` as *the `promoted` flag of the
last `kWritableFileSync` entry in the ledger*. Every clause of that was true only by accident:

1. **the last file, not the WAL.** A group lives in the WAL. Until B2 the WAL was the only file this
   engine ever synced, so "the last Sync" and "the WAL's Sync" were the same entry. The flush syncs
   three — the table, the manifest, and the WAL — and reading the last one reports the *manifest's*
   durability as the group's.
2. **only a `Sync`, not any call that promoted.** A torn injection at a `Flush` promotes a prefix,
   and the promotion is recorded on the **Flush** entry. A filter that looked only at Sync calls
   reported "not durable" about bytes that were.
3. **the last one, not any one.** Durability is not undone. The flush creates a *second* WAL inside
   the same `Sync`, and that empty file's own sync promotes nothing — so the last `.log` entry says
   "not durable" about a group made durable moments earlier by the WAL being retired.

**Why all three survived B1.** Without scoping the question to *this* `Sync`, one successful Sync
answers for every group after it. That masked (1) and (2) completely: the last `.log` Sync in the
whole run was almost always a successful earlier one, so the fact was accidentally `true` exactly
when it needed to be. **Scoping made the harness strict, and strictness is what exposed the other
two.** Fixing one defect is what made the others visible — the reverse of the usual order.

**The lesson.** HARNESS-011 said a harness record that under-reports is worse than an engine that
over-reports, because it blames the engine. This is the same shape one level up: **a harness FACT
derived from "the last event of a kind" is a fact about the world having one of that kind.** The
question the oracle asks is "did *this* Sync make *the WAL's* in-flight group durable"; the code now
asks exactly that, scoped, filtered, and monotone.

---

### HARNESS-013 — the mutant lane waited eleven and a half hours for a lane that was never going to report

| field | value |
|---|---|
| **Found by** | reading `ps` while answering "how long will the catalogue take" |
| **Phase** | B1.3 (the lane), found at B2's close |
| **Reproduce** | before the fix: `make cpp-mutants` on a tree where `BM35-tag-sorts-ascending` is reached; it never returns |
| **Invariant that caught it** | none — **nothing was watching**, which is the entry |
| **Mutant class** | none can be added: a permanent catalogue member that hangs would hang the catalogue. The mechanism was induced with a throwaway patch that makes a lane `sleep`, and the watchdog was seen to fire, report TIMEOUT and fail the lane |
| **Fix commit** | this one |

**Symptom.** The catalogue sat at `control BM35-tag-sorts-ascending: cpp-build still alive, as it
must` for **11 hours 34 minutes**, with `rift_engine_test` in that mutant's scratch tree burning
**690 minutes of CPU at 99.3%**. Nothing was wrong with the machine and nothing was logged. I read the
log's last line, saw a lane in progress, and gave an estimate for the remaining patches built entirely
on the assumption that progress was what I was looking at.

**Root cause, two halves.**

*The engine half.* `BM35` inverts the tag half of the internal key order. `IterImpl`'s advance and
retreat loops carry comments reading *"strictly advances, so this loop terminates"* — an invariant
that rests **entirely on the comparator being the order it claims to be**. Invert the comparator and
the loops no longer terminate. `Flush.ReadsSeeTheMemtableAndTheTablesTogether` is where it spins,
because that is the test with a backward scan over a merged view.

*The lane half, and it is the one that matters.* `run_lane` was `( cd "$1" && $MAKE "$2" ) >"$3" 2>&1`
with **no timeout**. A mutation that makes a lane HANG is neither a kill nor a survival: the lane
never reports. So the catalogue waits, forever, and **a waiting catalogue is indistinguishable from a
working one**.

**The fix, both halves.** The loops now `RIFT_CHECK` the progress their comments assert — the user key
is compared bytewise, which is a property of the merged order that does *not* depend on the tag
comparator, so the assertion can catch the comparator being wrong. `BM35` now aborts in **0 seconds**
instead of spinning for eleven hours. And `cpp-mutants` and `cpp-campaign` grew a per-lane watchdog
that kills the whole process tree and reports **TIMEOUT** as a distinct outcome, counted as broken,
failing the lane.

**THREE DEFECTS INSIDE THE REMEDY, ONE INTERACTION, ONE ENTRY.** Naming it once:

> **AN EXPECTED NON-ZERO EXIT UNDER `set -e` KILLS THE SCRIPT THAT WAS SUPPOSED TO INTERPRET IT.**

| # | where | what it did |
|---|---|---|
| 1 | `run_lane`'s `wait` | killed the hung lane correctly, then **died itself, printing nothing** |
| 2 | `run_lane`'s two call sites | the function returned 124 and `; rc=$?` never ran |
| 3 | `build_and_sweep`'s sweep call | **killed the campaign at its first class** |

**The third is the worst, and for the reason the first two are not.** A patched sweep exits non-zero
**by design** — that non-zero *is* the detection the campaign counts — so the failure lands at the
first class **with both baselines already printed and the log reading healthy**. It is the same tell
as the entry itself: a run that has stopped, wearing the appearance of one that has not.

The first two took two rounds of induction to see, because round one looked like *"the lane stopped"*,
which is what the watchdog was supposed to produce. **A watchdog that cannot report is the defect it
was written to fix** — GF-1 recurring inside its own remedy, for the second time in this track.

**The general form, and it is the sharpest one this project has produced about lanes rather than
code:**

> **A HANG IS NOT A FAILURE, AND EVERY LANE MUST BE ABLE TO DECIDE.** A lane reports pass or fail; a
> lane that can do neither has stopped being a lane, and it stops *silently*, wearing the appearance
> of work. This is Amendment A4's "inconclusive is a first-class outcome" arriving one level out: A4
> is about a checker that ran and could not conclude, and this is about a checker that never
> finished. Both must be named, neither may be waited on, and neither is evidence.

**THE SPECIFIC TELL, AND IT IS WHAT COST THE ELEVEN HOURS.** *A stalled log is indistinguishable from
a slow one from outside.* Both show a last line and no error. So **progress must be read from
something that ADVANCES — a counter, a timestamp, a CPU-time delta — and never from the absence of an
error.** Every estimate given during this run was arithmetic over an appearance of progress, and the
appearance was the whole of the evidence.

**THE SHARPER OF THE TWO ENGINE-SIDE FIXES IS THE `RIFT_CHECK`, AND IT IS SHARPER FOR A REASON WORTH
STATING SEPARATELY.** The loops carried comments reading *"strictly advances, so this loop
terminates"* — and that invariant rested **entirely on the comparator being the order it claims to
be**, which is exactly the thing the mutant changes.

> **A TERMINATION ARGUMENT THAT ASSUMES THE THING BEING MUTATED IS NOT A TERMINATION ARGUMENT.**

What replaces it is a progress property that does *not* depend on the comparator under test: the user
key is compared **bytewise**, which is a fact about the merged order rather than about the tag half
of it. So the assertion survives the comparator being wrong and can catch it being wrong — which a
check written in terms of the comparator could not.

**What it cost.** Eleven and a half hours of wall clock and one confidently wrong estimate given to
Ansh. What it did not cost: any result. The 30 patches that completed before the hang all reported
correctly, and the campaign that ran before it was green.

---

### The baseline gate's running tally — four defects, none of them what it was built to detect

The mutant lane's baseline gate exists for one reason: **every lane a patch is declared against must
pass on the UNPATCHED tree first**, because a red baseline makes every subsequent failure
unattributable. It has now found **four defects, and not one of them was an unattributable kill.**

| # | phase | what it caught |
|---|---|---|
| 1 | B1.0 | `HARNESS-001` — `tar --exclude` silently dropped three files from every scratch copy |
| 2 | B1.9a | `HARNESS-007` — a `Slice` bound to a temporary string, caught by ASan in the baseline run |
| 3 | B1.9a | the `-Werror` failures six direction controls separated from real kills |
| 4 | B2.7 | `HARNESS-013`'s third `set -e` defect — `cpp-campaign` red on the unpatched tree |

**The argument this is evidence for: GATES THAT CHECK PRECONDITIONS BEAT GATES THAT CHECK OUTCOMES.**
A gate on the *outcome* can only find the failure it was written to look for. A gate on the
*precondition* — "is this measurement even attributable?" — runs the whole machine in a known-good
configuration on every invocation, and so finds whatever is wrong with the machine, including the
things nobody thought to look for. Four for four, none of them the thing it was built to detect.

---

### GF-6 — a rate is a ratio, and a floor on one needs a floor on the count beside it

**Raised by** B2.7's re-measurement of every harness-power floor. **Second instance** of a rate
moving for a reason unrelated to detection power.

A floor on a detection RATE cannot tell a loss of power from **a denominator that grew into territory
where the class was never detectable**. B2 added a manifest, so the sweep visits 300 kill points where
it visited 175 — and every added point is one at which `BM2`, `BM4` and `BM5` cannot be detected.
**Every rate in `FLOORS.txt` fell. Not one detection count did.** A lane that broke the build on that
would be reporting arithmetic as a regression; a maintainer who then lowered the rate floor would have
lowered it for the wrong reason and lost the bound for the right one.

**The rule.** A rate floor needs one of two things beside it:

- **a floor on the COUNT** — immune to the denominator, blind to per-point dilution. The rate is the
  reverse, which is why this is a *third* bound and not a replacement; or
- **a REGIME LABEL** that stops incomparable denominators from being compared at all.

**Track A learned the regime half at A6. This is the count half.** Both are now columns in
`FLOORS.txt`.

**Where it bites hardest, and B2 has an example.** `BM4-missing-dir-sync` in the default regime now
measures **290 of 290, first at kill point 1** — its count rose from 80 to 290 because the manifest
NAMES the WAL, so a lost directory entry is refused at every kill point rather than only where the
loss mattered. That is a real strengthening **and it costs the class its usefulness as a measure of
sweep power**: a class detected everywhere measures only that the lane ran. Its rate and ceiling are
kept because a collapse would still cross them, and are recorded as no longer discriminating so that
nobody reads 1000 per mille as a result.


