package rigfs

import (
	"io"
	"io/fs"
)

// Fsys is a filesystem on the remote host
type Fsys interface {
	fs.FS
	fs.StatFS
	fs.ReadFileFS
	fs.ReadDirFS
	OS
	Opener
	Sha256summer
}

// File is a file in the remote filesystem
type File interface {
	Name() string
	fs.File
	io.Seeker
	io.ReadCloser
	io.Writer
	Copier
}

// OS is a os/filesystem utility interface, these operations are modeled after stdlib's OS package
type OS interface { //nolint:interfacebloat // intentionally large interface
	Remove(path string) error
	RemoveAll(path string) error
	Mkdir(path string, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	MkdirTemp(dir, prefix string) (string, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	FileExist(path string) bool
	CommandExist(cmd string) bool
	Join(elem ...string) string
	Chmod(path string, mode fs.FileMode) error
	Chown(path string, uid, gid int) error
	Chtimes(path string, atime, mtime int64) error
	Touch(path string) error
	Truncate(path string, size int64) error
	Getenv(key string) string
	Rename(oldpath, newpath string) error
	Hostname() (string, error)
	LongHostname() (string, error)
	TempDir() string
	UserCacheDir() string
	UserConfigDir() string
	UserHomeDir() string
}

// Opener is a file opener interface, modeled after stdlib's OS package
type Opener interface {
	OpenFile(path string, flag int, perm fs.FileMode) (File, error)
}

// Sha256summer implementing struct can calculate sha256 checksum of a file
type Sha256summer interface {
	Sha256(path string) (string, error)
}

// Copier is a file-like struct that can copy data to and from io.Reader and io.Writer
type Copier interface {
	CopyFrom(src io.Reader) (int64, error)
	CopyTo(dst io.Writer) (int64, error)
}
