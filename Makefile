# Rift -- test lanes.
#
# Lanes marked STUB exist so CI wiring is real from day one, but they check
# nothing yet and say so loudly. `make lanes` prints the status of every lane.
# No lane may remain a stub past the phase named against it.

GO           ?= go
STUB         := ./scripts/lane-stub.sh
TOOLING_ONLY := ./scripts/tooling-only.sh
BLIND        := ./scripts/blind-analyzer.sh
MUTANTS      := ./scripts/mutants.sh
POWERMUTANTS := ./scripts/power-mutants.sh

# ---- Track B (C++ engine). DESIGN-B1 section 9.3.
CMAKE        ?= cmake
CPP_SRC      := engine-cpp
CPP_BUILD    ?= engine-cpp/build
CPP_BUILD_CI := engine-cpp/build-ci
VENDOR_CHECK := ./scripts/cpp-vendor-check.sh
NO_NETWORK   := ./scripts/cpp-no-network.sh
CPP_ROT      := scripts/cpp-rot.sh
CPP_MUTANTS  := ./scripts/cpp-mutants.sh
CPP_SCAN     := ./scripts/cpp-scan.sh
CPP_CAMPAIGN := ./scripts/cpp-campaign.sh
CPP_SCAN_BLIND := ./scripts/cpp-scan-blind.sh
COLD_CACHE   := ./scripts/cpp-cold-cache.sh
# The lane set `make cpp-ci` runs under network isolation. It grows as
# lanes un-stub; every member must be runnable by hand, because nothing
# runs it for us.
CPP_LANES    := cpp-vendor-check cpp-scan cpp-scan-blind cpp-vendor-build cpp-test cpp-asan cpp-ubsan cpp-tsan cpp-sweep cpp-diff cpp-cgo
WORKERS ?= $(shell sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)

SMOKE_SEEDS ?= 500
SOAK_SEEDS  ?= 10000

# The tiers, and why `make test` is -short.
#
# # `make test` could not run, and CI has never noticed
#
# It was `go test ./...` with nothing set. The A1 exit run defaults to TEN
# THOUSAND seeds and only shortens when RAFT_SEEDS says so, which at A6's cost is
# roughly twenty-six hours -- so the lane CI runs on every push has been
# unrunnable since A1, and would have died on Go's ten-minute default timeout
# long before finishing. Nobody saw it because there is no remote: CI has never
# executed, and `make test` was only ever run per-package by hand.
#
# # Bounding the seed count was not enough, and the measurement says why
#
# The first fix set RAFT_SEEDS=200 and it still timed out. Measured at A6's cost:
# TestRaftExitCriteria alone takes 233s at TWENTY-FIVE seeds, and `sim/hunt`
# holds some fifteen covering tests that each sweep a range. **The cost is driven
# by the number of sweeping tests, not by any one bound** -- so no value of
# RAFT_SEEDS makes this lane fast.
#
# So the tiers follow CLAUDE.md, which had the answer all along: *Go unit plus
# race on every push; 500-seed smoke on every push; 10k-seed soak nightly.*
#
#   make test      -short: every package's unit tests, every covering test
#                  capped to a handful of seeds -- proves the path RUNS.
#                  Measured at A6's cost: 398s for the whole repository, of
#                  which sim/hunt is 396s, and CPU-bound rather than contended
#                  (378s user of 398s real). Six and a half minutes is not fast,
#                  and it is what an every-change lane costs once a seed costs
#                  four seconds. The alternatives measured 1800s and TIMED OUT,
#                  twice.
#   make smoke     500-seed toy sweep, cheap, per push
#   make covering  sim/hunt at full seed ranges -- proves the paths are SILENT
#   make soak      10k seeds, nightly
#   make exit-run  the phase gate, sharded
#
# LANE_SEEDS bounds the three A6 lanes, which are reduced-seed by design.
LANE_SEEDS     ?= 12
TEST_TIMEOUT   ?= 1800s
COVER_TIMEOUT  ?= 360m

# The exit run's split. The seed count is Ansh's and does not move; the SHARD
# count is a scheduling choice and may.
EXIT_SEEDS  ?= 25000
EXIT_SHARDS ?= 8
EXIT_OUT    ?= .exitrun

