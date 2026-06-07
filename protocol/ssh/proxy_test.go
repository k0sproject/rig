package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestMain handles the helper-proxy subprocess invoked by TestConnectViaProxyCommand.
// When RIG_TEST_SSH_PROXY=1 the binary dials RIG_TEST_SSH_PROXY_DEST and bridges stdin/stdout.
func TestMain(m *testing.M) {
	if os.Getenv("RIG_TEST_SSH_PROXY") == "1" {
		dest := os.Getenv("RIG_TEST_SSH_PROXY_DEST")
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).Dial("tcp", dest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper proxy: dial %s: %v\n", dest, err)
			os.Exit(1)
		}
		defer conn.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			io.Copy(conn, os.Stdin) //nolint:errcheck
		}()
		io.Copy(os.Stdout, conn) //nolint:errcheck
		<-done
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startMinimalSSHServer starts a minimal SSH server with NoClientAuth (accepts
// connections without credentials), serves no channels, and returns its listener
// address plus the host signer so tests can pin the host key.
func startMinimalSSHServer(t *testing.T) (addr string, hostSigner ssh.Signer) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostSigner, err = ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	cfg := &ssh.ServerConfig{
		NoClientAuth:  true,
		// "linux" in the version string causes isKnownPosix() to return true,
		// short-circuiting the ver.exe probe in detectWindows and avoiding a
		// reconnect loop that would block the test for the full context duration.
		ServerVersion: "SSH-2.0-test-linux",
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, lErr := ln.Accept()
			if lErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sconn, chans, reqs, hsErr := ssh.NewServerConn(c, cfg)
				if hsErr != nil {
					return
				}
				defer sconn.Close()
				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					newChan.Reject(ssh.UnknownChannelType, "not supported") //nolint:errcheck
				}
			}(conn)
		}
	}()

	return ln.Addr().String(), hostSigner
}

// TestParseProxyJump exercises the ProxyJump parser for various formats.
func TestParseProxyJump(t *testing.T) {
	cases := []struct {
		name        string
		jump        string
		defaultUser string
		wantAddr    string
		wantUser    string
		wantPort    int
		wantErr     bool
	}{
		{
			name:        "host only",
			jump:        "example.com",
			defaultUser: "alice",
			wantAddr:    "example.com",
			wantUser:    "alice",
			wantPort:    22,
		},
		{
			name:        "host:port",
			jump:        "example.com:2222",
			defaultUser: "alice",
			wantAddr:    "example.com",
			wantUser:    "alice",
			wantPort:    2222,
		},
		{
			name:        "user@host",
			jump:        "bob@example.com",
			defaultUser: "alice",
			wantAddr:    "example.com",
			wantUser:    "bob",
			wantPort:    22,
		},
		{
			name:        "user@host:port",
			jump:        "bob@example.com:2222",
			defaultUser: "alice",
			wantAddr:    "example.com",
			wantUser:    "bob",
			wantPort:    2222,
		},
		{
			name:        "ssh:// URI",
			jump:        "ssh://bob@example.com:2222",
			defaultUser: "alice",
			wantAddr:    "example.com",
			wantUser:    "bob",
			wantPort:    2222,
		},
		{
			name:        "ssh:// URI host only",
			jump:        "ssh://example.com",
			defaultUser: "alice",
			wantAddr:    "example.com",
			wantUser:    "alice",
			wantPort:    22,
		},
		{
			name:        "default user falls back to root when empty",
			jump:        "example.com",
			defaultUser: "",
			wantAddr:    "example.com",
			wantUser:    "root",
			wantPort:    22,
		},
		{
			name:        "IPv4",
			jump:        "192.0.2.1",
			defaultUser: "u",
			wantAddr:    "192.0.2.1",
			wantUser:    "u",
			wantPort:    22,
		},
		{
			name:        "IPv4 with port",
			jump:        "192.0.2.1:2222",
			defaultUser: "u",
			wantAddr:    "192.0.2.1",
			wantUser:    "u",
			wantPort:    2222,
		},
		{
			name:        "bracketed IPv6 with port",
			jump:        "[::1]:2222",
			defaultUser: "u",
			wantAddr:    "::1",
			wantUser:    "u",
			wantPort:    2222,
		},
		{
			name:    "empty string",
			jump:    "",
			wantErr: true,
		},
		{
			name:    "invalid port",
			jump:    "example.com:notaport",
			wantErr: true,
		},
		{
			name:        "bracketed IPv6 without port strips brackets",
			jump:        "[::1]",
			defaultUser: "u",
			wantAddr:    "::1",
			wantUser:    "u",
			wantPort:    22,
		},
		{
			name:    "trailing colon rejected",
			jump:    "example.com:",
			wantErr: true,
		},
		{
			name:    "unbracketed IPv6 rejected",
			jump:    "::1",
			wantErr: true,
		},
		{
			name:    "unbracketed IPv6 with port rejected",
			jump:    "::1:22",
			wantErr: true,
		},
		{
			name:    "malformed bracketed IPv6 missing closing bracket rejected",
			jump:    "[::1",
			wantErr: true,
		},
		{
			name:    "port zero rejected",
			jump:    "example.com:0",
			wantErr: true,
		},
		{
			name:    "port too large rejected",
			jump:    "example.com:65536",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseProxyJump(tc.jump, tc.defaultUser)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantAddr, cfg.Address)
			assert.Equal(t, tc.wantUser, cfg.User)
			assert.Equal(t, tc.wantPort, cfg.Port)
		})
	}
}

