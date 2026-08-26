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

Counts: 14 entries.

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

**All six are meaning #1 or #2. MEANING #3 HAS NEVER OCCURRED IN TRACK B**, and that is worth stating
precisely rather than as reassurance, because it is the only meaning whose correct response is to
**delete** something.

> If meaning #3 never occurs, either **the code has no dead paths**, or **the classification cannot
> see them**. We do not currently know which.

Nothing here distinguishes those two, and no lane is pointed at the distinction. A coverage
instrument would be — B4's differential rig is the phase where one becomes affordable — and until
then the honest statement is that meaning #3 is a category with no observations, not a category shown
to be empty. **A tired reader reaching for it is reaching for the one answer this catalogue has never
had evidence for.**

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

**B3.4 ADDS THREE, AND ONE OF THEM IS THE FIRST INSTANCE OF MEANING #3.**

`BM73` removed `L1FileFor`'s check that the file the binary search found actually *contains* the key,
and **nothing failed** — a key in the gap between two files of the run makes the search return the
next file along, whose `Get` cannot find it either, so the answer is identical and only a filter probe
is wasted. **The line is a cost guard, not a correctness one.** That is `BM55`'s question answered the
other way: the property *"a range test decides containment"* IS load-bearing, in the compaction's
**input selection**, where getting it wrong resurrects deleted data — so `BM80` was written there and
`BM73` was **deleted**. GF-7, second instance, and the first time this catalogue has deleted a mutant
rather than re-aimed it.

`BM76` and `BM79` are meaning #1 again, and `BM79` is a **sub-form worth naming**:

| mutant | why it survived | what the fixture was really watching |
|---|---|---|
| `BM76` | the tombstone sat at the **top sequence**, kept by the watermark pin for an unrelated reason | the pin, called the drop rule |
| `BM79` | with no live snapshot the drop rule leaves **one version per key**, so no key can span a file roll | a situation that could not occur |

> **A MUTANT THAT SURVIVES BECAUSE ITS PRECONDITION IS UNREACHABLE IS NOT A WEAK MUTANT; IT IS A
> WORKLOAD THE SUITE NEVER RAN.**

That is a sharper statement of meaning #1 and it points at the fix rather than the symptom: `BM79`'s
test now holds forty snapshots so that a key HAS many surviving versions, which is a workload the
suite had never run and which B3.6 is about.

**AND THE TALLY'S OWN INSTRUMENT MISREAD ONE.** `BM78` was recorded as a survival on its first
induction because the script looked for a failing test — and the kill is an **abort**, so the process
died before the summary printed. `FLOORS.txt`'s header already warns of exactly this, and the
`RIFT PARTIAL RUN` marker was already in the output being read. **The remedy existed and was not
looked for**, which is the standing provenance rule one level up: a signal read without asking what
its absence would look like.

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

### HARNESS-014 — a registry cross-check that matched nothing, and would have passed forever

| field | value |
|---|---|
| **Found by** | `make cpp-scan`, on the first run of the rule it was part of |
| **Phase** | B3.0a, found the day it was written |
| **Reproduce** | before the fix: mark a file `RIFT_ORACLE`, leave it out of `ORACLES.txt`, and the scan says nothing |
| **Invariant that caught it** | the rule's own both-ways check, which fired on the OTHER direction and exposed this one |
| **Mutant class** | the four inductions in `B3.0a`'s commit, each observed and restored |
| **Fix commit** | `3e6d2c0` |

**Symptom.** `ARTIFACTS.txt` and `ORACLES.txt` were built, the registry cross-check was written, and
it **matched nothing** — every lookup fell through to the "not registered" branch. Two defects, and
only the first is interesting.

**Root cause 1, and it is a VACUOUS CHECK rather than a script bug.** The registry lists were built
with `grep | awk` and matched with `case " $list " in *" $item "*`. The lists are **newline**
separated and the pattern needs **spaces**, so no item ever matched. Had the check been written in
the direction that passes on no match, **it would have passed forever, on every tree, reporting a
boundary it was never testing.** It failed loudly here only because this particular direction reports
on *absence* of a match — an accident of which way round it was written, not a property of the check.

That is `GF-1`'s family at the level of a registry: *a check that cannot distinguish "nothing
violates this" from "I compared nothing" is not a check.* The remedy is the same one `GF-1` names —
run it where the thing could be present, and assert that it saw something. `cpp-scan` now prints
`parses 10 artifact(s)`, which is a count, and **a count nobody asserts is decoration**: it is
asserted by the four inductions.

