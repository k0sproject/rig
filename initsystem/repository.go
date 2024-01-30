package initsystem

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/k0sproject/rig/exec"
)

// ServiceManager defines the methods for interacting with an init system like OpenRC.
type ServiceManager interface {
	StartService(ctx context.Context, h exec.ContextRunner, s string) error
	StopService(ctx context.Context, h exec.ContextRunner, s string) error
	ServiceScriptPath(ctx context.Context, h exec.ContextRunner, s string) (string, error)
	EnableService(ctx context.Context, h exec.ContextRunner, s string) error
	DisableService(ctx context.Context, h exec.ContextRunner, s string) error
	ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool
}

type ServiceManagerRestarter interface {
	RestartService(ctx context.Context, h exec.ContextRunner, s string) error
}

type ServiceManagerReloader interface {
	DaemonReload(ctx context.Context, h exec.ContextRunner) error
}

type ServiceEnvironmentManager interface {
	ServiceEnvironmentPath(ctx context.Context, h exec.ContextRunner, s string) (string, error)
	ServiceEnvironmentContent(env map[string]string) string
}

type ServiceManagerFactory func(c exec.ContextRunner) ServiceManager

var (
	DefaultInitSystemRepository = NewRepository()
	repository                  atomic.Value
)

func init() {
	RegisterSystemd(DefaultInitSystemRepository)
	RegisterOpenRC(DefaultInitSystemRepository)
	RegisterUpstart(DefaultInitSystemRepository)
	RegisterSysVinit(DefaultInitSystemRepository)
	RegisterWinSCM(DefaultInitSystemRepository)
	RegisterRunit(DefaultInitSystemRepository)
	RegisterLaunchd(DefaultInitSystemRepository)

	SetRepository(DefaultInitSystemRepository)
}

func GetRepository() *Repository {
	return repository.Load().(*Repository)
}

func SetRepository(r *Repository) {
	repository.Store(r)
}

type Repository struct {
	systems []ServiceManagerFactory
}

func (r *Repository) Register(factory ServiceManagerFactory) {
	r.systems = append(r.systems, factory)
}

func (r *Repository) Get(c exec.ContextRunner) (ServiceManager, error) {
	for _, factory := range r.systems {
		system := factory(c)
		if system != nil {
			return system, nil
		}
	}
	return nil, fmt.Errorf("no init system found")
}

func NewRepository() *Repository {
	return &Repository{}
}
