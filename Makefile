# Rift -- test lanes.
#
# Lanes marked STUB exist so CI wiring is real from day one, but they check
# nothing yet and say so loudly. `make lanes` prints the status of every lane.
# No lane may remain a stub past the phase named against it.

GO           ?= go
STUB         := ./scripts/lane-stub.sh
TOOLING_ONLY := ./scripts/tooling-only.sh
BLIND        := ./scripts/blind-analyzer.sh

# ---- Track B (C++ engine). DESIGN-B1 section 9.3.
CMAKE        ?= cmake
CPP_SRC      := engine-cpp
CPP_BUILD    ?= engine-cpp/build
CPP_BUILD_CI := engine-cpp/build-ci
VENDOR_CHECK := ./scripts/cpp-vendor-check.sh
NO_NETWORK   := ./scripts/cpp-no-network.sh
CPP_MUTANTS  := ./scripts/cpp-mutants.sh
CPP_SCAN     := ./scripts/cpp-scan.sh
CPP_SCAN_BLIND := ./scripts/cpp-scan-blind.sh
COLD_CACHE   := ./scripts/cpp-cold-cache.sh
# The lane set `make cpp-ci` runs under network isolation. It grows as
# lanes un-stub; every member must be runnable by hand, because nothing
# runs it for us.
CPP_LANES    := cpp-vendor-check cpp-scan cpp-scan-blind cpp-vendor-build cpp-test cpp-asan cpp-ubsan cpp-tsan
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

.PHONY: hatches
hatches: ## Assert HATCHES.txt matches the tree exactly (never rewrites it; -update-hatches is local-only)
	$(GO) test -count=1 -run TestHatchRegistry ./tools/determinismcheck/

.PHONY: blind
blind: ## Mutation-test the determinism pass itself; each blinded rule must fail its own test
	@$(BLIND)

.PHONY: lint
lint: vet fmt-check determinism tooling-only hatches ## vet + formatting + the determinism vet pass

.PHONY: ci
ci: build lint test race blind smoke mutants ## Everything the push lane runs

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

.PHONY: cpp-mutants
cpp-mutants: ## Track B mutant catalogue: each patch must redden the lane it names
	@$(CPP_MUTANTS)


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
	@echo "       hatches blind"
	@echo "       cpp-vendor-check cpp-vendor-build cpp-ci cpp-mutants"
	@echo "       cpp-test cpp-asan cpp-ubsan cpp-tsan cpp-scan cpp-scan-blind"
	@echo "       cpp-build"
	@echo "STUB : smoke(A0.10) soak(A0.11) mutants(A0.12) bench(B5/I2)"
	@echo "       killpoints(B4) differential(B4)"
	@echo
	@echo "A stub lane passes trivially and proves nothing."

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