# The solo slot's own seed count. Reduced on Ansh's A5 ruling: the unthrottled
# collector answers "what happens at all", not "what happens ten thousand times".
SOLO_LANE_SEEDS ?= 40

.DEFAULT_GOAL := help

# ---------------------------------------------------------------- real lanes

.PHONY: build
build: ## Compile everything
	$(GO) build ./...

.PHONY: test
test: ## Go unit tests, -short: every path runs, no path is swept (see the tiers above)
	LANE_SEEDS=$(LANE_SEEDS) $(GO) test -short -timeout $(TEST_TIMEOUT) ./...

.PHONY: covering
covering: ## sim/hunt at FULL seed ranges: the covering tests' silence claims (nightly)
	$(GO) test -count=1 -timeout $(COVER_TIMEOUT) ./sim/hunt/

# The race lane, SPLIT, with budgets taken from a measurement rather than
# inherited.
#
# # What the measurement said
#
# At A5's 0.36 s/seed the whole repository under `-race` was 90 minutes. At A6's
# measured 8.4 s/seed it does not finish: `sim/hunt` at RAFT_SEEDS=50 blew the
# 5400s budget, with one test at 36m20s -- about **43 s/seed instrumented**. So
# the package at 50 seeds is on the order of nine hours and 200 seeds is four
# times that.
#
# "Does the seed count move or does the budget move" therefore has a third
# answer: neither alone. The lane is split by what it is for.
#
# # `race`: the structural claim, per push
#
# The question this lane asks is *does any cross-goroutine interaction reach node
# state off the mailbox* (Amendment A1). That question lives in `raft/`, `store/`,
# `node/`, `kv/` and `sim/` -- the system and its real-mode driver -- and it is
# answered by their tests rather than by a seed search. **Measured: 191 seconds
# for every package except `sim/hunt`.** The budget is 900s, about five times the
# measurement: ordinary drift does not fail the lane and a doubling does.
#
# # `race-soak`: the seed search, nightly and sharded
#
# `sim/hunt` is where the driver, mailbox and simulator meet, so it is the half
# with the most to say and the half nothing can afford per push. It moves to the
# nightly tier beside `covering` and `soak`, and it shards the way the exit run
# does, on the same argument: a seed's verdict does not depend on which
# invocation ran it.
#
# # What the split gives up, stated rather than absorbed
#
# The per-push lane no longer instruments the simulator driver. A race introduced
# there is caught nightly instead of on push. That is a real reduction and it is
# the honest one available: the alternative was a seed count in single digits,
# which A1's ruling -- *a few hundred simulated seeds answer this lane's
# question* -- does not authorise. Shrinking a recorded scope to fit a budget
# without saying so is the move this file exists to prevent.
RACE_SEEDS   ?= 8
RACE_TIMEOUT ?= 900s

# The nightly half. 200 seeds is A1's ruling and it is what the shards cover
# between them; the shard count is a scheduling choice and may move.
RACE_SOAK_SEEDS   ?= 200
RACE_SOAK_SHARDS  ?= 8

.PHONY: race
race: ## -race over every package but sim/hunt: the mailbox claim, per push (measured 191s)
	RAFT_SEEDS=$(RACE_SEEDS) LANE_SEEDS=4 $(GO) test -race -count=1 -timeout $(RACE_TIMEOUT) \
		$$($(GO) list ./... | grep -v 'sim/hunt')

.PHONY: race-soak
race-soak: ## [nightly] sim/hunt under -race, sharded: the seed search half
	sh scripts/race-soak.sh $(RACE_SOAK_SEEDS) $(RACE_SOAK_SHARDS)

.PHONY: vet
vet: ## Standard go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail if anything is unformatted
	@out="$$(gofmt -l . | grep -v '^$$' || true)"; \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy
	@cp go.mod go.mod.bak; [ -f go.sum ] && cp go.sum go.sum.bak || true; \
	$(GO) mod tidy >/dev/null 2>&1; \
	rc=0; cmp -s go.mod go.mod.bak || rc=1; \
	[ -f go.sum ] && { cmp -s go.sum go.sum.bak || rc=1; }; \
	mv go.mod.bak go.mod; [ -f go.sum.bak ] && mv go.sum.bak go.sum || true; \
	if [ $$rc -ne 0 ]; then echo "go.mod/go.sum not tidy; run 'go mod tidy'"; exit 1; fi

