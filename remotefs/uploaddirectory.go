package remotefs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// UploadDirectory uploads all files and directories recursively to the remote system.
func UploadDirectory(fsys FS, src, dst string) error {
	walkErr := filepath.WalkDir(src, func(path string, dir fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk local directory: %w", err)
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("calculate relative path: %w", err)
		}
		targetPath := filepath.Join(dst, relPath)

		if dir.IsDir() {
			dirInfo, err := dir.Info()
			if err != nil {
				return fmt.Errorf("get dir info: %w", err)
			}
			if err := fsys.MkdirAll(targetPath, dirInfo.Mode()&os.ModePerm); err != nil {
				return fmt.Errorf("create remote directory: %w", err)
			}
		} else {
			if err := Upload(fsys, path, targetPath); err != nil {
				return fmt.Errorf("upload directory: %w", err)
			}
		}
		return nil
	})

	if walkErr != nil {
		return fmt.Errorf("walk remote directory tree: %w", walkErr)
	}
	return nil
}
