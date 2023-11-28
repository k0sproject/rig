KEY_TYPE ?= rsa
KEY_SIZE ?= 4096
KEY_PASSPHRASE ?= ""
KEY_PATH ?= ".ssh/identity"
REPLICAS ?= 1
LINUX_IMAGE ?= "quay.io/k0sproject/bootloose-ubuntu20.04"

.PHONY: test
test: gomod 
	./test.sh

bootloose := $(shell which bootloose)
ifeq ($(bootloose),)
bootloose := $(shell go env GOPATH)/bin/bootloose
endif

envsubst := $(shell which envsubst)
ifeq ($(envsubst),)
$(error 'envsubst' NOT found in path, please install it and re-run)
endif

sshkeygen := $(shell which ssh-keygen)
ifeq ($(sshkeygen),)
$(error 'ssh-keygen' NOT found in path, please install it and re-run)
endif

$(bootloose):
	go install github.com/k0sproject/bootloose@latest

.ssh:
	mkdir -p .ssh

.PHONY: gomod
gomod: 
	go mod download

.ssh/identity: .ssh
	rm -f .ssh/identity
	ssh-keygen -t $(KEY_TYPE) -b $(KEY_SIZE) -f .ssh/identity -N $(KEY_PASSPHRASE)

.PHONY: docker-network
docker-network:
	docker network inspect bootloose-cluster || docker network create bootloose-cluster --subnet 172.16.86.0/24 --gateway 172.16.86.1 --attachable

bootloose.yaml: .ssh/identity $(bootloose)
	$(bootloose) config create \
		--config bootloose.yaml \
	  --image $(LINUX_IMAGE) \
		--name rigtest \
	  --key .ssh/identity \
		--networks bootloose-cluster \
    --override \
		--replicas $(REPLICAS)

.PHONY: create-host
create-host: bootloose.yaml docker-network
	$(bootloose) create -c bootloose.yaml

.PHONY: delete-host
delete-host: bootloose.yaml
	$(bootloose) delete -c bootloose.yaml

.PHONY: clean
clean: delete-host
	rm -f bootloose.yaml identity
	rm -rf .ssh
	docker network rm bootloose-cluster || true

.PHONY: sshport
sshport:
	@$(bootloose) show node0 -o json|grep hostPort|grep -oE "[0-9]+"

.PHONY: run
run: create-host gomod
	go test -v ./... -args \
		-host 127.0.0.1 \
		-port $(shell $(MAKE) sshport) \
	  -ssh-keypath $(KEY_PATH) \
		-user root