**Root cause 2, recorded because it is cheap to record and expensive to re-find.** The new code used
a variable named `found`, which is part 3's `mktemp` path. Clobbering it made two *unrelated*
registry entries report as stale. **The lane doing to itself exactly what it does to the tree**, on
its first run.

**What it cost.** Nothing, because the rule was induced in four directions before being trusted —
which is the only reason either defect surfaced on day one rather than at the gate.

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

### A category worth watching: shell-dialect assumptions, not logic errors

Both defects in CF-2's own execution were **assumptions about which shell was running**, not mistakes
in reasoning:

| where | the assumption | what it produced |
|---|---|---|
| the labelling script, invoked from **zsh** | that `sh label.sh $IDS` word-splits an unquoted parameter — **zsh does not** | all 28 names arrived as ONE argument; the loop ended after a single `ROT` line, and *"1 of 28 done"* read as slow progress |
| `cpp-scan` part 6, running under **`sh`** | that `<(...)` is available — it is a **bashism** | the check printed its own heading and then died with a syntax error: **a lane that looks like it ran** |

**This is a category now, because the lanes are written in three dialects.** The `Makefile`'s recipes
and every `scripts/*.sh` run under POSIX `sh`; the shell these are authored and tried out in is
`zsh`; and `awk` is a fourth language inside `cpp-scan-rules.awk`. **A construct that works when
tried interactively may not run in a lane, and the reverse.**

**WHICH MECHANISM CATCHES WHICH, RECORDED EXPLICITLY, BECAUSE A LINTER THAT CATCHES ONE OF TWO
DIALECT FAILURES WILL BE TRUSTED FOR BOTH:**

| defect | `sh -n` | the provenance rule | why |
|---|---|---|---|
| `<(...)` under `sh` | **CATCHES** | — | it is a **syntax** error; the parser refuses it without running anything |
| `$IDS` unsplit in zsh | **CANNOT** | **CATCHES** | `sh label.sh $IDS` is **syntactically perfect**. It is semantically empty, and no parser can know that the caller meant 28 arguments |

**So `sh -n` is a defence against exactly one of these two, and the temptation is to treat a green
`sh -n` as covering "shell problems".** It does not cover the class where a construct is valid in
both dialects and *means* something different in each — which is the more dangerous class, because it
produces a running program that does the wrong thing rather than one that refuses to start.

For that class the defence is the standing provenance rule: **read progress from something that
advances.** The process not existing is what said "1 of 28" was not slowness.

---

### The standing rule: a signal read without its provenance

**Six instances now, and they are listed rather than counted so the number is checkable.** Each is a
signal that was read as if it meant one thing while its provenance made it mean another — and in
every case the misreading was *indistinguishable from the correct reading* without going and looking
at where the number came from.

| # | instance | the signal | what its provenance made it mean |
|---|---|---|---|
| 1 | `HARNESS-010` | BM2 detected at 0 per mille | not "the defect is unreachable" but "the snapshot was hiding the damage" |
| 2 | `HARNESS-012` | the last `Sync`'s `promoted` flag | not "the group is durable" but "some file's sync promoted, and until B2 there was only one file" |
| 3 | `HARNESS-013` | a log whose last line is not an error | not "still working" but *a stalled log is indistinguishable from a slow one* |
| 4 | `HARNESS-014` | a cross-check reporting no violations | not "nothing violates this" but "I compared nothing" |
| 5 | `GF-6` | a detection rate that fell | not "power was lost" but "the denominator grew into territory where the class is undetectable" |
| 6 | **the truncated suite, B3.1** | **no `DropCheck` test failing under either reader mutant** | not "the checker cannot see fabrication" but **"an earlier `RIFT_CHECK` killed the process before those tests ran"** |
| 7 | the labelling run, B3.1 | *1 of 28 done* | not "slow" but **"dead after one, and a rate computed from it claimed 396 minutes for a run already over"** — the THIRD rate computed over an appearance |
| 8 | `ORACLE-includes-engine`'s label | a scan reporting **NONE** | not "the rule no longer catches it" but **"my `grep` pattern did not match the line the rule printed"** — the rule catches it, and a mutant whose target moves is **re-pointed, not deleted** |

**The sixth nearly produced the opposite ruling.** The aliasing condition would have been reported as
**unacceptable** — requiring the rig to grow its own parser — on a zero that was an artifact. The
form it takes is the same as the third:

> **A TEST BINARY THAT ABORTS REPORTS FEWER FAILURES THAN EXIST, AND FEWER FAILURES REPORTED IS
> INDISTINGUISHABLE FROM FEWER FAILURES EXISTING.**

**The mechanical answer, and it is cheap: PUT THE PROVENANCE IN THE SIGNAL.** `RIFT_CHECK`'s failure
path now prints

