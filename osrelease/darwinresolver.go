package osrelease

import "errors"

type DarwinResolver struct{}

func (g *DarwinResolver) Get(r runner) (*OSRelease, error) {
	if r.IsWindows() {
		return nil, errNoMatch
	}

	if _, err := r.Run("uname | grep -q Darwin"); err != nil {
		return nil, errNoMatch
	}

	version, err := r.Run("sw_vers -productVersion")
	if err != nil {
		return nil, errors.Join(errNoMatch, err)
	}

	name, err := r.Run(`grep "SOFTWARE LICENSE AGREEMENT FOR " "/System/Library/CoreServices/Setup Assistant.app/Contents/Resources/en.lproj/OSXSoftwareLicense.rtf" | sed -E "s/^.*SOFTWARE LICENSE AGREEMENT FOR (.+)\\\/\1/"`)
	if err != nil {
		return nil, errors.Join(errNoMatch, err)
	}

	return &OSRelease{
		ID:      "darwin",
		IDLike:  "darwin",
		Version: version,
		Name:    name,
	}, nil
}
