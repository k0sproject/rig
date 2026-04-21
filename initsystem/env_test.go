package initsystem_test

import (
	"context"
	"testing"

	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestSystemdServiceEnvironmentPath(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(
		rigtest.Equal(`systemctl show -p FragmentPath mysvc.service | cut '-d"="' -f2`),
		"/etc/systemd/system/mysvc.service\n",
	)

	got, err := initsystem.Systemd{}.ServiceEnvironmentPath(context.Background(), mr, "mysvc")
	require.NoError(t, err)
	require.Equal(t, "/etc/systemd/system/mysvc.service.d/env.conf", got)
}

func TestSystemdServiceEnvironmentContent(t *testing.T) {
	t.Run("keys are sorted", func(t *testing.T) {
		env := map[string]string{
			"ZEBRA": "last",
			"ALPHA": "first",
			"MANGO": "middle",
		}
		content := initsystem.Systemd{}.ServiceEnvironmentContent(env)
		require.Equal(t, "[Service]\nEnvironment=ALPHA=first\nEnvironment=MANGO=middle\nEnvironment=ZEBRA=last\n", content)
	})

	t.Run("values with spaces are quoted", func(t *testing.T) {
		env := map[string]string{"KEY": "hello world"}
		content := initsystem.Systemd{}.ServiceEnvironmentContent(env)
		require.Equal(t, "[Service]\nEnvironment='KEY=hello world'\n", content)
	})
}

func TestOpenRCServiceEnvironmentPath(t *testing.T) {
	got, err := initsystem.OpenRC{}.ServiceEnvironmentPath(context.Background(), rigtest.NewMockRunner(), "mysvc")
	require.NoError(t, err)
	require.Equal(t, "/etc/conf.d/mysvc", got)
}

func TestOpenRCServiceEnvironmentContent(t *testing.T) {
	env := map[string]string{
		"ZEBRA": "last",
		"ALPHA": "first",
		"MANGO": "middle",
	}
	content := initsystem.OpenRC{}.ServiceEnvironmentContent(env)
	require.Equal(t, "export ALPHA=first\nexport MANGO=middle\nexport ZEBRA=last\n", content)
}