```
*** RIFT PARTIAL RUN: aborted here, so any count above this line is a LOWER BOUND
    and any absence is unproven ***
```

so a count grepped out of that output **carries the fact that it is partial**. Induced against
`BM35`, which aborts. The same principle covers instance 3 — read progress from something that
*advances* — and instance 4, where the lane now prints `parses N artifact(s)` rather than nothing.

**The rule.** *Before reporting what a number means, establish that the run which produced it
completed, that the comparison it summarises actually compared something, and that its denominator is
the one the previous measurement used.* Three different questions, one shape: **a signal is not
evidence until its provenance is.**

---

### HARNESS-016 — a test observed the engine through a path that ROTATES, and destroyed what it observed

**Symptom.** A compaction test read the manifest to count tables per level, and the engine's next
manifest append failed: `kIoError: appending to a vanished file: db/MANIFEST-000001`.

**Root cause.** The helper called `sst::Manifest::Open`. That is not a reader — **every Open rotates**:
it replays, writes a NEW manifest, installs a new `CURRENT`, and **deletes the one it replaced**. The
test destroyed the live manifest underneath a running engine.

**The point is that this was already written down, in the other direction.** `manifest_format.h`
exists because `manifest.h` failed B3-D2a's artifact mark on `Manifest::Open(Env*, ...)` — *"opening a
manifest is AN ACT WITH AN OPINION about what the current state is, and it verifies, rotates and
installs."* The rule was applied to oracles and not generalised.

> **THE ARTIFACT/BELIEF SPLIT IS USUALLY ARGUED AS A RULE ABOUT WHAT A VERDICT MAY REST ON. IT IS
> ALSO A RULE ABOUT WHAT AN OBSERVATION MAY COST.** A path with an opinion has side effects, and a
> test that observes through one is running the engine, not watching it.

**Fix.** `rig/manifest_image.h` — a pure replay of manifest bytes, no Env, no rotation, no install.
The oracle's private copy was folded into it, so there is one parse rather than two, and the test now
reads `CURRENT` and the image it names.

**A SECOND INSTANCE, THE SAME HOUR, IN A THROWAWAY SCRIPT.** The helper that applies one mutant,
reads the failing assertion and reverts undid the patch with `git checkout -- engine-cpp/src` — which
reverts **everything uncommitted under that path**, not the patch it applied. It silently discarded an
unrelated comment written minutes earlier. The comment's absence then shifted the context lines of a
mutant patch generated against it, and the next lane run reported that patch as **`ROT` — "the code
moved and the mutation did not."** The lane was right, and it named the situation exactly.

> **A HELPER'S SIDE EFFECT MUST BE NO WIDER THAN ITS PURPOSE.** Both instances are the same shape:
> one observed through a path that rotates, one reverted through a path that reverts a directory. In
> both, the wider effect was invisible at the call site and showed up as something else entirely — a
> vanished file, a rotten patch.

Recorded because the second one cost only a regenerated patch **only because the lane already had a
`ROT` outcome to report it as**. Without that, it would have presented as a mutant that mysteriously
stopped applying.

---

### HARNESS-015 — the registry cross-check matched a file that says it is NOT an oracle

**Symptom.** `rig/image_fixture.h` — a constructor, not a judge — was reported as `carries RIFT_ORACLE
and is not in ORACLES.txt`. Its header line reads: *"it CONSTRUCTS; it never judges, so it carries no
RIFT_ORACLE marker."* The check was a substring `grep`, so **a file could not say what it is not.**

**Root cause.** The marker was treated as an *occurrence of a token* rather than as **a declaration**.
Every real oracle already declares it identically: `// RIFT_ORACLE` as the file's **first line**.

**Fix.** Both directions of the cross-check now read `head -1 | grep '^// RIFT_ORACLE'`. Induced in
all three directions before it counted: an unregistered file declaring it (BAD), a registered file
losing it (BAD), and a prose mention below line 1 (clean — `image_fixture.h` itself, live in the
tree, is the standing witness).

**The pair with HARNESS-014 is the point.** That one **matched nothing** and would have passed
forever; this one **matched too much** and failed loudly. Same instrument, opposite failures, and
only the loud one is self-announcing.

> **A REGISTRY CROSS-CHECK HAS TWO FAILURE MODES AND ONLY ONE OF THEM TELLS YOU.** Both directions
> get induced, or the quiet one is what you have.

---

### GF-15 — a rule derived from one contract is not permission under the others

**Raised by the watermark pin, B3.4.** `B3-D1` says a compaction **may** drop an entry that no reader
can observe. That "may" is permission **with respect to reads**, and reading it as permission full
stop breaks a promise `B3-D1` never mentions:

