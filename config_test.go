package rig

import (
	"testing"

	"github.com/k0sproject/rig/client/localhost"
	"github.com/k0sproject/rig/client/ssh"
	"github.com/k0sproject/rig/client/winrm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Run("no protocol configuration", func(t *testing.T) {
		c := &Config{}
		_, err := c.NewClient()
		assert.ErrorIs(t, ErrNoProtocolConfiguration, err)
	})

	t.Run("multiple protocols configured", func(t *testing.T) {
		c := &Config{
			SSHConfig:   &ssh.Config{},
			WinRMConfig: &winrm.Config{},
		}
		_, err := c.NewClient()
		assert.ErrorIs(t, ErrMultipleProtocolsConfigured, err)
	})

	t.Run("valid Localhost configuration", func(t *testing.T) {
		c := &Config{
			LocalhostConfig: &localhost.Config{
				Enabled: true,
			},
		}
		conn, err := c.NewClient()
		require.NoError(t, err)
		assert.IsType(t, &localhost.Client{}, conn)
	})
}
