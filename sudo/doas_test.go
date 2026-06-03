package sudo_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/assert"
)

// TestDoasGolden pins the exact command strings produced by the Doas decorator.
func TestDoasGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple command",
			input: "apt-get install -y docker.io",
			want:  `doas -n -- "${SHELL-sh}" -c 'apt-get install -y docker.io'`,
		},
		{
			name:  "command with embedded single quotes",
			input: "bash -c 'systemctl restart k0s'",
			want:  "doas -n -- \"${SHELL-sh}\" -c 'bash -c '\"'\"'systemctl restart k0s'\"'\"''",
		},
		{
			name:  "plain path",
			input: "cat /etc/k0s/k0s.yaml",
			want:  `doas -n -- "${SHELL-sh}" -c 'cat /etc/k0s/k0s.yaml'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sudo.Doas(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