| contract | what it is about | what compaction owes it |
|---|---|---|
| the drop claim | **the ANSWER a reader gets** at each observable sequence | preserve every answer |
| `DurableSeq` | **a PROMISE ABOUT A SEQUENCE**, monotone non-decreasing | preserve the engine's proof of it |

`Open` re-derives the durable floor as the maximum `largest_seq` over the live tables, and it must:
D7's forward binding forbids the manifest from recording a durable sequence, so **the tables' own
bytes are the only place that number can come from**. Drop the highest-sequenced entry — a tombstone
with nothing left to mask, exactly what the claim permits — and the maximum falls. Every answer is
preserved; `DurableSeq` goes backwards across a restart.

> **A COMPONENT OBEYS MORE THAN ONE CONTRACT. A RULE DERIVED FROM ONE OF THEM IS NOT PERMISSION UNDER
> THE OTHERS, AND THE RULE WILL NOT SAY SO — because the contract it came from does not know the
> others exist.**

**What makes it findable rather than lucky:** the question is *what else does this operation touch
that someone has already been promised something about?* Compaction touches the set of live tables,
and the durable floor is derived from that set. The link is one step and nobody walks it while
reading a claim that is locally airtight.

**`BM77` plants it, and the mutant IS the faithful implementation of `B3-D1`.** Not a typo, not a
weakened check — what a careful reader of the claim would write. That is precisely the blind spot the
suite exists for, and it is why the mutant's header says so.

---

### GF-14 — a complementarity claim is asserted in both directions or it is folklore

**Raised by** `IsTheMergeOfItsInputs` and `AdjudicateDrops`, B3.4.

Two instruments are said to be complementary: one sees order and values, the other sees drops in any
durable image. **Stating that is not asserting it.** The test that carries the claim asserts *both*
halves against **one state**:

```
RefusesAnOutputWhoseValuesAreShiftedByOne
    the merge adjudicator  REFUSES  the shifted output
    the drop adjudicator   PASSES   the same state
```

**The first half alone is not the claim.** One instrument refusing says nothing about whether the
other was needed; only the second half — *this state gets past the other one* — makes "we need both"
a statement with a failing case.

> **A COMPLEMENTARITY CLAIM IS ASSERTED IN BOTH DIRECTIONS OR IT IS FOLKLORE.**

**What it protects against is silent degradation.** The day someone widens the drop adjudicator to
look at values — a reasonable change, and an improvement in isolation — the pair stops being
complementary and *nothing notices*, because a one-directional test still passes. With the second
half, that change fails a test whose message says exactly what to reconsider. **The claim gets
revisited rather than repeated.**

**This is Track A's bidirectional-gap discipline applied to two CHECKERS rather than to a gap.** The
same move: do not assert only that the thing fires, assert also that it was needed — and here
"needed" means *the other instrument does not cover this*.

---

### Fixture defects: one shape, twice, and the class made unreachable

Two fixtures produced verdicts that looked like checker bugs. **Both times the checker was right.**

| where | what the fixture omitted | what the checker correctly reported |
|---|---|---|
| B3.0, the drop adjudicator | a **directory sync** after writing the table — so its NAME was never durable | the image did not contain the table, so every version was **dropped** |
| B3.4, the merge adjudicator | **the manifest** naming the table — so it was an orphan | a table nothing refers to holds nothing, so every version was **dropped** |

> **A FIXTURE THAT DOES NOT DESCRIBE WHAT ITS AUTHOR MEANT PRODUCES A CORRECT VERDICT ABOUT THE WRONG
> THING, AND IT PRESENTS AS A CHECKER BUG** — which is the expensive way to find out, because the
> debugging starts in the wrong component.

**What the two omissions share is the useful part.** Neither was a typo. Each left out something
**the engine's own invariants require**: a durable table has a durable name; a live table is named by
the manifest. A fixture assembling an image by hand must reproduce every one of those invariants from
memory, and *will not*.

**So the class is made unreachable rather than remembered.** `rig/image_fixture.h` builds images
**through the engine's own construction path** — write, sync, **sync the directory**, open, validate,
name in the manifest — and both tests now go through it. There is one place that knows the whole
sequence, and a fixture cannot forget half of it. It was cheap: one file, and it removed a duplicated
key encoder on the way.

---

### GF-13 — a bound derived from another instrument's measurement cannot be raised

**Raised by** B3.4's merge, and it is a stronger property than the condition that asked for it.

