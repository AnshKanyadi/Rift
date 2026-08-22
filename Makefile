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
#                  capped to a handful of seeds -- proves the path RUNS
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

# RACE_SEEDS bounds the A1 exit run inside the race lane, and the number is
# stated here rather than hidden in a skip.
#
# The race lane asks one question: does any cross-goroutine interaction reach
# node state off the mailbox? That is answered by the real-mode driver's tests
# and by a few hundred simulated seeds; it is not answered any better by ten
# thousand. The instrumentation costs about 20x, so the default exit run takes
# roughly ten hours under -race and about three minutes without it. Seed
# coverage is `make test` and `make soak`, which run the full 10k uninstrumented.
#
# A4 widened what RACE_SEEDS bounds. It capped only the exit run; every covering
# test ran its full seed search under -race, and the package crossed Go's
# ten-minute default with no data race in it at all -- a timeout reported as a
# failure. It now caps every seed search in sim/hunt (see boundSeeds), and what
# that costs is written down where it happens: a capped search proves nothing
# about DETECTION, only that no cross-goroutine interaction reaches core state
# while that code runs. Detection is claimed by the unraced lanes and the mutant
# lane, which run the full ranges.
#
# The explicit timeout is here so the next time this budget is exceeded the lane
# says so instead of panicking at a default nobody chose.
RACE_SEEDS ?= 200
# 90 minutes. A4 roughly doubled the per-seed cost -- more ranges, more
# messages, and 1.85 million routing refusals across the exit sweep -- and
# sim/hunt crossed 30 minutes under instrumentation with zero data races in it.
#
# The budget is what moves, not the seed bound. RACE_SEEDS is 200 because A1
# ruled that a few hundred simulated seeds answer this lane's question, and
# lowering it to buy wall clock would be trading a recorded scope for a number
# nobody ruled on.
RACE_TIMEOUT ?= 5400s

.PHONY: race
race: ## Go unit tests under -race (seed searches bounded to $(RACE_SEEDS) seeds; see the note above)
	RAFT_SEEDS=$(RACE_SEEDS) $(GO) test -race -timeout $(RACE_TIMEOUT) ./...

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
	$(GO) run ./tools/determinismcheck/cmd/determinismcheck ./...

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

.PHONY: assertions
assertions: ## Every declared every-run assertion mechanism must actually be invoked
	$(GO) test -count=1 -run 'TestEveryAssertionMechanismIsInvoked|TestAssertionRegistryIsWellFormed' ./sim/hunt/

.PHONY: power
power: power-toy power-mutants ## Harness-power floors: every planted flaw class must still be detected at its floor

.PHONY: power-toy
power-toy: ## The toy's four flaw classes, floored since A0
	$(GO) test -count=1 -run 'TestHarnessPower|TestEveryObservableFlawHasAFloor' ./sim/hunt/

.PHONY: power-mutants
power-mutants: ## Every MUTANT class: detection rate against a standing floor, or an explicit opt-out
	@$(POWERMUTANTS)

.PHONY: lane-coverage
lane-coverage: ## Every lane in the `ci` target actually runs in .github/workflows/ci.yml
	sh scripts/lane-coverage.sh

.PHONY: lint
lint: vet fmt-check determinism tooling-only hatches ## vet + formatting + the determinism vet pass

.PHONY: ci
ci: build lint test race blind power assertions provenance corpus corpus-reproduces bundle-seeds smoke mutants lane-coverage ## Everything the push lane runs

# ---------------------------------------------------------------- stub lanes

.PHONY: smoke
smoke: ## $(SMOKE_SEEDS)-seed simulator smoke: the correct toy, all checkers on
	$(GO) run ./cmd/simctl hunt --from 0 --to $(SMOKE_SEEDS) --workers $(WORKERS)

.PHONY: exit-run
exit-run: ## The A6 exit run: $(EXIT_SEEDS) seeds across $(EXIT_SHARDS) contiguous shards, aggregated
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

.PHONY: cpp-test
cpp-test: ## [STUB->B1] C++ engine unit tests
	@$(STUB) cpp-test B1 "CMake + GoogleTest in engine-cpp/ (Track B)"

.PHONY: cpp-asan
cpp-asan: ## [STUB->B1] C++ engine tests under AddressSanitizer
	@$(STUB) cpp-asan B1 "Track B"

.PHONY: cpp-ubsan
cpp-ubsan: ## [STUB->B1] C++ engine tests under UndefinedBehaviorSanitizer
	@$(STUB) cpp-ubsan B1 "Track B"

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
	@echo "       hatches blind power power-toy power-mutants assertions provenance corpus"
	@echo "       smoke soak"
	@echo "       mutants"
	@echo "STUB : (none in A0)"
	@echo "       bench(B5/I2) cpp-test(B1) cpp-asan(B1) cpp-ubsan(B1)"
	@echo "       killpoints(B4) differential(B4)"
	@echo
	@echo "A stub lane passes trivially and proves nothing."

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