// TestNewConnectionProxyJumpSetsBastion verifies that ProxyJump in sshconfig
// populates Config.Bastion when no explicit Bastion is configured.
func TestNewConnectionProxyJumpSetsBastion(t *testing.T) {
	withConfigParser(t, "Host target\n  ProxyJump jump.example.com\n")
	t.Setenv("SSH_KNOWN_HOSTS", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	c, err := NewConnection(Config{Address: "target", User: "alice", AuthMethods: []ssh.AuthMethod{ssh.Password("x")}})
	require.NoError(t, err)
	require.NotNil(t, c.Config.Bastion, "ProxyJump must populate Config.Bastion")
	assert.Equal(t, "jump.example.com", c.Config.Bastion.Address)
	assert.Equal(t, "alice", c.Config.Bastion.User)
	assert.Equal(t, 22, c.Config.Bastion.Port)
}

// TestNewConnectionProxyJumpNoneIgnored verifies that ProxyJump "none" is a no-op.
func TestNewConnectionProxyJumpNoneIgnored(t *testing.T) {
	withConfigParser(t, "Host target\n  ProxyJump none\n")
	t.Setenv("SSH_KNOWN_HOSTS", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	c, err := NewConnection(Config{Address: "target", User: "alice", AuthMethods: []ssh.AuthMethod{ssh.Password("x")}})
	require.NoError(t, err)
	assert.Nil(t, c.Config.Bastion, "ProxyJump none must not set a Bastion")
}

// TestNewConnectionExplicitBastionWinsOverProxyJump verifies that an explicit
// Bastion in Config takes precedence over the sshconfig ProxyJump value.
func TestNewConnectionExplicitBastionWinsOverProxyJump(t *testing.T) {
	withConfigParser(t, "Host target\n  ProxyJump ssh-config-bastion.example.com\n")
	t.Setenv("SSH_KNOWN_HOSTS", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	explicit := &Config{Address: "explicit.example.com", User: "alice", Port: 22}
	c, err := NewConnection(Config{
		Address:     "target",
		User:        "alice",
		Bastion:     explicit,
		AuthMethods: []ssh.AuthMethod{ssh.Password("x")},
	})
	require.NoError(t, err)
	require.NotNil(t, c.Config.Bastion)
	assert.Equal(t, "explicit.example.com", c.Config.Bastion.Address, "explicit Bastion must not be overwritten by ProxyJump")
}

// TestConnectProxyCommandPrecedence verifies that ProxyCommand wins over a
// ProxyJump-populated bastion.  The ProxyCommand here points to a fast-failing
// binary so we can confirm the ProxyCommand path is entered without a real
// SSH server.
func TestConnectProxyCommandPrecedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-specific ProxyCommand values (false); not portable to Windows")
	}

	withConfigParser(t, "Host target\n  ProxyJump jump.example.com\n  ProxyCommand false\n")
	t.Setenv("SSH_KNOWN_HOSTS", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	c, err := NewConnection(Config{Address: "target", User: "alice", AuthMethods: []ssh.AuthMethod{ssh.Password("x")}})
	require.NoError(t, err)
	// ProxyJump was wired as a bastion...
	require.NotNil(t, c.Config.Bastion, "ProxyJump should still populate Bastion")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connectErr := c.Connect(ctx)
	// The error must come from the ProxyCommand path ("proxy command"), not the
	// bastion path ("bastion dial" / "bastion connect").
	require.Error(t, connectErr)
	assert.Contains(t, connectErr.Error(), "proxy command", "error should originate from the ProxyCommand path")
	assert.NotContains(t, connectErr.Error(), "bastion", "bastion path must not be entered when ProxyCommand is set")
}

// TestConnectExplicitBastionBeatsProxyCommand verifies that an explicit Config.Bastion
// takes precedence over a ProxyCommand from sshconfig.
func TestConnectExplicitBastionBeatsProxyCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-specific ProxyCommand values (false); not portable to Windows")
	}

	withConfigParser(t, "Host target\n  ProxyCommand false\n")
	t.Setenv("SSH_KNOWN_HOSTS", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	explicit := &Config{Address: "explicit.example.com", User: "alice", Port: 22}
	c, err := NewConnection(Config{
		Address:     "target",
		User:        "alice",
		Bastion:     explicit,
		AuthMethods: []ssh.AuthMethod{ssh.Password("x")},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connectErr := c.Connect(ctx)
	// The explicit bastion path is entered, not the ProxyCommand path.
	require.Error(t, connectErr)
	assert.NotContains(t, connectErr.Error(), "proxy command", "ProxyCommand must not be used when an explicit Bastion is configured")
}

// TestConnectViaProxyCommand performs an end-to-end Connect via a ProxyCommand
// subprocess. The ProxyCommand re-invokes the test binary in helper-proxy mode,
// which dials the in-process SSH server and bridges stdin/stdout.
func TestConnectViaProxyCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-specific shell commands and quoting; not portable to Windows")
	}

	// Isolate from the developer's ~/.ssh/config so ProxyCommand is the only
	// connection path and no extra options can interfere.
	withConfigParser(t, "")
	t.Setenv("SSH_AUTH_SOCK", "")

	serverAddr, hostSigner := startMinimalSSHServer(t)

	// Parse the actual server port so the connection's dst matches the
	// known_hosts entry and host-key verification is exercised, not bypassed.
	serverHost, serverPortStr, err := net.SplitHostPort(serverAddr)
	require.NoError(t, err)
	serverPort, err := strconv.Atoi(serverPortStr)
	require.NoError(t, err)

	// Pin the host key: MarshalAuthorizedKey already includes the key type.
	// knownhosts.Normalize converts "host:port" to "[host]:port" for non-22 ports.
	hostPub := hostSigner.PublicKey()
	normalizedAddr := knownhosts.Normalize(serverAddr)
	knownHostsLine := fmt.Sprintf("%s %s\n",
		normalizedAddr,
		strings.TrimRight(string(ssh.MarshalAuthorizedKey(hostPub)), "\n"),
	)
	khFile := t.TempDir() + "/known_hosts"
	require.NoError(t, os.WriteFile(khFile, []byte(knownHostsLine), 0o600))
	t.Setenv("SSH_KNOWN_HOSTS", khFile)

	// Build ProxyCommand: re-invoke this test binary in helper-proxy mode.
	// All env vars are passed via t.Setenv so the child inherits them — inline
	// shell assignments (VAR=val cmd) are incompatible with the exec prefix that
	// connectViaProxyCommand adds to eliminate the shell wrapper process.
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("PROXY_CMD_EXE", exe)
	t.Setenv("RIG_TEST_SSH_PROXY", "1")
	t.Setenv("RIG_TEST_SSH_PROXY_DEST", serverAddr)
	proxyCmd := `"$PROXY_CMD_EXE" -test.run='^$'`

	conn, err := NewConnection(Config{
		Address:     serverHost,
		Port:        serverPort,
		User:        "test",
		AuthMethods: []ssh.AuthMethod{ssh.Password("any")},
	})
	require.NoError(t, err)
	conn.sshConfig.ProxyCommand = proxyCmd
	t.Cleanup(conn.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, conn.Connect(ctx))
}
