package sshconfig_test

import (
	"fmt"
	"log"
	"reflect"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/stretchr/testify/require"
)

func ExampleNewSetter() {
	type MyConfig struct {
		Host string
		Port int
	}
	obj := &MyConfig{}
	setter, err := sshconfig.NewSetter(obj)
	if err != nil {
		log.Fatal(err)
	}
	err = setter.Set("Host", "example.com")
	if err != nil {
		log.Fatal(err)
	}
	_ = setter.Set("Port", "2022")
	// On most of the config keys the first value to be set will remain in effect.
	_ = setter.Set("Port", "22")
	fmt.Println("Host:", obj.Host)
	fmt.Println("Port:", obj.Port)
	// Output:
	// Host: example.com
	// Port: 2022
}

func ExampleSetter_Set() {
	type MyConfig struct {
		Port int
	}
	obj := &MyConfig{}
	setter, err := sshconfig.NewSetter(obj)
	if err != nil {
		log.Fatal(err)
	}
	_ = setter.Set("Port", "2022")
	fmt.Println("Port:", obj.Port)
	// Output:
	// Port: 2022
}

func TestSetter(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	require.NoError(t, setter.Set("Host", "test"))
	require.NoError(t, setter.Set("Port", "22"))
	require.NoError(t, setter.Set("User", "root"))
	require.Equal(t, "test", obj.Host)
	require.Equal(t, 22, obj.Port)
	require.Equal(t, "root", obj.User)
}

func TestSetterBasicPrecedence(t *testing.T) {
	obj := sshconfig.Config{
		User:               "foo",
		UserKnownHostsFile: []string{},
	}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	require.NoError(t, setter.Set("User", "bar"))
	require.Equal(t, "foo", obj.User, "a set value should not override an existing value")
	require.NoError(t, setter.Set("UserKnownHostsFile", "/test", "/test2"))
	require.Equal(t, []string{"/test", "/test2"}, obj.UserKnownHostsFile, "an empty slice should be replaced by the new value")
	require.NoError(t, setter.Set("UserKnownHostsFile", "/test3"))
	require.Equal(t, []string{"/test", "/test2"}, obj.UserKnownHostsFile, "a populated list should not be appended")
}

func TestSetterBooleanOptions(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	require.NoError(t, setter.Set("BatchMode", "yes"))
	require.True(t, obj.BatchMode.IsTrue())
	require.False(t, obj.BatchMode.IsFalse())
	require.NoError(t, setter.Set("BatchMode", "no"))
	require.True(t, obj.BatchMode.IsTrue(), "a set boolean should not change value")
}

func TestSetterInvalidBoolean(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	require.Error(t, setter.Set("BatchMode", "maybe"))
}

func TestSetterFieldHost(t *testing.T) {
	t.Run("preset value", func(t *testing.T) {
		obj := sshconfig.Config{Host: "test"}
		setter, err := sshconfig.NewSetter(&obj)
		require.NoError(t, err)
		require.Equal(t, "test", setter.OriginalHost)
		require.NoError(t, setter.Set("Host", "test2"))
		require.Equal(t, "test2", obj.Host)
		require.Equal(t, "test", setter.OriginalHost)
		require.True(t, setter.HostChanged(), "changing existing value should be considered a change")
	})

	t.Run("valid", func(t *testing.T) {
		obj := sshconfig.Config{}
		setter, err := sshconfig.NewSetter(&obj)
		require.NoError(t, err)
		require.NoError(t, setter.Set("Host", "test"))
		require.Equal(t, "test", obj.Host)
		require.Equal(t, "test", setter.OriginalHost)
		require.False(t, setter.HostChanged(), "first value should not be considered a change")
		require.NoError(t, setter.Set("Host", "test2"))
		require.Equal(t, "test2", obj.Host)
		require.Equal(t, "test", setter.OriginalHost, "originalhost should remain unchanged")
		require.True(t, setter.HostChanged(), "new value should be considered a change")
	})

	t.Run("invalid", func(t *testing.T) {
		obj := sshconfig.Config{}
		setter, err := sshconfig.NewSetter(&obj)
		require.NoError(t, err)
		require.Error(t, setter.Set("Host", ""))
		require.Error(t, setter.Set("Host", "foo", "bar"))
	})
}

func TestSetterFieldMatch(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("invalid", func(t *testing.T) {
		require.Error(t, setter.Set("Match", "foo"), "Match should not be settable")
	})
}

func TestSetterFieldAddKeysToAgent(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		_ = setter.Reset("AddKeysToAgent")
		require.NoError(t, setter.Set("AddKeysToAgent", "yes"))
		require.True(t, obj.AddKeysToAgent.IsTrue())
		require.False(t, obj.AddKeysToAgent.HasInterval())

		_ = setter.Reset("AddKeysToAgent")
		require.NoError(t, setter.Set("AddKeysToAgent", "confirm", "80"))
		require.False(t, obj.AddKeysToAgent.IsTrue())
		require.True(t, obj.AddKeysToAgent.HasInterval())
		iv, err := obj.AddKeysToAgent.Interval()
		require.NoError(t, err)
		require.Equal(t, 80*time.Second, iv)

		_ = setter.Reset("AddKeysToAgent")
		require.NoError(t, setter.Set("AddKeysToAgent", "80"))
		require.False(t, obj.AddKeysToAgent.IsTrue())
		require.True(t, obj.AddKeysToAgent.HasInterval())
		iv, err = obj.AddKeysToAgent.Interval()
		require.NoError(t, err)
		require.Equal(t, 80*time.Second, iv)
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("AddKeysToAgent"))
		require.Error(t, setter.Set("AddKeysToAgent", "maybe"))
		require.Error(t, setter.Set("AddKeysToAgent", "confirm", "sometime"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("AddKeysToAgent"))
		require.NoError(t, setter.Set("AddKeysToAgent", "confirm", "80"))
		require.False(t, obj.AddKeysToAgent.IsTrue())
		require.True(t, obj.AddKeysToAgent.HasInterval())

		require.NoError(t, setter.Set("AddKeysToAgent", "yes"))
		require.Equal(t, "confirm 80", obj.AddKeysToAgent.String(), "original value should stick")
	})
}

func TestSetterBooleanOptionFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	basicBools := []string{
		"AppleMultiPath",
		"UseKeychain",
		"BatchMode",
		"CanonicalizeFallbackLocal",
		"CheckHostIP",
		"ClearAllForwardings",
		"Compression",
		"EnableEscapeCommandline",
		"EnableSSHKeysign",
		"ExitOnForwardFailure",
		"ForkAfterAuthentication",
		"ForwardX11",
		"ForwardX11Trusted",
		"GatewayPorts",
		"GSSAPIAuthentication",
		"GSSAPIDelegateCredentials",
		"HashKnownHosts",
		"HostbasedAuthentication",
		"IdentitiesOnly",
		"KbdInteractiveAuthentication",
		"NoHostAuthenticationForLocalhost",
		"PasswordAuthentication",
		"PermitLocalCommand",
		"ProxyUseFdpass",
		"StdinNull",
		"StreamLocalBindUnlink",
		"TCPKeepAlive",
		"VisualHostKey",
	}
	type boolOpt interface {
		IsTrue() bool
		IsFalse() bool
	}
	for _, field := range basicBools {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "no"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				var value boolOpt
				var ok bool
				if f.Kind() == reflect.Ptr && !f.IsNil() {
					value, ok = f.Elem().Interface().(boolOpt)
				} else if f.CanAddr() {
					// Use Addr() only if the field is addressable
					value, ok = f.Addr().Interface().(boolOpt)
				}
				require.True(t, ok)
				require.False(t, value.IsTrue())
				require.True(t, value.IsFalse())

				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "yes"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				value, ok = f.Addr().Interface().(boolOpt)
				require.True(t, ok)
				require.True(t, value.IsTrue())
				require.False(t, value.IsFalse())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, "maybe"))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "no"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				value, ok := f.Addr().Interface().(boolOpt)
				require.True(t, ok)
				require.True(t, value.IsFalse())

				require.NoError(t, setter.Set(field, "yes"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				value, ok = f.Addr().Interface().(boolOpt)
				require.True(t, ok)
				require.True(t, value.IsFalse(), "original value should stick")
			})
		})
	}
}

func TestSetterRegularStringFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	basicStrings := []string{
		"User",
		"Hostname",
		"BindAddress",
		"BindInterface",
		"HostKeyAlias",
		"Tag",
		"TunnelDevice",
		"KnownHostsCommand",
		"LocalCommand",
		"ProxyCommand",
		"RemoteCommand",
		"PKCS11Provider",
	}
	for _, field := range basicStrings {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, "value", f.String())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, "value", f.String())

				require.NoError(t, setter.Set(field, "value2"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, "value", f.String(), "original value should stick")
			})
		})
	}
}

func TestSetterRegularStringSliceFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	stringSlices := []string{
		"CanonicalDomains",
		"GlobalKnownHostsFile",
		"IgnoreUnknown",
		"LogVerbose",
	}
	for _, field := range stringSlices {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len())
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value", "another"))
				require.Equal(t, 2, f.Len())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value", "value2"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 2, f.Len())
				require.NoError(t, setter.Set(field, "value3"))
				require.Equal(t, 2, f.Len(), "original values should stick")
				require.Equal(t, "value", f.Index(0).String(), "original values should stick")
				require.Equal(t, "value2", f.Index(1).String(), "original values should stick")
			})
		})
	}
}

func TestSetterPathFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	pathFields := []string{
		"ControlPath",
		"RevokedHostKeys",
		"SecurityKeyProvider",
		"XAuthLocation",
	}
	// regular path fields should be treated as regular strings.
	// tilde expansion should be done elsewhere.
	// the only directive with special relative path handling is "Include",
	// you can put ./foo for IdentityFile in ~/.ssh/config and the resulting
	// path will be ./foo, not /home/user/.ssh/./foo.
	for _, field := range pathFields {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, "value", f.String())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, "value", f.String())

				require.NoError(t, setter.Set(field, "value2"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, "value", f.String(), "original value should stick")
			})
		})
	}
}

func TestSetterAlgoFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	algoFields := []string{
		"CASignatureAlgorithms",
		"Ciphers",
		"HostbasedAcceptedAlgorithms",
		"HostKeyAlgorithms",
		"KexAlgorithms",
		"MACs",
		"PubkeyAcceptedAlgorithms",
	}
	// the precedence in these fields is standard.
	// the modifiers only work when the field is empty.
	// the modifiers work ONCE against the default values.
	// for example, something like -aes128 will apply the defaults minus aes128
	// and after that ^aes256 or +aes128 won't do anything.

	for _, field := range algoFields {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len())
				require.Equal(t, "value", f.Index(0).String())
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value,another"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 2, f.Len())
				require.Equal(t, "value", f.Index(0).String())
				require.Equal(t, "another", f.Index(1).String())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
				require.Error(t, setter.Set(field, "hello", "world"))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len())
				require.Equal(t, "value", f.Index(0).String())
				require.NoError(t, setter.Set(field, "value,another"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len(), "original value should stick")
				require.Equal(t, "value", f.Index(0).String(), "original value should stick")
			})
			t.Run("modifiers", func(t *testing.T) {
				t.Run("^", func(t *testing.T) {
					require.NoError(t, setter.Reset(field))
					require.NoError(t, setter.Set(field, "^value,another"))
					f := reflect.ValueOf(&obj).Elem().FieldByName(field)
					require.True(t, f.Len() > 2)
					require.Equal(t, "value", f.Index(0).String())
					require.Equal(t, "another", f.Index(1).String())
				})
				t.Run("+", func(t *testing.T) {
					require.NoError(t, setter.Reset(field))
					require.NoError(t, setter.Set(field, "+value,another"))
					f := reflect.ValueOf(&obj).Elem().FieldByName(field)
					require.True(t, f.Len() > 2)
					require.Equal(t, "value", f.Index(f.Len()-2).String())
					require.Equal(t, "another", f.Index(f.Len()-1).String())
				})
				t.Run("-", func(t *testing.T) {
					// need to run -dummy first to figure out the defaults
					require.NoError(t, setter.Reset(field))
					require.NoError(t, setter.Set(field, "-dummy"))
					f := reflect.ValueOf(&obj).Elem().FieldByName(field)
					values, ok := f.Interface().([]string)
					require.True(t, ok)
					require.NotContains(t, values, "dummy")
					origLen := len(values)
					if origLen > 3 {
						// the ssh version on github runners has empty CASignatureAlgorithms
						require.True(t, origLen > 3)
						// now we can test the actual modifier
						require.NoError(t, setter.Reset(field))
						remove := values[2]
						require.NoError(t, setter.Set(field, "-"+remove))
						f = reflect.ValueOf(&obj).Elem().FieldByName(field)
						values, ok = f.Interface().([]string)
						require.True(t, ok)
						require.NotContains(t, values, remove)
						require.Equal(t, origLen-1, len(values))
						// the op should only happen once
						remove = values[2]
						require.NoError(t, setter.Set(field, "-"+remove))
						f = reflect.ValueOf(&obj).Elem().FieldByName(field)
						values, ok = f.Interface().([]string)
						require.True(t, ok)
						require.Contains(t, values, remove, "removal should only happen once")
					}
				})
			})
		})
	}
}

