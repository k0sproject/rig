SCHEMA_TOOL := go run .
SCHEMA_TYPES := ssh openssh winrm localhost
TARGET := ../../schemas

.PHONY: all json yaml clean deps

all: json yaml

json: $(SCHEMA_TYPES:%=$(TARGET)/%.json)

yaml: $(SCHEMA_TYPES:%=$(TARGET)/%.yaml)

deps:
	go mod tidy

$(TARGET): deps
	@mkdir -p $(TARGET)

GEN_SOURCES := gen_schema.go go.mod go.sum

$(TARGET)/%.json: $(TARGET) ../../%.go $(GEN_SOURCES)
	$(SCHEMA_TOOL) -type=$(basename $(notdir $(basename $@))) > $@

$(TARGET)/%.yaml: $(TARGET) ../../%.go $(GEN_SOURCES)
	$(SCHEMA_TOOL) -yaml -type=$(basename $(notdir $(basename $@))) > $@

clean:
	@for t in $(SCHEMA_TYPES); do \
		rm -f $(TARGET)/$$t.json $(TARGET)/$$t.yaml; \
	done
