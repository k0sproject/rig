SCHEMA_TOOL := go run .
SCHEMA_TYPES := ssh openssh winrm localhost
TARGET := ../../schemas

.PHONY: all json yaml clean deps test

all: json yaml

json: $(SCHEMA_TYPES:%=$(TARGET)/%.json)

yaml: $(SCHEMA_TYPES:%=$(TARGET)/%.yaml)

deps:
	go mod tidy

test:
	go test ./...

PROTO_SOURCES := $(wildcard ../../protocol/ssh/*.go ../../protocol/openssh/*.go ../../protocol/winrm/*.go)

$(TARGET)/%.json: gen_schema.go go.mod go.sum $(PROTO_SOURCES)
	@mkdir -p $(TARGET)
	$(SCHEMA_TOOL) -type=$(basename $(notdir $(basename $@))) > $@

$(TARGET)/%.yaml: gen_schema.go go.mod go.sum $(PROTO_SOURCES)
	@mkdir -p $(TARGET)
	$(SCHEMA_TOOL) -yaml -type=$(basename $(notdir $(basename $@))) > $@

clean:
	@for t in $(SCHEMA_TYPES); do \
		rm -f $(TARGET)/$$t.json $(TARGET)/$$t.yaml; \
	done
