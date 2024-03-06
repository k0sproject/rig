package rig_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/rigtest"
	"github.com/k0sproject/rig/sudo"
	"github.com/stretchr/testify/require"
)

func TestGetRemoteFS(t *testing.T) {
	mr := rigtest.NewMockRunner()

	t.Run("posix", func(t *testing.T) {
		fs, err := rig.GetRemoteFS(mr)
		// the current implementation never returns an error, the result
		// will be either posixfs or winfs.
		require.NoError(t, err)
		require.IsType(t, &remotefs.PosixFS{}, fs)
	})

	t.Run("windows", func(t *testing.T) {
		mr.Windows = true
		fs, err := rig.GetRemoteFS(mr)
		// the current implementation never returns an error, the result
		// will be either posixfs or winfs.
		require.NoError(t, err)
		require.IsType(t, &remotefs.WinFS{}, fs)
	})
}

func TestGetServiceManager(t *testing.T) {
	t.Run("systemd", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommand(rigtest.Equal("stat /run/systemd/system"), func(a *rigtest.A) error { return nil })

		sm, err := rig.GetServiceManager(mr)
		require.NoError(t, err)
		require.IsType(t, initsystem.Systemd{}, sm)
	})

	t.Run("upstart", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommand(rigtest.HasPrefix("command -v initctl"), func(a *rigtest.A) error { return nil })

		sm, err := rig.GetServiceManager(mr)
		require.NoError(t, err)
		require.IsType(t, initsystem.Upstart{}, sm)
	})

	t.Run("error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")

		_, err := rig.GetServiceManager(mr)
		require.ErrorIs(t, err, initsystem.ErrNoInitSystem)
	})
}

func TestGetPackageManager(t *testing.T) {
	t.Run("apk", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommand(rigtest.Equal("command -v apk"), func(a *rigtest.A) error { return nil })

		pm, err := rig.GetPackageManager(mr)
		require.NoError(t, err)
		require.NotNil(t, pm)

		_ = pm.Install(context.Background(), "package")
		rigtest.ReceivedContains(t, mr, "apk add package")
	})

	t.Run("yum", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommand(rigtest.Equal("command -v yum"), func(a *rigtest.A) error { return nil })

		pm, err := rig.GetPackageManager(mr)
		require.NoError(t, err)
		require.NotNil(t, pm)

		_ = pm.Install(context.Background(), "package")
		rigtest.ReceivedContains(t, mr, "yum install -y package")
	})

	t.Run("error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")

		_, err := rig.GetPackageManager(mr)
		require.ErrorIs(t, err, packagemanager.ErrNoPackageManager)
	})
}

func TestGetSudoRunner(t *testing.T) {

	t.Run("sudo", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommand(rigtest.Match("sudo.*true"), func(a *rigtest.A) error { return nil })

		sudoRunner, err := rig.GetSudoRunner(mr)
		require.NoError(t, err)
		require.NotNil(t, sudoRunner)

		_ = sudoRunner.Exec("hello")
		rigtest.ReceivedMatch(t, mr, "^sudo.*hello$")
	})

	t.Run("doas", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommand(rigtest.Match("doas.*true"), func(a *rigtest.A) error { return nil })

		sudoRunner, err := rig.GetSudoRunner(mr)
		require.NoError(t, err)
		require.NotNil(t, sudoRunner)

		_ = sudoRunner.Exec("hello")
		rigtest.ReceivedMatch(t, mr, `^doas.*hello$`)
	})

	t.Run("error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")

		_, err := rig.GetSudoRunner(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})
}
func TestGetOSRelease(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		builder := strings.Builder{}
		// this is easier than trying to keep format when gofmt will mess it up
		fmt.Fprintln(&builder, `PRETTY_NAME="Foo 1.2.3"`)
		fmt.Fprintln(&builder, `NAME="Foo"`)
		fmt.Fprintln(&builder, `ID="foo"`)
		fmt.Fprintln(&builder, `VERSION_ID="1.0"`)
		fmt.Fprintln(&builder, `FOO="BAR"`)
		osRelease := builder.String()

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")
		mr.AddCommandSuccess(rigtest.Match("uname.*Linux"))
		mr.AddCommand(rigtest.HasPrefix("cat /etc/os-release"), func(a *rigtest.A) error {
			t.Log("executing", a.Command)
			_, _ = fmt.Fprint(a.Stdout, osRelease)
			return nil
		})

		t.Log("getting release")
		os, err := rig.GetOSRelease(mr)
		require.NoError(t, err)
		require.Equal(t, "Foo", os.Name)
		require.Equal(t, "1.0", os.Version)
		require.Equal(t, "foo", os.ID)
		require.Equal(t, "BAR", os.ExtraFields["FOO"])
		require.Equal(t, "Foo 1.2.3", os.ExtraFields["PRETTY_NAME"])
	})

	t.Run("error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errors.New("mock error")

		_, err := rig.GetOSRelease(mr)
		require.ErrorIs(t, err, os.ErrNotRecognized)
	})
}
