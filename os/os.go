package os

// BaseOS provides the basic functionality of accessing a host in an OS module
type Base struct {
	host Host
}

// New returns a new instance that has a host assigned to it
func (b *Base) SetHost(h Host) {
	b.host = h
}

// Host returns the host
func (b Base) Host() Host {
	return b.host
}
