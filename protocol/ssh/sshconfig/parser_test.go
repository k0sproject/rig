package sshconfig_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig"
	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig/value"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	type hostconfig struct {
		sshconfig.RequiredFields
		IdentityFile value.PathListValue
	}
	parser, err := sshconfig.NewParser(nil)
	require.NoError(t, err)
	obj := &hostconfig{}
	err = parser.Parse(obj, "example.com")
	require.NoError(t, err)
}

func TestParseIgnoreUnknown(t *testing.T) {
	content := "Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  UnknownOption yes\n"
	t.Run("no ignoreunknown, no ErrorOnUnknown", func(t *testing.T) {
		type hostconfig struct {
			sshconfig.RequiredFields
			IdentityFile value.PathListValue
		}
		obj := &hostconfig{}
		parser, err := sshconfig.NewParser(strings.NewReader(content))
		require.NoError(t, err)
		require.NoError(t, parser.Parse(obj, "example.com"))
	})
	t.Run("no ignoreunknown, ErrorOnUnknown true", func(t *testing.T) {
		type hostconfig struct {
			sshconfig.RequiredFields
			IdentityFile value.PathListValue
		}
		obj := &hostconfig{}
		parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithErrorOnUnknown())
		require.NoError(t, err)
		err = parser.Parse(obj, "example.com")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown field")
	})
	t.Run("ignoreunknown set, ErrorOnUnknown true", func(t *testing.T) {
		obj := &sshconfig.SSHConfig{}
		obj.IgnoreUnknown.SetString("unknown*", "")
		parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithErrorOnUnknown())
		require.NoError(t, err)
		err = parser.Parse(obj, "example.com")
		require.NoError(t, err)
	})
}

func TestParseFull(t *testing.T) {
	obj := &sshconfig.SSHConfig{}
	parser, err := sshconfig.NewParser(nil)
	require.NoError(t, err)
	err = parser.Parse(obj, "example.com")
	require.NoError(t, err)
	require.NotEmpty(t, obj.IdentityFile.String())
}

func TestParsePrecedence(t *testing.T) {
	gcReader := strings.NewReader("GlobalKnownHostsFile /foo/ssh/ssh_known_hosts\nSendEnv LANG\nStrictHostKeyChecking yes\n")
	ucReader := strings.NewReader("GlobalKnownHostsFile /dev/null\nStrictHostKeyChecking no\nHost example.com\nSendEnv TERM\nStrictHostKeyChecking ask\n")
	obj := &sshconfig.SSHConfig{}
	parser, err := sshconfig.NewParser(nil, sshconfig.WithGlobalConfigReader(gcReader), sshconfig.WithUserConfigReader(ucReader))
	require.NoError(t, err)
	err = parser.Parse(obj, "example.com")
	require.NoError(t, err)
	require.Equal(t, "/dev/null", obj.GlobalKnownHostsFile.String(), "The first value should stick")
	require.Equal(t, "TERM LANG", obj.SendEnv.String(), "SendEnv is always appending")
	require.Equal(t, "no", obj.StrictHostKeyChecking.String(), "The first value was in user config's global section and it should stick")
}

func TestHostAndMatchBlocks(t *testing.T) {
	content := `
        Host example.com
            IdentityFile ~/.ssh/id_example
            Port 22

        Host *.example.net
            IdentityFile ~/.ssh/id_example_net
            Port 2222

        Match host "specific.example.org"
            IdentityFile ~/.ssh/id_specific
            Port 2200

        Host *
            IdentityFile ~/.ssh/id_default
            Port 22
    `

	// Create parser instance with the test content
	parser, err := sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)

	// Test cases
	testCases := []struct {
		hostName string
		identity string
		port     string
	}{
		{"example.com", "/.ssh/id_example", "22"},
		{"random.example.net", "/.ssh/id_example_net", "2222"},
		{"specific.example.org", "/.ssh/id_specific", "2200"},
		{"anotherhost", "/.ssh/id_default", "22"},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.SSHConfig{} // Using the full SSHConfig struct
			err = parser.Parse(obj, tc.hostName)
			require.NoError(t, err)

			require.True(t, strings.HasSuffix(obj.IdentityFile.String(), tc.identity), "IdentityFile should end with %s but is %s", tc.identity, obj.IdentityFile.String())
			require.False(t, strings.HasPrefix(obj.IdentityFile.String(), "~"), "tilde paths should be expanded, but got %s", obj.IdentityFile.String())
			require.Equal(t, tc.port, obj.Port.String())
		})
	}
}