.PHONY: determinism
determinism: ## Custom vet pass: no time.Now, no global rand, no map range, mailbox rule
# GOFLAGS, NOT -tags. This line used to read `determinismcheck -tags rift_cgo`,
# with a comment saying the cgo engine was therefore LOADED AND ANALYZED. It was
# not. `singlechecker.Main` registers -tags and does not forward it to the
# loader, which is BUG-037 -- so the flag was accepted, dropped, and the package
# stayed absent from ./... entirely. Measured at the merge:
#
#   determinismcheck -tags rift_cgo ./engine/riftcgo/   build constraints exclude all Go files
#   determinismcheck -tags rift_cgo ./...               SILENT: exit 0, zero mentions
#   GOFLAGS=-tags=rift_cgo determinismcheck ./...       loads clean
#
# The `./...` form is the dangerous one: a tagged package is not in the list at
# all, so there is no error to notice. The lane was green over a package it had
# never opened. GOFLAGS reaches the loader because it reaches `go list`.
#
# The pass only typechecks, which needs no C++ archive -- also measured, since
# `notAnalysed` claimed the opposite: `go build -tags rift_cgo ./engine/riftcgo/`
# succeeds with no archive anywhere in the tree.
	GOFLAGS=-tags=rift_cgo $(GO) run ./tools/determinismcheck/cmd/determinismcheck ./...

.PHONY: tooling-only
tooling-only: ## Assert golang.org/x/tools never enters a shipping binary (DESIGN-A0 Q4)
	@$(TOOLING_ONLY)

.PHONY: hatches
hatches: ## Assert HATCHES.txt matches the tree exactly (never rewrites it; -update-hatches is local-only)
	$(GO) test -count=1 -run TestHatchRegistry ./tools/determinismcheck/

.PHONY: blind
blind: ## Mutation-test the determinism pass itself; each blinded rule must fail its own test
	@$(BLIND)

.PHONY: provenance
provenance: ## The ledger's inputs are harness-observed; a system-reported fact must not compile into a verdict
	$(GO) test -count=1 ./tools/provcheck/

.PHONY: corpus
corpus: ## Replay every bundle in seeds/; a bundle that stops reproducing fails the build
	$(GO) test -count=1 -run 'TestEveryStoredBundleReplays|TestCorpusLaneDetectsRot' ./cmd/simctl/

.PHONY: bundle-seeds
bundle-seeds: ## BUGS.md's stated bundle seed matches the bundle it points at
	sh scripts/bundle-seeds.sh

.PHONY: corpus-reproduces
corpus-reproduces: ## Every bundle still EXERCISES its defect: apply its mutant, replay, require a difference
	sh scripts/corpus-reproduces.sh

.PHONY: anchors
anchors: ## Every mutant patch anchors on CODE, not on prose that a comment edit can move
	$(GO) test -count=1 ./tools/anchorcheck/
	@sh scripts/patch-rot-kind.sh --self-test

.PHONY: blockers
blockers: ## Re-ask every CARRY-FORWARD blocker that declares a machine-checkable condition
	$(GO) test -count=1 ./tools/blockercheck/

.PHONY: assertions
assertions: ## Every declared every-run assertion mechanism must actually be invoked
	$(GO) test -count=1 -run 'TestEveryAssertionMechanismIsInvoked|TestAssertionRegistryIsWellFormed' ./sim/hunt/

.PHONY: power
power: power-toy power-decl power-refute power-mutants ## Harness-power floors: every planted flaw class must still be detected at its floor

.PHONY: power-toy
power-toy: ## The toy's four flaw classes, floored since A0
	$(GO) test -count=1 -run 'TestHarnessPower|TestEveryObservableFlawHasAFloor' ./sim/hunt/

.PHONY: mutant-covered
mutant-covered: ## Every covering test EXECUTES the line its mutant changes (not around it)
	@#     make mutant-covered ONLY="M46-split-inherits-the-appended-configuration"
	@# A space-separated list runs the lane for those classes only. With no CI, a
	@# lane's cost is a fact about whether it gets run at all.
	ONLY="$(ONLY)" sh scripts/mutant-covered.sh

