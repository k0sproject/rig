package rig

// OSVersion host operating system version information
type OSVersion struct {
	ID          string
	IDLike      string
	Name        string
	Version     string
	extraFields map[string]string
}

// String returns a human readable representation of OSVersion
func (o OSVersion) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}

// SetExtraField sets an extra field
func (o *OSVersion) SetExtraField(key, value string) {
	if o.extraFields == nil {
		o.extraFields = make(map[string]string)
	}
	o.extraFields[key] = value
}

// GetExtraField retrieves an Extra Field value if it is set
func (o OSVersion) GetExtraField(key string) (string, bool) {
	if o.extraFields == nil {
		return "", false
	}
	v, ok := o.extraFields[key]
	return v, ok
}
