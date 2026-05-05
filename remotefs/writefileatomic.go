package remotefs

import (
	"fmt"
	"io/fs"
)

// WriteFileAtomic writes data to path atomically: a temp file is created in the
// same directory, written with restricted permissions, chmod'd to perm, then
// renamed into place. Parent directories are created as needed. Cleanup of the
// temp file is always attempted; if Remove fails the error is ignored.
func WriteFileAtomic(host OS, path string, data []byte, perm fs.FileMode) error {
	dir := host.Dir(path)
	if err := host.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("write-file-atomic %s: %w", path, err)
	}
	tmp, err := host.CreateTemp(dir, ".tmp-")
	if err != nil {
		return fmt.Errorf("write-file-atomic %s: %w", path, err)
	}
	defer func() { _ = host.Remove(tmp) }()
	if err := host.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write-file-atomic %s: %w", path, err)
	}
	if err := host.Chmod(tmp, perm); err != nil {
		return fmt.Errorf("write-file-atomic %s: %w", path, err)
	}
	if err := host.Rename(tmp, path); err != nil {
		return fmt.Errorf("write-file-atomic %s: %w", path, err)
	}
	return nil
}
