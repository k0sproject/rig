package rig_test

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/rigtest"
	"github.com/k0sproject/rig/sudo"
	"github.com/stretchr/testify/require"
)

func TestGetRemoteFS(t *testing.T) {
	mc := rigtest.NewMockConnection()
	runner := exec.NewHostRunner(mc)

	t.Run("posix", func(t *testing.T) {
		fs, err := rig.GetRemoteFS(runner)
		// the current implementation never returns an error, the result
		// will be either posixfs or winfs.
		require.NoError(t, err)
		require.IsType(t, &remotefs.PosixFS{}, fs)
	})

	t.Run("windows", func(t *testing.T) {
		mc.Windows = true
		fs, err := rig.GetRemoteFS(runner)
		// the current implementation never returns an error, the result
		// will be either posixfs or winfs.
		require.NoError(t, err)
		require.IsType(t, &remotefs.WinFS{}, fs)
	})
}

func TestGetServiceManager(t *testing.T) {
	t.Run("systemd", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner, _ := rig.NewRunner(mc)
		mc.AddMockCommand(regexp.MustCompile(`^stat /run/systemd/system`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		sm, err := rig.GetServiceManager(runner)
		require.NoError(t, err)
		require.IsType(t, initsystem.Systemd{}, sm)
	})

	t.Run("upstart", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)
		mc.AddMockCommand(regexp.MustCompile(`^command -v initctl`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		sm, err := rig.GetServiceManager(runner)
		require.NoError(t, err)
		require.IsType(t, initsystem.Upstart{}, sm)
	})

	t.Run("error", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})
		runner := exec.NewHostRunner(mc)
		_, err := rig.GetServiceManager(runner)
		require.ErrorIs(t, err, initsystem.ErrNoInitSystem)
	})
}

func TestGetPackageManager(t *testing.T) {
	t.Run("apk", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)
		mc.AddMockCommand(regexp.MustCompile(`^command -v apk`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		pm, err := rig.GetPackageManager(runner)
		require.NoError(t, err)
		require.NotNil(t, pm)

		_ = pm.Install(context.Background(), "package")
		require.Contains(t, mc.Commands(), "apk add package")
	})

	t.Run("yum", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)
		mc.AddMockCommand(regexp.MustCompile(`^command -v yum`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		pm, err := rig.GetPackageManager(runner)
		require.NoError(t, err)
		require.NotNil(t, pm)

		_ = pm.Install(context.Background(), "package")
		require.Contains(t, mc.Commands(), "yum install -y package")
	})

	t.Run("error", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})
		runner := exec.NewHostRunner(mc)
		_, err := rig.GetPackageManager(runner)
		require.ErrorIs(t, err, packagemanager.ErrNoPackageManager)
	})
}

func TestGetSudoRunner(t *testing.T) {

	t.Run("sudo", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)

		mc.AddMockCommand(regexp.MustCompile(`^sudo.*true`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		sudoRunner, err := rig.GetSudoRunner(runner)
		require.NoError(t, err)
		require.NotNil(t, sudoRunner)

		_ = sudoRunner.Exec("hello")
		commands := mc.Commands()
		lastCommand := commands[len(commands)-1]
		require.Regexp(t, `^sudo.*hello`, lastCommand)
	})

	t.Run("doas", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)

		mc.AddMockCommand(regexp.MustCompile(`^doas.*true`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})
		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		sudoRunner, err := rig.GetSudoRunner(runner)
		require.NoError(t, err)
		require.NotNil(t, sudoRunner)

		_ = sudoRunner.Exec("hello")
		commands := mc.Commands()
		lastCommand := commands[len(commands)-1]
		require.Regexp(t, `^doas.*hello`, lastCommand)
	})

	t.Run("error", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)

		mc.AddMockCommand(regexp.MustCompile(`^.*`), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		_, err := rig.GetSudoRunner(runner)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})
}

func TestGetOSRelease(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		builder := strings.Builder{}
		// this is easier than trying to keep format when gofmt will mess it up
		builder.WriteString("PRETTY_NAME=\"Foo\"\n")
		builder.WriteString("ID=\"foo\"\n")
		builder.WriteString("VERSION_ID=\"1.0\"\n")
		builder.WriteString("FOO=\"BAR\"\n")
		osRelease := builder.String()

		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)
		mc.AddMockCommand(regexp.MustCompile("^uname"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return nil
		})

		mc.AddMockCommand(regexp.MustCompile("^cat /etc/os-release"), func(_ context.Context, _ io.Reader, stdout, _ io.Writer) error {
			_, _ = stdout.Write([]byte(osRelease))
			return nil
		})

		os, err := rig.GetOSRelease(runner)
		require.NoError(t, err)
		require.Equal(t, "Foo", os.Name)
		require.Equal(t, "1.0", os.Version)
		require.Equal(t, "foo", os.ID)
		require.Equal(t, "BAR", os.ExtraFields["FOO"])
	})

	t.Run("error", func(t *testing.T) {
		mc := rigtest.NewMockConnection()
		runner := exec.NewHostRunner(mc)
		mc.AddMockCommand(regexp.MustCompile("^.*"), func(_ context.Context, _ io.Reader, _, _ io.Writer) error {
			return errors.New("mock error")
		})

		_, err := rig.GetOSRelease(runner)
		require.ErrorIs(t, err, os.ErrNotRecognized)
	})

}
