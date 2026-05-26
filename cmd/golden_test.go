package cmd_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWindowsCommandWrappingGolden pins the exact command strings sent to the
// connection when running on a Windows host. The cmd.exe /C prefix must be
// added for plain commands and must NOT be added for *.exe commands.
func TestWindowsCommandWrappingGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain command gets cmd.exe /C prefix",
			input: "k0s version",
			want:  "cmd.exe /C k0s version",
		},
		{
			name:  "powershell.exe command is not wrapped",
			input: "powershell.exe -Command Get-Date",
			want:  "powershell.exe -Command Get-Date",
		},
		{
			name:  "cmd.exe command is not wrapped",
			input: "cmd.exe /C echo hello",
			want:  "cmd.exe /C echo hello",
		},
		{
			name:  "path-qualified exe is not wrapped",
			input: `C:\Windows\System32\ipconfig.exe /all`,
			want:  `C:\Windows\System32\ipconfig.exe /all`,
		},
		{
			name:  "k0s install with flags gets wrapped",
			input: "k0s install controller --config C:\\k0s\\k0s.yaml",
			want:  "cmd.exe /C k0s install controller --config C:\\k0s\\k0s.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := rigtest.NewMockConnection()
			conn.Windows = true
			// Accept any command (we capture what was received).
			conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })
			runner := cmd.NewExecutor(conn)
			_ = runner.Exec(tt.input)
			rigtest.ReceivedEqual(t, conn, tt.want)
		})
	}
}

// TestRedactPreservesWireCommand verifies that sensitive strings passed via Redact()
// do not alter the command sent over the wire — only log representations are masked.
func TestRedactPreservesWireCommand(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	secret := "s3cr3tP@ss"
	err := runner.Exec("curl -u admin:"+secret+" https://registry.example.com/v2/", cmd.Redact(secret))
	require.NoError(t, err)

	// The raw command sent to the connection must still contain the secret —
	// redaction is a logging-only concern and must not alter the wire command.
	rigtest.ReceivedContains(t, conn, secret)
}

// TestDecoratorPipelineGolden pins the exact string produced when global and
// per-call decorators are applied in order: per-call first, global second.
// Both decorators are prefixes so the order is observable in the output.
func TestDecoratorPipelineGolden(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })

	// Global decorator: prefix all commands with "env VAR=1".
	globalDec := func(c string) string { return "env VAR=1 " + c }
	runner := cmd.NewExecutor(conn, globalDec)

	// Per-call decorator: prefix with "sudo".
	// Applied before the global decorator, so the expected output is
	// "env VAR=1 sudo myapp run" (not "sudo env VAR=1 myapp run").
	callDec := func(c string) string { return "sudo " + c }
	_ = runner.Exec("myapp run", cmd.Decorate(callDec))

	// Expected: per-call applied first ("sudo myapp run"), then global prefix.
	rigtest.ReceivedEqual(t, conn, "env VAR=1 sudo myapp run")
}

// TestExecOutputTrimGolden verifies that ExecOutput trims trailing whitespace
// by default and preserves it when TrimOutput(false) is used.
func TestExecOutputTrimGolden(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("hostname"), "node-01.example.com\n")

	out, err := mr.ExecOutput("hostname")
	require.NoError(t, err)
	assert.Equal(t, "node-01.example.com", out, "output should be trimmed by default")

	out, err = mr.ExecOutput("hostname", cmd.TrimOutput(false))
	require.NoError(t, err)
	assert.Equal(t, "node-01.example.com\n", out, "output should be preserved with TrimOutput(false)")
}
