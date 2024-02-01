package rigfs

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// CreateTemp creates a new temporary file in the directory dir with a name built using the
// pattern, opens the file for reading and writing, and returns the resulting File.
// If dir is the empty string, CreateTemp uses the default directory for temporary files.
func CreateTemp(fsys Fsys, dir, pattern string) (File, error) {
	if dir == "" {
		dir = fsys.TempDir()
	}

	rnd, err := randHexString(8)
	if err != nil {
		rnd = strconv.FormatInt(time.Now().UnixNano(), 16)
	}

	var path string

	switch {
	case pattern == "":
		path = fsys.Join(dir, "tmp."+rnd)
	case strings.Contains(pattern, "*"):
		path = fsys.Join(dir, strings.ReplaceAll(pattern, "*", rnd))
	default:
		path = fsys.Join(dir, pattern+"."+rnd)
	}
	f, err := fsys.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("createtemp %s: %w", path, err)
	}
	return f, nil
}
