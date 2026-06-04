---
name: project_ssh_config_coverage
description: which sshconfig keys are wired in native SSH, which have todo cards, which are no-ops
metadata:
  type: project
---

The `sshconfig` package parses the full OpenSSH spec. What gets consumed depends on the protocol.

**OpenSSH mode** (`protocol/openssh`): does not use the parser at all — delegates to the system `ssh` binary which applies everything natively.

**Native SSH mode** (`protocol/ssh`): uses the parser but only wires a subset.

## Currently wired (7 keys)

`Hostname` (alias→address), `Port`, `User`, `IdentityFile`, `StrictHostKeyChecking`, `UserKnownHostsFile`, `HashKnownHosts`

## Todo cards created (in `todo/next/`)

| Card | Keys |
|---|---|
| `ssh-config-algorithm-fields` | `Ciphers`, `KexAlgorithms`, `MACs`, `HostKeyAlgorithms`, `ConnectTimeout`, `IdentitiesOnly` |
| `ssh-config-proxyjump` | `ProxyJump` → auto-populate `Bastion` |
| `ssh-config-global-known-hosts` | `GlobalKnownHostsFile` |
| `ssh-config-identity-agent` | `IdentityAgent` (custom agent socket) |
| `ssh-config-certificate-file` | `CertificateFile` (cert-based auth) |
| `ssh-config-host-key-alias` | `HostKeyAlias` |
| `ssh-config-rekey-limit` | `RekeyLimit` → `RekeyThreshold` |
| `ssh-config-server-alive` | `ServerAliveInterval`, `ServerAliveCountMax` |
| `ssh-config-pubkey-password-auth` | `PubkeyAuthentication`, `PasswordAuthentication` |
| `ssh-config-batch-mode` | `BatchMode` (suppress PasswordCallback for key decryption) |
| `ssh-config-address-family` | `AddressFamily` → tcp4/tcp6 in dialer |
| `ssh-config-bind-address` | `BindAddress`, `BindInterface` → `net.Dialer.LocalAddr` |
| `ssh-config-proxy-command` | `ProxyCommand` → exec+pipe shim as net.Conn |
| `ssh-config-check-host-ip` | `CheckHostIP` → also verify IP in known_hosts |

## Key implementation facts

- `sshconfig.Finalize()` only does token/env expansion — it does NOT pre-populate algorithm fields with OpenSSH defaults. So `sshConfig.Ciphers` is nil when the user set nothing. Copy-when-non-nil is safe.
- `preloadedDefaultsSetter` resolves `+`/`-`/`^` modifier prefixes against OpenSSH's own default lists. crypto/ssh silently drops algorithm names it doesn't implement.
- `net.Dialer{}` in Go 1.13+ enables TCP keepalive by default — `TCPKeepAlive` is already the right default, no card needed.
- Rig never adds `ssh.Password()` to `config.Auth` — it doesn't do password-as-SSH-auth. `PasswordAuthentication` is effectively a no-op.
- Port forwarding: `Connection.Dial()` is already exposed; consumers can build local forwarding on it. Wiring from sshconfig config is a new managed-goroutine API — not planned.
- `SendEnv` (`session.Setenv`): requires `AcceptEnv` on server; unreliable. Not planned.
- `Compression`: `golang.org/x/crypto/ssh` Config has no compression field. Not implementable.
- `GSSAPI*`: not supported by crypto/ssh. Not implementable.
- `Protocol` (SSHv1/v2), `Cipher` (singular), `CompressionLevel`: legacy/deprecated, no-op.

## Address-as-alias

`ssh.Config.Address` can be a Host alias from `~/.ssh/config`. After `Apply()`, if `sshConfig.Hostname != ""`, rig uses it as the real address and keeps the alias for display. Confirmed in `connection.go:137-139`.
