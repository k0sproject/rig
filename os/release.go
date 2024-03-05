// Package os provides remote OS release information detection
package os

// Release describes host operating system version information.
type Release struct {
	ID          string            `kv:"ID"`
	IDLike      string            `kv:"ID_LIKE"`
	Name        string            `kv:"NAME"`
	Version     string            `kv:"VERSION_ID"`
	ExtraFields map[string]string `kv:"*"`
}

// String returns a human readable representation of the release information.
func (o Release) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}
