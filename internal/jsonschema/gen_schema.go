package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"

	"github.com/k0sproject/rig/v2/protocol/openssh"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/winrm"
)

const (
	fieldCertPath = "certPath"
	fieldKeyPath  = "keyPath"
)

// clearFlowStyle recursively clears the flow style on a yaml.Node tree so the
// encoder produces block-style YAML instead of JSON-in-YAML inline notation.
func clearFlowStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		clearFlowStyle(child)
	}
}

// ipOrHostname replaces a plain string schema with anyOf: hostname | ipv4 | ipv6.
func ipOrHostname(prop *jsonschema.Schema) {
	prop.Format = ""
	prop.Type = ""
	prop.AnyOf = []*jsonschema.Schema{
		{Type: "string", Format: "hostname"},
		{Type: "string", Format: "ipv4"},
		{Type: "string", Format: "ipv6"},
	}
}

// applyWinRMAuthConstraint adds auth constraints to the WinRM definition:
// anyOf requires user, password, or both certPath+keyPath; dependentRequired
// ensures certPath and keyPath are always provided together even when other
// auth fields satisfy the anyOf.
func applyWinRMAuthConstraint(schema *jsonschema.Schema) {
	def, ok := schema.Definitions["WinRM"]
	if !ok {
		return
	}
	def.AnyOf = []*jsonschema.Schema{
		{Required: []string{"user", "password"}},
		{Required: []string{fieldCertPath, fieldKeyPath}},
	}
	def.DependentRequired = map[string][]string{
		fieldCertPath: {fieldKeyPath},
		fieldKeyPath:  {fieldCertPath},
	}
}

// applyAddressFormats walks all $defs and converts any "address"-named string
// property to the hostname|ipv4|ipv6 anyOf shape.
func applyAddressFormats(schema *jsonschema.Schema) {
	for _, def := range schema.Definitions {
		if def.Properties == nil {
			continue
		}
		if prop, ok := def.Properties.Get("address"); ok {
			ipOrHostname(prop)
		}
		if prop, ok := def.Properties.Get("tlsServerName"); ok {
			ipOrHostname(prop)
		}
	}
}

// localhostSchema returns a schema for the LocalhostConfig type, which accepts
// both the canonical boolean true and the legacy {enabled: true} object form.
func localhostSchema() *jsonschema.Schema {
	enabledProp := &jsonschema.Schema{
		Const: true,
	}
	props := jsonschema.NewProperties()
	props.Set("enabled", enabledProp)
	legacyForm := &jsonschema.Schema{
		Type:                 "object",
		Properties:           props,
		Required:             []string{"enabled"},
		AdditionalProperties: jsonschema.FalseSchema,
	}
	return &jsonschema.Schema{
		Version: "https://json-schema.org/draft/2020-12/schema",
		ID:      "https://github.com/k0sproject/rig/v2/localhost",
		OneOf: []*jsonschema.Schema{
			{Const: true},
			legacyForm,
		},
	}
}

func main() {
	var name string
	var useYAML bool

	flag.StringVar(&name, "type", "", "Type to generate schema for (ssh, openssh, winrm, localhost)")
	flag.BoolVar(&useYAML, "yaml", false, "Output YAML instead of JSON")
	flag.Parse()

	var schema *jsonschema.Schema
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		Namer: func(t reflect.Type) string {
			pkg := t.PkgPath()
			switch {
			case strings.HasSuffix(pkg, "protocol/ssh"):
				return "SSH"
			case strings.HasSuffix(pkg, "protocol/winrm"):
				return "WinRM"
			case strings.HasSuffix(pkg, "protocol/openssh"):
				return "OpenSSH"
			default:
				return t.Name()
			}
		},
	}

	switch name {
	case "ssh":
		schema = reflector.Reflect(new(ssh.Config))
		schema.ID = "https://github.com/k0sproject/rig/v2/ssh"
		applyAddressFormats(schema)
	case "openssh":
		schema = reflector.Reflect(new(openssh.Config))
		schema.ID = "https://github.com/k0sproject/rig/v2/open-ssh"
	case "winrm":
		schema = reflector.Reflect(new(winrm.Config))
		schema.ID = "https://github.com/k0sproject/rig/v2/win-rm"
		applyAddressFormats(schema)
		applyWinRMAuthConstraint(schema)
	case "localhost":
		schema = localhostSchema()
	default:
		fmt.Fprintf(os.Stderr, "unknown type: %q\n", name)
		os.Exit(1)
	}

	if useYAML {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(schema); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode JSON: %v\n", err)
			os.Exit(1)
		}

		var node yaml.Node
		if err := yaml.Unmarshal(buf.Bytes(), &node); err != nil {
			fmt.Fprintf(os.Stderr, "failed to unmarshal JSON: %v\n", err)
			os.Exit(1)
		}
		clearFlowStyle(&node)

		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(&node); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode YAML: %v\n", err)
			os.Exit(1)
		}
		if err := enc.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close YAML encoder: %v\n", err)
			os.Exit(1)
		}
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode schema: %v\n", err)
		os.Exit(1)
	}
}
