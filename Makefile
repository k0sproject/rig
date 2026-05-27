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
	@go list ./... | while read pkg; do \
		go test "$$pkg" -list '^Fuzz' | grep '^Fuzz' | while read fuzz; do \
			echo "==> $$pkg $$fuzz"; \
			go test "$$pkg" -run=^$$ -fuzz="^$$fuzz$$" -fuzztime=$(FUZZ_TIME); \
		done; \
	done
