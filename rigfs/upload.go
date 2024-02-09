package rigfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrChecksumMismatch is returned when the checksum of the uploaded file does not match the local checksum.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// Upload a file to the remote host.
func Upload(fsys Fsys, src, dst string) error {
	local, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open file for upload: %w", err)
	}
	defer local.Close()

	stat, err := local.Stat()
	if err != nil {
		return fmt.Errorf("stat local file for upload: %w", err)
	}

	shasum := sha256.New()

	remote, err := fsys.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return fmt.Errorf("open remote file for upload: %w", err)
	}
	defer remote.Close()

	localReader := io.TeeReader(local, shasum)
	if _, err := remote.CopyFrom(localReader); err != nil {
		_ = remote.Close()
		return fmt.Errorf("copy file to remote host: %w", err)
	}
	if err := remote.Close(); err != nil {
		return fmt.Errorf("close remote file after upload: %w", err)
	}

	remoteSum, err := fsys.Sha256(dst)
	if err != nil {
		return fmt.Errorf("get checksum of uploaded file: %w", err)
	}

	if remoteSum != hex.EncodeToString(shasum.Sum(nil)) {
		return ErrChecksumMismatch
	}

	return nil
}
