package osrelease

type LinuxResolver struct{}

func (g *LinuxResolver) Get(r runner) (*OSRelease, error) {
	if r.IsWindows() {
		return nil, errNoMatch
	}
	if _, err := r.Run("uname | grep -q Linux"); err != nil {
		return nil, errNoMatch
	}

	output, err := r.Run(`cat /etc/os-release || cat /usr/lib/os-release || (command -v lsb_release 2>&1 > /dev/null && printf "NAME=%s\nID=%s\nVERSION_ID=%s\n" "$(lsb_release -sd | cut -d' ' -f1)" "$(lsb_release -si | tr '[:upper:]' '[:lower:]')" "$(lsb_release -sr)")`)
	if err != nil {
		return nil, err
	}
	return DecodeString(output)
}
