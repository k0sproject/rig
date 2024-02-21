package rig

// OSVersion host operating system version information
type OSVersion struct {
	ID          string
	IDLike      string
	Name        string
	Version     string
	ExtraFields map[string]string
}

// NewOSVersion creates a new instance of OSVersion with initialized ExtraFields
func NewOSVersion() *OSVersion {
	return &OSVersion{
		ExtraFields: make(map[string]string),
	}
}

// String returns a human readable representation of OSVersion
func (o OSVersion) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}
