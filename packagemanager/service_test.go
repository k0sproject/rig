package packagemanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestPackageManagerService_SuccessfulInitialization(t *testing.T) {
	mr := rigtest.NewMockRunner()

	mr.AddCommand(rigtest.Equal("command -v zypper"), func(a *rigtest.A) error {
		return nil
	})
	mr.ErrDefault = errors.New("command not found")

	pms := packagemanager.NewPackageManagerService(packagemanager.DefaultProvider(), mr)

	pm, err := pms.GetPackageManager()
	require.NoError(t, err)
	require.NotNil(t, pm)

	err = pms.PackageManager().Install(context.Background(), "sample-package")
	require.ErrorContains(t, err, "command not found")

	rigtest.ReceivedEqual(t, mr, "command -v zypper")
	rigtest.ReceivedContains(t, mr, "sample-package")
}

func TestPackageManagerService_InitializationFailure(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.ErrDefault = errors.New("mock error")

	pms := packagemanager.NewPackageManagerService(packagemanager.DefaultProvider(), mr)
	t.Run("GetPackageManager", func(t *testing.T) {
		pm, err := pms.GetPackageManager()
		require.ErrorIs(t, err, packagemanager.ErrNoPackageManager)
		require.Nil(t, pm)
	})
	t.Run("PackageManager", func(t *testing.T) {
		require.NotNil(t, pms.PackageManager(), "PackageManager should return a intentionally dysfunctional PackageManager if none can be found")
		err := pms.PackageManager().Install(context.Background(), "sample-package")
		require.ErrorIs(t, err, packagemanager.ErrNoPackageManager)
		rigtest.ReceivedEqual(t, mr, "command -v zypper")
		rigtest.ReceivedEqual(t, mr, "command -v apk")
		rigtest.NotReceivedContains(t, mr, "sample-package")
	})
}