.PHONY: power-decl
power-decl: ## Every mutant's power DECLARATION is consistent -- milliseconds, no sweep
	sh scripts/power-decl.sh

.PHONY: power-mutants
power-mutants: ## Every MUTANT class: detection rate against a standing floor, or an explicit opt-out
	@$(POWERMUTANTS)

# The refutation pass, and why it is a lane rather than a one-off audit.
#
# `power-mutants` SKIPS any patch carrying a `power:` line. So a floored class is
# re-measured every time that lane runs and an opted-out class is re-measured
# NEVER -- an opt-out exempts itself from the only instrument that could refute
# it. `M56` cost a phase and a half to that: an opt-out reasoned by analogy,
# never measured, and false on the day it was written (280 of 300).
#
# `power-refute` re-measures every opt-out the probe can judge SOUNDLY, and
# refuses an exemption that is not earned by the patch's own file list. Where the
# patch modifies the instrument, measurement is unsound rather than merely weak,
# and the class carries a written argument saying what a sound refutation would
# have to look like.
#
# `power-refute-decl` is its cheap half -- the partition and the headers, no
# probe, milliseconds -- and it is in the pre-push hook for the reason `power-decl`
# is: the failure that actually happens is a declaration nobody could satisfy, on
# a lane nothing runs.
.PHONY: power-refute
power-refute: ## Every OPT-OUT re-measured where measurement is sound; exemptions earned by the file list
	sh scripts/power-refute.sh

.PHONY: power-refute-decl
power-refute-decl: ## The refutation pass's declarations only -- milliseconds, no probe
	sh scripts/power-refute.sh --declarations

.PHONY: hygiene
hygiene: ## No tracked .orig/.rej: patch leftovers are stale duplicate source
	sh scripts/hygiene.sh

.PHONY: lane-coverage
lane-coverage: ## Every lane in the `ci` target actually runs in .github/workflows/ci.yml
	sh scripts/lane-coverage.sh

.PHONY: chaos-smoke
chaos-smoke: ## I2's real-mode mechanisms, ACTUALLY RUN -- see GF-63
# # Seven skips, and nobody noticed for a phase
#
# `make test` is `go test -short ./...`, and every real chaos test guards on
# testing.Short(). So the process supervisor, the end-to-end cluster, the
# composition test and the chaos run had NEVER EXECUTED IN THE PUSH LANE. They
# ran because I ran them by hand.
#
#   EVERY MECHANISM I2 BUILT WAS PROTECTED BY SOMEBODY REMEMBERING TO RUN IT.
#
# The -short guards stay -- they are right for `go test ./...`, which must not
# fork processes -- and this lane calls the same tests without it. That is the
# whole fix: the guard was never wrong, the missing lane was.
#
# THE C++ ARCHIVE IS BUILT FIRST, because a restart schedule on engine/model is
# not a crash (BUG-056) and the gate refuses it. A lane that silently ran the
# non-persistent configuration would report a green about a different system.
chaos-smoke:
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
	$(CMAKE) --build $(CPP_BUILD)/test --target rift_capi -j $(WORKERS)
	$(GO) test -count=1 -timeout 600s ./chaos/ ./net/... ./bench/

.PHONY: lint
lint: vet fmt-check determinism tooling-only hatches hygiene ## vet + formatting + the determinism vet pass

.PHONY: ci
ci: build lint test race blind power anchors blockers assertions provenance corpus corpus-reproduces bundle-seeds smoke chaos-smoke mutants mutant-covered lane-coverage cpp-ci ## Everything the push lane runs
# cpp-ci joins at the I1 merge. Before it, the two tracks had two lane sets and
# `make ci` ran one of them -- which is the lane-dependency shape: a target that
# exists and is never reached is a lane nobody runs and everybody counts.

# ------------------------------------------------------------- Track B lanes
#
# GoogleTest is vendored whole at a pinned commit, not fetched. A build step
# that reaches the network fails in exactly the situation where "reproduces
# from a clean clone" matters most, which is a stranger checking our work.
# DESIGN-B1 section 9.2.

