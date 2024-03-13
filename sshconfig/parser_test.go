package sshconfig_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/stretchr/testify/require"
)

func Example_simple() {
	hostconfig, err := sshconfig.ConfigFor("example")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(hostconfig.Port)
	// Output: 22
}

func Example() {
	// this will read the configurations from the default locations.
	parser, err := sshconfig.NewParser(nil)

	if err != nil {
		log.Fatal(err)
	}
	host := &sshconfig.Config{}
	if err := parser.Apply(host, "example.com"); err != nil {
		log.Fatal(err)
	}
}

// To read from a specific file or a string instead of the default files, pass in an io.Reader:
func Example_configFromReader() {
	parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\nIdentityFile id_example\n"))
	if err != nil {
		log.Fatal(err)
	}
	host := &sshconfig.Config{}
	if err := parser.Apply(host, "example.com"); err != nil {
		log.Fatal(err)
	}
	for _, identityFile := range host.IdentityFile {
		fmt.Println(identityFile)
	}
	// Output: id_example
}

// You can also use the parser to apply configuration values into your own custom Config object.
func Example_withCustomConfigObject() {
	type customConfig struct {
		Host         string
		IdentityFile []string
	}

	// To read from a specific file or a string instead of the default files, pass in an io.Reader:
	parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\nIdentityFile id_example\n"))
	if err != nil {
		log.Fatal(err)
	}
	host := &customConfig{}
	if err := parser.Apply(host, "example.com"); err != nil {
		log.Fatal(err)
	}
	fmt.Println(host.IdentityFile[0])
	// Output: id_example
}

func TestParse(t *testing.T) {
	config := sshconfig.Config{}
	parser, err := sshconfig.NewParser(nil)
	require.NoError(t, err)
	err = parser.Apply(&config, "example.com")
	require.NoError(t, err)
}

func TestParseIgnoreUnknown(t *testing.T) {
	content := "Host example.com\n  IdentityFile ~/.ssh/id_rsa\n  UnknownOption yes\n"
	parser, err := sshconfig.NewParser(strings.NewReader(content))
	obj := &sshconfig.Config{}
	t.Run("ErrorOnUnknown false", func(t *testing.T) {
		t.Run("IgnoreUnknown not set", func(t *testing.T) {
			obj.IgnoreUnknown = nil
			require.NoError(t, err)
			require.NoError(t, parser.Apply(obj, "example.com"))
		})
		t.Run("IgnoreUnknown set", func(t *testing.T) {
			obj := &sshconfig.Config{}
			obj.IgnoreUnknown = []string{"unknown*"}
			parser, err := sshconfig.NewParser(strings.NewReader(content))
			require.NoError(t, err)
			require.NoError(t, parser.Apply(obj, "example.com"))
		})
	})
	t.Run("ErrorOnUnknown true", func(t *testing.T) {
		t.Run("IgnoreUnknown not set", func(t *testing.T) {
			obj := &sshconfig.Config{}
			obj.IgnoreUnknown = nil
			parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithStrict())
			require.NoError(t, err)
			require.ErrorContains(t, parser.Apply(obj, "example.com"), "unknown key")
		})
		t.Run("IgnoreUnknown set", func(t *testing.T) {
			obj := &sshconfig.Config{}
			obj.IgnoreUnknown = []string{"unknown*"}
			parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithStrict())
			require.NoError(t, err)
			require.NoError(t, parser.Apply(obj, "example.com"))
		})
	})
}

