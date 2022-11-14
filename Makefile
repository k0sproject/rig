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

