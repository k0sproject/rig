package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"

	"github.com/k0sproject/rig"
)

func main() {
	var name string
	var useYAML bool

	flag.StringVar(&name, "type", "", "Type to generate schema for (ssh, openssh, winrm, localhost)")
	flag.BoolVar(&useYAML, "yaml", false, "Output YAML instead of JSON")
	flag.Parse()

	var schema *jsonschema.Schema
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
	}

	switch name {
	case "ssh":
		schema = reflector.Reflect(new(rig.SSH))
	case "openssh":
		schema = reflector.Reflect(new(rig.OpenSSH))
	case "winrm":
		schema = reflector.Reflect(new(rig.WinRM))
	case "localhost":
		schema = reflector.Reflect(new(rig.Localhost))
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

		var raw any
		if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
			fmt.Fprintf(os.Stderr, "failed to unmarshal JSON: %v\n", err)
			os.Exit(1)
		}

		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(raw); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode YAML: %v\n", err)
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