func TestParsePrecedence(t *testing.T) {
	ucReader := strings.NewReader("GlobalKnownHostsFile /dev/null\nStrictHostKeyChecking no\nHost example.com\nSendEnv TERM\nStrictHostKeyChecking ask\n")
	gcReader := strings.NewReader("GlobalKnownHostsFile /foo/ssh/ssh_known_hosts\nSendEnv LANG\nStrictHostKeyChecking yes\n")
	obj := &sshconfig.Config{}
	parser, err := sshconfig.NewParser(nil, sshconfig.WithGlobalConfigReader(gcReader), sshconfig.WithUserConfigReader(ucReader))
	require.NoError(t, err)
	err = parser.Apply(obj, "example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"/dev/null"}, obj.GlobalKnownHostsFile, "User config should be loaded first and the value should stick")
	require.Equal(t, []string{"TERM", "LANG"}, obj.SendEnv, "SendEnv is always appending")
	require.Equal(t, "no", obj.StrictHostKeyChecking.String(), "The first occurence was in user config and it should stick")
}

func TestHostAndMatchBlocks(t *testing.T) {
	content := `
        Host example.com
            IdentityFile ~/.ssh/id_example
            Port 23

        Host *.example.net
            IdentityFile ~/.ssh/id_example_net
            Port 2222

        Match host="some.random.example,specific.example.net"
            IdentityFile ~/.ssh/id_specific
            Port 2200

        Host *
            IdentityFile ~/.ssh/id_default
            Port 22
    `

	parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithUserHome("/tmp"))
	require.NoError(t, err)

	exampleCom := &sshconfig.Config{}
	require.NoError(t, parser.Apply(exampleCom, "example.com"))
	require.Equal(t, []string{"/tmp/.ssh/id_example", "/tmp/.ssh/id_default"}, exampleCom.IdentityFile)
	require.Equal(t, 23, exampleCom.Port)

	randomExampleNet := &sshconfig.Config{}
	require.NoError(t, parser.Apply(randomExampleNet, "random.example.net"))
	require.Equal(t, []string{"/tmp/.ssh/id_example_net", "/tmp/.ssh/id_default"}, randomExampleNet.IdentityFile)
	require.Equal(t, 2222, randomExampleNet.Port)

	specificExampleNet := &sshconfig.Config{}
	require.NoError(t, parser.Apply(specificExampleNet, "specific.example.net"))
	require.Equal(t, []string{"/tmp/.ssh/id_example_net", "/tmp/.ssh/id_specific", "/tmp/.ssh/id_default"}, specificExampleNet.IdentityFile)
	require.Equal(t, 2222, specificExampleNet.Port)

	fooExampleCom := &sshconfig.Config{}
	require.NoError(t, parser.Apply(fooExampleCom, "foo.example.com"))
	require.Equal(t, []string{"/tmp/.ssh/id_default"}, fooExampleCom.IdentityFile)
	require.Equal(t, 22, fooExampleCom.Port)
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
	err = os.WriteFile(filepath.Join(globalConfigDir, "ssh_config.d", "config1"), []byte("Host global\n  UserKnownHostsFile /global/known"), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(userConfigDir, "ssh_config"), []byte(`Include user_config.d/*`), 0644)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(userConfigDir, "user_config.d"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(userConfigDir, "user_config.d", "config1"), []byte("Host user\n  UserKnownHostsFile /user/known"), 0644)
	require.NoError(t, err)

	// Test with global and user config paths
	parser, err := sshconfig.NewParser(nil, sshconfig.WithGlobalConfigPath(filepath.Join(globalConfigDir, "ssh_config")), sshconfig.WithUserConfigPath(filepath.Join(userConfigDir, "ssh_config")))
	require.NoError(t, err)

	testCases := []struct {
		hostName   string
		knownhosts string
	}{
		{"global", "/global/known"},
		{"user", "/user/known"},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.Config{}
			err = parser.Apply(obj, tc.hostName)

			require.NoError(t, err)
			require.Equal(t, tc.knownhosts, strings.Join(obj.UserKnownHostsFile, ","))
		})
	}
}

func TestIncludeDirectives(t *testing.T) {
	// Setup temporary directory for mock config files
	tmpDir := t.TempDir()

	// Mock include files with distinct settings
	include1Path := filepath.Join(tmpDir, "include1")
	include2Path := filepath.Join(tmpDir, "include2")
	err := os.WriteFile(include1Path, []byte("Host included1\n  UserKnownHostsFile /included1/known"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(include2Path, []byte("Host included2\n  UserKnownHostsFile /included2/known"), 0644)
	require.NoError(t, err)

	// Main config content with include directives
	content := fmt.Sprintf(`
        Include %s
        Include %s

        Host default
            UserKnownHostsFile /.ssh/known_default
    `, include1Path, include2Path)

	parser, err := sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)

	testCases := []struct {
		hostName   string
		knownhosts string
	}{
		{"included1", "/included1/known"},
		{"included2", "/included2/known"},
		{"default", "/.ssh/known_default"},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.Config{}
			err = parser.Apply(obj, tc.hostName)
			require.NoError(t, err)

			require.Equal(t, tc.knownhosts, strings.Join(obj.UserKnownHostsFile, ","))
		})
	}
}

