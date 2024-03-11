package remotefs

import (
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// Service provides a unified interface to interact with the filesystem on a
// remote host. It ensures that a suitable remotefs.FS implementation is
// lazily initialized and made available for filesystem operations. It
// supports operations like opening files and implements io/fs.FS.
type Service struct {
	lazy *plumbing.LazyService[cmd.Runner, FS]
}

// GetFS returns a FS or an error if a filesystem client could not be initialized.
func (p *Service) GetFS() (FS, error) {
	fs, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get filesystem: %w", err)
	}
	return fs, nil
}

// FS provides easy access to the remote filesystem instance. It initializes the
// filesystem implementation if it has not been initialized yet. If the
// initialization fails, a FS implementation that errors out on all operations
// will be returned instead.
func (p *Service) FS() FS {
	fs, err := p.lazy.Get()
	if err != nil {
		errRunner := cmd.NewErrorExecutor(err)
		return NewPosixFS(errRunner)
	}
	return fs
}

// NewRemoteFSService creates a new instance of Service with the
// provided remotefs Provider and runner.
func NewRemoteFSService(provider RemoteFSProvider, runner cmd.Runner) *Service {
	return &Service{plumbing.NewLazyService[cmd.Runner, FS](provider, runner)}
}
