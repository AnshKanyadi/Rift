# Rift -- test lanes.
#
# Lanes marked STUB exist so CI wiring is real from day one, but they check
# nothing yet and say so loudly. `make lanes` prints the status of every lane.
# No lane may remain a stub past the phase named against it.

GO           ?= go
STUB         := ./scripts/lane-stub.sh
TOOLING_ONLY := ./scripts/tooling-only.sh
BLIND        := ./scripts/blind-analyzer.sh
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

.PHONY: race
race: ## Go unit tests under -race
	$(GO) test -race ./...

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

.PHONY: blind
blind: ## Mutation-test the determinism pass itself; each blinded rule must fail its own test
	@$(BLIND)

.PHONY: lint
lint: vet fmt-check determinism tooling-only ## vet + formatting + the determinism vet pass

.PHONY: ci
ci: build lint test race blind smoke mutants ## Everything the push lane runs

# ---------------------------------------------------------------- stub lanes

.PHONY: smoke
smoke: ## [STUB->A0.10] $(SMOKE_SEEDS)-seed simulator smoke
	@$(STUB) smoke A0.10 "simctl run over $(SMOKE_SEEDS) seeds, all checkers on"

.PHONY: soak
soak: ## [STUB->A0.11] $(SOAK_SEEDS)-seed nightly soak
	@$(STUB) soak A0.11 "simctl hunt --workers $(WORKERS) --seeds $(SOAK_SEEDS)"

.PHONY: mutants
mutants: ## [STUB->A0.12] Mutant suite; must kill every mutant within budget, records kill-time
	@$(STUB) mutants A0.12 "sim/toy/mutants; kill-time per mutant recorded (CLAUDE.md A2)"

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
	@echo "REAL : build test race vet fmt-check tidy-check determinism tooling-only blind"
	@echo "STUB : smoke(A0.10) soak(A0.11) mutants(A0.12)"
	@echo "       bench(B5/I2) cpp-test(B1) cpp-asan(B1) cpp-ubsan(B1)"
	@echo "       killpoints(B4) differential(B4)"
	@echo
	@echo "A stub lane passes trivially and proves nothing."

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