func TestSetterFieldAddressFamily(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"any", "inet", "inet6"} {
			require.NoError(t, setter.Reset("AddressFamily"))
			require.NoError(t, setter.Set("AddressFamily", val))
			require.Equal(t, val, obj.AddressFamily)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("AddressFamily"))
		require.Error(t, setter.Set("AddressFamily", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("AddressFamily"))
		require.NoError(t, setter.Set("AddressFamily", "inet"))
		require.Equal(t, "inet", obj.AddressFamily)
		require.NoError(t, setter.Set("AddressFamily", "any"))
		require.Equal(t, "inet", obj.AddressFamily, "original value should stick")
	})
}

func TestSetterFieldCanonicalizeHostname(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("CanonicalizeHostname"))
		require.NoError(t, setter.Set("CanonicalizeHostname", "yes"))
		require.False(t, obj.CanonicalizeHostname.IsAlways())
		require.True(t, obj.CanonicalizeHostname.IsTrue())
		require.NoError(t, setter.Reset("CanonicalizeHostname"))
		require.NoError(t, setter.Set("CanonicalizeHostname", "no"))
		require.True(t, obj.CanonicalizeHostname.IsFalse())
		require.False(t, obj.CanonicalizeHostname.IsTrue())
		require.NoError(t, setter.Reset("CanonicalizeHostname"))
		require.NoError(t, setter.Set("CanonicalizeHostname", "always"))
		require.True(t, obj.CanonicalizeHostname.IsTrue())
		require.True(t, obj.CanonicalizeHostname.IsAlways())
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("CanonicalizeHostname"))
		require.Error(t, setter.Set("CanonicalizeHostname", "maybe"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("CanonicalizeHostname"))
		require.NoError(t, setter.Set("CanonicalizeHostname", "yes"))
		require.NoError(t, setter.Set("CanonicalizeHostname", "no"))
		require.Equal(t, "yes", obj.CanonicalizeHostname.String(), "original value should stick")
	})
}

func TestSetterIntFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)

	intFields := []string{
		"CanonicalizeMaxDots",
		"ConnectionAttempts",
		"NumberOfPasswordPrompts",
		"RequiredRSASize",
		"ServerAliveCountMax",
	}
	for _, field := range intFields {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "5"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				if f.Kind() == reflect.Ptr {
					require.Equal(t, 5, int(f.Elem().Int()))
				} else {
					require.Equal(t, 5, int(f.Int()))
				}
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
				require.Error(t, setter.Set(field, "1", "2"))
				require.Error(t, setter.Set(field, "hello"))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "5"))
				require.NoError(t, setter.Set(field, "1"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				if f.Kind() == reflect.Ptr {
					require.Equal(t, 5, int(f.Elem().Int()), "original value should stick")
				} else {
					require.Equal(t, 5, int(f.Int()), "original value should stick")
				}
			})
		})
	}
}

func TestSetterFieldCanonicalizePermittedCNAMEs(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("CanonicalizePermittedCNAMEs"))
		require.NoError(t, setter.Set("CanonicalizePermittedCNAMEs", "key:value", "key2:value2"))
		require.Equal(t, "key:value", obj.CanonicalizePermittedCNAMEs[0])
		require.Equal(t, "key2:value2", obj.CanonicalizePermittedCNAMEs[1])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("CanonicalizePermittedCNAMEs"))
		require.Error(t, setter.Set("CanonicalizePermittedCNAMEs", "key"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("CanonicalizePermittedCNAMEs"))
		require.NoError(t, setter.Set("CanonicalizePermittedCNAMEs", "key:value", "key2:value2"))
		require.Equal(t, 2, len(obj.CanonicalizePermittedCNAMEs))
		require.NoError(t, setter.Set("CanonicalizePermittedCNAMEs", "key3:value3"))
		require.Equal(t, 2, len(obj.CanonicalizePermittedCNAMEs), "original values should stick")
	})
}

func TestSetterFieldCertificateFile(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("CertificateFile"))
		require.NoError(t, setter.Set("CertificateFile", "value"))
		require.Len(t, obj.CertificateFile, 1)
		require.Equal(t, "value", obj.CertificateFile[0])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("CertificateFile"))
		require.Error(t, setter.Set("CertificateFile", ""))
		require.Error(t, setter.Set("CertificateFile", "none", "hello"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("CertificateFile"))
		require.NoError(t, setter.Set("CertificateFile", "value"))
		require.Len(t, obj.CertificateFile, 1)
		require.Equal(t, "value", obj.CertificateFile[0])
		require.NoError(t, setter.Set("CertificateFile", "value2", "value3"))
		require.Len(t, obj.CertificateFile, 3, "certificatefile should accumulate")
		require.NoError(t, setter.Set("CertificateFile", "none"))
		require.Len(t, obj.CertificateFile, 1, "none should override the whole list")
		require.Equal(t, "none", obj.CertificateFile[0])
		require.NoError(t, setter.Set("CertificateFile", "value2", "value3"))
		require.Len(t, obj.CertificateFile, 1, "none should override the whole list forever")
		require.Equal(t, "none", obj.CertificateFile[0])
	})
}