func TestPatternMatchingAndNegation(t *testing.T) {
	content := `
        Host *.example.com
            IdentityFile ~/.ssh/id_wildcard

        Host ??.example.net
            IdentityFile ~/.ssh/id_question

        Host !forbidden.example.com
            IdentityFile ~/.ssh/id_negation

        Host example.*
            IdentityFile ~/.ssh/id_domain

        Host *
            IdentityFile ~/.ssh/id_default
    `

	for _, win := range []bool{true, false} {
		t.Run(fmt.Sprintf("windows_lf=%v", win), func(t *testing.T) {
			if win {
				content = strings.ReplaceAll(content, "\n", "\r\n")
			}
			parser, err := sshconfig.NewParser(strings.NewReader(content))
			require.NoError(t, err)

			testCases := []struct {
				hostName string
				identity string
			}{
				{"test.example.com", "/.ssh/id_wildcard"},
				{"aa.example.net", "/.ssh/id_question"},
				{"acceptable.example.com", "/.ssh/id_wildcard"},
				{"example.org", "/.ssh/id_domain"},
				{"forbidden.example.com", "/.ssh/id_wildcard"},
				{"randomhost", "/.ssh/id_default"},
			}

			for _, tc := range testCases {
				t.Run(tc.hostName, func(t *testing.T) {
					obj := &sshconfig.SSHConfig{}
					err = parser.Parse(obj, tc.hostName)
					require.NoError(t, err)
					require.True(t, strings.HasSuffix(obj.IdentityFile.String(), tc.identity), "IdentityFile should end with %s but is %s", tc.identity, obj.IdentityFile.String())
					require.False(t, strings.HasPrefix(obj.IdentityFile.String(), "~"), "tilde paths should be expanded, but got %s", obj.IdentityFile.String())
				})
			}
		})
	}
}

func TestIncludeDirectives(t *testing.T) {
	// Setup temporary directory for mock config files
	tmpDir := t.TempDir()

	// Mock include files with distinct settings
	include1Path := filepath.Join(tmpDir, "include1")
	include2Path := filepath.Join(tmpDir, "include2")
	err := os.WriteFile(include1Path, []byte("Host included1\n  IdentityFile /included1/id_rsa"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(include2Path, []byte("Host included2\n  IdentityFile /included2/id_rsa"), 0644)
	require.NoError(t, err)

	// Main config content with include directives
	content := fmt.Sprintf(`
        Include %s
        Include %s

        Host default
            IdentityFile /.ssh/id_default
    `, include1Path, include2Path)

	parser, err := sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)

	testCases := []struct {
		hostName string
		identity string
	}{
		{"included1", "/included1/id_rsa"},
		{"included2", "/included2/id_rsa"},
		{"default", "/.ssh/id_default"},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.SSHConfig{}
			err = parser.Parse(obj, tc.hostName)
			require.NoError(t, err)

			require.Equal(t, tc.identity, obj.IdentityFile.String())
		})
	}
}

func TestParserIncludeRelativePaths(t *testing.T) {
	// Setup temporary directory for mock config files
	tmpDir := t.TempDir()

	// Mock global and user config directories
	globalConfigDir := filepath.Join(tmpDir, "etc", "ssh")
	userConfigDir := filepath.Join(tmpDir, "home", "user", ".ssh")
	err := os.MkdirAll(globalConfigDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(userConfigDir, 0755)
	require.NoError(t, err)

	// Mock include files with relative paths
	err = os.WriteFile(filepath.Join(globalConfigDir, "ssh_config"), []byte(`Include ssh_config.d/*`), 0644)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(globalConfigDir, "ssh_config.d"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(globalConfigDir, "ssh_config.d", "config1"), []byte("Host global\n  IdentityFile /global/id_rsa"), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(userConfigDir, "ssh_config"), []byte(`Include user_config.d/*`), 0644)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(userConfigDir, "user_config.d"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(userConfigDir, "user_config.d", "config1"), []byte("Host user\n  IdentityFile /user/id_rsa"), 0644)
	require.NoError(t, err)

	// Test with global and user config paths
	parser, err := sshconfig.NewParser(nil, sshconfig.WithGlobalConfigPath(filepath.Join(globalConfigDir, "ssh_config")), sshconfig.WithUserConfigPath(filepath.Join(userConfigDir, "ssh_config")))
	require.NoError(t, err)

	testCases := []struct {
		hostName string
		identity string
	}{
		{"global", "/global/id_rsa"},
		{"user", "/user/id_rsa"},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.SSHConfig{}
			err = parser.Parse(obj, tc.hostName)

			require.NoError(t, err)
			require.Equal(t, tc.identity, obj.IdentityFile.String())
		})
	}
}

