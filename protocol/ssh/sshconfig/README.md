## sshconfig

This directory contains an implementation of a parser for openssh's [`ssh_config`](https://man7.org/linux/man-pages/man5/ssh_config.5.html) file format. 

The format and its parsing rules are a bit complicated. 

Further reading:

- [The ssh_config(5) man page](https://man.openbsd.org/ssh_config)
- [readconf.c](https://github.com/openssh/openssh-portable/blob/master/readconf.c) from the openssh source code.
- [Quirks of parsing SSH configs](https://sthbrx.github.io/blog/2023/08/04/quirks-of-parsing-ssh-configs/)

### This implementation

Unlike the alternatives, this implementation does not even try to read the configuration tree into memory first and then query it for values. Instead, it uses the configuration files as kind of transformers for the single host configuration instance and applies the settings in the order they are defined in the configuration files. As a downside, when getting the configuration for multiple hosts, all of the configuration files are read and parsed again for each host. If this becomes a performance issue, the implementation can be changed to read the configuration files only once and then query the in-memory configuration tree for each host.

Implemented features:

- All of the fields listed in the [ssh_config(5)](https://man.openbsd.org/ssh_config) man page (and two additional apple specific fields).
- Partial [`Match`](https://man.openbsd.org/ssh_config#Match) directive support. `LocalAddress`, `LocalPort` and `RDomain` are not implemented.
- Partial [`TOKENS`](https://man.openbsd.org/ssh_config#TOKENS) expansion support. Expanding some of the tokens would require an established connection to the host.
- Full `Include` directive support. The parser will follow the `Include` directive and parse the included files as well.
- Expansion of `~` and environment variables in the values.
- List modifier prefixes for fields like `HostKeyAlgorithms` or `KexAlgorithms` where you can use `+` prefix to append to the existing list or `-` prefix to remove from the existing list.
- Support for boolean fields which can also have string values (`yes`, `no`, `ask`, `always`, `none`, etc.)
- "Strict" mode for supporting the [`IgnoreUnknown`](https://man.openbsd.org/ssh_config#IgnoreUnknown) directive. When enabled, the parser will throw an error when it encounters an unknown directive or a directive with an unknown value. By default this is not enabled.
- The origin of each value can be determined.
- The origin based value precedence is correctly implemented as described in the specification.
- Hostname canonicalization is implemented.
- Proper unquoting and splitting of values based on a converted `argv_split` from the original source.

### Status

The parser has not been tested outside of the development environment0 **at all** yet.

If there's signs of interest, the parser can be extracted from the `rig` repository and published as a separate module.

### Usage

Using the parser requires setting up a struct with fields defined using the types in `configvalue.go`. You can also use the complete `sshconfig.SSHConfig` struct if you want to parse all possible settings. The object is required to have at least the fields specified in `sshconfig.RequiredFields`.

The `Host` field must be set to what ever you are looking for the configuration for. This is the alias you would use when connecting to the host using the `ssh` command, such as the `example.com` in `ssh example.com`.

```go
package main

import(
    "fmt"
    "github.com/k0sproject/rig/v2/protocol/ssh/sshconfig"
)

type hostconfig struct {
    sshconfig.RequiredFields
    IdentityFile sshconfig.PathListValue
}

func main() {
	parser, err := sshconfig.NewParser(nil)
    if err != nil {
        panic(err)
    }
    host := &hostconfig{}
    host.SetHost("example.com") // this sets the host alias for looking up the configuration, like `ssh example.com` would.
    if err := parser.Parse(host); err != nil {
        panic(err)
    }
    fmt.Println(host.IdentityFile)
}
```


### Alternatives

Currently there seems to exist two alternatives:

- [`kevinburke/ssh_config`](https://github.com/kevinburke/ssh_config)

  This is a rather complete implementation but there are some issues with it:

	- Does not support [`Match`](https://man.openbsd.org/ssh_config#Match) directives
	- Does not support [Hostname canonicalization](https://man.openbsd.org/ssh_config#CanonicalizeHostname)
	- Doesn't handle comments correctly when key and value are separated by a whitespace. (fixed in [this fork](https://github.com/trzsz/ssh_config/commit/eadc486e19919bf80d8fad53d6a5c6eb9c299a71))
	- Not all of the Boolean fields listed [here](https://github.com/kevinburke/ssh_config/blob/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L19) are actually boolean. For example, `ForwardAgent` can be a path to agent socket or an environment variable name.
	- Not all of the default values are correct or set. For example, the list of [identity files](https://github.com/kevinburke/ssh_config/blob/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L165) is incomplete.
	- It does not support the list modifier prefixes for fields like `HostKeyAlgorithms` or `KexAlgorithms` where you can use `+` prefix to append to the existing list or `-` prefix to remove from the existing list.
	- It doesn't handle the `CertificateFile` directive correctly. Specifying it should always append to the existing list of certificate files.
	- When querying for values, it is very difficult to determine if the value is a default value or if it was set explicitly in a config file.
	- When you need to know multiple settings for a given host, you need to query for each setting separately.
	- Values for list fields need to be split manually.
	- No expansion of `~` or environment variables in the values.
	- No expansion of the [TOKENS](https://man.openbsd.org/ssh_config#TOKENS).
	- It doesn't correctly follow the rules for origin based value precedence. 

- [`mikkeloscar/sshconfig`](https://github.com/mikkeloscar/sshconfig)

	This is a newer arrival. It supports a very limited subset of fields (`Host, HostName, User, Port, IdentityFile, HostKeyAlgorithms, ProxyCommand, LocalForward, RemoteForward, DynamicForward, Ciphers and MACs`). It implements even less features and quirks of the configuration syntax.

### Contributing

Issues and PRs are welcome. Especially ones that eliminate `//nolint` comments.