func TestSetterFieldChannelTimeout(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ChannelTimeout"))
		require.NoError(t, setter.Set("ChannelTimeout", "agent-connection=1m", "direct-tcpip=60"))
		require.Len(t, obj.ChannelTimeout, 2)
		require.Equal(t, 1*time.Minute, obj.ChannelTimeout["agent-connection"])
		require.Equal(t, 1*time.Minute, obj.ChannelTimeout["direct-tcpip"])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ChannelTimeout"))
		require.Error(t, setter.Set("ChannelTimeout", "foo"))
		require.Error(t, setter.Set("ChannelTimeout", "foo=bar"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("ChannelTimeout"))
		require.NoError(t, setter.Set("ChannelTimeout", "agent-connection=1m", "direct-tcpip=60"))
		require.NoError(t, setter.Set("ChannelTimeout", "agent-connection=2m", "tun-connection=40"))
		require.Len(t, obj.ChannelTimeout, 2, "original values should stick")
		require.Equal(t, 1*time.Minute, obj.ChannelTimeout["agent-connection"], "original values should stick")
		require.Equal(t, 1*time.Minute, obj.ChannelTimeout["direct-tcpip"], "original values should stick")
	})
}

func TestSetterDurationFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)

	durationFields := []string{
		"ConnectTimeout",
		"ForwardX11Timeout",
		"ServerAliveInterval",
	}

	for _, field := range durationFields {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "5"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				val, ok := f.Interface().(time.Duration)
				require.True(t, ok)
				require.Equal(t, 5*time.Second, val)

				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "3m"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				val, ok = f.Interface().(time.Duration)
				require.True(t, ok)
				require.Equal(t, 180*time.Second, val)

				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "none"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.True(t, ok)
				require.True(t, f.IsZero() || f.IsNil())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
				require.Error(t, setter.Set(field, "abc"))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "5"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				val, ok := f.Interface().(time.Duration)
				require.True(t, ok)
				require.Equal(t, 5*time.Second, val)

				require.NoError(t, setter.Set(field, "3m"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				val, ok = f.Interface().(time.Duration)
				require.True(t, ok)
				require.Equal(t, 5*time.Second, val, "original value should stick")
			})
		})
	}
}

func TestSetterFieldControlMaster(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ControlMaster"))
		require.NoError(t, setter.Set("ControlMaster", "yes"))
		require.True(t, obj.ControlMaster.IsTrue())
		require.NoError(t, setter.Reset("ControlMaster"))
		require.NoError(t, setter.Set("ControlMaster", "no"))
		require.True(t, obj.ControlMaster.IsFalse())
		require.NoError(t, setter.Reset("ControlMaster"))
		require.NoError(t, setter.Set("ControlMaster", "auto"))
		require.True(t, obj.ControlMaster.IsAuto())
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ControlMaster"))
		require.Error(t, setter.Set("ControlMaster", "hmm"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("ControlMaster"))
		require.NoError(t, setter.Set("ControlMaster", "auto"))
		require.True(t, obj.ControlMaster.IsAuto())
		require.NoError(t, setter.Set("ControlMaster", "no"))
		require.True(t, obj.ControlMaster.IsAuto(), "original value should stick")
	})
}

func TestSetterFieldControlPersist(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ControlPersist"))
		require.NoError(t, setter.Set("ControlPersist", "yes"))
		require.False(t, obj.ControlPersist.HasInterval())
		require.True(t, obj.ControlPersist.IsTrue())
		_, err := obj.ControlPersist.Interval()
		require.Error(t, err)
		require.NoError(t, setter.Reset("ControlPersist"))
		require.NoError(t, setter.Set("ControlPersist", "no"))
		require.True(t, obj.ControlPersist.IsFalse())
		require.NoError(t, setter.Reset("ControlPersist"))
		require.NoError(t, setter.Set("ControlPersist", "5m"))
		require.True(t, obj.ControlPersist.HasInterval())
		iv, err := obj.ControlPersist.Interval()
		require.NoError(t, err)
		require.Equal(t, 5*time.Minute, iv)
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ControlPersist"))
		require.Error(t, setter.Set("ControlPersist", "maybe"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("ControlPersist"))
		require.NoError(t, setter.Set("ControlPersist", "yes"))
		require.NoError(t, setter.Set("ControlPersist", "no"))
		require.True(t, obj.ControlPersist.IsTrue(), "original value should stick")
	})
}

func TestSetterFieldDynamicForward(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("DynamicForward"))
		require.NoError(t, setter.Set("DynamicForward", "value"))
		require.Len(t, obj.DynamicForward, 1)
		require.Equal(t, "value", obj.DynamicForward[0])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("DynamicForward"))
		require.Error(t, setter.Set("DynamicForward", ""))
		require.Error(t, setter.Set("DynamicForward", "none", "hello"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("DynamicForward"))
		require.NoError(t, setter.Set("DynamicForward", "value"))
		require.Len(t, obj.DynamicForward, 1)
		require.Equal(t, "value", obj.DynamicForward[0])
		require.NoError(t, setter.Set("DynamicForward", "value2", "value3"))
		require.Len(t, obj.DynamicForward, 3, "dynamicforward should accumulate")
		require.NoError(t, setter.Set("DynamicForward", "none"))
		require.Len(t, obj.DynamicForward, 1, "none should override the whole list")
		require.Equal(t, "none", obj.DynamicForward[0])
		require.NoError(t, setter.Set("DynamicForward", "value2", "value3"))
		require.Len(t, obj.DynamicForward, 1, "none should override the whole list forever")
		require.Equal(t, "none", obj.DynamicForward[0])
	})
}

func TestSetterFieldFingerprintHash(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"MD5", "SHA256"} {
			require.NoError(t, setter.Reset("FingerprintHash"))
			require.NoError(t, setter.Set("FingerprintHash", val))
			require.Equal(t, val, obj.FingerprintHash.String())
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("FingerprintHash"))
		require.Error(t, setter.Set("FingerprintHash", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("FingerprintHash"))
		require.NoError(t, setter.Set("FingerprintHash", "MD5"))
		require.NoError(t, setter.Set("FingerprintHash", "SHA256"))
		require.Equal(t, "MD5", obj.FingerprintHash.String(), "original value should stick")
	})
}

