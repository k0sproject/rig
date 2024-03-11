**Note:** The main branch contains a work in progress for rig v2. If you are looking for the stable version, please browse the tags of v0.x releases.

### Rig

[![GoDoc](https://godoc.org/github.com/k0sproject/rig?status.svg)](https://godoc.org/github.com/k0sproject/rig)
[![Go Report Card](https://goreportcard.com/badge/github.com/k0sproject/rig)](https://goreportcard.com/report/github.com/k0sproject/rig)
[![Build Status](https://travis-ci.com/k0sproject/rig.svg?branch=main)](https://travis-ci.com/k0sproject/rig)
[![codecov](https://codecov.io/gh/k0sproject/rig/branch/main/graph/badge.svg)](https://codecov.io/gh/k0sproject/rig)

<img src=".github/logo.webp" alt="Rig" width="200" align="left"/>

A golang package for adding multi-protocol connectivity and multi-os operation functionality to your application's Host objects.

#### Design goals

Rig's intention is to be easy to use and extend.

It should be easy to add support for new operating systems and to add new components to the multi-os support mechanism without breaking type safety and testability.

#### Protocols

Currently rig comes with the most common ways to connect to hosts:

- SSH for connecting to hosts that accept SSH connections. With ssh agent and config support and sane familiar defaults. Pageant
or [openssh agent](https://docs.microsoft.com/en-us/windows-server/administration/openssh/openssh_install_firstuse)
can be used on Windows.
- OpenSSH for connecting to hosts using the system's own openssh "ssh" executable and utilizing session multiplexing for performance.
- WinRM as an alternative to SSH for windows hosts (SSH works too)
- Localhost for treating the local host as it was a remote host using go's os/exec.

#### Usage

TBD - for now see godoc, tests and sources.

