package packagemanager

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

type PackageManager interface {
	Install(ctx context.Context, packageNames ...string) error
	Remove(ctx context.Context, packageNames ...string) error
	Update(ctx context.Context) error
}

var (
	DefaultPackageManagerRepository = NewRepository()
	repository                      atomic.Value
)

func init() {
	RegisterApk(DefaultPackageManagerRepository)
	RegisterApt(DefaultPackageManagerRepository)
	RegisterYum(DefaultPackageManagerRepository)
	RegisterDnf(DefaultPackageManagerRepository)
	RegisterPacman(DefaultPackageManagerRepository)
	RegisterZypper(DefaultPackageManagerRepository)
	RegisterWindowsMultiManager(DefaultPackageManagerRepository)
	RegisterHomebrew(DefaultPackageManagerRepository)
	RegisterMacports(DefaultPackageManagerRepository)

	SetRepository(DefaultPackageManagerRepository)
}

func SetRepository(r *Repository) {
	repository.Store(r)
}

func GetRepository() *Repository {
	return repository.Load().(*Repository)
}

type PackageManagerFactoryFunc func(c exec.ContextRunner) PackageManager

type Repository struct {
	managers []PackageManagerFactoryFunc
}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) Register(factory PackageManagerFactoryFunc) {
	r.managers = append(r.managers, factory)
}

func (r *Repository) Get(c exec.ContextRunner) (PackageManager, error) {
	for _, builder := range r.managers {
		if mgr := builder(c); mgr != nil {
			return mgr, nil
		}
	}
	return nil, fmt.Errorf("no supported package manager found")
}

func (r *Repository) getAll(c exec.ContextRunner) []PackageManager {
	var managers []PackageManager
	for _, builder := range r.managers {
		if mgr := builder(c); mgr != nil {
			managers = append(managers, mgr)
		}
	}
	return managers
}

func buildCommand(basecmd, keyword string, packages ...string) string {
	cmd := &strings.Builder{}
	cmd.WriteString(basecmd)
	cmd.WriteRune(' ')
	cmd.WriteString(keyword)
	for _, pkg := range packages {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}
	return cmd.String()
}
