# SSH Configuration Precedence

When the pure-Go SSH protocol establishes a connection it builds its effective
configuration from three sources, in decreasing priority order:

1. **Native fields** (`address`, `user`, `port`, `keyPath`, …)
   Strongly-typed fields on `ssh.Config`. These always win.

2. **`options`** (`ssh.Config.SSHConfigOptions`, YAML key `options`)
   A map of raw ssh_config directives, e.g. `{"Ciphers": "aes128-ctr"}`.
   Applied after native fields; fills gaps they leave.

3. **`~/.ssh/config`** (read by `sshconfig.ConfigParser` when enabled)
   Applied last, filling whatever the two sources above did not set.

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

## Native field defaults

Native fields carry Go struct defaults (e.g. `user` defaults to `root`,
`port` defaults to `22`). Because native fields have the highest priority,
these defaults also win over `options` and `~/.ssh/config`. If you need
`~/.ssh/config` or `options` to control a value that has a native
equivalent, set the native field explicitly to its zero/empty value in your
configuration.

## OpenSSH protocol

The OpenSSH (`protocol/openssh`) variant passes options directly to the
`ssh` binary as `-o Key=Value` flags, so the precedence rules are those of
OpenSSH itself. The `options` YAML key is the same (`openssh.Config.Options`).
