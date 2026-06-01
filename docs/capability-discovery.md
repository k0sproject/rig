# Capability Discovery

`Client.Capabilities()` returns a snapshot of everything rig can detect about a connected
remote host: OS, filesystem, package manager, init system, sudo access, and protocol features.

## Quick start

```go
if err := client.Connect(ctx); err != nil {
    log.Fatal(err)
}

caps := client.Capabilities()
fmt.Println(caps)
// protocol=SSH os="Alpine Linux" fs=available package-manager=apk init-system=openrc sudo=yes interactive-exec=yes
```

## The Capabilities struct

| Field | Type | Description |
|---|---|---|
| `Protocol` | `string` | Connection protocol name (`"SSH"`, `"OpenSSH"`, `"WinRM"`, `"Local"`) |
| `OS` | `*os.Release` | Detected OS release info, or nil on failure |
| `OSErr` | `error` | OS detection error |
| `RemoteFS` | `bool` | True if the remote filesystem is available via `client.FS()` |
| `RemoteFSErr` | `error` | Remote filesystem init error; nil means `client.FS()` is usable |
| `PackageManager` | `string` | Detected package manager name (e.g. `"apt"`, `"yum"`, `"dnf"`, `"apk"`) |
| `PackageManagerErr` | `error` | Package manager detection error |
| `InitSystem` | `string` | Detected init system name (e.g. `"systemd"`, `"openrc"`, `"launchd"`) |
| `InitSystemErr` | `error` | Init system detection error |
| `Sudo` | `bool` | True if `client.Sudo()` will work — either already root/admin or sudo/doas detected |
| `SudoErr` | `error` | Sudo detection error |
| `InteractiveExec` | `bool` | True if the connection supports `ExecInteractive` |

`Capabilities.String()` returns a one-line human-readable summary for logging.

## Caching and remote commands

Detection is lazy and memoized. The table below shows when each probe first runs a
remote command, and what triggers it.

| Capability | Runs remote commands | When first probed |
|---|---|---|
| `OS` | Yes — reads `/etc/os-release` (Linux), `sw_vers` (macOS), or CIM query via PowerShell (Windows) | First call to `client.OS()` or `client.Capabilities()` |
| `RemoteFS` | No — selects a filesystem implementation locally (POSIX or Windows); remote commands occur only on the first filesystem operation | First call to `client.FS()` or `client.Capabilities()` |
| `PackageManager` | Yes — probes for package manager binaries | First call to `client.PackageManager()` or `client.Capabilities()` |
| `InitSystem` | Yes — probes for init system binaries/processes | First call to `client.ServiceManager()` or `client.Capabilities()` |
| `Sudo` | Yes — tests the preferred escalation method (`sudo`, `doas`, etc.) | First call to `client.Sudo()` or `client.Capabilities()` |
| `InteractiveExec` | No — local interface check on the connection object | Every call (free) |
| `Protocol` | No — returns cached value from the connection object | Every call (free) |

Once a probe has run, subsequent `Capabilities()` calls return the memoized result
for that field with no additional remote commands. Interleaved direct accessor calls
(e.g. `client.OS()` followed by `client.Capabilities()`) share the same cache — OS
is only resolved once regardless of call order.

## Partial availability

Every capability has an independent error field. A failure to detect one capability
(e.g. the remote host has no known package manager) does not affect the others.
You can check individual errors:

```go
caps := client.Capabilities()

if caps.OSErr != nil {
    log.Printf("OS detection failed: %v", caps.OSErr)
}

if caps.PackageManagerErr != nil {
    log.Printf("no supported package manager: %v", caps.PackageManagerErr)
}

if !caps.Sudo {
    log.Printf("sudo unavailable — some operations may require elevated access")
}
```

## Protocol optional features

`InteractiveExec` reflects whether the underlying protocol connection implements
`protocol.InteractiveExecer`. This controls whether `client.ExecInteractive` can be
used for interactive sessions (e.g. shells, editors).

Built-in protocol support:

| Protocol | `InteractiveExec` |
|---|---|
| SSH (native) | Yes |
| OpenSSH | Yes |
| WinRM | Yes |
| Local | Yes |

Custom protocol implementations can opt in by implementing `protocol.InteractiveExecer`.

## Detected init system names

| Name | Init system |
|---|---|
| `"systemd"` | systemd (most Linux distributions) |
| `"openrc"` | OpenRC (Alpine Linux, Gentoo) |
| `"launchd"` | launchd (macOS) |
| `"upstart"` | Upstart (Ubuntu 14.04 and older) |
| `"sysvinit"` | SysVinit (legacy Linux) |
| `"runit"` | runit |
| `"winscm"` | Windows SCM |

## Detected package manager names

| Name | Package manager |
|---|---|
| `"apt"` | APT (Debian, Ubuntu) |
| `"dnf"` | DNF (Fedora, RHEL 8+) |
| `"yum"` | YUM (RHEL 7 and older, CentOS) |
| `"apk"` | APK (Alpine Linux) |
| `"pacman"` | Pacman (Arch Linux) |
| `"zypper"` | Zypper (openSUSE) |
| `"homebrew"` | Homebrew (macOS) |
| `"macports"` | MacPorts (macOS) |
| `"chocolatey"` | Chocolatey (Windows) |
| `"winget"` | WinGet (Windows) |
| `"scoop"` | Scoop (Windows) |
| `"windows-multi"` | Windows multi-manager (tries all detected Windows managers) |
