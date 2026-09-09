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

.PHONY: schemas
schemas:
	$(MAKE) -C internal/jsonschema

# Highest release tag for this module's major version that HEAD descends from.
# Override to compare against something else: make apidiff API_BASE=v2.0.0
API_BASE ?= $(shell git tag --merged HEAD --sort=-v:refname | grep -E '^v2\.[0-9]+\.[0-9]+$$' | head -n1)

.PHONY: apidiff
apidiff:
	@set -e; \
	base="$(API_BASE)"; \
	test -n "$$base" || { echo "apidiff: no v2 tag reachable from HEAD; pass API_BASE=<ref>" >&2; exit 1; }; \
	worktree=$$(mktemp -d); \
	bindir=$$(mktemp -d); \
	binary="$$bindir/apidiff"; \
	trap 'git worktree remove --force "$$worktree" >/dev/null 2>&1 || rm -rf "$$worktree"; rm -rf "$$bindir"' EXIT; \
	git worktree add --detach --quiet "$$worktree" "$$base"; \
	go build -C internal/apidiff -o "$$binary" .; \
	echo "# API diff: $$base -> working tree"; \
	echo; \
	status=0; \
	"$$binary" -base "$$worktree" -head "$(CURDIR)" || status=$$?; \
	case $$status in 0|3) ;; *) exit $$status ;; esac

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