Every loop needs a progress quantity and every unbounded quantity needs a bound. **The usual bound is
a CHOSEN number** — a timeout, a retry count, a maximum iteration — and a chosen number has one
predictable life: it is hit under some workload nobody anticipated, and it is **raised**. Not because
anyone is careless, but because the alternative is refusing a correct run, and a limit that refuses
correct runs loses that argument every time.

**The merge's bound is not chosen. It is DERIVED:**

> **`inputs_consumed ≤ Σ entries(f)` over the compaction's input files, counted by `ValidateTable`
> before the merge starts.**

A correct compaction consumes each input entry **exactly once**, so it terminates *at* the bound.
**Hitting it exactly is correct; exceeding it is the only failure** — and exceeding it can only mean
a source was rewound or an entry counted twice, which are the two ways a merge loops forever.

> **A BOUND DERIVED FROM ANOTHER INSTRUMENT'S MEASUREMENT CANNOT BE RAISED WITHOUT CONTRADICTING THAT
> INSTRUMENT, SO THE PRESSURE THAT NORMALLY ERODES A LIMIT HAS NOWHERE TO GO.**

To raise this one you must claim a table holds more entries than the classifier says it holds — and
the classifier's count is itself asserted (`SstClassifier.AcceptsACanonicalTable`). There is no
number in the source to edit. **That is a difference in KIND, not in degree**: a chosen bound is a
judgement that can be revised, and a derived bound is a consequence that can only be revised by
falsifying something else.

**Where to look for the pattern.** Prefer a bound that is *already being measured for another
reason*. `kMaxRecordBytes` and `kWalBufferBytes` are chosen and carry their derivations in prose
precisely because nothing measures them; this one needed no prose, because `ValidateTable` was
already counting.

---

### GF-12 — a termination assertion is not a correctness assertion

**Raised by** B3.3's CF-3 mutants, and it is the danger *inside* a rule that is working.

CF-3 requires every loop to assert the movement it terminates on, over a quantity it does not derive
from the thing it might be wrong about. `ConcatIter` does that, and the assertions hold. **Two of its
three mutants pass every one of them:**

| mutant | what it breaks | what the progress assertion does |
|---|---|---|
| `BM68-concat-seek-wrong-half` | the binary search takes the **wrong half** | **holds throughout.** `hi - lo` shrinks whichever direction is taken, so the loop terminates cleanly with its interval invariant intact — **and lands on the wrong file** |
| `BM69-concat-next-skips-a-file` | `Next` advances **two** files | **holds throughout.** `file_` strictly increases, so the walk terminates — **having silently dropped a whole table's contents** |

> **A LOOP WITH A PROVEN PROGRESS QUANTITY CAN ADVANCE MONOTONICALLY INTO A WRONG ANSWER.**

**THE DANGER IS NOT THAT CF-3 FAILS. IT IS THAT CF-3 SUCCEEDING FEELS LIKE COVERAGE IT DOES NOT
PROVIDE.** A reader who sees `RIFT_CHECK(hi - lo < before)` beside a search, and knows the phase made
a point of loop assertions, has every reason to think the loop is checked. It is — for termination,
and for nothing else. In `BM67`'s terms: **a checked-looking loop that returns wrong answers.**

**What actually covers the traversal, named so nobody reads the wrong green as evidence:**

| instrument | what it covers |
|---|---|
| `ConcatIter.EverySeekTargetLandsWhereALinearScanWould` | **the seek sweep** — every probe in and around the run compared against what a linear scan returns. A search wrong for one input class is wrong invisibly, because every other input still works |
| `ConcatIter.WalksTheWholeRunInOrder` and `.WalksBackwardsToTheSameSequence` | **the traversal** — and the pair matters, because forward and backward cross the same file boundaries in opposite orders |

`FLOORS.txt` labels these **`covers: correctness`** rather than leaving them as bare `covered-by:`
entries, so the distinction survives being read by someone in a hurry.

**THE SAME SHAPE ONE LEVEL UP: THE DROP ADJUDICATOR.** It is correct about what it checks and
**silent about what a reader assumes it covers.** It works over **sets** of `(user_key, seq)` — so it
is blind to ordering entirely, and blind to values.

> **A merge that emitted every required entry, in REVERSE ORDER, with EVERY VALUE SHIFTED BY ONE
> POSITION, would satisfy all three of its directions.**

That example is stated concretely on purpose: it is specific enough that nobody can talk themselves
out of it, which an abstract "it does not check ordering" would not be. `CompactionOutput.IsTheMergeOfItsInputs`
is what closes it — the harness merges the inputs itself, filters by the drop claim, and asserts the
output is **exactly that sequence, in order, with matching values.**

**The two are COMPLEMENTARY, NOT REDUNDANT, and the distinction is written down because someone will
later notice they overlap and propose deleting one:**

