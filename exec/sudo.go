package exec

import (
	"fmt"
	"sync/atomic"
)

var (
	ErrNoSudoFound                = fmt.Errorf("no usable sudo method found")
	defaultSudoProviderRepository atomic.Value
)

type PasswordCallback func() (string, error)

type SudoProvider interface {
	New(*Runner, PasswordCallback) (SudoFn, error)
}

type SudoProviderRepository interface {
	Find(*Runner, PasswordCallback) (SudoFn, error)
}

type sudoProviderRepository struct {
	providers map[string]SudoProvider
}

func (i sudoProviderRepository) Find(r *Runner, cb PasswordCallback) (SudoFn, error) {
	for _, p := range i.providers {
		if fn, err := p.New(r, cb); err == nil {
			return fn, nil
		}
	}
	return nil, ErrNoSudoFound
}

func (i sudoProviderRepository) Register(name string, p SudoProvider) {
	i.providers[name] = p
}

func DefaultSudoProviderRepository() *SudoProviderRepository {
	return defaultSudoProviderRepository.Load().(*SudoProviderRepository)
}

func init() {
	defaultRepo := &sudoProviderRepository{}
	defaultRepo.Register("nop", NopSudo{})
	defaultRepo.Register("sudo", SudoDoas{command: "sudo"})
	defaultRepo.Register("doas", SudoDoas{command: "doas"})
	defaultSudoProviderRepository.Store(defaultRepo)
}
