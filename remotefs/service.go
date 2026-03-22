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

// FS returns a FS or an error if a filesystem client could not be initialized.
func (p *Provider) FS() (FS, error) {
	fs, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get filesystem: %w", err)
	}
	return fs, nil
}

// NewRemoteFSProvider creates a new instance of Provider with the
// provided FSProvider function and runner.
func NewRemoteFSProvider(get FSProvider, runner cmd.Runner) *Provider {
	return &Provider{plumbing.NewLazyService(get, runner)}
}