| instrument | runs where | sees |
|---|---|---|
| `IsTheMergeOfItsInputs` | **only** where the harness knows both the input and the output files — a compaction in isolation | order, values, and drops |
| `AdjudicateDrops` | **any durable image**, including one produced by a crash MID-COMPACTION | drops only |

Delete the second and every crash schedule loses its drop verdict. Delete the first and a merge can
reverse its output undetected.

---

**THE STANDING REQUIREMENT, and it is not a B3 rule:**

> **EVERY LOOP THIS ENGINE ADDS GETS TWO INSTRUMENTS, ANSWERING DIFFERENT QUESTIONS: *does it stop*,
> and *does it stop in the right place*.**

CF-3 is the first. **It was never the second**, and the phase that treats it as both ships `BM68`.
`FLOORS.txt` keeps them apart mechanically — `covered-by:` against `covers-correctness:` — so the
distinction survives a hurried reading rather than depending on one.

---

### BM67 — the phase's exemplar of a defect NO GENERAL CHECKER FINDS

**One character.** `<` becomes `<=` in `RangeTombstone::Covers`, and the range stops being half-open.

Closed and half-open **agree on every key except one**: the range's end. A tombstone for `[b, d)`
that has become `[b, d]` deletes `d` — one key more than the caller asked for, **forever, and
silently**, because nothing anywhere reports a key that was deleted slightly too eagerly.

**GO THROUGH THE ORACLES THIS PROJECT HAS BUILT AND NONE OF THEM SEES IT:**

| checker | why it is blind to this |
|---|---|
| the **drop adjudicator** | asks *what may be dropped* against the harness's version model. It is about DROPS, not about **bound arithmetic** — the tombstone is legitimately present and its sequence is legitimately required |
| **recovery equivalence** | compares WAL-only recovery against WAL-plus-tables. **Both sides use the same `Covers`**, so both delete `d` and the two agree perfectly |
| the **kill-point sweep** | injects crashes and torn syncs. **Nothing crashes.** Every kill point recovers to a promised watermark, because the watermark is right and only the CONTENT is one key short |
| **`ValidateTable`** | judges bytes against a format. The bytes are perfectly legal — this is a defect in what they MEAN, not in what they are |
| **the differential rig at B4** | *would* catch it, against `engine/model` — which is a phase away, and by then the convention would have been established by the code rather than chosen |

**What catches it is `RangeTombstone.TheBoundsAreHalfOpen`** — a fixture that probes `a`, `b`, `c`,
`d`, `e` around a `[b, d)` tombstone and asserts each answer individually, written **before any code
could establish a convention by accident**.

> **THIS IS B3.2's ORDERING ARGUMENT STATED AS A CONSEQUENCE RATHER THAN A PRINCIPLE.** A classifier
> written after a writer inherits the writer's convention and asserts it back. There is no general
> checker for "the engine is consistently wrong about a boundary" — consistency is exactly what a
> differential or equivalence check confirms. **The only defence is to fix the convention in a
> fixture before there is an implementation to read it off.**

---

### An empty condition is evidence only when the question was asked mechanically

CF-3 carries a condition: *if a loop cannot state a progress quantity independent of what it might be
wrong about, that is a finding to report before it is a loop to write.* At B3.3 it **came back
empty** — every loop the step adds has an independent quantity.

**An empty result is worth exactly as much as the procedure that produced it.** What makes this one
evidence rather than a shrug is that the question was asked **before the code existed**, in a table
with one row per loop and an explicit independence column:

```
loop            might be wrong about        progress quantity     independent?
Next / Prev     which table holds the key   file_ (an index)      YES
Seek            CompareInternalKey          hi - lo               YES  <- see below
```

Had it been asked by reading the finished code and nodding, "nothing to report" would have meant
"nothing noticed". **The table is what makes it the first rather than the second**, and it also
produced the phase's forward flag: B3.4's merge both advances a cursor *and* drops entries, so it has
no single cursor as a progress quantity, and its honest answer may be a bounded work count.

**The general shape: a check that returns "nothing" is only as good as the enumeration behind it.**
It is the same argument `HARNESS-006`'s audit made — enumerate the population first, then check each
member — applied to a question rather than to a set of functions.

---

### GF-11 — a rule stated in one place invites transfer to an adjacent place where it is false

**Raised by** the range-tombstone bounds, B3.2.

`table_check.h` refuses **a key too short to carry a tag**, and it is right: point entries are
internal keys. A range tombstone's bounds are **user keys** — the tag is a separate field — so the
**empty user key is a valid bound**, meaning "from the beginning".

