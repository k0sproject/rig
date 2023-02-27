package rigfs

import (
	"errors"
	"io"
	"io/fs"

	"github.com/k0sproject/rig/exec"
)

// ErrCommandFailed is returned when a remote command fails
var ErrCommandFailed = errors.New("command failed")

// Waiter is an interface that has a Wait() function that blocks until a command is finished
type Waiter interface {
	Wait() error
}

type connection interface {
	IsWindows() bool
	Exec(string, ...exec.Option) error
	ExecOutput(string, ...exec.Option) (string, error)
	ExecStreams(string, io.ReadCloser, io.Writer, io.Writer, ...exec.Option) (Waiter, error)
}

// File is a file in the remote filesystem
type File interface {
	fs.File
	io.WriteCloser
	io.Seeker
	Copy(io.Writer) (int64, error)
	CopyFromN(io.Reader, int64, io.Writer) (int64, error)
}

// Fsys is a filesystem on the remote host
type Fsys interface {
	fs.FS
	OpenFile(string, FileMode, FileMode) (File, error)
	Sha256(string) (string, error)
	Stat(string) (fs.FileInfo, error)
	Delete(string) error
}

// FileMode is used to set the type of allowed operations when opening remote files
type FileMode int

const (
	ModeRead      FileMode = 1                    // ModeRead = Read only
	ModeWrite     FileMode = 2                    // ModeWrite = Write only
	ModeReadWrite FileMode = ModeRead | ModeWrite // ModeReadWrite = Read and Write
	ModeCreate    FileMode = 4 | ModeWrite        // ModeCreate = Create a new file or truncate an existing one. Includes write permission.
	ModeAppend    FileMode = 8 | ModeCreate       // ModeAppend = Append to an existing file. Includes create and write permissions.
)
