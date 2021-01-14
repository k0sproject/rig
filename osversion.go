package rig

// OSVersion host operating system version information
type OSVersion struct {
	ID      string
	IDLike  string
	Name    string
	Version string
}

// String implements Stringer
func (o *OSVersion) String() string {
	return o.Name
}
