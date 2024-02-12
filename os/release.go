package osrelease

// Release describes host operating system version information
type Release struct {
	ID          string
	IDLike      string
	Name        string
	Version     string
	ExtraFields map[string]string
}

// String returns a human readable representation of OSRelease
func (o Release) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}