func TestSetterFieldForwardAgent(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ForwardAgent"))
		require.NoError(t, setter.Set("ForwardAgent", "yes"))
		require.True(t, obj.ForwardAgent.IsTrue())
		require.NoError(t, setter.Reset("ForwardAgent"))
		require.NoError(t, setter.Set("ForwardAgent", "no"))
		require.True(t, obj.ForwardAgent.IsFalse())

		t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")
		require.NoError(t, setter.Reset("ForwardAgent"))
		require.NoError(t, setter.Set("ForwardAgent", "$SSH_AUTH_SOCK"))
		require.True(t, obj.ForwardAgent.IsSocket())

		require.Equal(t, "/tmp/ssh-agent", obj.ForwardAgent.Socket())
		require.NoError(t, setter.Reset("ForwardAgent"))
		require.NoError(t, setter.Set("ForwardAgent", "${SSH_AUTH_SOCK}"))
		require.True(t, obj.ForwardAgent.IsSocket())
		require.Equal(t, "/tmp/ssh-agent", obj.ForwardAgent.Socket())
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ForwardAgent"))
		require.Error(t, setter.Set("ForwardAgent"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("ForwardAgent"))
		require.NoError(t, setter.Set("ForwardAgent", "no"))
		require.True(t, obj.ForwardAgent.IsFalse())

		t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")
		require.NoError(t, setter.Set("ForwardAgent", "$SSH_AUTH_SOCK"))
		require.False(t, obj.ForwardAgent.IsSocket(), "original value should stick")
		require.True(t, obj.ForwardAgent.IsFalse(), "original value should stick")
	})
}

func TestSetterFieldGlobalKnownHostsFile(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("GlobalKnownHostsFile"))
		require.NoError(t, setter.Set("GlobalKnownHostsFile", "value", "value2"))
		require.Len(t, obj.GlobalKnownHostsFile, 2)
		require.Equal(t, []string{"value", "value2"}, obj.GlobalKnownHostsFile)
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("GlobalKnownHostsFile"))
		require.Error(t, setter.Set("GlobalKnownHostsFile", ""))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("GlobalKnownHostsFile"))
		require.NoError(t, setter.Set("GlobalKnownHostsFile", "value", "value2"))
		require.Len(t, obj.GlobalKnownHostsFile, 2)
		require.Equal(t, []string{"value", "value2"}, obj.GlobalKnownHostsFile)
		require.NoError(t, setter.Set("GlobalKnownHostsFile", "value3"))
		require.Len(t, obj.GlobalKnownHostsFile, 2, "original values should stick")
		require.Equal(t, []string{"value", "value2"}, obj.GlobalKnownHostsFile, "original values should stick")
	})
}

func TestSetterFieldIdentityAgent(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("IdentityAgent"))
		require.NoError(t, setter.Set("IdentityAgent", "foo"))
		require.Equal(t, "foo", obj.IdentityAgent.String())

		t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")
		require.NoError(t, setter.Reset("IdentityAgent"))
		require.NoError(t, setter.Set("IdentityAgent", "SSH_AUTH_SOCK"))
		require.Equal(t, "/tmp/ssh-agent", obj.IdentityAgent.Socket())

		require.NoError(t, setter.Reset("IdentityAgent"))
		require.NoError(t, setter.Set("IdentityAgent", "${SSH_AUTH_SOCK}"))
		require.Equal(t, "/tmp/ssh-agent", obj.IdentityAgent.Socket())
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("IdentityAgent"))
		require.Error(t, setter.Set("IdentityAgent", ""))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("IdentityAgent"))
		require.NoError(t, setter.Set("IdentityAgent", "foo"))
		require.NoError(t, setter.Set("IdentityAgent", "bar"))
		require.Equal(t, "foo", obj.IdentityAgent.String(), "original value should stick")
	})
}

func TestSetterFieldIdentityFile(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("IdentityFile"))
		require.NoError(t, setter.Set("IdentityFile", "value"))
		require.Len(t, obj.IdentityFile, 1)
		require.Equal(t, "value", obj.IdentityFile[0])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("IdentityFile"))
		require.Error(t, setter.Set("IdentityFile", ""))
		require.Error(t, setter.Set("IdentityFile", "none", "hello"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("IdentityFile"))
		require.NoError(t, setter.Set("IdentityFile", "value"))
		require.Len(t, obj.IdentityFile, 1)
		require.Equal(t, "value", obj.IdentityFile[0])
		require.NoError(t, setter.Set("IdentityFile", "value2", "value3"))
		require.Len(t, obj.IdentityFile, 3, "identityfile should accumulate")
		require.NoError(t, setter.Set("IdentityFile", "none"))
		require.Len(t, obj.IdentityFile, 1, "none should override the whole list")
		require.Equal(t, "none", obj.IdentityFile[0])
		require.NoError(t, setter.Set("IdentityFile", "value2", "value3"))
		require.Len(t, obj.IdentityFile, 1, "none should override the whole list forever")
		require.Equal(t, "none", obj.IdentityFile[0])
	})
}

func TestSetterFieldIPQoS(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"af11", "af12", "af13", "af21", "af22", "af23", "af31", "af32", "af33", "af41", "af42", "af43", "cs0", "cs1", "cs2", "cs3", "cs4", "cs5", "cs6", "cs7", "ef", "lowdelay", "throughput", "reliability"} {
			require.NoError(t, setter.Reset("IPQoS"))
			require.NoError(t, setter.Set("IPQoS", val))
		}
		require.NoError(t, setter.Reset("IPQoS"))
		require.NoError(t, setter.Set("IPQoS", "af11"))
		require.Equal(t, "af11", obj.IPQoS[0])

		require.NoError(t, setter.Reset("IPQoS"))
		require.NoError(t, setter.Set("IPQoS", "af11", "lowdelay"))
		require.Equal(t, "af11", obj.IPQoS[0])
		require.Equal(t, "lowdelay", obj.IPQoS[1])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("IPQoS"))
		require.Error(t, setter.Set("IPQoS", "af11", "lowdelay", "reliability"))
		require.Error(t, setter.Set("IPQoS", "hello"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("IPQoS"))
		require.NoError(t, setter.Set("IPQoS", "af11"))
		require.Equal(t, "af11", obj.IPQoS[0])

		require.NoError(t, setter.Set("IPQoS", "af11", "lowdelay"))
		require.Equal(t, "af11", obj.IPQoS[0])
		require.Len(t, obj.IPQoS, 1, "original values should stick")
	})
}

func TestSetterCSVFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)

	csvFields := []string{
		"KbdInteractiveDevices",
		"PreferredAuthentications",
		"ProxyJump",
	}
	for _, field := range csvFields {
		t.Run(field, func(t *testing.T) {
			t.Run("valid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len())
				require.Equal(t, "value", f.Index(0).String())
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value,another"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 2, f.Len())
				require.Equal(t, "value", f.Index(0).String())
				require.Equal(t, "another", f.Index(1).String())
			})
			t.Run("invalid", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.Error(t, setter.Set(field, ""))
				require.Error(t, setter.Set(field, "hello", "world"))
			})
			t.Run("precedence", func(t *testing.T) {
				require.NoError(t, setter.Reset(field))
				require.NoError(t, setter.Set(field, "value"))
				f := reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len())
				require.Equal(t, "value", f.Index(0).String())
				require.NoError(t, setter.Set(field, "value,another"))
				f = reflect.ValueOf(&obj).Elem().FieldByName(field)
				require.Equal(t, 1, f.Len(), "original value should stick")
				require.Equal(t, "value", f.Index(0).String(), "original value should stick")
			})
		})
	}
}

func TestSetterForwardFields(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("LocalForward", func(t *testing.T) {
		t.Run("valid", func(t *testing.T) {
			require.NoError(t, setter.Reset("LocalForward"))
			require.NoError(t, setter.Set("LocalForward", "key", "value"))
			require.Len(t, obj.LocalForward, 1)
		})
		t.Run("invalid", func(t *testing.T) {
			require.NoError(t, setter.Reset("LocalForward"))
			require.Error(t, setter.Set("LocalForward", "key"))
		})
		t.Run("precedence", func(t *testing.T) {
			require.NoError(t, setter.Reset("LocalForward"))
			require.NoError(t, setter.Set("LocalForward", "key", "value"))
			require.Len(t, obj.LocalForward, 1)
			require.Equal(t, "value", obj.LocalForward["key"])
			require.NoError(t, setter.Set("LocalForward", "key2", "value2"))
			require.Len(t, obj.LocalForward, 2, "forwarding fields should accumulate")
			require.Equal(t, "value2", obj.LocalForward["key2"])
		})
	})
	t.Run("RemoteForward", func(t *testing.T) {
		t.Run("valid", func(t *testing.T) {
			require.NoError(t, setter.Reset("RemoteForward"))
			require.NoError(t, setter.Set("RemoteForward", "key", "value"))
			require.Len(t, obj.RemoteForward, 1)
		})
		t.Run("invalid", func(t *testing.T) {
			require.NoError(t, setter.Reset("RemoteForward"))
			require.Error(t, setter.Set("RemoteForward", "key"))
		})
		t.Run("precedence", func(t *testing.T) {
			require.NoError(t, setter.Reset("RemoteForward"))
			require.NoError(t, setter.Set("RemoteForward", "key", "value"))
			require.Len(t, obj.RemoteForward, 1)
			require.Equal(t, "value", obj.RemoteForward["key"])
			require.NoError(t, setter.Set("RemoteForward", "key2", "value2"))
			require.Len(t, obj.RemoteForward, 2, "forwarding fields should accumulate")
			require.Equal(t, "value2", obj.RemoteForward["key2"])
		})
	})
}

func TestSetterFieldLogLevel(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"QUIET", "FATAL", "ERROR", "INFO", "VERBOSE", "DEBUG", "DEBUG1", "DEBUG2", "DEBUG3"} {
			require.NoError(t, setter.Reset("LogLevel"))
			require.NoError(t, setter.Set("LogLevel", val))
			require.Equal(t, val, obj.LogLevel)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("LogLevel"))
		require.Error(t, setter.Set("LogLevel", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("LogLevel"))
		require.NoError(t, setter.Set("LogLevel", "QUIET"))
		require.NoError(t, setter.Set("LogLevel", "DEBUG"))
		require.Equal(t, "QUIET", obj.LogLevel, "original value should stick")
	})
}

func TestSetterFieldObscureKeystrokeTiming(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ObscureKeystrokeTiming"))
		require.NoError(t, setter.Set("ObscureKeystrokeTiming", "yes"))
		require.True(t, obj.ObscureKeystrokeTiming.IsTrue())
		require.NoError(t, setter.Reset("ObscureKeystrokeTiming"))
		require.NoError(t, setter.Set("ObscureKeystrokeTiming", "no"))
		require.True(t, obj.ObscureKeystrokeTiming.IsFalse())
		require.NoError(t, setter.Reset("ObscureKeystrokeTiming"))
		require.NoError(t, setter.Set("ObscureKeystrokeTiming", "interval:30"))
		require.True(t, obj.ObscureKeystrokeTiming.HasInterval())
		iv, err := obj.ObscureKeystrokeTiming.Interval()
		require.NoError(t, err)
		require.Equal(t, 30*time.Millisecond, iv)
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("ObscureKeystrokeTiming"))
		require.Error(t, setter.Set("ObscureKeystrokeTiming", "maybe"))
		require.Error(t, setter.Set("ObscureKeystrokeTiming", "interval:5d"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("ObscureKeystrokeTiming"))
		require.NoError(t, setter.Set("ObscureKeystrokeTiming", "yes"))
		require.NoError(t, setter.Set("ObscureKeystrokeTiming", "no"))
		require.True(t, obj.ObscureKeystrokeTiming.IsTrue(), "original value should stick")
	})
}

func TestSetterFieldPermitRemoteOpen(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("PermitRemoteOpen"))
		require.NoError(t, setter.Set("PermitRemoteOpen", "key:value", "key2:value2"))
		require.Equal(t, "key:value", obj.PermitRemoteOpen[0])
		require.Equal(t, "key2:value2", obj.PermitRemoteOpen[1])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("PermitRemoteOpen"))
		require.Error(t, setter.Set("PermitRemoteOpen", "key"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("PermitRemoteOpen"))
		require.NoError(t, setter.Set("PermitRemoteOpen", "key:value", "key2:value2"))
		require.Equal(t, 2, len(obj.PermitRemoteOpen))
		require.NoError(t, setter.Set("PermitRemoteOpen", "key3:value3"))
		require.Equal(t, 2, len(obj.PermitRemoteOpen), "original values should stick")
	})
}

