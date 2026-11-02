package sshconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultList(t *testing.T) {
	// Every key that takes the +, - and ^ modifiers, by its current name.
	keys := []string{
		"CASignatureAlgorithms",
		"Ciphers",
		"HostbasedAcceptedAlgorithms",
		"HostKeyAlgorithms",
		"KexAlgorithms",
		"MACs",
		"PubkeyAcceptedAlgorithms",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			list := defaultList(key)
			require.NotEmpty(t, list, "the built-in default config must set %s", key)
			require.NotContains(t, strings.Split(list, ","), "", "the list must not hold empty entries")
			require.Equal(t, list, defaultList(strings.ToLower(key)), "the key is case-insensitive")
		})
	}

	t.Run("deprecated aliases", func(t *testing.T) {
		require.Equal(t, defaultList("HostbasedAcceptedAlgorithms"), defaultList("HostbasedKeyTypes"))
		require.Equal(t, defaultList("PubkeyAcceptedAlgorithms"), defaultList("PubkeyAcceptedKeyTypes"))
	})

	t.Run("unknown key", func(t *testing.T) {
		require.Empty(t, defaultList("NoSuchList"))
	})
}