Both rules are correct. **The danger is not either rule; it is the transfer.** A reader who has
internalised the first arrives at the second and applies it, because the two structures sit in the
same format, are decoded by the same block decoder, and are described in adjacent sections.

> **A FORMAT DECISION THAT CONTRADICTS A NEARBY ONE IS DOCUMENTED AT THE CONTRADICTION, NOT ONLY AT
> ITS OWN SITE.**

So `range_tombstone.h` does not merely say *bounds are user keys*. It says:

> *"THE BOUNDS ARE USER KEYS AND NOT INTERNAL KEYS, so the empty user key is a valid bound and there
> is no minimum length. This is the OPPOSITE of the point entry rule — which refuses a key too short
> to carry a tag — and it is stated here because a reader who has internalised that rule will assume
> it carries over. The tag is a separate field precisely so the bounds do not have to."*

**The last clause is what stops the transfer**, and it is the part a shorter comment would drop: it
does not just deny the other rule, it says *why the design is different*, which is the only form a
reader can check. `RangeTombstone.TheEmptyUserKeyIsAValidBound` asserts it, so the sentence has a
failing case.

**The relationship to GF-7.** GF-7 is a claim attached to the wrong line. This is a claim attached to
the right line **that a reader will carry to the wrong one** — the same damage by a different route,
and the remedy is different too: GF-7's is to move the comment, this one's is to write the
contradiction down where it can be met.

---

### GF-10 — a set of assertions all pointed the same way has a blind spot the size of its agreement

**Raised by** B3.1's aliasing condition, and it is a **decision** that came out of it rather than an
audit finding — see `B3-D2c`.

The drop adjudicator had two directions, and they looked complementary:

```
kept      >= required     nothing a reader can reach was dropped
dropped   <= permitted    no tombstone was dropped over what it masked
```

**Both ask whether something is MISSING.** So neither could see a reader that reports a record the
bytes do not contain — which makes a real drop look survived, and produces a **false pass**. The
agreement between them was not corroboration; **it was the shape of the hole.**

> **A SET OF ASSERTIONS ALL POINTED THE SAME WAY HAS A BLIND SPOT THE SAME SIZE AS ITS AGREEMENT.**
>
> **Asking what a checker CANNOT SEE is a different question from asking whether it works** — and the
> second question is the one that gets asked, because it is the one a green lane answers.

The third direction, `survived ⊆ submitted`, is what closed it, and it is stated as its own decision
rather than as defensive clutter precisely because a future reader will otherwise remove it: it never
fires in normal operation, and *nothing else in the tree asks its question*.

**How to ask the harder question.** Enumerate what each assertion *rules out* and look for the
direction none of them names. Here: "missing" was ruled out twice and "present but never written" was
ruled out zero times. It is the same move `HARNESS-006`'s audit made across the evidentiary deciders
— enumerate the population, then check each member in both directions — applied to assertions rather
than to functions.

---

### GF-9 — a correctness claim written before its checker is a hypothesis, and building the checker is how it gets tested

**Twice observed, both in Track B, and both times the correction was in the CLAIM rather than in the
code.**

| phase | the claim | what building its checker found |
|---|---|---|
| B2.0 | *entries in a block are strictly ascending* | ascending **by `memcmp`** is wrong: internal keys sort user-key-ascending, tag-DESCENDING, and the fixtures that could show it did not exist because they used keys with no tag |
| B3.0 | *`keep(k)` is the newest version at each observable sequence* | it **over-requires**: a deletion's answer is `kNotFound`, which dropping the deletion preserves exactly, and the stricter form forbids the one drop that makes compaction terminate in space |

**What makes this different from the ordering rule it comes from.** *The observer lands before the
observed* is usually argued as protection against a checker written to agree with the thing it
checks. That is real, and it is not what happened either time. **Both times the CLAIM was wrong, and
writing the enforcement is what tested it** — because a claim in prose has no failing case, and a
checker has to be handed inputs.

**The consequence worth recording, and it is `HARNESS-006`'s shape.** A checker built to an
over-strict claim **refuses correct behaviour**, and it presents as *a bug in the component being
checked*. `HARNESS-006` cost its debugging to the wrong component because it was found late. Both of
these were found **before the component existed to be blamed** — B2's before any writer, B3's before
any compaction — which is the same ordering rule buying something other than what it is usually sold
for.

---

### GF-8 — when a rule distinguishes two kinds of dependency, find the SIGNATURE that separates them, not the sentence

**Raised by** B3-D2a, correcting the oracle-independence rule at `B3-Q1`.

The rule to be enforced was *an oracle may parse the engine's artifacts and may not consult its
beliefs*. That sentence is correct and it is **a discipline**, and this catalogue records five
disciplines that failed. What replaced it is two greps:

