package rig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsHostKeyError(t *testing.T) {
	t.Run("detects host key verification failed", func(t *testing.T) {
		require.True(t, isHostKeyError("Host key verification failed."))
	})
	t.Run("detects remote host identification changed", func(t *testing.T) {
		require.True(t, isHostKeyError("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!"))
	})
	t.Run("ignores unrelated errors", func(t *testing.T) {
		require.False(t, isHostKeyError("Connection refused"))
		require.False(t, isHostKeyError(""))
	})
}
