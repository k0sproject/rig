package remotefs

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// Download a file from the remote host.
func Download(fs FS, src, dst string) error {
	remote, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("open remote file for download: %w", err)
	}
	defer remote.Close()

	remoteStat, err := remote.Stat()
	if err != nil {
		return fmt.Errorf("stat remote file for download: %w", err)
	}

	remoteSum := sha256.New()
	localSum := sha256.New()

	local, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, remoteStat.Mode())
	if err != nil {
		return fmt.Errorf("open local file for download: %w", err)
	}
	defer local.Close()

	remoteReader := io.TeeReader(remote, remoteSum)
	if _, err := io.Copy(io.MultiWriter(local, localSum), remoteReader); err != nil {
		_ = local.Close()
		return fmt.Errorf("copy file from remote host: %w", err)
	}
	if err := local.Close(); err != nil {
		return fmt.Errorf("close local file after download: %w", err)
	}

	if !bytes.Equal(localSum.Sum(nil), remoteSum.Sum(nil)) {
		return fmt.Errorf("downloading %s failed: %w", src, ErrChecksumMismatch)
	}

	return nil
}
