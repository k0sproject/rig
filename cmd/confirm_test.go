package cmd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandGateAllows(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Equal("echo hi"), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	var seen string
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		seen = command
		return nil
	}))

	require.NoError(t, runner.Exec("echo hi"))
	assert.Equal(t, "echo hi", seen, "gate should receive the formatted command")
	rigtest.ReceivedEqual(t, conn, "echo hi")
}

func TestCommandGateRejects(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	denied := errors.New("nope")
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, _ string) error {
		return denied
	}))

	err := runner.Exec("rm -rf /")
	require.Error(t, err)
	assert.ErrorIs(t, err, cmd.ErrCommandRejected, "error must wrap ErrCommandRejected")
	assert.ErrorIs(t, err, denied, "error must wrap the gate's own error")
	assert.Zero(t, conn.Len(), "a rejected command must never reach the connection")
}

func TestCommandGateReceivesDecoratedCommand(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn, func(c string) string { return "sudo " + c })

	var seen string
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		seen = command
		return nil
	}))

	require.NoError(t, runner.Exec("whoami"))
	assert.Equal(t, "sudo whoami", seen, "gate must see the fully decorated command")
}

func TestCommandGateRedactsSecrets(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	secret := "s3cr3t"
	var seen string
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		seen = command
		return nil
	}))

	require.NoError(t, runner.Exec("login --token "+secret, cmd.Redact(secret)))
	assert.NotContains(t, seen, secret, "gate must receive the redacted command")
	assert.Contains(t, seen, cmd.DefaultRedactMask)
}

func TestCommandGateNilHasNoEffect(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Equal("echo hi"), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	runner.SetCommandGate(nil)

	require.NoError(t, runner.Exec("echo hi"))
	rigtest.ReceivedEqual(t, conn, "echo hi")
}
