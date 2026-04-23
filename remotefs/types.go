package remotefs

import (
	"errors"
	"io"
	"io/fs"
	"time"
)

// ErrEmptyMachineID is returned by MachineID when the host returns an empty value.
var ErrEmptyMachineID = errors.New("machine-id: empty")

// FS is a filesystem on the remote host.
type FS interface {
	fs.FS
	fs.StatFS
	fs.ReadFileFS
	fs.ReadDirFS
	OS
	HTTPTransport
	Opener
	Sha256summer
}

// File is a file in the remote filesystem.
type File interface {
	Name() string
	fs.File
	io.Seeker
	io.ReadCloser
	io.Writer
	Copier
}

// OS is a os/filesystem utility interface, these operations are modeled after stdlib's OS package.
type OS interface { //nolint:interfacebloat // intentionally large interface
	Remove(path string) error
	RemoveAll(path string) error
	Mkdir(path string, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	MkdirTemp(dir, prefix string) (string, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	FileExist(path string) bool
	LookPath(cmd string) (string, error)
	Join(elem ...string) string
	Chmod(path string, mode fs.FileMode) error
	Chown(path string, owner string) error
	ChownInt(path string, uid, gid int) error
	ChownTree(path string, owner string) error
	ChownTreeInt(path string, uid, gid int) error
	Chtimes(path string, atime, mtime int64) error
	Touch(path string, ts ...time.Time) error
	Truncate(path string, size int64) error
	Getenv(key string) string
	Rename(oldpath, newpath string) error
	FileContains(path, substr string) (bool, error)
	IsContainer() (bool, error)
	Hostname() (string, error)
	LongHostname() (string, error)
	MachineID() (string, error)
	SystemTime() (time.Time, error)
	TempDir() string
	UserCacheDir() string
	UserConfigDir() string
	UserHomeDir() string
	Dir(path string) string
	Base(path string) string
	CommandExist(name string) bool
}

// Opener is a file opener interface, modeled after stdlib's OS package.
type Opener interface {
	OpenFile(path string, flag int, perm fs.FileMode) (File, error)
}

// Sha256summer implementing struct can calculate sha256 checksum of a file.
type Sha256summer interface {
	Sha256(path string) (string, error)
}

// Copier is a file-like struct that can copy data to and from io.Reader and io.Writer.
type Copier interface {
	CopyFrom(src io.Reader) (int64, error)
	CopyTo(dst io.Writer) (int64, error)
}
