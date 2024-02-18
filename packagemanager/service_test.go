package packagemanager_test

import (
	"context"
	"errors"
	"io"
	"regexp"
	"testing"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigtest"
	"github.com/stretchr/testify/require"
)

func TestPackageManagerService_SuccessfulInitialization(t *testing.T) {
	mc := rigtest.NewMockConnection()
	runner := exec.NewHostRunner(mc)

	mc.AddMockCommand(regexp.MustCompile("^command -v zypper"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
		return nil
	})
	mc.AddMockCommand(regexp.MustCompile("^.*"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
		return errors.New("mock error")
	})

	pms := packagemanager.NewPackageManagerService(packagemanager.DefaultProvider, runner)

	pm, err := pms.GetPackageManager()
	require.NoError(t, err)
	require.NotNil(t, pm)

	err = pms.PackageManager().Install(context.Background(), "sample-package")
	require.ErrorContains(t, err, "mock error")

	require.Contains(t, mc.Commands(), "command -v zypper")
	require.Contains(t, mc.Commands(), "zypper install -y sample-package")
}

func TestPackageManagerService_InitializationFailure(t *testing.T) {
	mc := rigtest.NewMockConnection()
	runner := exec.NewHostRunner(mc)

	mc.AddMockCommand(regexp.MustCompile(".*"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
		return errors.New("mock error")
	})

	pms := packagemanager.NewPackageManagerService(packagemanager.DefaultProvider, runner)
	pm, err := pms.GetPackageManager()
	require.ErrorIs(t, err, packagemanager.ErrNoPackageManager)
	require.Nil(t, pm)

	require.NotNil(t, pms.PackageManager())
	err = pms.PackageManager().Install(context.Background(), "sample-package")
	require.ErrorContains(t, err, "get package manager")
	commands := mc.Commands()
	require.Contains(t, commands, "command -v zypper")
	require.Contains(t, commands, "command -v apk")
	for _, c := range commands {
		require.NotContains(t, c, "sample-package")
	}
}
