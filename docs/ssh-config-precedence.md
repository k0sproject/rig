# SSH Configuration Precedence

When the pure-Go SSH protocol establishes a connection it builds its effective
configuration from three sources, in decreasing priority order:

1. **Native fields** (`address`, `user`, `keyPath`, …)
   Strongly-typed fields on `ssh.Config`. Once set to a non-empty/non-zero
   value these win over all other sources.

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

## OpenSSH protocol

The OpenSSH (`protocol/openssh`) variant passes options directly to the
`ssh` binary as `-o Key=Value` flags, so the precedence rules are those of
OpenSSH itself. The `options` YAML key is the same (`openssh.Config.Options`).