.PHONY: cpp-vendor-check
cpp-vendor-check: ## Vendored GoogleTest matches its recorded tree hash (offline)
	@$(VENDOR_CHECK)

.PHONY: cpp-vendor-build
cpp-vendor-build: ## Vendored GoogleTest configures and builds, with no network
	@printf '\n  vendored framework build\n'
	@printf '  ----------------------------------------------------------\n'
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/vendor -DCMAKE_BUILD_TYPE=Debug
	$(CMAKE) --build $(CPP_BUILD)/vendor --target gtest_main -j $(WORKERS)

.PHONY: cpp-lane-set
cpp-lane-set: $(CPP_LANES) ## Every Track B lane, in order, without isolation

.PHONY: cpp-ci
cpp-ci: ## The whole Track B lane set with networking disabled, and proof it was
	@# COLD BUILD DIR, DELIBERATELY. A warm one carries whatever a previous
	@# networked run downloaded -- a populated FetchContent cache above all --
	@# and the lane then passes under isolation for the one reason that has
	@# nothing to do with the claim. BM21 survived this lane until the build
	@# directory was made cold; the mutant was right and the lane was wrong.
	@# NO rm -rf HERE. The cold-cache check must be about a state this
	@# recipe did not create, or it asserts the absence of something it
	@# just deleted -- which is green forever, including in the exact
	@# state HARNESS-002 occurred in. A successful run removes its own
	@# build tree at the end; a failed one leaves it to be looked at.
	@$(COLD_CACHE) $(CPP_BUILD_CI) before
	@$(NO_NETWORK) $(MAKE) cpp-lane-set CPP_BUILD=$(CPP_BUILD_CI)
	@$(COLD_CACHE) $(CPP_BUILD_CI) after

.PHONY: cpp-campaign
cpp-campaign: ## A floor under every planted flaw class; fails when one drops below it
	@# Not a member of CPP_LANES: it rebuilds the sweep once per class and costs
	@# minutes. Run it when the sweep, the workload or a mutant changes -- those
	@# are exactly the edits that move detection power without moving any lane.
	@#     make cpp-campaign MEASURE=--measure   prints instead of asserting
	@$(CPP_CAMPAIGN) $(MEASURE)

.PHONY: cpp-mutants
cpp-mutants: ## Track B mutant catalogue: each patch must redden the lane it names
	@# COST, MEASURED, BECAUSE NOTHING ELSE RUNS THIS. There is no CI here, so a
	@# lane's wall-clock is a fact about whether it gets run at all -- Track A
	@# records that as RISK-1. The full catalogue is minutes, not seconds, because
	@# each patch needs a control run and a covering run and both build from cold.
	@#
	@# To work on one mutant without paying for all of them:
	@#     make cpp-mutants ONLY="BM4-missing-dir-sync BM9-apply-does-io"
	@# The baseline gate still runs for every lane those patches name, so a subset
	@# run is a smaller experiment and not a weaker one.
	@$(CPP_MUTANTS) engine-cpp/mutants $(ONLY)


# Four lanes, four separate build directories, four separate reasons to go red.
# Each asserts AT COMPILE TIME that it has the sanitizer it claims -- and
# cpp-test asserts it has none, because it is the uninstrumented control that
# makes a red in the other three attributable. See
# engine-cpp/test/sanitizer_lane_test.cc; a lane that lost its -fsanitize flag
# fails to build rather than passing quietly.

.PHONY: cpp-scan
cpp-scan: ## Env surface: one wrapper, one Do*, one CallSite -- names, not just counts
	@$(CPP_SCAN)

.PHONY: cpp-scan-blind
cpp-scan-blind: ## Blind one scope-scan rule at a time; each must stop firing on its fixture
	@$(CPP_SCAN_BLIND)

.PHONY: cpp-sweep
cpp-sweep: ## The kill-point sweep: every Env call, killed before and after its effect
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
	$(CMAKE) --build $(CPP_BUILD)/test --target rift_sweep -j $(WORKERS)
# BOTH REGIMES, ALWAYS. The default regime never flushes -- its threshold is
# four megabytes and the workload writes six keys -- so a lane that ran only it
# would visit no kill point on the flush path at all, and every gate B2 put
# there would be green for the reason that nothing reached it. They are run
# separately and their numbers are never aggregated (section 8.4).
	$(CPP_BUILD)/test/rift_sweep default
	$(CPP_BUILD)/test/rift_sweep flush
