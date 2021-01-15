### Rig

A toolkit library for interacting with hosts running various operating systems over multiple protocols.

#### Design goals

Rig's intention is to be easy to use and extend.

It should be easy to add support for new operating systems and to add new commands to the multi-os support mechanism without breaking go's type checking.

All of the relevant structs have YAML tags and default values to make unmarshaling from YAML configurations as easy as possible.

#### Protocols

Currently rig comes with the most common ways to connect to hosts:
- SSH for connecting to hosts that accept SSH connections
- WinRM as an alternative to SSH for windows hosts (SSH works too)
- Local for treating the localhost as it was one of the remote hosts

#### Usage

Rig provides a struct that can be embedded into your host objects to give them multi-protocol connectivity and multi-os oeration support.

Example:

```go
package main

import "github.com/k0sproject/rig"

type host struct {
  rig.Connection
}

func main() {
  h := host{
    connection: rig.Connection{
      SSH: &rig.SSH{
        Address: 10.0.0.1
      }
    }
  }

  if err := h.Connect(); err != nil {
    panic(err)
  }

  output, err := h.ExecOutput("ls -al")
  if err != nil {
    panic(err)
  }
  println(output)
}
```

See more usage examples in the [examples/](examples/) directory.
