# SSH Configuration Precedence

When the pure-Go SSH protocol establishes a connection it builds its effective
configuration from four sources, in decreasing priority order:

1. **Native fields** (`address`, `user`, `keyPath`, …)
   Strongly-typed fields on `ssh.Config`. Once set to a non-empty/non-zero
   value these win over all other sources.

2. **`options`** (`ssh.Config.SSHConfigOptions`, YAML key `options`)
   A map of raw ssh_config directives, e.g. `{"Ciphers": "aes128-ctr"}`.
   Applied after native fields; fills gaps they leave.

3. **`~/.ssh/config`** and the system-wide `ssh_config` (read by
   `ssh.ConfigParser` in `protocol/ssh` when enabled)
   Fills whatever the two sources above did not set. Can be skipped entirely
   — see [Skipping the ssh config files](#skipping-the-ssh-config-files).

4. **OpenSSH built-in defaults**
   The compiled-in defaults OpenSSH itself would use (`IdentityFile`,
   `UserKnownHostsFile`, `StrictHostKeyChecking`, `Port`, cipher lists, …).
   Applied last of all, and kept by `ignoreSSHConfig` and when the config
   files turn out to be unparseable. They come from the same parser as layer
   3, so the one way to lose them is assigning nil to `ssh.ConfigParser` in
   Go, which turns the whole ssh config layer off.

## Example

```yaml
ssh:
  address: 192.0.2.1
  user: deploy          # native field — highest priority
  options:
    ServerAliveInterval: 30   # fills a gap; no native equivalent
    Ciphers: aes128-ctr
  # ~/.ssh/config may supply IdentityFile, KnownHostsFile, etc.
  # for this host if not already provided above
```

## Native field defaults and port handling

Several native fields carry Go struct defaults applied before precedence is
evaluated (e.g. `user` defaults to `root`). Once defaults are applied the
field is no longer empty, so it wins over `options` and `~/.ssh/config`
just like an explicitly set value would.

`port` is a special case: a port of `22` (the SSH default, whether explicit
or filled in by the Go zero value) is intentionally treated as "not
explicitly set" and does **not** propagate into the ssh_config layer. This
means `~/.ssh/config` can still supply a per-host port when the rig
configuration leaves `port` at `22` or omits it entirely. Set `port` to any
other value to have it take precedence over `~/.ssh/config`.

## Skipping the ssh config files

This applies to the pure-Go `ssh:` protocol only. For `openssh:` see
[OpenSSH protocol](#openssh-protocol) below.

Set `ignoreSSHConfig` (Go: `ssh.Config.IgnoreSSHConfig`, or the
`ssh.WithoutSSHConfig()` option) to stop rig from reading `~/.ssh/config` and
the system-wide `ssh_config` for a host. This mirrors `ssh -F none`.

```yaml
ssh:
  address: 192.0.2.1
  user: deploy
  ignoreSSHConfig: true
```

Only layer 3 above is dropped. Native fields, `options` and OpenSSH's built-in
defaults still apply, so the connection keeps working defaults for identity
files, `known_hosts`, host key checking and algorithm lists.

Use it when a config file on the machine running rig cannot be parsed, or
contains directives that should not influence rig — for example an unrelated
`Include` file dropped in by corporate IT or a VPN helper. Without the opt-out
a parse failure anywhere in the evaluated configuration aborts every
connection, even for hosts the offending stanza does not match.

The setting is inherited by bastion connections, including a bastion derived
from `ProxyJump`.

## OpenSSH protocol

The OpenSSH (`protocol/openssh`) variant passes options directly to the
`ssh` binary as `-o Key=Value` flags, so the precedence rules are those of
OpenSSH itself. The `options` YAML key is the same (`openssh.Config.Options`).

Because rig never parses the config files for this protocol, `ignoreSSHConfig`
does not exist on `openssh.Config`. OpenSSH's own switch covers it: `configPath`
(`openssh.Config.ConfigPath`) becomes `ssh -F <path>`, so `none` reads no
configuration files at all.

```yaml
openssh:
  address: 192.0.2.1
  configPath: none
```