# AND COMPACTION, ADDED AT B3.7 AS ITS OWN REGIME RATHER THAN BY GROWING
# `flush`. Reaching the L0 trigger needs four flushes -- about four times the
# flush regime's whole workload -- and folding that in would have multiplied its
# kill-point count, which is the DENOMINATOR OF EVERY RATE in FLOORS.txt, and
# diluted every B2 class measured against it. A separate regime leaves the other
# two byte-identical, so no floor moves (section 8.2a).
	$(CPP_BUILD)/test/rift_sweep compact

.PHONY: cpp-diff
cpp-diff: ## B4: the differential harness -- the C++ engine against engine/model
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
	$(CMAKE) --build $(CPP_BUILD)/test --target rift_diff -j $(WORKERS)
# THE GO JUDGE RUNS THE COMPARISON. It reads artifacts and never links the C++
# engine, so this lane is two processes by construction rather than by
# discipline -- see docs/DESIGN-B4-verification.md section 4.
	$(GO) test ./engine/differential/ -count=1

.PHONY: cpp-cgo
cpp-cgo: ## B5: the Go wrapper over the C boundary, against engine/model
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
# rift_diff IS BUILT HERE TOO, and its absence is why BM120 survived. The
# cgo differential takes its workloads from real rift_diff artifacts and SKIPS
# when the binary is missing -- and a skip inside a passing `go test` is a
# green lane. The mutant runner deletes engine-cpp/build in the copied tree, so
# every cgo-differential mutant was being scored against a test that never ran.
#
#   A LANE THAT DEPENDS ON AN ARTIFACT IT DOES NOT BUILD REPORTS THE ABSENCE OF
#   THE ARTIFACT AS SUCCESS.
	$(CMAKE) --build $(CPP_BUILD)/test --target rift_capi rift_diff -j $(WORKERS)
# ONLY THE ARCHIVE PATH IS HERE. The header's location is package-relative and
# lives in the source via ${SRCDIR}, so this package TYPECHECKS with no C++ build
# present -- which is what lets `make determinism` load the whole tree without a
# C++ toolchain. The archive is a build artifact whose directory this recipe
# chooses, so it is the one thing the lane has to say.
	CGO_LDFLAGS="-L$(CURDIR)/$(CPP_BUILD)/test -lrift_capi -lrift_engine" \
	$(GO) test -tags rift_cgo ./engine/riftcgo/ -count=1

.PHONY: cpp-rot
cpp-rot: ## Every mutant patch still applies -- seconds, where the catalogue is hours
	@# DELIBERATELY NOT IN CPP_LANES YET. It currently reports the 14 classes
	@# that rotted across B3.5-B4.2, found when B5's close ran the full
	@# catalogue for the first time since B3. Adding it to the lane set before
	@# those are re-aimed would put a red in front of a merge for a debt that
	@# predates the branch being merged -- which is the pressure GF-39 says is
	@# the wrong moment to make this kind of decision under. It joins the lane
	@# set when Ansh rules on the fourteen.
	@$(CPP_ROT)

.PHONY: cpp-bench
cpp-bench: ## B5.5: the numbers -- model, C++ native, C++ through cgo, in one table
	@# A SEPARATE, RELEASE BUILD DIRECTORY, AND IT IS NOT A DETAIL.
	@# Every other lane here builds Debug, which is right for them: assertions
	@# on, optimiser off, a failure that says where it was. The first table
	@# taken from that directory reported ~4 microseconds for a single memtable
	@# Set and a readrandom cost that did not move with batch size -- numbers
	@# that describe the compiler's -O0 output and nothing about this engine.
	@#
	@#   A BENCHMARK FROM A DEBUG BUILD IS NOT A SLOW NUMBER. IT IS NOT A NUMBER.
	@#
	@# It is a separate directory rather than a flag on the shared one so that
	@# nothing else silently starts running Release: the sweep's kill-point
	@# counts and every floor in FLOORS.txt are measured against Debug builds,
	@# and a lane that quietly changed build type underneath them would move
	@# denominators nobody was watching.
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/bench -DRIFT_SANITIZER=none -DCMAKE_BUILD_TYPE=Release
	$(CMAKE) --build $(CPP_BUILD)/bench --target rift_bench rift_capi -j $(WORKERS)
	RIFT_BENCH=1 RIFT_BENCH_BIN=$(CURDIR)/$(CPP_BUILD)/bench/rift_bench \
	CGO_LDFLAGS="-L$(CURDIR)/$(CPP_BUILD)/bench -lrift_capi -lrift_engine" \
	$(GO) test -tags rift_cgo ./engine/riftcgo/ -run TestBenchmarkTable -v -count=1 -timeout 40m

