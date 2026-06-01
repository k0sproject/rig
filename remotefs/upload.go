package remotefs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// ErrChecksumMismatch is returned when the checksum of the uploaded file does not match the local checksum.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// UploadOption is a functional option for Upload.
type UploadOption func(*uploadOptions)

type uploadOptions struct {
	perm    fs.FileMode
	hasPerm bool
}

// WithPermissions sets the file mode for the uploaded file. If not set, the local
// file's mode is used.
func WithPermissions(mode fs.FileMode) UploadOption {
	return func(o *uploadOptions) {
		o.perm = mode
		o.hasPerm = true
	}
}

// copyAndVerifyUpload copies src to tmpPath via a hash writer and verifies remote checksum.
func copyAndVerifyUpload(fsys FS, tmpPath string, src io.Reader) error {
	localHash := sha256.New()
	reader := io.TeeReader(src, localHash)

	remote, err := fsys.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp file for upload: %w", err)
	}
	if _, err := remote.CopyFrom(reader); err != nil {
		_ = remote.Close()
		return fmt.Errorf("copy file to remote host: %w", err)
	}
	if err := remote.Close(); err != nil {
		return fmt.Errorf("close temp file after upload: %w", err)
	}

	remoteSum, err := fsys.Sha256(tmpPath)
	if err != nil {
		return fmt.Errorf("get checksum of uploaded file: %w", err)
	}
	expectedSum := hex.EncodeToString(localHash.Sum(nil))
	if remoteSum != expectedSum {
		return ErrChecksumMismatch
	}
	return nil
}

// Upload a file to the remote host atomically: the content is written to a
// temporary file in the same directory as dst, verified via SHA-256, and then
// renamed into place. The temporary file is removed on any failure.
//
// Permissions: the temporary file is created with mode 0o600 and then chmod'd
// to perm before the rename. When WithPermissions is not given, perm is taken
// from the local file's mode. This means that overwriting an existing remote
// file always sets its mode to perm — unlike a direct truncating write, which
// would leave the remote file's existing mode unchanged.
func Upload(fsys FS, src, dst string, opts ...UploadOption) error {
	options := &uploadOptions{}
	for _, opt := range opts {
		opt(options)
	}

	local, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open file for upload: %w", err)
	}
	defer local.Close()

	perm := options.perm
	if !options.hasPerm {
		stat, err := local.Stat()
		if err != nil {
			return fmt.Errorf("stat local file for upload: %w", err)
		}
		perm = stat.Mode()
	}

	dir := fsys.Dir(dst)
	tmpPath, err := fsys.CreateTemp(dir, ".upload-")
	if err != nil {
		return fmt.Errorf("create temp file for upload: %w", err)
	}
	defer func() { _ = fsys.Remove(tmpPath) }()

	if err := copyAndVerifyUpload(fsys, tmpPath, local); err != nil {
		return err
	}

	if err := fsys.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod uploaded file: %w", err)
	}
	if err := fsys.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename uploaded file into place: %w", err)
	}
	return nil
}