func TestSetterFieldPort(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("Port"))
		require.NoError(t, setter.Set("Port", "22"))
		require.Equal(t, 22, obj.Port)
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("Port"))
		require.Error(t, setter.Set("Port", "abc"))
		require.Error(t, setter.Set("Port", "99999"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("Port"))
		require.NoError(t, setter.Set("Port", "22"))
		require.Equal(t, 22, obj.Port)
		require.NoError(t, setter.Set("Port", "23"))
		require.Equal(t, 22, obj.Port, "original value should stick")
	})
}

func TestSetterFieldPubkeyAuthentication(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"yes", "no", "unbound", "host-bound"} {
			require.NoError(t, setter.Reset("PubkeyAuthentication"))
			require.NoError(t, setter.Set("PubkeyAuthentication", val))
			require.Equal(t, val, obj.PubkeyAuthentication.String())
		}
		require.NoError(t, setter.Reset("PubkeyAuthentication"))
		require.NoError(t, setter.Set("PubkeyAuthentication", "yes"))
		require.True(t, obj.PubkeyAuthentication.IsTrue())
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("PubkeyAuthentication"))
		require.Error(t, setter.Set("PubkeyAuthentication", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("PubkeyAuthentication"))
		require.NoError(t, setter.Set("PubkeyAuthentication", "yes"))
		require.NoError(t, setter.Set("PubkeyAuthentication", "no"))
		require.True(t, obj.PubkeyAuthentication.IsTrue(), "original value should stick")
	})
}

func TestSetterFieldRekeyLimit(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("RekeyLimit"))
		require.NoError(t, setter.Set("RekeyLimit", "1G", "1h"))
		require.Equal(t, "1G 1h", obj.RekeyLimit.String())
		require.Equal(t, 1<<30, int(obj.RekeyLimit.MaxData))
		require.Equal(t, time.Hour, obj.RekeyLimit.MaxTime)
		require.NoError(t, setter.Reset("RekeyLimit"))
		require.NoError(t, setter.Set("RekeyLimit", "1K"))
		require.Equal(t, "1K", obj.RekeyLimit.String())
		require.Equal(t, 1024, int(obj.RekeyLimit.MaxData))
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("RekeyLimit"))
		require.Error(t, setter.Set("RekeyLimit", "1G", "1h", "1m"))
		require.Error(t, setter.Set("RekeyLimit", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("RekeyLimit"))
		require.NoError(t, setter.Set("RekeyLimit", "1G", "1h"))
		require.Equal(t, "1G 1h", obj.RekeyLimit.String())
		require.Equal(t, time.Hour, obj.RekeyLimit.MaxTime)
		require.NoError(t, setter.Set("RekeyLimit", "1K"))
		require.Equal(t, "1G 1h", obj.RekeyLimit.String(), "original value should stick")
		require.Equal(t, time.Hour, obj.RekeyLimit.MaxTime, "original value should stick")
	})
}

func TestSetterFieldRequestTTY(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"yes", "no", "force", "auto"} {
			require.NoError(t, setter.Reset("RequestTTY"))
			require.NoError(t, setter.Set("RequestTTY", val))
			require.Equal(t, val, obj.RequestTTY.String())
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("RequestTTY"))
		require.Error(t, setter.Set("RequestTTY", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("RequestTTY"))
		require.NoError(t, setter.Set("RequestTTY", "yes"))
		require.NoError(t, setter.Set("RequestTTY", "no"))
		require.True(t, obj.RequestTTY.IsTrue(), "original value should stick")
	})
}

func TestSetterFieldSendEnv(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("SendEnv"))
		require.NoError(t, setter.Set("SendEnv", "FOO_BAR", "BAR_FOO"))
		require.Len(t, obj.SendEnv, 2)
		require.Equal(t, "FOO_BAR", obj.SendEnv[0])
		require.NoError(t, setter.Set("SendEnv", "-FOO_*"))
		require.Len(t, obj.SendEnv, 1)
		require.Equal(t, "BAR_FOO", obj.SendEnv[0])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("SendEnv"))
		require.Error(t, setter.Set("SendEnv", ""))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("SendEnv"))
		require.NoError(t, setter.Set("SendEnv", "FOO_BAR", "BAR_FOO"))
		require.Len(t, obj.SendEnv, 2)
		require.Equal(t, "FOO_BAR", obj.SendEnv[0])
		require.NoError(t, setter.Set("SendEnv", "-FOO_*"))
		require.Len(t, obj.SendEnv, 1)
		require.Equal(t, "BAR_FOO", obj.SendEnv[0])
		require.NoError(t, setter.Set("SendEnv", "FOO_BAR"))
		require.Len(t, obj.SendEnv, 2, "sendenv should accumulate")
		require.Equal(t, "BAR_FOO", obj.SendEnv[0])
		require.Equal(t, "FOO_BAR", obj.SendEnv[1])
	})
}

func TestSetterFieldSessionType(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"none", "subsystem", "default"} {
			require.NoError(t, setter.Reset("SessionType"))
			require.NoError(t, setter.Set("SessionType", val))
			require.Equal(t, val, obj.SessionType)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("SessionType"))
		require.Error(t, setter.Set("SessionType", "hello"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("SessionType"))
		require.NoError(t, setter.Set("SessionType", "default"))
		require.NoError(t, setter.Set("SessionType", "subsystem"))
		require.Equal(t, "default", obj.SessionType, "original value should stick")
	})
}

func TestSetterFieldSetEnv(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("SetEnv"))
		require.NoError(t, setter.Set("SetEnv", "FOO=BAR", "BAR=FOO"))
		require.Len(t, obj.SetEnv, 2)
		require.Equal(t, "BAR", obj.SetEnv["FOO"])
		require.Equal(t, "FOO", obj.SetEnv["BAR"])
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("SetEnv"))
		require.Error(t, setter.Set("SetEnv", "FOO"))
		require.Error(t, setter.Set("SetEnv"))
		require.Error(t, setter.Set("SetEnv", ""))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("SetEnv"))
		require.NoError(t, setter.Set("SetEnv", "FOO=BAR", "BAR=FOO"))
		require.NoError(t, setter.Set("SetEnv", "BAZ=FOO"))
		require.Len(t, obj.SetEnv, 2, "original values should stick")
		require.Equal(t, "BAR", obj.SetEnv["FOO"], "original values should stick")
		require.Equal(t, "FOO", obj.SetEnv["BAR"], "original values should stick")
	})
}

