package packagemanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestPackageManagerProvider_SuccessfulInitialization(t *testing.T) {
	mr := rigtest.NewMockRunner()

	mr.AddCommand(rigtest.Equal("command -v zypper"), func(a *rigtest.A) error {
		return nil
	})
	mr.ErrDefault = errors.New("command not found")

	pms := packagemanager.NewPackageManagerProvider(packagemanager.DefaultRegistry(), mr)

	pm, err := pms.PackageManager()
	require.NoError(t, err)
	require.NotNil(t, pm)

	err = pm.Install(context.Background(), "sample-package")
	require.ErrorContains(t, err, "command not found")

	rigtest.ReceivedEqual(t, mr, "command -v zypper")
	rigtest.ReceivedContains(t, mr, "sample-package")
}

func TestPackageManagerProvider_InitializationFailure(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.ErrDefault = errors.New("mock error")

	pms := packagemanager.NewPackageManagerProvider(packagemanager.DefaultRegistry(), mr)
	t.Run("PackageManager", func(t *testing.T) {
		pm, err := pms.PackageManager()
		require.ErrorIs(t, err, packagemanager.ErrNoPackageManager)
		require.Nil(t, pm)
	})
}
