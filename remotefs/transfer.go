package remotefs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrChecksumMismatch is returned by Upload and DownloadFile when the checksum
// of the transferred file does not match the checksum of its source.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// UploadOption is a functional option for Upload and DownloadFile.
type UploadOption func(*uploadOptions)

type uploadOptions struct {
	perm    fs.FileMode
	hasPerm bool
}

// WithPermissions sets the file mode for the transferred file. If not set, the
// mode of the source file is used.
func WithPermissions(mode fs.FileMode) UploadOption {
	return func(o *uploadOptions) {
		o.perm = mode
		o.hasPerm = true
	}
}

func applyUploadOptions(opts []UploadOption) *uploadOptions {
	options := &uploadOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
	return options
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
		err = fmt.Errorf("copy file to remote host: %w", err)
		// Keep a failure to close as well: if the session timed out, that is
		// what tells Upload the connection is gone and cleanup must not be
		// attempted over it.
		if closeErr := remote.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return err
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
// renamed into place. The temporary file is removed on failure, except when
// the remote session timed out: the connection is gone at that point, so the
// cleanup would block on it, and the file is left for the next upload.
//
// Permissions: the temporary file is created with mode 0o600 and then chmod'd
// to perm before the rename. When WithPermissions is not given, perm is taken
// from the local file's mode. This means that overwriting an existing remote
// file always sets its mode to perm — unlike a direct truncating write, which
// would leave the remote file's existing mode unchanged.
func Upload(fsys FS, src, dst string, opts ...UploadOption) error {
	options := applyUploadOptions(opts)

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
	// RemoveAll rather than Remove: on the success path the rename has already
	// moved tmpPath away, and only RemoveAll accepts a path that is not there.
	// Not after a session timeout though -- the connection is gone, so the
	// cleanup would run against it and block there, undoing the bound that
	// timeout just established. Nor after the context fsys is bound to has
	// ended, which fails every command, the cleanup included. The temp file is
	// left for the next upload.
	var failure error
	defer func() {
		// ErrTimeout wraps context.DeadlineExceeded, so this covers it too.
		if isContextErr(failure) {
			return
		}
		_ = fsys.RemoveAll(tmpPath)
	}()

	if failure = copyAndVerifyUpload(fsys, tmpPath, local); failure != nil {
		return failure
	}

	if failure = fsys.Chmod(tmpPath, perm); failure != nil {
		return fmt.Errorf("chmod uploaded file: %w", failure)
	}
	if failure = fsys.Rename(tmpPath, dst); failure != nil {
		return fmt.Errorf("rename uploaded file into place: %w", failure)
	}
	return nil
}

// DownloadFile copies the remote file src to the local path dst atomically:
// the content is written to a temporary file in the same directory as dst,
// verified against the remote file's SHA-256, and then renamed into place.
// The temporary file is removed on failure, so dst is either left as it was
// or fully replaced.
//
// The local file gets the remote file's modification time and, unless
// WithPermissions is given, its mode. Windows hosts only report a read-only
// flag, which maps to 0o555 or 0o777, so pass an explicit mode when
// downloading from one.
//
// src must be a regular file. Symlinks are not followed, as the remote stat
// describes the link itself, and are rejected along with directories.
func DownloadFile(fsys FS, src, dst string, opts ...UploadOption) error {
	options := applyUploadOptions(opts)

	stat, err := fsys.Stat(src)
	if err != nil {
		return fmt.Errorf("stat remote file for download: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("download %s: %w: not a regular file (mode %s)", src, fs.ErrInvalid, stat.Mode())
	}

	perm := stat.Mode().Perm()
	if options.hasPerm {
		perm = options.perm
	}

	remote, err := fsys.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open remote file for download: %w", err)
	}
	// The handle is closed before the checksum is taken, as the Windows daemon
	// opens files without sharing and Sha256 would hit a sharing violation; the
	// deferred call covers the earlier failures.
	closeRemote := sync.OnceValue(remote.Close)
	defer func() { _ = closeRemote() }()

	return writeDownload(fsys, src, remote, closeRemote, dst, perm, stat.ModTime())
}

// writeDownload copies remote into a temporary file next to dst, verifies it,
// applies perm and modTime, and renames it into place. The temporary file is
// removed if any step fails.
func writeDownload(fsys FS, src string, remote File, closeRemote func() error, dst string, perm fs.FileMode, modTime time.Time) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".download-*")
	if err != nil {
		return fmt.Errorf("create temp file for download: %w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := copyAndVerifyDownload(fsys, src, remote, closeRemote, tmp); err != nil {
		return err
	}

	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod downloaded file: %w", err)
	}
	// A zero access time leaves it unchanged.
	if err := os.Chtimes(tmpPath, time.Time{}, modTime); err != nil {
		return fmt.Errorf("set modification time of downloaded file: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename downloaded file into place: %w", err)
	}
	keep = true
	return nil
}

// copyAndVerifyDownload copies remote into the local file tmp, closes both,
// and compares the copied bytes against the checksum of src on the remote host.
func copyAndVerifyDownload(fsys FS, src string, remote File, closeRemote func() error, tmp *os.File) error {
	localHash := sha256.New()
	if _, err := remote.CopyTo(io.MultiWriter(tmp, localHash)); err != nil {
		return fmt.Errorf("copy file from remote host: %w", err)
	}
	if err := closeRemote(); err != nil {
		return fmt.Errorf("close remote file after download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file after download: %w", err)
	}

	remoteSum, err := fsys.Sha256(src)
	if err != nil {
		return fmt.Errorf("get checksum of remote file: %w", err)
	}
	if remoteSum != hex.EncodeToString(localHash.Sum(nil)) {
		return fmt.Errorf("download %s: %w", src, ErrChecksumMismatch)
	}
	return nil
}
