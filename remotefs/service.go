package remotefs

import (
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// Provider provides a unified interface to interact with the filesystem on a
// remote host. It ensures that a suitable remotefs.FS implementation is
// lazily initialized and made available for filesystem operations. It
// supports operations like opening files and implements io/fs.FS.
type Provider struct {
	lazy *plumbing.LazyService[cmd.Runner, FS]
}

// GetFS returns a FS or an error if a filesystem client could not be initialized.
func (p *Provider) GetFS() (FS, error) {
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
func (p *Provider) FS() FS {
	fs, err := p.lazy.Get()
	if err != nil {
		errRunner := cmd.NewErrorExecutor(err)
		return NewPosixFS(errRunner)
	}
	return fs
}

// NewRemoteFSProvider creates a new instance of Provider with the
// provided remotefs RemoteFSFactory and runner.
func NewRemoteFSProvider(factory RemoteFSFactory, runner cmd.Runner) *Provider {
	return &Provider{plumbing.NewLazyService[cmd.Runner, FS](factory, runner)}
}
