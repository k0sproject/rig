KEY_TYPE ?= rsa
KEY_SIZE ?= 4096
KEY_PASSPHRASE ?= ""
KEY_PATH ?= ".ssh/identity"

footloose := $(shell which footloose)
ifeq ($(footloose),)
footloose := $(shell go env GOPATH)/bin/footloose
endif

envsubst := $(shell which envsubst)
ifeq ($(envsubst),)
$(error 'envsubst' NOT found in path, please install it and re-run)
endif

sshkeygen := $(shell which ssh-keygen)
ifeq ($(sshkeygen),)
$(error 'ssh-keygen' NOT found in path, please install it and re-run)
endif

.PHONY: rigtest
rigtest:
	go build -o rigtest ../cmd/rigtest

$(footloose):
	go install github.com/weaveworks/footloose/...@0.6.3

.ssh:
	mkdir -p .ssh

.ssh/identity: .ssh
	ssh-keygen -t $(KEY_TYPE) -b $(KEY_SIZE) -f .ssh/identity -N $(KEY_PASSPHRASE)

footloose.yaml: .ssh/identity
	$(footloose) config create \
		--config footloose.yaml \
	  --image quay.io/footloose/ubuntu18.04 \
		--name rigtest \
	  --key .ssh/identity \
		--replicas 1 \
    --override

.PHONY: create-host
create-host: footloose.yaml
	$(footloose) create -c footloose.yaml

.PHONY: delete-host
delete-host: footloose.yaml
	$(footloose) delete -c footloose.yaml

.PHONY: clean
clean: delete-host
	rm -f footloose.yaml identity rigtest
	rm -rf .ssh

.PHONY: sshport
sshport:
	@$(footloose) show node0 -o json|grep hostPort|grep -oE "[0-9]+"

.PHONY: run
run: rigtest create-host
	./rigtest \
		-host 127.0.0.1 \
		-port $(shell $(MAKE) sshport) \
	  -keypath $(KEY_PATH) \
		-user root

.PHONY: test
test: rigtest
	./test.sh