.PHONY: cpp-amp
cpp-amp: ## B3.7b: compaction amplification -- the measurement that decides B3-D3
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
	$(CMAKE) --build $(CPP_BUILD)/test --target rift_amp -j $(WORKERS)
	$(CPP_BUILD)/test/rift_amp

.PHONY: cpp-build
cpp-build: ## Build every C++ target and run nothing -- the control for "did the patch compile?"
	@# Not a member of CPP_LANES: cpp-test subsumes it. It exists so a mutant can
	@# declare a control that separates "the lane caught the defect" from "the
	@# patch broke the build", which are different results that look identical in
	@# an exit code.
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
	$(CMAKE) --build $(CPP_BUILD)/test -j $(WORKERS)

.PHONY: cpp-test
cpp-test: ## C++ unit suite, uninstrumented -- the control the other three need
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/test -DRIFT_SANITIZER=none
	$(CMAKE) --build $(CPP_BUILD)/test --target rift_engine_test rift_tsan_harness -j $(WORKERS)
	$(CPP_BUILD)/test/rift_engine_test
	@# The TSan harness is BUILT here and deliberately NOT RUN. Building it
	@# makes cpp-test a real control for the TSan canary -- the race patch
	@# compiles, so cpp-tsan's red is the race and not a broken build.
	@# Running it here would defeat that: an unlocked counter across four
	@# threads produces a wrong total often enough that cpp-test would go
	@# red too, and the control would be gone.

.PHONY: cpp-asan
cpp-asan: ## C++ unit suite under AddressSanitizer
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/asan -DRIFT_SANITIZER=address
	$(CMAKE) --build $(CPP_BUILD)/asan --target rift_engine_test -j $(WORKERS)
	ASAN_OPTIONS=abort_on_error=0:detect_leaks=0 $(CPP_BUILD)/asan/rift_engine_test

.PHONY: cpp-ubsan
cpp-ubsan: ## C++ unit suite under UndefinedBehaviorSanitizer
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/ubsan -DRIFT_SANITIZER=undefined
	$(CMAKE) --build $(CPP_BUILD)/ubsan --target rift_engine_test -j $(WORKERS)
	UBSAN_OPTIONS=print_stacktrace=1 $(CPP_BUILD)/ubsan/rift_engine_test

.PHONY: cpp-tsan
cpp-tsan: ## ThreadSanitizer over the dedicated multi-threaded harness, not the unit suite
	$(CMAKE) -S $(CPP_SRC) -B $(CPP_BUILD)/tsan -DRIFT_SANITIZER=thread
	$(CMAKE) --build $(CPP_BUILD)/tsan --target rift_tsan_harness -j $(WORKERS)
	TSAN_OPTIONS=halt_on_error=1 $(CPP_BUILD)/tsan/rift_tsan_harness

# ---------------------------------------------------------------- stub lanes

