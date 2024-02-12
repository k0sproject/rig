package osrelease

// OSRelease describes host operating system version information
type OSRelease struct {
	ID          string
	IDLike      string
	Name        string
	Version     string
	ExtraFields map[string]string
}

// String returns a human readable representation of OSRelease
func (o OSRelease) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}
