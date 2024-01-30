package rig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/rigfs"
)

func Upload(fsys rigfs.Fsys, src, dst string) error {
	local, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPath, err)
	}
	defer local.Close()

	stat, err := local.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat local file %s: %w", ErrInvalidPath, src, err)
	}

	shasum := sha256.New()

	remote, err := fsys.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return fmt.Errorf("%w: open remote file %s for writing: %w", ErrInvalidPath, dst, err)
	}
	defer remote.Close()

	localReader := io.TeeReader(local, shasum)
	if _, err := remote.CopyFrom(localReader); err != nil {
		_ = remote.Close()
		return fmt.Errorf("%w: copy file %s to remote host: %w", ErrUploadFailed, dst, err)
	}
	if err := remote.Close(); err != nil {
		return fmt.Errorf("%w: close remote file %s: %w", ErrUploadFailed, dst, err)
	}

	log.Debugf("%s: post-upload validate checksum of %s", fsys, dst)
	remoteSum, err := fsys.Sha256(dst)
	if err != nil {
		return fmt.Errorf("%w: validate %s checksum: %w", ErrUploadFailed, dst, err)
	}

	if remoteSum != hex.EncodeToString(shasum.Sum(nil)) {
		return fmt.Errorf("%w: checksum mismatch", ErrUploadFailed)
	}

	return nil
}
