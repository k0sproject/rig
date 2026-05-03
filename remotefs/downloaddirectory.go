package remotefs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DownloadDirectory downloads all files and directories recursively from the remote system to local directory.
func DownloadDirectory(fsys FS, src, dst string) error {
	walkErr := fs.WalkDir(fsys, src, func(path string, dir fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk remote directory: %w", err)
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
			if err := os.MkdirAll(targetPath, dirInfo.Mode()&os.ModePerm); err != nil {
				return fmt.Errorf("create local directory: %w", err)
			}
		} else {
			if err := Download(fsys, path, targetPath); err != nil {
				return fmt.Errorf("download directory: %w", err)
			}
		}
		return nil
	})

	if walkErr != nil {
		return fmt.Errorf("walk remote directory tree: %w", walkErr)
	}
	return nil
}
