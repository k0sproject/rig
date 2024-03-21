## sshconfig

[![GoDoc](https://godoc.org/github.com/k0sproject/rig/v2/protocol/ssh/sshconfig/?status.svg)](https://godoc.org/github.com/k0sproject/rig/v2/protocol/ssh/sshconfig)

This directory contains an implementation of a parser for openssh's [`ssh_config`](https://man7.org/linux/man-pages/man5/ssh_config.5.html) file format. 

The format and its parsing rules are a bit complicated. 

Further reading:

- [The ssh_config(5) man page](https://man.openbsd.org/ssh_config)
- [readconf.c](https://github.com/openssh/openssh-portable/blob/master/readconf.c) from the openssh source code.
- [Quirks of parsing SSH configs](https://sthbrx.github.io/blog/2023/08/04/quirks-of-parsing-ssh-configs/)

### This implementation

Implemented features:

- All of the fields listed in the [ssh_config(5)](https://man.openbsd.org/ssh_config) man page (and two additional apple specific fields).
- Partial [`Match`](https://man.openbsd.org/ssh_config#Match) directive support. `Address`, `LocalAddress`, `LocalPort` and `RDomain` are not implemented (because they require an established connection or insights of the local network, which could be achievable later if needed).
- Partial [`TOKENS`](https://man.openbsd.org/ssh_config#TOKENS) expansion support. Like above, expanding some of the tokens would require an established connection to the host.
- `Include` directive support, the parser will follow the `Include` directives as expected, in lexical order like the openssh implementation. It will also detect circular includes.
- Expansion of `~` and environment variables in the values where applicable (the enabled fields are listed in the man page).
- Support for list modifier prefixes for fields like `HostKeyAlgorithms` or `KexAlgorithms` where you can use `+` prefix to append to the existing list or `-` prefix to remove from the existing list and ^ to prepend to the existing list.
- Support for boolean fields which can also have string values (`yes`, `no`, `ask`, `always`, `none`, etc.). These are not enforced like in the openssh implementation, if the field is a MultiStateBooleanValue, it will accept any string value, but Bool() will return the value and an ok flag if it's one of the known boolean values.
- "Strict" mode for supporting the [`IgnoreUnknown`](https://man.openbsd.org/ssh_config#IgnoreUnknown) directive. When enabled, the parser will throw an error when it encounters an unknown directive or a directive with an unknown value. By default this is not enabled. To enable, use the `sshconfig.WithErrorOnUnknown()` option when creating the parser.
- The origin of each value can be determined.
- The origin based value precedence is correctly implemented as described in the specification.
- Hostname canonicalization is implemented.
- Original-like unquoting and splitting of values based on `argv_split` from the original source converted to go.

### Status

The parser has not been tested outside of the development environment0 **at all** yet.

If there's interest, the parser can be extracted from the `rig` repository and published as a separate module.

### Usage

You can either use the complete `sshconfig.SSHConfig` which includes all the known fields or you can define a struct with a subset of the fields. The struct must have at least the fields specified in `sshconfig.RequiredFields` for parsing to work.

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
    // this will read the configurations from the default locations.
	parser, err := sshconfig.NewParser(nil) 
    // To read from a specific file or a string, pass in an io.Reader like:
    // parser, err := sshconfig.NewParser(strings.NewReader("Host example.com\nIdentityFile ~/.ssh/id_rsa\n"))

    if err != nil {
        panic(err)
    }
    host := &hostconfig{}
    if err := parser.Parse(host, "example.com"); err != nil {
        panic(err)
    }
    fmt.Println(host.IdentityFile.String())
}
```


### Alternatives

Currently there seems to exist two alternatives:

- [`kevinburke/ssh_config`](https://github.com/kevinburke/ssh_config)

  This is a rather complete implementation but there are some issues with it:

	- Does not support [`Match`](https://man.openbsd.org/ssh_config#Match) directives at all.
	- Does not support [Hostname canonicalization](https://man.openbsd.org/ssh_config#CanonicalizeHostname)
	- Not all of the Boolean fields listed [here](https://github.com/kevinburke/ssh_config/blob/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L19) are actually strictly boolean. For example, `ForwardAgent` can be a path to agent socket or an environment variable name.
	- Not all of the default values are correct or set. In fact, [the list of defaults](https://github.com/kevinburke/ssh_config/blame/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L18) is from 2017.
	- It does not support the list modifier prefixes for fields like `HostKeyAlgorithms` or `KexAlgorithms` where you can use `+` prefix to append to the existing list or `-` prefix to remove from the existing list.
    - It sets [some fields](https://github.com/kevinburke/ssh_config/blame/1d09c0b50564c4a7f8c56c9d5d6d935e06ee94da/validators.go#L176) as "plural" when they are not. For example `IdentityFile` values are not supposed to be collected from multiple lines.
	- When you need to know multiple settings for a given host, you need to query for each setting separately. There doesn't seem to be a function that returns a list of settings that apply to a given host alias.
	- Values need to be unquoted manually.
    - Values for list fields need to be split manually.
	- No expansion of `~` or environment variables in the values (there's a [pull request](https://github.com/kevinburke/ssh_config/pull/31) from 2021).
	- No expansion of the [TOKENS](https://man.openbsd.org/ssh_config#TOKENS) (there's a [pull request](https://github.com/kevinburke/ssh_config/pull/49) from 2022).
    - Doesn't seem to be actively maintained, there are open unanswered issues and unmerged pull requests from several years ago.

- [`mikkeloscar/sshconfig`](https://github.com/mikkeloscar/sshconfig)

	This is a newer arrival. It supports a very limited subset of fields (`Host, HostName, User, Port, IdentityFile, HostKeyAlgorithms, ProxyCommand, LocalForward, RemoteForward, DynamicForward, Ciphers and MACs`). It implements even less features and quirks of the syntax.

### Contributing

Issues and PRs are welcome. Especially ones that eliminate `//nolint` comments.
