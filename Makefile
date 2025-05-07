GO_TESTS := $(shell find . -type f -name '*_test.go')
INT_TESTS := $(shell git ls-files test/)

alpine_version = 3.21
alpine_patch_version = $(alpine_version).3
go_version = 1.24.2
golang_buildimage=docker.io/library/golang:$(go_version)-alpine$(alpine_version)

GOLANG_IMAGE ?= $(golang_buildimage)

RIG_GO_BUILD_CACHE ?= build/cache
DOCKER ?= docker
BUILD_UID ?= $(shell id -u)
BUILD_GID ?= $(shell id -g)

$(RIG_GO_BUILD_CACHE):
	mkdir -p -- '$@'

.rigbuild.docker-image.k0s: build/Dockerfile | $(RIG_GO_BUILD_CACHE)
	$(DOCKER) build --progress=plain --iidfile '$@' \
	  --build-arg BUILDIMAGE=$(GOLANG_IMAGE) \
	  -t rigbuild.docker-image.k0s - <build/Dockerfile

GO_ENV ?= $(DOCKER) run --rm \
  -v '$(realpath $(RIG_GO_BUILD_CACHE))':/run/k0s-build \
  -v '$(CURDIR)':/go/src/github.com/k0sproject/rig \
  -w /go/src/github.com/k0sproject/rig \
  -e GOOS \
  -e CGO_ENABLED \
  -e CGO_CFLAGS \
  -e GOARCH \
  --user $(BUILD_UID):$(BUILD_GID) \
  -- '$(shell cat .rigbuild.docker-image.k0s)'
GO ?= $(GO_ENV) go

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

GO_LINT_DIRS ?= $(shell ls -d */ | grep -v build/)

.PHONY: lint
lint: .rigbuild.docker-image.k0s
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	$(GO_ENV) golangci-lint run --verbose --build-tags=$(subst $(space),$(comma),$(BUILD_GO_TAGS)) $(GOLANGCI_LINT_FLAGS) $(GO_LINT_DIRS) 

.PHONY: clean-gocache
clean-gocache:
	-chmod -R u+w -- '$(RIG_GO_BUILD_CACHE)/go/mod'
	rm -rf -- '$(RIG_GO_BUILD_CACHE)/go'