.PHONY: smoke
smoke: ## $(SMOKE_SEEDS)-seed smoke: green means NO VIOLATION FOUND, not all-seeds-passed
# # WHAT GREEN MEANS HERE, stated because it was not
#
# This lane gates on VIOLATIONS. It exits 0 while carrying inconclusives, and the
# most recent run did exactly that: `0 violation, 1 inconclusive, 0 harness
# errors` out of 500 seeds, exit 0.
#
# That is consistent with Amendment A4 and it is worth saying why, since the
# opposite reading is available. A4 forbids BANKING an inconclusive as a pass --
# counting it toward a claim, quoting it in a total, letting it stand in for a
# seed that was checked. This lane banks nothing: it is a SEARCH for a violation,
# it prints the inconclusive count on its own line beside the violation count,
# which is the form A4 requires of the public claim, and the word it prints when
# it succeeds is `no violation found` rather than `all seeds passed`.
#
# **What A4 does require and nothing here does yet: watch the RATE.** A4 says a
# rising inconclusive rate is the signal to shrink history windows or partition
# harder per key, and never to loosen the checker. Nobody tracks the rate. One
# in 500 is where it stands the day this was written, recorded so a later reader
# has a number to compare against rather than an impression.
	$(GO) run ./cmd/simctl hunt --from 0 --to $(SMOKE_SEEDS) --workers $(WORKERS)

.PHONY: exit-run
exit-run: ## The exit run (shape read from the options it sweeps): $(EXIT_SEEDS) seeds across $(EXIT_SHARDS) contiguous shards, aggregated
	sh scripts/exit-run.sh $(EXIT_SEEDS) $(EXIT_SHARDS) $(EXIT_OUT)
	RAFT_SHARD_DIR=$(EXIT_OUT) RAFT_TOTAL=$(EXIT_SEEDS) \
		$(GO) test -count=1 -run TestRaftExitAggregate -v ./sim/hunt/

.PHONY: soak
soak: ## $(SOAK_SEEDS)-seed nightly soak
	$(GO) run ./cmd/simctl hunt --from 0 --to $(SOAK_SEEDS) --workers $(WORKERS)

.PHONY: mutants
mutants: ## Mutant suite: every planted defect must be caught by its declared test
	@$(MUTANTS)

.PHONY: hooks
hooks: ## Install the pre-push hook that runs the every-change lanes
	@mkdir -p .git/hooks
	@cp scripts/hooks/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@printf '  installed .git/hooks/pre-push -- the every-change lanes now run on push.\n'
	@printf '  It is not a remote. It is the half of a remote that can be had without one.\n'

.PHONY: nightly
nightly: covering race-soak soak ## The nightly tier: full seed ranges, then the 10k soak
	@printf '\n  nightly complete. The three solo measurements are `make solo`.\n\n'

.PHONY: solo
solo: ## The three measurements that need the machine to themselves, in order
	@printf '\n  SOLO SLOT: three measurements, none of which may share a machine.\n'
	@printf '  Nothing else should be running. This is hours, not minutes.\n\n'
	LANE_SEEDS=$(SOLO_LANE_SEEDS) $(GO) test -count=1 -timeout 600m -v \
		-run TestUnthrottledCollector ./sim/hunt/
	$(MAKE) power-mutants
	sh scripts/race-curve.sh

.PHONY: race-curve
race-curve: ## Measure what RACE_SEEDS actually needs to be (50, 100, 200)
	sh scripts/race-curve.sh

.PHONY: bench
bench: ## [STUB->B5/I2] Benchmark smoke with regression tracking
	@$(STUB) bench B5/I2 "no benchmark number is published until it reproduces by script"

.PHONY: killpoints
killpoints: ## [STUB->B4] Crash-consistency kill-point sweep across the write path
	@$(STUB) killpoints B4 "Track B"

.PHONY: differential
differential: ## [STUB->B4] Differential engine lane: C++ engine vs engine/model
	@$(STUB) differential B4 "iterator output must be byte-identical"

# ---------------------------------------------------------------------- meta

.PHONY: lanes
lanes: ## Show which lanes are real and which are still stubs
	@echo "REAL : build test race vet fmt-check tidy-check determinism tooling-only"
	@echo "       hatches blind power power-toy power-mutants power-refute assertions"
	@echo "       provenance corpus"
	@echo "       smoke soak"
	@echo "       mutants"
	@echo "       cpp-vendor-check cpp-vendor-build cpp-ci cpp-mutants"
	@echo "       cpp-test cpp-asan cpp-ubsan cpp-tsan cpp-scan cpp-scan-blind cpp-sweep"
	@echo "       cpp-campaign cpp-build cpp-diff cpp-cgo"
	@echo "STUB : bench(I2)"
	@echo "       killpoints(B4) differential(B4)"
	@echo
	@echo "A stub lane passes trivially and proves nothing."

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
