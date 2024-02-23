package homedir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Expand does ~/ style path expansion for files under current user home. On windows, this supports paths like %USERPROFILE%\path.
func Expand(path string) (string, error) {
	parts := strings.Split(path, string(os.PathSeparator))
	if parts[0] != "~" && parts[0] != "%USERPROFILE%" && parts[0] != "%userprofile%" && parts[0] != "%HOME%" && parts[0] != "%home%" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("homedir expand: get user home: %w", err)
	}
	parts[0] = home
	return filepath.Join(parts...), nil
}
