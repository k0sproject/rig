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
	o.Set("Compression", nil)
	if o.IsSet("Compression") {
		t.Error("IsSet(Compression) = true after Set(nil), want false")
	}
}

func TestOptionArgumentsNilValueIsSentinel(t *testing.T) {
	// A map constructed with an explicit nil value (e.g. from YAML "key: null")
	// must be treated as set so that SetIfUnset does not override it.
	o := sshconfig.OptionArguments{"BatchMode": nil}
	if !o.IsSet("BatchMode") {
		t.Error("IsSet(BatchMode) = false for explicit nil value, want true")
	}
	o.SetIfUnset("BatchMode", true)
	if o["BatchMode"] != nil {
		t.Errorf("SetIfUnset overwrote nil sentinel; got %v, want nil", o["BatchMode"])
	}
}

func TestOptionArgumentsSetNilDeletes(t *testing.T) {
	o := sshconfig.OptionArguments{"Port": 22, "User": "alice"}
	o.Set("Port", nil)
	if o.IsSet("Port") {
		t.Error("IsSet(Port) = true after Set(nil), want false")
	}
	args := o.ToArgs()
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "-o" && strings.HasPrefix(args[i+1], "Port=") {
			t.Errorf("ToArgs() emitted deleted key Port: %v", args[i+1])
		}
	}
	// SetIfUnset can now fill in a deleted key
	o.SetIfUnset("Port", 2222)
	if o["Port"] != 2222 {
		t.Errorf("SetIfUnset after nil-delete: got %v, want 2222", o["Port"])
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
		{
			name:      "string slice renders space-separated",
			opts:      sshconfig.OptionArguments{"IdentityFile": []string{"/a/id_rsa", "/b/id_ecdsa"}},
			wantPairs: map[string]string{"IdentityFile": "/a/id_rsa /b/id_ecdsa"},
		},
		{
			name:      "any slice renders space-separated",
			opts:      sshconfig.OptionArguments{"SendEnv": []any{"LC_ALL", "LC_LANG"}},
			wantPairs: map[string]string{"SendEnv": "LC_ALL LC_LANG"},
		},
		{
			name:      "CSV string slice renders comma-separated",
			opts:      sshconfig.OptionArguments{"Ciphers": []string{"aes128-ctr", "aes256-ctr"}},
			wantPairs: map[string]string{"Ciphers": "aes128-ctr,aes256-ctr"},
		},
		{
			name:      "CSV any slice renders comma-separated",
			opts:      sshconfig.OptionArguments{"KexAlgorithms": []any{"curve25519-sha256", "diffie-hellman-group14-sha256"}},
			wantPairs: map[string]string{"KexAlgorithms": "curve25519-sha256,diffie-hellman-group14-sha256"},
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

	t.Run("nil setter returns nil error", func(t *testing.T) {
		opts := sshconfig.OptionArguments{"User": "alice"}
		if err := opts.ApplyTo(nil); err != nil {
			t.Errorf("ApplyTo(nil) error: %v, want nil", err)
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

	t.Run("slice value expands into multiple setter args", func(t *testing.T) {
		cfg := &sshconfig.Config{}
		setter, err := sshconfig.NewSetter(cfg)
		if err != nil {
			t.Fatalf("NewSetter() error: %v", err)
		}
		opts := sshconfig.OptionArguments{
			"SendEnv": []string{"LC_ALL", "LC_LANG"},
		}
		if err := opts.ApplyTo(setter); err != nil {
			t.Fatalf("ApplyTo() error: %v", err)
		}
		if len(cfg.SendEnv) < 2 {
			t.Fatalf("cfg.SendEnv = %v, want at least 2 entries", cfg.SendEnv)
		}
		if cfg.SendEnv[0] != "LC_ALL" || cfg.SendEnv[1] != "LC_LANG" {
			t.Errorf("cfg.SendEnv = %v, want [LC_ALL LC_LANG]", cfg.SendEnv)
		}
	})

	t.Run("any slice value expands into multiple setter args", func(t *testing.T) {
		cfg := &sshconfig.Config{}
		setter, err := sshconfig.NewSetter(cfg)
		if err != nil {
			t.Fatalf("NewSetter() error: %v", err)
		}
		opts := sshconfig.OptionArguments{
			"SendEnv": []any{"LC_ALL", "LC_LANG"},
		}
		if err := opts.ApplyTo(setter); err != nil {
			t.Fatalf("ApplyTo() error: %v", err)
		}
		if len(cfg.SendEnv) < 2 {
			t.Fatalf("cfg.SendEnv = %v, want at least 2 entries", cfg.SendEnv)
		}
		if cfg.SendEnv[0] != "LC_ALL" || cfg.SendEnv[1] != "LC_LANG" {
			t.Errorf("cfg.SendEnv = %v, want [LC_ALL LC_LANG]", cfg.SendEnv)
		}
	})

	t.Run("CSV string slice is accepted for comma-separated directives", func(t *testing.T) {
		cfg := &sshconfig.Config{}
		setter, err := sshconfig.NewSetter(cfg)
		if err != nil {
			t.Fatalf("NewSetter() error: %v", err)
		}
		opts := sshconfig.OptionArguments{
			"Ciphers": []string{"aes128-ctr", "aes256-ctr"},
		}
		if err := opts.ApplyTo(setter); err != nil {
			t.Fatalf("ApplyTo() error: %v", err)
		}
		if len(cfg.Ciphers) < 2 {
			t.Fatalf("cfg.Ciphers = %v, want at least 2 entries", cfg.Ciphers)
		}
		if cfg.Ciphers[0] != "aes128-ctr" || cfg.Ciphers[1] != "aes256-ctr" {
			t.Errorf("cfg.Ciphers = %v, want [aes128-ctr aes256-ctr]", cfg.Ciphers)
		}
	})

	t.Run("CSV any slice is accepted for comma-separated directives", func(t *testing.T) {
		cfg := &sshconfig.Config{}
		setter, err := sshconfig.NewSetter(cfg)
		if err != nil {
			t.Fatalf("NewSetter() error: %v", err)
		}
		opts := sshconfig.OptionArguments{
			"KexAlgorithms": []any{"curve25519-sha256", "diffie-hellman-group14-sha256"},
		}
		if err := opts.ApplyTo(setter); err != nil {
			t.Fatalf("ApplyTo() error: %v", err)
		}
		if len(cfg.KexAlgorithms) < 2 {
			t.Fatalf("cfg.KexAlgorithms = %v, want at least 2 entries", cfg.KexAlgorithms)
		}
		if cfg.KexAlgorithms[0] != "curve25519-sha256" || cfg.KexAlgorithms[1] != "diffie-hellman-group14-sha256" {
			t.Errorf("cfg.KexAlgorithms = %v, want [curve25519-sha256 diffie-hellman-group14-sha256]", cfg.KexAlgorithms)
		}
	})
}
