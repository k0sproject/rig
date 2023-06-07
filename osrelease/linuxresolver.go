package osrelease

import "fmt"

type LinuxResolver struct{}

const (
	fileCmd       = `cat /etc/os-release || cat /usr/lib/os-release`
	lsbreleaseCmd = `command -v lsb_release 2>&1 > /dev/null && printf "NAME=%s\nID=%s\nVERSION_ID=%s\n" "$(lsb_release -sd | cut -d' ' -f1)" "$(lsb_release -si | tr '[:upper:]' '[:lower:]')" "$(lsb_release -sr)"`
)

func (g *LinuxResolver) Get(r runner) (*OSRelease, error) {
	if r.IsWindows() {
		return nil, errNoMatch
	}
	if _, err := r.Run("uname | grep -q Linux"); err != nil {
		return nil, errNoMatch
	}

	output, err := g.tryGet(r)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve linux release info: %w", err)
	}

	return DecodeString(output)
}

func (g *LinuxResolver) tryGet(r runner) (string, error) {
	if output, err := r.Run(fileCmd); err == nil {
		return output, nil
	}
	return r.Run(lsbreleaseCmd)
}
