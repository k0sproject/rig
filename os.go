package rig

// Os host operating system version information
type Os struct {
	ID      string
	IDLike  string
	Name    string
	Version string
}

// String implements Stringer
func (o *Os) String() string {
	return o.Name
}

type OsSupport interface {
	InstallPackage(...string) error
	CheckPrivilege() error
}
