package ssh

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigSetDefaultsCascadesIgnoreSSHConfig(t *testing.T) {
	t.Run("cascades to bastion", func(t *testing.T) {
		cfg := Config{
			Address:         "192.0.2.10",
			IgnoreSSHConfig: true,
			Bastion:         &Config{Address: "192.0.2.20"},
		}
		cfg.SetDefaults()
		require.True(t, cfg.Bastion.IgnoreSSHConfig, "bastion must inherit IgnoreSSHConfig")
	})

	t.Run("cascades through a bastion chain", func(t *testing.T) {
		cfg := Config{
			Address:         "192.0.2.10",
			IgnoreSSHConfig: true,
			Bastion: &Config{
				Address: "192.0.2.20",
				Bastion: &Config{Address: "192.0.2.30"},
			},
		}
		cfg.SetDefaults()
		require.True(t, cfg.Bastion.IgnoreSSHConfig)
		require.True(t, cfg.Bastion.Bastion.IgnoreSSHConfig)
	})

	t.Run("does not turn the bastion setting off", func(t *testing.T) {
		cfg := Config{
			Address: "192.0.2.10",
			Bastion: &Config{Address: "192.0.2.20", IgnoreSSHConfig: true},
		}
		cfg.SetDefaults()
		require.False(t, cfg.IgnoreSSHConfig, "parent must be left alone")
		require.True(t, cfg.Bastion.IgnoreSSHConfig, "bastion setting must be preserved")
	})

	t.Run("nil bastion is fine", func(t *testing.T) {
		cfg := Config{Address: "192.0.2.10", IgnoreSSHConfig: true}
		require.NotPanics(t, func() { cfg.SetDefaults() })
	})
}
