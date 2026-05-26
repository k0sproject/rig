package sh_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/sh"
	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/stretchr/testify/assert"
)

// TestCommandGolden pins the exact output of sh.Command for realistic fixtures.
// Any change to quoting logic will be caught here.
func TestCommandGolden(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args []string
		want string
	}{
		{
			name: "no args returns bare command",
			cmd:  "true",
			want: "true",
		},
		{
			name: "word with special chars is quoted",
			cmd:  "echo",
			args: []string{"hello world"},
			want: "echo 'hello world'",
		},
		{
			name: "path with spaces is quoted",
			cmd:  "cat",
			args: []string{"/path/to my/file.txt"},
			want: "cat '/path/to my/file.txt'",
		},
		{
			name: "plain flags are not quoted",
			cmd:  "k0s",
			args: []string{"install", "--config", "/etc/k0s/k0s.yaml"},
			want: "k0s install --config /etc/k0s/k0s.yaml",
		},
		{
			name: "value with percent sign is quoted",
			cmd:  "systemctl",
			args: []string{"set-property", "k0s", "CPUQuota=50%"},
			want: "systemctl set-property k0s 'CPUQuota=50%'",
		},
		{
			name: "path containing single quote is escaped",
			cmd:  "ls",
			args: []string{"/home/bob's dir"},
			want: "ls '/home/bob'\"'\"'s dir'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sh.Command(tt.cmd, tt.args...)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCommandBuilderGolden pins the exact string produced by CommandBuilder
// chains for realistic use-cases found in k0sctl and rig internals.
func TestCommandBuilderGolden(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "pipe grep filters output",
			got:  sh.CommandBuilder("cat /etc/os-release").Pipe("grep", "^ID=").String(),
			// Note: ^ is not in shellescape's special-char set so ^ID= is not quoted.
			want: "cat /etc/os-release | grep ^ID=",
		},
		{
			name: "stderr to null suppresses errors",
			got:  sh.CommandBuilder("which k0s").ErrToNull().String(),
			want: "which k0s 2>/dev/null",
		},
		{
			name: "stdout and stderr merged",
			got:  sh.CommandBuilder("journalctl -u k0s").ErrToOut().String(),
			want: "journalctl -u k0s 2>&1",
		},
		{
			name: "output redirected to file",
			got:  sh.CommandBuilder("k0s kubectl get nodes -o json").OutToFile("/tmp/nodes.json").String(),
			want: "k0s kubectl get nodes -o json >/tmp/nodes.json",
		},
		{
			name: "output redirected to path with spaces",
			got:  sh.CommandBuilder("echo ok").OutToFile("/tmp/my output.txt").String(),
			want: "echo ok >'/tmp/my output.txt'",
		},
		{
			name: "arg appended to builder",
			got:  sh.CommandBuilder("ls").Arg("-la").Arg("/var/lib/k0s").String(),
			want: "ls -la /var/lib/k0s",
		},
		{
			name: "pipe chains build left-to-right",
			got: sh.CommandBuilder("cat /proc/mounts").
				Pipe("grep", "overlay").
				Pipe("awk", "{print $2}").
				String(),
			want: "cat /proc/mounts | grep overlay | awk '{print $2}'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

// TestQuoteGolden pins the exact output of shellescape.Quote for edge cases
// that affect correctness when commands are interpreted by a POSIX shell.
func TestQuoteGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string produces paired single quotes",
			input: "",
			want:  "''",
		},
		{
			name:  "plain identifier is returned unquoted",
			input: "foo.example.com",
			want:  "foo.example.com",
		},
		{
			name:  "dollar sign forces single quotes",
			input: "$HOME",
			want:  "'$HOME'",
		},
		{
			name:  "pipe forces single quotes",
			input: "a|b",
			want:  "'a|b'",
		},
		{
			name:  "both single quote and special chars use escaped form",
			input: "it's $special",
			want:  "'it'\"'\"'s $special'",
		},
		{
			name:  "double quote wraps in single quotes",
			input: `say "hello"`,
			want:  `'say "hello"'`,
		},
		{
			name:  "backslash forces single quotes",
			input: `C:\Windows`,
			want:  `'C:\Windows'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellescape.Quote(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