> **AN ARTIFACT HEADER DECLARES NOTHING TAKING AN `Env*` AND NOTHING TAKING A SNAPSHOT.**

`Env*` means *it went and looked*, which is an act with an opinion about what the current state is. A
snapshot parameter means *it decided what a caller should be allowed to see*, which is the engine's
visibility rule. A header with neither is bytes in, structure out.

**The general form.** When a rule separates two kinds of dependency — permitted and forbidden,
parsing and consulting, reading and asking — **the enforceable version is a property of the
DECLARATION, not a description of the intent.** Look for the signature that differs. If no signature
differs, the two kinds are not actually distinct and the rule is describing a preference.

**Corroboration, and it is the stronger half.** The rule was written to draw a boundary and it
**found unnecessary coupling instead**: `sst/table.h` fails both marks, and it turned out the drop
checker never needed it — `table_format.h` enumerates every entry in a table, and enumerating is the
whole job. `sst/manifest.h` failed the `Env*` mark and split, which was a correction rather than a
concession.

> **A RULE THAT FINDS UNNECESSARY COUPLING IS DOING MORE THAN ENFORCING A BOUNDARY.** The signature
> test asks "does this dependency have the shape of a judgement", and a dependency that cannot answer
> is usually one that did not need to exist.

**The counterpart obligation, and it is the one a future reader will get wrong.** See `HARNESS-014`:
permission to *read* an artifact is not permission to *derive the expectation* from it.

---

### GF-7 — a misplaced invariant: a comment asserting a load-bearing property for a line that does not carry it

**Raised by** `BM55-tables-oldest-first`, B2's last catalogue run. The phase's best finding, and it is
not the vacuous-green class and not one of the three survival meanings as previously stated.

> **A COMMENT THAT ASSERTS A LOAD-BEARING PROPERTY FOR A LINE WHERE IT IS NOT LOAD-BEARING IS WORSE
> THAN NO COMMENT.** It is where someone looks for the invariant, and it sends them somewhere nothing
> depends on it.

**The instance.** `Version::Build`'s table loop carried *"NEWEST FIRST. Order is not cosmetic: a
deletion in a newer table must hide a value in an older one, so the first source holding a user key
wins."* Every word of that is true — **of `VersionGet`'s point-read walk, a hundred lines below.**
Where it stood it was false: `MergedIter` orders by KEY, sequences are unique, so there are no ties
for source order to break and the order it is handed is irrelevant.

**How it was found, and it could not have been found by reading.** A mutant was aimed at that line
*because the comment said so*. It survived — correctly — and the survival is the only thing that
distinguished "this line carries the property" from "a comment near this line says it does". **A
misplaced invariant is invisible to review by construction: the reviewer's question is whether the
comment is true, and it is.**

**The family, and this is why it is a named shape rather than an incident.** It is the same failure as
Track A's `power-config a3` and the A6 banner — **a name describing something other than what it is
attached to** — with one difference that makes it more dangerous: those are *labels*, and this is a
**correctness claim**. A wrong label misdirects; a wrong correctness claim gets *relied on*. The next
person to touch `Version::Build` would have preserved an order they did not need and, finding the
same words there, would have had no reason to look for the walk that actually needs it.

**The fix, and where the induction has to land.** Re-point the comment at the line that carries the
property, and **re-point the mutant there too** — a general form whose remedy is not itself induced is
a general form nobody has tested. `BM55` now reverses `VersionGet`'s walk, where the first table
holding the user key wins and the walk stops.

**The standing question it adds.** When a mutant survives, before reaching for any of the three
meanings: *is the line this patch is aimed at actually the line that carries the property, or is a
comment answering that question for me?*

---

**THE SAME SHAPE IN THE LABEL FILE, and it is where it does the most damage.** `FLOORS.txt`'s
`covered-by:` is a claim that a named assertion catches a named class. CF-2 landed 47 of them, and
**every one was DETERMINED — the patch applied, the tree built, the failing assertion read — never
INFERRED from what the patch says it blinds.**

Inferring would have been faster and would have produced entries that are *plausible and wrong*: the
`blinds` line describes the defect, not the assertion that notices it, and the two coincide often
enough to make the guess feel safe. Three of the 47 have **no failing test at all**.

> **A WRONG `covered-by` IS WORSE THAN NONE, because it is consulted precisely when someone is about
> to delete something.** That is GF-7 arriving in the label file rather than in a comment: a claim
> attached to the wrong thing, in the one place a reader trusts before removing an assertion.

**The rule: a label that names an instrument is determined by induction, or it is not written.**

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


