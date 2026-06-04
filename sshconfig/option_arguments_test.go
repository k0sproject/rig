package sshconfig_test

import (
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/sshconfig"
)

func TestOptionArgumentsCopy(t *testing.T) {
	orig := sshconfig.OptionArguments{"Key": "val"}
	dup := orig.Copy()
	dup["Key"] = "changed"
	if orig["Key"] != "val" {
		t.Errorf("Copy() shares map with original; orig[Key] = %v, want %q", orig["Key"], "val")
	}
}

func TestOptionArgumentsSetIsSet(t *testing.T) {
	o := sshconfig.OptionArguments{}
	if o.IsSet("Compression") {
		t.Error("IsSet(Compression) = true on empty map, want false")
	}
	o.Set("Compression", true)
	if !o.IsSet("Compression") {
		t.Error("IsSet(Compression) = false after Set, want true")
	}
}

func TestOptionArgumentsSetIfUnset(t *testing.T) {
	o := sshconfig.OptionArguments{"Port": 22}
	o.SetIfUnset("Port", 2222)
	if o["Port"] != 22 {
		t.Errorf("SetIfUnset overwrote existing value; got %v, want %v", o["Port"], 22)
	}
	o.SetIfUnset("User", "root")
	if o["User"] != "root" {
		t.Errorf("SetIfUnset(User) = %v, want %q", o["User"], "root")
	}
}

func TestOptionArgumentsToArgs(t *testing.T) {
	tests := []struct {
		name      string
		opts      sshconfig.OptionArguments
		wantPairs map[string]string
	}{
		{
			name:      "bool true renders yes",
			opts:      sshconfig.OptionArguments{"Compression": true},
			wantPairs: map[string]string{"Compression": "yes"},
		},
		{
			name:      "bool false renders no",
			opts:      sshconfig.OptionArguments{"Compression": false},
			wantPairs: map[string]string{"Compression": "no"},
		},
		{
			name:      "string value",
			opts:      sshconfig.OptionArguments{"User": "alice"},
			wantPairs: map[string]string{"User": "alice"},
		},
		{
			name:      "integer value",
			opts:      sshconfig.OptionArguments{"Port": 2222},
			wantPairs: map[string]string{"Port": "2222"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.opts.ToArgs()
			if len(args)%2 != 0 {
				t.Fatalf("ToArgs() returned odd-length slice: %v", args)
			}
			got := map[string]string{}
			for i := 0; i < len(args); i += 2 {
				if args[i] != "-o" {
					t.Errorf("ToArgs()[%d] = %q, want %q", i, args[i], "-o")
				}
				key, val, ok := strings.Cut(args[i+1], "=")
				if !ok {
					t.Errorf("ToArgs()[%d] = %q, want Key=Value form", i+1, args[i+1])
					continue
				}
				got[key] = val
			}
			for k, wantVal := range tt.wantPairs {
				if got[k] != wantVal {
					t.Errorf("ToArgs() option %q = %q, want %q", k, got[k], wantVal)
				}
			}
		})
	}
}

func TestOptionArgumentsApplyTo(t *testing.T) {
	t.Run("valid options are applied", func(t *testing.T) {
		cfg := &sshconfig.Config{}
		setter, err := sshconfig.NewSetter(cfg)
		if err != nil {
			t.Fatalf("NewSetter() error: %v", err)
		}
		opts := sshconfig.OptionArguments{
			"User":        "alice",
			"Compression": true,
		}
		if err := opts.ApplyTo(setter); err != nil {
			t.Fatalf("ApplyTo() error: %v", err)
		}
		if cfg.User != "alice" {
			t.Errorf("cfg.User = %q, want %q", cfg.User, "alice")
		}
		if !cfg.Compression.IsTrue() {
			t.Errorf("cfg.Compression = %q, want yes", cfg.Compression)
		}
	})

	t.Run("unknown key with ErrorOnUnknownFields returns error", func(t *testing.T) {
		cfg := &sshconfig.Config{}
		setter, err := sshconfig.NewSetter(cfg)
		if err != nil {
			t.Fatalf("NewSetter() error: %v", err)
		}
		setter.ErrorOnUnknownFields = true
		opts := sshconfig.OptionArguments{"NoSuchOption": "value"}
		if err := opts.ApplyTo(setter); err == nil {
			t.Error("ApplyTo() with unknown key: got nil error, want error")
		}
	})
}
