GO_TESTS := $(shell find . -type f -name '*_test.go')
INT_TESTS := $(shell git ls-files test/)

gotest := $(shell which gotest)
ifeq ($(gotest),)
gotest := go test
endif

.PHONY: test
test: $(GO_SRCS) $(GO_TESTS)
	$(gotest) -v ./...

.PHONY: inttest
inttest: $(GO_SRCS} $(INT_TESTS)
	$(MAKE) -C test

ifeq ($(FIX),true)
fixparam := --fix
else
fixparam :=
endif

.PHONY: lint
lint:
	golangci-lint run -v $(fixparam)

FUZZ_TIME := 10s
.PHONY: fuzz
fuzz:
	@failed=0; \
	for pkg in $$(go list ./...); do \
		fuzz_list=$$(go test "$$pkg" -list '^Fuzz' 2>&1); \
		list_status=$$?; \
		if [ $$list_status -ne 0 ]; then \
			printf '%s\n' "$$fuzz_list"; \
			failed=1; \
			continue; \
		fi; \
		for fuzz in $$(printf '%s\n' "$$fuzz_list" | grep '^Fuzz'); do \
			echo "==> $$pkg $$fuzz"; \
			go test "$$pkg" -run=^$$ -fuzz="^$$fuzz$$" -fuzztime=$(FUZZ_TIME) || failed=1; \
		done; \
	done; \
	exit $$failed