func TestPatternMatchingAndNegation(t *testing.T) {
	content := `
        Host *.example.com
            UserKnownHostsFile ~/.ssh/wildcard

        Host ??.example.net
            UserKnownHostsFile ~/.ssh/question

        Host !forbidden.example.com
            UserKnownHostsFile ~/.ssh/negation

        Host example.*
            UserKnownHostsFile ~/.ssh/domain

        Host *
            UserKnownHostsFile ~/.ssh/known_default
    `

	for _, win := range []bool{true, false} {
		t.Run(fmt.Sprintf("windows_lf=%v", win), func(t *testing.T) {
			if win {
				content = strings.ReplaceAll(content, "\n", "\r\n")
			}
			parser, err := sshconfig.NewParser(strings.NewReader(content))
			require.NoError(t, err)

			testCases := []struct {
				hostName   string
				knownhosts string
			}{
				{"test.example.com", "/.ssh/wildcard"},
				{"aa.example.net", "/.ssh/question"},
				{"acceptable.example.com", "/.ssh/wildcard"},
				{"example.org", "/.ssh/domain"},
				{"forbidden.example.com", "/.ssh/wildcard"},
				{"randomhost", "/.ssh/known_default"},
			}

			for _, tc := range testCases {
				t.Run(tc.hostName, func(t *testing.T) {
					obj := &sshconfig.Config{}
					err = parser.Apply(obj, tc.hostName)
					require.NoError(t, err)
					require.True(t, strings.HasSuffix(strings.Join(obj.UserKnownHostsFile, ","), tc.knownhosts), "UserKnownHostsFile should end with %s but is %s", tc.knownhosts, obj.UserKnownHostsFile)
					require.False(t, strings.HasPrefix(strings.Join(obj.UserKnownHostsFile, ","), "~"), "tilde paths should be expanded, but got %s", obj.UserKnownHostsFile)
				})
			}
		})
	}
}

func TestMatchHostHostname(t *testing.T) {
	content := `
    Host example.com
      Hostname foo.example.com
    Match host=example.com
      Port 2021
    Match host=foo.example.com
      Port 2022
	`

	parser, err := sshconfig.NewParser(strings.NewReader(content))
	require.NoError(t, err)

	obj := &sshconfig.Config{}
	require.NoError(t, parser.Apply(obj, "example.com"))
	require.Equal(t, 2022, obj.Port, "host rule in Match should match 'hostname' value once defined")
}

func TestMatchConditions(t *testing.T) {
	content := `
        Match host="example.com" user="user1"
            UserKnownHostsFile ${TEST_SSH_KNOWN}/example_com
            Port 2222

        Match host="example.net" exec="test -f /some/file"
            UserKnownHostsFile ${TEST_SSH_KNOWN}/example_net
            Port 23

        Host *
            UserKnownHostsFile ${TEST_SSH_KNOWN}/default
            Port 22
    `

	// Mocking an environment variable for the UserKnownHostsFile paths
	t.Setenv("TEST_SSH_KNOWN", "/mock/ssh")

	me := &mockExecutor{}
	parser, err := sshconfig.NewParser(strings.NewReader(content), sshconfig.WithExecutor(me))
	require.NoError(t, err)

	testCases := []struct {
		hostName string
		user     string
		execCmd  string
		known    string
		port     int
	}{
		{"example.com", "user1", "", "/mock/ssh/example_com", 2222},
		{"example.net", "", "test -f /some/file", "/mock/ssh/example_net", 23},
		{"otherhost", "", "", "/mock/ssh/default", 22},
	}

	for _, tc := range testCases {
		t.Run(tc.hostName, func(t *testing.T) {
			obj := &sshconfig.Config{}
			obj.User = tc.user
			if tc.execCmd != "" {
				me.expect = map[string]bool{tc.execCmd: true}
			}
			me.received = []string{}

			err = parser.Apply(obj, tc.hostName)
			require.NoError(t, err)

			require.Equal(t, tc.known, strings.Join(obj.UserKnownHostsFile, ","))
			require.Equal(t, tc.port, obj.Port)
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
	_, err := sshconfig.NewParser(strings.NewReader("Host example.com\n  UserKnownHostsFile ~/.ssh/id_known\n  UnknownOption\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, sshconfig.ErrSyntax)
}