func TestSetterFieldStreamLocalBindMask(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("StreamLocalBindMask"))
		require.NoError(t, setter.Set("StreamLocalBindMask", "0644"))
		require.Equal(t, 0644, int(*obj.StreamLocalBindMask))
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("StreamLocalBindMask"))
		require.Error(t, setter.Set("StreamLocalBindMask", "abc"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("StreamLocalBindMask"))
		require.NoError(t, setter.Set("StreamLocalBindMask", "0644"))
		require.NoError(t, setter.Set("StreamLocalBindMask", "0700"))
		require.Equal(t, 0644, int(*obj.StreamLocalBindMask), "original value should stick")
	})
}

func TestSetterFieldStrictHostKeyChecking(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"yes", "no", "ask", "accept-new"} {
			require.NoError(t, setter.Reset("StrictHostKeyChecking"))
			require.NoError(t, setter.Set("StrictHostKeyChecking", val))
			require.Equal(t, val, obj.StrictHostKeyChecking.String())
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("StrictHostKeyChecking"))
		require.Error(t, setter.Set("StrictHostKeyChecking", "maybe"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("StrictHostKeyChecking"))
		require.NoError(t, setter.Set("StrictHostKeyChecking", "yes"))
		require.NoError(t, setter.Set("StrictHostKeyChecking", "no"))
		require.True(t, obj.StrictHostKeyChecking.IsTrue(), "original value should stick")
	})
}

func TestSetterFieldSyslogFacility(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"DAEMON", "USER", "AUTH", "AUTHPRIV", "LOCAL0", "LOCAL1", "LOCAL2", "LOCAL3", "LOCAL4", "LOCAL5", "LOCAL6", "LOCAL7"} {
			require.NoError(t, setter.Reset("SyslogFacility"))
			require.NoError(t, setter.Set("SyslogFacility", val))
			require.Equal(t, val, obj.SyslogFacility)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("SyslogFacility"))
		require.Error(t, setter.Set("SyslogFacility", "none"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("SyslogFacility"))
		require.NoError(t, setter.Set("SyslogFacility", "DAEMON"))
		require.NoError(t, setter.Set("SyslogFacility", "USER"))
		require.Equal(t, "DAEMON", obj.SyslogFacility, "original value should stick")
	})
}

func TestSetterFieldUpdateHostKeys(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"yes", "no", "ask"} {
			require.NoError(t, setter.Reset("UpdateHostKeys"))
			require.NoError(t, setter.Set("UpdateHostKeys", val))
			require.Equal(t, val, obj.UpdateHostKeys.String())
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("UpdateHostKeys"))
		require.Error(t, setter.Set("UpdateHostKeys", "maybe"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("UpdateHostKeys"))
		require.NoError(t, setter.Set("UpdateHostKeys", "ask"))
		require.NoError(t, setter.Set("UpdateHostKeys", "no"))
		require.True(t, obj.UpdateHostKeys.IsAsk())
	})
}

func TestSetterFieldVerifyHostKeyDNS(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)
	t.Run("valid", func(t *testing.T) {
		for _, val := range []string{"yes", "no", "ask"} {
			require.NoError(t, setter.Reset("VerifyHostKeyDNS"))
			require.NoError(t, setter.Set("VerifyHostKeyDNS", val))
			require.Equal(t, val, obj.VerifyHostKeyDNS.String())
		}
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("VerifyHostKeyDNS"))
		require.Error(t, setter.Set("VerifyHostKeyDNS", "maybe"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("VerifyHostKeyDNS"))
		require.NoError(t, setter.Set("VerifyHostKeyDNS", "ask"))
		require.NoError(t, setter.Set("VerifyHostKeyDNS", "no"))
		require.True(t, obj.VerifyHostKeyDNS.IsAsk())
	})
}

func TestSetterFieldEscapeChar(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		require.NoError(t, setter.Reset("EscapeChar"))
		require.NoError(t, setter.Set("EscapeChar", "none"))
		require.Equal(t, "none", obj.EscapeChar.String())
		require.NoError(t, setter.Reset("EscapeChar"))
		require.NoError(t, setter.Set("EscapeChar", "^K"))
		require.Equal(t, 11, int(obj.EscapeChar.Byte()))
	})
	t.Run("invalid", func(t *testing.T) {
		require.NoError(t, setter.Reset("EscapeChar"))
		require.Error(t, setter.Set("EscapeChar", ""))
		require.Error(t, setter.Set("EscapeChar", "none", "hello"))
	})
	t.Run("precedence", func(t *testing.T) {
		require.NoError(t, setter.Reset("EscapeChar"))
		require.NoError(t, setter.Set("EscapeChar", "none"))
		require.Equal(t, "none", obj.EscapeChar.String())
		require.NoError(t, setter.Set("EscapeChar", "~"))
		require.Equal(t, "none", obj.EscapeChar.String(), "original value should stick")
	})
}

func TestSetterFinalize(t *testing.T) {
	obj := sshconfig.Config{}
	setter, err := sshconfig.NewSetter(&obj)
	require.NoError(t, err)

	require.NoError(t, setter.Set("CertificateFile", "none"))
	require.Equal(t, "none", obj.CertificateFile[0])
	require.NoError(t, setter.Finalize())
	require.Len(t, obj.CertificateFile, 0)

	obj.Port = 22
	require.NoError(t, setter.Set("IdentityFile", "~/foo", "/tmp/%p.txt"))
	require.Equal(t, "~/foo", obj.IdentityFile[0])
	require.Equal(t, "/tmp/%p.txt", obj.IdentityFile[1])
	require.NoError(t, setter.Finalize())
	require.Len(t, obj.IdentityFile, 2)
	require.NotEqual(t, "~/foo", obj.IdentityFile[0])
	require.Equal(t, "/tmp/22.txt", obj.IdentityFile[1])
}
