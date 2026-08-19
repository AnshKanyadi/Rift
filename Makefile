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

.DEFAULT_GOAL := help

# ---------------------------------------------------------------- real lanes

.PHONY: build
build: ## Compile everything
	$(GO) build ./...

.PHONY: test
test: ## Go unit tests
	$(GO) test ./...

# RACE_SEEDS bounds the A1 exit run inside the race lane, and the number is
# stated here rather than hidden in a skip.
#
# The race lane asks one question: does any cross-goroutine interaction reach
# node state off the mailbox? That is answered by the real-mode driver's tests
# and by a few hundred simulated seeds; it is not answered any better by ten
# thousand. The instrumentation costs about 20x, so the default exit run takes
# roughly ten hours under -race and about three minutes without it. Seed
# coverage is `make test` and `make soak`, which run the full 10k uninstrumented.
RACE_SEEDS ?= 200

.PHONY: race
race: ## Go unit tests under -race (A1 exit run bounded to $(RACE_SEEDS) seeds; see the note above)
	RAFT_SEEDS=$(RACE_SEEDS) $(GO) test -race ./...

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

.PHONY: lint
lint: vet fmt-check determinism tooling-only hatches ## vet + formatting + the determinism vet pass

.PHONY: ci
ci: build lint test race blind power assertions provenance corpus smoke mutants ## Everything the push lane runs

# ---------------------------------------------------------------- stub lanes

.PHONY: smoke
smoke: ## $(SMOKE_SEEDS)-seed simulator smoke: the correct toy, all checkers on
	$(GO) run ./cmd/simctl hunt --from 0 --to $(SMOKE_SEEDS) --workers $(WORKERS)

.PHONY: soak
soak: ## $(SOAK_SEEDS)-seed nightly soak
	$(GO) run ./cmd/simctl hunt --from 0 --to $(SOAK_SEEDS) --workers $(WORKERS)

.PHONY: mutants
mutants: ## Mutant suite: every planted defect must be caught by its declared test
	@$(MUTANTS)

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
