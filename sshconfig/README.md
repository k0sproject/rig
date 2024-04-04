## sshconfig

[![GoDoc](https://godoc.org/github.com/k0sproject/rig/v2/sshconfig/?status.svg)](https://godoc.org/github.com/k0sproject/rig/v2/sshconfig)

This directory contains an implementation of a parser for OpenSSH's [`ssh_config`](https://man7.org/linux/man-pages/man5/ssh_config.5.html) file format. 

The format and its parsing rules are slightly complicated. 

Further reading:

- [The ssh_config(5) man page](https://man.openbsd.org/ssh_config)
- [readconf.c](https://github.com/openssh/openssh-portable/blob/master/readconf.c) from the openssh source code.
- [Quirks of parsing SSH configs](https://sthbrx.github.io/blog/2023/08/04/quirks-of-parsing-ssh-configs/)

### This implementation

Implemented features:

- All of the fields listed in the [ssh_config(5)](https://man.openbsd.org/ssh_config) man page (and two additional Apple specific fields).
- Partial [`Match`](https://man.openbsd.org/ssh_config#Match) directive support. `Address`, `LocalAddress`, `LocalPort` and `RDomain` are not implemented (because they require an established connection, which could be achievable later if needed).
- Partial [`TOKENS`](https://man.openbsd.org/ssh_config#TOKENS) expansion support. Like above, expanding some of the tokens would require an established connection to the host while parsing the config.
- `Include` directive support, the parser will follow the `Include` directives as expected, in lexical order like the OpenSSH implementation. It will also detect circular includes.
- Expansion of `~` and environment variables in the values for the supported fields listed on the man page.
- Support for list modifier prefixes for fields like `HostKeyAlgorithms` or `KexAlgorithms` where you can use a `+` prefix to append to the default list, a `-` prefix to remove from the default list, or `^` to prepend to the default list.
- Support and validation for "multistate fields" as they are called in OpenSSH's `readconf.c` which can act like booleans but can also contain other string values, such as `yes`, `no`, `ask`, `always`, `none`, etc. 
- A "strict" mode for supporting the [`IgnoreUnknown`](https://man.openbsd.org/ssh_config#IgnoreUnknown) directive. When enabled, the parser will throw an error when it encounters an unknown directive. To enable, use the `sshconfig.WithStrict()` option when creating the parser.
- The origin based value precedence is correctly implemented as described in the specification and as observed in the OpenSSH implementation.
- [Hostname canonicalization](https://sleeplessbeastie.eu/2020/08/24/how-to-perform-hostname-canonicalization/).
- Original-like unquoting and splitting of values based on `argv_split` from the original C source converted to go.

### Status

The parser has not been tested outside of the development environment **at all** yet.

If there's interest, the parser can be extracted from the `rig` repository and published as a separate module.

### Usage

Typically you first create a parser via `sshconfig.NewParser(nil)` and then call `Apply(obj)` on it with a struct that you want to populate with the values for a given host from the system's ssh configuration files. You can also pass in an `io.Reader` to `NewParser` to read from a custom source instead of the default locations.

You can use the provided `sshconfig.Config` which includes all the known configuration fields or you can define a struct with only a subset of the fields. The object must have the same field names as listed in the `ssh_config` man page and at least a `Host string` field must exist for the parsing to work.

```go
package main

import(
  "fmt"
  "github.com/k0sproject/rig/v2/sshconfig"
)

func main() {
  // this will read the configurations from the default locations.
  parser, err := sshconfig.NewParser(nil) 
  // To read from a specific file or a string, pass in an io.Reader like:
  // parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\nIdentityFile ~/.ssh/id_rsa\n"))

  if err != nil {
    panic(err)
  }
  host := &sshconfig.Config{}
  if err := parser.Apply(host, "example.com"); err != nil {
    panic(err)
  }
  fmt.Println(host.IdentityFile[0])
}
```

There's also a `sshconfig.ConfigFor` shorthand for when you're not expecting to need the configuration for more than one host:

```go
package main

import(
  "fmt"
  "github.com/k0sproject/rig/v2/sshconfig"
)

func main() {
  config, err := sshconfig.ConfigFor("example.com") 
  if err != nil {
    panic(err)
  }
  fmt.Println(config.IdentityFile[0])
}
```

You can output a `ssh_config` formatted string using `sshconfig.Dump`:

```go
package main

import(
  "fmt"
  "github.com/k0sproject/rig/v2/sshconfig"
)

func main() {
  config, err := sshconfig.ConfigFor("example.com") 
  if err != nil {
    panic(err)
  }
  str, err := sshconfig.Dump(config)
  fmt.Println(str)
}
```

This will output something like:

```text
Host example.com
  AddKeysToAgent no
  AddressFamily any
  BatchMode no
  ...
```

### Alternatives

Currently there seems to exist two alternatives:

- [`kevinburke/ssh_config`](https://github.com/kevinburke/ssh_config)

  This is a somewhat complete implementation but there are several issues with it:

  - Does not support [`Match`](https://man.openbsd.org/ssh_config#Match) directives at all.
  - Does not support [Hostname canonicalization](https://man.openbsd.org/ssh_config#CanonicalizeHostname), which requires an additional pass of parsing if the hostname gets changed.
  - Not all of the Boolean fields listed [here](https://github.com/kevinburke/ssh_config/blob/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L19) are actually regular booleans. For example, `ForwardAgent` can be a path to an agent socket or an environment variable name.
  - Not all of the default values are correct or set. In fact, [the list of defaults](https://github.com/kevinburke/ssh_config/blame/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L18) is from 2017. The default for `IdentityFile` is [`~/.ssh/identity`](https://github.com/kevinburke/ssh_config/blob/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L120) which started to phase out upon the release of OpenSSH 3.0 in 2001 which started defaulting to `~/.ssh/id_dsa`. The list of [`defaultProtocol2Identities`](https://github.com/kevinburke/ssh_config/blob/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L165-L170) is not used and is missing a couple of entries.
  - It does not support the list modifier prefixes for fields like `HostKeyAlgorithms` or `KexAlgorithms` where you can use the `+` or `^` prefixes to append or prepend to the default list or the `-` prefix to remove from it.
  - When you need to know multiple settings for a given host, you have to query for each setting separately.
  - Values need to be unquoted and expanded and converted to correct types manually and values for white-space or comma separated list fields need to be split manually.
  - No expansion of `~/` or environment variables in the values (there's a [pull request](https://github.com/kevinburke/ssh_config/pull/31) from 2021).
  - No expansion of the [TOKENS](https://man.openbsd.org/ssh_config#TOKENS) (there's a [pull request](https://github.com/kevinburke/ssh_config/pull/49) from 2022).
  - Doesn't seem to be actively maintained, there are open unanswered issues and unmerged pull requests from several years ago. See [PR comment](https://github.com/kevinburke/ssh_config/pull/37#issuecomment-1095599334).

- [`mikkeloscar/sshconfig`](https://github.com/mikkeloscar/sshconfig)

  A simplistic implementation that only supports a very limited subset of fields (`Host, HostName, User, Port, IdentityFile, HostKeyAlgorithms, ProxyCommand, LocalForward, RemoteForward, DynamicForward, Ciphers and MACs`). It implements even less features and quirks of the syntax. The GPL-3 license may be problematic for some.

### Contributing

Issues and PRs are welcome. Especially ones that eliminate any of the `//nolint` comments.
