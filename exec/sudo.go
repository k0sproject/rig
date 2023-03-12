package exec

import (
	"fmt"
	"sync/atomic"
)

var ErrNoSudoFound = fmt.Errorf("no usable sudo method found")

type SudoProvider interface {
	Check(r *Runner) bool
	Sudo(cmd string) (string, error)
}

type SudoProviderRepository interface {
	Find(r *Runner) (SudoProvider, error)
	Register(string, SudoProvider)
}

type InMemorySudoProviderRepository struct {
	providers map[string]SudoProvider
}

func (i InMemorySudoProviderRepository) Find(r *Runner) (SudoProvider, error) {
	for _, p := range i.providers {
		if p.Check(r) {
			return p, nil
		}
	}
	return nil, ErrNoSudoFound
}

func (i InMemorySudoProviderRepository) Register(name string, p SudoProvider) {
	i.providers[name] = p
}

var defaultSudoProviderRepository atomic.Value

func DefaultSudoProviderRepository() SudoProviderRepository {
	return defaultSudoProviderRepository.Load().(SudoProviderRepository)
}

func init() {
	defaultRepo := &InMemorySudoProviderRepository{}
	defaultRepo.Register("nop", NopSudo{})
	defaultRepo.Register("sudo", SudoDoas{command: "sudo"})
	defaultRepo.Register("doas", SudoDoas{command: "doas"})
	defaultRepo.Register("runas", WindowsNop{})
	defaultSudoProviderRepository.Store(defaultRepo)
}