func TestParserCircularInclude(t *testing.T) {
	// Setup temporary directory for mock config files
	tmpDir := t.TempDir()

	userConfigDir := filepath.Join(tmpDir, "home", "user", ".ssh")
	err := os.MkdirAll(userConfigDir, 0755)
	require.NoError(t, err)

	// Circular import setup
	err = os.WriteFile(filepath.Join(userConfigDir, "circular"), []byte(`Include circular`), 0644)
	require.NoError(t, err)

	// Test with global and user config paths
	_, err = sshconfig.NewParser(nil, sshconfig.WithUserConfigPath(filepath.Join(userConfigDir, "circular")))
	require.Error(t, err)
	require.ErrorContains(t, err, "circular include")
}

func TestParserEnvVarExpansion(t *testing.T) {
	// Setting a mock environment variable for the test
	t.Setenv("TEST_SSH_IDENTITY", "/mock/id_rsa")

	content := `
        Host example
            IdentityFile ${TEST_SSH_IDENTITY}
    `

	parser, err := sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)

	t.Run("IdentityFileEnvExpansion", func(t *testing.T) {
		obj := &sshconfig.SSHConfig{}
		err = parser.Parse(obj, "example")
		require.NoError(t, err)

		require.Equal(t, "/mock/id_rsa", obj.IdentityFile.String())
	})
}

func TestMatchConditions(t *testing.T) {
	content := `
        Match host "example.com" user "user1"
            IdentityFile ${TEST_SSH_IDENTITY}/id_example_com
            Port 2222

        Match host "example.net" exec "test -f /some/file"
            IdentityFile ${TEST_SSH_IDENTITY}/id_example_net
            Port 23

        Host *
            IdentityFile ${TEST_SSH_IDENTITY}/id_default
            Port 22
    `

	// Mocking an environment variable for the IdentityFile paths
	t.Setenv("TEST_SSH_IDENTITY", "/mock/ssh")

	// Create the parser instance with the test content
	me := &mockExecutor{}
	parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithExecutor(me))
	require.NoError(t, err)

	testCases := []struct {
		hostName string
		user     string
		execCmd  string
		identity string
		port     string
	}{
		{"example.com", "user1", "", "/mock/ssh/id_example_com", "2222"},
		{"example.net", "", "test -f /some/file", "/mock/ssh/id_example_net", "23"},
		{"otherhost", "", "", "/mock/ssh/id_default", "22"},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.SSHConfig{}
			obj.SetUser(tc.user)
			if tc.execCmd != "" {
				me.expect = map[string]bool{tc.execCmd: true}
			}
			me.received = []string{}

			err = parser.Parse(obj, tc.hostName)
			require.NoError(t, err)

			// Check if the parsed configuration matches the expected values
			require.Equal(t, tc.identity, obj.IdentityFile.String())
			require.Equal(t, tc.port, obj.Port.String())
			if tc.execCmd != "" {
				require.Contains(t, me.received, tc.execCmd)
			}
		})
	}
}

type mockExecutor struct {
	expect   map[string]bool
	received []string
}

func (m *mockExecutor) Run(cmd string, args ...string) error {
	stringified := fmt.Sprintf("%s %s", cmd, strings.Join(args, " "))
	m.received = append(m.received, stringified)
	if res, ok := m.expect[stringified]; ok {
		if res {
			return nil
		}
		return fmt.Errorf("command failed")
	}
	return fmt.Errorf("unexpected command")
}

func TestSyntaxError(t *testing.T) {
	_, err := sshconfig.NewParser(strings.NewReader("Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  UnknownOption\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, sshconfig.ErrSyntax)
}
